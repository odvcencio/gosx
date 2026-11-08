# GoSX contributor guidance

Keep work scoped to the requested subsystem. Reuse current source, tests, and CLI help; load the matching `using-gosx-*` skill only when useful.

- Keep static/request content on the server. Use actions for mutations, islands for small browser interactions, engines for sustained runtime work, and hubs for shared state.
- Preserve server-side auth/CSRF and private-cache boundaries. Edit authored sources and regenerate affected outputs.
- Use `rg` for literal text. For broad structural analysis, reuse a valid Canopy index; `make canopy-index` uses the repository's bounded wrapper.
- During iteration, run tests for changed packages or named cases. Select additional Makefile targets for the affected JavaScript, WASM, editor, browser, race, or runtime surface.
- `make test-unit` excludes CLI integration; `make test-cli` covers CLI tests and may require TinyGo. A focused result is not proof that all CI passed.
- Follow `.github/workflows/ci.yml` for PR checks and `release-governed.yml` / `make release-gate` for releases. These gates remain required at their normal boundaries.
- Reuse passing evidence only while its code, configuration, and environment remain applicable. Prose-only edits need link/format review, not product builds.
- Preserve unrelated changes. Report the changed behavior, checks actually run, and remaining gaps concisely.
