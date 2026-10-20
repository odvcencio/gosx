package headless

import (
	"encoding/binary"
	"math"
	"testing"

	"m31labs.dev/gosx/render/gpu"
)

// specPutF32 writes a float32 little-endian at byte off of data.
func specPutF32(data []byte, off int, v float32) {
	binary.LittleEndian.PutUint32(data[off:], math.Float32bits(v))
}

// runActiveMaterial builds a binding-0 material buffer entry over data and
// runs the actual activeMaterial reader on it.
func runActiveMaterial(t *testing.T, data []byte, offset, size int) materialState {
	t.Helper()
	enc := &RenderPassEncoder{
		bindGroups: map[int]*BindGroup{
			1: {
				desc: gpu.BindGroupDesc{
					Entries: []gpu.BindGroupEntry{
						{Binding: 0, Buffer: &Buffer{data: data}, Offset: offset, Size: size},
					},
				},
			},
		},
	}
	return enc.activeMaterial()
}

func TestActiveMaterialSpecularDefaults(t *testing.T) {
	// No binding at all: the state keeps defaultMaterialState, i.e. the
	// 0.04 dielectric F0 replicated as the specular fallback with F90 = 1.
	empty := &RenderPassEncoder{bindGroups: map[int]*BindGroup{}}
	state := empty.activeMaterial()
	if state.specularF0 != [3]float32{0.04, 0.04, 0.04} || state.specularF90 != 1 {
		t.Fatalf("no-binding specular = %v F90 %v, want defaultMaterialState 0.04/F90 1", state.specularF0, state.specularF90)
	}

	// A short 100-byte binding: not even the legacy IOR lane is present, so
	// the state carries the 0.04 dielectric default replicated as the
	// specular fallback with F90 = 1.
	short := make([]byte, 100)
	st := runActiveMaterial(t, short, 0, 0)
	if st.dielectricF0 != 0.04 {
		t.Fatalf("short binding dielectricF0 = %v, want default 0.04", st.dielectricF0)
	}
	if st.specularF0 != [3]float32{0.04, 0.04, 0.04} || st.specularF90 != 1 {
		t.Fatalf("short binding specular = %v F90 %v, want 0.04 replicated with F90 1", st.specularF0, st.specularF90)
	}

	// Negative and huge offsets must not panic and must keep the defaults.
	// Use an architecture-portable max int instead of 1<<40, which overflows
	// int on 32-bit platforms.
	st = runActiveMaterial(t, make([]byte, 128), -16, 0)
	if st.specularF0 != [3]float32{0.04, 0.04, 0.04} || st.specularF90 != 1 {
		t.Fatalf("negative-offset specular = %v F90 %v, want defaults", st.specularF0, st.specularF90)
	}
	maxInt := int(^uint(0) >> 1)
	st = runActiveMaterial(t, make([]byte, 128), maxInt, 0)
	if st.specularF0 != [3]float32{0.04, 0.04, 0.04} || st.specularF90 != 1 {
		t.Fatalf("max-int-offset specular = %v F90 %v, want defaults", st.specularF0, st.specularF90)
	}

	// A very short 8-byte binding cannot even hold the base colour vec4:
	// the baseColour/opacity entry is skipped and every default stays.
	st = runActiveMaterial(t, make([]byte, 8), 0, 0)
	if st.baseColor != [3]float32{1, 1, 1} || st.opacity != 1 {
		t.Fatalf("8-byte binding baseColor/opacity = %v/%v, want defaults", st.baseColor, st.opacity)
	}
	if st.specularF0 != [3]float32{0.04, 0.04, 0.04} || st.specularF90 != 1 || st.dielectricF0 != 0.04 {
		t.Fatalf("8-byte binding specular = %v F90 %v F0 %v, want defaults", st.specularF0, st.specularF90, st.dielectricF0)
	}

	// A declared Size of 8 inside a 128-byte backing must clip the read to
	// the binding end: same default outcome as the short buffer above.
	st = runActiveMaterial(t, make([]byte, 128), 0, 8)
	if st.baseColor != [3]float32{1, 1, 1} || st.opacity != 1 {
		t.Fatalf("size-8-in-128 baseColor/opacity = %v/%v, want defaults", st.baseColor, st.opacity)
	}
	if st.specularF0 != [3]float32{0.04, 0.04, 0.04} || st.specularF90 != 1 || st.dielectricF0 != 0.04 {
		t.Fatalf("size-8-in-128 specular = %v F90 %v F0 %v, want defaults", st.specularF0, st.specularF90, st.dielectricF0)
	}
}

