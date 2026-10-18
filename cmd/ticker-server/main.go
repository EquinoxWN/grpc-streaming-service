// Command ticker-server serves ticker.v1.PriceService on a loopback address.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/EquinoxWN/grpc-streaming-service/internal/market"
	"github.com/EquinoxWN/grpc-streaming-service/internal/server"
)

// main starts the server and stops it gracefully on SIGINT or SIGTERM.
func main() {
	addr := flag.String("addr", "127.0.0.1:50051", "listen address (loopback only: M1 has no TLS)")
	seed := flag.Uint64("seed", 1, "market seed")
	step := flag.Duration("step", 100*time.Millisecond, "market tick")
	flag.Parse()
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	if err := run(*addr, *seed, *step, log); err != nil {
		log.Error("server failed", "err", err)
		os.Exit(1)
	}
}

// run listens, serves until a signal, then drains for up to 5 seconds.
func run(addr string, seed uint64, step time.Duration, log *slog.Logger) error {
	if err := loopback(addr); err != nil {
		return err
	}
	m, err := market.New(seed, market.DefaultSymbols, step, time.Now)
	if err != nil {
		return err
	}
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	g := server.NewGRPCServer(server.New(m))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		done := make(chan struct{})
		go func() { g.GracefulStop(); close(done) }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			g.Stop()
		}
	}()
	log.Info("serving", "addr", lis.Addr().String(), "symbols", m.Symbols())
	return g.Serve(lis)
}

// loopback refuses addresses other than 127.0.0.1, ::1 or localhost.
func loopback(addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return err
	}
	if host == "localhost" {
		return nil
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return nil
	}
	return fmt.Errorf("address %q is not loopback; M1 serves plaintext gRPC on loopback only", addr)
}
