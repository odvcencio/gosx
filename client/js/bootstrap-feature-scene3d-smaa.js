(()=>{const q=window;if(q.__gosx_scene3d_smaa_api)return;const m=(e,t,a,n)=>{const l=Number(e);return Number.isFinite(l)&&l>0?Math.max(a,Math.min(n,l)):t};function U(e){return{threshold:m(e&&e.threshold,.1,.02,.5),steps:Math.round(m(e&&e.maxSearchSteps,16,4,32)),corner:m(e&&e.cornerRounding,.25,0,1)}}function A(e,t,a){if(typeof t!="function")return null;let n=e[a];return(!n||n.kind!=="smaa")&&(n&&n.pass&&n.pass.dispose(),n=e[a]={kind:"smaa",pass:t(e.device||e.gl)}),n.pass}function D(e,t,a,n,l,d,b,w,R,k,F,P){const _=A(e,S.webgpu,a);if(!_||!_.apply(t,n,l,d,b,R,k))return l;const r=q.__gosx_scene3d_render_truth_api;return P&&r&&r.mark(P,a,"ok",1),S.stats.frames++,d}function M(e,t,a,n,l,d,b){const w=A(e,S.webgl,a),R=e.width||d,k=e.height||b;return!w||!w.apply(t,n,l,R,k,d,b)?n:(S.stats.frames++,l?l.colorTex:null)}function N(e){for(const t of Object.keys(e)){const a=e[t];a&&a.kind==="smaa"&&a.pass&&(a.pass.dispose(),delete e[t])}}const S={version:1,settings:U,webgpuRun:D,webglRun:M,dispose:N,stats:{frames:0},webgpu:null,webgl:null};q.__gosx_scene3d_smaa_api=S})(),(()=>{const m=window.__gosx_scene3d_smaa_api;if(!m||m.webgpu)return;const U=`@vertex fn vertexMain(@builtin(vertex_index) i: u32) -> @builtin(position) vec4f {
  let c = vec2f(f32((i << 1u) & 2u), f32(i & 2u));
  return vec4f(c.x * 2.0 - 1.0, 1.0 - c.y * 2.0, 0.0, 1.0);
}
`,A=`struct SMAAParams { threshold: f32, steps: f32, corner: f32, pad: f32 };
@group(0) @binding(1) var<uniform> prm: SMAAParams;
`,D=A+`@group(0) @binding(0) var src: texture_2d<f32>;
fn luma(p: vec2i) -> f32 {
  let c = textureLoad(src, clamp(p, vec2i(0), vec2i(textureDimensions(src)) - 1), 0).rgb;
  return dot(clamp(c, vec3f(0.0), vec3f(1.0)), vec3f(0.2126, 0.7152, 0.0722));
}
@fragment fn fragmentMain(@builtin(position) pos: vec4f) -> @location(0) vec4f {
  let p = vec2i(pos.xy);
  let c = luma(p);
  let l = luma(p + vec2i(-1, 0));
  let t = luma(p + vec2i(0, -1));
  let d = abs(vec2f(c - l, c - t));
  var e = step(vec2f(prm.threshold), d);
  if (e.x + e.y == 0.0) { return vec4f(0.0); }
  let r = abs(c - luma(p + vec2i(1, 0)));
  let b = abs(c - luma(p + vec2i(0, 1)));
  let m = max(max(max(d.x, d.y), max(r, b)), max(abs(l - luma(p + vec2i(-2, 0))), abs(t - luma(p + vec2i(0, -2)))));
  e *= step(vec2f(m), 2.0 * d);
  return vec4f(e, 0.0, 1.0);
}
`,M=A+`@group(0) @binding(0) var edges: texture_2d<f32>;
var<private> dims: vec2i;
fn E(p: vec2i) -> vec2f {
  if (any(p < vec2i(0)) || any(p >= dims)) { return vec2f(0.0); }
  return textureLoad(edges, p, 0).rg;
}
fn ch(v: vec2f, k: i32) -> f32 { return select(v.x, v.y, k == 1); }
// The height the line reaches at an end: -0.5 when the crossing turns into this
// pixel's row, 0.5 when it turns into Q's, 0 for neither or both. A crossing
// that runs on past one pixel is a corner and keeps prm.corner of the rounding.
fn endHeight(cp: f32, cq: f32, kp: f32, kq: f32) -> f32 {
  if (cp * cq > 0.0) { return 0.0; }
  return 0.5 * (cq * select(1.0, prm.corner, kq > 0.0) - cp * select(1.0, prm.corner, kp > 0.0));
}
// Walks from p along its edge in direction s (-1 or 1) and returns the distance
// in pixels to the end of the edge and the line's height there. The walk stops
// at a crossing edge, where the edge ends, or after prm.steps pixels.
fn walk(p: vec2i, s: i32, k: i32) -> vec2f {
  let a = select(vec2i(0, 1), vec2i(1, 0), k == 1);
  let q = select(vec2i(-1, 0), vec2i(0, -1), k == 1);
  let n = i32(prm.steps);
  let first = select(1, 0, s < 0);
  var d = 0;
  loop {
    let b = p + a * (s * d + first);
    let cp = ch(E(b), 1 - k);
    let cq = ch(E(b + q), 1 - k);
    if (cp + cq > 0.0) {
      return vec2f(f32(d), endHeight(cp, cq, ch(E(b - q), 1 - k), ch(E(b + 2 * q), 1 - k)));
    }
    if (d >= n || ch(E(p + a * (s * (d + 1))), k) == 0.0) { break; }
    d += 1;
  }
  return vec2f(f32(d), 0.0);
}
// The integral of the line from (x0, h0) to (x1, h1) over this pixel's span
// [0, 1], split into the part below the edge (x) and above it (y).
fn span(x0: f32, x1: f32, h0: f32, h1: f32) -> vec2f {
  let a = max(x0, 0.0);
  let b = min(x1, 1.0);
  if (b <= a) { return vec2f(0.0); }
  let v = (b - a) * (h0 + (h1 - h0) * (0.5 * (a + b) - x0) / (x1 - x0));
  return vec2f(max(-v, 0.0), max(v, 0.0));
}
// The segment runs from d1 pixels before this pixel to d2 pixels after it; the
// line falls from h1 at the first end to 0 at the middle and rises to h2.
fn area(d1: f32, d2: f32, h1: f32, h2: f32) -> vec2f {
  let m = 0.5 * (d2 + 1.0 - d1);
  return span(-d1, m, h1, 0.0) + span(m, d2 + 1.0, 0.0, h2);
}
@fragment fn fragmentMain(@builtin(position) pos: vec4f) -> @location(0) vec4f {
  dims = vec2i(textureDimensions(edges));
  let p = vec2i(pos.xy);
  let e = E(p);
  var o = vec4f(0.0);
  if (e.y > 0.0) {
    let l = walk(p, -1, 1);
    let r = walk(p, 1, 1);
    o = vec4f(area(l.x, r.x, l.y, r.y), 0.0, 0.0);
  }
  if (e.x > 0.0) {
    let u = walk(p, -1, 0);
    let v = walk(p, 1, 0);
    o = vec4f(o.xy, area(u.x, v.x, u.y, v.y));
  }
  return o;
}
`,N=`@group(0) @binding(0) var src: texture_2d<f32>;
@group(0) @binding(1) var wts: texture_2d<f32>;
var<private> dims: vec2i;
fn lin(p: vec2i) -> vec4f {
  let c = textureLoad(src, clamp(p, vec2i(0), dims - 1), 0);
  return vec4f(pow(max(c.rgb, vec3f(0.0)), vec3f(2.2)), c.a);
}
fn W(p: vec2i) -> vec4f {
  if (any(p >= dims)) { return vec4f(0.0); }
  return textureLoad(wts, p, 0);
}
@fragment fn fragmentMain(@builtin(position) pos: vec4f) -> @location(0) vec4f {
  dims = vec2i(textureDimensions(src));
  let p = vec2i(pos.xy);
  let own = W(p);
  let up = own.x;
  let left = own.z;
  let down = W(p + vec2i(0, 1)).y;
  let right = W(p + vec2i(1, 0)).w;
  if (up + left + down + right == 0.0) { return textureLoad(src, p, 0); }
  var a = vec2i(0, -1);
  var wa = up;
  var wb = down;
  if (max(left, right) > max(up, down)) { a = vec2i(-1, 0); wa = left; wb = right; }
  let m = lin(p) * (1.0 - wa - wb) + lin(p + a) * wa + lin(p - a) * wb;
  return vec4f(pow(m.rgb, vec3f(1.0 / 2.2)), m.a);
}
`;function S(e){const t=globalThis.GPUShaderStage?globalThis.GPUShaderStage.FRAGMENT:2,a=globalThis.GPUTextureUsage||{RENDER_ATTACHMENT:16,TEXTURE_BINDING:4},n=globalThis.GPUBufferUsage||{UNIFORM:64,COPY_DST:8},l={texture:{sampleType:"unfilterable-float"}},d=e.createBindGroupLayout({label:"gosx-smaa-params",entries:[{binding:0,visibility:t,...l},{binding:1,visibility:t,buffer:{type:"uniform"}}]}),b=e.createBindGroupLayout({label:"gosx-smaa-blend",entries:[{binding:0,visibility:t,...l},{binding:1,visibility:t,...l}]}),w=e.createBuffer({label:"gosx-smaa-params",size:16,usage:n.UNIFORM|n.COPY_DST}),R=new Map,k=new Map;let F=0,P=0,_=null,r=null,s=null,x=null,v="";function f(o,c,g,T){const h=o+":"+T;let y=R.get(h);if(!y){const C=e.createShaderModule({label:"gosx-smaa-"+o,code:U+c});y=e.createRenderPipeline({label:"gosx-smaa-"+o,layout:e.createPipelineLayout({bindGroupLayouts:[g]}),vertex:{module:C,entryPoint:"vertexMain"},fragment:{module:C,entryPoint:"fragmentMain",targets:[{format:T}]},primitive:{topology:"triangle-list"}}),R.set(h,y)}return y}function u(o,c,g){let T=k.get(o);T||k.set(o,T=new Map);let h=T.get(c);h||T.set(c,h=new Map);let y=h.get(g);return y||(y=e.createBindGroup({layout:o,entries:[{binding:0,resource:c},{binding:1,resource:g===w?{buffer:w}:g}]}),h.set(g,y)),y}function i(o,c){o===F&&c===P||(E(),F=o,P=c,_=e.createTexture({label:"gosx-smaa-edges",size:[o,c],format:"rg8unorm",usage:a.RENDER_ATTACHMENT|a.TEXTURE_BINDING}),r=e.createTexture({label:"gosx-smaa-weights",size:[o,c],format:"rgba8unorm",usage:a.RENDER_ATTACHMENT|a.TEXTURE_BINDING}),s=_.createView(),x=r.createView())}function E(){_&&_.destroy(),r&&r.destroy(),_=r=s=x=null,k.clear(),F=P=0}function p(o,c,g,T){const h=o.beginRenderPass({label:c.label,colorAttachments:[{view:T,loadOp:"clear",clearValue:{r:0,g:0,b:0,a:0},storeOp:"store"}]});h.setPipeline(c),h.setBindGroup(0,g),h.draw(3),h.end()}return{apply(o,c,g,T,h,y,C){if(!g||!T||y<1||C<1)return!1;i(y,C);const G=m.settings(o),L=G.threshold+":"+G.steps+":"+G.corner;return L!==v&&(e.queue.writeBuffer(w,0,new Float32Array([G.threshold,G.steps,G.corner,0])),v=L),p(c,f("edges",D,d,"rg8unorm"),u(d,g,w),s),p(c,f("weights",M,d,"rgba8unorm"),u(d,s,w),x),p(c,f("blend",N,b,h),u(b,g,x),T),!0},dispose(){E(),w.destroy(),R.clear()}}}m.webgpu=S,m.wgsl={edges:D,weights:M,blend:N,vertex:U}})(),(()=>{const m=window.__gosx_scene3d_smaa_api;if(!m||m.webgl)return;const U=`#version 300 es
void main() {
  vec2 c = vec2(float((gl_VertexID << 1) & 2), float(gl_VertexID & 2));
  gl_Position = vec4(c * 2.0 - 1.0, 0.0, 1.0);
}`,A=`#version 300 es
precision highp float;
precision highp int;
const ivec2 UP = ivec2(0, 1);
uniform vec3 u_params;
out vec4 fragColor;
`,D=A+`uniform sampler2D u_src;
float luma(ivec2 p) {
  vec3 c = texelFetch(u_src, clamp(p, ivec2(0), textureSize(u_src, 0) - 1), 0).rgb;
  return dot(clamp(c, 0.0, 1.0), vec3(0.2126, 0.7152, 0.0722));
}
void main() {
  ivec2 p = ivec2(gl_FragCoord.xy);
  float c = luma(p), l = luma(p + ivec2(-1, 0)), t = luma(p + UP);
  vec2 d = abs(vec2(c - l, c - t));
  vec2 e = step(vec2(u_params.x), d);
  if (e.x + e.y == 0.0) { fragColor = vec4(0.0); return; }
  float r = abs(c - luma(p + ivec2(1, 0))), b = abs(c - luma(p - UP));
  float m = max(max(max(d.x, d.y), max(r, b)), max(abs(l - luma(p + ivec2(-2, 0))), abs(t - luma(p + 2 * UP))));
  e *= step(vec2(m), 2.0 * d);
  fragColor = vec4(e, 0.0, 1.0);
}`,M=A+`uniform sampler2D u_edges;
ivec2 dims;
vec2 E(ivec2 p) {
  if (any(lessThan(p, ivec2(0))) || any(greaterThanEqual(p, dims))) return vec2(0.0);
  return texelFetch(u_edges, p, 0).rg;
}
float ch(vec2 v, int k) { return k == 1 ? v.y : v.x; }
float endHeight(float cp, float cq, float kp, float kq) {
  if (cp * cq > 0.0) return 0.0;
  return 0.5 * (cq * (kq > 0.0 ? u_params.z : 1.0) - cp * (kp > 0.0 ? u_params.z : 1.0));
}
vec2 walk(ivec2 p, int s, int k) {
  ivec2 a = k == 1 ? ivec2(1, 0) : -UP;
  ivec2 q = k == 1 ? UP : ivec2(-1, 0);
  int n = int(u_params.y);
  int first = s < 0 ? 0 : 1;
  int d = 0;
  for (int i = 0; i <= 32; i++) {
    ivec2 b = p + a * (s * d + first);
    float cp = ch(E(b), 1 - k), cq = ch(E(b + q), 1 - k);
    if (cp + cq > 0.0) return vec2(float(d), endHeight(cp, cq, ch(E(b - q), 1 - k), ch(E(b + 2 * q), 1 - k)));
    if (d >= n || ch(E(p + a * (s * (d + 1))), k) == 0.0) break;
    d += 1;
  }
  return vec2(float(d), 0.0);
}
vec2 span(float x0, float x1, float h0, float h1) {
  float a = max(x0, 0.0), b = min(x1, 1.0);
  if (b <= a) return vec2(0.0);
  float v = (b - a) * (h0 + (h1 - h0) * (0.5 * (a + b) - x0) / (x1 - x0));
  return vec2(max(-v, 0.0), max(v, 0.0));
}
vec2 area(float d1, float d2, float h1, float h2) {
  float m = 0.5 * (d2 + 1.0 - d1);
  return span(-d1, m, h1, 0.0) + span(m, d2 + 1.0, 0.0, h2);
}
void main() {
  dims = textureSize(u_edges, 0);
  ivec2 p = ivec2(gl_FragCoord.xy);
  vec2 e = E(p);
  vec4 o = vec4(0.0);
  if (e.y > 0.0) { vec2 l = walk(p, -1, 1), r = walk(p, 1, 1); o.xy = area(l.x, r.x, l.y, r.y); }
  if (e.x > 0.0) { vec2 u = walk(p, -1, 0), v = walk(p, 1, 0); o.zw = area(u.x, v.x, u.y, v.y); }
  fragColor = o;
}`,N=A+`uniform sampler2D u_src;
uniform sampler2D u_wts;
uniform vec2 u_out;
ivec2 dims;
vec4 lin(ivec2 p) {
  vec4 c = texelFetch(u_src, clamp(p, ivec2(0), dims - 1), 0);
  return vec4(pow(max(c.rgb, 0.0), vec3(2.2)), c.a);
}
vec4 W(ivec2 p) {
  if (any(lessThan(p, ivec2(0))) || any(greaterThanEqual(p, dims))) return vec4(0.0);
  return texelFetch(u_wts, p, 0);
}
vec4 resolve(ivec2 p) {
  p = clamp(p, ivec2(0), dims - 1);
  vec4 own = W(p);
  float up = own.x, left = own.z, down = W(p - UP).y, right = W(p + ivec2(1, 0)).w;
  if (up + left + down + right == 0.0) return lin(p);
  ivec2 a = UP;
  float wa = up, wb = down;
  if (max(left, right) > max(up, down)) { a = ivec2(-1, 0); wa = left; wb = right; }
  return lin(p) * (1.0 - wa - wb) + lin(p + a) * wa + lin(p - a) * wb;
}
void main() {
  dims = textureSize(u_src, 0);
  vec4 m;
  if (u_out == vec2(dims)) {
    m = resolve(ivec2(gl_FragCoord.xy));
  } else {
    vec2 t = gl_FragCoord.xy / u_out * vec2(dims) - 0.5;
    ivec2 i = ivec2(floor(t));
    vec2 f = fract(t);
    m = mix(mix(resolve(i), resolve(i + ivec2(1, 0)), f.x), mix(resolve(i + ivec2(0, 1)), resolve(i + ivec2(1, 1)), f.x), f.y);
  }
  fragColor = vec4(pow(max(m.rgb, 0.0), vec3(1.0 / 2.2)), m.a);
}`;function S(e){const t={},a=e.createVertexArray();let n=0,l=0,d=null,b=null,w=!1;function R(r,s){if(t[r]!==void 0)return t[r];const x=(i,E)=>{const p=e.createShader(i);return p?(e.shaderSource(p,E),e.compileShader(p),e.getShaderParameter(p,e.COMPILE_STATUS)?p:(console.error("[gosx] smaa "+r+": "+e.getShaderInfoLog(p)),e.deleteShader(p),null)):null},v=x(e.VERTEX_SHADER,U),f=x(e.FRAGMENT_SHADER,s);let u=null;if(v&&f){const i=e.createProgram();e.attachShader(i,v),e.attachShader(i,f),e.linkProgram(i),e.getProgramParameter(i,e.LINK_STATUS)?u={program:i,loc:E=>e.getUniformLocation(i,E)}:console.error("[gosx] smaa "+r+": "+e.getProgramInfoLog(i))}return v&&e.deleteShader(v),f&&e.deleteShader(f),u||(w=!0),t[r]=u}function k(r,s,x,v){const f=e.createTexture();e.bindTexture(e.TEXTURE_2D,f),e.texImage2D(e.TEXTURE_2D,0,r,x,v,0,s,e.UNSIGNED_BYTE,null);for(const[i,E]of[[e.TEXTURE_MIN_FILTER,e.NEAREST],[e.TEXTURE_MAG_FILTER,e.NEAREST],[e.TEXTURE_WRAP_S,e.CLAMP_TO_EDGE],[e.TEXTURE_WRAP_T,e.CLAMP_TO_EDGE]])e.texParameteri(e.TEXTURE_2D,i,E);const u=e.createFramebuffer();return e.bindFramebuffer(e.FRAMEBUFFER,u),e.framebufferTexture2D(e.FRAMEBUFFER,e.COLOR_ATTACHMENT0,e.TEXTURE_2D,f,0),{fbo:u,colorTex:f}}function F(){for(const r of[d,b])r&&(e.deleteFramebuffer(r.fbo),e.deleteTexture(r.colorTex));d=b=null,n=l=0}function P(r,s){r===n&&s===l||(F(),d=k(e.RG8,e.RG,r,s),b=k(e.RGBA8,e.RGBA,r,s),n=r,l=s)}function _(r,s,x,v,f,u){e.bindFramebuffer(e.FRAMEBUFFER,s),e.drawBuffers([s?e.COLOR_ATTACHMENT0:e.BACK]),e.viewport(0,0,x,v),e.disable(e.BLEND),e.disable(e.DEPTH_TEST),e.disable(e.SCISSOR_TEST),s&&(e.clearColor(0,0,0,0),e.clear(e.COLOR_BUFFER_BIT)),e.useProgram(r.program),f.forEach(([i,E],p)=>{e.activeTexture(e.TEXTURE0+p),e.bindTexture(e.TEXTURE_2D,E),e.uniform1i(r.loc(i),p)}),e.uniform3f(r.loc("u_params"),u.threshold,u.steps,u.corner),e.bindVertexArray(a),e.drawArrays(e.TRIANGLES,0,3),e.bindVertexArray(null);for(let i=f.length-1;i>=0;i--)e.activeTexture(e.TEXTURE0+i),e.bindTexture(e.TEXTURE_2D,null)}return{apply(r,s,x,v,f,u,i){if(!s||w||v<1||f<1)return!1;const E=R("edges",D),p=R("weights",M),o=R("blend",N);if(!E||!p||!o)return!1;P(v,f);const c=m.settings(r);return _(E,d.fbo,v,f,[["u_src",s]],c),_(p,b.fbo,v,f,[["u_edges",d.colorTex]],c),e.useProgram(o.program),e.uniform2f(o.loc("u_out"),u,i),_(o,x?x.fbo:null,u,i,[["u_src",s],["u_wts",b.colorTex]],c),!0},dispose(){F();for(const r of Object.keys(t))t[r]&&e.deleteProgram(t[r].program);e.deleteVertexArray(a)}}}m.webgl=S,m.glsl={edges:D,weights:M,blend:N,vertex:U}})();
