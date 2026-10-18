package main

import (
	"bytes"
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/EquinoxWN/grpc-streaming-service/internal/market"
	"github.com/EquinoxWN/grpc-streaming-service/internal/server"
)

// serve starts the real server on a loopback TCP port and returns its address.
func serve(t *testing.T) string {
	t.Helper()
	m, err := market.New(1, market.DefaultSymbols, 10*time.Millisecond, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	g := server.NewGRPCServer(server.New(m))
	go func() { _ = g.Serve(lis) }()
	t.Cleanup(g.Stop)
	return lis.Addr().String()
}

// TestEndToEndOverTCP runs the CLI against the server for all three call types.
func TestEndToEndOverTCP(t *testing.T) {
	addr := serve(t)
	var out bytes.Buffer
	steps := [][]string{
		{"-addr", addr, "quote", "ACME"},
		{"-addr", addr, "stream", "-symbols", "ACME,BLUE", "-interval-ms", "20", "-n", "6"},
		{"-addr", addr, "watch", "-add", "ACME,BLUE", "-then-add", "CRNA", "-then-drop", "ACME", "-interval-ms", "20", "-n", "4"},
	}
	for _, args := range steps {
		if err := run(context.Background(), args, &out); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, out.String())
		}
	}
	text := out.String()
	for _, want := range []string{
		"quote  ACME",
		"stream ended by server after 6 quotes",
		"watch  now watching [ACME BLUE]",
		"watch  now watching [BLUE CRNA]",
		"watch  ended by server after the client closed its side",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("output lacks %q:\n%s", want, text)
		}
	}
	if n := strings.Count(text, "\nstream ACME") + strings.Count(text, "\nstream BLUE"); n != 6 {
		t.Errorf("%d streamed quote lines, want 6", n)
	}
}

func TestErrorsReachTheCaller(t *testing.T) {
	addr := serve(t)
	var out bytes.Buffer
	err := run(context.Background(), []string{"-addr", addr, "quote", "NOPE"}, &out)
	if err == nil || !strings.Contains(err.Error(), "NotFound") {
		t.Fatalf("unknown symbol: %v", err)
	}
	err = run(context.Background(), []string{"-addr", addr, "-timeout", "50ms", "stream", "-symbols", "ACME", "-n", "0"}, &out)
	if err == nil || !strings.Contains(err.Error(), "DeadlineExceeded") {
		t.Fatalf("deadline: %v", err)
	}
	if err := run(context.Background(), []string{"-addr", addr}, &out); err == nil {
		t.Fatal("missing command accepted")
	}
}
