(function(){"use strict";function F(S){if(S&&typeof S=="object"){for(const O of Object.keys(S))F(S[O]);Object.freeze(S)}return S}window.__gosx_scene3d_elio_ocean=F({elio:"v0.4.3+ocean",set:"ocean",sha256:"62e1caa85b44b5d9d12e8ca3a9ee69c9b89cdafa3485d2eea64780997f79a5b2",kernels:{oceanSpectrum:{wgsl:`struct OceanSpectrumParams {
  n : u32,
  cascade : u32,
  seed : u32,
  _pad : u32,
  windSpeed : f32,
  fetchKm : f32,
  gamma : f32,
  spread : f32,
  swell : f32,
  windDir : f32,
  swellDir : f32,
  tile : f32,
  kMin : f32,
  kMax : f32,
  loopPeriod : f32,
  _pad2 : f32,
};

const G : f32 = 9.81;

@group(0) @binding(0) var<uniform> spec : OceanSpectrumParams;
@group(0) @binding(1) var<storage, read_write> h0 : array<vec4<f32>>;
@group(0) @binding(2) var<storage, read_write> omega : array<f32>;

@compute @workgroup_size(64)
fn ocean_spectrum(@builtin(global_invocation_id) gid : vec3<u32>) {
  let i = gid.x;
  let n = spec.n;
  if ((i >= (n * n))) {
    return;
  }
  let x = (i % n);
  let z = (i / n);
  var ix0 : i32 = i32(x);
  if ((x >= (n / 2u))) {
    ix0 = (i32(x) - i32(n));
  }
  var iz0 : i32 = i32(z);
  if ((z >= (n / 2u))) {
    iz0 = (i32(z) - i32(n));
  }
  let dk = (6.283185307179586 / spec.tile);
  let k0 = (dk * sqrt(((f32(ix0) * f32(ix0)) + (f32(iz0) * f32(iz0)))));
  var tloop = spec.loopPeriod;
  if ((tloop <= 0.0)) {
    tloop = 1000.0;
  }
  let omega0 = (6.283185307179586 / tloop);
  omega[i] = (omega0 * floor((sqrt((G * k0)) / omega0)));
  var inband = true;
  if ((x == (n / 2u))) {
    inband = false;
  }
  if ((z == (n / 2u))) {
    inband = false;
  }
  if ((ix0 == 0)) {
    if ((iz0 == 0)) {
      inband = false;
    }
  }
  if ((k0 < spec.kMin)) {
    inband = false;
  }
  if ((k0 >= spec.kMax)) {
    inband = false;
  }
  var hr = 0.0;
  var hi = 0.0;
  var mr = 0.0;
  var mi = 0.0;
  if (inband) {
    let fetch = (spec.fetchKm * 1000.0);
    let wp = (22.0 * pow(((G * G) / (spec.windSpeed * fetch)), 0.3333333333333333));
    let alpha = (0.076 * pow(((spec.windSpeed * spec.windSpeed) / (fetch * G)), 0.22));
    for (var sgn : u32 = 0u; (sgn < 2u); sgn = (sgn + 1u)) {
      var rix : i32 = ix0;
      var riz : i32 = iz0;
      if ((sgn == 1u)) {
        rix = (0 - ix0);
        riz = (0 - iz0);
      }
      let rseed = spec.seed;
      let rcascade = spec.cascade;
      var rb1 : u32 = 0u;
      var rb2 : u32 = 0u;
      var rh : u32 = 0u;
      for (var rr : u32 = 0u; (rr < 6u); rr = (rr + 1u)) {
        var rw : u32 = rseed;
        if ((rr == 1u)) {
          rw = rcascade;
        }
        if ((rr == 2u)) {
          rw = (u32(rix) & 4294967295u);
        }
        if ((rr == 3u)) {
          rw = (u32(riz) & 4294967295u);
        }
        if ((rr == 4u)) {
          rw = 2654435769u;
        }
        if ((rr == 5u)) {
          rw = 2246822519u;
        }
        let rs = ((((rh ^ rw) * 747796405u) + 2891336453u) & 4294967295u);
        let rv = (((rs >> ((rs >> 28u) + 4u)) ^ rs) * 277803737u);
        rh = ((rv >> 22u) ^ rv);
        if ((rr == 4u)) {
          rb1 = rh;
        }
        if ((rr == 5u)) {
          rb2 = rh;
        }
      }
      let ru1 = ((f32((rb1 / 256u)) + 1.0) / 16777216.0);
      let ru2 = ((f32((rb2 / 256u)) + 1.0) / 16777216.0);
      let rrad = sqrt((-2.0 * log(ru1)));
      let z0 = (rrad * cos((6.283185307179586 * ru2)));
      let z1 = (rrad * sin((6.283185307179586 * ru2)));
      let kx = (dk * f32(rix));
      let kz = (dk * f32(riz));
      let kk = sqrt(((kx * kx) + (kz * kz)));
      let w = sqrt((G * kk));
      var dens = 0.0;
      for (var c : u32 = 0u; (c < 2u); c = (c + 1u)) {
        var weight = (1.0 - spec.swell);
        var wpk = wp;
        var alph = alpha;
        var gam = spec.gamma;
        var sp = spec.spread;
        var dir = spec.windDir;
        if ((c == 1u)) {
          weight = spec.swell;
          wpk = (0.5 * wp);
          alph = (alpha * 0.0625);
          gam = 7.0;
          sp = min((4.0 * spec.spread), 64.0);
          dir = spec.swellDir;
        }
        if ((weight > 0.0)) {
          var sigma = 0.07;
          if ((w > wpk)) {
            sigma = 0.09;
          }
          let rpk = exp(((-(w - wpk) * (w - wpk)) / ((((2.0 * sigma) * sigma) * wpk) * wpk)));
          let sw = (((((alph * G) * G) / pow(w, 5.0)) * exp((-1.25 * pow((wpk / w), 4.0)))) * pow(gam, rpk));
          var s = (sp * pow((w / wpk), 4.06));
          if ((w > wpk)) {
            s = (sp * pow((w / wpk), -2.33));
          }
          var lnn = -1.2655121234846454;
          for (var q : u32 = 0u; (q < 2u); q = (q + 1u)) {
            var xq = (s + 1.0);
            var sign = 1.0;
            if ((q == 1u)) {
              xq = (s + 0.5);
              sign = -1.0;
            }
            let xs = (xq + 6.0);
            let stirling = (((((((xs - 0.5) * log(xs)) - xs) + 0.9189385332046727) + (1.0 / (12.0 * xs))) - (1.0 / (((360.0 * xs) * xs) * xs))) + (1.0 / (((((1260.0 * xs) * xs) * xs) * xs) * xs)));
            let prod = (((((xq * (xq + 1.0)) * (xq + 2.0)) * (xq + 3.0)) * (xq + 4.0)) * (xq + 5.0));
            lnn = (lnn + (sign * (stirling - log(prod))));
          }
          let cosT = (((kx * cos(dir)) + (kz * sin(dir))) / kk);
          let base = max((0.5 * (1.0 + cosT)), 0.00000001);
          dens = (dens + (((((weight * sw) * exp(lnn)) * pow(base, s)) * G) / ((2.0 * w) * kk)));
        }
      }
      let amp = ((0.5 * sqrt(dens)) * dk);
      if ((sgn == 0u)) {
        hr = (z0 * amp);
        hi = (z1 * amp);
      } else {
        mr = (z0 * amp);
        mi = (0.0 - (z1 * amp));
      }
    }
  }
  h0[i] = vec4<f32>(hr, hi, mr, mi);
}
`,entry:"ocean_spectrum",workgroup:[64,1,1],bindings:[{group:0,binding:0,name:"spec",kind:"uniform",elementType:"OceanSpectrumParams"},{group:0,binding:1,name:"h0",kind:"storage-rw",elementType:"vec4<f32>"},{group:0,binding:2,name:"omega",kind:"storage-rw",elementType:"f32"}],uniforms:[{group:0,binding:0,name:"spec",struct:"OceanSpectrumParams",size:64,fields:[{name:"n",type:"u32",offset:0,size:4},{name:"cascade",type:"u32",offset:4,size:4},{name:"seed",type:"u32",offset:8,size:4},{name:"_pad",type:"u32",offset:12,size:4},{name:"windSpeed",type:"f32",offset:16,size:4},{name:"fetchKm",type:"f32",offset:20,size:4},{name:"gamma",type:"f32",offset:24,size:4},{name:"spread",type:"f32",offset:28,size:4},{name:"swell",type:"f32",offset:32,size:4},{name:"windDir",type:"f32",offset:36,size:4},{name:"swellDir",type:"f32",offset:40,size:4},{name:"tile",type:"f32",offset:44,size:4},{name:"kMin",type:"f32",offset:48,size:4},{name:"kMax",type:"f32",offset:52,size:4},{name:"loopPeriod",type:"f32",offset:56,size:4},{name:"_pad2",type:"f32",offset:60,size:4}]}]},oceanEvolve:{wgsl:`struct OceanEvolveParams {
  n : u32,
  _pad0 : u32,
  _pad1 : u32,
  _pad2 : u32,
  t : f32,
  tile : f32,
  loopPeriod : f32,
  _pad4 : f32,
};

@group(0) @binding(0) var<uniform> ev : OceanEvolveParams;
@group(0) @binding(1) var<storage, read> h0 : array<vec4<f32>>;
@group(0) @binding(2) var<storage, read> omega : array<f32>;
@group(0) @binding(3) var<storage, read_write> outA : array<vec4<f32>>;
@group(0) @binding(4) var<storage, read_write> outB : array<vec4<f32>>;

@compute @workgroup_size(64)
fn ocean_evolve(@builtin(global_invocation_id) gid : vec3<u32>) {
  let i = gid.x;
  let n = ev.n;
  if ((i >= (n * n))) {
    return;
  }
  let x = (i % n);
  let z = (i / n);
  var ix : i32 = i32(x);
  if ((x >= (n / 2u))) {
    ix = (i32(x) - i32(n));
  }
  var iz : i32 = i32(z);
  if ((z >= (n / 2u))) {
    iz = (i32(z) - i32(n));
  }
  let dk = (6.283185307179586 / ev.tile);
  let kx = (dk * f32(ix));
  let kz = (dk * f32(iz));
  let kk = sqrt(((kx * kx) + (kz * kz)));
  var inv = 0.0;
  if ((kk > 0.0)) {
    inv = (1.0 / kk);
  }
  let spec = h0[i];
  var tloop = ev.loopPeriod;
  if ((tloop <= 0.0)) {
    tloop = 1000.0;
  }
  let mcycles = floor((((omega[i] * tloop) / 6.283185307179586) + 0.5));
  let t1 = floor(ev.t);
  let t2 = (ev.t - t1);
  let big = (mcycles * t1);
  var rem = (big - (floor((big / tloop)) * tloop));
  if ((rem < 0.0)) {
    rem = (rem + tloop);
  }
  if ((rem >= tloop)) {
    rem = (rem - tloop);
  }
  let cyc = ((rem + (mcycles * t2)) / tloop);
  let phase = (6.283185307179586 * (cyc - floor(cyc)));
  let cs = cos(phase);
  let sn = sin(phase);
  let hr = ((((spec.x * cs) + (spec.y * sn)) + (spec.z * cs)) - (spec.w * sn));
  let hi = ((((spec.y * cs) - (spec.x * sn)) + (spec.w * cs)) + (spec.z * sn));
  let ux = (kx * inv);
  let uz = (kz * inv);
  let dxr = (0.0 - (ux * hi));
  let dxi = (ux * hr);
  let dzr = (0.0 - (uz * hi));
  let dzi = (uz * hr);
  let sxr = (0.0 - (kx * hi));
  let sxi = (kx * hr);
  let szr = (0.0 - (kz * hi));
  let szi = (kz * hr);
  let axx = (0.0 - ((kx * kx) * inv));
  let azz = (0.0 - ((kz * kz) * inv));
  let axz = (0.0 - ((kx * kz) * inv));
  outA[i] = vec4<f32>((hr - dxi), (hi + dxr), (dzr - sxi), (dzi + sxr));
  outB[i] = vec4<f32>((szr - (axx * hi)), (szi + (axx * hr)), ((azz * hr) - (axz * hi)), ((azz * hi) + (axz * hr)));
}
`,entry:"ocean_evolve",workgroup:[64,1,1],bindings:[{group:0,binding:0,name:"ev",kind:"uniform",elementType:"OceanEvolveParams"},{group:0,binding:1,name:"h0",kind:"storage-read",elementType:"vec4<f32>"},{group:0,binding:2,name:"omega",kind:"storage-read",elementType:"f32"},{group:0,binding:3,name:"outA",kind:"storage-rw",elementType:"vec4<f32>"},{group:0,binding:4,name:"outB",kind:"storage-rw",elementType:"vec4<f32>"}],uniforms:[{group:0,binding:0,name:"ev",struct:"OceanEvolveParams",size:32,fields:[{name:"n",type:"u32",offset:0,size:4},{name:"_pad0",type:"u32",offset:4,size:4},{name:"_pad1",type:"u32",offset:8,size:4},{name:"_pad2",type:"u32",offset:12,size:4},{name:"t",type:"f32",offset:16,size:4},{name:"tile",type:"f32",offset:20,size:4},{name:"loopPeriod",type:"f32",offset:24,size:4},{name:"_pad4",type:"f32",offset:28,size:4}]}],glsl:`#version 300 es
precision highp float;
precision highp int;
precision highp sampler2D;

uniform uint elio_width;

layout(std140) uniform OceanEvolveParams_ubo {
  uint n;
  uint _pad0;
  uint _pad1;
  uint _pad2;
  float t;
  float tile;
  float loopPeriod;
  float _pad4;
} ev;
uniform highp sampler2D h0_tex;
uniform uint h0_len;
uniform highp sampler2D omega_tex;
uniform uint omega_len;
layout(location = 0) out vec4 outA_out;
layout(location = 1) out vec4 outB_out;

vec4 h0_at(uint i) {
  return texelFetch(h0_tex, ivec2(int(i % elio_width), int(i / elio_width)), 0);
}

float omega_at(uint i) {
  return texelFetch(omega_tex, ivec2(int(i % elio_width), int(i / elio_width)), 0).x;
}

void main() {
  outA_out = vec4(0.0);
  outB_out = vec4(0.0);
  uint i = (uint(gl_FragCoord.y) * elio_width + uint(gl_FragCoord.x));
  uint n = ev.n;
  if ((i >= (n * n))) {
    return;
  }
  uint x = (i % n);
  uint z = (i / n);
  int ix = int(x);
  if ((x >= (n / 2u))) {
    ix = (int(x) - int(n));
  }
  int iz = int(z);
  if ((z >= (n / 2u))) {
    iz = (int(z) - int(n));
  }
  float dk = (6.283185307179586 / ev.tile);
  float kx = (dk * float(ix));
  float kz = (dk * float(iz));
  float kk = sqrt(((kx * kx) + (kz * kz)));
  float inv = 0.0;
  if ((kk > 0.0)) {
    inv = (1.0 / kk);
  }
  vec4 spec = h0_at(i);
  float tloop = ev.loopPeriod;
  if ((tloop <= 0.0)) {
    tloop = 1000.0;
  }
  float mcycles = floor((((omega_at(i) * tloop) / 6.283185307179586) + 0.5));
  float t1 = floor(ev.t);
  float t2 = (ev.t - t1);
  float big = (mcycles * t1);
  float rem = (big - (floor((big / tloop)) * tloop));
  if ((rem < 0.0)) {
    rem = (rem + tloop);
  }
  if ((rem >= tloop)) {
    rem = (rem - tloop);
  }
  float cyc = ((rem + (mcycles * t2)) / tloop);
  float phase = (6.283185307179586 * (cyc - floor(cyc)));
  float cs = cos(phase);
  float sn = sin(phase);
  float hr = ((((spec.x * cs) + (spec.y * sn)) + (spec.z * cs)) - (spec.w * sn));
  float hi = ((((spec.y * cs) - (spec.x * sn)) + (spec.w * cs)) + (spec.z * sn));
  float ux = (kx * inv);
  float uz = (kz * inv);
  float dxr = (0.0 - (ux * hi));
  float dxi = (ux * hr);
  float dzr = (0.0 - (uz * hi));
  float dzi = (uz * hr);
  float sxr = (0.0 - (kx * hi));
  float sxi = (kx * hr);
  float szr = (0.0 - (kz * hi));
  float szi = (kz * hr);
  float axx = (0.0 - ((kx * kx) * inv));
  float azz = (0.0 - ((kz * kz) * inv));
  float axz = (0.0 - ((kx * kz) * inv));
  outA_out = vec4((hr - dxi), (hi + dxr), (dzr - sxi), (dzi + sxr));
  outB_out = vec4((szr - (axx * hi)), (szi + (axx * hr)), ((azz * hr) - (axz * hi)), ((azz * hi) + (axz * hr)));
}
`,gatherLayout:{kernel:"ocean_evolve",inputs:[{name:"h0",elem:"vec4",sampler:"h0_tex",length:"h0_len",binding:1},{name:"omega",elem:"f32",sampler:"omega_tex",length:"omega_len",binding:2}],outputs:[{name:"outA",elem:"vec4",format:"",location:0,variable:"outA_out"},{name:"outB",elem:"vec4",format:"",location:1,variable:"outB_out"}],uniforms:[{name:"ev",block:"OceanEvolveParams_ubo",struct:"OceanEvolveParams",binding:0}]}},oceanAssemble:{wgsl:`struct OceanAssembleParams {
  n : u32,
  _pad0 : u32,
  _pad1 : u32,
  _pad2 : u32,
  lambda : f32,
  _pad3 : f32,
  _pad4 : f32,
  _pad5 : f32,
};

@group(0) @binding(0) var<uniform> assem : OceanAssembleParams;
@group(0) @binding(1) var<storage, read> fieldA : array<vec4<f32>>;
@group(0) @binding(2) var<storage, read> fieldB : array<vec4<f32>>;
@group(0) @binding(3) var disp : texture_storage_2d<rgba16float, write>;
@group(0) @binding(4) var slope : texture_storage_2d<rgba16float, write>;

@compute @workgroup_size(64)
fn ocean_assemble(@builtin(global_invocation_id) gid : vec3<u32>) {
  let i = gid.x;
  if ((i >= (assem.n * assem.n))) {
    return;
  }
  let a = fieldA[i];
  let b = fieldB[i];
  let l = assem.lambda;
  textureStore(disp, vec2<i32>(vec2<u32>((i % assem.n), (i / assem.n))), vec4<f32>((l * a.y), a.x, (l * a.z), (l * b.w)));
  textureStore(slope, vec2<i32>(vec2<u32>((i % assem.n), (i / assem.n))), vec4<f32>(a.w, b.x, (l * b.y), (l * b.z)));
}
`,entry:"ocean_assemble",workgroup:[64,1,1],bindings:[{group:0,binding:0,name:"assem",kind:"uniform",elementType:"OceanAssembleParams"},{group:0,binding:1,name:"fieldA",kind:"storage-read",elementType:"vec4<f32>"},{group:0,binding:2,name:"fieldB",kind:"storage-read",elementType:"vec4<f32>"},{group:0,binding:3,name:"disp",kind:"storage-texture",format:"rgba16float",elementType:"vec4<f32>"},{group:0,binding:4,name:"slope",kind:"storage-texture",format:"rgba16float",elementType:"vec4<f32>"}],uniforms:[{group:0,binding:0,name:"assem",struct:"OceanAssembleParams",size:32,fields:[{name:"n",type:"u32",offset:0,size:4},{name:"_pad0",type:"u32",offset:4,size:4},{name:"_pad1",type:"u32",offset:8,size:4},{name:"_pad2",type:"u32",offset:12,size:4},{name:"lambda",type:"f32",offset:16,size:4},{name:"_pad3",type:"f32",offset:20,size:4},{name:"_pad4",type:"f32",offset:24,size:4},{name:"_pad5",type:"f32",offset:28,size:4}]}],glsl:`#version 300 es
precision highp float;
precision highp int;
precision highp sampler2D;

uniform uint elio_width;

layout(std140) uniform OceanAssembleParams_ubo {
  uint n;
  uint _pad0;
  uint _pad1;
  uint _pad2;
  float lambda;
  float _pad3;
  float _pad4;
  float _pad5;
} assem;
uniform highp sampler2D fieldA_tex;
uniform uint fieldA_len;
uniform highp sampler2D fieldB_tex;
uniform uint fieldB_len;
layout(location = 0) out vec4 disp_out;
layout(location = 1) out vec4 slope_out;

vec4 fieldA_at(uint i) {
  return texelFetch(fieldA_tex, ivec2(int(i % elio_width), int(i / elio_width)), 0);
}

vec4 fieldB_at(uint i) {
  return texelFetch(fieldB_tex, ivec2(int(i % elio_width), int(i / elio_width)), 0);
}

void main() {
  disp_out = vec4(0.0);
  slope_out = vec4(0.0);
  uint i = (uint(gl_FragCoord.y) * elio_width + uint(gl_FragCoord.x));
  if ((i >= (assem.n * assem.n))) {
    return;
  }
  vec4 a = fieldA_at(i);
  vec4 b = fieldB_at(i);
  float l = assem.lambda;
  disp_out = vec4((l * a.y), a.x, (l * a.z), (l * b.w));
  slope_out = vec4(a.w, b.x, (l * b.y), (l * b.z));
}
`,gatherLayout:{kernel:"ocean_assemble",inputs:[{name:"fieldA",elem:"vec4",sampler:"fieldA_tex",length:"fieldA_len",binding:1},{name:"fieldB",elem:"vec4",sampler:"fieldB_tex",length:"fieldB_len",binding:2}],outputs:[{name:"disp",elem:"vec4",format:"rgba16float",location:0,variable:"disp_out"},{name:"slope",elem:"vec4",format:"rgba16float",location:1,variable:"slope_out"}],uniforms:[{name:"assem",block:"OceanAssembleParams_ubo",struct:"OceanAssembleParams",binding:0}]}},oceanMip:{wgsl:`struct OceanMipParams {
  srcWidth : u32,
  srcHeight : u32,
  dstWidth : u32,
  dstHeight : u32,
};

@group(0) @binding(0) var<uniform> mip : OceanMipParams;
@group(0) @binding(1) var<storage, read> src : array<vec4<f32>>;
@group(0) @binding(2) var dst : texture_storage_2d<rgba16float, write>;

@compute @workgroup_size(64)
fn ocean_mip(@builtin(global_invocation_id) gid : vec3<u32>) {
  let i = gid.x;
  if ((i >= (mip.dstWidth * mip.dstHeight))) {
    return;
  }
  let dx = (i % mip.dstWidth);
  let dy = (i / mip.dstWidth);
  let x0 = (dx * 2u);
  let y0 = (dy * 2u);
  let x1 = min((x0 + 1u), (mip.srcWidth - 1u));
  let y1 = min((y0 + 1u), (mip.srcHeight - 1u));
  let sum = (((src[((y0 * mip.srcWidth) + x0)] + src[((y0 * mip.srcWidth) + x1)]) + src[((y1 * mip.srcWidth) + x0)]) + src[((y1 * mip.srcWidth) + x1)]);
  textureStore(dst, vec2<i32>(vec2<u32>((i % mip.dstWidth), (i / mip.dstWidth))), (sum * 0.25));
}
`,entry:"ocean_mip",workgroup:[64,1,1],bindings:[{group:0,binding:0,name:"mip",kind:"uniform",elementType:"OceanMipParams"},{group:0,binding:1,name:"src",kind:"storage-read",elementType:"vec4<f32>"},{group:0,binding:2,name:"dst",kind:"storage-texture",format:"rgba16float",elementType:"vec4<f32>"}],uniforms:[{group:0,binding:0,name:"mip",struct:"OceanMipParams",size:16,fields:[{name:"srcWidth",type:"u32",offset:0,size:4},{name:"srcHeight",type:"u32",offset:4,size:4},{name:"dstWidth",type:"u32",offset:8,size:4},{name:"dstHeight",type:"u32",offset:12,size:4}]}],glsl:`#version 300 es
precision highp float;
precision highp int;
precision highp sampler2D;

uniform uint elio_width;

layout(std140) uniform OceanMipParams_ubo {
  uint srcWidth;
  uint srcHeight;
  uint dstWidth;
  uint dstHeight;
} mip;
uniform highp sampler2D src_tex;
uniform uint src_len;
layout(location = 0) out vec4 dst_out;

vec4 src_at(uint i) {
  return texelFetch(src_tex, ivec2(int(i % elio_width), int(i / elio_width)), 0);
}

void main() {
  dst_out = vec4(0.0);
  uint i = (uint(gl_FragCoord.y) * elio_width + uint(gl_FragCoord.x));
  if ((i >= (mip.dstWidth * mip.dstHeight))) {
    return;
  }
  uint dx = (i % mip.dstWidth);
  uint dy = (i / mip.dstWidth);
  uint x0 = (dx * 2u);
  uint y0 = (dy * 2u);
  uint x1 = min((x0 + 1u), (mip.srcWidth - 1u));
  uint y1 = min((y0 + 1u), (mip.srcHeight - 1u));
  vec4 sum = (((src_at(((y0 * mip.srcWidth) + x0)) + src_at(((y0 * mip.srcWidth) + x1))) + src_at(((y1 * mip.srcWidth) + x0))) + src_at(((y1 * mip.srcWidth) + x1)));
  dst_out = (sum * 0.25);
}
`,gatherLayout:{kernel:"ocean_mip",inputs:[{name:"src",elem:"vec4",sampler:"src_tex",length:"src_len",binding:1}],outputs:[{name:"dst",elem:"vec4",format:"rgba16float",location:0,variable:"dst_out"}],uniforms:[{name:"mip",block:"OceanMipParams_ubo",struct:"OceanMipParams",binding:0}]}},oceanFoam:{wgsl:`struct OceanFoamParams {
  width : u32,
  cascades : u32,
  _pad0 : u32,
  _pad1 : u32,
  originX : i32,
  originZ : i32,
  shiftX : i32,
  shiftZ : i32,
  dt : f32,
  tau : f32,
  threshold : f32,
  gain : f32,
  lambda : f32,
  cell : f32,
  _pad2 : f32,
  _pad3 : f32,
  n0 : u32,
  n1 : u32,
  n2 : u32,
  n3 : u32,
  size0 : f32,
  size1 : f32,
  size2 : f32,
  size3 : f32,
};

@group(0) @binding(0) var<uniform> foam : OceanFoamParams;
@group(0) @binding(1) var<storage, read> foamIn : array<f32>;
@group(0) @binding(2) var<storage, read> fieldB0 : array<vec4<f32>>;
@group(0) @binding(3) var<storage, read> fieldB1 : array<vec4<f32>>;
@group(0) @binding(4) var<storage, read> fieldB2 : array<vec4<f32>>;
@group(0) @binding(5) var<storage, read> fieldB3 : array<vec4<f32>>;
@group(0) @binding(6) var<storage, read_write> foamOut : array<f32>;
@group(0) @binding(7) var foamTex : texture_storage_2d<rgba16float, write>;

@compute @workgroup_size(64)
fn ocean_foam(@builtin(global_invocation_id) gid : vec3<u32>) {
  let i = gid.x;
  let w = foam.width;
  if ((i >= (w * w))) {
    return;
  }
  let wi = i32(w);
  let ru = ((((i32((i % w)) - foam.originX) % wi) + wi) % wi);
  let rv = ((((i32((i / w)) - foam.originZ) % wi) + wi) % wi);
  let wx = (foam.originX + ru);
  let wz = (foam.originZ + rv);
  var entered = false;
  if ((foam.shiftX > 0)) {
    if ((ru >= (wi - foam.shiftX))) {
      entered = true;
    }
  }
  if ((foam.shiftX < 0)) {
    if ((ru < (0 - foam.shiftX))) {
      entered = true;
    }
  }
  if ((foam.shiftZ > 0)) {
    if ((rv >= (wi - foam.shiftZ))) {
      entered = true;
    }
  }
  if ((foam.shiftZ < 0)) {
    if ((rv < (0 - foam.shiftZ))) {
      entered = true;
    }
  }
  let px = ((f32(wx) + 0.5) * foam.cell);
  let pz = ((f32(wz) + 0.5) * foam.cell);
  var sxx = 0.0;
  var szz = 0.0;
  var sxz = 0.0;
  for (var c : u32 = 0u; (c < foam.cascades); c = (c + 1u)) {
    var nc = foam.n0;
    var lc = foam.size0;
    if ((c == 1u)) {
      nc = foam.n1;
      lc = foam.size1;
    }
    if ((c == 2u)) {
      nc = foam.n2;
      lc = foam.size2;
    }
    if ((c == 3u)) {
      nc = foam.n3;
      lc = foam.size3;
    }
    let u = (px / lc);
    let v = (pz / lc);
    let fxs = ((u - floor(u)) * f32(nc));
    let fzs = ((v - floor(v)) * f32(nc));
    let fxf = floor(fxs);
    let fzf = floor(fzs);
    let x0 = min(u32(fxf), (nc - 1u));
    let z0 = min(u32(fzf), (nc - 1u));
    let rx = (fxs - fxf);
    let rz = (fzs - fzf);
    let x1 = ((x0 + 1u) % nc);
    let z1 = ((z0 + 1u) % nc);
    var v00 = vec4<f32>(0.0, 0.0, 0.0, 0.0);
    var v10 = vec4<f32>(0.0, 0.0, 0.0, 0.0);
    var v01 = vec4<f32>(0.0, 0.0, 0.0, 0.0);
    var v11 = vec4<f32>(0.0, 0.0, 0.0, 0.0);
    if ((c == 0u)) {
      v00 = fieldB0[((z0 * nc) + x0)];
      v10 = fieldB0[((z0 * nc) + x1)];
      v01 = fieldB0[((z1 * nc) + x0)];
      v11 = fieldB0[((z1 * nc) + x1)];
    }
    if ((c == 1u)) {
      v00 = fieldB1[((z0 * nc) + x0)];
      v10 = fieldB1[((z0 * nc) + x1)];
      v01 = fieldB1[((z1 * nc) + x0)];
      v11 = fieldB1[((z1 * nc) + x1)];
    }
    if ((c == 2u)) {
      v00 = fieldB2[((z0 * nc) + x0)];
      v10 = fieldB2[((z0 * nc) + x1)];
      v01 = fieldB2[((z1 * nc) + x0)];
      v11 = fieldB2[((z1 * nc) + x1)];
    }
    if ((c == 3u)) {
      v00 = fieldB3[((z0 * nc) + x0)];
      v10 = fieldB3[((z0 * nc) + x1)];
      v01 = fieldB3[((z1 * nc) + x0)];
      v11 = fieldB3[((z1 * nc) + x1)];
    }
    let bil = (((1.0 - rz) * (((1.0 - rx) * v00) + (rx * v10))) + (rz * (((1.0 - rx) * v01) + (rx * v11))));
    sxx = (sxx + bil.y);
    szz = (szz + bil.z);
    sxz = (sxz + bil.w);
  }
  let jac = (((1.0 + (foam.lambda * sxx)) * (1.0 + (foam.lambda * szz))) - (((foam.lambda * sxz) * foam.lambda) * sxz));
  var f = (foamIn[i] * exp((0.0 - (foam.dt / foam.tau))));
  if (entered) {
    f = 0.0;
  }
  f = min((f + clamp(((foam.threshold - jac) * foam.gain), 0.0, 1.0)), 1.0);
  foamOut[i] = f;
  textureStore(foamTex, vec2<i32>(vec2<u32>((i % w), (i / w))), vec4<f32>(f, jac, 0.0, 1.0));
}
`,entry:"ocean_foam",workgroup:[64,1,1],bindings:[{group:0,binding:0,name:"foam",kind:"uniform",elementType:"OceanFoamParams"},{group:0,binding:1,name:"foamIn",kind:"storage-read",elementType:"f32"},{group:0,binding:2,name:"fieldB0",kind:"storage-read",elementType:"vec4<f32>"},{group:0,binding:3,name:"fieldB1",kind:"storage-read",elementType:"vec4<f32>"},{group:0,binding:4,name:"fieldB2",kind:"storage-read",elementType:"vec4<f32>"},{group:0,binding:5,name:"fieldB3",kind:"storage-read",elementType:"vec4<f32>"},{group:0,binding:6,name:"foamOut",kind:"storage-rw",elementType:"f32"},{group:0,binding:7,name:"foamTex",kind:"storage-texture",format:"rgba16float",elementType:"vec4<f32>"}],uniforms:[{group:0,binding:0,name:"foam",struct:"OceanFoamParams",size:96,fields:[{name:"width",type:"u32",offset:0,size:4},{name:"cascades",type:"u32",offset:4,size:4},{name:"_pad0",type:"u32",offset:8,size:4},{name:"_pad1",type:"u32",offset:12,size:4},{name:"originX",type:"i32",offset:16,size:4},{name:"originZ",type:"i32",offset:20,size:4},{name:"shiftX",type:"i32",offset:24,size:4},{name:"shiftZ",type:"i32",offset:28,size:4},{name:"dt",type:"f32",offset:32,size:4},{name:"tau",type:"f32",offset:36,size:4},{name:"threshold",type:"f32",offset:40,size:4},{name:"gain",type:"f32",offset:44,size:4},{name:"lambda",type:"f32",offset:48,size:4},{name:"cell",type:"f32",offset:52,size:4},{name:"_pad2",type:"f32",offset:56,size:4},{name:"_pad3",type:"f32",offset:60,size:4},{name:"n0",type:"u32",offset:64,size:4},{name:"n1",type:"u32",offset:68,size:4},{name:"n2",type:"u32",offset:72,size:4},{name:"n3",type:"u32",offset:76,size:4},{name:"size0",type:"f32",offset:80,size:4},{name:"size1",type:"f32",offset:84,size:4},{name:"size2",type:"f32",offset:88,size:4},{name:"size3",type:"f32",offset:92,size:4}]}],glsl:`#version 300 es
precision highp float;
precision highp int;
precision highp sampler2D;

uniform uint elio_width;

layout(std140) uniform OceanFoamParams_ubo {
  uint width;
  uint cascades;
  uint _pad0;
  uint _pad1;
  int originX;
  int originZ;
  int shiftX;
  int shiftZ;
  float dt;
  float tau;
  float threshold;
  float gain;
  float lambda;
  float cell;
  float _pad2;
  float _pad3;
  uint n0;
  uint n1;
  uint n2;
  uint n3;
  float size0;
  float size1;
  float size2;
  float size3;
} foam;
uniform highp sampler2D foamIn_tex;
uniform uint foamIn_len;
uniform highp sampler2D fieldB0_tex;
uniform uint fieldB0_len;
uniform highp sampler2D fieldB1_tex;
uniform uint fieldB1_len;
uniform highp sampler2D fieldB2_tex;
uniform uint fieldB2_len;
uniform highp sampler2D fieldB3_tex;
uniform uint fieldB3_len;
layout(location = 0) out vec4 foamOut_out;
layout(location = 1) out vec4 foamTex_out;

float foamIn_at(uint i) {
  return texelFetch(foamIn_tex, ivec2(int(i % elio_width), int(i / elio_width)), 0).x;
}

vec4 fieldB0_at(uint i) {
  return texelFetch(fieldB0_tex, ivec2(int(i % elio_width), int(i / elio_width)), 0);
}

vec4 fieldB1_at(uint i) {
  return texelFetch(fieldB1_tex, ivec2(int(i % elio_width), int(i / elio_width)), 0);
}

vec4 fieldB2_at(uint i) {
  return texelFetch(fieldB2_tex, ivec2(int(i % elio_width), int(i / elio_width)), 0);
}

vec4 fieldB3_at(uint i) {
  return texelFetch(fieldB3_tex, ivec2(int(i % elio_width), int(i / elio_width)), 0);
}

void main() {
  foamOut_out = vec4(0.0);
  foamTex_out = vec4(0.0);
  uint i = (uint(gl_FragCoord.y) * elio_width + uint(gl_FragCoord.x));
  uint w = foam.width;
  if ((i >= (w * w))) {
    return;
  }
  int wi = int(w);
  int ru = ((((int((i % w)) - foam.originX) % wi) + wi) % wi);
  int rv = ((((int((i / w)) - foam.originZ) % wi) + wi) % wi);
  int wx = (foam.originX + ru);
  int wz = (foam.originZ + rv);
  bool entered = false;
  if ((foam.shiftX > 0)) {
    if ((ru >= (wi - foam.shiftX))) {
      entered = true;
    }
  }
  if ((foam.shiftX < 0)) {
    if ((ru < (0 - foam.shiftX))) {
      entered = true;
    }
  }
  if ((foam.shiftZ > 0)) {
    if ((rv >= (wi - foam.shiftZ))) {
      entered = true;
    }
  }
  if ((foam.shiftZ < 0)) {
    if ((rv < (0 - foam.shiftZ))) {
      entered = true;
    }
  }
  float px = ((float(wx) + 0.5) * foam.cell);
  float pz = ((float(wz) + 0.5) * foam.cell);
  float sxx = 0.0;
  float szz = 0.0;
  float sxz = 0.0;
  for (uint c = 0u; (c < foam.cascades); c = (c + 1u)) {
    uint nc = foam.n0;
    float lc = foam.size0;
    if ((c == 1u)) {
      nc = foam.n1;
      lc = foam.size1;
    }
    if ((c == 2u)) {
      nc = foam.n2;
      lc = foam.size2;
    }
    if ((c == 3u)) {
      nc = foam.n3;
      lc = foam.size3;
    }
    float u = (px / lc);
    float v = (pz / lc);
    float fxs = ((u - floor(u)) * float(nc));
    float fzs = ((v - floor(v)) * float(nc));
    float fxf = floor(fxs);
    float fzf = floor(fzs);
    uint x0 = min(uint(fxf), (nc - 1u));
    uint z0 = min(uint(fzf), (nc - 1u));
    float rx = (fxs - fxf);
    float rz = (fzs - fzf);
    uint x1 = ((x0 + 1u) % nc);
    uint z1 = ((z0 + 1u) % nc);
    vec4 v00 = vec4(0.0, 0.0, 0.0, 0.0);
    vec4 v10 = vec4(0.0, 0.0, 0.0, 0.0);
    vec4 v01 = vec4(0.0, 0.0, 0.0, 0.0);
    vec4 v11 = vec4(0.0, 0.0, 0.0, 0.0);
    if ((c == 0u)) {
      v00 = fieldB0_at(((z0 * nc) + x0));
      v10 = fieldB0_at(((z0 * nc) + x1));
      v01 = fieldB0_at(((z1 * nc) + x0));
      v11 = fieldB0_at(((z1 * nc) + x1));
    }
    if ((c == 1u)) {
      v00 = fieldB1_at(((z0 * nc) + x0));
      v10 = fieldB1_at(((z0 * nc) + x1));
      v01 = fieldB1_at(((z1 * nc) + x0));
      v11 = fieldB1_at(((z1 * nc) + x1));
    }
    if ((c == 2u)) {
      v00 = fieldB2_at(((z0 * nc) + x0));
      v10 = fieldB2_at(((z0 * nc) + x1));
      v01 = fieldB2_at(((z1 * nc) + x0));
      v11 = fieldB2_at(((z1 * nc) + x1));
    }
    if ((c == 3u)) {
      v00 = fieldB3_at(((z0 * nc) + x0));
      v10 = fieldB3_at(((z0 * nc) + x1));
      v01 = fieldB3_at(((z1 * nc) + x0));
      v11 = fieldB3_at(((z1 * nc) + x1));
    }
    vec4 bil = (((1.0 - rz) * (((1.0 - rx) * v00) + (rx * v10))) + (rz * (((1.0 - rx) * v01) + (rx * v11))));
    sxx = (sxx + bil.y);
    szz = (szz + bil.z);
    sxz = (sxz + bil.w);
  }
  float jac = (((1.0 + (foam.lambda * sxx)) * (1.0 + (foam.lambda * szz))) - (((foam.lambda * sxz) * foam.lambda) * sxz));
  float f = (foamIn_at(i) * exp((0.0 - (foam.dt / foam.tau))));
  if (entered) {
    f = 0.0;
  }
  f = min((f + clamp(((foam.threshold - jac) * foam.gain), 0.0, 1.0)), 1.0);
  foamOut_out = vec4(f, 0.0, 0.0, 0.0);
  foamTex_out = vec4(f, jac, 0.0, 1.0);
}
`,gatherLayout:{kernel:"ocean_foam",inputs:[{name:"foamIn",elem:"f32",sampler:"foamIn_tex",length:"foamIn_len",binding:1},{name:"fieldB0",elem:"vec4",sampler:"fieldB0_tex",length:"fieldB0_len",binding:2},{name:"fieldB1",elem:"vec4",sampler:"fieldB1_tex",length:"fieldB1_len",binding:3},{name:"fieldB2",elem:"vec4",sampler:"fieldB2_tex",length:"fieldB2_len",binding:4},{name:"fieldB3",elem:"vec4",sampler:"fieldB3_tex",length:"fieldB3_len",binding:5}],outputs:[{name:"foamOut",elem:"f32",format:"",location:0,variable:"foamOut_out"},{name:"foamTex",elem:"vec4",format:"rgba16float",location:1,variable:"foamTex_out"}],uniforms:[{name:"foam",block:"OceanFoamParams_ubo",struct:"OceanFoamParams",binding:0}]}},oceanProbe:{wgsl:`struct OceanProbeParams {
  count : u32,
  cascades : u32,
  _pad0 : u32,
  _pad1 : u32,
  n0 : u32,
  n1 : u32,
  n2 : u32,
  n3 : u32,
  size0 : f32,
  size1 : f32,
  size2 : f32,
  size3 : f32,
  t : f32,
  normalStep : f32,
  _pad2 : f32,
  _pad3 : f32,
};

@group(0) @binding(0) var<uniform> probe : OceanProbeParams;
@group(0) @binding(1) var<storage, read> points : array<vec4<f32>>;
@group(0) @binding(2) var<storage, read> disp0 : array<vec4<f32>>;
@group(0) @binding(3) var<storage, read> disp1 : array<vec4<f32>>;
@group(0) @binding(4) var<storage, read> disp2 : array<vec4<f32>>;
@group(0) @binding(5) var<storage, read> disp3 : array<vec4<f32>>;
@group(0) @binding(6) var<storage, read_write> out : array<vec4<f32>>;

@compute @workgroup_size(64)
fn ocean_probe(@builtin(global_invocation_id) gid : vec3<u32>) {
  let i = gid.x;
  if ((i >= probe.count)) {
    return;
  }
  let pt = points[i];
  let e = probe.normalStep;
  var h0 = 0.0;
  var h1 = 0.0;
  var h2 = 0.0;
  var h3 = 0.0;
  var h4 = 0.0;
  for (var tg : u32 = 0u; (tg < 5u); tg = (tg + 1u)) {
    var qx = pt.x;
    var qz = pt.y;
    if ((tg == 1u)) {
      qx = (qx + e);
    }
    if ((tg == 2u)) {
      qx = (qx - e);
    }
    if ((tg == 3u)) {
      qz = (qz + e);
    }
    if ((tg == 4u)) {
      qz = (qz - e);
    }
    var px = qx;
    var pz = qz;
    for (var it : u32 = 0u; (it < 7u); it = (it + 1u)) {
      var sdx = 0.0;
      var sdy = 0.0;
      var sdz = 0.0;
      var gxx = 0.0;
      var gzx = 0.0;
      var gxz = 0.0;
      var gzz = 0.0;
      for (var c : u32 = 0u; (c < probe.cascades); c = (c + 1u)) {
        var nc = probe.n0;
        var lc = probe.size0;
        if ((c == 1u)) {
          nc = probe.n1;
          lc = probe.size1;
        }
        if ((c == 2u)) {
          nc = probe.n2;
          lc = probe.size2;
        }
        if ((c == 3u)) {
          nc = probe.n3;
          lc = probe.size3;
        }
        let u = (px / lc);
        let v = (pz / lc);
        let fxs = ((u - floor(u)) * f32(nc));
        let fzs = ((v - floor(v)) * f32(nc));
        let fxf = floor(fxs);
        let fzf = floor(fzs);
        let x0 = min(u32(fxf), (nc - 1u));
        let z0 = min(u32(fzf), (nc - 1u));
        let rx = (fxs - fxf);
        let rz = (fzs - fzf);
        let x1 = ((x0 + 1u) % nc);
        let z1 = ((z0 + 1u) % nc);
        var v00 = vec4<f32>(0.0, 0.0, 0.0, 0.0);
        var v10 = vec4<f32>(0.0, 0.0, 0.0, 0.0);
        var v01 = vec4<f32>(0.0, 0.0, 0.0, 0.0);
        var v11 = vec4<f32>(0.0, 0.0, 0.0, 0.0);
        if ((c == 0u)) {
          v00 = disp0[((z0 * nc) + x0)];
          v10 = disp0[((z0 * nc) + x1)];
          v01 = disp0[((z1 * nc) + x0)];
          v11 = disp0[((z1 * nc) + x1)];
        }
        if ((c == 1u)) {
          v00 = disp1[((z0 * nc) + x0)];
          v10 = disp1[((z0 * nc) + x1)];
          v01 = disp1[((z1 * nc) + x0)];
          v11 = disp1[((z1 * nc) + x1)];
        }
        if ((c == 2u)) {
          v00 = disp2[((z0 * nc) + x0)];
          v10 = disp2[((z0 * nc) + x1)];
          v01 = disp2[((z1 * nc) + x0)];
          v11 = disp2[((z1 * nc) + x1)];
        }
        if ((c == 3u)) {
          v00 = disp3[((z0 * nc) + x0)];
          v10 = disp3[((z0 * nc) + x1)];
          v01 = disp3[((z1 * nc) + x0)];
          v11 = disp3[((z1 * nc) + x1)];
        }
        let cell = (lc / f32(nc));
        let bil = (((1.0 - rz) * (((1.0 - rx) * v00) + (rx * v10))) + (rz * (((1.0 - rx) * v01) + (rx * v11))));
        let gx = ((((1.0 - rz) * (v10 - v00)) + (rz * (v11 - v01))) / cell);
        let gz = ((((1.0 - rx) * (v01 - v00)) + (rx * (v11 - v10))) / cell);
        sdx = (sdx + bil.x);
        sdy = (sdy + bil.y);
        sdz = (sdz + bil.z);
        gxx = (gxx + gx.x);
        gzx = (gzx + gx.z);
        gxz = (gxz + gz.x);
        gzz = (gzz + gz.z);
      }
      if ((it < 2u)) {
        px = (qx - sdx);
        pz = (qz - sdz);
      }
      if ((it >= 2u)) {
        if ((it < 6u)) {
          let fx = ((px + sdx) - qx);
          let fz = ((pz + sdz) - qz);
          let ja = (1.0 + gxx);
          let jb = gxz;
          let jc = gzx;
          let jd = (1.0 + gzz);
          let det = ((ja * jd) - (jb * jc));
          if ((abs(det) >= 0.000001)) {
            px = (px - (((jd * fx) - (jb * fz)) / det));
            pz = (pz - (((ja * fz) - (jc * fx)) / det));
          }
        }
      }
      if ((it == 6u)) {
        if ((tg == 0u)) {
          h0 = sdy;
        }
        if ((tg == 1u)) {
          h1 = sdy;
        }
        if ((tg == 2u)) {
          h2 = sdy;
        }
        if ((tg == 3u)) {
          h3 = sdy;
        }
        if ((tg == 4u)) {
          h4 = sdy;
        }
      }
    }
  }
  let dhdx = ((h1 - h2) / (2.0 * e));
  let dhdz = ((h3 - h4) / (2.0 * e));
  let inv = (1.0 / sqrt((((dhdx * dhdx) + (dhdz * dhdz)) + 1.0)));
  out[i] = vec4<f32>(h0, (0.0 - (dhdx * inv)), (0.0 - (dhdz * inv)), probe.t);
}
`,entry:"ocean_probe",workgroup:[64,1,1],bindings:[{group:0,binding:0,name:"probe",kind:"uniform",elementType:"OceanProbeParams"},{group:0,binding:1,name:"points",kind:"storage-read",elementType:"vec4<f32>"},{group:0,binding:2,name:"disp0",kind:"storage-read",elementType:"vec4<f32>"},{group:0,binding:3,name:"disp1",kind:"storage-read",elementType:"vec4<f32>"},{group:0,binding:4,name:"disp2",kind:"storage-read",elementType:"vec4<f32>"},{group:0,binding:5,name:"disp3",kind:"storage-read",elementType:"vec4<f32>"},{group:0,binding:6,name:"out",kind:"storage-rw",elementType:"vec4<f32>"}],uniforms:[{group:0,binding:0,name:"probe",struct:"OceanProbeParams",size:64,fields:[{name:"count",type:"u32",offset:0,size:4},{name:"cascades",type:"u32",offset:4,size:4},{name:"_pad0",type:"u32",offset:8,size:4},{name:"_pad1",type:"u32",offset:12,size:4},{name:"n0",type:"u32",offset:16,size:4},{name:"n1",type:"u32",offset:20,size:4},{name:"n2",type:"u32",offset:24,size:4},{name:"n3",type:"u32",offset:28,size:4},{name:"size0",type:"f32",offset:32,size:4},{name:"size1",type:"f32",offset:36,size:4},{name:"size2",type:"f32",offset:40,size:4},{name:"size3",type:"f32",offset:44,size:4},{name:"t",type:"f32",offset:48,size:4},{name:"normalStep",type:"f32",offset:52,size:4},{name:"_pad2",type:"f32",offset:56,size:4},{name:"_pad3",type:"f32",offset:60,size:4}]}]},fftCompute64:{wgsl:`@group(0) @binding(0) var<storage, read_write> data : array<vec4<f32>>;

var<workgroup> rows_buf : array<vec4<f32>, 128>;
var<workgroup> cols_buf : array<vec4<f32>, 128>;

@compute @workgroup_size(32)
fn fft_rows(@builtin(local_invocation_id) lid : vec3<u32>, @builtin(workgroup_id) wid : vec3<u32>) {
  let j = lid.x;
  let line = wid.x;
  rows_buf[j] = data[((line * 64u) + j)];
  rows_buf[(j + 32u)] = data[((line * 64u) + (j + 32u))];
  workgroupBarrier();
  for (var t : u32 = 0u; (t < 6u); t = (t + 1u)) {
    let s = (1u << t);
    let p = (j / s);
    let rd = ((t % 2u) * 64u);
    let wr = (((t + 1u) % 2u) * 64u);
    let a = rows_buf[(rd + j)];
    let b = rows_buf[((rd + j) + 32u)];
    let o = (wr + (j + (s * p)));
    rows_buf[o] = (a + b);
    let wc = cos(((6.283185307179586 * f32((p * s))) / f32(64u)));
    let ws = sin(((6.283185307179586 * f32((p * s))) / f32(64u)));
    let d = (a - b);
    let tw = vec4<f32>(((d.x * wc) - (d.y * ws)), ((d.x * ws) + (d.y * wc)), ((d.z * wc) - (d.w * ws)), ((d.z * ws) + (d.w * wc)));
    rows_buf[(o + s)] = tw;
    workgroupBarrier();
  }
  data[((line * 64u) + j)] = rows_buf[((0u * 64u) + j)];
  data[((line * 64u) + (j + 32u))] = rows_buf[(((0u * 64u) + j) + 32u)];
}

@compute @workgroup_size(32)
fn fft_cols(@builtin(local_invocation_id) lid : vec3<u32>, @builtin(workgroup_id) wid : vec3<u32>) {
  let j = lid.x;
  let line = wid.x;
  cols_buf[j] = data[((j * 64u) + line)];
  cols_buf[(j + 32u)] = data[(((j + 32u) * 64u) + line)];
  workgroupBarrier();
  for (var t : u32 = 0u; (t < 6u); t = (t + 1u)) {
    let s = (1u << t);
    let p = (j / s);
    let rd = ((t % 2u) * 64u);
    let wr = (((t + 1u) % 2u) * 64u);
    let a = cols_buf[(rd + j)];
    let b = cols_buf[((rd + j) + 32u)];
    let o = (wr + (j + (s * p)));
    cols_buf[o] = (a + b);
    let wc = cos(((6.283185307179586 * f32((p * s))) / f32(64u)));
    let ws = sin(((6.283185307179586 * f32((p * s))) / f32(64u)));
    let d = (a - b);
    let tw = vec4<f32>(((d.x * wc) - (d.y * ws)), ((d.x * ws) + (d.y * wc)), ((d.z * wc) - (d.w * ws)), ((d.z * ws) + (d.w * wc)));
    cols_buf[(o + s)] = tw;
    workgroupBarrier();
  }
  data[((j * 64u) + line)] = cols_buf[((0u * 64u) + j)];
  data[(((j + 32u) * 64u) + line)] = cols_buf[(((0u * 64u) + j) + 32u)];
}
`,entry:"fft_rows",workgroup:[32,1,1],entries:[{entry:"fft_rows",workgroup:[32,1,1]},{entry:"fft_cols",workgroup:[32,1,1]}],bindings:[{group:0,binding:0,name:"data",kind:"storage-rw",elementType:"vec4<f32>"}],uniforms:[]},fftGather64:{wgsl:`struct FFTStage {
  n : u32,
  stage : u32,
  axis : u32,
  _pad : u32,
};

@group(0) @binding(0) var<uniform> fft : FFTStage;
@group(0) @binding(1) var<storage, read> src : array<vec4<f32>>;
@group(0) @binding(2) var<storage, read_write> dst : array<vec4<f32>>;

@compute @workgroup_size(64)
fn fft_stage(@builtin(global_invocation_id) gid : vec3<u32>) {
  let idx = gid.x;
  let n = fft.n;
  if ((idx >= (n * n))) {
    return;
  }
  var line : u32 = (idx / n);
  var pos : u32 = (idx % n);
  var ls : u32 = n;
  var ps : u32 = 1u;
  if ((fft.axis == 1u)) {
    line = (idx % n);
    pos = (idx / n);
    ls = 1u;
    ps = n;
  }
  let s = (1u << fft.stage);
  let q = (pos % s);
  let u = (pos / s);
  let r = (u % 2u);
  let p = (u / 2u);
  let j = ((p * s) + q);
  let a = src[((line * ls) + (j * ps))];
  let b = src[((line * ls) + ((j + (n / 2u)) * ps))];
  if ((r == 0u)) {
    dst[idx] = (a + b);
  } else {
    let wc = cos(((6.283185307179586 * f32((p * s))) / f32(n)));
    let ws = sin(((6.283185307179586 * f32((p * s))) / f32(n)));
    let d = (a - b);
    let tw = vec4<f32>(((d.x * wc) - (d.y * ws)), ((d.x * ws) + (d.y * wc)), ((d.z * wc) - (d.w * ws)), ((d.z * ws) + (d.w * wc)));
    dst[idx] = tw;
  }
}
`,entry:"fft_stage",workgroup:[64,1,1],bindings:[{group:0,binding:0,name:"fft",kind:"uniform",elementType:"FFTStage"},{group:0,binding:1,name:"src",kind:"storage-read",elementType:"vec4<f32>"},{group:0,binding:2,name:"dst",kind:"storage-rw",elementType:"vec4<f32>"}],uniforms:[{group:0,binding:0,name:"fft",struct:"FFTStage",size:16,fields:[{name:"n",type:"u32",offset:0,size:4},{name:"stage",type:"u32",offset:4,size:4},{name:"axis",type:"u32",offset:8,size:4},{name:"_pad",type:"u32",offset:12,size:4}]}],glsl:`#version 300 es
precision highp float;
precision highp int;
precision highp sampler2D;

uniform uint elio_width;

layout(std140) uniform FFTStage_ubo {
  uint n;
  uint stage;
  uint axis;
  uint _pad;
} fft;
uniform highp sampler2D src_tex;
uniform uint src_len;
layout(location = 0) out vec4 dst_out;

vec4 src_at(uint i) {
  return texelFetch(src_tex, ivec2(int(i % elio_width), int(i / elio_width)), 0);
}

void main() {
  dst_out = vec4(0.0);
  uint idx = (uint(gl_FragCoord.y) * elio_width + uint(gl_FragCoord.x));
  uint n = fft.n;
  if ((idx >= (n * n))) {
    return;
  }
  uint line = (idx / n);
  uint pos = (idx % n);
  uint ls = n;
  uint ps = 1u;
  if ((fft.axis == 1u)) {
    line = (idx % n);
    pos = (idx / n);
    ls = 1u;
    ps = n;
  }
  uint s = (1u << fft.stage);
  uint q = (pos % s);
  uint u = (pos / s);
  uint r = (u % 2u);
  uint p = (u / 2u);
  uint j = ((p * s) + q);
  vec4 a = src_at(((line * ls) + (j * ps)));
  vec4 b = src_at(((line * ls) + ((j + (n / 2u)) * ps)));
  if ((r == 0u)) {
    dst_out = (a + b);
  } else {
    float wc = cos(((6.283185307179586 * float((p * s))) / float(n)));
    float ws = sin(((6.283185307179586 * float((p * s))) / float(n)));
    vec4 d = (a - b);
    vec4 tw = vec4(((d.x * wc) - (d.y * ws)), ((d.x * ws) + (d.y * wc)), ((d.z * wc) - (d.w * ws)), ((d.z * ws) + (d.w * wc)));
    dst_out = tw;
  }
}
`,gatherLayout:{kernel:"fft_stage",inputs:[{name:"src",elem:"vec4",sampler:"src_tex",length:"src_len",binding:1}],outputs:[{name:"dst",elem:"vec4",format:"",location:0,variable:"dst_out"}],uniforms:[{name:"fft",block:"FFTStage_ubo",struct:"FFTStage",binding:0}]}},fftCompute128:{wgsl:`@group(0) @binding(0) var<storage, read_write> data : array<vec4<f32>>;

var<workgroup> rows_buf : array<vec4<f32>, 256>;
var<workgroup> cols_buf : array<vec4<f32>, 256>;

@compute @workgroup_size(64)
fn fft_rows(@builtin(local_invocation_id) lid : vec3<u32>, @builtin(workgroup_id) wid : vec3<u32>) {
  let j = lid.x;
  let line = wid.x;
  rows_buf[j] = data[((line * 128u) + j)];
  rows_buf[(j + 64u)] = data[((line * 128u) + (j + 64u))];
  workgroupBarrier();
  for (var t : u32 = 0u; (t < 7u); t = (t + 1u)) {
    let s = (1u << t);
    let p = (j / s);
    let rd = ((t % 2u) * 128u);
    let wr = (((t + 1u) % 2u) * 128u);
    let a = rows_buf[(rd + j)];
    let b = rows_buf[((rd + j) + 64u)];
    let o = (wr + (j + (s * p)));
    rows_buf[o] = (a + b);
    let wc = cos(((6.283185307179586 * f32((p * s))) / f32(128u)));
    let ws = sin(((6.283185307179586 * f32((p * s))) / f32(128u)));
    let d = (a - b);
    let tw = vec4<f32>(((d.x * wc) - (d.y * ws)), ((d.x * ws) + (d.y * wc)), ((d.z * wc) - (d.w * ws)), ((d.z * ws) + (d.w * wc)));
    rows_buf[(o + s)] = tw;
    workgroupBarrier();
  }
  data[((line * 128u) + j)] = rows_buf[((1u * 128u) + j)];
  data[((line * 128u) + (j + 64u))] = rows_buf[(((1u * 128u) + j) + 64u)];
}

@compute @workgroup_size(64)
fn fft_cols(@builtin(local_invocation_id) lid : vec3<u32>, @builtin(workgroup_id) wid : vec3<u32>) {
  let j = lid.x;
  let line = wid.x;
  cols_buf[j] = data[((j * 128u) + line)];
  cols_buf[(j + 64u)] = data[(((j + 64u) * 128u) + line)];
  workgroupBarrier();
  for (var t : u32 = 0u; (t < 7u); t = (t + 1u)) {
    let s = (1u << t);
    let p = (j / s);
    let rd = ((t % 2u) * 128u);
    let wr = (((t + 1u) % 2u) * 128u);
    let a = cols_buf[(rd + j)];
    let b = cols_buf[((rd + j) + 64u)];
    let o = (wr + (j + (s * p)));
    cols_buf[o] = (a + b);
    let wc = cos(((6.283185307179586 * f32((p * s))) / f32(128u)));
    let ws = sin(((6.283185307179586 * f32((p * s))) / f32(128u)));
    let d = (a - b);
    let tw = vec4<f32>(((d.x * wc) - (d.y * ws)), ((d.x * ws) + (d.y * wc)), ((d.z * wc) - (d.w * ws)), ((d.z * ws) + (d.w * wc)));
    cols_buf[(o + s)] = tw;
    workgroupBarrier();
  }
  data[((j * 128u) + line)] = cols_buf[((1u * 128u) + j)];
  data[(((j + 64u) * 128u) + line)] = cols_buf[(((1u * 128u) + j) + 64u)];
}
`,entry:"fft_rows",workgroup:[64,1,1],entries:[{entry:"fft_rows",workgroup:[64,1,1]},{entry:"fft_cols",workgroup:[64,1,1]}],bindings:[{group:0,binding:0,name:"data",kind:"storage-rw",elementType:"vec4<f32>"}],uniforms:[]},fftGather128:{wgsl:`struct FFTStage {
  n : u32,
  stage : u32,
  axis : u32,
  _pad : u32,
};

@group(0) @binding(0) var<uniform> fft : FFTStage;
@group(0) @binding(1) var<storage, read> src : array<vec4<f32>>;
@group(0) @binding(2) var<storage, read_write> dst : array<vec4<f32>>;

@compute @workgroup_size(64)
fn fft_stage(@builtin(global_invocation_id) gid : vec3<u32>) {
  let idx = gid.x;
  let n = fft.n;
  if ((idx >= (n * n))) {
    return;
  }
  var line : u32 = (idx / n);
  var pos : u32 = (idx % n);
  var ls : u32 = n;
  var ps : u32 = 1u;
  if ((fft.axis == 1u)) {
    line = (idx % n);
    pos = (idx / n);
    ls = 1u;
    ps = n;
  }
  let s = (1u << fft.stage);
  let q = (pos % s);
  let u = (pos / s);
  let r = (u % 2u);
  let p = (u / 2u);
  let j = ((p * s) + q);
  let a = src[((line * ls) + (j * ps))];
  let b = src[((line * ls) + ((j + (n / 2u)) * ps))];
  if ((r == 0u)) {
    dst[idx] = (a + b);
  } else {
    let wc = cos(((6.283185307179586 * f32((p * s))) / f32(n)));
    let ws = sin(((6.283185307179586 * f32((p * s))) / f32(n)));
    let d = (a - b);
    let tw = vec4<f32>(((d.x * wc) - (d.y * ws)), ((d.x * ws) + (d.y * wc)), ((d.z * wc) - (d.w * ws)), ((d.z * ws) + (d.w * wc)));
    dst[idx] = tw;
  }
}
`,entry:"fft_stage",workgroup:[64,1,1],bindings:[{group:0,binding:0,name:"fft",kind:"uniform",elementType:"FFTStage"},{group:0,binding:1,name:"src",kind:"storage-read",elementType:"vec4<f32>"},{group:0,binding:2,name:"dst",kind:"storage-rw",elementType:"vec4<f32>"}],uniforms:[{group:0,binding:0,name:"fft",struct:"FFTStage",size:16,fields:[{name:"n",type:"u32",offset:0,size:4},{name:"stage",type:"u32",offset:4,size:4},{name:"axis",type:"u32",offset:8,size:4},{name:"_pad",type:"u32",offset:12,size:4}]}],glsl:`#version 300 es
precision highp float;
precision highp int;
precision highp sampler2D;

uniform uint elio_width;

layout(std140) uniform FFTStage_ubo {
  uint n;
  uint stage;
  uint axis;
  uint _pad;
} fft;
uniform highp sampler2D src_tex;
uniform uint src_len;
layout(location = 0) out vec4 dst_out;

vec4 src_at(uint i) {
  return texelFetch(src_tex, ivec2(int(i % elio_width), int(i / elio_width)), 0);
}

void main() {
  dst_out = vec4(0.0);
  uint idx = (uint(gl_FragCoord.y) * elio_width + uint(gl_FragCoord.x));
  uint n = fft.n;
  if ((idx >= (n * n))) {
    return;
  }
  uint line = (idx / n);
  uint pos = (idx % n);
  uint ls = n;
  uint ps = 1u;
  if ((fft.axis == 1u)) {
    line = (idx % n);
    pos = (idx / n);
    ls = 1u;
    ps = n;
  }
  uint s = (1u << fft.stage);
  uint q = (pos % s);
  uint u = (pos / s);
  uint r = (u % 2u);
  uint p = (u / 2u);
  uint j = ((p * s) + q);
  vec4 a = src_at(((line * ls) + (j * ps)));
  vec4 b = src_at(((line * ls) + ((j + (n / 2u)) * ps)));
  if ((r == 0u)) {
    dst_out = (a + b);
  } else {
    float wc = cos(((6.283185307179586 * float((p * s))) / float(n)));
    float ws = sin(((6.283185307179586 * float((p * s))) / float(n)));
    vec4 d = (a - b);
    vec4 tw = vec4(((d.x * wc) - (d.y * ws)), ((d.x * ws) + (d.y * wc)), ((d.z * wc) - (d.w * ws)), ((d.z * ws) + (d.w * wc)));
    dst_out = tw;
  }
}
`,gatherLayout:{kernel:"fft_stage",inputs:[{name:"src",elem:"vec4",sampler:"src_tex",length:"src_len",binding:1}],outputs:[{name:"dst",elem:"vec4",format:"",location:0,variable:"dst_out"}],uniforms:[{name:"fft",block:"FFTStage_ubo",struct:"FFTStage",binding:0}]}},fftCompute256:{wgsl:`@group(0) @binding(0) var<storage, read_write> data : array<vec4<f32>>;

var<workgroup> rows_buf : array<vec4<f32>, 512>;
var<workgroup> cols_buf : array<vec4<f32>, 512>;

@compute @workgroup_size(128)
fn fft_rows(@builtin(local_invocation_id) lid : vec3<u32>, @builtin(workgroup_id) wid : vec3<u32>) {
  let j = lid.x;
  let line = wid.x;
  rows_buf[j] = data[((line * 256u) + j)];
  rows_buf[(j + 128u)] = data[((line * 256u) + (j + 128u))];
  workgroupBarrier();
  for (var t : u32 = 0u; (t < 8u); t = (t + 1u)) {
    let s = (1u << t);
    let p = (j / s);
    let rd = ((t % 2u) * 256u);
    let wr = (((t + 1u) % 2u) * 256u);
    let a = rows_buf[(rd + j)];
    let b = rows_buf[((rd + j) + 128u)];
    let o = (wr + (j + (s * p)));
    rows_buf[o] = (a + b);
    let wc = cos(((6.283185307179586 * f32((p * s))) / f32(256u)));
    let ws = sin(((6.283185307179586 * f32((p * s))) / f32(256u)));
    let d = (a - b);
    let tw = vec4<f32>(((d.x * wc) - (d.y * ws)), ((d.x * ws) + (d.y * wc)), ((d.z * wc) - (d.w * ws)), ((d.z * ws) + (d.w * wc)));
    rows_buf[(o + s)] = tw;
    workgroupBarrier();
  }
  data[((line * 256u) + j)] = rows_buf[((0u * 256u) + j)];
  data[((line * 256u) + (j + 128u))] = rows_buf[(((0u * 256u) + j) + 128u)];
}

@compute @workgroup_size(128)
fn fft_cols(@builtin(local_invocation_id) lid : vec3<u32>, @builtin(workgroup_id) wid : vec3<u32>) {
  let j = lid.x;
  let line = wid.x;
  cols_buf[j] = data[((j * 256u) + line)];
  cols_buf[(j + 128u)] = data[(((j + 128u) * 256u) + line)];
  workgroupBarrier();
  for (var t : u32 = 0u; (t < 8u); t = (t + 1u)) {
    let s = (1u << t);
    let p = (j / s);
    let rd = ((t % 2u) * 256u);
    let wr = (((t + 1u) % 2u) * 256u);
    let a = cols_buf[(rd + j)];
    let b = cols_buf[((rd + j) + 128u)];
    let o = (wr + (j + (s * p)));
    cols_buf[o] = (a + b);
    let wc = cos(((6.283185307179586 * f32((p * s))) / f32(256u)));
    let ws = sin(((6.283185307179586 * f32((p * s))) / f32(256u)));
    let d = (a - b);
    let tw = vec4<f32>(((d.x * wc) - (d.y * ws)), ((d.x * ws) + (d.y * wc)), ((d.z * wc) - (d.w * ws)), ((d.z * ws) + (d.w * wc)));
    cols_buf[(o + s)] = tw;
    workgroupBarrier();
  }
  data[((j * 256u) + line)] = cols_buf[((0u * 256u) + j)];
  data[(((j + 128u) * 256u) + line)] = cols_buf[(((0u * 256u) + j) + 128u)];
}
`,entry:"fft_rows",workgroup:[128,1,1],entries:[{entry:"fft_rows",workgroup:[128,1,1]},{entry:"fft_cols",workgroup:[128,1,1]}],bindings:[{group:0,binding:0,name:"data",kind:"storage-rw",elementType:"vec4<f32>"}],uniforms:[]},fftGather256:{wgsl:`struct FFTStage {
  n : u32,
  stage : u32,
  axis : u32,
  _pad : u32,
};

@group(0) @binding(0) var<uniform> fft : FFTStage;
@group(0) @binding(1) var<storage, read> src : array<vec4<f32>>;
@group(0) @binding(2) var<storage, read_write> dst : array<vec4<f32>>;

@compute @workgroup_size(64)
fn fft_stage(@builtin(global_invocation_id) gid : vec3<u32>) {
  let idx = gid.x;
  let n = fft.n;
  if ((idx >= (n * n))) {
    return;
  }
  var line : u32 = (idx / n);
  var pos : u32 = (idx % n);
  var ls : u32 = n;
  var ps : u32 = 1u;
  if ((fft.axis == 1u)) {
    line = (idx % n);
    pos = (idx / n);
    ls = 1u;
    ps = n;
  }
  let s = (1u << fft.stage);
  let q = (pos % s);
  let u = (pos / s);
  let r = (u % 2u);
  let p = (u / 2u);
  let j = ((p * s) + q);
  let a = src[((line * ls) + (j * ps))];
  let b = src[((line * ls) + ((j + (n / 2u)) * ps))];
  if ((r == 0u)) {
    dst[idx] = (a + b);
  } else {
    let wc = cos(((6.283185307179586 * f32((p * s))) / f32(n)));
    let ws = sin(((6.283185307179586 * f32((p * s))) / f32(n)));
    let d = (a - b);
    let tw = vec4<f32>(((d.x * wc) - (d.y * ws)), ((d.x * ws) + (d.y * wc)), ((d.z * wc) - (d.w * ws)), ((d.z * ws) + (d.w * wc)));
    dst[idx] = tw;
  }
}
`,entry:"fft_stage",workgroup:[64,1,1],bindings:[{group:0,binding:0,name:"fft",kind:"uniform",elementType:"FFTStage"},{group:0,binding:1,name:"src",kind:"storage-read",elementType:"vec4<f32>"},{group:0,binding:2,name:"dst",kind:"storage-rw",elementType:"vec4<f32>"}],uniforms:[{group:0,binding:0,name:"fft",struct:"FFTStage",size:16,fields:[{name:"n",type:"u32",offset:0,size:4},{name:"stage",type:"u32",offset:4,size:4},{name:"axis",type:"u32",offset:8,size:4},{name:"_pad",type:"u32",offset:12,size:4}]}],glsl:`#version 300 es
precision highp float;
precision highp int;
precision highp sampler2D;

uniform uint elio_width;

layout(std140) uniform FFTStage_ubo {
  uint n;
  uint stage;
  uint axis;
  uint _pad;
} fft;
uniform highp sampler2D src_tex;
uniform uint src_len;
layout(location = 0) out vec4 dst_out;

vec4 src_at(uint i) {
  return texelFetch(src_tex, ivec2(int(i % elio_width), int(i / elio_width)), 0);
}

void main() {
  dst_out = vec4(0.0);
  uint idx = (uint(gl_FragCoord.y) * elio_width + uint(gl_FragCoord.x));
  uint n = fft.n;
  if ((idx >= (n * n))) {
    return;
  }
  uint line = (idx / n);
  uint pos = (idx % n);
  uint ls = n;
  uint ps = 1u;
  if ((fft.axis == 1u)) {
    line = (idx % n);
    pos = (idx / n);
    ls = 1u;
    ps = n;
  }
  uint s = (1u << fft.stage);
  uint q = (pos % s);
  uint u = (pos / s);
  uint r = (u % 2u);
  uint p = (u / 2u);
  uint j = ((p * s) + q);
  vec4 a = src_at(((line * ls) + (j * ps)));
  vec4 b = src_at(((line * ls) + ((j + (n / 2u)) * ps)));
  if ((r == 0u)) {
    dst_out = (a + b);
  } else {
    float wc = cos(((6.283185307179586 * float((p * s))) / float(n)));
    float ws = sin(((6.283185307179586 * float((p * s))) / float(n)));
    vec4 d = (a - b);
    vec4 tw = vec4(((d.x * wc) - (d.y * ws)), ((d.x * ws) + (d.y * wc)), ((d.z * wc) - (d.w * ws)), ((d.z * ws) + (d.w * wc)));
    dst_out = tw;
  }
}
`,gatherLayout:{kernel:"fft_stage",inputs:[{name:"src",elem:"vec4",sampler:"src_tex",length:"src_len",binding:1}],outputs:[{name:"dst",elem:"vec4",format:"",location:0,variable:"dst_out"}],uniforms:[{name:"fft",block:"FFTStage_ubo",struct:"FFTStage",binding:0}]}}}})})(),(function(){const F={uniform:0,sampler:1,disp:2,slope:6,foam:10};function S(a,u,m,j){const V=a.indexOf(u);if(V<0)throw new Error("ocean-fft: anchor not found: "+u.slice(0,48));const Q=a.indexOf(m,V);if(Q<0)throw new Error("ocean-fft: anchor not found: "+m.slice(0,48));return a.slice(0,V)+j+a.slice(Q)}function O(a,u,m){if(a.indexOf(u)<0)throw new Error("ocean-fft: anchor not found: "+u.slice(0,48));return a.replace(u,()=>m)}function X(a){let u=`struct OceanSpec { c: array<vec4f, 4>, f: vec4f, m: vec4f };
`;u+="@group("+a+") @binding("+F.uniform+`) var<uniform> sp: OceanSpec;
`,u+="@group("+a+") @binding("+F.sampler+`) var spSampler: sampler;
`;for(let m=0;m<4;m++)u+="@group("+a+") @binding("+(F.disp+m)+") var spDisp"+m+`: texture_2d<f32>;
`;for(let m=0;m<4;m++)u+="@group("+a+") @binding("+(F.slope+m)+") var spSlope"+m+`: texture_2d<f32>;
`;return u+="@group("+a+") @binding("+F.foam+`) var spFoam: texture_2d<f32>;
`,u+=`fn spLod(c: vec4f, spacing: f32) -> f32 { return max(log2(max(spacing, 1e-3) * c.z / c.x), 0.0); }
`,u}function c(){let a=`  var p = vec3f(xz.x, level, xz.y); var dx = vec3f(1.0, 0.0, 0.0); var dz = vec3f(0.0, 0.0, 1.0);
`;a+=`  var amp = sp.m.x;
`,a+=`  let spacing = max(ocean.p[8].z * ocean.p[8].w * exp(ocean.p[8].w * f32(ring)), r * 6.2831853 / f32(segs));
`,a+=`  let wxz = xz - sp.m.yz;
`;for(let u=0;u<4;u++)a+="  if (sp.c["+u+`].w > 0.5) {
`,a+="    let uv = wxz / sp.c["+u+"].x; let lod = spLod(sp.c["+u+"], spacing); let k = smoothstep(0.0, 1.2, depth0 * sp.c["+u+`].y);
`,a+="    let a = textureSampleLevel(spDisp"+u+", spSampler, uv, lod); let b = textureSampleLevel(spSlope"+u+`, spSampler, uv, lod);
`,a+=`    p += a.xyz * k; dx += vec3f(b.z, b.x, a.w) * k; dz += vec3f(a.w, b.y, b.w) * k;
`,a+=`  }
`;return a}function e(){let a=`  var sGx = 0.0; var sGz = 0.0; var sXX = 0.0; var sZZ = 0.0; var sXZ = 0.0;
`;a+=`  let wxz = in.rest - sp.m.yz;
`;for(let u=0;u<4;u++)a+="  if (sp.c["+u+`].w > 0.5) {
`,a+="    let uv = wxz / sp.c["+u+"].x; let k = smoothstep(0.0, 1.2, in.depth0 * sp.c["+u+`].y);
`,a+="    let a = textureSample(spDisp"+u+", spSampler, uv); let b = textureSample(spSlope"+u+`, spSampler, uv);
`,a+=`    sGx += b.x * k; sGz += b.y * k; sXX += b.z * k; sZZ += b.w * k; sXZ += a.w * k;
`,a+=`  }
`;return a+=`  let sJ = (1.0 + sXX) * (1.0 + sZZ) - sXZ * sXZ;
`,a+=`  let sPersist = textureSampleLevel(spFoam, spSampler, wxz * sp.f.x, 0.0).r;
`,a+=`  let N = normalize(vec3f(sGz * sXZ - (1.0 + sZZ) * sGx, max(sJ, 0.1), sXZ * sGx - sGz * (1.0 + sXX)));
`,a}const C=`    let sNear = 1.0 - smoothstep(0.7 * sp.m.w, sp.m.w, length(in.world.xz - ocean.p[7].xz));
    let sStateless = clamp((sp.f.z - sJ) * sp.f.w, 0.0, 1.0) * lace * bu;
    let sKept = smoothstep(0.04, 0.6, sPersist) * (0.55 + 0.45 * lace);
    foam = mix(sStateless, select(sStateless, sKept, sp.f.y > 0.5), sNear);
`;function I(a,u){let m=a;return m=O(m,"@location(4) depth0: f32 };","@location(4) depth0: f32, @location(5) rest: vec2f };"),m=S(m,"  var p = vec3f(xz.x, level, xz.y);","  let ph = fract(t * ocean.p[0].w / 9.0",c()),m=O(m,"out.depth0 = depth0;","out.depth0 = depth0; out.rest = xz;"),m=O(m,`  let N = od(normalize(in.normal), in.world.xz, t, ocean.p[9].xy);
`,e()),m=O(m,"  if (dist < 250.0) {","  if (dist < 6000.0) {"),m=O(m,`    foam = (1.0 - smoothstep(0.12, 0.35, in.jacobian)) * lace * bu;
`,C),m=O(m,"(1.0 - smoothstep(80.0, 250.0, dist))","(1.0 - smoothstep(1500.0, 6000.0, dist))"),m+`
`+X(u)}window.__gosx_scene3d_ocean_fft_surface={wgsl:I,SLOT:F}})(),(function(){const F=window.__gosx_scene3d_elio_ocean.kernels,S=window.__gosx_scene3d_ocean_fft_surface.SLOT,O=1e3,X=512,c=240,C=[{size:1600,kMin:Math.PI*2/1600,kMax:.12},{size:250,kMin:.12,kMax:.8},{size:37,kMin:.8,kMax:5.4},{size:5.3,kMin:5.4,kMax:37}],I={tau:5,threshold:.72,gain:1,statelessGain:3.5},a=["struct MipParams { w: u32, h: u32, a: u32, b: u32 };","@group(0) @binding(0) var<uniform> mp: MipParams;","@group(0) @binding(1) var src: texture_2d<f32>;","@group(0) @binding(2) var dst: texture_storage_2d<rgba16float, write>;","@compute @workgroup_size(8, 8) fn mip(@builtin(global_invocation_id) id: vec3<u32>) {","  if (id.x >= mp.w || id.y >= mp.h) { return; }","  let b = vec2<i32>(id.xy) * 2;","  let s = textureLoad(src, b, 0) + textureLoad(src, b + vec2<i32>(1, 0), 0) + textureLoad(src, b + vec2<i32>(0, 1), 0) + textureLoad(src, b + vec2<i32>(1, 1), 0);","  textureStore(dst, vec2<i32>(id.xy), s * 0.25);","}"].join(`
`),u=[{group:0,binding:0,name:"mp",kind:"uniform"},{group:0,binding:1,name:"src",kind:"sampled-texture"},{group:0,binding:2,name:"dst",kind:"storage-texture",format:"rgba16float"}];function m(s){return Math.PI/2-s*Math.PI/180}function j(s,n,o,f){const w=Number.isFinite(s)&&s!==0?s:n;return Math.min(f,Math.max(o,w))}function V(s,n){const o=s.spectrum||{},f=n!=="low"&&n!=="medium",w=n==="low"?64:n==="medium"?128:256,b=Array.isArray(o.cascades)&&o.cascades.length?o.cascades:C,_=(n==="low"&&!(Array.isArray(o.cascades)&&o.cascades.length)?b.slice(0,3):b.slice(0,4)).map(function(p,t){const i=t===0?64:w,g=j(p.size,C[Math.min(t,3)].size,1,1e5),R=Math.min(p.kMax,.98*Math.PI*64/g);return{size:g,kMin:Math.max(0,p.kMin),kMax:R,n:i}});return{high:f,windSpeed:j(o.windSpeed,9,.5,40),fetchKm:j(o.fetch,300,1,2e3),gamma:j(o.peakEnhancement,3.3,1,7),spread:j(o.spread,8,1,32),swell:Math.min(1,Math.max(0,Number.isFinite(o.swell)?o.swell:0)),windDir:m(Number.isFinite(s.windDirection)?s.windDirection:0),swellDir:m(Number.isFinite(o.swellDirection)?o.swellDirection:Number.isFinite(s.windDirection)?s.windDirection:0),seed:o.seed>>>0,lambda:Math.max(0,Number.isFinite(s.choppiness)?s.choppiness:.6),cascades:_}}function Q(s){return s.cascades.map(function(n){return n.n+"@"+n.size}).join(",")+"|"+(s.high?1:0)}function r(s){return[s.windSpeed,s.fetchKm,s.gamma,s.spread,s.swell,s.windDir,s.swellDir,s.seed].join(",")+"|"+s.cascades.map(function(n){return n.kMin+":"+n.kMax}).join(",")}function d(s,n){const o=new ArrayBuffer(Math.max(16,Math.ceil(s.size/16)*16)),f=new DataView(o),w={};for(const b of s.fields){let U=n[b.name];if(U===void 0){if(!/^_pad/.test(b.name))throw new Error("ocean-fft: uniform "+s.struct+" lacks a value for "+b.name);U=0}if(w[b.name]=!0,b.type==="u32")f.setUint32(b.offset,U>>>0,!0);else if(b.type==="i32")f.setInt32(b.offset,U|0,!0);else if(b.type==="f32")f.setFloat32(b.offset,U,!0);else throw new Error("ocean-fft: unsupported uniform type "+b.type)}for(const b of Object.keys(n))if(!w[b])throw new Error("ocean-fft: uniform "+s.struct+" has no field "+b);return o}function T(s){return s.map(function(n){const o=Object.assign(Object.create(null),{binding:n.binding,visibility:GPUShaderStage.COMPUTE});return n.kind==="uniform"?o.buffer={type:"uniform"}:n.kind==="storage-read"?o.buffer={type:"read-only-storage"}:n.kind==="storage-rw"?o.buffer={type:"storage"}:n.kind==="sampled-texture"?o.texture={sampleType:"float",viewDimension:"2d"}:o.storageTexture={access:"write-only",format:n.format,viewDimension:"2d"},o})}function M(s,n){const o="gosx-ocean-fft-",f=Object.create(null),w=Object.create(null);let b=null,U="",_="",p=null,t=null,i=null,g=0;const R={device:s,ready:!1,layout:null,group:null,resolved:null,disposed:!1,update:ze,encode:ke,dispose:Ee,buildCount:0,fail:A};function A(k){const E="ocean-fft: "+(k&&k.message?k.message:String(k));n.guard&&n.guard.fail("gosx-ocean-fft-surface",E)}const N=s.createBindGroupLayout({label:o+"surface",entries:v()});R.layout=N;const W=s.createSampler({label:o+"sampler",magFilter:"linear",minFilter:"linear",mipmapFilter:"linear",addressModeU:"repeat",addressModeV:"repeat"}),K=s.createBuffer({label:o+"surface",size:96,usage:GPUBufferUsage.UNIFORM|GPUBufferUsage.COPY_DST}),ae=new Float32Array(24),le=s.createTexture({label:o+"dummy",size:[1,1,1],format:"rgba16float",usage:GPUTextureUsage.TEXTURE_BINDING|GPUTextureUsage.COPY_DST}),l=le.createView();function v(){const k=GPUShaderStage.VERTEX|GPUShaderStage.FRAGMENT,E=[];E.push({binding:S.uniform,visibility:k,buffer:{type:"uniform"}},{binding:S.sampler,visibility:k,sampler:{type:"filtering"}});for(let B=0;B<4;B++)E.push({binding:S.disp+B,visibility:k,texture:{sampleType:"float"}},{binding:S.slope+B,visibility:k,texture:{sampleType:"float"}});return E.push({binding:S.foam,visibility:k,texture:{sampleType:"float"}}),E}function x(k,E,B,L,te){if(f[k])return f[k];const me=w[E]||(w[E]=s.createBindGroupLayout({label:o+E,entries:T(L)})),ie=s.createShaderModule({label:o+k,code:B}),de=n.create(s,"compute",{label:o+k,layout:s.createPipelineLayout({bindGroupLayouts:[me]}),compute:{module:ie,entryPoint:te}});return f[k]={handle:de,layout:me,kernel:E}}function h(k){x("spectrum","oceanSpectrum",F.oceanSpectrum.wgsl,F.oceanSpectrum.bindings,F.oceanSpectrum.entry),x("evolve","oceanEvolve",F.oceanEvolve.wgsl,F.oceanEvolve.bindings,F.oceanEvolve.entry),x("assemble","oceanAssemble",F.oceanAssemble.wgsl,F.oceanAssemble.bindings,F.oceanAssemble.entry),x("mip","mip",a,u,"mip"),x("foam","oceanFoam",F.oceanFoam.wgsl,F.oceanFoam.bindings,F.oceanFoam.entry);for(const E of k.cascades){const B=F["fftCompute"+E.n];x("fft-rows-"+E.n,"fftCompute"+E.n,B.wgsl,B.bindings,"fft_rows"),x("fft-cols-"+E.n,"fftCompute"+E.n,B.wgsl,B.bindings,"fft_cols")}}function D(k){const E=n.validation&&n.validation.get(k.handle);return!E||E.ready===!0?E&&E.pipeline||k.handle:null}function G(){for(const k of Object.keys(f))if(!D(f[k]))return!1;return!0}function ne(k,E,B){return s.createBuffer({label:o+k,size:Math.max(16,Math.ceil(E/4)*4),usage:B})}const z=GPUBufferUsage.STORAGE|GPUBufferUsage.COPY_DST,H=GPUBufferUsage.UNIFORM|GPUBufferUsage.COPY_DST;function ee(k,E){const B=k.kernel==="mip"?{bindings:u}:F[k.kernel];return s.createBindGroup({label:o+"bind",layout:k.layout,entries:B.bindings.map(function(L){if(!(L.name in E))throw new Error("ocean-fft: no resource for binding "+L.name);return{binding:L.binding,resource:E[L.name]}})})}function ce(k){return k.uniforms[0]}function he(k){_e(),R.buildCount++;const E={buffers:[],textures:[]},B=[],L=function(Y,fe,q){const J=ne(Y,fe,q);return E.buffers.push(J),J};for(let Y=0;Y<k.cascades.length;Y++){const fe=k.cascades[Y],q=fe.n,J=q*q,ve=Math.log2(q)-1,ge=function($){const oe=s.createTexture({label:o+$+Y,size:[q,q,1],format:"rgba16float",mipLevelCount:ve,usage:GPUTextureUsage.STORAGE_BINDING|GPUTextureUsage.TEXTURE_BINDING});return E.textures.push(oe),oe},P=Object.assign(Object.create(null),{c:fe,n:q,levels:ve,h0:L("h0",J*16,z),omega:L("omega",J*4,z),a:L("fieldA",J*16,z),b:L("fieldB",J*16,z),specU:L("spectrum-u",256,H),evolveU:L("evolve-u",256,H),assembleU:L("assemble-u",256,H),disp:ge("disp"),slope:ge("slope")}),we=function($,oe){return $.createView({baseMipLevel:oe,mipLevelCount:1})};P.spectrumGroup=ee(f.spectrum,{spec:{buffer:P.specU},h0:{buffer:P.h0},omega:{buffer:P.omega}}),P.evolveGroup=ee(f.evolve,{ev:{buffer:P.evolveU},h0:{buffer:P.h0},omega:{buffer:P.omega},outA:{buffer:P.a},outB:{buffer:P.b}}),P.fftA=ee(f["fft-rows-"+q],{data:{buffer:P.a}}),P.fftB=ee(f["fft-rows-"+q],{data:{buffer:P.b}}),P.assembleGroup=ee(f.assemble,{assem:{buffer:P.assembleU},fieldA:{buffer:P.a},fieldB:{buffer:P.b},disp:we(P.disp,0),slope:we(P.slope,0)}),P.mips=[];for(let $=1;$<ve;$++){const oe=q>>$,pe=L("mip-u",16,H);s.queue.writeBuffer(pe,0,new Uint32Array([oe,oe,0,0]));for(const re of[P.disp,P.slope])P.mips.push({w:oe,group:ee(f.mip,{mp:{buffer:pe},src:we(re,$-1),dst:we(re,$)})})}P.dispView=P.disp.createView(),P.slopeView=P.slope.createView(),B.push(P)}const te=k.high?1024:512,me=X/te,ie=Object.assign(Object.create(null),{width:te,cell:me,buffers:[L("foam-a",te*te*4,z),L("foam-b",te*te*4,z)],u:L("foam-u",256,H)});ie.tex=s.createTexture({label:o+"foam",size:[te,te,1],format:"rgba16float",usage:GPUTextureUsage.STORAGE_BINDING|GPUTextureUsage.TEXTURE_BINDING}),E.textures.push(ie.tex),ie.view=ie.tex.createView(),ie.groups=[0,1].map(function(Y){const fe={foam:{buffer:ie.u},foamIn:{buffer:ie.buffers[Y]},foamOut:{buffer:ie.buffers[1-Y]},foamTex:ie.view};for(let q=0;q<4;q++)fe["fieldB"+q]={buffer:B[Math.min(q,B.length-1)].b};return ee(f.foam,fe)});const de=[{binding:S.uniform,resource:{buffer:K}},{binding:S.sampler,resource:W}];for(let Y=0;Y<4;Y++)de.push({binding:S.disp+Y,resource:B[Y]?B[Y].dispView:l}),de.push({binding:S.slope+Y,resource:B[Y]?B[Y].slopeView:l});de.push({binding:S.foam,resource:ie.view}),R.group=s.createBindGroup({label:o+"surface",layout:N,entries:de}),b={owned:E,cascades:B,foam:ie,needSpectrum:!0},i=null,g=0}function _e(){if(b){for(const k of b.owned.buffers)k.destroy();for(const k of b.owned.textures)k.destroy();b=null,R.group=null}}function ze(k){const E=V(k.ocean,k.tier);p=R.resolved=E,h(E);const B=Q(E),L=r(E);return!b||B!==U?(U=B,he(E),_=L):L!==_&&(_=L,b.needSpectrum=!0),R.ready=G(),R.ready}function ke(k,E){if(!R.ready||!b)return;const B=p,L=s.queue;let te=E.time%O;te>=0||(te=te<0?te+O:0);const me=t===null?0:Math.min(.1,Math.max(0,E.time-t));t=E.time;const ie=ce(F.oceanSpectrum),de=ce(F.oceanEvolve),Y=ce(F.oceanAssemble),fe=k.beginComputePass({label:o+"pass"}),q=function(Z,se,xe,ue=1){fe.setPipeline(D(Z)),fe.setBindGroup(0,se),fe.dispatchWorkgroups(xe,ue,1)};for(let Z=0;Z<b.cascades.length;Z++){const se=b.cascades[Z],xe=se.c,ue=se.n,ye=Math.ceil(ue*ue/64);b.needSpectrum&&(L.writeBuffer(se.specU,0,d(ie,{n:ue,cascade:Z,seed:B.seed,windSpeed:B.windSpeed,fetchKm:B.fetchKm,gamma:B.gamma,spread:B.spread,swell:B.swell,windDir:B.windDir,swellDir:B.swellDir,tile:xe.size,kMin:xe.kMin,kMax:xe.kMax,loopPeriod:O})),q(f.spectrum,se.spectrumGroup,ye)),L.writeBuffer(se.evolveU,0,d(de,{n:ue,t:te,tile:xe.size,loopPeriod:O})),L.writeBuffer(se.assembleU,0,d(Y,{n:ue,lambda:B.lambda})),q(f.evolve,se.evolveGroup,ye);for(const be of[se.fftA,se.fftB])q(f["fft-rows-"+ue],be,ue),q(f["fft-cols-"+ue],be,ue);q(f.assemble,se.assembleGroup,ye);for(const be of se.mips)q(f.mip,be.group,Math.ceil(be.w/8),Math.ceil(be.w/8))}b.needSpectrum=!1;const J=b.foam,ve=E.camera||{},ge=Math.floor((ve.x||0)/J.cell)-J.width/2,P=Math.floor((ve.z||0)/J.cell)-J.width/2,we=i||[ge,P];i=[ge,P];const $=b.cascades,oe=[1,1,1,1],pe=[1,1,1,1];for(let Z=0;Z<$.length;Z++)oe[Z]=$[Z].c.size,pe[Z]=$[Z].n;L.writeBuffer(J.u,0,d(ce(F.oceanFoam),{width:J.width,cascades:$.length,originX:ge,originZ:P,shiftX:ge-we[0],shiftZ:P-we[1],dt:me,tau:I.tau,threshold:I.threshold,gain:I.gain*me*60,lambda:B.lambda,cell:J.cell,n0:pe[0],n1:pe[1],n2:pe[2],n3:pe[3],size0:oe[0],size1:oe[1],size2:oe[2],size3:oe[3]})),q(f.foam,J.groups[g],Math.ceil(J.width*J.width/64)),g=1-g,fe.end();const re=ae;re.fill(0);for(let Z=0;Z<$.length;Z++)re[Z*4]=$[Z].c.size,re[Z*4+1]=Math.sqrt(Math.max($[Z].c.kMin,1e-6)*$[Z].c.kMax),re[Z*4+2]=$[Z].n,re[Z*4+3]=1;re[16]=1/(J.cell*J.width),re[17]=1,re[18]=I.threshold,re[19]=I.statelessGain,re[20]=Math.max(.3,.14*B.windSpeed*B.windSpeed/9.81),re[23]=c,L.writeBuffer(K,0,re)}function Ee(){R.disposed=!0,R.ready=!1,_e(),K.destroy(),le.destroy()}return R}function y(s,n){const o=n.environment&&n.environment.ocean;let f=s.fft;if(!o||!o.spectrum||n.guard&&n.guard.disabled.has("oceanFFT")||f&&f.device!==n.device)return f&&(f.dispose(),s.fft=null),null;f||(f=s.fft=M(n.device,n));const w={ocean:o,tier:n.tier,time:Number.isFinite(n.time)?n.time:0,camera:n.camera};return f.update(w)&&f.encode(n.encoder,w),f}window.__gosx_scene3d_ocean_fft_api={frame:y,resolve:V,pack:d,travelAngle:m,MIP_WGSL:a,LOOP_PERIOD:O},window.__gosx_scene3d_ocean_fft_api.shared={layoutKey:Q,paramKey:r,FOAM:I,FOAM_WINDOW:X,FOAM_RADIUS:c,LOOP_PERIOD:O}})(),(function(){function F(r){return r.replace(/_([a-z0-9])/g,function(d,T){return T.toUpperCase()})}function S(r){return typeof r=="boolean"?r?1:0:Number(r)}function O(r,d,T){if(typeof r=="string"){if(r.indexOf("param:")!==0)throw new Error("elio-graph: "+T+": bad value "+r);const M=r.slice(6);if(!(M in d))throw new Error("elio-graph: parameter "+M+" was not supplied");return S(d[M])}return S(r)}function X(r,d,T,M){const y=new ArrayBuffer(Math.max(16,Math.ceil(r.size/16)*16)),s=new DataView(y),n={};for(const o of r.fields){let f=d[o.name];if(f===void 0){if(!/^_pad/.test(o.name))throw new Error("elio-graph: "+M+": uniform "+r.struct+" lacks a value for "+o.name);f=0}n[o.name]=!0;const w=O(f,T,M);if(o.type==="u32")s.setUint32(o.offset,w>>>0,!0);else if(o.type==="i32")s.setInt32(o.offset,w|0,!0);else if(o.type==="f32")s.setFloat32(o.offset,w,!0);else throw new Error("elio-graph: "+M+": unsupported uniform type "+o.type)}for(const o of Object.keys(d))if(!n[o])throw new Error("elio-graph: "+M+": uniform "+r.struct+" has no field "+o);return y}function c(r){for(const d of Object.keys(r))if(typeof r[d]=="string")return!1;return!0}const e={time:1,dt:1,cameraX:1,cameraY:1,cameraZ:1},C={add:function(r,d){return r+d},sub:function(r,d){return r-d},mul:function(r,d){return r*d},div:function(r,d){return d===0?0:r/d},mod:function(r,d){return d===0?0:r-Math.floor(r/d)*d},floor:function(r){return Math.floor(r)},min:function(r,d){return Math.min(r,d)},max:function(r,d){return Math.max(r,d)}};function I(r,d){return/\[\]$/.test(r)?Float32Array.from(d||[]):(d=Number(d),Number.isFinite(d)?r==="bool"?d?1:0:r==="u32"?Math.max(0,Math.floor(d))>>>0:r==="i32"?Math.trunc(d)|0:d:0)}function a(r){const d={},T={},M={},y={},s={};let n=!0;for(const w of r){const b=w.source||{kind:"value",value:0};b.kind==="value"&&(s[w.name]=I(w.type,/\[\]$/.test(w.type)?b.value:b.value||0))}const o=function(w){if(typeof w=="number")return w;if(typeof w=="string"&&w.indexOf("param:")===0){const b=w.slice(6);return b in d?d[b]:0}throw new Error("elio-graph: bad argument "+w)},f={current:d,changed:{},set:function(w,b){const U=r.find(function(p){return p.name===w});if(!U||U.source&&U.source.kind!=="value")throw new Error("elio-graph: "+w+" is not a value parameter");const _=I(U.type,b);if(_!==s[w]){if(typeof _!="number"){s[w]=_;return}s[w]=_,U.tween>0&&!n?M[w]={from:d[w],start:null}:delete M[w]}},evaluate:function(w){const b=Number(w.time)||0,U=Number(w.dt)||0;for(const _ of r){const p=_.source||{kind:"value",value:0};let t=0;if(p.kind==="value"){t=s[_.name];const i=M[_.name];if(i){i.start===null&&(i.start=b);const g=Math.min(1,Math.max(0,(b-i.start)/_.tween));g>=1?delete M[_.name]:t=i.from+(t-i.from)*g}}else if(p.kind==="frame"){if(!e[p.name])throw new Error("elio-graph: unknown frame source "+p.name);t=p.name==="time"?b:p.name==="dt"?U:Number(w[p.name])||0}else if(p.kind==="expr"){const i=C[p.op];if(!i)throw new Error("elio-graph: unknown op "+p.op);t=i(o(p.args[0]),p.args.length>1?o(p.args[1]):0)}else if(p.kind==="delta"){const i=String(p.of).replace(/^param:/,"");t=n?0:(d[i]||0)-(T[i]||0)}else if(p.kind==="accumulate")y[_.name]=(y[_.name]||0)+o(p.of)*U,t=y[_.name];else throw new Error("elio-graph: unknown parameter source "+p.kind);d[_.name]=/\[\]$/.test(_.type)?s[_.name]:I(_.type,t)}for(const _ of r)f.changed[_.name]=n||d[_.name]!==T[_.name];for(const _ of r)T[_.name]=d[_.name];n=!1}};return f}function u(r,d,T){if(!r||r==="always")return!0;if(r.indexOf("param:")===0)return!!d[r.slice(6)];if(r.indexOf("changed:")===0)return r.slice(8).split(",").some(function(M){return!!T[M.trim()]});throw new Error("elio-graph: bad when "+r)}function m(r,d){const T=d+(r.offset||0);return r.shr?Math.max(r.shr>>T,1):T}function j(r){const d={},T={},M={};for(const f of r.resources)d[f.name]=f;for(const f of r.params)M[f.name]=!0;const y=function(f){return f in T?T[f]:f},s=function(f,w,b,U){const _=[];for(const p of f){if(p.type!=="repeat"){_.push(n(p,w,b,U));continue}const t=b+p.name;if(!(p.count>=1))throw new Error("elio-graph: repeat "+t+": count "+p.count+", want at least 1");if(!p.index||M[p.index])throw new Error("elio-graph: repeat "+t+": index "+p.index+" is empty or names a declared param");if(!p.body||!p.body.length)throw new Error("elio-graph: repeat "+t+": body is empty");if(p.index in w)throw new Error("elio-graph: repeat "+t+": index "+p.index+" is already in use by an outer repeat");for(const g of p.swap||[])if(!d[g[0]]||!d[g[1]])throw new Error("elio-graph: repeat "+t+": swap names an unknown resource");const i=p.when&&p.when!=="always"?p.when:U;for(let g=0;g<p.count;g++){const R=Object.assign({},w,{[p.index]:g});for(const A of s(p.body,R,t+"["+g+"].",i))_.push(A);for(const A of p.swap||[]){const N=y(A[0]),W=y(A[1]);T[A[0]]=W,T[A[1]]=N}}}return _},n=function(f,w,b,U){const _=Object.assign({},f,{name:b+f.name});return U&&(!f.when||f.when==="always")&&(_.when=U),f.resource&&(_.resource=y(f.resource)),f.bindings&&(_.bindings=f.bindings.map(function(p){const t=Object.assign({},p,{resource:y(p.resource)});if(p.levelAt){if(!(p.levelAt.index in w))throw new Error("elio-graph: pass "+_.name+": index "+p.levelAt.index+" is not in scope");t.level=m(p.levelAt,w[p.levelAt.index]),delete t.levelAt}return t})),f.uniforms&&(_.uniforms=f.uniforms.map(function(p){const t={};for(const i of Object.keys(p.values)){let g=p.values[i];if(g&&typeof g=="object"){if(!(g.index in w))throw new Error("elio-graph: pass "+_.name+": index "+g.index+" is not in scope");g=m(g,w[g.index])}else typeof g=="string"&&g.indexOf("param:")===0&&g.slice(6)in w&&(g=w[g.slice(6)]);t[i]=g}return Object.assign({},p,{values:t})})),_},o=s(r.passes,{},"","");for(const f of Object.keys(T))if(T[f]!==f)throw new Error("elio-graph: swaps are not restored: "+f+" still refers to "+T[f]+" at the end of the passes");return o}function V(r,d,T){if(!r||r.version!==1)throw new Error("elio-graph: unsupported graph version");if(r.backend!==T)throw new Error("elio-graph: graph is for "+r.backend+", not "+T);const M={};for(const n of r.resources)M[n.name]=n;const y={};for(const n of r.params)y[n.name]=!0;const s=function(n){if(typeof n=="string"&&n.indexOf("param:")===0&&!y[n.slice(6)])throw new Error("elio-graph: undeclared parameter "+n.slice(6))};for(const n of j(r)){const o=n.when||"always";if(o.indexOf("param:")===0)s(o);else if(o.indexOf("changed:")===0){for(const f of o.slice(8).split(","))if(!y[f.trim()])throw new Error("elio-graph: undeclared parameter "+f.trim())}else if(o!=="always")throw new Error("elio-graph: pass "+n.name+": bad when "+o);if(n.type==="mipmap"){if(!M[n.resource])throw new Error("elio-graph: pass "+n.name+": unknown resource "+n.resource);continue}if(!d.kernels[F(n.kernel)])throw new Error("elio-graph: pass "+n.name+": the module has no kernel "+n.kernel);for(const f of n.bindings)if(!M[f.resource])throw new Error("elio-graph: pass "+n.name+": unknown resource "+f.resource);for(const f of n.uniforms||[])for(const w of Object.keys(f.values))s(f.values[w])}for(const n of r.exportParams||[])if(!y[n])throw new Error("elio-graph: exportParams names undeclared parameter "+n);for(const n of r.resources)if(n.feed&&!y[n.feed])throw new Error("elio-graph: resource "+n.name+" is fed by undeclared parameter "+n.feed);for(const n of r.frameSwaps||[])for(const o of n)if(!M[o])throw new Error("elio-graph: frameSwaps names unknown resource "+o);for(const n of Object.keys(r.exports||{})){const o=M[r.exports[n]];if(!o||o.kind!=="texture"&&T!=="webgpu")throw new Error("elio-graph: export "+n+" must name a texture resource")}}function Q(r){const d=r.backend,T=r.module,M=r.host||{},y={graph:null,backend:d,ready:!1,failed:null,disposed:!1,buildCount:0,run:w,setValue:f,params:null,exportParams:b,export:U,dispose:p,fail:_};let s=null,n=null;const o=function(t){if(!y.failed){if(y.failed=t&&t.fallback||"error",y.error=t,s){try{s.dispose()}catch{}s=null}y.ready=!1,r.onFail&&r.onFail(y.failed,t),!(t&&t.fallback)&&typeof console<"u"&&console.warn&&console.warn("elio-graph: graph disabled: "+(t&&t.message?t.message:String(t)))}};try{const t=r.graph||T.graphs&&T.graphs[d+"/"+(r.tier||"high")];if(!t)throw Object.assign(new Error("elio-graph: no graph for "+d+"/"+r.tier),{fallback:"no-graph"});V(t,T,d);const i=window.__gosx_elio_graph.executors[d];if(!i)throw Object.assign(new Error("elio-graph: no executor for "+d),{fallback:"no-executor"});y.graph=t,n=y.params=a(t.params),s=i({steps:j(t).map(function(g){return{pass:g}}),graph:t,module:T,host:M,pack:X,isStatic:c,kernelKey:F,holds:u,label:r.label||t.name}),y.buildCount=1,y.ready=s.isReady()}catch(t){o(t)}function f(t,i){if(!(!n||y.failed))try{n.set(t,i)}catch(g){o(g)}}function w(t){if(!s||y.failed||y.disposed)return!1;try{if(y.ready=s.isReady(),!y.ready)return!1;const i=t||{};return n.evaluate(i),s.run(n.current,i,n.changed)!==!1}catch(i){return o(i),!1}}function b(){const t={};if(n&&y.graph)for(const i of y.graph.exportParams||[])t[i]=n.current[i];return t}function U(t){return s&&!y.failed?s.export(t):null}function _(t){o({message:t,fallback:t})}function p(){if(!y.disposed&&(y.disposed=!0,y.ready=!1,s)){try{s.dispose()}catch{}s=null}}return y}window.__gosx_elio_graph={create:Q,expand:j,pack:X,kernelKey:F,check:V,holds:u,createParams:a,executors:{}}})(),(function(){const F=`#version 300 es
void main() { vec2 p = vec2(float((gl_VertexID << 1) & 2), float(gl_VertexID & 2)); gl_Position = vec4(p * 2. - 1., 0., 1.); }`;function S(c){return{draw:c.getParameter(c.DRAW_FRAMEBUFFER_BINDING),read:c.getParameter(c.READ_FRAMEBUFFER_BINDING),viewport:c.getParameter(c.VIEWPORT),program:c.getParameter(c.CURRENT_PROGRAM),vao:c.getParameter(c.VERTEX_ARRAY_BINDING),active:c.getParameter(c.ACTIVE_TEXTURE),blend:c.isEnabled(c.BLEND),depth:c.isEnabled(c.DEPTH_TEST),cull:c.isEnabled(c.CULL_FACE),scissor:c.isEnabled(c.SCISSOR_TEST),colorMask:c.getParameter(c.COLOR_WRITEMASK),depthMask:c.getParameter(c.DEPTH_WRITEMASK)}}function O(c,e){c.bindBuffer(c.UNIFORM_BUFFER,null),c.bindBufferBase(c.UNIFORM_BUFFER,0,null),c.bindFramebuffer(c.DRAW_FRAMEBUFFER,e.draw),c.bindFramebuffer(c.READ_FRAMEBUFFER,e.read),c.viewport(e.viewport[0],e.viewport[1],e.viewport[2],e.viewport[3]),c.useProgram(e.program),c.bindVertexArray(e.vao),c.activeTexture(e.active);const C=function(I,a){a?c.enable(I):c.disable(I)};C(c.BLEND,e.blend),C(c.DEPTH_TEST,e.depth),C(c.CULL_FACE,e.cull),C(c.SCISSOR_TEST,e.scissor),c.colorMask(e.colorMask[0],e.colorMask[1],e.colorMask[2],e.colorMask[3]),c.depthMask(e.depthMask)}function X(c){const e=c.host.gl,C=c.graph,I=c.module,a=I.kernels,u=c.host.firstUnit||4;if(!(c.host.caps?c.host.caps(e):{}).colorBufferFloat)throw Object.assign(new Error("EXT_color_buffer_float is not available"),{fallback:"EXT_color_buffer_float"});const j=function(l,v){const x=e.getParameter(l);return typeof x=="number"?x:v};if(j(e.MAX_DRAW_BUFFERS,0)<2||j(e.MAX_TEXTURE_IMAGE_UNITS,0)<u+5)throw Object.assign(new Error("too few texture units or draw buffers"),{fallback:"texture-units"});for(const l of c.steps){const v=l.pass;if(!v||v.type!=="kernel")continue;const x=a[c.kernelKey(v.kernel)];if(!x.glsl||!x.gatherLayout)throw Object.assign(new Error("kernel "+v.kernel+" has no GLSL gather form"),{fallback:"kernel:"+v.kernel})}const V=[],Q=[],r=[],d=[],T=Object.create(null),M={};for(const l of Object.keys(C.exports||{}))M[C.exports[l]]=!0;const y=e.createVertexArray(),s={split:!1,disposed:!1};let n=null;const o=Object.create(null),f=new Map;let w=0;const b={rgba16float:e.RGBA16F,rgba32float:e.RGBA32F,r32float:e.R32F};function U(l,v,x,h,D){const G=e.createTexture();return V.push(G),e.activeTexture(e.TEXTURE0+u),e.bindTexture(e.TEXTURE_2D,G),e.texStorage2D(e.TEXTURE_2D,h,x,l,v),e.texParameteri(e.TEXTURE_2D,e.TEXTURE_MIN_FILTER,D?h>1?e.LINEAR_MIPMAP_LINEAR:e.LINEAR:e.NEAREST),e.texParameteri(e.TEXTURE_2D,e.TEXTURE_MAG_FILTER,D?e.LINEAR:e.NEAREST),e.texParameteri(e.TEXTURE_2D,e.TEXTURE_WRAP_S,D?e.REPEAT:e.CLAMP_TO_EDGE),e.texParameteri(e.TEXTURE_2D,e.TEXTURE_WRAP_T,D?e.REPEAT:e.CLAMP_TO_EDGE),G}function _(l,v){const x=e.createShader(l);if(d.push(x),e.shaderSource(x,v),e.compileShader(x),!e.getShaderParameter(x,e.COMPILE_STATUS))throw new Error("elio-graph: shader did not compile: "+e.getShaderInfoLog(x));return x}function p(l){if(T[l])return T[l];const v=a[c.kernelKey(l)];n||(n=_(e.VERTEX_SHADER,F));const x=_(e.FRAGMENT_SHADER,v.glsl),h=e.createProgram();e.attachShader(h,n),e.attachShader(h,x),e.linkProgram(h);const D=v.gatherLayout,G=T[l]={handle:h,layout:D,kernel:v};if(!e.getProgramParameter(h,e.LINK_STATUS))throw new Error("elio-graph: program "+l+" did not link: "+e.getProgramInfoLog(h));return e.useProgram(h),G.width=e.getUniformLocation(h,"elio_width"),G.lens=D.inputs.map(function(ne){return e.getUniformLocation(h,ne.length)}),D.inputs.forEach(function(ne,z){e.uniform1i(e.getUniformLocation(h,ne.sampler),u+z)}),D.uniforms.forEach(function(ne,z){e.uniformBlockBinding(h,e.getUniformBlockIndex(h,ne.block),z)}),G}function t(l,v){const h=(l.uniformsStd140||l.uniforms).find(function(D){return D.name===v});if(!h)throw new Error("elio-graph: kernel has no uniform block "+v);return h}function i(){for(const l of C.resources){let v;if(l.kind==="buffer"){const x=Math.ceil(l.count/l.width);v={name:l.name,id:++w,w:l.width,h:x,levels:1,tex:U(l.width,x,e.RGBA32F,1,!1)}}else{const x=l.mipLevels||1,h=b[l.format];if(!h)throw new Error("elio-graph: unsupported format "+l.format);const D=l.format==="rgba16float"&&(x>1||!!M[l.name]);v={name:l.name,id:++w,w:l.width,h:l.height,levels:x,tex:U(l.width,l.height,h,x,D)}}o[l.name]=v}}function g(l){const v=e.createFramebuffer();Q.push(v),e.bindFramebuffer(e.FRAMEBUFFER,v);const x=[];for(const h of l){for(e.framebufferTexture2D(e.FRAMEBUFFER,e.COLOR_ATTACHMENT0+h.location,e.TEXTURE_2D,h.res.tex,h.level);x.length<=h.location;)x.push(e.NONE);x[h.location]=e.COLOR_ATTACHMENT0+h.location}return e.drawBuffers(x),{fbo:v,complete:e.checkFramebufferStatus(e.FRAMEBUFFER)===e.FRAMEBUFFER_COMPLETE}}function R(l){const v=l.map(function(G){return G.res.id+"@"+G.level+":"+G.location}).join(","),x=f.get(v+(s.split?"s":""));if(x)return x;let h;const D=function(G){const ne=g([G]);if(!ne.complete)throw new Error("elio-graph: framebuffer incomplete");return ne};if(l.length===1)h=[D(l[0])];else{const G=s.split?null:g(l);G&&G.complete?h=[G]:(s.split=!0,h=l.map(D))}return f.set(v+(s.split?"s":""),h),h}function A(){if(!s.disposed){s.disposed=!0;for(const l of V)e.deleteTexture(l);for(const l of Q)e.deleteFramebuffer(l);for(const l of r)e.deleteBuffer(l);for(const l of Object.keys(T))e.deleteProgram(T[l].handle);for(const l of d)e.deleteShader(l);e.deleteVertexArray(y),V.length=Q.length=r.length=0,f.clear()}}let N=[];const W=S(e);try{N=c.steps.map(function(l){const v=l.pass;if(v.type==="mipmap")return{pass:v};const x=p(v.kernel),h=x.layout,D=h.inputs.map(function(z){const H=v.bindings.find(function(ee){return ee.name===z.name});if(!H)throw new Error("elio-graph: pass "+v.name+" does not bind input "+z.name);return H}),G=h.outputs.map(function(z){const H=v.bindings.find(function(ee){return ee.name===z.name});if(!H)throw new Error("elio-graph: pass "+v.name+" does not bind output "+z.name);return{binding:H,location:z.location}}),ne=(v.uniforms||[]).map(function(z){const H=h.uniforms.findIndex(function(ze){return ze.name===z.name});if(H<0)throw new Error("elio-graph: pass "+v.name+" has no uniform block "+z.name);const ee=t(x.kernel,z.name),ce=e.createBuffer(),he=Math.max(16,Math.ceil(ee.size/16)*16);return r.push(ce),e.bindBuffer(e.UNIFORM_BUFFER,ce),e.bufferData(e.UNIFORM_BUFFER,he,e.DYNAMIC_DRAW),{index:H,layout:ee,values:z.values,buffer:ce,size:he,written:!1,constant:c.isStatic(z.values)}});return{pass:v,p:x,inputs:D,outputs:G,blocks:ne}}),i()}catch(l){throw A(),l}finally{O(e,W)}function K(l){for(const v of l){const x=o[v[0]];o[v[0]]=o[v[1]],o[v[1]]=x}}function ae(l){for(const v of C.resources){if(!v.feed)continue;const x=o[v.name],h=new Float32Array(x.w*x.h*4);h.set(l[v.feed].subarray(0,h.length)),e.activeTexture(e.TEXTURE0+u),e.bindTexture(e.TEXTURE_2D,x.tex),e.texSubImage2D(e.TEXTURE_2D,0,0,0,x.w,x.h,e.RGBA,e.FLOAT,h)}}function le(l,v){const x=l.pass,h=l.p,D=x.target,G=l.outputs.map(function(z){return{res:o[z.binding.resource],level:z.binding.level||0,location:z.location}}),ne=l.inputs.map(function(z){return o[z.resource]});l.inputs.forEach(function(z,H){for(const ee of G)if(ee.res===ne[H]&&ee.level===(z.level||0))throw new Error("elio-graph: pass "+x.name+" reads "+ee.res.name+", which it writes")}),e.useProgram(h.handle),e.uniform1ui(h.width,D.elioWidth),ne.forEach(function(z,H){e.activeTexture(e.TEXTURE0+u+H),e.bindTexture(e.TEXTURE_2D,z.tex),h.lens[H]&&e.uniform1ui(h.lens[H],z.w*z.h)});for(const z of l.blocks)(!z.constant||!z.written)&&(e.bindBuffer(e.UNIFORM_BUFFER,z.buffer),e.bufferSubData(e.UNIFORM_BUFFER,0,new Uint8Array(c.pack(z.layout,z.values,v,x.name))),z.written=!0),e.bindBufferRange(e.UNIFORM_BUFFER,z.index,z.buffer,0,z.size);e.viewport(0,0,D.width,D.height);for(const z of R(G))e.bindFramebuffer(e.FRAMEBUFFER,z.fbo),e.drawArrays(e.TRIANGLES,0,3)}return{isReady:function(){return!s.disposed},run:function(l,v,x){if(e.isContextLost&&e.isContextLost())return!1;const h=S(e);try{e.disable(e.BLEND),e.disable(e.DEPTH_TEST),e.disable(e.CULL_FACE),e.disable(e.SCISSOR_TEST),e.colorMask(!0,!0,!0,!0),e.bindVertexArray(y),ae(l);for(const G of N)c.holds(G.pass.when,l,x)&&(G.pass.type==="mipmap"?(e.activeTexture(e.TEXTURE0+u),e.bindTexture(e.TEXTURE_2D,o[G.pass.resource].tex),e.generateMipmap(e.TEXTURE_2D)):le(G,l));K(C.frameSwaps||[])}finally{O(e,h)}const D=c.host.textureCache;return D&&(D._sceneTextureEpoch=(D._sceneTextureEpoch||0)+1),!0},export:function(l){const v=o[C.exports[l]];return v?{tex:v.tex,width:v.w,height:v.h,levels:v.levels}:null},split:function(){return s.split},dispose:A}}window.__gosx_elio_graph.executors.webgl2=X})(),(function(){const F={f32:4,"vec2<f32>":8,"vec4<f32>":16};function S(O){const X=O.host.device,c=O.graph,e=O.module.kernels,C=O.host;if(!X)throw Object.assign(new Error("no device"),{fallback:"no-device"});const I="gosx-elio-"+O.label+"-",a=[],u=[],m=Object.create(null),j=Object.create(null),V=Object.create(null),Q=new Map,r={disposed:!1};let d=0;const T={};for(const t of Object.keys(c.exports||{}))T[c.exports[t]]=!0;const M=X.queue,y=GPUBufferUsage.STORAGE|GPUBufferUsage.COPY_DST,s=GPUBufferUsage.UNIFORM|GPUBufferUsage.COPY_DST;for(const t of c.resources)if(t.kind==="buffer"){const i=Math.max(16,Math.ceil(t.count*(F[t.elementType]||16)/4)*4),g=X.createBuffer({label:I+t.name,size:i,usage:y});a.push(g),m[t.name]={name:t.name,id:++d,buffer:g}}else{const i=X.createTexture({label:I+t.name,size:[t.width,t.height,1],format:t.format,mipLevelCount:t.mipLevels||1,usage:GPUTextureUsage.STORAGE_BINDING|GPUTextureUsage.TEXTURE_BINDING});u.push(i),m[t.name]={name:t.name,id:++d,texture:i,w:t.width,h:t.height,levels:t.mipLevels||1,format:t.format}}function n(t){return t.map(function(i){const g={binding:i.binding,visibility:GPUShaderStage.COMPUTE};return i.kind==="uniform"?g.buffer={type:"uniform"}:i.kind==="storage-read"?g.buffer={type:"read-only-storage"}:i.kind==="storage-rw"?g.buffer={type:"storage"}:i.kind==="sampled-texture"||i.kind==="texture"?g.texture={sampleType:"unfilterable-float",viewDimension:"2d"}:g.storageTexture={access:"write-only",format:i.format,viewDimension:"2d"},g})}for(const t of O.steps){const i=t.pass;if(!i)continue;if(i.type!=="kernel")throw new Error("elio-graph: pass "+i.name+": "+i.type+" passes exist only on webgl2");const g=i.kernel+"/"+i.entry;if(j[g])continue;const R=e[O.kernelKey(i.kernel)],A=V[i.kernel]||(V[i.kernel]=X.createBindGroupLayout({label:I+i.kernel,entries:n(R.bindings)})),N=X.createShaderModule({label:I+i.kernel,code:R.wgsl}),W=C.create(X,"compute",{label:I+i.kernel,layout:X.createPipelineLayout({bindGroupLayouts:[A]}),compute:{module:N,entryPoint:i.entry}});j[g]={handle:W,layout:A,kernel:R}}function o(t){const i=C.validation&&C.validation.get(t.handle);return!i||i.ready===!0?i&&i.pipeline||t.handle:null}let f=0;const w=O.steps.map(function(t){const i=t.pass,g=j[i.kernel+"/"+i.entry],R=(i.uniforms||[]).map(function(A){const N=g.kernel.uniforms.find(function(ae){return ae.name===A.name});if(!N)throw new Error("elio-graph: pass "+i.name+" has no uniform block "+A.name);const W=Math.max(16,Math.ceil(N.size/16)*16),K=X.createBuffer({label:I+i.name+"-"+A.name,size:W,usage:s});return a.push(K),{name:A.name,layout:N,values:A.values,buffer:K,constant:O.isStatic(A.values),written:!1}});return{pass:i,p:g,blocks:R,id:++f}});function b(t){const i=t.pass,g=[],R=[i.name,t.id];for(const W of t.p.kernel.bindings){let K;if(W.kind==="uniform")K={buffer:t.blocks.find(function(ae){return ae.name===W.name}).buffer};else{const ae=i.bindings.find(function(l){return l.name===W.name});if(!ae)throw new Error("elio-graph: pass "+i.name+" does not bind "+W.name);const le=m[ae.resource];R.push(le.id+"@"+(ae.level||0)),W.kind==="storage-read"||W.kind==="storage-rw"?K={buffer:le.buffer}:K=le.texture.createView({baseMipLevel:ae.level||0,mipLevelCount:1})}g.push({binding:W.binding,resource:K})}const A=R.join("|");let N=Q.get(A);return N||(N=X.createBindGroup({label:I+i.name,layout:t.p.layout,entries:g}),Q.set(A,N)),N}function U(t){for(const i of t){const g=m[i[0]];m[i[0]]=m[i[1]],m[i[1]]=g}}const _=X.lost&&X.lost.then?X.lost:null;_&&_.then(function(){p()},function(){p()});function p(){if(!r.disposed){r.disposed=!0;for(const t of a)t.destroy();for(const t of u)t.destroy();a.length=u.length=0,Q.clear()}}return{isReady:function(){if(r.disposed)return!1;for(const t of Object.keys(j))if(!o(j[t]))return!1;return!0},run:function(t,i,g){const R=i.encoder;if(!R)throw new Error("elio-graph: a webgpu frame needs frame.encoder");for(const A of c.resources)A.feed&&M.writeBuffer(m[A.name].buffer,0,t[A.feed]);for(const A of w){const N=A.pass;if(!O.holds(N.when,t,g))continue;for(const K of A.blocks)K.constant&&K.written||(M.writeBuffer(K.buffer,0,new Uint8Array(O.pack(K.layout,K.values,t,N.name))),K.written=!0);const W=R.beginComputePass({label:I+N.name});W.setPipeline(o(A.p)),W.setBindGroup(0,b(A)),W.dispatchWorkgroups(N.dispatch[0],N.dispatch[1]||1,N.dispatch[2]||1),W.end()}return U(c.frameSwaps||[]),!0},export:function(t){const i=m[c.exports[t]];return i&&i.buffer?{buffer:i.buffer}:i?{texture:i.texture,view:i.texture.createView(),width:i.w,height:i.h,levels:i.levels}:null},dispose:p}}window.__gosx_elio_graph.executors.webgpu=S})(),(function(){const F={disp:4,slope:8,foam:12};function S(a,u,m){if(a.indexOf(u)<0)throw new Error("ocean-fft: anchor not found: "+u.slice(0,48));return a.replace(u,()=>m)}function O(a,u,m,j){const V=a.indexOf(u);if(V<0)throw new Error("ocean-fft: anchor not found: "+u.slice(0,48));const Q=a.indexOf(m,V);if(Q<0)throw new Error("ocean-fft: anchor not found: "+m.slice(0,48));return a.slice(0,V)+j+a.slice(Q)}function X(){let a=`uniform vec4 u_sp[6];
`;for(let u=0;u<4;u++)a+="uniform sampler2D u_spDisp"+u+`;
`;for(let u=0;u<4;u++)a+="uniform sampler2D u_spSlope"+u+`;
`;return a+=`uniform sampler2D u_spFoam;
`,a+=`float spLod(vec4 c, float spacing) { return max(log2(max(spacing, 1e-3) * c.z / c.x), 0.); }
`,a}function c(){let a=`  vec3 p = vec3(xz.x, level, xz.y), dx = vec3(1., 0., 0.), dz = vec3(0., 0., 1.);
`;a+=`  float amp = u_sp[5].x;
`,a+=`  float spacing = max(u_ocean[8].z * u_ocean[8].w * exp(u_ocean[8].w * float(ring)), r * 6.2831853 / float(segs));
`,a+=`  vec2 wxz = xz - u_sp[5].yz;
`;for(let u=0;u<4;u++)a+="  if (u_sp["+u+`].w > 0.5) {
`,a+="    vec2 suv = wxz / u_sp["+u+"].x; float slod = spLod(u_sp["+u+"], spacing); float sk = smoothstep(0., 1.2, depth0 * u_sp["+u+`].y);
`,a+="    vec4 sa = textureLod(u_spDisp"+u+", suv, slod), sb = textureLod(u_spSlope"+u+`, suv, slod);
`,a+=`    p += sa.xyz * sk; dx += vec3(sb.z, sb.x, sa.w) * sk; dz += vec3(sa.w, sb.y, sb.w) * sk;
`,a+=`  }
`;return a}function e(){let a=`  float sGx = 0., sGz = 0., sXX = 0., sZZ = 0., sXZ = 0.;
`;a+=`  vec2 wxz = v_rest - u_sp[5].yz;
`;for(let u=0;u<4;u++)a+="  if (u_sp["+u+`].w > 0.5) {
`,a+="    vec2 suv = wxz / u_sp["+u+"].x; float sk = smoothstep(0., 1.2, v_d0 * u_sp["+u+`].y);
`,a+="    vec4 sa = texture(u_spDisp"+u+", suv), sb = texture(u_spSlope"+u+`, suv);
`,a+=`    sGx += sb.x * sk; sGz += sb.y * sk; sXX += sb.z * sk; sZZ += sb.w * sk; sXZ += sa.w * sk;
`,a+=`  }
`;return a+=`  float sJ = (1. + sXX) * (1. + sZZ) - sXZ * sXZ;
`,a+=`  float sPersist = textureLod(u_spFoam, wxz * u_sp[4].x, 0.).r;
`,a+=`  vec3 N = normalize(vec3(sGz * sXZ - (1. + sZZ) * sGx, max(sJ, 0.1), sXZ * sGx - sGz * (1. + sXX)));
`,a}const C=`    float sNear = 1. - smoothstep(0.7 * u_sp[5].w, u_sp[5].w, length(v_w.xz - u_ocean[7].xz));
    float sStateless = clamp((u_sp[4].z - sJ) * u_sp[4].w, 0., 1.) * lace * bu;
    float sKept = smoothstep(0.04, 0.6, sPersist) * (0.55 + 0.45 * lace);
    foam = mix(sStateless, u_sp[4].y > 0.5 ? sKept : sStateless, sNear);
`;function I(a,u){let m=a;m=S(m,"out float v_d0;","out float v_d0; out vec2 v_rest;"),m=S(m,"void main() {",X()+"void main() {"),m=O(m,"  vec3 p = vec3(xz.x, level, xz.y), dx","  float ph = fract(t * u_ocean[0].w / 9.",c()),m=S(m,"v_d0 = depth0;","v_d0 = depth0; v_rest = xz;");let j=u;return j=S(j,"in float v_d0;","in float v_d0; in vec2 v_rest;"),j=S(j,"void main() {",X()+"void main() {"),j=S(j,`  vec3 N = od(normalize(v_n), v_w.xz, t, u_ocean[9].xy);
`,e()),j=S(j,"  if (dist < 250.) {","  if (dist < 6000.) {"),j=S(j,`    foam = (1. - smoothstep(0.12, 0.35, v_j)) * lace * bu;
`,C),j=S(j,"(1. - smoothstep(80., 250., dist))","(1. - smoothstep(1500., 6000., dist))"),{vertex:m,fragment:j}}window.__gosx_scene3d_ocean_fft_surface_gl={glsl:I,UNIT:F}})();
