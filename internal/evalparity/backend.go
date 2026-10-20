package evalparity

// Backend names one of the three expression evaluators under test.
type Backend int

const (
	// Transpile is the real-Go-compiler path.
	Transpile Backend = iota
	// Route is the file router's per-request reflect interpreter.
	Route
	// VM is the client/vm island bytecode walker.
	VM
)

// backendOrder fixes the column order the support matrix and test output
// use, independent of Go's unordered map iteration.
var backendOrder = []Backend{Transpile, Route, VM}

// String names a Backend for test failure messages and the generated
// support matrix.
func (b Backend) String() string {
	switch b {
	case Transpile:
		return "transpile"
	case Route:
		return "route"
	case VM:
		return "client-vm"
	default:
		return "unknown"
	}
}
