//go:build race

package quant

func init() {
	raceEnabled = true
}
