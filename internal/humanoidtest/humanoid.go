// Package humanoidtest builds a small rigged humanoid as a binary glTF (GLB)
// for tests: 60 joints in a T-pose, Y up, facing +Z, about 1.8 m tall, a box
// skinned rigidly to each bone, and looping Idle, Walk and Run clips, an Aim
// pose, a one-shot Jump, a two-character pair (PairA lunges in and reaches,
// PairB braces and is pushed back; both with root motion), and additive
// Waddle and Bounce style clips. Bone names follow a chosen naming scheme
// (Mixamo, Rigify, Unreal, Meshy), and proportions can change (short legs, a
// long neck, a big head), so tests can exercise humanoid mapping,
// retargeting, paired moves and stylized mascots without third-party assets.
package humanoidtest

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
)

// Scheme selects the bone naming convention.
type Scheme string

const (
	Mixamo Scheme = "mixamo"
	Rigify Scheme = "rigify"
	Unreal Scheme = "unreal"
	Meshy  Scheme = "meshy"
)

// Options configures Build.
type Options struct {
	Scheme Scheme
	// ArmatureScale scales the armature node (Mixamo exports use 0.01 with
	// centimeter bones). Zero means 1; bone offsets scale inversely so the
	// character keeps its size.
	ArmatureScale float64
	// NoMesh omits the skinned mesh, for animation-only clip sources.
	NoMesh bool
	// NoClips omits the animations, for a model that plays retargeted clips.
	NoClips bool
	// LegScale, NeckScale and HeadScale change proportions (zero means 1):
	// leg bone lengths with the hips lowered to keep the feet on the ground,
	// the neck length, and the head box size.
	LegScale, NeckScale, HeadScale float64
	// Color is the mesh base color (linear RGBA). Zero means orange.
	Color [4]float64
}

// Clip durations in seconds, and the footstep times in Walk and Run.
const (
	IdleDuration = 2.0
	WalkDuration = 1.0
	RunDuration  = 0.6
	AimDuration  = 1.0
	JumpDuration = 0.8
	PairDuration = 1.2
	// PairContact is the clip time at which PairA's hands reach PairB.
	PairContact   = 0.5
	StyleDuration = 1.0
)

// PairA's root moves 1.1 m forward (+Z) by contact and 0.5 m more by the
// end; PairB holds its ground until contact, then is pushed 0.6 m back.
// Authored together, PairA's character origin stood 1.5 m behind the
// alignment root facing +Z, and PairB's 0.4 m ahead of it facing -Z.
var (
	PairAPlacement = [3]float64{0, -1.5, 0} // x, z, yaw
	PairBPlacement = [3]float64{0, 0.4, math.Pi}
)

// Footsteps lists the clip times (seconds) at which a foot is furthest
// forward, where a heel strikes: left first, then right.
var Footsteps = map[string][]float64{"Walk": {0.25, 0.75}, "Run": {0.15, 0.45}}

type bone struct {
	canonical string
	parent    int
	offset    [3]float64 // from the parent joint, meters
	thick     float64    // box half-thickness; 0 for end bones without a mesh box
}

