package docs

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/odvcencio/gosx/action"
	docsapp "github.com/odvcencio/gosx/examples/gosx-docs/app"
	"github.com/odvcencio/gosx/route"
	"github.com/odvcencio/gosx/server"
	"github.com/odvcencio/gosx/session"
)

type cmsDocument struct {
	Blocks []cmsBlock `json:"blocks"`
}

type cmsBlock struct {
	ID          string `json:"id"`
	Type        string `json:"type"`
	Eyebrow     string `json:"eyebrow,omitempty"`
	Title       string `json:"title,omitempty"`
	Body        string `json:"body,omitempty"`
	CTA         string `json:"cta,omitempty"`
	Stat        string `json:"stat,omitempty"`
	Attribution string `json:"attribution,omitempty"`
}

func init() {
	docsapp.RegisterStaticDocsPage(
		"CMS Demo",
		"Compose a routed page with drag-and-drop blocks, live preview, and a single publish action.",
		route.FileModuleOptions{
			Metadata: func(ctx *route.RouteContext, page route.FilePage, data any) (server.Metadata, error) {
				return server.Metadata{
					Title: server.Title{Absolute: "CMS Demo | GoSX"},
				}, nil
			},
			Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
				ctx.ManagedScript(docsapp.PublicAssetURL("cms-demo.js"), server.ManagedScriptOptions{})

				doc := defaultCMSDocument()
				if state, ok := ctx.ActionState("publish"); ok {
					if draft, err := decodeCMSDocument(state.Value("document")); err == nil {
						doc = draft
					}
				}

				return map[string]any{
					"document":     cmsDocumentData(doc),
					"documentJSON": mustMarshalCMSDocument(doc),
					"metrics": []map[string]string{
						{
							"value": fmt.Sprintf("%d blocks", len(doc.Blocks)),
							"label": "Dragging, editing, and preview all stay inside the same routed page.",
						},
						{
							"value": "1 publish action",
							"label": "No per-block save buttons. The document ships as one server-owned payload.",
						},
						{
							"value": "Live preview",
							"label": "Every edit updates the published composition board immediately.",
						},
					},
				}, nil
			},
			Actions: route.FileActions{
				"publish": func(ctx *action.Context) error {
					doc, err := parseCMSDocument(ctx.FormData["document"])
					if err != nil {
						return action.Validation(
							"Add at least one valid block before publishing.",
							map[string]string{"document": err.Error()},
							ctx.FormData,
						)
					}

					session.AddFlash(ctx.Request, "notice", fmt.Sprintf(
						"Published %d blocks through the normal GoSX action pipeline.",
						len(doc.Blocks),
					))
					return ctx.Success("Draft published.", nil)
				},
			},
		},
	)
}

func defaultCMSDocument() cmsDocument {
	return cmsDocument{
		Blocks: []cmsBlock{
			{
				ID:      "hero-foundation",
				Type:    "hero",
				Eyebrow: "Feature launch",
				Title:   "Ship the editorial surface and the authoring surface from one GoSX app.",
				Body:    "The hero block sets the message, the route owns the data, and the editor never steps outside the product shell.",
				CTA:     "Start the release",
			},
			{
				ID:    "feature-runtime",
				Type:  "feature",
				Stat:  "03",
				Title: "Runtime details stay selective.",
				Body:  "Use browser code where the experience benefits from it, while publish remains a straightforward form action.",
			},
			{
				ID:          "quote-signal",
				Type:        "quote",
				Body:        "The page builder feels immediate, but the actual commit point is still a normal Go handler with session-backed feedback.",
				Attribution: "GoSX editorial demo",
			},
		},
	}
}

func mustMarshalCMSDocument(doc cmsDocument) string {
	payload, err := json.Marshal(doc)
	if err != nil {
		return `{"blocks":[]}`
	}
	return string(payload)
}

func cmsDocumentData(doc cmsDocument) map[string]any {
	blocks := make([]map[string]string, 0, len(doc.Blocks))
	for _, block := range doc.Blocks {
		blocks = append(blocks, map[string]string{
			"id":          block.ID,
			"type":        block.Type,
			"eyebrow":     block.Eyebrow,
			"title":       block.Title,
			"body":        block.Body,
			"cta":         block.CTA,
			"stat":        block.Stat,
			"attribution": block.Attribution,
		})
	}
	return map[string]any{"blocks": blocks}
}

func parseCMSDocument(raw string) (cmsDocument, error) {
	doc, err := decodeCMSDocument(raw)
	if err != nil {
		return cmsDocument{}, err
	}
	if len(doc.Blocks) == 0 {
		return doc, fmt.Errorf("add at least one block to publish the page")
	}
	if len(doc.Blocks) > 12 {
		return doc, fmt.Errorf("keep the demo draft to 12 blocks or fewer")
	}
	for _, block := range doc.Blocks {
		switch block.Type {
		case "hero", "feature":
			if strings.TrimSpace(block.Title) == "" {
				return doc, fmt.Errorf("headline blocks need a title before publish")
			}
		}
		if strings.TrimSpace(block.Body) == "" {
			return doc, fmt.Errorf("every block needs body copy before publish")
		}
	}
	return doc, nil
}

func decodeCMSDocument(raw string) (cmsDocument, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return cmsDocument{}, fmt.Errorf("drag a block into the document before publishing")
	}

	var doc cmsDocument
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		return cmsDocument{}, fmt.Errorf("the draft payload could not be decoded")
	}

	doc.Blocks = normalizeCMSBlocks(doc.Blocks)
	return doc, nil
}

func normalizeCMSBlocks(blocks []cmsBlock) []cmsBlock {
	out := make([]cmsBlock, 0, len(blocks))
	for index, block := range blocks {
		block = normalizeCMSBlock(block, index)
		if block.Type == "" {
			continue
		}
		out = append(out, block)
	}
	return out
}

func normalizeCMSBlock(block cmsBlock, index int) cmsBlock {
	block.Type = strings.TrimSpace(strings.ToLower(block.Type))
	switch block.Type {
	case "hero", "feature", "quote":
	default:
		return cmsBlock{}
	}

	block.ID = strings.TrimSpace(block.ID)
	if block.ID == "" {
		block.ID = fmt.Sprintf("%s-%d", block.Type, index+1)
	}

	block.Eyebrow = trimCMSField(block.Eyebrow, 60)
	block.Title = trimCMSField(block.Title, 140)
	block.Body = trimCMSField(block.Body, 320)
	block.CTA = trimCMSField(block.CTA, 40)
	block.Stat = trimCMSField(block.Stat, 16)
	block.Attribution = trimCMSField(block.Attribution, 80)

	switch block.Type {
	case "hero":
		if block.Eyebrow == "" {
			block.Eyebrow = "Feature launch"
		}
		if block.CTA == "" {
			block.CTA = "Publish this story"
		}
	case "feature":
		if block.Stat == "" {
			block.Stat = "01"
		}
	case "quote":
		if block.Attribution == "" {
			block.Attribution = "Editorial desk"
		}
	}

	return block
}

func trimCMSField(value string, limit int) string {
	value = strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	if limit <= 0 || len(value) <= limit {
		return value
	}
	return strings.TrimSpace(value[:limit])
}
