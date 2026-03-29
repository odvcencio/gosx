package server

import "strings"

// CSSLayer describes the ownership layer of document-managed CSS.
type CSSLayer string

const (
	CSSLayerGlobal  CSSLayer = "global"
	CSSLayerLayout  CSSLayer = "layout"
	CSSLayerPage    CSSLayer = "page"
	CSSLayerRuntime CSSLayer = "runtime"
)

// StylesheetOptions configures document-level stylesheet ownership metadata.
type StylesheetOptions struct {
	Layer  CSSLayer
	Owner  string
	Source string
}

func normalizeCSSLayer(layer CSSLayer) CSSLayer {
	switch strings.TrimSpace(string(layer)) {
	case string(CSSLayerLayout):
		return CSSLayerLayout
	case string(CSSLayerPage):
		return CSSLayerPage
	case string(CSSLayerRuntime):
		return CSSLayerRuntime
	case string(CSSLayerGlobal):
		fallthrough
	default:
		return CSSLayerGlobal
	}
}

func stylesheetSource(href string) string {
	href = strings.TrimSpace(href)
	if href == "" {
		return ""
	}
	return AssetURL(href)
}

func stylesheetOwner(owner string) string {
	owner = strings.TrimSpace(owner)
	if owner == "" {
		return "document"
	}
	return owner
}
