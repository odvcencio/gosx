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
	want := map[string]float64{"Armature|Idle": 2, "Armature|Walk": 1, "Armature|Run": 0.6, "Armature|Aim": 1, "Armature|Jump": 0.8}
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
