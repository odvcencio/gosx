# GoSX Editor

`github.com/odvcencio/gosx/editor` is an optional Markdown++ editor module with
its own `go.mod`. It ships the server-rendered editor shell, toolbar model,
text operations, and the native browser assets used for live preview, autosave,
outline, gallery, and metadata stats.

Mount the assets only in apps that use the editor:

```go
app.Mount("/editor/", http.StripPrefix("/editor/", editor.AssetHandler()))
```

Render the component from request-scoped options:

```go
ed := editor.New("post-editor", editor.Options{
	Content:     post.Content,
	Title:       post.Title,
	Slug:        post.Slug,
	FormAction:  ctx.ActionPath("update"),
	AutoSaveURL: ctx.ActionPath("autosave"),
	PreviewURL:  ctx.ActionPath("preview"),
	UploadURL:   ctx.ActionPath("upload"),
	ImagesURL:   ctx.ActionPath("images"),
	CSRFToken:   token,
})
return ed.Render()
```

The preview endpoint remains application-owned. It should accept `content`,
return JSON with an `html` string, and may include `redirect` when a slug or
document identity changes.

Markdown++ rendering is intentionally not a dependency of this module. Apps
should import `github.com/odvcencio/mdpp` directly. Upgrading the renderer
should not require a GoSX framework or editor release, and upgrading the editor
should not require a framework release.
