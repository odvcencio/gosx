//go:build js && wasm

package scene

import (
	"strings"
	"syscall/js"
	"testing"
	"time"
)

func burstFixture(t *testing.T) js.Value {
	t.Helper()
	f := js.Global().Get("Function").New(`
const saved = Object.getOwnPropertyDescriptor(globalThis, '__gosx');
const f = { calls: [], reject: false };
globalThis.__gosx = { scene3d: { burstParticles(target, plan) {
  f.calls.push({ target, plan });
  return f.reject ? Promise.reject(new Error("no mount")) : Promise.resolve({ finished: Promise.resolve({ finished: true }) });
} } };
f.restore = () => { if (saved) Object.defineProperty(globalThis, '__gosx', saved); else delete globalThis.__gosx; };
return f;
`).Invoke()
	t.Cleanup(func() { f.Call("restore") })
	return f
}

func turfBurst() ParticleBurst {
	return ParticleBurst{
		ID: "hit-7", Count: 24, Delay: 30 * time.Millisecond,
		Emitter:  ParticleEmitter{Kind: "disc", Position: Vec3(2, .05, -40), Radius: .4, Lifetime: .5, Scatter: .3},
		Forces:   []ParticleForce{{Kind: "gravity", Strength: 1}},
		Material: ParticleMaterial{Color: "#3c5a2a", Size: .05, Opacity: .8},
	}
}

func TestStartParticleBurstHandsThePlanToThePage(t *testing.T) {
	f := burstFixture(t)
	if err := StartParticleBurst("arena", turfBurst()); err != nil {
		t.Fatal(err)
	}
	calls := f.Get("calls")
	if calls.Length() != 1 {
		t.Fatalf("the page saw %d bursts, want 1", calls.Length())
	}
	call := calls.Index(0)
	if call.Get("target").String() != "arena" {
		t.Fatalf("target = %q", call.Get("target").String())
	}
	plan := call.Get("plan")
	if plan.Get("version").Int() != 1 || plan.Get("id").String() != "hit-7" {
		t.Fatalf("plan version %v id %v", plan.Get("version"), plan.Get("id"))
	}
	// The delay is carried in seconds: 30 ms.
	if got := plan.Get("delay").Float(); got != .03 {
		t.Fatalf("delay = %v s, want 0.03", got)
	}
	if plan.Get("particles").Get("count").Int() != 24 {
		t.Fatalf("particle count = %v", plan.Get("particles").Get("count"))
	}
}

func TestStartParticleBurstReportsWhatItCanAtCallTime(t *testing.T) {
	f := burstFixture(t)
	if err := StartParticleBurst("", turfBurst()); err == nil {
		t.Fatal("an empty target was accepted")
	}
	bad := turfBurst()
	bad.Count = 0
	if err := StartParticleBurst("arena", bad); err == nil {
		t.Fatal("a burst with no particles was accepted")
	}
	if f.Get("calls").Length() != 0 {
		t.Fatal("a refused burst reached the page")
	}
	// A page-side refusal arrives after the call returns: no error here, and
	// the rejection is handled, so it is not an unhandled promise rejection.
	f.Set("reject", true)
	if err := StartParticleBurst("arena", turfBurst()); err != nil {
		t.Fatalf("a later page-side refusal surfaced as a call error: %v", err)
	}
	time.Sleep(5 * time.Millisecond)
	// With no Scene3D runtime the call says so.
	js.Global().Get("__gosx").Delete("scene3d")
	if err := StartParticleBurst("arena", turfBurst()); err == nil || !strings.Contains(err.Error(), "unavailable") {
		t.Fatalf("missing runtime error = %v", err)
	}
}
