package web

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"math"
)

// The stress scene's figures are built here, in code, so the example needs no
// art files. Each figure is one skinned primitive made of boxes, one box per
// joint, with looping clips. One primitive per figure is what the Scene3D
// crowd path needs (docs/scene3d-crowd-motion.md).

type rigJoint struct {
	name   string
	parent int
	pos    [3]float32 // bind position in model space
}

type rigPart struct {
	joint        int
	center, half [3]float32
}

// rigChannel animates one joint: a rotation about axis (0 X, 2 Z) of
// amplitude*sin(phase+offset)+bias, or, with lift > 0, the root's height.
type rigChannel struct {
	joint             int
	axis              int
	amplitude, offset float64
	bias              float64
	harmonic          float64
	lift              float64
	rectify           bool // keep only the positive half of the wave
}

type rigClip struct {
	name     string
	duration float64
	channels []rigChannel
}

type rig struct {
	joints []rigJoint
	parts  []rigPart
	clips  []rigClip
}

// runnerRig is a 13-joint humanoid with Run and Idle clips. The runner faces +Z.
func runnerRig() rig {
	j := []rigJoint{
		{"hips", -1, [3]float32{0, 0.95, 0}},
		{"spine", 0, [3]float32{0, 1.15, 0}},
		{"chest", 1, [3]float32{0, 1.35, 0}},
		{"neck", 2, [3]float32{0, 1.55, 0}},
		{"head", 3, [3]float32{0, 1.62, 0}},
		{"upperArm.L", 2, [3]float32{0.24, 1.47, 0}},
		{"forearm.L", 5, [3]float32{0.24, 1.2, 0}},
		{"upperArm.R", 2, [3]float32{-0.24, 1.47, 0}},
		{"forearm.R", 7, [3]float32{-0.24, 1.2, 0}},
		{"thigh.L", 0, [3]float32{0.1, 0.92, 0}},
		{"shin.L", 9, [3]float32{0.1, 0.5, 0}},
		{"thigh.R", 0, [3]float32{-0.1, 0.92, 0}},
		{"shin.R", 11, [3]float32{-0.1, 0.5, 0}},
	}
	p := []rigPart{
		{0, [3]float32{0, 0.95, 0}, [3]float32{0.16, 0.08, 0.1}},
		{2, [3]float32{0, 1.3, 0}, [3]float32{0.19, 0.21, 0.11}},
		{4, [3]float32{0, 1.73, 0}, [3]float32{0.1, 0.12, 0.11}},
		{5, [3]float32{0.24, 1.34, 0}, [3]float32{0.05, 0.14, 0.05}},
		{6, [3]float32{0.24, 1.07, 0}, [3]float32{0.045, 0.13, 0.045}},
		{7, [3]float32{-0.24, 1.34, 0}, [3]float32{0.05, 0.14, 0.05}},
		{8, [3]float32{-0.24, 1.07, 0}, [3]float32{0.045, 0.13, 0.045}},
		{9, [3]float32{0.1, 0.71, 0}, [3]float32{0.075, 0.21, 0.075}},
		{10, [3]float32{0.1, 0.27, 0.02}, [3]float32{0.065, 0.25, 0.08}},
		{11, [3]float32{-0.1, 0.71, 0}, [3]float32{0.075, 0.21, 0.075}},
		{12, [3]float32{-0.1, 0.27, 0.02}, [3]float32{0.065, 0.25, 0.08}},
	}
	run := rigClip{name: "Run", duration: 0.7, channels: []rigChannel{
		{joint: 9, amplitude: 0.75},
		{joint: 11, amplitude: 0.75, offset: math.Pi},
		{joint: 10, amplitude: -1.1, offset: math.Pi / 2, rectify: true},
		{joint: 12, amplitude: -1.1, offset: 3 * math.Pi / 2, rectify: true},
		{joint: 5, amplitude: 0.65, offset: math.Pi},
		{joint: 7, amplitude: 0.65},
		{joint: 6, amplitude: 0, bias: -0.9},
		{joint: 8, amplitude: 0, bias: -0.9},
		{joint: 1, amplitude: 0.06, harmonic: 2, bias: 0.12},
		{joint: 0, lift: 0.05, harmonic: 2},
	}}
	idle := rigClip{name: "Idle", duration: 2.4, channels: []rigChannel{
		{joint: 2, amplitude: 0.03},
		{joint: 5, axis: 2, amplitude: 0.04, bias: 0.08},
		{joint: 7, axis: 2, amplitude: -0.04, bias: -0.08},
		{joint: 0, lift: 0.01},
	}}
	return rig{joints: j, parts: p, clips: []rigClip{run, idle}}
}

