// Drives page input for the worker input test: keyboard, mouse and a fake
// gamepad (headless Chromium has none, so navigator.getGamepads is replaced).
export async function init(page) {
  await page.addInitScript(() => {
    window.__pads = [];
    Object.defineProperty(navigator, "getGamepads", { configurable: true, value: () => window.__pads });
  });
}

// Built inside the page so timestamp uses the page's clock, as a real pad's does.
const setPad = (page, buttons, axes) => page.evaluate(({ buttons, axes }) => {
  window.__pads = [{ index: 0, id: "Fake Pad (STANDARD GAMEPAD)", mapping: "standard", connected: true, timestamp: performance.now(),
    buttons: buttons.map(value => ({ value, pressed: value > 0.5 })), axes }];
}, { buttons, axes });

export async function run(page) {
  await page.waitForFunction(() => (window.__reports || []).some(r => r.kind === "input-ready"), null, { timeout: 30000 });
  await page.mouse.move(40, 50);
  await page.keyboard.down("KeyA");
  await page.keyboard.down("KeyA");  // auto-repeat: must not be forwarded
  await page.keyboard.up("KeyA");
  await page.mouse.move(60, 70);
  await page.mouse.down();
  await page.mouse.up();
  await setPad(page, [0, 0, 0, 0], [0, 0]);
  await page.waitForTimeout(60);
  await setPad(page, [1, 0, 0, 0], [0, 0]);
  await page.waitForTimeout(60);
  await setPad(page, [1, 0, 0, 0], [0.75, -0.5]);
  await page.waitForTimeout(60);
  await page.evaluate(() => { window.__pads = [null]; });
  await page.waitForTimeout(60);
  await page.evaluate(() => { window.__done = true; });
}
