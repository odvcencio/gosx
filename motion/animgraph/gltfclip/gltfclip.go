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