// fanRig is a 5-joint, 60-triangle figure for the crowd, with Cheer and Sway.
func fanRig() rig {
	j := []rigJoint{
		{"hips", -1, [3]float32{0, 0.9, 0}},
		{"chest", 0, [3]float32{0, 1.2, 0}},
		{"head", 1, [3]float32{0, 1.5, 0}},
		{"arm.L", 1, [3]float32{0.22, 1.42, 0}},
		{"arm.R", 1, [3]float32{-0.22, 1.42, 0}},
	}
	p := []rigPart{
		{0, [3]float32{0, 0.45, 0}, [3]float32{0.15, 0.45, 0.11}},
		{1, [3]float32{0, 1.17, 0}, [3]float32{0.19, 0.24, 0.11}},
		{2, [3]float32{0, 1.6, 0}, [3]float32{0.1, 0.11, 0.1}},
		{3, [3]float32{0.25, 1.22, 0}, [3]float32{0.05, 0.21, 0.05}},
		{4, [3]float32{-0.25, 1.22, 0}, [3]float32{0.05, 0.21, 0.05}},
	}
	cheer := rigClip{name: "Cheer", duration: 1.1, channels: []rigChannel{
		{joint: 3, axis: 2, amplitude: 1.1, bias: 1.6},
		{joint: 4, axis: 2, amplitude: -1.1, bias: -1.6},
		{joint: 0, lift: 0.08, harmonic: 2, rectify: true},
	}}
	sway := rigClip{name: "Sway", duration: 3.2, channels: []rigChannel{
		{joint: 1, axis: 2, amplitude: 0.08},
		{joint: 3, axis: 2, amplitude: 0.05, bias: 0.15},
		{joint: 4, axis: 2, amplitude: -0.05, bias: -0.15},
	}}
	return rig{joints: j, parts: p, clips: []rigClip{cheer, sway}}
}

// RunnerGLB returns the binary glTF of the skinned runner figure.
func RunnerGLB() []byte { return runnerRig().glb() }

// FanGLB returns the binary glTF of the crowd figure.
func FanGLB() []byte { return fanRig().glb() }

// Triangles reports the triangle count of a figure, for the docs.
func (r rig) triangles() int { return len(r.parts) * 12 }

