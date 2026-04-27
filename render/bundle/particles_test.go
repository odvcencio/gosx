package bundle

import "testing"

func TestRegisterParticleForceKindAliasesExistingShaderForce(t *testing.T) {
	cleanup := RegisterParticleForceKind("lift", "wind")
	defer cleanup()

	if got := particleForceKind("lift"); got != particleForceWind {
		t.Fatalf("particleForceKind(lift) = %d, want wind %d", got, particleForceWind)
	}

	cleanup()
	if got := particleForceKind("lift"); got != particleForceNone {
		t.Fatalf("particleForceKind(lift) after cleanup = %d, want none", got)
	}
}