// skeleton returns the 60 bones in parent-first order. Canonical names follow
// VRM 1.0; "*End" bones are leaf end markers like Mixamo's HeadTop_End.
func skeleton(opts Options) []bone {
	leg, neckScale, headScale := scaleOr1(opts.LegScale), scaleOr1(opts.NeckScale), scaleOr1(opts.HeadScale)
	// The ankle stays 0.08 m above the ground: hips = 0.08 + 0.05 + 0.82 leg.
	b := []bone{{"hips", -1, [3]float64{0, 0.13 + 0.82*leg, 0}, 0.11}}
	add := func(name string, parent int, off [3]float64, thick float64) int {
		b = append(b, bone{name, parent, off, thick})
		return len(b) - 1
	}
	spine := add("spine", 0, [3]float64{0, 0.1, 0}, 0.11)
	chest := add("chest", spine, [3]float64{0, 0.12, 0}, 0.12)
	upper := add("upperChest", chest, [3]float64{0, 0.12, 0}, 0.12)
	neck := add("neck", upper, [3]float64{0, 0.14, 0}, 0.04)
	head := add("head", neck, [3]float64{0, 0.08 * neckScale, 0.01}, 0.09*headScale)
	add("headEnd", head, [3]float64{0, 0.2, 0}, 0)
	add("leftEye", head, [3]float64{0.03, 0.07, 0.08}, 0)
	add("rightEye", head, [3]float64{-0.03, 0.07, 0.08}, 0)
	add("jaw", head, [3]float64{0, 0.01, 0.04}, 0)
	for _, side := range []struct {
		name string
		x    float64
	}{{"left", 1}, {"right", -1}} {
		s := side.x
		shoulder := add(side.name+"Shoulder", upper, [3]float64{0.04 * s, 0.1, 0}, 0.04)
		arm := add(side.name+"UpperArm", shoulder, [3]float64{0.12 * s, 0, 0}, 0.045)
		fore := add(side.name+"LowerArm", arm, [3]float64{0.27 * s, 0, 0}, 0.04)
		hand := add(side.name+"Hand", fore, [3]float64{0.25 * s, 0, 0}, 0.035)
		for _, finger := range []struct {
			name string
			z    float64
			segs int
		}{{"Thumb", 0.035, 4}, {"Index", 0.02, 3}, {"Middle", 0, 3}, {"Ring", -0.02, 3}, {"Little", -0.04, 3}} {
			parent := hand
			segNames := []string{"Proximal", "Intermediate", "Distal", "End"}
			if finger.name == "Thumb" {
				segNames = []string{"Metacarpal", "Proximal", "Distal", "End"}
			}
			for k := 0; k < finger.segs; k++ {
				off := [3]float64{0.03 * s, 0, 0}
				if k == 0 {
					off = [3]float64{0.08 * s, 0, finger.z}
				}
				thick := 0.008
				if k == 3 {
					thick = 0
				}
				parent = add(side.name+finger.name+segNames[k], parent, off, thick)
			}
		}
		up := add(side.name+"UpperLeg", 0, [3]float64{0.09 * s, -0.05, 0}, 0.06)
		low := add(side.name+"LowerLeg", up, [3]float64{0, -0.42 * leg, 0.01}, 0.05)
		foot := add(side.name+"Foot", low, [3]float64{0, -0.4 * leg, -0.01}, 0.045)
		toes := add(side.name+"Toes", foot, [3]float64{0, -0.06, 0.12}, 0.03)
		add(side.name+"ToesEnd", toes, [3]float64{0, 0, 0.05}, 0)
	}
	return b
}

// JointCount is the number of skin joints Build writes.
const JointCount = 60

func scaleOr1(v float64) float64 {
	if v > 0 {
		return v
	}
	return 1
}

func init() {
	if n := len(skeleton(Options{})); n != JointCount {
		panic(fmt.Sprintf("humanoidtest: skeleton has %d bones, want %d", n, JointCount))
	}
}

// BoneName returns the node name of a canonical bone under a scheme.
func BoneName(scheme Scheme, canonical string) string {
	side, rest := "", canonical
	for _, prefix := range []string{"left", "right"} {
		if len(canonical) > len(prefix) && canonical[:len(prefix)] == prefix && canonical[len(prefix)] >= 'A' && canonical[len(prefix)] <= 'Z' {
			side, rest = prefix, canonical[len(prefix):]
		}
	}
	switch scheme {
	case Rigify:
		return rigifyName(side, rest)
	case Unreal:
		return unrealName(side, rest)
	case Meshy:
		return mixamoName(side, rest, false)
	default:
		return mixamoName(side, rest, true)
	}
}

func mixamoName(side, rest string, prefix bool) string {
	sideName := map[string]string{"left": "Left", "right": "Right"}[side]
	names := map[string]string{
		"hips": "Hips", "spine": "Spine", "chest": "Spine1", "upperChest": "Spine2", "neck": "Neck", "head": "Head",
		"headEnd": "HeadTop_End", "jaw": "Jaw",
		"Eye": "Eye", "Shoulder": "Shoulder", "UpperArm": "Arm", "LowerArm": "ForeArm", "Hand": "Hand",
		"UpperLeg": "UpLeg", "LowerLeg": "Leg", "Foot": "Foot", "Toes": "ToeBase", "ToesEnd": "Toe_End",
	}
	if !prefix {
		names["chest"], names["upperChest"], names["neck"], names["headEnd"] = "Spine01", "Spine02", "neck", "head_end"
	}
	name, ok := names[rest]
	if !ok {
		name = "Hand" + fingerMixamo(rest)
	}
	if prefix {
		return "mixamorig:" + sideName + name
	}
	return sideName + name
}

