// Package action exposes the clean-break managed POST contract.
//
// Actions are registered with Router.RegisterManagedPOST. The framework owns
// the endpoint, parser, request limits, CSRF decision, response envelope, and
// terminal upload cleanup; there is no redirect-backed handler or registry
// compatibility surface.
package action
