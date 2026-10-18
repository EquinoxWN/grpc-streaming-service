# Security policy

## Reporting a vulnerability

Please report security problems privately through GitHub:
**Security > Report a vulnerability** on this repository
(`https://github.com/EquinoxWN/grpc-streaming-service/security/advisories/new`). Do not open a public issue.

Include what you found, how to reproduce it, and the impact you expect. You will get an answer
within 7 days. Fixes are released as soon as they are ready, and you are credited unless you ask
not to be.

## Supported versions

This project is pre-1.0. Only the latest commit on `main` receives fixes.

## Scope

- **In scope:** requests that make the server use unbounded memory or CPU, streams that keep
  running after the client cancels, crashes from crafted messages, and the server listening beyond
  loopback.
- **Out of scope:** the simulated prices (they are not market data), and plaintext transport on
  loopback, which M1 documents as a limit.

## How this repository protects itself

- The server refuses non-loopback addresses; M1 has no TLS, so it never listens on a network.
- Receive size is capped at 64 KiB, concurrent streams at 256 per connection, symbols at 20 per
  request, intervals at 10 ms to 60 s and stream length at 100,000 quotes; keepalive pings are
  rate-limited.
- Streaming handlers stop when the client cancels or its deadline passes.
- Error messages echo at most 16 characters of client input.
- buf is downloaded from the official release and checked against a pinned SHA-256; the code
  generators are pinned by version; generated code is checked against the contract in CI.
- Every GitHub Action is pinned to a full commit SHA, and workflows run with read-only
  permissions and without persisted credentials.
- Dependabot update pull requests are paused (`open-pull-requests-limit: 0`) so the repository keeps a single `main` branch; dependencies are updated by hand and checked by the CI audit.
- CI runs lint, tests and a known-vulnerability check (govulncheck) on every push and pull request.
