package ir_test

import (
	"testing"

	"m31labs.dev/gosx/ir"
)

// TestValidateWarningsFlagsNearMissAttrName covers gosx#249 check 5: a
// misspelled declarative navigation primitive renders and silently
// no-ops, with nothing anywhere to explain why. ValidateWarnings must
// catch a near-miss of a real primitive name.
func TestValidateWarningsFlagsNearMissAttrName(t *testing.T) {
	source := []byte(`package main

func Page() Node {
	return <span data-gosx-hearbeat="/api/version" data-gosx-heartbeat-interval="4s"></span>
}
`)
	prog, err := parse(t, source)
	if err != nil {
		t.Fatalf("Lower failed: %v", err)
	}

	warnings := ir.ValidateWarnings(prog)
	if len(warnings) != 1 {
		t.Fatalf("expected exactly one warning, got %+v", warnings)
	}
	want := `"data-gosx-hearbeat" is not a recognized gosx navigation attribute; did you mean "data-gosx-heartbeat"?`
	if warnings[0].Message != want {
		t.Fatalf("unexpected warning message: got %q, want %q", warnings[0].Message, want)
	}
	if warnings[0].Severity != ir.SeverityWarning {
		t.Fatalf("expected SeverityWarning, got %v", warnings[0].Severity)
	}
}

// TestValidateWarningsAllowsRecognizedAttrName proves a correctly spelled
// primitive produces no warning.
func TestValidateWarningsAllowsRecognizedAttrName(t *testing.T) {
	source := []byte(`package main

func Page() Node {
	return <span data-gosx-heartbeat="/api/version" data-gosx-heartbeat-interval="4s"></span>
}
`)
	prog, err := parse(t, source)
	if err != nil {
		t.Fatalf("Lower failed: %v", err)
	}

	if warnings := ir.ValidateWarnings(prog); len(warnings) != 0 {
		t.Fatalf("expected no warnings for a recognized attribute, got %+v", warnings)
	}
}

func TestValidateWarningsRecognizesDisclosureAttrs(t *testing.T) {
	valid := []byte(`package main

func Page() Node {
	return <div>
		<button data-gosx-disclosure-target="#panel"></button>
		<div data-gosx-disclosure-backdrop="#panel"></div>
		<section id="panel" data-gosx-disclosure data-gosx-disclosure-modal>
			<button data-gosx-disclosure-close="#panel" data-gosx-disclosure-initial-focus></button>
		</section>
	</div>
}
`)
	prog, err := parse(t, valid)
	if err != nil {
		t.Fatal(err)
	}
	if warnings := ir.ValidateWarnings(prog); len(warnings) != 0 {
		t.Fatalf("valid disclosure attrs produced warnings: %+v", warnings)
	}
}

// TestValidateWarningsIgnoresUnrelatedDataGosxAttr proves an attribute
// belonging to an entirely different subsystem's own contract (here,
// scene3d) is out of scope -- it is not close to any navigation primitive,
// so it must not be flagged as a typo of one.
func TestValidateWarningsIgnoresUnrelatedDataGosxAttr(t *testing.T) {
	source := []byte(`package main

func Page() Node {
	return <canvas data-gosx-scene3d-poster="/poster.png"></canvas>
}
`)
	prog, err := parse(t, source)
	if err != nil {
		t.Fatalf("Lower failed: %v", err)
	}

	if warnings := ir.ValidateWarnings(prog); len(warnings) != 0 {
		t.Fatalf("expected no warnings for an unrelated data-gosx- attribute, got %+v", warnings)
	}
}

// TestValidateWarningsIgnoresMotionAttr is a regression test for a real
// false positive gosx#249 caught running this check across this
// repository's own examples: "data-gosx-motion" (the motion package's own
// attribute) sits at Levenshtein distance 2 from "data-gosx-action" --
// close enough that an earlier, looser threshold flagged it as a likely
// typo of an unrelated primitive.
func TestValidateWarningsIgnoresMotionAttr(t *testing.T) {
	source := []byte(`package main

func Page() Node {
	return <div data-gosx-motion="fade-in"></div>
}
`)
	prog, err := parse(t, source)
	if err != nil {
		t.Fatalf("Lower failed: %v", err)
	}
	if warnings := ir.ValidateWarnings(prog); len(warnings) != 0 {
		t.Fatalf("expected no warnings for data-gosx-motion, got %+v", warnings)
	}
}

// TestValidateWarningsNeverReturnsAnError proves ValidateWarnings and
// Validate are true siblings: a warning-only finding must never also
// appear from Validate, and vice versa (see Severity's doc comment).
func TestValidateWarningsNeverReturnsAnError(t *testing.T) {
	source := []byte(`package main

func Page() Node {
	return <span data-gosx-hearbeat="/api/version"></span>
}
`)
	prog, err := parse(t, source)
	if err != nil {
		t.Fatalf("Lower failed: %v", err)
	}

	if diags := ir.Validate(prog); len(diags) != 0 {
		t.Fatalf("expected Validate to find nothing for a warning-only finding, got %+v", diags)
	}
	for _, w := range ir.ValidateWarnings(prog) {
		if w.Severity != ir.SeverityWarning {
			t.Fatalf("expected every ValidateWarnings result to be SeverityWarning, got %+v", w)
		}
	}
}

