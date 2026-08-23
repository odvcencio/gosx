# GoSX Editor

`m31labs.dev/gosx/editor` is an optional Markdown++ editor module with
its own `go.mod`. It ships the server-rendered editor shell, toolbar model,
text operations, and the native browser assets used for live preview, autosave,
outline, gallery, and metadata stats.

## Agent Skill

Agents working on GoSX editor integration should use the [using-gosx-ecosystem](https://github.com/odvcencio/m31labs-skills/blob/main/skills/using-gosx-ecosystem/SKILL.md) skill.

Mount the assets only in apps that use the editor:

```go
app.Mount("/editor/", http.StripPrefix("/editor/", editor.AssetHandler()))
```

Render the component from request-scoped options:

```go
import "m31labs.dev/gosx/action"

ed := editor.New("post-editor", editor.Options{
	Content:     post.Content,
	Title:       post.Title,
	Slug:        post.Slug,
	FormAction:  action.ActionPath("update"),
	AutoSaveURL: action.ActionPath("autosave"),
	PreviewURL:  action.ActionPath("preview"),
	UploadURL:   action.ActionPath("upload"),
	ImagesURL:   action.ActionPath("images"),
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
