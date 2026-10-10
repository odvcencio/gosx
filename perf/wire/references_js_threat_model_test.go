package wire

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"golang.org/x/net/html"
)

func scanJavaScriptPolicyContext(source, context string) (ReferenceSet, error) {
	switch context {
	case "script":
		return ScanReferences([]byte(source), KindScript)
	case "inline":
		return ScanReferences([]byte("<script>"+source+"</script>"), KindDocument)
	case "handler":
		return ScanReferences([]byte(executableAttributeDocument("handler", source)), KindDocument)
	case "srcdoc", "srcdoc-handler":
		kind := "script"
		if context == "srcdoc-handler" {
			kind = "handler"
		}
		body := `<iframe srcdoc="` + html.EscapeString(executableAttributeDocument(kind, source)) + `"></iframe>`
		return ScanReferences([]byte(body), KindDocument)
	case "javascript-url":
		return ScanReferences([]byte(executableAttributeDocument(context, source)), KindDocument)
	default:
		panic(context)
	}
}

func TestReferencesEnumerationReflectionRegressions(t *testing.T) {
	for _, hidden := range []string{
		`Object.values(globalThis).find(value => typeof value === "function" && value.name === "fetch")("/hidden.json");`,
		`for (const [key, value] of Object.entries(globalThis)) { if (key === "fetch") value("/hidden.json"); }`,
	} {
		for _, context := range []string{"script", "inline", "handler", "srcdoc"} {
			set, err := scanJavaScriptPolicyContext(`fetch("/before.json");`+hidden+`import("/after.js");`, context)
			want := []Reference{{"/after.js", KindScript, false}, {"/before.json", KindOther, false}}
			if err != nil || set.Complete || !reflect.DeepEqual(set.Resources, want) {
				t.Errorf("enumeration lost uncertainty/references: context=%s set=%+v err=%v", context, set, err)
			}
		}
	}
}

func TestReferencesGeneratedJavaScriptThreatFamilies(t *testing.T) {
	type idiom struct{ family, source string }
	idioms := []idiom{}
	add := func(family, source string) { idioms = append(idioms, idiom{family, source}) }
	// Independent of the production denylist: each named enumeration API must
	// reject coverage even on a local receiver, without a bare global witness.
	for _, method := range strings.Fields("values entries keys getOwnPropertyNames getOwnPropertySymbols getOwnPropertyDescriptor getOwnPropertyDescriptors getPrototypeOf fromEntries assign") {
		for _, pattern := range []string{`Object.NAME(receiver);`, `const enumerate = Object.NAME; enumerate(receiver);`, `consume(Object.NAME);`} {
			add("enumeration/"+method, strings.ReplaceAll(pattern, "NAME", method))
		}
	}
	for _, method := range strings.Fields("apply construct defineProperty deleteProperty get getOwnPropertyDescriptor getPrototypeOf has isExtensible ownKeys preventExtensions set setPrototypeOf futureMethod") {
		add("reflection/"+method, "Reflect."+method+"(receiver);")
	}
	for _, alias := range strings.Fields("globalThis window self top parent frames opener defaultView") {
		for _, pattern := range []string{
			`const root = ALIAS; consume(root);`, `consume(ALIAS);`, `typeof ALIAS;`,
			`const copy = {...ALIAS}; consume(copy);`, `const {fetch: request} = ALIAS; request("/hidden.json");`,
			`const root = (0, ALIAS); consume(root);`, `ALIAS = receiver;`, `const wrapper = {ALIAS}; consume(wrapper);`,
			`const {"ALIAS": root} = receiver; consume(root);`,
		} {
			add("global-alias/"+alias, strings.ReplaceAll(pattern, "ALIAS", alias))
		}
		for _, property := range []string{`"fetch"`, `"fet" + "ch"`, `selected`} {
			add("computed/"+alias, fmt.Sprintf(`%s[%s]("/hidden.json");`, alias, property))
		}
	}
	for _, source := range []string{
		`consume(document.defaultView);`, `const root = document.defaultView; consume(root);`,
		`consume(window.parent);`, `consume(window.frames);`, `consume(window.opener);`,
	} {
		add("global-alias/member", source)
	}
	for _, source := range []string{
		`for (const key in receiver) { consume(key); }`, `for (let key in globalThis) consume(key);`,
		`for (const key in window) { if (key === "fetch") window[key]("/hidden.json"); }`,
	} {
		add("enumeration/for-in", source)
	}
	for _, source := range []string{
		`eval("fetch('/hidden.json')");`, `new Function("fetch('/hidden.json')")();`,
		`handler.constructor("fetch('/hidden.json')")();`, `setTimeout("fetch('/hidden.json')", 10);`,
		`setInterval("fetch('/hidden.json')", 10);`, `receiver[selected]("/hidden.json");`,
	} {
		add("construction/computed", source)
	}
	for _, source := range []string{
		`document.createElement("script");`, `document.write("<img>");`, `new Image();`,
		`new XMLHttpRequest();`, `receiver.src = "/hidden.js";`, `navigator.sendBeacon("/hidden.json");`,
	} {
		add("loader-denylist", source)
	}
	contexts := []string{"script", "inline", "handler", "srcdoc", "srcdoc-handler", "javascript-url"}
	count := 0
	for i, idiom := range idioms {
		for _, context := range contexts {
			t.Run(fmt.Sprintf("%s/%d/%s", idiom.family, i, context), func(t *testing.T) {
				set, err := scanJavaScriptPolicyContext(idiom.source+`import("/visible.js");`, context)
				if err != nil || set.Complete || !reflect.DeepEqual(set.Resources, []Reference{{"/visible.js", KindScript, false}}) {
					t.Fatalf("family silently certified/lost known reference: %+v err=%v source=%s", set, err, idiom.source)
				}
			})
			count++
		}
	}
	t.Logf("threat-family idioms=%d contexts=%d generated cases=%d", len(idioms), len(contexts), count)
	if count != 960 {
		t.Fatalf("threat-family matrix changed: %d; audit the inventory", count)
	}
}

func TestReferencesStaticGlobalMembersStayComplete(t *testing.T) {
	for _, alias := range strings.Fields("globalThis window self top parent frames opener defaultView document.defaultView window.parent window.frames window.opener") {
		for _, source := range []string{alias + `.fetch("/literal.json");`, `typeof ` + alias + `.fixtureReady; fetch("/literal.json");`} {
			set, err := ScanReferences([]byte(source), KindScript)
			if err != nil || !set.Complete || !reflect.DeepEqual(set.Resources, []Reference{{"/literal.json", KindOther, false}}) {
				t.Errorf("static member rejected: %q %+v err=%v", source, set, err)
			}
		}
	}
	set, err := ScanReferences([]byte(`for (const entry of [1, 2]) console.log(entry); fetch("/literal.json");`), KindScript)
	if err != nil || !set.Complete || !reflect.DeepEqual(set.Resources, []Reference{{"/literal.json", KindOther, false}}) {
		t.Fatalf("for-of confused with enumeration: %+v err=%v", set, err)
	}
}
