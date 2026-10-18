# ADR 0003: Check breaking changes against a committed Buf image as well as the base branch

- **Status:** Accepted

## Context

`buf breaking` needs something to compare against. The usual input is the base branch
(`.git#branch=main`). That works in pull requests, but not on the first commit, in a fresh clone
without history, or on a developer's machine before pushing, and it only protects against changes
since the last merge: a series of small merged changes can still drift away from what released
clients use.

## Decision

The released contract is stored as a Buf image, `contract/ticker-v1.binpb` (built with
`buf build -o`). `make breaking` compares the working tree with it, and the same script proves
the gate works: four typical breaking edits (field type change, field number change, removed
RPC, renamed field) must be refused, and one compatible edit (a new field) must pass. In CI,
pull requests are additionally compared with their base branch.

## Consequences

- The gate works offline and without git history, and it compares against what clients were
  built with, not just the previous commit.
- Accepting a breaking change is an explicit, reviewable step: `make baseline` rewrites the image,
  and the diff shows a changed binary file. A breaking change should come with a new package
  (`ticker.v2`), not a rewritten v1 baseline.
- The FILE rule set also flags renamed fields (JSON names change), which is stricter than wire
  compatibility alone; that is intended because Connect-RPC (M3) uses JSON.
- The image is a binary file (10.5 KB); `.gitattributes` marks it binary.
