// @ts-check
// GoSX browser host: accessible disclosure and modal authority.
//
// This file is intentionally shipped by both the fetched bootstrap bundles and
// app.EnableNavigation's inline runtime. The public namespace guard is the
// ownership boundary: whichever path evaluates first installs the one set of
// delegated listeners; every later evaluation reuses that exact authority.
(function () {
  var installed = window.__gosx.disclosure;
  if (installed && installed.__installed === true) {
    gosxHost.disclosure = installed;
    return;
  }

  var stack = [];
  var records = new Map();
  var FOCUSABLE_SELECTOR = 'a[href], button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])';

  function targetFor(selector) {
    if (!selector || typeof document.querySelector !== "function") return null;
    try { return document.querySelector(selector); } catch (_) { return null; }
  }

  function selectorForPanel(panel) {
    var id = panel && panel.getAttribute && panel.getAttribute("id");
    return id ? "#" + id : "";
  }

  function panelFor(subject) {
    if (!subject || !subject.getAttribute) return null;
    var selector = subject.getAttribute("data-gosx-disclosure-target") ||
      subject.getAttribute("data-gosx-disclosure-close") ||
      subject.getAttribute("data-gosx-disclosure-backdrop");
    if (selector) return targetFor(selector);
    return subject.hasAttribute && subject.hasAttribute("data-gosx-disclosure") ? subject : null;
  }

  function connected(node) {
    if (!node) return false;
    if (typeof node.isConnected === "boolean") return node.isConnected;
    if (typeof document.contains === "function") return document.contains(node);
    return true;
  }

  function backdropsFor(panel) {
    var selector = selectorForPanel(panel);
    if (!selector || typeof document.querySelectorAll !== "function") return [];
    try {
      return Array.prototype.slice.call(document.querySelectorAll('[data-gosx-disclosure-backdrop="' + selector + '"]'));
    } catch (_) {
      return [];
    }
  }

  function setHidden(element, hidden) {
    if (!element) return;
    element.hidden = hidden;
    if (!element.setAttribute || !element.removeAttribute) return;
    if (hidden) element.setAttribute("hidden", "");
    else element.removeAttribute("hidden");
  }

  function setDisclosureHidden(panel, hidden) {
    if (!panel) return;
    setHidden(panel, hidden);
    backdropsFor(panel).forEach(function (backdrop) { setHidden(backdrop, hidden); });
  }

  function triggerFor(panel, record) {
    if (record && connected(record.trigger)) return record.trigger;
    var selector = selectorForPanel(panel);
    return selector ? targetFor('[data-gosx-disclosure-target="' + selector + '"]') : null;
  }

  function childrenOf(node) {
    return Array.prototype.slice.call(node && node.children || []);
  }

  function contains(root, node) {
    if (!root || !node) return false;
    if (root === node) return true;
    return typeof root.contains === "function" && root.contains(node);
  }

  function rememberAndInert(element, record) {
    if (!element || !element.setAttribute) return;
    var hadAttribute = element.hasAttribute && element.hasAttribute("inert");
    var attributeValue = hadAttribute ? element.getAttribute("inert") : null;
    var hadProperty = "inert" in element;
    var propertyValue = hadProperty ? element.inert : undefined;
    record.inert.push({
      element: element,
      hadAttribute: hadAttribute,
      attributeValue: attributeValue,
      hadProperty: hadProperty,
      propertyValue: propertyValue,
    });
    element.inert = true;
    element.setAttribute("inert", "");
  }

  function inertModalBackground(panel, record) {
    if (!document.body) return;
    var allowed = [panel].concat(backdropsFor(panel));
    function visit(container) {
      childrenOf(container).forEach(function (child) {
        var exact = allowed.indexOf(child) >= 0;
        var path = allowed.some(function (node) { return contains(child, node); });
        if (exact) return;
        if (path) {
          visit(child);
          return;
        }
        rememberAndInert(child, record);
      });
    }
    visit(document.body);
  }

  function restoreModalBackground(record) {
    for (var index = record.inert.length - 1; index >= 0; index -= 1) {
      var saved = record.inert[index];
      var element = saved.element;
      if (!element) continue;
      if (saved.hadProperty) element.inert = saved.propertyValue;
      else {
        try { delete element.inert; } catch (_) { element.inert = false; }
      }
      if (!element.setAttribute || !element.removeAttribute) continue;
      if (saved.hadAttribute) element.setAttribute("inert", saved.attributeValue == null ? "" : saved.attributeValue);
      else element.removeAttribute("inert");
    }
    record.inert = [];
  }

  function focusables(panel) {
    if (!panel || !panel.querySelectorAll) return [];
    return Array.prototype.slice.call(panel.querySelectorAll(FOCUSABLE_SELECTOR)).filter(function (node) {
      return connected(node) && !node.disabled && !node.hidden;
    });
  }

  function focusPanel(panel, record) {
    if (!panel || typeof panel.focus !== "function") return;
    if (panel.hasAttribute && panel.hasAttribute("tabindex")) {
      record.panelTabindexHad = true;
      record.panelTabindex = panel.getAttribute("tabindex");
    } else if (panel.setAttribute) {
      panel.setAttribute("tabindex", "-1");
      record.panelTabindexAdded = true;
    }
    panel.focus();
  }

  function focusInitial(panel, record) {
    var initial = panel.querySelector && panel.querySelector("[data-gosx-disclosure-initial-focus]");
    if (initial && connected(initial) && typeof initial.focus === "function") {
      initial.focus();
      return;
    }
    var first = focusables(panel)[0];
    if (first && typeof first.focus === "function") {
      first.focus();
      return;
    }
    focusPanel(panel, record);
  }

  function restorePanelTabindex(panel, record) {
    if (!panel || !panel.setAttribute || !panel.removeAttribute) return;
    if (record.panelTabindexAdded) panel.removeAttribute("tabindex");
    else if (record.panelTabindexHad) panel.setAttribute("tabindex", record.panelTabindex == null ? "" : record.panelTabindex);
  }

  function removeFromStack(panel) {
    stack = stack.filter(function (candidate) { return candidate !== panel; });
  }

  function topmost() {
    while (stack.length > 0) {
      var panel = stack[stack.length - 1];
      if (records.has(panel) && connected(panel)) return panel;
      cleanupPanel(panel);
    }
    return null;
  }

  function open(subject) {
    var panel = panelFor(subject);
    if (!panel) return false;
    var existing = records.get(panel);
    if (existing) {
      removeFromStack(panel);
      stack.push(panel);
      focusInitial(panel, existing);
      return true;
    }
    var trigger = subject && subject.getAttribute && subject.getAttribute("data-gosx-disclosure-target") ? subject : null;
    var record = {
      panel: panel,
      trigger: trigger,
      previousFocus: document.activeElement || trigger,
      inert: [],
      panelTabindexAdded: false,
      panelTabindexHad: false,
      panelTabindex: null,
    };
    records.set(panel, record);
    stack.push(panel);
    setDisclosureHidden(panel, false);
    if (trigger && trigger.setAttribute) trigger.setAttribute("aria-expanded", "true");
    if (panel.getAttribute && (panel.getAttribute("aria-modal") === "true" || panel.hasAttribute("data-gosx-disclosure-modal"))) {
      inertModalBackground(panel, record);
    }
    focusInitial(panel, record);
    return true;
  }

  function close(subject, options) {
    var panel = panelFor(subject) || subject;
    var record = panel && records.get(panel);
    if (!panel || !record) return false;
    var restoreFocus = !options || options.restoreFocus !== false;
    setDisclosureHidden(panel, true);
    restoreModalBackground(record);
    restorePanelTabindex(panel, record);
    var trigger = triggerFor(panel, record);
    if (trigger && trigger.setAttribute) trigger.setAttribute("aria-expanded", "false");
    removeFromStack(panel);
    records.delete(panel);
    var previous = record.previousFocus || trigger;
    if (restoreFocus && connected(previous) && typeof previous.focus === "function") previous.focus();
    return true;
  }

  function cleanupPanel(panel) {
    var record = panel && records.get(panel);
    if (!record) {
      removeFromStack(panel);
      return;
    }
    restoreModalBackground(record);
    restorePanelTabindex(panel, record);
    var trigger = triggerFor(panel, record);
    if (trigger && trigger.setAttribute) trigger.setAttribute("aria-expanded", "false");
    removeFromStack(panel);
    records.delete(panel);
  }

  function closeAll(restoreFocus) {
    var panels = stack.slice().reverse();
    panels.forEach(function (panel) { close(panel, { restoreFocus: restoreFocus }); });
  }

  function clickHandler(event) {
    var target = event && event.target;
    if (!target || !target.closest) return;
    var closer = target.closest("[data-gosx-disclosure-close], [data-gosx-disclosure-backdrop]");
    if (closer) {
      if (closer.tagName !== "A" || !closer.getAttribute("href")) event.preventDefault();
      close(panelFor(closer));
      return;
    }
    var trigger = target.closest("[data-gosx-disclosure-target]");
    if (!trigger) return;
    event.preventDefault();
    open(trigger);
  }

  function keydownHandler(event) {
    if (!event) return;
    var panel = topmost();
    if (!panel) return;
    if (event.key === "Escape") {
      event.preventDefault();
      close(panel);
      return;
    }
    if (event.key !== "Tab") return;
    var items = focusables(panel);
    if (items.length === 0) {
      event.preventDefault();
      focusPanel(panel, records.get(panel));
      return;
    }
    var first = items[0];
    var last = items[items.length - 1];
    var active = document.activeElement;
    if (!contains(panel, active)) {
      event.preventDefault();
      (event.shiftKey ? last : first).focus();
    } else if (event.shiftKey && active === first) {
      event.preventDefault();
      last.focus();
    } else if (!event.shiftKey && active === last) {
      event.preventDefault();
      first.focus();
    }
  }

  function navigationHandler() {
    closeAll(false);
  }

  if (typeof document !== "undefined" && typeof document.addEventListener === "function") {
    document.addEventListener("click", clickHandler, true);
    document.addEventListener("keydown", keydownHandler, true);
    document.addEventListener("gosx:navigate", navigationHandler);
  }

  var api = {
    __installed: true,
    open: open,
    close: close,
    closeAll: function () { closeAll(false); },
    top: topmost,
    size: function () { return stack.length; },
  };
  window.__gosx.disclosure = api;
  gosxHost.disclosure = api;
})();
