package gosx

import (
	"strings"
	"sync"
	"testing"
)

func TestManagedLinkRenderContext(t *testing.T) {
	attrs := Attrs(Attr("class", "nav"), Attr("prefetch", "intent"))
	before := RenderAttrs(attrs)
	node := ManagedLink("/docs?tab=one", attrs, Text("Docs <one>"))
	if node.IsZero() || PlainText(node) != "Docs <one>" {
		t.Fatal("managed node lost children")
	}
	for _, tc := range []struct{ target, relation string }{
		{"/docs?tab=one", "page"}, {"/docs?tab=two", "none"}, {"/other", "none"},
	} {
		got := RenderHTMLWithContext(node, RenderContext{RequestTarget: tc.target})
		if !strings.Contains(got, `data-gosx-link-current="`+tc.relation+`"`) || !strings.Contains(got, "Docs &lt;one&gt;") || !strings.Contains(got, `data-gosx-prefetch="intent"`) {
			t.Fatalf("%s: %s", tc.target, got)
		}
	}
	if RenderAttrs(attrs) != before {
		t.Fatal("render mutated supplied attrs")
	}
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			target, relation := "/docs?tab=one", "page"
			if i%2 == 1 {
				target, relation = "/docs?tab=two", "none"
			}
			got := RenderHTMLWithContext(node, RenderContext{RequestTarget: target})
			if !strings.Contains(got, `data-gosx-link-current="`+relation+`"`) {
				t.Error(got)
			}
		}(i)
	}
	wg.Wait()
	if strings.Contains(RenderHTML(node), `aria-current="page"`) {
		t.Fatal("default target must be root")
	}
	ordinary := El("div", Attrs(Attr("title", "<&")), Text("<text>"))
	want := `<div title="&lt;&amp;">&lt;text&gt;</div>`
	if RenderHTML(ordinary) != want || RenderHTMLWithContext(ordinary, RenderContext{RequestTarget: "/docs?x=1"}) != want {
		t.Fatal("ordinary output changed")
	}
	ancestor := RenderHTMLWithContext(ManagedLink("/docs", nil), RenderContext{RequestTarget: "/docs/child"})
	if !strings.Contains(ancestor, `data-gosx-link-current="ancestor"`) {
		t.Fatal(ancestor)
	}
}
