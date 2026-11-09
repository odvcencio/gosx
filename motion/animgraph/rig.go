package animgraph

import (
	"hash/fnv"
	"sort"
	"strconv"
)

// HumanoidBones lists the canonical humanoid bone names, in parent-before-
// child order. They follow the VRM 1.0 humanoid names. Masks, IK chains,
// look-at chains and Rig.Bones accept these names; the renderer maps them to
// the model's own node names (Mixamo, Rigify, Unreal, Meshy and Blender
// conventions are detected).
var HumanoidBones = []string{
	"hips", "spine", "chest", "upperChest", "neck", "head", "leftEye", "rightEye", "jaw",
	"leftUpperLeg", "leftLowerLeg", "leftFoot", "leftToes",
	"rightUpperLeg", "rightLowerLeg", "rightFoot", "rightToes",
	"leftShoulder", "leftUpperArm", "leftLowerArm", "leftHand",
	"rightShoulder", "rightUpperArm", "rightLowerArm", "rightHand",
	"leftThumbMetacarpal", "leftThumbProximal", "leftThumbDistal",
	"leftIndexProximal", "leftIndexIntermediate", "leftIndexDistal",
	"leftMiddleProximal", "leftMiddleIntermediate", "leftMiddleDistal",
	"leftRingProximal", "leftRingIntermediate", "leftRingDistal",
	"leftLittleProximal", "leftLittleIntermediate", "leftLittleDistal",
	"rightThumbMetacarpal", "rightThumbProximal", "rightThumbDistal",
	"rightIndexProximal", "rightIndexIntermediate", "rightIndexDistal",
	"rightMiddleProximal", "rightMiddleIntermediate", "rightMiddleDistal",
	"rightRingProximal", "rightRingIntermediate", "rightRingDistal",
	"rightLittleProximal", "rightLittleIntermediate", "rightLittleDistal",
}

// Humanoid builds the usual leg and look-at declarations for a humanoid rig:
// foot IK chains named "leftFoot" and "rightFoot" (ground placement and
// alignment, weighted by the optional clip curves "leftFootIK" and
// "rightFootIK" when declared), hand chains "leftHand" and "rightHand", and a
// "head" look-at chain over spine, chest, neck and head. Append the results to
// Graph.IK and Graph.LookAt.
func Humanoid(footCurves bool) ([]TwoBoneIK, []LookAt) {
	left, right := "", ""
	if footCurves {
		left, right = "leftFootIK", "rightFootIK"
	}
	ik := []TwoBoneIK{
		{Name: "leftFoot", Root: "leftUpperLeg", Mid: "leftLowerLeg", End: "leftFoot", Foot: true, AlignToGround: true, WeightCurve: left},
		{Name: "rightFoot", Root: "rightUpperLeg", Mid: "rightLowerLeg", End: "rightFoot", Foot: true, AlignToGround: true, WeightCurve: right},
		{Name: "leftHand", Root: "leftUpperArm", Mid: "leftLowerArm", End: "leftHand"},
		{Name: "rightHand", Root: "rightUpperArm", Mid: "rightLowerArm", End: "rightHand"},
	}
	look := []LookAt{{Name: "head", Bones: []string{"spine", "chest", "neck", "head"}, Weights: []float64{0.15, 0.2, 0.3, 0.35}}}
	return ik, look
}

