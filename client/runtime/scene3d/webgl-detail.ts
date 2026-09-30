// Opt-in WebGL detail programs and packed texture arrays. Plain JS for harnesses.
function sceneWebGLDetailFragment(source, detail) {
  if (!detail) return source;
  return source.replace("void main() {", sceneDetailShaderSource("glsl") + "\nvoid main() {\n    vec3 detailDx = dFdx(v_worldPosition);\n    vec3 detailDy = dFdy(v_worldPosition);")
    .replace("    vec3 V = normalize(u_cameraPosition - v_worldPosition);", `    DetailResult detailResult = detailApply(v_worldPosition, normalize(v_normal), detailDx, detailDy, u_cameraPosition, albedo, N, roughness);
    albedo = detailResult.albedo; N = detailResult.normal; roughness = detailResult.roughness;
    vec3 V = normalize(u_cameraPosition - v_worldPosition);`);
}

function sceneWebGLDetailProgram(gl, resources, kind) {
  const key = sceneDetailVariantKey(kind, true);
  if (resources.programs.has(key)) return resources.programs.get(key);
  const factories = {
    base: function() { return createScenePBRProgram(gl, true); },
    skinned: function() { return createScenePBRSkinnedProgram(gl, true); },
    instanced: function() { return createScenePBRInstancedProgram(gl, false, true); },
    crowd: function() { return createScenePBRInstancedProgram(gl, true, true); },
    motion: function() { return createScenePBRCrowdMotionProgram(gl, true); },
  };
  const program = factories[kind]();
  if (program) {
    Object.assign(program.uniforms, { detail: gl.getUniformLocation(program.program, "u_detail[0]"),
      detailAtlas: gl.getUniformLocation(program.program, "u_detailAtlas") });
  }
  resources.programs.set(key, program);
  return program;
}

function sceneWebGLDetailBakeResources(gl, resources) {
  if (resources.bake) return resources.bake;
  const vertex = "#version 300 es\nprecision highp float;\nout vec2 uv;\nvoid main(){vec2 p=vec2(float((gl_VertexID<<1)&2),float(gl_VertexID&2));uv=p;gl_Position=vec4(p*2.0-1.0,0,1);}";
  const fragment = "#version 300 es\nprecision highp float;\nin vec2 uv;\nout vec4 color;\nuniform sampler2D src;\nuniform sampler2D rough;\nuniform vec3 flags;\nuniform bool normalPass;\nvoid main(){color=vec4(0.5);if(normalPass){color=vec4(0.5,0.5,1,1);if(flags.y>0.5)color=texture(src,uv);}else{if(flags.x>0.5)color.rgb=texture(src,uv).rgb;if(flags.z>0.5)color.a=texture(rough,uv).r;}}";
  const vs = scenePBRCompileShader(gl, gl.VERTEX_SHADER, vertex);
  const fs = scenePBRCompileShader(gl, gl.FRAGMENT_SHADER, fragment);
  const linked = scenePBRLinkProgram(gl, vs, fs, "detail atlas");
  const placeholder = gl.createTexture();
  scenePBRBindTexture(gl, 14, placeholder, gl.TEXTURE_2D);
  gl.texImage2D(gl.TEXTURE_2D, 0, gl.RGBA, 1, 1, 0, gl.RGBA, gl.UNSIGNED_BYTE, new Uint8Array([128, 128, 255, 255]));
  gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.LINEAR);
  resources.bake = { placeholder: placeholder, program: linked, vs: vs, fs: fs, vao: gl.createVertexArray(), fbo: gl.createFramebuffer() };
  return resources.bake;
}

