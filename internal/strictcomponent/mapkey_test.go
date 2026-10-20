package strictcomponent

import (
	"reflect"
	"testing"
)

func TestStaticMapKeyPaths(t *testing.T) {
	for _, key := range []string{"", "email", "profile.email", "a[0]", "x\"\n", "日本語"} {
		segment := MapKeySegment(key)
		if got, ok := MapKey(segment); !ok || got != key {
			t.Fatalf("map key %q round trip = %q, %v", key, got, ok)
		}
	}
	source := `"Value: " + props.Form.Values["profile.email"]`
	if err := ValidateServerExpression(source); err != nil {
		t.Fatal(err)
	}
	want := [][]string{{"Form", "Values", MapKeySegment("profile.email")}}
	if got := ServerExpressionPropPaths(source); !reflect.DeepEqual(got, want) {
		t.Fatalf("map paths = %#v, want %#v", got, want)
	}
	for _, source := range []string{`props.Form.Values[props.Key]`, `props.Items[0]`, `values["email"]`} {
		if err := ValidateServerExpression(source); err == nil {
			t.Fatalf("accepted %s", source)
		}
	}
}
