# E0 — Align Elio with the gosx sibling checkout (repo: elio)

## Why

Elio's `go.mod` has `replace m31labs.dev/gosx => ../gosx`. At the baseline,
gosx requires `github.com/odvcencio/gotreesitter v0.50.1` while Elio pins
`v0.47.0`. The first `go` command in Elio therefore fails with
`go: updates to go.mod needed`. Letting Go update `go.mod` selects gotreesitter
v0.50.1. Under v0.50.1, Elio's parser rejects **every** `//` comment
(`syntax error near "// ..."`), even with a regenerated `parse/grammar.bin`.
Both were reproduced while writing this spec. Pinning v0.47.0 with a `replace`
restores a fully green Elio suite against gosx HEAD.

Do this task when check 1 or check 2 below fails. At the baseline, check 1
fails. Check 2 fails in any checkout where `go.mod` was already updated to
gotreesitter v0.50.1 (for example by `go mod tidy` or `GOFLAGS=-mod=mod`).

## Checks

1. `cd <root>/elio && go build ./...` → fails with `go: updates to go.mod needed`.
2. Write a file with a comment and parse it:

   ```sh
   printf '// c\n@group(0) @binding(0) storage read_write a: []f32;\n@workgroup(64) kernel k(gid: global_invocation_id) {\n  let i = gid.x;\n  a[i] = 1.0;\n}\n' > /tmp/c.elio
   go run ./cmd/elio check /tmp/c.elio
   ```

   Expected output is `ok`. Under gotreesitter v0.50.1 it prints
   `1:1: syntax error near "// c"`.

## Steps

1. `cd <root>/elio`
2. `go mod edit -replace github.com/odvcencio/gotreesitter=github.com/odvcencio/gotreesitter@v0.47.0`
3. `GOFLAGS=-mod=mod go build ./...`. Go updates the `require` block. Expected
   `go.mod` diff: `gotreesitter v0.47.0 → v0.50.1` in `require` (the replace
   keeps v0.47.0 in effect), `selena v0.4.0 → v0.5.2`,
   `turboquant v0.2.0 → v0.2.1` (indirect), plus the new `replace` line.
   `go.sum` gains entries.
4. Do **not** regenerate `parse/grammar.bin`. The committed blob is correct for
   v0.47.0.

## Verify

```sh
go list -m github.com/odvcencio/gotreesitter
# github.com/odvcencio/gotreesitter v0.50.1 => github.com/odvcencio/gotreesitter v0.47.0
go run ./cmd/elio check /tmp/c.elio      # ok
go test ./...                            # every package ok (naga/glslang subtests may SKIP)
git status --short                       # only go.mod and go.sum modified
```

## Commit (elio)

`fix(deps): pin gotreesitter v0.47.0 for the gosx sibling workspace`

- gosx at the sibling path requires gotreesitter v0.50.1; under v0.50.1 the
  embedded Elio grammar rejects every `//` comment.
- The replace keeps the parser on the version grammar.bin was generated for,
  and lets `go build` succeed without editing gosx.

Also report the upstream problem to the maintainer. It is recorded in
appendix C.
