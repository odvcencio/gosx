package scene

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"math"
	"strings"
	"sync"
)

// GeometryAssets is a bounded, content-addressed store of public mesh data.
// Only put geometry here: transforms, user data and private state stay in the
// recipient's scene. Treat interned streams as immutable for the store lifetime.
// Exhaustion leaves new geometry inline; it never evicts a URL already issued.
type GeometryAssets struct {
	mu       sync.Mutex
	prefix   string
	budget   int
	bytes    int
	geometry map[string]geometryAsset
}
type geometryAsset struct {
	data     []byte
	vertices *MeshVertices
}

func NewGeometryAssets(prefix string, byteBudget int) *GeometryAssets {
	return &GeometryAssets{prefix: strings.TrimRight(prefix, "/") + "/", budget: max(0, byteBudget), geometry: make(map[string]geometryAsset)}
}

// GeometryHash identifies every authored vertex stream without rounding floats.
func GeometryHash(v *MeshVertices) string {
	h := sha256.New()
	var word [8]byte
	var block [4096]byte
	used := 0
	writeWord := func(value uint64) {
		binary.LittleEndian.PutUint64(block[used:], value)
		used += 8
		if used == len(block) {
			h.Write(block[:])
			used = 0
		}
	}
	for _, values := range [][]float64{v.Positions, v.Normals, v.UVs, v.Tangents} {
		writeWord(uint64(len(values)))
		for _, value := range values {
			writeWord(math.Float64bits(value))
		}
	}
	writeWord(uint64(len(v.Indices)))
	for _, value := range v.Indices {
		writeWord(uint64(value))
	}
	writeWord(uint64(v.Count))
	h.Write(block[:used])
	if v.Immutable {
		h.Write([]byte{1})
	} else {
		h.Write([]byte{0})
	}
	if v.Dynamic {
		h.Write([]byte{1})
	} else {
		h.Write([]byte{0})
	}
	if v.Revision != nil {
		h.Write([]byte{1})
		binary.LittleEndian.PutUint64(word[:], *v.Revision)
		h.Write(word[:])
	} else {
		h.Write([]byte{0})
	}
	if len(v.Attributes) > 0 {
		data, _ := json.Marshal(v.Attributes)
		h.Write([]byte("attributes"))
		h.Write(data)
	}
	return hex.EncodeToString(h.Sum(nil)) + ".json"
}

// Intern shares equivalent immutable data and returns its public URL. Dynamic
// meshes and values that cannot be encoded remain inline.
func (a *GeometryAssets) Intern(v *MeshVertices) (string, *MeshVertices) {
	if v == nil || v.Dynamic {
		return "", v
	}
	name := GeometryHash(v)
	a.mu.Lock()
	defer a.mu.Unlock()
	if entry, ok := a.geometry[name]; ok {
		return a.prefix + name, entry.vertices
	}
	data, err := json.Marshal(v)
	if err != nil || len(data) > a.budget-a.bytes {
		return "", v
	}
	a.geometry[name] = geometryAsset{data: data, vertices: v}
	a.bytes += len(data)
	return a.prefix + name, v
}

// Retain deduplicates scene snapshots without changing their inline wire shape.
func (a *GeometryAssets) Retain(ir *SceneIR) {
	for i := range ir.Objects {
		_, ir.Objects[i].Vertices = a.Intern(ir.Objects[i].Vertices)
	}
}

// Object replaces geometry with a managed reference, preserving typed metadata.
func (a *GeometryAssets) Object(o ObjectIR) ObjectIR {
	if o.MaterialKind == "standard" && o.Wireframe == nil {
		o.Wireframe = Bool(false)
	}
	if uri, _ := a.Intern(o.Vertices); uri != "" {
		o.VerticesURL, o.Vertices = uri, nil
	}
	return o
}

func (a *GeometryAssets) Commands(commands []Command) []Command {
	out := append([]Command(nil), commands...)
	for i := range out {
		if payload, ok := out[i].Data.(CommandPayload); ok {
			if o, ok := payload.Props.(ObjectIR); ok {
				payload.Props = a.Object(o)
				out[i].Data = payload
			}
		}
	}
	return out
}
