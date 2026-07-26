// 16a1 — Selena uniform packing for the WebGPU backend.
//
// These functions turn a Selena material's declared uniform block into a
// std140 Float32Array. They were declared inside createSceneWebGPURenderer, an
// 11_600-line function, so module-scope code could not call them. The custom
// post-effect path in wgpuCreatePostProcessor (16a) did call
// sceneSelenaUniformData from module scope and threw ReferenceError every time
// a page ran a custom post effect with a shader layout.
//
// Dependency direction: this file reads the scene core helper sceneNumber and
// nothing inside the renderer. The renderer reads this file. The renderer owns
// the per-frame state and passes it in as `frame`, an object with:
//
//   frame.viewProjection  Float32Array(16), the current view-projection matrix
//   frame.time            number, seconds, the clock for `param time`
//
// A caller with no frame, such as the post-effect path, passes nothing. The
// reserved auto-uniforms then fall back to identity and zero.
//
// This file joins the chunk inside the same IIFE as 16a-scene-webgpu.js, so it
// carries no wrapper of its own.

  // Shared read-only identity matrix. Every caller copies out of it.
  var selenaIdentityMatrix4 = new Float32Array([
    1, 0, 0, 0,
    0, 1, 0, 0,
    0, 0, 1, 0,
    0, 0, 0, 1,
  ]);

  function sceneSelenaMaterialLayout(material) {
    var layout = material && material.shaderLayout;
    if (!layout || typeof layout !== "object") return null;
    if (!layout.uniformBlock || typeof layout.uniformBlock !== "object") return null;
    if (!Array.isArray(layout.uniformBlock.fields)) return null;
    return layout;
  }

  function sceneSelenaFloatCount(type) {
    switch (String(type || "")) {
    case "float": return 1;
    case "vec2": return 2;
    case "vec3": return 3;
    case "vec4": return 4;
    case "mat3": return 9;
    case "mat4": return 16;
    default: return 1;
    }
  }

  function sceneSelenaUniformDefault(layout, name) {
    var defaults = layout && layout.uniformBlock && Array.isArray(layout.uniformBlock.defaults)
      ? layout.uniformBlock.defaults
      : [];
    for (var i = 0; i < defaults.length; i++) {
      if (defaults[i] && defaults[i].name === name) {
        return defaults[i].values;
      }
    }
    return undefined;
  }

  function sceneSelenaRenderContextUniformValue(renderContext, field) {
    var uniforms = renderContext && renderContext.uniforms;
    var name = field && field.name;
    if (!uniforms || typeof uniforms !== "object" || !name) return undefined;
    if (Object.prototype.hasOwnProperty.call(uniforms, name)) return uniforms[name];
    return undefined;
  }

  function sceneSelenaMaterialValue(material, name) {
    var values = material && material.customUniforms;
    if (values && typeof values === "object" && name && Object.prototype.hasOwnProperty.call(values, name)) {
      return values[name];
    }
    if (material && name && Object.prototype.hasOwnProperty.call(material, name)) {
      return material[name];
    }
    return undefined;
  }

  function webGPUObjectModelMatrix(obj) {
    var matrix = obj && obj.modelMatrix;
    return matrix && typeof matrix.length === "number" && matrix.length >= 16
      ? matrix
      : selenaIdentityMatrix4;
  }

  function webGPUSelenaObjectModelMatrix(obj) {
    if (obj && obj.directVertices === true) {
      return webGPUObjectModelMatrix(obj);
    }
    return selenaIdentityMatrix4;
  }

  function sceneSelenaUniformValue(material, layout, field, owner, renderContext, frame) {
    var name = field && field.name;
    if (name === "mvp" || name === "viewProjectionMatrix") {
      return (frame && frame.viewProjection) || selenaIdentityMatrix4;
    }
    if (name === "modelMatrix") return webGPUSelenaObjectModelMatrix(owner);
    if (name === "normalMatrix") return [1, 0, 0, 0, 1, 0, 0, 0, 1];
    var contextValue = sceneSelenaRenderContextUniformValue(renderContext, field);
    if (contextValue !== undefined) return contextValue;
    // time is a reserved auto-uniform (like mvp/normalMatrix): forced BEFORE
    // customUniforms so a declared `param time` — whose compiled default ships
    // in customUniforms via selenaDefaultUniforms — can't shadow the clock.
    if (name === "time") return sceneNumber(frame && frame.time, 0);
    var value = sceneSelenaMaterialValue(material, name);
    if (value !== undefined) return value;
    var def = sceneSelenaUniformDefault(layout, name);
    if (def !== undefined) return def;
    var count = sceneSelenaFloatCount(field && field.type);
    if (count === 16) return selenaIdentityMatrix4;
    if (count === 9) return [1, 0, 0, 0, 1, 0, 0, 0, 1];
    return 0;
  }

  function sceneSelenaScalar(value) {
    if (Array.isArray(value) || (value && typeof value.length === "number")) {
      return sceneNumber(value[0], 0);
    }
    return sceneNumber(value, 0);
  }

  // G1 -- array uniform packing. A descriptor field with `count > 1` (e.g.
  // the water passes' `context { spheres : array<vec4,32> }`) is an ARRAY
  // uniform: std140 requires every element to start at its own `stride`
  // (bytes) boundary regardless of the element type's own natural size (for
  // array<vec4,N>, stride==16==the vec4's own size, so elements are simply
  // contiguous vec4s; a hypothetical array<float,N> would still pad each
  // element out to 16 bytes). `value` is a FLAT Float32Array/Array sized
  // count*componentsPerElement (see sceneWaterSpheresContextArray in 16a,
  // which builds exactly this shape for the water "spheres" context array).
  // Element `i`'s components land at `base + i*(stride/4) .. +componentCount-1`.
  function sceneSelenaWriteArrayUniformField(f32, base, type, value, arrayCount, strideBytes) {
    var componentsPerElement = sceneSelenaFloatCount(type);
    var strideFloats = Math.max(componentsPerElement, Math.floor(sceneNumber(strideBytes, componentsPerElement * 4) / 4));
    var flat = (Array.isArray(value) || (value && typeof value.length === "number")) ? value : [];
    for (var i = 0; i < arrayCount; i++) {
      var elementBase = base + i * strideFloats;
      for (var c = 0; c < componentsPerElement; c++) {
        f32[elementBase + c] = sceneNumber(flat[i * componentsPerElement + c], 0);
      }
    }
  }

  function sceneSelenaWriteUniformField(f32, base, type, value, field) {
    var arrayCount = field ? Math.floor(sceneNumber(field.count, 0)) : 0;
    if (arrayCount > 1) {
      sceneSelenaWriteArrayUniformField(f32, base, type, value, arrayCount, field && field.stride);
      return;
    }
    var count = sceneSelenaFloatCount(type);
    if (type === "float") {
      f32[base] = sceneSelenaScalar(value);
      return;
    }
    if (type === "mat3") {
      for (var c = 0; c < 3; c++) {
        f32[base + c * 4] = sceneNumber(value && value[c * 3], c === 0 ? 1 : 0);
        f32[base + c * 4 + 1] = sceneNumber(value && value[c * 3 + 1], c === 1 ? 1 : 0);
        f32[base + c * 4 + 2] = sceneNumber(value && value[c * 3 + 2], c === 2 ? 1 : 0);
      }
      return;
    }
    var vectorValue = Array.isArray(value) || (value && typeof value.length === "number");
    if (!vectorValue) {
      f32[base] = sceneSelenaScalar(value);
      for (var zeroIndex = 1; zeroIndex < count; zeroIndex++) {
        f32[base + zeroIndex] = 0;
      }
      return;
    }
    for (var i = 0; i < count; i++) {
      f32[base + i] = sceneNumber(value[i], 0);
    }
  }

  function sceneSelenaUniformData(material, owner, renderContext, frame) {
    var layout = sceneSelenaMaterialLayout(material);
    if (!layout) return null;
    var size = Math.max(16, Math.floor(sceneNumber(layout.uniformBlock.size, 16)));
    var f32 = new Float32Array(Math.ceil(size / 4));
    var fields = layout.uniformBlock.fields;
    for (var i = 0; i < fields.length; i++) {
      var field = fields[i];
      if (!field || typeof field.name !== "string") continue;
      sceneSelenaWriteUniformField(
        f32,
        Math.floor(sceneNumber(field.offset, 0) / 4),
        String(field.type || "float"),
        sceneSelenaUniformValue(material, layout, field, owner, renderContext, frame),
        field
      );
    }
    return f32;
  }
