(function () {
  "use strict";

  // Tunables
  var DEBOUNCE_MS = 200;
  var ISLAND_ID = "playground-preview";

  function ready(fn) {
    if (document.readyState === "loading") {
      document.addEventListener("DOMContentLoaded", fn);
    } else {
      fn();
    }
  }

  function waitForRuntime() {
    return new Promise(function (resolve) {
      if (typeof window.__gosx_hydrate === "function") {
        return resolve();
      }
      var interval = setInterval(function () {
        if (typeof window.__gosx_hydrate === "function") {
          clearInterval(interval);
          resolve();
        }
      }, 50);
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
    var preview = shell.querySelector('[data-gosx-island="' + ISLAND_ID + '"]');
    var errors = shell.querySelector(".play__errors");
    var compilerBody = shell.querySelector(".play__compiler-body");
    var picker = shell.querySelector(".play__preset-select");

    if (!compileURL || !textarea || !preview) {
      return;
    }

    var timer = null;
    var inFlight = false;
    // Track the last successfully hydrated component name so subsequent
    // recompiles of the same source can reuse it. Falls back to "Page".
    var latestComponent = "Page";

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
      if (inFlight) {
        return;
      }
      inFlight = true;
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
      })
        .then(function (resp) {
          return resp.json();
        })
        .then(function (result) {
          if (!result || !result.ok || !result.data) {
            showErrors([
              {
                line: 0,
                column: 0,
                message:
                  (result && result.message) || "compile failed",
              },
            ]);
            return;
          }
          var data = result.data;
          showErrors(data.diagnostics || []);
          showCompilerInfo(data);
          if (data.diagnostics && data.diagnostics.length > 0) {
            return;
          }
          if (!data.program) {
            return;
          }
          var bytes = base64ToBytes(data.program);
          // Dispose the previous island instance before mounting the new one
          // to avoid leaking signal subscriptions and DOM event listeners.
          try {
            if (typeof window.__gosx_dispose === "function") {
              window.__gosx_dispose(ISLAND_ID);
            }
          } catch (e) {
            // Ignore dispose errors — a missing island is fine on first run.
          }
          // The component name is not echoed by the compile endpoint, so we
          // reuse the last known name. The preview frame carries
          // data-component after the first hydration; read it from there.
          var frame = shell.querySelector(
            '[data-gosx-island="' + ISLAND_ID + '"]'
          );
          if (frame && frame.getAttribute("data-component")) {
            latestComponent = frame.getAttribute("data-component");
          }
          var ret = window.__gosx_hydrate(
            ISLAND_ID,
            latestComponent,
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
          }
        })
        .catch(function (err) {
          showErrors([
            {
              line: 0,
              column: 0,
              message: "network error: " + ((err && err.message) || err),
            },
          ]);
        })
        .then(function () {
          inFlight = false;
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
    waitForRuntime().then(function () {
      compile(textarea.value);
    });
  }

  ready(mount);
})();
