package web

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"net/url"
	"strings"
	"testing"
)

// glbJSON returns the JSON chunk of a binary glTF.
func glbJSON(t *testing.T, glb []byte) map[string]any {
	t.Helper()
	if len(glb) < 20 || string(glb[:4]) != "glTF" || binary.LittleEndian.Uint32(glb[8:]) != uint32(len(glb)) {
		t.Fatalf("not a binary glTF of the right length")
	}
	n := binary.LittleEndian.Uint32(glb[12:])
	var doc map[string]any
	if err := json.Unmarshal(bytes.TrimRight(glb[20:20+n], " "), &doc); err != nil {
		t.Fatal(err)
	}
	return doc
}

func TestFiguresAreSinglePrimitiveSkinsWithClips(t *testing.T) {
	for name, tc := range map[string]struct {
		glb    []byte
		joints int
		clips  []string
	}{
		"runner": {RunnerGLB(), 13, []string{"Run", "Idle"}},
		"fan":    {FanGLB(), 5, []string{"Cheer", "Sway"}},
	} {
		doc := glbJSON(t, tc.glb)
		meshes := doc["meshes"].([]any)
		prims := meshes[0].(map[string]any)["primitives"].([]any)
		if len(meshes) != 1 || len(prims) != 1 {
			t.Fatalf("%s: %d meshes, %d primitives; the crowd path needs one primitive", name, len(meshes), len(prims))
		}
		skin := doc["skins"].([]any)[0].(map[string]any)
		if got := len(skin["joints"].([]any)); got != tc.joints {
			t.Fatalf("%s: %d joints, want %d", name, got, tc.joints)
		}
		var clips []string
		for _, a := range doc["animations"].([]any) {
			clips = append(clips, a.(map[string]any)["name"].(string))
		}
		if strings.Join(clips, ",") != strings.Join(tc.clips, ",") {
			t.Fatalf("%s: clips %v, want %v", name, clips, tc.clips)
		}
	}
}

func TestPageDeclaresSceneAndEngine(t *testing.T) {
	run := ParseRun(url.Values{"crowd": {"100"}, "thread": {"worker"}, "backend": {"webgl"}})
	if run.Players != 22 || run.Crowd != 100 || run.Thread != "worker" || run.Backend != "webgl" || run.Audio != "off" {
		t.Fatalf("ParseRun = %+v", run)
	}
	page := Page(run, false)
	for _, want := range []string{`id="arena"`, `"GameStress"`, `/engines/stress.wasm`, `src="/gosx/bootstrap-`, `"thread":"worker"`, `window.__stressRun=`} {
		if !strings.Contains(page, want) {
			t.Errorf("page lacks %s", want)
		}
	}
	if ParseRun(url.Values{}).Crowd != 40000 {
		t.Fatal("the default crowd is not 40000")
	}
}