func fingerMixamo(rest string) string {
	for _, f := range []struct{ canon, mixamo string }{{"Thumb", "Thumb"}, {"Index", "Index"}, {"Middle", "Middle"}, {"Ring", "Ring"}, {"Little", "Pinky"}} {
		if len(rest) > len(f.canon) && rest[:len(f.canon)] == f.canon {
			seg := rest[len(f.canon):]
			n := map[string]string{"Metacarpal": "1", "Proximal": "1", "Intermediate": "2", "Distal": "3", "End": "4"}[seg]
			if f.canon == "Thumb" {
				n = map[string]string{"Metacarpal": "1", "Proximal": "2", "Distal": "3", "End": "4"}[seg]
			}
			return f.mixamo + n
		}
	}
	return rest
}

func rigifyName(side, rest string) string {
	suffix := map[string]string{"left": ".L", "right": ".R"}[side]
	names := map[string]string{
		"hips": "DEF-spine", "spine": "DEF-spine.001", "chest": "DEF-spine.002", "upperChest": "DEF-spine.003", "neck": "DEF-spine.004",
		"head": "DEF-spine.006", "headEnd": "DEF-head_end", "jaw": "DEF-jaw",
		"Eye": "DEF-eye", "Shoulder": "DEF-shoulder", "UpperArm": "DEF-upper_arm", "LowerArm": "DEF-forearm", "Hand": "DEF-hand",
		"UpperLeg": "DEF-thigh", "LowerLeg": "DEF-shin", "Foot": "DEF-foot", "Toes": "DEF-toe", "ToesEnd": "DEF-toe_end",
	}
	name, ok := names[rest]
	if !ok {
		for _, f := range []struct{ canon, rigify string }{{"Thumb", "thumb"}, {"Index", "f_index"}, {"Middle", "f_middle"}, {"Ring", "f_ring"}, {"Little", "f_pinky"}} {
			if len(rest) > len(f.canon) && rest[:len(f.canon)] == f.canon {
				seg := rest[len(f.canon):]
				n := map[string]string{"Metacarpal": "01", "Proximal": "01", "Intermediate": "02", "Distal": "03", "End": "end"}[seg]
				if f.canon == "Thumb" {
					n = map[string]string{"Metacarpal": "01", "Proximal": "02", "Distal": "03", "End": "end"}[seg]
				}
				name = "DEF-" + f.rigify + "." + n
			}
		}
	}
	return name + suffix
}

func unrealName(side, rest string) string {
	suffix := map[string]string{"left": "_l", "right": "_r"}[side]
	names := map[string]string{
		"hips": "pelvis", "spine": "spine_01", "chest": "spine_02", "upperChest": "spine_03", "neck": "neck_01", "head": "head",
		"headEnd": "head_end", "jaw": "jaw",
		"Eye": "eye", "Shoulder": "clavicle", "UpperArm": "upperarm", "LowerArm": "lowerarm", "Hand": "hand",
		"UpperLeg": "thigh", "LowerLeg": "calf", "Foot": "foot", "Toes": "ball", "ToesEnd": "ball_end",
	}
	name, ok := names[rest]
	if !ok {
		for _, f := range []struct{ canon, ue string }{{"Thumb", "thumb"}, {"Index", "index"}, {"Middle", "middle"}, {"Ring", "ring"}, {"Little", "pinky"}} {
			if len(rest) > len(f.canon) && rest[:len(f.canon)] == f.canon {
				seg := rest[len(f.canon):]
				n := map[string]string{"Metacarpal": "01", "Proximal": "01", "Intermediate": "02", "Distal": "03", "End": "end"}[seg]
				if f.canon == "Thumb" {
					n = map[string]string{"Metacarpal": "01", "Proximal": "02", "Distal": "03", "End": "end"}[seg]
				}
				name = f.ue + "_" + n
			}
		}
	}
	return name + suffix
}

type quat [4]float64

func axisAngle(x, y, z, angle float64) quat {
	l := math.Sqrt(x*x + y*y + z*z)
	s := math.Sin(angle / 2)
	return quat{x / l * s, y / l * s, z / l * s, math.Cos(angle / 2)}
}

