// Package server implements ticker.v1.PriceService over the simulated market.
package server

import (
	"context"
	"errors"
	"io"
	"slices"
	"sync/atomic"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	tickerv1 "github.com/EquinoxWN/grpc-streaming-service/gen/ticker/v1"
	"github.com/EquinoxWN/grpc-streaming-service/internal/market"
)

// Request limits; anything outside them is InvalidArgument.
const (
	MaxSymbols        = 20
	MinInterval       = 10 * time.Millisecond
	MaxInterval       = 60 * time.Second
	DefaultInterval   = 200 * time.Millisecond
	MaxUpdates        = 100_000
	MaxRecvMsgBytes   = 64 << 10
	MaxStreamsPerConn = 256
)

// Service is the PriceService implementation.
type Service struct {
	tickerv1.UnimplementedPriceServiceServer
	market *market.Market
	active atomic.Int64 // open streams, for tests and metrics
}

// New returns a service over a market.
func New(m *market.Market) *Service { return &Service{market: m} }

// ActiveStreams reports how many streaming calls are running.
func (s *Service) ActiveStreams() int64 { return s.active.Load() }

// NewGRPCServer returns a server with message, stream and keepalive limits, the service and health.
func NewGRPCServer(s *Service) *grpc.Server {
	g := grpc.NewServer(
		grpc.MaxRecvMsgSize(MaxRecvMsgBytes),
		grpc.MaxConcurrentStreams(MaxStreamsPerConn),
		grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{MinTime: 10 * time.Second, PermitWithoutStream: false}),
		grpc.KeepaliveParams(keepalive.ServerParameters{MaxConnectionIdle: 5 * time.Minute}),
	)
	tickerv1.RegisterPriceServiceServer(g, s)
	h := health.NewServer()
	h.SetServingStatus(tickerv1.PriceService_ServiceDesc.ServiceName, healthpb.HealthCheckResponse_SERVING)
	healthpb.RegisterHealthServer(g, h)
	return g
}

// toProto converts a market quote.
func toProto(q market.Quote) *tickerv1.Quote {
	return &tickerv1.Quote{Symbol: q.Symbol, PriceMicros: q.PriceMicros, ChangeMicros: q.ChangeMicros,
		Tick: q.Tick, Time: timestamppb.New(q.Time)}
}

// checkSymbols validates a symbol list: listed, distinct, at most MaxSymbols.
func (s *Service) checkSymbols(symbols []string, allowEmpty bool) error {
	if len(symbols) == 0 && !allowEmpty {
		return status.Error(codes.InvalidArgument, "at least one symbol is required")
	}
	if len(symbols) > MaxSymbols {
		return status.Errorf(codes.InvalidArgument, "%d symbols, at most %d", len(symbols), MaxSymbols)
	}
	seen := map[string]bool{}
	for _, sym := range symbols {
		if seen[sym] {
			return status.Errorf(codes.InvalidArgument, "symbol %q repeated", truncate(sym))
		}
		seen[sym] = true
		if !s.market.Has(sym) {
			return status.Errorf(codes.NotFound, "unknown symbol %q", truncate(sym))
		}
	}
	return nil
}

// interval converts and bounds a requested interval; 0 gives the fallback.
func interval(ms uint32, fallback time.Duration) (time.Duration, error) {
	if ms == 0 {
		return fallback, nil
	}
	d := time.Duration(ms) * time.Millisecond
	if d < MinInterval || d > MaxInterval {
		return 0, status.Errorf(codes.InvalidArgument, "interval_ms %d out of range %d..%d", ms, MinInterval.Milliseconds(), MaxInterval.Milliseconds())
	}
	return d, nil
}

// truncate keeps error messages bounded when clients send long strings.
func truncate(s string) string {
	if len(s) > 16 {
		return s[:16] + "..."
	}
	return s
}

// GetQuote returns the latest quote for one symbol.
func (s *Service) GetQuote(_ context.Context, req *tickerv1.GetQuoteRequest) (*tickerv1.GetQuoteResponse, error) {
	if err := s.checkSymbols([]string{req.GetSymbol()}, false); err != nil {
		return nil, err
	}
	q, err := s.market.Quote(req.GetSymbol())
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &tickerv1.GetQuoteResponse{Quote: toProto(q)}, nil
}

