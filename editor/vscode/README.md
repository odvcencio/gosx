# GoSX VS Code Extension

This extension provides:

- native GSX language registration for `.gsx` files
- TextMate syntax highlighting
- `gosx lsp` language-server integration for diagnostics and formatting

Usage:

1. Install dependencies in this folder: `npm install`
2. Open this folder in VS Code
3. Run the extension in a development host

The extension starts the GoSX language server by launching:

```bash
gosx lsp
```

If `gosx` is not on your `PATH`, set `gosx.languageServer.path` in VS Code settings.
