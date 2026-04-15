  // Scene canvas — Canvas 2D fallback renderer.

  function strokeLine(ctx2d, from, to) {
    ctx2d.beginPath();
    ctx2d.moveTo(from.x, from.y);
    ctx2d.lineTo(to.x, to.y);
    ctx2d.stroke();
  }

  function sceneBundleUsesWorldProjection(bundle) {
    return Boolean(
      bundle &&
      bundle.camera &&
      bundle.sourceCamera &&
      !sceneCameraEquivalent(bundle.sourceCamera, bundle.camera) &&
      bundle.worldVertexCount > 0 &&
      bundle.worldPositions &&
      bundle.worldColors
    );
  }

  function renderSceneCanvasWorldBundle(ctx2d, bundle, width, height) {
    const positions = bundle && bundle.worldPositions;
    const colors = bundle && bundle.worldColors;
    // worldLineWidths is a parallel per-segment Float32Array populated by
    // appendSceneObjectToBundle. Segment index = index / 2 because each
    // segment is two vertices. Undefined/zero falls back to the legacy
    // 1.8px default so pre-v0.15.1 scenes render unchanged.
    const widths = bundle && bundle.worldLineWidths;
    const vertexCount = Math.max(0, Math.floor(sceneNumber(bundle && bundle.worldVertexCount, 0)));
    for (let index = 0; index + 1 < vertexCount; index += 2) {
      const fromWorld = sceneWorldPointAt(positions, index);
      const toWorld = sceneWorldPointAt(positions, index + 1);
      if (!fromWorld || !toWorld) {
        continue;
      }
      const from = sceneProjectPoint(fromWorld, bundle.camera, width, height);
      const to = sceneProjectPoint(toWorld, bundle.camera, width, height);
      if (!from || !to) {
        continue;
      }
      const colorOffset = index * 4;
      ctx2d.strokeStyle = sceneRGBAString([
        sceneNumber(colors && colors[colorOffset], 0.55),
        sceneNumber(colors && colors[colorOffset + 1], 0.88),
        sceneNumber(colors && colors[colorOffset + 2], 1),
        sceneNumber(colors && colors[colorOffset + 3], 1),
      ]);
      const segmentIndex = index / 2;
      const segmentWidth = widths && segmentIndex < widths.length ? widths[segmentIndex] : 0;
      ctx2d.lineWidth = segmentWidth > 0 ? segmentWidth : 1.8;
      strokeLine(ctx2d, from, to);
    }
  }

  function createSceneCanvasRenderer(ctx2d, canvas) {
    return {
      kind: "canvas",
      render(bundle, viewport) {
        const devicePixelRatio = Math.max(1, sceneViewportValue(viewport, "devicePixelRatio", 1));
        const lines = Array.isArray(bundle && bundle.lines) ? bundle.lines : [];
        ctx2d.clearRect(0, 0, canvas.width, canvas.height);
        ctx2d.fillStyle = bundle && bundle.background ? bundle.background : "#08151f";
        ctx2d.fillRect(0, 0, canvas.width, canvas.height);
        if (typeof ctx2d.save === "function") {
          ctx2d.save();
        }
        if (devicePixelRatio !== 1 && typeof ctx2d.scale === "function") {
          ctx2d.scale(devicePixelRatio, devicePixelRatio);
        }
        if (sceneBundleUsesWorldProjection(bundle)) {
          renderSceneCanvasWorldBundle(ctx2d, bundle, sceneViewportValue(viewport, "cssWidth", canvas.width), sceneViewportValue(viewport, "cssHeight", canvas.height));
        } else {
          for (const line of lines) {
            ctx2d.strokeStyle = line.color;
            ctx2d.lineWidth = line.lineWidth;
            strokeLine(ctx2d, line.from, line.to);
          }
        }
        if (typeof ctx2d.restore === "function") {
          ctx2d.restore();
        }
      },
      dispose() {},
    };
  }

  if (typeof sceneBackendRegistry !== "undefined" && sceneBackendRegistry) {
    sceneBackendRegistry.register("canvas2d", {
      capabilities: ["canvas"],
      create: function(canvas) {
        const ctx2d = typeof canvas.getContext === "function" ? canvas.getContext("2d") : null;
        return ctx2d ? createSceneCanvasRenderer(ctx2d, canvas) : null;
      },
    });
  }
