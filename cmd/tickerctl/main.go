// Command tickerctl calls the three PriceService endpoints: quote (unary), stream (server
// streaming) and watch (bidirectional).
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	tickerv1 "github.com/EquinoxWN/grpc-streaming-service/gen/ticker/v1"
)

const usage = `usage: tickerctl [-addr host:port] [-timeout d] <command>
  quote SYMBOL                                   one quote (unary)
  stream -symbols A,B [-interval-ms N] [-n N]    N quotes (server streaming)
  watch -add A,B [-then-add C] [-then-drop A] [-n N]
                                                 change the set while quotes arrive (bidirectional)`

// main runs one command and exits non-zero on error.
func main() {
	if err := run(context.Background(), os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "tickerctl:", err)
		os.Exit(1)
	}
}

// run parses global flags, dials and dispatches.
func run(ctx context.Context, args []string, out io.Writer) error {
	fs := flag.NewFlagSet("tickerctl", flag.ContinueOnError)
	addr := fs.String("addr", "127.0.0.1:50051", "server address")
	timeout := fs.Duration("timeout", 10*time.Second, "deadline for the whole call")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() == 0 {
		return errors.New(usage)
	}
	conn, err := grpc.NewClient(*addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return err
	}
	defer conn.Close()
	client := tickerv1.NewPriceServiceClient(conn)
	ctx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()
	rest := fs.Args()[1:]
	switch fs.Arg(0) {
	case "quote":
		return quote(ctx, client, rest, out)
	case "stream":
		return stream(ctx, client, rest, out)
	case "watch":
		return watch(ctx, client, rest, out)
	}
	return errors.New(usage)
}

// format prints a quote with the price in currency units.
func format(q *tickerv1.Quote) string {
	return fmt.Sprintf("%-5s tick %-6d %12.6f (%+.6f)", q.GetSymbol(), q.GetTick(),
		float64(q.GetPriceMicros())/1e6, float64(q.GetChangeMicros())/1e6)
}

// split turns "A,B" into a list, dropping empty parts.
func split(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// quote calls GetQuote.
func quote(ctx context.Context, c tickerv1.PriceServiceClient, args []string, out io.Writer) error {
	if len(args) != 1 {
		return errors.New("quote needs exactly one SYMBOL")
	}
	resp, err := c.GetQuote(ctx, &tickerv1.GetQuoteRequest{Symbol: args[0]})
	if err != nil {
		return err
	}
	fmt.Fprintln(out, "quote ", format(resp.GetQuote()))
	return nil
}

// stream calls StreamQuotes and prints every quote.
func stream(ctx context.Context, c tickerv1.PriceServiceClient, args []string, out io.Writer) error {
	fs := flag.NewFlagSet("stream", flag.ContinueOnError)
	symbols := fs.String("symbols", "ACME", "comma-separated symbols")
	every := fs.Uint("interval-ms", 100, "milliseconds between rounds")
	n := fs.Uint("n", 10, "quotes to receive")
	if err := fs.Parse(args); err != nil {
		return err
	}
	s, err := c.StreamQuotes(ctx, &tickerv1.StreamQuotesRequest{Symbols: split(*symbols), IntervalMs: uint32(*every), MaxUpdates: uint32(*n)})
	if err != nil {
		return err
	}
	got := 0
	for {
		resp, err := s.Recv()
		if errors.Is(err, io.EOF) {
			fmt.Fprintf(out, "stream ended by server after %d quotes\n", got)
			return nil
		}
		if err != nil {
			return err
		}
		got++
		fmt.Fprintln(out, "stream", format(resp.GetQuote()))
	}
}

// watch opens WatchQuotes, changes the set after a few quotes, then closes its side.
func watch(ctx context.Context, c tickerv1.PriceServiceClient, args []string, out io.Writer) error {
	fs := flag.NewFlagSet("watch", flag.ContinueOnError)
	add := fs.String("add", "ACME,BLUE", "symbols to watch first")
	thenAdd := fs.String("then-add", "CRNA", "symbols to add after n quotes")
	thenDrop := fs.String("then-drop", "ACME", "symbols to drop after n quotes")
	every := fs.Uint("interval-ms", 100, "milliseconds between rounds")
	n := fs.Int("n", 4, "quotes per phase")
	if err := fs.Parse(args); err != nil {
		return err
	}
	s, err := c.WatchQuotes(ctx)
	if err != nil {
		return err
	}
	phases := []*tickerv1.WatchQuotesRequest{
		{Subscribe: split(*add), IntervalMs: uint32(*every)},
		{Subscribe: split(*thenAdd), Unsubscribe: split(*thenDrop)},
	}
	if err := s.Send(phases[0]); err != nil {
		return err
	}
	phase, quotes := 0, 0
	for {
		resp, err := s.Recv()
		if errors.Is(err, io.EOF) {
			fmt.Fprintln(out, "watch  ended by server after the client closed its side")
			return nil
		}
		if err != nil {
			return err
		}
		if sub := resp.GetSubscription(); sub != nil {
			fmt.Fprintf(out, "watch  now watching %v\n", sub.GetSymbols())
			continue
		}
		fmt.Fprintln(out, "watch ", format(resp.GetQuote()))
		if quotes++; quotes < *n {
			continue
		}
		quotes = 0
		if phase++; phase < len(phases) {
			if err := s.Send(phases[phase]); err != nil {
				return err
			}
		} else if phase == len(phases) {
			if err := s.CloseSend(); err != nil {
				return err
			}
		}
	}
}
