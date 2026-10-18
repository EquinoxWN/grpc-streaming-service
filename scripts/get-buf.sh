#!/usr/bin/env bash
# Installs pinned buf (SHA-256 checked) and the two Go code generators into .tmp/bin.
set -euo pipefail

BUF_VERSION=v1.73.0
PROTOC_GEN_GO=google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.12
PROTOC_GEN_GO_GRPC=google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.6.2

cd "$(dirname "$0")/.."
BIN=$PWD/.tmp/bin
mkdir -p "$BIN"

case "$(uname -s)" in
  Linux) ASSET=buf-Linux-x86_64 WANT=8f2986298ad08f0cc1bf999b9797b7c383adf32d7edf0f73d6f1e1a701baeac1 EXE= ;;
  MINGW*|MSYS*|CYGWIN*) ASSET=buf-Windows-x86_64.exe WANT=13542f2892c4f774150ddb525266d6421d457b3e741297056b64427853526e36 EXE=.exe ;;
  *) echo "unsupported OS $(uname -s)" >&2; exit 1 ;;
esac
[ "$(uname -m)" = x86_64 ] || { echo "only amd64 is pinned" >&2; exit 1; }

if [ ! -x "$BIN/buf$EXE" ]; then
  curl --proto '=https' --tlsv1.2 -fsSL --retry 3 -o "$BIN/buf$EXE.part" \
    "https://github.com/bufbuild/buf/releases/download/$BUF_VERSION/$ASSET"
  got=$(sha256sum "$BIN/buf$EXE.part" | cut -d' ' -f1)
  [ "$got" = "$WANT" ] || { echo "buf checksum mismatch: got $got, want $WANT" >&2; rm -f "$BIN/buf$EXE.part"; exit 1; }
  mv "$BIN/buf$EXE.part" "$BIN/buf$EXE" && chmod +x "$BIN/buf$EXE"
fi
GOBIN=$BIN go install "$PROTOC_GEN_GO"
GOBIN=$BIN go install "$PROTOC_GEN_GO_GRPC"
"$BIN/buf$EXE" --version
