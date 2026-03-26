// Package action defines the event and action model for GoSX components.
//
// Actions are explicit, named event handlers that can be client-callable
// or server-callable. They replace arbitrary closure serialization with
// a tractable binding model.
package action

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"

	"github.com/odvcencio/gosx/session"
)

const maxActionBodyBytes = 1024 * 1024

const actionFlashKey = "__gosx_action_state"

func isBodyTooLarge(err error) bool {
	var maxErr *http.MaxBytesError
	return errors.As(err, &maxErr)
}

// Handler is a server-side action handler.
type Handler func(ctx *Context) error

// Result is the structured response contract for actions.
type Result struct {
	OK          bool              `json:"ok"`
	Message     string            `json:"message,omitempty"`
	Data        json.RawMessage   `json:"data,omitempty"`
	FieldErrors map[string]string `json:"fieldErrors,omitempty"`
	Values      map[string]string `json:"values,omitempty"`
	Redirect    string            `json:"redirect,omitempty"`
}

// ResultError is a structured action error with an explicit HTTP status code.
type ResultError struct {
	Status int
	Result Result
}

func (e *ResultError) Error() string {
	if e == nil {
		return ""
	}
	if e.Result.Message != "" {
		return e.Result.Message
	}
	return "action failed"
}

func (e *ResultError) StatusCode() int {
	if e == nil || e.Status == 0 {
		if len(e.Result.FieldErrors) > 0 {
			return http.StatusUnprocessableEntity
		}
		return http.StatusBadRequest
	}
	return e.Status
}

func (e *ResultError) ActionResult() Result {
	if e == nil {
		return Result{OK: false}
	}
	res := e.Result
	res.OK = false
	return res
}

// Error constructs a structured action error.
func Error(status int, message string) *ResultError {
	return &ResultError{
		Status: status,
		Result: Result{
			OK:      false,
			Message: message,
		},
	}
}

// Validation constructs a structured validation failure.
func Validation(message string, fieldErrors map[string]string, values map[string]string) *ResultError {
	return &ResultError{
		Status: http.StatusUnprocessableEntity,
		Result: Result{
			OK:          false,
			Message:     message,
			FieldErrors: cloneStrings(fieldErrors),
			Values:      cloneStrings(values),
		},
	}
}

// Redirect constructs a successful redirect result.
func Redirect(url string) *ResultError {
	return &ResultError{
		Status: http.StatusSeeOther,
		Result: Result{
			OK:       true,
			Redirect: url,
		},
	}
}

type resultProvider interface {
	ActionResult() Result
	StatusCode() int
}

// Context provides action execution context.
type Context struct {
	// Request is the originating HTTP request (for server actions).
	Request *http.Request

	// FormData contains parsed form values.
	FormData map[string]string

	// Payload is the JSON request body (for client-invoked actions).
	Payload json.RawMessage

	status int
	result *Result
}

// View is the browser-facing action state surfaced back to HTML pages after a
// redirect-backed form submission.
type View struct {
	Name   string `json:"name"`
	Status int    `json:"status"`
	Result Result `json:"result"`
}

// Submitted reports whether this action has a flashed result for the request.
func (v View) Submitted() bool {
	return v.Name != ""
}

// OK reports whether the action completed successfully.
func (v View) OK() bool {
	return v.Result.OK
}

// Message returns the action message.
func (v View) Message() string {
	return v.Result.Message
}

// Value returns a submitted form value by key.
func (v View) Value(name string) string {
	return v.Result.Values[name]
}

// Error returns a field-level error by key.
func (v View) Error(name string) string {
	return v.Result.FieldErrors[name]
}

// HasError reports whether the named field has a validation error.
func (v View) HasError(name string) bool {
	return v.Error(name) != ""
}

// Redirect returns the redirect target carried by the action result.
func (v View) Redirect() string {
	return v.Result.Redirect
}

// SetResult replaces the structured action result.
func (c *Context) SetResult(result Result) {
	if result.Values == nil && len(c.FormData) > 0 {
		result.Values = cloneStrings(c.FormData)
	}
	c.result = &result
}

// SetStatus overrides the HTTP status for the eventual response.
func (c *Context) SetStatus(status int) {
	c.status = status
}

// Success stores a structured successful result with optional message/data.
func (c *Context) Success(message string, data any) error {
	res := Result{
		OK:      true,
		Message: message,
		Values:  cloneStrings(c.FormData),
	}
	if data != nil {
		raw, err := json.Marshal(data)
		if err != nil {
			return err
		}
		res.Data = raw
	}
	c.result = &res
	if c.status == 0 {
		c.status = http.StatusOK
	}
	return nil
}

