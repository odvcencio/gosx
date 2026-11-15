(()=>{const b=window;if(b.__gosx_scene3d_depthfx_api)return;function $(r,n,s){for(let u=0;u<4;u++){const d=s[u*4],c=s[u*4+1],L=s[u*4+2],w=s[u*4+3];for(let R=0;R<4;R++)r[u*4+R]=n[R]*d+n[4+R]*c+n[8+R]*L+n[12+R]*w}return r}function K(r,n){const s=n[0],u=n[1],d=n[2],c=n[3],L=n[4],w=n[5],R=n[6],O=n[7],v=n[8],_=n[9],f=n[10],E=n[11],x=n[12],X=n[13],G=n[14],M=n[15],z=s*w-u*L,N=s*R-d*L,H=s*O-c*L,i=u*R-d*w,a=u*O-c*w,F=d*O-c*R,l=v*X-_*x,o=v*G-f*x,y=v*M-E*x,V=_*G-f*X,W=_*M-E*X,ee=f*M-E*G;let B=z*ee-N*W+H*V+i*y-a*o+F*l;return!B||!isFinite(B)?!1:(B=1/B,r[0]=(w*ee-R*W+O*V)*B,r[1]=(d*W-u*ee-c*V)*B,r[2]=(X*F-G*a+M*i)*B,r[3]=(f*a-_*F-E*i)*B,r[4]=(R*y-L*ee-O*o)*B,r[5]=(s*ee-d*y+c*o)*B,r[6]=(G*H-x*F-M*N)*B,r[7]=(v*F-f*H+E*N)*B,r[8]=(L*W-w*y+O*l)*B,r[9]=(u*y-s*W-c*l)*B,r[10]=(x*a-X*H+M*z)*B,r[11]=(_*H-v*a-E*z)*B,r[12]=(w*o-L*V-R*l)*B,r[13]=(s*V-u*o+d*l)*B,r[14]=(X*N-x*i-G*z)*B,r[15]=(v*i-_*N+f*z)*B,!0)}function te(r,n,s){const u=(d,c)=>n[c*4+d];for(let d=0;d<6;d++){const c=d>>1,L=d&1?-1:1;let w,R,O,v;c===2&&s&&L===1?(w=u(2,0),R=u(2,1),O=u(2,2),v=u(2,3)):(w=u(3,0)+L*u(c,0),R=u(3,1)+L*u(c,1),O=u(3,2)+L*u(c,2),v=u(3,3)+L*u(c,3));const _=Math.hypot(w,R,O)||1;r[d*4]=w/_,r[d*4+1]=R/_,r[d*4+2]=O/_,r[d*4+3]=v/_}return r}function Z(r,n,s,u,d){for(let c=0;c<6;c++)if(r[c*4]*n+r[c*4+1]*s+r[c*4+2]*u+r[c*4+3]<-d)return!1;return!0}function U(r){return r<=.04045?r/12.92:Math.pow((r+.055)/1.055,2.4)}function q(r,n,s,u){let d=u,c=u,L=u;if(typeof r=="string"){const w=r.trim().replace(/^#/,"");if(/^[0-9a-f]{3}$/i.test(w))d=parseInt(w[0]+w[0],16)/255,c=parseInt(w[1]+w[1],16)/255,L=parseInt(w[2]+w[2],16)/255;else if(/^[0-9a-f]{6}([0-9a-f]{2})?$/i.test(w))d=parseInt(w.slice(0,2),16)/255,c=parseInt(w.slice(2,4),16)/255,L=parseInt(w.slice(4,6),16)/255;else return J(n,s,u,u,u);return J(n,s,U(d),U(c),U(L))}J(n,s,d,c,L)}function J(r,n,s,u,d){r[n]=s,r[n+1]=u,r[n+2]=d}const e=new Float32Array(3);function t(r,n,s,u){n[0]=n[1]=n[2]=0;const d=r&&r.environment||{},c=Number(d.ambientIntensity);if(isFinite(c)&&c>0){q(d.ambientColor,e,0,1);for(let v=0;v<3;v++)n[v]+=e[v]*c}const L=Number(d.skyIntensity),w=Number(d.groundIntensity);if(isFinite(L)&&L>0){q(d.skyColor,e,0,1);for(let v=0;v<3;v++)n[v]+=e[v]*L*.5}if(isFinite(w)&&w>0){q(d.groundColor,e,0,1);for(let v=0;v<3;v++)n[v]+=e[v]*w*.25}let R=!1;const O=r&&r.lights;if(Array.isArray(O))for(let v=0;v<O.length;v++){const _=O[v];if(!_)continue;const f=String(_.kind||"").toLowerCase(),E=isFinite(Number(_.intensity))?Number(_.intensity):1;if(f==="ambient"){q(_.color,e,0,1);for(let x=0;x<3;x++)n[x]+=e[x]*E}else if(f==="hemisphere"){q(_.color,e,0,1);for(let x=0;x<3;x++)n[x]+=e[x]*E*.5}else if(f==="directional"&&!R){let x=Number(_.directionX),X=Number(_.directionY),G=Number(_.directionZ);(!isFinite(x)||!isFinite(X)||!isFinite(G))&&(x=0,X=-1,G=0);const M=Math.hypot(x,X,G)||1;s[0]=x/M,s[1]=X/M,s[2]=G/M,q(_.color,u,0,1);for(let z=0;z<3;z++)u[z]*=E;R=!0}}return R||(s[0]=0,s[1]=-1,s[2]=0,u[0]=u[1]=u[2]=0),R}function I(r){const n=r&&r.meta;if(!n)return"full";const s=n.qualityProfile||{};return String(n.qualityEnabled&&(s.tier||n.qualityTier)||"full")}function P(r){return fetch(r,{mode:"cors",credentials:"same-origin"}).then(n=>{if(!n.ok)throw new Error("decal texture "+n.status+": "+r);return n.blob()}).then(n=>createImageBitmap(n,{premultiplyAlpha:"none",colorSpaceConversion:"none"}))}function m(r,n,s,u){const d=n[s.kind];if(!d)return null;let c=r[u];return(!c||c.kind!==s.kind)&&(c&&c.pass.dispose(),c=r[u]={kind:s.kind,pass:d(r.device||r.gl)}),c.pass}function h(r,n,s,u,d,c,L,w,R,O,v,_){const f=m(r,T.webgpu,n,s);if(!f||!f.apply(n,u,d,c,L,w,R,O,v))return d;const E=window.__gosx_scene3d_render_truth_api;return _&&E&&E.mark(_,s,"ok",1),c}function S(r,n,s,u,d,c,L,w,R){const O=m(r,T.webgl,n,s);return!O||!O.apply(n,u,d,c,L,w,R)?u:d?d.colorTex:null}function D(r){for(const n of Object.keys(r)){if(n==="device"||n==="gl")continue;const s=r[n];s&&s.pass&&s.pass.dispose(),delete r[n]}}const T={version:1,mul:$,invert:K,frustumPlanes:te,sphereVisible:Z,parseColor:q,lightSummary:t,qualityTier:I,loadBitmap:P,handles:r=>r==="decals"||r==="motionBlur",webgpuRun:h,webglRun:S,dispose:D,stats:{decals:{frames:0,visible:0},motionBlur:{frames:0,moverFrames:0}},webgpu:{},webgl:{}};b.__gosx_scene3d_depthfx_api=T})(),(()=>{const b=window.__gosx_scene3d_depthfx_api;if(!b)return;const $=20,K=40,te=8192,Z=16,U=e=>e<=.04045?e/12.92:Math.pow((e+.055)/1.055,2.4);function q(e){const t=new Float32Array(Z),I=(h,S)=>typeof h=="number"&&isFinite(h)?h:S,P=Math.PI/180;t[0]=Math.cos(I(e.angleFadeStart,65)*P),t[1]=Math.cos(I(e.angleFadeEnd,88)*P),t[2]=Math.max(.001,Math.min(.5,I(e.edgeFade,.1))),t[3]=I(e.normalStrength,1),t[4]=Math.max(.02,Math.min(1,I(e.roughness,.8))),t[5]=Math.max(0,I(e.emissiveIntensity,0)),t[6]=e.normalMap?1:0,t[7]=e.roughnessMap?1:0,b.parseColor(e.emissiveColor,t,8,1),t[11]=e.emissiveMap?1:0,b.parseColor(e.color,t,12,1);const m=String(e.blend||"").toLowerCase()==="multiply";return t[15]=m?1:0,{name:String(e.name||""),albedo:e.albedo||"",normal:e.normalMap||"",roughness:e.roughnessMap||"",emissive:e.emissiveMap||"",columns:Math.max(1,Math.floor(I(e.columns,1))),rows:Math.max(1,Math.floor(I(e.rows,1))),multiply:m,uniforms:t}}function J(){let e=null,t=[],I=new Float32Array(0),P=[],m=120,h=new Float32Array(0),S=new Uint32Array(0),D=new Float32Array(0),T=new Float32Array(0),r=new Int32Array(0),n=new Float32Array(0),s=0,u=0;const d=new Float64Array(16);let c=-1;const L=new Float32Array(16);let w=new Float32Array(0),R=new Int32Array(0),O=new Int32Array(0),v=new Int32Array(0),_=0,f=0;const E=new Float32Array(24),x=(i,a)=>D[i]-D[a]||i-a;function X(i){const a=window.__gosx_scene3d_decal_pools;return a&&a[i]||null}function G(i){e=i,t=(Array.isArray(i.materials)?i.materials:[]).map(q),I=Array.isArray(i.records)?Float32Array.from(i.records):new Float32Array(0),P=Array.isArray(i.pools)?i.pools.filter(a=>typeof a=="string").slice(0,8):[],m=typeof i.maxDistance=="number"&&i.maxDistance>0?i.maxDistance:120,d.fill(-1),u++}function M(){let i=!1;for(let a=0;a<P.length;a++){const F=X(P[a]),l=F?F.count|0:0,o=F&&+F.revision||0;(d[a*2]!==l||d[a*2+1]!==o)&&(d[a*2]=l,d[a*2+1]=o,i=!0)}return i}function z(i){if(h.length>=i*$)return;const a=Math.min(te,Math.max(i,64,h.length/$*2));h=new Float32Array(a*$),S=new Uint32Array(a),D=new Float32Array(a),T=new Float32Array(a*K),r=new Int32Array(a),n=new Float32Array(a*4),w=new Float32Array(a*K),R=new Int32Array(a),O=new Int32Array(a),v=new Int32Array(a)}function N(){let i=I.length/$|0;const a=[];for(let o=0;o<P.length;o++){const y=X(P[o]);a.push(y),y&&y.data&&(i+=Math.min(y.count|0,y.data.length/$|0))}i=Math.min(i,te),z(i);let F=0;h.set(I.subarray(0,Math.min(I.length,i*$)),0),F=Math.min(I.length/$|0,i);for(let o=0;o<a.length&&F<i;o++){const y=a[o];if(!y||!y.data)continue;const V=Math.min(y.count|0,y.data.length/$|0,i-F);V>0&&(h.set(y.data.subarray(0,V*$),F*$),F+=V)}s=F;for(let o=0;o<s;o++)S[o]=o,D[o]=h[o*$+16];S.subarray(0,s).sort(x);let l=0;for(let o=0;o<s;o++){const y=S[o]*$,V=h[y+18]|0,W=t[V];if(!W)continue;const ee=l*K;H(h,y,W,T,ee),r[l]=V;const B=h[y+8],p=h[y+9],A=h[y+10];n[l*4]=h[y],n[l*4+1]=h[y+1],n[l*4+2]=h[y+2],n[l*4+3]=.5*Math.sqrt(B*B+p*p+A*A),l++}s=l,u++}function H(i,a,F,l,o){const y=i[a],V=i[a+1],W=i[a+2],ee=i[a+3],B=i[a+4],p=i[a+5],A=i[a+6],j=i[a+7],g=Math.max(1e-4,i[a+8]),k=Math.max(1e-4,i[a+9]),Y=Math.max(1e-4,i[a+10]),C=B+B,re=p+p,Q=A+A,ne=B*C,le=B*re,ae=B*Q,pe=p*re,se=p*Q,ie=A*Q,me=j*C,xe=j*re,ye=j*Q,ve=1-(pe+ie),ue=le+ye,oe=ae-xe,ce=le-ye,fe=1-(ne+ie),de=se+me,be=ae+xe,he=se-me,Te=1-(ne+pe);l[o]=ve*g,l[o+1]=ue*g,l[o+2]=oe*g,l[o+3]=0,l[o+4]=ce*k,l[o+5]=fe*k,l[o+6]=de*k,l[o+7]=0,l[o+8]=be*Y,l[o+9]=he*Y,l[o+10]=Te*Y,l[o+11]=0,l[o+12]=y,l[o+13]=V,l[o+14]=W,l[o+15]=1;const ge=ve*y+ue*V+oe*W,Ae=ce*y+fe*V+de*W,Pe=be*y+he*V+Te*W;l[o+16]=ve/g,l[o+17]=ce/k,l[o+18]=be/Y,l[o+19]=0,l[o+20]=ue/g,l[o+21]=fe/k,l[o+22]=he/Y,l[o+23]=0,l[o+24]=oe/g,l[o+25]=de/k,l[o+26]=Te/Y,l[o+27]=0,l[o+28]=-ge/g,l[o+29]=-Ae/k,l[o+30]=-Pe/Y,l[o+31]=1;const Ee=Math.max(0,Math.floor(i[a+11])),_e=F.columns,we=F.rows,Fe=Ee%_e,Se=Math.floor(Ee/_e)%we;l[o+32]=Fe/_e,l[o+33]=Se/we,l[o+34]=1/_e,l[o+35]=1/we,l[o+36]=U(i[a+12]),l[o+37]=U(i[a+13]),l[o+38]=U(i[a+14]),l[o+39]=i[a+15]*ee}return{get materials(){return t},get visible(){return f},get instances(){return w},get runs(){return _},get runMaterial(){return R},get runFirst(){return O},get runLength(){return v},get version(){return u},update(i){let a=!1;return e&&i.materials===e.materials&&i.records===e.records&&i.pools===e.pools&&i.maxDistance===e.maxDistance?e=i:(G(i),a=!0),(M()||a)&&(N(),a=!0),a},prepare(i,a,F){let l=c===u;for(let y=0;l&&y<16;y++)L[y]!==i[y]&&(l=!1);if(l)return f;L.set(i),c=u,b.frustumPlanes(E,i,a),f=0,_=0;let o=-1;for(let y=0;y<s;y++){const V=n[y*4],W=n[y*4+1],ee=n[y*4+2],B=n[y*4+3],p=V-F[0],A=W-F[1],j=ee-F[2],g=m+B;if(p*p+A*A+j*j>g*g||!b.sphereVisible(E,V,W,ee,B))continue;w.set(T.subarray(y*K,y*K+K),f*K);const k=r[y];k!==o&&(R[_]=k,O[_]=f,v[_]=0,_++,o=k),v[_-1]++,f++}return f}}}b.createDecalSet=J,b.DECAL_INSTANCE_FLOATS=K,b.DECAL_MATERIAL_FLOATS=Z})(),(()=>{const b=window.__gosx_scene3d_depthfx_api;if(!b)return;const $="1u,3u,7u,1u,7u,5u,0u,6u,2u,0u,4u,6u,2u,7u,3u,2u,6u,7u,0u,1u,5u,0u,5u,4u,4u,5u,7u,4u,7u,6u,0u,2u,3u,0u,3u,1u",K=`
@group(0) @binding(0) var src: texture_2d<f32>;
@group(0) @binding(1) var samp: sampler;
struct V { @builtin(position) p: vec4f, @location(0) uv: vec2f };
@vertex fn vs(@builtin(vertex_index) i: u32) -> V {
  let p = vec2f(f32((i << 1u) & 2u), f32(i & 2u));
  var o: V; o.p = vec4f(p * 2.0 - 1.0, 0.0, 1.0); o.uv = vec2f(p.x, 1.0 - p.y); return o;
}
@fragment fn fs(v: V) -> @location(0) vec4f { return textureSample(src, samp, v.uv); }`,te=`
struct Frame { viewProj: mat4x4f, invViewProj: mat4x4f, eye: vec4f, sunDir: vec4f, sunColor: vec4f, ambient: vec4f, size: vec4f };
struct Inst { model: mat4x4f, inv: mat4x4f, rect: vec4f, tint: vec4f };
struct Mat { fade: vec4f, pbr: vec4f, emissive: vec4f, color: vec4f };
@group(0) @binding(0) var<uniform> frame: Frame;
@group(0) @binding(1) var depthTex: texture_depth_2d;
@group(0) @binding(2) var<storage, read> insts: array<Inst>;
@group(1) @binding(0) var<uniform> mat: Mat;
@group(1) @binding(1) var albedoTex: texture_2d<f32>;
@group(1) @binding(2) var normalTex: texture_2d<f32>;
@group(1) @binding(3) var roughTex: texture_2d<f32>;
@group(1) @binding(4) var emissiveTex: texture_2d<f32>;
@group(1) @binding(5) var samp: sampler;
var<private> CUBE = array<u32, 36>(${$});
struct VOut { @builtin(position) pos: vec4f, @location(0) @interpolate(flat) id: u32 };
@vertex fn vs(@builtin(vertex_index) vi: u32, @builtin(instance_index) ii: u32) -> VOut {
  let c = CUBE[vi];
  let corner = vec3f(select(-0.5, 0.5, (c & 1u) != 0u), select(-0.5, 0.5, (c & 2u) != 0u), select(-0.5, 0.5, (c & 4u) != 0u));
  var o: VOut;
  o.pos = frame.viewProj * (insts[ii].model * vec4f(corner, 1.0));
  o.id = ii;
  return o;
}
fn depthAt(t: vec2i) -> f32 {
  let dims = vec2i(textureDimensions(depthTex));
  return textureLoad(depthTex, clamp(t, vec2i(0), dims - vec2i(1)), 0);
}
fn worldAt(t: vec2i) -> vec3f {
  let dims = vec2f(textureDimensions(depthTex));
  let uv = (vec2f(t) + 0.5) / dims;
  let w = frame.invViewProj * vec4f(uv.x * 2.0 - 1.0, 1.0 - uv.y * 2.0, depthAt(t), 1.0);
  return w.xyz / w.w;
}
struct FOut { @location(0) color: vec4f };
@fragment fn fs(in: VOut) -> FOut {
  let dims = vec2f(textureDimensions(depthTex));
  let tc = vec2i(in.pos.xy / frame.size.xy * dims);
  let d = depthAt(tc);
  if (d >= 0.999999) { discard; }
  let inst = insts[in.id];
  let p = worldAt(tc);
  let lp = (inst.inv * vec4f(p, 1.0)).xyz;
  if (any(abs(lp) > vec3f(0.5))) { discard; }
  let pl = worldAt(tc + vec2i(-1, 0)); let pr = worldAt(tc + vec2i(1, 0));
  let pu = worldAt(tc + vec2i(0, -1)); let pd = worldAt(tc + vec2i(0, 1));
  let useR = length(pr - p) < length(p - pl);
  let useD = length(pd - p) < length(p - pu);
  let dx = select(p - pl, pr - p, useR);
  let dy = select(p - pu, pd - p, useD);
  var n = normalize(cross(dx, dy));
  let toEye = frame.eye.xyz - p;
  if (dot(n, toEye) < 0.0) { n = -n; }
  let axisX = normalize(inst.model[0].xyz); let axisY = normalize(inst.model[1].xyz); let axisZ = normalize(inst.model[2].xyz);
  let angle = smoothstep(mat.fade.y, mat.fade.x, dot(n, axisZ));
  let e = mat.fade.z;
  let edge = (1.0 - smoothstep(0.5 - e, 0.5, abs(lp.x))) * (1.0 - smoothstep(0.5 - e, 0.5, abs(lp.y))) * (1.0 - smoothstep(0.5 - e, 0.5, abs(lp.z)));
  let inset = 0.5 / vec2f(textureDimensions(albedoTex, 0));
  let uv = clamp(vec2f(lp.x + 0.5, 0.5 - lp.y) * inst.rect.zw + inst.rect.xy, inst.rect.xy + inset, inst.rect.xy + inst.rect.zw - inset);
  let lx = (inst.inv * vec4f(select(p - pl, pr - p, useR) + p, 1.0)).xy - lp.xy;
  let ly = (inst.inv * vec4f(select(p - pu, pd - p, useD) + p, 1.0)).xy - lp.xy;
  let ddx = vec2f(lx.x, -lx.y) * inst.rect.zw;
  let ddy = vec2f(ly.x, -ly.y) * inst.rect.zw;
  let albedo = textureSampleGrad(albedoTex, samp, uv, ddx, ddy);
  let alpha = albedo.a * inst.tint.a * edge * angle;
  if (alpha <= 0.002) { discard; }
  let base = albedo.rgb * inst.tint.rgb * mat.color.rgb;
  var o: FOut;
  if (mat.color.w > 0.5) {
    o.color = vec4f(mix(vec3f(1.0), base, alpha), 0.0);
    return o;
  }
  var nm = n;
  if (mat.pbr.z > 0.5) {
    let t = textureSampleGrad(normalTex, samp, uv, ddx, ddy).xyz * 2.0 - 1.0;
    nm = normalize(n + (axisX * t.x + axisY * t.y) * mat.fade.w);
  }
  var rough = mat.pbr.x;
  if (mat.pbr.w > 0.5) { rough = rough * textureSampleGrad(roughTex, samp, uv, ddx, ddy).g; }
  rough = clamp(rough, 0.04, 1.0);
  let toSun = -frame.sunDir.xyz;
  let ndl = max(dot(nm, toSun), 0.0);
  var light = frame.ambient.rgb + frame.sunColor.rgb * ndl;
  let h = normalize(toSun + normalize(toEye));
  let shininess = 2.0 / (rough * rough) - 2.0;
  let spec = frame.sunColor.rgb * pow(max(dot(nm, h), 0.0), shininess) * (1.0 - rough) * 0.25 * select(0.0, 1.0, ndl > 0.0);
  var emit = vec3f(0.0);
  if (mat.emissive.w > 0.5) { emit = textureSampleGrad(emissiveTex, samp, uv, ddx, ddy).rgb * mat.emissive.rgb * mat.pbr.y; }
  else { emit = mat.emissive.rgb * mat.pbr.y; }
  o.color = vec4f((base * light + spec + emit) * alpha, alpha);
  return o;
}`;function Z(U){const q=b.createDecalSet(),J=U.createSampler({magFilter:"linear",minFilter:"linear",mipmapFilter:"linear",addressModeU:"clamp-to-edge",addressModeV:"clamp-to-edge",maxAnisotropy:4}),e=U.createSampler({magFilter:"linear",minFilter:"linear"}),t=new Float32Array(56),I=new Float32Array(16),P=new Float32Array(3),m=new Float32Array(3),h=new Float32Array(3),S=new Float32Array(3),D=U.createBuffer({label:"gosx-decal-frame",size:224,usage:GPUBufferUsage.UNIFORM|GPUBufferUsage.COPY_DST});let T=null,r=0;const n=new Map;let s=null,u=null,d=null,c=null,L=null,w=null,R=null;const O=new WeakMap;let v=!1,_=!1,f=[],E=null;const x={};function X(p,A,j){if(x[p])return x[p];const g=U.createTexture({size:[1,1],format:j,usage:GPUTextureUsage.TEXTURE_BINDING|GPUTextureUsage.COPY_DST});return U.queue.writeTexture({texture:g},new Uint8Array(A),{bytesPerRow:4},[1,1]),x[p]={texture:g,view:g.createView()}}const G=new Map;let M=new Map;function z(){return N||(N=U.createShaderModule({label:"gosx-decal-blit",code:K}))}let N=null;function H(){return s||(s=U.createBindGroupLayout({entries:[{binding:0,visibility:GPUShaderStage.FRAGMENT,texture:{sampleType:"float"}},{binding:1,visibility:GPUShaderStage.FRAGMENT,sampler:{type:"filtering"}}]}))}function i(p,A){let j=A.get(p);if(!j){const g=z();j=U.createRenderPipeline({label:"gosx-decal-blit:"+p,layout:U.createPipelineLayout({bindGroupLayouts:[H()]}),vertex:{module:g,entryPoint:"vs"},fragment:{module:g,entryPoint:"fs",targets:[{format:p}]},primitive:{topology:"triangle-list"}}),A.set(p,j)}return j}function a(p,A,j){const g=U.createCommandEncoder({label:"gosx-decal-mips"}),k=i(A,M);for(let Y=1;Y<j;Y++){const C=U.createBindGroup({layout:H(),entries:[{binding:0,resource:p.createView({baseMipLevel:Y-1,mipLevelCount:1})},{binding:1,resource:e}]}),re=g.beginRenderPass({colorAttachments:[{view:p.createView({baseMipLevel:Y,mipLevelCount:1}),loadOp:"clear",clearValue:{r:0,g:0,b:0,a:0},storeOp:"store"}]});re.setPipeline(k),re.setBindGroup(0,C),re.draw(3),re.end()}U.queue.submit([g.finish()])}function F(p,A){const j=A==="albedo"||A==="emissive",g=A==="normal"?X("normal",[128,128,255,255],"rgba8unorm"):A==="rough"?X("rough",[255,255,255,255],"rgba8unorm"):A==="emissive"?X("emissive",[0,0,0,255],"rgba8unorm-srgb"):X("albedo",[255,255,255,255],"rgba8unorm-srgb");if(!p)return{view:g.view,rev:0};const k=A+"|"+p;let Y=G.get(k);return Y||(Y={view:g.view,rev:0,texture:null},G.set(k,Y),b.loadBitmap(p).then(C=>{if(_){C.close&&C.close();return}const re=j?"rgba8unorm-srgb":"rgba8unorm",Q=Math.floor(Math.log2(Math.max(C.width,C.height)))+1,ne=U.createTexture({size:[C.width,C.height],format:re,mipLevelCount:Q,usage:GPUTextureUsage.TEXTURE_BINDING|GPUTextureUsage.COPY_DST|GPUTextureUsage.RENDER_ATTACHMENT});U.queue.copyExternalImageToTexture({source:C},{texture:ne},[C.width,C.height]),Q>1&&a(ne,re,Q),C.close&&C.close(),Y.texture=ne,Y.view=ne.createView(),Y.rev++}).catch(C=>{console.warn("[gosx] decal texture failed:",p,C&&C.message||C)}),Y)}function l(){if(u)return;u=U.createBindGroupLayout({entries:[{binding:0,visibility:GPUShaderStage.VERTEX|GPUShaderStage.FRAGMENT,buffer:{type:"uniform"}},{binding:1,visibility:GPUShaderStage.FRAGMENT,texture:{sampleType:"depth"}},{binding:2,visibility:GPUShaderStage.VERTEX|GPUShaderStage.FRAGMENT,buffer:{type:"read-only-storage"}}]});const p=A=>({binding:A,visibility:GPUShaderStage.FRAGMENT,texture:{sampleType:"float"}});d=U.createBindGroupLayout({entries:[{binding:0,visibility:GPUShaderStage.FRAGMENT,buffer:{type:"uniform"}},p(1),p(2),p(3),p(4),{binding:5,visibility:GPUShaderStage.FRAGMENT,sampler:{type:"filtering"}}]}),c=U.createPipelineLayout({bindGroupLayouts:[u,d]})}function o(p,A){const j=p+(A?"|mul":"|lit");let g=n.get(j);if(g)return g;l();let k=n.get("module");k||(k=U.createShaderModule({label:"gosx-decals",code:te}),n.set("module",k));const Y=A?{color:{srcFactor:"dst",dstFactor:"zero",operation:"add"},alpha:{srcFactor:"zero",dstFactor:"one",operation:"add"}}:{color:{srcFactor:"one",dstFactor:"one-minus-src-alpha",operation:"add"},alpha:{srcFactor:"one",dstFactor:"one-minus-src-alpha",operation:"add"}};return g=U.createRenderPipeline({label:"gosx-decals:"+j,layout:c,vertex:{module:k,entryPoint:"vs"},fragment:{module:k,entryPoint:"fs",targets:[{format:p,blend:Y}]},primitive:{topology:"triangle-list",cullMode:"front",frontFace:"ccw"}}),n.set(j,g),g}function y(){f.forEach(A=>A.uniformBuffer&&A.uniformBuffer.destroy()),f=q.materials.map(A=>{const j=U.createBuffer({label:"gosx-decal-material",size:64,usage:GPUBufferUsage.UNIFORM|GPUBufferUsage.COPY_DST});return U.queue.writeBuffer(j,0,A.uniforms),{uniformBuffer:j,group:null,revSum:-1,tex:[F(A.albedo,"albedo"),F(A.normal,"normal"),F(A.roughness,"rough"),F(A.emissive,"emissive")]}})}function V(p){const A=p.tex[0].rev+p.tex[1].rev*1e3+p.tex[2].rev*1e6+p.tex[3].rev*1e9;return p.group&&p.revSum===A||(l(),p.group=U.createBindGroup({layout:d,entries:[{binding:0,resource:{buffer:p.uniformBuffer}},{binding:1,resource:p.tex[0].view},{binding:2,resource:p.tex[1].view},{binding:3,resource:p.tex[2].view},{binding:4,resource:p.tex[3].view},{binding:5,resource:J}]}),p.revSum=A),p.group}function W(p){const A=Math.max(160,p*4);T&&r>=A||(T&&T.destroy(),r=Math.max(A,r*2,10240),T=U.createBuffer({label:"gosx-decal-instances",size:r,usage:GPUBufferUsage.STORAGE|GPUBufferUsage.COPY_DST}),L=null)}function ee(p,A,j,g,k,Y,C,re,Q){if(v||_||!Q||!Q.viewProj)return!1;try{(q.update(p)||E!==q.materials)&&(E=q.materials,y());const le=Q.camera||{};P[0]=+le.x||0,P[1]=+le.y||0,P[2]=+le.z||0;const ae=q.prepare(Q.viewProj,!0,P);if(ae===0||!b.invert(I,Q.viewProj))return!1;W(ae*b.DECAL_INSTANCE_FLOATS),U.queue.writeBuffer(T,0,q.instances,0,ae*b.DECAL_INSTANCE_FLOATS);const pe=b.lightSummary(Q,m,h,S);t.set(Q.viewProj,0),t.set(I,16),t[32]=P[0],t[33]=P[1],t[34]=P[2],t[36]=h[0],t[37]=h[1],t[38]=h[2],t[39]=pe?1:0,t[40]=S[0],t[41]=S[1],t[42]=S[2],t[44]=m[0],t[45]=m[1],t[46]=m[2],t[48]=C,t[49]=re,U.queue.writeBuffer(D,0,t),l(),(!L||w!==Y||R!==T)&&(L=U.createBindGroup({layout:u,entries:[{binding:0,resource:{buffer:D}},{binding:1,resource:Y},{binding:2,resource:{buffer:T}}]}),w=Y,R=T);let se=O.get(j);se||(se=U.createBindGroup({layout:H(),entries:[{binding:0,resource:j},{binding:1,resource:e}]}),O.set(j,se));const ie=A.beginRenderPass({label:"gosx-decals",colorAttachments:[{view:g,loadOp:"clear",clearValue:{r:0,g:0,b:0,a:0},storeOp:"store"}]});ie.setPipeline(i(k,B)),ie.setBindGroup(0,se),ie.draw(3),ie.setBindGroup(0,L);const me=q.materials,xe=q.runMaterial,ye=q.runFirst,ve=q.runLength;let ue="";for(let oe=0;oe<q.runs;oe++){const ce=xe[oe],fe=me[ce].multiply,de=fe?"m":"l";ue!==de&&(ie.setPipeline(o(k,fe)),ue=de),ie.setBindGroup(1,V(f[ce])),ie.draw(36,ve[oe],0,ye[oe])}return ie.end(),b.stats.decals.frames++,b.stats.decals.visible=ae,!0}catch(ne){return v=!0,console.error("[gosx] decal pass disabled:",ne&&ne.message||ne),!1}}const B=new Map;return{apply:ee,stats:()=>({visible:q.visible,runs:q.runs}),dispose(){_=!0,T&&T.destroy(),D.destroy(),f.forEach(p=>p.uniformBuffer&&p.uniformBuffer.destroy()),G.forEach(p=>p.texture&&p.texture.destroy()),Object.keys(x).forEach(p=>x[p].texture.destroy())}}}b.webgpu.decals=Z})(),(()=>{const b=window.__gosx_scene3d_depthfx_api;if(!b)return;const K=`#version 300 es
precision highp float;
precision highp int;
layout(location=0) in vec4 a_m0; layout(location=1) in vec4 a_m1; layout(location=2) in vec4 a_m2; layout(location=3) in vec4 a_m3;
layout(location=4) in vec4 a_i0; layout(location=5) in vec4 a_i1; layout(location=6) in vec4 a_i2; layout(location=7) in vec4 a_i3;
layout(location=8) in vec4 a_rect; layout(location=9) in vec4 a_tint;
uniform mat4 u_viewProj;
flat out vec4 v_i0; flat out vec4 v_i1; flat out vec4 v_i2; flat out vec4 v_i3;
flat out vec4 v_rect; flat out vec4 v_tint;
flat out vec3 v_ax; flat out vec3 v_ay; flat out vec3 v_az;
const int CUBE[36] = int[36](${[1,3,7,1,7,5,0,6,2,0,4,6,2,7,3,2,6,7,0,1,5,0,5,4,4,5,7,4,7,6,0,2,3,0,3,1].join(",")});
void main() {
  int c = CUBE[gl_VertexID];
  vec3 corner = vec3((c & 1) != 0 ? 0.5 : -0.5, (c & 2) != 0 ? 0.5 : -0.5, (c & 4) != 0 ? 0.5 : -0.5);
  mat4 model = mat4(a_m0, a_m1, a_m2, a_m3);
  gl_Position = u_viewProj * (model * vec4(corner, 1.0));
  v_i0 = a_i0; v_i1 = a_i1; v_i2 = a_i2; v_i3 = a_i3; v_rect = a_rect; v_tint = a_tint;
  v_ax = normalize(a_m0.xyz); v_ay = normalize(a_m1.xyz); v_az = normalize(a_m2.xyz);
}`,te=`#version 300 es
precision highp float;
precision highp int;
precision highp sampler2D;
flat in vec4 v_i0; flat in vec4 v_i1; flat in vec4 v_i2; flat in vec4 v_i3;
flat in vec4 v_rect; flat in vec4 v_tint;
flat in vec3 v_ax; flat in vec3 v_ay; flat in vec3 v_az;
uniform mat4 u_invViewProj;
uniform vec3 u_eye; uniform vec3 u_sunDir; uniform vec3 u_sunColor; uniform vec3 u_ambient; uniform vec2 u_size;
uniform sampler2D u_depth; uniform sampler2D u_albedo; uniform sampler2D u_normal; uniform sampler2D u_rough; uniform sampler2D u_emissive;
uniform vec4 u_fade; uniform vec4 u_pbr; uniform vec4 u_emissiveColor; uniform vec4 u_color;
out vec4 fragColor;
float depthAt(ivec2 t) {
  ivec2 dims = textureSize(u_depth, 0);
  return texelFetch(u_depth, clamp(t, ivec2(0), dims - ivec2(1)), 0).r;
}
vec3 worldAt(ivec2 t) {
  vec2 uv = (vec2(t) + 0.5) / vec2(textureSize(u_depth, 0));
  vec4 w = u_invViewProj * vec4(uv * 2.0 - 1.0, depthAt(t) * 2.0 - 1.0, 1.0);
  return w.xyz / w.w;
}
void main() {
  vec2 dims = vec2(textureSize(u_depth, 0));
  ivec2 tc = ivec2(gl_FragCoord.xy / u_size * dims);
  float d = depthAt(tc);
  if (d >= 0.999999) discard;
  mat4 inv = mat4(v_i0, v_i1, v_i2, v_i3);
  vec3 p = worldAt(tc);
  vec3 lp = (inv * vec4(p, 1.0)).xyz;
  if (any(greaterThan(abs(lp), vec3(0.5)))) discard;
  vec3 pl = worldAt(tc + ivec2(-1, 0)); vec3 pr = worldAt(tc + ivec2(1, 0));
  vec3 pu = worldAt(tc + ivec2(0, -1)); vec3 pd = worldAt(tc + ivec2(0, 1));
  bool useR = length(pr - p) < length(p - pl);
  bool useD = length(pd - p) < length(p - pu);
  vec3 dx = useR ? pr - p : p - pl;
  vec3 dy = useD ? pd - p : p - pu;
  vec3 n = normalize(cross(dx, dy));
  vec3 toEye = u_eye - p;
  if (dot(n, toEye) < 0.0) n = -n;
  float angle = smoothstep(u_fade.y, u_fade.x, dot(n, v_az));
  float e = u_fade.z;
  float edge = (1.0 - smoothstep(0.5 - e, 0.5, abs(lp.x))) * (1.0 - smoothstep(0.5 - e, 0.5, abs(lp.y))) * (1.0 - smoothstep(0.5 - e, 0.5, abs(lp.z)));
  vec2 inset = 0.5 / vec2(textureSize(u_albedo, 0));
  vec2 uv = clamp(vec2(lp.x + 0.5, 0.5 - lp.y) * v_rect.zw + v_rect.xy, v_rect.xy + inset, v_rect.xy + v_rect.zw - inset);
  vec2 lx = (inv * vec4(dx + p, 1.0)).xy - lp.xy;
  vec2 ly = (inv * vec4(dy + p, 1.0)).xy - lp.xy;
  vec2 ddx = vec2(lx.x, -lx.y) * v_rect.zw;
  vec2 ddy = vec2(ly.x, -ly.y) * v_rect.zw;
  vec4 albedo = textureGrad(u_albedo, uv, ddx, ddy);
  float alpha = albedo.a * v_tint.a * edge * angle;
  if (alpha <= 0.002) discard;
  vec3 base = albedo.rgb * v_tint.rgb * u_color.rgb;
  if (u_color.a > 0.5) { fragColor = vec4(mix(vec3(1.0), base, alpha), 0.0); return; }
  vec3 nm = n;
  if (u_pbr.z > 0.5) {
    vec3 t = textureGrad(u_normal, uv, ddx, ddy).xyz * 2.0 - 1.0;
    nm = normalize(n + (v_ax * t.x + v_ay * t.y) * u_fade.w);
  }
  float rough = u_pbr.x;
  if (u_pbr.w > 0.5) rough *= textureGrad(u_rough, uv, ddx, ddy).g;
  rough = clamp(rough, 0.04, 1.0);
  vec3 toSun = -u_sunDir;
  float ndl = max(dot(nm, toSun), 0.0);
  vec3 light = u_ambient + u_sunColor * ndl;
  vec3 h = normalize(toSun + normalize(toEye));
  float shininess = 2.0 / (rough * rough) - 2.0;
  vec3 spec = ndl > 0.0 ? u_sunColor * pow(max(dot(nm, h), 0.0), shininess) * (1.0 - rough) * 0.25 : vec3(0.0);
  vec3 emit = u_emissiveColor.w > 0.5 ? textureGrad(u_emissive, uv, ddx, ddy).rgb * u_emissiveColor.rgb * u_pbr.y : u_emissiveColor.rgb * u_pbr.y;
  fragColor = vec4((base * light + spec + emit) * alpha, alpha);
}`,Z=`#version 300 es
out vec2 v_uv;
void main() { vec2 p = vec2(float((gl_VertexID << 1) & 2), float(gl_VertexID & 2)); v_uv = p; gl_Position = vec4(p * 2.0 - 1.0, 0.0, 1.0); }`,U=`#version 300 es
precision highp float;
in vec2 v_uv; uniform sampler2D u_texture; out vec4 fragColor;
void main() { fragColor = texture(u_texture, v_uv); }`;function q(e,t,I){const P=(D,T)=>{const r=e.createShader(D);if(e.shaderSource(r,T),e.compileShader(r),!e.getShaderParameter(r,e.COMPILE_STATUS))throw new Error("decal shader: "+e.getShaderInfoLog(r));return r},m=e.createProgram(),h=P(e.VERTEX_SHADER,t),S=P(e.FRAGMENT_SHADER,I);if(e.attachShader(m,h),e.attachShader(m,S),e.linkProgram(m),e.deleteShader(h),e.deleteShader(S),!e.getProgramParameter(m,e.LINK_STATUS))throw new Error("decal program: "+e.getProgramInfoLog(m));return m}function J(e){const t=b.createDecalSet(),I=b.DECAL_INSTANCE_FLOATS*4,P=new Float32Array(16),m=new Float32Array(3),h=new Float32Array(3),S=new Float32Array(3),D=new Float32Array(3);let T=null,r=null,n=null,s=null,u=0,d=null;const c={},L={};let w=!1,R=!1,O=[],v=null;const _=new Map,f={};function E(z,N){if(f[z])return f[z];const H=e.createTexture();return e.bindTexture(e.TEXTURE_2D,H),e.texImage2D(e.TEXTURE_2D,0,e.RGBA8,1,1,0,e.RGBA,e.UNSIGNED_BYTE,new Uint8Array(N)),e.texParameteri(e.TEXTURE_2D,e.TEXTURE_MIN_FILTER,e.NEAREST),e.texParameteri(e.TEXTURE_2D,e.TEXTURE_MAG_FILTER,e.NEAREST),f[z]=H}function x(z,N){const H=E(N,N==="normal"?[128,128,255,255]:N==="emissive"?[0,0,0,255]:[255,255,255,255]);if(!z)return{texture:H};const i=N+"|"+z;let a=_.get(i);return a||(a={texture:H},_.set(i,a),b.loadBitmap(z).then(F=>{if(R){F.close&&F.close();return}const l=e.createTexture();e.activeTexture(e.TEXTURE0),e.bindTexture(e.TEXTURE_2D,l),e.pixelStorei(e.UNPACK_FLIP_Y_WEBGL,!1),e.pixelStorei(e.UNPACK_PREMULTIPLY_ALPHA_WEBGL,!1);const o=N==="albedo"||N==="emissive";e.texImage2D(e.TEXTURE_2D,0,o?e.SRGB8_ALPHA8:e.RGBA8,F.width,F.height,0,e.RGBA,e.UNSIGNED_BYTE,F),e.generateMipmap(e.TEXTURE_2D),e.texParameteri(e.TEXTURE_2D,e.TEXTURE_MIN_FILTER,e.LINEAR_MIPMAP_LINEAR),e.texParameteri(e.TEXTURE_2D,e.TEXTURE_MAG_FILTER,e.LINEAR),e.texParameteri(e.TEXTURE_2D,e.TEXTURE_WRAP_S,e.CLAMP_TO_EDGE),e.texParameteri(e.TEXTURE_2D,e.TEXTURE_WRAP_T,e.CLAMP_TO_EDGE),F.close&&F.close(),a.texture=l,a.loaded=!0}).catch(F=>{console.warn("[gosx] decal texture failed:",z,F&&F.message||F)}),a)}function X(){T=q(e,K,te);for(const z of["u_viewProj","u_invViewProj","u_eye","u_sunDir","u_sunColor","u_ambient","u_size","u_depth","u_albedo","u_normal","u_rough","u_emissive","u_fade","u_pbr","u_emissiveColor","u_color"])c[z]=e.getUniformLocation(T,z);r=q(e,Z,U),L.texture=e.getUniformLocation(r,"u_texture"),d=e.createVertexArray(),s=e.createBuffer(),n=e.createVertexArray()}function G(z){e.bindVertexArray(n),e.bindBuffer(e.ARRAY_BUFFER,s);for(let N=0;N<10;N++)e.enableVertexAttribArray(N),e.vertexAttribPointer(N,4,e.FLOAT,!1,I,z*I+N*16),e.vertexAttribDivisor(N,1)}function M(z,N,H,i,a,F,l){if(w||R||!F||!l||!l.viewProj)return!1;try{T||X(),(t.update(z)||v!==t.materials)&&(v=t.materials,O=t.materials.map(g=>[x(g.albedo,"albedo"),x(g.normal,"normal"),x(g.roughness,"rough"),x(g.emissive,"emissive")]));const y=l.camera||{};m[0]=+y.x||0,m[1]=+y.y||0,m[2]=+y.z||0;const V=t.prepare(l.viewProj,!1,m);if(V===0||!b.invert(P,l.viewProj))return!1;b.lightSummary(l,h,S,D),e.bindFramebuffer(e.FRAMEBUFFER,H?H.fbo:null),e.viewport(0,0,i,a),e.disable(e.DEPTH_TEST),e.disable(e.BLEND),e.disable(e.CULL_FACE),e.useProgram(r),e.bindVertexArray(d),e.activeTexture(e.TEXTURE0),e.bindTexture(e.TEXTURE_2D,N),e.uniform1i(L.texture,0),e.drawArrays(e.TRIANGLES,0,3),e.bindBuffer(e.ARRAY_BUFFER,s);const W=V*I;W>u&&(u=Math.max(W,u*2),e.bufferData(e.ARRAY_BUFFER,u,e.DYNAMIC_DRAW)),e.bufferSubData(e.ARRAY_BUFFER,0,t.instances,0,V*b.DECAL_INSTANCE_FLOATS),e.useProgram(T),e.uniformMatrix4fv(c.u_viewProj,!1,l.viewProj),e.uniformMatrix4fv(c.u_invViewProj,!1,P),e.uniform3fv(c.u_eye,m),e.uniform3fv(c.u_sunDir,S),e.uniform3fv(c.u_sunColor,D),e.uniform3fv(c.u_ambient,h),e.uniform2f(c.u_size,i,a),e.activeTexture(e.TEXTURE1),e.bindTexture(e.TEXTURE_2D,F),e.uniform1i(c.u_depth,1),e.uniform1i(c.u_albedo,2),e.uniform1i(c.u_normal,3),e.uniform1i(c.u_rough,4),e.uniform1i(c.u_emissive,5),e.enable(e.BLEND),e.enable(e.CULL_FACE),e.cullFace(e.FRONT);const ee=t.materials,B=t.runMaterial,p=t.runFirst,A=t.runLength;let j=-1;for(let g=0;g<t.runs;g++){const k=B[g],Y=ee[k],C=Y.uniforms;j!==(Y.multiply?1:0)&&(j=Y.multiply?1:0,Y.multiply?e.blendFuncSeparate(e.DST_COLOR,e.ZERO,e.ZERO,e.ONE):e.blendFuncSeparate(e.ONE,e.ONE_MINUS_SRC_ALPHA,e.ONE,e.ONE_MINUS_SRC_ALPHA));const re=O[k];for(let Q=0;Q<4;Q++)e.activeTexture(e.TEXTURE2+Q),e.bindTexture(e.TEXTURE_2D,re[Q].texture);e.uniform4f(c.u_fade,C[0],C[1],C[2],C[3]),e.uniform4f(c.u_pbr,C[4],C[5],C[6],C[7]),e.uniform4f(c.u_emissiveColor,C[8],C[9],C[10],C[11]),e.uniform4f(c.u_color,C[12],C[13],C[14],C[15]),G(p[g]),e.drawArraysInstanced(e.TRIANGLES,0,36,A[g])}for(let g=0;g<10;g++)e.disableVertexAttribArray(g);e.bindVertexArray(null),e.disable(e.BLEND),e.disable(e.CULL_FACE);for(let g=5;g>=1;g--)e.activeTexture(e.TEXTURE0+g),e.bindTexture(e.TEXTURE_2D,null);return e.activeTexture(e.TEXTURE0),b.stats.decals.frames++,b.stats.decals.visible=V,!0}catch(o){return w=!0,console.error("[gosx] decal pass disabled:",o&&o.message||o),!1}}return{apply:M,stats:()=>({visible:t.visible,runs:t.runs}),dispose(){R=!0,T&&e.deleteProgram(T),r&&e.deleteProgram(r),s&&e.deleteBuffer(s),n&&e.deleteVertexArray(n),d&&e.deleteVertexArray(d),_.forEach(z=>z.loaded&&e.deleteTexture(z.texture)),Object.keys(f).forEach(z=>e.deleteTexture(f[z]))}}}b.webgl.decals=J})(),(()=>{const b=window.__gosx_scene3d_depthfx_api;if(!b)return;const $=["survival","minimal","low","off"],K=["reduced","balanced","medium"],te={low:{samples:6,tile:32},medium:{samples:10,tile:24},high:{samples:16,tile:16}},Z={skip:!1,samples:10,tile:24,maxBlur:32,strength:.5,quality:"medium"};function U(P,m,h){const S=b.qualityTier(m);Z.skip=$.indexOf(S)>=0;let D=typeof P.quality=="string"&&te[P.quality]?P.quality:"medium";K.indexOf(S)>=0&&(D="low");const T=te[D];Z.quality=D,Z.samples=T.samples,Z.tile=T.tile;const r=Number(P.strength);Z.strength=isFinite(r)&&r>0?Math.min(1,r):.5;const n=Number(P.maxBlur),s=(isFinite(n)&&n>0?Math.min(128,n):32)*Math.max(.25,h/1080);return Z.maxBlur=Math.max(1,Math.min(s,Z.tile)),Z}function q(){const P=new Float32Array(16),m=new Float32Array(16),h=new Float32Array(3);let S=!1,D=!1,T=0;return{get prev(){return P},begin(r,n,s){if(S&&r-T>=3){const u=n[0]-h[0],d=n[1]-h[1],c=n[2]-h[2];D=r-T<=250&&u*u+d*d+c*c<100,P.set(m)}return(!S||r-T>=3)&&(T=r),S=!0,m.set(s),h.set(n),D},reset(){S=!1,D=!1}}}const J=new Float32Array(4),e=new Float32Array(4);function t(P,m,h,S,D){const T=m[0]*h+m[4]*S+m[8]*D+m[12],r=m[1]*h+m[5]*S+m[9]*D+m[13],n=m[2]*h+m[6]*S+m[10]*D+m[14],s=m[3]*h+m[7]*S+m[11]*D+m[15];P[0]=T/s,P[1]=r/s,P[2]=n/s}function I(P,m,h,S,D){t(J,P,0,0,m),t(e,P,0,0,h);const T=e[0]-J[0],r=e[1]-J[1],n=e[2]-J[2],s=Math.hypot(T,r,n)||1;D[0]=T/s,D[1]=r/s,D[2]=n/s,S[0]=J[0],S[1]=J[1],S[2]=J[2]}b.motionBlurPlan=U,b.createMotionHistory=q,b.eyeAndForward=I})(),(()=>{const b=window.__gosx_scene3d_depthfx_api;if(!b||!b.motionBlurPlan)return;const $=`
struct VOut { @builtin(position) pos: vec4f };
@vertex fn vs(@builtin(vertex_index) i: u32) -> VOut {
  let p = vec2f(f32((i << 1u) & 2u), f32(i & 2u));
  var o: VOut; o.pos = vec4f(p * 2.0 - 1.0, 0.0, 1.0); return o;
}`,te=$+`
struct Params {
  invViewProj: mat4x4f, prevViewProj: mat4x4f, viewProj: mat4x4f,
  eye: vec4f, forward: vec4f,
  settings: vec4f,  // strength, maxBlur px, hasPrevious, hasMoverVelocity
  size: vec4f,      // width, height, 1/width, 1/height
};`+`
@group(0) @binding(0) var<uniform> P: Params;
@group(0) @binding(1) var depthTex: texture_depth_2d;
@group(0) @binding(2) var moverTex: texture_2d<f32>;
@fragment fn fs(in: VOut) -> @location(0) vec4f {
  let dims = vec2i(textureDimensions(depthTex));
  let px = clamp(vec2i(in.pos.xy), vec2i(0), dims - vec2i(1));
  let depth = textureLoad(depthTex, px, 0);
  let uv = (vec2f(px) + 0.5) * P.size.zw;
  var world = P.invViewProj * vec4f(uv.x * 2.0 - 1.0, 1.0 - uv.y * 2.0, depth, 1.0);
  world = world / world.w;
  var v = vec2f(0.0);
  if (P.settings.z > 0.5) {
    let prev = P.prevViewProj * vec4f(world.xyz, 1.0);
    if (prev.w > 0.0) { v = vec2f(prev.x / prev.w * 0.5 + 0.5, 0.5 - prev.y / prev.w * 0.5) - uv; }
  }
  if (P.settings.w > 0.5) {
    let m = textureLoad(moverTex, px, 0);
    let surfaceW = (P.viewProj * vec4f(world.xyz, 1.0)).w;
    if (m.a > 0.5 && m.a < 1.5 && abs(m.b - surfaceW) <= 0.02 * abs(surfaceW) + 0.02) { v = m.xy; }
  }
  var vp = v * P.size.xy * P.settings.x;
  let len = length(vp);
  if (len > P.settings.y) { vp = vp * (P.settings.y / len); }
  if (len < 0.5) { vp = vec2f(0.0); }
  return vec4f(vp, -dot(world.xyz - P.eye.xyz, P.forward.xyz), 1.0); // view-space z: negative in front, as the gather expects
}`;function Z(e){return $+`
@group(0) @binding(0) var velTex: texture_2d<f32>;
@fragment fn fs(in: VOut) -> @location(0) vec2f {
  let dims = vec2i(textureDimensions(velTex));
  let base = vec2i(in.pos.xy) * ${e};
  var best = vec2f(0.0); var bestLen = 0.0;
  for (var y = 0; y < ${e}; y++) {
    for (var x = 0; x < ${e}; x++) {
      let p = base + vec2i(x, y);
      if (p.x >= dims.x || p.y >= dims.y) { continue; }
      let v = textureLoad(velTex, p, 0).xy;
      let l = dot(v, v);
      if (l > bestLen) { bestLen = l; best = v; }
    }
  }
  return best;
}`}const U=$+`
@group(0) @binding(0) var tileTex: texture_2d<f32>;
@fragment fn fs(in: VOut) -> @location(0) vec2f {
  let dims = vec2i(textureDimensions(tileTex));
  let c = vec2i(in.pos.xy);
  var best = vec2f(0.0); var bestLen = 0.0;
  for (var y = -1; y <= 1; y++) {
    for (var x = -1; x <= 1; x++) {
      let v = textureLoad(tileTex, clamp(c + vec2i(x, y), vec2i(0), dims - vec2i(1)), 0).xy;
      let l = dot(v, v);
      if (l > bestLen) { bestLen = l; best = v; }
    }
  }
  return best;
}`;function q(e,t){return $+`
@group(0) @binding(0) var colorTex: texture_2d<f32>;
@group(0) @binding(1) var velTex: texture_2d<f32>;
@group(0) @binding(2) var neighborTex: texture_2d<f32>;
const SAMPLES = ${t};
const SOFT_Z = 0.25;
fn softDepth(a: f32, b: f32, extent: f32) -> f32 { return clamp(1.0 - (a - b) / extent, 0.0, 1.0); }
fn cone(dist: f32, len: f32) -> f32 { return clamp(1.0 - dist / max(len, 0.0001), 0.0, 1.0); }
fn cylinder(dist: f32, len: f32) -> f32 { return 1.0 - smoothstep(0.95 * len, 1.05 * len, dist); }
@fragment fn fs(in: VOut) -> @location(0) vec4f {
  let dims = vec2i(textureDimensions(colorTex));
  let px = vec2i(in.pos.xy);
  let c0 = textureLoad(colorTex, px, 0);
  let tileDims = vec2i(textureDimensions(neighborTex));
  let vN = textureLoad(neighborTex, clamp(px / ${e}, vec2i(0), tileDims - vec2i(1)), 0).xy;
  let lenN = length(vN);
  if (lenN < 0.5) { return c0; }
  let center = textureLoad(velTex, px, 0);
  let vX = center.xy; let zX = center.z;
  let lenX = length(vX);
  let pos = vec2f(px) + 0.5;
  let jitter = fract(52.9829189 * fract(dot(in.pos.xy, vec2f(0.06711056, 0.00583715)))) - 0.5;
  let extent = max(0.25, abs(zX) * 0.1);
  var sum = c0 * (1.0 / max(lenX, 1.0) * f32(SAMPLES) / 40.0);
  var weight = (1.0 / max(lenX, 1.0) * f32(SAMPLES) / 40.0);
  for (var i = 0; i < SAMPLES; i++) {
    let t = (f32(i) + 0.5 + jitter * 0.5) / f32(SAMPLES) * 2.0 - 1.0;
    // Alternate between the neighborhood's dominant velocity and this pixel's own.
    let dir = select(vN, select(vN, vX, lenX > 0.5), (i & 1) == 1);
    let q = clamp(vec2i(pos + dir * t), vec2i(0), dims - vec2i(1));
    let s = textureLoad(velTex, q, 0);
    let vY = s.xy; let zY = s.z;
    let dist = length(vec2f(q) + 0.5 - pos);
    let f = softDepth(zX, zY, extent);   // Y in front of X
    let b = softDepth(zY, zX, extent);   // Y behind X
    let lenY = length(vY);
    let alpha = f * cone(dist, lenY) + b * cone(dist, lenX) + cylinder(dist, lenY) * cylinder(dist, lenX) * 2.0;
    sum = sum + textureLoad(colorTex, q, 0) * alpha;
    weight = weight + alpha;
  }
  return vec4f(sum.rgb / max(weight, 0.0001), c0.a);
}`}function J(e){const t=b.createMotionHistory(),I=new Float32Array(64),P=e.createBuffer({label:"gosx-motion-blur",size:256,usage:GPUBufferUsage.UNIFORM|GPUBufferUsage.COPY_DST}),m=new Float32Array(16),h=new Float32Array(3),S=new Float32Array(3),D=e.createTexture({size:[1,1],format:"rgba16float",usage:GPUTextureUsage.TEXTURE_BINDING}),T=D.createView(),r=new Map,n=new Map;let s=null,u=!1,d=!1;function c(f,E){let x=n.get(f);return x||(x=e.createBindGroupLayout({entries:E}),n.set(f,x)),x}const L=(f,E="unfilterable-float")=>({binding:f,visibility:GPUShaderStage.FRAGMENT,texture:{sampleType:E}});function w(f,E,x,X){let G=r.get(f);if(!G){const M=e.createShaderModule({label:"gosx-motion-blur:"+f,code:E});G=e.createRenderPipeline({label:"gosx-motion-blur:"+f,layout:e.createPipelineLayout({bindGroupLayouts:[x]}),vertex:{module:M,entryPoint:"vs"},fragment:{module:M,entryPoint:"fs",targets:[{format:X}]},primitive:{topology:"triangle-list"}}),r.set(f,G)}return G}function R(f,E,x){if(s&&s.w===f&&s.h===E&&s.tile===x)return s;O();const X=Math.ceil(f/x),G=Math.ceil(E/x),M=(z,N,H)=>{const i=e.createTexture({size:[z,N],format:H,usage:GPUTextureUsage.RENDER_ATTACHMENT|GPUTextureUsage.TEXTURE_BINDING});return{texture:i,view:i.createView()}};return s={w:f,h:E,tile:x,velocity:M(f,E,"rgba16float"),tileMax:M(X,G,"rg16float"),neighbor:M(X,G,"rg16float")},s}function O(){if(s){for(const f of["velocity","tileMax","neighbor"])s[f].texture.destroy();s=null}}function v(f,E,x,X,G){const M=f.beginRenderPass({label:E,colorAttachments:[{view:x,loadOp:"clear",clearValue:{r:0,g:0,b:0,a:0},storeOp:"store"}]});M.setPipeline(X),M.setBindGroup(0,G),M.draw(3),M.end()}function _(f,E,x,X,G,M,z,N,H){if(u||d||!H||!H.viewProj)return!1;try{const i=b.motionBlurPlan(f,H,N),a=typeof performance<"u"?performance.now():Date.now();if(!b.invert(m,H.viewProj))return!1;b.eyeAndForward(m,0,1,h,S);const F=t.begin(a,h,H.viewProj),l=t.prev,o=H.motion,y=o&&o.velocityView||null,V=I;if(i.skip||!F)return!1;const W=R(z,N,i.tile);b.stats.motionBlur.frames++,y&&b.stats.motionBlur.moverFrames++,V.set(m,0),V.set(l,16),V.set(H.viewProj,32),V.set([h[0],h[1],h[2],0],48),V.set([S[0],S[1],S[2],0],52),V.set([i.strength,i.maxBlur,1,y?1:0],56),V.set([z,N,1/z,1/N],60),e.queue.writeBuffer(P,0,V);const ee=c("velocity",[{binding:0,visibility:GPUShaderStage.FRAGMENT,buffer:{type:"uniform"}},{binding:1,visibility:GPUShaderStage.FRAGMENT,texture:{sampleType:"depth"}},L(2)]),B=g=>c("tex"+g,Array.from({length:g},(k,Y)=>L(Y))),p=e.createBindGroup({layout:ee,entries:[{binding:0,resource:{buffer:P}},{binding:1,resource:M},{binding:2,resource:y||T}]});v(E,"gosx-motion-velocity",W.velocity.view,w("velocity:"+G,te,ee,"rgba16float"),p);const A=B(1);v(E,"gosx-motion-tilemax",W.tileMax.view,w("tile:"+i.tile,Z(i.tile),A,"rg16float"),e.createBindGroup({layout:A,entries:[{binding:0,resource:W.velocity.view}]})),v(E,"gosx-motion-neighbor",W.neighbor.view,w("neighbor",U,A,"rg16float"),e.createBindGroup({layout:A,entries:[{binding:0,resource:W.tileMax.view}]}));const j=B(3);return v(E,"gosx-motion-gather",X,w("gather:"+i.tile+":"+i.samples+":"+G,q(i.tile,i.samples),j,G),e.createBindGroup({layout:j,entries:[{binding:0,resource:x},{binding:1,resource:W.velocity.view},{binding:2,resource:W.neighbor.view}]})),!0}catch(i){return u=!0,console.error("[gosx] motion blur disabled:",i&&i.message||i),!1}}return{apply:_,dispose(){d=!0,O(),P.destroy(),D.destroy()}}}b.webgpu.motionBlur=J})(),(()=>{const b=window.__gosx_scene3d_depthfx_api;if(!b||!b.motionBlurPlan)return;const $=`#version 300 es
void main() { vec2 p = vec2(float((gl_VertexID << 1) & 2), float(gl_VertexID & 2)); gl_Position = vec4(p * 2.0 - 1.0, 0.0, 1.0); }`,K=`#version 300 es
precision highp float;
precision highp int;
precision highp sampler2D;
out vec4 fragColor;
`,te=K+`
uniform sampler2D u_depth;
uniform mat4 u_invViewProj; uniform mat4 u_prevViewProj;
uniform vec3 u_eye; uniform vec3 u_forward;
uniform vec4 u_settings; uniform vec2 u_size;
void main() {
  ivec2 px = ivec2(gl_FragCoord.xy);
  float depth = texelFetch(u_depth, px, 0).r;
  vec2 uv = (gl_FragCoord.xy) / u_size;
  vec4 world = u_invViewProj * vec4(uv * 2.0 - 1.0, depth * 2.0 - 1.0, 1.0);
  world /= world.w;
  vec2 v = vec2(0.0);
  if (u_settings.z > 0.5) {
    vec4 prev = u_prevViewProj * vec4(world.xyz, 1.0);
    if (prev.w > 0.0) v = prev.xy / prev.w * 0.5 + 0.5 - uv;
  }
  vec2 vp = v * u_size * u_settings.x;
  float len = length(vp);
  if (len > u_settings.y) vp *= u_settings.y / len;
  if (len < 0.5) vp = vec2(0.0);
  fragColor = vec4(vp, -dot(world.xyz - u_eye, u_forward), 1.0); // view-space z: negative in front, as the gather expects
}`,Z=t=>K+`
uniform sampler2D u_velocity;
void main() {
  ivec2 dims = textureSize(u_velocity, 0);
  ivec2 base = ivec2(gl_FragCoord.xy) * ${t};
  vec2 best = vec2(0.0); float bestLen = 0.0;
  for (int y = 0; y < ${t}; y++) for (int x = 0; x < ${t}; x++) {
    ivec2 p = base + ivec2(x, y);
    if (p.x >= dims.x || p.y >= dims.y) continue;
    vec2 v = texelFetch(u_velocity, p, 0).xy;
    float l = dot(v, v);
    if (l > bestLen) { bestLen = l; best = v; }
  }
  fragColor = vec4(best, 0.0, 1.0);
}`,U=K+`
uniform sampler2D u_tiles;
void main() {
  ivec2 dims = textureSize(u_tiles, 0);
  ivec2 c = ivec2(gl_FragCoord.xy);
  vec2 best = vec2(0.0); float bestLen = 0.0;
  for (int y = -1; y <= 1; y++) for (int x = -1; x <= 1; x++) {
    vec2 v = texelFetch(u_tiles, clamp(c + ivec2(x, y), ivec2(0), dims - ivec2(1)), 0).xy;
    float l = dot(v, v);
    if (l > bestLen) { bestLen = l; best = v; }
  }
  fragColor = vec4(best, 0.0, 1.0);
}`,q=(t,I)=>K+`
uniform sampler2D u_color; uniform sampler2D u_velocity; uniform sampler2D u_neighbor;
const int SAMPLES = ${I};
float softDepth(float a, float b, float extent) { return clamp(1.0 - (a - b) / extent, 0.0, 1.0); }
float cone(float dist, float len) { return clamp(1.0 - dist / max(len, 0.0001), 0.0, 1.0); }
float cylinder(float dist, float len) { return 1.0 - smoothstep(0.95 * len, 1.05 * len, dist); }
void main() {
  ivec2 dims = textureSize(u_color, 0);
  ivec2 px = ivec2(gl_FragCoord.xy);
  vec4 c0 = texelFetch(u_color, px, 0);
  ivec2 tileDims = textureSize(u_neighbor, 0);
  vec2 vN = texelFetch(u_neighbor, clamp(px / ${t}, ivec2(0), tileDims - ivec2(1)), 0).xy;
  float lenN = length(vN);
  if (lenN < 0.5) { fragColor = c0; return; }
  vec4 center = texelFetch(u_velocity, px, 0);
  vec2 vX = center.xy; float zX = center.z;
  float lenX = length(vX);
  vec2 pos = gl_FragCoord.xy;
  float jitter = fract(52.9829189 * fract(dot(gl_FragCoord.xy, vec2(0.06711056, 0.00583715)))) - 0.5;
  float extent = max(0.25, abs(zX) * 0.1);
  float w0 = 1.0 / max(lenX, 1.0) * float(SAMPLES) / 40.0;
  vec4 sum = c0 * w0; float weight = w0;
  for (int i = 0; i < SAMPLES; i++) {
    float t = (float(i) + 0.5 + jitter * 0.5) / float(SAMPLES) * 2.0 - 1.0;
    vec2 dir = ((i & 1) == 1 && lenX > 0.5) ? vX : vN;
    ivec2 q = clamp(ivec2(pos + dir * t), ivec2(0), dims - ivec2(1));
    vec4 s = texelFetch(u_velocity, q, 0);
    vec2 vY = s.xy; float zY = s.z;
    float dist = length(vec2(q) + 0.5 - pos);
    float f = softDepth(zX, zY, extent);
    float b = softDepth(zY, zX, extent);
    float lenY = length(vY);
    float alpha = f * cone(dist, lenY) + b * cone(dist, lenX) + cylinder(dist, lenY) * cylinder(dist, lenX) * 2.0;
    sum += texelFetch(u_color, q, 0) * alpha;
    weight += alpha;
  }
  fragColor = vec4(sum.rgb / max(weight, 0.0001), c0.a);
}`;function J(t,I){const P=(D,T)=>{const r=t.createShader(D);if(t.shaderSource(r,T),t.compileShader(r),!t.getShaderParameter(r,t.COMPILE_STATUS))throw new Error("motion blur shader: "+t.getShaderInfoLog(r));return r},m=t.createProgram(),h=P(t.VERTEX_SHADER,$),S=P(t.FRAGMENT_SHADER,I);if(t.attachShader(m,h),t.attachShader(m,S),t.linkProgram(m),t.deleteShader(h),t.deleteShader(S),!t.getProgramParameter(m,t.LINK_STATUS))throw new Error("motion blur program: "+t.getProgramInfoLog(m));return m}function e(t){const I=b.createMotionHistory(),P=new Float32Array(16),m=new Float32Array(3),h=new Float32Array(3),S=new Map;let D=null,T=null,r=!1,n=!1;const s=!!t.getExtension("EXT_color_buffer_float");function u(v,_){let f=S.get(v);return f||(f=J(t,_),S.set(v,f)),f}function d(v,_,f,E){const x=t.createTexture();t.bindTexture(t.TEXTURE_2D,x),t.texImage2D(t.TEXTURE_2D,0,f,v,_,0,E,t.HALF_FLOAT,null),t.texParameteri(t.TEXTURE_2D,t.TEXTURE_MIN_FILTER,t.NEAREST),t.texParameteri(t.TEXTURE_2D,t.TEXTURE_MAG_FILTER,t.NEAREST),t.texParameteri(t.TEXTURE_2D,t.TEXTURE_WRAP_S,t.CLAMP_TO_EDGE),t.texParameteri(t.TEXTURE_2D,t.TEXTURE_WRAP_T,t.CLAMP_TO_EDGE);const X=t.createFramebuffer();if(t.bindFramebuffer(t.FRAMEBUFFER,X),t.framebufferTexture2D(t.FRAMEBUFFER,t.COLOR_ATTACHMENT0,t.TEXTURE_2D,x,0),t.checkFramebufferStatus(t.FRAMEBUFFER)!==t.FRAMEBUFFER_COMPLETE)throw new Error("motion blur target incomplete");return{texture:x,fbo:X,width:v,height:_}}function c(v,_,f){if(T&&T.w===v&&T.h===_&&T.tile===f)return T;L();const E=Math.ceil(v/f),x=Math.ceil(_/f);return T={w:v,h:_,tile:f,velocity:d(v,_,t.RGBA16F,t.RGBA),tileMax:d(E,x,t.RG16F,t.RG),neighbor:d(E,x,t.RG16F,t.RG)},T}function L(){if(T){for(const v of["velocity","tileMax","neighbor"])t.deleteTexture(T[v].texture),t.deleteFramebuffer(T[v].fbo);T=null}}function w(v,_,f,E){t.activeTexture(t.TEXTURE0+v),t.bindTexture(t.TEXTURE_2D,_),t.uniform1i(t.getUniformLocation(f,E),v)}function R(v,_,f,E){t.bindFramebuffer(t.FRAMEBUFFER,v),t.viewport(0,0,_,f),t.useProgram(E),t.drawArrays(t.TRIANGLES,0,3)}function O(v,_,f,E,x,X,G){if(r||n||!s||!X||!G||!G.viewProj)return!1;try{const M=b.motionBlurPlan(v,G,x),z=typeof performance<"u"?performance.now():Date.now();if(!b.invert(P,G.viewProj))return!1;b.eyeAndForward(P,-1,1,m,h);const N=I.begin(z,m,G.viewProj),H=I.prev;if(M.skip||!N)return!1;const i=c(E,x,M.tile);b.stats.motionBlur.frames++,D||(D=t.createVertexArray()),t.bindVertexArray(D),t.disable(t.DEPTH_TEST),t.disable(t.BLEND),t.disable(t.CULL_FACE);const a=u("velocity",te);t.useProgram(a),w(0,X,a,"u_depth"),t.uniformMatrix4fv(t.getUniformLocation(a,"u_invViewProj"),!1,P),t.uniformMatrix4fv(t.getUniformLocation(a,"u_prevViewProj"),!1,H),t.uniform3fv(t.getUniformLocation(a,"u_eye"),m),t.uniform3fv(t.getUniformLocation(a,"u_forward"),h),t.uniform4f(t.getUniformLocation(a,"u_settings"),M.strength,M.maxBlur,1,0),t.uniform2f(t.getUniformLocation(a,"u_size"),E,x),R(i.velocity.fbo,E,x,a);const F=u("tile:"+M.tile,Z(M.tile));t.useProgram(F),w(0,i.velocity.texture,F,"u_velocity"),R(i.tileMax.fbo,i.tileMax.width,i.tileMax.height,F);const l=u("neighbor",U);t.useProgram(l),w(0,i.tileMax.texture,l,"u_tiles"),R(i.neighbor.fbo,i.neighbor.width,i.neighbor.height,l);const o=u("gather:"+M.tile+":"+M.samples,q(M.tile,M.samples));t.useProgram(o),w(0,_,o,"u_color"),w(1,i.velocity.texture,o,"u_velocity"),w(2,i.neighbor.texture,o,"u_neighbor"),R(f?f.fbo:null,E,x,o);for(let y=2;y>=0;y--)t.activeTexture(t.TEXTURE0+y),t.bindTexture(t.TEXTURE_2D,null);return t.activeTexture(t.TEXTURE0),t.bindVertexArray(null),!0}catch(M){return r=!0,console.error("[gosx] motion blur disabled:",M&&M.message||M),!1}}return{apply:O,dispose(){n=!0,L(),S.forEach(v=>t.deleteProgram(v)),D&&t.deleteVertexArray(D)}}}b.webgl.motionBlur=e})();
