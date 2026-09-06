//go:build e2e

package e2e

import (
	"fmt"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"
	"m31labs.dev/gosx"
	"m31labs.dev/gosx/components"
	"m31labs.dev/gosx/ui"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestUIButtonDisabledLinksServed(t *testing.T) {
	reg, ok := ui.Registry().Render("Button", components.Props{"id": "registered", "href": "#forbidden", "disabled": true}, gosx.Text("Registry disabled"))
	if !ok {
		t.Fatal("registry")
	}
	nodes := gosx.Fragment(
		ui.Button(ui.ButtonProps{BaseProps: ui.BaseProps{ID: "before"}, Href: "#enabled"}, gosx.Text("Enabled link")),
		ui.Button(ui.ButtonProps{BaseProps: ui.BaseProps{ID: "direct"}, Href: "#forbidden", Disabled: true}, gosx.Text("Direct disabled")),
		reg,
		ui.Button(ui.ButtonProps{BaseProps: ui.BaseProps{ID: "native-disabled"}, Disabled: true}, gosx.Text("Disabled native")),
		ui.Button(ui.ButtonProps{BaseProps: ui.BaseProps{ID: "after"}}, gosx.Text("Enabled native")),
	)
	html := "<!doctype html><html><head>" + gosx.RenderHTML(ui.Styles()) + "</head><body>" + gosx.RenderHTML(nodes) + "</body></html>"
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, html) }))
	defer s.Close()
	p := newBrowserPage(t, e2eChromePath(t), nil, 800, 600, "", 75*time.Second)
	var valid bool
	if err := chromedp.Run(p.ctx, chromedp.Navigate(s.URL), chromedp.Evaluate(`['direct','registered'].every(id=>{const n=document.getElementById(id);return n.tagName==='SPAN'&&n.getAttribute('aria-disabled')==='true'&&!n.hasAttribute('href')&&!n.hasAttribute('tabindex')})&&document.querySelectorAll('a[href="#forbidden"]').length===0&&document.scripts.length===0`, &valid)); err != nil {
		t.Fatal(err)
	}
	if !valid {
		t.Fatal("semantics")
	}
	for _, id := range []string{"direct", "registered"} {
		var url string
		if err := chromedp.Run(p.ctx, chromedp.Click("#"+id, chromedp.ByQuery), chromedp.Evaluate(`location.href`, &url)); err != nil {
			t.Fatal(err)
		}
		if url != s.URL+"/" {
			t.Fatalf("disabled navigated %s", url)
		}
	}
	var focus string
	var clicks int
	if err := chromedp.Run(p.ctx, chromedp.Focus("#before", chromedp.ByQuery), chromedp.KeyEvent(kb.Tab), chromedp.Evaluate(`document.activeElement.id`, &focus), chromedp.Evaluate(`window.clicks=0;document.getElementById('after').addEventListener('click',()=>window.clicks++)`, nil), chromedp.KeyEvent(kb.Enter), chromedp.KeyEvent(" "), chromedp.Evaluate(`window.clicks`, &clicks)); err != nil {
		t.Fatal(err)
	}
	if focus != "after" || clicks != 2 {
		t.Fatalf("focus=%s clicks=%d", focus, clicks)
	}
	var hash string
	if err := chromedp.Run(p.ctx, chromedp.Focus("#before", chromedp.ByQuery), chromedp.KeyEvent(kb.Enter), chromedp.Evaluate(`location.hash`, &hash)); err != nil {
		t.Fatal(err)
	}
	if hash != "#enabled" {
		t.Fatal(hash)
	}
}
