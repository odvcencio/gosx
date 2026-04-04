package docs

func Page() Node {
	return <section class="galaxy-demo" aria-label="Galaxy particle demo">
		<div class="galaxy-demo__scene">
			<Scene3D {...data.scene} />
		</div>
		<div class="galaxy-demo__overlay">
			<h1 class="chrome-text">Galaxy</h1>
			<p>2,800 GPU compute particles</p>
		</div>
	</section>
}
