package checkers

func Page() Node {
	return <main class="checkers-showcase chinese-checkers" aria-labelledby="checkers-title">
		<div class="checkers-showcase__scene" aria-label="Three-dimensional Chinese Checkers board scaffold">
			<Scene3D {...data.scene} />
		</div>
		<section class="checkers-showcase__intro" data-checkers-root>
			<p class="checkers-showcase__eyebrow">
				<span>Live</span>
				· GoSX Scene3D · authoritative Go Hub
			</p>
			<h1 id="checkers-title">Chinese Checkers</h1>
			<p>
				Select a piece, then a highlighted destination. Go validates and commits every move.
			</p>
			<dl class="checkers-showcase__facts">
				<div>
					<dt>Board</dt>
					<dd>121 instanced sockets</dd>
				</div>
				<div>
					<dt>Match</dt>
					<dd>
						2 × 10 active pieces · six-seat topology
					</dd>
				</div>
				<div>
					<dt>Turn</dt>
					<dd id="checkers-turn">connecting…</dd>
				</div>
			</dl>
			<p class="checkers-showcase__status" id="checkers-status" role="status">Connecting to the GoSX Hub…</p>
			<div class="checkers-showcase__controls" aria-label="Game controls">
				<label class="checkers-showcase__material">
					<span>Table material</span>
					<select id="checkers-material" name="material">
						<option value="imperial-jade" selected={data.material == "imperial-jade"}>Imperial Jade</option>
						<option value="carved-wood" selected={data.material == "carved-wood"}>Carved Wood</option>
						<option value="brushed-steel" selected={data.material == "brushed-steel"}>Brushed Steel</option>
					</select>
				</label>
				<label class="checkers-showcase__material">
					<span>CPU personality</span>
					<select id="checkers-personality">
						<option value="jade-crane">Jade Crane</option>
						<option value="iron-fox">Iron Fox</option>
						<option value="cedar-turtle">Cedar Turtle</option>
					</select>
				</label>
				<label class="checkers-showcase__material">
					<span>Difficulty</span>
					<select id="checkers-difficulty">
						<option value="friendly">Friendly</option>
						<option value="club" selected>Club</option>
						<option value="expert">Expert</option>
						<option value="grandmaster">Grandmaster</option>
					</select>
				</label>
				<button type="button" id="checkers-undo" disabled>Undo</button>
				<button type="button" id="checkers-restart">Restart</button>
			</div>
			<dl class="checkers-showcase__search" aria-label="CPU search telemetry">
				<div>
					<dt>Depth</dt>
					<dd id="checkers-search-depth">—</dd>
				</div>
				<div>
					<dt>Nodes</dt>
					<dd id="checkers-search-nodes">—</dd>
				</div>
				<div>
					<dt>Think</dt>
					<dd id="checkers-search-time">—</dd>
				</div>
				<div>
					<dt>TT hits</dt>
					<dd id="checkers-search-cache">—</dd>
				</div>
			</dl>
		</section>
		<p class="checkers-showcase__policy" id="checkers-policy">CPU policy: safe fallback loading…</p>
		<details class="checkers-showcase__board-panel" open>
			<summary>Keyboard board · 121 holes</summary>
			<p id="checkers-board-help">
				Use arrow keys to move between neighboring holes. Press Enter or Space to select and move.
			</p>
			<div
				class="checkers-showcase__board"
				id="checkers-board"
				role="grid"
				aria-label="Chinese Checkers board"
				aria-describedby="checkers-board-help"
			>
				<Each of={data.holes} as="hole">
					<button
						type="button"
						data-checkers-hole={hole.ID}
						data-owner={hole.Owner}
						data-x={hole.X}
						data-y={hole.Y}
						data-z={hole.Z}
						aria-label={hole.Label}
						aria-pressed="false"
						role="gridcell"
						tabindex={hole.ID == 0 ? 0 : -1}
					>{hole.ID}</button>
				</Each>
			</div>
		</details>
		<p class="checkers-showcase__limitations">
			Demo limitations: this is an in-memory two-player Hub match, not product multiplayer. The CPU uses a bounded Arbiter policy fallback when no evaluator is linked; Elio GPU hints remain optional and inactive.
		</p>
		<noscript>
			<p class="checkers-showcase__noscript">
				The semantic board summary remains available without JavaScript; the 3D renderer requires the GoSX client runtime.
			</p>
		</noscript>
		<script src="/checkers-client.js" defer></script>
	</main>
}
