  // Scene canvas — Canvas 2D fallback renderer.

  function createSceneCanvasLineBatch(ctx2d) {
    let active = false;
    let strokeStyle = "";
    let lineWidth = 0;
    let lineDashKey = "";
    function flush() {
      if (!active) {
        return;
      }
      ctx2d.stroke();
      active = false;
    }
    return {
      line(from, to, color, width, dash) {
        const nextStrokeStyle = color || "#8de1ff";
        const nextLineWidth = Math.max(0.1, sceneNumber(width, 1.8));
        const dashed = dash && dash.dashed;
        const dashSize = Math.max(0.1, sceneNumber(dash && dash.dashSize, nextLineWidth * 2.5));
        const gapSize = Math.max(0.1, sceneNumber(dash && dash.gapSize, dashSize));
        const nextDashKey = dashed ? (dashSize + ":" + gapSize) : "";
        if (!active || nextStrokeStyle !== strokeStyle || nextLineWidth !== lineWidth || nextDashKey !== lineDashKey) {
          flush();
          strokeStyle = nextStrokeStyle;
          lineWidth = nextLineWidth;
          lineDashKey = nextDashKey;
          ctx2d.strokeStyle = strokeStyle;
          ctx2d.lineWidth = lineWidth;
          if (typeof ctx2d.setLineDash === "function") {
            ctx2d.setLineDash(dashed ? [dashSize, gapSize] : []);
          }
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
    const dashes = bundle && bundle.worldLineDashes;
    const dashSizes = bundle && bundle.worldLineDashSizes;
    const gapSizes = bundle && bundle.worldLineGapSizes;
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
      batch.line(from, to, color, segmentWidth > 0 ? segmentWidth : 1.8, {
        dashed: Boolean(dashes && dashes[segmentIndex]),
        dashSize: dashSizes && segmentIndex < dashSizes.length ? dashSizes[segmentIndex] : 0,
        gapSize: gapSizes && segmentIndex < gapSizes.length ? gapSizes[segmentIndex] : 0,
      });
    }
    batch.flush();
  }

  // Point clouds for the Canvas 2D fallback. This is the last-resort Scene3D
  // backend (no WebGL/WebGPU, or webgl-context-lost): point-only scenes like
  // the site-wide starfield must still read as stars instead of collapsing to
  // a background wash. Best-effort fidelity: entry position offsets apply,
  // rigid rotation/spin do not; screen-space point sizes only.
  function renderSceneCanvasPoints(ctx2d, bundle, width, height) {
    const entries = Array.isArray(bundle && bundle.points) ? bundle.points : [];
    if (entries.length === 0) {
      return;
    }
    const prevComposite = ctx2d.globalCompositeOperation;
    const prevAlpha = ctx2d.globalAlpha;
    const world = { x: 0, y: 0, z: 0 };
    for (const entry of entries) {
      if (!entry) {
        continue;
      }
      const positions = entry.positions;
      if (!positions || !positions.length) {
        continue;
      }
      let count = Math.floor(positions.length / 3);
      const declared = sceneNumber(entry.count, count);
      if (declared > 0 && declared < count) {
        count = declared;
      }
      const offsetX = sceneNumber(entry.x, 0);
      const offsetY = sceneNumber(entry.y, 0);
      const offsetZ = sceneNumber(entry.z, 0);
      const sizes = entry.sizes && entry.sizes.length ? entry.sizes : null;
      const colors = entry.colors && entry.colors.length ? entry.colors : null;
      const baseSize = Math.max(0.1, sceneNumber(entry.size, 1));
      const defaultColor = typeof entry.color === "string" && entry.color !== "" ? entry.color : "#e8f2ff";
      ctx2d.globalCompositeOperation = entry.blendMode === "additive" ? "lighter" : prevComposite;
      ctx2d.globalAlpha = Math.max(0, Math.min(1, sceneNumber(entry.opacity, 1)));
      for (let index = 0; index < count; index++) {
        world.x = positions[index * 3] + offsetX;
        world.y = positions[index * 3 + 1] + offsetY;
        world.z = positions[index * 3 + 2] + offsetZ;
        const screen = sceneProjectPoint(world, bundle.camera, width, height);
        if (!screen) {
          continue;
        }
        if (screen.x < -4 || screen.y < -4 || screen.x > width + 4 || screen.y > height + 4) {
          continue;
        }
        const pointSize = sizes && index < sizes.length ? sizes[index] : baseSize;
        const radius = Math.max(0.5, Math.min(4, pointSize * 0.6));
        ctx2d.fillStyle = colors && index < colors.length && colors[index] ? colors[index] : defaultColor;
        ctx2d.beginPath();
        ctx2d.arc(screen.x, screen.y, radius, 0, 2 * Math.PI);
        ctx2d.fill();
      }
    }
    ctx2d.globalCompositeOperation = prevComposite;
    ctx2d.globalAlpha = prevAlpha;
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
        const cssWidth = sceneViewportValue(viewport, "cssWidth", canvas.width);
        const cssHeight = sceneViewportValue(viewport, "cssHeight", canvas.height);
        renderSceneCanvasPoints(ctx2d, bundle, cssWidth, cssHeight);
        if (sceneBundleUsesWorldProjection(bundle)) {
          renderSceneCanvasWorldBundle(ctx2d, bundle, cssWidth, cssHeight);
        } else {
          const batch = createSceneCanvasLineBatch(ctx2d);
          for (const line of lines) {
            batch.line(line.from, line.to, line.color, line.lineWidth, {
              dashed: Boolean(line.lineDash),
              dashSize: line.dashSize,
              gapSize: line.gapSize,
            });
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
