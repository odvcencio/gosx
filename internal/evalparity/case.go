package evalparity

// Case is one row of the differential table: a component body (an
// expression inside `{}`, plus an optional typed Props struct) rendered
// through every backend that can express it.
//
// The source GoSX text is generated (source.go) from ID, Expr, and
// PropsFields, so every backend compiles from byte-identical markup — the
// only per-backend difference is how each one evaluates that markup.
type Case struct {
	// ID is a unique, valid-Go-identifier-safe snake_case name, e.g.
	// "numeric_int_div". It becomes the component and Props type names
	// (see source.go's identifier()), so it must be unique across the
	// whole table.
	ID string

	// Category groups related cases for the support matrix (e.g.
	// "numeric", "string", "comparison", "ternary", "nil", "html-escape").
	Category string

	// Note is a one-line, human-readable description of what the case
	// pins down, e.g. "int / int truncates like Go, not float division".
	Note string

	// Expr is the Go/GSX expression placed inside `{}` as the sole child
	// of a <div>, e.g. "props.A / props.B" or "7 / 2".
	Expr string

	// PropsFields, when non-empty, is the Go struct-field block (one field
	// per line, no braces) declared as this case's Props type, e.g.
	// "A int\nB int". Empty means the component takes no props.
	PropsFields string

	// ExtraTypes, when non-empty, is one or more complete Go type
	// declarations (e.g. "type Nested struct {\n\tX int\n}") emitted
	// before the Props type, for a case whose Props references a named
	// nested type. Ignored when PropsFields is empty.
	ExtraTypes string

	// PropsLiteral is the field-value list (no braces, no type name) used
	// to construct this case's Props value in the generated transpile
	// program, e.g. `A: 3, B: 4`. Must be empty exactly when PropsFields
	// is empty.
	PropsLiteral string

	// PropsValue mirrors PropsLiteral as a Go value for the route backend
	// (bound as env.Values["props"]) and, JSON-encoded, as the VM's props
	// payload. nil exactly when PropsFields is empty.
	PropsValue map[string]any

	// Want is the expected rendered text inside "<div>...</div>" that
	// every backend NOT listed in Unsupported or Diverges must produce.
	Want string

	// Unsupported records, per backend, a one-line reason that backend
	// cannot run this case at all (a compile/lower error is expected,
	// not a bug). A case absent from this map must run cleanly on that
	// backend.
	Unsupported map[Backend]string

	// Diverges records a known, accepted mismatch: the backend runs
	// without error but its output differs from Want. The value is the
	// exact output the harness pins (so a silent behavior change is
	// still caught) plus, in DivergesReason, why it's accepted rather
	// than fixed.
	Diverges       map[Backend]string
	DivergesReason map[Backend]string
}
