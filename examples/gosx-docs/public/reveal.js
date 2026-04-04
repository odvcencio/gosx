(function () {
  'use strict';

  var THRESHOLD = 0.15;
  var STAGGER_MS = 80;
  var MAX_STAGGER = 6;

  if (window.matchMedia('(prefers-reduced-motion: reduce)').matches) {
    document.querySelectorAll('.reveal').forEach(function (el) {
      el.classList.add('visible');
    });
    return;
  }

  var observer = new IntersectionObserver(function (entries) {
    entries.forEach(function (entry) {
      if (!entry.isIntersecting) return;
      var el = entry.target;
      observer.unobserve(el);

      if (!el.hasAttribute('data-reveal-stagger')) {
        el.classList.add('visible');
        return;
      }

      var children = el.querySelectorAll('.reveal');
      var count = Math.min(children.length, MAX_STAGGER);
      children.forEach(function (child, i) {
        var delay = Math.min(i, count - 1) * STAGGER_MS;
        if (delay === 0) {
          child.classList.add('visible');
        } else {
          setTimeout(function () { child.classList.add('visible'); }, delay);
        }
      });
      el.classList.add('visible');
    });
  }, { threshold: THRESHOLD });

  function observe() {
    document.querySelectorAll('.reveal:not(.visible)').forEach(function (el) {
      observer.observe(el);
    });
  }

  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', observe);
  } else {
    observe();
  }

  document.addEventListener('gosx:navigate', observe);
})();
