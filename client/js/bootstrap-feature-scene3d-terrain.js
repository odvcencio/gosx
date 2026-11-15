const SCENE_TERRAIN_NODE_QUADS=32,SCENE_TERRAIN_PATCH_QUADS=16,SCENE_TERRAIN_MORPH=.3,SCENE_TERRAIN_MAX_PATCHES=4096,SCENE_TERRAIN_MAX_LEVELS=12,SCENE_TERRAIN_VEC4S=36;function sceneTerrainBytes(e){const t=atob(e),r=new Uint8Array(t.length);for(let n=0;n<t.length;n++)r[n]=t.charCodeAt(n);return r}function sceneTerrainSamples(e,t){if(!e||e.length!==t*2)return null;const r=new Uint16Array(t);for(let n=0;n<t;n++)r[n]=e[2*n]|e[2*n+1]<<8;return r}function sceneTerrainNumber(e,t){const r=Number(e);return Number.isFinite(r)?r:t}function sceneTerrainLinear(e){const t=Math.max(0,Math.min(1,e));return t<=.04045?t/12.92:Math.pow((t+.055)/1.055,2.4)}function sceneTerrainColor(e){let t=typeof e=="string"?e.trim().replace(/^#/,""):"";t.length===3&&(t=t[0]+t[0]+t[1]+t[1]+t[2]+t[2]);const r=/^[0-9a-fA-F]{6}$/.test(t)?parseInt(t,16):8421504;return[sceneTerrainLinear((r>>16&255)/255),sceneTerrainLinear((r>>8&255)/255),sceneTerrainLinear((r&255)/255)]}function sceneTerrainBuild(e,t){const r=Math.floor(e.columns),n=Math.floor(e.rows),a=sceneTerrainNumber(e.sizeX,256),i=sceneTerrainNumber(e.sizeZ,a),o=sceneTerrainNumber(e.heightScale,32),c=sceneTerrainNumber(e.baseY,0),u=Math.max(1,Math.min(12,Math.floor(sceneTerrainNumber(e.levels,1)))),f=sceneTerrainNumber(e.spacing,Math.max(a,i)/(32*Math.pow(2,u-1))),l=new Float32Array(r*n);for(let x=0;x<l.length;x++)l[x]=t[x]/65535;const s={ir:e,columns:r,rows:n,sizeX:a,sizeZ:i,scale:o,baseY:c,levels:u,spacing:f,heights:l,minX:sceneTerrainNumber(e.minX,-a/2),minZ:sceneTerrainNumber(e.minZ,-i/2),texelX:a/(r-1),texelZ:i/(n-1),nodeSize:[],nodeCount:[],nodeMin:[],nodeMax:[],nodeDiag:[]};return s.normals=sceneTerrainNormalMips(s),sceneTerrainBounds(s),s}function sceneTerrainTexel(e,t,r){const n=Math.max(0,Math.min(e.columns-1,t)),a=Math.max(0,Math.min(e.rows-1,r));return e.heights[a*e.columns+n]}function sceneTerrainHeight(e,t,r){const n=Math.max(0,Math.min(e.columns-1,(t-e.minX)/e.texelX)),a=Math.max(0,Math.min(e.rows-1,(r-e.minZ)/e.texelZ)),i=Math.min(Math.floor(n),e.columns-2),o=Math.min(Math.floor(a),e.rows-2),c=n-i,u=a-o,f=sceneTerrainTexel(e,i,o),l=sceneTerrainTexel(e,i+1,o),s=sceneTerrainTexel(e,i,o+1),x=sceneTerrainTexel(e,i+1,o+1),h=c>=u?f+c*(l-f)+u*(x-l):f+u*(s-f)+c*(x-s);return e.baseY+h*e.scale}function sceneTerrainNormal(e,t,r,n){const a=Math.max(0,t-1),i=Math.min(e.columns-1,t+1),o=Math.max(0,r-1),c=Math.min(e.rows-1,r+1),u=(sceneTerrainTexel(e,i,r)-sceneTerrainTexel(e,a,r))*e.scale/((i-a)*e.texelX),f=(sceneTerrainTexel(e,t,c)-sceneTerrainTexel(e,t,o))*e.scale/((c-o)*e.texelZ),l=Math.hypot(u,1,f);return n[0]=-u/l,n[1]=1/l,n[2]=-f/l,n}function sceneTerrainNormalMips(e){const t=[0,0,0];let r=e.columns,n=e.rows,a=new Float32Array(r*n*3);for(let o=0;o<n;o++)for(let c=0;c<r;c++)sceneTerrainNormal(e,c,o,t),a.set(t,(o*r+c)*3);const i=[];for(;;){const o=new Uint8Array(r*n*4);for(let l=0;l<r*n;l++){const s=a[3*l],x=a[3*l+1],h=a[3*l+2],d=Math.hypot(s,x,h)||1;o[4*l]=Math.round((s/d*.5+.5)*255),o[4*l+1]=Math.round((x/d*.5+.5)*255),o[4*l+2]=Math.round((h/d*.5+.5)*255),o[4*l+3]=255}if(i.push({width:r,height:n,data:o}),r===1&&n===1)break;const c=Math.max(1,r>>1),u=Math.max(1,n>>1),f=new Float32Array(c*u*3);for(let l=0;l<u;l++)for(let s=0;s<c;s++)for(let x=0;x<2;x++)for(let h=0;h<2;h++){const d=Math.min(r-1,s*2+h),p=Math.min(n-1,l*2+x);for(let m=0;m<3;m++)f[(l*c+s)*3+m]+=a[(p*r+d)*3+m]*.25}a=f,r=c,n=u}return i}function sceneTerrainBounds(e){for(let n=0;n<e.levels;n++){const a=1<<e.levels-1-n;e.nodeSize[n]=32*e.spacing*Math.pow(2,n),e.nodeCount[n]=a,e.nodeMin[n]=new Float32Array(a*a).fill(1/0),e.nodeMax[n]=new Float32Array(a*a).fill(-1/0)}const t=e.nodeCount[0],r=e.nodeSize[0];for(let n=0;n<t;n++){const a=n*r,i=a+r,o=Math.max(0,Math.floor(a/e.texelZ)),c=Math.min(e.rows-1,Math.ceil(i/e.texelZ));for(let u=0;u<t;u++){const f=u*r,l=f+r;if(f>e.sizeX||a>e.sizeZ)continue;const s=Math.max(0,Math.floor(f/e.texelX)),x=Math.min(e.columns-1,Math.ceil(l/e.texelX));let h=1/0,d=-1/0;for(let p=o;p<=c;p++)for(let m=s;m<=x;m++){const v=e.heights[p*e.columns+m];v<h&&(h=v),v>d&&(d=v)}e.nodeMin[0][n*t+u]=e.baseY+h*e.scale,e.nodeMax[0][n*t+u]=e.baseY+d*e.scale}}for(let n=1;n<e.levels;n++){const a=e.nodeCount[n],i=e.nodeCount[n-1];for(let o=0;o<a;o++)for(let c=0;c<a;c++){let u=1/0,f=-1/0;for(let l=0;l<4;l++){const s=(2*o+(l>>1))*i+2*c+(l&1);u=Math.min(u,e.nodeMin[n-1][s]),f=Math.max(f,e.nodeMax[n-1][s])}e.nodeMin[n][o*a+c]=u,e.nodeMax[n][o*a+c]=f}}for(let n=0;n<e.levels;n++){let a=0;const i=e.nodeMin[n],o=e.nodeMax[n];for(let u=0;u<i.length;u++)o[u]>=i[u]&&(a=Math.max(a,o[u]-i[u]));const c=e.nodeSize[n];e.nodeDiag[n]=Math.sqrt(2*c*c+a*a)}}function sceneTerrainRanges(e,t,r,n,a){const i=Math.max(1,Math.min(170,sceneTerrainNumber(r,60)))*Math.PI/180,o=Math.max(1,t)/(2*Math.tan(i/2)),c=Array.isArray(e.ir.lodErrors)?e.ir.lodErrors:[];let u=0;for(let f=0;f<e.levels-1;f++){const l=sceneTerrainNumber(c[f+1],0)*o/Math.max(.05,n),s=f===0?e.nodeDiag[0]:u+e.nodeDiag[f-1]/(1-.3);u=Math.max(l,s),a[f]=u}return a[e.levels-1]=1e30,a}function sceneTerrainPlanes(e,t){for(let r=0;r<4;r++){const n=r>>1,a=r&1?-1:1;for(let i=0;i<4;i++)t[r*4+i]=e[i*4+3]+a*e[i*4+n]}return t}function sceneTerrainBoxVisible(e,t,r,n,a,i,o){if(!e)return!0;for(let c=0;c<4;c++){const u=e[c*4],f=e[c*4+1],l=e[c*4+2],s=e[c*4+3];if(u*(u>=0?a:t)+f*(f>=0?i:r)+l*(l>=0?o:n)+s<0)return!1}return!0}function sceneTerrainSphereBox(e,t,r,n,a,i,o,c){const u=Math.max(r-e.x,0,e.x-i),f=Math.max(n-e.y,0,e.y-o),l=Math.max(a-e.z,0,e.z-c);return u*u+f*f+l*l<=t*t}function sceneTerrainSelect(e,t,r,n,a){const i={model:e,eye:t,planes:r,ranges:n,out:a,count:0,max:Math.floor(a.length/4)};return sceneTerrainSelectNode(i,e.levels-1,0,0),i.count}function sceneTerrainEmit(e,t,r,n,a){const i=e.model,o=i.nodeSize[t]/2,c=2*r+(a&1),u=2*n+(a>>1);if(c*o>=i.sizeX||u*o>=i.sizeZ||e.count>=e.max)return;const f=Math.pow(2,t),l=e.count*4;e.out[l]=c*16*f,e.out[l+1]=u*16*f,e.out[l+2]=f,e.out[l+3]=t,e.count++}function sceneTerrainSelectNode(e,t,r,n){const a=e.model,i=a.nodeSize[t],o=a.nodeCount[t],c=a.minX+r*i,u=a.minZ+n*i;if(r*i>=a.sizeX||n*i>=a.sizeZ)return!0;const f=n*o+r,l=a.nodeMin[t][f],s=a.nodeMax[t][f],x=c+i,h=u+i;if(t<a.levels-1&&!sceneTerrainSphereBox(e.eye,e.ranges[t],c,l,u,x,s,h))return!1;if(!sceneTerrainBoxVisible(e.planes,c,l,u,x,s,h))return!0;if(t===0||!sceneTerrainSphereBox(e.eye,e.ranges[t-1],c,l,u,x,s,h)){for(let d=0;d<4;d++)sceneTerrainEmit(e,t,r,n,d);return!0}for(let d=0;d<4;d++)sceneTerrainSelectNode(e,t-1,2*r+(d&1),2*n+(d>>1))||sceneTerrainEmit(e,t,r,n,d);return!0}function sceneTerrainIndices(){const t=new Uint16Array(1536);let r=0;for(let n=0;n<16;n++)for(let a=0;a<16;a++){const i=n*17+a,o=i+1,c=i+16+1,u=c+1;t[r++]=i,t[r++]=u,t[r++]=o,t[r++]=i,t[r++]=c,t[r++]=u}return t}function sceneTerrainUniforms(e,t,r,n,a){const i=e.ir,o=Array.isArray(i.layers)?i.layers.slice(0,4):[];a.fill(0),a.set([e.minX,e.minZ,e.sizeX,e.sizeZ,e.baseY,e.scale,e.columns,e.rows],0),a.set([e.spacing,16,Math.max(1,o.length),sceneTerrainNumber(i.triplanarSlope,35)],8);const c=i.splat;a.set([c?1:0,c?c.columns:1,c?c.rows:1,n?1:0],12);for(let u=0;u<12;u++){const f=16+u*4;if(u>=e.levels-1){a[f]=1e30;continue}const l=u===0?0:t[u-1],s=t[u],x=l+(1-.3)*(s-l);a[f]=x,a[f+1]=1/Math.max(1e-6,(s-x)*.99)}for(let u=0;u<4;u++){const f=o[u]||o[0]||{},l=64+u*16,s=sceneTerrainColor(f.color),x=sceneTerrainNumber(f.minSlope,0),h=sceneTerrainNumber(f.maxSlope,90);a.set([s[0],s[1],s[2],sceneTerrainNumber(f.roughness,.9)],l),a.set([Math.max(.01,sceneTerrainNumber(f.scale,4)),r[u]&1?1:0,r[u]&2?1:0,0],l+4),a.set([sceneTerrainNumber(f.minHeight,-1e6),sceneTerrainNumber(f.maxHeight,1e6),Math.max(.001,sceneTerrainNumber(f.heightBlend,2)),0],l+8),a.set([x<=0?-1e6:x,h>=90?1e6:h,Math.max(.001,sceneTerrainNumber(f.slopeBlend,6)),0],l+12)}return a}function sceneTerrainSplat(e){const t=e&&e.splat;if(!t||typeof t.data!="string")return null;const r=sceneTerrainBytes(t.data);return r.length===t.columns*t.rows*4?{width:t.columns,height:t.rows,data:r}:null}function sceneTerrainKey(e){return[e.id,e.columns,e.rows,e.src||"",e.heights?e.heights.length:0,e.minX,e.minZ,e.sizeX,e.sizeZ,e.baseY,e.heightScale,e.levels].join("|")}function sceneTerrainModelFor(e,t){const r=sceneTerrainKey(t);if(e.modelKey===r&&e.model)return e.model;if(e.modelKey!==r){e.modelKey=r,e.model=null,e.modelError="";const n=Math.floor(t.columns)*Math.floor(t.rows);if(typeof t.heights=="string"&&t.heights){const a=sceneTerrainSamples(sceneTerrainBytes(t.heights),n);a?e.model=sceneTerrainBuild(t,a):e.modelError="heights"}else typeof t.src=="string"&&t.src&&typeof fetch=="function"?fetch(t.src).then(function(a){return a.arrayBuffer()}).then(function(a){if(e.modelKey!==r)return;const i=sceneTerrainSamples(new Uint8Array(a),n);i?e.model=sceneTerrainBuild(t,i):e.modelError="src"},function(){e.modelKey===r&&(e.modelError="src")}):e.modelError="heights"}return e.model}function sceneTerrainFrame(e,t,r,n,a,i){e.patches||(e.patches=new Float32Array(4096*4),e.ranges=new Float64Array(12),e.planes=new Float64Array(16));const o=typeof performance<"u"?performance.now():0,c=sceneTerrainNumber(t.ir.pixelError,1)*i;sceneTerrainRanges(t,a,r&&r.fov,c,e.ranges);const u={x:sceneTerrainNumber(r&&r.x,0),y:sceneTerrainNumber(r&&r.y,0),z:sceneTerrainNumber(r&&r.z,0)},f=n&&n.length>=16?sceneTerrainPlanes(n,e.planes):null;return e.count=sceneTerrainSelect(t,u,f,e.ranges,e.patches),e.selectMS=typeof performance<"u"?performance.now()-o:0,e.count}function sceneTerrainTint(){return typeof window<"u"&&window.__gosx_scene3d_terrain_lod_tint===!0}const SCENE_WIND_STATE=new Float32Array(8),SCENE_WIND_BATCH=new Float32Array(12);function sceneWindUpdate(e,t){const r=e&&e.wind,n=SCENE_WIND_STATE;return r?(n[0]=sceneTerrainNumber(r.directionX,0),n[1]=sceneTerrainNumber(r.directionZ,1),n[2]=sceneTerrainNumber(r.speed,4),n[3]=sceneTerrainNumber(t,0),n[4]=sceneTerrainNumber(r.gust,.35),n[5]=Math.max(1,sceneTerrainNumber(r.gustScale,24)),n[6]=sceneTerrainNumber(r.gustSpeed,n[2]),n[7]=sceneTerrainNumber(r.frequency,1.3)):(n.fill(0),n[5]=1),typeof window<"u"&&(window.__gosx_scene3d_wind=n),n}function sceneWindFor(e){return SCENE_WIND_BATCH.set(SCENE_WIND_STATE,0),SCENE_WIND_BATCH[8]=e&&SCENE_WIND_STATE[2]>0?sceneTerrainNumber(e.sway,0):0,SCENE_WIND_BATCH[9]=Math.max(.001,sceneTerrainNumber(e&&e.height,1)),SCENE_WIND_BATCH[10]=0,SCENE_WIND_BATCH[11]=0,SCENE_WIND_BATCH}function sceneWindGust(e,t,r,n){const a=t*e[0]+r*e[1],i=t*e[1]-r*e[0],o=(a-e[6]*n)*(2*Math.PI)/e[5]+.35*Math.sin(i*(2*Math.PI)/(e[5]*1.7));return 1+e[4]*Math.sin(o)}function sceneTerrainBands(e,t){if(!Array.isArray(e)||!e.some(function(i){return i&&i.lodGroup}))return e;const r=sceneTerrainNumber(t&&t.x,0),n=sceneTerrainNumber(t&&t.y,0),a=sceneTerrainNumber(t&&t.z,0);return e.filter(function(i){if(!i||!i.lodGroup)return!0;const o=Math.hypot(sceneTerrainNumber(i.x,0)-r,sceneTerrainNumber(i.y,0)-n,sceneTerrainNumber(i.z,0)-a),c=sceneTerrainNumber(i.lodMaxDistance,0);return o+1e-4>=sceneTerrainNumber(i.lodMinDistance,0)&&(!(c>0)||o<c)})}const SCENE_TERRAIN_WGSL_BLOCK=`struct TerrainBlock { p: array<vec4f, 36> };
@group(2) @binding(0) var<uniform> terrain: TerrainBlock;
`;function sceneTerrainVertexWGSL(e){return e+`
`+SCENE_TERRAIN_WGSL_BLOCK+`
@group(2) @binding(1) var terrainHeights: texture_2d<f32>;
@group(0) @binding(0) var<uniform> frame: FrameUniforms;
struct TerrainIn { @builtin(vertex_index) vid: u32, @location(0) tile: vec4f };
struct VertexOutput {
  @builtin(position) clipPos: vec4f,
  @location(0) worldPos: vec3f,
  @location(1) normal: vec3f,
  @location(2) uv: vec2f,
  @location(3) tangent: vec3f,
  @location(4) bitangent: vec3f,
  @location(5) @interpolate(flat) instanceColor: vec4f,
};
fn terrainTexel(c: vec2i) -> f32 {
  return textureLoad(terrainHeights, clamp(c, vec2i(0), vec2i(terrain.p[1].zw) - vec2i(1)), 0).r;
}
fn terrainGrid(xz: vec2f) -> vec2f {
  let last = terrain.p[1].zw - vec2f(1.0);
  return clamp((xz - terrain.p[0].xy) / terrain.p[0].zw * last, vec2f(0.0), last);
}
fn terrainSurface(xz: vec2f) -> f32 {
  let g = terrainGrid(xz);
  let cell = min(floor(g), terrain.p[1].zw - vec2f(2.0));
  let f = g - cell;
  let c = vec2i(cell);
  let h00 = terrainTexel(c);
  let h10 = terrainTexel(c + vec2i(1, 0));
  let h01 = terrainTexel(c + vec2i(0, 1));
  let h11 = terrainTexel(c + vec2i(1, 1));
  var h = h00 + f.y * (h01 - h00) + f.x * (h11 - h01);
  if (f.x >= f.y) { h = h00 + f.x * (h10 - h00) + f.y * (h11 - h10); }
  return terrain.p[1].x + h * terrain.p[1].y;
}
fn terrainTint(level: u32) -> vec4f {
  let k = f32(level % 6u);
  return vec4f(0.55 + 0.45 * cos(6.2831 * (k / 6.0 + vec3f(0.0, 0.33, 0.67))), 1.0);
}
@vertex fn vertexMain(in: TerrainIn) -> VertexOutput {
  let q = u32(terrain.p[2].y);
  let ij = vec2f(f32(in.vid % (q + 1u)), f32(in.vid / (q + 1u)));
  let level = u32(in.tile.w);
  let lo = terrain.p[0].xy;
  let hi = lo + terrain.p[0].zw;
  let s = terrain.p[2].x;
  var index = in.tile.xy + ij * in.tile.z;
  let p0 = min(lo + index * s, hi);
  let d = distance(frame.cameraPos, vec3f(p0.x, terrainSurface(p0), p0.y));
  let m = terrain.p[4u + min(level, 11u)];
  let k = clamp((d - m.x) * m.y, 0.0, 1.0);
  index = index - (ij - 2.0 * floor(ij * 0.5)) * in.tile.z * k;
  let xz = min(lo + index * s, hi);
  let texel = terrain.p[0].zw / (terrain.p[1].zw - vec2f(1.0));
  let dx = terrainSurface(xz + vec2f(texel.x, 0.0)) - terrainSurface(xz - vec2f(texel.x, 0.0));
  let dz = terrainSurface(xz + vec2f(0.0, texel.y)) - terrainSurface(xz - vec2f(0.0, texel.y));
  let N = normalize(vec3f(-dx / (2.0 * texel.x), 1.0, -dz / (2.0 * texel.y)));
  var out: VertexOutput;
  out.worldPos = vec3f(xz.x, terrainSurface(xz), xz.y);
  out.normal = N;
  out.uv = (terrainGrid(xz) + vec2f(0.5)) / terrain.p[1].zw;
  let T = normalize(vec3f(1.0, 0.0, 0.0) - N * N.x);
  out.tangent = T;
  out.bitangent = cross(N, T);
  out.instanceColor = select(vec4f(1.0), terrainTint(level), terrain.p[3].w > 0.5);
  out.clipPos = frame.projMatrix * frame.viewMatrix * vec4f(out.worldPos, 1.0);
  return out;
}
`}function sceneTerrainFragmentWGSL(e){const t=["@fragment fn fragmentMain(in: VertexOutput)","fn gosxPBRColor(in: VertexOutput) -> vec4f {","    let V = normalize(frame.cameraPos - in.worldPos);"];for(const r of t)if(e.indexOf(r)<0)throw new Error("terrain: PBR fragment anchor missing: "+r.trim());return e.replace("@fragment fn fragmentMain(in: VertexOutput)",SCENE_TERRAIN_WGSL_BLOCK+`
struct TerrainSurface { albedo: vec3f, normal: vec3f, roughness: f32 };
fn terrainMask(lo: f32, hi: f32, blend: f32, v: f32) -> f32 {
  let b = blend * 0.5;
  return smoothstep(lo - b, lo + b, v) * (1.0 - smoothstep(hi - b, hi + b, v));
}
fn terrainTap(layer: u32, uv: vec2f, gx: vec2f, gy: vec2f) -> vec3f {
  if (layer == 0u) { return textureSampleGrad(roughnessTex, roughnessSamp, uv, gx, gy).rgb; }
  if (layer == 1u) { return textureSampleGrad(metalnessTex, metalnessSamp, uv, gx, gy).rgb; }
  if (layer == 2u) { return textureSampleGrad(emissiveTex, emissiveSamp, uv, gx, gy).rgb; }
  return textureSampleGrad(specularColorTex, specularColorSamp, uv, gx, gy).rgb;
}
fn terrainLayer(layer: u32, p: vec3f, dx: vec3f, dy: vec3f, w: vec3f, tri: f32) -> vec3f {
  let a = terrain.p[16u + layer * 4u];
  let b = terrain.p[17u + layer * 4u];
  if (b.y < 0.5) { return a.rgb; }
  let s = 1.0 / b.x;
  var c = terrainTap(layer, p.xz * s, dx.xz * s, dy.xz * s);
  if (tri > 0.001) {
    let cx = terrainTap(layer, p.zy * s, dx.zy * s, dy.zy * s);
    let cz = terrainTap(layer, p.xy * s, dx.xy * s, dy.xy * s);
    c = mix(c, cx * w.x + c * w.y + cz * w.z, tri);
  }
  return a.rgb * c;
}
fn terrainSurfaceAt(in: VertexOutput, dx: vec3f, dy: vec3f, ux: vec2f, uy: vec2f) -> TerrainSurface {
  let N = normalize(textureSampleGrad(albedoTex, albedoSamp, in.uv, ux, uy).xyz * 2.0 - 1.0);
  let slope = degrees(acos(clamp(N.y, -1.0, 1.0)));
  let p = in.worldPos;
  let edge = terrain.p[2].w;
  let tri = select(0.0, smoothstep(edge - 5.0, edge + 5.0, slope), edge < 89.5);
  var w = pow(abs(N), vec3f(4.0));
  w = w / max(w.x + w.y + w.z, 1e-5);
  let dim = max(terrain.p[3].yz, vec2f(1.0));
  let suv = (clamp((p.xz - terrain.p[0].xy) / terrain.p[0].zw, vec2f(0.0), vec2f(1.0)) * (dim - 1.0) + 0.5) / dim;
  let splat = select(vec4f(1.0), textureSampleLevel(normalTex, normalSamp, suv, 0.0), terrain.p[3].x > 0.5);
  var albedo = terrainLayer(0u, p, dx, dy, w, tri);
  var rough = terrain.p[16].w;
  let count = u32(terrain.p[2].z);
  for (var i = 1u; i < 4u; i++) {
    if (i >= count) { break; }
    let hb = terrain.p[18u + i * 4u];
    let sb = terrain.p[19u + i * 4u];
    let paint = select(splat.b, select(splat.g, splat.r, i == 1u), i < 3u);
    let m = terrainMask(hb.x, hb.y, hb.z, p.y) * terrainMask(sb.x, sb.y, sb.z, slope) * paint;
    if (m > 0.001) {
      albedo = mix(albedo, terrainLayer(i, p, dx, dy, w, tri), m);
      rough = mix(rough, terrain.p[16u + i * 4u].w, m);
    }
  }
  return TerrainSurface(albedo * in.instanceColor.rgb, N, clamp(rough, 0.04, 1.0));
}
@fragment fn fragmentMain(in: VertexOutput)`).replace("fn gosxPBRColor(in: VertexOutput) -> vec4f {",`fn gosxPBRColor(in: VertexOutput) -> vec4f {
    let terrainDx = dpdx(in.worldPos); let terrainDy = dpdy(in.worldPos);
    let terrainUx = dpdx(in.uv); let terrainUy = dpdy(in.uv);`).replace("    let V = normalize(frame.cameraPos - in.worldPos);",`    let terrainS = terrainSurfaceAt(in, terrainDx, terrainDy, terrainUx, terrainUy);
    albedo = terrainS.albedo; N = terrainS.normal; roughness = terrainS.roughness;
    let V = normalize(frame.cameraPos - in.worldPos);`)}function sceneTerrainGPUTexture(e,t,r,n,a,i,o){const c=e.createTexture({label:o,size:[t,r,1],format:n,mipLevelCount:a.length,usage:GPUTextureUsage.TEXTURE_BINDING|GPUTextureUsage.COPY_DST});for(let u=0;u<a.length;u++){const f=a[u];e.queue.writeTexture({texture:c,mipLevel:u},f.data,{bytesPerRow:f.width*i,rowsPerImage:f.height},[f.width,f.height,1])}return c}function sceneTerrainGPUDisposeModel(e){for(const t of["heightTex","normalTex","splatTex"])e[t]&&e[t].destroy(),e[t]=null;e.gpuModel=null,e.group1=null,e.group2=null}function sceneTerrainGPUDispose(e){if(!(!e||e.kind!=="webgpu")){sceneTerrainGPUDisposeModel(e);for(const t of["index","patchBuffer","uniform","material","slot","white"])e[t]&&e[t].destroy(),e[t]=null;e.pipelines=null,e.kind="",e.model=null,e.modelKey=""}}function sceneTerrainGPUStatic(e,t){if(e.kind==="webgpu"&&e.device===t)return;sceneTerrainGPUDispose(e),e.kind="webgpu",e.device=t,e.dispose=function(){sceneTerrainGPUDispose(e)};const r=sceneTerrainIndices();e.index=t.createBuffer({label:"gosx-terrain-index",size:r.byteLength,usage:GPUBufferUsage.INDEX|GPUBufferUsage.COPY_DST}),t.queue.writeBuffer(e.index,0,r),e.indexCount=r.length,e.patchBuffer=t.createBuffer({label:"gosx-terrain-patches",size:4096*16,usage:GPUBufferUsage.VERTEX|GPUBufferUsage.COPY_DST}),e.uniformData=new Float32Array(144),e.uniform=t.createBuffer({label:"gosx-terrain-uniform",size:e.uniformData.byteLength,usage:GPUBufferUsage.UNIFORM|GPUBufferUsage.COPY_DST}),e.slot=t.createBuffer({label:"gosx-terrain-object",size:256,usage:GPUBufferUsage.UNIFORM|GPUBufferUsage.COPY_DST}),e.white=t.createTexture({label:"gosx-terrain-white",size:[1,1,1],format:"rgba8unorm",usage:GPUTextureUsage.TEXTURE_BINDING|GPUTextureUsage.COPY_DST}),t.queue.writeTexture({texture:e.white},new Uint8Array([255,255,255,255]),{bytesPerRow:4},[1,1,1]),e.whiteView=e.white.createView(),e.clamp=t.createSampler({magFilter:"linear",minFilter:"linear",mipmapFilter:"linear",addressModeU:"clamp-to-edge",addressModeV:"clamp-to-edge"}),e.repeat=t.createSampler({magFilter:"linear",minFilter:"linear",mipmapFilter:"linear",addressModeU:"repeat",addressModeV:"repeat",maxAnisotropy:8}),e.layout=t.createBindGroupLayout({label:"gosx-terrain",entries:[{binding:0,visibility:GPUShaderStage.VERTEX|GPUShaderStage.FRAGMENT,buffer:{type:"uniform"}},{binding:1,visibility:GPUShaderStage.VERTEX,texture:{sampleType:"unfilterable-float"}}]}),e.pipelines=new Map}function sceneTerrainGPUModel(e,t,r){if(e.gpuModel===r)return;sceneTerrainGPUDisposeModel(e),e.heightTex=sceneTerrainGPUTexture(t,r.columns,r.rows,"r32float",[{width:r.columns,height:r.rows,data:r.heights}],4,"gosx-terrain-heights"),e.normalTex=sceneTerrainGPUTexture(t,r.columns,r.rows,"rgba8unorm",r.normals,4,"gosx-terrain-normals");const n=sceneTerrainSplat(r.ir)||{width:1,height:1,data:new Uint8Array([255,255,255,255])};e.splatTex=sceneTerrainGPUTexture(t,n.width,n.height,"rgba8unorm",[n],4,"gosx-terrain-splat"),e.heightView=e.heightTex.createView(),e.normalView=e.normalTex.createView(),e.splatView=e.splatTex.createView(),e.gpuModel=r}function sceneTerrainGPULayers(e,t,r){const n=Array.isArray(r.ir.layers)?r.ir.layers:[],a=[0,0,0,0];e.layerViews=[e.whiteView,e.whiteView,e.whiteView,e.whiteView];for(let i=0;i<4&&i<n.length;i++){const o=n[i]&&n[i].albedo,c=o&&typeof t.load=="function"?t.load(t.device,o,t.textureCache,null,"base-color","srgb"):null;c&&c.loaded&&!c.failed&&c.view&&(e.layerViews[i]=c.view,a[i]=1)}return a}function sceneTerrainGPUGroups(e,t){const r=[e.normalView,e.splatView,e.layerViews[0],e.layerViews[1],e.layerViews[2],e.whiteView,e.whiteView,e.layerViews[3],e.whiteView],n=[e.clamp,e.clamp,e.repeat,e.repeat,e.repeat,e.clamp,e.clamp,e.repeat,e.clamp],a=r.concat([t.materialLayout,e.material]);if(!e.group1||e.group1Key.some(function(i,o){return i!==a[o]})){const i=[{binding:0,resource:{buffer:e.material}}];for(let o=0;o<9;o++)i.push({binding:2*o+1,resource:r[o]},{binding:2*o+2,resource:n[o]});i.push({binding:19,resource:{buffer:e.slot,size:112}}),e.group1=t.device.createBindGroup({label:"gosx-terrain-material",layout:t.materialLayout,entries:i}),e.group1Key=a}(!e.group2||e.group2View!==e.heightView)&&(e.group2=t.device.createBindGroup({label:"gosx-terrain",layout:e.layout,entries:[{binding:0,resource:{buffer:e.uniform}},{binding:1,resource:e.heightView}]}),e.group2View=e.heightView)}function sceneTerrainGPUPipeline(e,t){const r=t.format+"|"+t.samples+"|"+(t.specular?1:0);let n=e.pipelines.get(r);if(n)return n;const a=t.device;(!e.vertexModule||e.vertexSource!==t.frameStructs)&&(e.vertexModule=a.createShaderModule({label:"gosx-terrain-vert",code:sceneTerrainVertexWGSL(t.frameStructs)}),e.vertexSource=t.frameStructs),(!e.fragmentModule||e.fragmentSource!==t.fragment)&&(e.fragmentModule=a.createShaderModule({label:"gosx-terrain-frag",code:sceneTerrainFragmentWGSL(t.fragment)}),e.fragmentSource=t.fragment);const i=t.specular?[{format:t.format},{format:"rgba16float"}]:[{format:t.format}],o={label:"gosx-terrain",layout:a.createPipelineLayout({bindGroupLayouts:[t.frameLayout,t.materialLayout,e.layout]}),vertex:{module:e.vertexModule,entryPoint:"vertexMain",buffers:[{arrayStride:16,stepMode:"instance",attributes:[{shaderLocation:0,offset:0,format:"float32x4"}]}]},fragment:{module:e.fragmentModule,entryPoint:t.specular?"fragmentMainSpecular":"fragmentMain",targets:i},primitive:{topology:"triangle-list",cullMode:"none"},multisample:{count:Math.max(1,t.samples|0)},depthStencil:{format:"depth24plus",depthWriteEnabled:!0,depthCompare:"less-equal"}};return n=typeof t.create=="function"?t.create(a,"render",o):a.createRenderPipeline(o),e.pipelines.set(r,n),n}function sceneTerrainGPUDraw(e,t,r){const n=r.environment&&r.environment.terrain;let a="none";try{const i=sceneWindUpdate(r.environment,r.time);if(r.frameBuffer&&r.device.queue.writeBuffer(r.frameBuffer,160,i),!n)return e.kind&&sceneTerrainGPUDispose(e),a;sceneTerrainGPUStatic(e,r.device);const o=sceneTerrainModelFor(e,n);if(a=e.modelError?"failed":"pending",!o)return a;sceneTerrainGPUModel(e,r.device,o);const c=sceneTerrainGPULayers(e,r,o),u=r.material({color:"#ffffff",roughness:1,metalness:0},n.receiveShadow!==!1),f=u&&u.data;(!e.material||e.materialSize!==f.byteLength)&&(e.material&&e.material.destroy(),e.material=r.device.createBuffer({label:"gosx-terrain-material",size:Math.max(256,f.byteLength),usage:GPUBufferUsage.UNIFORM|GPUBufferUsage.COPY_DST}),e.materialSize=f.byteLength),r.device.queue.writeBuffer(e.material,0,f);const l=sceneTerrainGPUPipeline(e,r),s=r.validation&&r.validation.get(l);if(s&&!s.ready)return"pending";const x=sceneTerrainFrame(e,o,r.camera,r.viewProj,r.height,1);sceneTerrainUniforms(o,e.ranges,c,sceneTerrainTint(),e.uniformData),r.device.queue.writeBuffer(e.uniform,0,e.uniformData),x>0&&r.device.queue.writeBuffer(e.patchBuffer,0,e.patches,0,x*4),sceneTerrainGPUGroups(e,r),x>0&&(t.setPipeline(s&&s.pipeline||l),t.setBindGroup(0,r.frameGroup),t.setBindGroup(1,e.group1,[0]),t.setBindGroup(2,e.group2),t.setVertexBuffer(0,e.patchBuffer),t.setIndexBuffer(e.index,"uint16"),t.drawIndexed(e.indexCount,x)),a="ready"}catch(i){e.error=String(i&&i.message||i),a="failed"}finally{typeof r.attr=="function"&&(r.attr("data-gosx-scene3d-terrain",a),r.attr("data-gosx-scene3d-terrain-patches",String(e.count||0)),a==="failed"&&r.attr("data-gosx-scene3d-terrain-error",e.error||e.modelError||""))}return a}const SCENE_TERRAIN_GL_PIXEL_SCALE=2,SCENE_TERRAIN_GL_LAYER_UNITS=[2,3,4,7],SCENE_TERRAIN_GL_LAYER_SAMPLERS=["u_roughnessMap","u_metalnessMap","u_emissiveMap","u_specularColorMap"],SCENE_TERRAIN_VERTEX_GLSL=`#version 300 es
precision highp float;
precision highp int;
in vec4 a_terrainTile;
uniform mat4 u_viewMatrix;
uniform mat4 u_projectionMatrix;
uniform vec3 u_cameraPosition;
uniform vec4 u_terrain[36];
uniform highp sampler2D u_terrainHeights;
out vec3 v_worldPosition;
out vec3 v_normal;
out vec2 v_uv;
out vec3 v_tangent;
out vec3 v_bitangent;
flat out vec4 v_instanceColor;
float terrainTexel(ivec2 c) {
  return texelFetch(u_terrainHeights, clamp(c, ivec2(0), ivec2(u_terrain[1].zw) - 1), 0).r;
}
vec2 terrainGrid(vec2 xz) {
  vec2 last = u_terrain[1].zw - 1.0;
  return clamp((xz - u_terrain[0].xy) / u_terrain[0].zw * last, vec2(0.0), last);
}
float terrainSurface(vec2 xz) {
  vec2 g = terrainGrid(xz);
  vec2 cell = min(floor(g), u_terrain[1].zw - 2.0);
  vec2 f = g - cell;
  ivec2 c = ivec2(cell);
  float h00 = terrainTexel(c);
  float h10 = terrainTexel(c + ivec2(1, 0));
  float h01 = terrainTexel(c + ivec2(0, 1));
  float h11 = terrainTexel(c + ivec2(1, 1));
  float h = f.x >= f.y ? h00 + f.x * (h10 - h00) + f.y * (h11 - h10) : h00 + f.y * (h01 - h00) + f.x * (h11 - h01);
  return u_terrain[1].x + h * u_terrain[1].y;
}
vec4 terrainTint(int level) {
  float k = float(level % 6);
  return vec4(0.55 + 0.45 * cos(6.2831 * (k / 6.0 + vec3(0.0, 0.33, 0.67))), 1.0);
}
void main() {
  int q = int(u_terrain[2].y);
  vec2 ij = vec2(float(gl_VertexID % (q + 1)), float(gl_VertexID / (q + 1)));
  int level = int(a_terrainTile.w);
  vec2 lo = u_terrain[0].xy;
  vec2 hi = lo + u_terrain[0].zw;
  float s = u_terrain[2].x;
  vec2 index = a_terrainTile.xy + ij * a_terrainTile.z;
  vec2 p0 = min(lo + index * s, hi);
  float d = distance(u_cameraPosition, vec3(p0.x, terrainSurface(p0), p0.y));
  vec4 m = u_terrain[4 + min(level, 11)];
  float k = clamp((d - m.x) * m.y, 0.0, 1.0);
  index -= (ij - 2.0 * floor(ij * 0.5)) * a_terrainTile.z * k;
  vec2 xz = min(lo + index * s, hi);
  vec2 texel = u_terrain[0].zw / (u_terrain[1].zw - 1.0);
  float dx = terrainSurface(xz + vec2(texel.x, 0.0)) - terrainSurface(xz - vec2(texel.x, 0.0));
  float dz = terrainSurface(xz + vec2(0.0, texel.y)) - terrainSurface(xz - vec2(0.0, texel.y));
  vec3 N = normalize(vec3(-dx / (2.0 * texel.x), 1.0, -dz / (2.0 * texel.y)));
  v_worldPosition = vec3(xz.x, terrainSurface(xz), xz.y);
  v_normal = N;
  v_uv = (terrainGrid(xz) + 0.5) / u_terrain[1].zw;
  v_tangent = normalize(vec3(1.0, 0.0, 0.0) - N * N.x);
  v_bitangent = cross(N, v_tangent);
  v_instanceColor = u_terrain[3].w > 0.5 ? terrainTint(level) : vec4(1.0);
  gl_Position = u_projectionMatrix * u_viewMatrix * vec4(v_worldPosition, 1.0);
}`;function sceneTerrainFragmentGLSL(e){const t=["void main() {","    vec3 V = normalize(u_cameraPosition - v_worldPosition);"];for(const r of t)if(e.indexOf(r)<0)throw new Error("terrain: PBR fragment anchor missing: "+r.trim());return e.replace("void main() {",`uniform vec4 u_terrain[36];
float terrainMask(float lo, float hi, float blend, float v) {
  float b = blend * 0.5;
  return smoothstep(lo - b, lo + b, v) * (1.0 - smoothstep(hi - b, hi + b, v));
}
vec3 terrainTap(int layer, vec2 uv, vec2 gx, vec2 gy) {
  if (layer == 0) return textureGrad(u_roughnessMap, uv, gx, gy).rgb;
  if (layer == 1) return textureGrad(u_metalnessMap, uv, gx, gy).rgb;
  if (layer == 2) return textureGrad(u_emissiveMap, uv, gx, gy).rgb;
  return textureGrad(u_specularColorMap, uv, gx, gy).rgb;
}
vec3 terrainLayer(int layer, vec3 p, vec3 dx, vec3 dy, vec3 w, float tri) {
  vec4 a = u_terrain[16 + layer * 4];
  vec4 b = u_terrain[17 + layer * 4];
  if (b.y < 0.5) return a.rgb;
  float s = 1.0 / b.x;
  vec3 c = terrainTap(layer, p.xz * s, dx.xz * s, dy.xz * s);
  if (tri > 0.001) {
    vec3 cx = terrainTap(layer, p.zy * s, dx.zy * s, dy.zy * s);
    vec3 cz = terrainTap(layer, p.xy * s, dx.xy * s, dy.xy * s);
    c = mix(c, cx * w.x + c * w.y + cz * w.z, tri);
  }
  return a.rgb * c;
}
void terrainSurfaceAt(vec3 dx, vec3 dy, vec2 ux, vec2 uy, out vec3 albedoOut, out vec3 normalOut, out float roughOut) {
  vec3 N = normalize(textureGrad(u_albedoMap, v_uv, ux, uy).xyz * 2.0 - 1.0);
  float slope = degrees(acos(clamp(N.y, -1.0, 1.0)));
  vec3 p = v_worldPosition;
  float edge = u_terrain[2].w;
  float tri = edge < 89.5 ? smoothstep(edge - 5.0, edge + 5.0, slope) : 0.0;
  vec3 w = pow(abs(N), vec3(4.0));
  w /= max(w.x + w.y + w.z, 1e-5);
  vec2 dim = max(u_terrain[3].yz, vec2(1.0));
  vec2 suv = (clamp((p.xz - u_terrain[0].xy) / u_terrain[0].zw, vec2(0.0), vec2(1.0)) * (dim - 1.0) + 0.5) / dim;
  vec4 splat = u_terrain[3].x > 0.5 ? textureLod(u_normalMap, suv, 0.0) : vec4(1.0);
  vec3 albedo = terrainLayer(0, p, dx, dy, w, tri);
  float rough = u_terrain[16].w;
  int count = int(u_terrain[2].z);
  for (int i = 1; i < 4; i++) {
    if (i >= count) break;
    vec4 hb = u_terrain[18 + i * 4];
    vec4 sb = u_terrain[19 + i * 4];
    float paint = i == 1 ? splat.r : (i == 2 ? splat.g : splat.b);
    float m = terrainMask(hb.x, hb.y, hb.z, p.y) * terrainMask(sb.x, sb.y, sb.z, slope) * paint;
    if (m > 0.001) {
      albedo = mix(albedo, terrainLayer(i, p, dx, dy, w, tri), m);
      rough = mix(rough, u_terrain[16 + i * 4].w, m);
    }
  }
  albedoOut = albedo * v_instanceColor.rgb;
  normalOut = N;
  roughOut = clamp(rough, 0.04, 1.0);
}
void main() {
    vec3 terrainDx = dFdx(v_worldPosition);
    vec3 terrainDy = dFdy(v_worldPosition);
    vec2 terrainUx = dFdx(v_uv);
    vec2 terrainUy = dFdy(v_uv);`).replace("    vec3 V = normalize(u_cameraPosition - v_worldPosition);",`    terrainSurfaceAt(terrainDx, terrainDy, terrainUx, terrainUy, albedo, N, roughness);
    vec3 V = normalize(u_cameraPosition - v_worldPosition);`)}function sceneTerrainGLShader(e,t,r){const n=e.createShader(t);if(e.shaderSource(n,r),e.compileShader(n),!e.getShaderParameter(n,e.COMPILE_STATUS)){const a=e.getShaderInfoLog(n);throw e.deleteShader(n),new Error("terrain shader: "+a)}return n}function sceneTerrainGLProgram(e,t,r){if(e.program)return e.program;const n=typeof r.fragment=="function"?r.fragment():r.fragment,a=sceneTerrainGLShader(t,t.VERTEX_SHADER,SCENE_TERRAIN_VERTEX_GLSL),i=sceneTerrainGLShader(t,t.FRAGMENT_SHADER,sceneTerrainFragmentGLSL(String(n||""))),o=t.createProgram();if(t.attachShader(o,a),t.attachShader(o,i),t.bindAttribLocation(o,0,"a_terrainTile"),t.linkProgram(o),t.deleteShader(a),t.deleteShader(i),!t.getProgramParameter(o,t.LINK_STATUS)){const u=t.getProgramInfoLog(o);throw t.deleteProgram(o),new Error("terrain program: "+u)}const c=typeof r.uniforms=="function"?r.uniforms(t,o):{};return c.terrain=t.getUniformLocation(o,"u_terrain[0]")||t.getUniformLocation(o,"u_terrain"),c.terrainHeights=t.getUniformLocation(o,"u_terrainHeights"),c.terrainMaps=["u_albedoMap","u_normalMap"].concat(SCENE_TERRAIN_GL_LAYER_SAMPLERS).map(function(u){return t.getUniformLocation(o,u)}),e.program={program:o,uniforms:c},e.program}function sceneTerrainGLTexture(e,t,r,n,a,i){const o=e.createTexture();e.bindTexture(e.TEXTURE_2D,o);for(let c=0;c<t.length;c++){const u=t[c];e.texImage2D(e.TEXTURE_2D,c,r,u.width,u.height,0,n,a,u.data)}return e.texParameteri(e.TEXTURE_2D,e.TEXTURE_MIN_FILTER,i?t.length>1?e.LINEAR_MIPMAP_LINEAR:e.LINEAR:e.NEAREST),e.texParameteri(e.TEXTURE_2D,e.TEXTURE_MAG_FILTER,i?e.LINEAR:e.NEAREST),e.texParameteri(e.TEXTURE_2D,e.TEXTURE_WRAP_S,e.CLAMP_TO_EDGE),e.texParameteri(e.TEXTURE_2D,e.TEXTURE_WRAP_T,e.CLAMP_TO_EDGE),o}function sceneTerrainGLDisposeModel(e,t){for(const r of["heightTex","normalTex","splatTex"])e[r]&&t.deleteTexture(e[r]),e[r]=null;e.glModel=null}function sceneTerrainGLDispose(e){if(!e||e.kind!=="webgl")return;const t=e.gl;sceneTerrainGLDisposeModel(e,t),e.program&&t.deleteProgram(e.program.program),e.vao&&t.deleteVertexArray(e.vao);for(const r of["patchBuffer","indexBuffer"])e[r]&&t.deleteBuffer(e[r]);e.white&&t.deleteTexture(e.white),e.repeat&&t.deleteSampler(e.repeat),Object.assign(e,{kind:"",program:null,vao:null,patchBuffer:null,indexBuffer:null,white:null,repeat:null,model:null,modelKey:""})}function sceneTerrainGLStatic(e,t){if(e.kind==="webgl"&&e.gl===t)return;sceneTerrainGLDispose(e),e.kind="webgl",e.gl=t,e.dispose=function(){sceneTerrainGLDispose(e)};const r=t.getParameter(t.VERTEX_ARRAY_BINDING);e.vao=t.createVertexArray(),t.bindVertexArray(e.vao),e.patchBuffer=t.createBuffer(),t.bindBuffer(t.ARRAY_BUFFER,e.patchBuffer),t.bufferData(t.ARRAY_BUFFER,4096*16,t.DYNAMIC_DRAW),t.enableVertexAttribArray(0),t.vertexAttribPointer(0,4,t.FLOAT,!1,16,0),t.vertexAttribDivisor(0,1);const n=sceneTerrainIndices();e.indexBuffer=t.createBuffer(),t.bindBuffer(t.ELEMENT_ARRAY_BUFFER,e.indexBuffer),t.bufferData(t.ELEMENT_ARRAY_BUFFER,n,t.STATIC_DRAW),e.indexCount=n.length,t.bindVertexArray(r),e.white=sceneTerrainGLTexture(t,[{width:1,height:1,data:new Uint8Array([255,255,255,255])}],t.RGBA8,t.RGBA,t.UNSIGNED_BYTE,!0),e.uniformData=new Float32Array(144),e.repeat=t.createSampler(),t.samplerParameteri(e.repeat,t.TEXTURE_WRAP_S,t.REPEAT),t.samplerParameteri(e.repeat,t.TEXTURE_WRAP_T,t.REPEAT),t.samplerParameteri(e.repeat,t.TEXTURE_MIN_FILTER,t.LINEAR_MIPMAP_LINEAR),t.samplerParameteri(e.repeat,t.TEXTURE_MAG_FILTER,t.LINEAR);const a=t.getParameter(t.MAX_COMBINED_TEXTURE_IMAGE_UNITS),i=t.getParameter(t.MAX_TEXTURE_IMAGE_UNITS);e.heightUnit=a-2>=i?a-2:8}function sceneTerrainGLModel(e,t,r){if(e.glModel===r)return;sceneTerrainGLDisposeModel(e,t);const n=t.getParameter(t.UNPACK_FLIP_Y_WEBGL),a=t.getParameter(t.UNPACK_PREMULTIPLY_ALPHA_WEBGL),i=t.getParameter(t.UNPACK_ALIGNMENT);t.pixelStorei(t.UNPACK_FLIP_Y_WEBGL,!1),t.pixelStorei(t.UNPACK_PREMULTIPLY_ALPHA_WEBGL,!1),t.pixelStorei(t.UNPACK_ALIGNMENT,1),e.heightTex=sceneTerrainGLTexture(t,[{width:r.columns,height:r.rows,data:r.heights}],t.R32F,t.RED,t.FLOAT,!1),e.normalTex=sceneTerrainGLTexture(t,r.normals,t.RGBA8,t.RGBA,t.UNSIGNED_BYTE,!0);const o=sceneTerrainSplat(r.ir)||{width:1,height:1,data:new Uint8Array([255,255,255,255])};e.splatTex=sceneTerrainGLTexture(t,[o],t.RGBA8,t.RGBA,t.UNSIGNED_BYTE,!0),t.pixelStorei(t.UNPACK_FLIP_Y_WEBGL,n),t.pixelStorei(t.UNPACK_PREMULTIPLY_ALPHA_WEBGL,a),t.pixelStorei(t.UNPACK_ALIGNMENT,i),e.glModel=r}function sceneTerrainGLDraw(e,t,r){const n=r.environment&&r.environment.terrain;let a="none";try{if(sceneWindUpdate(r.environment,r.time),!n)return e.kind&&sceneTerrainGLDispose(e),a;sceneTerrainGLStatic(e,t);const i=sceneTerrainModelFor(e,n);if(a=e.modelError?"failed":"pending",!i)return a;sceneTerrainGLModel(e,t,i);const o=sceneTerrainGLProgram(e,t,r),c=Array.isArray(n.layers)?n.layers:[],u=[0,0,0,0],f=[e.white,e.white,e.white,e.white];for(let p=0;p<4&&p<c.length;p++){const m=c[p]&&c[p].albedo&&typeof r.load=="function"?r.load(t,c[p].albedo,r.textureCache,null,"base-color","srgb"):null;m&&m.loaded&&m.texture&&(f[p]=m.texture,u[p]=1)}const l=sceneTerrainFrame(e,i,r.camera,r.viewProj,r.height,SCENE_TERRAIN_GL_PIXEL_SCALE);if(sceneTerrainUniforms(i,e.ranges,u,sceneTerrainTint(),e.uniformData),a="ready",l<=0)return a;t.useProgram(o.program);const s=o.uniforms;typeof r.prepare=="function"&&r.prepare(s,{color:"#ffffff",roughness:1,metalness:0}),s.receiveShadow&&t.uniform1i(s.receiveShadow,n.receiveShadow===!1?0:1),t.uniform4fv(s.terrain,e.uniformData);const x=[0,1].concat(SCENE_TERRAIN_GL_LAYER_UNITS),h=[e.normalTex,e.splatTex].concat(f);for(let p=0;p<x.length;p++)t.activeTexture(t.TEXTURE0+x[p]),t.bindTexture(t.TEXTURE_2D,h[p]),s.terrainMaps[p]&&t.uniform1i(s.terrainMaps[p],x[p]);for(const p of SCENE_TERRAIN_GL_LAYER_UNITS)t.bindSampler(p,e.repeat);t.activeTexture(t.TEXTURE0+e.heightUnit),t.bindTexture(t.TEXTURE_2D,e.heightTex),t.uniform1i(s.terrainHeights,e.heightUnit),t.disable(t.BLEND),t.enable(t.DEPTH_TEST),t.depthMask(!0),t.depthFunc(t.LEQUAL);const d=t.getParameter(t.VERTEX_ARRAY_BINDING);t.bindVertexArray(e.vao),t.bindBuffer(t.ARRAY_BUFFER,e.patchBuffer),t.bufferSubData(t.ARRAY_BUFFER,0,e.patches,0,l*4),t.drawElementsInstanced(t.TRIANGLES,e.indexCount,t.UNSIGNED_SHORT,0,l);for(const p of SCENE_TERRAIN_GL_LAYER_UNITS)t.bindSampler(p,null);t.bindVertexArray(d),t.activeTexture(t.TEXTURE0),r.textureCache&&(r.textureCache._sceneTextureEpoch=(r.textureCache._sceneTextureEpoch||0)+1),r.program&&t.useProgram(r.program)}catch(i){e.error=String(i&&i.message||i),a="failed"}finally{r.mount&&typeof r.mount.setAttribute=="function"&&(r.mount.setAttribute("data-gosx-scene3d-terrain",a),r.mount.setAttribute("data-gosx-scene3d-terrain-patches",String(e.count||0)),a==="failed"&&r.mount.setAttribute("data-gosx-scene3d-terrain-error",e.error||e.modelError||""))}return a}function sceneTerrainHeightAt(e,t,r){return e?sceneTerrainHeight(e,t,r):0}typeof window<"u"&&(window.__gosx_scene3d_terrain_api={webgpu:sceneTerrainGPUDraw,webgl:sceneTerrainGLDraw,build:sceneTerrainBuild,height:sceneTerrainHeightAt,select:sceneTerrainSelect,ranges:sceneTerrainRanges,samples:sceneTerrainSamples,bytes:sceneTerrainBytes,windFor:sceneWindFor,bands:sceneTerrainBands,windUpdate:sceneWindUpdate,windGust:sceneWindGust});
