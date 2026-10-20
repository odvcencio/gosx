package telemetry

// Miscellaneous metadata has one shared reservation. Independent observer,
// adapter and codec admissions must not each consume the same 2 MiB arena.
func (t *Telemetry) reserveMisc(bytes int64) bool {
	if bytes < 0 {
		return false
	}
	for {
		used := t.miscBytes.Load()
		if bytes > hubMiscBytes-used {
			return false
		}
		if t.miscBytes.CompareAndSwap(used, used+bytes) {
			return true
		}
	}
}

func (t *Telemetry) releaseMisc(bytes int64) { t.miscBytes.Add(-bytes) }