// Redirect sends a browser-friendly redirect after a successful action.
func (c *Context) Redirect(url string) {
	c.result = &Result{
		OK:       true,
		Redirect: url,
		Values:   cloneStrings(c.FormData),
	}
	if c.status == 0 {
		c.status = http.StatusSeeOther
	}
}

// ValidationFailure records field-level validation errors for the response.
func (c *Context) ValidationFailure(message string, fieldErrors map[string]string) {
	c.result = &Result{
		OK:          false,
		Message:     message,
		FieldErrors: cloneStrings(fieldErrors),
		Values:      cloneStrings(c.FormData),
	}
	c.status = http.StatusUnprocessableEntity
}

// Registry maps action names to handlers.
type Registry struct {
	mu       sync.RWMutex
	handlers map[string]Handler
}

// NewRegistry creates an empty action registry.
func NewRegistry() *Registry {
	return &Registry{
		handlers: make(map[string]Handler),
	}
}

// Register adds a named action handler.
func (r *Registry) Register(name string, handler Handler) {
	r.mu.Lock()
	r.handlers[name] = handler
	r.mu.Unlock()
}

// Invoke calls a named action handler.
func (r *Registry) Invoke(name string, ctx *Context) error {
	r.mu.RLock()
	h, ok := r.handlers[name]
	r.mu.RUnlock()

	if !ok {
		return fmt.Errorf("action %q not registered", name)
	}
	return h(ctx)
}

// Has returns true if the named action is registered.
func (r *Registry) Has(name string) bool {
	r.mu.RLock()
	_, ok := r.handlers[name]
	r.mu.RUnlock()
	return ok
}

// List returns all registered action names.
func (r *Registry) List() []string {
	r.mu.RLock()
	names := make([]string, 0, len(r.handlers))
	for name := range r.handlers {
		names = append(names, name)
	}
	r.mu.RUnlock()
	return names
}

// ServeHTTP handles action invocations over HTTP.
// POST /gosx/action/{name} with JSON body or form data.
func (r *Registry) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	name := req.PathValue("name")
	if name == "" {
		// Fallback: extract from URL path (for routers without PathValue support).
		const prefix = "/gosx/action/"
		if !strings.HasPrefix(req.URL.Path, prefix) {
			http.Error(w, "invalid action path", http.StatusBadRequest)
			return
		}
		name = strings.TrimPrefix(req.URL.Path, prefix)
		if name == "" {
			http.Error(w, "action name required", http.StatusBadRequest)
			return
		}
	}

	r.mu.RLock()
	handler, ok := r.handlers[name]
	r.mu.RUnlock()
	if !ok {
		http.Error(w, fmt.Sprintf("action %q not found", name), http.StatusNotFound)
		return
	}

	ServeHandler(w, req, handler)
}

// ServeHandler handles a single action handler over HTTP using the same form,
// JSON, redirect, and validation semantics as Registry.ServeHTTP.
func ServeHandler(w http.ResponseWriter, req *http.Request, handler Handler) {
	if handler == nil {
		http.Error(w, "action handler required", http.StatusNotFound)
		return
	}

	req.Body = http.MaxBytesReader(w, req.Body, maxActionBodyBytes)
	defer req.Body.Close()

	ctx := &Context{
		Request:  req,
		FormData: make(map[string]string),
	}

	// Parse form data or JSON body
	contentType := req.Header.Get("Content-Type")
	if strings.HasPrefix(contentType, "application/json") {
		var payload json.RawMessage
		decoder := json.NewDecoder(req.Body)
		if err := decoder.Decode(&payload); err != nil && !errors.Is(err, io.EOF) {
			if isBodyTooLarge(err) {
				http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
				return
			}
			http.Error(w, "invalid json body", http.StatusBadRequest)
			return
		}
		ctx.Payload = payload
	} else {
		if err := req.ParseForm(); err != nil {
			if isBodyTooLarge(err) {
				http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
				return
			}
			http.Error(w, "invalid form body", http.StatusBadRequest)
			return
		}

		for k, v := range req.Form {
			if len(v) > 0 {
				ctx.FormData[k] = v[0]
			}
		}
	}

	if err := handler(ctx); err != nil {
		result, status := resultFromError(err)
		writeResponse(w, req, status, result)
		return
	}

	result, status := resultFromContext(ctx)
	writeResponse(w, req, status, result)
}

// FormValues extracts typed form values from a request.
type FormValues struct {
	values map[string]string
}

// NewFormValues creates FormValues from a map.
func NewFormValues(m map[string]string) FormValues {
	return FormValues{values: m}
}

// Get returns a form value by key.
func (f FormValues) Get(key string) string {
	return f.values[key]
}

// Has returns true if the key exists.
func (f FormValues) Has(key string) bool {
	_, ok := f.values[key]
	return ok
}

