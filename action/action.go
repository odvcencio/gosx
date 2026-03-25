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
)

const maxActionBodyBytes = 1024 * 1024

func isBodyTooLarge(err error) bool {
	var maxErr *http.MaxBytesError
	return errors.As(err, &maxErr)
}

// Handler is a server-side action handler.
type Handler func(ctx *Context) error

// Context provides action execution context.
type Context struct {
	// Request is the originating HTTP request (for server actions).
	Request *http.Request

	// FormData contains parsed form values.
	FormData map[string]string

	// Payload is the JSON request body (for client-invoked actions).
	Payload json.RawMessage
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

	if !r.Has(name) {
		http.Error(w, fmt.Sprintf("action %q not found", name), http.StatusNotFound)
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

	if err := r.Invoke(name, ctx); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"ok":true}`))
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
