// Package gltfclip reads animation clip names and durations from a glTF 2.0
// asset (.glb or .gltf) so an animgraph.Graph can declare its clips without
// hand-typed durations. It parses only the JSON: glTF requires min and max on
// every animation sampler input accessor, and the largest max is the clip
// length. For a GLB whose input accessors omit max, it falls back to the last
// key in the embedded binary chunk.
//
// It lives apart from animgraph because it uses encoding/json; game builds
// that declare durations in code do not link it.
package gltfclip

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"strings"

	"m31labs.dev/gosx/motion/animgraph"
)

// Info describes one glTF animation.
type Info struct {
	// Name is the glTF animation name, for example "Armature|Walk".
	Name string
	// Duration is the last key time over all of the animation's samplers.
	Duration float64
	// Channels counts the animation's channels.
	Channels int
}

type document struct {
	Accessors []struct {
		BufferView *int      `json:"bufferView"`
		ByteOffset int       `json:"byteOffset"`
		Count      int       `json:"count"`
		Max        []float64 `json:"max"`
		Component  int       `json:"componentType"`
	} `json:"accessors"`
	BufferViews []struct {
		Buffer     int `json:"buffer"`
		ByteOffset int `json:"byteOffset"`
		ByteStride int `json:"byteStride"`
	} `json:"bufferViews"`
	Animations []struct {
		Name     string `json:"name"`
		Channels []any  `json:"channels"`
		Samplers []struct {
			Input int `json:"input"`
		} `json:"samplers"`
	} `json:"animations"`
}

// ReadFile reads clip info from a .glb or .gltf file.
func ReadFile(path string) ([]Info, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Read(data)
}

// Read reads clip info from GLB bytes or glTF JSON bytes, in animation order.
func Read(data []byte) ([]Info, error) {
	jsonChunk, bin := data, []byte(nil)
	if len(data) >= 12 && binary.LittleEndian.Uint32(data) == 0x46546C67 {
		var err error
		jsonChunk, bin, err = splitGLB(data)
		if err != nil {
			return nil, err
		}
	}
	var doc document
	if err := json.Unmarshal(jsonChunk, &doc); err != nil {
		return nil, fmt.Errorf("gltfclip: parse glTF JSON: %w", err)
	}
	out := make([]Info, 0, len(doc.Animations))
	for i, anim := range doc.Animations {
		info := Info{Name: anim.Name, Channels: len(anim.Channels)}
		for _, sampler := range anim.Samplers {
			end, err := inputEnd(&doc, sampler.Input, bin)
			if err != nil {
				return nil, fmt.Errorf("gltfclip: animation %d (%q): %w", i, anim.Name, err)
			}
			info.Duration = math.Max(info.Duration, end)
		}
		out = append(out, info)
	}
	return out, nil
}

func splitGLB(data []byte) (jsonChunk, bin []byte, err error) {
	if binary.LittleEndian.Uint32(data[4:]) != 2 {
		return nil, nil, errors.New("gltfclip: only GLB version 2 is supported")
	}
	at := 12
	for at+8 <= len(data) {
		length := int(binary.LittleEndian.Uint32(data[at:]))
		kind := binary.LittleEndian.Uint32(data[at+4:])
		at += 8
		if length < 0 || at+length > len(data) {
			return nil, nil, errors.New("gltfclip: truncated GLB chunk")
		}
		switch kind {
		case 0x4E4F534A:
			jsonChunk = data[at : at+length]
		case 0x004E4942:
			bin = data[at : at+length]
		}
		at += length
	}
	if jsonChunk == nil {
		return nil, nil, errors.New("gltfclip: GLB has no JSON chunk")
	}
	return jsonChunk, bin, nil
}

func inputEnd(doc *document, index int, bin []byte) (float64, error) {
	if index < 0 || index >= len(doc.Accessors) {
		return 0, fmt.Errorf("sampler input accessor %d out of range", index)
	}
	a := doc.Accessors[index]
	if len(a.Max) > 0 {
		return a.Max[0], nil
	}
	if bin == nil || a.BufferView == nil || *a.BufferView < 0 || *a.BufferView >= len(doc.BufferViews) || a.Count <= 0 || a.Component != 5126 {
		return 0, errors.New("sampler input accessor has no max and no readable float keys")
	}
	v := doc.BufferViews[*a.BufferView]
	if v.Buffer != 0 {
		return 0, errors.New("sampler input keys are outside the GLB binary chunk")
	}
	stride := v.ByteStride
	if stride == 0 {
		stride = 4
	}
	at := v.ByteOffset + a.ByteOffset + (a.Count-1)*stride
	if at < 0 || at+4 > len(bin) {
		return 0, errors.New("sampler input keys run past the binary chunk")
	}
	return float64(math.Float32frombits(binary.LittleEndian.Uint32(bin[at:]))), nil
}

