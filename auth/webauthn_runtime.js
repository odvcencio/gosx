(function () {
  if (window.GoSXWebAuthn) {
    return;
  }

  function b64ToBytes(value) {
    if (!value) return new Uint8Array();
    var normalized = String(value).replace(/-/g, "+").replace(/_/g, "/");
    while (normalized.length % 4) normalized += "=";
    var binary = atob(normalized);
    var out = new Uint8Array(binary.length);
    for (var i = 0; i < binary.length; i++) out[i] = binary.charCodeAt(i);
    return out;
  }

  function bytesToB64(value) {
    if (!value) return "";
    var bytes = value instanceof Uint8Array ? value : new Uint8Array(value);
    var binary = "";
    for (var i = 0; i < bytes.length; i++) binary += String.fromCharCode(bytes[i]);
    return btoa(binary).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/g, "");
  }

  function normalizeDescriptor(item) {
    return {
      type: item.type || "public-key",
      id: b64ToBytes(item.id),
      transports: item.transports || undefined,
    };
  }

  function normalizeCreateOptions(options) {
    var next = Object.assign({}, options);
    next.challenge = b64ToBytes(options.challenge);
    next.user = Object.assign({}, options.user, { id: b64ToBytes(options.user.id) });
    if (Array.isArray(options.excludeCredentials)) {
      next.excludeCredentials = options.excludeCredentials.map(normalizeDescriptor);
    }
    return next;
  }

  function normalizeRequestOptions(options) {
    var next = Object.assign({}, options);
    next.challenge = b64ToBytes(options.challenge);
    if (Array.isArray(options.allowCredentials)) {
      next.allowCredentials = options.allowCredentials.map(normalizeDescriptor);
    }
    return next;
  }

  async function fetchJSON(url, payload) {
    var body = Object.assign({}, payload || {});
    var csrfToken = body.csrfToken || body.csrf_token || "";
    delete body.csrfToken;
    delete body.csrf_token;
    var response = await fetch(url, {
      method: "POST",
      credentials: "same-origin",
      headers: {
        "Content-Type": "application/json",
        "Accept": "application/json",
        "X-CSRF-Token": csrfToken,
      },
      body: JSON.stringify(body),
    });
    var body = await response.json().catch(function () {
      return {};
    });
    if (!response.ok) {
      var message = body && body.error ? body.error : ("Request failed with status " + response.status);
      throw new Error(message);
    }
    return body;
  }

  function credentialToJSON(credential) {
    var response = credential.response || {};
    var payload = {
      id: credential.id,
      rawId: bytesToB64(credential.rawId),
      type: credential.type,
      response: {},
    };
    if (response.clientDataJSON) payload.response.clientDataJSON = bytesToB64(response.clientDataJSON);
    if (response.authenticatorData) payload.response.authenticatorData = bytesToB64(response.authenticatorData);
    if (response.signature) payload.response.signature = bytesToB64(response.signature);
    if (response.userHandle) payload.response.userHandle = bytesToB64(response.userHandle);
    if (typeof response.getPublicKey === "function") {
      var publicKey = response.getPublicKey();
      if (publicKey) payload.response.publicKey = bytesToB64(publicKey);
    }
    if (typeof response.getPublicKeyAlgorithm === "function") {
      payload.response.publicKeyAlgorithm = response.getPublicKeyAlgorithm();
    }
    if (typeof response.getTransports === "function") {
      payload.response.transports = response.getTransports();
    }
    return payload;
  }

  async function register(optionsURL, finishURL, payload) {
    var begin = await fetchJSON(optionsURL, payload);
    var credential = await navigator.credentials.create({
      publicKey: normalizeCreateOptions(begin.options),
    });
    return fetchJSON(finishURL, credentialToJSON(credential));
  }

  async function authenticate(optionsURL, finishURL, payload) {
    var begin = await fetchJSON(optionsURL, payload);
    var credential = await navigator.credentials.get({
      publicKey: normalizeRequestOptions(begin.options),
    });
    return fetchJSON(finishURL, credentialToJSON(credential));
  }

  window.GoSXWebAuthn = {
    register: register,
    authenticate: authenticate,
    b64ToBytes: b64ToBytes,
    bytesToB64: bytesToB64,
  };
})();
