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
    frame.srcdoc = '<!doctype html><html><head><meta name="viewport" content="width=device-width, initial-scale=1"></head><body>' + template.innerHTML + '</body></html>';
    hero.dataset.heroState = 'loading';
    frame.addEventListener('load', () => {
      function reveal() {
        if (!current()) { stop(); return; }
        const mount = frame.contentDocument?.querySelector('[data-gosx-scene3d]');
        if (mount?.getAttribute('data-gosx-scene3d-revealed') === 'true' &&
            /^(webgl|webgpu)$/.test(mount.getAttribute('data-gosx-scene3d-backend')) &&
            (mount.getAttribute('data-gosx-scene3d-backend') === 'webgpu' ||
             mount.getAttribute('data-gosx-scene3d-software-webgl') !== 'true')) {
          clearTimeout(timer);
          frame.classList.add('hero__live--visible');
          hero.dataset.heroState = 'live';
        } else {
          paint = requestAnimationFrame(reveal);
        }
      }
      paint = requestAnimationFrame(reveal);
    }, { once: true });
    hero.append(frame);
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
