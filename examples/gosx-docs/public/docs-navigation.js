(function () {
  "use strict";
  if (window.__gosxDocsNavigationInstalled) return;
  window.__gosxDocsNavigationInstalled = true;

  document.addEventListener("keydown", function (event) {
    if (event.key !== "Escape" || window.location.hash !== "#nav-overlay") return;
    event.preventDefault();
    window.location.hash = "main-content";
    const opener = document.querySelector('[aria-controls="nav-overlay"]');
    if (opener) opener.focus();
  });
})();
