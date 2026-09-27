package docs

import docsapp "m31labs.dev/gosx/examples/gosx-docs/app"

func Page() Node {
	return <article class="prose first-app-tutorial">
		<p class="lede">
			Build one small app in four steps. Each step adds one GoSX capability and shows the complete file you will replace or add.
		</p>
		<p>
			Start from the project created by
			<code>gosx init my-app</code>
			, then run
			<code>cd my-app</code>
			. Keep the development server open in another terminal with
			<code>gosx dev .</code>
			.
		</p>
		<section class="tutorial-step" id="step-server-data">
			<p class="eyebrow">Step 1 · Server data</p>
			<h2>Render data from Go</h2>
			<p>
				The loader returns a typed value. GoSX renders it into ordinary HTML on the server.
			</p>
			<h3>
				<code>app/page.server.go</code>
			</h3>
			{CodeBlock("go", docsapp.DocSample("tutorial/step-01-page-server.go.sample"))}
			<h3>
				<code>app/page.gsx</code>
			</h3>
			{CodeBlock("gosx", docsapp.DocSample("tutorial/step-01-page.gsx.sample"))}
			<p class="tutorial-result">
				You should see a page headed “Hello from Go” with today’s visitor count.
			</p>
			<figure>
				<img
					src="/docs/tutorial/step-01.jpg"
					alt="The first app page showing Hello from Go and a visitor count."
					width="1000"
					height="620"
				></img>
				<figcaption>
					Step 1: Go data rendered in server HTML.
				</figcaption>
			</figure>
		</section>
		<section class="tutorial-step" id="step-island">
			<p class="eyebrow">Step 2 · Island</p>
			<h2>Add an interactive counter</h2>
			<p>
				Replace
				<code>app/page.gsx</code>
				. The counter is a strict island; its button updates without a page request.
			</p>
			<h3><code>app/counter_props.go</code></h3>
			{CodeBlock("go", docsapp.DocSample("tutorial/step-02-counter-props.go.sample"))}
			<h3><code>app/page.server.go</code></h3>
			{CodeBlock("go", docsapp.DocSample("tutorial/step-02-page-server.go.sample"))}
			<h3><code>app/page.gsx</code></h3>
			{CodeBlock("gosx", docsapp.DocSample("tutorial/step-02-page.gsx.sample"))}
			<p class="tutorial-result">
				You should see a counter that starts at zero. Select “Add one” to increment it.
			</p>
			<figure>
				<img
					src="/docs/tutorial/step-02.jpg"
					alt="The page-data example with an interactive counter displaying one."
					width="1000"
					height="620"
				></img>
				<figcaption>
					Step 2: the counter responds in the browser.
				</figcaption>
			</figure>
		</section>
		<section class="tutorial-step" id="step-hub">
			<p class="eyebrow">Step 3 · Hub</p>
			<h2>Show how many tabs are open</h2>
			<p>
				Add the hub handler, replace
				<code>main.go</code>
				to mount it, and bind its presence event in the page loader. The hub broadcasts a count when tabs join or leave. A shared signal updates the count in the browser without refreshing the page.
			</p>
			<h3>
				<code>app/tab_hub.go</code>
			</h3>
			{CodeBlock("go", docsapp.DocSample("tutorial/step-03-tab-hub.go.sample"))}
			<h3>
				<code>main.go</code>
			</h3>
			{CodeBlock("go", docsapp.DocSample("tutorial/step-03-main.go.sample"))}
			<h3>
				<code>app/page.server.go</code>
			</h3>
			{CodeBlock("go", docsapp.DocSample("tutorial/step-03-page-server.go.sample"))}
			<h3>
				<code>app/page.gsx</code>
			</h3>
			{CodeBlock("gosx", docsapp.DocSample("tutorial/step-03-page.gsx.sample"))}
			<p class="tutorial-result">
				Open this page in two tabs. Both should show the live number of connected tabs.
			</p>
			<figure>
				<img
					src="/docs/tutorial/step-03.jpg"
					alt="A page showing the live open-tab count after two tabs connect."
					width="1000"
					height="620"
				></img>
				<figcaption>
					Step 3: both tabs share the hub presence count.
				</figcaption>
			</figure>
		</section>
		<section class="tutorial-step" id="step-scene3d">
			<p class="eyebrow">Step 4 · Scene3D</p>
			<h2>Light one sphere in Go</h2>
			<p>
				Add a typed scene to the loader, then replace
				<code>app/page.gsx</code>
				. Scene3D builds one sphere, a camera, and a directional light from Go values.
			</p>
			<h3>
				<code>app/page.server.go</code>
			</h3>
			{CodeBlock("go", docsapp.DocSample("tutorial/step-04-page-server.go.sample"))}
			<h3>
				<code>app/page.gsx</code>
			</h3>
			{CodeBlock("gosx", docsapp.DocSample("tutorial/step-04-page.gsx.sample"))}
			<p class="tutorial-result">
				You should see the page data, the counter, the open-tab count, and a shaded gold sphere.
			</p>
			<figure>
				<img
					src="/docs/tutorial/step-04.jpg"
					alt="The final app with its counter, live tab count, and one lit gold sphere."
					width="1000"
					height="620"
				></img>
				<figcaption>
					Step 4: the app combines server data, an island, a hub, and Scene3D.
				</figcaption>
			</figure>
		</section>
	</article>
}
