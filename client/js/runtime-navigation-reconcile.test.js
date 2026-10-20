"use strict";

// Focused coverage for navigation.ts's document reconciliation and automatic
// safe-navigation defaults. The broad lifecycle suite stays in runtime-14;
// these tests pin node identity, live form state, and native opt-out behavior.

const test = require("node:test");
const assert = require("node:assert/strict");

const {
  navigationSource,
  FakeElement,
  createContext,
  runScript,
  flushAsyncWork,
  buildNavigatedDocument,
} = require("./runtime-test-harness.js");

function navigationEvent(target, overrides = {}) {
  let prevented = false;
  const event = Object.assign({
    type: "click",
    target,
    button: 0,
    ctrlKey: false,
    metaKey: false,
    shiftKey: false,
    altKey: false,
    defaultPrevented: false,
    preventDefault() {
      prevented = true;
      this.defaultPrevented = true;
    },
  }, overrides);
  return { event, prevented: () => prevented };
}

function submitEvent(form, submitter = null) {
  return navigationEvent(form, {
    type: "submit",
    submitter,
  });
}

test("soft navigation reconciles stable panels and forms in place while updating server content", async () => {
  const currentMain = new FakeElement("main", null);
  currentMain.setAttribute("class", "studio-shell old-shell");

  const currentStatus = new FakeElement("section", null);
  currentStatus.setAttribute("data-gosx-key", "studio-status");
  currentStatus.textContent = "Revision 7";

  const currentPanel = new FakeElement("aside", null);
  currentPanel.id = "inspector-panel";
  currentPanel.setAttribute("class", "panel old");
  const currentHeading = new FakeElement("h2", null);
  currentHeading.textContent = "Transform";

  const currentForm = new FakeElement("form", null);
  currentForm.setAttribute("method", "post");
  currentForm.setAttribute("action", "/scene/__actions/set-transform");
  const currentInput = new FakeElement("input", null);
  currentInput.id = "transform-x";
  currentInput.setAttribute("name", "x");
  currentInput.setAttribute("value", "0");
  currentInput.setAttribute("placeholder", "0.0");
  currentInput.value = "1.25";
  currentInput.selectionStart = 1;
  currentInput.selectionEnd = 4;
  currentForm.appendChild(currentInput);
  const currentUntouched = new FakeElement("input", null);
  currentUntouched.id = "transform-y";
  currentUntouched.setAttribute("name", "y");
  currentUntouched.setAttribute("value", "0");
  currentUntouched.value = "0";
  currentForm.appendChild(currentUntouched);

  const currentIsland = new FakeElement("div", null);
  currentIsland.id = "computed-inspector";
  currentIsland.setAttribute("data-gosx-island", "InspectorStats");
  currentIsland.textContent = "stale runtime state";

  currentPanel.appendChild(currentHeading);
  currentPanel.appendChild(currentForm);
  currentPanel.appendChild(currentIsland);
  currentMain.appendChild(currentStatus);
  currentMain.appendChild(currentPanel);

  const parsedDocs = new Map();
  const env = createContext({
    elements: [currentMain],
    fetchRoutes: {
      "http://localhost:3000/scene?selection=cube": {
        text: "__RECONCILED_SCENE__",
        url: "http://localhost:3000/scene?selection=cube",
      },
    },
    parseHTML(html) {
      return parsedDocs.get(html);
    },
  });
  env.context.__gosx_dispose_page = async function() {};
  env.context.__gosx_bootstrap_page = async function() {};
  currentInput.focus();

  const nextMain = new FakeElement("main", null);
  nextMain.setAttribute("class", "studio-shell selected-shell");
  const nextPanel = new FakeElement("aside", null);
  nextPanel.id = "inspector-panel";
  nextPanel.setAttribute("class", "panel selected");
  nextPanel.setAttribute("data-selection", "cube");
  const nextHeading = new FakeElement("h2", null);
  nextHeading.textContent = "Transform · Cube";
  const nextForm = new FakeElement("form", null);
  nextForm.setAttribute("method", "post");
  nextForm.setAttribute("action", "/scene/__actions/set-transform");
  nextForm.setAttribute("data-revision", "8");
  const nextInput = new FakeElement("input", null);
  nextInput.id = "transform-x";
  nextInput.setAttribute("name", "x");
  nextInput.setAttribute("value", "2");
  nextInput.setAttribute("placeholder", "world X");
  nextInput.value = "2";
  nextForm.appendChild(nextInput);
  const nextUntouched = new FakeElement("input", null);
  nextUntouched.id = "transform-y";
  nextUntouched.setAttribute("name", "y");
  nextUntouched.setAttribute("value", "3");
  nextUntouched.value = "3";
  nextForm.appendChild(nextUntouched);
  const nextIsland = new FakeElement("div", null);
  nextIsland.id = "computed-inspector";
  nextIsland.setAttribute("data-gosx-island", "InspectorStats");
  nextIsland.textContent = "fresh server fallback";
  nextPanel.appendChild(nextHeading);
  nextPanel.appendChild(nextForm);
  nextPanel.appendChild(nextIsland);

  const nextStatus = new FakeElement("section", null);
  nextStatus.setAttribute("data-gosx-key", "studio-status");
  nextStatus.setAttribute("data-revision", "8");
  nextStatus.textContent = "Revision 8";
  // Both keyed children move, proving identity is keyed rather than tied to
  // their previous sibling index.
  nextMain.appendChild(nextPanel);
  nextMain.appendChild(nextStatus);

  parsedDocs.set("__RECONCILED_SCENE__", buildNavigatedDocument({
    title: "Scene · Cube",
    bodyNodes: [nextMain],
  }));

  runScript(navigationSource, env.context, "navigation_runtime.js");
  await env.context.__gosx.navigation.navigate("http://localhost:3000/scene?selection=cube");
  await flushAsyncWork();

  assert.strictEqual(env.document.body.firstChild, currentMain, "stable outer layout should not be destroyed");
  assert.strictEqual(currentMain.childNodes[0], currentPanel, "id-keyed panel should move without being recreated");
  assert.strictEqual(currentMain.childNodes[1], currentStatus, "data-gosx-key node should move without being recreated");
  assert.strictEqual(currentPanel.childNodes[1], currentForm, "same-purpose unkeyed form should reconcile positionally");
  assert.strictEqual(env.document.getElementById("transform-x"), currentInput);
  assert.strictEqual(env.document.getElementById("transform-y"), currentUntouched);
  assert.equal(currentMain.getAttribute("class"), "studio-shell selected-shell");
  assert.equal(currentPanel.getAttribute("class"), "panel selected");
  assert.equal(currentPanel.getAttribute("data-selection"), "cube");
  assert.equal(currentHeading.textContent, "Transform · Cube");
  assert.equal(currentForm.getAttribute("data-revision"), "8");
  assert.equal(currentStatus.textContent, "Revision 8");
  assert.equal(currentStatus.getAttribute("data-revision"), "8");

  assert.equal(currentInput.getAttribute("value"), "2", "new server default should update");
  assert.equal(currentInput.getAttribute("placeholder"), "world X");
  assert.equal(currentInput.value, "1.25", "dirty live value should survive reconciliation");
  assert.equal(currentInput.selectionStart, 1);
  assert.equal(currentInput.selectionEnd, 4);
  assert.strictEqual(env.document.activeElement, currentInput, "focused stable control should keep focus");
  assert.equal(currentUntouched.getAttribute("value"), "3");
  assert.equal(currentUntouched.value, "3", "untouched control should accept the incoming server value");

  const liveIsland = env.document.getElementById("computed-inspector");
  assert.notStrictEqual(liveIsland, currentIsland, "runtime-owned island root must follow replace/remount lifecycle");
  assert.equal(liveIsland.textContent, "fresh server fallback");
});

