package docs

func Page() Node {
	return <div class="prose getting-started">
		<section class="quickstart" aria-labelledby="quickstart-heading">
			<div class="quickstart__grid">
				<div class="quickstart__commands">
					<h2 id="quickstart-heading">Start in three commands</h2>
					{CodeBlock("bash", data.sample001)}
				</div>
				<figure class="quickstart__preview">
					<img
						src="/docs/quickstart-app.jpg"
						alt="The scaffolded GoSX app with a welcome page and starter form."
						width={1440}
						height={900}
					></img>
					<figcaption>
						After it starts, open
						<code>http://localhost:8080</code>
						.
					</figcaption>
				</figure>
			</div>
			<p class="quickstart__tutorial">
				The starter app is running. Continue with
				<a href="/docs/your-first-app" data-gosx-link="true">Your first GoSX app</a>
				to add server data, a counter island, a live hub, and a Scene3D.
			</p>
		</section>
		<section id="prerequisites" class="docs-section-block">
			<h2>Prerequisites</h2>
			<p>
				Install Go 1.26 or newer. GoSX declares Go 1.26 in its module file. An older toolchain may download a newer version or stop before the app starts.
			</p>
		</section>
		<section id="timing" class="docs-section-block">
			<h2>How long it took</h2>
			<p>
				On 2026-09-26, a cold run with an empty module cache on Linux x86_64 (Intel Core Ultra 9 285, 20 CPUs, 19 GiB RAM; Go 1.26.4) reached its first HTTP 200 in 75 seconds: 42 seconds to install the CLI, 7 seconds to scaffold the app, and 27 seconds to start it. It downloaded about 74 MB of modules and 137 MB of Go toolchains.
			</p>
		</section>
		<section id="troubleshooting" class="docs-section-block">
			<h2>Troubleshooting</h2>
			<h3>Go is too old</h3>
			<p>
				Check
				<code>go version</code>
				. Install Go 1.26 or newer, then rerun
				<code>
					go install m31labs.dev/gosx/cmd/gosx@latest
				</code>
				.
			</p>
			<h3>Port 8080 is already in use</h3>
			<p>
				Start the app on a free port with
				<code>PORT=8116 go run .</code>
				, then open
				<code>http://localhost:8116</code>
				.
			</p>
		</section>
		<section id="project-files" class="docs-section-block">
			<h2>Where the files live</h2>
			<p>
				Page templates are in
				<code>app/</code>
				. A page can pair
				<code>page.gsx</code>
				markup with
				<code>page.server.go</code>
				data and actions. Shared public files live in
				<code>public/</code>
				.
			</p>
			{CodeBlock("text", data.sample002)}
			<p>
				Use
				<code>gosx dev</code>
				while editing. It watches the project and refreshes connected browser tabs after a successful rebuild.
			</p>
			{CodeBlock("bash", data.sample003)}
		</section>
	</div>
}
