// Package browser supplies the authoring marker used by GoSX island handlers
// for browser-owned effects.
//
// Calls in a //gosx:island handler are lowered to OpHostCall and executed by
// the island WASM host receiver. These Go functions deliberately do nothing:
// server rendering must never perform navigation, focus, clipboard, dialog,
// form, or event side effects. Import this package as
// "m31labs.dev/gosx/browser" so ordinary Go tooling can resolve the receiver
// while the GoSX compiler enforces the narrower method/arity allowlist.
package browser

// Open opens a root-scoped dialog/disclosure in an island handler.
func Open(args ...any) bool { return false }

// Close closes a root-scoped dialog/disclosure in an island handler.
func Close(args ...any) bool { return false }

// Focus focuses a root-scoped element in an island handler.
func Focus(args ...any) bool { return false }

// FocusMove moves focus through visible root-scoped matches with wrapping.
func FocusMove(args ...any) bool { return false }

// Activate defers a click on the first visible root-scoped match.
func Activate(args ...any) bool { return false }

// ClipboardWrite copies text from an island handler.
func ClipboardWrite(args ...any) bool { return false }

// Navigate accepts HTTP(S), using managed same-origin navigation and a safe
// hard-navigation fallback. Other URL schemes are rejected.
func Navigate(args ...any) bool { return false }

// Refresh starts managed same-URL network revalidation from an island handler.
func Refresh(args ...any) bool { return false }

// Submit defers requestSubmit until after island reconciliation.
func Submit(args ...any) bool { return false }

// PreventDefault prevents the event currently dispatching an island handler.
func PreventDefault(args ...any) bool { return false }

// StopPropagation stops the current event and remaining global-island fanout.
func StopPropagation(args ...any) bool { return false }

// ScrollIntoView scrolls a root-scoped element into view after reconciliation.
func ScrollIntoView(args ...any) bool { return false }
