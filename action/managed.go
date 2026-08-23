package action

// This file contains the framework-owned managed action substrate. Managed
// actions have one immutable registration policy, one bounded body reader,
// one parser cache, and one terminal cleanup path.

import (
	"bytes"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"os"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"m31labs.dev/gosx/session"
)

const (
	defaultMaxRequestBodyBytes       int64 = 1 << 20
	defaultMaxFieldBytes             int64 = 1 << 20
	defaultMaxFields                       = 256
	defaultMaxMultipartMetadataBytes int64 = 64 << 10
	defaultMaxMemoryFileBytes        int64 = 256 << 10
	defaultMaxFieldNameBytes               = 255
	defaultMaxClientFilenameBytes          = 255
	maxResultValuesBytes             int64 = 16 << 10
)

// Limits bounds every byte and part budget for a managed action. Zero values
// resolve to the documented safe defaults at registration time; no value is
// interpreted as "unlimited".
type Limits struct {
	MaxRequestBodyBytes       int64
	MaxFieldBytes             int64
	MaxFields                 int
	MaxMultipartMetadataBytes int64
	MaxMemoryFileBytes        int64
	MaxFileBytes              int64
	MaxFiles                  int
	MaxFieldNameBytes         int
	MaxClientFilenameBytes    int
}

// CSRFConfig selects the authoritative header and optional body fallback
// field. An all-zero value resolves to X-CSRF-Token and csrf_token.
type CSRFConfig struct {
	HeaderName    string
	BodyFieldName string
}

// ResultValuesConfig controls the explicitly allowlisted scalar values that a
// managed action may return.
type ResultValuesConfig struct {
	AllowedNames       []string
	MaxSerializedBytes int64
	NativeFlashValues  bool
}

// Config is the complete policy for one managed POST action.
type Config struct {
	Limits       Limits
	CSRF         CSRFConfig
	ResultValues ResultValuesConfig
}

// Result is the safe application outcome returned by a managed action.
type Result struct {
	OK             bool              `json:"ok"`
	Message        string            `json:"message,omitempty"`
	Data           json.RawMessage   `json:"data,omitempty"`
	FieldErrors    map[string]string `json:"fieldErrors,omitempty"`
	Values         map[string]string `json:"values,omitempty"`
	Redirect       string            `json:"redirect,omitempty"`
	RedirectStatus int               `json:"redirectStatus,omitempty"`
}

// Context is the handler-goroutine-scoped input for one managed action.
// Form contains only bounded body values; query parameters remain available
// through Request.URL.Query(). The framework invalidates Form uploads when
// the action returns.
type Context struct {
	Request *http.Request
	Form    ActionForm
	Payload any
}

// ActionForm is an immutable, body-only view of a successful bounded parse.
// Every accessor returns a copy so an action cannot mutate the parser cache.
type ActionForm interface {
	Value(name string) string
	Values(name string) []string
	File(name string) (Upload, error)
	Files(name string) []Upload
}

// Upload describes one bounded multipart file. Open returns a fresh reader
// for each call made before the action returns.
type Upload struct {
	FieldName         string
	ClientFilename    string
	DeclaredMediaType string
	Size              int64

	state *uploadState
}

// Open opens a fresh reader for this upload. Readers and uploads are
// invalidated during managed action cleanup after the handler returns.
func (u Upload) Open() (io.ReadCloser, error) {
	if u.state == nil {
		return nil, errors.New("upload is not available")
	}
	return u.state.open()
}

type uploadState struct {
	mu             sync.Mutex
	valid          bool
	openFn         func() (io.ReadCloser, error)
	tempPath       string
	openKind       RequestErrorKind
	closeFn        func(io.Closer) error
	removeFn       func(string) error
	readers        map[*revocableUploadReader]struct{}
	invalidateOnce sync.Once
	invalidateErr  error
	cleanupOnce    sync.Once
	cleanupErr     error
}

func (s *uploadState) open() (io.ReadCloser, error) {
	if s == nil {
		return nil, errors.New("upload is not available")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.valid || s.openFn == nil {
		return nil, errors.New("upload is no longer valid")
	}
	// Keep the state lock across the storage open.  Invalidation cannot race a
	// reader between the validity check and registration, and cleanup therefore
	// always observes and revokes every reader before removing a temp path.
	openKind := s.openKind
	r, err := s.openFn()
	if err != nil {
		if openKind == "" {
			openKind = RequestErrorUploadOpen
		}
		return nil, requestError(openKind, err)
	}
	if r == nil {
		return nil, requestError(openKindOrUploadOpen(openKind), errors.New("upload storage returned a nil reader"))
	}
	reader := &revocableUploadReader{source: r, closeFn: s.closeFn}
	if !s.valid {
		if err := reader.Close(); err != nil {
			return nil, errors.Join(errors.New("upload is no longer valid"), err)
		}
		return nil, errors.New("upload is no longer valid")
	}
	if s.readers == nil {
		s.readers = make(map[*revocableUploadReader]struct{})
	}
	s.readers[reader] = struct{}{}
	return reader, nil
}

func (s *uploadState) invalidate() error {
	if s == nil {
		return nil
	}
	s.invalidateOnce.Do(func() {
		s.mu.Lock()
		s.valid = false
		// Dropping openFn is important for in-memory uploads: it releases the
		// closure and its backing byte slice as soon as the action returns.
		s.openFn = nil
		readers := make([]*revocableUploadReader, 0, len(s.readers))
		for reader := range s.readers {
			readers = append(readers, reader)
		}
		s.readers = nil
		s.mu.Unlock()

		var errs []error
		for _, reader := range readers {
			if err := reader.revoke(); err != nil && !errors.Is(err, os.ErrClosed) && !errors.Is(err, os.ErrInvalid) {
				errs = append(errs, err)
			}
		}
		s.mu.Lock()
		s.invalidateErr = errors.Join(errs...)
		s.mu.Unlock()
	})
	s.mu.Lock()
	err := s.invalidateErr
	s.mu.Unlock()
	return err
}

func (s *uploadState) removeTemp() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	tempPath := s.tempPath
	remove := s.removeFn
	s.tempPath = ""
	s.removeFn = nil
	s.mu.Unlock()
	if tempPath == "" {
		return nil
	}
	if remove == nil {
		remove = os.Remove
	}
	var removeErr error
	removeErr = remove(tempPath)
	if errors.Is(removeErr, os.ErrNotExist) {
		removeErr = nil
	}
	if removeErr != nil {
		return requestError(RequestErrorTempRemove, removeErr)
	}
	return nil
}

func (s *uploadState) cleanup() error {
	if s == nil {
		return nil
	}
	s.cleanupOnce.Do(func() {
		closeErr := s.invalidate()
		removeErr := s.removeTemp()
		s.mu.Lock()
		s.cleanupErr = errors.Join(closeErr, removeErr)
		s.mu.Unlock()
	})
	s.mu.Lock()
	err := s.cleanupErr
	s.mu.Unlock()
	return err
}

func openKindOrUploadOpen(kind RequestErrorKind) RequestErrorKind {
	if kind == "" {
		return RequestErrorUploadOpen
	}
	return kind
}

var errUploadReaderRevoked = errors.New("upload reader is no longer valid")

// revocableUploadReader is the sole reader type exposed by Upload.Open.  It
// serializes Read/Close/revoke, makes Close idempotent, and refuses reads as
// soon as the owning action leaves its handler.
type revocableUploadReader struct {
	mu      sync.Mutex
	source  io.ReadCloser
	closeFn func(io.Closer) error
	closed  bool
	revoked bool
}

func (r *revocableUploadReader) Read(p []byte) (int, error) {
	if r == nil {
		return 0, errUploadReaderRevoked
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed || r.revoked || r.source == nil {
		return 0, errUploadReaderRevoked
	}
	return r.source.Read(p)
}

func (r *revocableUploadReader) Close() error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil
	}
	r.closed = true
	if r.source == nil {
		return nil
	}
	if r.closeFn != nil {
		return r.closeFn(r.source)
	}
	return r.source.Close()
}

func (r *revocableUploadReader) revoke() error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.revoked {
		return nil
	}
	r.revoked = true
	if r.closed {
		return nil
	}
	r.closed = true
	if r.source == nil {
		return nil
	}
	if r.closeFn != nil {
		return r.closeFn(r.source)
	}
	return r.source.Close()
}

type uploadTempFile interface {
	io.Writer
	io.Closer
	Name() string
}

type uploadStorageHooks struct {
	createTemp func() (uploadTempFile, error)
	write      func(io.Writer, []byte) error
	open       func(string) (io.ReadCloser, error)
	close      func(io.Closer) error
	remove     func(string) error
}

var defaultUploadStorage = uploadStorageHooks{
	createTemp: func() (uploadTempFile, error) {
		return os.CreateTemp("", "gosx-upload-")
	},
	write: writeUploadBytes,
	open: func(name string) (io.ReadCloser, error) {
		file, err := os.Open(name)
		if err != nil {
			return nil, err
		}
		return &opaqueUploadReader{Reader: file, closeFn: file.Close}, nil
	},
	close: func(closer io.Closer) error {
		if closer == nil {
			return nil
		}
		return closer.Close()
	},
	remove: os.Remove,
}

// uploadStorage is a package seam used by failure-path tests. Production
// code leaves it at defaultUploadStorage; callers must replace it only before
// serving requests.
var uploadStorage = defaultUploadStorage