test("plain same-origin anchors are managed and data-gosx-native opts out", async () => {
  const managed = new FakeElement("a", null);
  managed.setAttribute("href", "/next");
  managed.textContent = "Next";
  const native = new FakeElement("a", null);
  native.setAttribute("href", "/native");
  native.setAttribute("data-gosx-native", "");
  native.textContent = "Native";

  const parsedDocs = new Map();
  const env = createContext({
    elements: [managed, native],
    fetchRoutes: {
      "http://localhost:3000/next": { text: "__NEXT__", url: "http://localhost:3000/next" },
    },
    parseHTML(html) {
      return parsedDocs.get(html);
    },
  });
  env.context.__gosx_dispose_page = async function() {};
  env.context.__gosx_bootstrap_page = async function() {};
  const nextMain = new FakeElement("main", null);
  nextMain.id = "next-page";
  nextMain.textContent = "Next page";
  parsedDocs.set("__NEXT__", buildNavigatedDocument({ title: "Next", bodyNodes: [nextMain] }));

  runScript(navigationSource, env.context, "navigation_runtime.js");
  assert.equal(managed.getAttribute("data-gosx-link-state"), "idle");
  assert.equal(native.hasAttribute("data-gosx-link-state"), false);

  const click = env.document.eventListeners.get("click")[0];
  const nativeClick = navigationEvent(native);
  click(nativeClick.event);
  await flushAsyncWork();
  assert.equal(nativeClick.prevented(), false);
  assert.equal(env.fetchCalls.length, 0);

  const managedClick = navigationEvent(managed);
  click(managedClick.event);
  await flushAsyncWork();
  assert.equal(managedClick.prevented(), true);
  assert.equal(env.fetchCalls[0].url, "http://localhost:3000/next");
  assert.equal(env.document.getElementById("next-page").textContent, "Next page");
});

