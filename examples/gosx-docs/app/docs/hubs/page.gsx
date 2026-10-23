package docs

func Page() Node {
	return <article class="prose">
		<section class="docs-live-example" aria-label="Live hub presence example">
			<p class="eyebrow">Live hub presence</p>
			<div
				data-gosx-region="true"
				data-gosx-region-url="/api/docs-hubs/open-tabs"
				data-gosx-region-field="html"
				data-gosx-region-signal="$docs.guidePresence"
			>
				<h2>
					Open guide tabs:
					{data.openTabs}
				</h2>
			</div>
			<p>
				Open this guide in another tab. The hub sends join and leave counts to a shared signal, so the example updates without refreshing the page.
			</p>
			<a
				href="https://github.com/odvcencio/gosx/blob/main/examples/gosx-docs/app/docs/hubs/hub.go"
				rel="noopener"
			>View the hub handler</a>
			<a
				href="https://github.com/odvcencio/gosx/blob/main/examples/gosx-docs/app/docs/hubs/page.server.go"
				rel="noopener"
			>View the page binding</a>
		</section>
		<div class="page-topper">
			<span class="eyebrow">Long-lived coordination</span>
			<p class="lede">
				A hub is an HTTP WebSocket handler with named event callbacks, presence, shared process state, selective fanout, and optional binary CRDT synchronization.
			</p>
		</div>
		<h2 id="hub-model">Hub model</h2>
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
		<p>
			The browser runtime's own hub client reconnects automatically after an unexpected close: the delay grows from 500 ms, doubling per attempt with up to 25 percent jitter, up to a 15 second cap, and resets to 500 ms the moment a reconnect actually opens. It reports four connection states: connecting, open, reconnecting and error (see Connection state and resync below).
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
		<h2 id="css-vars">CSS custom properties from hub payloads</h2>
		<p>
			<span class="inline-code">HubBinding.CSSVars</span>
			writes fields of a hub payload to CSS custom properties, so a page can follow live state without page JavaScript. Each variable maps a payload path to a name that must start with
			<span class="inline-code">--</span>
			. The target is the document element unless
			<span class="inline-code">Target</span>
			holds a selector. A flash class marks a changed apply and a live class marks the first one. The browser skips the flash class under reduced motion, in a hidden tab, or when the page URL carries a
			<span class="inline-code">__gosx_visual_seed</span>
			query parameter. It never removes a property, and it skips values that are not a string, a finite number or a boolean. An invalid mapping is dropped when the manifest is built and reported by
			<span class="inline-code">Manifest.Validate()</span>
			. Values are checked in the browser before they are written. By default a value may not contain parentheses, backslashes, quotes, semicolons or control characters, and it may not exceed 256 characters, so a payload cannot build a
			<span class="inline-code">url()</span>
			for a stylesheet that reads the variable. Set
			<span class="inline-code">Kind</span>
			on a variable to a stricter grammar: number, length, color, ident or string (written as a quoted CSS string). The browser also re-checks the
			<span class="inline-code">--</span>
			prefix. The code loads in its own chunk, only on pages with a
			<span class="inline-code">CSSVars</span>
			binding.
		</p>
		<CodeBlock lang="go" source={data.cssVarsSample} />
		<p>
			<span class="inline-code">hub.Broadcast</span>
			encodes a
			<span class="inline-code">[]byte</span>
			field as a base64 string. Wrap pre-encoded JSON in
			<span class="inline-code">json.RawMessage</span>
			so the browser receives an object.
		</p>
		<h2 id="lifecycle">Connection state and resync</h2>
		<p>
			The browser reports the connection state as connecting, open, reconnecting or error. Set
			<span class="inline-code">ConnectionSignal</span>
			through
			<span class="inline-code">SetHubLifecycle</span>
			and the shared signal receives an object with
			<span class="inline-code">state</span>
			,
			<span class="inline-code">attempt</span>
			and
			<span class="inline-code">at</span>
			(milliseconds). For status text without a signal, mark any element with
			<span class="inline-code">data-gosx-hub-status</span>
			set to the hub name. The runtime then sets
			<span class="inline-code">data-gosx-hub-connection</span>
			on it, and CSS can select on the value.
		</p>
		<p>
			<span class="inline-code">HubResyncConfig</span>
			makes the browser send a
			<span class="inline-code">resync</span>
			event when the tab becomes visible, the window gains focus, the network returns, the page leaves the back-forward cache, after the socket opens, and on an optional wall-clock schedule. It sends at most one request per
			<span class="inline-code">MinIntervalMS</span>
			(1.2 s by default). Register a handler with
			<span class="inline-code">hub.On("resync", ...)</span>
			; hubs that use
			<span class="inline-code">Latch</span>
			can answer with
			<span class="inline-code">ReplayLatched</span>
			. Scheduled resyncs add random jitter of up to 10 percent of the period (at most 5 s), so clients do not all ask at once. Only the first hub entry that shares a socket supplies the resync settings. 
			<span class="inline-code">ReplayLatched</span>
			answers one client at most once per second, because any browser can send
			<span class="inline-code">resync</span>
			as fast as its connection allows and
			<span class="inline-code">MaxMessagesPerSecond</span>
			defaults to unlimited. Set it on hubs that accept resync from the public.
		</p>
		<CodeBlock lang="go" source={data.resyncSample} />
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