func (a quat) mul(b quat) quat {
	return quat{
		a[3]*b[0] + a[0]*b[3] + a[1]*b[2] - a[2]*b[1],
		a[3]*b[1] - a[0]*b[2] + a[1]*b[3] + a[2]*b[0],
		a[3]*b[2] + a[0]*b[1] - a[1]*b[0] + a[2]*b[3],
		a[3]*b[3] - a[0]*b[0] - a[1]*b[1] - a[2]*b[2],
	}
}

var identity = quat{0, 0, 0, 1}

const deg = math.Pi / 180

// pose returns each bone's local rotation and the hips translation for clip
// name at time t (seconds).
func pose(bones []bone, index map[string]int, name string, t float64) ([]quat, [3]float64) {
	rot := make([]quat, len(bones))
	for i := range rot {
		rot[i] = identity
	}
	hips := bones[0].offset
	set := func(bone string, q quat) { rot[index[bone]] = q }
	swing := func(period, legAmp, kneeAmp, armAmp, bob float64) {
		p := 2 * math.Pi * t / period
		for _, side := range []struct {
			name  string
			phase float64
		}{{"left", 0}, {"right", math.Pi}} {
			a := p + side.phase
			// Positive X rotation swings a Y-down leg backward (-Z), so the
			// forward swing is negative.
			set(side.name+"UpperLeg", axisAngle(1, 0, 0, -legAmp*math.Sin(a)))
			set(side.name+"LowerLeg", axisAngle(1, 0, 0, kneeAmp*math.Max(0, math.Sin(a-math.Pi/2))))
			set(side.name+"Foot", axisAngle(1, 0, 0, -0.3*legAmp*math.Sin(a)))
			set(side.name+"UpperArm", axisAngle(1, 0, 0, armAmp*math.Sin(a)).mul(axisAngle(0, 0, 1, -70*deg*map[string]float64{"left": 1, "right": -1}[side.name])))
			set(side.name+"LowerArm", axisAngle(0, 1, 0, 20*deg*map[string]float64{"left": 1, "right": -1}[side.name]))
		}
		hips[1] += bob * math.Cos(2*p)
	}
	switch name {
	case "Idle":
		breath := math.Sin(2 * math.Pi * t / IdleDuration)
		set("chest", axisAngle(1, 0, 0, 2*deg*breath))
		for _, side := range []string{"left", "right"} {
			sign := map[string]float64{"left": 1, "right": -1}[side]
			set(side+"UpperArm", axisAngle(0, 0, 1, -72*deg*sign))
		}
	case "Walk":
		swing(WalkDuration, 25*deg, 40*deg, 18*deg, 0.02)
	case "Run":
		swing(RunDuration, 45*deg, 75*deg, 35*deg, 0.04)
		set("spine", axisAngle(1, 0, 0, 10*deg))
	case "Aim":
		set("chest", axisAngle(0, 1, 0, 15*deg))
		set("rightUpperArm", axisAngle(0, 1, 0, -80*deg))
		set("leftUpperArm", axisAngle(0, 1, 0, 60*deg).mul(axisAngle(0, 0, 1, -20*deg)))
	case "PairA":
		u := smooth01(t / PairContact)
		z := 1.1 * u
		if t > PairContact {
			z = 1.1 + 0.5*(t-PairContact)/(PairDuration-PairContact)
		}
		hips[2] += z
		hips[1] -= 0.1 * u
		set("spine", axisAngle(1, 0, 0, 25*deg*u))
		set("leftUpperArm", axisAngle(0, 1, 0, -80*deg*u))
		set("rightUpperArm", axisAngle(0, 1, 0, 80*deg*u))
		set("leftUpperLeg", axisAngle(1, 0, 0, -30*deg*u))
		set("leftLowerLeg", axisAngle(1, 0, 0, 30*deg*u))
	case "PairB":
		brace := smooth01(t / PairContact)
		push := 0.0
		if t > PairContact {
			push = (t - PairContact) / (PairDuration - PairContact)
		}
		hips[2] -= 0.6 * push
		set("spine", axisAngle(1, 0, 0, 10*deg*brace-25*deg*push))
		set("leftUpperArm", axisAngle(0, 1, 0, -40*deg*brace).mul(axisAngle(0, 0, 1, -30*deg*brace)))
		set("rightUpperArm", axisAngle(0, 1, 0, 40*deg*brace).mul(axisAngle(0, 0, 1, 30*deg*brace)))
	case "Waddle":
		// Additive: the first frame is the rest pose, so the offsets are
		// the side-to-side hip roll and the spine's counter-roll.
		a := math.Sin(2 * math.Pi * t / StyleDuration)
		set("hips", axisAngle(0, 0, 1, 10*deg*a))
		set("spine", axisAngle(0, 0, 1, -6*deg*a))
	case "Bounce":
		hips[1] += 0.06 * math.Abs(math.Sin(2*math.Pi*t/StyleDuration))
	case "Jump":
		u := t / JumpDuration
		crouch := math.Max(0, math.Cos(math.Pi*u*2)) * 0.6
		for _, side := range []string{"left", "right"} {
			set(side+"UpperLeg", axisAngle(1, 0, 0, -crouch))
			set(side+"LowerLeg", axisAngle(1, 0, 0, 2*crouch))
		}
		hips[1] += 0.3*math.Sin(math.Pi*u) - 0.15*crouch
	}
	return rot, hips
}

