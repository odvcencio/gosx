//go:build !race

package main

// raceDetectorEnabled is false in non-race builds; the slow
// subprocess-build tests run normally (and do in the `make test` gate).
const raceDetectorEnabled = false
