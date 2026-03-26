# GoSX Punchlist

This is the working framework punchlist for driving GoSX from "credible core" to "delightful, extensible, batteries-included web app framework."

## Phase 1

- [x] File-route server conventions
- [x] `route.FileLayout(...)` for `layout.gsx`
- [x] File-route module registry with `Load`, `Metadata`, `Render`, and `Actions`
- [x] Relative `__actions/<name>` endpoints for file-routed pages
- [x] Starter scaffold dogfoods `page.server.go`

## Phase 2

- [x] First-class forms and actions for file-routed apps
- [x] Better authoring story than raw `route.MustRegisterFileModule(...)`
- [x] Validation/error-state helpers for HTML forms
- [x] Route-aware action helpers in `.gsx` and Go APIs
- [x] Sessions, cookies, flash messages, and CSRF
- [x] Auth guards and user-context conventions

## Phase 3

- [x] Nested filesystem layouts discovered automatically
- [x] Route groups
- [x] Directory-scoped middleware/config
- [x] Directory-scoped `not-found` and `error` composition
- [x] File-based metadata conventions beyond manual registration

## Phase 4

- [x] Cache and revalidation primitives
- [x] Page/data cache semantics
- [x] Conditional requests and validator support where appropriate
- [x] Explicit invalidation and revalidation APIs

## Phase 5

- [x] Productize `gosx dev`
- [x] Productize `gosx build`
- [x] Document production deployment patterns
- [x] Validate the browser runtime in real app flows, not just unit-style tests
- [x] Tighten asset/runtime integration across dev and prod

## Phase 6

- [x] Styling and asset conventions that feel batteries-included
- [x] Route/component-owned CSS and head inclusion ergonomics
- [x] Better public asset and image ownership story

## Phase 7

- [x] Stable extensibility model
- [x] Framework hooks/plugins for auth, image backends, observability, and build steps
- [x] Clear boundaries between framework internals and supported extension APIs

## Phase 8

- [x] App-level testing batteries
- [x] Route rendering test helpers
- [x] Action/form test helpers
- [x] Browser/E2E harness guidance and fixtures
- [x] Golden-path testing for scaffolded apps

## Phase 9

- [x] SSG / static-export path for file-routed `.gsx` apps
- [ ] Hybrid SSR + prerendered route story with one build pipeline
- [ ] Export-safe asset, metadata, and link generation for static output

## Phase 10

- [ ] `.gsx`-first engine surfaces so apps stop dropping to bare `gosx.El(...)` for advanced client runtimes
- [ ] First-class 3D scene/component model in `.gsx`, analogous to a native GoSX answer to Three.js-style authoring
- [ ] Engine/runtime APIs that let `.gsx` drive worker, canvas, and surface-backed experiences without bespoke imperative glue

## Phase 11

- [ ] ISR / incremental static regeneration on top of cache revalidation and static export
- [ ] Edge runtime target for routing, rendering, and actions
- [ ] Vercel-tier deployment platform / hosted distribution story for GoSX apps

## Tracked Migration Blockers

- [ ] Replace remaining app-facing `gosx.El(...)` trees in `blog.go`, `dashboard.go`, `editor.go`, and `pages.go` once file-routed `.gsx` pages can consume arbitrary loader-provided app data
- [ ] Extend the file-module / file-eval environment so `page.server.go` loaders can pass complex store-backed data and helpers through to `.gsx` templates without falling back to programmatic Go nodes
- [ ] File-routed `.gsx` rendering still evaluates IR directly instead of executing arbitrary Go component bodies, so loops, conditionals, and store-driven composition in complex pages remain blocked behind `gosx.El(...)`
- [x] Add a built-in file-route component/runtime seam so framework-native components like `Link`, `Image`, and future 3D scene primitives can render from `.gsx` without collapsing to placeholder `<div data-gosx-component=...>`
- [ ] Make nested `page.server.go` registration fully automatic so real `.gsx` apps do not need manual side-effect import buckets once they outgrow the scaffolded `modules` package
- [x] Remove the current parser limitation that makes tags with multiple expression-valued attributes brittle, so `.gsx` authoring does not have to lean on spread-attr workarounds
