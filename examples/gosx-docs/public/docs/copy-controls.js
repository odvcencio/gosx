(function () {
	var root = document.documentElement;
	var selector = "[data-gosx-copy-button]";
	if (!root || window.__gosxDocsCopyControlsInstalled) return;

	function copyFallback(text) {
		var field = document.createElement("textarea");
		field.value = text;
		field.setAttribute("readonly", "");
		field.setAttribute("aria-hidden", "true");
		field.style.cssText = "position:fixed;opacity:0;pointer-events:none";
		(document.body || document.documentElement).appendChild(field);
		try {
			field.select();
			return typeof document.execCommand === "function" && document.execCommand("copy");
		} finally {
			field.remove();
		}
	}

	function copyCode(button) {
		var scope = button.closest("[data-gosx-copy-scope]");
		var code = scope && scope.querySelector("pre code");
		if (!code) return;
		var status = scope.querySelector("[data-gosx-copy-status]");
		var label = button.getAttribute("data-gosx-copy-label") || "Copy";
		var text = code.textContent || "";
		var finish = function (copied) {
			button.disabled = false;
			button.textContent = copied ? "Copied" : "Try again";
			if (status) status.textContent = copied
				? "Copied code to the clipboard."
				: "Copy failed. Select the code and copy it manually.";
			setTimeout(function () {
				button.textContent = label;
				if (status) status.textContent = "";
			}, 1800);
		};

		button.disabled = true;
		button.textContent = "Copying…";
		if (status) status.textContent = "Copying code.";

		var clipboard = window.navigator && window.navigator.clipboard;
		var fallbackIsPrimary = !(clipboard && typeof clipboard.writeText === "function");
		var result;
		try {
			result = !fallbackIsPrimary
				? clipboard.writeText(text)
				: Promise.resolve(copyFallback(text));
		} catch (error) {
			result = Promise.reject(error);
		}
		Promise.resolve(result).then(function (copied) {
			finish(fallbackIsPrimary ? !!copied : true);
		}, function () {
			var fallbackWorked = false;
			try {
				fallbackWorked = copyFallback(text);
			} catch (_) {}
			finish(fallbackWorked);
		});
	}

	document.addEventListener("click", function (event) {
		var target = event.target;
		var button = target && typeof target.closest === "function" ? target.closest(selector) : null;
		if (!button || root.getAttribute("data-gosx-runtime-ready") === "true") return;
		event.preventDefault();
		event.stopImmediatePropagation();
		if (!button.disabled) copyCode(button);
	});

	window.__gosxDocsCopyControlsInstalled = true;
	root.setAttribute("data-gosx-copy-ready", "true");
})();
