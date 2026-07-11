(function () {
  "use strict";

  // Tunables
  var DEBOUNCE_MS = 200;
  var RUNTIME_TIMEOUT_MS = 8000;

  function ready(fn) {
    if (document.readyState === "loading") {
      document.addEventListener("DOMContentLoaded", fn);
    } else {
      fn();
    }
  }

  function waitForRuntime() {
    return new Promise(function (resolve, reject) {
      if (typeof window.__gosx_hydrate === "function") {
        return resolve();
      }
      var interval = setInterval(function () {
        if (typeof window.__gosx_hydrate === "function") {
          clearInterval(interval);
          resolve();
        }
      }, 50);
      setTimeout(function () {
        clearInterval(interval);
        reject(new Error("GoSX runtime did not become ready"));
      }, RUNTIME_TIMEOUT_MS);
    });
  }

  function base64ToBytes(b64) {
    var binary = atob(b64);
    var len = binary.length;
    var bytes = new Uint8Array(len);
    for (var i = 0; i < len; i++) {
      bytes[i] = binary.charCodeAt(i);
    }
    return bytes;
  }

  function mount() {
    var shell = document.querySelector(".play");
    if (!shell) {
      return;
    }

    var compileURL = shell.getAttribute("data-compile-url");
    var csrfToken = shell.getAttribute("data-csrf-token") || "";
    var textarea = shell.querySelector(".play__source");
    var preview = shell.querySelector(".play__preview-frame [data-gosx-island]");
    var errors = shell.querySelector(".play__errors");
    var compilerBody = shell.querySelector(".play__compiler-body");
    var picker = shell.querySelector(".play__preset-select");
    var presetDescription = shell.querySelector(".play__preset-description");

    if (!compileURL || !textarea || !preview) {
      return;
    }
    var islandID = preview.getAttribute("data-gosx-island");

    var timer = null;
    var requestGeneration = 0;
    var activeController = null;
    var status = shell.querySelector(".play__preview-status");

    function setStatus(state, message) {
      shell.setAttribute("data-playground-state", state);
      if (status) status.textContent = message;
    }

    function showErrors(diagnostics) {
      if (!errors) {
        return;
      }
      if (!diagnostics || diagnostics.length === 0) {
        errors.textContent = "";
        errors.removeAttribute("data-active");
        return;
      }
      errors.setAttribute("data-active", "true");
      errors.textContent = diagnostics
        .map(function (d) {
          var prefix = d.line > 0 ? d.line + ":" + d.column + " " : "";
          return prefix + d.message;
        })
        .join("\n");
    }

    function showCompilerInfo(data) {
      if (!compilerBody) {
        return;
      }
      var programInfo = data.program
        ? "program: " +
          Math.ceil(data.program.length * 0.75) +
          " bytes (base64 " +
          data.program.length +
          " chars)"
        : "program: none";
      var diagsN = (data.diagnostics || []).length;
      compilerBody.textContent = programInfo + "\ndiagnostics: " + diagsN;
    }

    function compile(source) {
      var generation = ++requestGeneration;
      if (activeController) activeController.abort();
      activeController = typeof AbortController === "function" ? new AbortController() : null;
      setStatus("compiling", "Compiling GoSX…");
      fetch(compileURL, {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          Accept: "application/json",
          "X-CSRF-Token": csrfToken,
          "X-Requested-With": "gosx-playground",
        },
        credentials: "same-origin",
        body: JSON.stringify({ source: source }),
        signal: activeController ? activeController.signal : undefined,
      })
        .then(function (resp) {
          if (generation !== requestGeneration) return null;
          if (!resp.ok) {
            var error = new Error(resp.status === 429 ? "Rate limited — pause for a moment" : "Compile request failed (" + resp.status + ")");
            error.rateLimited = resp.status === 429;
            throw error;
          }
          return resp.json();
        })
        .then(function (result) {
          if (generation !== requestGeneration || !result) return;
          if (!result || !result.ok || !result.data) {
            showErrors([
              {
                line: 0,
                column: 0,
                message:
                  (result && result.message) || "compile failed",
              },
            ]);
            setStatus("network-error", "Compile request failed");
            return;
          }
          var data = result.data;
          showErrors(data.diagnostics || []);
          showCompilerInfo(data);
          if (data.diagnostics && data.diagnostics.length > 0) {
            var diagnosticText = data.diagnostics.map(function (d) { return d.message || ""; }).join(" ");
            var isRateLimited = /rate limit|too many requests/i.test(diagnosticText);
            setStatus(isRateLimited ? "rate-limited" : "diagnostic", isRateLimited ? "Rate limited — pause for a moment" : "Fix the diagnostic to update preview");
            return;
          }
          if (!data.program) {
            setStatus("diagnostic", "No runnable island program");
            return;
          }
          if (!data.component) {
            showErrors([{ line: 0, column: 0, message: "compiler did not return a component identity" }]);
            setStatus("diagnostic", "Missing component identity");
            return;
          }
          var bytes = base64ToBytes(data.program);
          // Dispose the previous island instance before mounting the new one
          // to avoid leaking signal subscriptions and DOM event listeners.
          try {
            if (typeof window.__gosx_dispose === "function") {
              window.__gosx_dispose(islandID);
            }
          } catch (e) {
            // Ignore dispose errors — a missing island is fine on first run.
          }
          var ret = window.__gosx_hydrate(
            islandID,
            data.component,
            "{}",
            bytes,
            "bin"
          );
          if (typeof ret === "string" && ret.indexOf("error:") === 0) {
            showErrors([
              {
                line: 0,
                column: 0,
                message: ret.replace(/^error:\s*/, ""),
              },
            ]);
            setStatus("diagnostic", "Hydration failed");
            return;
          }
          preview.setAttribute("data-component", data.component);
          setStatus("hydrated", data.component + " hydrated from GoSX island bytecode");
        })
        .catch(function (err) {
          if (generation !== requestGeneration || (err && err.name === "AbortError")) return;
	          showErrors([
	            {
	              line: 0,
	              column: 0,
	              message: ((err && err.message) || "network error"),
	            },
	            ]);
          setStatus(err && err.rateLimited ? "rate-limited" : "network-error", err && err.rateLimited ? "Rate limited — pause for a moment" : "Network error — preview unchanged");
        });
    }

    function scheduleCompile() {
      if (timer) {
        clearTimeout(timer);
      }
      timer = setTimeout(function () {
        compile(textarea.value);
      }, DEBOUNCE_MS);
    }

    textarea.addEventListener("input", scheduleCompile);

    // Preset picker: when the user selects a new preset, update the textarea
    // and trigger an immediate compile (no debounce — the user took a
    // deliberate action).
    if (picker) {
      function updatePresetDescription(opt) {
        if (presetDescription && opt) {
          presetDescription.textContent = opt.getAttribute("data-description") || "";
        }
      }
      picker.addEventListener("change", function () {
        var opt = picker.options[picker.selectedIndex];
        if (!opt) {
          return;
        }
        var src = opt.getAttribute("data-source");
        if (src == null) {
          return;
        }
        textarea.value = src;
        updatePresetDescription(opt);
        compile(src);
      });
    }

    // Reset button: reload the source from the currently selected preset
    // option and trigger a compile.
    var resetBtn = shell.querySelector(".play__reset-btn");
    if (resetBtn) {
      resetBtn.addEventListener("click", function () {
        if (!picker) {
          return;
        }
        var opt = picker.options[picker.selectedIndex];
        if (!opt) {
          return;
        }
        var src = opt.getAttribute("data-source");
        if (src == null) {
          return;
        }
        textarea.value = src;
        compile(src);
      });
    }

    // Initial compile: run once after the GoSX wasm runtime has loaded so the
    // preview shows the default preset immediately on page load.
    setStatus("waiting", "Waiting for the GoSX runtime…");
    waitForRuntime()
      .then(function () { compile(textarea.value); })
      .catch(function (err) {
        showErrors([{ line: 0, column: 0, message: err.message }]);
        setStatus("runtime-error", "GoSX runtime unavailable");
      });
  }

  ready(mount);
})();
