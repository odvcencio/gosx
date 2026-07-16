package editor

// EditorOptions contains reusable document-editing configuration. It is the
// preferred home for settings that are meaningful in a blog, docs page, chat,
// or any other Markdown++ editor surface.
type EditorOptions struct {
	Content     string
	Label       string
	Placeholder string
	Language    Lang
	Theme       Theme
	Prose       ProseStyle
	Toolbar     Toolbar
	Keymap      Keymap
	Panels      []Panel
	Extensions  []Extension
	ReadOnly    bool
}

// MetadataOptions contains publishing/post-specific chrome. Keeping this
// group separate lets a generic editor omit CMS fields without inventing a
// second document model.
type MetadataOptions struct {
	Title           string
	Slug            string
	Excerpt         string
	Tags            string
	CoverImage      string
	PublishAt       string
	Status          Status
	Mood            string
	MoodChoices     []MoodChoice
	Music           string
	Scratch         string
	ExtraFields     map[string]string
	Buttons         []FormButton
	ScheduleButtons []FormButton
}

// RuntimeOptions contains transport, asset, and preview integration settings.
// It is shared by document and publishing surfaces and does not decide how a
// document is stored or rendered.
type RuntimeOptions struct {
	BackHref                  string
	FormAction                string
	AutoSaveURL               string
	PreviewURL                string
	DiagnosticsURL            string
	UploadURL                 string
	ImagesURL                 string
	StylesheetURL             string
	ProseStylesheetURL        string
	ProseScriptURL            string
	DiagramScriptURL          string
	ScriptURL                 string
	CSRFToken                 string
	LoadingText               string
	InitialPreviewHTML        string
	InitialPreviewPlaceholder string
}

func hasEditorProfile(options EditorOptions) bool {
	return options.Content != "" || options.Label != "" || options.Placeholder != "" ||
		options.Language != "" || options.Theme != "" || options.Prose != (ProseStyle{}) ||
		len(options.Toolbar.Items) > 0 || options.Keymap != nil || options.Panels != nil ||
		options.Extensions != nil || options.ReadOnly
}

func hasMetadataProfile(options MetadataOptions) bool {
	return options.Title != "" || options.Slug != "" || options.Excerpt != "" ||
		options.Tags != "" || options.CoverImage != "" || options.PublishAt != "" ||
		options.Status != "" || options.Mood != "" || options.MoodChoices != nil ||
		options.Music != "" || options.Scratch != "" || options.ExtraFields != nil ||
		options.Buttons != nil || options.ScheduleButtons != nil
}

