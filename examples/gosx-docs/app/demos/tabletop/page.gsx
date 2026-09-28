package docs

import (
	"m31labs.dev/gosx/browser"
	"m31labs.dev/gosx/signal"
)

func Page() Node {
	return <div class="tabletop" data-gosx-scene3d-status-scope>
		<div class="tabletop__inner">
			<header class="tabletop__header">
				<div>
					<a class="tabletop__back" href="/demos" data-gosx-link="true">All demos</a>
					<h1 id="tabletop-title">A room you can shape.</h1>
					<p>
						On supported GPUs, the browser renders the scene live. Object placement, rotation, removal, lighting, and visitor count sync to everyone in the room after the scene is ready. Reduced motion or an unsupported GPU keeps the server-rendered poster.
					</p>
				</div>
				<div
					class="tabletop__presence"
					aria-live="polite"
					data-gosx-live-on="room:ui"
					data-gosx-live-mode="event"
				>
					<strong>
						<span data-gosx-live-bind="visitors">{data.visitors}</span>
						here
					</strong>
					<span>
						Room
						{data.roomID}
					</span>
				</div>
			</header>
			<div class="tabletop__workspace">
				<section class="tabletop__scene-panel" aria-label="Shared tabletop scene">
					<div class="tabletop__stage">
						<img
							class="tabletop__poster"
							src={data.posterURL}
							alt="The tabletop before this room's edits"
							width="1100"
							height="760"
							fetchpriority="high"
							decoding="async"
						 />
						<div class="tabletop__poster-copy" aria-hidden="true">
							<span class="tabletop__poster-eyebrow">A shared tabletop</span>
							<strong>A small room, ready for company.</strong>
							<span>The live scene takes over after this preview paints.</span>
						</div>
						<Scene3D
							id="tabletop-scene"
							{...data.scene}
							startPolicy="idle-visible-hardware"
							maxFrameRate={30}
							respectReducedMotion={true}
							data-gosx-scene3d-reveal-class="tabletop-scene-ready"
						>
							<p class="tabletop__fallback">{data.scene.UnsupportedMessage}</p>
						</Scene3D>
					</div>
					<p class="tabletop__hint">
						When the live scene starts, tap an open spot to place an object. Select one to rotate or remove it.
					</p>
					<div
						class="tabletop__scene-facts"
						aria-live="polite"
						data-gosx-live-on="room:ui"
						data-gosx-live-mode="event"
					>
						<p>
							<span>Objects</span>
							<strong data-gosx-live-bind="objects">{data.objects}</strong>
						</p>
						<p>
							<span>Shared document</span>
							<strong>
								<span data-gosx-live-bind="documentBytes">{data.documentBytes}</span>
								bytes
							</strong>
						</p>
						<p>
							<span>Rendering</span>
							<strong>
								<output data-gosx-scene3d-status="renderer">starting…</output>
							</strong>
						</p>
						<output data-gosx-scene3d-status="fallback" hidden></output>
					</div>
				</section>
				<aside class="tabletop__rail" aria-label="Room controls">
					<TabletopControls shareURL={data.shareURL} />
					<section class="tabletop__metrics" aria-label="Live measurements">
						<h2>Live measurements</h2>
						<dl>
							<div>
								<dt>Frame p95</dt>
								<dd>
									<output data-gosx-scene3d-status="frame-p95">measuring…</output>
								</dd>
							</div>
						</dl>
					</section>
					<details class="tabletop__build">
						<summary>How this is built</summary>
						<p>
							Go renders the page and room, validates edits, and syncs the scene with a CRDT document. Room state stays in server memory and expires after 30 idle minutes. There are no accounts, permanent saves, or freeform text editing.
						</p>
						<dl class="tabletop__receipts">
							<Each of={data.receipt.SourceFiles} as="source">
								<div>
									<dt>{source.Path}</dt>
									<dd>
										{source.Lines}
										lines
									</dd>
								</div>
							</Each>
						</dl>
						<p>
							JavaScript written for this demo:
							<strong>
								{data.receipt.JavaScriptLines}
								lines
							</strong>
						</p>
						<p class="tabletop__limit">
							Room state is in memory and disappears after 30 idle minutes. Each room accepts up to 64 objects and 8 visitors.
						</p>
					</details>
				</aside>
			</div>
		</div>
	</div>
}