test("GET and framework action forms are automatic while other POST and native forms stay native", async () => {
  const search = new FakeElement("form", null);
  search.setAttribute("action", "/search");
  search.setAttribute("method", "get");
  const query = new FakeElement("input", null);
  query.setAttribute("name", "q");
  query.value = "mesh";
  search.appendChild(query);

  const action = new FakeElement("form", null);
  action.setAttribute("action", "/scene/__actions/save");
  action.setAttribute("method", "post");
  const name = new FakeElement("input", null);
  name.setAttribute("name", "name");
  name.value = "Board";
  action.appendChild(name);

  const ordinaryPost = new FakeElement("form", null);
  ordinaryPost.setAttribute("action", "/upload");
  ordinaryPost.setAttribute("method", "post");
  const nativeSearch = new FakeElement("form", null);
  nativeSearch.setAttribute("action", "/native-search");
  nativeSearch.setAttribute("method", "get");
  nativeSearch.setAttribute("data-gosx-native", "");

  const parsedDocs = new Map();
  const env = createContext({
    elements: [search, action, ordinaryPost, nativeSearch],
    fetchRoutes: {
      "http://localhost:3000/search?q=mesh": {
        text: "__SEARCH__",
        url: "http://localhost:3000/search?q=mesh",
      },
      "http://localhost:3000/scene/__actions/save": {
        text: '{"ok":true}',
        url: "http://localhost:3000/scene/__actions/save",
      },
    },
    parseHTML(html) {
      return parsedDocs.get(html);
    },
  });
  env.context.__gosx_dispose_page = async function() {};
  env.context.__gosx_bootstrap_page = async function() {};
  const results = new FakeElement("main", null);
  results.id = "results";
  results.textContent = "mesh results";
  parsedDocs.set("__SEARCH__", buildNavigatedDocument({ title: "Search", bodyNodes: [results] }));

  runScript(navigationSource, env.context, "navigation_runtime.js");
  const submit = env.document.eventListeners.get("submit")[0];

  const ordinaryEvent = submitEvent(ordinaryPost);
  submit(ordinaryEvent.event);
  const nativeEvent = submitEvent(nativeSearch);
  submit(nativeEvent.event);
  await flushAsyncWork();
  assert.equal(ordinaryEvent.prevented(), false);
  assert.equal(nativeEvent.prevented(), false);
  assert.equal(env.fetchCalls.length, 0);

  const actionEvent = submitEvent(action);
  submit(actionEvent.event);
  await flushAsyncWork();
  assert.equal(actionEvent.prevented(), true);
  assert.equal(env.fetchCalls[0].url, "http://localhost:3000/scene/__actions/save");
  assert.equal(env.fetchCalls[0].init.method, "POST");

  const searchEvent = submitEvent(search);
  submit(searchEvent.event);
  await flushAsyncWork();
  assert.equal(searchEvent.prevented(), true);
  assert.equal(env.fetchCalls[1].url, "http://localhost:3000/search?q=mesh");
  assert.equal(env.document.getElementById("results").textContent, "mesh results");
});

