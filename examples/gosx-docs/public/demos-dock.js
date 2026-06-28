(function() {
  function currentDemoSlug() {
    var path = String(window.location && window.location.pathname || "").replace(/\/$/, "");
    var marker = "/demos/";
    var index = path.indexOf(marker);
    if (index < 0) return "";
    return path.slice(index + marker.length).split("/")[0] || "";
  }

  function setDockOpen(body, button, open) {
    if (!body || !button) return;
    if (open) {
      body.setAttribute("data-dock-open", "true");
      button.setAttribute("aria-expanded", "true");
    } else {
      body.removeAttribute("data-dock-open");
      button.setAttribute("aria-expanded", "false");
    }
  }

  function syncDemoDock() {
    var shell = document.querySelector(".demos-shell");
    if (!shell) return;

    var slug = currentDemoSlug();
    if (slug) {
      shell.setAttribute("data-demo-slug", slug);
    }

    var links = shell.querySelectorAll(".demo-dock__link[data-demo]");
    for (var i = 0; i < links.length; i++) {
      var link = links[i];
      var item = link.closest ? link.closest("li") : null;
      var active = slug && link.getAttribute("data-demo") === slug;
      if (active) {
        link.setAttribute("aria-current", "page");
        if (item) item.setAttribute("aria-current", "page");
      } else {
        link.removeAttribute("aria-current");
        if (item) item.removeAttribute("aria-current");
      }
    }

    var body = shell.querySelector(".demos-body");
    var button = shell.querySelector(".demos-topbar__menu");
    var dock = shell.querySelector("#demo-dock");
    if (!body || !button || button.__gosxDemosDockBound) return;
    button.__gosxDemosDockBound = true;

    button.addEventListener("click", function() {
      setDockOpen(body, button, !body.hasAttribute("data-dock-open"));
    });
    if (dock) {
      dock.addEventListener("click", function(event) {
        var target = event.target;
        var link = target && target.closest ? target.closest(".demo-dock__link") : null;
        if (link) setDockOpen(body, button, false);
      });
    }
    document.addEventListener("keydown", function(event) {
      if (event && event.key === "Escape") {
        setDockOpen(body, button, false);
      }
    });
  }

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", syncDemoDock);
  } else {
    syncDemoDock();
  }
  document.addEventListener("gosx:navigate", syncDemoDock);
})();
