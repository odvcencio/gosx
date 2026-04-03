package route

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"
)

type testSceneSpread struct {
	Width int
}

func (testSceneSpread) GoSXSpreadProps() map[string]any {
	return map[string]any{
		"width": 640,
		"scene": map[string]any{
			"objects": []map[string]any{{"kind": "box"}},
		},
	}
}

func TestTryCallValueBuildsStructAndVariadicArgs(t *testing.T) {
	type props struct {
		Name  string
		Count int
	}

	fn := func(input props, suffix ...string) string {
		return input.Name + ":" + string(rune('0'+input.Count)) + ":" + suffix[0] + "," + suffix[1]
	}

	got, ok := tryCallValue(fn, []any{
		map[string]any{"name": "docs", "count": "2"},
		"alpha",
		"beta",
	})
	if !ok {
		t.Fatal("expected reflected call to succeed")
	}
	if got != "docs:2:alpha,beta" {
		t.Fatalf("unexpected result: %#v", got)
	}
}

func TestTryCallValueRejectsErrorResult(t *testing.T) {
	fn := func(string) (string, error) {
		return "", errors.New("boom")
	}

	got, ok := tryCallValue(fn, []any{"x"})
	if ok {
		t.Fatalf("expected reflected call to fail, got %#v", got)
	}
}

func TestIndirectValueHelpersSupportPointerCollections(t *testing.T) {
	values := []string{"zero", "one", "two"}
	if got := indexValue(&values, 1); got != "one" {
		t.Fatalf("indexValue returned %#v", got)
	}

	record := struct{ Name string }{Name: "gosx"}
	if got := selectValue(&record, "Name"); got != "gosx" {
		t.Fatalf("selectValue returned %#v", got)
	}

	lookup := map[string]int{"count": 3}
	if got, ok := mapLookup(&lookup, "count"); !ok || !reflect.DeepEqual(got, 3) {
		t.Fatalf("mapLookup returned (%#v, %v)", got, ok)
	}
}

func TestReflectValueSupportsPointerAndNilTargets(t *testing.T) {
	ptrValue, ok := reflectValue("7", reflect.TypeOf((*int)(nil)))
	if !ok {
		t.Fatal("expected pointer target conversion to succeed")
	}
	ptr, ok := ptrValue.Interface().(*int)
	if !ok || ptr == nil || *ptr != 7 {
		t.Fatalf("unexpected pointer conversion: %#v", ptrValue.Interface())
	}

	nilValue, ok := reflectValue(nil, reflect.TypeOf((*int)(nil)))
	if !ok {
		t.Fatal("expected nil pointer conversion to succeed")
	}
	if !nilValue.IsNil() {
		t.Fatalf("expected nil pointer, got %#v", nilValue.Interface())
	}
}

func TestSpreadPropsUsesGoSXSpreadPropsWhenAvailable(t *testing.T) {
	got := spreadProps(testSceneSpread{Width: 320})
	if want := 640; !reflect.DeepEqual(got["width"], want) {
		t.Fatalf("expected width %#v, got %#v", want, got["width"])
	}
	sceneValue, ok := got["scene"].(map[string]any)
	if !ok {
		t.Fatalf("expected lowered scene map, got %#v", got["scene"])
	}
	objects, ok := sceneValue["objects"].([]map[string]any)
	if !ok || len(objects) != 1 || objects[0]["kind"] != "box" {
		t.Fatalf("expected lowered scene objects, got %#v", sceneValue["objects"])
	}
}

func TestMarshalEnginePropsCanonicalizesCaseAliasesRecursively(t *testing.T) {
	raw := marshalEngineProps(map[string]any{
		"Background": "#ff00ff",
		"background": "#08151f",
		"Camera": map[string]any{
			"Near": 0.15,
			"Far":  64,
		},
		"Scene": map[string]any{
			"Objects": []map[string]any{
				{
					"Kind":  "box",
					"Color": "#8de1ff",
				},
			},
		},
	})
	if len(raw) == 0 {
		t.Fatal("expected non-empty engine props payload")
	}

	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal engine props: %v", err)
	}

	if got := decoded["background"]; got != "#08151f" {
		t.Fatalf("expected lower-camel background to win, got %#v", got)
	}
	if _, ok := decoded["Background"]; ok {
		t.Fatalf("did not expect exported background alias in %#v", decoded)
	}

	camera, ok := decoded["camera"].(map[string]any)
	if !ok {
		t.Fatalf("expected canonical camera map, got %#v", decoded["camera"])
	}
	if camera["near"] != 0.15 || camera["far"] != float64(64) {
		t.Fatalf("expected canonical clip planes, got %#v", camera)
	}
	if _, ok := camera["Near"]; ok {
		t.Fatalf("did not expect exported camera aliases in %#v", camera)
	}

	sceneValue, ok := decoded["scene"].(map[string]any)
	if !ok {
		t.Fatalf("expected canonical scene map, got %#v", decoded["scene"])
	}
	objects, ok := sceneValue["objects"].([]any)
	if !ok || len(objects) != 1 {
		t.Fatalf("expected canonical scene objects, got %#v", sceneValue["objects"])
	}
	object, ok := objects[0].(map[string]any)
	if !ok {
		t.Fatalf("expected canonical object map, got %#v", objects[0])
	}
	if object["kind"] != "box" || object["color"] != "#8de1ff" {
		t.Fatalf("expected lower-camel object keys, got %#v", object)
	}
	if _, ok := object["Kind"]; ok {
		t.Fatalf("did not expect exported object aliases in %#v", object)
	}
}
