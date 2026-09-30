// Geometry reflection capture; independent of the material shading path.
function sceneOceanReflectGLSL() { return [
  "uniform mat4 u_reflectVP, u_reflectInverse, u_planarVP;",
  "uniform vec4 u_reflect; // strength, SSR samples, planar enabled, linear capture",
  "uniform sampler2D u_opaqueColor, u_opaqueDepth, u_planarColor;",
  "vec3 reflectionLinear(vec3 c) {",
  "  return u_reflect.w > 0.5 ? c : mix(pow((c+0.055)/1.055,vec3(2.4)),c/12.92,lessThanEqual(c,vec3(0.04045)));",
  "}",
  "vec3 reflectionWorld(vec2 uv, float depth) {",
  "  vec4 p = u_reflectInverse * vec4(uv*2.-1.,depth*2.-1.,1.);",
  "  return p.xyz / p.w;",
  "}",
  "vec3 reflectionUV(vec3 p) {",
  "  vec4 c = u_reflectVP*vec4(p,1.);",
  "  return vec3(c.xy/c.w*0.5+0.5,c.w);",
  "}",
  "float reflectionDelta(vec3 p, vec2 uv) {",
  "  float d = textureLod(u_opaqueDepth,uv,0.).r;",
  "  if (d >= 0.999999) return -1e6;",
  "  return length(p-u_ocean[7].xyz)-length(reflectionWorld(uv,d)-u_ocean[7].xyz);",
  "}",
  "vec3 oceanGeometryReflection(vec3 world, vec3 ray, vec3 N, float rough, vec3 sky) {",
  "  if (u_reflect.x <= 0.) return sky;",
  "  vec3 result = sky;",
  "  if (u_reflect.z > 0.5) {",
  "    vec4 c = u_planarVP*vec4(world,1.);",
  "    vec2 uv = c.xy/c.w*0.5+0.5+N.xz*0.018;",
  "    if (c.w > 0. && all(greaterThan(uv,vec2(0.))) && all(lessThan(uv,vec2(1.)))) {",
  "      vec4 probe = textureLod(u_planarColor,uv,0.);",
  "      result = mix(sky,reflectionLinear(probe.rgb),probe.a);",
  "    }",
  "  }",
  "  float previous = 0.35;",
  "  vec3 start = world+N*0.15;",
  "  for (int i = 0; i < 20; i++) {",
  "    if (float(i) >= u_reflect.y) break;",
  "    float t = 0.6+float(i)*0.7+float(i*i)*0.65;",
  "    vec3 p = start+ray*t, screen = reflectionUV(p);",
  "    vec2 uv = screen.xy;",
  "    if (screen.z <= 0. || any(lessThan(uv,vec2(0.015))) || any(greaterThan(uv,vec2(0.985)))) break;",
  "    float delta = reflectionDelta(p,uv);",
  "    if (delta >= 0.) {",
  "      float lo = previous, hi = t;",
  "      for (int j = 0; j < 5; j++) {",
  "        float mid = (lo+hi)*0.5;",
  "        vec3 q = start+ray*mid; vec2 quv = reflectionUV(q).xy;",
  "        if (reflectionDelta(q,quv) > 0.) hi = mid; else lo = mid;",
  "      }",
  "      p = start+ray*hi; uv = reflectionUV(p).xy;",
  "      delta = reflectionDelta(p,uv);",
  "      float thickness = 0.35+hi*0.012;",
  "      float edge = smoothstep(0.015,0.12,min(min(uv.x,uv.y),min(1.-uv.x,1.-uv.y)));",
  "      float hit = edge*(1.-smoothstep(thickness*0.5,thickness,delta));",
  "      result = mix(result,reflectionLinear(textureLod(u_opaqueColor,uv,0.).rgb),hit);",
  "      break;",
  "    }",
  "    previous = t;",
  "  }",
  "  return mix(sky,result,u_reflect.x*(1.-smoothstep(0.18,0.5,rough)));",
  "}",
  "// Cox-Munk style anisotropic unresolved slope distribution. Broadens the",
  "// glitter path along the wind while preserving the Fresnel energy factor.",
  "vec3 oceanSunPath(vec3 N, vec3 H, vec3 L, vec3 V, float rough, vec3 sun) {",
  "  vec2 wind = normalize(u_ocean[9].xy), crossWind = vec2(-wind.y,wind.x);",
  "  vec2 slope = H.xz/max(H.y,0.08)-N.xz/max(N.y,0.15);",
  "  float along = 0.025+rough*rough*1.8, across = 0.012+rough*rough*0.8;",
  "  float exponent = dot(slope,wind)*dot(slope,wind)/along+dot(slope,crossWind)*dot(slope,crossWind)/across;",
  "  float D = exp(-min(exponent,80.))/(3.14159265*sqrt(along*across)*pow(max(H.y,0.08),4.));",
  "  float fresnel = 0.02+0.98*pow(1.-max(dot(H,V),0.),5.);",
  "  float visibility = max(L.y,0.)/(max(L.y,0.)+0.12)*smoothstep(-0.01,0.02,L.y);",
  "  return min(sun*D*fresnel*visibility/max(4.*max(dot(N,V),0.05),0.2),vec3(32.));",
  "}"
].join("\n"); }

