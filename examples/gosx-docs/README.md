# GoSX documentation site

Getting Started and Deployment are authored in Markdown++ under
`content/docs/` and loaded through GoSX Pages. Their GoSX route files retain
the existing layouts, scene headers, and runtime behavior.

Frontmatter contains metadata, historical section IDs, and table-of-contents
labels. The renderer uses the existing GoSX `CodeBlock` component. The docs
catalog links to each guide’s Markdown source.

The other guides remain in their existing GoSX source files. Migrate them
through `LoadMarkdownDoc` after moving their prose and samples into a Markdown
file; keep request-dependent components and live demo handlers in GoSX.

This checkout uses a local Pages dependency at `../gosx-pages` from the GoSX
repository root. Publish Pages and replace that dependency with a released
version before distributing the migration.

From the repository root:

```sh
GOWORK=off go test ./examples/gosx-docs/...
GOWORK=off go run ./examples/gosx-docs
```
