(function () {
  "use strict";

  var HUB_NAME = "checkers";
  var STATE_EVENT = "checkers:state";
  var SET_INSTANCED_MESHES_KIND = 8;
  var root, board, statusEl, turnEl, undoButton, restartButton, materialSelect, personalitySelect, difficultySelect, policyEl;
  var depthEl, nodesEl, timeEl, cacheEl;
  var state = null;
  var revision = -1;
  var visualMatchRevision = -1;
  var activeAnim = null;
  var reduceMotionQuery = typeof window.matchMedia === "function" ? window.matchMedia("(prefers-reduced-motion: reduce)") : null;

  function mount() {
    root = document.querySelector("[data-checkers-root]");
    board = document.getElementById("checkers-board");
    statusEl = document.getElementById("checkers-status");
    turnEl = document.getElementById("checkers-turn");
    undoButton = document.getElementById("checkers-undo");
    restartButton = document.getElementById("checkers-restart");
    materialSelect = document.getElementById("checkers-material");
    personalitySelect = document.getElementById("checkers-personality");
    difficultySelect = document.getElementById("checkers-difficulty");
    policyEl = document.getElementById("checkers-policy");
    depthEl = document.getElementById("checkers-search-depth");
    nodesEl = document.getElementById("checkers-search-nodes");
    timeEl = document.getElementById("checkers-search-time");
    cacheEl = document.getElementById("checkers-search-cache");
    if (!root || !board) return;
	 syncMaterialFromURL();
    board.addEventListener("click", onBoardClick);
    board.addEventListener("keydown", onBoardKeydown);
    if (undoButton) undoButton.addEventListener("click", function () { send("checkers:undo", {}); });
    if (restartButton) restartButton.addEventListener("click", function () { send("checkers:restart", {}); });
    if (materialSelect) materialSelect.addEventListener("change", onMaterialChange);
    if (personalitySelect) personalitySelect.addEventListener("change", onSettingsChange);
    if (difficultySelect) difficultySelect.addEventListener("change", onSettingsChange);
    document.addEventListener("gosx:hub:event", onHubEvent);
    document.addEventListener("gosx:navigate", teardown, { once: true });
  }

  function syncMaterialFromURL() {
	var requested = new URL(window.location.href).searchParams.get("material") || "carved-wood";
	var valid = { "imperial-jade": true, "carved-wood": true, "brushed-steel": true, "midnight-lacquer": true, "moon-porcelain": true };
	var active = valid[requested] ? requested : "carved-wood";
	if (materialSelect) materialSelect.value = active;
	root.setAttribute("data-checkers-material", active);
	root.setAttribute("data-checkers-material-source", valid[requested] ? "url" : "fallback");
  }

  function onMaterialChange() {
    var url = new URL(window.location.href);
    url.searchParams.set("material", materialSelect.value);
    window.location.assign(url.toString());
  }

  function onSettingsChange() { send("checkers:settings", { personality: personalitySelect.value, difficulty: difficultySelect.value }); }

  function onBoardKeydown(event) {
    var current = event.target.closest("[data-checkers-hole]");
    if (!current) return;
    var deltas = {
      ArrowRight: [1, -1, 0],
      ArrowLeft: [-1, 1, 0],
      ArrowUp: [0, -1, 1],
      ArrowDown: [0, 1, -1]
    };
    if (event.key === "Home" || event.key === "End") {
      var buttons = board.querySelectorAll("[data-checkers-hole]");
      moveBoardFocus(current, buttons[event.key === "Home" ? 0 : buttons.length - 1]);
      event.preventDefault();
      return;
    }
    var delta = deltas[event.key];
    if (!delta) return;
    var x = Number(current.dataset.x) + delta[0];
    var y = Number(current.dataset.y) + delta[1];
    var z = Number(current.dataset.z) + delta[2];
    var next = board.querySelector('[data-x="' + x + '"][data-y="' + y + '"][data-z="' + z + '"]');
    if (next) moveBoardFocus(current, next);
    event.preventDefault();
  }

  function moveBoardFocus(current, next) {
    if (!next) return;
    current.tabIndex = -1;
    next.tabIndex = 0;
    next.focus();
  }

  function onHubEvent(event) {
    var detail = event.detail;
    if (!detail || detail.hubName !== HUB_NAME || detail.event !== STATE_EVENT) return;
    if (!detail.data || Number(detail.data.revision) <= revision) return;
    revision = Number(detail.data.revision);
    state = detail.data;
    render();
  }

  function onBoardClick(event) {
    var button = event.target.closest("[data-checkers-hole]");
    if (!button || !state || state.finished || state.thinking) return;
    var hole = Number(button.getAttribute("data-checkers-hole"));
    var owner = Number(state.board[hole] || 0);
    if (state.selected < 0 || owner === state.active + 1) {
      send("checkers:source", { hole: hole });
      return;
    }
    send("checkers:destination", { hole: hole });
  }

  function dispatchScene(sceneMount, commands) {
    sceneMount.dispatchEvent(new CustomEvent("gosx:scene3d:commands", { detail: { revision: state.revision, commands: commands } }));
  }

  function stopMoveAnimation(applyFinal) {
    if (!activeAnim) return;
    if (activeAnim.raf) cancelAnimationFrame(activeAnim.raf);
    var anim = activeAnim;
    activeAnim = null;
    if (applyFinal) dispatchScene(anim.mount, anim.finalCommands);
  }

  function syncScene() {
    var sceneMount = document.querySelector(".checkers-showcase__scene [data-gosx-scene3d-mounted]");
    if (!sceneMount || !Array.isArray(state.sceneCommands)) return;
    var rev = Number(state.matchRevision);
    if (rev === visualMatchRevision) return;
    var previous = visualMatchRevision;
    visualMatchRevision = rev;
    stopMoveAnimation(true);
    var move = state.lastMove;
    var animatable = move && Number(move.forRevision) === rev && previous >= 0 && rev === previous + 1 &&
      Array.isArray(move.path) && move.path.length >= 2 &&
      !(reduceMotionQuery && reduceMotionQuery.matches) && document.visibilityState !== "hidden";
    if (animatable && animateMove(sceneMount, move, state.sceneCommands)) return;
    dispatchScene(sceneMount, state.sceneCommands);
  }

  // The committed snapshot is authoritative; the tween only displaces the one
  // moved instance along the server-provided waypoint path, then lands on the
  // exact final command payload.
  function animateMove(sceneMount, move, finalCommands) {
    var target = findMoverInstance(finalCommands, move);
    if (!target) return false;
    var segments = move.path.length - 1;
    var perSegment = move.kind === "hop" ? 260 : 210;
    var total = Math.min(160 + perSegment * segments, 1300);
    var lift = move.kind === "hop" ? 0.42 : 0.15;
    var startTime = null;
    activeAnim = { raf: 0, mount: sceneMount, finalCommands: finalCommands };
    function frame(now) {
      if (!activeAnim) return;
      if (startTime === null) startTime = now;
      var t = Math.min((now - startTime) / total, 1);
      if (t >= 1) {
        stopMoveAnimation(true);
        return;
      }
      dispatchScene(sceneMount, patchedCommands(finalCommands, target, pathPosition(move.path, t, lift)));
      activeAnim.raf = requestAnimationFrame(frame);
    }
    activeAnim.raf = requestAnimationFrame(frame);
    return true;
  }

  function findMoverInstance(commands, move) {
    var end = move.path[move.path.length - 1];
    for (var c = 0; c < commands.length; c++) {
      var command = commands[c];
      if (!command || command.kind !== SET_INSTANCED_MESHES_KIND || !command.data || !Array.isArray(command.data.instancedMeshes)) continue;
      var meshes = command.data.instancedMeshes;
      for (var m = 0; m < meshes.length; m++) {
        var mesh = meshes[m];
        if (!mesh || mesh.id !== "checkers-player-" + move.player) continue;
        var stride = mesh.transformStride || 16;
        if (stride < 15 || !Array.isArray(mesh.transforms)) return null;
        var count = Math.floor(mesh.transforms.length / stride);
        for (var i = 0; i < count; i++) {
          var base = i * stride;
          if (Math.abs(mesh.transforms[base + 12] - end.x) < 1e-4 && Math.abs(mesh.transforms[base + 14] - end.z) < 1e-4) {
            return { commandIndex: c, meshIndex: m, instanceBase: base };
          }
        }
      }
    }
    return null;
  }

  function pathPosition(path, t, lift) {
    var segments = path.length - 1;
    var scaled = t * segments;
    var index = Math.min(Math.floor(scaled), segments - 1);
    var local = scaled - index;
    var eased = local < 0.5 ? 2 * local * local : 1 - Math.pow(-2 * local + 2, 2) / 2;
    var a = path[index];
    var b = path[index + 1];
    return {
      x: a.x + (b.x - a.x) * eased,
      y: a.y + (b.y - a.y) * eased + Math.sin(Math.PI * eased) * lift,
      z: a.z + (b.z - a.z) * eased
    };
  }

  function patchedCommands(commands, target, pos) {
    return commands.map(function (command, c) {
      if (c !== target.commandIndex) return command;
      var data = {};
      for (var dataKey in command.data) data[dataKey] = command.data[dataKey];
      data.instancedMeshes = command.data.instancedMeshes.map(function (mesh, m) {
        if (m !== target.meshIndex) return mesh;
        var patched = {};
        for (var key in mesh) patched[key] = mesh[key];
        patched.transforms = mesh.transforms.slice();
        patched.transforms[target.instanceBase + 12] = pos.x;
        patched.transforms[target.instanceBase + 13] = pos.y;
        patched.transforms[target.instanceBase + 14] = pos.z;
        return patched;
      });
      return { kind: command.kind, objectId: command.objectId, data: data };
    });
  }

  function formatCount(value) {
    var n = Number(value) || 0;
    if (n >= 1000000) return (n / 1000000).toFixed(1) + "M";
    if (n >= 1000) return (n / 1000).toFixed(1) + "k";
    return String(n);
  }

  function render() {
    syncScene();
    var lastFrom = -1;
    var lastTo = -1;
    if (state.lastMove && Number(state.lastMove.forRevision) === Number(state.matchRevision)) {
      lastFrom = Number(state.lastMove.from);
      lastTo = Number(state.lastMove.to);
    }
    var legal = Object.create(null);
    var hops = Object.create(null);
    (state.legal || []).forEach(function (hole) { legal[hole] = true; });
    (state.legalHops || []).forEach(function (hole) { hops[hole] = true; });
    board.querySelectorAll("[data-checkers-hole]").forEach(function (button) {
      var hole = Number(button.getAttribute("data-checkers-hole"));
      var owner = Number(state.board[hole] || 0);
      var selected = hole === state.selected;
      button.setAttribute("data-owner", String(owner));
      button.toggleAttribute("data-legal", !!legal[hole]);
      button.toggleAttribute("data-hop", !!hops[hole]);
      button.toggleAttribute("data-last-from", hole === lastFrom);
      button.toggleAttribute("data-last-to", hole === lastTo);
      button.setAttribute("aria-pressed", selected ? "true" : "false");
      button.setAttribute("aria-label", "Hole " + hole + (owner ? ", player " + owner + " piece" : ", empty") + (legal[hole] ? ", legal destination" : ""));
    });
    root.setAttribute("data-checkers-revision", String(state.revision));
    root.setAttribute("data-checkers-match-revision", String(state.matchRevision));
    root.toggleAttribute("data-checkers-thinking", !!state.thinking);
    if (statusEl) statusEl.textContent = state.message;
    if (turnEl) turnEl.textContent = state.finished ? "Finished" : "Player " + (state.active + 1) + " · turn " + state.turn;
    if (undoButton) undoButton.disabled = !state.canUndo;
    if (personalitySelect) personalitySelect.value = state.personality;
    if (difficultySelect) difficultySelect.value = state.difficulty;
    if (policyEl) policyEl.textContent = "CPU policy: " + (state.policyLabel || "safe fallback") + (state.policyFallback ? " · compiled fallback" : " · Arbiter");
    if (depthEl) depthEl.textContent = state.searchDepth ? String(state.searchDepth) : "—";
    if (nodesEl) nodesEl.textContent = state.searchNodes ? formatCount(state.searchNodes) : "—";
    if (timeEl) timeEl.textContent = state.searchMS ? state.searchMS + " ms" : "—";
    if (cacheEl) cacheEl.textContent = state.searchCacheHits ? formatCount(state.searchCacheHits) : "—";
    board.toggleAttribute("aria-busy", !!state.thinking);
  }

  function send(event, data) {
    var sent = false;
    var hubs = window.__gosx && window.__gosx.hubs;
    if (hubs) hubs.forEach(function (record) {
      if (record && record.entry && record.entry.name === HUB_NAME && record.socket && record.socket.readyState === 1) {
        record.socket.send(JSON.stringify({ event: event, data: data }));
        sent = true;
      }
    });
    if (!sent && statusEl) statusEl.textContent = "Hub reconnecting — no action was sent.";
    return sent;
  }

  function teardown() {
    stopMoveAnimation(false);
    document.removeEventListener("gosx:hub:event", onHubEvent);
    if (board) board.removeEventListener("click", onBoardClick);
    if (board) board.removeEventListener("keydown", onBoardKeydown);
    if (materialSelect) materialSelect.removeEventListener("change", onMaterialChange);
    if (personalitySelect) personalitySelect.removeEventListener("change", onSettingsChange);
    if (difficultySelect) difficultySelect.removeEventListener("change", onSettingsChange);
  }

  if (document.readyState === "loading") document.addEventListener("DOMContentLoaded", mount);
  else mount();
})();
