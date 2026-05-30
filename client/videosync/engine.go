package videosync

// Engine is the top-level drift-correction engine.
// Sub-machines (SyncEngine, DriftCorrector, PreloadManager, codec) are added
// in later tasks and attach here as fields.
type Engine struct {
	cfg Config
	// Sub-machines are added in later tasks.
}

// New constructs an Engine with the given config.
// It records no wall-clock time; all timing arrives as caller-supplied args.
func New(cfg Config) *Engine {
	return &Engine{cfg: cfg}
}
