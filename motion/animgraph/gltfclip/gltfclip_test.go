package gltfclip

import (
	"encoding/binary"
	"math"
	"testing"

	"m31labs.dev/gosx/internal/humanoidtest"
)

func TestReadHumanoidGLB(t *testing.T) {
	data, err := humanoidtest.Build(humanoidtest.Options{Scheme: humanoidtest.Mixamo})
	if err != nil {
		t.Fatal(err)
	}
	infos, err := Read(data)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]float64{
		"Armature|Idle": 2, "Armature|Walk": 1, "Armature|Run": 0.6, "Armature|Aim": 1, "Armature|Jump": 0.8,
		"Armature|PairA": 1.2, "Armature|PairB": 1.2, "Armature|Waddle": 1, "Armature|Bounce": 1,
	}
	if len(infos) != len(want) {
		t.Fatalf("infos = %+v", infos)
	}
	for _, info := range infos {
		if d, ok := want[info.Name]; !ok || math.Abs(info.Duration-d) > 1e-6 || info.Channels < 50 {
			t.Fatalf("info %+v, want duration %v and a channel per animated bone", info, d)
		}
	}
	clips := Clips(infos, func(name string) bool { return name != "Jump" })
	if clips[1].Name != "Walk" || !clips[1].Loop || clips[4].Name != "Jump" || clips[4].Loop {
		t.Fatalf("clips = %+v", clips)
	}
}

func TestReadFallsBackToBinaryKeysWithoutMax(t *testing.T) {
	json := `{"asset":{"version":"2.0"},"accessors":[{"bufferView":0,"componentType":5126,"count":3,"type":"SCALAR"}],` +
		`"bufferViews":[{"buffer":0,"byteOffset":0,"byteLength":12}],"buffers":[{"byteLength":12}],` +
		`"animations":[{"name":"Wave","channels":[{}],"samplers":[{"input":0}]}]}`
	for len(json)%4 != 0 {
		json += " "
	}
	bin := make([]byte, 12)
	for i, v := range []float32{0, 0.5, 1.25} {
		binary.LittleEndian.PutUint32(bin[i*4:], math.Float32bits(v))
	}
	glb := binary.LittleEndian.AppendUint32(nil, 0x46546C67)
	glb = binary.LittleEndian.AppendUint32(glb, 2)
	glb = binary.LittleEndian.AppendUint32(glb, uint32(12+8+len(json)+8+len(bin)))
	glb = binary.LittleEndian.AppendUint32(glb, uint32(len(json)))
	glb = binary.LittleEndian.AppendUint32(glb, 0x4E4F534A)
	glb = append(glb, json...)
	glb = binary.LittleEndian.AppendUint32(glb, uint32(len(bin)))
	glb = binary.LittleEndian.AppendUint32(glb, 0x004E4942)
	glb = append(glb, bin...)
	infos, err := Read(glb)
	if err != nil {
		t.Fatal(err)
	}
	if len(infos) != 1 || infos[0].Duration != 1.25 {
		t.Fatalf("infos = %+v", infos)
	}
	if _, err := Read([]byte(`{"accessors":[{"count":2}],"animations":[{"samplers":[{"input":0}]}]}`)); err == nil {
		t.Fatal("glTF JSON without max or a binary chunk was accepted")
	}
}

func TestRootPathReadsTheAuthoredRootInModelSpace(t *testing.T) {
	for _, scale := range []float64{1, 0.01} {
		data, err := humanoidtest.Build(humanoidtest.Options{Scheme: humanoidtest.Mixamo, ArmatureScale: scale})
		if err != nil {
			t.Fatal(err)
		}
		keys, err := RootPath(data, "PairA", "", 30)
		if err != nil {
			t.Fatal(err)
		}
		// 1.2 s at 30 Hz: keys at 0, 1/30, ..., 1.2 (37 keys). PairA's
		// root moves 1.1 m forward by the 0.5 s contact, 1.6 m by the end;
		// the armature's 0.01 scale must not change that.
		if len(keys) != 37 || keys[0].T != 0 || math.Abs(keys[36].T-1.2) > 1e-6 {
			t.Fatalf("scale %v: %d keys from %v to %v", scale, len(keys), keys[0].T, keys[len(keys)-1].T)
		}
		if math.Abs(keys[0].Z) > 1e-5 || math.Abs(keys[15].Z-1.1) > 1e-4 || math.Abs(keys[36].Z-1.6) > 1e-4 || math.Abs(keys[15].X) > 1e-6 {
			t.Fatalf("scale %v: path start %+v contact %+v end %+v", scale, keys[0], keys[15], keys[36])
		}
	}
	data, _ := humanoidtest.Build(humanoidtest.Options{Scheme: humanoidtest.Mixamo})
	if _, err := RootPath(data, "Nope", "", 30); err == nil {
		t.Fatal("unknown clip accepted")
	}
	if _, err := RootPath(data, "Walk", "mixamorig:Nobody", 30); err == nil {
		t.Fatal("unknown node accepted")
	}
}
