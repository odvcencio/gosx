# PR142 runtime hardening transplant boundary

This Stack 05 branch is intentionally based on Stack 02 commit
`b557f1a31d29a79020addc024e3bbf55eaf8cb9a`.

The runtime ABI hardening implementation remains commit `1e5b784` on
`codex/o6-runtime-hardening`. It cannot be re-expressed against Stack 02
without importing the feature implementation it is supposed to harden:

- `client/runtime/wasm/{abi,loader,mailbox}.{go,ts}` first appear in PR142
  commits `c3c1025` and `c230127`.
- `client/runtime/generated/runtime-abi.ts` and
  `cmd/buildbootstrap/runtime_contract.go` are absent from Stack 02.
- capability-linked `WASMVariants`, variant metadata, and renderer selection
  first appear in `c230127`.
- the authored TypeScript host relay at `client/runtime/host/relay.ts` first
  appears in `f9bfb35`.

The cherry-pick boundary is therefore:

1. Land Stack 03/04 through `f9bfb35` (or its converged equivalents).
2. Cherry-pick `1e5b784`.
3. Cherry-pick the relay-hardening commit following this dependency record
   (or the equivalent original `24a5b26` after the TypeScript host lands).

Attempting to cherry-pick the hardening commit directly on Stack 02 produces
modify/delete conflicts for the generated ABI and build contract, plus missing
source failures for the ABI, loader, mailbox, runtime handshake, capability
variant manifest, and TypeScript relay. Resolving those conflicts by adding
the missing files would silently fold Stack 03/04's product feature into the
hardening stack and destroy the intended stack boundaries.

The relay hardening is re-expressed on this Stack 02 branch against the
current `client/js/relay.js` source; its Go parser and 11 Node relay tests pass
here. Stack 04 should carry the same logic into its authored TypeScript source.

The ABI source hardening commit has focused passing evidence recorded on
`codex/o6-runtime-hardening`: Go runtime/manifest/selector tests,
build-bootstrap generated-contract/source-order tests, and 18 Node loader and
relay adversarial tests.
