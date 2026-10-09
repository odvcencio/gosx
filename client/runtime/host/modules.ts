// Native ESM interop for Go-authored engines. The browser enforces module CSP
// and CORS; no source strings, inline scripts or eval bridges are constructed.
function gosxModuleImporter(load) {
  const pending = new Map();
  return function(specifier) {
    let url;
    try {
      if (typeof specifier !== "string" || !specifier.trim()) throw new Error("module URL is required");
      url = new URL(specifier, document.baseURI || window.location.href);
      if (url.protocol !== "https:" && url.protocol !== "http:") throw new Error("module URL must use HTTP or HTTPS");
      if (url.username || url.password) throw new Error("module URL must not contain credentials");
    } catch (error) {
      return Promise.reject(error);
    }
    const key = url.href;
    if (pending.has(key)) return pending.get(key);
    const result = Promise.resolve().then(function() { return load(key); });
    pending.set(key, result);
    result.catch(function() { if (pending.get(key) === result) pending.delete(key); });
    return result;
  };
}

gosxRuntime.modules = gosxRuntime.modules || {
  import: gosxModuleImporter(function(url) { return import(url); }),
};
