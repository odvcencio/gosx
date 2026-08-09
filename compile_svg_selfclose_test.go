//go:build !tinygo

package gosx

import "testing"

// Reduced from m31labs.dev /build (gosx#139): a multi-line svg whose <path/>
// self-closes inside a nested section/div/form context failed to parse,
// while the same markup on one line, or with an explicit </path>, parsed
// fine. The failure was context-dependent: the button+svg alone compiled.
func TestCompileMultilineSelfClosingSVGPathInNestedContext(t *testing.T) {
	source := []byte(`package build

func Page() Node {
	return <div class="build-page">
		<section id="contact" class="build-contact">
			<div class="build-contact-panel glass-panel">
				<h2 class="chrome-text">Get in Touch</h2>
				<p class="build-contact-lead">Tell us.</p>
				<form method="POST" action="/x" class="contact-form">
					<button type="submit" class="services-cta">
						Send Message
						<svg width="16" height="16" viewBox="0 0 16 16" fill="none" aria-hidden="true">
							<path d="M3 8h10" stroke="currentColor"/>
						</svg>
					</button>
				</form>
			</div>
		</section>
	</div>
}
`)
	if _, err := Compile(source); err != nil {
		t.Fatalf("Compile failed: %v", err)
	}
}

// The single-line form of the same markup always parsed; keep it pinned so a
// fix for the multi-line case cannot trade one shape for the other.
func TestCompileSingleLineSelfClosingSVGPathInNestedContext(t *testing.T) {
	source := []byte(`package build

func Page() Node {
	return <div class="build-page">
		<section id="contact"><div class="panel"><form method="POST" action="/x">
			<button type="submit">Send<svg width="16" height="16"><path d="M3 8h10" stroke="currentColor"/></svg></button>
		</form></div></section>
	</div>
}
`)
	if _, err := Compile(source); err != nil {
		t.Fatalf("Compile failed: %v", err)
	}
}