func snapshotUploadStorage() uploadStorageHooks {
	storage := uploadStorage
	if storage.createTemp == nil {
		storage.createTemp = defaultUploadStorage.createTemp
	}
	if storage.write == nil {
		storage.write = defaultUploadStorage.write
	}
	if storage.open == nil {
		storage.open = defaultUploadStorage.open
	}
	if storage.close == nil {
		storage.close = defaultUploadStorage.close
	}
	if storage.remove == nil {
		storage.remove = defaultUploadStorage.remove
	}
	return storage
}

// opaqueUploadReader deliberately hides the backing *os.File and its path
// from action code. The only supported lifecycle is Read/Close via the
// io.ReadCloser returned by Upload.Open.
type opaqueUploadReader struct {
	io.Reader
	closeFn func() error
}

func (r *opaqueUploadReader) Close() error {
	if r == nil || r.closeFn == nil {
		return nil
	}
	return r.closeFn()
}

type managedForm struct {
	values map[string][]string
	files  map[string][]Upload
}

func (f *managedForm) Value(name string) string {
	if f == nil {
		return ""
	}
	values := f.values[name]
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func (f *managedForm) Values(name string) []string {
	if f == nil || len(f.values[name]) == 0 {
		return nil
	}
	return append([]string(nil), f.values[name]...)
}

func (f *managedForm) File(name string) (Upload, error) {
	if f == nil || len(f.files[name]) == 0 {
		return Upload{}, &MissingUploadError{FieldName: name}
	}
	if len(f.files[name]) > 1 {
		return Upload{}, &MultipleUploadsError{FieldName: name, Count: len(f.files[name])}
	}
	return f.files[name][0], nil
}

func (f *managedForm) Files(name string) []Upload {
	if f == nil || len(f.files[name]) == 0 {
		return []Upload{}
	}
	return append([]Upload(nil), f.files[name]...)
}

// ActionErrorKind identifies a safe app-authored managed outcome.
type ActionErrorKind string

const (
	ActionErrorBadRequest ActionErrorKind = "bad_request"
	ActionErrorValidation ActionErrorKind = "validation"
)

// ActionError is the only app-authored error type with a client-safe managed
// response. Arbitrary errors are always converted to a generic 500 outcome.
type ActionError struct {
	Kind        ActionErrorKind
	Message     string
	FieldErrors map[string]string
}

func (e *ActionError) Error() string {
	if e == nil || e.Message == "" {
		return "action failed"
	}
	return e.Message
}

// BadRequest constructs a safe 400 managed action error.
func BadRequest(message string) *ActionError {
	return &ActionError{Kind: ActionErrorBadRequest, Message: message}
}

// Validation constructs a safe 422 managed action error.
func Validation(message string, fieldErrors map[string]string) *ActionError {
	return &ActionError{
		Kind:        ActionErrorValidation,
		Message:     message,
		FieldErrors: cloneStrings(fieldErrors),
	}
}

// ManagedAction is invoked only after method, media, CSRF, parsing, and all
// request limits have succeeded.
type ManagedAction func(*Context) (Result, error)

// RequestErrorKind is the stable diagnostic category for framework-owned
// request failures.
type RequestErrorKind string

const (
	RequestErrorMalformed              RequestErrorKind = "malformed"
	RequestErrorMalformedJSON          RequestErrorKind = "malformed_json"
	RequestErrorBodyRead               RequestErrorKind = "body_read"
	RequestErrorCSRF                   RequestErrorKind = "csrf"
	RequestErrorMethod                 RequestErrorKind = "method"
	RequestErrorUnsupportedMedia       RequestErrorKind = "unsupported_media"
	RequestErrorBodyTooLarge           RequestErrorKind = "body_too_large"
	RequestErrorFieldsTooLarge         RequestErrorKind = "fields_too_large"
	RequestErrorTooManyFields          RequestErrorKind = "too_many_fields"
	RequestErrorFieldNameTooLarge      RequestErrorKind = "field_name_too_large"
	RequestErrorMetadataTooLarge       RequestErrorKind = "metadata_too_large"
	RequestErrorFilesNotAllowed        RequestErrorKind = "files_not_allowed"
	RequestErrorTooManyFiles           RequestErrorKind = "too_many_files"
	RequestErrorFileTooLarge           RequestErrorKind = "file_too_large"
	RequestErrorMissingUpload          RequestErrorKind = "missing_upload"
	RequestErrorMultipleUpload         RequestErrorKind = "multiple_upload"
	RequestErrorMissingPolicy          RequestErrorKind = "missing_policy"
	RequestErrorTempCreate             RequestErrorKind = "temp_create"
	RequestErrorTempWrite              RequestErrorKind = "temp_write"
	RequestErrorTempClose              RequestErrorKind = "temp_close"
	RequestErrorTempOpen               RequestErrorKind = "temp_open"
	RequestErrorTempRemove             RequestErrorKind = "temp_remove"
	RequestErrorUploadOpen             RequestErrorKind = "upload_open"
	RequestErrorHandler                RequestErrorKind = "handler"
	RequestErrorSerialization          RequestErrorKind = "serialization"
	RequestErrorResultValuesOverBudget RequestErrorKind = "result_values_over_budget"
	RequestErrorUnknown                RequestErrorKind = "unknown"
)

// RequestError is a framework-owned failure. Its cause is retained for
// errors.Is/errors.As diagnostics but never rendered to the client.
type RequestError struct {
	kind        RequestErrorKind
	statusCode  int
	managedCode string
	cause       error
}

func (e *RequestError) Error() string {
	if e == nil {
		return "managed request failed"
	}
	if e.managedCode == "" {
		return "managed request failed"
	}
	return "managed " + e.managedCode
}

func (e *RequestError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

func (e *RequestError) Kind() RequestErrorKind {
	if e == nil {
		return RequestErrorUnknown
	}
	return e.kind
}

func (e *RequestError) StatusCode() int {
	if e == nil || e.statusCode == 0 {
		return http.StatusInternalServerError
	}
	return e.statusCode
}

func (e *RequestError) ManagedCode() string {
	if e == nil || e.managedCode == "" {
		return "internal_error"
	}
	return e.managedCode
}

// MissingUploadError is returned by ActionForm.File when no file has the
// requested name.
type MissingUploadError struct{ FieldName string }

func (e *MissingUploadError) Error() string {
	return "missing upload"
}

func (e *MissingUploadError) Unwrap() error { return ErrMissingUpload }

// MultipleUploadsError is returned by ActionForm.File when a scalar lookup
// would be ambiguous.
type MultipleUploadsError struct {
	FieldName string
	Count     int
}

func (e *MultipleUploadsError) Error() string { return "multiple uploads" }

func (e *MultipleUploadsError) Unwrap() error { return ErrMultipleUploads }

var (
	ErrMissingUpload   = errors.New("missing upload")
	ErrMultipleUploads = errors.New("multiple uploads")
)

// ManagedOutcome is the exact lower-camel-case JSON envelope emitted by a
// managed action router.
type ManagedOutcome struct {
	OK             bool              `json:"ok"`
	Code           string            `json:"code"`
	Message        string            `json:"message,omitempty"`
	Data           json.RawMessage   `json:"data,omitempty"`
	FieldErrors    map[string]string `json:"fieldErrors,omitempty"`
	Values         map[string]string `json:"values,omitempty"`
	Redirect       string            `json:"redirect,omitempty"`
	RedirectStatus int               `json:"redirectStatus,omitempty"`
}

type resolvedPolicy struct {
	limits        Limits
	csrf          CSRFConfig
	allowedValues map[string]struct{}
	result        ResultValuesConfig
}

// Router serves managed action routes at /gosx/action/{name}. It is separate
// from route.Router so an action can be registered and tested without a page
// tree; applications may mount it as an ordinary http.Handler.
type Router struct {
	mu      sync.RWMutex
	actions map[string]managedRoute
	frozen  bool
}

type managedRoute struct {
	policy *resolvedPolicy
	action ManagedAction
}

// NewRouter creates an empty managed action router.
func NewRouter() *Router {
	return &Router{actions: make(map[string]managedRoute)}
}

// RegisterManagedPOST validates and atomically installs one managed POST
// route. No route is installed when validation fails.
func (r *Router) RegisterManagedPOST(name string, cfg Config, action ManagedAction) error {
	if r == nil {
		return errors.New("action router is nil")
	}
	policy, err := resolvePolicy(name, cfg, action)
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.frozen {
		return errors.New("managed action router is frozen after Build")
	}
	if r.actions == nil {
		r.actions = make(map[string]managedRoute)
	}
	if _, exists := r.actions[name]; exists {
		return fmt.Errorf("managed action %q is already registered", name)
	}
	r.actions[name] = managedRoute{policy: policy, action: action}
	return nil
}

// Freeze makes the registration set immutable for the lifetime of a built
// route tree. Build may be repeated, but registration cannot diverge after
// the first build has exposed the endpoint.
func (r *Router) Freeze() {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.frozen = true
	r.mu.Unlock()
}

// Snapshot returns an immutable registration snapshot suitable for embedding
// in a compiled route tree.  A built tree must never consult the mutable
// Router maps again: doing so would let post-Build registration or test-only
// mutation drift the handler selected by the compiled mux.
func (r *Router) Snapshot() *Router {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	snapshot := &Router{
		actions: make(map[string]managedRoute, len(r.actions)),
		frozen:  true,
	}
	for name, entry := range r.actions {
		policy := entry.policy
		if policy != nil {
			copyPolicy := *policy
			copyPolicy.allowedValues = make(map[string]struct{}, len(policy.allowedValues))
			for key := range policy.allowedValues {
				copyPolicy.allowedValues[key] = struct{}{}
			}
			copyPolicy.result.AllowedNames = append([]string(nil), policy.result.AllowedNames...)
			policy = &copyPolicy
		}
		snapshot.actions[name] = managedRoute{policy: policy, action: entry.action}
	}
	return snapshot
}

// BuildManagedActionHandler freezes the registration set and returns the
// immutable handler that an App build may safely retain. Directly mounting a
// managed Router therefore has the same build-time immutability contract as
// mounting a compiled route tree: later registrations cannot change either
// dispatch or the CSRF capability matcher.
func (r *Router) BuildManagedActionHandler() http.Handler {
	if r == nil {
		return nil
	}
	r.Freeze()
	return r.Snapshot()
}

// PreservesManagedActionCapability marks the framework-owned router as an
// explicit capability boundary.  session.Protect follows this marker only on
// wrappers that deliberately preserve the capability; an arbitrary Unwrap
// method is never treated as authorization to bypass CSRF parsing.
func (r *Router) PreservesManagedActionCapability() bool { return r != nil }

// IsManagedActionRequest reports whether the exact request path names an
// action registered on this router. It is intentionally exact: a merely
// similar path is not allowed to bypass session.Protect's ordinary CSRF
// parser. The method is deliberately ignored because the managed dispatcher
// owns the JSON 405 response for every non-POST method.
func (r *Router) IsManagedActionRequest(req *http.Request) bool {
	if r == nil || req == nil || req.URL == nil {
		return false
	}
	path := req.URL.Path
	if !strings.HasPrefix(path, "/gosx/action/") {
		return false
	}
	name := strings.TrimPrefix(path, "/gosx/action/")
	if name == "" || strings.Contains(name, "/") || strings.ContainsAny(name, "?#") || ValidateActionName(name) != nil {
		return false
	}
	r.mu.RLock()
	_, ok := r.actions[name]
	r.mu.RUnlock()
	return ok
}

// ServeHTTP dispatches a registered managed action. Unknown action paths do
// not read the request body.
func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	name := managedActionName(req)
	if name == "" {
		http.NotFound(w, req)
		return
	}
	r.mu.RLock()
	entry, ok := r.actions[name]
	r.mu.RUnlock()
	if !ok {
		http.NotFound(w, req)
		return
	}
	managedActionDispatch(w, req, entry.policy, entry.action)
}

func managedActionName(req *http.Request) string {
	if req == nil || req.URL == nil {
		return ""
	}
	path := req.URL.Path
	const prefix = "/gosx/action/"
	if strings.HasPrefix(path, prefix) {
		name := strings.TrimPrefix(path, prefix)
		if name != "" && !strings.Contains(name, "/") && ValidateActionName(name) == nil {
			return name
		}
	}
	return ""
}

func resolvePolicy(name string, cfg Config, action ManagedAction) (*resolvedPolicy, error) {
	if err := ValidateActionName(name); err != nil {
		return nil, err
	}
	if action == nil {
		return nil, errors.New("managed action is nil")
	}
	limits, err := resolveLimits(cfg.Limits)
	if err != nil {
		return nil, err
	}
	csrf, err := resolveCSRF(cfg.CSRF, limits.MaxFieldNameBytes)
	if err != nil {
		return nil, err
	}
	result, allowed, err := resolveResultValues(cfg.ResultValues, limits.MaxFieldNameBytes)
	if err != nil {
		return nil, err
	}
	return &resolvedPolicy{limits: limits, csrf: csrf, result: result, allowedValues: allowed}, nil
}

// ValidateActionName validates the one-segment grammar used by registration,
// dispatch, forms, and generated action URLs. Length is measured in bytes so
// the route budget is deterministic even for invalid Unicode input.
func ValidateActionName(name string) error {
	// Action names become one URL path segment and are also embedded in the
	// browser's action target.  Keep the grammar deliberately small and
	// bounded so encoded separators, control bytes, Unicode normalization, and
	// pathological names cannot create ambiguous routes.
	if len(name) == 0 || len(name) > 64 || !utf8.ValidString(name) {
		return fmt.Errorf("invalid managed action name")
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		if i == 0 {
			if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')) {
				return fmt.Errorf("invalid managed action name")
			}
			continue
		}
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
			(c >= '0' && c <= '9') || c == '_' || c == '-' {
			continue
		}
		return fmt.Errorf("invalid managed action name")
	}
	return nil
}

