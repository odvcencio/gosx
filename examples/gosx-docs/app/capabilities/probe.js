(() => {
	const scope = document.querySelector("[data-gosx-capability-probe]");
	if (!scope) return;

	const labels = {
		webgpu: "WebGPU",
		webgl: "WebGL2",
		canvas: "Canvas2D",
		unsupported: "no supported renderer",
	};
	const output = scope.querySelector("[data-gosx-browser-renderer-label]");
	const select = (renderer) => {
		scope.setAttribute("data-gosx-browser-renderer", renderer);
		if (output) output.textContent = labels[renderer];
	};

	async function probe() {
		if (navigator.gpu && typeof navigator.gpu.requestAdapter === "function") {
			try {
				const adapter = await navigator.gpu.requestAdapter();
				if (adapter) {
					const device = await adapter.requestDevice();
					device.destroy();
					select("webgpu");
					return;
				}
			} catch (_) {}
		}

		const canvas = document.createElement("canvas");
		try {
			const gl = canvas.getContext("webgl2");
			if (gl) {
				gl.getExtension("WEBGL_lose_context")?.loseContext();
				select("webgl");
				return;
			}
		} catch (_) {}

		try {
			if (canvas.getContext("2d")) {
				select("canvas");
				return;
			}
		} catch (_) {}
		select("unsupported");
	}

	const start = () => probe().catch(() => select("unsupported"));
	if (typeof window.requestIdleCallback === "function") {
		window.requestIdleCallback(start, { timeout: 1500 });
	} else {
		window.setTimeout(start, 1);
	}
})();
