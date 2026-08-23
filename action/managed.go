package action

// This file contains the framework-owned managed action substrate. It is
// deliberately separate from the redirect-backed Handler API in action.go:
// managed actions have one immutable registration policy, one bounded body
// reader, one parser cache, and one terminal cleanup path.

import (
	"bytes"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
	mu       sync.Mutex
	valid    bool
	openFn   func() (io.ReadCloser, error)
	tempPath string
	readers  []io.ReadCloser
}

func (s *uploadState) open() (io.ReadCloser, error) {
	if s == nil {
		return nil, errors.New("upload is not available")
	}
	s.mu.Lock()
	valid := s.valid
	openFn := s.openFn
	s.mu.Unlock()
	if !valid || openFn == nil {
		return nil, errors.New("upload is no longer valid")
	}
	r, err := openFn()
	if err != nil {
		kind := RequestErrorUploadOpen
		if s.tempPath != "" {
			kind = RequestErrorTempOpen
		}
		return nil, requestError(kind, err)
	}
	s.mu.Lock()
	if !s.valid {
		s.mu.Unlock()
		_ = r.Close()
		return nil, errors.New("upload is no longer valid")
	}
	s.readers = append(s.readers, r)
	s.mu.Unlock()
	return r, nil
}

func (s *uploadState) cleanup() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.valid = false
	readers := append([]io.ReadCloser(nil), s.readers...)
	s.readers = nil
	tempPath := s.tempPath
	s.mu.Unlock()
	for _, r := range readers {
		_ = r.Close()
	}
	if tempPath != "" {
		_ = os.Remove(tempPath)
	}
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
	if r.actions == nil {
		r.actions = make(map[string]managedRoute)
	}
	if _, exists := r.actions[name]; exists {
		return fmt.Errorf("managed action %q is already registered", name)
	}
	r.actions[name] = managedRoute{policy: policy, action: action}
	return nil
}

