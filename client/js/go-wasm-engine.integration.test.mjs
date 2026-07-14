import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import vm from "node:vm";
import { execFileSync } from "node:child_process";
import { fileURLToPath } from "node:url";

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);
const repoRoot = path.resolve(__dirname, "..", "..");

test("engine/wasm registers and disposes through the version-matched standard-Go runtime", async () => {
  const tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "gosx-go-wasm-engine-"));
  const wasmPath = path.join(tempDir, "fixture.wasm");
  try {
    const loaderSource = fs.readFileSync(
      path.join(repoRoot, "client", "js", "bootstrap-src", "30-tail.js"),
      "utf8",
    );
    const envContract = loaderSource.match(
      /goWASMEngineRegistrationTokenEnv\s*=\s*"([^"]+)"/,
    );
    assert.ok(envContract, "Go-WASM loader registration-token environment contract is missing");
    assert.match(
      loaderSource,
      /__gosx_register_go_wasm_engine_factory\s*=\s*function\(token, name, factory\)/,
      "Go-WASM loader registrar must accept token, component name, and factory",
    );

    execFileSync("go", ["build", "-o", wasmPath, "./engine/wasm/testdata/fixture"], {
      cwd: repoRoot,
      env: { ...process.env, GOOS: "js", GOARCH: "wasm", CGO_ENABLED: "0" },
      stdio: "pipe",
    });
    const goroot = execFileSync("go", ["env", "GOROOT"], {
      cwd: repoRoot,
      encoding: "utf8",
    }).trim();
    const wasmExecPath = [
      path.join(goroot, "lib", "wasm", "wasm_exec.js"),
      path.join(goroot, "misc", "wasm", "wasm_exec.js"),
    ].find((candidate) => fs.existsSync(candidate));
    assert.ok(wasmExecPath, `wasm_exec.js not found under ${goroot}`);
    const standardGoWrapper = [
      `(function(global){var had=Object.prototype.hasOwnProperty.call(global,"Go");var previous=global.Go;var captured;try{`,
      fs.readFileSync(wasmExecPath, "utf8"),
      `captured=global.Go;}finally{if(had)global.Go=previous;else delete global.Go;}Object.defineProperty(global,"__gosx_standard_go_wasm_ctor",{value:captured,writable:false,configurable:true});})(globalThis);`,
    ].join("\n");
    const sharedRuntimeCtor = function SharedRuntimeGo() {};
    globalThis.Go = sharedRuntimeCtor;
    vm.runInThisContext(standardGoWrapper, { filename: wasmExecPath });
    assert.equal(globalThis.Go, sharedRuntimeCtor, "standard-Go shim must restore the shared runtime constructor");

    const token = "integration-registration-token";
    const factories = new Map();
    let registered;
    const registration = new Promise((resolve) => { registered = resolve; });
    globalThis.__gosx_register_go_wasm_engine_factory = (candidate, component, callback) => {
      if (candidate !== token || typeof component !== "string" || typeof callback !== "function") {
        return false;
      }
      factories.set(component, callback);
      if (factories.size === 2) registered();
      return true;
    };

    const go = new globalThis.__gosx_standard_go_wasm_ctor();
    go.env[envContract[1]] = token;
    const result = await WebAssembly.instantiate(fs.readFileSync(wasmPath), go.importObject);
    const runPromise = go.run(result.instance);
    await registration;
    const factory = factories.get("GoWASMFixture");
    assert.equal(typeof factory, "function");
    assert.equal(typeof factories.get("GoWASMFixtureAlias"), "function");

    const mount = { dataset: {}, textContent: "server fallback" };
    let emitted = null;
    const handle = await factory({
      id: "real-go-wasm-engine",
      kind: "surface",
      component: "GoWASMFixture",
      programRef: "/engines/fixture.wasm",
      runtimeMode: "go-wasm",
      mount,
      props: { label: "real standard-Go mount" },
      capabilities: [],
      requiredCapabilities: ["wasm"],
      emit(name, detail) { emitted = { name, detail }; },
    });
    assert.equal(mount.textContent, "real standard-Go mount");
    assert.equal(mount.dataset.engineID, "real-go-wasm-engine");
    assert.equal(mount.dataset.wasmCapability, "true");
    assert.equal(mount.dataset.emitErrorSafe, "true");
    assert.deepEqual(emitted, {
      name: "mounted",
      detail: {
        engineID: "real-go-wasm-engine",
        payload: { label: "real standard-Go mount", mounted: true },
      },
    });

    handle.dispose();
    assert.equal(mount.dataset.disposeCount, "1");
    assert.equal(handle.dispose, undefined, "the Go callback releases itself after exact-once disposal");
    void runPromise;
  } finally {
    delete globalThis.__gosx_register_go_wasm_engine_factory;
    delete globalThis.__gosx_standard_go_wasm_ctor;
    delete globalThis.Go;
    fs.rmSync(tempDir, { recursive: true, force: true });
  }
});
