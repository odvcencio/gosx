package main

func Page() Node {
	return <main style="margin:0;background:#070c13;color:#dbe4eb;font:16px system-ui">
		<p>Rigid GLB crowd · {data.count} actors · {data.mode} · {data.backend}</p>
		<Scene3D {...data.scene} />
	</main>
}
