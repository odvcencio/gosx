package editor

// Signal names for engine-island-runtime communication.
const (
	SignalContent          = "$editor.content"
	SignalCursor           = "$editor.cursor"
	SignalSelection        = "$editor.selection"
	SignalDirty            = "$editor.dirty"
	SignalFocus            = "$editor.focus"
	SignalViewport         = "$editor.viewport"
	SignalSyncState        = "$editor.sync_state"
	SignalWordCount        = "$editor.word_count"
	SignalFileDrop         = "$editor.file_drop"
	SignalClipboardContent = "$editor.clipboard_content"
	SignalCursorRect       = "$editor.cursor_rect"
	SignalToolbarAction    = "$toolbar.action"
)
