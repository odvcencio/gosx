package scene

import (
	"math"
	"strings"
	"testing"
)

func TestTransformControlsScaleOffsetsOwnedByLocalGeometry(t *testing.T) {
	for _, size := range []float64{0, .25, 2} {
		wantSize := size
		if wantSize == 0 {
			wantSize = 1.25
		}
		anchor := Vec3(2, 3, 4)
		props := Props{Graph: NewGraph(TransformControls{ID: "offsets", Size: size, Position: anchor, Mode: "scale"})}
		ir := props.SceneIR()
		found := 0
		for _, obj := range ir.Objects {
			if obj.GizmoFormMode != "scale" {
				continue
			}
			found++
			if obj.X != anchor.X || obj.Y != anchor.Y || obj.Z != anchor.Z {
				t.Fatalf("size%g helper%s lacks common anchor: %g,%g,%g", size, obj.ID, obj.X, obj.Y, obj.Z)
			}
			if len(obj.Points) != 8 {
				t.Fatalf("line segment vertices=%d", len(obj.Points))
			}
			var center Vector3
			for _, p := range obj.Points {
				center.X += p.X
				center.Y += p.Y
				center.Z += p.Z
			}
			center.X /= float64(len(obj.Points))
			center.Y /= float64(len(obj.Points))
			center.Z /= float64(len(obj.Points))
			want := Vector3{}
			switch {
			case strings.HasSuffix(obj.ID, "-x"):
				want.X = wantSize
			case strings.HasSuffix(obj.ID, "-y"):
				want.Y = wantSize
			case strings.HasSuffix(obj.ID, "-z"):
				want.Z = wantSize
			}
			if math.Abs(center.X-want.X) > 1e-9 || math.Abs(center.Y-want.Y) > 1e-9 || math.Abs(center.Z-want.Z) > 1e-9 {
				t.Fatalf("size%g helper%s local center=%+v want%+v", size, obj.ID, center, want)
			}
			if obj.GizmoControlID != "offsets" || obj.GizmoSize != wantSize {
				t.Fatal("group metadata changed")
			}
		}
		if found != 3 {
			t.Fatalf("scale helpers=%d", found)
		}
	}
	// Shared box geometry remains centered and separate allocations do not alias.
	a, b := boxHelperGeometry(1, 1, 1, 2), boxHelperGeometry(1, 1, 1, 2)
	a.Points[0].X = 99
	if b.Points[0].X != -.5 {
		t.Fatal("shared box geometry mutated")
	}
}