// Clips lists the clip names Build writes, in order.
var Clips = []string{"Idle", "Walk", "Run", "Aim", "Jump", "PairA", "PairB", "Waddle", "Bounce"}

var durations = map[string]float64{
	"Idle": IdleDuration, "Walk": WalkDuration, "Run": RunDuration, "Aim": AimDuration, "Jump": JumpDuration,
	"PairA": PairDuration, "PairB": PairDuration, "Waddle": StyleDuration, "Bounce": StyleDuration,
}

func smooth01(u float64) float64 {
	if u <= 0 {
		return 0
	}
	if u >= 1 {
		return 1
	}
	return u * u * (3 - 2*u)
}

// Build writes the humanoid as GLB bytes.
func Build(opts Options) ([]byte, error) {
	bones := skeleton(opts)
	index := map[string]int{}
	for i, b := range bones {
		index[b.canonical] = i
	}
	scale := opts.ArmatureScale
	if scale == 0 {
		scale = 1
	}
	color := opts.Color
	if color == [4]float64{} {
		color = [4]float64{0.9, 0.35, 0.08, 1}
	}
	var bin bytes.Buffer
	type view struct{ offset, length int }
	var views []view
	addView := func(data any) int {
		for bin.Len()%4 != 0 {
			bin.WriteByte(0)
		}
		start := bin.Len()
		_ = binary.Write(&bin, binary.LittleEndian, data)
		views = append(views, view{start, bin.Len() - start})
		return len(views) - 1
	}
	type accessor = map[string]any
	var accessors []accessor
	addAccessor := func(viewIndex, componentType, count int, kind string, extra map[string]any) int {
		a := accessor{"bufferView": viewIndex, "componentType": componentType, "count": count, "type": kind}
		for k, v := range extra {
			a[k] = v
		}
		accessors = append(accessors, a)
		return len(accessors) - 1
	}
	// Nodes: 0 is the armature (scaled), 1..60 the bones, 61 the mesh.
	nodes := []map[string]any{{"name": "Armature", "children": []int{1}}}
	if scale != 1 {
		nodes[0]["scale"] = []float64{scale, scale, scale}
	}
	world := make([][3]float64, len(bones))
	for i, b := range bones {
		node := map[string]any{"name": BoneName(opts.Scheme, b.canonical), "translation": []float64{b.offset[0] / scale, b.offset[1] / scale, b.offset[2] / scale}}
		var kids []int
		for j, c := range bones {
			if c.parent == i {
				kids = append(kids, j+1)
			}
		}
		if len(kids) > 0 {
			node["children"] = kids
		}
		nodes = append(nodes, node)
		if b.parent >= 0 {
			p := world[b.parent]
			world[i] = [3]float64{p[0] + b.offset[0], p[1] + b.offset[1], p[2] + b.offset[2]}
		} else {
			world[i] = b.offset
		}
	}
	joints := make([]int, len(bones))
	for i := range bones {
		joints[i] = i + 1
	}
	// Inverse bind matrices: the armature scale times the joint's rest
	// world translation, inverted (rest rotations are identity).
	ibm := make([]float32, 0, 16*len(bones))
	for i := range bones {
		w := world[i]
		inv := 1 / scale
		ibm = append(ibm, float32(inv), 0, 0, 0, 0, float32(inv), 0, 0, 0, 0, float32(inv), 0, float32(-w[0]/scale), float32(-w[1]/scale), float32(-w[2]/scale), 1)
	}
	gltf := map[string]any{
		"asset": map[string]any{"version": "2.0", "generator": "gosx internal/humanoidtest"},
		"scene": 0,
	}
	sceneNodes := []int{0}
	if !opts.NoMesh {
		positions, normals, jointIDs, weights, indices := meshBoxes(bones, world)
		pos := addView(positions)
		minP, maxP := bounds(positions)
		posAcc := addAccessor(pos, 5126, len(positions)/3, "VEC3", map[string]any{"min": minP, "max": maxP})
		norAcc := addAccessor(addView(normals), 5126, len(normals)/3, "VEC3", nil)
		jntAcc := addAccessor(addView(jointIDs), 5123, len(jointIDs)/4, "VEC4", nil)
		wgtAcc := addAccessor(addView(weights), 5126, len(weights)/4, "VEC4", nil)
		idxAcc := addAccessor(addView(indices), 5125, len(indices), "SCALAR", nil)
		ibmAcc := addAccessor(addView(ibm), 5126, len(bones), "MAT4", nil)
		gltf["meshes"] = []any{map[string]any{"name": "Body", "primitives": []any{map[string]any{
			"attributes": map[string]int{"POSITION": posAcc, "NORMAL": norAcc, "JOINTS_0": jntAcc, "WEIGHTS_0": wgtAcc},
			"indices":    idxAcc, "material": 0,
		}}}}
		gltf["materials"] = []any{map[string]any{"name": "Skin", "pbrMetallicRoughness": map[string]any{"baseColorFactor": color, "metallicFactor": 0, "roughnessFactor": 0.8}}}
		gltf["skins"] = []any{map[string]any{"joints": joints, "inverseBindMatrices": ibmAcc, "skeleton": 1}}
		nodes = append(nodes, map[string]any{"name": "Body", "mesh": 0, "skin": 0})
		sceneNodes = append(sceneNodes, len(nodes)-1)
	}
	gltf["nodes"] = nodes
	gltf["scenes"] = []any{map[string]any{"nodes": sceneNodes}}
	var animations []any
	clips := Clips
	if opts.NoClips {
		clips = nil
	}
	for _, name := range clips {
		duration := durations[name]
		keys := int(math.Round(duration*30)) + 1
		times := make([]float32, keys)
		for k := range times {
			times[k] = float32(duration * float64(k) / float64(keys-1))
		}
		timeAcc := addAccessor(addView(times), 5126, keys, "SCALAR", map[string]any{"min": []float64{0}, "max": []float64{float64(times[keys-1])}})
		var samplers, channels []any
		rotations := make([][]float32, len(bones))
		hips := make([]float32, 0, keys*3)
		for k := 0; k < keys; k++ {
			rot, h := pose(bones, index, name, float64(times[k]))
			for i := range bones {
				rotations[i] = append(rotations[i], float32(rot[i][0]), float32(rot[i][1]), float32(rot[i][2]), float32(rot[i][3]))
			}
			hips = append(hips, float32(h[0]/scale), float32(h[1]/scale), float32(h[2]/scale))
		}
		for i := range bones {
			if bones[i].thick == 0 {
				continue // end bones are not animated, as in exported rigs
			}
			acc := addAccessor(addView(rotations[i]), 5126, keys, "VEC4", nil)
			samplers = append(samplers, map[string]any{"input": timeAcc, "output": acc, "interpolation": "LINEAR"})
			channels = append(channels, map[string]any{"sampler": len(samplers) - 1, "target": map[string]any{"node": i + 1, "path": "rotation"}})
		}
		acc := addAccessor(addView(hips), 5126, keys, "VEC3", nil)
		samplers = append(samplers, map[string]any{"input": timeAcc, "output": acc, "interpolation": "LINEAR"})
		channels = append(channels, map[string]any{"sampler": len(samplers) - 1, "target": map[string]any{"node": 1, "path": "translation"}})
		animations = append(animations, map[string]any{"name": "Armature|" + name, "samplers": samplers, "channels": channels})
	}
	if len(animations) > 0 {
		gltf["animations"] = animations
	}
	for bin.Len()%4 != 0 {
		bin.WriteByte(0)
	}
	bufferViews := make([]any, len(views))
	for i, v := range views {
		bufferViews[i] = map[string]any{"buffer": 0, "byteOffset": v.offset, "byteLength": v.length}
	}
	gltf["bufferViews"] = bufferViews
	gltf["accessors"] = accessors
	gltf["buffers"] = []any{map[string]any{"byteLength": bin.Len()}}
	jsonBytes, err := json.Marshal(gltf)
	if err != nil {
		return nil, err
	}
	for len(jsonBytes)%4 != 0 {
		jsonBytes = append(jsonBytes, ' ')
	}
	var out bytes.Buffer
	total := 12 + 8 + len(jsonBytes) + 8 + bin.Len()
	_ = binary.Write(&out, binary.LittleEndian, []uint32{0x46546C67, 2, uint32(total), uint32(len(jsonBytes)), 0x4E4F534A})
	out.Write(jsonBytes)
	_ = binary.Write(&out, binary.LittleEndian, []uint32{uint32(bin.Len()), 0x004E4942})
	out.Write(bin.Bytes())
	return out.Bytes(), nil
}

