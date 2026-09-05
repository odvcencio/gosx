//go:build e2e

package e2e

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"
	"m31labs.dev/gosx"
	"m31labs.dev/gosx/components"
	"m31labs.dev/gosx/ui"
)

func TestUIStaticTabsServed(t *testing.T) {
	items := []ui.TabItem{{ID: "one", Label: "One", Href: "#one", Active: true}, {ID: "disabled", Label: "Disabled link", Href: "#forbidden", Disabled: true}, {ID: "button", Label: "Disabled button", Disabled: true}, {ID: "action", Label: "Action"}, {ID: "two", Label: "Two", Href: "#two"}}
	direct := ui.Tabs(ui.TabsProps{BaseProps: ui.BaseProps{ID: "direct"}, Items: items})
	registered, ok := ui.Registry().Render("Tabs", components.Props{"id": "registered", "active": "two", "items": items})
	if !ok {
		t.Fatal("Tabs registry missing")
	}
	html := "<!doctype html><html><head>" + gosx.RenderHTML(ui.Styles()) + "</head><body>" + gosx.RenderHTML(gosx.Fragment(direct, registered)) + "</body></html>"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, html)
	}))
	defer server.Close()
	page := newBrowserPage(t, e2eChromePath(t), nil, 800, 600, "", 75*time.Second)
	if err := chromedp.Run(page.ctx, chromedp.Navigate(server.URL)); err != nil {
		t.Fatal(err)
	}
	// Instrumentation observes native clicks; it does not implement application behavior.
	var clicks int
	if err := chromedp.Run(page.ctx, chromedp.Evaluate(`window.disabledClicks=0;document.querySelector('#direct > :nth-child(3)').addEventListener('click',()=>window.disabledClicks++)`, nil), chromedp.Click(`#direct > :nth-child(3)`, chromedp.ByQuery), chromedp.Evaluate(`window.disabledClicks`, &clicks)); err != nil {
		t.Fatal(err)
	}
	if clicks != 0 {
		t.Errorf("disabled button dispatched %d native clicks", clicks)
	}
	var hash string
	if err := chromedp.Run(page.ctx, chromedp.Click(`#direct > :nth-child(2)`, chromedp.ByQuery), chromedp.Evaluate(`location.hash`, &hash)); err != nil {
		t.Fatal(err)
	}
	if hash != "" {
		t.Errorf("disabled link navigated to %q", hash)
	}
	var semantics bool
	if err := chromedp.Run(page.ctx, chromedp.Evaluate(`!document.querySelector('[role=tab],[role=tablist],[aria-selected]') && document.querySelectorAll('[aria-current=page]').length===2 && document.querySelector('#direct > :first-child').getAttribute('aria-current')==='page' && document.querySelector('#registered > :last-child').getAttribute('aria-current')==='page' && document.querySelector('#direct > :nth-child(2)').tagName==='SPAN' && !document.querySelector('#direct > :nth-child(2)').hasAttribute('href') && !document.querySelector('#direct > :nth-child(2)').hasAttribute('tabindex') && document.querySelector('#direct > :nth-child(3)').disabled && document.scripts.length===0`, &semantics)); err != nil {
		t.Fatal(err)
	}
	if !semantics {
		t.Error("static native semantics or independent current-page relationship incorrect")
	}
	// Reset position before proving browser keyboard navigation, skipping both disabled controls.
	var focused string
	if err := chromedp.Run(page.ctx, chromedp.Focus(`#direct > :first-child`, chromedp.ByQuery), chromedp.KeyEvent(kb.Tab), chromedp.Evaluate(`document.activeElement.textContent`, &focused)); err != nil {
		t.Fatal(err)
	}
	if focused != "Action" {
		t.Errorf("Tab focus=%q, want Action", focused)
	}
	if err := chromedp.Run(page.ctx, chromedp.Evaluate(`window.actionClicks=0;document.activeElement.addEventListener('click',()=>window.actionClicks++)`, nil), chromedp.KeyEvent(kb.Enter), chromedp.KeyEvent(" "), chromedp.Evaluate(`window.actionClicks`, &clicks)); err != nil {
		t.Fatal(err)
	}
	if clicks != 2 {
		t.Errorf("enabled Action Enter/Space clicks=%d, want 2", clicks)
	}
	if err := chromedp.Run(page.ctx, chromedp.KeyEvent(kb.Tab), chromedp.KeyEvent(kb.Enter), chromedp.Evaluate(`location.hash`, &hash)); err != nil {
		t.Fatal(err)
	}
	if hash != "#two" {
		t.Errorf("enabled keyboard link hash=%q", hash)
	}
	if err := chromedp.Run(page.ctx, chromedp.KeyEvent(kb.Tab), chromedp.Evaluate(`document.activeElement.parentElement.id+':'+document.activeElement.textContent`, &focused)); err != nil {
		t.Fatal(err)
	}
	if focused != "registered:One" {
		t.Errorf("cross-instance Tab focus=%q", focused)
	}
	if err := chromedp.Run(page.ctx, chromedp.Click(`#registered > :first-child`, chromedp.ByQuery), chromedp.Evaluate(`location.hash`, &hash)); err != nil {
		t.Fatal(err)
	}
	if hash != "#one" {
		t.Errorf("enabled pointer link hash=%q", hash)
	}
	var visible bool
	if err := chromedp.Run(page.ctx, emulation.SetEmulatedMedia().WithFeatures([]*emulation.MediaFeature{{Name: "forced-colors", Value: "active"}, {Name: "prefers-reduced-motion", Value: "reduce"}}), chromedp.Focus(`#direct > :first-child`, chromedp.ByQuery), chromedp.KeyEvent(kb.Tab), chromedp.Evaluate(`(()=>{const focus=getComputedStyle(document.activeElement), active=getComputedStyle(document.querySelector('#direct > :first-child')), inactive=getComputedStyle(document.querySelector('#direct > :last-child'));window.tabVisualEvidence={outline:focus.outlineColor,background:focus.backgroundColor,activeBorder:active.borderBottomColor,inactiveBorder:inactive.borderBottomColor,currentDecoration:active.textDecorationLine,inactiveDecoration:inactive.textDecorationLine};return matchMedia('(forced-colors: active)').matches && matchMedia('(prefers-reduced-motion: reduce)').matches && focus.outlineStyle!=='none' && parseFloat(focus.outlineWidth)>0 && focus.outlineColor!=='rgba(0, 0, 0, 0)' && focus.outlineColor!==focus.backgroundColor && document.querySelector('#direct > :first-child').getAttribute('aria-current')==='page' && !document.querySelector('#direct > :last-child').hasAttribute('aria-current') && active.textDecorationLine.includes('underline') && inactive.textDecorationLine==='none'})()`, &visible)); err != nil {
		t.Fatal(err)
	}
	if !visible {
		t.Error("forced-colors/reduced-motion focus/current indicator missing")
	}
	var evidence string
	if err := chromedp.Run(page.ctx, chromedp.Evaluate(`JSON.stringify(window.tabVisualEvidence)`, &evidence)); err != nil {
		t.Fatal(err)
	}
	t.Log(evidence)
}
