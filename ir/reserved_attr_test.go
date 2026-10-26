package ir_test

import (
	"strings"
	"testing"

	"m31labs.dev/gosx"
	. "m31labs.dev/gosx/ir"
)

func lowerReservedAttr(t *testing.T, island bool, attr string) error {
	t.Helper()
	directive := ""
	if island {
		directive = "//gosx:island\n"
	}
	src := "package app\n\n" + directive + "func Counter(props any) Node {\n\treturn <button " + attr + ">+</button>\n}\n"
	tree, lang, err := gosx.Parse([]byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	_, lowerErr := Lower(tree.RootNode(), []byte(src), lang)
	return lowerErr
}

func TestIslandRejectsReservedGosxAttrs(t *testing.T) {
	cases := []struct{ name, attr, want string }{
		{"on-click expr", `data-gosx-on-click={props.h}`, "data-gosx-on-click"},
		{"on-click static", `data-gosx-on-click="x"`, "data-gosx-on-click"},
		{"on-input", `data-gosx-on-input="x"`, "data-gosx-on-input"},
		{"handler", `data-gosx-handler="x"`, "data-gosx-handler"},
		{"path", `data-gosx-path="0.1"`, "data-gosx-path"},
		{"mixed case", `Data-Gosx-On-Click="x"`, "Data-Gosx-On-Click"},
		{"upper handler", `DATA-GOSX-HANDLER="x"`, "DATA-GOSX-HANDLER"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := lowerReservedAttr(t, true, tc.attr)
			if err == nil {
				t.Fatalf("expected a diagnostic for %s", tc.attr)
			}
			msg := err.Error()
			if !strings.Contains(msg, tc.want) || !strings.Contains(msg, "reserved") || !strings.Contains(msg, "data-app-") {
				t.Fatalf("diagnostic should name the attribute, say reserved, and suggest a name: %s", msg)
			}
		})
	}
}

func TestIslandAllowsOrdinaryDataAttrs(t *testing.T) {
	for _, attr := range []string{
		`data-id="1"`, `data-gosx-link`, `data-gosx-region`, `data-app-on-click="x"`,
		`data-gosx-pathology="x"`, `data-gosx-handlers="x"`, `onClick={inc}`,
	} {
		if err := lowerReservedAttr(t, true, attr); err != nil {
			t.Errorf("%s should lower in an island: %v", attr, err)
		}
	}
}

func TestNonIslandKeepsReservedGosxAttrs(t *testing.T) {
	if err := lowerReservedAttr(t, false, `data-gosx-handler="x"`); err != nil {
		t.Fatalf("server components are out of scope for the rule: %v", err)
	}
}
