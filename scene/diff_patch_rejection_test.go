package scene

import (
	"math/rand/v2"
	"reflect"
	"testing"
)

func TestObjectPatchRejectionMatchesEncoder(t *testing.T) {
	generator := &recordGenerator{rng: rand.New(rand.NewPCG(0xc0, 0x9f)), nonFinite: true}
	for iteration := range 600 {
		previous := ObjectIR{}
		generator.fill(reflect.ValueOf(&previous).Elem(), 1)
		next := previous
		if iteration%3 == 0 {
			generator.fill(reflect.ValueOf(&next).Elem(), 1)
		} else if iteration%3 == 1 {
			field := iteration % reflect.TypeFor[ObjectIR]().NumField()
			generator.fill(reflect.ValueOf(&next).Elem().Field(field), 1)
		}
		assertObjectPatchMatchesReference(t, previous, next)
	}
}

func TestObjectPatchRejectionDoesNotAllocate(t *testing.T) {
	previous := ObjectIR{ID: "mesh", Color: "white", Interactive: true, Vertices: &MeshVertices{Positions: make([]float64, 18000), Count: 6000}}
	for _, state := range []bool{false, true} {
		next := previous
		if state {
			next.Color = "red"
		} else {
			next.Interactive = false
		}
		mask := objectProofOmitTransform
		if state {
			mask |= objectProofOmitState
		}
		if objectPatchFieldsEqual(&previous, &next, mask) {
			t.Fatal("non-patch edit was lost")
		}
		if n := testing.AllocsPerRun(100, func() {
			if objectPatchFieldsEqual(&previous, &next, mask) {
				panic("non-patch edit was lost")
			}
		}); n != 0 {
			t.Fatalf("state=%v: rejection allocated %g times", state, n)
		}
		assertObjectPatchMatchesReference(t, previous, next)
	}
}

func BenchmarkDiffSceneMeshPatchRejection(b *testing.B) {
	for _, name := range []string{"transform-interaction", "transform-color", "state-color"} {
		b.Run(name, func(b *testing.B) {
			previous, next := SceneIR{}, SceneIR{}
			for i := range 28 {
				mesh := ObjectIR{ID: string(rune('a' + i)), Color: "white", Kind: "gltf-mesh", Interactive: true, Vertices: &MeshVertices{Positions: make([]float64, 18000), Count: 6000}}
				previous.Objects = append(previous.Objects, mesh)
				if name == "transform-interaction" {
					mesh.Interactive = false
				} else {
					mesh.Color = "red"
				}
				next.Objects = append(next.Objects, mesh)
			}
			options := DiffOptions{PatchTransforms: true, PatchObjectState: name == "state-color"}
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				DiffScene(previous, next, options)
			}
		})
	}
}
