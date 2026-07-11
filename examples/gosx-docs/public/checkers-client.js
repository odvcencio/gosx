(function () {
  "use strict";

  var HUB_NAME = "checkers";
  var STATE_EVENT = "checkers:state";
  var root, board, statusEl, turnEl, undoButton, restartButton, materialSelect, personalitySelect, difficultySelect, policyEl;
  var state = null;
  var revision = -1;
  var visualMatchRevision = -1;

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
    if (!root || !board) return;
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

  function render() {
	var sceneMount = document.querySelector(".checkers-showcase__scene [data-gosx-scene3d-mounted]");
	if (sceneMount && Array.isArray(state.sceneCommands) && Number(state.matchRevision) !== visualMatchRevision) {
	  sceneMount.dispatchEvent(new CustomEvent("gosx:scene3d:commands", { detail: { revision: state.revision, commands: state.sceneCommands } }));
	  visualMatchRevision = Number(state.matchRevision);
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
      button.setAttribute("aria-pressed", selected ? "true" : "false");
      button.setAttribute("aria-label", "Hole " + hole + (owner ? ", player " + owner + " piece" : ", empty") + (legal[hole] ? ", legal destination" : ""));
    });
    root.setAttribute("data-checkers-revision", String(state.revision));
    root.setAttribute("data-checkers-match-revision", String(state.matchRevision));
    if (statusEl) statusEl.textContent = state.message;
    if (turnEl) turnEl.textContent = state.finished ? "Finished" : "Player " + (state.active + 1) + " · turn " + state.turn;
    if (undoButton) undoButton.disabled = !state.canUndo;
    if (personalitySelect) personalitySelect.value = state.personality;
    if (difficultySelect) difficultySelect.value = state.difficulty;
    if (policyEl) policyEl.textContent = "CPU policy: " + (state.policyLabel || "safe fallback") + (state.policyFallback ? " · compiled fallback" : " · Arbiter") + (state.searchDepth ? " · depth " + state.searchDepth + " · " + state.searchNodes + " nodes · " + state.searchMS + " ms" : "");
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
