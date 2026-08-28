# Browser compatibility window

`window.__gosx` is the single GoSX browser namespace. New runtime code uses its
two owned surfaces:

- `window.__gosx.host` for browser-host services such as patching and relay
  transport.
- `window.__gosx.runtime` for ABI support, mailbox decoding, verified loading,
  and the running WASM ABI.

The O4/O5 ABI additions live beneath that namespace instead of growing the
ambient `window.__gosx_*` surface:

| Removed ambient name | Owned facade endpoint |
| --- | --- |
| `__gosx_runtime_abi_support` | `__gosx.runtime.support` |
| `__gosx_runtime_mailbox` | `__gosx.runtime.mailbox` |
| `__gosx_runtime_wasm_loader` | `__gosx.runtime.loader` |
| `__gosx_runtime_abi` / `__gosx_runtime_exports` | `__gosx.runtime.abi` |
| `__gosx_apply_patch_mailbox` | `__gosx.host.patch.applyMailbox` |

The remaining `window.__gosx_*` adapters in `host/compatibility.ts` exist only
for the Go/WASM bridge and applications built before v0.38. Each adapter has an
owner and removal checkpoint; new names are forbidden by the buildbootstrap
ambient audit. Unowned names fail closed at installation, read, and removal.

The compatibility window is v0.38 through v0.39. During v0.39 each owner must
either migrate its last consumer to `window.__gosx` or document a new public
ABI commitment before extending an adapter.