function sceneReflectWebGLBegin(resources, gl, opts) {
  const config = sceneOceanReflections(opts.environment && opts.environment.ocean && opts.environment.ocean.reflections);
  const active = config && sceneAtmosphereQuality(opts.meta).reflections;
  if (!active) { sceneReflectDispose(resources); return opts.target; }
  if (!resources.reflection) resources.reflection = createSceneReflectWebGL(gl);
  return resources.reflection.begin(opts);
}
function createSceneReflectWebGL(gl) {
  let scene = null, opaque = null, planar = null;
  function target(previous, w, h, depth) {
    if (previous && previous.width === w && previous.height === h) return previous;
    if (previous) disposeScenePostFBO(gl, previous);
    return createScenePostFBO(gl, w, h, depth);
  }
  return {
    begin: function(opts) {
      if (opts.target.framebuffer) return opts.target;
      scene = target(scene, opts.width, opts.height, true);
      gl.bindFramebuffer(gl.FRAMEBUFFER, scene.fbo);
      return Object.assign({}, opts.target, { framebuffer: scene.fbo });
    },
    capture: function(opts, config, quality) {
      const main = gl.getParameter(gl.FRAMEBUFFER_BINDING);
      const w = Math.max(1, Math.round(opts.width*config.resolution));
      const h = Math.max(1, Math.round(opts.height*config.resolution));
      opaque = target(opaque, w, h, true);
      const matrices = sceneReflectionMatrices(opts.view, opts.proj, opts.environment.ocean.level || 0, false);
      gl.bindFramebuffer(gl.READ_FRAMEBUFFER, main);
      gl.bindFramebuffer(gl.DRAW_FRAMEBUFFER, opaque.fbo);
      gl.blitFramebuffer(0,0,opts.width,opts.height,0,0,w,h,gl.COLOR_BUFFER_BIT|gl.DEPTH_BUFFER_BIT,gl.NEAREST);
      const doPlanar = config.mode.includes("planar") && quality.planar;
      if (doPlanar && opts.draw) {
        planar = target(planar, w, h, true);
        gl.bindFramebuffer(gl.FRAMEBUFFER, planar.fbo); gl.viewport(0,0,w,h);
        gl.clearColor(0,0,0,0); gl.clearDepth(1); gl.clear(gl.COLOR_BUFFER_BIT|gl.DEPTH_BUFFER_BIT);
        opts.draw(matrices.view, matrices.projection);
      }
      gl.bindFramebuffer(gl.FRAMEBUFFER, main); gl.viewport(0,0,opts.width,opts.height);
      return { matrices, color: opaque.colorTex, depth: opaque.depthTex, planar: doPlanar && planar ? planar.colorTex : opaque.colorTex,
        settings: [config.strength, config.mode.includes("ssr") ? quality.reflectionSteps : 0, doPlanar ? 1 : 0, opts.linear ? 1 : 0] };
    },
    end: function(w,h) {
      if (!scene || gl.getParameter(gl.FRAMEBUFFER_BINDING) !== scene.fbo) return;
      gl.bindFramebuffer(gl.READ_FRAMEBUFFER,scene.fbo); gl.bindFramebuffer(gl.DRAW_FRAMEBUFFER,null);
      gl.blitFramebuffer(0,0,w,h,0,0,w,h,gl.COLOR_BUFFER_BIT,gl.NEAREST);
      gl.bindFramebuffer(gl.FRAMEBUFFER,null);
    },
    dispose: function() { [scene,opaque,planar].forEach(t => { if (t) disposeScenePostFBO(gl,t); }); scene = opaque = planar = null; },
  };
}
function sceneReflectWebGL(resources, gl, opts) {
  const config = sceneOceanReflections(opts.environment && opts.environment.ocean && opts.environment.ocean.reflections);
  const quality = sceneAtmosphereQuality(opts.meta);
  if (!config || !quality.reflections) return null;
  if (!resources.reflection) resources.reflection = createSceneReflectWebGL(gl);
  return resources.reflection.capture(opts, config, quality);
}
function sceneReflectWebGLEnd(resources, width, height) {
  if (resources.reflection) resources.reflection.end(width,height);
}
function sceneReflectWebGLBind(gl, program, record) {
  const s = record ? record.settings : [0,0,0,1];
  gl.uniform4fv(gl.getUniformLocation(program,"u_reflect"),s);
  if (!record) return;
  const m = record.matrices;
  gl.uniformMatrix4fv(gl.getUniformLocation(program,"u_reflectVP"),false,m.vp);
  gl.uniformMatrix4fv(gl.getUniformLocation(program,"u_reflectInverse"),false,m.inverse);
  gl.uniformMatrix4fv(gl.getUniformLocation(program,"u_planarVP"),false,m.planarVP);
  [record.color,record.depth,record.planar].forEach((texture,i) => {
    scenePBRBindTexture(gl,i+1,texture,gl.TEXTURE_2D);
    gl.uniform1i(gl.getUniformLocation(program,["u_opaqueColor","u_opaqueDepth","u_planarColor"][i]),i+1);
  });
}
function sceneReflectWebGLDrawOpaque(gl, ctx, view, proj) {
  const cull = gl.isEnabled(gl.CULL_FACE), level = ctx.bundle.environment.ocean.level || 0;
  const originalView = new Float32Array(ctx.view), originalProj = new Float32Array(ctx.proj), cameraY = ctx.camera.y;
  // Frame uploads in skinned/rigid programs read these retained arrays. Use
  // the mirrored camera for every program, then restore before the ocean.
  const maps = [ctx.visibility,ctx.batches].filter(Boolean), entries = maps.map(m => Array.from(m.entries()));
  maps.forEach(m => m.clear());
  ctx.view.set(view); ctx.proj.set(proj); ctx.camera.y = 2*level-cameraY;
  try {
    gl.disable(gl.CULL_FACE); gl.disable(gl.BLEND); gl.enable(gl.DEPTH_TEST); gl.depthMask(true);
    gl.useProgram(ctx.program);
    gl.uniformMatrix4fv(ctx.uniforms.viewMatrix,false,ctx.view);
    gl.uniformMatrix4fv(ctx.uniforms.projectionMatrix,false,ctx.proj);
    gl.uniform3f(ctx.uniforms.cameraPosition,ctx.camera.x,ctx.camera.y,ctx.camera.z);
    ctx.draw(sceneReflectOpaqueList(ctx.list.opaque,ctx.materials));
  } finally {
    ctx.view.set(originalView); ctx.proj.set(originalProj); ctx.camera.y = cameraY;
    maps.forEach((m,i) => entries[i].forEach(([key,value]) => m.set(key,value)));
    gl.useProgram(ctx.program);
    gl.uniformMatrix4fv(ctx.uniforms.viewMatrix,false,ctx.view);
    gl.uniformMatrix4fv(ctx.uniforms.projectionMatrix,false,ctx.proj);
    gl.uniform3f(ctx.uniforms.cameraPosition,ctx.camera.x,ctx.camera.y,ctx.camera.z);
    if (cull) gl.enable(gl.CULL_FACE);
  }
}