// ActionPath returns the canonical managed-action URL for a valid name. An
// empty string is returned for an invalid name so render helpers cannot emit
// an endpoint that registration would reject.
func ActionPath(name string) string {
	if err := ValidateActionName(name); err != nil {
		return ""
	}
	return "/gosx/action/" + name
}

func resolveLimits(in Limits) (Limits, error) {
	if in.MaxRequestBodyBytes < 0 || in.MaxFieldBytes < 0 || in.MaxMultipartMetadataBytes < 0 || in.MaxMemoryFileBytes < 0 || in.MaxFileBytes < 0 || in.MaxFields < 0 || in.MaxFiles < 0 || in.MaxFieldNameBytes < 0 || in.MaxClientFilenameBytes < 0 {
		return Limits{}, errors.New("managed action limits must not be negative")
	}
	out := in
	if out.MaxRequestBodyBytes == 0 {
		out.MaxRequestBodyBytes = defaultMaxRequestBodyBytes
	}
	if out.MaxFieldBytes == 0 {
		out.MaxFieldBytes = defaultMaxFieldBytes
	}
	if out.MaxFields == 0 {
		out.MaxFields = defaultMaxFields
	}
	if out.MaxMultipartMetadataBytes == 0 {
		out.MaxMultipartMetadataBytes = defaultMaxMultipartMetadataBytes
	}
	if out.MaxMemoryFileBytes == 0 {
		out.MaxMemoryFileBytes = defaultMaxMemoryFileBytes
	}
	if out.MaxFieldNameBytes == 0 {
		out.MaxFieldNameBytes = defaultMaxFieldNameBytes
	}
	if out.MaxClientFilenameBytes == 0 {
		out.MaxClientFilenameBytes = defaultMaxClientFilenameBytes
	}
	if out.MaxRequestBodyBytes <= 0 || out.MaxFieldBytes <= 0 || out.MaxFields <= 0 || out.MaxMultipartMetadataBytes <= 0 || out.MaxMemoryFileBytes <= 0 || out.MaxFieldNameBytes <= 0 || out.MaxClientFilenameBytes <= 0 {
		return Limits{}, errors.New("managed action limits must be positive after defaults")
	}
	if out.MaxFiles == 0 {
		if out.MaxFileBytes != 0 {
			return Limits{}, errors.New("MaxFileBytes must be zero when MaxFiles is zero")
		}
	} else if out.MaxFileBytes <= 0 {
		return Limits{}, errors.New("MaxFileBytes must be positive when files are enabled")
	}
	return out, nil
}

func resolveCSRF(in CSRFConfig, maxName int) (CSRFConfig, error) {
	out := in
	if in.HeaderName == "" && in.BodyFieldName == "" {
		out = CSRFConfig{HeaderName: "X-CSRF-Token", BodyFieldName: "csrf_token"}
	} else if in.HeaderName == "" || in.BodyFieldName == "" {
		return CSRFConfig{}, errors.New("CSRF config must provide both names")
	}
	if !validHeaderFieldName(out.HeaderName) {
		return CSRFConfig{}, errors.New("invalid CSRF header name")
	}
	canonical := textproto.CanonicalMIMEHeaderKey(out.HeaderName)
	if canonical == "" {
		return CSRFConfig{}, errors.New("invalid CSRF header name")
	}
	if !utf8.ValidString(out.BodyFieldName) || out.BodyFieldName == "" || len(out.BodyFieldName) > maxName {
		return CSRFConfig{}, errors.New("invalid CSRF body field name")
	}
	out.HeaderName = canonical
	return out, nil
}

func validHeaderFieldName(name string) bool {
	if name == "" {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		switch c {
		case '(', ')', '<', '>', '@', ',', ';', ':', '\\', '"', '/', '[', ']', '?', '=', '{', '}', ' ', '\t':
			return false
		}
		if c <= 0x20 || c >= 0x7f {
			return false
		}
	}
	return true
}

func resolveResultValues(in ResultValuesConfig, maxName int) (ResultValuesConfig, map[string]struct{}, error) {
	out := in
	out.AllowedNames = append([]string(nil), in.AllowedNames...)
	allowed := make(map[string]struct{}, len(out.AllowedNames))
	if len(out.AllowedNames) == 0 {
		if out.MaxSerializedBytes != 0 || out.NativeFlashValues {
			return ResultValuesConfig{}, nil, errors.New("result values require a non-empty allowlist and positive budget")
		}
		return out, allowed, nil
	}
	if out.MaxSerializedBytes <= 0 || out.MaxSerializedBytes > maxResultValuesBytes {
		return ResultValuesConfig{}, nil, errors.New("invalid result values serialization budget")
	}
	for _, name := range out.AllowedNames {
		if name == "" || !utf8.ValidString(name) || len(name) > maxName {
			return ResultValuesConfig{}, nil, errors.New("invalid result values name")
		}
		if _, exists := allowed[name]; exists {
			return ResultValuesConfig{}, nil, fmt.Errorf("duplicate result values name %q", name)
		}
		allowed[name] = struct{}{}
	}
	return out, allowed, nil
}

