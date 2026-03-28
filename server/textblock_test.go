package server

import (
	"regexp"
	"strings"
	"testing"

	"github.com/odvcencio/gosx"
	"github.com/odvcencio/gosx/textlayout"
)

func TestEstimateTextBlockMetricsUsesApproximateLayout(t *testing.T) {
	metrics, ok := EstimateTextBlockMetrics(TextBlockProps{
		Source:     "hello world from gosx",
		Font:       "600 16px serif",
		LineHeight: 20,
		MaxWidth:   88,
	})
	if !ok {
		t.Fatal("expected metrics estimate")
	}
	if metrics.LineCount < 2 {
		t.Fatalf("expected wrapped estimate, got %+v", metrics)
	}
	if metrics.Height != float64(metrics.LineCount)*20 {
		t.Fatalf("expected lineHeight-based height, got %+v", metrics)
	}
}

func TestTextBlockDerivesSourceAndHintsFromChildren(t *testing.T) {
	node := TextBlock(TextBlockProps{
		Font:       "600 16px serif",
		LineHeight: 20,
		MaxWidth:   88,
	}, gosx.El("span", gosx.Text("hello world from gosx")))
	html := gosx.RenderHTML(node)

	for _, snippet := range []string{
		`data-gosx-text-layout`,
		`data-gosx-text-layout-role="block"`,
		`data-gosx-text-layout-surface="dom"`,
		`data-gosx-text-layout-state="hint"`,
		`data-gosx-text-layout-ready="false"`,
		`data-gosx-text-layout-source="hello world from gosx"`,
		`data-gosx-text-layout-line-count-hint="`,
		`data-gosx-text-layout-height-hint="`,
	} {
		if !strings.Contains(html, snippet) {
			t.Fatalf("expected %q in text block html %q", snippet, html)
		}
	}
}

func TestTextBlockRendersTextPropWhenNoChildrenProvided(t *testing.T) {
	node := TextBlock(TextBlockProps{
		Text:       "badge tone-success",
		Font:       "600 14px serif",
		LineHeight: 18,
		MaxWidth:   200,
	})
	html := gosx.RenderHTML(node)

	if !strings.Contains(html, ">badge tone-success</div>") {
		t.Fatalf("expected text prop to render as body content, got %q", html)
	}
	if !strings.Contains(html, `data-gosx-text-layout-source="badge tone-success"`) {
		t.Fatalf("expected rendered text source attr, got %q", html)
	}
	match := regexp.MustCompile(`data-gosx-text-layout-line-count-hint="([0-9]+)"`).FindStringSubmatch(html)
	if len(match) != 2 || match[1] == "0" {
		t.Fatalf("expected non-zero line count hint in %q", html)
	}
}

func TestTextBlockRendersClampAttrs(t *testing.T) {
	node := TextBlock(TextBlockProps{
		Text:       "hello world from gosx",
		Font:       "600 16px serif",
		LineHeight: 20,
		MaxWidth:   88,
		MaxLines:   1,
		Overflow:   textlayout.OverflowEllipsis,
	})
	html := gosx.RenderHTML(node)

	for _, snippet := range []string{
		`data-gosx-text-layout-max-lines="1"`,
		`data-gosx-text-layout-overflow="ellipsis"`,
	} {
		if !strings.Contains(html, snippet) {
			t.Fatalf("expected %q in text block html %q", snippet, html)
		}
	}
}

func TestTextBlockRendersAlignmentAttrs(t *testing.T) {
	node := TextBlock(TextBlockProps{
		Text:       "aligned copy",
		Font:       "600 16px serif",
		Align:      "center",
		LineHeight: 20,
		MaxWidth:   180,
	})
	html := gosx.RenderHTML(node)

	for _, snippet := range []string{
		`data-gosx-text-layout-align="center"`,
		`align="center"`,
	} {
		if !strings.Contains(html, snippet) {
			t.Fatalf("expected %q in text block html %q", snippet, html)
		}
	}
}
