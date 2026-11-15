package rooms

func Page() Node {
	return <section class="rooms" aria-labelledby="rooms-title">
		<header class="rooms__header">
			<div>
				<p class="rooms__eyebrow">A server-authoritative room</p>
				<h1 id="rooms-title">Rooms</h1>
				<p>Steer a body against a bot. The server runs the match; your browser predicts it.</p>
			</div>
			<span class="rooms__badge">30 second round</span>
		</header>
		<div class="rooms__frame">
			<iframe
				class="rooms__client"
				title="Rooms game client"
				src={data.clientSrc}
				loading="lazy"
				referrerpolicy="same-origin"
			></iframe>
			<aside class="rooms__panel" aria-label="How a round works">
				<h2>One round</h2>
				<ol class="rooms__phases">
					<li><strong>Lobby</strong><span>You join a room. Play starts at once against a local bot.</span></li>
					<li><strong>Countdown</strong><span>The room counts down three seconds.</span></li>
					<li><strong>Run</strong><span>A bot takes the empty seat and chases your body. Wall bounces are counted.</span></li>
					<li><strong>Results</strong><span>The server sends each seat's bounce count, then closes the room.</span></li>
				</ol>
				<h2>Controls</h2>
				<p class="rooms__keys"><kbd>←</kbd> <kbd>↑</kbd> <kbd>↓</kbd> <kbd>→</kbd> or <kbd>W</kbd> <kbd>A</kbd> <kbd>S</kbd> <kbd>D</kbd> to move, <kbd>Shift</kbd> to boost.</p>
			</aside>
		</div>
		<div class="rooms__footer">
			<p>
				Drop the connection and the client redials with its resume token and keeps your seat. If the server stops answering, the same match continues offline against bots.
			</p>
			<p>
				Rooms are in memory, capped, and closed after a short idle time. Open a second tab to take over the bot's seat.
			</p>
		</div>
	</section>
}