func TestActiveMaterialLegacy112(t *testing.T) {
	// A legacy 112-byte binding carries the byte-100 IOR lane but no vec4.
	data := make([]byte, 112)
	specPutF32(data, 100, 0.11)
	st := runActiveMaterial(t, data, 0, 0)
	if st.dielectricF0 != 0.11 {
		t.Fatalf("legacy dielectricF0 = %v, want 0.11", st.dielectricF0)
	}
	if st.specularF0 != [3]float32{0.11, 0.11, 0.11} || st.specularF90 != 1 {
		t.Fatalf("legacy specular = %v F90 %v, want 0.11 replicated with F90 1", st.specularF0, st.specularF90)
	}

	// An explicit Size of 112 on a 256-byte backing hides the new vec4, so
	// the read must fall back to the replicated legacy lane.
	big := make([]byte, 256)
	specPutF32(big, 100, 0.11)
	specPutF32(big, 112, 0.9)
	specPutF32(big, 124, 0.7)
	st = runActiveMaterial(t, big, 0, 112)
	if st.specularF0 != [3]float32{0.11, 0.11, 0.11} || st.specularF90 != 1 {
		t.Fatalf("size-112 fallback = %v F90 %v, want legacy 0.11 replicated", st.specularF0, st.specularF90)
	}

	// A Size of 64 is too short even for the legacy IOR lane: the binding
	// must not read past its declared end, so the default 0.04 applies.
	st = runActiveMaterial(t, big, 0, 64)
	if st.dielectricF0 != 0.04 {
		t.Fatalf("size-64 dielectricF0 = %v, want default 0.04", st.dielectricF0)
	}
	if st.specularF0 != [3]float32{0.04, 0.04, 0.04} || st.specularF90 != 1 {
		t.Fatalf("size-64 specular = %v F90 %v, want default fallback", st.specularF0, st.specularF90)
	}

	// A derived F0 of exactly 0 at byte 100 is an authored value, not a
	// missing lane: the specular F0 decodes to zeros (corresponding to
	// IOR 1) while F90 stays at the legacy implicit 1 (IOR 1 means no
	// Fresnel boost, not absence).
	zero := make([]byte, 112)
	specPutF32(zero, 100, 0)
	st = runActiveMaterial(t, zero, 0, 0)
	if st.dielectricF0 != 0 {
		t.Fatalf("legacy F0 0 dielectricF0 = %v, want 0", st.dielectricF0)
	}
	if st.specularF0 != [3]float32{} || st.specularF90 != 1 {
		t.Fatalf("legacy F0 0 specular = %v F90 %v, want zeros with F90 1", st.specularF0, st.specularF90)
	}
}

func TestActiveMaterialOffset16(t *testing.T) {
	// Bound unbounded, the 144-byte buffer holds a complete new-style 128-byte
	// record after the offset: the trailing zero vec4 is a legitimate authored
	// intensity of zero, so F90 decodes as 0.
	data := make([]byte, 128+16)
	st := runActiveMaterial(t, data, 16, 0)
	if st.specularF0 != [3]float32{} || st.specularF90 != 0 {
		t.Fatalf("offset-16 full record = %v F90 %v, want zeros with F90 0", st.specularF0, st.specularF90)
	}

	// To exercise the LEGACY layout at offset 16, bind an explicit Size of
	// 112 so the vec4 lane is outside the binding and the fallback applies.
	specPutF32(data, 16+100, 0.11)
	st = runActiveMaterial(t, data, 16, 112)
	if st.specularF0 != [3]float32{0.11, 0.11, 0.11} || st.specularF90 != 1 {
		t.Fatalf("offset-16 legacy = %v F90 %v, want 0.11 replicated", st.specularF0, st.specularF90)
	}
}

func TestActiveMaterialFullVec4(t *testing.T) {
	// A full 128-byte binding with an all-zero vec4 is a legitimate authored
	// intensity of zero: return zeros, never re-default.
	zero := make([]byte, 128)
	st := runActiveMaterial(t, zero, 0, 0)
	if st.specularF0 != [3]float32{} || st.specularF90 != 0 {
		t.Fatalf("all-zero vec4 = %v F90 %v, want zeros", st.specularF0, st.specularF90)
	}

	// Distinct RGB F0 plus an F90 of 0.5 must decode verbatim.
	data := make([]byte, 128)
	specPutF32(data, 112, 0.08)
	specPutF32(data, 116, 0.13)
	specPutF32(data, 120, 0.21)
	specPutF32(data, 124, 0.5)
	st = runActiveMaterial(t, data, 0, 0)
	want := [3]float32{0.08, 0.13, 0.21}
	if st.specularF0 != want || st.specularF90 != 0.5 {
		t.Fatalf("vec4 decoded as %v F90 %v, want %v F90 0.5", st.specularF0, st.specularF90, want)
	}
}
