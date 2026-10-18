# RFC 0001: grpc-streaming-service design

- **Status:** Accepted (M1 built)
- **Author:** EquinoxWN
- **Created:** 2026-10-18

## Problem (Define)

Real-time feeds (prices, notifications, telemetry) are usually built as streams, and the hard
parts are not the happy path: clients that change what they want while the stream runs, clients
that stop reading, cancelled calls that keep server work alive, and contract changes that silently
break old clients. A team needs a reference service that gets the contract and the three gRPC call
shapes right first, then adds deadlines, backpressure, browser access and load numbers on top.

Users: backend engineers designing a streaming API, reviewers who need to see how cancellation and
contract compatibility are handled, and anyone learning gRPC streaming in Go.

### Critical-to-quality measures

| CTQ | Target (M1) | How it is measured |
|---|---|---|
| Contract style | 0 `buf lint` findings (STANDARD rules), `buf format` clean | `make lint` in CI |
| Contract compatibility | 0 breaking changes against the committed baseline; 4 of 4 typical breaking edits refused, 1 of 1 compatible edit accepted | `scripts/breaking-check.sh` in CI; `buf breaking` against the base branch on pull requests |
| Generated code in sync | `gen/` identical to a fresh `buf generate` | `make generate-check` |
| Three call shapes work end to end | unary, server streaming and bidirectional calls answer over real TCP | `cmd/tickerctl` end-to-end test and `make demo` |
| No wasted work after cancel | streaming handlers return after the client cancels | `TestServerStreamStopsWhenTheClientCancels` (active stream count back to 0) |
| Bounded input | refused: over 20 symbols, repeated symbols, intervals outside 10 ms to 60 s, more than 100,000 updates, messages over 64 KiB | server tests |

## Goals

- M1: the `ticker.v1` contract in a Buf module with lint, format, generated-code drift and
  breaking-change checks; a price-ticker service with `GetQuote` (unary), `StreamQuotes` (server
  streaming) and `WatchQuotes` (bidirectional: the client adds and removes symbols while quotes
  arrive); a CLI client for all three.
- Deterministic prices (seeded), so tests and demos are reproducible.

## Non-goals

- Server-side default deadlines and deadline propagation to downstream calls (M2).
- Bounded per-subscriber buffers and slow-consumer handling beyond what M1's sampling gives (M2).
- Connect-RPC for browsers and ghz load tests (M3).
- TLS and authentication: M1 serves plaintext on loopback only and refuses other addresses.
- Real market data.

## Design

```
proto/ticker/v1/ticker.proto  --buf lint / format / breaking-->  contract/ticker-v1.binpb (baseline)
          | buf generate (local protoc-gen-go v1.36.12, protoc-gen-go-grpc v1.6.2)
          v
gen/ticker/v1  <--  internal/server (PriceService)  <--  internal/market (seeded random walk)
                         ^ gRPC over TCP (127.0.0.1)
                    cmd/tickerctl (quote | stream | watch)
```

- **Market:** each symbol follows a random walk; tick n's move is derived from SHA-256(seed, tick,
  symbol) and bounded to ±0.50% per tick. The walk advances lazily to the tick for the current
  time, so prices are the same for the same seed and time whoever asks.
- **GetQuote:** validates the symbol and returns the current quote.
- **StreamQuotes:** validates symbols, interval and limit, then sends one quote per symbol each
  round until `max_updates` or cancellation. It samples the latest price each round rather than
  queueing every tick ([ADR 0002](../adr/0002-sample-latest-price-per-round.md)).
- **WatchQuotes:** one goroutine reads client requests and hands them to the sending loop through
  a channel, so only one goroutine calls `Send`. Each request is acknowledged with the new sorted
  set; the stream ends cleanly when the client closes its side.
- **Server:** 64 KiB receive limit, 256 concurrent streams per connection, keepalive enforcement,
  standard gRPC health service, graceful stop on SIGTERM with a 5 s limit.
- **Contract gate:** a committed Buf image is the compatibility baseline
  ([ADR 0003](../adr/0003-committed-contract-baseline.md)); pull requests are also checked against
  their base branch.

## Alternatives considered

| Option | Why not (yet) |
|---|---|
| REST with Server-Sent Events | Simple for browsers, but one-way: the watch pattern needs a second channel for subscription changes, and there is no typed contract with a breaking-change checker. |
| WebSockets with JSON messages | Two-way, but message types, versioning and compatibility checks are all hand-made. |
| Push every tick through a buffered channel per subscriber | Delivers every tick but needs a policy for full buffers (drop, block, disconnect); this is M2's backpressure work, and M1's sampling avoids unbounded queues meanwhile. |
| protoc with checked-in plugins instead of Buf | Works, but no built-in lint rules, formatting or breaking-change detection. |
| Buf remote plugins (BSR) for generation | No local plugin install, but needs network access to the registry at generation time; local pinned plugins keep generation offline and reproducible. |

## Measurement plan (Measure)

- Local and CI: the check script (lint, format, drift, breaking gate, tests, govulncheck) and the
  demo log. Repeated runs of the streaming tests (5 times) to check for flakiness.
- M3: ghz throughput and latency per call shape, and a slow-consumer demo within a memory limit.

## Milestones

- **M1 (done):** RFC; Buf module with lint and breaking-change checks; unary, server-streaming and
  bidirectional endpoints with a CLI client.
- **M2:** deadlines on every call with cancellation propagation; flow control and bounded buffers
  for slow consumers.
- **M3:** Connect-RPC for browsers; ghz load tests; published load numbers and slow-consumer demo.

## Risks and open questions

- Sampling hides individual ticks from slow or slow-polling clients; clients that need every tick
  (for example to compute volume) need M2's buffered mode.
- The baseline image must be updated on purpose (`make baseline`) when a breaking change is
  accepted, ideally together with a new package version (`ticker.v2`).
- Plaintext gRPC is fine on loopback only; any other deployment needs TLS first.
