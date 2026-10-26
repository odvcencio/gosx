package app

import "m31labs.dev/gosx/signal"

// Chart creates whole <svg> elements after hydration: each press adds one.
//
//gosx:island
func Chart() Node {
	shapes := signal.New([]string{})
	add := func() { shapes.Set(append(shapes.Get(), "s")) }
	return <section id="chart">
		<button id="chart-add" onClick={add}>Add chart</button>
		<div id="chart-host">
			<Each of={shapes.Get()} as="shape">
				<svg class="spark" width="120" height="60" viewBox="0 0 120 60" preserveAspectRatio="xMidYMid meet">
					<g>
						<polyline points="0,50 30,20 60,35 90,10 120,25" fill="none" stroke="#c0392b" stroke-width="3"></polyline>
						<path d="M0 55 L120 55" stroke="#2c3e50" stroke-width="2"></path>
					</g>
				</svg>
			</Each>
		</div>
	</section>
}
