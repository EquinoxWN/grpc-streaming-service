# ADR 0002: Stream the latest price each round instead of queueing every tick

- **Status:** Accepted

## Context

`StreamQuotes` and `WatchQuotes` send quotes at a client-chosen interval (10 ms to 60 s), while the
market moves on its own step (100 ms by default). Two ways to feed a stream:

1. Fan-out: the market pushes every tick into a channel per subscriber, and the handler sends
   everything it receives.
2. Sampling: each round, the handler reads the current quote for each watched symbol and sends it.

With fan-out, a client that reads slowly (or asks for a long interval) makes its channel fill up,
and the server must decide whether to block the market, drop ticks or disconnect the client. That
policy is the M2 backpressure work.

## Decision

M1 samples. Each round, the handler asks the market for the current quote (the walk advances
lazily to the current tick) and sends one quote per symbol. gRPC flow control already blocks
`Send` when the client stops reading, so a slow client slows only its own handler: nothing queues
on the server beyond gRPC's own window.

## Consequences

- Memory per stream is constant: no per-subscriber queue can grow.
- Quotes carry the market tick, so a client sees when ticks were skipped (tick 28, 30, 32 at a
  100 ms interval on a 50 ms market in the demo).
- Clients that need every tick cannot get them from M1. M2 adds an opt-in buffered mode with a
  bounded queue and an explicit overflow policy, measured with a slow-consumer test.
- The market does work only when someone asks for a quote, so an idle server uses no CPU for
  price generation.
