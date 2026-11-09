// Package stage lays out the stress scene: the arena, the stands and the
// seats of the crowd. The server uses it to declare the scene and the engine
// uses it to send the crowd's motion, so both agree on every seat. It links no
// server packages, so the js/wasm engine can import it.
package stage

import (
	"math"
	"strconv"

	"m31labs.dev/gosx/examples/game-stress/stress"
)

// MountID is the Scene3D mount the engine drives.
const MountID = "arena"

// Batch IDs of the scene's instanced figures.
const (
	PlayersBatch = "figures"
	CrowdPrefix  = "crowd-"
)

// Clip names in the generated figures (see package web).
const (
	ClipRun   = "Run"
	ClipIdle  = "Idle"
	ClipCheer = "Cheer"
	ClipSway  = "Sway"
)

// Palette tints crowd figures; TeamColors tints the two teams of agents.
var (
	Palette    = []string{"#d94f3d", "#f2c14e", "#3d7dd9", "#e8e8e8", "#5fb36b", "#9b59b6", "#2b2b2b", "#f08a4b"}
	TeamColors = [2]string{"#2f6fd6", "#e04848"}
)

// Seat is one crowd figure: a position, the direction it faces and its tint.
type Seat struct {
	X, Y, Z float64
	Yaw     float64
	Tint    int     // index into Palette
	Phase   float64 // clip start offset in seconds
	Cheer   bool    // plays Cheer instead of Sway
}

// Section is one block of the stands.
type Section struct {
	ID    string
	Seats []Seat
	// The stand under the seats, as a box: centre, size and the tilt about
	// its long axis.
	CX, CY, CZ, W, H, D, Tilt, Yaw float64
}

// SeatID names seat i of a section; frames address instances by ID.
func SeatID(i int) string { return "s" + strconv.Itoa(i) }

// Crowd places total figures in sections blocks of terraced stands around an
// arena of half extents halfX by halfZ. Long sides hold 60% of the crowd.
// The layout is deterministic.
func Crowd(total, sections int, halfX, halfZ float64) []Section {
	if total <= 0 || sections <= 0 {
		return nil
	}
	if sections%4 != 0 {
		sections = 4 * ((sections + 3) / 4)
	}
	perSide := sections / 4
	const spacing, rowDepth, rise, gap = 0.62, 0.85, 0.42, 4.0
	out := make([]Section, 0, sections)
	assigned := 0
	rng := uint64(0x9e3779b97f4a7c15)
	next := func() float64 {
		rng ^= rng << 13
		rng ^= rng >> 7
		rng ^= rng << 17
		return float64(rng>>11) / (1 << 53)
	}
	for side := 0; side < 4; side++ {
		long := side < 2
		length, inner := 2*halfX, halfZ+gap
		share := 0.3
		if !long {
			length, inner, share = 2*halfZ, halfX+gap, 0.2
		}
		for part := 0; part < perSide; part++ {
			idx := len(out)
			n := int(float64(total) * share / float64(perSide))
			if idx == sections-1 {
				n = total - assigned
			}
			n = max(0, min(n, total-assigned))
			assigned += n
			span := length / float64(perSide)
			cols := int(span / spacing)
			if cols < 1 {
				cols = 1
			}
			rows := (n + cols - 1) / cols
			start := -length/2 + span*float64(part)
			sec := Section{ID: CrowdPrefix + strconv.Itoa(idx), Seats: make([]Seat, n)}
			for i := 0; i < n; i++ {
				col, row := i%cols, i/cols
				along := start + (float64(col)+0.5)*spacing
				out := inner + float64(row)*rowDepth
				y := float64(row) * rise
				var x, z, yaw float64
				switch side {
				case 0: // south, facing north (-Z)
					x, z, yaw = along, out, math.Pi
				case 1: // north, facing +Z
					x, z, yaw = along, -out, 0
				case 2: // east, facing -X
					x, z, yaw = out, along, -math.Pi/2
				default: // west, facing +X
					x, z, yaw = -out, along, math.Pi/2
				}
				r := next()
				sec.Seats[i] = Seat{X: x, Y: y, Z: z, Yaw: yaw + (next()-0.5)*0.3,
					Tint: int(r * float64(len(Palette))), Phase: next() * 1.1, Cheer: next() < 0.55}
			}
			depth := float64(rows) * rowDepth
			height := float64(rows) * rise
			tilt := math.Atan2(rise, rowDepth)
			mid := inner + depth/2
			sec.W, sec.H, sec.D = span, 0.4, math.Hypot(depth, height)
			sec.CY, sec.Tilt = height/2-0.3, tilt
			// Scene3D rotates X first, and a positive X angle turns +Z down,
			// so a stand that rises toward +Z (south, and east after the yaw)
			// takes a negative tilt.
			switch side {
			case 0:
				sec.CX, sec.CZ, sec.Yaw, sec.Tilt = start+span/2, mid, 0, -tilt
			case 1:
				sec.CX, sec.CZ, sec.Yaw = start+span/2, -mid, 0
			case 2:
				sec.CX, sec.CZ, sec.Yaw = mid, start+span/2, math.Pi/2
				sec.Tilt = -tilt
			default:
				sec.CX, sec.CZ, sec.Yaw = -mid, start+span/2, math.Pi/2
			}
			out = append(out, sec)
		}
	}
	return out
}

// EngineProps is what the page passes to the Go engine.
type EngineProps struct {
	Target      string `json:"target"`
	Players     int    `json:"players"`
	Crowd       int    `json:"crowd"`
	Sections    int    `json:"sections"`
	ReplayEvery int    `json:"replayEvery"`
	ReplayTicks int    `json:"replayTicks"`
	Seed        uint64 `json:"seed"`
	FrameHz     int    `json:"workerFrameHz,omitempty"`
}

// SimConfig is the workload for a run; the engine builds the same one.
func (p EngineProps) SimConfig() stress.Config {
	cfg := stress.DefaultConfig()
	cfg.Agents = p.Players
	if p.Seed != 0 {
		cfg.Seed = p.Seed
	}
	return cfg
}

// GoRecordFloats is the length, in float64s, of one per-frame record the
// engine writes (see ../engine). The layout is shared with harnessJS.
const GoRecordFloats = 20

// Fields of a Go frame record.
const (
	RecFrame      = iota // frame index, from 1
	RecTimeMS            // performance.now() at the start of the frame, in the engine's realm
	RecHookMS            // whole engine frame: Step and pose output
	RecSimMS             // simulation without the AI: new ticks, replays, restores
	RecAIMS              // AI and rules in every tick, new and replayed
	RecReplayMS          // wall time of the replays (part of sim plus AI)
	RecAnimMS            // animation state, pose frame build, encode and dispatch
	RecTicks             // new ticks this frame
	RecReplayed          // ticks run again this frame
	RecGCCPUMS           // Go GC CPU time this frame (runtime/metrics estimate)
	RecGCPauseMS         // Go stop-the-world GC pause time this frame
	RecGCCycles          // GC cycles completed this frame
	RecAllocKB           // heap bytes allocated this frame, in KiB
	RecSimTick           // simulation tick after the frame
	RecMismatches        // replays whose state differed (must stay 0)
	RecHashHi            // state hash at a checkpoint tick, high 32 bits (else 0)
	RecHashLo            // low 32 bits
	RecHashTick          // the checkpoint tick of RecHashHi/Lo, else 0
	RecHeapMB            // live Go heap, MiB
	RecPostMS            // worker: time spent posting frames to the page
)
