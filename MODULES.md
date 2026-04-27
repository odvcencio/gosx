# GoSX Modules

This repository contains independently versioned Go modules:

- `github.com/odvcencio/gosx`: the core framework.
- `github.com/odvcencio/gosx/editor`: the optional browser editor shell and assets.

Markdown++ rendering is the canonical content-source layer for the core
`content` package through `github.com/odvcencio/mdpp`. The framework owns the
collection-loading contract: mdpp handles parsing/rendering, typed frontmatter,
diagnostics, and renderer options, while applications can still import mdpp
directly for lower-level renderer control.
