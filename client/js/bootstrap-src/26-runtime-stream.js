// 26-runtime-stream.js — bootstrap-owned streamed-template consumption.
//
// Server streaming normally executes the tiny replacement script emitted next
// to each template. During enhanced navigation, however, body nodes are
// cloned into the live document and cloned inline scripts do not execute.
// The declarative template contract makes the same stream safe in both paths.
(function () {
  "use strict";

  if (typeof window === "undefined" || typeof document === "undefined") return;
  if (window.__gosx_stream_templates) return;

  const selector = "template[data-gosx-stream-template][data-gosx-stream-target]";

  function observe(level, message, fields) {
    if (typeof window.__gosx_emit !== "function") return;
    window.__gosx_emit(level, "stream", message, fields || {});
  }

  function targetFor(template) {
    const id = String(template && template.getAttribute && template.getAttribute("data-gosx-stream-target") || "").trim();
    if (!id || typeof document.getElementById !== "function") return null;
    return document.getElementById(id);
  }

  function removeTemplate(template) {
    if (!template) return;
    if (typeof template.remove === "function") {
      template.remove();
    } else if (template.parentNode && typeof template.parentNode.removeChild === "function") {
      template.parentNode.removeChild(template);
    }
  }

  function replaceTarget(target, content) {
    const dom = window.__gosx && window.__gosx.dom;
    if (dom && typeof dom.replaceFragment === "function") {
      return dom.replaceFragment(target, content);
    }
    if (typeof window.__gosx_replace_runtime_fragment === "function") {
      return window.__gosx_replace_runtime_fragment(target, content);
    }
    if (!target || !target.parentNode) return false;
    if (typeof target.replaceWith === "function") {
      target.replaceWith(content);
      return true;
    }
    if (typeof target.parentNode.replaceChild === "function") {
      target.parentNode.replaceChild(content, target);
      return true;
    }
    return false;
  }

  function consume(root) {
    const host = root || document.body || document.documentElement;
    if (!host || typeof host.querySelectorAll !== "function") return 0;
    let consumed = 0;
    const templates = Array.from(host.querySelectorAll(selector));
    for (const template of templates) {
      const target = targetFor(template);
      if (!target || !target.parentNode) continue;
      const content = template.content && typeof template.content.cloneNode === "function"
        ? template.content.cloneNode(true)
        : null;
      if (!content) continue;
      if (!replaceTarget(target, content)) continue;
      removeTemplate(template);
      consumed += 1;
      observe("debug", "stream template consumed", { target: target.id || "" });
      if (typeof document.dispatchEvent === "function" && typeof CustomEvent === "function") {
        document.dispatchEvent(new CustomEvent("gosx:stream:consume", {
          detail: { target, template },
        }));
      }
    }
    return consumed;
  }

  window.__gosx_mount_stream_templates = consume;
  window.__gosx_stream_templates = {
    mount: consume,
    consume,
  };
  if (window.__gosx) {
    window.__gosx.stream = window.__gosx.stream || {};
    window.__gosx.stream.consume = consume;
  }
})();
