package playground

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/odvcencio/gosx"
	"github.com/odvcencio/gosx/action"
	"github.com/odvcencio/gosx/examples/gosx-docs/app/demos/democtl"
	"github.com/odvcencio/gosx/ir"
	"github.com/odvcencio/gosx/island/program"
)

// ---------------------------------------------------------------------------
// Configuration defaults
// ---------------------------------------------------------------------------

const (
	defaultMaxSourceBytes = 16 << 10 // 16 KB
	defaultMaxNodes       = 4096
	defaultMaxExprs       = 4096
	defaultParseTimeout   = 250 * time.Millisecond
	defaultCacheCapacity  = 256
	defaultCacheTTL       = 5 * time.Minute
	defaultRateLimitRate  = 5
	defaultRateLimitBurst = 20
)

// ---------------------------------------------------------------------------
// Result types and sentinel errors
// ---------------------------------------------------------------------------

// Diagnostic is a user-facing parse/validate error for the playground editor.
type Diagnostic struct {
	Line    int    `json:"line"`
	Column  int    `json:"column"`
	Message string `json:"message"`
}

// CompileResult is the pure output of the playground pipeline.
type CompileResult struct {
	// HTML is the SSR placeholder the client runtime hydrates. In this task
	// it is a static hydration target; future tasks may enrich it.
	HTML string `json:"html"`

	// Program is the binary-encoded island VM program. Callers are expected
	// to base64 the bytes when they travel over JSON.
	Program []byte `json:"-"`

	// Diagnostics are non-fatal user-facing errors (parse or validation
	// failures). A non-empty Diagnostics slice with a zero Program means the
	// input had a problem the user needs to see.
	Diagnostics []Diagnostic `json:"diagnostics"`
}

// ErrEmptySource is returned when CompileSource receives empty input.
var ErrEmptySource = errors.New("playground: source is empty")

// ErrNoComponents is returned when the source compiles but yields no components.
var ErrNoComponents = errors.New("playground: no components in source")

// ErrFirstComponentNotIsland is returned when Components[0] is not an island.
// The playground expects the first component to be the island under test
// (matching the preset shape).
var ErrFirstComponentNotIsland = errors.New("playground: first component is not an island (missing //gosx:island directive?)")

// ErrSourceTooLarge is returned when the input exceeds the configured byte cap.
var ErrSourceTooLarge = errors.New("playground: source exceeds max bytes")

// ErrRateLimited is returned when the caller's token bucket is exhausted.
var ErrRateLimited = errors.New("playground: rate limited")

// ErrParseTimeout is returned when the compile pipeline does not finish within
// the configured wall-clock budget.
var ErrParseTimeout = errors.New("playground: parse timeout")

// ErrTooManyNodes is returned when the lowered island program exceeds the node cap.
var ErrTooManyNodes = errors.New("playground: island program exceeds node cap")

// ErrTooManyExprs is returned when the lowered island program exceeds the
// expression cap.
var ErrTooManyExprs = errors.New("playground: island program exceeds expression cap")

// ---------------------------------------------------------------------------
// Core pipeline
// ---------------------------------------------------------------------------

// compileSourceWithCountsFn is the package-level function variable for the
// core pipeline. Tests can swap it to inject a fake (e.g., to simulate
// timeouts). Always call via this variable, never directly.
var compileSourceWithCountsFn = compileSourceWithCountsImpl

// compileSourceWithCountsImpl is the real implementation of the compile
// pipeline. It returns the CompileResult alongside the node and expression
// counts from the lowered island program, which the Compiler uses to enforce
// caps without re-parsing.
func compileSourceWithCountsImpl(source []byte) (CompileResult, int, int, error) {
	if len(source) == 0 {
		return CompileResult{}, 0, 0, ErrEmptySource
	}

	prog, err := gosx.Compile(source)
	if err != nil {
		// Parse or validation error — return as a diagnostic, not a fatal err.
		return CompileResult{
			Diagnostics: []Diagnostic{{Message: err.Error()}},
		}, 0, 0, nil
	}
	if len(prog.Components) == 0 {
		return CompileResult{}, 0, 0, ErrNoComponents
	}
	if !prog.Components[0].IsIsland {
		return CompileResult{}, 0, 0, ErrFirstComponentNotIsland
	}

	island, err := ir.LowerIsland(prog, 0)
	if err != nil {
		return CompileResult{
			Diagnostics: []Diagnostic{{Message: err.Error()}},
		}, 0, 0, nil
	}

	nNodes := len(island.Nodes)
	nExprs := len(island.Exprs)

	bin, err := program.EncodeBinary(island)
	if err != nil {
		return CompileResult{}, nNodes, nExprs, fmt.Errorf("encode island program: %w", err)
	}

	return CompileResult{
		HTML:    renderPlaygroundSSR(prog.Components[0].Name),
		Program: bin,
	}, nNodes, nExprs, nil
}