type bodyLimitError struct{ limit int64 }

func (e *bodyLimitError) Error() string { return "request body exceeds configured limit" }

type bodyReadError struct{ cause error }

func (e *bodyReadError) Error() string { return "request body read failed" }
func (e *bodyReadError) Unwrap() error { return e.cause }

type countingBody struct {
	inner io.ReadCloser
	limit int64
	count int64
	fail  error
}

func (b *countingBody) Read(p []byte) (int, error) {
	if b == nil || b.inner == nil {
		return 0, io.EOF
	}
	if b.fail != nil {
		return 0, b.fail
	}
	if len(p) == 0 {
		return 0, nil
	}
	remaining := b.limit - b.count
	if remaining < 0 {
		b.fail = &bodyLimitError{limit: b.limit}
		return 0, b.fail
	}
	readLen := len(p)
	if remaining < int64(readLen) {
		readLen = int(remaining) + 1
	}
	n, err := b.inner.Read(p[:readLen])
	if n < 0 || n > len(p[:readLen]) || int64(n) > b.limit-b.count {
		b.fail = &bodyLimitError{limit: b.limit}
		return n, b.fail
	}
	b.count += int64(n)
	if err != nil {
		if errors.Is(err, io.EOF) {
			return n, io.EOF
		}
		b.fail = &bodyReadError{cause: err}
		return n, b.fail
	}
	return n, nil
}

func (b *countingBody) Close() error {
	if b == nil || b.inner == nil {
		return nil
	}
	return b.inner.Close()
}

type parsedManagedBody struct {
	values      map[string][]string
	files       map[string][]Upload
	payload     any
	isMultipart bool
	uploads     []*uploadState
}

func (p *parsedManagedBody) registerUpload(upload Upload) {
	if p == nil || upload.state == nil {
		return
	}
	p.uploads = append(p.uploads, upload.state)
}

// managedActionDispatch is the sole composed dispatch point for a managed
// action. Policy resolution happens before this function is reachable.
func managedActionDispatch(w http.ResponseWriter, req *http.Request, policy *resolvedPolicy, action ManagedAction) {
	if policy == nil || action == nil {
		writeManagedError(w, requestError(RequestErrorMissingPolicy, nil))
		return
	}
	if req == nil {
		writeManagedError(w, requestError(RequestErrorMalformed, nil))
		return
	}
	inner := req.Body
	if inner == nil {
		inner = io.NopCloser(strings.NewReader(""))
	}
	body := &countingBody{inner: inner, limit: policy.limits.MaxRequestBodyBytes}
	req.Body = body
	storage := snapshotUploadStorage()
	var parsed *parsedManagedBody
	cleanupDone := false
	var cleanupErr error
	cleanupNow := func() error {
		if cleanupDone || parsed == nil {
			return cleanupErr
		}
		cleanupDone = true
		cleanupErr = cleanupManagedUploads(parsed)
		if cleanupErr != nil {
			log.Printf("[gosx] managed action cleanup failed: %s", cleanupErrorDetail(cleanupErr))
		}
		return cleanupErr
	}
	defer func() {
		cleanupNow()
		if err := body.Close(); err != nil {
			log.Printf("[gosx] managed request body close failed: %s", cleanupErrorDetail(err))
		}
	}()

	// Session middleware is intentionally observed without touching the body.
	// A managed action with no resolved session policy is a server wiring error,
	// not a client-side CSRF failure.
	if session.Current(req) == nil {
		writeManagedError(w, requestError(RequestErrorMissingPolicy, nil))
		return
	}

	if req.Method != http.MethodPost {
		err := requestError(RequestErrorMethod, nil)
		w.Header().Set("Allow", http.MethodPost)
		writeManagedError(w, err)
		return
	}
	media, boundary, err := requestMedia(req)
	if err != nil {
		writeManagedError(w, err)
		return
	}
	expected := session.Token(req)
	headerPresent, headerValue, headerValid := csrfHeader(req, policy.csrf.HeaderName)
	if headerPresent {
		if !headerValid || !constantTimeEqual(expected, headerValue) {
			writeManagedError(w, requestError(RequestErrorCSRF, nil))
			return
		}
	} else if media == "application/json" {
		// JSON has no body-token fallback. Reject before touching the body so a
		// missing header cannot be turned into a parser diagnostic.
		writeManagedError(w, requestError(RequestErrorCSRF, nil))
		return
	}
	if req.ContentLength > policy.limits.MaxRequestBodyBytes && req.ContentLength >= 0 {
		writeManagedError(w, requestError(RequestErrorBodyTooLarge, nil))
		return
	}

	parsed, parseErr := parseManagedBody(req, media, boundary, policy, storage)
	if parseErr != nil {
		writeManagedError(w, parseErr)
		return
	}
	if !headerPresent {
		if !bodyCSRFMatches(parsed, policy.csrf.BodyFieldName, expected) {
			writeManagedError(w, requestError(RequestErrorCSRF, nil))
			cleanupNow()
			return
		}
	}

	form := &managedForm{values: parsed.values, files: parsed.files}
	ctx := &Context{Request: req, Form: form, Payload: parsed.payload}
	setManagedRequestForms(req, parsed.values)

	result, actionErr := runManagedAction(action, ctx, parsed)
	if actionErr == nil {
		validated := validateManagedResult(result, policy)
		if validated != nil {
			actionErr = validated
		}
	}
	// Native URL-encoded redirects may explicitly allow selected scalar
	// values to become session flashes. Add them before the single terminal
	// session commit; writing a flash after Commit would falsely report success
	// while dropping the mutation from the cookie.
	if actionErr == nil && media == "application/x-www-form-urlencoded" && policy.result.NativeFlashValues && result.Redirect != "" && !managedWantsJSON(req) {
		for key, value := range result.Values {
			if err := session.AddFlash(req, key, value); err != nil {
				actionErr = requestError(RequestErrorHandler, err)
				break
			}
		}
	}
	// Actions may mutate the signed session. Seal that mutation before emitting
	// any outcome, then remove terminal spill files after the cookie boundary.
	commitErr := session.Commit(w, req)
	cleanupErr = cleanupNow()
	if commitErr != nil {
		writeManagedError(w, requestError(RequestErrorHandler, commitErr))
		return
	}
	if actionErr != nil {
		if requestErr, ok := actionErr.(*RequestError); ok {
			writeManagedError(w, requestErr)
		} else {
			writeManagedActionError(w, actionErr, policy)
		}
		return
	}
	outcome := outcomeFromResult(result)
	writeManagedResult(w, req, media, policy, result, outcome)
}

func runManagedAction(action ManagedAction, ctx *Context, parsed *parsedManagedBody) (result Result, err error) {
	defer func() {
		if recover() != nil {
			result = Result{}
			err = requestError(RequestErrorHandler, nil)
		}
	}()
	return action(ctx)
}

func requestMedia(req *http.Request) (string, string, *RequestError) {
	for _, headerValue := range headerValuesFold(req.Header, "Content-Encoding") {
		for _, encoding := range strings.Split(headerValue, ",") {
			encoding = strings.TrimSpace(encoding)
			if encoding == "" {
				return "", "", requestError(RequestErrorMalformed, nil)
			}
			if !strings.EqualFold(encoding, "identity") {
				return "", "", requestError(RequestErrorUnsupportedMedia, nil)
			}
		}
	}
	contentTypes := headerValuesFold(req.Header, "Content-Type")
	if len(contentTypes) > 1 {
		return "", "", requestError(RequestErrorMalformed, nil)
	}
	contentType := ""
	if len(contentTypes) == 1 {
		contentType = contentTypes[0]
	}
	if contentType == "" {
		return "", "", requestError(RequestErrorUnsupportedMedia, nil)
	}
	media, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		return "", "", requestError(RequestErrorMalformed, err)
	}
	switch media {
	case "application/json", "application/x-www-form-urlencoded":
		return media, "", nil
	case "multipart/form-data":
		boundary, ok := params["boundary"]
		if !ok || boundary == "" || len(boundary) > 70 || strings.ContainsAny(boundary, "\r\n") {
			return "", "", requestError(RequestErrorMalformed, nil)
		}
		return media, boundary, nil
	default:
		return "", "", requestError(RequestErrorUnsupportedMedia, nil)
	}
}

func headerValuesFold(header http.Header, name string) []string {
	var values []string
	for key, got := range header {
		if strings.EqualFold(key, name) {
			values = append(values, got...)
		}
	}
	return values
}

func csrfHeader(req *http.Request, name string) (present bool, value string, valid bool) {
	if req == nil {
		return false, "", false
	}
	var values []string
	for key, got := range req.Header {
		if strings.EqualFold(key, name) {
			present = true
			values = append(values, got...)
		}
	}
	if !present || len(values) != 1 || values[0] == "" {
		return present, "", false
	}
	return true, values[0], true
}

func constantTimeEqual(expected, actual string) bool {
	return expected != "" && subtle.ConstantTimeCompare([]byte(expected), []byte(actual)) == 1
}