// All returns all form values.
func (f FormValues) All() map[string]string {
	cp := make(map[string]string, len(f.values))
	for k, v := range f.values {
		cp[k] = v
	}
	return cp
}

func resultFromError(err error) (Result, int) {
	var structured resultProvider
	if errors.As(err, &structured) {
		return structured.ActionResult(), structured.StatusCode()
	}
	return Result{
		OK:      false,
		Message: err.Error(),
	}, http.StatusInternalServerError
}

func resultFromContext(ctx *Context) (Result, int) {
	if ctx == nil || ctx.result == nil {
		return Result{OK: true}, http.StatusOK
	}

	result := *ctx.result
	if result.Values == nil && len(ctx.FormData) > 0 {
		result.Values = cloneStrings(ctx.FormData)
	}
	if ctx.status != 0 {
		return result, ctx.status
	}
	if !result.OK {
		if hasFieldErrors(result.FieldErrors) {
			return result, http.StatusUnprocessableEntity
		}
		return result, http.StatusBadRequest
	}
	if result.Redirect != "" {
		return result, http.StatusSeeOther
	}
	return result, http.StatusOK
}

func writeResponse(w http.ResponseWriter, req *http.Request, status int, result Result) {
	if status == 0 {
		status = http.StatusOK
	}

	if shouldFlashRedirect(req) {
		if target := redirectTarget(req, result); target != "" {
			flashActionResult(req, requestActionName(req), status, result)
			http.Redirect(w, req, target, http.StatusSeeOther)
			return
		}
	}

	if shouldRedirect(req, status, result) {
		http.Redirect(w, req, redirectTarget(req, result), http.StatusSeeOther)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(result)
}

func shouldRedirect(req *http.Request, status int, result Result) bool {
	if wantsJSON(req) {
		return false
	}
	if result.Redirect != "" {
		return true
	}
	if status >= 200 && status < 300 && req != nil && req.Method == http.MethodPost && redirectTarget(req, result) != "" {
		return true
	}
	return false
}

func shouldFlashRedirect(req *http.Request) bool {
	return !wantsJSON(req) && session.Current(req) != nil
}

func redirectTarget(req *http.Request, result Result) string {
	if result.Redirect != "" {
		return result.Redirect
	}
	if req == nil {
		return ""
	}
	if target := strings.TrimSpace(req.Header.Get("Referer")); target != "" {
		return target
	}
	if actionTarget := stripActionPath(req.URL.Path); actionTarget != "" {
		return actionTarget
	}
	return req.Header.Get("Referer")
}

func wantsJSON(req *http.Request) bool {
	if req == nil {
		return true
	}
	contentType := req.Header.Get("Content-Type")
	accept := req.Header.Get("Accept")
	return strings.HasPrefix(contentType, "application/json") ||
		strings.Contains(accept, "application/json") ||
		req.Header.Get("X-Requested-With") == "XMLHttpRequest"
}

func hasFieldErrors(fieldErrors map[string]string) bool {
	return len(fieldErrors) > 0
}

func cloneStrings(src map[string]string) map[string]string {
	if len(src) == 0 {
		return nil
	}
	dst := make(map[string]string, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

// State returns the flashed action state for the given action name.
func State(req *http.Request, name string) (View, bool) {
	states := States(req)
	view, ok := states[name]
	return view, ok
}

// States returns all flashed action states for the current request.
func States(req *http.Request) map[string]View {
	values := session.FlashValues(req)
	rawViews, ok := values[actionFlashKey]
	if !ok || len(rawViews) == 0 {
		return map[string]View{}
	}

	states := make(map[string]View, len(rawViews))
	for _, raw := range rawViews {
		var view View
		data, err := json.Marshal(raw)
		if err != nil {
			continue
		}
		if err := json.Unmarshal(data, &view); err != nil || view.Name == "" {
			continue
		}
		states[view.Name] = view
	}
	return states
}

func flashActionResult(req *http.Request, name string, status int, result Result) {
	if name == "" || req == nil {
		return
	}
	session.AddFlash(req, actionFlashKey, View{
		Name:   name,
		Status: status,
		Result: result,
	})
}

func requestActionName(req *http.Request) string {
	if req == nil {
		return ""
	}
	for _, key := range []string{"__gosx_action", "name"} {
		if value := req.PathValue(key); value != "" {
			return value
		}
	}
	path := strings.Trim(req.URL.Path, "/")
	if path == "" {
		return ""
	}
	parts := strings.Split(path, "/")
	return parts[len(parts)-1]
}

func stripActionPath(path string) string {
	if path == "" {
		return ""
	}
	switch {
	case strings.Contains(path, "/__actions/"):
		base := path[:strings.Index(path, "/__actions/")]
		if base == "" {
			return "/"
		}
		return base
	default:
		return ""
	}
}
