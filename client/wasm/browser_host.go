//go:build js && wasm

package main

import (
	"fmt"
	"strings"
	"syscall/js"

	"m31labs.dev/gosx/client/vm"
	"m31labs.dev/gosx/island/program"
)

// browserHostReceiver is the browser-owned capability surface bound under the
// reserved `browser` receiver for DOM island handlers. It holds only the root
// id: every selector lookup is repeated against the current root so navigation
// reconciliation and deferred form submission never retain detached nodes.
type browserHostReceiver struct {
	islandID string
}

func newBrowserHostReceiver(islandID string) *browserHostReceiver {
	return &browserHostReceiver{islandID: islandID}
}

func (r *browserHostReceiver) Call(method string, args []vm.Value) (result vm.Value, err error) {
	// syscall/js surfaces a JavaScript exception as a Go panic. A malformed CSS
	// selector or browser API must become a VM diagnostic, never tear down WASM.
	defer func() {
		if recovered := recover(); recovered != nil {
			result = vm.ZeroValue(program.TypeAny)
			err = fmt.Errorf("browser.%s: %v", method, recovered)
		}
	}()

	callArgs, enabled := browserEffectArgs(args)
	switch method {
	case "Open":
		if err := requireBrowserArity(method, callArgs, 1, 1); err != nil {
			return vm.BoolVal(false), err
		}
		if !enabled {
			return vm.BoolVal(false), nil
		}
		selector, err := browserStringArg(method, callArgs, 0)
		if err != nil || selector == "" {
			return vm.BoolVal(false), err
		}
		return vm.BoolVal(browserDefer(func() { r.open(selector) })), nil
	case "Close":
		if err := requireBrowserArity(method, callArgs, 1, 1); err != nil {
			return vm.BoolVal(false), err
		}
		if !enabled {
			return vm.BoolVal(false), nil
		}
		selector, err := browserStringArg(method, callArgs, 0)
		if err != nil || selector == "" {
			return vm.BoolVal(false), err
		}
		return vm.BoolVal(browserDefer(func() { r.close(selector) })), nil
	case "Focus":
		if err := requireBrowserArity(method, callArgs, 1, 1); err != nil {
			return vm.BoolVal(false), err
		}
		if !enabled {
			return vm.BoolVal(false), nil
		}
		selector, err := browserStringArg(method, callArgs, 0)
		if err != nil || selector == "" {
			return vm.BoolVal(false), err
		}
		return vm.BoolVal(browserDefer(func() { r.callElement(selector, "focus") })), nil
	case "FocusMove":
		if err := requireBrowserArity(method, callArgs, 2, 2); err != nil {
			return vm.BoolVal(false), err
		}
		if !enabled {
			return vm.BoolVal(false), nil
		}
		selector, err := browserStringArg(method, callArgs, 0)
		if err != nil || selector == "" {
			return vm.BoolVal(false), err
		}
		direction, err := browserIntArg(method, callArgs, 1)
		if err != nil || direction == 0 {
			return vm.BoolVal(false), err
		}
		return vm.BoolVal(browserDefer(func() { r.focusMove(selector, direction) })), nil
	case "Activate":
		if err := requireBrowserArity(method, callArgs, 1, 1); err != nil {
			return vm.BoolVal(false), err
		}
		if !enabled {
			return vm.BoolVal(false), nil
		}
		selector, err := browserStringArg(method, callArgs, 0)
		if err != nil || selector == "" {
			return vm.BoolVal(false), err
		}
		// Resolve the element in the microtask too: reconciliation may replace
		// the result list after this handler updates a signal.
		return vm.BoolVal(browserDefer(func() { r.activate(selector) })), nil
	case "ClipboardWrite":
		if err := requireBrowserArity(method, callArgs, 1, 1); err != nil {
			return vm.BoolVal(false), err
		}
		if !enabled {
			return vm.BoolVal(false), nil
		}
		text, err := browserRawStringArg(method, callArgs, 0)
		if err != nil {
			return vm.BoolVal(false), err
		}
		return vm.BoolVal(browserClipboardWrite(text)), nil
	case "Navigate":
		if err := requireBrowserArity(method, callArgs, 1, 3); err != nil {
			return vm.BoolVal(false), err
		}
		if !enabled {
			return vm.BoolVal(false), nil
		}
		url, err := browserStringArg(method, callArgs, 0)
		if err != nil || url == "" {
			return vm.BoolVal(false), err
		}
		replace, err := browserOptionalBoolArg(method, callArgs, 1)
		if err != nil {
			return vm.BoolVal(false), err
		}
		preserveScroll, err := browserOptionalBoolArg(method, callArgs, 2)
		if err != nil {
			return vm.BoolVal(false), err
		}
		return vm.BoolVal(browserNavigate(url, replace, preserveScroll)), nil
	case "Refresh":
		if err := requireBrowserArity(method, callArgs, 0, 0); err != nil {
			return vm.BoolVal(false), err
		}
		if !enabled {
			return vm.BoolVal(false), nil
		}
		return vm.BoolVal(browserRefresh()), nil
	case "Submit":
		if err := requireBrowserArity(method, callArgs, 1, 1); err != nil {
			return vm.BoolVal(false), err
		}
		if !enabled {
			return vm.BoolVal(false), nil
		}
		selector, err := browserStringArg(method, callArgs, 0)
		if err != nil || selector == "" {
			return vm.BoolVal(false), err
		}
		// Queue after Dispatch/Reconcile returns so signal-derived hidden inputs
		// are patched before requestSubmit observes the form.
		return vm.BoolVal(browserDefer(func() { r.submit(selector) })), nil
	case "PreventDefault":
		if err := requireBrowserArity(method, callArgs, 0, 0); err != nil {
			return vm.BoolVal(false), err
		}
		if !enabled {
			return vm.BoolVal(false), nil
		}
		return vm.BoolVal(browserCurrentEventCall("preventDefault")), nil
	case "StopPropagation":
		if err := requireBrowserArity(method, callArgs, 0, 0); err != nil {
			return vm.BoolVal(false), err
		}
		if !enabled {
			return vm.BoolVal(false), nil
		}
		return vm.BoolVal(browserCurrentEventCall("stopPropagation")), nil
	case "ScrollIntoView":
		if err := requireBrowserArity(method, callArgs, 1, 2); err != nil {
			return vm.BoolVal(false), err
		}
		if !enabled {
			return vm.BoolVal(false), nil
		}
		selector, err := browserStringArg(method, callArgs, 0)
		if err != nil || selector == "" {
			return vm.BoolVal(false), err
		}
		behavior := ""
		if len(callArgs) == 2 {
			behavior, err = browserStringArg(method, callArgs, 1)
			if err != nil {
				return vm.BoolVal(false), err
			}
		}
		// Resolve after reconciliation so a signal may reveal, create, or move
		// the target before the browser computes its scroll position.
		return vm.BoolVal(browserDefer(func() { r.scrollIntoView(selector, behavior) })), nil
	default:
		return vm.ZeroValue(program.TypeAny), fmt.Errorf("unknown browser method %q", method)
	}
}

