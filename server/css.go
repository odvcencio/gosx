package server

import "strings"

// CSSLayer describes the ownership layer of document-managed CSS.
type CSSLayer string

// CSSOwner describes the owner category of document-managed CSS.
type CSSOwner string

const (
	CSSLayerGlobal  CSSLayer = "global"
	CSSLayerLayout  CSSLayer = "layout"
	CSSLayerPage    CSSLayer = "page"
	CSSLayerRuntime CSSLayer = "runtime"

	CSSOwnerDocumentGlobal CSSOwner = "document-global"
	CSSOwnerDocumentLayout CSSOwner = "document-layout"
	CSSOwnerDocumentPage   CSSOwner = "document-page"
	CSSOwnerGlobalFile     CSSOwner = "global-file"
	CSSOwnerLayoutFile     CSSOwner = "layout-file"
	CSSOwnerPageFile       CSSOwner = "page-file"
	CSSOwnerMetadata       CSSOwner = "metadata"
	CSSOwnerRuntime        CSSOwner = "gosx-bootstrap"
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

// DefaultStylesheetOwner returns the default document-managed owner for a layer.
func DefaultStylesheetOwner(layer CSSLayer) string {
	switch normalizeCSSLayer(layer) {
	case CSSLayerLayout:
		return string(CSSOwnerDocumentLayout)
	case CSSLayerPage:
		return string(CSSOwnerDocumentPage)
	case CSSLayerRuntime:
		return string(CSSOwnerRuntime)
	case CSSLayerGlobal:
		fallthrough
	default:
		return string(CSSOwnerDocumentGlobal)
	}
}

// FileStylesheetOwner returns the canonical owner for a route/file-managed CSS layer.
func FileStylesheetOwner(layer CSSLayer) string {
	switch normalizeCSSLayer(layer) {
	case CSSLayerLayout:
		return string(CSSOwnerLayoutFile)
	case CSSLayerPage:
		return string(CSSOwnerPageFile)
	case CSSLayerRuntime:
		return string(CSSOwnerRuntime)
	case CSSLayerGlobal:
		fallthrough
	default:
		return string(CSSOwnerGlobalFile)
	}
}

// NormalizeStylesheetOwner resolves an explicit or default owner for a layer.
func NormalizeStylesheetOwner(layer CSSLayer, owner string) string {
	owner = strings.TrimSpace(owner)
	if owner == "" {
		return DefaultStylesheetOwner(layer)
	}
	return owner
}
