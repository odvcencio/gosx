package motion

import (
	"fmt"
	"math"
	"testing"
)

func keyframeLookupTrack(count int) Track {
	track := Track{TargetID: 0, PropID: 0, Keys: make([]Key, count)}
	for i := range track.Keys {
		track.Keys[i] = Key{
			T:     float64(i) + float64(i%3)/8,
			Value: ScalarV(float64(i%11)*13 - 7),
		}
	}
	return track
}

var keyframeLookupBenchmarkSink float64

// Include ordinary two-key UI motion as well as long imported clips. Both
// ordered playback and arbitrary seeks use the public, stateless evaluator.
func BenchmarkEvalKeyframeSeeks(b *testing.B) {
	for _, count := range []int{2, 8, 64, 1024, 8192} {
		for _, shuffled := range []bool{false, true} {
			order := "playback"
			if shuffled {
				order = "seeks"
			}
			b.Run(fmt.Sprintf("keys=%d/%s", count, order), func(b *testing.B) {
				track := keyframeLookupTrack(count)
				timeline := &Timeline{Children: []Positioned{{Track: &track}}}
				var times [1024]float64
				for i := range times {
					index := i
					if shuffled {
						index = (i * 149) % len(times)
					}
					times[i] = (float64(index) + .5) / float64(len(times)) * track.Keys[count-1].T
				}
				out := NewWriteBuf(16)
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					out.Reset()
					Eval(timeline, times[i%len(times)], Policy{}, out)
				}
				keyframeLookupBenchmarkSink = out.Writes()[3]
			})
		}
	}
}

// Every interval is compared with its immediate neighbors in a short track.
// This isolates segment selection without copying the search implementation or
// turning an interior knot into an endpoint (which changes quaternion rounding).
func TestEvalLongTrackMatchesIndividualSegments(t *testing.T) {
	for _, count := range []int{8, 9, 64, 257} {
		for _, template := range append(valueTracks(), Track{Keys: []Key{{Value: ScalarV(-7)}, {Value: ScalarV(19)}}}) {
			track := template
			track.Keys = make([]Key, count)
			for i := range track.Keys {
				track.Keys[i] = template.Keys[i%len(template.Keys)]
				track.Keys[i].T = float64(i) + float64(i%3)/8
			}
			got, want := NewWriteBuf(16), NewWriteBuf(16)
			for i := len(track.Keys) - 2; i >= 0; i-- {
				pair := track
				pair.Keys = track.Keys[max(0, i-1):min(len(track.Keys), i+3)]
				for _, at := range []float64{
					track.Keys[i].T,
					math.Nextafter(track.Keys[i].T, math.Inf(1)),
					(track.Keys[i].T + track.Keys[i+1].T) / 2,
					math.Nextafter(track.Keys[i+1].T, math.Inf(-1)),
				} {
					got.Reset()
					want.Reset()
					evalTrack(&track, at+5.125, 5.125, Policy{}, got)
					evalTrack(&pair, at+5.125, 5.125, Policy{}, want)
					for j, value := range want.Writes() {
						if math.Float64bits(got.Writes()[j]) != math.Float64bits(value) {
							t.Fatalf("%d keys, arity %d, interpolation %d, segment %d, t=%g: %v, want %v", count, track.Keys[i].Value.Arity, track.Interp, i, at, got.Writes(), want.Writes())
						}
					}
				}
			}
		}
	}
}
