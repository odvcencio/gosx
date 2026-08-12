# Browser compatibility window

`window.__gosx.host` is the sole browser-host facade. The legacy
`window.__gosx_*` adapters in `host/compatibility.ts` remain only for the Go/WASM
ABI and applications built before v0.38. Each adapter has an owner and removal
checkpoint; new host globals are forbidden by the buildbootstrap ambient audit.

The compatibility window is v0.38 through v0.39. During v0.39 the owners must
either migrate their last ABI consumer to `window.__gosx.host` or document a new
public ABI commitment before extending an entry. Unowned names fail closed at
installation time, and product host modules may install, read, or clear legacy
names only through the compatibility adapter.
