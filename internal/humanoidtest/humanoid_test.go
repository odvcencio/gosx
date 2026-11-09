package humanoidtest

import (
	"encoding/binary"
	"encoding/json"
	"strings"
	"testing"
)

func TestBuildWritesAValidSkinnedGLB(t *testing.T) {
	for _, scheme := range []Scheme{Mixamo, Rigify, Unreal, Meshy} {
		data, err := Build(Options{Scheme: scheme, ArmatureScale: 0.01})
		if err != nil {
			t.Fatal(err)
		}
		if binary.LittleEndian.Uint32(data) != 0x46546C67 || int(binary.LittleEndian.Uint32(data[8:])) != len(data) {
			t.Fatalf("%s: bad GLB header", scheme)
		}
		n := binary.LittleEndian.Uint32(data[12:])
		var doc struct {
			Nodes []struct {
				Name string `json:"name"`
			} `json:"nodes"`
			Skins []struct {
				Joints []int `json:"joints"`
			} `json:"skins"`
			Animations []struct {
				Name string `json:"name"`
			} `json:"animations"`
		}
		if err := json.Unmarshal(data[20:20+n], &doc); err != nil {
			t.Fatalf("%s: %v", scheme, err)
		}
		if len(doc.Skins) != 1 || len(doc.Skins[0].Joints) != JointCount || len(doc.Animations) != len(Clips) {
			t.Fatalf("%s: skins %d joints %d animations %d", scheme, len(doc.Skins), len(doc.Skins[0].Joints), len(doc.Animations))
		}
		seen := map[string]bool{}
		for _, node := range doc.Nodes {
			if seen[node.Name] {
				t.Fatalf("%s: duplicate node name %q", scheme, node.Name)
			}
			seen[node.Name] = true
		}
	}
	if BoneName(Mixamo, "leftUpperLeg") != "mixamorig:LeftUpLeg" || BoneName(Rigify, "head") != "DEF-spine.006" ||
		BoneName(Unreal, "rightLittleDistal") != "pinky_03_r" || !strings.HasPrefix(BoneName(Meshy, "chest"), "Spine01") {
		t.Fatal("bone names do not follow their schemes")
	}
}
