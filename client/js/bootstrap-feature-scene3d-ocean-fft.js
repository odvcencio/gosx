(function(){"use strict";function U(R){if(R&&typeof R=="object"){for(const O of Object.keys(R))U(R[O]);Object.freeze(R)}return R}window.__gosx_scene3d_elio_ocean=U({elio:"v0.4.3+ocean",set:"ocean",sha256:"e46e6e37557772f654a205cc027621f31842e34c634763713ac9b37f33f600ca",kernels:{oceanSpectrum:{wgsl:`struct OceanSpectrumParams {
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
`,entry:"ocean_spectrum",workgroup:[64,1,1],bindings:[{group:0,binding:0,name:"spec",kind:"uniform",elementType:"OceanSpectrumParams"},{group:0,binding:1,name:"h0",kind:"storage-rw",elementType:"vec4<f32>"},{group:0,binding:2,name:"omega",kind:"storage-rw",elementType:"f32"}],uniforms:[{group:0,binding:0,name:"spec",struct:"OceanSpectrumParams",size:64,fields:[{name:"n",type:"u32",offset:0,size:4},{name:"cascade",type:"u32",offset:4,size:4},{name:"seed",type:"u32",offset:8,size:4},{name:"_pad",type:"u32",offset:12,size:4},{name:"windSpeed",type:"f32",offset:16,size:4},{name:"fetchKm",type:"f32",offset:20,size:4},{name:"gamma",type:"f32",offset:24,size:4},{name:"spread",type:"f32",offset:28,size:4},{name:"swell",type:"f32",offset:32,size:4},{name:"windDir",type:"f32",offset:36,size:4},{name:"swellDir",type:"f32",offset:40,size:4},{name:"tile",type:"f32",offset:44,size:4},{name:"kMin",type:"f32",offset:48,size:4},{name:"kMax",type:"f32",offset:52,size:4},{name:"loopPeriod",type:"f32",offset:56,size:4},{name:"_pad2",type:"f32",offset:60,size:4}]}],glsl:`#version 300 es
precision highp float;
precision highp int;
precision highp sampler2D;

uniform uint elio_width;

const float G = 9.81;

layout(std140) uniform OceanSpectrumParams_ubo {
  uint n;
  uint cascade;
  uint seed;
  uint _pad;
  float windSpeed;
  float fetchKm;
  float gamma;
  float spread;
  float swell;
  float windDir;
  float swellDir;
  float tile;
  float kMin;
  float kMax;
  float loopPeriod;
  float _pad2;
} spec;
layout(location = 0) out vec4 h0_out;
layout(location = 1) out vec4 omega_out;

void main() {
  h0_out = vec4(0.0);
  omega_out = vec4(0.0);
  uint i = (uint(gl_FragCoord.y) * elio_width + uint(gl_FragCoord.x));
  uint n = spec.n;
  if ((i >= (n * n))) {
    return;
  }
  uint x = (i % n);
  uint z = (i / n);
  int ix0 = int(x);
  if ((x >= (n / 2u))) {
    ix0 = (int(x) - int(n));
  }
  int iz0 = int(z);
  if ((z >= (n / 2u))) {
    iz0 = (int(z) - int(n));
  }
  float dk = (6.283185307179586 / spec.tile);
  float k0 = (dk * sqrt(((float(ix0) * float(ix0)) + (float(iz0) * float(iz0)))));
  float tloop = spec.loopPeriod;
  if ((tloop <= 0.0)) {
    tloop = 1000.0;
  }
  float omega0 = (6.283185307179586 / tloop);
  omega_out = vec4((omega0 * floor((sqrt((G * k0)) / omega0))), 0.0, 0.0, 0.0);
  bool inband = true;
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
  float hr = 0.0;
  float hi = 0.0;
  float mr = 0.0;
  float mi = 0.0;
  if (inband) {
    float fetch = (spec.fetchKm * 1000.0);
    float wp = (22.0 * pow(((G * G) / (spec.windSpeed * fetch)), 0.3333333333333333));
    float alpha = (0.076 * pow(((spec.windSpeed * spec.windSpeed) / (fetch * G)), 0.22));
    for (uint sgn = 0u; (sgn < 2u); sgn = (sgn + 1u)) {
      int rix = ix0;
      int riz = iz0;
      if ((sgn == 1u)) {
        rix = (0 - ix0);
        riz = (0 - iz0);
      }
      uint rseed = spec.seed;
      uint rcascade = spec.cascade;
      uint rb1 = 0u;
      uint rb2 = 0u;
      uint rh = 0u;
      for (uint rr = 0u; (rr < 6u); rr = (rr + 1u)) {
        uint rw = rseed;
        if ((rr == 1u)) {
          rw = rcascade;
        }
        if ((rr == 2u)) {
          rw = (uint(rix) & 4294967295u);
        }
        if ((rr == 3u)) {
          rw = (uint(riz) & 4294967295u);
        }
        if ((rr == 4u)) {
          rw = 2654435769u;
        }
        if ((rr == 5u)) {
          rw = 2246822519u;
        }
        uint rs = ((((rh ^ rw) * 747796405u) + 2891336453u) & 4294967295u);
        uint rv = (((rs >> ((rs >> 28u) + 4u)) ^ rs) * 277803737u);
        rh = ((rv >> 22u) ^ rv);
        if ((rr == 4u)) {
          rb1 = rh;
        }
        if ((rr == 5u)) {
          rb2 = rh;
        }
      }
      float ru1 = ((float((rb1 / 256u)) + 1.0) / 16777216.0);
      float ru2 = ((float((rb2 / 256u)) + 1.0) / 16777216.0);
      float rrad = sqrt((-2.0 * log(ru1)));
      float z0 = (rrad * cos((6.283185307179586 * ru2)));
      float z1 = (rrad * sin((6.283185307179586 * ru2)));
      float kx = (dk * float(rix));
      float kz = (dk * float(riz));
      float kk = sqrt(((kx * kx) + (kz * kz)));
      float w = sqrt((G * kk));
      float dens = 0.0;
      for (uint c = 0u; (c < 2u); c = (c + 1u)) {
        float weight = (1.0 - spec.swell);
        float wpk = wp;
        float alph = alpha;
        float gam = spec.gamma;
        float sp = spec.spread;
        float dir = spec.windDir;
        if ((c == 1u)) {
          weight = spec.swell;
          wpk = (0.5 * wp);
          alph = (alpha * 0.0625);
          gam = 7.0;
          sp = min((4.0 * spec.spread), 64.0);
          dir = spec.swellDir;
        }
        if ((weight > 0.0)) {
          float sigma = 0.07;
          if ((w > wpk)) {
            sigma = 0.09;
          }
          float rpk = exp(((-(w - wpk) * (w - wpk)) / ((((2.0 * sigma) * sigma) * wpk) * wpk)));
          float sw = (((((alph * G) * G) / pow(w, 5.0)) * exp((-1.25 * pow((wpk / w), 4.0)))) * pow(gam, rpk));
          float s = (sp * pow((w / wpk), 4.06));
          if ((w > wpk)) {
            s = (sp * pow((w / wpk), -2.33));
          }
          float lnn = -1.2655121234846454;
          for (uint q = 0u; (q < 2u); q = (q + 1u)) {
            float xq = (s + 1.0);
            float sign = 1.0;
            if ((q == 1u)) {
              xq = (s + 0.5);
              sign = -1.0;
            }
            float xs = (xq + 6.0);
            float stirling = (((((((xs - 0.5) * log(xs)) - xs) + 0.9189385332046727) + (1.0 / (12.0 * xs))) - (1.0 / (((360.0 * xs) * xs) * xs))) + (1.0 / (((((1260.0 * xs) * xs) * xs) * xs) * xs)));
            float prod = (((((xq * (xq + 1.0)) * (xq + 2.0)) * (xq + 3.0)) * (xq + 4.0)) * (xq + 5.0));
            lnn = (lnn + (sign * (stirling - log(prod))));
          }
          float cosT = (((kx * cos(dir)) + (kz * sin(dir))) / kk);
          float base = max((0.5 * (1.0 + cosT)), 0.00000001);
          dens = (dens + (((((weight * sw) * exp(lnn)) * pow(base, s)) * G) / ((2.0 * w) * kk)));
        }
      }
      float amp = ((0.5 * sqrt(dens)) * dk);
      if ((sgn == 0u)) {
        hr = (z0 * amp);
        hi = (z1 * amp);
      } else {
        mr = (z0 * amp);
        mi = (0.0 - (z1 * amp));
      }
    }
  }
  h0_out = vec4(hr, hi, mr, mi);
}
`,gatherLayout:{kernel:"ocean_spectrum",inputs:[],outputs:[{name:"h0",elem:"vec4",format:"",location:0,variable:"h0_out"},{name:"omega",elem:"f32",format:"",location:1,variable:"omega_out"}],uniforms:[{name:"spec",block:"OceanSpectrumParams_ubo",struct:"OceanSpectrumParams",binding:0}]}},oceanEvolve:{wgsl:`struct OceanEvolveParams {
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
`,gatherLayout:{kernel:"ocean_evolve",inputs:[{name:"h0",kind:"buffer",elem:"vec4",sampler:"h0_tex",length:"h0_len",binding:1},{name:"omega",kind:"buffer",elem:"f32",sampler:"omega_tex",length:"omega_len",binding:2}],outputs:[{name:"outA",elem:"vec4",format:"",location:0,variable:"outA_out"},{name:"outB",elem:"vec4",format:"",location:1,variable:"outB_out"}],uniforms:[{name:"ev",block:"OceanEvolveParams_ubo",struct:"OceanEvolveParams",binding:0}]}},oceanAssemble:{wgsl:`struct OceanAssembleParams {
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
`,gatherLayout:{kernel:"ocean_assemble",inputs:[{name:"fieldA",kind:"buffer",elem:"vec4",sampler:"fieldA_tex",length:"fieldA_len",binding:1},{name:"fieldB",kind:"buffer",elem:"vec4",sampler:"fieldB_tex",length:"fieldB_len",binding:2}],outputs:[{name:"disp",elem:"vec4",format:"rgba16float",location:0,variable:"disp_out"},{name:"slope",elem:"vec4",format:"rgba16float",location:1,variable:"slope_out"}],uniforms:[{name:"assem",block:"OceanAssembleParams_ubo",struct:"OceanAssembleParams",binding:0}]}},oceanMip:{wgsl:`struct OceanMipParams {
  srcLevel : u32,
  dstWidth : u32,
  dstHeight : u32,
  pad : u32,
};

@group(0) @binding(0) var<uniform> mip : OceanMipParams;
@group(0) @binding(1) var src : texture_2d<f32>;
@group(0) @binding(2) var dst : texture_storage_2d<rgba16float, write>;

@compute @workgroup_size(64)
fn ocean_mip(@builtin(global_invocation_id) gid : vec3<u32>) {
  let i = gid.x;
  if ((i >= (mip.dstWidth * mip.dstHeight))) {
    return;
  }
  let dx = (i % mip.dstWidth);
  let dy = (i / mip.dstWidth);
  let size = textureDimensions(src, i32(mip.srcLevel));
  let x0 = (dx * 2u);
  let y0 = (dy * 2u);
  let x1 = min((x0 + 1u), (size.x - 1u));
  let y1 = min((y0 + 1u), (size.y - 1u));
  let sum = (((textureLoad(src, vec2<i32>(vec2<u32>(x0, y0)), i32(mip.srcLevel)) + textureLoad(src, vec2<i32>(vec2<u32>(x1, y0)), i32(mip.srcLevel))) + textureLoad(src, vec2<i32>(vec2<u32>(x0, y1)), i32(mip.srcLevel))) + textureLoad(src, vec2<i32>(vec2<u32>(x1, y1)), i32(mip.srcLevel)));
  textureStore(dst, vec2<i32>(vec2<u32>((i % mip.dstWidth), (i / mip.dstWidth))), (sum * 0.25));
}
`,entry:"ocean_mip",workgroup:[64,1,1],bindings:[{group:0,binding:0,name:"mip",kind:"uniform",elementType:"OceanMipParams"},{group:0,binding:1,name:"src",kind:"texture",elementType:"vec4<f32>"},{group:0,binding:2,name:"dst",kind:"storage-texture",format:"rgba16float",elementType:"vec4<f32>"}],uniforms:[{group:0,binding:0,name:"mip",struct:"OceanMipParams",size:16,fields:[{name:"srcLevel",type:"u32",offset:0,size:4},{name:"dstWidth",type:"u32",offset:4,size:4},{name:"dstHeight",type:"u32",offset:8,size:4},{name:"pad",type:"u32",offset:12,size:4}]}],glsl:`#version 300 es
precision highp float;
precision highp int;
precision highp sampler2D;

uniform uint elio_width;

layout(std140) uniform OceanMipParams_ubo {
  uint srcLevel;
  uint dstWidth;
  uint dstHeight;
  uint pad;
} mip;
uniform highp sampler2D src;
layout(location = 0) out vec4 dst_out;

void main() {
  dst_out = vec4(0.0);
  uint i = (uint(gl_FragCoord.y) * elio_width + uint(gl_FragCoord.x));
  if ((i >= (mip.dstWidth * mip.dstHeight))) {
    return;
  }
  uint dx = (i % mip.dstWidth);
  uint dy = (i / mip.dstWidth);
  uvec2 size = uvec2(textureSize(src, int(mip.srcLevel)));
  uint x0 = (dx * 2u);
  uint y0 = (dy * 2u);
  uint x1 = min((x0 + 1u), (size.x - 1u));
  uint y1 = min((y0 + 1u), (size.y - 1u));
  vec4 sum = (((texelFetch(src, ivec2(uvec2(x0, y0)), int(mip.srcLevel)) + texelFetch(src, ivec2(uvec2(x1, y0)), int(mip.srcLevel))) + texelFetch(src, ivec2(uvec2(x0, y1)), int(mip.srcLevel))) + texelFetch(src, ivec2(uvec2(x1, y1)), int(mip.srcLevel)));
  dst_out = (sum * 0.25);
}
`,gatherLayout:{kernel:"ocean_mip",inputs:[{name:"src",kind:"texture",elem:"vec4",sampler:"src",length:"",binding:1}],outputs:[{name:"dst",elem:"vec4",format:"rgba16float",location:0,variable:"dst_out"}],uniforms:[{name:"mip",block:"OceanMipParams_ubo",struct:"OceanMipParams",binding:0}]}},oceanFoam:{wgsl:`struct OceanFoamParams {
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
  omX0 : f32,
  omX1 : f32,
  omX2 : f32,
  omX3 : f32,
  omZ0 : f32,
  omZ1 : f32,
  omZ2 : f32,
  omZ3 : f32,
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
  var sxx = 0.0;
  var szz = 0.0;
  var sxz = 0.0;
  for (var c : u32 = 0u; (c < foam.cascades); c = (c + 1u)) {
    var nc = foam.n0;
    var lc = foam.size0;
    var ox = foam.omX0;
    var oz = foam.omZ0;
    if ((c == 1u)) {
      nc = foam.n1;
      lc = foam.size1;
      ox = foam.omX1;
      oz = foam.omZ1;
    }
    if ((c == 2u)) {
      nc = foam.n2;
      lc = foam.size2;
      ox = foam.omX2;
      oz = foam.omZ2;
    }
    if ((c == 3u)) {
      nc = foam.n3;
      lc = foam.size3;
      ox = foam.omX3;
      oz = foam.omZ3;
    }
    let u = ((ox + ((f32(ru) + 0.5) * foam.cell)) / lc);
    let v = ((oz + ((f32(rv) + 0.5) * foam.cell)) / lc);
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
`,entry:"ocean_foam",workgroup:[64,1,1],bindings:[{group:0,binding:0,name:"foam",kind:"uniform",elementType:"OceanFoamParams"},{group:0,binding:1,name:"foamIn",kind:"storage-read",elementType:"f32"},{group:0,binding:2,name:"fieldB0",kind:"storage-read",elementType:"vec4<f32>"},{group:0,binding:3,name:"fieldB1",kind:"storage-read",elementType:"vec4<f32>"},{group:0,binding:4,name:"fieldB2",kind:"storage-read",elementType:"vec4<f32>"},{group:0,binding:5,name:"fieldB3",kind:"storage-read",elementType:"vec4<f32>"},{group:0,binding:6,name:"foamOut",kind:"storage-rw",elementType:"f32"},{group:0,binding:7,name:"foamTex",kind:"storage-texture",format:"rgba16float",elementType:"vec4<f32>"}],uniforms:[{group:0,binding:0,name:"foam",struct:"OceanFoamParams",size:128,fields:[{name:"width",type:"u32",offset:0,size:4},{name:"cascades",type:"u32",offset:4,size:4},{name:"_pad0",type:"u32",offset:8,size:4},{name:"_pad1",type:"u32",offset:12,size:4},{name:"originX",type:"i32",offset:16,size:4},{name:"originZ",type:"i32",offset:20,size:4},{name:"shiftX",type:"i32",offset:24,size:4},{name:"shiftZ",type:"i32",offset:28,size:4},{name:"dt",type:"f32",offset:32,size:4},{name:"tau",type:"f32",offset:36,size:4},{name:"threshold",type:"f32",offset:40,size:4},{name:"gain",type:"f32",offset:44,size:4},{name:"lambda",type:"f32",offset:48,size:4},{name:"cell",type:"f32",offset:52,size:4},{name:"_pad2",type:"f32",offset:56,size:4},{name:"_pad3",type:"f32",offset:60,size:4},{name:"n0",type:"u32",offset:64,size:4},{name:"n1",type:"u32",offset:68,size:4},{name:"n2",type:"u32",offset:72,size:4},{name:"n3",type:"u32",offset:76,size:4},{name:"size0",type:"f32",offset:80,size:4},{name:"size1",type:"f32",offset:84,size:4},{name:"size2",type:"f32",offset:88,size:4},{name:"size3",type:"f32",offset:92,size:4},{name:"omX0",type:"f32",offset:96,size:4},{name:"omX1",type:"f32",offset:100,size:4},{name:"omX2",type:"f32",offset:104,size:4},{name:"omX3",type:"f32",offset:108,size:4},{name:"omZ0",type:"f32",offset:112,size:4},{name:"omZ1",type:"f32",offset:116,size:4},{name:"omZ2",type:"f32",offset:120,size:4},{name:"omZ3",type:"f32",offset:124,size:4}]}],glsl:`#version 300 es
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
  float omX0;
  float omX1;
  float omX2;
  float omX3;
  float omZ0;
  float omZ1;
  float omZ2;
  float omZ3;
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
  float sxx = 0.0;
  float szz = 0.0;
  float sxz = 0.0;
  for (uint c = 0u; (c < foam.cascades); c = (c + 1u)) {
    uint nc = foam.n0;
    float lc = foam.size0;
    float ox = foam.omX0;
    float oz = foam.omZ0;
    if ((c == 1u)) {
      nc = foam.n1;
      lc = foam.size1;
      ox = foam.omX1;
      oz = foam.omZ1;
    }
    if ((c == 2u)) {
      nc = foam.n2;
      lc = foam.size2;
      ox = foam.omX2;
      oz = foam.omZ2;
    }
    if ((c == 3u)) {
      nc = foam.n3;
      lc = foam.size3;
      ox = foam.omX3;
      oz = foam.omZ3;
    }
    float u = ((ox + ((float(ru) + 0.5) * foam.cell)) / lc);
    float v = ((oz + ((float(rv) + 0.5) * foam.cell)) / lc);
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
`,gatherLayout:{kernel:"ocean_foam",inputs:[{name:"foamIn",kind:"buffer",elem:"f32",sampler:"foamIn_tex",length:"foamIn_len",binding:1},{name:"fieldB0",kind:"buffer",elem:"vec4",sampler:"fieldB0_tex",length:"fieldB0_len",binding:2},{name:"fieldB1",kind:"buffer",elem:"vec4",sampler:"fieldB1_tex",length:"fieldB1_len",binding:3},{name:"fieldB2",kind:"buffer",elem:"vec4",sampler:"fieldB2_tex",length:"fieldB2_len",binding:4},{name:"fieldB3",kind:"buffer",elem:"vec4",sampler:"fieldB3_tex",length:"fieldB3_len",binding:5}],outputs:[{name:"foamOut",elem:"f32",format:"",location:0,variable:"foamOut_out"},{name:"foamTex",elem:"vec4",format:"rgba16float",location:1,variable:"foamTex_out"}],uniforms:[{name:"foam",block:"OceanFoamParams_ubo",struct:"OceanFoamParams",binding:0}]}},oceanProbe:{wgsl:`struct OceanProbeParams {
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
  lambda : f32,
  _pad3 : f32,
  omX0 : f32,
  omX1 : f32,
  omX2 : f32,
  omX3 : f32,
  omZ0 : f32,
  omZ1 : f32,
  omZ2 : f32,
  omZ3 : f32,
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
        var ox = probe.omX0;
        var oz = probe.omZ0;
        if ((c == 1u)) {
          nc = probe.n1;
          lc = probe.size1;
          ox = probe.omX1;
          oz = probe.omZ1;
        }
        if ((c == 2u)) {
          nc = probe.n2;
          lc = probe.size2;
          ox = probe.omX2;
          oz = probe.omZ2;
        }
        if ((c == 3u)) {
          nc = probe.n3;
          lc = probe.size3;
          ox = probe.omX3;
          oz = probe.omZ3;
        }
        let u = ((px + ox) / lc);
        let v = ((pz + oz) / lc);
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
        sdx = (sdx + (probe.lambda * bil.y));
        sdy = (sdy + bil.x);
        sdz = (sdz + (probe.lambda * bil.z));
        gxx = (gxx + (probe.lambda * gx.y));
        gzx = (gzx + (probe.lambda * gx.z));
        gxz = (gxz + (probe.lambda * gz.y));
        gzz = (gzz + (probe.lambda * gz.z));
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
`,entry:"ocean_probe",workgroup:[64,1,1],bindings:[{group:0,binding:0,name:"probe",kind:"uniform",elementType:"OceanProbeParams"},{group:0,binding:1,name:"points",kind:"storage-read",elementType:"vec4<f32>"},{group:0,binding:2,name:"disp0",kind:"storage-read",elementType:"vec4<f32>"},{group:0,binding:3,name:"disp1",kind:"storage-read",elementType:"vec4<f32>"},{group:0,binding:4,name:"disp2",kind:"storage-read",elementType:"vec4<f32>"},{group:0,binding:5,name:"disp3",kind:"storage-read",elementType:"vec4<f32>"},{group:0,binding:6,name:"out",kind:"storage-rw",elementType:"vec4<f32>"}],uniforms:[{group:0,binding:0,name:"probe",struct:"OceanProbeParams",size:96,fields:[{name:"count",type:"u32",offset:0,size:4},{name:"cascades",type:"u32",offset:4,size:4},{name:"_pad0",type:"u32",offset:8,size:4},{name:"_pad1",type:"u32",offset:12,size:4},{name:"n0",type:"u32",offset:16,size:4},{name:"n1",type:"u32",offset:20,size:4},{name:"n2",type:"u32",offset:24,size:4},{name:"n3",type:"u32",offset:28,size:4},{name:"size0",type:"f32",offset:32,size:4},{name:"size1",type:"f32",offset:36,size:4},{name:"size2",type:"f32",offset:40,size:4},{name:"size3",type:"f32",offset:44,size:4},{name:"t",type:"f32",offset:48,size:4},{name:"normalStep",type:"f32",offset:52,size:4},{name:"lambda",type:"f32",offset:56,size:4},{name:"_pad3",type:"f32",offset:60,size:4},{name:"omX0",type:"f32",offset:64,size:4},{name:"omX1",type:"f32",offset:68,size:4},{name:"omX2",type:"f32",offset:72,size:4},{name:"omX3",type:"f32",offset:76,size:4},{name:"omZ0",type:"f32",offset:80,size:4},{name:"omZ1",type:"f32",offset:84,size:4},{name:"omZ2",type:"f32",offset:88,size:4},{name:"omZ3",type:"f32",offset:92,size:4}]}]},fftCompute64:{wgsl:`@group(0) @binding(0) var<storage, read_write> data : array<vec4<f32>>;

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
`,gatherLayout:{kernel:"fft_stage",inputs:[{name:"src",kind:"buffer",elem:"vec4",sampler:"src_tex",length:"src_len",binding:1}],outputs:[{name:"dst",elem:"vec4",format:"",location:0,variable:"dst_out"}],uniforms:[{name:"fft",block:"FFTStage_ubo",struct:"FFTStage",binding:0}]}},fftCompute128:{wgsl:`@group(0) @binding(0) var<storage, read_write> data : array<vec4<f32>>;

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
`,gatherLayout:{kernel:"fft_stage",inputs:[{name:"src",kind:"buffer",elem:"vec4",sampler:"src_tex",length:"src_len",binding:1}],outputs:[{name:"dst",elem:"vec4",format:"",location:0,variable:"dst_out"}],uniforms:[{name:"fft",block:"FFTStage_ubo",struct:"FFTStage",binding:0}]}},fftCompute256:{wgsl:`@group(0) @binding(0) var<storage, read_write> data : array<vec4<f32>>;

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
`,gatherLayout:{kernel:"fft_stage",inputs:[{name:"src",kind:"buffer",elem:"vec4",sampler:"src_tex",length:"src_len",binding:1}],outputs:[{name:"dst",elem:"vec4",format:"",location:0,variable:"dst_out"}],uniforms:[{name:"fft",block:"FFTStage_ubo",struct:"FFTStage",binding:0}]}}},graphs:{"webgpu/high":{name:"ocean",backend:"webgpu",tier:"high",version:1,params:[{name:"windSpeed",type:"f32",source:{kind:"value",value:9},tween:20},{name:"fetch",type:"f32",source:{kind:"value",value:300},tween:20},{name:"gamma",type:"f32",source:{kind:"value",value:3.3},tween:20},{name:"spread",type:"f32",source:{kind:"value",value:8},tween:20},{name:"swell",type:"f32",source:{kind:"value",value:0},tween:20},{name:"windDirDeg",type:"f32",source:{kind:"value",value:0},tween:20},{name:"swellDirDeg",type:"f32",source:{kind:"value",value:0},tween:20},{name:"seed",type:"u32",source:{kind:"value",value:0}},{name:"loopPeriod",type:"f32",source:{kind:"value",value:1e3}},{name:"lambda",type:"f32",source:{kind:"value",value:1},tween:20},{name:"timeScale",type:"f32",source:{kind:"value",value:1}},{name:"driftX",type:"f32",source:{kind:"value",value:0}},{name:"driftZ",type:"f32",source:{kind:"value",value:0}},{name:"foamTau",type:"f32",source:{kind:"value",value:4}},{name:"foamThreshold",type:"f32",source:{kind:"value",value:.9}},{name:"foamGain",type:"f32",source:{kind:"value",value:2}},{name:"probeCount",type:"u32",source:{kind:"value",value:0}},{name:"probeOriginX",type:"f32",source:{kind:"value",value:0}},{name:"probeOriginZ",type:"f32",source:{kind:"value",value:0}},{name:"probePoints",type:"vec4<f32>[]",count:64,source:{kind:"value",value:0}},{name:"dt",type:"f32",source:{kind:"frame",name:"dt"}},{name:"cameraX",type:"f32",source:{kind:"frame",name:"cameraX"}},{name:"cameraZ",type:"f32",source:{kind:"frame",name:"cameraZ"}},{name:"windDirRad",type:"f32",source:{args:["param:windDirDeg",.017453292519943295],kind:"expr",op:"mul"}},{name:"windDir",type:"f32",source:{args:[1.5707963267948966,"param:windDirRad"],kind:"expr",op:"sub"}},{name:"swellDirRad",type:"f32",source:{args:["param:swellDirDeg",.017453292519943295],kind:"expr",op:"mul"}},{name:"swellDir",type:"f32",source:{args:[1.5707963267948966,"param:swellDirRad"],kind:"expr",op:"sub"}},{name:"simTime",type:"f32",source:{kind:"accumulate",of:"param:timeScale"}},{name:"phaseTime",type:"f32",source:{args:["param:simTime","param:loopPeriod"],kind:"expr",op:"mod"}},{name:"waterX",type:"f32",source:{kind:"accumulate",of:"param:driftX"}},{name:"waterZ",type:"f32",source:{kind:"accumulate",of:"param:driftZ"}},{name:"waterModX0",type:"f32",source:{args:["param:waterX",1600],kind:"expr",op:"mod"}},{name:"waterModZ0",type:"f32",source:{args:["param:waterZ",1600],kind:"expr",op:"mod"}},{name:"waterModX1",type:"f32",source:{args:["param:waterX",250],kind:"expr",op:"mod"}},{name:"waterModZ1",type:"f32",source:{args:["param:waterZ",250],kind:"expr",op:"mod"}},{name:"waterModX2",type:"f32",source:{args:["param:waterX",37],kind:"expr",op:"mod"}},{name:"waterModZ2",type:"f32",source:{args:["param:waterZ",37],kind:"expr",op:"mod"}},{name:"waterModX3",type:"f32",source:{args:["param:waterX",5.3],kind:"expr",op:"mod"}},{name:"waterModZ3",type:"f32",source:{args:["param:waterZ",5.3],kind:"expr",op:"mod"}},{name:"foamRelX",type:"f32",source:{args:["param:cameraX","param:waterX"],kind:"expr",op:"sub"}},{name:"foamTexelX",type:"f32",source:{args:["param:foamRelX",.5],kind:"expr",op:"div"}},{name:"foamCellX",type:"f32",source:{args:["param:foamTexelX"],kind:"expr",op:"floor"}},{name:"foamOriginX",type:"i32",source:{args:["param:foamCellX",512],kind:"expr",op:"sub"}},{name:"foamShiftX",type:"i32",source:{kind:"delta",of:"param:foamOriginX"}},{name:"foamOriginMetersX",type:"f32",source:{args:["param:foamOriginX",.5],kind:"expr",op:"mul"}},{name:"foamOriginMod0X",type:"f32",source:{args:["param:foamOriginMetersX",1600],kind:"expr",op:"mod"}},{name:"foamOriginMod1X",type:"f32",source:{args:["param:foamOriginMetersX",250],kind:"expr",op:"mod"}},{name:"foamOriginMod2X",type:"f32",source:{args:["param:foamOriginMetersX",37],kind:"expr",op:"mod"}},{name:"foamOriginMod3X",type:"f32",source:{args:["param:foamOriginMetersX",5.3],kind:"expr",op:"mod"}},{name:"probeLocalX",type:"f32",source:{args:["param:probeOriginX","param:waterX"],kind:"expr",op:"sub"}},{name:"probeOriginMod0X",type:"f32",source:{args:["param:probeLocalX",1600],kind:"expr",op:"mod"}},{name:"probeOriginMod1X",type:"f32",source:{args:["param:probeLocalX",250],kind:"expr",op:"mod"}},{name:"probeOriginMod2X",type:"f32",source:{args:["param:probeLocalX",37],kind:"expr",op:"mod"}},{name:"probeOriginMod3X",type:"f32",source:{args:["param:probeLocalX",5.3],kind:"expr",op:"mod"}},{name:"foamRelZ",type:"f32",source:{args:["param:cameraZ","param:waterZ"],kind:"expr",op:"sub"}},{name:"foamTexelZ",type:"f32",source:{args:["param:foamRelZ",.5],kind:"expr",op:"div"}},{name:"foamCellZ",type:"f32",source:{args:["param:foamTexelZ"],kind:"expr",op:"floor"}},{name:"foamOriginZ",type:"i32",source:{args:["param:foamCellZ",512],kind:"expr",op:"sub"}},{name:"foamShiftZ",type:"i32",source:{kind:"delta",of:"param:foamOriginZ"}},{name:"foamOriginMetersZ",type:"f32",source:{args:["param:foamOriginZ",.5],kind:"expr",op:"mul"}},{name:"foamOriginMod0Z",type:"f32",source:{args:["param:foamOriginMetersZ",1600],kind:"expr",op:"mod"}},{name:"foamOriginMod1Z",type:"f32",source:{args:["param:foamOriginMetersZ",250],kind:"expr",op:"mod"}},{name:"foamOriginMod2Z",type:"f32",source:{args:["param:foamOriginMetersZ",37],kind:"expr",op:"mod"}},{name:"foamOriginMod3Z",type:"f32",source:{args:["param:foamOriginMetersZ",5.3],kind:"expr",op:"mod"}},{name:"probeLocalZ",type:"f32",source:{args:["param:probeOriginZ","param:waterZ"],kind:"expr",op:"sub"}},{name:"probeOriginMod0Z",type:"f32",source:{args:["param:probeLocalZ",1600],kind:"expr",op:"mod"}},{name:"probeOriginMod1Z",type:"f32",source:{args:["param:probeLocalZ",250],kind:"expr",op:"mod"}},{name:"probeOriginMod2Z",type:"f32",source:{args:["param:probeLocalZ",37],kind:"expr",op:"mod"}},{name:"probeOriginMod3Z",type:"f32",source:{args:["param:probeLocalZ",5.3],kind:"expr",op:"mod"}}],resources:[{name:"h0_0",kind:"buffer",elementType:"vec4<f32>",count:4096},{name:"omega_0",kind:"buffer",elementType:"f32",count:4096},{name:"fftA_0",kind:"buffer",elementType:"vec4<f32>",count:4096},{name:"fftB_0",kind:"buffer",elementType:"vec4<f32>",count:4096},{name:"dispTex0",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"slopeTex0",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"h0_1",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"omega_1",kind:"buffer",elementType:"f32",count:65536},{name:"fftA_1",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"fftB_1",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"dispTex1",kind:"texture",format:"rgba16float",width:256,height:256,mipLevels:9},{name:"slopeTex1",kind:"texture",format:"rgba16float",width:256,height:256,mipLevels:9},{name:"h0_2",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"omega_2",kind:"buffer",elementType:"f32",count:65536},{name:"fftA_2",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"fftB_2",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"dispTex2",kind:"texture",format:"rgba16float",width:256,height:256,mipLevels:9},{name:"slopeTex2",kind:"texture",format:"rgba16float",width:256,height:256,mipLevels:9},{name:"h0_3",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"omega_3",kind:"buffer",elementType:"f32",count:65536},{name:"fftA_3",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"fftB_3",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"dispTex3",kind:"texture",format:"rgba16float",width:256,height:256,mipLevels:9},{name:"slopeTex3",kind:"texture",format:"rgba16float",width:256,height:256,mipLevels:9},{name:"foamA",kind:"buffer",elementType:"f32",count:1048576},{name:"foamB",kind:"buffer",elementType:"f32",count:1048576},{name:"foamTex",kind:"texture",format:"rgba16float",width:1024,height:1024,mipLevels:11},{name:"probePoints",kind:"buffer",elementType:"vec4<f32>",count:64,feed:"probePoints"},{name:"probeOut",kind:"buffer",elementType:"vec4<f32>",count:64}],passes:[{name:"c0.spectrum",type:"kernel",kernel:"ocean_spectrum",entry:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",dispatch:[64,1,1],bindings:[{name:"h0",resource:"h0_0"},{name:"omega",resource:"omega_0"}],uniforms:[{name:"spec",values:{cascade:0,fetchKm:"param:fetch",gamma:"param:gamma",kMax:.12,kMin:.003926990816987242,loopPeriod:"param:loopPeriod",n:64,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:1600,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c0.evolve",type:"kernel",kernel:"ocean_evolve",entry:"ocean_evolve",dispatch:[64,1,1],bindings:[{name:"h0",resource:"h0_0"},{name:"omega",resource:"omega_0"},{name:"outA",resource:"fftA_0"},{name:"outB",resource:"fftB_0"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:64,t:"param:phaseTime",tile:1600}}]},{name:"c0.fftA.fft_rows",type:"kernel",kernel:"fft_compute_64",entry:"fft_rows",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftA_0"}]},{name:"c0.fftA.fft_cols",type:"kernel",kernel:"fft_compute_64",entry:"fft_cols",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftA_0"}]},{name:"c0.fftB.fft_rows",type:"kernel",kernel:"fft_compute_64",entry:"fft_rows",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftB_0"}]},{name:"c0.fftB.fft_cols",type:"kernel",kernel:"fft_compute_64",entry:"fft_cols",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftB_0"}]},{name:"c0.assemble",type:"kernel",kernel:"ocean_assemble",entry:"ocean_assemble",dispatch:[64,1,1],bindings:[{name:"fieldA",resource:"fftA_0"},{name:"fieldB",resource:"fftB_0"},{name:"disp",resource:"dispTex0"},{name:"slope",resource:"slopeTex0"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:64}}]},{name:"c0.dispTex0.mips",type:"repeat",count:6,index:"l",body:[{name:"mip",type:"kernel",kernel:"ocean_mip",entry:"ocean_mip",dispatch:[16,1,1],bindings:[{name:"src",resource:"dispTex0",levelAt:{index:"l"}},{name:"dst",resource:"dispTex0",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:64},dstWidth:{index:"l",offset:1,shr:64},pad:0,srcLevel:0}}]}]},{name:"c0.slopeTex0.mips",type:"repeat",count:6,index:"l",body:[{name:"mip",type:"kernel",kernel:"ocean_mip",entry:"ocean_mip",dispatch:[16,1,1],bindings:[{name:"src",resource:"slopeTex0",levelAt:{index:"l"}},{name:"dst",resource:"slopeTex0",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:64},dstWidth:{index:"l",offset:1,shr:64},pad:0,srcLevel:0}}]}]},{name:"c1.spectrum",type:"kernel",kernel:"ocean_spectrum",entry:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",dispatch:[1024,1,1],bindings:[{name:"h0",resource:"h0_1"},{name:"omega",resource:"omega_1"}],uniforms:[{name:"spec",values:{cascade:1,fetchKm:"param:fetch",gamma:"param:gamma",kMax:.8,kMin:.12,loopPeriod:"param:loopPeriod",n:256,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:250,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c1.evolve",type:"kernel",kernel:"ocean_evolve",entry:"ocean_evolve",dispatch:[1024,1,1],bindings:[{name:"h0",resource:"h0_1"},{name:"omega",resource:"omega_1"},{name:"outA",resource:"fftA_1"},{name:"outB",resource:"fftB_1"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:256,t:"param:phaseTime",tile:250}}]},{name:"c1.fftA.fft_rows",type:"kernel",kernel:"fft_compute_256",entry:"fft_rows",dispatch:[256,1,1],bindings:[{name:"data",resource:"fftA_1"}]},{name:"c1.fftA.fft_cols",type:"kernel",kernel:"fft_compute_256",entry:"fft_cols",dispatch:[256,1,1],bindings:[{name:"data",resource:"fftA_1"}]},{name:"c1.fftB.fft_rows",type:"kernel",kernel:"fft_compute_256",entry:"fft_rows",dispatch:[256,1,1],bindings:[{name:"data",resource:"fftB_1"}]},{name:"c1.fftB.fft_cols",type:"kernel",kernel:"fft_compute_256",entry:"fft_cols",dispatch:[256,1,1],bindings:[{name:"data",resource:"fftB_1"}]},{name:"c1.assemble",type:"kernel",kernel:"ocean_assemble",entry:"ocean_assemble",dispatch:[1024,1,1],bindings:[{name:"fieldA",resource:"fftA_1"},{name:"fieldB",resource:"fftB_1"},{name:"disp",resource:"dispTex1"},{name:"slope",resource:"slopeTex1"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:256}}]},{name:"c1.dispTex1.mips",type:"repeat",count:8,index:"l",body:[{name:"mip",type:"kernel",kernel:"ocean_mip",entry:"ocean_mip",dispatch:[256,1,1],bindings:[{name:"src",resource:"dispTex1",levelAt:{index:"l"}},{name:"dst",resource:"dispTex1",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:256},dstWidth:{index:"l",offset:1,shr:256},pad:0,srcLevel:0}}]}]},{name:"c1.slopeTex1.mips",type:"repeat",count:8,index:"l",body:[{name:"mip",type:"kernel",kernel:"ocean_mip",entry:"ocean_mip",dispatch:[256,1,1],bindings:[{name:"src",resource:"slopeTex1",levelAt:{index:"l"}},{name:"dst",resource:"slopeTex1",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:256},dstWidth:{index:"l",offset:1,shr:256},pad:0,srcLevel:0}}]}]},{name:"c2.spectrum",type:"kernel",kernel:"ocean_spectrum",entry:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",dispatch:[1024,1,1],bindings:[{name:"h0",resource:"h0_2"},{name:"omega",resource:"omega_2"}],uniforms:[{name:"spec",values:{cascade:2,fetchKm:"param:fetch",gamma:"param:gamma",kMax:5.4,kMin:.8,loopPeriod:"param:loopPeriod",n:256,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:37,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c2.evolve",type:"kernel",kernel:"ocean_evolve",entry:"ocean_evolve",dispatch:[1024,1,1],bindings:[{name:"h0",resource:"h0_2"},{name:"omega",resource:"omega_2"},{name:"outA",resource:"fftA_2"},{name:"outB",resource:"fftB_2"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:256,t:"param:phaseTime",tile:37}}]},{name:"c2.fftA.fft_rows",type:"kernel",kernel:"fft_compute_256",entry:"fft_rows",dispatch:[256,1,1],bindings:[{name:"data",resource:"fftA_2"}]},{name:"c2.fftA.fft_cols",type:"kernel",kernel:"fft_compute_256",entry:"fft_cols",dispatch:[256,1,1],bindings:[{name:"data",resource:"fftA_2"}]},{name:"c2.fftB.fft_rows",type:"kernel",kernel:"fft_compute_256",entry:"fft_rows",dispatch:[256,1,1],bindings:[{name:"data",resource:"fftB_2"}]},{name:"c2.fftB.fft_cols",type:"kernel",kernel:"fft_compute_256",entry:"fft_cols",dispatch:[256,1,1],bindings:[{name:"data",resource:"fftB_2"}]},{name:"c2.assemble",type:"kernel",kernel:"ocean_assemble",entry:"ocean_assemble",dispatch:[1024,1,1],bindings:[{name:"fieldA",resource:"fftA_2"},{name:"fieldB",resource:"fftB_2"},{name:"disp",resource:"dispTex2"},{name:"slope",resource:"slopeTex2"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:256}}]},{name:"c2.dispTex2.mips",type:"repeat",count:8,index:"l",body:[{name:"mip",type:"kernel",kernel:"ocean_mip",entry:"ocean_mip",dispatch:[256,1,1],bindings:[{name:"src",resource:"dispTex2",levelAt:{index:"l"}},{name:"dst",resource:"dispTex2",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:256},dstWidth:{index:"l",offset:1,shr:256},pad:0,srcLevel:0}}]}]},{name:"c2.slopeTex2.mips",type:"repeat",count:8,index:"l",body:[{name:"mip",type:"kernel",kernel:"ocean_mip",entry:"ocean_mip",dispatch:[256,1,1],bindings:[{name:"src",resource:"slopeTex2",levelAt:{index:"l"}},{name:"dst",resource:"slopeTex2",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:256},dstWidth:{index:"l",offset:1,shr:256},pad:0,srcLevel:0}}]}]},{name:"c3.spectrum",type:"kernel",kernel:"ocean_spectrum",entry:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",dispatch:[1024,1,1],bindings:[{name:"h0",resource:"h0_3"},{name:"omega",resource:"omega_3"}],uniforms:[{name:"spec",values:{cascade:3,fetchKm:"param:fetch",gamma:"param:gamma",kMax:37,kMin:5.4,loopPeriod:"param:loopPeriod",n:256,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:5.3,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c3.evolve",type:"kernel",kernel:"ocean_evolve",entry:"ocean_evolve",dispatch:[1024,1,1],bindings:[{name:"h0",resource:"h0_3"},{name:"omega",resource:"omega_3"},{name:"outA",resource:"fftA_3"},{name:"outB",resource:"fftB_3"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:256,t:"param:phaseTime",tile:5.3}}]},{name:"c3.fftA.fft_rows",type:"kernel",kernel:"fft_compute_256",entry:"fft_rows",dispatch:[256,1,1],bindings:[{name:"data",resource:"fftA_3"}]},{name:"c3.fftA.fft_cols",type:"kernel",kernel:"fft_compute_256",entry:"fft_cols",dispatch:[256,1,1],bindings:[{name:"data",resource:"fftA_3"}]},{name:"c3.fftB.fft_rows",type:"kernel",kernel:"fft_compute_256",entry:"fft_rows",dispatch:[256,1,1],bindings:[{name:"data",resource:"fftB_3"}]},{name:"c3.fftB.fft_cols",type:"kernel",kernel:"fft_compute_256",entry:"fft_cols",dispatch:[256,1,1],bindings:[{name:"data",resource:"fftB_3"}]},{name:"c3.assemble",type:"kernel",kernel:"ocean_assemble",entry:"ocean_assemble",dispatch:[1024,1,1],bindings:[{name:"fieldA",resource:"fftA_3"},{name:"fieldB",resource:"fftB_3"},{name:"disp",resource:"dispTex3"},{name:"slope",resource:"slopeTex3"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:256}}]},{name:"c3.dispTex3.mips",type:"repeat",count:8,index:"l",body:[{name:"mip",type:"kernel",kernel:"ocean_mip",entry:"ocean_mip",dispatch:[256,1,1],bindings:[{name:"src",resource:"dispTex3",levelAt:{index:"l"}},{name:"dst",resource:"dispTex3",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:256},dstWidth:{index:"l",offset:1,shr:256},pad:0,srcLevel:0}}]}]},{name:"c3.slopeTex3.mips",type:"repeat",count:8,index:"l",body:[{name:"mip",type:"kernel",kernel:"ocean_mip",entry:"ocean_mip",dispatch:[256,1,1],bindings:[{name:"src",resource:"slopeTex3",levelAt:{index:"l"}},{name:"dst",resource:"slopeTex3",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:256},dstWidth:{index:"l",offset:1,shr:256},pad:0,srcLevel:0}}]}]},{name:"foam",type:"kernel",kernel:"ocean_foam",entry:"ocean_foam",dispatch:[16384,1,1],bindings:[{name:"foamIn",resource:"foamA"},{name:"fieldB0",resource:"fftB_0"},{name:"fieldB1",resource:"fftB_1"},{name:"fieldB2",resource:"fftB_2"},{name:"fieldB3",resource:"fftB_3"},{name:"foamOut",resource:"foamB"},{name:"foamTex",resource:"foamTex"}],uniforms:[{name:"foam",values:{cascades:4,cell:.5,dt:"param:dt",gain:"param:foamGain",lambda:"param:lambda",n0:64,n1:256,n2:256,n3:256,omX0:"param:foamOriginMod0X",omX1:"param:foamOriginMod1X",omX2:"param:foamOriginMod2X",omX3:"param:foamOriginMod3X",omZ0:"param:foamOriginMod0Z",omZ1:"param:foamOriginMod1Z",omZ2:"param:foamOriginMod2Z",omZ3:"param:foamOriginMod3Z",originX:"param:foamOriginX",originZ:"param:foamOriginZ",shiftX:"param:foamShiftX",shiftZ:"param:foamShiftZ",size0:1600,size1:250,size2:37,size3:5.3,tau:"param:foamTau",threshold:"param:foamThreshold",width:1024}}]},{name:"foamTex.mips",type:"repeat",count:10,index:"l",body:[{name:"mip",type:"kernel",kernel:"ocean_mip",entry:"ocean_mip",dispatch:[4096,1,1],bindings:[{name:"src",resource:"foamTex",levelAt:{index:"l"}},{name:"dst",resource:"foamTex",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:1024},dstWidth:{index:"l",offset:1,shr:1024},pad:0,srcLevel:0}}]}]},{name:"probe",type:"kernel",kernel:"ocean_probe",entry:"ocean_probe",dispatch:[1,1,1],bindings:[{name:"points",resource:"probePoints"},{name:"disp0",resource:"fftA_0"},{name:"disp1",resource:"fftA_1"},{name:"disp2",resource:"fftA_2"},{name:"disp3",resource:"fftA_3"},{name:"out",resource:"probeOut"}],uniforms:[{name:"probe",values:{cascades:4,count:"param:probeCount",lambda:"param:lambda",n0:64,n1:256,n2:256,n3:256,normalStep:.5,omX0:"param:probeOriginMod0X",omX1:"param:probeOriginMod1X",omX2:"param:probeOriginMod2X",omX3:"param:probeOriginMod3X",omZ0:"param:probeOriginMod0Z",omZ1:"param:probeOriginMod1Z",omZ2:"param:probeOriginMod2Z",omZ3:"param:probeOriginMod3Z",size0:1600,size1:250,size2:37,size3:5.3,t:"param:simTime"}}]}],frameSwaps:[["foamA","foamB"]],exports:{disp0:"dispTex0",disp1:"dispTex1",disp2:"dispTex2",disp3:"dispTex3",foam:"foamTex",probe:"probeOut",slope0:"slopeTex0",slope1:"slopeTex1",slope2:"slopeTex2",slope3:"slopeTex3"},exportParams:["waterModX0","waterModZ0","waterModX1","waterModZ1","waterModX2","waterModZ2","waterModX3","waterModZ3"]},"webgpu/medium":{name:"ocean",backend:"webgpu",tier:"medium",version:1,params:[{name:"windSpeed",type:"f32",source:{kind:"value",value:9},tween:20},{name:"fetch",type:"f32",source:{kind:"value",value:300},tween:20},{name:"gamma",type:"f32",source:{kind:"value",value:3.3},tween:20},{name:"spread",type:"f32",source:{kind:"value",value:8},tween:20},{name:"swell",type:"f32",source:{kind:"value",value:0},tween:20},{name:"windDirDeg",type:"f32",source:{kind:"value",value:0},tween:20},{name:"swellDirDeg",type:"f32",source:{kind:"value",value:0},tween:20},{name:"seed",type:"u32",source:{kind:"value",value:0}},{name:"loopPeriod",type:"f32",source:{kind:"value",value:1e3}},{name:"lambda",type:"f32",source:{kind:"value",value:1},tween:20},{name:"timeScale",type:"f32",source:{kind:"value",value:1}},{name:"driftX",type:"f32",source:{kind:"value",value:0}},{name:"driftZ",type:"f32",source:{kind:"value",value:0}},{name:"foamTau",type:"f32",source:{kind:"value",value:4}},{name:"foamThreshold",type:"f32",source:{kind:"value",value:.9}},{name:"foamGain",type:"f32",source:{kind:"value",value:2}},{name:"probeCount",type:"u32",source:{kind:"value",value:0}},{name:"probeOriginX",type:"f32",source:{kind:"value",value:0}},{name:"probeOriginZ",type:"f32",source:{kind:"value",value:0}},{name:"probePoints",type:"vec4<f32>[]",count:64,source:{kind:"value",value:0}},{name:"dt",type:"f32",source:{kind:"frame",name:"dt"}},{name:"cameraX",type:"f32",source:{kind:"frame",name:"cameraX"}},{name:"cameraZ",type:"f32",source:{kind:"frame",name:"cameraZ"}},{name:"windDirRad",type:"f32",source:{args:["param:windDirDeg",.017453292519943295],kind:"expr",op:"mul"}},{name:"windDir",type:"f32",source:{args:[1.5707963267948966,"param:windDirRad"],kind:"expr",op:"sub"}},{name:"swellDirRad",type:"f32",source:{args:["param:swellDirDeg",.017453292519943295],kind:"expr",op:"mul"}},{name:"swellDir",type:"f32",source:{args:[1.5707963267948966,"param:swellDirRad"],kind:"expr",op:"sub"}},{name:"simTime",type:"f32",source:{kind:"accumulate",of:"param:timeScale"}},{name:"phaseTime",type:"f32",source:{args:["param:simTime","param:loopPeriod"],kind:"expr",op:"mod"}},{name:"waterX",type:"f32",source:{kind:"accumulate",of:"param:driftX"}},{name:"waterZ",type:"f32",source:{kind:"accumulate",of:"param:driftZ"}},{name:"waterModX0",type:"f32",source:{args:["param:waterX",1600],kind:"expr",op:"mod"}},{name:"waterModZ0",type:"f32",source:{args:["param:waterZ",1600],kind:"expr",op:"mod"}},{name:"waterModX1",type:"f32",source:{args:["param:waterX",250],kind:"expr",op:"mod"}},{name:"waterModZ1",type:"f32",source:{args:["param:waterZ",250],kind:"expr",op:"mod"}},{name:"waterModX2",type:"f32",source:{args:["param:waterX",37],kind:"expr",op:"mod"}},{name:"waterModZ2",type:"f32",source:{args:["param:waterZ",37],kind:"expr",op:"mod"}},{name:"waterModX3",type:"f32",source:{args:["param:waterX",5.3],kind:"expr",op:"mod"}},{name:"waterModZ3",type:"f32",source:{args:["param:waterZ",5.3],kind:"expr",op:"mod"}},{name:"foamRelX",type:"f32",source:{args:["param:cameraX","param:waterX"],kind:"expr",op:"sub"}},{name:"foamTexelX",type:"f32",source:{args:["param:foamRelX",.5],kind:"expr",op:"div"}},{name:"foamCellX",type:"f32",source:{args:["param:foamTexelX"],kind:"expr",op:"floor"}},{name:"foamOriginX",type:"i32",source:{args:["param:foamCellX",512],kind:"expr",op:"sub"}},{name:"foamShiftX",type:"i32",source:{kind:"delta",of:"param:foamOriginX"}},{name:"foamOriginMetersX",type:"f32",source:{args:["param:foamOriginX",.5],kind:"expr",op:"mul"}},{name:"foamOriginMod0X",type:"f32",source:{args:["param:foamOriginMetersX",1600],kind:"expr",op:"mod"}},{name:"foamOriginMod1X",type:"f32",source:{args:["param:foamOriginMetersX",250],kind:"expr",op:"mod"}},{name:"foamOriginMod2X",type:"f32",source:{args:["param:foamOriginMetersX",37],kind:"expr",op:"mod"}},{name:"foamOriginMod3X",type:"f32",source:{args:["param:foamOriginMetersX",5.3],kind:"expr",op:"mod"}},{name:"probeLocalX",type:"f32",source:{args:["param:probeOriginX","param:waterX"],kind:"expr",op:"sub"}},{name:"probeOriginMod0X",type:"f32",source:{args:["param:probeLocalX",1600],kind:"expr",op:"mod"}},{name:"probeOriginMod1X",type:"f32",source:{args:["param:probeLocalX",250],kind:"expr",op:"mod"}},{name:"probeOriginMod2X",type:"f32",source:{args:["param:probeLocalX",37],kind:"expr",op:"mod"}},{name:"probeOriginMod3X",type:"f32",source:{args:["param:probeLocalX",5.3],kind:"expr",op:"mod"}},{name:"foamRelZ",type:"f32",source:{args:["param:cameraZ","param:waterZ"],kind:"expr",op:"sub"}},{name:"foamTexelZ",type:"f32",source:{args:["param:foamRelZ",.5],kind:"expr",op:"div"}},{name:"foamCellZ",type:"f32",source:{args:["param:foamTexelZ"],kind:"expr",op:"floor"}},{name:"foamOriginZ",type:"i32",source:{args:["param:foamCellZ",512],kind:"expr",op:"sub"}},{name:"foamShiftZ",type:"i32",source:{kind:"delta",of:"param:foamOriginZ"}},{name:"foamOriginMetersZ",type:"f32",source:{args:["param:foamOriginZ",.5],kind:"expr",op:"mul"}},{name:"foamOriginMod0Z",type:"f32",source:{args:["param:foamOriginMetersZ",1600],kind:"expr",op:"mod"}},{name:"foamOriginMod1Z",type:"f32",source:{args:["param:foamOriginMetersZ",250],kind:"expr",op:"mod"}},{name:"foamOriginMod2Z",type:"f32",source:{args:["param:foamOriginMetersZ",37],kind:"expr",op:"mod"}},{name:"foamOriginMod3Z",type:"f32",source:{args:["param:foamOriginMetersZ",5.3],kind:"expr",op:"mod"}},{name:"probeLocalZ",type:"f32",source:{args:["param:probeOriginZ","param:waterZ"],kind:"expr",op:"sub"}},{name:"probeOriginMod0Z",type:"f32",source:{args:["param:probeLocalZ",1600],kind:"expr",op:"mod"}},{name:"probeOriginMod1Z",type:"f32",source:{args:["param:probeLocalZ",250],kind:"expr",op:"mod"}},{name:"probeOriginMod2Z",type:"f32",source:{args:["param:probeLocalZ",37],kind:"expr",op:"mod"}},{name:"probeOriginMod3Z",type:"f32",source:{args:["param:probeLocalZ",5.3],kind:"expr",op:"mod"}}],resources:[{name:"h0_0",kind:"buffer",elementType:"vec4<f32>",count:4096},{name:"omega_0",kind:"buffer",elementType:"f32",count:4096},{name:"fftA_0",kind:"buffer",elementType:"vec4<f32>",count:4096},{name:"fftB_0",kind:"buffer",elementType:"vec4<f32>",count:4096},{name:"dispTex0",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"slopeTex0",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"h0_1",kind:"buffer",elementType:"vec4<f32>",count:16384},{name:"omega_1",kind:"buffer",elementType:"f32",count:16384},{name:"fftA_1",kind:"buffer",elementType:"vec4<f32>",count:16384},{name:"fftB_1",kind:"buffer",elementType:"vec4<f32>",count:16384},{name:"dispTex1",kind:"texture",format:"rgba16float",width:128,height:128,mipLevels:8},{name:"slopeTex1",kind:"texture",format:"rgba16float",width:128,height:128,mipLevels:8},{name:"h0_2",kind:"buffer",elementType:"vec4<f32>",count:16384},{name:"omega_2",kind:"buffer",elementType:"f32",count:16384},{name:"fftA_2",kind:"buffer",elementType:"vec4<f32>",count:16384},{name:"fftB_2",kind:"buffer",elementType:"vec4<f32>",count:16384},{name:"dispTex2",kind:"texture",format:"rgba16float",width:128,height:128,mipLevels:8},{name:"slopeTex2",kind:"texture",format:"rgba16float",width:128,height:128,mipLevels:8},{name:"h0_3",kind:"buffer",elementType:"vec4<f32>",count:16384},{name:"omega_3",kind:"buffer",elementType:"f32",count:16384},{name:"fftA_3",kind:"buffer",elementType:"vec4<f32>",count:16384},{name:"fftB_3",kind:"buffer",elementType:"vec4<f32>",count:16384},{name:"dispTex3",kind:"texture",format:"rgba16float",width:128,height:128,mipLevels:8},{name:"slopeTex3",kind:"texture",format:"rgba16float",width:128,height:128,mipLevels:8},{name:"foamA",kind:"buffer",elementType:"f32",count:1048576},{name:"foamB",kind:"buffer",elementType:"f32",count:1048576},{name:"foamTex",kind:"texture",format:"rgba16float",width:1024,height:1024,mipLevels:11},{name:"probePoints",kind:"buffer",elementType:"vec4<f32>",count:64,feed:"probePoints"},{name:"probeOut",kind:"buffer",elementType:"vec4<f32>",count:64}],passes:[{name:"c0.spectrum",type:"kernel",kernel:"ocean_spectrum",entry:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",dispatch:[64,1,1],bindings:[{name:"h0",resource:"h0_0"},{name:"omega",resource:"omega_0"}],uniforms:[{name:"spec",values:{cascade:0,fetchKm:"param:fetch",gamma:"param:gamma",kMax:.12,kMin:.003926990816987242,loopPeriod:"param:loopPeriod",n:64,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:1600,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c0.evolve",type:"kernel",kernel:"ocean_evolve",entry:"ocean_evolve",dispatch:[64,1,1],bindings:[{name:"h0",resource:"h0_0"},{name:"omega",resource:"omega_0"},{name:"outA",resource:"fftA_0"},{name:"outB",resource:"fftB_0"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:64,t:"param:phaseTime",tile:1600}}]},{name:"c0.fftA.fft_rows",type:"kernel",kernel:"fft_compute_64",entry:"fft_rows",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftA_0"}]},{name:"c0.fftA.fft_cols",type:"kernel",kernel:"fft_compute_64",entry:"fft_cols",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftA_0"}]},{name:"c0.fftB.fft_rows",type:"kernel",kernel:"fft_compute_64",entry:"fft_rows",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftB_0"}]},{name:"c0.fftB.fft_cols",type:"kernel",kernel:"fft_compute_64",entry:"fft_cols",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftB_0"}]},{name:"c0.assemble",type:"kernel",kernel:"ocean_assemble",entry:"ocean_assemble",dispatch:[64,1,1],bindings:[{name:"fieldA",resource:"fftA_0"},{name:"fieldB",resource:"fftB_0"},{name:"disp",resource:"dispTex0"},{name:"slope",resource:"slopeTex0"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:64}}]},{name:"c0.dispTex0.mips",type:"repeat",count:6,index:"l",body:[{name:"mip",type:"kernel",kernel:"ocean_mip",entry:"ocean_mip",dispatch:[16,1,1],bindings:[{name:"src",resource:"dispTex0",levelAt:{index:"l"}},{name:"dst",resource:"dispTex0",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:64},dstWidth:{index:"l",offset:1,shr:64},pad:0,srcLevel:0}}]}]},{name:"c0.slopeTex0.mips",type:"repeat",count:6,index:"l",body:[{name:"mip",type:"kernel",kernel:"ocean_mip",entry:"ocean_mip",dispatch:[16,1,1],bindings:[{name:"src",resource:"slopeTex0",levelAt:{index:"l"}},{name:"dst",resource:"slopeTex0",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:64},dstWidth:{index:"l",offset:1,shr:64},pad:0,srcLevel:0}}]}]},{name:"c1.spectrum",type:"kernel",kernel:"ocean_spectrum",entry:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",dispatch:[256,1,1],bindings:[{name:"h0",resource:"h0_1"},{name:"omega",resource:"omega_1"}],uniforms:[{name:"spec",values:{cascade:1,fetchKm:"param:fetch",gamma:"param:gamma",kMax:.8,kMin:.12,loopPeriod:"param:loopPeriod",n:128,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:250,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c1.evolve",type:"kernel",kernel:"ocean_evolve",entry:"ocean_evolve",dispatch:[256,1,1],bindings:[{name:"h0",resource:"h0_1"},{name:"omega",resource:"omega_1"},{name:"outA",resource:"fftA_1"},{name:"outB",resource:"fftB_1"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:128,t:"param:phaseTime",tile:250}}]},{name:"c1.fftA.fft_rows",type:"kernel",kernel:"fft_compute_128",entry:"fft_rows",dispatch:[128,1,1],bindings:[{name:"data",resource:"fftA_1"}]},{name:"c1.fftA.fft_cols",type:"kernel",kernel:"fft_compute_128",entry:"fft_cols",dispatch:[128,1,1],bindings:[{name:"data",resource:"fftA_1"}]},{name:"c1.fftB.fft_rows",type:"kernel",kernel:"fft_compute_128",entry:"fft_rows",dispatch:[128,1,1],bindings:[{name:"data",resource:"fftB_1"}]},{name:"c1.fftB.fft_cols",type:"kernel",kernel:"fft_compute_128",entry:"fft_cols",dispatch:[128,1,1],bindings:[{name:"data",resource:"fftB_1"}]},{name:"c1.assemble",type:"kernel",kernel:"ocean_assemble",entry:"ocean_assemble",dispatch:[256,1,1],bindings:[{name:"fieldA",resource:"fftA_1"},{name:"fieldB",resource:"fftB_1"},{name:"disp",resource:"dispTex1"},{name:"slope",resource:"slopeTex1"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:128}}]},{name:"c1.dispTex1.mips",type:"repeat",count:7,index:"l",body:[{name:"mip",type:"kernel",kernel:"ocean_mip",entry:"ocean_mip",dispatch:[64,1,1],bindings:[{name:"src",resource:"dispTex1",levelAt:{index:"l"}},{name:"dst",resource:"dispTex1",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:128},dstWidth:{index:"l",offset:1,shr:128},pad:0,srcLevel:0}}]}]},{name:"c1.slopeTex1.mips",type:"repeat",count:7,index:"l",body:[{name:"mip",type:"kernel",kernel:"ocean_mip",entry:"ocean_mip",dispatch:[64,1,1],bindings:[{name:"src",resource:"slopeTex1",levelAt:{index:"l"}},{name:"dst",resource:"slopeTex1",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:128},dstWidth:{index:"l",offset:1,shr:128},pad:0,srcLevel:0}}]}]},{name:"c2.spectrum",type:"kernel",kernel:"ocean_spectrum",entry:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",dispatch:[256,1,1],bindings:[{name:"h0",resource:"h0_2"},{name:"omega",resource:"omega_2"}],uniforms:[{name:"spec",values:{cascade:2,fetchKm:"param:fetch",gamma:"param:gamma",kMax:5.4,kMin:.8,loopPeriod:"param:loopPeriod",n:128,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:37,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c2.evolve",type:"kernel",kernel:"ocean_evolve",entry:"ocean_evolve",dispatch:[256,1,1],bindings:[{name:"h0",resource:"h0_2"},{name:"omega",resource:"omega_2"},{name:"outA",resource:"fftA_2"},{name:"outB",resource:"fftB_2"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:128,t:"param:phaseTime",tile:37}}]},{name:"c2.fftA.fft_rows",type:"kernel",kernel:"fft_compute_128",entry:"fft_rows",dispatch:[128,1,1],bindings:[{name:"data",resource:"fftA_2"}]},{name:"c2.fftA.fft_cols",type:"kernel",kernel:"fft_compute_128",entry:"fft_cols",dispatch:[128,1,1],bindings:[{name:"data",resource:"fftA_2"}]},{name:"c2.fftB.fft_rows",type:"kernel",kernel:"fft_compute_128",entry:"fft_rows",dispatch:[128,1,1],bindings:[{name:"data",resource:"fftB_2"}]},{name:"c2.fftB.fft_cols",type:"kernel",kernel:"fft_compute_128",entry:"fft_cols",dispatch:[128,1,1],bindings:[{name:"data",resource:"fftB_2"}]},{name:"c2.assemble",type:"kernel",kernel:"ocean_assemble",entry:"ocean_assemble",dispatch:[256,1,1],bindings:[{name:"fieldA",resource:"fftA_2"},{name:"fieldB",resource:"fftB_2"},{name:"disp",resource:"dispTex2"},{name:"slope",resource:"slopeTex2"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:128}}]},{name:"c2.dispTex2.mips",type:"repeat",count:7,index:"l",body:[{name:"mip",type:"kernel",kernel:"ocean_mip",entry:"ocean_mip",dispatch:[64,1,1],bindings:[{name:"src",resource:"dispTex2",levelAt:{index:"l"}},{name:"dst",resource:"dispTex2",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:128},dstWidth:{index:"l",offset:1,shr:128},pad:0,srcLevel:0}}]}]},{name:"c2.slopeTex2.mips",type:"repeat",count:7,index:"l",body:[{name:"mip",type:"kernel",kernel:"ocean_mip",entry:"ocean_mip",dispatch:[64,1,1],bindings:[{name:"src",resource:"slopeTex2",levelAt:{index:"l"}},{name:"dst",resource:"slopeTex2",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:128},dstWidth:{index:"l",offset:1,shr:128},pad:0,srcLevel:0}}]}]},{name:"c3.spectrum",type:"kernel",kernel:"ocean_spectrum",entry:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",dispatch:[256,1,1],bindings:[{name:"h0",resource:"h0_3"},{name:"omega",resource:"omega_3"}],uniforms:[{name:"spec",values:{cascade:3,fetchKm:"param:fetch",gamma:"param:gamma",kMax:37,kMin:5.4,loopPeriod:"param:loopPeriod",n:128,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:5.3,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c3.evolve",type:"kernel",kernel:"ocean_evolve",entry:"ocean_evolve",dispatch:[256,1,1],bindings:[{name:"h0",resource:"h0_3"},{name:"omega",resource:"omega_3"},{name:"outA",resource:"fftA_3"},{name:"outB",resource:"fftB_3"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:128,t:"param:phaseTime",tile:5.3}}]},{name:"c3.fftA.fft_rows",type:"kernel",kernel:"fft_compute_128",entry:"fft_rows",dispatch:[128,1,1],bindings:[{name:"data",resource:"fftA_3"}]},{name:"c3.fftA.fft_cols",type:"kernel",kernel:"fft_compute_128",entry:"fft_cols",dispatch:[128,1,1],bindings:[{name:"data",resource:"fftA_3"}]},{name:"c3.fftB.fft_rows",type:"kernel",kernel:"fft_compute_128",entry:"fft_rows",dispatch:[128,1,1],bindings:[{name:"data",resource:"fftB_3"}]},{name:"c3.fftB.fft_cols",type:"kernel",kernel:"fft_compute_128",entry:"fft_cols",dispatch:[128,1,1],bindings:[{name:"data",resource:"fftB_3"}]},{name:"c3.assemble",type:"kernel",kernel:"ocean_assemble",entry:"ocean_assemble",dispatch:[256,1,1],bindings:[{name:"fieldA",resource:"fftA_3"},{name:"fieldB",resource:"fftB_3"},{name:"disp",resource:"dispTex3"},{name:"slope",resource:"slopeTex3"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:128}}]},{name:"c3.dispTex3.mips",type:"repeat",count:7,index:"l",body:[{name:"mip",type:"kernel",kernel:"ocean_mip",entry:"ocean_mip",dispatch:[64,1,1],bindings:[{name:"src",resource:"dispTex3",levelAt:{index:"l"}},{name:"dst",resource:"dispTex3",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:128},dstWidth:{index:"l",offset:1,shr:128},pad:0,srcLevel:0}}]}]},{name:"c3.slopeTex3.mips",type:"repeat",count:7,index:"l",body:[{name:"mip",type:"kernel",kernel:"ocean_mip",entry:"ocean_mip",dispatch:[64,1,1],bindings:[{name:"src",resource:"slopeTex3",levelAt:{index:"l"}},{name:"dst",resource:"slopeTex3",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:128},dstWidth:{index:"l",offset:1,shr:128},pad:0,srcLevel:0}}]}]},{name:"foam",type:"kernel",kernel:"ocean_foam",entry:"ocean_foam",dispatch:[16384,1,1],bindings:[{name:"foamIn",resource:"foamA"},{name:"fieldB0",resource:"fftB_0"},{name:"fieldB1",resource:"fftB_1"},{name:"fieldB2",resource:"fftB_2"},{name:"fieldB3",resource:"fftB_3"},{name:"foamOut",resource:"foamB"},{name:"foamTex",resource:"foamTex"}],uniforms:[{name:"foam",values:{cascades:4,cell:.5,dt:"param:dt",gain:"param:foamGain",lambda:"param:lambda",n0:64,n1:128,n2:128,n3:128,omX0:"param:foamOriginMod0X",omX1:"param:foamOriginMod1X",omX2:"param:foamOriginMod2X",omX3:"param:foamOriginMod3X",omZ0:"param:foamOriginMod0Z",omZ1:"param:foamOriginMod1Z",omZ2:"param:foamOriginMod2Z",omZ3:"param:foamOriginMod3Z",originX:"param:foamOriginX",originZ:"param:foamOriginZ",shiftX:"param:foamShiftX",shiftZ:"param:foamShiftZ",size0:1600,size1:250,size2:37,size3:5.3,tau:"param:foamTau",threshold:"param:foamThreshold",width:1024}}]},{name:"foamTex.mips",type:"repeat",count:10,index:"l",body:[{name:"mip",type:"kernel",kernel:"ocean_mip",entry:"ocean_mip",dispatch:[4096,1,1],bindings:[{name:"src",resource:"foamTex",levelAt:{index:"l"}},{name:"dst",resource:"foamTex",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:1024},dstWidth:{index:"l",offset:1,shr:1024},pad:0,srcLevel:0}}]}]},{name:"probe",type:"kernel",kernel:"ocean_probe",entry:"ocean_probe",dispatch:[1,1,1],bindings:[{name:"points",resource:"probePoints"},{name:"disp0",resource:"fftA_0"},{name:"disp1",resource:"fftA_1"},{name:"disp2",resource:"fftA_2"},{name:"disp3",resource:"fftA_3"},{name:"out",resource:"probeOut"}],uniforms:[{name:"probe",values:{cascades:4,count:"param:probeCount",lambda:"param:lambda",n0:64,n1:128,n2:128,n3:128,normalStep:.5,omX0:"param:probeOriginMod0X",omX1:"param:probeOriginMod1X",omX2:"param:probeOriginMod2X",omX3:"param:probeOriginMod3X",omZ0:"param:probeOriginMod0Z",omZ1:"param:probeOriginMod1Z",omZ2:"param:probeOriginMod2Z",omZ3:"param:probeOriginMod3Z",size0:1600,size1:250,size2:37,size3:5.3,t:"param:simTime"}}]}],frameSwaps:[["foamA","foamB"]],exports:{disp0:"dispTex0",disp1:"dispTex1",disp2:"dispTex2",disp3:"dispTex3",foam:"foamTex",probe:"probeOut",slope0:"slopeTex0",slope1:"slopeTex1",slope2:"slopeTex2",slope3:"slopeTex3"},exportParams:["waterModX0","waterModZ0","waterModX1","waterModZ1","waterModX2","waterModZ2","waterModX3","waterModZ3"]},"webgpu/low":{name:"ocean",backend:"webgpu",tier:"low",version:1,params:[{name:"windSpeed",type:"f32",source:{kind:"value",value:9},tween:20},{name:"fetch",type:"f32",source:{kind:"value",value:300},tween:20},{name:"gamma",type:"f32",source:{kind:"value",value:3.3},tween:20},{name:"spread",type:"f32",source:{kind:"value",value:8},tween:20},{name:"swell",type:"f32",source:{kind:"value",value:0},tween:20},{name:"windDirDeg",type:"f32",source:{kind:"value",value:0},tween:20},{name:"swellDirDeg",type:"f32",source:{kind:"value",value:0},tween:20},{name:"seed",type:"u32",source:{kind:"value",value:0}},{name:"loopPeriod",type:"f32",source:{kind:"value",value:1e3}},{name:"lambda",type:"f32",source:{kind:"value",value:1},tween:20},{name:"timeScale",type:"f32",source:{kind:"value",value:1}},{name:"driftX",type:"f32",source:{kind:"value",value:0}},{name:"driftZ",type:"f32",source:{kind:"value",value:0}},{name:"foamTau",type:"f32",source:{kind:"value",value:4}},{name:"foamThreshold",type:"f32",source:{kind:"value",value:.9}},{name:"foamGain",type:"f32",source:{kind:"value",value:2}},{name:"probeCount",type:"u32",source:{kind:"value",value:0}},{name:"probeOriginX",type:"f32",source:{kind:"value",value:0}},{name:"probeOriginZ",type:"f32",source:{kind:"value",value:0}},{name:"probePoints",type:"vec4<f32>[]",count:64,source:{kind:"value",value:0}},{name:"dt",type:"f32",source:{kind:"frame",name:"dt"}},{name:"cameraX",type:"f32",source:{kind:"frame",name:"cameraX"}},{name:"cameraZ",type:"f32",source:{kind:"frame",name:"cameraZ"}},{name:"windDirRad",type:"f32",source:{args:["param:windDirDeg",.017453292519943295],kind:"expr",op:"mul"}},{name:"windDir",type:"f32",source:{args:[1.5707963267948966,"param:windDirRad"],kind:"expr",op:"sub"}},{name:"swellDirRad",type:"f32",source:{args:["param:swellDirDeg",.017453292519943295],kind:"expr",op:"mul"}},{name:"swellDir",type:"f32",source:{args:[1.5707963267948966,"param:swellDirRad"],kind:"expr",op:"sub"}},{name:"simTime",type:"f32",source:{kind:"accumulate",of:"param:timeScale"}},{name:"phaseTime",type:"f32",source:{args:["param:simTime","param:loopPeriod"],kind:"expr",op:"mod"}},{name:"waterX",type:"f32",source:{kind:"accumulate",of:"param:driftX"}},{name:"waterZ",type:"f32",source:{kind:"accumulate",of:"param:driftZ"}},{name:"waterModX0",type:"f32",source:{args:["param:waterX",1600],kind:"expr",op:"mod"}},{name:"waterModZ0",type:"f32",source:{args:["param:waterZ",1600],kind:"expr",op:"mod"}},{name:"waterModX1",type:"f32",source:{args:["param:waterX",250],kind:"expr",op:"mod"}},{name:"waterModZ1",type:"f32",source:{args:["param:waterZ",250],kind:"expr",op:"mod"}},{name:"waterModX2",type:"f32",source:{args:["param:waterX",37],kind:"expr",op:"mod"}},{name:"waterModZ2",type:"f32",source:{args:["param:waterZ",37],kind:"expr",op:"mod"}},{name:"foamRelX",type:"f32",source:{args:["param:cameraX","param:waterX"],kind:"expr",op:"sub"}},{name:"foamTexelX",type:"f32",source:{args:["param:foamRelX",.5],kind:"expr",op:"div"}},{name:"foamCellX",type:"f32",source:{args:["param:foamTexelX"],kind:"expr",op:"floor"}},{name:"foamOriginX",type:"i32",source:{args:["param:foamCellX",512],kind:"expr",op:"sub"}},{name:"foamShiftX",type:"i32",source:{kind:"delta",of:"param:foamOriginX"}},{name:"foamOriginMetersX",type:"f32",source:{args:["param:foamOriginX",.5],kind:"expr",op:"mul"}},{name:"foamOriginMod0X",type:"f32",source:{args:["param:foamOriginMetersX",1600],kind:"expr",op:"mod"}},{name:"foamOriginMod1X",type:"f32",source:{args:["param:foamOriginMetersX",250],kind:"expr",op:"mod"}},{name:"foamOriginMod2X",type:"f32",source:{args:["param:foamOriginMetersX",37],kind:"expr",op:"mod"}},{name:"probeLocalX",type:"f32",source:{args:["param:probeOriginX","param:waterX"],kind:"expr",op:"sub"}},{name:"probeOriginMod0X",type:"f32",source:{args:["param:probeLocalX",1600],kind:"expr",op:"mod"}},{name:"probeOriginMod1X",type:"f32",source:{args:["param:probeLocalX",250],kind:"expr",op:"mod"}},{name:"probeOriginMod2X",type:"f32",source:{args:["param:probeLocalX",37],kind:"expr",op:"mod"}},{name:"foamRelZ",type:"f32",source:{args:["param:cameraZ","param:waterZ"],kind:"expr",op:"sub"}},{name:"foamTexelZ",type:"f32",source:{args:["param:foamRelZ",.5],kind:"expr",op:"div"}},{name:"foamCellZ",type:"f32",source:{args:["param:foamTexelZ"],kind:"expr",op:"floor"}},{name:"foamOriginZ",type:"i32",source:{args:["param:foamCellZ",512],kind:"expr",op:"sub"}},{name:"foamShiftZ",type:"i32",source:{kind:"delta",of:"param:foamOriginZ"}},{name:"foamOriginMetersZ",type:"f32",source:{args:["param:foamOriginZ",.5],kind:"expr",op:"mul"}},{name:"foamOriginMod0Z",type:"f32",source:{args:["param:foamOriginMetersZ",1600],kind:"expr",op:"mod"}},{name:"foamOriginMod1Z",type:"f32",source:{args:["param:foamOriginMetersZ",250],kind:"expr",op:"mod"}},{name:"foamOriginMod2Z",type:"f32",source:{args:["param:foamOriginMetersZ",37],kind:"expr",op:"mod"}},{name:"probeLocalZ",type:"f32",source:{args:["param:probeOriginZ","param:waterZ"],kind:"expr",op:"sub"}},{name:"probeOriginMod0Z",type:"f32",source:{args:["param:probeLocalZ",1600],kind:"expr",op:"mod"}},{name:"probeOriginMod1Z",type:"f32",source:{args:["param:probeLocalZ",250],kind:"expr",op:"mod"}},{name:"probeOriginMod2Z",type:"f32",source:{args:["param:probeLocalZ",37],kind:"expr",op:"mod"}}],resources:[{name:"h0_0",kind:"buffer",elementType:"vec4<f32>",count:4096},{name:"omega_0",kind:"buffer",elementType:"f32",count:4096},{name:"fftA_0",kind:"buffer",elementType:"vec4<f32>",count:4096},{name:"fftB_0",kind:"buffer",elementType:"vec4<f32>",count:4096},{name:"dispTex0",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"slopeTex0",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"h0_1",kind:"buffer",elementType:"vec4<f32>",count:4096},{name:"omega_1",kind:"buffer",elementType:"f32",count:4096},{name:"fftA_1",kind:"buffer",elementType:"vec4<f32>",count:4096},{name:"fftB_1",kind:"buffer",elementType:"vec4<f32>",count:4096},{name:"dispTex1",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"slopeTex1",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"h0_2",kind:"buffer",elementType:"vec4<f32>",count:4096},{name:"omega_2",kind:"buffer",elementType:"f32",count:4096},{name:"fftA_2",kind:"buffer",elementType:"vec4<f32>",count:4096},{name:"fftB_2",kind:"buffer",elementType:"vec4<f32>",count:4096},{name:"dispTex2",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"slopeTex2",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"foamA",kind:"buffer",elementType:"f32",count:1048576},{name:"foamB",kind:"buffer",elementType:"f32",count:1048576},{name:"foamTex",kind:"texture",format:"rgba16float",width:1024,height:1024,mipLevels:11},{name:"probePoints",kind:"buffer",elementType:"vec4<f32>",count:64,feed:"probePoints"},{name:"probeOut",kind:"buffer",elementType:"vec4<f32>",count:64}],passes:[{name:"c0.spectrum",type:"kernel",kernel:"ocean_spectrum",entry:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",dispatch:[64,1,1],bindings:[{name:"h0",resource:"h0_0"},{name:"omega",resource:"omega_0"}],uniforms:[{name:"spec",values:{cascade:0,fetchKm:"param:fetch",gamma:"param:gamma",kMax:.12,kMin:.003926990816987242,loopPeriod:"param:loopPeriod",n:64,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:1600,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c0.evolve",type:"kernel",kernel:"ocean_evolve",entry:"ocean_evolve",dispatch:[64,1,1],bindings:[{name:"h0",resource:"h0_0"},{name:"omega",resource:"omega_0"},{name:"outA",resource:"fftA_0"},{name:"outB",resource:"fftB_0"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:64,t:"param:phaseTime",tile:1600}}]},{name:"c0.fftA.fft_rows",type:"kernel",kernel:"fft_compute_64",entry:"fft_rows",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftA_0"}]},{name:"c0.fftA.fft_cols",type:"kernel",kernel:"fft_compute_64",entry:"fft_cols",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftA_0"}]},{name:"c0.fftB.fft_rows",type:"kernel",kernel:"fft_compute_64",entry:"fft_rows",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftB_0"}]},{name:"c0.fftB.fft_cols",type:"kernel",kernel:"fft_compute_64",entry:"fft_cols",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftB_0"}]},{name:"c0.assemble",type:"kernel",kernel:"ocean_assemble",entry:"ocean_assemble",dispatch:[64,1,1],bindings:[{name:"fieldA",resource:"fftA_0"},{name:"fieldB",resource:"fftB_0"},{name:"disp",resource:"dispTex0"},{name:"slope",resource:"slopeTex0"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:64}}]},{name:"c0.dispTex0.mips",type:"repeat",count:6,index:"l",body:[{name:"mip",type:"kernel",kernel:"ocean_mip",entry:"ocean_mip",dispatch:[16,1,1],bindings:[{name:"src",resource:"dispTex0",levelAt:{index:"l"}},{name:"dst",resource:"dispTex0",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:64},dstWidth:{index:"l",offset:1,shr:64},pad:0,srcLevel:0}}]}]},{name:"c0.slopeTex0.mips",type:"repeat",count:6,index:"l",body:[{name:"mip",type:"kernel",kernel:"ocean_mip",entry:"ocean_mip",dispatch:[16,1,1],bindings:[{name:"src",resource:"slopeTex0",levelAt:{index:"l"}},{name:"dst",resource:"slopeTex0",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:64},dstWidth:{index:"l",offset:1,shr:64},pad:0,srcLevel:0}}]}]},{name:"c1.spectrum",type:"kernel",kernel:"ocean_spectrum",entry:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",dispatch:[64,1,1],bindings:[{name:"h0",resource:"h0_1"},{name:"omega",resource:"omega_1"}],uniforms:[{name:"spec",values:{cascade:1,fetchKm:"param:fetch",gamma:"param:gamma",kMax:.8,kMin:.12,loopPeriod:"param:loopPeriod",n:64,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:250,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c1.evolve",type:"kernel",kernel:"ocean_evolve",entry:"ocean_evolve",dispatch:[64,1,1],bindings:[{name:"h0",resource:"h0_1"},{name:"omega",resource:"omega_1"},{name:"outA",resource:"fftA_1"},{name:"outB",resource:"fftB_1"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:64,t:"param:phaseTime",tile:250}}]},{name:"c1.fftA.fft_rows",type:"kernel",kernel:"fft_compute_64",entry:"fft_rows",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftA_1"}]},{name:"c1.fftA.fft_cols",type:"kernel",kernel:"fft_compute_64",entry:"fft_cols",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftA_1"}]},{name:"c1.fftB.fft_rows",type:"kernel",kernel:"fft_compute_64",entry:"fft_rows",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftB_1"}]},{name:"c1.fftB.fft_cols",type:"kernel",kernel:"fft_compute_64",entry:"fft_cols",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftB_1"}]},{name:"c1.assemble",type:"kernel",kernel:"ocean_assemble",entry:"ocean_assemble",dispatch:[64,1,1],bindings:[{name:"fieldA",resource:"fftA_1"},{name:"fieldB",resource:"fftB_1"},{name:"disp",resource:"dispTex1"},{name:"slope",resource:"slopeTex1"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:64}}]},{name:"c1.dispTex1.mips",type:"repeat",count:6,index:"l",body:[{name:"mip",type:"kernel",kernel:"ocean_mip",entry:"ocean_mip",dispatch:[16,1,1],bindings:[{name:"src",resource:"dispTex1",levelAt:{index:"l"}},{name:"dst",resource:"dispTex1",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:64},dstWidth:{index:"l",offset:1,shr:64},pad:0,srcLevel:0}}]}]},{name:"c1.slopeTex1.mips",type:"repeat",count:6,index:"l",body:[{name:"mip",type:"kernel",kernel:"ocean_mip",entry:"ocean_mip",dispatch:[16,1,1],bindings:[{name:"src",resource:"slopeTex1",levelAt:{index:"l"}},{name:"dst",resource:"slopeTex1",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:64},dstWidth:{index:"l",offset:1,shr:64},pad:0,srcLevel:0}}]}]},{name:"c2.spectrum",type:"kernel",kernel:"ocean_spectrum",entry:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",dispatch:[64,1,1],bindings:[{name:"h0",resource:"h0_2"},{name:"omega",resource:"omega_2"}],uniforms:[{name:"spec",values:{cascade:2,fetchKm:"param:fetch",gamma:"param:gamma",kMax:5.4,kMin:.8,loopPeriod:"param:loopPeriod",n:64,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:37,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c2.evolve",type:"kernel",kernel:"ocean_evolve",entry:"ocean_evolve",dispatch:[64,1,1],bindings:[{name:"h0",resource:"h0_2"},{name:"omega",resource:"omega_2"},{name:"outA",resource:"fftA_2"},{name:"outB",resource:"fftB_2"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:64,t:"param:phaseTime",tile:37}}]},{name:"c2.fftA.fft_rows",type:"kernel",kernel:"fft_compute_64",entry:"fft_rows",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftA_2"}]},{name:"c2.fftA.fft_cols",type:"kernel",kernel:"fft_compute_64",entry:"fft_cols",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftA_2"}]},{name:"c2.fftB.fft_rows",type:"kernel",kernel:"fft_compute_64",entry:"fft_rows",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftB_2"}]},{name:"c2.fftB.fft_cols",type:"kernel",kernel:"fft_compute_64",entry:"fft_cols",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftB_2"}]},{name:"c2.assemble",type:"kernel",kernel:"ocean_assemble",entry:"ocean_assemble",dispatch:[64,1,1],bindings:[{name:"fieldA",resource:"fftA_2"},{name:"fieldB",resource:"fftB_2"},{name:"disp",resource:"dispTex2"},{name:"slope",resource:"slopeTex2"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:64}}]},{name:"c2.dispTex2.mips",type:"repeat",count:6,index:"l",body:[{name:"mip",type:"kernel",kernel:"ocean_mip",entry:"ocean_mip",dispatch:[16,1,1],bindings:[{name:"src",resource:"dispTex2",levelAt:{index:"l"}},{name:"dst",resource:"dispTex2",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:64},dstWidth:{index:"l",offset:1,shr:64},pad:0,srcLevel:0}}]}]},{name:"c2.slopeTex2.mips",type:"repeat",count:6,index:"l",body:[{name:"mip",type:"kernel",kernel:"ocean_mip",entry:"ocean_mip",dispatch:[16,1,1],bindings:[{name:"src",resource:"slopeTex2",levelAt:{index:"l"}},{name:"dst",resource:"slopeTex2",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:64},dstWidth:{index:"l",offset:1,shr:64},pad:0,srcLevel:0}}]}]},{name:"foam",type:"kernel",kernel:"ocean_foam",entry:"ocean_foam",dispatch:[16384,1,1],bindings:[{name:"foamIn",resource:"foamA"},{name:"fieldB0",resource:"fftB_0"},{name:"fieldB1",resource:"fftB_1"},{name:"fieldB2",resource:"fftB_2"},{name:"fieldB3",resource:"fftB_2"},{name:"foamOut",resource:"foamB"},{name:"foamTex",resource:"foamTex"}],uniforms:[{name:"foam",values:{cascades:3,cell:.5,dt:"param:dt",gain:"param:foamGain",lambda:"param:lambda",n0:64,n1:64,n2:64,n3:64,omX0:"param:foamOriginMod0X",omX1:"param:foamOriginMod1X",omX2:"param:foamOriginMod2X",omX3:"param:foamOriginMod2X",omZ0:"param:foamOriginMod0Z",omZ1:"param:foamOriginMod1Z",omZ2:"param:foamOriginMod2Z",omZ3:"param:foamOriginMod2Z",originX:"param:foamOriginX",originZ:"param:foamOriginZ",shiftX:"param:foamShiftX",shiftZ:"param:foamShiftZ",size0:1600,size1:250,size2:37,size3:37,tau:"param:foamTau",threshold:"param:foamThreshold",width:1024}}]},{name:"foamTex.mips",type:"repeat",count:10,index:"l",body:[{name:"mip",type:"kernel",kernel:"ocean_mip",entry:"ocean_mip",dispatch:[4096,1,1],bindings:[{name:"src",resource:"foamTex",levelAt:{index:"l"}},{name:"dst",resource:"foamTex",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:1024},dstWidth:{index:"l",offset:1,shr:1024},pad:0,srcLevel:0}}]}]},{name:"probe",type:"kernel",kernel:"ocean_probe",entry:"ocean_probe",dispatch:[1,1,1],bindings:[{name:"points",resource:"probePoints"},{name:"disp0",resource:"fftA_0"},{name:"disp1",resource:"fftA_1"},{name:"disp2",resource:"fftA_2"},{name:"disp3",resource:"fftA_2"},{name:"out",resource:"probeOut"}],uniforms:[{name:"probe",values:{cascades:3,count:"param:probeCount",lambda:"param:lambda",n0:64,n1:64,n2:64,n3:64,normalStep:.5,omX0:"param:probeOriginMod0X",omX1:"param:probeOriginMod1X",omX2:"param:probeOriginMod2X",omX3:"param:probeOriginMod2X",omZ0:"param:probeOriginMod0Z",omZ1:"param:probeOriginMod1Z",omZ2:"param:probeOriginMod2Z",omZ3:"param:probeOriginMod2Z",size0:1600,size1:250,size2:37,size3:37,t:"param:simTime"}}]}],frameSwaps:[["foamA","foamB"]],exports:{disp0:"dispTex0",disp1:"dispTex1",disp2:"dispTex2",foam:"foamTex",probe:"probeOut",slope0:"slopeTex0",slope1:"slopeTex1",slope2:"slopeTex2"},exportParams:["waterModX0","waterModZ0","waterModX1","waterModZ1","waterModX2","waterModZ2"]},"webgl2/high":{name:"ocean",backend:"webgl2",tier:"high",version:1,params:[{name:"windSpeed",type:"f32",source:{kind:"value",value:9},tween:20},{name:"fetch",type:"f32",source:{kind:"value",value:300},tween:20},{name:"gamma",type:"f32",source:{kind:"value",value:3.3},tween:20},{name:"spread",type:"f32",source:{kind:"value",value:8},tween:20},{name:"swell",type:"f32",source:{kind:"value",value:0},tween:20},{name:"windDirDeg",type:"f32",source:{kind:"value",value:0},tween:20},{name:"swellDirDeg",type:"f32",source:{kind:"value",value:0},tween:20},{name:"seed",type:"u32",source:{kind:"value",value:0}},{name:"loopPeriod",type:"f32",source:{kind:"value",value:1e3}},{name:"lambda",type:"f32",source:{kind:"value",value:1},tween:20},{name:"timeScale",type:"f32",source:{kind:"value",value:1}},{name:"driftX",type:"f32",source:{kind:"value",value:0}},{name:"driftZ",type:"f32",source:{kind:"value",value:0}},{name:"foamTau",type:"f32",source:{kind:"value",value:4}},{name:"foamThreshold",type:"f32",source:{kind:"value",value:.9}},{name:"foamGain",type:"f32",source:{kind:"value",value:2}},{name:"probeCount",type:"u32",source:{kind:"value",value:0}},{name:"dt",type:"f32",source:{kind:"frame",name:"dt"}},{name:"cameraX",type:"f32",source:{kind:"frame",name:"cameraX"}},{name:"cameraZ",type:"f32",source:{kind:"frame",name:"cameraZ"}},{name:"windDirRad",type:"f32",source:{args:["param:windDirDeg",.017453292519943295],kind:"expr",op:"mul"}},{name:"windDir",type:"f32",source:{args:[1.5707963267948966,"param:windDirRad"],kind:"expr",op:"sub"}},{name:"swellDirRad",type:"f32",source:{args:["param:swellDirDeg",.017453292519943295],kind:"expr",op:"mul"}},{name:"swellDir",type:"f32",source:{args:[1.5707963267948966,"param:swellDirRad"],kind:"expr",op:"sub"}},{name:"simTime",type:"f32",source:{kind:"accumulate",of:"param:timeScale"}},{name:"phaseTime",type:"f32",source:{args:["param:simTime","param:loopPeriod"],kind:"expr",op:"mod"}},{name:"waterX",type:"f32",source:{kind:"accumulate",of:"param:driftX"}},{name:"waterZ",type:"f32",source:{kind:"accumulate",of:"param:driftZ"}},{name:"waterModX0",type:"f32",source:{args:["param:waterX",1600],kind:"expr",op:"mod"}},{name:"waterModZ0",type:"f32",source:{args:["param:waterZ",1600],kind:"expr",op:"mod"}},{name:"waterModX1",type:"f32",source:{args:["param:waterX",250],kind:"expr",op:"mod"}},{name:"waterModZ1",type:"f32",source:{args:["param:waterZ",250],kind:"expr",op:"mod"}},{name:"waterModX2",type:"f32",source:{args:["param:waterX",37],kind:"expr",op:"mod"}},{name:"waterModZ2",type:"f32",source:{args:["param:waterZ",37],kind:"expr",op:"mod"}},{name:"waterModX3",type:"f32",source:{args:["param:waterX",5.3],kind:"expr",op:"mod"}},{name:"waterModZ3",type:"f32",source:{args:["param:waterZ",5.3],kind:"expr",op:"mod"}},{name:"foamRelX",type:"f32",source:{args:["param:cameraX","param:waterX"],kind:"expr",op:"sub"}},{name:"foamTexelX",type:"f32",source:{args:["param:foamRelX",.5],kind:"expr",op:"div"}},{name:"foamCellX",type:"f32",source:{args:["param:foamTexelX"],kind:"expr",op:"floor"}},{name:"foamOriginX",type:"i32",source:{args:["param:foamCellX",512],kind:"expr",op:"sub"}},{name:"foamShiftX",type:"i32",source:{kind:"delta",of:"param:foamOriginX"}},{name:"foamOriginMetersX",type:"f32",source:{args:["param:foamOriginX",.5],kind:"expr",op:"mul"}},{name:"foamOriginMod0X",type:"f32",source:{args:["param:foamOriginMetersX",1600],kind:"expr",op:"mod"}},{name:"foamOriginMod1X",type:"f32",source:{args:["param:foamOriginMetersX",250],kind:"expr",op:"mod"}},{name:"foamOriginMod2X",type:"f32",source:{args:["param:foamOriginMetersX",37],kind:"expr",op:"mod"}},{name:"foamOriginMod3X",type:"f32",source:{args:["param:foamOriginMetersX",5.3],kind:"expr",op:"mod"}},{name:"foamRelZ",type:"f32",source:{args:["param:cameraZ","param:waterZ"],kind:"expr",op:"sub"}},{name:"foamTexelZ",type:"f32",source:{args:["param:foamRelZ",.5],kind:"expr",op:"div"}},{name:"foamCellZ",type:"f32",source:{args:["param:foamTexelZ"],kind:"expr",op:"floor"}},{name:"foamOriginZ",type:"i32",source:{args:["param:foamCellZ",512],kind:"expr",op:"sub"}},{name:"foamShiftZ",type:"i32",source:{kind:"delta",of:"param:foamOriginZ"}},{name:"foamOriginMetersZ",type:"f32",source:{args:["param:foamOriginZ",.5],kind:"expr",op:"mul"}},{name:"foamOriginMod0Z",type:"f32",source:{args:["param:foamOriginMetersZ",1600],kind:"expr",op:"mod"}},{name:"foamOriginMod1Z",type:"f32",source:{args:["param:foamOriginMetersZ",250],kind:"expr",op:"mod"}},{name:"foamOriginMod2Z",type:"f32",source:{args:["param:foamOriginMetersZ",37],kind:"expr",op:"mod"}},{name:"foamOriginMod3Z",type:"f32",source:{args:["param:foamOriginMetersZ",5.3],kind:"expr",op:"mod"}}],resources:[{name:"h0_0",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"omega_0",kind:"buffer",elementType:"f32",count:4096,width:64},{name:"fftA_0",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"fftB_0",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"fftA2_0",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"fftB2_0",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"dispTex0",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"slopeTex0",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"h0_1",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"omega_1",kind:"buffer",elementType:"f32",count:65536,width:256},{name:"fftA_1",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"fftB_1",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"fftA2_1",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"fftB2_1",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"dispTex1",kind:"texture",format:"rgba16float",width:256,height:256,mipLevels:9},{name:"slopeTex1",kind:"texture",format:"rgba16float",width:256,height:256,mipLevels:9},{name:"h0_2",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"omega_2",kind:"buffer",elementType:"f32",count:65536,width:256},{name:"fftA_2",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"fftB_2",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"fftA2_2",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"fftB2_2",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"dispTex2",kind:"texture",format:"rgba16float",width:256,height:256,mipLevels:9},{name:"slopeTex2",kind:"texture",format:"rgba16float",width:256,height:256,mipLevels:9},{name:"h0_3",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"omega_3",kind:"buffer",elementType:"f32",count:65536,width:256},{name:"fftA_3",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"fftB_3",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"fftA2_3",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"fftB2_3",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"dispTex3",kind:"texture",format:"rgba16float",width:256,height:256,mipLevels:9},{name:"slopeTex3",kind:"texture",format:"rgba16float",width:256,height:256,mipLevels:9},{name:"foamA",kind:"buffer",elementType:"f32",count:1048576,width:1024},{name:"foamB",kind:"buffer",elementType:"f32",count:1048576,width:1024},{name:"foamTex",kind:"texture",format:"rgba16float",width:1024,height:1024,mipLevels:11}],passes:[{name:"c0.spectrum",type:"kernel",kernel:"ocean_spectrum",entry:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",target:{width:64,height:64,elioWidth:64},bindings:[{name:"h0",resource:"h0_0"},{name:"omega",resource:"omega_0"}],uniforms:[{name:"spec",values:{cascade:0,fetchKm:"param:fetch",gamma:"param:gamma",kMax:.12,kMin:.003926990816987242,loopPeriod:"param:loopPeriod",n:64,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:1600,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c0.evolve",type:"kernel",kernel:"ocean_evolve",entry:"ocean_evolve",target:{width:64,height:64,elioWidth:64},bindings:[{name:"h0",resource:"h0_0"},{name:"omega",resource:"omega_0"},{name:"outA",resource:"fftA_0"},{name:"outB",resource:"fftB_0"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:64,t:"param:phaseTime",tile:1600}}]},{name:"c0.fft.rows",type:"repeat",count:6,index:"s",swap:[["fftA_0","fftA2_0"],["fftB_0","fftB2_0"]],body:[{name:"A",type:"kernel",kernel:"fft_gather_64",entry:"fft_stage",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftA_0"},{name:"dst",resource:"fftA2_0"}],uniforms:[{name:"fft",values:{axis:0,n:64,stage:"param:s"}}]},{name:"B",type:"kernel",kernel:"fft_gather_64",entry:"fft_stage",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftB_0"},{name:"dst",resource:"fftB2_0"}],uniforms:[{name:"fft",values:{axis:0,n:64,stage:"param:s"}}]}]},{name:"c0.fft.cols",type:"repeat",count:6,index:"s",swap:[["fftA_0","fftA2_0"],["fftB_0","fftB2_0"]],body:[{name:"A",type:"kernel",kernel:"fft_gather_64",entry:"fft_stage",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftA_0"},{name:"dst",resource:"fftA2_0"}],uniforms:[{name:"fft",values:{axis:1,n:64,stage:"param:s"}}]},{name:"B",type:"kernel",kernel:"fft_gather_64",entry:"fft_stage",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftB_0"},{name:"dst",resource:"fftB2_0"}],uniforms:[{name:"fft",values:{axis:1,n:64,stage:"param:s"}}]}]},{name:"c0.assemble",type:"kernel",kernel:"ocean_assemble",entry:"ocean_assemble",target:{width:64,height:64,elioWidth:64},bindings:[{name:"fieldA",resource:"fftA_0"},{name:"fieldB",resource:"fftB_0"},{name:"disp",resource:"dispTex0"},{name:"slope",resource:"slopeTex0"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:64}}]},{name:"c0.dispTex0.mips",type:"mipmap",resource:"dispTex0"},{name:"c0.slopeTex0.mips",type:"mipmap",resource:"slopeTex0"},{name:"c1.spectrum",type:"kernel",kernel:"ocean_spectrum",entry:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",target:{width:256,height:256,elioWidth:256},bindings:[{name:"h0",resource:"h0_1"},{name:"omega",resource:"omega_1"}],uniforms:[{name:"spec",values:{cascade:1,fetchKm:"param:fetch",gamma:"param:gamma",kMax:.8,kMin:.12,loopPeriod:"param:loopPeriod",n:256,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:250,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c1.evolve",type:"kernel",kernel:"ocean_evolve",entry:"ocean_evolve",target:{width:256,height:256,elioWidth:256},bindings:[{name:"h0",resource:"h0_1"},{name:"omega",resource:"omega_1"},{name:"outA",resource:"fftA_1"},{name:"outB",resource:"fftB_1"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:256,t:"param:phaseTime",tile:250}}]},{name:"c1.fft.rows",type:"repeat",count:8,index:"s",swap:[["fftA_1","fftA2_1"],["fftB_1","fftB2_1"]],body:[{name:"A",type:"kernel",kernel:"fft_gather_256",entry:"fft_stage",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"fftA_1"},{name:"dst",resource:"fftA2_1"}],uniforms:[{name:"fft",values:{axis:0,n:256,stage:"param:s"}}]},{name:"B",type:"kernel",kernel:"fft_gather_256",entry:"fft_stage",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"fftB_1"},{name:"dst",resource:"fftB2_1"}],uniforms:[{name:"fft",values:{axis:0,n:256,stage:"param:s"}}]}]},{name:"c1.fft.cols",type:"repeat",count:8,index:"s",swap:[["fftA_1","fftA2_1"],["fftB_1","fftB2_1"]],body:[{name:"A",type:"kernel",kernel:"fft_gather_256",entry:"fft_stage",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"fftA_1"},{name:"dst",resource:"fftA2_1"}],uniforms:[{name:"fft",values:{axis:1,n:256,stage:"param:s"}}]},{name:"B",type:"kernel",kernel:"fft_gather_256",entry:"fft_stage",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"fftB_1"},{name:"dst",resource:"fftB2_1"}],uniforms:[{name:"fft",values:{axis:1,n:256,stage:"param:s"}}]}]},{name:"c1.assemble",type:"kernel",kernel:"ocean_assemble",entry:"ocean_assemble",target:{width:256,height:256,elioWidth:256},bindings:[{name:"fieldA",resource:"fftA_1"},{name:"fieldB",resource:"fftB_1"},{name:"disp",resource:"dispTex1"},{name:"slope",resource:"slopeTex1"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:256}}]},{name:"c1.dispTex1.mips",type:"mipmap",resource:"dispTex1"},{name:"c1.slopeTex1.mips",type:"mipmap",resource:"slopeTex1"},{name:"c2.spectrum",type:"kernel",kernel:"ocean_spectrum",entry:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",target:{width:256,height:256,elioWidth:256},bindings:[{name:"h0",resource:"h0_2"},{name:"omega",resource:"omega_2"}],uniforms:[{name:"spec",values:{cascade:2,fetchKm:"param:fetch",gamma:"param:gamma",kMax:5.4,kMin:.8,loopPeriod:"param:loopPeriod",n:256,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:37,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c2.evolve",type:"kernel",kernel:"ocean_evolve",entry:"ocean_evolve",target:{width:256,height:256,elioWidth:256},bindings:[{name:"h0",resource:"h0_2"},{name:"omega",resource:"omega_2"},{name:"outA",resource:"fftA_2"},{name:"outB",resource:"fftB_2"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:256,t:"param:phaseTime",tile:37}}]},{name:"c2.fft.rows",type:"repeat",count:8,index:"s",swap:[["fftA_2","fftA2_2"],["fftB_2","fftB2_2"]],body:[{name:"A",type:"kernel",kernel:"fft_gather_256",entry:"fft_stage",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"fftA_2"},{name:"dst",resource:"fftA2_2"}],uniforms:[{name:"fft",values:{axis:0,n:256,stage:"param:s"}}]},{name:"B",type:"kernel",kernel:"fft_gather_256",entry:"fft_stage",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"fftB_2"},{name:"dst",resource:"fftB2_2"}],uniforms:[{name:"fft",values:{axis:0,n:256,stage:"param:s"}}]}]},{name:"c2.fft.cols",type:"repeat",count:8,index:"s",swap:[["fftA_2","fftA2_2"],["fftB_2","fftB2_2"]],body:[{name:"A",type:"kernel",kernel:"fft_gather_256",entry:"fft_stage",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"fftA_2"},{name:"dst",resource:"fftA2_2"}],uniforms:[{name:"fft",values:{axis:1,n:256,stage:"param:s"}}]},{name:"B",type:"kernel",kernel:"fft_gather_256",entry:"fft_stage",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"fftB_2"},{name:"dst",resource:"fftB2_2"}],uniforms:[{name:"fft",values:{axis:1,n:256,stage:"param:s"}}]}]},{name:"c2.assemble",type:"kernel",kernel:"ocean_assemble",entry:"ocean_assemble",target:{width:256,height:256,elioWidth:256},bindings:[{name:"fieldA",resource:"fftA_2"},{name:"fieldB",resource:"fftB_2"},{name:"disp",resource:"dispTex2"},{name:"slope",resource:"slopeTex2"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:256}}]},{name:"c2.dispTex2.mips",type:"mipmap",resource:"dispTex2"},{name:"c2.slopeTex2.mips",type:"mipmap",resource:"slopeTex2"},{name:"c3.spectrum",type:"kernel",kernel:"ocean_spectrum",entry:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",target:{width:256,height:256,elioWidth:256},bindings:[{name:"h0",resource:"h0_3"},{name:"omega",resource:"omega_3"}],uniforms:[{name:"spec",values:{cascade:3,fetchKm:"param:fetch",gamma:"param:gamma",kMax:37,kMin:5.4,loopPeriod:"param:loopPeriod",n:256,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:5.3,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c3.evolve",type:"kernel",kernel:"ocean_evolve",entry:"ocean_evolve",target:{width:256,height:256,elioWidth:256},bindings:[{name:"h0",resource:"h0_3"},{name:"omega",resource:"omega_3"},{name:"outA",resource:"fftA_3"},{name:"outB",resource:"fftB_3"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:256,t:"param:phaseTime",tile:5.3}}]},{name:"c3.fft.rows",type:"repeat",count:8,index:"s",swap:[["fftA_3","fftA2_3"],["fftB_3","fftB2_3"]],body:[{name:"A",type:"kernel",kernel:"fft_gather_256",entry:"fft_stage",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"fftA_3"},{name:"dst",resource:"fftA2_3"}],uniforms:[{name:"fft",values:{axis:0,n:256,stage:"param:s"}}]},{name:"B",type:"kernel",kernel:"fft_gather_256",entry:"fft_stage",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"fftB_3"},{name:"dst",resource:"fftB2_3"}],uniforms:[{name:"fft",values:{axis:0,n:256,stage:"param:s"}}]}]},{name:"c3.fft.cols",type:"repeat",count:8,index:"s",swap:[["fftA_3","fftA2_3"],["fftB_3","fftB2_3"]],body:[{name:"A",type:"kernel",kernel:"fft_gather_256",entry:"fft_stage",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"fftA_3"},{name:"dst",resource:"fftA2_3"}],uniforms:[{name:"fft",values:{axis:1,n:256,stage:"param:s"}}]},{name:"B",type:"kernel",kernel:"fft_gather_256",entry:"fft_stage",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"fftB_3"},{name:"dst",resource:"fftB2_3"}],uniforms:[{name:"fft",values:{axis:1,n:256,stage:"param:s"}}]}]},{name:"c3.assemble",type:"kernel",kernel:"ocean_assemble",entry:"ocean_assemble",target:{width:256,height:256,elioWidth:256},bindings:[{name:"fieldA",resource:"fftA_3"},{name:"fieldB",resource:"fftB_3"},{name:"disp",resource:"dispTex3"},{name:"slope",resource:"slopeTex3"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:256}}]},{name:"c3.dispTex3.mips",type:"mipmap",resource:"dispTex3"},{name:"c3.slopeTex3.mips",type:"mipmap",resource:"slopeTex3"},{name:"foam",type:"kernel",kernel:"ocean_foam",entry:"ocean_foam",target:{width:1024,height:1024,elioWidth:1024},bindings:[{name:"foamIn",resource:"foamA"},{name:"fieldB0",resource:"fftB_0"},{name:"fieldB1",resource:"fftB_1"},{name:"fieldB2",resource:"fftB_2"},{name:"fieldB3",resource:"fftB_3"},{name:"foamOut",resource:"foamB"},{name:"foamTex",resource:"foamTex"}],uniforms:[{name:"foam",values:{cascades:4,cell:.5,dt:"param:dt",gain:"param:foamGain",lambda:"param:lambda",n0:64,n1:256,n2:256,n3:256,omX0:"param:foamOriginMod0X",omX1:"param:foamOriginMod1X",omX2:"param:foamOriginMod2X",omX3:"param:foamOriginMod3X",omZ0:"param:foamOriginMod0Z",omZ1:"param:foamOriginMod1Z",omZ2:"param:foamOriginMod2Z",omZ3:"param:foamOriginMod3Z",originX:"param:foamOriginX",originZ:"param:foamOriginZ",shiftX:"param:foamShiftX",shiftZ:"param:foamShiftZ",size0:1600,size1:250,size2:37,size3:5.3,tau:"param:foamTau",threshold:"param:foamThreshold",width:1024}}]},{name:"foamTex.mips",type:"mipmap",resource:"foamTex"}],frameSwaps:[["foamA","foamB"]],exports:{disp0:"dispTex0",disp1:"dispTex1",disp2:"dispTex2",disp3:"dispTex3",foam:"foamTex",slope0:"slopeTex0",slope1:"slopeTex1",slope2:"slopeTex2",slope3:"slopeTex3"},exportParams:["waterModX0","waterModZ0","waterModX1","waterModZ1","waterModX2","waterModZ2","waterModX3","waterModZ3"]},"webgl2/medium":{name:"ocean",backend:"webgl2",tier:"medium",version:1,params:[{name:"windSpeed",type:"f32",source:{kind:"value",value:9},tween:20},{name:"fetch",type:"f32",source:{kind:"value",value:300},tween:20},{name:"gamma",type:"f32",source:{kind:"value",value:3.3},tween:20},{name:"spread",type:"f32",source:{kind:"value",value:8},tween:20},{name:"swell",type:"f32",source:{kind:"value",value:0},tween:20},{name:"windDirDeg",type:"f32",source:{kind:"value",value:0},tween:20},{name:"swellDirDeg",type:"f32",source:{kind:"value",value:0},tween:20},{name:"seed",type:"u32",source:{kind:"value",value:0}},{name:"loopPeriod",type:"f32",source:{kind:"value",value:1e3}},{name:"lambda",type:"f32",source:{kind:"value",value:1},tween:20},{name:"timeScale",type:"f32",source:{kind:"value",value:1}},{name:"driftX",type:"f32",source:{kind:"value",value:0}},{name:"driftZ",type:"f32",source:{kind:"value",value:0}},{name:"foamTau",type:"f32",source:{kind:"value",value:4}},{name:"foamThreshold",type:"f32",source:{kind:"value",value:.9}},{name:"foamGain",type:"f32",source:{kind:"value",value:2}},{name:"probeCount",type:"u32",source:{kind:"value",value:0}},{name:"dt",type:"f32",source:{kind:"frame",name:"dt"}},{name:"cameraX",type:"f32",source:{kind:"frame",name:"cameraX"}},{name:"cameraZ",type:"f32",source:{kind:"frame",name:"cameraZ"}},{name:"windDirRad",type:"f32",source:{args:["param:windDirDeg",.017453292519943295],kind:"expr",op:"mul"}},{name:"windDir",type:"f32",source:{args:[1.5707963267948966,"param:windDirRad"],kind:"expr",op:"sub"}},{name:"swellDirRad",type:"f32",source:{args:["param:swellDirDeg",.017453292519943295],kind:"expr",op:"mul"}},{name:"swellDir",type:"f32",source:{args:[1.5707963267948966,"param:swellDirRad"],kind:"expr",op:"sub"}},{name:"simTime",type:"f32",source:{kind:"accumulate",of:"param:timeScale"}},{name:"phaseTime",type:"f32",source:{args:["param:simTime","param:loopPeriod"],kind:"expr",op:"mod"}},{name:"waterX",type:"f32",source:{kind:"accumulate",of:"param:driftX"}},{name:"waterZ",type:"f32",source:{kind:"accumulate",of:"param:driftZ"}},{name:"waterModX0",type:"f32",source:{args:["param:waterX",1600],kind:"expr",op:"mod"}},{name:"waterModZ0",type:"f32",source:{args:["param:waterZ",1600],kind:"expr",op:"mod"}},{name:"waterModX1",type:"f32",source:{args:["param:waterX",250],kind:"expr",op:"mod"}},{name:"waterModZ1",type:"f32",source:{args:["param:waterZ",250],kind:"expr",op:"mod"}},{name:"waterModX2",type:"f32",source:{args:["param:waterX",37],kind:"expr",op:"mod"}},{name:"waterModZ2",type:"f32",source:{args:["param:waterZ",37],kind:"expr",op:"mod"}},{name:"waterModX3",type:"f32",source:{args:["param:waterX",5.3],kind:"expr",op:"mod"}},{name:"waterModZ3",type:"f32",source:{args:["param:waterZ",5.3],kind:"expr",op:"mod"}},{name:"foamRelX",type:"f32",source:{args:["param:cameraX","param:waterX"],kind:"expr",op:"sub"}},{name:"foamTexelX",type:"f32",source:{args:["param:foamRelX",.5],kind:"expr",op:"div"}},{name:"foamCellX",type:"f32",source:{args:["param:foamTexelX"],kind:"expr",op:"floor"}},{name:"foamOriginX",type:"i32",source:{args:["param:foamCellX",512],kind:"expr",op:"sub"}},{name:"foamShiftX",type:"i32",source:{kind:"delta",of:"param:foamOriginX"}},{name:"foamOriginMetersX",type:"f32",source:{args:["param:foamOriginX",.5],kind:"expr",op:"mul"}},{name:"foamOriginMod0X",type:"f32",source:{args:["param:foamOriginMetersX",1600],kind:"expr",op:"mod"}},{name:"foamOriginMod1X",type:"f32",source:{args:["param:foamOriginMetersX",250],kind:"expr",op:"mod"}},{name:"foamOriginMod2X",type:"f32",source:{args:["param:foamOriginMetersX",37],kind:"expr",op:"mod"}},{name:"foamOriginMod3X",type:"f32",source:{args:["param:foamOriginMetersX",5.3],kind:"expr",op:"mod"}},{name:"foamRelZ",type:"f32",source:{args:["param:cameraZ","param:waterZ"],kind:"expr",op:"sub"}},{name:"foamTexelZ",type:"f32",source:{args:["param:foamRelZ",.5],kind:"expr",op:"div"}},{name:"foamCellZ",type:"f32",source:{args:["param:foamTexelZ"],kind:"expr",op:"floor"}},{name:"foamOriginZ",type:"i32",source:{args:["param:foamCellZ",512],kind:"expr",op:"sub"}},{name:"foamShiftZ",type:"i32",source:{kind:"delta",of:"param:foamOriginZ"}},{name:"foamOriginMetersZ",type:"f32",source:{args:["param:foamOriginZ",.5],kind:"expr",op:"mul"}},{name:"foamOriginMod0Z",type:"f32",source:{args:["param:foamOriginMetersZ",1600],kind:"expr",op:"mod"}},{name:"foamOriginMod1Z",type:"f32",source:{args:["param:foamOriginMetersZ",250],kind:"expr",op:"mod"}},{name:"foamOriginMod2Z",type:"f32",source:{args:["param:foamOriginMetersZ",37],kind:"expr",op:"mod"}},{name:"foamOriginMod3Z",type:"f32",source:{args:["param:foamOriginMetersZ",5.3],kind:"expr",op:"mod"}}],resources:[{name:"h0_0",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"omega_0",kind:"buffer",elementType:"f32",count:4096,width:64},{name:"fftA_0",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"fftB_0",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"fftA2_0",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"fftB2_0",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"dispTex0",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"slopeTex0",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"h0_1",kind:"buffer",elementType:"vec4<f32>",count:16384,width:128},{name:"omega_1",kind:"buffer",elementType:"f32",count:16384,width:128},{name:"fftA_1",kind:"buffer",elementType:"vec4<f32>",count:16384,width:128},{name:"fftB_1",kind:"buffer",elementType:"vec4<f32>",count:16384,width:128},{name:"fftA2_1",kind:"buffer",elementType:"vec4<f32>",count:16384,width:128},{name:"fftB2_1",kind:"buffer",elementType:"vec4<f32>",count:16384,width:128},{name:"dispTex1",kind:"texture",format:"rgba16float",width:128,height:128,mipLevels:8},{name:"slopeTex1",kind:"texture",format:"rgba16float",width:128,height:128,mipLevels:8},{name:"h0_2",kind:"buffer",elementType:"vec4<f32>",count:16384,width:128},{name:"omega_2",kind:"buffer",elementType:"f32",count:16384,width:128},{name:"fftA_2",kind:"buffer",elementType:"vec4<f32>",count:16384,width:128},{name:"fftB_2",kind:"buffer",elementType:"vec4<f32>",count:16384,width:128},{name:"fftA2_2",kind:"buffer",elementType:"vec4<f32>",count:16384,width:128},{name:"fftB2_2",kind:"buffer",elementType:"vec4<f32>",count:16384,width:128},{name:"dispTex2",kind:"texture",format:"rgba16float",width:128,height:128,mipLevels:8},{name:"slopeTex2",kind:"texture",format:"rgba16float",width:128,height:128,mipLevels:8},{name:"h0_3",kind:"buffer",elementType:"vec4<f32>",count:16384,width:128},{name:"omega_3",kind:"buffer",elementType:"f32",count:16384,width:128},{name:"fftA_3",kind:"buffer",elementType:"vec4<f32>",count:16384,width:128},{name:"fftB_3",kind:"buffer",elementType:"vec4<f32>",count:16384,width:128},{name:"fftA2_3",kind:"buffer",elementType:"vec4<f32>",count:16384,width:128},{name:"fftB2_3",kind:"buffer",elementType:"vec4<f32>",count:16384,width:128},{name:"dispTex3",kind:"texture",format:"rgba16float",width:128,height:128,mipLevels:8},{name:"slopeTex3",kind:"texture",format:"rgba16float",width:128,height:128,mipLevels:8},{name:"foamA",kind:"buffer",elementType:"f32",count:1048576,width:1024},{name:"foamB",kind:"buffer",elementType:"f32",count:1048576,width:1024},{name:"foamTex",kind:"texture",format:"rgba16float",width:1024,height:1024,mipLevels:11}],passes:[{name:"c0.spectrum",type:"kernel",kernel:"ocean_spectrum",entry:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",target:{width:64,height:64,elioWidth:64},bindings:[{name:"h0",resource:"h0_0"},{name:"omega",resource:"omega_0"}],uniforms:[{name:"spec",values:{cascade:0,fetchKm:"param:fetch",gamma:"param:gamma",kMax:.12,kMin:.003926990816987242,loopPeriod:"param:loopPeriod",n:64,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:1600,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c0.evolve",type:"kernel",kernel:"ocean_evolve",entry:"ocean_evolve",target:{width:64,height:64,elioWidth:64},bindings:[{name:"h0",resource:"h0_0"},{name:"omega",resource:"omega_0"},{name:"outA",resource:"fftA_0"},{name:"outB",resource:"fftB_0"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:64,t:"param:phaseTime",tile:1600}}]},{name:"c0.fft.rows",type:"repeat",count:6,index:"s",swap:[["fftA_0","fftA2_0"],["fftB_0","fftB2_0"]],body:[{name:"A",type:"kernel",kernel:"fft_gather_64",entry:"fft_stage",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftA_0"},{name:"dst",resource:"fftA2_0"}],uniforms:[{name:"fft",values:{axis:0,n:64,stage:"param:s"}}]},{name:"B",type:"kernel",kernel:"fft_gather_64",entry:"fft_stage",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftB_0"},{name:"dst",resource:"fftB2_0"}],uniforms:[{name:"fft",values:{axis:0,n:64,stage:"param:s"}}]}]},{name:"c0.fft.cols",type:"repeat",count:6,index:"s",swap:[["fftA_0","fftA2_0"],["fftB_0","fftB2_0"]],body:[{name:"A",type:"kernel",kernel:"fft_gather_64",entry:"fft_stage",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftA_0"},{name:"dst",resource:"fftA2_0"}],uniforms:[{name:"fft",values:{axis:1,n:64,stage:"param:s"}}]},{name:"B",type:"kernel",kernel:"fft_gather_64",entry:"fft_stage",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftB_0"},{name:"dst",resource:"fftB2_0"}],uniforms:[{name:"fft",values:{axis:1,n:64,stage:"param:s"}}]}]},{name:"c0.assemble",type:"kernel",kernel:"ocean_assemble",entry:"ocean_assemble",target:{width:64,height:64,elioWidth:64},bindings:[{name:"fieldA",resource:"fftA_0"},{name:"fieldB",resource:"fftB_0"},{name:"disp",resource:"dispTex0"},{name:"slope",resource:"slopeTex0"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:64}}]},{name:"c0.dispTex0.mips",type:"mipmap",resource:"dispTex0"},{name:"c0.slopeTex0.mips",type:"mipmap",resource:"slopeTex0"},{name:"c1.spectrum",type:"kernel",kernel:"ocean_spectrum",entry:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",target:{width:128,height:128,elioWidth:128},bindings:[{name:"h0",resource:"h0_1"},{name:"omega",resource:"omega_1"}],uniforms:[{name:"spec",values:{cascade:1,fetchKm:"param:fetch",gamma:"param:gamma",kMax:.8,kMin:.12,loopPeriod:"param:loopPeriod",n:128,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:250,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c1.evolve",type:"kernel",kernel:"ocean_evolve",entry:"ocean_evolve",target:{width:128,height:128,elioWidth:128},bindings:[{name:"h0",resource:"h0_1"},{name:"omega",resource:"omega_1"},{name:"outA",resource:"fftA_1"},{name:"outB",resource:"fftB_1"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:128,t:"param:phaseTime",tile:250}}]},{name:"c1.fft.rows",type:"repeat",count:7,index:"s",swap:[["fftA_1","fftA2_1"],["fftB_1","fftB2_1"]],body:[{name:"A",type:"kernel",kernel:"fft_gather_128",entry:"fft_stage",target:{width:128,height:128,elioWidth:128},bindings:[{name:"src",resource:"fftA_1"},{name:"dst",resource:"fftA2_1"}],uniforms:[{name:"fft",values:{axis:0,n:128,stage:"param:s"}}]},{name:"B",type:"kernel",kernel:"fft_gather_128",entry:"fft_stage",target:{width:128,height:128,elioWidth:128},bindings:[{name:"src",resource:"fftB_1"},{name:"dst",resource:"fftB2_1"}],uniforms:[{name:"fft",values:{axis:0,n:128,stage:"param:s"}}]}]},{name:"c1.fft.cols",type:"repeat",count:7,index:"s",swap:[["fftA_1","fftA2_1"],["fftB_1","fftB2_1"]],body:[{name:"A",type:"kernel",kernel:"fft_gather_128",entry:"fft_stage",target:{width:128,height:128,elioWidth:128},bindings:[{name:"src",resource:"fftA_1"},{name:"dst",resource:"fftA2_1"}],uniforms:[{name:"fft",values:{axis:1,n:128,stage:"param:s"}}]},{name:"B",type:"kernel",kernel:"fft_gather_128",entry:"fft_stage",target:{width:128,height:128,elioWidth:128},bindings:[{name:"src",resource:"fftB_1"},{name:"dst",resource:"fftB2_1"}],uniforms:[{name:"fft",values:{axis:1,n:128,stage:"param:s"}}]}]},{name:"c1.assemble",type:"kernel",kernel:"ocean_assemble",entry:"ocean_assemble",target:{width:128,height:128,elioWidth:128},bindings:[{name:"fieldA",resource:"fftA_1"},{name:"fieldB",resource:"fftB_1"},{name:"disp",resource:"dispTex1"},{name:"slope",resource:"slopeTex1"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:128}}]},{name:"c1.dispTex1.mips",type:"mipmap",resource:"dispTex1"},{name:"c1.slopeTex1.mips",type:"mipmap",resource:"slopeTex1"},{name:"c2.spectrum",type:"kernel",kernel:"ocean_spectrum",entry:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",target:{width:128,height:128,elioWidth:128},bindings:[{name:"h0",resource:"h0_2"},{name:"omega",resource:"omega_2"}],uniforms:[{name:"spec",values:{cascade:2,fetchKm:"param:fetch",gamma:"param:gamma",kMax:5.4,kMin:.8,loopPeriod:"param:loopPeriod",n:128,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:37,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c2.evolve",type:"kernel",kernel:"ocean_evolve",entry:"ocean_evolve",target:{width:128,height:128,elioWidth:128},bindings:[{name:"h0",resource:"h0_2"},{name:"omega",resource:"omega_2"},{name:"outA",resource:"fftA_2"},{name:"outB",resource:"fftB_2"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:128,t:"param:phaseTime",tile:37}}]},{name:"c2.fft.rows",type:"repeat",count:7,index:"s",swap:[["fftA_2","fftA2_2"],["fftB_2","fftB2_2"]],body:[{name:"A",type:"kernel",kernel:"fft_gather_128",entry:"fft_stage",target:{width:128,height:128,elioWidth:128},bindings:[{name:"src",resource:"fftA_2"},{name:"dst",resource:"fftA2_2"}],uniforms:[{name:"fft",values:{axis:0,n:128,stage:"param:s"}}]},{name:"B",type:"kernel",kernel:"fft_gather_128",entry:"fft_stage",target:{width:128,height:128,elioWidth:128},bindings:[{name:"src",resource:"fftB_2"},{name:"dst",resource:"fftB2_2"}],uniforms:[{name:"fft",values:{axis:0,n:128,stage:"param:s"}}]}]},{name:"c2.fft.cols",type:"repeat",count:7,index:"s",swap:[["fftA_2","fftA2_2"],["fftB_2","fftB2_2"]],body:[{name:"A",type:"kernel",kernel:"fft_gather_128",entry:"fft_stage",target:{width:128,height:128,elioWidth:128},bindings:[{name:"src",resource:"fftA_2"},{name:"dst",resource:"fftA2_2"}],uniforms:[{name:"fft",values:{axis:1,n:128,stage:"param:s"}}]},{name:"B",type:"kernel",kernel:"fft_gather_128",entry:"fft_stage",target:{width:128,height:128,elioWidth:128},bindings:[{name:"src",resource:"fftB_2"},{name:"dst",resource:"fftB2_2"}],uniforms:[{name:"fft",values:{axis:1,n:128,stage:"param:s"}}]}]},{name:"c2.assemble",type:"kernel",kernel:"ocean_assemble",entry:"ocean_assemble",target:{width:128,height:128,elioWidth:128},bindings:[{name:"fieldA",resource:"fftA_2"},{name:"fieldB",resource:"fftB_2"},{name:"disp",resource:"dispTex2"},{name:"slope",resource:"slopeTex2"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:128}}]},{name:"c2.dispTex2.mips",type:"mipmap",resource:"dispTex2"},{name:"c2.slopeTex2.mips",type:"mipmap",resource:"slopeTex2"},{name:"c3.spectrum",type:"kernel",kernel:"ocean_spectrum",entry:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",target:{width:128,height:128,elioWidth:128},bindings:[{name:"h0",resource:"h0_3"},{name:"omega",resource:"omega_3"}],uniforms:[{name:"spec",values:{cascade:3,fetchKm:"param:fetch",gamma:"param:gamma",kMax:37,kMin:5.4,loopPeriod:"param:loopPeriod",n:128,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:5.3,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c3.evolve",type:"kernel",kernel:"ocean_evolve",entry:"ocean_evolve",target:{width:128,height:128,elioWidth:128},bindings:[{name:"h0",resource:"h0_3"},{name:"omega",resource:"omega_3"},{name:"outA",resource:"fftA_3"},{name:"outB",resource:"fftB_3"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:128,t:"param:phaseTime",tile:5.3}}]},{name:"c3.fft.rows",type:"repeat",count:7,index:"s",swap:[["fftA_3","fftA2_3"],["fftB_3","fftB2_3"]],body:[{name:"A",type:"kernel",kernel:"fft_gather_128",entry:"fft_stage",target:{width:128,height:128,elioWidth:128},bindings:[{name:"src",resource:"fftA_3"},{name:"dst",resource:"fftA2_3"}],uniforms:[{name:"fft",values:{axis:0,n:128,stage:"param:s"}}]},{name:"B",type:"kernel",kernel:"fft_gather_128",entry:"fft_stage",target:{width:128,height:128,elioWidth:128},bindings:[{name:"src",resource:"fftB_3"},{name:"dst",resource:"fftB2_3"}],uniforms:[{name:"fft",values:{axis:0,n:128,stage:"param:s"}}]}]},{name:"c3.fft.cols",type:"repeat",count:7,index:"s",swap:[["fftA_3","fftA2_3"],["fftB_3","fftB2_3"]],body:[{name:"A",type:"kernel",kernel:"fft_gather_128",entry:"fft_stage",target:{width:128,height:128,elioWidth:128},bindings:[{name:"src",resource:"fftA_3"},{name:"dst",resource:"fftA2_3"}],uniforms:[{name:"fft",values:{axis:1,n:128,stage:"param:s"}}]},{name:"B",type:"kernel",kernel:"fft_gather_128",entry:"fft_stage",target:{width:128,height:128,elioWidth:128},bindings:[{name:"src",resource:"fftB_3"},{name:"dst",resource:"fftB2_3"}],uniforms:[{name:"fft",values:{axis:1,n:128,stage:"param:s"}}]}]},{name:"c3.assemble",type:"kernel",kernel:"ocean_assemble",entry:"ocean_assemble",target:{width:128,height:128,elioWidth:128},bindings:[{name:"fieldA",resource:"fftA_3"},{name:"fieldB",resource:"fftB_3"},{name:"disp",resource:"dispTex3"},{name:"slope",resource:"slopeTex3"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:128}}]},{name:"c3.dispTex3.mips",type:"mipmap",resource:"dispTex3"},{name:"c3.slopeTex3.mips",type:"mipmap",resource:"slopeTex3"},{name:"foam",type:"kernel",kernel:"ocean_foam",entry:"ocean_foam",target:{width:1024,height:1024,elioWidth:1024},bindings:[{name:"foamIn",resource:"foamA"},{name:"fieldB0",resource:"fftB_0"},{name:"fieldB1",resource:"fftB_1"},{name:"fieldB2",resource:"fftB_2"},{name:"fieldB3",resource:"fftB_3"},{name:"foamOut",resource:"foamB"},{name:"foamTex",resource:"foamTex"}],uniforms:[{name:"foam",values:{cascades:4,cell:.5,dt:"param:dt",gain:"param:foamGain",lambda:"param:lambda",n0:64,n1:128,n2:128,n3:128,omX0:"param:foamOriginMod0X",omX1:"param:foamOriginMod1X",omX2:"param:foamOriginMod2X",omX3:"param:foamOriginMod3X",omZ0:"param:foamOriginMod0Z",omZ1:"param:foamOriginMod1Z",omZ2:"param:foamOriginMod2Z",omZ3:"param:foamOriginMod3Z",originX:"param:foamOriginX",originZ:"param:foamOriginZ",shiftX:"param:foamShiftX",shiftZ:"param:foamShiftZ",size0:1600,size1:250,size2:37,size3:5.3,tau:"param:foamTau",threshold:"param:foamThreshold",width:1024}}]},{name:"foamTex.mips",type:"mipmap",resource:"foamTex"}],frameSwaps:[["foamA","foamB"]],exports:{disp0:"dispTex0",disp1:"dispTex1",disp2:"dispTex2",disp3:"dispTex3",foam:"foamTex",slope0:"slopeTex0",slope1:"slopeTex1",slope2:"slopeTex2",slope3:"slopeTex3"},exportParams:["waterModX0","waterModZ0","waterModX1","waterModZ1","waterModX2","waterModZ2","waterModX3","waterModZ3"]},"webgl2/low":{name:"ocean",backend:"webgl2",tier:"low",version:1,params:[{name:"windSpeed",type:"f32",source:{kind:"value",value:9},tween:20},{name:"fetch",type:"f32",source:{kind:"value",value:300},tween:20},{name:"gamma",type:"f32",source:{kind:"value",value:3.3},tween:20},{name:"spread",type:"f32",source:{kind:"value",value:8},tween:20},{name:"swell",type:"f32",source:{kind:"value",value:0},tween:20},{name:"windDirDeg",type:"f32",source:{kind:"value",value:0},tween:20},{name:"swellDirDeg",type:"f32",source:{kind:"value",value:0},tween:20},{name:"seed",type:"u32",source:{kind:"value",value:0}},{name:"loopPeriod",type:"f32",source:{kind:"value",value:1e3}},{name:"lambda",type:"f32",source:{kind:"value",value:1},tween:20},{name:"timeScale",type:"f32",source:{kind:"value",value:1}},{name:"driftX",type:"f32",source:{kind:"value",value:0}},{name:"driftZ",type:"f32",source:{kind:"value",value:0}},{name:"foamTau",type:"f32",source:{kind:"value",value:4}},{name:"foamThreshold",type:"f32",source:{kind:"value",value:.9}},{name:"foamGain",type:"f32",source:{kind:"value",value:2}},{name:"probeCount",type:"u32",source:{kind:"value",value:0}},{name:"dt",type:"f32",source:{kind:"frame",name:"dt"}},{name:"cameraX",type:"f32",source:{kind:"frame",name:"cameraX"}},{name:"cameraZ",type:"f32",source:{kind:"frame",name:"cameraZ"}},{name:"windDirRad",type:"f32",source:{args:["param:windDirDeg",.017453292519943295],kind:"expr",op:"mul"}},{name:"windDir",type:"f32",source:{args:[1.5707963267948966,"param:windDirRad"],kind:"expr",op:"sub"}},{name:"swellDirRad",type:"f32",source:{args:["param:swellDirDeg",.017453292519943295],kind:"expr",op:"mul"}},{name:"swellDir",type:"f32",source:{args:[1.5707963267948966,"param:swellDirRad"],kind:"expr",op:"sub"}},{name:"simTime",type:"f32",source:{kind:"accumulate",of:"param:timeScale"}},{name:"phaseTime",type:"f32",source:{args:["param:simTime","param:loopPeriod"],kind:"expr",op:"mod"}},{name:"waterX",type:"f32",source:{kind:"accumulate",of:"param:driftX"}},{name:"waterZ",type:"f32",source:{kind:"accumulate",of:"param:driftZ"}},{name:"waterModX0",type:"f32",source:{args:["param:waterX",1600],kind:"expr",op:"mod"}},{name:"waterModZ0",type:"f32",source:{args:["param:waterZ",1600],kind:"expr",op:"mod"}},{name:"waterModX1",type:"f32",source:{args:["param:waterX",250],kind:"expr",op:"mod"}},{name:"waterModZ1",type:"f32",source:{args:["param:waterZ",250],kind:"expr",op:"mod"}},{name:"waterModX2",type:"f32",source:{args:["param:waterX",37],kind:"expr",op:"mod"}},{name:"waterModZ2",type:"f32",source:{args:["param:waterZ",37],kind:"expr",op:"mod"}},{name:"foamRelX",type:"f32",source:{args:["param:cameraX","param:waterX"],kind:"expr",op:"sub"}},{name:"foamTexelX",type:"f32",source:{args:["param:foamRelX",.5],kind:"expr",op:"div"}},{name:"foamCellX",type:"f32",source:{args:["param:foamTexelX"],kind:"expr",op:"floor"}},{name:"foamOriginX",type:"i32",source:{args:["param:foamCellX",512],kind:"expr",op:"sub"}},{name:"foamShiftX",type:"i32",source:{kind:"delta",of:"param:foamOriginX"}},{name:"foamOriginMetersX",type:"f32",source:{args:["param:foamOriginX",.5],kind:"expr",op:"mul"}},{name:"foamOriginMod0X",type:"f32",source:{args:["param:foamOriginMetersX",1600],kind:"expr",op:"mod"}},{name:"foamOriginMod1X",type:"f32",source:{args:["param:foamOriginMetersX",250],kind:"expr",op:"mod"}},{name:"foamOriginMod2X",type:"f32",source:{args:["param:foamOriginMetersX",37],kind:"expr",op:"mod"}},{name:"foamRelZ",type:"f32",source:{args:["param:cameraZ","param:waterZ"],kind:"expr",op:"sub"}},{name:"foamTexelZ",type:"f32",source:{args:["param:foamRelZ",.5],kind:"expr",op:"div"}},{name:"foamCellZ",type:"f32",source:{args:["param:foamTexelZ"],kind:"expr",op:"floor"}},{name:"foamOriginZ",type:"i32",source:{args:["param:foamCellZ",512],kind:"expr",op:"sub"}},{name:"foamShiftZ",type:"i32",source:{kind:"delta",of:"param:foamOriginZ"}},{name:"foamOriginMetersZ",type:"f32",source:{args:["param:foamOriginZ",.5],kind:"expr",op:"mul"}},{name:"foamOriginMod0Z",type:"f32",source:{args:["param:foamOriginMetersZ",1600],kind:"expr",op:"mod"}},{name:"foamOriginMod1Z",type:"f32",source:{args:["param:foamOriginMetersZ",250],kind:"expr",op:"mod"}},{name:"foamOriginMod2Z",type:"f32",source:{args:["param:foamOriginMetersZ",37],kind:"expr",op:"mod"}}],resources:[{name:"h0_0",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"omega_0",kind:"buffer",elementType:"f32",count:4096,width:64},{name:"fftA_0",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"fftB_0",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"fftA2_0",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"fftB2_0",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"dispTex0",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"slopeTex0",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"h0_1",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"omega_1",kind:"buffer",elementType:"f32",count:4096,width:64},{name:"fftA_1",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"fftB_1",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"fftA2_1",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"fftB2_1",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"dispTex1",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"slopeTex1",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"h0_2",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"omega_2",kind:"buffer",elementType:"f32",count:4096,width:64},{name:"fftA_2",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"fftB_2",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"fftA2_2",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"fftB2_2",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"dispTex2",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"slopeTex2",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"foamA",kind:"buffer",elementType:"f32",count:1048576,width:1024},{name:"foamB",kind:"buffer",elementType:"f32",count:1048576,width:1024},{name:"foamTex",kind:"texture",format:"rgba16float",width:1024,height:1024,mipLevels:11}],passes:[{name:"c0.spectrum",type:"kernel",kernel:"ocean_spectrum",entry:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",target:{width:64,height:64,elioWidth:64},bindings:[{name:"h0",resource:"h0_0"},{name:"omega",resource:"omega_0"}],uniforms:[{name:"spec",values:{cascade:0,fetchKm:"param:fetch",gamma:"param:gamma",kMax:.12,kMin:.003926990816987242,loopPeriod:"param:loopPeriod",n:64,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:1600,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c0.evolve",type:"kernel",kernel:"ocean_evolve",entry:"ocean_evolve",target:{width:64,height:64,elioWidth:64},bindings:[{name:"h0",resource:"h0_0"},{name:"omega",resource:"omega_0"},{name:"outA",resource:"fftA_0"},{name:"outB",resource:"fftB_0"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:64,t:"param:phaseTime",tile:1600}}]},{name:"c0.fft.rows",type:"repeat",count:6,index:"s",swap:[["fftA_0","fftA2_0"],["fftB_0","fftB2_0"]],body:[{name:"A",type:"kernel",kernel:"fft_gather_64",entry:"fft_stage",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftA_0"},{name:"dst",resource:"fftA2_0"}],uniforms:[{name:"fft",values:{axis:0,n:64,stage:"param:s"}}]},{name:"B",type:"kernel",kernel:"fft_gather_64",entry:"fft_stage",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftB_0"},{name:"dst",resource:"fftB2_0"}],uniforms:[{name:"fft",values:{axis:0,n:64,stage:"param:s"}}]}]},{name:"c0.fft.cols",type:"repeat",count:6,index:"s",swap:[["fftA_0","fftA2_0"],["fftB_0","fftB2_0"]],body:[{name:"A",type:"kernel",kernel:"fft_gather_64",entry:"fft_stage",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftA_0"},{name:"dst",resource:"fftA2_0"}],uniforms:[{name:"fft",values:{axis:1,n:64,stage:"param:s"}}]},{name:"B",type:"kernel",kernel:"fft_gather_64",entry:"fft_stage",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftB_0"},{name:"dst",resource:"fftB2_0"}],uniforms:[{name:"fft",values:{axis:1,n:64,stage:"param:s"}}]}]},{name:"c0.assemble",type:"kernel",kernel:"ocean_assemble",entry:"ocean_assemble",target:{width:64,height:64,elioWidth:64},bindings:[{name:"fieldA",resource:"fftA_0"},{name:"fieldB",resource:"fftB_0"},{name:"disp",resource:"dispTex0"},{name:"slope",resource:"slopeTex0"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:64}}]},{name:"c0.dispTex0.mips",type:"mipmap",resource:"dispTex0"},{name:"c0.slopeTex0.mips",type:"mipmap",resource:"slopeTex0"},{name:"c1.spectrum",type:"kernel",kernel:"ocean_spectrum",entry:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",target:{width:64,height:64,elioWidth:64},bindings:[{name:"h0",resource:"h0_1"},{name:"omega",resource:"omega_1"}],uniforms:[{name:"spec",values:{cascade:1,fetchKm:"param:fetch",gamma:"param:gamma",kMax:.8,kMin:.12,loopPeriod:"param:loopPeriod",n:64,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:250,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c1.evolve",type:"kernel",kernel:"ocean_evolve",entry:"ocean_evolve",target:{width:64,height:64,elioWidth:64},bindings:[{name:"h0",resource:"h0_1"},{name:"omega",resource:"omega_1"},{name:"outA",resource:"fftA_1"},{name:"outB",resource:"fftB_1"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:64,t:"param:phaseTime",tile:250}}]},{name:"c1.fft.rows",type:"repeat",count:6,index:"s",swap:[["fftA_1","fftA2_1"],["fftB_1","fftB2_1"]],body:[{name:"A",type:"kernel",kernel:"fft_gather_64",entry:"fft_stage",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftA_1"},{name:"dst",resource:"fftA2_1"}],uniforms:[{name:"fft",values:{axis:0,n:64,stage:"param:s"}}]},{name:"B",type:"kernel",kernel:"fft_gather_64",entry:"fft_stage",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftB_1"},{name:"dst",resource:"fftB2_1"}],uniforms:[{name:"fft",values:{axis:0,n:64,stage:"param:s"}}]}]},{name:"c1.fft.cols",type:"repeat",count:6,index:"s",swap:[["fftA_1","fftA2_1"],["fftB_1","fftB2_1"]],body:[{name:"A",type:"kernel",kernel:"fft_gather_64",entry:"fft_stage",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftA_1"},{name:"dst",resource:"fftA2_1"}],uniforms:[{name:"fft",values:{axis:1,n:64,stage:"param:s"}}]},{name:"B",type:"kernel",kernel:"fft_gather_64",entry:"fft_stage",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftB_1"},{name:"dst",resource:"fftB2_1"}],uniforms:[{name:"fft",values:{axis:1,n:64,stage:"param:s"}}]}]},{name:"c1.assemble",type:"kernel",kernel:"ocean_assemble",entry:"ocean_assemble",target:{width:64,height:64,elioWidth:64},bindings:[{name:"fieldA",resource:"fftA_1"},{name:"fieldB",resource:"fftB_1"},{name:"disp",resource:"dispTex1"},{name:"slope",resource:"slopeTex1"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:64}}]},{name:"c1.dispTex1.mips",type:"mipmap",resource:"dispTex1"},{name:"c1.slopeTex1.mips",type:"mipmap",resource:"slopeTex1"},{name:"c2.spectrum",type:"kernel",kernel:"ocean_spectrum",entry:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",target:{width:64,height:64,elioWidth:64},bindings:[{name:"h0",resource:"h0_2"},{name:"omega",resource:"omega_2"}],uniforms:[{name:"spec",values:{cascade:2,fetchKm:"param:fetch",gamma:"param:gamma",kMax:5.4,kMin:.8,loopPeriod:"param:loopPeriod",n:64,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:37,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c2.evolve",type:"kernel",kernel:"ocean_evolve",entry:"ocean_evolve",target:{width:64,height:64,elioWidth:64},bindings:[{name:"h0",resource:"h0_2"},{name:"omega",resource:"omega_2"},{name:"outA",resource:"fftA_2"},{name:"outB",resource:"fftB_2"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:64,t:"param:phaseTime",tile:37}}]},{name:"c2.fft.rows",type:"repeat",count:6,index:"s",swap:[["fftA_2","fftA2_2"],["fftB_2","fftB2_2"]],body:[{name:"A",type:"kernel",kernel:"fft_gather_64",entry:"fft_stage",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftA_2"},{name:"dst",resource:"fftA2_2"}],uniforms:[{name:"fft",values:{axis:0,n:64,stage:"param:s"}}]},{name:"B",type:"kernel",kernel:"fft_gather_64",entry:"fft_stage",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftB_2"},{name:"dst",resource:"fftB2_2"}],uniforms:[{name:"fft",values:{axis:0,n:64,stage:"param:s"}}]}]},{name:"c2.fft.cols",type:"repeat",count:6,index:"s",swap:[["fftA_2","fftA2_2"],["fftB_2","fftB2_2"]],body:[{name:"A",type:"kernel",kernel:"fft_gather_64",entry:"fft_stage",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftA_2"},{name:"dst",resource:"fftA2_2"}],uniforms:[{name:"fft",values:{axis:1,n:64,stage:"param:s"}}]},{name:"B",type:"kernel",kernel:"fft_gather_64",entry:"fft_stage",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftB_2"},{name:"dst",resource:"fftB2_2"}],uniforms:[{name:"fft",values:{axis:1,n:64,stage:"param:s"}}]}]},{name:"c2.assemble",type:"kernel",kernel:"ocean_assemble",entry:"ocean_assemble",target:{width:64,height:64,elioWidth:64},bindings:[{name:"fieldA",resource:"fftA_2"},{name:"fieldB",resource:"fftB_2"},{name:"disp",resource:"dispTex2"},{name:"slope",resource:"slopeTex2"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:64}}]},{name:"c2.dispTex2.mips",type:"mipmap",resource:"dispTex2"},{name:"c2.slopeTex2.mips",type:"mipmap",resource:"slopeTex2"},{name:"foam",type:"kernel",kernel:"ocean_foam",entry:"ocean_foam",target:{width:1024,height:1024,elioWidth:1024},bindings:[{name:"foamIn",resource:"foamA"},{name:"fieldB0",resource:"fftB_0"},{name:"fieldB1",resource:"fftB_1"},{name:"fieldB2",resource:"fftB_2"},{name:"fieldB3",resource:"fftB_2"},{name:"foamOut",resource:"foamB"},{name:"foamTex",resource:"foamTex"}],uniforms:[{name:"foam",values:{cascades:3,cell:.5,dt:"param:dt",gain:"param:foamGain",lambda:"param:lambda",n0:64,n1:64,n2:64,n3:64,omX0:"param:foamOriginMod0X",omX1:"param:foamOriginMod1X",omX2:"param:foamOriginMod2X",omX3:"param:foamOriginMod2X",omZ0:"param:foamOriginMod0Z",omZ1:"param:foamOriginMod1Z",omZ2:"param:foamOriginMod2Z",omZ3:"param:foamOriginMod2Z",originX:"param:foamOriginX",originZ:"param:foamOriginZ",shiftX:"param:foamShiftX",shiftZ:"param:foamShiftZ",size0:1600,size1:250,size2:37,size3:37,tau:"param:foamTau",threshold:"param:foamThreshold",width:1024}}]},{name:"foamTex.mips",type:"mipmap",resource:"foamTex"}],frameSwaps:[["foamA","foamB"]],exports:{disp0:"dispTex0",disp1:"dispTex1",disp2:"dispTex2",foam:"foamTex",slope0:"slopeTex0",slope1:"slopeTex1",slope2:"slopeTex2"},exportParams:["waterModX0","waterModZ0","waterModX1","waterModZ1","waterModX2","waterModZ2"]}}})})(),(function(){function U(o){return o.replace(/_([a-z0-9])/g,function(h,M){return M.toUpperCase()})}function R(o){return typeof o=="boolean"?o?1:0:Number(o)}function O(o,h,M){if(typeof o=="string"){if(o.indexOf("param:")!==0)throw new Error("elio-graph: "+M+": bad value "+o);const S=o.slice(6);if(!(S in h))throw new Error("elio-graph: parameter "+S+" was not supplied");return R(h[S])}return R(o)}function W(o,h,M,S){const z=new ArrayBuffer(Math.max(16,Math.ceil(o.size/16)*16)),X=new DataView(z),g={};for(const y of o.fields){let b=h[y.name];if(b===void 0){if(!/^_pad/.test(y.name))throw new Error("elio-graph: "+S+": uniform "+o.struct+" lacks a value for "+y.name);b=0}g[y.name]=!0;const F=O(b,M,S);if(y.type==="u32")X.setUint32(y.offset,F>>>0,!0);else if(y.type==="i32")X.setInt32(y.offset,F|0,!0);else if(y.type==="f32")X.setFloat32(y.offset,F,!0);else throw new Error("elio-graph: "+S+": unsupported uniform type "+y.type)}for(const y of Object.keys(h))if(!g[y])throw new Error("elio-graph: "+S+": uniform "+o.struct+" has no field "+y);return z}function c(o){for(const h of Object.keys(o))if(typeof o[h]=="string")return!1;return!0}const e={time:1,dt:1,cameraX:1,cameraY:1,cameraZ:1},C={add:function(o,h){return o+h},sub:function(o,h){return o-h},mul:function(o,h){return o*h},div:function(o,h){return h===0?0:o/h},mod:function(o,h){return h===0?0:o-Math.floor(o/h)*h},floor:function(o){return Math.floor(o)},min:function(o,h){return Math.min(o,h)},max:function(o,h){return Math.max(o,h)}};function G(o,h){return/\[\]$/.test(o)?Float32Array.from(h||[]):(h=Number(h),Number.isFinite(h)?o==="bool"?h?1:0:o==="u32"?Math.max(0,Math.floor(h))>>>0:o==="i32"?Math.trunc(h)|0:h:0)}function f(o){const h={},M={},S={},z={},X={};let g=!0,y=null;const b=function(A){return A.tween>0?y===null?A.tween:y:0};for(const A of o){const P=A.source||{kind:"value",value:0};P.kind==="value"&&(X[A.name]=G(A.type,/\[\]$/.test(A.type)?P.value:P.value||0))}const F=function(A){if(typeof A=="number")return A;if(typeof A=="string"&&A.indexOf("param:")===0){const P=A.slice(6);return P in h?h[P]:0}throw new Error("elio-graph: bad argument "+A)},j={current:h,changed:{},setTween:function(A){y=Number.isFinite(A)&&A>=0?A:null},set:function(A,P){const T=o.find(function(l){return l.name===A});if(!T||T.source&&T.source.kind!=="value")throw new Error("elio-graph: "+A+" is not a value parameter");const B=G(T.type,P);if(B!==X[A]){if(typeof B!="number"){X[A]=B;return}X[A]=B,b(T)>0&&!g?S[A]={from:h[A],start:null}:delete S[A]}},evaluate:function(A){const P=Number(A.time)||0,T=Number(A.dt)||0;for(const B of o){const l=B.source||{kind:"value",value:0};let n=0;if(l.kind==="value"){n=X[B.name];const m=S[B.name];if(m){m.start===null&&(m.start=P);const Z=Math.min(1,Math.max(0,(P-m.start)/b(B)));Z>=1?delete S[B.name]:n=m.from+(n-m.from)*Z}}else if(l.kind==="frame"){if(!e[l.name])throw new Error("elio-graph: unknown frame source "+l.name);n=l.name==="time"?P:l.name==="dt"?T:Number(A[l.name])||0}else if(l.kind==="expr"){const m=C[l.op];if(!m)throw new Error("elio-graph: unknown op "+l.op);n=m(F(l.args[0]),l.args.length>1?F(l.args[1]):0)}else if(l.kind==="delta"){const m=String(l.of).replace(/^param:/,"");n=g?0:(h[m]||0)-(M[m]||0)}else if(l.kind==="accumulate")z[B.name]=(z[B.name]||0)+F(l.of)*T,n=z[B.name];else throw new Error("elio-graph: unknown parameter source "+l.kind);h[B.name]=/\[\]$/.test(B.type)?X[B.name]:G(B.type,n)}for(const B of o)j.changed[B.name]=g||h[B.name]!==M[B.name];for(const B of o)M[B.name]=h[B.name];g=!1}};return j}function s(o,h,M){if(!o||o==="always")return!0;if(o.indexOf("param:")===0)return!!h[o.slice(6)];if(o.indexOf("changed:")===0)return o.slice(8).split(",").some(function(S){return!!M[S.trim()]});throw new Error("elio-graph: bad when "+o)}function _(o,h){const M=h+(o.offset||0);return o.shr?Math.max(o.shr>>M,1):M}function D(o){const h={},M={},S={};for(const b of o.resources)h[b.name]=b;for(const b of o.params)S[b.name]=!0;const z=function(b){return b in M?M[b]:b},X=function(b,F,j,A){const P=[];for(const T of b){if(T.type!=="repeat"){P.push(g(T,F,j,A));continue}const B=j+T.name;if(!(T.count>=1))throw new Error("elio-graph: repeat "+B+": count "+T.count+", want at least 1");if(!T.index||S[T.index])throw new Error("elio-graph: repeat "+B+": index "+T.index+" is empty or names a declared param");if(!T.body||!T.body.length)throw new Error("elio-graph: repeat "+B+": body is empty");if(T.index in F)throw new Error("elio-graph: repeat "+B+": index "+T.index+" is already in use by an outer repeat");for(const n of T.swap||[])if(!h[n[0]]||!h[n[1]])throw new Error("elio-graph: repeat "+B+": swap names an unknown resource");const l=T.when&&T.when!=="always"?T.when:A;for(let n=0;n<T.count;n++){const m=Object.assign({},F,{[T.index]:n});for(const Z of X(T.body,m,B+"["+n+"].",l))P.push(Z);for(const Z of T.swap||[]){const E=z(Z[0]),I=z(Z[1]);M[Z[0]]=I,M[Z[1]]=E}}}return P},g=function(b,F,j,A){const P=Object.assign({},b,{name:j+b.name});return A&&(!b.when||b.when==="always")&&(P.when=A),b.resource&&(P.resource=z(b.resource)),b.bindings&&(P.bindings=b.bindings.map(function(T){const B=Object.assign({},T,{resource:z(T.resource)});if(T.levelAt){if(!(T.levelAt.index in F))throw new Error("elio-graph: pass "+P.name+": index "+T.levelAt.index+" is not in scope");B.level=_(T.levelAt,F[T.levelAt.index]),delete B.levelAt}return B})),b.uniforms&&(P.uniforms=b.uniforms.map(function(T){const B={};for(const l of Object.keys(T.values)){let n=T.values[l];if(n&&typeof n=="object"){if(!(n.index in F))throw new Error("elio-graph: pass "+P.name+": index "+n.index+" is not in scope");n=_(n,F[n.index])}else typeof n=="string"&&n.indexOf("param:")===0&&n.slice(6)in F&&(n=F[n.slice(6)]);B[l]=n}return Object.assign({},T,{values:B})})),P},y=X(o.passes,{},"","");for(const b of Object.keys(M))if(M[b]!==b)throw new Error("elio-graph: swaps are not restored: "+b+" still refers to "+M[b]+" at the end of the passes");return y}function K(o,h,M){if(!o||o.version!==1)throw new Error("elio-graph: unsupported graph version");if(o.backend!==M)throw new Error("elio-graph: graph is for "+o.backend+", not "+M);const S={};for(const g of o.resources)S[g.name]=g;const z={};for(const g of o.params)z[g.name]=!0;const X=function(g){if(typeof g=="string"&&g.indexOf("param:")===0&&!z[g.slice(6)])throw new Error("elio-graph: undeclared parameter "+g.slice(6))};for(const g of D(o)){const y=g.when||"always";if(y.indexOf("param:")===0)X(y);else if(y.indexOf("changed:")===0){for(const b of y.slice(8).split(","))if(!z[b.trim()])throw new Error("elio-graph: undeclared parameter "+b.trim())}else if(y!=="always")throw new Error("elio-graph: pass "+g.name+": bad when "+y);if(g.type==="mipmap"){if(!S[g.resource])throw new Error("elio-graph: pass "+g.name+": unknown resource "+g.resource);continue}if(!h.kernels[U(g.kernel)])throw new Error("elio-graph: pass "+g.name+": the module has no kernel "+g.kernel);for(const b of g.bindings)if(!S[b.resource])throw new Error("elio-graph: pass "+g.name+": unknown resource "+b.resource);for(const b of g.uniforms||[])for(const F of Object.keys(b.values))X(b.values[F])}for(const g of o.exportParams||[])if(!z[g])throw new Error("elio-graph: exportParams names undeclared parameter "+g);for(const g of o.resources)if(g.feed&&!z[g.feed])throw new Error("elio-graph: resource "+g.name+" is fed by undeclared parameter "+g.feed);for(const g of o.frameSwaps||[])for(const y of g)if(!S[y])throw new Error("elio-graph: frameSwaps names unknown resource "+y);for(const g of Object.keys(o.exports||{})){const y=S[o.exports[g]];if(!y||y.kind!=="texture"&&M!=="webgpu")throw new Error("elio-graph: export "+g+" must name a texture resource")}}function H(o){const h=o.backend,M=o.module,S=o.host||{},z={graph:null,backend:h,ready:!1,failed:null,disposed:!1,buildCount:0,run:P,setValue:A,setTween:F,value:j,hasParam:b,params:null,exportParams:T,export:B,dispose:n,fail:l};let X=null,g=null;const y=function(m){if(!z.failed){if(z.failed=m&&m.fallback||"error",z.error=m,X){try{X.dispose()}catch{}X=null}z.ready=!1,o.onFail&&o.onFail(z.failed,m),!(m&&m.fallback)&&typeof console<"u"&&console.warn&&console.warn("elio-graph: graph disabled: "+(m&&m.message?m.message:String(m)))}};try{const m=o.graph||M.graphs&&M.graphs[h+"/"+(o.tier||"high")];if(!m)throw Object.assign(new Error("elio-graph: no graph for "+h+"/"+o.tier),{fallback:"no-graph"});K(m,M,h);const Z=window.__gosx_elio_graph.executors[h];if(!Z)throw Object.assign(new Error("elio-graph: no executor for "+h),{fallback:"no-executor"});z.graph=m,g=z.params=f(m.params),X=Z({steps:D(m).map(function(E){return{pass:E}}),graph:m,module:M,host:S,pack:W,isStatic:c,kernelKey:U,holds:s,label:o.label||m.name}),z.buildCount=1,z.ready=X.isReady()}catch(m){y(m)}function b(m){return!!(z.graph&&z.graph.params.some(function(Z){return Z.name===m}))}function F(m){g&&g.setTween(m)}function j(m){return g&&m in g.current?g.current[m]:0}function A(m,Z){if(!(!g||z.failed))try{g.set(m,Z)}catch(E){y(E)}}function P(m){if(!X||z.failed||z.disposed)return!1;try{if(z.ready=X.isReady(),!z.ready)return!1;const Z=m||{};return g.evaluate(Z),X.run(g.current,Z,g.changed)!==!1}catch(Z){return y(Z),!1}}function T(){const m={};if(g&&z.graph)for(const Z of z.graph.exportParams||[])m[Z]=g.current[Z];return m}function B(m){return X&&!z.failed?X.export(m):null}function l(m){y({message:m,fallback:m})}function n(){if(!z.disposed&&(z.disposed=!0,z.ready=!1,X)){try{X.dispose()}catch{}X=null}}return z}window.__gosx_elio_graph={create:H,expand:D,pack:W,kernelKey:U,check:K,holds:s,createParams:f,executors:{}}})(),(function(){const U=`#version 300 es
void main() { vec2 p = vec2(float((gl_VertexID << 1) & 2), float(gl_VertexID & 2)); gl_Position = vec4(p * 2. - 1., 0., 1.); }`;function R(c){return{draw:c.getParameter(c.DRAW_FRAMEBUFFER_BINDING),read:c.getParameter(c.READ_FRAMEBUFFER_BINDING),viewport:c.getParameter(c.VIEWPORT),program:c.getParameter(c.CURRENT_PROGRAM),vao:c.getParameter(c.VERTEX_ARRAY_BINDING),active:c.getParameter(c.ACTIVE_TEXTURE),blend:c.isEnabled(c.BLEND),depth:c.isEnabled(c.DEPTH_TEST),cull:c.isEnabled(c.CULL_FACE),scissor:c.isEnabled(c.SCISSOR_TEST),colorMask:c.getParameter(c.COLOR_WRITEMASK),depthMask:c.getParameter(c.DEPTH_WRITEMASK)}}function O(c,e){c.bindBuffer(c.UNIFORM_BUFFER,null),c.bindBufferBase(c.UNIFORM_BUFFER,0,null),c.bindFramebuffer(c.DRAW_FRAMEBUFFER,e.draw),c.bindFramebuffer(c.READ_FRAMEBUFFER,e.read),c.viewport(e.viewport[0],e.viewport[1],e.viewport[2],e.viewport[3]),c.useProgram(e.program),c.bindVertexArray(e.vao),c.activeTexture(e.active);const C=function(G,f){f?c.enable(G):c.disable(G)};C(c.BLEND,e.blend),C(c.DEPTH_TEST,e.depth),C(c.CULL_FACE,e.cull),C(c.SCISSOR_TEST,e.scissor),c.colorMask(e.colorMask[0],e.colorMask[1],e.colorMask[2],e.colorMask[3]),c.depthMask(e.depthMask)}function W(c){const e=c.host.gl,C=c.graph,G=c.module,f=G.kernels,s=c.host.firstUnit||4;if(!(c.host.caps?c.host.caps(e):{}).colorBufferFloat)throw Object.assign(new Error("EXT_color_buffer_float is not available"),{fallback:"EXT_color_buffer_float"});const D=function(a,r){const u=e.getParameter(a);return typeof u=="number"?u:r};if(D(e.MAX_DRAW_BUFFERS,0)<2||D(e.MAX_TEXTURE_IMAGE_UNITS,0)<s+5)throw Object.assign(new Error("too few texture units or draw buffers"),{fallback:"texture-units"});for(const a of c.steps){const r=a.pass;if(!r||r.type!=="kernel")continue;const u=f[c.kernelKey(r.kernel)];if(!u.glsl||!u.gatherLayout)throw Object.assign(new Error("kernel "+r.kernel+" has no GLSL gather form"),{fallback:"kernel:"+r.kernel})}const K=[],H=[],o=[],h=[],M=Object.create(null),S={};for(const a of Object.keys(C.exports||{}))S[C.exports[a]]=!0;const z=e.createVertexArray(),X={split:!1,disposed:!1};let g=null;const y=Object.create(null),b=new Map;let F=0;const j={rgba16float:e.RGBA16F,rgba32float:e.RGBA32F,r32float:e.R32F};function A(a,r,u,t,x){const k=e.createTexture();return K.push(k),e.activeTexture(e.TEXTURE0+s),e.bindTexture(e.TEXTURE_2D,k),e.texStorage2D(e.TEXTURE_2D,t,u,a,r),e.texParameteri(e.TEXTURE_2D,e.TEXTURE_MIN_FILTER,x?t>1?e.LINEAR_MIPMAP_LINEAR:e.LINEAR:e.NEAREST),e.texParameteri(e.TEXTURE_2D,e.TEXTURE_MAG_FILTER,x?e.LINEAR:e.NEAREST),e.texParameteri(e.TEXTURE_2D,e.TEXTURE_WRAP_S,x?e.REPEAT:e.CLAMP_TO_EDGE),e.texParameteri(e.TEXTURE_2D,e.TEXTURE_WRAP_T,x?e.REPEAT:e.CLAMP_TO_EDGE),k}function P(a,r){const u=e.createShader(a);if(h.push(u),e.shaderSource(u,r),e.compileShader(u),!e.getShaderParameter(u,e.COMPILE_STATUS))throw new Error("elio-graph: shader did not compile: "+e.getShaderInfoLog(u));return u}function T(a){if(M[a])return M[a];const r=f[c.kernelKey(a)];g||(g=P(e.VERTEX_SHADER,U));const u=P(e.FRAGMENT_SHADER,r.glsl),t=e.createProgram();e.attachShader(t,g),e.attachShader(t,u),e.linkProgram(t);const x=r.gatherLayout,k=M[a]={handle:t,layout:x,kernel:r};if(!e.getProgramParameter(t,e.LINK_STATUS))throw new Error("elio-graph: program "+a+" did not link: "+e.getProgramInfoLog(t));return e.useProgram(t),k.width=e.getUniformLocation(t,"elio_width"),k.lens=x.inputs.map(function(q){return e.getUniformLocation(t,q.length)}),x.inputs.forEach(function(q,w){e.uniform1i(e.getUniformLocation(t,q.sampler),s+w)}),x.uniforms.forEach(function(q,w){e.uniformBlockBinding(t,e.getUniformBlockIndex(t,q.block),w)}),k}function B(a,r){const t=(a.uniformsStd140||a.uniforms).find(function(x){return x.name===r});if(!t)throw new Error("elio-graph: kernel has no uniform block "+r);return t}function l(){for(const a of C.resources){let r;if(a.kind==="buffer"){const u=Math.ceil(a.count/a.width);r={name:a.name,id:++F,w:a.width,h:u,levels:1,tex:A(a.width,u,e.RGBA32F,1,!1)}}else{const u=a.mipLevels||1,t=j[a.format];if(!t)throw new Error("elio-graph: unsupported format "+a.format);const x=a.format==="rgba16float"&&(u>1||!!S[a.name]);r={name:a.name,id:++F,w:a.width,h:a.height,levels:u,tex:A(a.width,a.height,t,u,x)}}y[a.name]=r}}function n(a){const r=e.createFramebuffer();H.push(r),e.bindFramebuffer(e.FRAMEBUFFER,r);const u=[];for(const t of a){for(e.framebufferTexture2D(e.FRAMEBUFFER,e.COLOR_ATTACHMENT0+t.location,e.TEXTURE_2D,t.res.tex,t.level);u.length<=t.location;)u.push(e.NONE);u[t.location]=e.COLOR_ATTACHMENT0+t.location}return e.drawBuffers(u),{fbo:r,complete:e.checkFramebufferStatus(e.FRAMEBUFFER)===e.FRAMEBUFFER_COMPLETE}}function m(a){const r=a.map(function(k){return k.res.id+"@"+k.level+":"+k.location}).join(","),u=b.get(r+(X.split?"s":""));if(u)return u;let t;const x=function(k){const q=n([k]);if(!q.complete)throw new Error("elio-graph: framebuffer incomplete");return q};if(a.length===1)t=[x(a[0])];else{const k=X.split?null:n(a);k&&k.complete?t=[k]:(X.split=!0,t=a.map(x))}return b.set(r+(X.split?"s":""),t),t}function Z(){if(!X.disposed){X.disposed=!0;for(const a of K)e.deleteTexture(a);for(const a of H)e.deleteFramebuffer(a);for(const a of o)e.deleteBuffer(a);for(const a of Object.keys(M))e.deleteProgram(M[a].handle);for(const a of h)e.deleteShader(a);e.deleteVertexArray(z),K.length=H.length=o.length=0,b.clear()}}let E=[];const I=R(e);try{E=c.steps.map(function(a){const r=a.pass;if(r.type==="mipmap")return{pass:r};const u=T(r.kernel),t=u.layout,x=t.inputs.map(function(w){const v=r.bindings.find(function(L){return L.name===w.name});if(!v)throw new Error("elio-graph: pass "+r.name+" does not bind input "+w.name);return v}),k=t.outputs.map(function(w){const v=r.bindings.find(function(L){return L.name===w.name});if(!v)throw new Error("elio-graph: pass "+r.name+" does not bind output "+w.name);return{binding:v,location:w.location}}),q=(r.uniforms||[]).map(function(w){const v=t.uniforms.findIndex(function(Q){return Q.name===w.name});if(v<0)throw new Error("elio-graph: pass "+r.name+" has no uniform block "+w.name);const L=B(u.kernel,w.name),$=e.createBuffer(),ae=Math.max(16,Math.ceil(L.size/16)*16);return o.push($),e.bindBuffer(e.UNIFORM_BUFFER,$),e.bufferData(e.UNIFORM_BUFFER,ae,e.DYNAMIC_DRAW),{index:v,layout:L,values:w.values,buffer:$,size:ae,written:!1,constant:c.isStatic(w.values)}});return{pass:r,p:u,inputs:x,outputs:k,blocks:q}}),l()}catch(a){throw Z(),a}finally{O(e,I)}function i(a){for(const r of a){const u=y[r[0]];y[r[0]]=y[r[1]],y[r[1]]=u}}function p(a){for(const r of C.resources){if(!r.feed)continue;const u=y[r.name],t=new Float32Array(u.w*u.h*4);t.set(a[r.feed].subarray(0,t.length)),e.activeTexture(e.TEXTURE0+s),e.bindTexture(e.TEXTURE_2D,u.tex),e.texSubImage2D(e.TEXTURE_2D,0,0,0,u.w,u.h,e.RGBA,e.FLOAT,t)}}function d(a,r){const u=a.pass,t=a.p,x=u.target,k=a.outputs.map(function(w){return{res:y[w.binding.resource],level:w.binding.level||0,location:w.location}}),q=a.inputs.map(function(w){return y[w.resource]});a.inputs.forEach(function(w,v){for(const L of k)if(L.res===q[v]&&L.level===(w.level||0))throw new Error("elio-graph: pass "+u.name+" reads "+L.res.name+", which it writes")}),e.useProgram(t.handle),e.uniform1ui(t.width,x.elioWidth),q.forEach(function(w,v){e.activeTexture(e.TEXTURE0+s+v),e.bindTexture(e.TEXTURE_2D,w.tex),t.lens[v]&&e.uniform1ui(t.lens[v],w.w*w.h)});for(const w of a.blocks)(!w.constant||!w.written)&&(e.bindBuffer(e.UNIFORM_BUFFER,w.buffer),e.bufferSubData(e.UNIFORM_BUFFER,0,new Uint8Array(c.pack(w.layout,w.values,r,u.name))),w.written=!0),e.bindBufferRange(e.UNIFORM_BUFFER,w.index,w.buffer,0,w.size);e.viewport(0,0,x.width,x.height);for(const w of m(k))e.bindFramebuffer(e.FRAMEBUFFER,w.fbo),e.drawArrays(e.TRIANGLES,0,3)}return{isReady:function(){return!X.disposed},run:function(a,r,u){if(e.isContextLost&&e.isContextLost())return!1;const t=R(e);try{e.disable(e.BLEND),e.disable(e.DEPTH_TEST),e.disable(e.CULL_FACE),e.disable(e.SCISSOR_TEST),e.colorMask(!0,!0,!0,!0),e.bindVertexArray(z),p(a);for(const k of E)c.holds(k.pass.when,a,u)&&(k.pass.type==="mipmap"?(e.activeTexture(e.TEXTURE0+s),e.bindTexture(e.TEXTURE_2D,y[k.pass.resource].tex),e.generateMipmap(e.TEXTURE_2D)):d(k,a));i(C.frameSwaps||[])}finally{O(e,t)}const x=c.host.textureCache;return x&&(x._sceneTextureEpoch=(x._sceneTextureEpoch||0)+1),!0},export:function(a){const r=y[C.exports[a]];return r?{tex:r.tex,width:r.w,height:r.h,levels:r.levels}:null},split:function(){return X.split},dispose:Z}}window.__gosx_elio_graph.executors.webgl2=W})(),(function(){const U={f32:4,"vec2<f32>":8,"vec4<f32>":16};function R(O){const W=O.host.device,c=O.graph,e=O.module.kernels,C=O.host;if(!W)throw Object.assign(new Error("no device"),{fallback:"no-device"});const G="gosx-elio-"+O.label+"-",f=[],s=[],_=Object.create(null),D=Object.create(null),K=Object.create(null),H=new Map,o=new Map,h={disposed:!1};let M=0;const S={};for(const l of Object.keys(c.exports||{}))S[c.exports[l]]=!0;const z=W.queue,X=GPUBufferUsage.STORAGE|GPUBufferUsage.COPY_DST|GPUBufferUsage.COPY_SRC,g=GPUBufferUsage.UNIFORM|GPUBufferUsage.COPY_DST;for(const l of c.resources)if(l.kind==="buffer"){const n=Math.max(16,Math.ceil(l.count*(U[l.elementType]||16)/4)*4),m=W.createBuffer({label:G+l.name,size:n,usage:X});f.push(m),_[l.name]={name:l.name,id:++M,buffer:m}}else{const n=W.createTexture({label:G+l.name,size:[l.width,l.height,1],format:l.format,mipLevelCount:l.mipLevels||1,usage:GPUTextureUsage.STORAGE_BINDING|GPUTextureUsage.TEXTURE_BINDING});s.push(n),_[l.name]={name:l.name,id:++M,texture:n,w:l.width,h:l.height,levels:l.mipLevels||1,format:l.format}}function y(l){return l.map(function(n){const m={binding:n.binding,visibility:GPUShaderStage.COMPUTE};return n.kind==="uniform"?m.buffer={type:"uniform"}:n.kind==="storage-read"?m.buffer={type:"read-only-storage"}:n.kind==="storage-rw"?m.buffer={type:"storage"}:n.kind==="sampled-texture"||n.kind==="texture"?m.texture={sampleType:"unfilterable-float",viewDimension:"2d"}:m.storageTexture={access:"write-only",format:n.format,viewDimension:"2d"},m})}for(const l of O.steps){const n=l.pass;if(!n)continue;if(n.type!=="kernel")throw new Error("elio-graph: pass "+n.name+": "+n.type+" passes exist only on webgl2");const m=n.kernel+"/"+n.entry;if(D[m])continue;const Z=e[O.kernelKey(n.kernel)],E=K[n.kernel]||(K[n.kernel]=W.createBindGroupLayout({label:G+n.kernel,entries:y(Z.bindings)})),I=W.createShaderModule({label:G+n.kernel,code:Z.wgsl}),i=C.create(W,"compute",{label:G+n.kernel,layout:W.createPipelineLayout({bindGroupLayouts:[E]}),compute:{module:I,entryPoint:n.entry}});D[m]={handle:i,layout:E,kernel:Z}}function b(l){const n=C.validation&&C.validation.get(l.handle);return!n||n.ready===!0?n&&n.pipeline||l.handle:null}let F=0;const j=O.steps.map(function(l){const n=l.pass,m=D[n.kernel+"/"+n.entry],Z=(n.uniforms||[]).map(function(E){const I=m.kernel.uniforms.find(function(d){return d.name===E.name});if(!I)throw new Error("elio-graph: pass "+n.name+" has no uniform block "+E.name);const i=Math.max(16,Math.ceil(I.size/16)*16),p=W.createBuffer({label:G+n.name+"-"+E.name,size:i,usage:g});return f.push(p),{name:E.name,layout:I,values:E.values,buffer:p,constant:O.isStatic(E.values),written:!1}});return{pass:n,p:m,blocks:Z,id:++F}});function A(l){const n=l.pass,m=[],Z=[n.name,l.id];for(const i of l.p.kernel.bindings){let p;if(i.kind==="uniform")p={buffer:l.blocks.find(function(d){return d.name===i.name}).buffer};else{const d=n.bindings.find(function(r){return r.name===i.name});if(!d)throw new Error("elio-graph: pass "+n.name+" does not bind "+i.name);const a=_[d.resource];Z.push(a.id+"@"+(d.level||0)),i.kind==="storage-read"||i.kind==="storage-rw"?p={buffer:a.buffer}:p=a.texture.createView({baseMipLevel:d.level||0,mipLevelCount:1})}m.push({binding:i.binding,resource:p})}const E=Z.join("|");let I=H.get(E);return I||(I=W.createBindGroup({label:G+n.name,layout:l.p.layout,entries:m}),H.set(E,I)),I}function P(l){for(const n of l){const m=_[n[0]];_[n[0]]=_[n[1]],_[n[1]]=m}}const T=W.lost&&W.lost.then?W.lost:null;T&&T.then(function(){B()},function(){B()});function B(){if(!h.disposed){h.disposed=!0;for(const l of f)l.destroy();for(const l of s)l.destroy();f.length=s.length=0,H.clear(),o.clear()}}return{isReady:function(){if(h.disposed)return!1;for(const l of Object.keys(D))if(!b(D[l]))return!1;return!0},run:function(l,n,m){const Z=n.encoder;if(!Z)throw new Error("elio-graph: a webgpu frame needs frame.encoder");for(const E of c.resources)E.feed&&z.writeBuffer(_[E.name].buffer,0,l[E.feed]);for(const E of j){const I=E.pass;if(!O.holds(I.when,l,m))continue;for(const p of E.blocks)p.constant&&p.written||(z.writeBuffer(p.buffer,0,new Uint8Array(O.pack(p.layout,p.values,l,I.name))),p.written=!0);const i=Z.beginComputePass({label:G+I.name});i.setPipeline(b(E.p)),i.setBindGroup(0,A(E)),i.dispatchWorkgroups(I.dispatch[0],I.dispatch[1]||1,I.dispatch[2]||1),i.end()}return P(c.frameSwaps||[]),!0},export:function(l){const n=_[c.exports[l]];if(n&&n.buffer)return{buffer:n.buffer};if(!n)return null;let m=o.get(n.texture);return m||(m=n.texture.createView(),o.set(n.texture,m)),{texture:n.texture,view:m,width:n.w,height:n.h,levels:n.levels}},dispose:B}}window.__gosx_elio_graph.executors.webgpu=R})(),(function(){const U={uniform:0,sampler:1,disp:2,slope:6,foam:10};function R(f,s,_,D){const K=f.indexOf(s);if(K<0)throw new Error("ocean-fft: anchor not found: "+s.slice(0,48));const H=f.indexOf(_,K);if(H<0)throw new Error("ocean-fft: anchor not found: "+_.slice(0,48));return f.slice(0,K)+D+f.slice(H)}function O(f,s,_){if(f.indexOf(s)<0)throw new Error("ocean-fft: anchor not found: "+s.slice(0,48));return f.replace(s,()=>_)}function W(f){let s=`struct OceanSpec { c: array<vec4f, 4>, f: vec4f, m: vec4f, w: array<vec4f, 4> };
`;s+="@group("+f+") @binding("+U.uniform+`) var<uniform> sp: OceanSpec;
`,s+="@group("+f+") @binding("+U.sampler+`) var spSampler: sampler;
`;for(let _=0;_<4;_++)s+="@group("+f+") @binding("+(U.disp+_)+") var spDisp"+_+`: texture_2d<f32>;
`;for(let _=0;_<4;_++)s+="@group("+f+") @binding("+(U.slope+_)+") var spSlope"+_+`: texture_2d<f32>;
`;return s+="@group("+f+") @binding("+U.foam+`) var spFoam: texture_2d<f32>;
`,s+=`fn spLod(c: vec4f, spacing: f32) -> f32 { return max(log2(max(spacing, 1e-3) * c.z / c.x), 0.0); }
`,s}function c(){let f=`  var p = vec3f(xz.x, level, xz.y); var dx = vec3f(1.0, 0.0, 0.0); var dz = vec3f(0.0, 0.0, 1.0);
`;f+=`  var amp = sp.m.x;
`,f+=`  let spacing = max(ocean.p[8].z * ocean.p[8].w * exp(ocean.p[8].w * f32(ring)), r * 6.2831853 / f32(segs));
`;for(let s=0;s<4;s++)f+="  if (sp.c["+s+`].w > 0.5) {
`,f+="    let uv = (xz - sp.w["+s+"].xy) / sp.c["+s+"].x; let lod = spLod(sp.c["+s+"], spacing); let k = smoothstep(0.0, 1.2, depth0 * sp.c["+s+`].y);
`,f+="    let a = textureSampleLevel(spDisp"+s+", spSampler, uv, lod); let b = textureSampleLevel(spSlope"+s+`, spSampler, uv, lod);
`,f+=`    p += a.xyz * k; dx += vec3f(b.z, b.x, a.w) * k; dz += vec3f(a.w, b.y, b.w) * k;
`,f+=`  }
`;return f}function e(){let f=`  var sGx = 0.0; var sGz = 0.0; var sXX = 0.0; var sZZ = 0.0; var sXZ = 0.0;
`;f+=`  let wxz = in.rest - sp.m.yz;
`;for(let s=0;s<4;s++)f+="  if (sp.c["+s+`].w > 0.5) {
`,f+="    let uv = (in.rest - sp.w["+s+"].xy) / sp.c["+s+"].x; let k = smoothstep(0.0, 1.2, in.depth0 * sp.c["+s+`].y);
`,f+="    let a = textureSample(spDisp"+s+", spSampler, uv); let b = textureSample(spSlope"+s+`, spSampler, uv);
`,f+=`    sGx += b.x * k; sGz += b.y * k; sXX += b.z * k; sZZ += b.w * k; sXZ += a.w * k;
`,f+=`  }
`;return f+=`  let sJ = (1.0 + sXX) * (1.0 + sZZ) - sXZ * sXZ;
`,f+=`  let sPersist = textureSampleLevel(spFoam, spSampler, wxz * sp.f.x, 0.0).r;
`,f+=`  let N = normalize(vec3f(sGz * sXZ - (1.0 + sZZ) * sGx, max(sJ, 0.1), sXZ * sGx - sGz * (1.0 + sXX)));
`,f}const C=`    let sNear = 1.0 - smoothstep(0.7 * sp.m.w, sp.m.w, length(in.world.xz - ocean.p[7].xz));
    let sJf = (1.0 + sp.f.y * sXX) * (1.0 + sp.f.y * sZZ) - sp.f.y * sp.f.y * sXZ * sXZ;
    let sStateless = clamp((sp.f.z - sJf) * sp.f.w, 0.0, 1.0) * lace * bu;
    let sKept = smoothstep(0.04, 0.6, sPersist) * (0.55 + 0.45 * lace);
    foam = mix(sStateless, sKept, sNear);
`;function G(f,s){let _=f;return _=O(_,"@location(4) depth0: f32 };","@location(4) depth0: f32, @location(5) rest: vec2f };"),_=R(_,"  var p = vec3f(xz.x, level, xz.y);","  let ph = fract(t * ocean.p[0].w / 9.0",c()),_=O(_,"out.depth0 = depth0;","out.depth0 = depth0; out.rest = xz;"),_=O(_,`  let N = od(normalize(in.normal), in.world.xz, t, ocean.p[9].xy);
`,e()),_=O(_,"  if (dist < 250.0) {","  if (dist < 6000.0) {"),_=O(_,`    foam = (1.0 - smoothstep(0.12, 0.35, in.jacobian)) * lace * bu;
`,C),_=O(_,"(1.0 - smoothstep(80.0, 250.0, dist))","(1.0 - smoothstep(1500.0, 6000.0, dist))"),_=O(_,"(ambient * 0.55 + sunCol * 0.04 * max(dot(N, L), 0.0))","(ambient * 1.4 + sunCol * 0.2 * max(dot(N, L), 0.0) + vec3f(0.05))"),_+`
`+W(s)}window.__gosx_scene3d_ocean_fft_surface={wgsl:G,SLOT:U}})(),(function(){const U={disp:4,slope:8,foam:12};function R(f,s,_){if(f.indexOf(s)<0)throw new Error("ocean-fft: anchor not found: "+s.slice(0,48));return f.replace(s,()=>_)}function O(f,s,_,D){const K=f.indexOf(s);if(K<0)throw new Error("ocean-fft: anchor not found: "+s.slice(0,48));const H=f.indexOf(_,K);if(H<0)throw new Error("ocean-fft: anchor not found: "+_.slice(0,48));return f.slice(0,K)+D+f.slice(H)}function W(){let f=`uniform vec4 u_sp[10];
`;for(let s=0;s<4;s++)f+="uniform sampler2D u_spDisp"+s+`;
`;for(let s=0;s<4;s++)f+="uniform sampler2D u_spSlope"+s+`;
`;return f+=`uniform sampler2D u_spFoam;
`,f+=`float spLod(vec4 c, float spacing) { return max(log2(max(spacing, 1e-3) * c.z / c.x), 0.); }
`,f}function c(){let f=`  vec3 p = vec3(xz.x, level, xz.y), dx = vec3(1., 0., 0.), dz = vec3(0., 0., 1.);
`;f+=`  float amp = u_sp[5].x;
`,f+=`  float spacing = max(u_ocean[8].z * u_ocean[8].w * exp(u_ocean[8].w * float(ring)), r * 6.2831853 / float(segs));
`;for(let s=0;s<4;s++)f+="  if (u_sp["+s+`].w > 0.5) {
`,f+="    vec2 suv = (xz - u_sp["+(6+s)+"].xy) / u_sp["+s+"].x; float slod = spLod(u_sp["+s+"], spacing); float sk = smoothstep(0., 1.2, depth0 * u_sp["+s+`].y);
`,f+="    vec4 sa = textureLod(u_spDisp"+s+", suv, slod), sb = textureLod(u_spSlope"+s+`, suv, slod);
`,f+=`    p += sa.xyz * sk; dx += vec3(sb.z, sb.x, sa.w) * sk; dz += vec3(sa.w, sb.y, sb.w) * sk;
`,f+=`  }
`;return f}function e(){let f=`  float sGx = 0., sGz = 0., sXX = 0., sZZ = 0., sXZ = 0.;
`;f+=`  vec2 wxz = v_rest - u_sp[5].yz;
`;for(let s=0;s<4;s++)f+="  if (u_sp["+s+`].w > 0.5) {
`,f+="    vec2 suv = (v_rest - u_sp["+(6+s)+"].xy) / u_sp["+s+"].x; float sk = smoothstep(0., 1.2, v_d0 * u_sp["+s+`].y);
`,f+="    vec4 sa = texture(u_spDisp"+s+", suv), sb = texture(u_spSlope"+s+`, suv);
`,f+=`    sGx += sb.x * sk; sGz += sb.y * sk; sXX += sb.z * sk; sZZ += sb.w * sk; sXZ += sa.w * sk;
`,f+=`  }
`;return f+=`  float sJ = (1. + sXX) * (1. + sZZ) - sXZ * sXZ;
`,f+=`  float sPersist = textureLod(u_spFoam, wxz * u_sp[4].x, 0.).r;
`,f+=`  vec3 N = normalize(vec3(sGz * sXZ - (1. + sZZ) * sGx, max(sJ, 0.1), sXZ * sGx - sGz * (1. + sXX)));
`,f}const C=`    float sNear = 1. - smoothstep(0.7 * u_sp[5].w, u_sp[5].w, length(v_w.xz - u_ocean[7].xz));
    float sJf = (1. + u_sp[4].y * sXX) * (1. + u_sp[4].y * sZZ) - u_sp[4].y * u_sp[4].y * sXZ * sXZ;
    float sStateless = clamp((u_sp[4].z - sJf) * u_sp[4].w, 0., 1.) * lace * bu;
    float sKept = smoothstep(0.04, 0.6, sPersist) * (0.55 + 0.45 * lace);
    foam = mix(sStateless, sKept, sNear);
`;function G(f,s){let _=f;_=R(_,"out float v_d0;","out float v_d0; out vec2 v_rest;"),_=R(_,"void main() {",W()+"void main() {"),_=O(_,"  vec3 p = vec3(xz.x, level, xz.y), dx","  float ph = fract(t * u_ocean[0].w / 9.",c()),_=R(_,"v_d0 = depth0;","v_d0 = depth0; v_rest = xz;");let D=s;return D=R(D,"in float v_d0;","in float v_d0; in vec2 v_rest;"),D=R(D,"void main() {",W()+"void main() {"),D=R(D,`  vec3 N = od(normalize(v_n), v_w.xz, t, u_ocean[9].xy);
`,e()),D=R(D,"  if (dist < 250.) {","  if (dist < 6000.) {"),D=R(D,`    foam = (1. - smoothstep(0.12, 0.35, v_j)) * lace * bu;
`,C),D=R(D,"(1. - smoothstep(80., 250., dist))","(1. - smoothstep(1500., 6000., dist))"),D=R(D,"(ambient * 0.55 + sunCol * 0.04 * max(dot(N, L), 0.))","(ambient * 1.4 + sunCol * 0.2 * max(dot(N, L), 0.) + vec3(0.05))"),{vertex:_,fragment:D}}window.__gosx_scene3d_ocean_fft_surface_gl={glsl:G,UNIT:U}})(),(function(){const U=window.__gosx_elio_graph,R=window.__gosx_scene3d_elio_ocean,O=window.__gosx_scene3d_ocean_fft_surface,W=window.__gosx_scene3d_ocean_fft_surface_gl,c=.25,e=3.5,C=240,G=function(i){return Math.max(.3,.14*i*i/9.81)},f=40,s=4;let _=null;function D(i){return i==="low"||i==="medium"?i:"high"}function K(i){const p=[];let d={cell:.5,width:1024};for(const a of U.expand(i)){const r=a.uniforms&&a.uniforms[0]&&a.uniforms[0].values;a.kernel==="ocean_spectrum"&&r?p.push({tile:r.tile,kMin:r.kMin,kMax:r.kMax,n:r.n}):a.kernel==="ocean_foam"&&r&&(d={cell:r.cell,width:r.width})}return{cascades:p.slice(0,s),foam:d}}function H(i,p){const d=p.graphParams;if(d)for(const a of Object.keys(d))Number.isFinite(d[a])&&i.hasParam(a)&&i.setValue(a,d[a]);Number.isFinite(p.graphTween)&&i.setTween(p.graphTween)}function o(i,p,d){d.fill(0);const a=i.exportParams(),r=p.foam;p.cascades.forEach(function(x,k){d[k*4]=x.tile,d[k*4+1]=Math.sqrt(Math.max(x.kMin,1e-6)*x.kMax),d[k*4+2]=x.n,d[k*4+3]=1,d[24+k*4]=a["waterModX"+k]||0,d[24+k*4+1]=a["waterModZ"+k]||0});const u=r.cell*r.width,t=function(x){return x-Math.floor(x/u)*u};d[16]=1/u,d[17]=1,d[18]=i.value("foamThreshold"),d[19]=e,d[20]=G(i.value("windSpeed")),d[21]=t(i.value("waterX")),d[22]=t(i.value("waterZ")),d[23]=C}function h(i,p){const d=Number.isFinite(p.time)?p.time:0,a=i.last===null?0:Math.min(c,Math.max(0,d-i.last));i.last=d;const r=p.camera||{};return{time:d,dt:a,cameraX:r.x||0,cameraY:r.y||0,cameraZ:r.z||0}}function M(){return _}function S(i){_=i.failed?null:{x:i.value("waterX"),z:i.value("waterZ"),time:i.value("simTime")}}const z=3,X=250,g=64,y=1,b=8,F=16,j={collect:[],k:0,latest:null,previous:null};function A(){j.collect=[],j.k=0}function P(i,p,d,a){const r=j.k++;j.collect.length<g&&j.collect.push({x:i,z:p});const u=Number.isFinite(a)?a:typeof performance<"u"?performance.now():0,t=j.latest;if(!t||r>=t.count||u-t.at>X||Math.abs(t.points[r].x-i)>8||Math.abs(t.points[r].z-p)>8)return null;const x=j.previous&&j.previous.count>r?j.previous:null,k=x&&t.at>x.at?(t.heights[r]-x.heights[r])/((t.at-x.at)/1e3):0,q=t.heights[r]+k*((u-t.at)/1e3),w=t.nx[r],v=t.nz[r],L=Math.sqrt(Math.max(0,1-w*w-v*v));return{x:i,y:d+q,z:p,normal:{x:w,y:L,z:v},velocity:{x:0,y:k,z:0}}}function T(i,p){const d=[];for(let r=0;r<z;r++)d.push({buffer:i.createBuffer({label:"gosx-ocean-fft-probe-"+r,size:g*F,usage:y|b}),state:"free",meta:null});const a=function(r){const u=r.buffer,t=r.meta;r.state="mapping",u.mapAsync(y).then(function(){if(r.state!=="mapping")return;const x=new Float32Array(u.getMappedRange().slice(0,t.count*F));u.unmap();const k=[],q=[],w=[];for(let v=0;v<t.count;v++)k.push(x[v*4]),q.push(x[v*4+1]),w.push(x[v*4+2]);j.previous=j.latest,j.latest={at:t.at,count:t.count,points:t.points,heights:k,nx:q,nz:w},r.state="free"},function(){r.state="free"})};return{poll:function(){for(const r of d)r.state==="copied"&&a(r)},copy:function(r,u,t){const x=d.find(function(k){return k.state==="free"});x&&(r.copyBufferToBuffer(u,0,x.buffer,0,g*F),x.state="copied",x.meta={at:p(),count:t.length,points:t})},dispose:function(){for(const r of d)r.state="disposed",r.buffer.destroy()}}}function B(i,p){if(!i.hasParam("probePoints"))return!1;const d=Math.min(p.length,g);if(!d)return i.setValue("probeCount",0),!1;let a=0,r=0;for(let t=0;t<d;t++)a+=p[t].x,r+=p[t].z;a/=d,r/=d;const u=new Float32Array(g*4);for(let t=0;t<d;t++)u[t*4]=p[t].x-a,u[t*4+1]=p[t].z-r;return i.setValue("probeOriginX",a),i.setValue("probeOriginZ",r),i.setValue("probeCount",d),i.setValue("probePoints",u),!0}function l(i,p,d){const a=function(r,u){const t=_;if(!t)return i.sample(p,r,u,d);const x=i.sample(p,r-t.x,u-t.z,t.time);return x.x+=t.x,x.z+=t.z,x};return{sample:a,probed:function(r,u){return P(r,u,p.data[0])||a(r,u)},begin:A}}function n(i){const p=i.device,d="gosx-ocean-fft-",a=function(){const N=GPUShaderStage.VERTEX|GPUShaderStage.FRAGMENT,Y=[];Y.push({binding:O.SLOT.uniform,visibility:N,buffer:{type:"uniform"}},{binding:O.SLOT.sampler,visibility:N,sampler:{type:"filtering"}});for(let J=0;J<s;J++)Y.push({binding:O.SLOT.disp+J,visibility:N,texture:{sampleType:"float"}},{binding:O.SLOT.slope+J,visibility:N,texture:{sampleType:"float"}});return Y.push({binding:O.SLOT.foam,visibility:N,texture:{sampleType:"float"}}),Y},r=p.createBindGroupLayout({label:d+"surface",entries:a()}),u=p.createSampler({label:d+"sampler",magFilter:"linear",minFilter:"linear",mipmapFilter:"linear",addressModeU:"repeat",addressModeV:"repeat"}),t=p.createBuffer({label:d+"surface",size:f*4,usage:GPUBufferUsage.UNIFORM|GPUBufferUsage.COPY_DST}),x=p.createTexture({label:d+"dummy",size:[1,1,1],format:"rgba16float",usage:GPUTextureUsage.TEXTURE_BINDING|GPUTextureUsage.COPY_DST}),k=x.createView(),q=new Float32Array(f),w={last:null,tier:"",runner:null,info:null,viewList:null},v=T(p,function(){return i.now?i.now():typeof performance<"u"?performance.now():0}),L={device:p,ready:!1,layout:r,group:null,disposed:!1,tier:"",update:Q,dispose:oe,fail:$};function $(N){const Y="ocean-fft: "+(N&&N.message?N.message:String(N));i.guard&&i.guard.fail("gosx-ocean-fft-surface",Y)}function ae(N){w.runner&&w.runner.dispose();const Y=U.create({module:R,backend:"webgpu",tier:N,host:{device:p,create:i.create,validation:i.validation}});w.runner=Y,w.tier=N,w.last=null,w.viewList=null,L.group=null,L.ready=!1,w.info=Y.graph?K(Y.graph):null}function V(){const N=w.runner,Y=[],J=[{binding:O.SLOT.uniform,resource:{buffer:t}},{binding:O.SLOT.sampler,resource:u}];for(let ee=0;ee<s;ee++){const re=N.export("disp"+ee),ne=N.export("slope"+ee);J.push({binding:O.SLOT.disp+ee,resource:re?re.view:k},{binding:O.SLOT.slope+ee,resource:ne?ne.view:k}),Y.push(re?re.view:k,ne?ne.view:k)}const te=N.export("foam");J.push({binding:O.SLOT.foam,resource:te?te.view:k}),Y.push(te?te.view:k),!(w.viewList&&Y.every(function(ee,re){return ee===w.viewList[re]}))&&(w.viewList=Y,L.group=p.createBindGroup({label:d+"surface",layout:r,entries:J}))}function Q(N){const Y=D(N.tier);(!w.runner||Y!==w.tier)&&ae(Y);const J=w.runner;if(J.failed)return L.ready=!1,L.group=null,!1;H(J,N.ocean),v.poll();const te=j.collect.slice(0,g),ee=B(J,te),re=J.run(Object.assign(h(w,N),{encoder:N.encoder}));if(J.failed)return L.ready=!1,L.group=null,_=null,i.guard&&i.guard.fail("gosx-elio-ocean","ocean graph failed: "+J.failed),!1;if(!re)return L.ready;const ne=ee?J.export("probe"):null;return ne&&ne.buffer&&v.copy(N.encoder,ne.buffer,te),V(),o(J,w.info,q),p.queue.writeBuffer(t,0,q),S(J),L.ready=!0,!0}function oe(){L.disposed||(L.disposed=!0,L.ready=!1,L.group=null,w.runner&&w.runner.dispose(),_=null,j.latest=j.previous=null,v.dispose(),t.destroy(),x.destroy())}return Object.defineProperty(L,"runner",{get:function(){return w.runner}}),L}function m(i,p){const d=p.environment&&p.environment.ocean;let a=i.fft;const r=p.guard&&(p.guard.disabled.has("oceanFFT")||p.guard.disabled.has("elioGraph"));return!d||!d.spectrum||r||a&&a.device!==p.device?(a&&(a.dispose(),i.fft=null),null):(a||(a=i.fft=n(p)),a.update({ocean:d,tier:p.tier,time:p.time,camera:p.camera,encoder:p.encoder}),a)}function Z(i,p){const d={last:null},a=U.create({module:R,backend:"webgl2",tier:D(p.tier),host:{gl:i,caps:p.caps,textureCache:p.textureCache}}),r=new Float32Array(f),u=new Map,t=a.graph?K(a.graph):null,x={gl:i,runner:a,tier:D(p.tier),disposed:!1,update:k,bind:q,dispose:w,fail:function(v){a.fail(v)}};function k(v){return a.failed||(H(a,v.ocean),!a.run(h(d,v)))?!1:(o(a,t,r),S(a),!0)}function q(v,L){let $=u.get(L);if(!$){const V=function(Q){return v.getUniformLocation(L,Q)};$={sp:V("u_sp[0]"),disp:[0,1,2,3].map(function(Q){return V("u_spDisp"+Q)}),slope:[0,1,2,3].map(function(Q){return V("u_spSlope"+Q)}),foam:V("u_spFoam")},u.set(L,$)}const ae=function(V,Q,oe){v.activeTexture(v.TEXTURE0+V),v.bindTexture(v.TEXTURE_2D,oe?oe.tex:x.dummy),v.uniform1i(Q,V)};x.dummy||(x.dummy=v.createTexture(),v.bindTexture(v.TEXTURE_2D,x.dummy),v.texStorage2D(v.TEXTURE_2D,1,v.RGBA16F,1,1),v.texParameteri(v.TEXTURE_2D,v.TEXTURE_MIN_FILTER,v.LINEAR),v.texParameteri(v.TEXTURE_2D,v.TEXTURE_MAG_FILTER,v.LINEAR));for(let V=0;V<s;V++)ae(W.UNIT.disp+V,$.disp[V],a.export("disp"+V)),ae(W.UNIT.slope+V,$.slope[V],a.export("slope"+V));ae(W.UNIT.foam,$.foam,a.export("foam")),v.uniform4fv($.sp,r)}function w(){x.disposed||(x.disposed=!0,a.dispose(),x.dummy&&i.deleteTexture(x.dummy))}return x}function E(i,p,d){let a=i.fft;if(a&&(a.gl!==p||a.disposed||a.tier!==D(d.tier))&&(a.dispose(),a=i.fft=null),i.fftDisabled||p.isContextLost&&p.isContextLost())return null;a||(a=i.fft=Z(p,d));const r=a.update({ocean:d.ocean,time:d.time,camera:d.camera});return a.runner.failed?(i.fftDisabled=a.runner.failed,a.dispose(),i.fft=null,_=null,d.mount&&d.mount.setAttribute&&d.mount.setAttribute("data-gosx-scene3d-ocean-fft-fallback",i.fftDisabled),null):r?a:null}function I(i,p,d){i.fft&&i.fft.dispose(),i.fft=null,i.fftDisabled=d,_=null,p&&p.mount&&p.mount.setAttribute&&p.mount.setAttribute("data-gosx-scene3d-ocean-fft-fallback",d)}window.__gosx_scene3d_ocean_fft_api={frame:m,water:M,sampler:l,probe:{begin:A,sample:P},probeState:j,describe:K,feed:H,surfaceData:o},window.__gosx_scene3d_ocean_fft_gl_api={frame:E,fail:I}})();
