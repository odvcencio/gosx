package pagecaps

const (
	MaxDocumentCount = 4096
	MaxDocumentBytes = 16 << 20
)

// AnalysisLimit identifies bounded document analysis, rather than malformed
// input. Strict capability callers can reject it; resource scanners retain
// incomplete coverage. Bound contains a fixed identifier, never source text.
type AnalysisLimit struct {
	Bound string
	Limit int64
}

func (e *AnalysisLimit) Error() string { return "analysis-limit: " + e.Bound }