func (o *Options) applyProfiles() {
	if o.Editor.Content != "" {
		o.Content = o.Editor.Content
	}
	if o.Editor.Label != "" {
		o.Label = o.Editor.Label
	}
	if o.Editor.Placeholder != "" {
		o.Placeholder = o.Editor.Placeholder
	}
	if o.Editor.Language != "" {
		o.Language = o.Editor.Language
	}
	if o.Editor.Theme != "" {
		o.Theme = o.Editor.Theme
	}
	if o.Editor.Prose != (ProseStyle{}) {
		o.Prose = o.Editor.Prose
	}
	if len(o.Editor.Toolbar.Items) > 0 {
		o.Toolbar = o.Editor.Toolbar
	}
	if o.Editor.Keymap != nil {
		o.Keymap = o.Editor.Keymap
	}
	if o.Editor.Panels != nil {
		o.Panels = o.Editor.Panels
	}
	if o.Editor.Extensions != nil {
		o.Extensions = o.Editor.Extensions
	}
	if o.Editor.ReadOnly {
		o.ReadOnly = true
	}

	if o.Metadata.Title != "" {
		o.Title = o.Metadata.Title
	}
	if o.Metadata.Slug != "" {
		o.Slug = o.Metadata.Slug
	}
	if o.Metadata.Excerpt != "" {
		o.Excerpt = o.Metadata.Excerpt
	}
	if o.Metadata.Tags != "" {
		o.Tags = o.Metadata.Tags
	}
	if o.Metadata.CoverImage != "" {
		o.CoverImage = o.Metadata.CoverImage
	}
	if o.Metadata.PublishAt != "" {
		o.PublishAt = o.Metadata.PublishAt
	}
	if o.Metadata.Status != "" {
		o.Status = o.Metadata.Status
	}
	if o.Metadata.Mood != "" {
		o.Mood = o.Metadata.Mood
	}
	if o.Metadata.MoodChoices != nil {
		o.MoodChoices = o.Metadata.MoodChoices
	}
	if o.Metadata.Music != "" {
		o.Music = o.Metadata.Music
	}
	if o.Metadata.Scratch != "" {
		o.Scratch = o.Metadata.Scratch
	}
	if o.Metadata.ExtraFields != nil {
		o.ExtraFields = o.Metadata.ExtraFields
	}
	if o.Metadata.Buttons != nil {
		o.Buttons = o.Metadata.Buttons
	}
	if o.Metadata.ScheduleButtons != nil {
		o.ScheduleButtons = o.Metadata.ScheduleButtons
	}

	if o.Runtime.BackHref != "" {
		o.BackHref = o.Runtime.BackHref
	}
	if o.Runtime.FormAction != "" {
		o.FormAction = o.Runtime.FormAction
	}
	if o.Runtime.AutoSaveURL != "" {
		o.AutoSaveURL = o.Runtime.AutoSaveURL
	}
	if o.Runtime.PreviewURL != "" {
		o.PreviewURL = o.Runtime.PreviewURL
	}
	if o.Runtime.DiagnosticsURL != "" {
		o.DiagnosticsURL = o.Runtime.DiagnosticsURL
	}
	if o.Runtime.UploadURL != "" {
		o.UploadURL = o.Runtime.UploadURL
	}
	if o.Runtime.ImagesURL != "" {
		o.ImagesURL = o.Runtime.ImagesURL
	}
	if o.Runtime.StylesheetURL != "" {
		o.StylesheetURL = o.Runtime.StylesheetURL
	}
	if o.Runtime.ProseStylesheetURL != "" {
		o.ProseStylesheetURL = o.Runtime.ProseStylesheetURL
	}
	if o.Runtime.ProseScriptURL != "" {
		o.ProseScriptURL = o.Runtime.ProseScriptURL
	}
	if o.Runtime.DiagramScriptURL != "" {
		o.DiagramScriptURL = o.Runtime.DiagramScriptURL
	}
	if o.Runtime.ScriptURL != "" {
		o.ScriptURL = o.Runtime.ScriptURL
	}
	if o.Runtime.CSRFToken != "" {
		o.CSRFToken = o.Runtime.CSRFToken
	}
	if o.Runtime.LoadingText != "" {
		o.LoadingText = o.Runtime.LoadingText
	}
	if o.Runtime.InitialPreviewHTML != "" {
		o.InitialPreviewHTML = o.Runtime.InitialPreviewHTML
	}
	if o.Runtime.InitialPreviewPlaceholder != "" {
		o.InitialPreviewPlaceholder = o.Runtime.InitialPreviewPlaceholder
	}
}

func (o *Options) syncProfiles() {
	o.Editor = EditorOptions{
		Content:     o.Content,
		Label:       o.Label,
		Placeholder: o.Placeholder,
		Language:    o.Language,
		Theme:       o.Theme,
		Prose:       o.Prose,
		Toolbar:     cloneToolbar(o.Toolbar),
		Keymap:      cloneKeymap(o.Keymap),
		Panels:      clonePanels(o.Panels),
		Extensions:  cloneExtensions(o.Extensions),
		ReadOnly:    o.ReadOnly,
	}
	o.Metadata = MetadataOptions{
		Title:           o.Title,
		Slug:            o.Slug,
		Excerpt:         o.Excerpt,
		Tags:            o.Tags,
		CoverImage:      o.CoverImage,
		PublishAt:       o.PublishAt,
		Status:          o.Status,
		Mood:            o.Mood,
		MoodChoices:     append([]MoodChoice(nil), o.MoodChoices...),
		Music:           o.Music,
		Scratch:         o.Scratch,
		ExtraFields:     cloneStringMap(o.ExtraFields),
		Buttons:         append([]FormButton(nil), o.Buttons...),
		ScheduleButtons: append([]FormButton(nil), o.ScheduleButtons...),
	}
	o.Runtime = RuntimeOptions{
		BackHref:                  o.BackHref,
		FormAction:                o.FormAction,
		AutoSaveURL:               o.AutoSaveURL,
		PreviewURL:                o.PreviewURL,
		DiagnosticsURL:            o.DiagnosticsURL,
		UploadURL:                 o.UploadURL,
		ImagesURL:                 o.ImagesURL,
		StylesheetURL:             o.StylesheetURL,
		ProseStylesheetURL:        o.ProseStylesheetURL,
		ProseScriptURL:            o.ProseScriptURL,
		DiagramScriptURL:          o.DiagramScriptURL,
		ScriptURL:                 o.ScriptURL,
		CSRFToken:                 o.CSRFToken,
		LoadingText:               o.LoadingText,
		InitialPreviewHTML:        o.InitialPreviewHTML,
		InitialPreviewPlaceholder: o.InitialPreviewPlaceholder,
	}
}

func cloneStringMap(src map[string]string) map[string]string {
	if src == nil {
		return nil
	}
	dst := make(map[string]string, len(src))
	for key, value := range src {
		dst[key] = value
	}
	return dst
}