// CompileSource parses gsx source, lowers the first component to an island
// program, and encodes it. Fatal pipeline errors are returned as err. User-
// facing problems (parse/validation failures) are returned in the
// CompileResult.Diagnostics slice with a nil err.
func CompileSource(source []byte) (CompileResult, error) {
	result, _, _, err := compileSourceWithCountsFn(source)
	return result, err
}

// renderPlaygroundSSR emits the minimal hydration target element. The client
// replaces its children when the new program is hydrated. We keep the element
// slot stable across recompiles so the hydrator can find it.
func renderPlaygroundSSR(componentName string) string {
	return `<div data-gosx-island="playground-preview" data-component="` + componentName + `"></div>`
}

// ---------------------------------------------------------------------------
// CompileConfig and Compiler
// ---------------------------------------------------------------------------

// CompileConfig configures a Compiler with safety and throughput knobs.
type CompileConfig struct {
	MaxSourceBytes int           // hard cap on input length; 0 = default 16 KB
	MaxNodes       int           // cap on lowered island nodes; 0 = default 4096
	MaxExprs       int           // cap on lowered island exprs; 0 = default 4096
	ParseTimeout   time.Duration // 0 = default 250 ms
	RateLimit      *democtl.Limiter // required; nil = construction error
	CacheCapacity  int           // 0 = default 256
	CacheTTL       time.Duration // 0 = default 5 min
	Clock          democtl.Clock // nil = real clock
}

// DefaultCompileConfig returns a CompileConfig with production-sane defaults
// and a freshly-constructed 5/20 democtl.Limiter. Callers that need a shared
// limiter across demos should build their own CompileConfig.
func DefaultCompileConfig() CompileConfig {
	return CompileConfig{
		RateLimit: democtl.NewLimiter(defaultRateLimitRate, defaultRateLimitBurst),
	}
}

// Compiler owns the per-process mitigation state (rate limiter, cache).
// It is concurrency-safe.
type Compiler struct {
	cfg   CompileConfig
	cache *compileCache
	clock democtl.Clock
}

// NewCompiler returns a Compiler validated against cfg. Returns an error if
// cfg.RateLimit is nil.
func NewCompiler(cfg CompileConfig) (*Compiler, error) {
	if cfg.RateLimit == nil {
		return nil, errors.New("playground: CompileConfig.RateLimit must not be nil")
	}
	if cfg.MaxSourceBytes <= 0 {
		cfg.MaxSourceBytes = defaultMaxSourceBytes
	}
	if cfg.MaxNodes <= 0 {
		cfg.MaxNodes = defaultMaxNodes
	}
	if cfg.MaxExprs <= 0 {
		cfg.MaxExprs = defaultMaxExprs
	}
	if cfg.ParseTimeout <= 0 {
		cfg.ParseTimeout = defaultParseTimeout
	}
	clk := cfg.Clock
	if clk == nil {
		clk = realClock{}
	}
	return &Compiler{
		cfg:   cfg,
		cache: newCompileCache(cfg.CacheCapacity, cfg.CacheTTL, clk),
		clock: clk,
	}, nil
}

