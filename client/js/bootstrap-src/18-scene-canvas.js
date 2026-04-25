  // Scene canvas — Canvas 2D fallback renderer.

  function createSceneCanvasLineBatch(ctx2d) {
    let active = false;
    let strokeStyle = "";
    let lineWidth = 0;
    function flush() {
      if (!active) {
        return;
      }
      ctx2d.stroke();
      active = false;
    }
    return {
      line(from, to, color, width) {
        const nextStrokeStyle = color || "#8de1ff";
        const nextLineWidth = Math.max(0.1, sceneNumber(width, 1.8));
        if (!active || nextStrokeStyle !== strokeStyle || nextLineWidth !== lineWidth) {
          flush();
          strokeStyle = nextStrokeStyle;
          lineWidth = nextLineWidth;
          ctx2d.strokeStyle = strokeStyle;
          ctx2d.lineWidth = lineWidth;
          ctx2d.beginPath();
          active = true;
        }
        ctx2d.moveTo(from.x, from.y);
        ctx2d.lineTo(to.x, to.y);
      },
      flush,
    };
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
    const batch = createSceneCanvasLineBatch(ctx2d);
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
      const color = sceneRGBAString([
        sceneNumber(colors && colors[colorOffset], 0.55),
        sceneNumber(colors && colors[colorOffset + 1], 0.88),
        sceneNumber(colors && colors[colorOffset + 2], 1),
        sceneNumber(colors && colors[colorOffset + 3], 1),
      ]);
      const segmentIndex = index / 2;
      const segmentWidth = widths && segmentIndex < widths.length ? widths[segmentIndex] : 0;
      batch.line(from, to, color, segmentWidth > 0 ? segmentWidth : 1.8);
    }
    batch.flush();
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
          const batch = createSceneCanvasLineBatch(ctx2d);
          for (const line of lines) {
            batch.line(line.from, line.to, line.color, line.lineWidth);
          }
          batch.flush();
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
