(function(){"use strict";function c(_){if(_&&typeof _=="object"){for(const y of Object.keys(_))c(_[y]);Object.freeze(_)}return _}window.__gosx_scene3d_elio_ocean=c({elio:"v0.4.3+ocean",set:"ocean",sha256:"62e1caa85b44b5d9d12e8ca3a9ee69c9b89cdafa3485d2eea64780997f79a5b2",kernels:{oceanSpectrum:{wgsl:`struct OceanSpectrumParams {
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
`,gatherLayout:{kernel:"fft_stage",inputs:[{name:"src",elem:"vec4",sampler:"src_tex",length:"src_len",binding:1}],outputs:[{name:"dst",elem:"vec4",format:"",location:0,variable:"dst_out"}],uniforms:[{name:"fft",block:"FFTStage_ubo",struct:"FFTStage",binding:0}]}}}})})(),(function(){const c={uniform:0,sampler:1,disp:2,slope:6,foam:10};function _(o,f,u,se){const L=o.indexOf(f);if(L<0)throw new Error("ocean-fft: anchor not found: "+f.slice(0,48));const q=o.indexOf(u,L);if(q<0)throw new Error("ocean-fft: anchor not found: "+u.slice(0,48));return o.slice(0,L)+se+o.slice(q)}function y(o,f,u){if(o.indexOf(f)<0)throw new Error("ocean-fft: anchor not found: "+f.slice(0,48));return o.replace(f,()=>u)}function oe(o){let f=`struct OceanSpec { c: array<vec4f, 4>, f: vec4f, m: vec4f };
`;f+="@group("+o+") @binding("+c.uniform+`) var<uniform> sp: OceanSpec;
`,f+="@group("+o+") @binding("+c.sampler+`) var spSampler: sampler;
`;for(let u=0;u<4;u++)f+="@group("+o+") @binding("+(c.disp+u)+") var spDisp"+u+`: texture_2d<f32>;
`;for(let u=0;u<4;u++)f+="@group("+o+") @binding("+(c.slope+u)+") var spSlope"+u+`: texture_2d<f32>;
`;return f+="@group("+o+") @binding("+c.foam+`) var spFoam: texture_2d<f32>;
`,f+=`fn spLod(c: vec4f, spacing: f32) -> f32 { return max(log2(max(spacing, 1e-3) * c.z / c.x), 0.0); }
`,f}function fe(){let o=`  var p = vec3f(xz.x, level, xz.y); var dx = vec3f(1.0, 0.0, 0.0); var dz = vec3f(0.0, 0.0, 1.0);
`;o+=`  var amp = sp.m.x;
`,o+=`  let spacing = max(ocean.p[8].z * ocean.p[8].w * exp(ocean.p[8].w * f32(ring)), r * 6.2831853 / f32(segs));
`,o+=`  let wxz = xz - sp.m.yz;
`;for(let f=0;f<4;f++)o+="  if (sp.c["+f+`].w > 0.5) {
`,o+="    let uv = wxz / sp.c["+f+"].x; let lod = spLod(sp.c["+f+"], spacing); let k = smoothstep(0.0, 1.2, depth0 * sp.c["+f+`].y);
`,o+="    let a = textureSampleLevel(spDisp"+f+", spSampler, uv, lod); let b = textureSampleLevel(spSlope"+f+`, spSampler, uv, lod);
`,o+=`    p += a.xyz * k; dx += vec3f(b.z, b.x, a.w) * k; dz += vec3f(a.w, b.y, b.w) * k;
`,o+=`  }
`;return o}function ce(){let o=`  var sGx = 0.0; var sGz = 0.0; var sXX = 0.0; var sZZ = 0.0; var sXZ = 0.0;
`;o+=`  let wxz = in.rest - sp.m.yz;
`;for(let f=0;f<4;f++)o+="  if (sp.c["+f+`].w > 0.5) {
`,o+="    let uv = wxz / sp.c["+f+"].x; let k = smoothstep(0.0, 1.2, in.depth0 * sp.c["+f+`].y);
`,o+="    let a = textureSample(spDisp"+f+", spSampler, uv); let b = textureSample(spSlope"+f+`, spSampler, uv);
`,o+=`    sGx += b.x * k; sGz += b.y * k; sXX += b.z * k; sZZ += b.w * k; sXZ += a.w * k;
`,o+=`  }
`;return o+=`  let sJ = (1.0 + sXX) * (1.0 + sZZ) - sXZ * sXZ;
`,o+=`  let sPersist = textureSampleLevel(spFoam, spSampler, wxz * sp.f.x, 0.0).r;
`,o+=`  let N = normalize(vec3f(sGz * sXZ - (1.0 + sZZ) * sGx, max(sJ, 0.1), sXZ * sGx - sGz * (1.0 + sXX)));
`,o}const ae=`    let sNear = 1.0 - smoothstep(0.7 * sp.m.w, sp.m.w, length(in.world.xz - ocean.p[7].xz));
    let sJf = (1.0 + sp.f.y * sXX) * (1.0 + sp.f.y * sZZ) - sp.f.y * sp.f.y * sXZ * sXZ;
    let sStateless = clamp((sp.f.z - sJf) * sp.f.w, 0.0, 1.0) * lace * bu;
    let sKept = smoothstep(0.04, 0.6, sPersist) * (0.55 + 0.45 * lace);
    foam = mix(sStateless, sKept, sNear);
`;function M(o,f){let u=o;return u=y(u,"@location(4) depth0: f32 };","@location(4) depth0: f32, @location(5) rest: vec2f };"),u=_(u,"  var p = vec3f(xz.x, level, xz.y);","  let ph = fract(t * ocean.p[0].w / 9.0",fe()),u=y(u,"out.depth0 = depth0;","out.depth0 = depth0; out.rest = xz;"),u=y(u,`  let N = od(normalize(in.normal), in.world.xz, t, ocean.p[9].xy);
`,ce()),u=y(u,"  if (dist < 250.0) {","  if (dist < 6000.0) {"),u=y(u,`    foam = (1.0 - smoothstep(0.12, 0.35, in.jacobian)) * lace * bu;
`,ae),u=y(u,"(1.0 - smoothstep(80.0, 250.0, dist))","(1.0 - smoothstep(1500.0, 6000.0, dist))"),u=y(u,"(ambient * 0.55 + sunCol * 0.04 * max(dot(N, L), 0.0))","(ambient * 1.4 + sunCol * 0.2 * max(dot(N, L), 0.0) + vec3f(0.05))"),u+`
`+oe(f)}window.__gosx_scene3d_ocean_fft_surface={wgsl:M,SLOT:c}})(),(function(){const c=window.__gosx_scene3d_elio_ocean.kernels,_=window.__gosx_scene3d_ocean_fft_surface.SLOT,y=1e3,oe=512,fe=240,ae=[{size:1600,kMin:Math.PI*2/1600,kMax:.12},{size:250,kMin:.12,kMax:.8},{size:37,kMin:.8,kMax:5.4},{size:5.3,kMin:5.4,kMax:37}],M={tau:6,maxThreshold:.64,windLow:5,windHigh:9,gain:.9,statelessGain:3.5};function o(t,i){const e=Math.min(1,Math.max(0,(t-M.windLow)/(M.windHigh-M.windLow))),s=M.maxThreshold*e*e*(3-2*e);return s===0?0:1-i*(1-s)}function f(t,i){const e=t.graphParams;return e&&Number.isFinite(e.foamTau)&&Number.isFinite(e.foamThreshold)&&Number.isFinite(e.foamGain)?{tau:e.foamTau,threshold:e.foamThreshold,gain:e.foamGain}:{tau:M.tau,threshold:o(i.windSpeed,i.lambda),gain:M.gain}}const u=["struct MipParams { w: u32, h: u32, a: u32, b: u32 };","@group(0) @binding(0) var<uniform> mp: MipParams;","@group(0) @binding(1) var src: texture_2d<f32>;","@group(0) @binding(2) var dst: texture_storage_2d<rgba16float, write>;","@compute @workgroup_size(8, 8) fn mip(@builtin(global_invocation_id) id: vec3<u32>) {","  if (id.x >= mp.w || id.y >= mp.h) { return; }","  let b = vec2<i32>(id.xy) * 2;","  let s = textureLoad(src, b, 0) + textureLoad(src, b + vec2<i32>(1, 0), 0) + textureLoad(src, b + vec2<i32>(0, 1), 0) + textureLoad(src, b + vec2<i32>(1, 1), 0);","  textureStore(dst, vec2<i32>(id.xy), s * 0.25);","}"].join(`
`),se=[{group:0,binding:0,name:"mp",kind:"uniform"},{group:0,binding:1,name:"src",kind:"sampled-texture"},{group:0,binding:2,name:"dst",kind:"storage-texture",format:"rgba16float"}];function L(t){return Math.PI/2-t*Math.PI/180}function q(t,i,e,s){const m=Number.isFinite(t)&&t!==0?t:i;return Math.min(s,Math.max(e,m))}function pe(t,i){const e=t.spectrum||{},s=i!=="low"&&i!=="medium",m=i==="low"?64:i==="medium"?128:256,l=Array.isArray(e.cascades)&&e.cascades.length?e.cascades:ae,Q=(i==="low"&&!(Array.isArray(e.cascades)&&e.cascades.length)?l.slice(0,3):l.slice(0,4)).map(function(R,U){const $=U===0?64:m,C=q(R.size,ae[Math.min(U,3)].size,1,1e5),X=Math.min(R.kMax,.98*Math.PI*64/C);return{size:C,kMin:Math.max(0,R.kMin),kMax:X,n:$}});return{high:s,windSpeed:q(e.windSpeed,9,.5,40),fetchKm:q(e.fetch,300,1,2e3),gamma:q(e.peakEnhancement,3.3,1,7),spread:q(e.spread,8,1,32),swell:Math.min(1,Math.max(0,Number.isFinite(e.swell)?e.swell:0)),windDir:L(Number.isFinite(t.windDirection)?t.windDirection:0),swellDir:L(Number.isFinite(e.swellDirection)?e.swellDirection:0),seed:e.seed>>>0,lambda:Math.max(0,Number.isFinite(t.choppiness)?t.choppiness:.6),cascades:Q}}function ze(t){return t.cascades.map(function(i){return i.n+"@"+i.size}).join(",")+"|"+(t.high?1:0)}function me(t){return[t.windSpeed,t.fetchKm,t.gamma,t.spread,t.swell,t.windDir,t.swellDir,t.seed].join(",")+"|"+t.cascades.map(function(i){return i.kMin+":"+i.kMax}).join(",")}function ge(t){const i=t.presentation||{},e=function(s,m){return Number.isFinite(s)?s:m};return{timeScale:e(i.timeScale,1)>0?Math.min(4,e(i.timeScale,1)):1,driftX:e(i.waterDriftX,0),driftZ:e(i.waterDriftZ,0),transition:e(i.transitionSeconds,20)>0?e(i.transitionSeconds,20):20}}function he(t,i,e){const s=Object.assign({},i);for(const m of["windSpeed","fetchKm","gamma","spread","swell","lambda"])s[m]=t[m]+(i[m]-t[m])*e;for(const m of["windDir","swellDir"]){const l=Math.atan2(Math.sin(i[m]-t[m]),Math.cos(i[m]-t[m]));s[m]=t[m]+l*e}return s}function Y(t,i){const e=new ArrayBuffer(Math.max(16,Math.ceil(t.size/16)*16)),s=new DataView(e),m={};for(const l of t.fields){let D=i[l.name];if(D===void 0){if(!/^_pad/.test(l.name))throw new Error("ocean-fft: uniform "+t.struct+" lacks a value for "+l.name);D=0}if(m[l.name]=!0,l.type==="u32")s.setUint32(l.offset,D>>>0,!0);else if(l.type==="i32")s.setInt32(l.offset,D|0,!0);else if(l.type==="f32")s.setFloat32(l.offset,D,!0);else throw new Error("ocean-fft: unsupported uniform type "+l.type)}for(const l of Object.keys(i))if(!m[l])throw new Error("ocean-fft: uniform "+t.struct+" has no field "+l);return e}function ye(t){return t.map(function(i){const e=Object.assign(Object.create(null),{binding:i.binding,visibility:GPUShaderStage.COMPUTE});return i.kind==="uniform"?e.buffer={type:"uniform"}:i.kind==="storage-read"?e.buffer={type:"read-only-storage"}:i.kind==="storage-rw"?e.buffer={type:"storage"}:i.kind==="sampled-texture"?e.texture={sampleType:"float",viewDimension:"2d"}:e.storageTexture={access:"write-only",format:i.format,viewDimension:"2d"},e})}function ke(t,i){const e="gosx-ocean-fft-",s=Object.create(null),m=Object.create(null);let l=null,D="",Q="",R=null,U=null,$=null,C=0,X=null,ee=null,F=null,ue="",te=ge({});const K=[0,0],S={device:t,ready:!1,layout:null,group:null,resolved:null,disposed:!1,update:Ge,encode:Ae,dispose:qe,buildCount:0,fail:Be};function Be(a){const n="ocean-fft: "+(a&&a.message?a.message:String(a));i.guard&&i.guard.fail("gosx-ocean-fft-surface",n)}const ve=t.createBindGroupLayout({label:e+"surface",entries:Se()});S.layout=ve;const Te=t.createSampler({label:e+"sampler",magFilter:"linear",minFilter:"linear",mipmapFilter:"linear",addressModeU:"repeat",addressModeV:"repeat"}),le=t.createBuffer({label:e+"surface",size:96,usage:GPUBufferUsage.UNIFORM|GPUBufferUsage.COPY_DST}),Fe=new Float32Array(24),we=t.createTexture({label:e+"dummy",size:[1,1,1],format:"rgba16float",usage:GPUTextureUsage.TEXTURE_BINDING|GPUTextureUsage.COPY_DST}),xe=we.createView();function Se(){const a=GPUShaderStage.VERTEX|GPUShaderStage.FRAGMENT,n=[];n.push({binding:_.uniform,visibility:a,buffer:{type:"uniform"}},{binding:_.sampler,visibility:a,sampler:{type:"filtering"}});for(let r=0;r<4;r++)n.push({binding:_.disp+r,visibility:a,texture:{sampleType:"float"}},{binding:_.slope+r,visibility:a,texture:{sampleType:"float"}});return n.push({binding:_.foam,visibility:a,texture:{sampleType:"float"}}),n}function N(a,n,r,p,v){if(s[a])return s[a];const O=m[n]||(m[n]=t.createBindGroupLayout({label:e+n,entries:ye(p)})),x=t.createShaderModule({label:e+a,code:r}),E=i.create(t,"compute",{label:e+a,layout:t.createPipelineLayout({bindGroupLayouts:[O]}),compute:{module:x,entryPoint:v}});return s[a]={handle:E,layout:O,kernel:n}}function Oe(a){N("spectrum","oceanSpectrum",c.oceanSpectrum.wgsl,c.oceanSpectrum.bindings,c.oceanSpectrum.entry),N("evolve","oceanEvolve",c.oceanEvolve.wgsl,c.oceanEvolve.bindings,c.oceanEvolve.entry),N("assemble","oceanAssemble",c.oceanAssemble.wgsl,c.oceanAssemble.bindings,c.oceanAssemble.entry),N("mip","mip",u,se,"mip"),N("foam","oceanFoam",c.oceanFoam.wgsl,c.oceanFoam.bindings,c.oceanFoam.entry);for(const n of a.cascades){const r=c["fftCompute"+n.n];N("fft-rows-"+n.n,"fftCompute"+n.n,r.wgsl,r.bindings,"fft_rows"),N("fft-cols-"+n.n,"fftCompute"+n.n,r.wgsl,r.bindings,"fft_cols")}}function be(a){const n=i.validation&&i.validation.get(a.handle);return!n||n.ready===!0?n&&n.pipeline||a.handle:null}function Pe(){for(const a of Object.keys(s))if(!be(s[a]))return!1;return!0}function Me(a,n,r){return t.createBuffer({label:e+a,size:Math.max(16,Math.ceil(n/4)*4),usage:r})}const V=GPUBufferUsage.STORAGE|GPUBufferUsage.COPY_DST,ie=GPUBufferUsage.UNIFORM|GPUBufferUsage.COPY_DST;function W(a,n){const r=a.kernel==="mip"?{bindings:se}:c[a.kernel];return t.createBindGroup({label:e+"bind",layout:a.layout,entries:r.bindings.map(function(p){if(!(p.name in n))throw new Error("ocean-fft: no resource for binding "+p.name);return{binding:p.binding,resource:n[p.name]}})})}function re(a){return a.uniforms[0]}function De(a){_e(),S.buildCount++;const n={buffers:[],textures:[]},r=[],p=function(b,G,w){const k=Me(b,G,w);return n.buffers.push(k),k};for(let b=0;b<a.cascades.length;b++){const G=a.cascades[b],w=G.n,k=w*w,h=Math.log2(w)-1,ne=function(T){const z=t.createTexture({label:e+T+b,size:[w,w,1],format:"rgba16float",mipLevelCount:h,usage:GPUTextureUsage.STORAGE_BINDING|GPUTextureUsage.TEXTURE_BINDING});return n.textures.push(z),z},d=Object.assign(Object.create(null),{c:G,n:w,levels:h,h0:p("h0",k*16,V),omega:p("omega",k*4,V),a:p("fieldA",k*16,V),b:p("fieldB",k*16,V),specU:p("spectrum-u",256,ie),evolveU:p("evolve-u",256,ie),assembleU:p("assemble-u",256,ie),disp:ne("disp"),slope:ne("slope")}),A=function(T,z){return T.createView({baseMipLevel:z,mipLevelCount:1})};d.spectrumGroup=W(s.spectrum,{spec:{buffer:d.specU},h0:{buffer:d.h0},omega:{buffer:d.omega}}),d.evolveGroup=W(s.evolve,{ev:{buffer:d.evolveU},h0:{buffer:d.h0},omega:{buffer:d.omega},outA:{buffer:d.a},outB:{buffer:d.b}}),d.fftA=W(s["fft-rows-"+w],{data:{buffer:d.a}}),d.fftB=W(s["fft-rows-"+w],{data:{buffer:d.b}}),d.assembleGroup=W(s.assemble,{assem:{buffer:d.assembleU},fieldA:{buffer:d.a},fieldB:{buffer:d.b},disp:A(d.disp,0),slope:A(d.slope,0)}),d.mips=[];for(let T=1;T<h;T++){const z=w>>T,Z=p("mip-u",16,ie);t.queue.writeBuffer(Z,0,new Uint32Array([z,z,0,0]));for(const I of[d.disp,d.slope])d.mips.push({w:z,group:W(s.mip,{mp:{buffer:Z},src:A(I,T-1),dst:A(I,T)})})}d.dispView=d.disp.createView(),d.slopeView=d.slope.createView(),r.push(d)}const v=a.high?1024:512,O=oe/v,x=Object.assign(Object.create(null),{width:v,cell:O,buffers:[p("foam-a",v*v*4,V),p("foam-b",v*v*4,V)],u:p("foam-u",256,ie)});x.tex=t.createTexture({label:e+"foam",size:[v,v,1],format:"rgba16float",usage:GPUTextureUsage.STORAGE_BINDING|GPUTextureUsage.TEXTURE_BINDING}),n.textures.push(x.tex),x.view=x.tex.createView(),x.groups=[0,1].map(function(b){const G={foam:{buffer:x.u},foamIn:{buffer:x.buffers[b]},foamOut:{buffer:x.buffers[1-b]},foamTex:x.view};for(let w=0;w<4;w++)G["fieldB"+w]={buffer:r[Math.min(w,r.length-1)].b};return W(s.foam,G)});const E=[{binding:_.uniform,resource:{buffer:le}},{binding:_.sampler,resource:Te}];for(let b=0;b<4;b++)E.push({binding:_.disp+b,resource:r[b]?r[b].dispView:xe}),E.push({binding:_.slope+b,resource:r[b]?r[b].slopeView:xe});E.push({binding:_.foam,resource:x.view}),S.group=t.createBindGroup({label:e+"surface",layout:ve,entries:E}),l={owned:n,cascades:r,foam:x,needSpectrum:!0},$=null,C=0}function _e(){if(l){for(const a of l.owned.buffers)a.destroy();for(const a of l.owned.textures)a.destroy();l=null,S.group=null}}function Ge(a){const n=pe(a.ocean,a.tier);te=ge(a.ocean),ee=f(a.ocean,n);const r=ze(n),p=me(n)+"|"+n.lambda;let v=n;if(!X||!l||r!==D)F=null,X=n,ue=p;else{if(p!==ue&&(F={from:X,to:n,start:a.time,seconds:te.transition},ue=p),F){const x=Math.min(1,Math.max(0,(a.time-F.start)/F.seconds));F.to!==n&&(F.to=n),X=x>=1?F.to:he(F.from,F.to,x),x>=1&&(F=null)}else X=n;v=X}R=S.resolved=v,Oe(v);const O=me(v);return!l||r!==D?(D=r,De(v),Q=O):O!==Q&&(Q=O,l.needSpectrum=!0),S.ready=Pe(),S.ready}function Ae(a,n){if(!S.ready||!l)return;const r=R,p=t.queue;let v=n.time*te.timeScale%y;v>=0||(v=v<0?v+y:0);const O=U===null?0:Math.min(.1,Math.max(0,n.time-U)),x=U===null?0:Math.min(.25,Math.max(0,n.time-U));U=n.time,K[0]+=te.driftX*x,K[1]+=te.driftZ*x;const E=re(c.oceanSpectrum),b=re(c.oceanEvolve),G=re(c.oceanAssemble),w=a.beginComputePass({label:e+"pass"}),k=function(g,B,H,P=1){w.setPipeline(be(g)),w.setBindGroup(0,B),w.dispatchWorkgroups(H,P,1)};for(let g=0;g<l.cascades.length;g++){const B=l.cascades[g],H=B.c,P=B.n,de=Math.ceil(P*P/64);l.needSpectrum&&(p.writeBuffer(B.specU,0,Y(E,{n:P,cascade:g,seed:r.seed,windSpeed:r.windSpeed,fetchKm:r.fetchKm,gamma:r.gamma,spread:r.spread,swell:r.swell,windDir:r.windDir,swellDir:r.swellDir,tile:H.size,kMin:H.kMin,kMax:H.kMax,loopPeriod:y})),k(s.spectrum,B.spectrumGroup,de)),p.writeBuffer(B.evolveU,0,Y(b,{n:P,t:v,tile:H.size,loopPeriod:y})),p.writeBuffer(B.assembleU,0,Y(G,{n:P,lambda:r.lambda})),k(s.evolve,B.evolveGroup,de);for(const J of[B.fftA,B.fftB])k(s["fft-rows-"+P],J,P),k(s["fft-cols-"+P],J,P);k(s.assemble,B.assembleGroup,de);for(const J of B.mips)k(s.mip,J.group,Math.ceil(J.w/8),Math.ceil(J.w/8))}l.needSpectrum=!1;const h=l.foam,ne=n.camera||{},d=Math.floor(((ne.x||0)-K[0])/h.cell)-h.width/2,A=Math.floor(((ne.z||0)-K[1])/h.cell)-h.width/2,T=$||[d,A];$=[d,A];const z=l.cascades,Z=[1,1,1,1],I=[1,1,1,1];for(let g=0;g<z.length;g++)Z[g]=z[g].c.size,I[g]=z[g].n;p.writeBuffer(h.u,0,Y(re(c.oceanFoam),{width:h.width,cascades:z.length,originX:d,originZ:A,shiftX:d-T[0],shiftZ:A-T[1],dt:O,tau:ee.tau,threshold:ee.threshold,gain:ee.gain*O*60,lambda:r.lambda,cell:h.cell,n0:I[0],n1:I[1],n2:I[2],n3:I[3],size0:Z[0],size1:Z[1],size2:Z[2],size3:Z[3]})),k(s.foam,h.groups[C],Math.ceil(h.width*h.width/64)),C=1-C,w.end();const j=Fe;j.fill(0);for(let g=0;g<z.length;g++)j[g*4]=z[g].c.size,j[g*4+1]=Math.sqrt(Math.max(z[g].c.kMin,1e-6)*z[g].c.kMax),j[g*4+2]=z[g].n,j[g*4+3]=1;j[16]=1/(h.cell*h.width),j[17]=1,j[18]=ee.threshold,j[19]=M.statelessGain,j[20]=Math.max(.3,.14*r.windSpeed*r.windSpeed/9.81),j[21]=K[0],j[22]=K[1],j[23]=fe,p.writeBuffer(le,0,j)}function qe(){S.disposed=!0,S.ready=!1,_e(),le.destroy(),we.destroy()}return S}function je(t,i){const e=i.environment&&i.environment.ocean;let s=t.fft;if(!e||!e.spectrum||i.guard&&i.guard.disabled.has("oceanFFT")||s&&s.device!==i.device)return s&&(s.dispose(),t.fft=null),null;s||(s=t.fft=ke(i.device,i));const m={ocean:e,tier:i.tier,time:Number.isFinite(i.time)?i.time:0,camera:i.camera};return s.update(m)&&s.encode(i.encoder,m),s}window.__gosx_scene3d_ocean_fft_api={frame:je,resolve:pe,pack:Y,travelAngle:L,foamThreshold:o,foamOf:f,FOAM:M,MIP_WGSL:u,LOOP_PERIOD:y}})();