func browserEffectArgs(args []vm.Value) ([]vm.Value, bool) {
	if len(args) > 0 && args[0].Type == program.TypeBool {
		return args[1:], args[0].Truth()
	}
	return args, true
}

func requireBrowserArity(method string, args []vm.Value, minArgs, maxArgs int) error {
	if len(args) >= minArgs && len(args) <= maxArgs {
		return nil
	}
	if minArgs == maxArgs {
		return fmt.Errorf("browser.%s requires exactly %d arguments after an optional guard", method, minArgs)
	}
	return fmt.Errorf("browser.%s requires %d to %d arguments after an optional guard", method, minArgs, maxArgs)
}

func browserStringArg(method string, args []vm.Value, index int) (string, error) {
	value, err := browserRawStringArg(method, args, index)
	return strings.TrimSpace(value), err
}

func browserRawStringArg(method string, args []vm.Value, index int) (string, error) {
	if index >= len(args) || args[index].Type != program.TypeString {
		return "", fmt.Errorf("browser.%s argument %d must be a string", method, index+1)
	}
	return args[index].String(), nil
}

func browserOptionalBoolArg(method string, args []vm.Value, index int) (bool, error) {
	if index >= len(args) {
		return false, nil
	}
	if args[index].Type != program.TypeBool {
		return false, fmt.Errorf("browser.%s argument %d must be a boolean", method, index+1)
	}
	return args[index].Truth(), nil
}

