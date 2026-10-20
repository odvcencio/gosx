"use strict";
const test = require("node:test"), assert = require("node:assert/strict");
const { spawnSync } = require("node:child_process");
const fs = require("node:fs"), os = require("node:os"), path = require("node:path");
for (const available of [false, true]) test(`optional ocean raster ${available ? "retains shader failures" : "skips missing Python"}`, t => {
 const scratch = fs.mkdtempSync(path.join(os.tmpdir(), "gosx-ocean-raster-"));
 t.after(() => fs.rmSync(scratch, {recursive:true, force:true}));
 const preload = path.join(scratch, "preload.cjs");
 fs.writeFileSync(preload, `const fs=require("node:fs"),cp=require("node:child_process"),exists=fs.existsSync,spawn=cp.spawnSync;
 fs.existsSync=file=>file==="/usr/bin/python3"?false:exists(file);
 cp.spawnSync=(command,...args)=>/python3$/.test(command)?${available ? '{status:1,stderr:"shader compilation failed"}' : '{status:null,error:Object.assign(new Error("Python unavailable"),{code:"ENOENT"})}'}:spawn(command,...args);`);
 const env = {...process.env};delete env.NODE_TEST_CONTEXT;
 const run = spawnSync(process.execPath, ["--require", preload, "--test", "--test-reporter=tap", "--test-name-pattern=ocean horizon fog", path.join(__dirname,"scene3d-ocean.test.js")], {encoding:"utf8",timeout:60000,env});
 assert.equal(run.status, available ? 1 : 0, run.stdout + run.stderr);
 if(available) {assert.match(run.stdout,/shader compilation failed/);assert.doesNotMatch(run.stdout,/# SKIP Python/);}
 else assert.match(run.stdout,/# SKIP Python is unavailable/);
});