func (r rig) glb() []byte {
	var bin bytes.Buffer
	var views []map[string]any
	var accessors []map[string]any
	view := func(data any, target int) int {
		for bin.Len()%4 != 0 {
			bin.WriteByte(0)
		}
		offset := bin.Len()
		_ = binary.Write(&bin, binary.LittleEndian, data)
		v := map[string]any{"buffer": 0, "byteOffset": offset, "byteLength": bin.Len() - offset}
		if target != 0 {
			v["target"] = target
		}
		views = append(views, v)
		return len(views) - 1
	}
	accessor := func(viewIndex, componentType, count int, kind string, extra map[string]any) int {
		a := map[string]any{"bufferView": viewIndex, "componentType": componentType, "count": count, "type": kind}
		for k, v := range extra {
			a[k] = v
		}
		accessors = append(accessors, a)
		return len(accessors) - 1
	}

	// Geometry: one box (24 vertices, 12 triangles) per part.
	var pos, nrm []float32
	var jnt []uint8
	var wgt []float32
	var idx []uint16
	minP := [3]float32{math.MaxFloat32, math.MaxFloat32, math.MaxFloat32}
	maxP := [3]float32{-math.MaxFloat32, -math.MaxFloat32, -math.MaxFloat32}
	faces := [6]struct{ n, u, v [3]float32 }{
		{[3]float32{1, 0, 0}, [3]float32{0, 0, -1}, [3]float32{0, 1, 0}},
		{[3]float32{-1, 0, 0}, [3]float32{0, 0, 1}, [3]float32{0, 1, 0}},
		{[3]float32{0, 1, 0}, [3]float32{1, 0, 0}, [3]float32{0, 0, -1}},
		{[3]float32{0, -1, 0}, [3]float32{1, 0, 0}, [3]float32{0, 0, 1}},
		{[3]float32{0, 0, 1}, [3]float32{1, 0, 0}, [3]float32{0, 1, 0}},
		{[3]float32{0, 0, -1}, [3]float32{-1, 0, 0}, [3]float32{0, 1, 0}},
	}
	for _, part := range r.parts {
		for _, f := range faces {
			base := uint16(len(pos) / 3)
			for _, c := range [4][2]float32{{-1, -1}, {1, -1}, {1, 1}, {-1, 1}} {
				for k := 0; k < 3; k++ {
					p := part.center[k] + part.half[k]*(f.n[k]+c[0]*f.u[k]+c[1]*f.v[k])
					pos = append(pos, p)
					minP[k] = min(minP[k], p)
					maxP[k] = max(maxP[k], p)
				}
				nrm = append(nrm, f.n[0], f.n[1], f.n[2])
				jnt = append(jnt, uint8(part.joint), 0, 0, 0)
				wgt = append(wgt, 1, 0, 0, 0)
			}
			idx = append(idx, base, base+1, base+2, base, base+2, base+3)
		}
	}
	count := len(pos) / 3
	attrs := map[string]int{
		"POSITION":  accessor(view(pos, 34962), 5126, count, "VEC3", map[string]any{"min": minP[:], "max": maxP[:]}),
		"NORMAL":    accessor(view(nrm, 34962), 5126, count, "VEC3", nil),
		"JOINTS_0":  accessor(view(jnt, 34962), 5121, count, "VEC4", nil),
		"WEIGHTS_0": accessor(view(wgt, 34962), 5126, count, "VEC4", nil),
	}
	indices := accessor(view(idx, 34963), 5123, len(idx), "SCALAR", nil)

	// Joints: node 0 is the skinned mesh, nodes 1.. are the joints.
	nodes := []map[string]any{{"name": "figure", "mesh": 0, "skin": 0}}
	jointNodes := make([]int, len(r.joints))
	ibm := make([]float32, 0, 16*len(r.joints))
	for i, joint := range r.joints {
		local := joint.pos
		if joint.parent >= 0 {
			pp := r.joints[joint.parent].pos
			local = [3]float32{joint.pos[0] - pp[0], joint.pos[1] - pp[1], joint.pos[2] - pp[2]}
		}
		nodes = append(nodes, map[string]any{"name": joint.name, "translation": local[:]})
		jointNodes[i] = len(nodes) - 1
		ibm = append(ibm, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, -joint.pos[0], -joint.pos[1], -joint.pos[2], 1)
	}
	var roots []int
	for i, joint := range r.joints {
		if joint.parent < 0 {
			roots = append(roots, jointNodes[i])
			continue
		}
		parent := nodes[jointNodes[joint.parent]]
		children, _ := parent["children"].([]int)
		parent["children"] = append(children, jointNodes[i])
	}
	ibmAccessor := accessor(view(ibm, 0), 5126, len(r.joints), "MAT4", nil)

	// Clips: 13 keys per cycle, the last equal to the first, so loops are seamless.
	const keys = 13
	var animations []map[string]any
	for _, clip := range r.clips {
		times := make([]float32, keys)
		for k := range times {
			times[k] = float32(clip.duration * float64(k) / (keys - 1))
		}
		timeAccessor := accessor(view(times, 0), 5126, keys, "SCALAR", map[string]any{"min": []float32{0}, "max": []float32{float32(clip.duration)}})
		var samplers, channels []map[string]any
		for _, ch := range clip.channels {
			harmonic := ch.harmonic
			if harmonic == 0 {
				harmonic = 1
			}
			var out int
			var path string
			if ch.lift > 0 {
				base := r.joints[ch.joint].pos
				values := make([]float32, 0, keys*3)
				for k := 0; k < keys; k++ {
					w := math.Sin(2*math.Pi*harmonic*float64(k)/(keys-1) + ch.offset)
					if ch.rectify || ch.harmonic == 2 {
						w = math.Abs(w)
					}
					values = append(values, base[0], base[1]+float32(ch.lift*w), base[2])
				}
				out, path = accessor(view(values, 0), 5126, keys, "VEC3", nil), "translation"
			} else {
				values := make([]float32, 0, keys*4)
				for k := 0; k < keys; k++ {
					w := math.Sin(2*math.Pi*harmonic*float64(k)/(keys-1) + ch.offset)
					if ch.rectify && w < 0 {
						w = 0
					}
					angle := ch.bias + ch.amplitude*w
					s, c := float32(math.Sin(angle/2)), float32(math.Cos(angle/2))
					if ch.axis == 2 {
						values = append(values, 0, 0, s, c)
					} else {
						values = append(values, s, 0, 0, c)
					}
				}
				out, path = accessor(view(values, 0), 5126, keys, "VEC4", nil), "rotation"
			}
			samplers = append(samplers, map[string]any{"input": timeAccessor, "output": out, "interpolation": "LINEAR"})
			channels = append(channels, map[string]any{"sampler": len(samplers) - 1, "target": map[string]any{"node": jointNodes[ch.joint], "path": path}})
		}
		animations = append(animations, map[string]any{"name": clip.name, "samplers": samplers, "channels": channels})
	}

	doc := map[string]any{
		"asset":  map[string]any{"version": "2.0", "generator": "gosx examples/game-stress"},
		"scene":  0,
		"scenes": []map[string]any{{"nodes": append([]int{0}, roots...)}},
		"nodes":  nodes,
		"meshes": []map[string]any{{"primitives": []map[string]any{{"attributes": attrs, "indices": indices, "material": 0}}}},
		"materials": []map[string]any{{"name": "figure", "pbrMetallicRoughness": map[string]any{
			"baseColorFactor": []float32{1, 1, 1, 1}, "metallicFactor": 0, "roughnessFactor": 0.8}}},
		"skins":       []map[string]any{{"joints": jointNodes, "inverseBindMatrices": ibmAccessor, "skeleton": roots[0]}},
		"animations":  animations,
		"accessors":   accessors,
		"bufferViews": views,
		"buffers":     []map[string]any{{"byteLength": bin.Len()}},
	}
	chunk, _ := json.Marshal(doc)
	for len(chunk)%4 != 0 {
		chunk = append(chunk, ' ')
	}
	payload := bin.Bytes()
	for len(payload)%4 != 0 {
		payload = append(payload, 0)
	}
	var out bytes.Buffer
	out.WriteString("glTF")
	_ = binary.Write(&out, binary.LittleEndian, uint32(2))
	_ = binary.Write(&out, binary.LittleEndian, uint32(12+8+len(chunk)+8+len(payload)))
	_ = binary.Write(&out, binary.LittleEndian, uint32(len(chunk)))
	_ = binary.Write(&out, binary.LittleEndian, uint32(0x4E4F534A))
	out.Write(chunk)
	_ = binary.Write(&out, binary.LittleEndian, uint32(len(payload)))
	_ = binary.Write(&out, binary.LittleEndian, uint32(0x004E4942))
	out.Write(payload)
	return out.Bytes()
}