// rigJSON writes the static part of a graph the renderer needs. Map keys are
// sorted so the bytes, and therefore the hash, are stable.
func rigJSON(g Graph) []byte {
	b := make([]byte, 0, 512)
	b = append(b, `{"v":1,"humanoid":`...)
	humanoid := g.Rig.Humanoid
	if humanoid == "" {
		humanoid = "auto"
	}
	b = appendJSONString(b, humanoid)
	b = append(b, `,"bones":`...)
	b = appendJSONStringMap(b, g.Rig.Bones)
	maskIndex := map[string]int{}
	b = append(b, `,"masks":[`...)
	for i, mask := range g.Masks {
		maskIndex[mask.Name] = i
		if i > 0 {
			b = append(b, ',')
		}
		b = append(b, `{"name":`...)
		b = appendJSONString(b, mask.Name)
		b = append(b, `,"include":`...)
		b = appendJSONStrings(b, mask.Include)
		b = append(b, `,"exclude":`...)
		b = appendJSONStrings(b, mask.Exclude)
		b = append(b, `,"weights":{`...)
		keys := sortedKeys(mask.Weights)
		for k, key := range keys {
			if k > 0 {
				b = append(b, ',')
			}
			b = appendJSONString(b, key)
			b = append(b, ':')
			b = appendJSONNumber(b, clamp01(mask.Weights[key]))
		}
		b = append(b, "}}"...)
	}
	b = append(b, `],"layers":[`...)
	for i, layer := range g.Layers {
		if i > 0 {
			b = append(b, ',')
		}
		mask := -1
		if layer.Mask != "" {
			mask = maskIndex[layer.Mask]
		}
		b = append(b, `{"name":`...)
		b = appendJSONString(b, layer.Name)
		b = append(b, `,"mask":`...)
		b = strconv.AppendInt(b, int64(mask), 10)
		b = append(b, `,"additive":`...)
		b = strconv.AppendBool(b, layer.Additive)
		b = append(b, `,"refClip":`...)
		b = appendJSONString(b, layer.RefClip)
		b = append(b, `,"refTime":`...)
		b = appendJSONNumber(b, layer.RefTime)
		b = append(b, '}')
	}
	b = append(b, `],"ik":[`...)
	for i, chain := range g.IK {
		if i > 0 {
			b = append(b, ',')
		}
		b = append(b, `{"name":`...)
		b = appendJSONString(b, chain.Name)
		b = append(b, `,"root":`...)
		b = appendJSONString(b, chain.Root)
		b = append(b, `,"mid":`...)
		b = appendJSONString(b, chain.Mid)
		b = append(b, `,"end":`...)
		b = appendJSONString(b, chain.End)
		b = append(b, `,"foot":`...)
		b = strconv.AppendBool(b, chain.Foot)
		b = append(b, `,"align":`...)
		b = strconv.AppendBool(b, chain.Foot && chain.AlignToGround)
		b = append(b, '}')
	}
	b = append(b, `],"lookAt":[`...)
	for i, chain := range g.LookAt {
		if i > 0 {
			b = append(b, ',')
		}
		axis := chain.Axis
		if axis == [3]float64{} {
			axis = [3]float64{0, 0, 1}
		}
		maxAngle := chain.MaxAngle
		if maxAngle == 0 {
			maxAngle = defaultMaxLookAt
		}
		b = append(b, `{"name":`...)
		b = appendJSONString(b, chain.Name)
		b = append(b, `,"bones":`...)
		b = appendJSONStrings(b, chain.Bones)
		b = append(b, `,"weights":[`...)
		for k := range chain.Bones {
			if k > 0 {
				b = append(b, ',')
			}
			w := 1.0
			if len(chain.Weights) > 0 {
				w = chain.Weights[k]
			}
			b = appendJSONNumber(b, w)
		}
		b = append(b, `],"axis":[`...)
		b = appendJSONNumber(b, axis[0])
		b = append(b, ',')
		b = appendJSONNumber(b, axis[1])
		b = append(b, ',')
		b = appendJSONNumber(b, axis[2])
		b = append(b, `],"pivot":`...)
		b = appendJSONString(b, chain.Pivot)
		b = append(b, `,"maxAngle":`...)
		b = appendJSONNumber(b, maxAngle)
		b = append(b, '}')
	}
	bone := g.Pelvis.Bone
	if bone == "" {
		bone = "hips"
	}
	b = append(b, `],"pelvis":{"bone":`...)
	b = appendJSONString(b, bone)
	b = append(b, `,"maxDrop":`...)
	b = appendJSONNumber(b, g.Pelvis.MaxDrop)
	b = append(b, `},"sources":[`...)
	for i, source := range g.Rig.Sources {
		if i > 0 {
			b = append(b, ',')
		}
		h := source.Humanoid
		if h == "" {
			h = "auto"
		}
		b = append(b, `{"src":`...)
		b = appendJSONString(b, source.Src)
		b = append(b, `,"humanoid":`...)
		b = appendJSONString(b, h)
		b = append(b, `,"prefix":`...)
		b = appendJSONString(b, source.Prefix)
		b = append(b, '}')
	}
	b = append(b, "]}"...)
	return b
}

func rigHash(data []byte) uint32 {
	h := fnv.New32a()
	_, _ = h.Write(data)
	if sum := h.Sum32(); sum != 0 {
		return sum
	}
	return 1
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func appendJSONStringMap(b []byte, m map[string]string) []byte {
	b = append(b, '{')
	for i, key := range sortedKeys(m) {
		if i > 0 {
			b = append(b, ',')
		}
		b = appendJSONString(b, key)
		b = append(b, ':')
		b = appendJSONString(b, m[key])
	}
	return append(b, '}')
}

func appendJSONStrings(b []byte, values []string) []byte {
	b = append(b, '[')
	for i, v := range values {
		if i > 0 {
			b = append(b, ',')
		}
		b = appendJSONString(b, v)
	}
	return append(b, ']')
}

func appendJSONNumber(b []byte, v float64) []byte {
	if !finite(v) {
		v = 0
	}
	return strconv.AppendFloat(b, v, 'g', -1, 64)
}

const hexDigits = "0123456789abcdef"

func appendJSONString(b []byte, s string) []byte {
	b = append(b, '"')
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '"' || c == '\\':
			b = append(b, '\\', c)
		case c < 0x20:
			b = append(b, '\\', 'u', '0', '0', hexDigits[c>>4], hexDigits[c&0xf])
		default:
			b = append(b, c)
		}
	}
	return append(b, '"')
}