func browserIntArg(method string, args []vm.Value, index int) (int, error) {
	if index >= len(args) || args[index].Type != program.TypeInt {
		return 0, fmt.Errorf("browser.%s argument %d must be an integer", method, index+1)
	}
	return int(args[index].Number()), nil
}

func (r *browserHostReceiver) root() (js.Value, bool) {
	document := js.Global().Get("document")
	if document.IsUndefined() || document.IsNull() || document.Get("getElementById").Type() != js.TypeFunction {
		return js.Undefined(), false
	}
	root := document.Call("getElementById", r.islandID)
	return root, !root.IsUndefined() && !root.IsNull()
}

func (r *browserHostReceiver) element(selector string) (js.Value, bool) {
	root, ok := r.root()
	if !ok || selector == "" {
		return js.Undefined(), false
	}
	if selector == ":root" {
		return root, true
	}
	if root.Get("matches").Type() == js.TypeFunction && root.Call("matches", selector).Bool() {
		return root, true
	}
	if root.Get("querySelector").Type() != js.TypeFunction {
		return js.Undefined(), false
	}
	element := root.Call("querySelector", selector)
	if browserElementOwnedByRoot(element, root) {
		return element, true
	}
	// querySelector may have found a matching element owned by a nested island.
	// Continue through the result set so a later match owned by this root can
	// still receive the effect.
	if root.Get("querySelectorAll").Type() == js.TypeFunction {
		list := root.Call("querySelectorAll", selector)
		for i := 0; i < list.Get("length").Int(); i++ {
			element = list.Index(i)
			if browserElementOwnedByRoot(element, root) {
				return element, true
			}
		}
	}
	return js.Undefined(), false
}

func (r *browserHostReceiver) callElement(selector, method string) bool {
	element, ok := r.element(selector)
	if !ok || element.Get(method).Type() != js.TypeFunction {
		return false
	}
	element.Call(method)
	return true
}

func (r *browserHostReceiver) focusMove(selector string, direction int) bool {
	candidates := r.visibleElements(selector, "focus")
	if len(candidates) == 0 {
		return false
	}
	active := js.Global().Get("document").Get("activeElement")
	current := -1
	for i := range candidates {
		if candidates[i].Equal(active) {
			current = i
			break
		}
	}
	step := 1
	if direction < 0 {
		step = -1
	}
	next := 0
	if current < 0 {
		if step < 0 {
			next = len(candidates) - 1
		}
	} else {
		next = (current + step + len(candidates)) % len(candidates)
	}
	candidates[next].Call("focus")
	return true
}

func (r *browserHostReceiver) activate(selector string) {
	candidates := r.visibleElements(selector, "click")
	if len(candidates) > 0 {
		candidates[0].Call("click")
	}
}

func (r *browserHostReceiver) visibleElements(selector, method string) []js.Value {
	root, ok := r.root()
	if !ok || root.Get("querySelectorAll").Type() != js.TypeFunction {
		return nil
	}
	candidates := make([]js.Value, 0, 8)
	if browserMatchingElement(root, selector, method) {
		candidates = append(candidates, root)
	}
	list := root.Call("querySelectorAll", selector)
	length := list.Get("length").Int()
	for i := 0; i < length; i++ {
		element := list.Index(i)
		if browserElementOwnedByRoot(element, root) && browserElementVisible(element) && element.Get(method).Type() == js.TypeFunction {
			candidates = append(candidates, element)
		}
	}
	return candidates
}

// browserElementOwnedByRoot rejects selector matches inside a nested island.
// DOM Elements always expose parentNode; treating an absent property as owned
// keeps non-DOM host shims backward-compatible without weakening real browser
// traversal.
func browserElementOwnedByRoot(element, root js.Value) bool {
	if element.IsUndefined() || element.IsNull() {
		return false
	}
	for current := element; !current.IsUndefined() && !current.IsNull(); current = current.Get("parentNode") {
		if current.Equal(root) {
			return true
		}
		if current.Get("hasAttribute").Type() == js.TypeFunction && current.Call("hasAttribute", "data-gosx-island").Bool() {
			return false
		}
		if current.Get("parentNode").IsUndefined() {
			return true
		}
	}
	return false
}