// IsManagedActionPath reports whether path names an action registered on this
// router. It is intentionally exact: a merely similar path is not allowed to
// bypass session.Protect's ordinary CSRF parser.
func (r *Router) IsManagedActionPath(path string) bool {
	if r == nil || !strings.HasPrefix(path, "/gosx/action/") {
		return false
	}
	name := strings.TrimPrefix(path, "/gosx/action/")
	if name == "" || strings.Contains(name, "/") || strings.ContainsAny(name, "?#") {
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
	if req == nil {
		return ""
	}
	if name := req.PathValue("name"); name != "" {
		return name
	}
	path := req.URL.Path
	const prefix = "/gosx/action/"
	if strings.HasPrefix(path, prefix) {
		name := strings.TrimPrefix(path, prefix)
		if name != "" && !strings.Contains(name, "/") {
			return name
		}
	}
	return ""
}

func resolvePolicy(name string, cfg Config, action ManagedAction) (*resolvedPolicy, error) {
	if err := validateActionName(name); err != nil {
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

func validateActionName(name string) error {
	if name == "" || !utf8.ValidString(name) || strings.ContainsAny(name, "/?#") {
		return fmt.Errorf("invalid managed action name")
	}
	return nil
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
	b.count += int64(n)
	if b.count > b.limit {
		b.fail = &bodyLimitError{limit: b.limit}
		return n, b.fail
	}
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
	defer body.Close()

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

	parsed, parseErr := parseManagedBody(req, media, boundary, policy)
	if parseErr != nil {
		writeManagedError(w, parseErr)
		return
	}
	if !headerPresent {
		if !bodyCSRFMatches(parsed, policy.csrf.BodyFieldName, expected) {
			writeManagedError(w, requestError(RequestErrorCSRF, nil))
			cleanupManagedUploads(parsed)
			return
		}
	}

	form := &managedForm{values: parsed.values, files: parsed.files}
	ctx := &Context{Request: req, Form: form, Payload: parsed.payload}
	setManagedRequestForms(req, parsed.values)

	result, actionErr := runManagedAction(action, ctx, parsed)
	if actionErr != nil {
		writeManagedActionError(w, actionErr, policy)
		return
	}
	if err := validateManagedResult(result, policy); err != nil {
		writeManagedError(w, err)
		return
	}
	outcome := outcomeFromResult(result)
	writeManagedOutcome(w, http.StatusOK, outcome)
}

func runManagedAction(action ManagedAction, ctx *Context, parsed *parsedManagedBody) (result Result, err error) {
	defer cleanupManagedUploads(parsed)
	defer func() {
		if recover() != nil {
			result = Result{}
			err = requestError(RequestErrorHandler, nil)
		}
	}()
	return action(ctx)
}

func requestMedia(req *http.Request) (string, string, *RequestError) {
	encoding := strings.TrimSpace(req.Header.Get("Content-Encoding"))
	if encoding != "" && !strings.EqualFold(encoding, "identity") {
		return "", "", requestError(RequestErrorUnsupportedMedia, nil)
	}
	contentTypes := req.Header.Values("Content-Type")
	if len(contentTypes) > 1 {
		return "", "", requestError(RequestErrorMalformed, nil)
	}
	contentType := req.Header.Get("Content-Type")
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

func parseManagedBody(req *http.Request, media, boundary string, policy *resolvedPolicy) (*parsedManagedBody, *RequestError) {
	switch media {
	case "application/json":
		return parseJSONBody(req, policy)
	case "application/x-www-form-urlencoded":
		return parseURLBody(req, policy)
	case "multipart/form-data":
		return parseMultipartBody(req, boundary, policy)
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
		fieldBytes += int64(len(name) + len(value))
		if fieldBytes > limits.MaxFieldBytes {
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

func parseMultipartBody(req *http.Request, boundary string, policy *resolvedPolicy) (*parsedManagedBody, *RequestError) {
	reader := multipart.NewReader(req.Body, boundary)
	parsed := &parsedManagedBody{
		values:      make(map[string][]string),
		files:       make(map[string][]Upload),
		isMultipart: true,
	}
	var metadataTotal int64
	var fieldTotal int64
	var fields int
	var files int
	var memoryUsed int64

	for {
		part, err := reader.NextRawPart()
		if err == io.EOF {
			if drainErr := drainBody(req.Body); drainErr != nil {
				cleanupManagedUploads(parsed)
				return nil, drainErr
			}
			return parsed, nil
		}
		if err != nil {
			cleanupManagedUploads(parsed)
			if isBodyError(err) {
				return nil, classifyBodyError(err)
			}
			return nil, requestError(RequestErrorMalformed, err)
		}

		metadataTotal += multipartMetadataBytes(part)
		if metadataTotal > policy.limits.MaxMultipartMetadataBytes {
			cleanupManagedUploads(parsed)
			return nil, requestError(RequestErrorMetadataTooLarge, nil)
		}
		if hasNonEmptyHeader(part.Header, "Content-Transfer-Encoding") {
			cleanupManagedUploads(parsed)
			return nil, requestError(RequestErrorMalformed, nil)
		}
		name, filename, isFile, err := multipartDisposition(part.Header)
		if err != nil {
			cleanupManagedUploads(parsed)
			return nil, requestError(RequestErrorMalformed, err)
		}
		if !utf8.ValidString(name) || name == "" {
			cleanupManagedUploads(parsed)
			return nil, requestError(RequestErrorMalformed, nil)
		}
		if len(name) > policy.limits.MaxFieldNameBytes {
			cleanupManagedUploads(parsed)
			return nil, requestError(RequestErrorFieldNameTooLarge, nil)
		}
		declaredMediaType, mediaErr := multipartDeclaredMediaType(part.Header)
		if mediaErr != nil {
			cleanupManagedUploads(parsed)
			return nil, requestError(RequestErrorMalformed, mediaErr)
		}

		if isFile {
			files++
			if policy.limits.MaxFiles == 0 {
				cleanupManagedUploads(parsed)
				return nil, requestError(RequestErrorFilesNotAllowed, nil)
			}
			if files > policy.limits.MaxFiles {
				cleanupManagedUploads(parsed)
				return nil, requestError(RequestErrorTooManyFiles, nil)
			}
			upload, readErr := readMultipartUpload(part, name, sanitizeFilename(filename, policy.limits.MaxClientFilenameBytes), declaredMediaType, policy.limits, &memoryUsed)
			if readErr != nil {
				cleanupManagedUploads(parsed)
				return nil, readErr
			}
			parsed.files[name] = append(parsed.files[name], upload)
			continue
		}

		fields++
		if fields > policy.limits.MaxFields {
			cleanupManagedUploads(parsed)
			return nil, requestError(RequestErrorTooManyFields, nil)
		}
		value, readErr := readMultipartField(part, policy.limits.MaxFieldBytes-fieldTotal)
		if readErr != nil {
			cleanupManagedUploads(parsed)
			return nil, readErr
		}
		fieldTotal += int64(len(value))
		if fieldTotal > policy.limits.MaxFieldBytes {
			cleanupManagedUploads(parsed)
			return nil, requestError(RequestErrorFieldsTooLarge, nil)
		}
		parsed.values[name] = append(parsed.values[name], string(value))
	}
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
			total += int64(len(key) + 2 + len(value) + 2)
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
	return strings.ToLower(mediaType), nil
}

func readMultipartField(part io.Reader, remaining int64) ([]byte, *RequestError) {
	if remaining < 0 {
		remaining = 0
	}
	data, err := io.ReadAll(io.LimitReader(part, remaining+1))
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

func readMultipartUpload(part io.Reader, fieldName, filename, mediaType string, limits Limits, memoryUsed *int64) (Upload, *RequestError) {
	state := &uploadState{valid: true}
	var memory bytes.Buffer
	var temp *os.File
	var size int64
	cleanupState := func() {
		if temp != nil {
			_ = temp.Close()
		}
		state.cleanup()
	}
	defer func() {
		if temp != nil {
			_ = temp.Close()
		}
	}()

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
						_, _ = memory.Write(chunk[:keep])
						*memoryUsed += keep
					}
					if keep < int64(len(chunk)) {
						var createErr error
						temp, createErr = os.CreateTemp("", "gosx-upload-")
						if createErr != nil {
							cleanupState()
							return Upload{}, requestError(RequestErrorTempCreate, createErr)
						}
						state.tempPath = temp.Name()
						if _, createErr = temp.Write(memory.Bytes()); createErr == nil {
							_, createErr = temp.Write(chunk[keep:])
						}
						if createErr != nil {
							cleanupState()
							return Upload{}, requestError(RequestErrorTempWrite, createErr)
						}
					}
				} else if _, writeErr := temp.Write(chunk); writeErr != nil {
					cleanupState()
					return Upload{}, requestError(RequestErrorTempWrite, writeErr)
				}
			}
			size += int64(n)
			if int64(n) > allowed {
				cleanupState()
				return Upload{}, requestError(RequestErrorFileTooLarge, nil)
			}
		}
		if err != nil {
			if !errors.Is(err, io.EOF) {
				cleanupState()
				if isBodyError(err) {
					return Upload{}, classifyBodyError(err)
				}
				return Upload{}, requestError(RequestErrorBodyRead, err)
			}
			break
		}
	}
	if temp != nil {
		if err := temp.Close(); err != nil {
			cleanupState()
			return Upload{}, requestError(RequestErrorTempWrite, err)
		}
		temp = nil
		state.openFn = func() (io.ReadCloser, error) {
			file, err := os.Open(state.tempPath)
			if err != nil {
				return nil, err
			}
			return file, nil
		}
	} else {
		data := append([]byte(nil), memory.Bytes()...)
		state.openFn = func() (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(data)), nil
		}
	}
	return Upload{FieldName: fieldName, ClientFilename: filename, DeclaredMediaType: mediaType, Size: size, state: state}, nil
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

func cleanupManagedUploads(parsed *parsedManagedBody) {
	if parsed == nil {
		return
	}
	seen := make(map[*uploadState]struct{})
	for _, uploads := range parsed.files {
		for _, upload := range uploads {
			if upload.state == nil {
				continue
			}
			if _, ok := seen[upload.state]; ok {
				continue
			}
			seen[upload.state] = struct{}{}
			upload.state.cleanup()
		}
	}
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
		writeManagedOutcome(w, http.StatusUnprocessableEntity, ManagedOutcome{OK: false, Code: "validation_error", Message: "upload validation failed"})
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
	default:
		return "internal server error"
	}
}

func writeManagedOutcome(w http.ResponseWriter, status int, outcome ManagedOutcome) {
	if status == 0 {
		status = http.StatusOK
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(outcome)
}
