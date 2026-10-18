#!/usr/bin/env bash
# Proves the contract gate works: the current proto must be compatible with the committed baseline,
# four typical breaking edits must be refused, and one compatible edit must pass.
set -euo pipefail
cd "$(dirname "$0")/.."
BUF=.tmp/bin/buf
[ -x "$BUF" ] || BUF=.tmp/bin/buf.exe
BASE=contract/ticker-v1.binpb
WORK=.tmp/breaking

echo "== buf breaking proto --against $BASE"
"$BUF" breaking --against "$BASE"
echo "current contract: compatible with the baseline"

# mutate NAME EXPECT SED_EXPR: copy the module, apply one edit, run buf breaking, check the verdict.
refused=0 accepted=0
mutate() {
  local name=$1 expect=$2 expr=$3 dir=$WORK/$1
  rm -rf "$dir" && mkdir -p "$dir" && cp -r proto buf.yaml "$dir/"
  sed -i "$expr" "$dir/proto/ticker/v1/ticker.proto"
  if cmp -s proto/ticker/v1/ticker.proto "$dir/proto/ticker/v1/ticker.proto"; then
    echo "mutation $name did not change the file" >&2; exit 1
  fi
  if out=$("$BUF" breaking "$dir" --against "$BASE" 2>&1); then
    [ "$expect" = pass ] || { echo "FAIL $name: breaking change was not detected" >&2; exit 1; }
    echo "ok   $name: compatible change accepted"
    accepted=$((accepted + 1))
  else
    [ "$expect" = refuse ] || { echo "FAIL $name: compatible change refused: $out" >&2; exit 1; }
    echo "ok   $name: refused: $(echo "$out" | head -1 | sed 's/^.*ticker.proto:/ticker.proto:/')"
    refused=$((refused + 1))
  fi
}

mutate field-type-changed refuse 's/int64 price_micros = 2;/double price_micros = 2;/'
mutate field-number-changed refuse '0,/string symbol = 1;/s//string symbol = 6;/'
mutate rpc-removed refuse '/rpc WatchQuotes/d'
mutate field-renamed refuse 's/int64 tick = 4;/int64 tick_number = 4;/'
mutate field-added pass 's/google.protobuf.Timestamp time = 5;/google.protobuf.Timestamp time = 5;\n  string venue = 6;/'

echo "RESULT breaking-check: $refused/4 breaking changes refused, $accepted/1 compatible change accepted"
