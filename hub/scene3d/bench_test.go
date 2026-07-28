package scene3d

import (
	"fmt"
	"testing"

	"m31labs.dev/gosx/crdt"
	"m31labs.dev/gosx/scene"
)

// benchScene builds a scene of count mesh objects plus one light. Every object
// carries a realistic set of fields, so the JSON per record is close to what a
// real editor writes.
func benchScene(count int) scene.SceneIR {
	objects := make([]scene.ObjectIR, count)
	for i := range objects {
		objects[i] = scene.ObjectIR{
			ID:           fmt.Sprintf("obj-%05d", i),
			Kind:         "box",
			Width:        1 + float64(i%4),
			Height:       1,
			Depth:        1,
			X:            float64(i % 64),
			Y:            float64((i / 64) % 64),
			Z:            float64(i / 4096),
			Color:        "#4488cc",
			MaterialKind: "standard",
			Roughness:    0.4,
			Metalness:    0.1,
			CastShadow:   true,
		}
	}
	return scene.SceneIR{
		Objects: objects,
		Lights:  []scene.LightIR{{ID: "sun", Kind: "directional", Intensity: 1}},
	}
}

// nudge moves every tenth object, which models a multi-selection drag.
func nudge(source scene.SceneIR, delta float64) scene.SceneIR {
	next := source
	next.Objects = append([]scene.ObjectIR(nil), source.Objects...)
	for i := range next.Objects {
		if i%10 == 0 {
			next.Objects[i].X += delta
		}
	}
	return next
}

// BenchmarkDiffAndApplyFullScene measures the cost of building a scene from
// nothing: the diff plus the document writes plus one commit.
func BenchmarkDiffAndApplyFullScene(b *testing.B) {
	for _, count := range []int{100, 1000, 5000} {
		target := benchScene(count)
		b.Run(fmt.Sprintf("objects=%d", count), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				bound, err := Bind(crdt.NewDoc(), "scene")
				if err != nil {
					b.Fatal(err)
				}
				if _, _, err := bound.DiffScene(scene.SceneIR{}, target, "full"); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkDiffAndApplyIncrementalEdit measures the steady-state editing cost:
// one drag that moves a tenth of the scene inside an established document.
func BenchmarkDiffAndApplyIncrementalEdit(b *testing.B) {
	for _, count := range []int{100, 1000, 5000} {
		base := benchScene(count)
		b.Run(fmt.Sprintf("objects=%d", count), func(b *testing.B) {
			bound, err := Bind(crdt.NewDoc(), "scene")
			if err != nil {
				b.Fatal(err)
			}
			if _, _, err := bound.DiffScene(scene.SceneIR{}, base, "seed"); err != nil {
				b.Fatal(err)
			}
			previous := base
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				next := nudge(previous, 0.25)
				if _, _, err := bound.DiffScene(previous, next, "drag"); err != nil {
					b.Fatal(err)
				}
				previous = next
			}
		})
	}
}

// BenchmarkView measures the cost of materializing the whole scene from the
// document, which a joining peer and a server-side render both pay.
func BenchmarkView(b *testing.B) {
	for _, count := range []int{100, 1000, 5000} {
		b.Run(fmt.Sprintf("objects=%d", count), func(b *testing.B) {
			bound, err := Bind(crdt.NewDoc(), "scene")
			if err != nil {
				b.Fatal(err)
			}
			if _, _, err := bound.DiffScene(scene.SceneIR{}, benchScene(count), "seed"); err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				view, err := bound.View()
				if err != nil {
					b.Fatal(err)
				}
				if len(view.IR.Objects) != count {
					b.Fatalf("view lost objects: %d", len(view.IR.Objects))
				}
			}
		})
	}
}

// BenchmarkCommands measures the bootstrap stream a joining peer receives.
func BenchmarkCommands(b *testing.B) {
	for _, count := range []int{100, 1000, 5000} {
		b.Run(fmt.Sprintf("objects=%d", count), func(b *testing.B) {
			bound, err := Bind(crdt.NewDoc(), "scene")
			if err != nil {
				b.Fatal(err)
			}
			if _, _, err := bound.DiffScene(scene.SceneIR{}, benchScene(count), "seed"); err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				commands, err := bound.Commands()
				if err != nil {
					b.Fatal(err)
				}
				if len(commands) != count+1 {
					b.Fatalf("bootstrap stream length %d", len(commands))
				}
			}
		})
	}
}

// BenchmarkMergePartitionedEdits measures convergence: two peers each move a
// tenth of the scene while partitioned, then exchange history.
func BenchmarkMergePartitionedEdits(b *testing.B) {
	for _, count := range []int{100, 1000} {
		base := benchScene(count)
		b.Run(fmt.Sprintf("objects=%d", count), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				b.StopTimer()
				server, err := Bind(crdt.NewDoc(), "scene")
				if err != nil {
					b.Fatal(err)
				}
				if _, _, err := server.DiffScene(scene.SceneIR{}, base, "seed"); err != nil {
					b.Fatal(err)
				}
				left, err := Bind(crdt.NewDoc(), "scene")
				if err != nil {
					b.Fatal(err)
				}
				right, err := Bind(crdt.NewDoc(), "scene")
				if err != nil {
					b.Fatal(err)
				}
				if err := left.Doc().Merge(server.Doc()); err != nil {
					b.Fatal(err)
				}
				if err := right.Doc().Merge(server.Doc()); err != nil {
					b.Fatal(err)
				}
				if _, _, err := left.DiffScene(base, nudge(base, 1), "left"); err != nil {
					b.Fatal(err)
				}
				if _, _, err := right.DiffScene(base, nudge(base, -1), "right"); err != nil {
					b.Fatal(err)
				}
				b.StartTimer()

				if err := left.Doc().Merge(right.Doc()); err != nil {
					b.Fatal(err)
				}
				if err := right.Doc().Merge(left.Doc()); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkDiffOnly measures scene.DiffCommands alone, with no document write.
// Compare it against BenchmarkApplyOnly to see which side of the edge costs
// more. The diff compares records by marshaling both sides to JSON, so an edit
// that touches a tenth of the scene still pays for the WHOLE scene.
func BenchmarkDiffOnly(b *testing.B) {
	for _, count := range []int{100, 1000, 5000} {
		base := benchScene(count)
		next := nudge(base, 0.25)
		b.Run(fmt.Sprintf("objects=%d", count), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if len(scene.DiffCommands(base, next)) == 0 {
					b.Fatal("the diff produced no command")
				}
			}
		})
	}
}

// BenchmarkApplyOnly measures the document write for a fixed command stream, so
// the cost of this package is separated from the cost of the diff.
func BenchmarkApplyOnly(b *testing.B) {
	for _, count := range []int{100, 1000, 5000} {
		base := benchScene(count)
		commands := scene.DiffCommands(base, nudge(base, 0.25))
		b.Run(fmt.Sprintf("objects=%d/commands=%d", count, len(commands)), func(b *testing.B) {
			bound, err := Bind(crdt.NewDoc(), "scene")
			if err != nil {
				b.Fatal(err)
			}
			if _, _, err := bound.DiffScene(scene.SceneIR{}, base, "seed"); err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := bound.Apply(commands, "drag"); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
