package wire

import "testing"

func TestReferencesPreserveURLBaseAndWorkerContext(t *testing.T) {
	for _, tc := range []struct {
		body, base string
		worker     bool
	}{
		{`import "./x.js";`, ReferenceBaseSource, false},
		{`import("./x.js");`, ReferenceBaseSource, false},
		{`new URL("./x.js", import.meta.url);`, ReferenceBaseSource, false},
		{`fetch("./x.js");`, ReferenceBaseEnvironment, false},
		{`globalThis.fetch("./x.js");`, ReferenceBaseEnvironment, false},
		{`new Worker("./x.js");`, ReferenceBaseEnvironment, true},
		{`new SharedWorker("./x.js");`, ReferenceBaseEnvironment, true},
		{`new Worker(new URL("./x.js", import.meta.url));`, ReferenceBaseSource, true},
		{`new EventSource("./x.js");`, ReferenceBaseEnvironment, false},
		{`new globalThis.EventSource("./x.js");`, ReferenceBaseEnvironment, false},
		{`new WebSocket("./x.js");`, ReferenceBaseEnvironment, false},
		{`new window.WebSocket("./x.js");`, ReferenceBaseEnvironment, false},
		{`new XMLHttpRequest().open("GET", "./x.js");`, ReferenceBaseEnvironment, false},
		{`importScripts("./x.js");`, ReferenceBaseWorker, false},
		{`globalThis.importScripts("./x.js");`, ReferenceBaseWorker, false},
	} {
		t.Run(tc.body, func(t *testing.T) {
			set, err := ScanReferences([]byte(tc.body), KindScript)
			if err != nil || !set.Complete || len(set.Resources) != 1 {
				t.Fatal("literal API lost its reference context", set, err)
			}
			ref := set.Resources[0]
			if ref.URL != "./x.js" || ref.Base != tc.base || ref.Worker != tc.worker {
				t.Fatal("reference context differs", ref)
			}
		})
	}
	set, err := ScanReferences([]byte(`fetch("./x.js"); import("./x.js");`), KindScript)
	if err != nil || !set.Complete || len(set.Resources) != 2 {
		t.Fatal("identical spellings with different URL bases were merged", set, err)
	}
}

func TestReferencesDocumentBaseAndInlineContexts(t *testing.T) {
	set, err := ScanReferences([]byte(`<base target="_blank"><base href="/first/"><base href="/ignored/"><script src="./a.js"></script><script type="module">import "./b.js";fetch("./c.js")</script><style>@import "./d.css";</style><img src="./e.png" style="background:url('./f.png')">`), KindDocument)
	if err != nil || !set.Complete || !set.HasBaseHref || set.BaseHref != "/first/" || len(set.Resources) != 6 {
		t.Fatal("first base or inline resources differ", set, err)
	}
	for _, ref := range set.Resources {
		if ref.Base != ReferenceBaseDocument && ref.Base != ReferenceBaseEnvironment {
			t.Fatal("inline resource retained an external source base", ref)
		}
	}
	set, err = ScanReferences([]byte(`<base href=""><base href="/ignored/"><script src="a.js"></script>`), KindDocument)
	if err != nil || !set.HasBaseHref || set.BaseHref != "" {
		t.Fatal("empty first href did not win", set, err)
	}
	set, err = ScanReferences([]byte(`@import "a.css";.a{background:url('b.png')}`), KindStyle)
	if err != nil || !set.Complete || len(set.Resources) != 2 {
		t.Fatal(set, err)
	}
	for _, ref := range set.Resources {
		if ref.Base != ReferenceBaseSource {
			t.Fatal("stylesheet resource lost its source base", ref)
		}
	}
}