func bodyCSRFMatches(parsed *parsedManagedBody, field, expected string) bool {
	if parsed == nil {
		return false
	}
	if len(parsed.files[field]) != 0 {
		return false
	}
	values := parsed.values[field]
	return len(values) == 1 && values[0] != "" && constantTimeEqual(expected, values[0])
}

func parseManagedBody(req *http.Request, media, boundary string, policy *resolvedPolicy, storage uploadStorageHooks) (*parsedManagedBody, *RequestError) {
	switch media {
	case "application/json":
		return parseJSONBody(req, policy)
	case "application/x-www-form-urlencoded":
		return parseURLBody(req, policy)
	case "multipart/form-data":
		return parseMultipartBody(req, boundary, policy, storage)
	default:
		return nil, requestError(RequestErrorUnsupportedMedia, nil)
	}
}

func readCompleteBody(req *http.Request) ([]byte, *RequestError) {
	data, err := io.ReadAll(req.Body)
	if err == nil {
		return data, nil
	}
	return nil, classifyBodyError(err)
}

func classifyBodyError(err error) *RequestError {
	var tooLarge *bodyLimitError
	if errors.As(err, &tooLarge) {
		return requestError(RequestErrorBodyTooLarge, err)
	}
	return requestError(RequestErrorBodyRead, err)
}

// checkedAdd adds a non-negative budget component without allowing an
// overflowing int64 to wrap back below the configured limit.
func checkedAdd(current, delta, limit int64) (int64, bool) {
	if current < 0 || delta < 0 || limit < 0 || current > limit || delta > limit-current {
		return current, false
	}
	return current + delta, true
}

func parseURLBody(req *http.Request, policy *resolvedPolicy) (*parsedManagedBody, *RequestError) {
	data, err := readCompleteBody(req)
	if err != nil {
		return nil, err
	}
	values, parseErr := parseURLEncoded(data, policy.limits)
	if parseErr != nil {
		return nil, parseErr
	}
	return &parsedManagedBody{values: values, files: make(map[string][]Upload)}, nil
}

func parseURLEncoded(data []byte, limits Limits) (map[string][]string, *RequestError) {
	values := make(map[string][]string)
	if len(data) == 0 {
		return values, nil
	}
	parts := bytes.Split(data, []byte{'&'})
	var fieldBytes int64
	for i, part := range parts {
		if len(part) == 0 {
			return nil, requestError(RequestErrorMalformed, nil)
		}
		if bytes.Contains(part, []byte{';'}) {
			return nil, requestError(RequestErrorMalformed, nil)
		}
		if i >= limits.MaxFields {
			return nil, requestError(RequestErrorTooManyFields, nil)
		}
		eq := bytes.IndexByte(part, '=')
		if eq < 0 {
			return nil, requestError(RequestErrorMalformed, nil)
		}
		name, err := url.QueryUnescape(string(part[:eq]))
		if err != nil || !utf8.ValidString(name) || name == "" {
			return nil, requestError(RequestErrorMalformed, err)
		}
		if len(name) > limits.MaxFieldNameBytes {
			return nil, requestError(RequestErrorFieldNameTooLarge, nil)
		}
		value, err := url.QueryUnescape(string(part[eq+1:]))
		if err != nil || !utf8.ValidString(value) {
			return nil, requestError(RequestErrorMalformed, err)
		}
		var ok bool
		fieldBytes, ok = checkedAdd(fieldBytes, int64(len(name)), limits.MaxFieldBytes)
		if !ok {
			return nil, requestError(RequestErrorFieldsTooLarge, nil)
		}
		fieldBytes, ok = checkedAdd(fieldBytes, int64(len(value)), limits.MaxFieldBytes)
		if !ok {
			return nil, requestError(RequestErrorFieldsTooLarge, nil)
		}
		values[name] = append(values[name], value)
	}
	return values, nil
}