// Clips converts infos into animgraph clip declarations. The Blender
// "Armature|" prefix is removed from names (the renderer matches either
// form). loop reports which clips loop; nil means none do.
func Clips(infos []Info, loop func(name string) bool) []animgraph.Clip {
	out := make([]animgraph.Clip, 0, len(infos))
	for _, info := range infos {
		name := info.Name
		for i := len(name) - 1; i >= 0; i-- {
			if name[i] == '|' {
				name = name[i+1:]
				break
			}
		}
		if name == "" || info.Duration <= 0 {
			continue
		}
		out = append(out, animgraph.Clip{Name: name, Duration: info.Duration, Loop: loop != nil && loop(name)})
	}
	return out
}

type rootDoc struct {
	Nodes []struct {
		Name        string    `json:"name"`
		Children    []int     `json:"children"`
		Translation []float64 `json:"translation"`
		Rotation    []float64 `json:"rotation"`
		Scale       []float64 `json:"scale"`
		Matrix      []float64 `json:"matrix"`
	} `json:"nodes"`
	Accessors []struct {
		BufferView *int   `json:"bufferView"`
		ByteOffset int    `json:"byteOffset"`
		Count      int    `json:"count"`
		Component  int    `json:"componentType"`
		Type       string `json:"type"`
	} `json:"accessors"`
	BufferViews []struct {
		Buffer     int `json:"buffer"`
		ByteOffset int `json:"byteOffset"`
		ByteStride int `json:"byteStride"`
	} `json:"bufferViews"`
	Animations []struct {
		Name     string `json:"name"`
		Channels []struct {
			Sampler int `json:"sampler"`
			Target  struct {
				Node *int   `json:"node"`
				Path string `json:"path"`
			} `json:"target"`
		} `json:"channels"`
		Samplers []struct {
			Input         int    `json:"input"`
			Output        int    `json:"output"`
			Interpolation string `json:"interpolation"`
		} `json:"samplers"`
	} `json:"animations"`
}

// RootPath reads the authored ground-plane path of a clip's root bone from a
// GLB, for animgraph paired clips: the bone's position in the model's space
// (its parents' rest transforms applied, so an armature's 0.01 scale or axis
// rotation is accounted for), sampled hz times per second over the clip.
// node names the bone; empty picks the shallowest node with a translation
// track. clip matches the animation name or the part after "|". Cubic-spline
// tracks are sampled linearly between their keys.
func RootPath(data []byte, clip, node string, hz float64) ([]animgraph.RootKey, error) {
	if len(data) < 12 || binary.LittleEndian.Uint32(data) != 0x46546C67 {
		return nil, errors.New("gltfclip: RootPath needs GLB bytes")
	}
	jsonChunk, bin, err := splitGLB(data)
	if err != nil {
		return nil, err
	}
	var doc rootDoc
	if err := json.Unmarshal(jsonChunk, &doc); err != nil {
		return nil, fmt.Errorf("gltfclip: parse glTF JSON: %w", err)
	}
	if !(hz > 0) {
		hz = 30
	}
	anim := -1
	for i, a := range doc.Animations {
		name := a.Name
		if bar := strings.LastIndexByte(name, '|'); bar >= 0 && name != clip {
			name = name[bar+1:]
		}
		if a.Name == clip || name == clip {
			anim = i
			break
		}
	}
	if anim < 0 {
		return nil, fmt.Errorf("gltfclip: no animation named %q", clip)
	}
	parent := make([]int, len(doc.Nodes))
	for i := range parent {
		parent[i] = -1
	}
	for i, n := range doc.Nodes {
		for _, c := range n.Children {
			if c >= 0 && c < len(parent) {
				parent[c] = i
			}
		}
	}
	depth := func(i int) int {
		d := 0
		for p := parent[i]; p >= 0 && d < len(parent); p = parent[p] {
			d++
		}
		return d
	}
	channel := -1
	a := doc.Animations[anim]
	for i, ch := range a.Channels {
		if ch.Target.Path != "translation" || ch.Target.Node == nil || *ch.Target.Node < 0 || *ch.Target.Node >= len(doc.Nodes) {
			continue
		}
		n := *ch.Target.Node
		if node != "" {
			if doc.Nodes[n].Name == node {
				channel = i
				break
			}
			continue
		}
		if channel < 0 || depth(n) < depth(*a.Channels[channel].Target.Node) {
			channel = i
		}
	}
	if channel < 0 {
		return nil, fmt.Errorf("gltfclip: animation %q has no translation track for %q", clip, node)
	}
	ch := a.Channels[channel]
	if ch.Sampler < 0 || ch.Sampler >= len(a.Samplers) {
		return nil, errors.New("gltfclip: channel sampler out of range")
	}
	sampler := a.Samplers[ch.Sampler]
	times, err := readFloats(&doc, sampler.Input, 1, bin)
	if err != nil {
		return nil, err
	}
	values, err := readFloats(&doc, sampler.Output, 3, bin)
	if err != nil {
		return nil, err
	}
	stride, offset := 3, 0
	if sampler.Interpolation == "CUBICSPLINE" {
		stride, offset = 9, 3
	}
	if len(times) == 0 || len(values) < len(times)*stride {
		return nil, errors.New("gltfclip: root track is empty or short")
	}
	world := identity4()
	for p := parent[*ch.Target.Node]; p >= 0; p = parent[p] {
		world = mul4(nodeMatrix(&doc, p), world)
	}
	step := sampler.Interpolation == "STEP"
	end := times[len(times)-1]
	count := int(math.Floor(end*hz+1e-9)) + 1
	keys := make([]animgraph.RootKey, 0, count+1)
	sample := func(t float64) {
		i := 0
		for i < len(times)-1 && times[i+1] <= t {
			i++
		}
		v := [3]float64{}
		for k := 0; k < 3; k++ {
			v[k] = values[i*stride+offset+k]
			if !step && i < len(times)-1 && t > times[i] {
				f := (t - times[i]) / (times[i+1] - times[i])
				v[k] += (values[(i+1)*stride+offset+k] - v[k]) * f
			}
		}
		x := world[0]*v[0] + world[4]*v[1] + world[8]*v[2] + world[12]
		z := world[2]*v[0] + world[6]*v[1] + world[10]*v[2] + world[14]
		keys = append(keys, animgraph.RootKey{T: t, X: x, Z: z})
	}
	for k := 0; k < count; k++ {
		sample(times[0] + float64(k)/hz)
	}
	if last := keys[len(keys)-1].T; end-last > 1e-6 {
		sample(end)
	}
	return keys, nil
}

