package transpile

import (
	"strings"
	"testing"
)

func TestStrictManagedLinkEmission(t *testing.T) {
	source := []byte("package app\ntype Props struct { Href string; Label string }\ncomponent Page(props: Props) {\n return <Link href={props.Href} className=\"nav\">{props.Label}{children}</Link>\n}\n")
	got, err := Transpile(source, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "gosx.ManagedLink(props.Href,") || !strings.Contains(got, `gosx.Attr("class", "nav")`) {
		t.Fatal(got)
	}
}

func TestStrictManagedLinkLocalShadowEmission(t *testing.T) {
	for _, source := range []string{
		"package app\ncomponent Page() {\n return <Link/>\n}\ncomponent Link() {\n return <strong>local</strong>\n}\n",
		"package app\ntype P struct{Label string}\ncomponent Page() {\n return <Link Label=\"local\"/>\n}\nfunc Link(props P) Node {\n return <strong>{props.Label}</strong>\n}\n",
	} {
		generated, err := Transpile([]byte(source), Options{})
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(generated, "gosx.ManagedLink(") {
			t.Fatal(generated)
		}
	}
}

func TestStrictManagedLinkDoesNotPromoteIsland(t *testing.T) {
	source := []byte("package app\n//gosx:island\ncomponent Island() {\n return <Link href=\"/docs\">Docs</Link>\n}\n")
	generated, err := Transpile(source, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(generated, "gosx.ManagedLink(") {
		t.Fatal(generated)
	}
	if (&transpiler{strict: 1, strictServer: false}).isStrictManagedLinkTag("Link") {
		t.Fatal("nonserver body promoted")
	}
}