func parseJSONBody(req *http.Request, policy *resolvedPolicy) (*parsedManagedBody, *RequestError) {
	data, err := readCompleteBody(req)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 || !utf8.Valid(data) {
		return nil, requestError(RequestErrorMalformedJSON, nil)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var payload any
	if err := decoder.Decode(&payload); err != nil {
		return nil, requestError(RequestErrorMalformedJSON, err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return nil, requestError(RequestErrorMalformedJSON, err)
	}
	return &parsedManagedBody{values: make(map[string][]string), files: make(map[string][]Upload), payload: payload}, nil
}

func setManagedRequestForms(req *http.Request, values map[string][]string) {
	if req == nil {
		return
	}
	form := cloneURLValues(values)
	req.PostForm = cloneURLValues(form)
	req.Form = form
}

func cloneURLValues(values map[string][]string) url.Values {
	cloned := make(url.Values, len(values))
	for key, list := range values {
		cloned[key] = append([]string(nil), list...)
	}
	return cloned
}

func parseMultipartBody(req *http.Request, boundary string, policy *resolvedPolicy, storage uploadStorageHooks) (parsed *parsedManagedBody, parseErr *RequestError) {
	// multipart.Reader delegates header parsing to net/textproto.  On current
	// Go releases that parser materializes a MIME header line before returning
	// it, under its own 10 MiB guard.  Put the GoSX budget in front of that
	// materialization: the scanner rejects raw header bytes as they arrive,
	// while multipart.Reader still owns boundary parsing, quoted parameters,
	// continuations, duplicate headers, and part-body streaming.
	metadataReader := newMultipartMetadataBudgetReader(req.Body, boundary, policy.limits.MaxMultipartMetadataBytes)
	reader := multipart.NewReader(metadataReader, boundary)
	parsed = &parsedManagedBody{
		values:      make(map[string][]string),
		files:       make(map[string][]Upload),
		isMultipart: true,
	}
	owned := parsed
	// Parsing owns cleanup for every pre-action failure. The dispatcher owns
	// cleanup after a successful parse. Keeping this terminal boundary here
	// means all already-registered uploads are attempted exactly once and all
	// cleanup causes are logged in one aggregate record.
	defer func() {
		if parseErr == nil {
			return
		}
		currentCleanupErr := cleanupFailureFrom(parseErr)
		cleanupErr := cleanupManagedUploads(owned)
		if currentCleanupErr == nil && cleanupErr == nil {
			return
		}
		allCleanupErr := errors.Join(currentCleanupErr, cleanupErr)
		log.Printf("[gosx] managed action cleanup failed: %s", cleanupErrorDetail(allCleanupErr))
		if cleanupErr != nil {
			parseErr = requestErrorWithCleanup(parseErr, cleanupErr)
		}
	}()
	var fieldTotal int64
	var fields int
	var files int
	var memoryUsed int64

	for {
		part, err := reader.NextRawPart()
		if err == io.EOF {
			if drainErr := drainBody(req.Body); drainErr != nil {
				return nil, drainErr
			}
			return parsed, nil
		}
		if err != nil {
			if errors.Is(err, errMultipartMetadataLimit) {
				return nil, requestError(RequestErrorMetadataTooLarge, err)
			}
			if isBodyError(err) {
				return nil, classifyBodyError(err)
			}
			return nil, requestError(RequestErrorMalformed, err)
		}

		if hasNonEmptyHeader(part.Header, "Content-Transfer-Encoding") {
			return nil, requestError(RequestErrorMalformed, nil)
		}
		name, filename, isFile, err := multipartDisposition(part.Header)
		if err != nil {
			return nil, requestError(RequestErrorMalformed, err)
		}
		if !utf8.ValidString(name) || name == "" {
			return nil, requestError(RequestErrorMalformed, nil)
		}
		if len(name) > policy.limits.MaxFieldNameBytes {
			return nil, requestError(RequestErrorFieldNameTooLarge, nil)
		}
		declaredMediaType, mediaErr := multipartDeclaredMediaType(part.Header)
		if mediaErr != nil {
			return nil, requestError(RequestErrorMalformed, mediaErr)
		}

		if isFile {
			if policy.limits.MaxFiles == 0 {
				return nil, requestError(RequestErrorFilesNotAllowed, nil)
			}
			if files >= policy.limits.MaxFiles {
				return nil, requestError(RequestErrorTooManyFiles, nil)
			}
			files++
			upload, readErr := readMultipartUpload(part, name, sanitizeFilename(filename, policy.limits.MaxClientFilenameBytes), declaredMediaType, policy.limits, &memoryUsed, storage)
			if readErr != nil {
				return nil, readErr
			}
			parsed.files[name] = append(parsed.files[name], upload)
			parsed.registerUpload(upload)
			continue
		}

		if fields >= policy.limits.MaxFields {
			return nil, requestError(RequestErrorTooManyFields, nil)
		}
		fields++
		value, readErr := readMultipartField(part, policy.limits.MaxFieldBytes-fieldTotal)
		if readErr != nil {
			return nil, readErr
		}
		var fieldOK bool
		fieldTotal, fieldOK = checkedAdd(fieldTotal, int64(len(value)), policy.limits.MaxFieldBytes)
		if !fieldOK {
			return nil, requestError(RequestErrorFieldsTooLarge, nil)
		}
		parsed.values[name] = append(parsed.values[name], string(value))
	}
}

var errMultipartMetadataLimit = errors.New("multipart metadata exceeds configured limit")

type multipartMetadataScanState uint8

const (
	multipartScanPreamble multipartMetadataScanState = iota
	multipartScanHeaders
	multipartScanBody
	multipartScanDone
)

// multipartMetadataBudgetReader is a raw-byte guard in front of
// multipart.Reader. It recognizes only the RFC boundary line shape, so body
// bytes (including quoted values, duplicate headers, continuations, and
// arbitrary binary data) are left to the standard parser. Header bytes are
// counted before textproto can build its MIMEHeader map.
type multipartMetadataBudgetReader struct {
	inner          io.Reader
	delimiter      []byte
	max            int64
	metadata       int64
	state          multipartMetadataScanState
	lineStart      bool
	pendingCR      bool
	candidate      []byte
	candidatePhase uint8
	headerTail     [4]byte
	headerTailN    int
	exceeded       bool
}

func newMultipartMetadataBudgetReader(inner io.Reader, boundary string, max int64) *multipartMetadataBudgetReader {
	return &multipartMetadataBudgetReader{
		inner:     inner,
		delimiter: append([]byte("--"), []byte(boundary)...),
		max:       max,
		state:     multipartScanPreamble,
		lineStart: true,
	}
}

func (r *multipartMetadataBudgetReader) Read(p []byte) (int, error) {
	if r == nil || r.inner == nil {
		return 0, io.EOF
	}
	if r.exceeded {
		return 0, errMultipartMetadataLimit
	}
	if len(p) == 0 {
		return 0, nil
	}
	n, readErr := r.inner.Read(p)
	if n < 0 || n > len(p) {
		return 0, errors.New("multipart metadata reader returned an invalid byte count")
	}
	for i := 0; i < n; i++ {
		if err := r.scanByte(p[i]); err != nil {
			r.exceeded = true
			// Do not expose the first over-budget byte to textproto. The
			// underlying read may have filled the rest of p, but those bytes
			// remain private to this terminal read and are never materialized
			// as a header map.
			return i, err
		}
	}
	if readErr != nil {
		return n, readErr
	}
	return n, nil
}

func (r *multipartMetadataBudgetReader) scanByte(b byte) error {
	switch r.state {
	case multipartScanHeaders:
		return r.scanHeaderByte(b)
	case multipartScanPreamble, multipartScanBody:
		return r.scanOutsideByte(b)
	default:
		return nil
	}
}

func (r *multipartMetadataBudgetReader) scanHeaderByte(b byte) error {
	if r.metadata == math.MaxInt64 || r.metadata >= r.max {
		return errMultipartMetadataLimit
	}
	r.metadata++
	if r.headerTailN < len(r.headerTail) {
		r.headerTail[r.headerTailN] = b
		r.headerTailN++
	} else {
		copy(r.headerTail[:], r.headerTail[1:])
		r.headerTail[len(r.headerTail)-1] = b
	}
	if r.headerTailN == len(r.headerTail) && string(r.headerTail[:]) == "\r\n\r\n" {
		r.state = multipartScanBody
		// The CRLF that terminates the headers also places an empty part
		// body at a boundary line start.
		r.lineStart = true
		r.pendingCR = false
		r.headerTailN = 0
	}
	return nil
}

func (r *multipartMetadataBudgetReader) scanOutsideByte(b byte) error {
	if len(r.candidate) != 0 {
		return r.scanCandidateByte(b)
	}
	if r.lineStart && b == '-' {
		r.candidate = append(r.candidate[:0], b)
		r.candidatePhase = 0
		return nil
	}
	r.scanOrdinaryOutsideByte(b)
	return nil
}

func (r *multipartMetadataBudgetReader) scanCandidateByte(b byte) error {
	switch r.candidatePhase {
	case 0:
		if len(r.candidate) < len(r.delimiter) && b == r.delimiter[len(r.candidate)] {
			r.candidate = append(r.candidate, b)
			if len(r.candidate) == len(r.delimiter) {
				r.candidatePhase = 1
			}
			return nil
		}
		return r.replayCandidate(b)
	case 1:
		if b == '\r' {
			r.candidate = append(r.candidate, b)
			r.candidatePhase = 2
			return nil
		}
		if b == '-' {
			r.candidate = append(r.candidate, b)
			r.candidatePhase = 3
			return nil
		}
		return r.replayCandidate(b)
	case 2:
		if b == '\n' {
			r.state = multipartScanHeaders
			r.candidate = r.candidate[:0]
			r.candidatePhase = 0
			r.headerTailN = 0
			return nil
		}
		return r.replayCandidate(b)
	case 3:
		if b == '-' {
			r.state = multipartScanDone
			r.candidate = r.candidate[:0]
			r.candidatePhase = 0
			return nil
		}
		return r.replayCandidate(b)
	default:
		return r.replayCandidate(b)
	}
}

func (r *multipartMetadataBudgetReader) replayCandidate(current byte) error {
	candidate := append([]byte(nil), r.candidate...)
	r.candidate = r.candidate[:0]
	r.candidatePhase = 0
	for _, b := range candidate {
		r.scanOrdinaryOutsideByte(b)
	}
	r.scanOrdinaryOutsideByte(current)
	return nil
}

func (r *multipartMetadataBudgetReader) scanOrdinaryOutsideByte(b byte) {
	if r.pendingCR {
		r.pendingCR = false
		if b == '\n' {
			r.lineStart = true
			return
		}
		r.lineStart = false
	}
	if b == '\r' {
		r.pendingCR = true
		r.lineStart = false
		return
	}
	r.lineStart = false
}

func isBodyError(err error) bool {
	var limitErr *bodyLimitError
	var readErr *bodyReadError
	return errors.As(err, &limitErr) || errors.As(err, &readErr)
}

func drainBody(body io.Reader) *RequestError {
	if body == nil {
		return nil
	}
	_, err := io.Copy(io.Discard, body)
	if err != nil {
		return classifyBodyError(err)
	}
	return nil
}

func multipartMetadataBytes(part *multipart.Part) int64 {
	var total int64
	if part == nil {
		return 0
	}
	for key, values := range part.Header {
		for _, value := range values {
			delta := int64(len(key))
			if delta < 0 || delta > math.MaxInt64-2 {
				return math.MaxInt64
			}
			delta += 2
			if int64(len(value)) > math.MaxInt64-delta-2 {
				return math.MaxInt64
			}
			delta += int64(len(value)) + 2
			if total > math.MaxInt64-delta {
				return math.MaxInt64
			}
			total += delta
		}
	}
	return total
}

func hasNonEmptyHeader(header textproto.MIMEHeader, name string) bool {
	for key, values := range header {
		if !strings.EqualFold(key, name) {
			continue
		}
		for _, value := range values {
			if value != "" {
				return true
			}
		}
	}
	return false
}

func multipartDisposition(header textproto.MIMEHeader) (name, filename string, file bool, err error) {
	values := header.Values("Content-Disposition")
	if len(values) != 1 {
		return "", "", false, errors.New("multipart Content-Disposition is required exactly once")
	}
	mediaType, params, err := mime.ParseMediaType(values[0])
	if err != nil || !strings.EqualFold(mediaType, "form-data") {
		return "", "", false, errors.New("invalid multipart Content-Disposition")
	}
	if hasDispositionParameter(values[0], "filename*") || hasDispositionParameter(values[0], "name*") {
		return "", "", false, errors.New("extended multipart disposition parameter is not supported")
	}
	var hasName, hasFilename bool
	for key, value := range params {
		switch strings.ToLower(key) {
		case "name":
			if hasName {
				return "", "", false, errors.New("duplicate multipart name")
			}
			hasName, name = true, value
		case "filename":
			if hasFilename {
				return "", "", false, errors.New("duplicate multipart filename")
			}
			hasFilename, filename = true, value
		case "filename*":
			return "", "", false, errors.New("filename* is not supported")
		default:
			return "", "", false, errors.New("unsupported multipart disposition parameter")
		}
	}
	if !hasName || name == "" {
		return "", "", false, errors.New("multipart name is required")
	}
	file = hasFilename
	return name, filename, file, nil
}

func hasDispositionParameter(value, target string) bool {
	start := 0
	inQuote := false
	escaped := false
	for i := 0; i <= len(value); i++ {
		atEnd := i == len(value)
		if !atEnd {
			switch value[i] {
			case '\\':
				if inQuote {
					escaped = !escaped
					continue
				}
			case '"':
				if !escaped {
					inQuote = !inQuote
				}
			}
			if value[i] != '\\' {
				escaped = false
			}
		}
		if !atEnd && (value[i] != ';' || inQuote) {
			continue
		}
		segment := strings.TrimSpace(value[start:i])
		start = i + 1
		if eq := strings.IndexByte(segment, '='); eq >= 0 {
			name := strings.TrimSpace(segment[:eq])
			if strings.EqualFold(name, target) || strings.HasPrefix(strings.ToLower(name), target) {
				return true
			}
		}
	}
	return false
}

func multipartDeclaredMediaType(header textproto.MIMEHeader) (string, error) {
	var values []string
	for key, got := range header {
		if strings.EqualFold(key, "Content-Type") {
			values = append(values, got...)
		}
	}
	if len(values) == 0 {
		return "", nil
	}
	if len(values) != 1 || strings.TrimSpace(values[0]) == "" {
		return "", errors.New("invalid multipart Content-Type")
	}
	mediaType, _, err := mime.ParseMediaType(values[0])
	if err != nil || mediaType == "" {
		return "", errors.New("invalid multipart Content-Type")
	}
	mediaType = strings.ToLower(mediaType)
	return mediaType, nil
}

func readMultipartField(part io.Reader, remaining int64) ([]byte, *RequestError) {
	if remaining < 0 {
		remaining = 0
	}
	readLimit := remaining
	if readLimit < math.MaxInt64 {
		readLimit++
	}
	data, err := io.ReadAll(io.LimitReader(part, readLimit))
	if err != nil {
		if isBodyError(err) {
			return nil, classifyBodyError(err)
		}
		return nil, requestError(RequestErrorBodyRead, err)
	}
	if int64(len(data)) > remaining {
		return nil, requestError(RequestErrorFieldsTooLarge, nil)
	}
	return data, nil
}

func readMultipartUpload(part io.Reader, fieldName, filename, mediaType string, limits Limits, memoryUsed *int64, storage uploadStorageHooks) (Upload, *RequestError) {
	storage = normalizeUploadStorage(storage)
	closeFn := storage.close
	state := &uploadState{valid: true, closeFn: closeFn, openKind: RequestErrorUploadOpen}
	var memory bytes.Buffer
	var temp uploadTempFile
	var size int64
	closeTemp := func() error {
		if temp == nil {
			return nil
		}
		current := temp
		temp = nil
		return closeFn(current)
	}
	cleanupState := func() error {
		var errs []error
		if temp != nil {
			if err := closeTemp(); err != nil {
				errs = append(errs, requestError(RequestErrorTempClose, err))
			}
		}
		if err := state.cleanup(); err != nil {
			errs = append(errs, err)
		}
		return errors.Join(errs...)
	}
	fail := func(primary *RequestError) (Upload, *RequestError) {
		return Upload{}, requestErrorWithCleanup(primary, cleanupState())
	}

	buf := make([]byte, 32*1024)
	for {
		readBuf := buf
		remainingFile := limits.MaxFileBytes - size
		if remainingFile < int64(len(readBuf)) {
			readLen := int(remainingFile) + 1
			if readLen < 1 {
				readLen = 1
			}
			readBuf = buf[:readLen]
		}
		n, err := part.Read(readBuf)
		if n > 0 {
			if int64(n) > math.MaxInt64-size {
				return fail(requestError(RequestErrorFileTooLarge, nil))
			}
			allowed := int64(n)
			if size+allowed > limits.MaxFileBytes {
				allowed = limits.MaxFileBytes - size
				if allowed < 0 {
					allowed = 0
				}
			}
			if allowed > 0 {
				chunk := buf[:allowed]
				if temp == nil {
					memoryRemaining := limits.MaxMemoryFileBytes - *memoryUsed
					if memoryRemaining < 0 {
						memoryRemaining = 0
					}
					keep := int64(len(chunk))
					if keep > memoryRemaining {
						keep = memoryRemaining
					}
					if keep > 0 {
						written, memoryErr := memory.Write(chunk[:keep])
						if memoryErr != nil {
							return fail(requestError(RequestErrorTempWrite, memoryErr))
						}
						if written != int(keep) {
							return fail(requestError(RequestErrorTempWrite, io.ErrShortWrite))
						}
						updatedMemory, ok := checkedAdd(*memoryUsed, keep, limits.MaxMemoryFileBytes)
						if !ok {
							return fail(requestError(RequestErrorTempWrite, errors.New("memory upload budget overflow")))
						}
						*memoryUsed = updatedMemory
					}
					if keep < int64(len(chunk)) {
						var createErr error
						createTemp := storage.createTemp
						temp, createErr = createTemp()
						if createErr != nil || temp == nil {
							if createErr == nil {
								createErr = errors.New("temporary upload storage returned a nil file")
							}
							return fail(requestError(RequestErrorTempCreate, createErr))
						}
						state.tempPath = temp.Name()
						state.removeFn = storage.remove
						write := storage.write
						if createErr = write(temp, memory.Bytes()); createErr == nil {
							createErr = write(temp, chunk[keep:])
						}
						if createErr != nil {
							return fail(requestError(RequestErrorTempWrite, createErr))
						}
					}
				} else {
					write := storage.write
					if writeErr := write(temp, chunk); writeErr != nil {
						return fail(requestError(RequestErrorTempWrite, writeErr))
					}
				}
			}
			size += int64(n)
			if int64(n) > allowed {
				return fail(requestError(RequestErrorFileTooLarge, nil))
			}
		}
		if err != nil {
			if !errors.Is(err, io.EOF) {
				if isBodyError(err) {
					return fail(classifyBodyError(err))
				}
				return fail(requestError(RequestErrorBodyRead, err))
			}
			break
		}
	}
	if temp != nil {
		if err := closeTemp(); err != nil {
			cleanupErr := cleanupState()
			return Upload{}, requestErrorWithCleanup(requestError(RequestErrorTempClose, err), cleanupErr)
		}
		state.openKind = RequestErrorTempOpen
		state.openFn = func() (io.ReadCloser, error) {
			path := state.tempPath
			r, err := storage.open(path)
			if err != nil {
				return nil, err
			}
			return r, nil
		}
	} else {
		data := append([]byte(nil), memory.Bytes()...)
		state.openFn = func() (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(data)), nil
		}
	}
	return Upload{FieldName: fieldName, ClientFilename: filename, DeclaredMediaType: mediaType, Size: size, state: state}, nil
}

func normalizeUploadStorage(storage uploadStorageHooks) uploadStorageHooks {
	if storage.createTemp == nil {
		storage.createTemp = defaultUploadStorage.createTemp
	}
	if storage.write == nil {
		storage.write = defaultUploadStorage.write
	}
	if storage.open == nil {
		storage.open = defaultUploadStorage.open
	}
	if storage.close == nil {
		storage.close = defaultUploadStorage.close
	}
	if storage.remove == nil {
		storage.remove = defaultUploadStorage.remove
	}
	return storage
}

func writeUploadBytes(dst io.Writer, data []byte) error {
	if len(data) == 0 {
		return nil
	}
	n, err := dst.Write(data)
	if n != len(data) {
		return io.ErrShortWrite
	}
	return err
}

func sanitizeFilename(filename string, maxBytes int) string {
	if !utf8.ValidString(filename) {
		filename = strings.ToValidUTF8(filename, "�")
	}
	var out []rune
	for _, r := range filename {
		if r == '/' || r == '\\' || unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			out = append(out, '_')
			continue
		}
		out = append(out, r)
	}
	clean := string(out)
	if len(clean) <= maxBytes {
		return clean
	}
	encoded := []byte(clean)
	cut := maxBytes
	for cut > 0 && cut < len(encoded) && encoded[cut]&0xc0 == 0x80 {
		cut--
	}
	return string(encoded[:cut])
}

