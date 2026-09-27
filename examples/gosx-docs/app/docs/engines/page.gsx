package docs

func Page() Node {
	return <article class="prose">
		<section class="docs-live-example engine-live" aria-labelledby="engine-live-heading">
			<p class="eyebrow">Working Go example</p>
			<h2 id="engine-live-heading">Validate a worker engine</h2>
			<p>
				The route loader builds this descriptor with Go, validates it, and renders the accepted values here. A worker has no DOM mount.
			</p>
			<dl class="engine-live__facts">
				<div>
					<dt>Name</dt>
					<dd>{data.engineExample.name}</dd>
				</div>
				<div>
					<dt>Kind</dt>
					<dd>{data.engineExample.kind}</dd>
				</div>
				<div>
					<dt>Runtime</dt>
					<dd>{data.engineExample.runtime}</dd>
				</div>
				<div>
					<dt>Capabilities</dt>
					<dd>{data.engineExample.capabilities}</dd>
				</div>
				<div>
					<dt>Validation</dt>
					<dd role="status">{data.engineExample.status}</dd>
				</div>
			</dl>
			{CodeBlock("go", data.engineSample)}
		</section>
		<div class="page-topper">
			<span class="eyebrow">Unrestricted client computation</span>
			<p class="lede">
				Engines mount browser work that does not fit the constrained island VM: background workers, owned surfaces, managed video, and dedicated Go/WASM modules.
			</p>
		</div>
		<h2 id="engine-model">Engine model</h2>
		<p>
			An engine instance is described by
			<span class="inline-code">engine.Config</span>
			. Its
			<span class="inline-code">Kind</span>
			is
			<span class="inline-code">worker</span>
			,
			<span class="inline-code">surface</span>
			, or
			<span class="inline-code">video</span>
			. Workers have no DOM mount; surface and video engines require a stable
			<span class="inline-code">MountID</span>
			.
		</p>
		<p>
			This programmatic
			<span class="inline-code">engine.Config</span>
			path is the v1 engine surface. Strict engine declarations and automatic
			<span class="inline-code">.gsx //gosx:engine</span>
			discovery remain preview/post-v1.
		</p>
		<p>
			Engines own only their declared mount and communicate through props and events. They are separate from island DOM state and from the shared island VM.
		</p>
		<h2 id="mounting">Mounting an engine</h2>
		<CodeBlock lang="go" source={data.mountSample} />
		<p>
			Call
			<span class="inline-code">ctx.Engine</span>
			,
			<span class="inline-code">ctx.Runtime().Engine</span>
			, or the equivalent page-state helper while rendering a request. The fallback is server HTML shown when the runtime cannot mount the configured engine.
		</p>
		<p>
			<span class="inline-code">Props</span>
			is JSON. Validate marshal errors before mounting.
			<span class="inline-code">MountAttrs</span>
			is applied only to the server-rendered mount and is not serialized into the engine manifest.
		</p>
		<h2 id="capabilities">Capabilities and hard requirements</h2>
		<p>
			<span class="inline-code">Capabilities</span>
			describes APIs the instance can use.
			<span class="inline-code">RequiredCapabilities</span>
			is the hard gate: if a requirement is absent, GoSX reports an unsupported runtime issue instead of silently selecting a weaker behavior.
		</p>
		<CodeBlock lang="go" source={data.webgpuSample} />
		<p>
			Common constants cover canvas, WebGL, WebGL2, WebGPU, animation, storage, fetch, audio, workers, keyboard, pointer, gamepad, and pixel surfaces. WebGPU helpers encode optional features and minimum adapter or device limits.
		</p>
		<section class="callout">
			<strong>Fallback is an application decision</strong>
			<p>
				If WebGL2 is a valid implementation, declare it in the engine design and provide that path. Do not put WebGPU in
				<span class="inline-code">RequiredCapabilities</span>
				and then describe an automatic WebGL fallback.
			</p>
		</section>
		<h2 id="runtimes">Runtime modes</h2>
		<ul>
			<li>
				<span class="inline-code">RuntimeNone</span>
				uses a factory already registered with the browser bootstrap.
			</li>
			<li>
				<span class="inline-code">RuntimeShared</span>
				selects a framework-managed shared engine program.
			</li>
			<li>
				<span class="inline-code">RuntimeGoWASM</span>
				loads a standard Go WebAssembly module from
				<span class="inline-code">WASMPath</span>
				.
			</li>
		</ul>
		<CodeBlock lang="go" source={data.wasmConfigSample} />
		<p>
			<span class="inline-code">Config.Validate</span>
			checks kind, mount requirements, runtime requirements, and capability syntax. A Go/WASM runtime without a WASM path is invalid.
		</p>
		<h2 id="go-wasm">Dedicated Go/WASM modules</h2>
		<CodeBlock lang="go" source={data.wasmModuleSample} />
		<p>
			The module calls
			<span class="inline-code">wasm.Register</span>
			during synchronous startup. A factory receives
			<span class="inline-code">wasm.Context</span>
			with mount, props, instance identity, kind, runtime, and capability access. Use
			<span class="inline-code">DecodeProps</span>
			for typed JSON and
			<span class="inline-code">Emit</span>
			for framework events.
		</p>
		<h2 id="lifecycle">Lifecycle</h2>
		<p>
			One exact module URL boots once per document, while its factories may create multiple independent instances. Each factory returns a
			<span class="inline-code">wasm.Handle</span>
			; GoSX calls
			<span class="inline-code">Dispose</span>
			once when that instance is replaced or its page is disposed.
		</p>
		<p>
			Keep the module alive while instances are in use and release event listeners, animation loops, workers, and GPU resources from the handle.
		</p>
	</article>
}
