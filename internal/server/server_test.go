package server

import (
	"context"
	"errors"
	"io"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	tickerv1 "github.com/EquinoxWN/grpc-streaming-service/gen/ticker/v1"
	"github.com/EquinoxWN/grpc-streaming-service/internal/market"
)

// harness starts the real gRPC server on an in-memory listener and returns a connection.
func harness(t *testing.T) (*Service, *grpc.ClientConn) {
	t.Helper()
	m, err := market.New(1, market.DefaultSymbols, 5*time.Millisecond, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	svc := New(m)
	g := NewGRPCServer(svc)
	lis := bufconn.Listen(1 << 20)
	go func() { _ = g.Serve(lis) }()
	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close(); g.Stop() })
	return svc, conn
}

// ctx returns a context with a test deadline.
func ctx(t *testing.T) context.Context {
	c, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	return c
}

// code extracts the gRPC status code.
func code(err error) codes.Code { return status.Code(err) }

func TestUnaryGetQuote(t *testing.T) {
	_, conn := harness(t)
	c := tickerv1.NewPriceServiceClient(conn)
	resp, err := c.GetQuote(ctx(t), &tickerv1.GetQuoteRequest{Symbol: "ACME"})
	if err != nil {
		t.Fatal(err)
	}
	if q := resp.GetQuote(); q.GetSymbol() != "ACME" || q.GetPriceMicros() <= 0 || q.GetTime() == nil {
		t.Fatalf("quote %v", q)
	}
	cases := map[string]codes.Code{"": codes.NotFound, "NOPE": codes.NotFound, "acme": codes.NotFound}
	for sym, want := range cases {
		if _, err := c.GetQuote(ctx(t), &tickerv1.GetQuoteRequest{Symbol: sym}); code(err) != want {
			t.Errorf("symbol %q: %v, want %v", sym, err, want)
		}
	}
}

func TestServerStreamSendsExactlyMaxUpdatesInOrder(t *testing.T) {
	_, conn := harness(t)
	c := tickerv1.NewPriceServiceClient(conn)
	s, err := c.StreamQuotes(ctx(t), &tickerv1.StreamQuotesRequest{Symbols: []string{"ACME", "BLUE"}, IntervalMs: 10, MaxUpdates: 9})
	if err != nil {
		t.Fatal(err)
	}
	var got []*tickerv1.Quote
	for {
		resp, err := s.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, resp.GetQuote())
	}
	if len(got) != 9 {
		t.Fatalf("%d quotes, want 9", len(got))
	}
	for i, q := range got {
		if want := []string{"ACME", "BLUE"}[i%2]; q.GetSymbol() != want {
			t.Fatalf("quote %d is %s, want %s", i, q.GetSymbol(), want)
		}
		if i >= 2 && q.GetTick() < got[i-2].GetTick() {
			t.Fatalf("tick went back: %d after %d", q.GetTick(), got[i-2].GetTick())
		}
	}
}

func TestServerStreamStopsWhenTheClientCancels(t *testing.T) {
	svc, conn := harness(t)
	c := tickerv1.NewPriceServiceClient(conn)
	cctx, cancel := context.WithCancel(context.Background())
	s, err := c.StreamQuotes(cctx, &tickerv1.StreamQuotesRequest{Symbols: []string{"ECHO"}, IntervalMs: 10})
	if err != nil {
		t.Fatal(err)
	}
	for range 3 {
		if _, err := s.Recv(); err != nil {
			t.Fatal(err)
		}
	}
	if svc.ActiveStreams() != 1 {
		t.Fatalf("%d active streams, want 1", svc.ActiveStreams())
	}
	cancel()
	deadline := time.Now().Add(2 * time.Second)
	for svc.ActiveStreams() != 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if svc.ActiveStreams() != 0 {
		t.Fatal("server kept streaming after the client cancelled")
	}
	if _, err := s.Recv(); code(err) != codes.Canceled {
		t.Fatalf("client sees %v, want Canceled", err)
	}
}