//gosx:island
func TabletopControls(props TabletopControlsProps) Node {
	palette := signal.NewShared("$tabletop.palette", "ceramic")
	lighting := signal.NewShared("$tabletop.lighting", "softbox")
	selected := signal.NewShared("$tabletop.selected", "")
	rotate := signal.NewShared("$tabletop.rotate", 0)
	remove := signal.NewShared("$tabletop.remove", 0)
	rtt := signal.NewShared("$tabletop.rtt", -1.0)
	copied := signal.New("")
	rotateSelected := func() { rotate.Set(rotate.Get() + 1) }
	removeSelected := func() { remove.Set(remove.Get() + 1) }
	copyRoom := func() {
		copied.Set(browser.ClipboardWrite(props.ShareURL) ? "Room link copied" : "Clipboard unavailable. Copy the address bar link.")
	}
	return <div class="tabletop__controls">
		<section class="tabletop__control-group">
			<h2>Place an object</h2>
			<div class="tabletop__button-row" role="group" aria-label="Object palette">
				<button
					type="button"
					aria-pressed={palette.Get() == "ceramic"}
					data-on-click="palette.Set(\"ceramic\")"
				>Ceramic</button>
				<button type="button" aria-pressed={palette.Get() == "brass"} data-on-click="palette.Set(\"brass\")">Brass</button>
				<button type="button" aria-pressed={palette.Get() == "plant"} data-on-click="palette.Set(\"plant\")">Plant</button>
				<button type="button" aria-pressed={palette.Get() == "book"} data-on-click="palette.Set(\"book\")">Notebook</button>
				<button type="button" aria-pressed={palette.Get() == "candle"} data-on-click="palette.Set(\"candle\")">Candle</button>
				<button type="button" aria-pressed={palette.Get() == "orb"} data-on-click="palette.Set(\"orb\")">Sphere</button>
			</div>
		</section>
		<section class="tabletop__control-group">
			<h2>Lighting</h2>
			<div class="tabletop__button-row" role="group" aria-label="Lighting presets">
				<button
					type="button"
					aria-pressed={lighting.Get() == "softbox"}
					data-on-click="lighting.Set(\"softbox\")"
				>Soft studio</button>
				<button
					type="button"
					aria-pressed={lighting.Get() == "late-afternoon"}
					data-on-click="lighting.Set(\"late-afternoon\")"
				>Late afternoon</button>
				<button
					type="button"
					aria-pressed={lighting.Get() == "cool-studio"}
					data-on-click="lighting.Set(\"cool-studio\")"
				>Cool studio</button>
			</div>
		</section>
		<section class="tabletop__control-group">
			<h2>Selected object</h2>
			<div class="tabletop__button-row">
				<button type="button" disabled={selected.Get() == ""} onClick={rotateSelected}>Rotate 90°</button>
				<button type="button" disabled={selected.Get() == ""} onClick={removeSelected}>Remove</button>
			</div>
			<p class="tabletop__selection-hint">
				{selected.Get() == "" ? "Select a model in the scene first." : "Object selected. Changes are shared."}
			</p>
		</section>
		<section class="tabletop__control-group tabletop__share">
			<h2>Share this room</h2>
			<button type="button" class="tabletop__share-button" onClick={copyRoom}>Copy room link</button>
			<p role="status" aria-live="polite">{copied.Get()}</p>
		</section>
		<p class="tabletop__rtt-signal" aria-live="polite">
			Hub round trip:
			{rtt.Get() < 0 ? "measuring…" : rtt.Get() + " ms"}
		</p>
	</div>
}
