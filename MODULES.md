# GoSX Modules

This repository contains independently versioned Go modules:

- `github.com/odvcencio/gosx`: the core framework.
- `github.com/odvcencio/gosx/editor`: the optional browser editor shell and assets.

Keep Markdown++ rendering out of the core framework and editor dependency
graph. Applications should import and upgrade `github.com/odvcencio/mdpp`
directly without waiting for a framework or editor release.