func cleanupManagedUploads(parsed *parsedManagedBody) error {
	if parsed == nil {
		return nil
	}
	states := parsed.uploads
	if len(states) == 0 {
		for _, uploads := range parsed.files {
			for _, upload := range uploads {
				if upload.state != nil {
					states = append(states, upload.state)
				}
			}
		}
	}
	seen := make(map[*uploadState]struct{}, len(states))
	var errs []error
	for _, state := range states {
		if state == nil {
			continue
		}
		if _, ok := seen[state]; ok {
			continue
		}
		seen[state] = struct{}{}
		if err := state.cleanup(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func requestErrorWithCleanup(primary *RequestError, cleanupErr error) *RequestError {
	if primary == nil || cleanupErr == nil {
		return primary
	}
	return &RequestError{
		kind:        primary.kind,
		statusCode:  primary.statusCode,
		managedCode: primary.managedCode,
		cause:       errors.Join(primary, &cleanupFailure{err: cleanupErr}),
	}
}

type cleanupFailure struct{ err error }

func (e *cleanupFailure) Error() string {
	if e == nil || e.err == nil {
		return "managed upload cleanup failed"
	}
	return e.err.Error()
}

func (e *cleanupFailure) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.err
}

func cleanupFailureFrom(err error) error {
	if err == nil {
		return nil
	}
	var failure *cleanupFailure
	if errors.As(err, &failure) && failure != nil {
		return failure.err
	}
	return nil
}

// cleanupErrorDetail keeps the public RequestError message generic while
// retaining every underlying close/remove cause in the one terminal log
// record. It understands both errors.Join and ordinary wrapped errors.
func cleanupErrorDetail(err error) string {
	if err == nil {
		return ""
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		parts := make([]string, 0)
		for _, cause := range joined.Unwrap() {
			if detail := cleanupErrorDetail(cause); detail != "" {
				parts = append(parts, detail)
			}
		}
		return strings.Join(parts, "; ")
	}
	if wrapped, ok := err.(interface{ Unwrap() error }); ok {
		if cause := wrapped.Unwrap(); cause != nil {
			if detail := cleanupErrorDetail(cause); detail != "" {
				return err.Error() + " (cause: " + detail + ")"
			}
		}
	}
	return err.Error()
}

type requestErrorSpec struct {
	status int
	code   string
}

var requestErrorSpecs = map[RequestErrorKind]requestErrorSpec{
	RequestErrorMalformed:              {http.StatusBadRequest, "bad_request"},
	RequestErrorMalformedJSON:          {http.StatusBadRequest, "bad_request"},
	RequestErrorBodyRead:               {http.StatusBadRequest, "bad_request"},
	RequestErrorCSRF:                   {http.StatusForbidden, "csrf_failed"},
	RequestErrorMethod:                 {http.StatusMethodNotAllowed, "method_not_allowed"},
	RequestErrorUnsupportedMedia:       {http.StatusUnsupportedMediaType, "unsupported_media"},
	RequestErrorBodyTooLarge:           {http.StatusRequestEntityTooLarge, "request_too_large"},
	RequestErrorFieldsTooLarge:         {http.StatusRequestEntityTooLarge, "request_too_large"},
	RequestErrorTooManyFields:          {http.StatusRequestEntityTooLarge, "request_too_large"},
	RequestErrorFieldNameTooLarge:      {http.StatusRequestEntityTooLarge, "request_too_large"},
	RequestErrorMetadataTooLarge:       {http.StatusRequestEntityTooLarge, "request_too_large"},
	RequestErrorFilesNotAllowed:        {http.StatusRequestEntityTooLarge, "request_too_large"},
	RequestErrorTooManyFiles:           {http.StatusRequestEntityTooLarge, "request_too_large"},
	RequestErrorFileTooLarge:           {http.StatusRequestEntityTooLarge, "request_too_large"},
	RequestErrorMissingUpload:          {http.StatusUnprocessableEntity, "validation_error"},
	RequestErrorMultipleUpload:         {http.StatusUnprocessableEntity, "validation_error"},
	RequestErrorMissingPolicy:          {http.StatusInternalServerError, "internal_error"},
	RequestErrorTempCreate:             {http.StatusInternalServerError, "internal_error"},
	RequestErrorTempWrite:              {http.StatusInternalServerError, "internal_error"},
	RequestErrorTempClose:              {http.StatusInternalServerError, "internal_error"},
	RequestErrorTempOpen:               {http.StatusInternalServerError, "internal_error"},
	RequestErrorTempRemove:             {http.StatusInternalServerError, "internal_error"},
	RequestErrorUploadOpen:             {http.StatusInternalServerError, "internal_error"},
	RequestErrorHandler:                {http.StatusInternalServerError, "internal_error"},
	RequestErrorSerialization:          {http.StatusInternalServerError, "internal_error"},
	RequestErrorResultValuesOverBudget: {http.StatusInternalServerError, "internal_error"},
	RequestErrorUnknown:                {http.StatusInternalServerError, "internal_error"},
}

func requestError(kind RequestErrorKind, cause error) *RequestError {
	spec, ok := requestErrorSpecs[kind]
	if !ok {
		kind = RequestErrorUnknown
		spec = requestErrorSpecs[kind]
	}
	return &RequestError{kind: kind, statusCode: spec.status, managedCode: spec.code, cause: cause}
}

func writeManagedError(w http.ResponseWriter, err *RequestError) {
	if err == nil {
		err = requestError(RequestErrorUnknown, nil)
	}
	writeManagedOutcome(w, err.StatusCode(), ManagedOutcome{
		OK:      false,
		Code:    err.ManagedCode(),
		Message: managedClientMessage(err.StatusCode()),
	})
}

func writeManagedActionError(w http.ResponseWriter, err error, policy *resolvedPolicy) {
	var actionErr *ActionError
	if errors.As(err, &actionErr) {
		if actionErr == nil {
			writeManagedError(w, requestError(RequestErrorHandler, err))
			return
		}
		if actionErr.Kind != ActionErrorBadRequest && actionErr.Kind != ActionErrorValidation {
			writeManagedError(w, requestError(RequestErrorSerialization, err))
			return
		}
		if fieldErr := validateFieldErrors(actionErr.FieldErrors, policy.limits.MaxFieldNameBytes); fieldErr != nil {
			writeManagedError(w, fieldErr)
			return
		}
		status := http.StatusBadRequest
		code := "bad_request"
		if actionErr.Kind == ActionErrorValidation {
			status = http.StatusUnprocessableEntity
			code = "validation_error"
		}
		writeManagedOutcome(w, status, ManagedOutcome{OK: false, Code: code, Message: actionErr.Message, FieldErrors: cloneStrings(actionErr.FieldErrors)})
		return
	}
	missingUpload := errors.Is(err, ErrMissingUpload)
	multipleUploads := errors.Is(err, ErrMultipleUploads)
	if missingUpload && multipleUploads {
		writeManagedError(w, requestError(RequestErrorSerialization, err))
		return
	}
	if missingUpload || multipleUploads {
		kind := RequestErrorMissingUpload
		if multipleUploads {
			kind = RequestErrorMultipleUpload
		}
		writeManagedError(w, requestError(kind, err))
		return
	}
	var requestErr *RequestError
	if errors.As(err, &requestErr) {
		writeManagedError(w, requestErr)
		return
	}
	writeManagedError(w, requestError(RequestErrorHandler, err))
}

func validateFieldErrors(fieldErrors map[string]string, maxName int) *RequestError {
	for key, value := range fieldErrors {
		if key == "" || !utf8.ValidString(key) || len(key) > maxName || !utf8.ValidString(value) {
			return requestError(RequestErrorSerialization, nil)
		}
	}
	return nil
}

func validateManagedResult(result Result, policy *resolvedPolicy) *RequestError {
	if !result.OK {
		return requestError(RequestErrorSerialization, nil)
	}
	if result.Data != nil && !json.Valid(result.Data) {
		return requestError(RequestErrorSerialization, nil)
	}
	if err := validateFieldErrors(result.FieldErrors, policy.limits.MaxFieldNameBytes); err != nil {
		return err
	}
	if len(result.Values) > 0 {
		if len(policy.allowedValues) == 0 {
			return requestError(RequestErrorSerialization, nil)
		}
		for key, value := range result.Values {
			if _, ok := policy.allowedValues[key]; !ok || !utf8.ValidString(value) {
				return requestError(RequestErrorSerialization, nil)
			}
		}
		serialized, err := json.Marshal(result.Values)
		if err != nil {
			return requestError(RequestErrorSerialization, err)
		}
		if int64(len(serialized)) > policy.result.MaxSerializedBytes {
			return requestError(RequestErrorResultValuesOverBudget, nil)
		}
	}
	if result.Redirect == "" {
		if result.RedirectStatus != 0 {
			return requestError(RequestErrorSerialization, nil)
		}
	} else {
		if _, ok := safeManagedRedirect(result.Redirect); !ok {
			return requestError(RequestErrorSerialization, nil)
		}
		status := result.RedirectStatus
		if status == 0 {
			status = http.StatusSeeOther
		}
		switch status {
		case http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther, http.StatusTemporaryRedirect, http.StatusPermanentRedirect:
		default:
			return requestError(RequestErrorSerialization, nil)
		}
	}
	return nil
}

func outcomeFromResult(result Result) ManagedOutcome {
	status := result.RedirectStatus
	if result.Redirect != "" && status == 0 {
		status = http.StatusSeeOther
	}
	return ManagedOutcome{
		OK:             true,
		Code:           "ok",
		Message:        result.Message,
		Data:           append(json.RawMessage(nil), result.Data...),
		FieldErrors:    cloneStrings(result.FieldErrors),
		Values:         cloneStrings(result.Values),
		Redirect:       result.Redirect,
		RedirectStatus: status,
	}
}

func managedClientMessage(status int) string {
	switch status {
	case http.StatusBadRequest:
		return "request could not be processed"
	case http.StatusForbidden:
		return "request was not authorized"
	case http.StatusMethodNotAllowed:
		return "method is not allowed"
	case http.StatusUnsupportedMediaType:
		return "unsupported request format"
	case http.StatusRequestEntityTooLarge:
		return "request is too large"
	case http.StatusUnprocessableEntity:
		return "request could not be processed"
	default:
		return "internal server error"
	}
}

func writeManagedResult(w http.ResponseWriter, req *http.Request, media string, policy *resolvedPolicy, result Result, outcome ManagedOutcome) {
	if managedWantsJSON(req) || result.Redirect == "" {
		writeManagedOutcome(w, http.StatusOK, outcome)
		return
	}
	target, ok := safeManagedRedirect(result.Redirect)
	if !ok {
		writeManagedError(w, requestError(RequestErrorSerialization, nil))
		return
	}
	status := result.RedirectStatus
	if status == 0 {
		status = http.StatusSeeOther
	}
	http.Redirect(w, req, target, status)
}

func managedWantsJSON(req *http.Request) bool {
	if req == nil {
		return true
	}
	accept := req.Header.Values("Accept")
	if len(accept) == 0 {
		return true
	}
	for _, value := range accept {
		for _, item := range strings.Split(value, ",") {
			media, _, err := mime.ParseMediaType(strings.TrimSpace(item))
			if err == nil && strings.EqualFold(media, "application/json") {
				return true
			}
		}
	}
	return false
}

func safeManagedRedirect(raw string) (string, bool) {
	if raw == "" || len(raw) > 1024 || !strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, "//") || strings.ContainsAny(raw, "\r\n#") {
		return "", false
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.IsAbs() || parsed.Host != "" || parsed.Path == "" || !strings.HasPrefix(parsed.Path, "/") {
		return "", false
	}
	return parsed.RequestURI(), true
}

func writeManagedOutcome(w http.ResponseWriter, status int, outcome ManagedOutcome) {
	if status == 0 {
		status = http.StatusOK
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(outcome)
}

func cloneStrings(src map[string]string) map[string]string {
	if len(src) == 0 {
		return nil
	}
	dst := make(map[string]string, len(src))
	for key, value := range src {
		dst[key] = value
	}
	return dst
}