test("automatic action transport failure performs one native fallback without re-interception", async () => {
  const form = new FakeElement("form", null);
  form.setAttribute("action", "/scene/__actions/save");
  form.setAttribute("method", "post");
  let nativeMarkerDuringFallback = false;
  let fallbackCalls = 0;
  form.requestSubmit = function() {
    fallbackCalls += 1;
    nativeMarkerDuringFallback = form.hasAttribute("data-gosx-native");
  };

  const env = createContext({
    elements: [form],
    fetchRoutes: {
      "http://localhost:3000/scene/__actions/save"() {
        throw new Error("transport unavailable");
      },
    },
  });
  runScript(navigationSource, env.context, "navigation_runtime.js");

  const event = submitEvent(form);
  env.document.eventListeners.get("submit")[0](event.event);
  await flushAsyncWork();

  assert.equal(event.prevented(), true);
  assert.equal(fallbackCalls, 1);
  assert.equal(nativeMarkerDuringFallback, true, "fallback requestSubmit must run behind the native opt-out");
  assert.equal(form.hasAttribute("data-gosx-native"), false, "temporary opt-out must be restored afterward");
});

test("a superseded navigation cannot dispose the new page after awaiting async engine reuse", async () => {
  const parsedDocs = new Map();
  let reuseCall = 0;
  let releaseFirstReuse;
  const firstReuse = new Promise((resolve) => { releaseFirstReuse = resolve; });
  const disposeSets = [];
  const env = createContext({
    elements: [],
    fetchRoutes: {
      "http://localhost:3000/one": { text: "__ONE__", url: "http://localhost:3000/one" },
      "http://localhost:3000/two": { text: "__TWO__", url: "http://localhost:3000/two" },
    },
    parseHTML(html) {
      return parsedDocs.get(html);
    },
  });
  for (const [token, id] of [["__ONE__", "page-one"], ["__TWO__", "page-two"]]) {
    const main = new FakeElement("main", null);
    main.id = id;
    main.textContent = id;
    parsedDocs.set(token, buildNavigatedDocument({ title: id, bodyNodes: [main] }));
  }
  env.context.__gosx_reusable_engines = function() {
    reuseCall += 1;
    return reuseCall === 1 ? firstReuse : new env.context.Set();
  };
  env.context.__gosx_dispose_page = async function(ids) { disposeSets.push(ids); };
  env.context.__gosx_bootstrap_page = async function() {};

  runScript(navigationSource, env.context, "navigation_runtime.js");
  const first = env.context.__gosx.navigation.navigate("http://localhost:3000/one");
  for (let i = 0; i < 10 && reuseCall < 1; i += 1) await Promise.resolve();
  assert.equal(reuseCall, 1);
  assert.equal(disposeSets.length, 0, "disposal waits for the reuse decision");

  const second = env.context.__gosx.navigation.navigate("http://localhost:3000/two");
  assert.equal(await second, true);
  releaseFirstReuse(new env.context.Set(["stale-engine"]));
  assert.equal(await first, false);

  assert.equal(disposeSets.length, 1, "stale navigation must stop before disposal after its await");
  assert.equal(disposeSets[0].size, 0);
  assert.equal(env.document.getElementById("page-two").textContent, "page-two");
});