// StreamQuotes sends one quote per symbol each round until max_updates or cancellation.
func (s *Service) StreamQuotes(req *tickerv1.StreamQuotesRequest, stream grpc.ServerStreamingServer[tickerv1.StreamQuotesResponse]) error {
	if err := s.checkSymbols(req.GetSymbols(), false); err != nil {
		return err
	}
	every, err := interval(req.GetIntervalMs(), DefaultInterval)
	if err != nil {
		return err
	}
	limit := int(req.GetMaxUpdates())
	if limit > MaxUpdates {
		return status.Errorf(codes.InvalidArgument, "max_updates %d above %d", limit, MaxUpdates)
	}
	s.active.Add(1)
	defer s.active.Add(-1)
	ctx := stream.Context()
	t := time.NewTicker(every)
	defer t.Stop()
	sent := 0
	for {
		for _, sym := range req.GetSymbols() {
			q, err := s.market.Quote(sym)
			if err != nil {
				return status.Error(codes.Internal, err.Error())
			}
			if err := stream.Send(&tickerv1.StreamQuotesResponse{Quote: toProto(q)}); err != nil {
				return err
			}
			sent++
			if limit > 0 && sent >= limit {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return status.FromContextError(ctx.Err()).Err()
		case <-t.C:
		}
	}
}

// watchMsg is one client request or the end of the client's side.
type watchMsg struct {
	req *tickerv1.WatchQuotesRequest
	err error
}

// WatchQuotes streams quotes for a set the client changes while the stream runs.
func (s *Service) WatchQuotes(stream grpc.BidiStreamingServer[tickerv1.WatchQuotesRequest, tickerv1.WatchQuotesResponse]) error {
	s.active.Add(1)
	defer s.active.Add(-1)
	ctx, cancel := context.WithCancel(stream.Context())
	defer cancel()
	in := make(chan watchMsg, 1)
	go func() {
		for {
			req, err := stream.Recv()
			select {
			case in <- watchMsg{req, err}:
			case <-ctx.Done():
				return
			}
			if err != nil {
				return
			}
		}
	}()
	var watched []string
	every := DefaultInterval
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return status.FromContextError(ctx.Err()).Err()
		case m := <-in:
			if errors.Is(m.err, io.EOF) {
				return nil // the client closed its side: end the stream cleanly
			}
			if m.err != nil {
				return m.err
			}
			next, d, err := s.apply(watched, every, m.req)
			if err != nil {
				return err
			}
			if d != every {
				every = d
				t.Reset(every)
			}
			watched = next
			sub := &tickerv1.WatchQuotesResponse{Event: &tickerv1.WatchQuotesResponse_Subscription{
				Subscription: &tickerv1.Subscription{Symbols: slices.Clone(watched)}}}
			if err := stream.Send(sub); err != nil {
				return err
			}
		case <-t.C:
			for _, sym := range watched {
				q, err := s.market.Quote(sym)
				if err != nil {
					return status.Error(codes.Internal, err.Error())
				}
				if err := stream.Send(&tickerv1.WatchQuotesResponse{Event: &tickerv1.WatchQuotesResponse_Quote{Quote: toProto(q)}}); err != nil {
					return err
				}
			}
		}
	}
}

// apply returns the watched set and interval after one request, or InvalidArgument/NotFound.
func (s *Service) apply(watched []string, every time.Duration, req *tickerv1.WatchQuotesRequest) ([]string, time.Duration, error) {
	if err := s.checkSymbols(req.GetSubscribe(), true); err != nil {
		return nil, 0, err
	}
	if len(req.GetUnsubscribe()) > MaxSymbols {
		return nil, 0, status.Errorf(codes.InvalidArgument, "%d symbols to unsubscribe, at most %d", len(req.GetUnsubscribe()), MaxSymbols)
	}
	d, err := interval(req.GetIntervalMs(), every)
	if err != nil {
		return nil, 0, err
	}
	next := slices.Clone(watched)
	for _, sym := range req.GetSubscribe() {
		if !slices.Contains(next, sym) {
			next = append(next, sym)
		}
	}
	next = slices.DeleteFunc(next, func(sym string) bool { return slices.Contains(req.GetUnsubscribe(), sym) })
	if len(next) > MaxSymbols {
		return nil, 0, status.Errorf(codes.InvalidArgument, "watching %d symbols, at most %d", len(next), MaxSymbols)
	}
	slices.Sort(next)
	return next, d, nil
}
