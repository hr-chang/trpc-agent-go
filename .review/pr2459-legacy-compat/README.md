# PR #2459 legacy compatibility probe

This local-only module compiles one pre-PR public-API test source against two
checkouts:

- `base.mod` replaces `trpc-agent-go` with the root/base worktree.
- `head.mod` replaces it with the PR #2459 worktree.

The test records a canonical externally observable trace and checks its SHA-256
against the value frozen from the base revision. The trace covers default
non-batched knowledge loading and the existing OpenAI single-input path.
Its knowledge embedder has a structural batch method: base ignores that extra
method, while head detects the capability but must not call it without the opt.
`WithProgressStepSize` is positive in the compatibility trace; its zero and
negative behavior is intentionally outside this equality assertion.

Run from this directory:

```sh
GOWORK=off go test -modfile=base.mod -run '^TestPR2459LegacyCompatibilityTrace$' -count=1 -v .
GOWORK=off go test -modfile=head.mod -run '^TestPR2459LegacyCompatibilityTrace$' -count=1 -v .
```

For concurrency validation:

```sh
GOWORK=off go test -race -modfile=head.mod -run '^TestPR2459LegacyCompatibilityTrace$' -count=1 .
```
