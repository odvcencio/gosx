(function () {
  "use strict";
  async function factory(ctx) {
    const canvas = document.createElement("canvas");
    canvas.width = 360;
    canvas.height = 240;
    canvas.setAttribute("aria-label", "Interactive engine surface");
    const button = document.createElement("button");
    button.type = "button";
    button.textContent = "Count: 0";
    const paint = canvas.getContext("2d");
    if (!paint) throw new Error("fixture canvas unavailable");
    const shared = ctx.runtimeMode === "shared";
    if (shared) {
      if (!ctx.runtime.available()) throw new Error("fixture shared runtime unavailable");
      await ctx.runtime.hydrateFromProgramRef();
    }
    ctx.mount.replaceChildren(canvas, button);
    let disposed = false;
    let frame = 0;
    let count = 0;
    let first = true;
    function tick(time) {
      if (disposed) return;
      paint.fillStyle = "#18212b";
      paint.fillRect(0, 0, canvas.width, canvas.height);
      paint.strokeStyle = "#8de1ff";
      paint.beginPath();
      if (shared) {
        ctx.runtime.tick();
        const bundle = ctx.runtime.renderFrame(time / 1000, canvas.width, canvas.height);
        if (!bundle || !bundle.vertexCount || !Array.isArray(bundle.positions)) {
          throw new Error("fixture shared frame unavailable");
        }
        const points = bundle.positions;
        for (let i = 0; i + 5 < points.length; i += 6) {
          paint.moveTo((points[i] + 1) * 180, (1 - points[i + 1]) * 120);
          paint.lineTo((points[i + 3] + 1) * 180, (1 - points[i + 4]) * 120);
        }
      } else {
        const x = 60 + 20 * Math.sin(time / 1000);
        paint.rect(x, 60, 120, 120);
      }
      paint.stroke();
      if (first) {
        first = false;
        ctx.emit("first-tick", { usable: true });
      }
      frame = window.requestAnimationFrame(tick);
    }
    function click() {
      if (!disposed) button.textContent = "Count: " + (++count);
    }
    button.addEventListener("click", click);
    frame = window.requestAnimationFrame(tick);
    return {
      dispose() {
        if (disposed) return;
        disposed = true;
        window.cancelAnimationFrame(frame);
        button.removeEventListener("click", click);
        ctx.runtime.dispose();
        ctx.mount.replaceChildren();
      },
    };
  }
  if (typeof window.__gosx_register_engine_factory === "function") {
    window.__gosx_register_engine_factory("BudgetEngine", factory);
  } else {
    const factories = window.__gosx_engine_factories || (window.__gosx_engine_factories = Object.create(null));
    factories.BudgetEngine = factory;
  }
})();
