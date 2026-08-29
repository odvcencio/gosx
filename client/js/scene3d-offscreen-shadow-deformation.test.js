var test = require("node:test");
var assert = require("node:assert");
var fs = require("node:fs");
var path = require("node:path");
var vm = require("node:vm");

var src = fs.readFileSync(
  path.join(__dirname, "..", "runtime", "scene3d", "webgpu.ts"),
  "utf8"
);

function extractFn(name) {
  var start = src.indexOf("function " + name + "(");
  assert.ok(start >= 0, "missing " + name);
  var depth = 0;
  for (var i = src.indexOf("{", start); i < src.length; i++) {
    if (src[i] === "{") depth++;
    else if (src[i] === "}") { depth--; if (!depth) return src.slice(start, i + 1); }
  }
  throw new Error("unbalanced braces in " + name);
}

function makeEncoder(log) {
  var passes = 0;
  return {
    beginComputePass: function (opts) {
      passes++;
      log.passes.push({ label: opts && opts.label, dispatches: 0, pipelines: [] });
      var p = log.passes[log.passes.length - 1];
      return {
        setPipeline: function (pl) { p.pipelines.push(pl); },
        setBindGroup: function (i, bg) { p.bindGroups = (p.bindGroups || []).concat(bg); },
        dispatchWorkgroups: function (n) { p.dispatches += n; },
        end: function () { p.ended = true; },
      };
    },
  };
}

function runUpdater(kind, objects, includeOffscreen) {
  var log = { passes: [], recordCalls: [], snapshots: [] };
  var pipelines = { skin: "SKIN_PIPE", morph: "MORPH_PIPE" };
  var sandbox = {
    elioSkinPipeline: "SKIN_PIPE",
    computedMorphPipeline: "MORPH_PIPE",
    webGPUObjectIsSkinned: function (o) { return !!o.isSkin; },
    webGPUElioSkinRecord: function (o) {
      log.recordCalls.push({ kind: "skin", obj: o });
      if (o.badRecord === true) return null;
      if (!o.isSkin) return null;
      log.snapshots.push(Array.from(o.weights));
      return { bindGroup: { id: o.id }, workgroups: o.workgroups || 1, count: o.count || 4 };
    },
    webGPUComputedMorphRecord: function (o) {
      log.recordCalls.push({ kind: "morph", obj: o });
      if (o.badRecord === true) return null;
      if (o.isSkin) return null;
      log.snapshots.push(Array.from(o.weights));
      return { bindGroup: { id: o.id }, workgroups: o.workgroups || 1, count: o.count || 4 };
    },
  };
  var fnName = kind === "skin" ? "updateElioSkinnedMeshes" : "updateComputedMorphMeshes";
  vm.runInNewContext(extractFn(fnName) + "\n" + fnName, sandbox);
  var fn = sandbox[fnName];
  var stats = fn({ meshObjects: objects }, makeEncoder(log), includeOffscreen);
  return { log: log, stats: stats, expectedPipeline: pipelines[kind] };
}

function obj(over) {
  return Object.assign({ id: "o", isSkin: false, viewCulled: false, castShadow: false, weights: new Float32Array([1, 2]) }, over);
}

["skin", "morph"].forEach(function (kind) {
  var isSkin = kind === "skin";
  var run = function (objects, flag) { return runUpdater(kind, objects, flag); };

  test(kind + ": visible noncaster still updates", function () {
    var r = run([obj({ id: "a", isSkin: isSkin })], false);
    assert.equal(r.stats[kind === "skin" ? "elioSkinningDispatches" : "computedMorphDispatches"], 1);
    assert.equal(r.log.passes.length, 1);
    assert.equal(r.log.passes[0].pipelines[0], r.expectedPipeline);
  });

  test(kind + ": culled caster with flag true gets fresh record call with flag true", function () {
    var o = obj({ id: "c", isSkin: isSkin, viewCulled: true, castShadow: true });
    var r = run([o], true);
    assert.equal(r.log.recordCalls.length, 1);
    assert.equal(r.log.passes.length, 1);
    assert.equal(r.log.passes[0].dispatches, 1);
  });

  test(kind + ": culled noncaster skips even with flag true", function () {
    var r = run([obj({ id: "n", isSkin: isSkin, viewCulled: true, castShadow: false })], true);
    assert.equal(r.log.recordCalls.length, 0);
    assert.equal(r.log.passes.length, 0);
  });

  test(kind + ": undefined/false flag skips culled", function () {
    var o = obj({ id: "u", isSkin: isSkin, viewCulled: true, castShadow: true });
    var r1 = run([o], undefined);
    var r2 = run([o], false);
    assert.equal(r1.log.recordCalls.length, 0);
    assert.equal(r1.log.passes.length, 0);
    assert.equal(r2.log.recordCalls.length, 0);
    assert.equal(r2.log.passes.length, 0);
  });

  test(kind + ": null / invalid record / opposite type skips", function () {
    var r0 = run([null], true);
    assert.equal(r0.log.passes.length, 0);
    var r1 = run([obj({ id: "x", isSkin: !isSkin })], true);
    assert.equal(r1.log.passes.length, 0);
    var r2 = run([obj({ id: "y", isSkin: isSkin, badRecord: true })], true);
    assert.equal(r2.log.passes.length, 0);
  });

  test(kind + ": lazy single pass with correct dispatch count", function () {
    var r = run([
      obj({ id: "a", isSkin: isSkin, workgroups: 2 }),
      obj({ id: "b", isSkin: isSkin, workgroups: 3 }),
      obj({ id: "skip", isSkin: !isSkin }),
    ], false);
    assert.equal(r.log.passes.length, 1);
    assert.equal(r.log.passes[0].dispatches, 5);
    assert.equal(r.log.passes[0].ended, true);
    assert.equal(r.stats[kind === "skin" ? "elioSkinningVertices" : "computedMorphVertices"], 8);
  });

  test(kind + ": flags/stats unchanged, fresh input seen in place", function () {
    var o = obj({ id: "f", isSkin: isSkin, viewCulled: true, castShadow: true });
    var r1 = run([o], true);
    assert.equal(r1.stats[kind === "skin" ? "elioSkinningKernel" : "computedMorphKernel"].length > 0, true);
    o.weights.set([9, 9]);
    var r2 = run([o], true);
    assert.equal(o.viewCulled, true);
    assert.equal(o.castShadow, true);
    assert.deepEqual(r2.log.snapshots[0], [9, 9]);
    assert.notDeepEqual(r2.log.snapshots[0], r1.log.snapshots[0]);
  });
});
