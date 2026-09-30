// Page-local upgrade: the parent remains on bootstrap-lite. Template scripts
// cannot be fetched or run until the frame is created after load + paint + idle.
if (window.__gosxDocsHomeHero) {
  window.__gosxDocsHomeHero();
  return;
}
let dispose = () => {};
const motion = matchMedia('(prefers-reduced-motion: reduce)');
const connection = navigator.connection;
const keepStill = () => motion.matches || connection?.saveData ||
  /^(slow-2g|2g|3g)$/.test(connection?.effectiveType || '');

async function hardwareAvailable() {
  const canvas = document.createElement('canvas');
  let gl;
  try {
    gl = canvas.getContext('webgl2', { antialias: false, powerPreference: 'low-power' });
    if (gl && !sceneWebGLRendererLooksSoftware(sceneReadWebGLRendererMetadata(gl))) return true;
  } catch (_) {
    // Try a hardware WebGPU adapter if WebGL2 is unavailable.
  } finally {
    gl?.getExtension('WEBGL_lose_context')?.loseContext();
  }
  try {
    const adapter = await navigator.gpu?.requestAdapter({ powerPreference: 'low-power' });
    const info = adapter?.info;
    return !!adapter && !adapter.isFallbackAdapter && !sceneWebGLRendererLooksSoftware({
      vendor: info?.vendor || '', renderer: [info?.device, info?.description].join(' ')
    });
  } catch (_) {
    return false;
  }
}

// Runs inside the sandbox; readiness means an actual hardware frame exists.
function notifyReady() {
  function check() {
    const mount = document.querySelector('[data-gosx-scene3d]');
    const backend = mount?.getAttribute('data-gosx-scene3d-backend');
    if (mount?.getAttribute('data-gosx-scene3d-revealed') === 'true' &&
        /^(webgl|webgpu)$/.test(backend) &&
        (backend === 'webgpu' || mount.getAttribute('data-gosx-scene3d-software-webgl') !== 'true')) {
      parent.postMessage('gosx-home-hero-ready', '*');
    } else requestAnimationFrame(check);
  }
  requestAnimationFrame(check);
}

function init() {
  dispose();
  const template = document.querySelector('template[data-home-hero]');
  if (!template) return;
  const hero = template.parentElement;
  hero.dataset.heroState = 'still';
  let cancelled = false, timer, idle, paint, frame;
  const current = () => !cancelled && hero.isConnected && !keepStill();
  function stop() {
    cancelled = true;
    clearTimeout(timer);
    cancelAnimationFrame(paint);
    if (idle !== undefined) window.cancelIdleCallback?.(idle);
    window.removeEventListener('load', afterLoad);
    window.removeEventListener('message', revealed);
    motion.removeEventListener('change', stop);
    connection?.removeEventListener('change', preferencesChanged);
    frame?.remove();
    hero.dataset.heroState = 'still';
  }
  function preferencesChanged() { if (keepStill()) stop(); }
  dispose = stop;
  motion.addEventListener('change', stop);
  connection?.addEventListener('change', preferencesChanged);
  if (keepStill()) { stop(); return; }

  async function upgrade() {
    if (!current()) return;
    hero.dataset.heroState = 'probing';
    // Bound a driver probe that never settles; a late result cannot upgrade.
    timer = setTimeout(stop, 15000);
    if (!await hardwareAvailable() || !current()) { stop(); return; }
    frame = document.createElement('iframe');
    frame.className = 'hero__live';
    frame.title = 'Decorative GoSX scene';
    frame.tabIndex = -1;
    frame.setAttribute('aria-hidden', 'true');
    // An opaque sandbox lets Chrome isolate the scene's renderer from the page.
    // Only scripts run in this decorative document; it cannot access the parent.
    frame.setAttribute('sandbox', 'allow-scripts');
    frame.srcdoc = '<!doctype html><html><head><meta name="viewport" content="width=device-width, initial-scale=1"></head><body>' + template.innerHTML + '<script>(' + notifyReady.toString() + ')();<\/script></body></html>';
    hero.dataset.heroState = 'loading';
    window.addEventListener('message', revealed);
    hero.append(frame);
  }
  function revealed(event) {
    if (event.source !== frame?.contentWindow || event.data !== 'gosx-home-hero-ready' || !current()) return;
    clearTimeout(timer);
    window.removeEventListener('message', revealed);
    frame.classList.add('hero__live--visible');
    hero.dataset.heroState = 'live';
  }
  function afterLoad() {
    // Two frames guarantee the still has crossed a paint boundary, even when
    // this script runs during managed navigation or with a warm document.
    paint = requestAnimationFrame(() => {
      paint = requestAnimationFrame(() => {
        if (!current()) return;
        if (window.requestIdleCallback) idle = requestIdleCallback(upgrade, { timeout: 2000 });
        else timer = setTimeout(upgrade, 200);
      });
    });
  }
  if (document.readyState === 'complete') afterLoad();
  else window.addEventListener('load', afterLoad, { once: true });
}
window.__gosxDocsHomeHero = init;
document.addEventListener('gosx:navigate', init);
window.addEventListener('pagehide', () => dispose());
window.addEventListener('pageshow', event => { if (event.persisted) init(); });
init();