// --- Value-shape checks (error severity, gosx#249 check 5) ---------------

func TestValidateRejectsInvalidHeartbeatInterval(t *testing.T) {
	source := []byte(`package main

func Page() Node {
	return <body data-gosx-heartbeat="/api/version" data-gosx-heartbeat-interval="4 seconds"></body>
}
`)
	prog, err := parse(t, source)
	if err != nil {
		t.Fatalf("Lower failed: %v", err)
	}
	diags := ir.Validate(prog)
	if len(diags) != 1 {
		t.Fatalf("expected exactly one diagnostic, got %+v", diags)
	}
	want := `invalid data-gosx-heartbeat-interval value "4 seconds": must be a whole number of seconds or minutes`
	if diags[0].Message != want {
		t.Fatalf("unexpected diagnostic message: got %q, want %q", diags[0].Message, want)
	}
}

func TestValidateAllowsValidHeartbeatInterval(t *testing.T) {
	source := []byte(`package main

func Page() Node {
	return <body data-gosx-heartbeat="/api/version" data-gosx-heartbeat-interval="4s"></body>
}
`)
	prog, err := parse(t, source)
	if err != nil {
		t.Fatalf("Lower failed: %v", err)
	}
	if diags := ir.Validate(prog); len(diags) != 0 {
		t.Fatalf("expected no diagnostics for a valid heartbeat interval, got %+v", diags)
	}
}

func TestValidateRejectsInvalidHeartbeatHiddenInterval(t *testing.T) {
	source := []byte(`package main

func Page() Node {
	return <body data-gosx-heartbeat="/api/version" data-gosx-heartbeat-hidden-interval="1 minute"></body>
}
`)
	prog, err := parse(t, source)
	if err != nil {
		t.Fatalf("Lower failed: %v", err)
	}
	diags := ir.Validate(prog)
	if len(diags) != 1 {
		t.Fatalf("expected exactly one diagnostic, got %+v", diags)
	}
	want := `invalid data-gosx-heartbeat-hidden-interval value "1 minute": must be a whole number of seconds or minutes`
	if diags[0].Message != want {
		t.Fatalf("unexpected diagnostic message: got %q, want %q", diags[0].Message, want)
	}
}

func TestValidateAllowsValidHeartbeatHiddenInterval(t *testing.T) {
	source := []byte(`package main

func Page() Node {
	return <body data-gosx-heartbeat="/api/version" data-gosx-heartbeat-hidden-interval="60s"></body>
}
`)
	prog, err := parse(t, source)
	if err != nil {
		t.Fatalf("Lower failed: %v", err)
	}
	if diags := ir.Validate(prog); len(diags) != 0 {
		t.Fatalf("expected no diagnostics for a valid heartbeat hidden interval, got %+v", diags)
	}
}

func TestValidateRejectsInvalidLinkCurrentPolicy(t *testing.T) {
	source := []byte(`package main

func Page() Node {
	return <a href="/x" data-gosx-link data-gosx-link-current-policy="always"></a>
}
`)
	prog, err := parse(t, source)
	if err != nil {
		t.Fatalf("Lower failed: %v", err)
	}
	diags := ir.Validate(prog)
	if len(diags) != 1 {
		t.Fatalf("expected exactly one diagnostic, got %+v", diags)
	}
	want := `invalid data-gosx-link-current-policy value "always": must be "auto", "page", "ancestor", or "none"`
	if diags[0].Message != want {
		t.Fatalf("unexpected diagnostic message: got %q, want %q", diags[0].Message, want)
	}
}

func TestValidateRejectsInvalidPrefetchValue(t *testing.T) {
	source := []byte(`package main

func Page() Node {
	return <a href="/x" data-gosx-link data-gosx-prefetch="eager"></a>
}
`)
	prog, err := parse(t, source)
	if err != nil {
		t.Fatalf("Lower failed: %v", err)
	}
	diags := ir.Validate(prog)
	if len(diags) != 1 {
		t.Fatalf("expected exactly one diagnostic, got %+v", diags)
	}
	want := `invalid data-gosx-prefetch value "eager": must be "off", "intent", "render", or "force"`
	if diags[0].Message != want {
		t.Fatalf("unexpected diagnostic message: got %q, want %q", diags[0].Message, want)
	}
}

func TestValidateAllowsValidLinkAttrs(t *testing.T) {
	source := []byte(`package main

func Page() Node {
	return <a href="/x" data-gosx-link data-gosx-link-current-policy="ancestor" data-gosx-prefetch="intent"></a>
}
`)
	prog, err := parse(t, source)
	if err != nil {
		t.Fatalf("Lower failed: %v", err)
	}
	if diags := ir.Validate(prog); len(diags) != 0 {
		t.Fatalf("expected no diagnostics for valid link attributes, got %+v", diags)
	}
}
