package wire

import (
	"errors"

	"github.com/evanw/esbuild/pkg/api"
	ts "github.com/odvcencio/gotreesitter"

	"m31labs.dev/gosx/internal/pagecaps"
)

func validateFormattedReferences(result api.TransformResult) error {
	if len(result.Errors) != 0 {
		return referenceFailure()
	}
	if len(result.Code) > maxFormattedReferenceBytes {
		return referenceLimit("formatted-bytes", maxFormattedReferenceBytes)
	}
	return nil
}

const (
	maxReferenceASTNodes       = 250000
	maxReferenceDepth          = 256
	maxFormattedReferenceBytes = 32 << 20
)

// ReferenceDrop explains incomplete private analysis. File is a caller-supplied
// logical asset label, never an inferred native path; Offset is a body byte index.
// None of these diagnostics are copied into public performance reports.
type ReferenceDrop struct {
	Reason string
	Bound  string
	Limit  int64
	File   string
	Offset int64
}

func referenceLimit(bound string, limit int) error {
	return &pagecaps.AnalysisLimit{Bound: bound, Limit: int64(limit)}
}

// Internal walks use the typed limit to stop immediately. Public entry points
// retain partial references and turn it into an incomplete result, not an error.
func (out *referenceScanner) acceptLimit(err error) bool {
	var limit *pagecaps.AnalysisLimit
	if !errors.As(err, &limit) {
		return false
	}
	out.drop(dropAnalysisLimit)
	for _, drop := range out.Drops {
		if drop.Bound == limit.Bound && drop.Limit == limit.Limit {
			return true
		}
	}
	out.Drops = append(out.Drops, ReferenceDrop{Reason: "analysis-limit", Bound: limit.Bound, Limit: limit.Limit})
	return true
}

func referenceResult(out *referenceScanner, err error) (ReferenceSet, error) {
	if out.acceptLimit(err) {
		err = nil
	}
	if err != nil {
		out.drop(dropUnresolved)
	}
	return finishReferences(out), err
}

func (out *referenceScanner) documentLimits(tree *pagecaps.DocumentTree) {
	for _, limit := range tree.Limits {
		out.acceptLimit(&limit)
	}
}

// Parser limits can return a partial tree with a nil error. Inspect the stop
// receipt before interpreting missing nodes as malformed input or retrying it.
func referenceParseLimit(reason ts.ParseStopReason, runtime ts.ParseRuntime, timeout uint64) error {
	var bound string
	var limit int64
	switch reason {
	case ts.ParseStopIterationLimit:
		bound, limit = "parser-iterations", int64(runtime.IterationLimit)
	case ts.ParseStopStackDepthLimit:
		bound, limit = "parser-stack-depth", int64(runtime.StackDepthLimit)
	case ts.ParseStopNodeLimit:
		bound, limit = "parser-nodes", int64(runtime.NodeLimit)
	case ts.ParseStopMemoryBudget:
		bound, limit = "parser-memory-bytes", runtime.MemoryBudgetBytes
	case ts.ParseStopTimeout:
		bound, limit = "parser-time-micros", int64(timeout)
	default:
		return nil
	}
	return &pagecaps.AnalysisLimit{Bound: bound, Limit: limit}
}

func parseReferenceSyntax(parser *ts.Parser, body []byte) (*ts.Tree, error) {
	tree, err := parser.Parse(body)
	if tree != nil {
		if limit := referenceParseLimit(tree.ParseStopReason(), tree.ParseRuntime(), parser.TimeoutMicros()); limit != nil {
			tree.Release()
			return nil, limit
		}
	}
	if err != nil || tree == nil {
		if tree != nil {
			tree.Release()
		}
		return nil, referenceFailure()
	}
	return tree, nil
}
