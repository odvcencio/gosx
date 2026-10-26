package app

import "m31labs.dev/gosx/signal"

//gosx:island
func ForeignContent() Node {
	visible := signal.New(false)
	toggle := func() { visible.Set(!visible.Get()) }
	return <section id="foreign-content">
		<button id="foreign-toggle" onClick={toggle}>Toggle equation</button>
		<If cond={visible.Get()}>
			<math id="equation">
				<semantics>
					<mrow><mi>x</mi><mo>=</mo><mn>2</mn></mrow>
					<annotation-xml encoding="Text/HTML"><span id="equation-note">An annotated equation</span></annotation-xml>
				</semantics>
				<mtext><mglyph id="equation-glyph" alt="glyph"></mglyph><malignmark id="equation-mark"></malignmark><span id="equation-text">inline text</span></mtext>
			</math>
			<svg width="100" height="60" viewBox="0 0 100 60" id="foreign-vector">
				<rect x="5" y="5" width="90" height="50" fill="#168b7d"></rect>
				<foreignObject x="10" y="10" width="80" height="40"><div id="vector-note">HTML in SVG</div></foreignObject>
			</svg>
		</If>
	</section>
}
