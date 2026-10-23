package docs

func Page() Node {
	return <main id="main-content" aria-label="Deferred Scene3D test fixture">
		<Scene3D id={data.mountID} class="defer-fixture" {...data.scene}>
			<div data-fixture-poster="true" style="width:320px;height:200px;background:#123456"></div>
		</Scene3D>
		<a id="defer-next" href={data.next} data-gosx-link="true">next</a>
	</main>
}