// Compile runs the safety-gated pipeline. rateKey identifies the caller for
// rate-limiting (typically a client IP). An empty rateKey disables rate
// limiting for this call — test-only convenience.
//
// Errors returned here are operational. User-facing parse/validate/size
// failures come back in result.Diagnostics with a nil err.
func (c *Compiler) Compile(rateKey string, source []byte) (CompileResult, error) {
	// 1. Size cap.
	if len(source) > c.cfg.MaxSourceBytes {
		return CompileResult{}, ErrSourceTooLarge
	}
	// 2. Rate limit (unless rateKey is empty = test/bypass mode).
	if rateKey != "" {
		if !c.cfg.RateLimit.Allow(rateKey) {
			return CompileResult{}, ErrRateLimited
		}
	}
	// 3. Cache lookup.
	key := cacheKeyFor(source)
	if cached, ok := c.cache.Get(key); ok {
		return cached, nil
	}

	// 4. Run the core pipeline under a timeout.
	// Capture the function value before spawning the goroutine so that test
	// swaps of compileSourceWithCountsFn (restored via defer) don't race with
	// the orphan goroutine that may outlive the select.
	compileFn := compileSourceWithCountsFn
	type compileOutcome struct {
		result CompileResult
		nNodes int
		nExprs int
		err    error
	}
	done := make(chan compileOutcome, 1)
	go func() {
		r, n, e, err := compileFn(source)
		done <- compileOutcome{result: r, nNodes: n, nExprs: e, err: err}
	}()

	select {
	case outcome := <-done:
		if outcome.err != nil {
			return outcome.result, outcome.err
		}
		// 5. Node/expr caps — only enforce on successful compiles (non-nil
		// Program). Diagnostics results (parse errors) have zero counts; we
		// cache them as deterministic failures so the user gets instant
		// feedback on retry without re-parsing. See design note in task spec.
		if len(outcome.result.Program) > 0 {
			if outcome.nNodes > c.cfg.MaxNodes {
				return CompileResult{}, ErrTooManyNodes
			}
			if outcome.nExprs > c.cfg.MaxExprs {
				return CompileResult{}, ErrTooManyExprs
			}
		}
		// 6. Cache the result (including diagnostic-only results — they are
		// deterministic for a given source).
		c.cache.Put(key, outcome.result)
		return outcome.result, nil

	case <-time.After(c.cfg.ParseTimeout):
		// The orphan goroutine will complete eventually. Because rate-limiting
		// bounds the worst-case orphan accumulation rate, this is acceptable.
		return CompileResult{}, ErrParseTimeout
	}
}

// ---------------------------------------------------------------------------
// Action adapters
// ---------------------------------------------------------------------------

// NewCompileAction returns an action.Context handler that uses the given
// Compiler for all safety gating. The returned closure is the preferred way
// to wire the playground into a page's Actions map.
func NewCompileAction(compiler *Compiler) func(*action.Context) error {
	return func(ctx *action.Context) error {
		var req struct {
			Source string `json:"source"`
		}
		rateKey := clientIPFromRequest(ctx.Request)
		if err := json.Unmarshal(ctx.Payload, &req); err != nil {
			return ctx.Success("", map[string]any{
				"html":        "",
				"program":     "",
				"diagnostics": []Diagnostic{{Message: "invalid request body"}},
			})
		}
		result, err := compiler.Compile(rateKey, []byte(req.Source))
		// Convert sentinel errors to diagnostics so the client has one
		// uniform shape to render.
		if err != nil {
			return ctx.Success("", map[string]any{
				"html":        "",
				"program":     "",
				"diagnostics": []Diagnostic{{Message: sentinelMessage(err)}},
			})
		}
		return ctx.Success("", map[string]any{
			"html":        result.HTML,
			"program":     base64.StdEncoding.EncodeToString(result.Program),
			"diagnostics": result.Diagnostics,
		})
	}
}

// CompileAction is the legacy action.Context adapter kept for backwards
// compatibility. New callers should use NewCompileAction with a configured
// Compiler instead.
func CompileAction(ctx *action.Context) error {
	var req struct {
		Source string `json:"source"`
	}
	if err := json.Unmarshal(ctx.Payload, &req); err != nil {
		return ctx.Success("", map[string]any{
			"html":        "",
			"program":     "",
			"diagnostics": []Diagnostic{{Message: "invalid request body"}},
		})
	}
	result, err := CompileSource([]byte(req.Source))
	if err != nil {
		// Fatal — expose as a single diagnostic so the client renders something.
		return ctx.Success("", map[string]any{
			"html":        "",
			"program":     "",
			"diagnostics": []Diagnostic{{Message: err.Error()}},
		})
	}
	return ctx.Success("", map[string]any{
		"html":        result.HTML,
		"program":     base64.StdEncoding.EncodeToString(result.Program),
		"diagnostics": result.Diagnostics,
	})
}

// clientIPFromRequest extracts a stable rate-limiting key from the HTTP
// request. It prefers X-Forwarded-For (set by reverse proxies) and falls
// back to RemoteAddr.
func clientIPFromRequest(r *http.Request) string {
	if r == nil {
		return "playground"
	}
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		return fwd
	}
	return r.RemoteAddr
}

// sentinelMessage maps operational sentinel errors to user-readable strings
// suitable for display in the playground editor.
func sentinelMessage(err error) string {
	switch {
	case errors.Is(err, ErrSourceTooLarge):
		return "source too long — 16 KB max"
	case errors.Is(err, ErrRateLimited):
		return "too many requests — slow down"
	case errors.Is(err, ErrParseTimeout):
		return "parser timed out — source is too complex"
	case errors.Is(err, ErrTooManyNodes):
		return "island program is too large (node cap)"
	case errors.Is(err, ErrTooManyExprs):
		return "island program is too large (expression cap)"
	default:
		return err.Error()
	}
}
