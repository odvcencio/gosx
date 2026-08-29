'use strict';

// Helper probe module for the native (WebGPU) offscreen shadow browser test.
// No imports and no I/O at require time. readMorphState / rememberMorphState /
// dispatchMorphPose are serialized into the browser via function.toString(),
// so they must stay self-contained and touch browser globals only.

const MORPH_PRELOAD = [
  'window.__probeMorphDispatches=0;',
  '(function(){',
  'if(typeof GPUCommandEncoder==="undefined"||!GPUCommandEncoder.prototype||',
  '    !GPUCommandEncoder.prototype.beginComputePass)return;',
  'var oc=GPUCommandEncoder.prototype.beginComputePass;',
  'var passLabels=new WeakMap();',
  'GPUCommandEncoder.prototype.beginComputePass=function(){',
  '  var desc=arguments.length===1?arguments[0]:null;',
  '  var pass=oc.apply(this,arguments);',
  '  if(pass&&desc&&desc.label)passLabels.set(pass,desc.label);',
  '  return pass;',
  '};',
  'if(typeof GPUComputePassEncoder!=="undefined"&&GPUComputePassEncoder.prototype&&',
  '    GPUComputePassEncoder.prototype.dispatchWorkgroups){',
  '  var od=GPUComputePassEncoder.prototype.dispatchWorkgroups;',
  '  GPUComputePassEncoder.prototype.dispatchWorkgroups=function(){',
  '    if(passLabels.get(this)==="gosx-computed-morph")window.__probeMorphDispatches+=1;',
  '    return od.apply(this,arguments);',
  '  };',
  '}',
  '})();',
].join('\n');

function readMorphState(mountId) {
  var out = { found: false };
  var mount = document.getElementById(mountId);
  if (!mount) return out;
  var state = mount.__gosxScene3DState;
  if (!state || !state._modelSkins || !state.objects) return out;
  var record = null;
  var targetRecord = null;
  for (var i = 0; i < state._modelSkins.length; i += 1) {
    var rec = state._modelSkins[i];
    if (rec && rec.id === 'morph-caster') record = rec;
    if (rec && rec.id === 'morph-caster-guard') targetRecord = rec;
  }
  if (!record || !targetRecord || !Array.isArray(record.objectIDs) || record.objectIDs.length === 0) return out;
  var object = state.objects.get(record.objectIDs[0]);
  if (!object) return out;
  var targetObject = state.objects.get(targetRecord.objectIDs && targetRecord.objectIDs[0]);
  if (!targetObject || !targetRecord.model) return out;
  out.found = true;
  out.recordID = record.id;
  out.staticModel = record.staticModel === true;
  out.pose = record.computedPose || '';
  out.targetID = record.computedPoseTargetID || '';
  out.morphObjects = record.computedMorphObjects || 0;
  out.morphVertices = record.computedMorphVertices || 0;
  out.rootTransform = record.rootTransform ? Array.from(record.rootTransform) : null;
  out.objectCastShadow = object.castShadow === true;
  out.objectMorphCount = object.computedMorph ? object.computedMorph.count : 0;
  out.objectMorphAlpha = object.computedMorph ? object.computedMorph.alpha : 0;
  out.targetVisible = targetRecord.model ? targetRecord.model.visible !== false : false;
  out.targetCastShadow = !!(targetObject && targetObject.castShadow === true);
  out.targetStatic = targetRecord.staticModel === true;
  out.objectFirstPositionY = (object &&
    object.vertices &&
    object.vertices.positions &&
    typeof object.vertices.positions[1] === 'number' &&
    isFinite(object.vertices.positions[1])) ? object.vertices.positions[1] : null;
  out.culled = mount.getAttribute('data-gosx-scene3d-webgpu-mesh-view-culled');
  out.dispatches = mount.getAttribute('data-gosx-scene3d-webgpu-computed-morph-dispatches');
  out.nativeMorphDispatches = window.__probeMorphDispatches || 0;
  out.glDraws = window.__probeGLDraws || 0;
  out.wgPasses = window.__probeWGPasses || 0;
  out.wgSubmits = window.__probeWGSubmits || 0;
  var refs = window.__offscreenMorphRefs;
  out.sameMount = !!(refs && refs.mount === mount);
  out.sameCanvas = !!(refs && refs.canvas === mount.querySelector('canvas'));
  out.sameState = !!(refs && refs.state === state);
  out.sameRecord = !!(refs && refs.record === record);
  return out;
}

function rememberMorphState(mountId) {
  var mount = document.getElementById(mountId);
  if (!mount) return false;
  var state = mount.__gosxScene3DState;
  if (!state || !state._modelSkins) return false;
  var record = null;
  for (var i = 0; i < state._modelSkins.length; i += 1) {
    var rec = state._modelSkins[i];
    if (rec && rec.id === 'morph-caster') record = rec;
  }
  if (!record) return false;
  var canvas = mount.querySelector('canvas');
  if (!canvas) return false;
  window.__offscreenMorphRefs = { mount: mount, canvas: canvas, state: state, record: record };
  return true;
}

function dispatchMorphPose(pose, alpha) {
  var a = arguments.length >= 2 ? alpha : 1;
  document.dispatchEvent(new CustomEvent('gosx:hub:event', {
    detail: {
      event: 'pose-change',
      data: { 'morph-caster': { computedPose: pose, computedPoseAlpha: a } },
    },
  }));
  return true;
}

module.exports = { MORPH_PRELOAD: MORPH_PRELOAD, readMorphState: readMorphState, rememberMorphState: rememberMorphState, dispatchMorphPose: dispatchMorphPose };
