package checkers

func Page() Node {
	return <section class="checkers-showcase chinese-checkers" aria-labelledby="checkers-title" data-checkers-root>
		<header class="checkers-showcase__intro">
			<p class="checkers-showcase__eyebrow">
				Board / 01
				<span>Live match</span>
			</p>
			<h1 id="checkers-title">Chinese Checkers</h1>
			<p>
				Choose a piece, then move to a highlighted space. The Go Hub checks each move and plays the next turn.
			</p>
			<p class="checkers-showcase__status" id="checkers-status" role="status">Connecting to the match…</p>
		</header>
		<div class="checkers-showcase__scene" aria-label="Three-dimensional Chinese Checkers board scaffold">
			<img
				class="checkers-showcase__native-preview"
				src="/checkers-native-preview.png"
				alt=""
				width="960"
				height="600"
				decoding="async"
				fetchpriority="high"
			 />
			<Scene3D {...data.scene} />
			<p class="checkers-showcase__render-note">
				Pure-Go native preview ·
				<a href="/checkers-native-telemetry.json">inspect telemetry</a>
				· live Scene3D when available
			</p>
		</div>
		<section class="checkers-showcase__dashboard" aria-label="Match controls and live search statistics">
			<dl class="checkers-showcase__facts">
				<div>
					<dt>Board</dt>
					<dd>121 spaces</dd>
				</div>
				<div>
					<dt>Players</dt>
					<dd>You and the CPU</dd>
				</div>
				<div>
					<dt>Turn</dt>
					<dd id="checkers-turn">connecting…</dd>
				</div>
			</dl>
			<h2>Make it yours</h2>
			<div class="checkers-showcase__controls" aria-label="Game controls">
				<label class="checkers-showcase__material">
					<span>Table material</span>
					<select id="checkers-material" name="material">
						<option value="imperial-jade" selected={data.material == "imperial-jade"}>Imperial jade</option>
						<option value="carved-wood" selected={data.material == "carved-wood"}>Carved wood</option>
						<option value="brushed-steel" selected={data.material == "brushed-steel"}>Brushed steel</option>
						<option value="midnight-lacquer" selected={data.material == "midnight-lacquer"}>Midnight lacquer</option>
						<option value="moon-porcelain" selected={data.material == "moon-porcelain"}>Moon porcelain</option>
					</select>
				</label>
				<label class="checkers-showcase__material">
					<span>CPU personality</span>
					<select id="checkers-personality">
						<option value="jade-crane">Jade crane</option>
						<option value="iron-fox">Iron fox</option>
						<option value="cedar-turtle">Cedar turtle</option>
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
			<dl class="checkers-showcase__search" aria-label="CPU search statistics">
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
		<details class="checkers-showcase__board-panel">
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
		<details class="checkers-showcase__notes">
			<summary>Demo limitations</summary>
			<p class="checkers-showcase__policy" id="checkers-policy">Loading CPU policy…</p>
			<p class="checkers-showcase__limitations">
				This match lives in memory and has two players. The CPU uses a bounded policy when no evaluator is linked.
			</p>
		</details>
		<noscript>
			<p class="checkers-showcase__noscript">
				The semantic board summary remains available without JavaScript; the 3D renderer requires the GoSX client runtime.
			</p>
		</noscript>
		<script src="/checkers-client.js" defer></script>
	</section>
}
