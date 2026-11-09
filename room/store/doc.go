// Package store is a small durable key-value and append-log store built on the
// Go standard library. It keeps all data in memory and makes every change
// durable through a write-ahead log (WAL) plus periodic snapshots.
//
// # Files
//
// A store directory holds one generation at a time:
//
//	LOCK                      advisory lock; one writer process per directory
//	snap-<gen>.snap           full state at the start of generation <gen>
//	wal-<gen>.log             batches committed since that snapshot
//
// Each commit is one frame: a 4-byte length, a 4-byte CRC-32C, and the
// payload. A batch is atomic: recovery applies a whole frame or none of it.
// Compaction writes the next snapshot to a temporary file, syncs it, renames it
// into place, syncs the directory, starts the next WAL, and then removes the
// old generation. A crash at any step leaves a directory that Open recovers.
//
// # Recovery
//
// Open loads the newest snapshot and replays its WAL. The WAL ends at the
// first frame that is short, fails its checksum, or does not parse (a torn
// tail from a crash mid-write); Open truncates the file there. A snapshot is
// written complete or not at all, so a snapshot that fails to load means disk
// corruption and Open returns ErrCorrupt instead of discarding data.
//
// # Bounds
//
// Values, keys, and per-log entry counts have limits, the WAL is compacted
// when it passes Options.CompactAt, and Options.MaxDataBytes caps live data.
// Disk use stays below about MaxDataBytes (snapshot) + CompactAt (WAL) + one
// more snapshot while a compaction runs.
//
// The store is single-writer by design: it takes an exclusive file lock on
// supported platforms and a second Open on the same directory fails with
// ErrLocked (or waits, see Options.LockTimeout).
package store