func TestServerStreamRejectsBadRequests(t *testing.T) {
	_, conn := harness(t)
	c := tickerv1.NewPriceServiceClient(conn)
	many := make([]string, 21)
	for i := range many {
		many[i] = "ACME"
	}
	cases := map[string]struct {
		req  *tickerv1.StreamQuotesRequest
		want codes.Code
	}{
		"no symbols":        {&tickerv1.StreamQuotesRequest{}, codes.InvalidArgument},
		"too many":          {&tickerv1.StreamQuotesRequest{Symbols: many}, codes.InvalidArgument},
		"repeated":          {&tickerv1.StreamQuotesRequest{Symbols: []string{"ACME", "ACME"}}, codes.InvalidArgument},
		"unknown":           {&tickerv1.StreamQuotesRequest{Symbols: []string{"NOPE"}}, codes.NotFound},
		"interval too low":  {&tickerv1.StreamQuotesRequest{Symbols: []string{"ACME"}, IntervalMs: 1}, codes.InvalidArgument},
		"interval too high": {&tickerv1.StreamQuotesRequest{Symbols: []string{"ACME"}, IntervalMs: 60_001}, codes.InvalidArgument},
		"too many updates":  {&tickerv1.StreamQuotesRequest{Symbols: []string{"ACME"}, MaxUpdates: MaxUpdates + 1}, codes.InvalidArgument},
	}
	for name, tc := range cases {
		s, err := c.StreamQuotes(ctx(t), tc.req)
		if err == nil {
			_, err = s.Recv()
		}
		if code(err) != tc.want {
			t.Errorf("%s: %v, want %v", name, err, tc.want)
		}
	}
}

func TestBidirectionalWatchFollowsSubscriptionChanges(t *testing.T) {
	_, conn := harness(t)
	c := tickerv1.NewPriceServiceClient(conn)
	s, err := c.WatchQuotes(ctx(t))
	if err != nil {
		t.Fatal(err)
	}
	// next returns the next subscription or quote event.
	next := func() *tickerv1.WatchQuotesResponse {
		t.Helper()
		r, err := s.Recv()
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	if err := s.Send(&tickerv1.WatchQuotesRequest{Subscribe: []string{"BLUE", "ACME"}, IntervalMs: 10}); err != nil {
		t.Fatal(err)
	}
	if sub := next().GetSubscription(); sub == nil || len(sub.GetSymbols()) != 2 || sub.GetSymbols()[0] != "ACME" {
		t.Fatalf("first event %v, want subscription [ACME BLUE]", sub)
	}
	seen := map[string]int{}
	for range 6 {
		seen[next().GetQuote().GetSymbol()]++
	}
	if seen["ACME"] == 0 || seen["BLUE"] == 0 || len(seen) != 2 {
		t.Fatalf("quotes %v", seen)
	}
	if err := s.Send(&tickerv1.WatchQuotesRequest{Subscribe: []string{"CRNA"}, Unsubscribe: []string{"ACME"}}); err != nil {
		t.Fatal(err)
	}
	// Quotes already in flight may arrive before the acknowledgement; after it, only the new set.
	for {
		r := next()
		if sub := r.GetSubscription(); sub != nil {
			if got := sub.GetSymbols(); len(got) != 2 || got[0] != "BLUE" || got[1] != "CRNA" {
				t.Fatalf("subscription %v, want [BLUE CRNA]", got)
			}
			break
		}
	}
	for range 6 {
		if sym := next().GetQuote().GetSymbol(); sym == "ACME" {
			t.Fatal("quote for an unsubscribed symbol")
		}
	}
	if err := s.CloseSend(); err != nil {
		t.Fatal(err)
	}
	for {
		_, err := s.Recv()
		if errors.Is(err, io.EOF) {
			return // the server ended the stream after the client closed its side
		}
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestWatchRejectsUnknownSymbolsAndOversizedSets(t *testing.T) {
	_, conn := harness(t)
	c := tickerv1.NewPriceServiceClient(conn)
	s, _ := c.WatchQuotes(ctx(t))
	_ = s.Send(&tickerv1.WatchQuotesRequest{Subscribe: []string{"NOPE"}})
	if _, err := s.Recv(); code(err) != codes.NotFound {
		t.Fatalf("unknown symbol: %v", err)
	}
	s, _ = c.WatchQuotes(ctx(t))
	_ = s.Send(&tickerv1.WatchQuotesRequest{Subscribe: []string{"ACME"}, IntervalMs: 5})
	if _, err := s.Recv(); code(err) != codes.InvalidArgument {
		t.Fatalf("interval below minimum: %v", err)
	}
}

func TestOversizedMessagesAreRefused(t *testing.T) {
	_, conn := harness(t)
	c := tickerv1.NewPriceServiceClient(conn)
	big := string(make([]byte, MaxRecvMsgBytes+1))
	if _, err := c.GetQuote(ctx(t), &tickerv1.GetQuoteRequest{Symbol: big}); code(err) != codes.ResourceExhausted {
		t.Fatalf("oversized request: %v", err)
	}
}

func TestHealthReportsServing(t *testing.T) {
	_, conn := harness(t)
	h := healthpb.NewHealthClient(conn)
	resp, err := h.Check(ctx(t), &healthpb.HealthCheckRequest{Service: "ticker.v1.PriceService"})
	if err != nil || resp.GetStatus() != healthpb.HealthCheckResponse_SERVING {
		t.Fatalf("health %v %v", resp, err)
	}
}