func browserMatchingElement(element js.Value, selector, method string) bool {
	return element.Get("matches").Type() == js.TypeFunction &&
		element.Call("matches", selector).Bool() &&
		browserElementVisible(element) && element.Get(method).Type() == js.TypeFunction
}

func browserElementVisible(element js.Value) bool {
	if element.IsUndefined() || element.IsNull() {
		return false
	}
	if element.Get("hidden").Type() == js.TypeBoolean && element.Get("hidden").Bool() {
		return false
	}
	if element.Get("disabled").Type() == js.TypeBoolean && element.Get("disabled").Bool() {
		return false
	}
	if element.Get("getAttribute").Type() == js.TypeFunction && element.Call("getAttribute", "aria-hidden").String() == "true" {
		return false
	}
	if element.Get("getClientRects").Type() == js.TypeFunction && element.Call("getClientRects").Get("length").Int() == 0 {
		return false
	}
	return true
}

func (r *browserHostReceiver) open(selector string) bool {
	element, ok := r.element(selector)
	if !ok {
		return false
	}
	if element.Get("open").Type() == js.TypeBoolean && element.Get("open").Bool() {
		return true
	}
	if element.Get("showModal").Type() == js.TypeFunction {
		element.Call("showModal")
		return true
	}
	element.Set("hidden", false)
	if element.Get("removeAttribute").Type() == js.TypeFunction {
		element.Call("removeAttribute", "hidden")
	}
	if element.Get("setAttribute").Type() == js.TypeFunction {
		element.Call("setAttribute", "open", "")
		element.Call("setAttribute", "aria-hidden", "false")
	}
	return true
}

func (r *browserHostReceiver) close(selector string) bool {
	element, ok := r.element(selector)
	if !ok {
		return false
	}
	if element.Get("close").Type() == js.TypeFunction {
		element.Call("close")
		return true
	}
	// A disclosure's summary must remain rendered and interactive when the
	// details body closes; hiding the whole element would hide its opener too.
	if strings.EqualFold(element.Get("tagName").String(), "details") {
		element.Set("open", false)
		if element.Get("removeAttribute").Type() == js.TypeFunction {
			element.Call("removeAttribute", "open")
		}
		return true
	}
	element.Set("hidden", true)
	if element.Get("setAttribute").Type() == js.TypeFunction {
		element.Call("setAttribute", "hidden", "")
		element.Call("setAttribute", "aria-hidden", "true")
	}
	if element.Get("removeAttribute").Type() == js.TypeFunction {
		element.Call("removeAttribute", "open")
	}
	return true
}

func (r *browserHostReceiver) submit(selector string) {
	element, ok := r.element(selector)
	if !ok {
		return
	}
	if element.Get("requestSubmit").Type() == js.TypeFunction {
		element.Call("requestSubmit")
		return
	}
	if element.Get("submit").Type() == js.TypeFunction {
		element.Call("submit")
	}
}

func (r *browserHostReceiver) scrollIntoView(selector, behavior string) bool {
	element, ok := r.element(selector)
	if !ok || element.Get("scrollIntoView").Type() != js.TypeFunction {
		return false
	}
	if behavior == "" {
		element.Call("scrollIntoView")
	} else {
		element.Call("scrollIntoView", js.ValueOf(map[string]any{"behavior": behavior, "block": "nearest"}))
	}
	return true
}

func browserCurrentEventCall(method string) bool {
	event := js.Global().Get("__gosx_current_event")
	if event.IsUndefined() || event.IsNull() || event.Get(method).Type() != js.TypeFunction {
		return false
	}
	event.Call(method)
	if method == "stopPropagation" {
		event.Set("__gosx_stop_island_fanout", true)
	}
	return true
}

