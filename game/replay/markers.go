package replay

// Marker labels a tick of interest. Game code adds them with Recorder.Mark.
type Marker struct {
	Tick  uint64
	Label string
}