function sceneWebGLBakeDetailAtlas(gl, resources, atlas, records, masks) {
  const bake = sceneWebGLDetailBakeResources(gl, resources);
  const previous = {
    program: gl.getParameter(gl.CURRENT_PROGRAM), vao: gl.getParameter(gl.VERTEX_ARRAY_BINDING),
    draw: gl.getParameter(gl.DRAW_FRAMEBUFFER_BINDING), viewport: gl.getParameter(gl.VIEWPORT),
    active: gl.getParameter(gl.ACTIVE_TEXTURE), depth: gl.isEnabled(gl.DEPTH_TEST), blend: gl.isEnabled(gl.BLEND),
  };
  gl.useProgram(bake.program); gl.bindVertexArray(bake.vao); gl.bindFramebuffer(gl.DRAW_FRAMEBUFFER, bake.fbo);
  gl.disable(gl.DEPTH_TEST); gl.disable(gl.BLEND); gl.viewport(0, 0, 512, 512);
  for (let layer = 0; layer < 4; layer++) {
    const offset = Math.floor(layer / 2) * 3, normalPass = layer % 2 === 1;
    gl.framebufferTextureLayer(gl.DRAW_FRAMEBUFFER, gl.COLOR_ATTACHMENT0, atlas.texture, 0, layer);
    const first = records[offset + (normalPass ? 1 : 0)], rough = records[offset + 2];
    scenePBRBindTexture(gl, 14, first && first.loaded ? first.texture : bake.placeholder, gl.TEXTURE_2D);
    scenePBRBindTexture(gl, 15, rough && rough.loaded ? rough.texture : bake.placeholder, gl.TEXTURE_2D);
    gl.uniform1i(gl.getUniformLocation(bake.program, "src"), 14);
    gl.uniform1i(gl.getUniformLocation(bake.program, "rough"), 15);
    gl.uniform3fv(gl.getUniformLocation(bake.program, "flags"), masks.slice(offset, offset + 3));
    gl.uniform1i(gl.getUniformLocation(bake.program, "normalPass"), normalPass ? 1 : 0);
    gl.drawArrays(gl.TRIANGLES, 0, 3);
  }
  scenePBRBindTexture(gl, 14, atlas.texture, gl.TEXTURE_2D_ARRAY); gl.generateMipmap(gl.TEXTURE_2D_ARRAY);
  gl.bindFramebuffer(gl.DRAW_FRAMEBUFFER, previous.draw); gl.bindVertexArray(previous.vao); gl.useProgram(previous.program);
  gl.viewport(previous.viewport[0], previous.viewport[1], previous.viewport[2], previous.viewport[3]);
  if (previous.depth) gl.enable(gl.DEPTH_TEST); if (previous.blend) gl.enable(gl.BLEND);
  gl.activeTexture(previous.active);
}

function sceneWebGLPrepareDetail(gl, resources, material, textureCache) {
  const detail = material.detail;
  const key = JSON.stringify([detail.ground, detail.steep]);
  let atlas = resources.atlases.get(key);
  if (!atlas) {
    atlas = { texture: gl.createTexture(), signature: "", masks: [] };
    scenePBRBindTexture(gl, 14, atlas.texture, gl.TEXTURE_2D_ARRAY);
    gl.texStorage3D(gl.TEXTURE_2D_ARRAY, 10, gl.RGBA8, 512, 512, 4);
    gl.texParameteri(gl.TEXTURE_2D_ARRAY, gl.TEXTURE_MIN_FILTER, gl.LINEAR_MIPMAP_LINEAR);
    gl.texParameteri(gl.TEXTURE_2D_ARRAY, gl.TEXTURE_MAG_FILTER, gl.LINEAR);
    gl.texParameteri(gl.TEXTURE_2D_ARRAY, gl.TEXTURE_WRAP_S, gl.REPEAT);
    gl.texParameteri(gl.TEXTURE_2D_ARRAY, gl.TEXTURE_WRAP_T, gl.REPEAT);
    resources.atlases.set(key, atlas);
  }
  const inputs = sceneDetailTextureRecords(detail, function(url, role) {
    return scenePBRLoadTexture(gl, url, textureCache, null, role === "albedo" ? "base-color" : role, "linear");
  });
  const signature = inputs.masks.join("");
  if (signature !== atlas.signature) {
    sceneWebGLBakeDetailAtlas(gl, resources, atlas, inputs.records, inputs.masks);
    atlas.signature = signature; atlas.masks = inputs.masks;
  }
  resources.materials.set(material, atlas);
}

function sceneWebGLUploadDetail(gl, resources, uniforms, material, enabled) {
  if (!uniforms.detail || !material || !material.detail) return;
  const atlas = resources.materials.get(material);
  gl.uniform4fv(uniforms.detail, sceneDetailUniformData(material.detail, atlas.masks, enabled));
  scenePBRBindTexture(gl, 14, atlas.texture, gl.TEXTURE_2D_ARRAY);
  gl.uniform1i(uniforms.detailAtlas, 14);
}

function sceneWebGLDisposeDetail(gl, resources) {
  for (const atlas of resources.atlases.values()) gl.deleteTexture(atlas.texture);
  for (const program of resources.programs.values()) {
    if (program) { gl.deleteProgram(program.program); gl.deleteShader(program.vertexShader); gl.deleteShader(program.fragmentShader); }
  }
  const bake = resources.bake;
  if (bake) { gl.deleteTexture(bake.placeholder); gl.deleteProgram(bake.program); gl.deleteShader(bake.vs); gl.deleteShader(bake.fs); gl.deleteVertexArray(bake.vao); gl.deleteFramebuffer(bake.fbo); }
}
