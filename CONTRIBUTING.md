# Contributing

Thanks for helping. This is a Go gRPC project with a Buf-managed contract; issues and pull
requests are welcome.

## Set up and check your change

Needs Go 1.26+, make, and bash with curl (Git Bash on Windows). Downloads and caches stay in
git-ignored folders inside the repo (`.tmp/`).

```bash
make setup            # Go modules, buf (SHA-256 checked), protoc-gen-go, protoc-gen-go-grpc
make lint             # gofmt, go vet, buf lint, buf format
make generate         # regenerate gen/ after changing proto/
make generate-check   # gen/ must match the contract
make breaking         # compatibility with contract/ticker-v1.binpb
make test             # Go tests (race detector where cgo is available)
make audit            # govulncheck, as in CI
make demo             # server and client over loopback
```

A pull request is ready when `make ci` passes and CI is green.

## Guidelines

- Change `proto/` first, run `make generate`, and commit the generated code with it.
- Never edit `contract/ticker-v1.binpb` by hand. A breaking change needs a new package version
  (`ticker.v2`) and an ADR; `make baseline` is only for an accepted, versioned change.
- Every handler must stop on context cancellation and validate its input; add a test for both.
- Add or update a test for every behaviour change, and update `docs/results/` when numbers change.
- Significant design changes need an ADR in `docs/adr/` (copy `0001` for the format).
- Keep function comments to one short phrase; let names and tests explain the rest.
- Report security problems privately, as described in [SECURITY.md](SECURITY.md).

By contributing you agree that your work is released under the [MIT License](LICENSE).
