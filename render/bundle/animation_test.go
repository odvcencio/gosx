package bundle

import (
	"encoding/binary"
	"math"
	"strings"
	"testing"

	"m31labs.dev/gosx/engine"
)

func TestFrameAppliesTopLevelAnimationToInstancedMeshTransforms(t *testing.T) {
	d := newFakeDevice()
	r, err := New(Config{Device: d, Surface: fakeSurface{}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer r.Destroy()

	original := identityTransform()
	b := engine.RenderBundle{
		Camera: engine.RenderCamera{Z: 5, FOV: 1, Near: 0.1, Far: 100},
		InstancedMeshes: []engine.RenderInstancedMesh{{
			ID:            "hero",
			Kind:          "cube",
			VertexCount:   36,
			InstanceCount: 1,
			Transforms:    original,
		}},
		Animations: []engine.RenderAnimation{{
			Name:     "slide",
			Duration: 1,
			Channels: []engine.RenderAnimationChannel{{
				TargetID:      "hero",
				Property:      "translation",
				Times:         []float64{0, 1},
				Values:        []float64{0, 0, 0, 2, 0, 0},
				Interpolation: "LINEAR",
			}},
		}},
	}
	if err := r.Frame(b, 400, 300, 0.5); err != nil {
		t.Fatalf("Frame: %v", err)
	}

	data := latestWriteBytesPrefix(d.queue, "bundle.cull.input:")
	if len(data) < 64 {
		t.Fatalf("missing cull input transform write, got %d bytes", len(data))
	}
	matrix := float32sFromBytes(data[:64])
	if math.Abs(float64(matrix[12]-1)) > 0.0001 || matrix[13] != 0 || matrix[14] != 0 {
		t.Fatalf("animated transform translation = (%v,%v,%v), want (1,0,0)", matrix[12], matrix[13], matrix[14])
	}
	if original[12] != 0 {
		t.Fatalf("Frame mutated caller transform slice: %#v", original)
	}
}

func latestWriteBytesPrefix(q *fakeQueue, labelPrefix string) []byte {
	for i := len(q.writes) - 1; i >= 0; i-- {
		buffer, ok := q.writes[i].buffer.(*fakeBuffer)
		if !ok || !strings.HasPrefix(buffer.label, labelPrefix) {
			continue
		}
		return q.writes[i].data
	}
	return nil
}

func float32sFromBytes(data []byte) []float32 {
	out := make([]float32, len(data)/4)
	for i := range out {
		out[i] = math.Float32frombits(binary.LittleEndian.Uint32(data[i*4 : i*4+4]))
	}
	return out
}
