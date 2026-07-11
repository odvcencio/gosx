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
	var activeLink = null;
    for (var i = 0; i < links.length; i++) {
      var link = links[i];
      var item = link.closest ? link.closest("li") : null;
      var active = slug && link.getAttribute("data-demo") === slug;
      if (active) {
		activeLink = link;
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
	syncDetails(shell, activeLink);
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

  function syncDetails(shell, activeLink) {
	var openButton = shell.querySelector("[data-demo-details-open]");
	var drawer = shell.querySelector("#demo-details");
	var backdrop = shell.querySelector("[data-demo-details-backdrop]");
	if (!openButton || !drawer || !backdrop) return;

	var source = drawer.querySelector("[data-demo-details-source]");
	var values = {
	  title: activeLink && activeLink.getAttribute("data-demo-title"),
	  lesson: activeLink && activeLink.getAttribute("data-demo-lesson"),
	  facets: activeLink && activeLink.getAttribute("data-demo-facets"),
	  packages: activeLink && activeLink.getAttribute("data-demo-packages"),
	  renderMode: activeLink && activeLink.getAttribute("data-demo-render-mode"),
	  limitations: activeLink && activeLink.getAttribute("data-demo-limitations"),
	  sourcePath: activeLink && activeLink.getAttribute("data-demo-source-path"),
	  source: activeLink && activeLink.getAttribute("data-demo-source"),
	};
	setText(drawer, "#demo-details-title", values.title || "Demo details");
	setText(drawer, "[data-demo-details-lesson]", values.lesson || "Choose a demo to inspect how it is built.");
	setText(drawer, "[data-demo-details-facets]", values.facets || "—");
	setText(drawer, "[data-demo-details-packages]", values.packages || "—");
	setText(drawer, "[data-demo-details-render-mode]", values.renderMode || "—");
	setText(drawer, "[data-demo-details-limitations]", values.limitations || "—");
	setText(drawer, "[data-demo-details-source-path]", values.sourcePath || "");
	if (source) {
	  if (values.source) {
		source.setAttribute("href", values.source);
		source.removeAttribute("aria-disabled");
	  } else {
		source.removeAttribute("href");
		source.setAttribute("aria-disabled", "true");
	  }
	}

	if (openButton.__gosxDemoDetailsBound) return;
	openButton.__gosxDemoDetailsBound = true;
	var closeButton = drawer.querySelector("[data-demo-details-close]");
	var previousFocus = null;

	function closeDetails() {
	  if (drawer.hidden) return;
	  drawer.hidden = true;
	  backdrop.hidden = true;
	  openButton.setAttribute("aria-expanded", "false");
	  document.documentElement.removeAttribute("data-demo-details-open");
	  if (previousFocus && typeof previousFocus.focus === "function") previousFocus.focus();
	}

	function openDetails() {
	  previousFocus = document.activeElement;
	  drawer.hidden = false;
	  backdrop.hidden = false;
	  openButton.setAttribute("aria-expanded", "true");
	  document.documentElement.setAttribute("data-demo-details-open", "true");
	  if (closeButton) closeButton.focus();
	}

	openButton.addEventListener("click", openDetails);
	if (closeButton) closeButton.addEventListener("click", closeDetails);
	backdrop.addEventListener("click", closeDetails);
	drawer.addEventListener("keydown", function(event) {
	  if (!event) return;
	  if (event.key === "Escape") {
		event.preventDefault();
		closeDetails();
		return;
	  }
	  if (event.key !== "Tab") return;
	  var focusable = drawer.querySelectorAll('a[href], button:not([disabled]), [tabindex]:not([tabindex="-1"])');
	  if (!focusable.length) return;
	  var first = focusable[0];
	  var last = focusable[focusable.length - 1];
	  if (event.shiftKey && document.activeElement === first) {
		event.preventDefault();
		last.focus();
	  } else if (!event.shiftKey && document.activeElement === last) {
		event.preventDefault();
		first.focus();
	  }
	});
	document.addEventListener("keydown", function(event) {
	  if (event && event.key === "Escape") closeDetails();
	});
  }

  function setText(root, selector, value) {
	var element = root.querySelector(selector);
	if (element) element.textContent = value;
  }

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", syncDemoDock);
  } else {
    syncDemoDock();
  }
  document.addEventListener("gosx:navigate", syncDemoDock);
})();