func readFloats(doc *rootDoc, index, width int, bin []byte) ([]float64, error) {
	if index < 0 || index >= len(doc.Accessors) {
		return nil, fmt.Errorf("gltfclip: accessor %d out of range", index)
	}
	a := doc.Accessors[index]
	if a.Component != 5126 || a.BufferView == nil || *a.BufferView < 0 || *a.BufferView >= len(doc.BufferViews) || bin == nil {
		return nil, errors.New("gltfclip: root tracks must be float accessors in the GLB binary chunk")
	}
	v := doc.BufferViews[*a.BufferView]
	stride := v.ByteStride
	if stride == 0 {
		stride = 4 * width
	}
	out := make([]float64, 0, a.Count*width)
	for i := 0; i < a.Count; i++ {
		at := v.ByteOffset + a.ByteOffset + i*stride
		if at < 0 || at+4*width > len(bin) {
			return nil, errors.New("gltfclip: accessor runs past the binary chunk")
		}
		for k := 0; k < width; k++ {
			out = append(out, float64(math.Float32frombits(binary.LittleEndian.Uint32(bin[at+4*k:]))))
		}
	}
	return out, nil
}

func identity4() [16]float64 {
	return [16]float64{1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1}
}

func nodeMatrix(doc *rootDoc, i int) [16]float64 {
	n := doc.Nodes[i]
	if len(n.Matrix) == 16 {
		var m [16]float64
		copy(m[:], n.Matrix)
		return m
	}
	t, r, s := [3]float64{}, [4]float64{0, 0, 0, 1}, [3]float64{1, 1, 1}
	copy(t[:], n.Translation)
	copy(r[:], n.Rotation)
	copy(s[:], n.Scale)
	x, y, z, w := r[0], r[1], r[2], r[3]
	return [16]float64{
		(1 - 2*(y*y+z*z)) * s[0], 2 * (x*y + w*z) * s[0], 2 * (x*z - w*y) * s[0], 0,
		2 * (x*y - w*z) * s[1], (1 - 2*(x*x+z*z)) * s[1], 2 * (y*z + w*x) * s[1], 0,
		2 * (x*z + w*y) * s[2], 2 * (y*z - w*x) * s[2], (1 - 2*(x*x+y*y)) * s[2], 0,
		t[0], t[1], t[2], 1,
	}
}

func mul4(a, b [16]float64) [16]float64 {
	var out [16]float64
	for c := 0; c < 4; c++ {
		for r := 0; r < 4; r++ {
			out[c*4+r] = a[r]*b[c*4] + a[4+r]*b[c*4+1] + a[8+r]*b[c*4+2] + a[12+r]*b[c*4+3]
		}
	}
	return out
}
