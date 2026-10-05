// Shared enhanced request policy for full and isolated host transports.
// Resolve once using the same base as fetch, then keep automatic session
// credentials and token refreshes on the document's origin.
function gosxRequestURL(input) {
  return new URL(input.url || input, document.baseURI || window.location.href);
}
function gosxRefreshToken(response) {
  var token = response.headers && response.headers.get("X-CSRF-Token");
  if (token != null) {
    if (!document.querySelector('meta[name="csrf-token"]')) {
      var meta = document.createElement("meta");
      meta.setAttribute("name", "csrf-token");
      document.head.appendChild(meta);
    }
    document.querySelectorAll('meta[name="csrf-token"],input[name="csrf_token"]').forEach(function (el) {
      el.setAttribute(el.tagName === "META" ? "content" : "value", token);
      el.value = token;
    });
  }
  return response;
}

function gosxRequestToken() {
    const meta = document.querySelector && document.querySelector('meta[name="csrf-token"]');
    return meta ? String(meta.getAttribute("content") || "") : "";
  }

function gosxHeadersObject(headers) {
    const result = {};
    if (!headers) return result;
    if (typeof headers.forEach === "function") {
      headers.forEach((value, key) => { result[key] = value; });
      return result;
    }
    if (Array.isArray(headers)) {
      for (const entry of headers) {
        if (Array.isArray(entry) && entry.length >= 2) result[entry[0]] = entry[1];
      }
      return result;
    }
    for (const key of Object.keys(headers)) result[key] = headers[key];
    return result;
  }

function gosxHasHeader(headers, name) {
    const wanted = name.toLowerCase();
    return Object.keys(headers).some((key) => String(key).toLowerCase() === wanted);
  }

  async function gosxRequest(input, init) {
    if (typeof window.fetch !== "function") {
      return Promise.reject(new Error("fetch is not available"));
    }
    const url = gosxRequestURL(input);
    const sameOrigin = url.origin === window.location.origin;
    const options = Object.assign({}, init || {});
    const csrf = options.csrf !== false;
    delete options.csrf;
    const headers = gosxHeadersObject(options.headers);
    const method = options.method || (input && input.method) || "GET";
    if (csrf && sameOrigin && /^(POST|PUT|PATCH|DELETE)$/i.test(method) && !gosxHasHeader(headers, "X-CSRF-Token")) {
      const token = gosxRequestToken();
      if (token) headers["X-CSRF-Token"] = token;
    }
    if (Object.keys(headers).length > 0) options.headers = headers;
    return window.fetch(input.url ? input : url.href, options).then(function (response) {
      return sameOrigin && (!response.url || gosxRequestURL(response.url).origin === window.location.origin) ? gosxRefreshToken(response) : response;
    });
  }