// meshBoxes builds one flat-shaded box per bone with a mesh box, from the
// joint toward its first child (or along its own offset for leaves), skinned
// fully to that bone.
func meshBoxes(bones []bone, world [][3]float64) (pos, nor []float32, jnt []uint16, wgt []float32, idx []uint32) {
	for i, b := range bones {
		if b.thick == 0 {
			continue
		}
		start := world[i]
		end := start
		for j, c := range bones {
			if c.parent == i {
				end = world[j]
				break
			}
		}
		if end == start {
			end = [3]float64{start[0] + b.offset[0]*0.5, start[1] + b.offset[1]*0.5, start[2] + b.offset[2]*0.5}
		}
		lo, hi := [3]float64{}, [3]float64{}
		for a := 0; a < 3; a++ {
			lo[a] = math.Min(start[a], end[a]) - b.thick
			hi[a] = math.Max(start[a], end[a]) + b.thick
		}
		if i == 0 {
			lo[1], hi[1] = start[1]-0.08, start[1]+0.1
		}
		faces := [6]struct {
			n    [3]float64
			axis int
			side float64
		}{{[3]float64{1, 0, 0}, 0, 1}, {[3]float64{-1, 0, 0}, 0, 0}, {[3]float64{0, 1, 0}, 1, 1}, {[3]float64{0, -1, 0}, 1, 0}, {[3]float64{0, 0, 1}, 2, 1}, {[3]float64{0, 0, -1}, 2, 0}}
		for _, f := range faces {
			base := uint32(len(pos) / 3)
			u, v := (f.axis+1)%3, (f.axis+2)%3
			for _, c := range [4][2]float64{{0, 0}, {1, 0}, {1, 1}, {0, 1}} {
				var p [3]float64
				p[f.axis] = lo[f.axis] + (hi[f.axis]-lo[f.axis])*f.side
				p[u] = lo[u] + (hi[u]-lo[u])*c[0]
				p[v] = lo[v] + (hi[v]-lo[v])*c[1]
				pos = append(pos, float32(p[0]), float32(p[1]), float32(p[2]))
				nor = append(nor, float32(f.n[0]), float32(f.n[1]), float32(f.n[2]))
				jnt = append(jnt, uint16(i), 0, 0, 0)
				wgt = append(wgt, 1, 0, 0, 0)
			}
			if f.side == 1 {
				idx = append(idx, base, base+1, base+2, base, base+2, base+3)
			} else {
				idx = append(idx, base, base+2, base+1, base, base+3, base+2)
			}
		}
	}
	return
}

func bounds(p []float32) ([]float64, []float64) {
	lo := []float64{math.Inf(1), math.Inf(1), math.Inf(1)}
	hi := []float64{math.Inf(-1), math.Inf(-1), math.Inf(-1)}
	for i := 0; i < len(p); i += 3 {
		for a := 0; a < 3; a++ {
			lo[a] = math.Min(lo[a], float64(p[i+a]))
			hi[a] = math.Max(hi[a], float64(p[i+a]))
		}
	}
	return lo, hi
}
