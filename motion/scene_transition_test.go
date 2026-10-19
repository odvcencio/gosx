package motion_test

import (
	"encoding/json"
	"m31labs.dev/gosx/motion"
	"m31labs.dev/gosx/scene"
	"strings"
	"testing"
	"time"
)

func TestSceneTransitionGoAuthoringWireAndValidation(t *testing.T) {
	p := motion.NewProgram("form")
	progress := p.Hover("pose", "#form")
	transition := scene.CommandTransition{From: []scene.Command{scene.SetCameraCommand(map[string]any{"z": 10})}, To: []scene.Command{scene.SetCameraCommand(map[string]any{"z": 6})}}
	if err := p.BindSceneTransition(progress, " #form ", transition); err != nil {
		t.Fatal(err)
	}
	p.WithTransport(motion.TransportOptions{Duration: time.Second, Scenes: []string{"#form"}})
	if len(p.Transport.Scenes) != 1 {
		t.Fatal("binding selectors should be deduplicated and independent of authoring order")
	}
	data, err := p.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	var decoded motion.Program
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Bindings[0].Selector != "#form" || !strings.Contains(string(decoded.Bindings[0].Transition), `"version":1`) {
		t.Fatalf("bad transition wire: %s", data)
	}
	_, compiled, err := motion.CompileCSS(p)
	if err != nil {
		t.Fatal(err)
	}
	compiled.Bindings[0].Transition[0] = 'x'
	if _, err := p.Marshal(); err != nil {
		t.Fatal("CSS compilation aliases the source transition plan")
	}
	for _, binding := range []motion.Binding{
		{Target: motion.BindingSceneTransition, Property: "pose", Signal: "missing", Selector: "#form", Transition: decoded.Bindings[0].Transition},
		{Target: motion.BindingSceneTransition, Property: "pose", Signal: progress, Selector: " ", Transition: decoded.Bindings[0].Transition},
		{Target: motion.BindingSceneTransition, Property: "pose", Signal: progress, Selector: "#form", Transition: json.RawMessage(`{"version":2,"from":[],"to":[]}`)},
	} {
		p.Bindings = []motion.Binding{binding}
		if _, err := p.Marshal(); err == nil {
			t.Fatal("accepted invalid transition binding")
		}
	}
	if err := p.BindSceneTransition(progress, "#form", nil); err == nil {
		t.Fatal("accepted missing source")
	}
}
