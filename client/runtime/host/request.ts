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
    return headers && typeof headers[Symbol.iterator] === "function"
      ? Object.fromEntries(headers)
      : Object.assign({}, headers);
  }

  async function gosxRequest(input, init) {
    if (typeof window.fetch !== "function") {
      throw new Error("fetch is not available");
    }
    const url = gosxRequestURL(input);
    const sameOrigin = url.origin === window.location.origin;
    const options = Object.assign({}, init);
    const csrf = options.csrf !== false;
    delete options.csrf;
    const headers = gosxHeadersObject(options.headers);
    const method = options.method || (input && input.method) || "GET";
    if (csrf && sameOrigin && /^(POST|PUT|PATCH|DELETE)$/i.test(method) && !Object.keys(headers).some((key) => key.toLowerCase() === "x-csrf-token")) {
      const token = gosxRequestToken();
      if (token) headers["X-CSRF-Token"] = token;
    }
    if (Object.keys(headers).length > 0) options.headers = headers;
    return window.fetch(input.url ? input : url.href, options).then(function (response) {
      return sameOrigin && (!response.url || gosxRequestURL(response.url).origin === window.location.origin) ? gosxRefreshToken(response) : response;
    });
  }
