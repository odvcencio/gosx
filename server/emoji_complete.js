(function() {
  "use strict";

  var codes = null;
  var activeInput = null;
  var dropdown = null;
  var selectedIndex = 0;
  var triggerStart = -1;

  function ensureCodes(callback) {
    if (codes) { callback(); return; }
    var script = document.querySelector("[data-gosx-emoji-codes]");
    if (script) {
      try { codes = JSON.parse(script.textContent); callback(); } catch(e) {}
      return;
    }
    fetch("/_gosx/emoji-codes.json").then(function(r) { return r.json(); }).then(function(data) {
      codes = data;
      callback();
    }).catch(function() {});
  }

  function search(query, limit) {
    if (!codes || !query) return [];
    var q = query.toLowerCase();
    var exact = [];
    var prefix = [];
    var fuzzy = [];
    for (var i = 0; i < codes.length; i++) {
      var name = codes[i][0];
      if (name === q) { exact.push(codes[i]); }
      else if (name.indexOf(q) === 0) { prefix.push(codes[i]); }
      else if (name.indexOf(q) > 0) { fuzzy.push(codes[i]); }
      if (exact.length + prefix.length + fuzzy.length >= limit * 2) break;
    }
    return exact.concat(prefix).concat(fuzzy).slice(0, limit);
  }

  function createDropdown() {
    if (dropdown) return dropdown;
    dropdown = document.createElement("div");
    dropdown.className = "gosx-emoji-dropdown";
    dropdown.setAttribute("role", "listbox");
    dropdown.setAttribute("aria-label", "Emoji suggestions");
    dropdown.style.cssText = "position:fixed;z-index:10000;background:var(--bg,#1a1a18);border:1px solid var(--border,rgba(255,255,255,0.12));border-radius:0.5rem;padding:0.25rem;max-height:200px;overflow-y:auto;min-width:200px;box-shadow:0 4px 16px rgba(0,0,0,0.3);font-size:14px;display:none;";
    document.body.appendChild(dropdown);
    return dropdown;
  }

  function showDropdown(input, results, query) {
    var el = createDropdown();
    el.innerHTML = "";
    selectedIndex = 0;
    if (results.length === 0) { el.style.display = "none"; return; }
    for (var i = 0; i < results.length; i++) {
      var item = document.createElement("div");
      item.className = "gosx-emoji-item" + (i === 0 ? " gosx-emoji-selected" : "");
      item.setAttribute("role", "option");
      item.setAttribute("data-index", i);
      item.style.cssText = "padding:0.35rem 0.5rem;cursor:pointer;display:flex;align-items:center;gap:0.5rem;border-radius:0.25rem;";
      var emoji = document.createElement("span");
      emoji.textContent = results[i][1];
      emoji.style.fontSize = "1.2em";
      var label = document.createElement("span");
      label.style.cssText = "color:var(--text-secondary,rgba(255,255,255,0.7));";
      var name = results[i][0];
      var qi = name.indexOf(query);
      if (qi >= 0) {
        label.innerHTML = esc(name.slice(0, qi)) + "<strong style='color:var(--text-primary,rgba(255,255,255,0.92))'>" + esc(name.slice(qi, qi + query.length)) + "</strong>" + esc(name.slice(qi + query.length));
      } else {
        label.textContent = name;
      }
      item.appendChild(emoji);
      item.appendChild(label);
      (function(idx) {
        item.addEventListener("mousedown", function(e) {
          e.preventDefault();
          insertEmoji(input, results[idx]);
        });
        item.addEventListener("mouseenter", function() {
          selectItem(el, idx);
        });
      })(i);
      el.appendChild(item);
    }
    // Position near caret
    var rect = input.getBoundingClientRect();
    el.style.left = rect.left + "px";
    el.style.top = (rect.bottom + 4) + "px";
    el.style.display = "block";
  }

  function selectItem(el, idx) {
    var items = el.querySelectorAll(".gosx-emoji-item");
    for (var i = 0; i < items.length; i++) {
      items[i].className = "gosx-emoji-item" + (i === idx ? " gosx-emoji-selected" : "");
      if (i === idx) {
        items[i].style.background = "var(--surface-hover,rgba(255,255,255,0.06))";
      } else {
        items[i].style.background = "transparent";
      }
    }
    selectedIndex = idx;
  }

  function hideDropdown() {
    if (dropdown) dropdown.style.display = "none";
    triggerStart = -1;
  }

  function insertEmoji(input, entry) {
    var val = input.value;
    var end = input.selectionStart;
    var before = val.slice(0, triggerStart);
    var after = val.slice(end);
    var inserted = ":" + entry[0] + ": ";
    input.value = before + inserted + after;
    var newPos = before.length + inserted.length;
    input.setSelectionRange(newPos, newPos);
    input.focus();
    hideDropdown();
    input.dispatchEvent(new Event("input", { bubbles: true }));
  }

  function esc(s) {
    var d = document.createElement("span");
    d.textContent = s;
    return d.innerHTML;
  }

  function handleInput(e) {
    var input = e.target;
    if (!input || (input.tagName !== "TEXTAREA" && input.tagName !== "INPUT")) return;
    if (!input.hasAttribute("data-gosx-emoji-complete")) return;

    var val = input.value;
    var pos = input.selectionStart;
    // Find the last ':' before cursor
    var colonIdx = val.lastIndexOf(":", pos - 1);
    if (colonIdx < 0 || (colonIdx > 0 && val[colonIdx - 1] === ":")) {
      hideDropdown();
      return;
    }
    var query = val.slice(colonIdx + 1, pos);
    // Must be at least 2 chars, no spaces, no closing colon
    if (query.length < 2 || /\s/.test(query) || query.indexOf(":") >= 0) {
      hideDropdown();
      return;
    }
    triggerStart = colonIdx;
    activeInput = input;
    ensureCodes(function() {
      var results = search(query, 8);
      showDropdown(input, results, query);
    });
  }

  function handleKeydown(e) {
    if (!dropdown || dropdown.style.display === "none") return;
    var input = e.target;
    if (!input.hasAttribute("data-gosx-emoji-complete")) return;

    if (e.key === "ArrowDown") {
      e.preventDefault();
      var items = dropdown.querySelectorAll(".gosx-emoji-item");
      selectItem(dropdown, Math.min(selectedIndex + 1, items.length - 1));
      items[selectedIndex].scrollIntoView({ block: "nearest" });
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      selectItem(dropdown, Math.max(selectedIndex - 1, 0));
      dropdown.querySelectorAll(".gosx-emoji-item")[selectedIndex].scrollIntoView({ block: "nearest" });
    } else if (e.key === "Enter" || e.key === "Tab") {
      var items = dropdown.querySelectorAll(".gosx-emoji-item");
      if (items.length > 0 && dropdown.style.display !== "none") {
        e.preventDefault();
        var val = input.value;
        var pos = input.selectionStart;
        var query = val.slice(triggerStart + 1, pos);
        var results = search(query, 8);
        if (results[selectedIndex]) {
          insertEmoji(input, results[selectedIndex]);
        }
      }
    } else if (e.key === "Escape") {
      hideDropdown();
    }
  }

  function handleBlur() {
    setTimeout(hideDropdown, 150);
  }

  document.addEventListener("input", handleInput, true);
  document.addEventListener("keydown", handleKeydown, true);
  document.addEventListener("focusout", handleBlur, true);
})();