func browserClipboardWrite(text string) bool {
	navigator := js.Global().Get("navigator")
	if !navigator.IsUndefined() && !navigator.IsNull() {
		clipboard := navigator.Get("clipboard")
		if !clipboard.IsUndefined() && !clipboard.IsNull() && clipboard.Get("writeText").Type() == js.TypeFunction {
			request := clipboard.Call("writeText", text)
			browserClipboardFallbackOnReject(request, text)
			return true
		}
	}
	return browserClipboardFallbackRequest(text)
}

func browserClipboardFallbackOnReject(request js.Value, text string) {
	browserPromiseFallbackOnReject(request, func() { browserClipboardFallback(text) })
}

var browserPendingPromiseCallbacks int

func browserPromiseFallbackOnReject(request js.Value, fallback func()) {
	if request.IsUndefined() || request.IsNull() || request.Get("then").Type() != js.TypeFunction {
		return
	}
	var fulfilled, rejected js.Func
	released := false
	release := func() {
		if released {
			return
		}
		released = true
		browserPendingPromiseCallbacks--
		fulfilled.Release()
		rejected.Release()
	}
	fulfilled = js.FuncOf(func(js.Value, []js.Value) any {
		release()
		return nil
	})
	rejected = js.FuncOf(func(js.Value, []js.Value) any {
		defer release()
		fallback()
		return nil
	})
	browserPendingPromiseCallbacks++
	defer func() {
		if recover() != nil {
			release()
		}
	}()
	request.Call("then", fulfilled, rejected)
}

func browserClipboardFallbackRequest(text string) bool {
	_, _, ok := browserClipboardFallbackTarget()
	return ok && browserDefer(func() { browserClipboardFallback(text) })
}

func browserClipboardFallback(text string) (copied bool) {
	defer func() {
		if recover() != nil {
			copied = false
		}
	}()
	// Clipboard.writeText is unavailable on older/insecure contexts. The
	// compatibility path runs after the island dispatch, then removes its
	// temporary textarea and restores the prior focus before returning.
	document, body, ok := browserClipboardFallbackTarget()
	if !ok {
		return false
	}
	textarea := document.Call("createElement", "textarea")
	if textarea.IsUndefined() || textarea.IsNull() || textarea.Get("select").Type() != js.TypeFunction {
		return false
	}
	textarea.Set("value", text)
	if textarea.Get("setAttribute").Type() == js.TypeFunction {
		textarea.Call("setAttribute", "readonly", "")
		textarea.Call("setAttribute", "aria-hidden", "true")
	}
	style := textarea.Get("style")
	if !style.IsUndefined() && !style.IsNull() {
		style.Set("cssText", "position:fixed;left:-9999px;opacity:0")
	}
	previousFocus := document.Get("activeElement")
	body.Call("appendChild", textarea)
	defer func() {
		body.Call("removeChild", textarea)
		if !previousFocus.IsUndefined() && !previousFocus.IsNull() && previousFocus.Get("focus").Type() == js.TypeFunction {
			previousFocus.Call("focus")
		}
	}()
	textarea.Call("select")
	result := document.Call("execCommand", "copy")
	return result.Type() == js.TypeBoolean && result.Bool()
}

func browserClipboardFallbackTarget() (js.Value, js.Value, bool) {
	document := js.Global().Get("document")
	if document.IsUndefined() || document.IsNull() || document.Get("createElement").Type() != js.TypeFunction || document.Get("execCommand").Type() != js.TypeFunction {
		return js.Undefined(), js.Undefined(), false
	}
	body := document.Get("body")
	if body.IsUndefined() || body.IsNull() || body.Get("appendChild").Type() != js.TypeFunction || body.Get("removeChild").Type() != js.TypeFunction {
		return js.Undefined(), js.Undefined(), false
	}
	return document, body, true
}

func browserNavigation() js.Value {
	gosx := js.Global().Get("__gosx")
	if !gosx.IsUndefined() && !gosx.IsNull() {
		navigation := gosx.Get("navigation")
		if !navigation.IsUndefined() && !navigation.IsNull() {
			return navigation
		}
	}
	return js.Global().Get("__gosx_page_nav")
}

