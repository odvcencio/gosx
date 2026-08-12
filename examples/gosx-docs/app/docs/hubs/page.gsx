package docs

func Page() Node {
	return <article class="prose">
		<section class="doc-scene" aria-labelledby={docScene.HeadingID}>
			<div id={docScene.SurfaceID} class="doc-scene__surface">
				<Scene3D class="doc-scene__mount" {...docScene.Scene} respectReducedMotion={true}>
					<div class="doc-scene__fallback">{docScene.Scene.UnsupportedMessage}</div>
				</Scene3D>
			</div>
			<div class="doc-scene__teaching">
				<p class="doc-scene__eyebrow">{docScene.Eyebrow}</p>
				<p id={docScene.HeadingID} class="doc-scene__title" role="heading" aria-level="2">{docScene.Title}</p>
				<p class="doc-scene__summary">{docScene.Summary}</p>
				<dl class="doc-scene__facts">
					<div>
						<dt>Backend contract</dt>
						<dd>{docScene.BackendTruth}</dd>
					</div>
					<div>
						<dt>Interaction</dt>
						<dd>{docScene.InteractionHint}</dd>
					</div>
				</dl>
				<a href={docScene.DemoHref} data-gosx-link="true" class="doc-scene__link">{docScene.DemoLabel}</a>
			</div>
		</section>
		<div class="page-topper">
			<span class="eyebrow">Long-lived coordination</span>
			<p class="lede">
				A hub is an HTTP WebSocket handler with named event callbacks, presence, shared process state, selective fanout, and optional binary CRDT synchronization.
			</p>
		</div>
		<h1 id="hub-model">Hub model</h1>
		<CodeBlock lang="go" source={data.hubSample} />
		<p>
			<span class="inline-code">hub.New</span>
			creates an in-process coordinator. Mount the hub itself as an
			<span class="inline-code">http.Handler</span>
			. A handler receives
			<span class="inline-code">Client</span>
			,
			<span class="inline-code">Hub</span>
			,
			<span class="inline-code">Event</span>
			, and raw JSON
			<span class="inline-code">Data</span>
			.
		</p>
		<h2 id="events">Events and presence</h2>
		<p>
			Text frames use event and data fields. GoSX sends
			<span class="inline-code">__welcome</span>
			with the assigned client ID. Register
			<span class="inline-code">join</span>
			and
			<span class="inline-code">leave</span>
			handlers for lifecycle work.
		</p>
		<CodeBlock lang="go" source={data.lifecycleSample} />
		<p>
			<span class="inline-code">Presence().List()</span>
			and
			<span class="inline-code">Presence().Count()</span>
			describe connected clients.
			<span class="inline-code">GetState</span>
			and
			<span class="inline-code">SetState</span>
			provide values that are safe to access concurrently. The values remain in process memory and are not durable storage.
		</p>
		<h2 id="delivery">Delivery behavior</h2>
		<ul>
			<li>
				<span class="inline-code">Broadcast</span>
				sends to every connected client.
			</li>
			<li>
				<span class="inline-code">BroadcastWhere</span>
				filters by immutable server-side connection metadata.
			</li>
			<li>
				<span class="inline-code">Send</span>
				targets one client ID.
			</li>
			<li>
				<span class="inline-code">Latch</span>
				remembers the last value of a fixed topic in memory and replays it to later clients.
			</li>
		</ul>
		<p>
			Fanout never blocks on a slow client. A full 256-slot client buffer drops that message. Monitor
			<span class="inline-code">DropStats</span>
			and client drop statistics. A dropped text event is not replayed automatically.
		</p>
		<h2 id="documents">CRDT documents</h2>
		<CodeBlock lang="go" source={data.crdtSample} />
		<p>
			A document starts with the
			<span class="inline-code">crdt.Root</span>
			map. Mutations such as
			<span class="inline-code">Put</span>
			,
			<span class="inline-code">Delete</span>
			,
			<span class="inline-code">MakeMap</span>
			,
			<span class="inline-code">MakeList</span>
			, and
			<span class="inline-code">InsertAt</span>
			accumulate pending operations. Call
			<span class="inline-code">Commit</span>
			often. Only committed changes replicate and fire
			<span class="inline-code">OnChange</span>
			hooks.
		</p>
		<p>
			<span class="inline-code">Get</span>
			returns a
			<span class="inline-code">crdt.Value</span>
			and object ID. Read the field matching its kind, or call
			<span class="inline-code">ToAny</span>
			. Merge complete replicas with
			<span class="inline-code">doc.Merge(other)</span>
			.
		</p>
		<h2 id="sync">Synchronization</h2>
		<CodeBlock lang="go" source={data.manualSyncSample} />
		<p>
			Each peer pair keeps its own
			<span class="inline-code">crdt/sync.State</span>
			. Exchange generated messages in both directions until neither side has a message.
		</p>
		<CodeBlock lang="go" source={data.hubSyncSample} />
		<p>
			<span class="inline-code">SyncDoc</span>
			registers a document on the hub binary protocol and triggers synchronization after commits. Register documents and authorization gates before accepting connections.
		</p>
		<h2 id="security">Origin and authorization boundaries</h2>
		<p>
			WebSocket upgrades reject cross-origin requests by default and accept an absent origin or the request own host. Override
			<span class="inline-code">hub.SetCheckOrigin</span>
			only with an explicit trusted-origin policy.
		</p>
		<p>
			Without binary authorizers, every connected client may send changes for every synced document and receive every document.
			<span class="inline-code">SetBinaryAuthorizer</span>
			gates inbound document writes.
			<span class="inline-code">SetBinaryReadAuthorizer</span>
			gates server-to-client bootstrap and updates.
			<span class="inline-code">SetBinaryChangeAuthorizer</span>
			can validate concrete changes before merge.
		</p>
		<p>
			Use
			<span class="inline-code">ServeHTTPWithMetadata</span>
			to attach authenticated, immutable connection metadata at upgrade time. Do not trust identity supplied inside an event payload.
		</p>
	</article>
}
