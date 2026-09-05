package transpile_test

import (
	"m31labs.dev/gosx/transpile"
	"testing"
)

// Diagnostic source/output evidence for the generated invocation decision.
// This is not an assertion of hydration or generated ownership parity.
func TestServerSlotGeneratedInvocationInspection(t *testing.T) {
	for _, tc := range []struct{ name, source string }{
		{"ordinary", `package app
//gosx:island
component Panels() {
 return <div><input value="ordinary" /></div>
}
component Page() {
 return <main><Panels /></main>
}
`},
		{"server slots", `package app
//gosx:island
component Panels() {
 return <div><ServerSlot name="Profile" /></div>
}
component Page() {
 return <main><Panels><section slot="Profile"><input value="caller" /></section></Panels></main>
}
`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			generated, err := transpile.Transpile([]byte(tc.source), transpile.Options{})
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("%s generated output:\n%s", tc.name, generated)
		})
	}
}