func browserNavigate(url string, replace, preserveScroll bool) bool {
	target, sameOrigin, ok := browserHTTPNavigationURL(url)
	if !ok {
		return false
	}
	if !sameOrigin {
		return browserHardNavigate(target, replace)
	}
	navigation := browserNavigation()
	if !navigation.IsUndefined() && !navigation.IsNull() && navigation.Get("navigate").Type() == js.TypeFunction {
		request := navigation.Call("navigate", url, js.ValueOf(map[string]any{
			"replace":        replace,
			"preserveScroll": preserveScroll,
		}))
		browserPromiseFallbackOnReject(request, func() { browserHardNavigate(target, replace) })
		return true
	}
	return browserHardNavigate(target, replace)
}

func browserHTTPNavigationURL(raw string) (href string, sameOrigin, ok bool) {
	defer func() {
		if recover() != nil {
			href, sameOrigin, ok = "", false, false
		}
	}()
	location := js.Global().Get("location")
	constructor := js.Global().Get("URL")
	if location.IsUndefined() || location.IsNull() || constructor.Type() != js.TypeFunction {
		return "", false, false
	}
	base := location.Get("href").String()
	if base == "" {
		return "", false, false
	}
	target := constructor.New(raw, base)
	protocol := strings.ToLower(target.Get("protocol").String())
	if protocol != "http:" && protocol != "https:" {
		return "", false, false
	}
	current := constructor.New(base)
	return target.Get("href").String(), target.Get("origin").String() == current.Get("origin").String(), true
}

func browserHardNavigate(url string, replace bool) bool {
	location := js.Global().Get("location")
	if location.IsUndefined() || location.IsNull() {
		return false
	}
	if replace && location.Get("replace").Type() == js.TypeFunction {
		location.Call("replace", url)
	} else if location.Get("assign").Type() == js.TypeFunction {
		location.Call("assign", url)
	} else {
		location.Set("href", url)
	}
	return true
}

func browserReloadCurrent() bool {
	location := js.Global().Get("location")
	if location.IsUndefined() || location.IsNull() {
		return false
	}
	if location.Get("reload").Type() == js.TypeFunction {
		location.Call("reload")
		return true
	}
	url := location.Get("href").String()
	if url == "" {
		return false
	}
	return browserHardNavigate(url, true)
}

func browserRefresh() bool {
	navigation := browserNavigation()
	if !navigation.IsUndefined() && !navigation.IsNull() && navigation.Get("revalidate").Type() == js.TypeFunction {
		request := navigation.Call("revalidate", js.ValueOf(map[string]any{"preserveScroll": true}))
		browserPromiseFallbackOnReject(request, func() { browserReloadCurrent() })
		return true
	}
	pageCache := js.Global().Get("__gosx_page_cache")
	if !pageCache.IsUndefined() && !pageCache.IsNull() && pageCache.Get("clear").Type() == js.TypeFunction {
		pageCache.Call("clear")
	}
	location := js.Global().Get("location")
	if location.IsUndefined() || location.IsNull() {
		return false
	}
	url := location.Get("href").String()
	if url != "" && !navigation.IsUndefined() && !navigation.IsNull() && navigation.Get("navigate").Type() == js.TypeFunction {
		request := navigation.Call("navigate", url, js.ValueOf(map[string]any{
			"replace":        true,
			"preserveScroll": true,
			"force":          true,
			"revalidate":     true,
		}))
		browserPromiseFallbackOnReject(request, func() { browserReloadCurrent() })
		return true
	}
	return browserReloadCurrent()
}

func browserDefer(fn func()) (queued bool) {
	var callback js.Func
	callback = js.FuncOf(func(this js.Value, args []js.Value) any {
		defer callback.Release()
		defer func() { recover() }()
		fn()
		return nil
	})
	defer func() {
		if recover() != nil && !queued {
			callback.Release()
		}
	}()
	global := js.Global()
	if global.Get("queueMicrotask").Type() == js.TypeFunction {
		global.Call("queueMicrotask", callback)
		return true
	}
	promise := global.Get("Promise")
	if !promise.IsUndefined() && !promise.IsNull() && promise.Get("resolve").Type() == js.TypeFunction {
		promise.Call("resolve").Call("then", callback)
		return true
	}
	if global.Get("setTimeout").Type() == js.TypeFunction {
		global.Call("setTimeout", callback, 0)
		return true
	}
	callback.Release()
	return false
}
