// Immutable Scene3D geometry references. Shared by initial hydration and commands.
(function () {
  const host = window as any;
  if (host.__gosx_scene3d_assets) return;
  const cache = new Map<string, { data: any; bytes: number }>();
  const pending = new Map<string, Promise<any>>();
  const maxAssetBytes = 16 << 20;
  const maxCacheBytes = 64 << 20;
  let cacheBytes = 0;

  function validate(data: any): any {
    if (!data || !Number.isInteger(data.count) || data.count < 0 || data.count > 1000000) throw new Error("Invalid Scene3D geometry count");
    const count = data.count;
    for (const [name, width] of [["positions", 3], ["normals", 3], ["uvs", 2], ["tangents", 4]] as [string, number][]) {
      const values = data[name];
      if (values == null && (name !== "positions" || count === 0)) continue;
      if (!Array.isArray(values) || values.length !== count * width || !values.every(Number.isFinite)) throw new Error("Invalid Scene3D geometry " + name);
    }
    if (data.indices != null && (!Array.isArray(data.indices) || data.indices.length % 3 !== 0 || !data.indices.every((i: any) => Number.isInteger(i) && i >= 0 && i < count))) throw new Error("Invalid Scene3D geometry indices");
    if (data.attributes != null) {
      if (typeof data.attributes !== "object" || Array.isArray(data.attributes)) throw new Error("Invalid Scene3D geometry attributes");
      for (const attr of Object.values(data.attributes) as any[]) {
        if (!attr || !Number.isInteger(attr.itemSize) || attr.itemSize < 1 || attr.itemSize > 4 || !Array.isArray(attr.data) || attr.data.length !== count * attr.itemSize || !attr.data.every(Number.isFinite)) throw new Error("Invalid Scene3D geometry attribute");
      }
    }
    return data;
  }

  async function read(url: string): Promise<any> {
    const response = await fetch(url, { credentials: "same-origin", redirect: "error" });
    if (!response.ok) throw new Error("Scene3D geometry fetch failed: " + response.status);
    if (Number(response.headers.get("content-length")) > maxAssetBytes) throw new Error("Scene3D geometry exceeds asset budget");
    let text: string;
    if (response.body && typeof response.body.getReader === "function") {
      const reader = response.body.getReader();
      const decoder = new TextDecoder();
      let size = 0;
      const chunks: string[] = [];
      try {
        for (;;) {
          const item = await reader.read();
          if (item.done) break;
          size += item.value.byteLength;
          if (size > maxAssetBytes) { await reader.cancel(); throw new Error("Scene3D geometry exceeds asset budget"); }
          chunks.push(decoder.decode(item.value, { stream: true }));
        }
        chunks.push(decoder.decode());
        text = chunks.join("");
      } finally { reader.releaseLock(); }
    } else {
      text = await response.text();
      if (text.length > maxAssetBytes) throw new Error("Scene3D geometry exceeds asset budget");
    }
    const data = validate(JSON.parse(text));
    const bytes = text.length * 2;
    while (cacheBytes + bytes > maxCacheBytes && cache.size) {
      const key = cache.keys().next().value!;
      cacheBytes -= cache.get(key)!.bytes;
      cache.delete(key);
    }
    if (bytes <= maxCacheBytes) { cache.set(url, { data, bytes }); cacheBytes += bytes; }
    return data;
  }

  function load(value: string): Promise<any> {
    const url = new URL(value, window.location.href);
    if (!/^https?:$/.test(url.protocol) || url.origin !== window.location.origin || url.username || url.password) return Promise.reject(new Error("Scene3D geometry must use a same-origin HTTP URL"));
    const key = url.href;
    const hit = cache.get(key);
    if (hit) { cache.delete(key); cache.set(key, hit); return Promise.resolve(hit.data); }
    let work = pending.get(key);
    if (!work) {
      work = read(key).finally(() => pending.delete(key));
      pending.set(key, work);
    }
    return work;
  }

  async function object(value: any): Promise<any> {
    if (!value || !value.verticesURL || value.vertices) return value;
    const vertices = await load(value.verticesURL);
    return Object.assign({}, value, { vertices });
  }
  host.__gosx_scene3d_assets = {
    resolveObjects: (objects: any[]) => Promise.all((objects || []).map(object)),
    resolveCommands: (commands: any[]) => Promise.all(commands.map(async (command: any) => {
      const data = command && command.data;
      if (!data || !data.props || !data.props.verticesURL || data.props.vertices) return command;
      return Object.assign({}, command, { data: Object.assign({}, data, { props: await object(data.props) }) });
    })),
  };
})();
