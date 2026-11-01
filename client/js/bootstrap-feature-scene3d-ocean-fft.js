(function(){"use strict";function re(S){if(S&&typeof S=="object"){for(const L of Object.keys(S))re(S[L]);Object.freeze(S)}return S}window.__gosx_scene3d_elio_ocean=re({elio:"v0.5.0",set:"ocean",sha256:"3164f8e7d5544d0d8812b023cf2baa4774c1f688e98e9935fa8b702a90bd3125",kernels:{oceanSpectrum:{wgsl:`struct OceanSpectrumParams {
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
uniform uint h0_w;
uniform highp sampler2D omega_tex;
uniform uint omega_len;
uniform uint omega_w;
layout(location = 0) out vec4 outA_out;
layout(location = 1) out vec4 outB_out;

vec4 h0_at(uint i) {
  return texelFetch(h0_tex, ivec2(int(i % h0_w), int(i / h0_w)), 0);
}

float omega_at(uint i) {
  return texelFetch(omega_tex, ivec2(int(i % omega_w), int(i / omega_w)), 0).x;
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
`,gatherLayout:{kernel:"ocean_evolve",inputs:[{name:"h0",kind:"buffer",elem:"vec4",sampler:"h0_tex",length:"h0_len",width:"h0_w",binding:1},{name:"omega",kind:"buffer",elem:"f32",sampler:"omega_tex",length:"omega_len",width:"omega_w",binding:2}],outputs:[{name:"outA",elem:"vec4",format:"",location:0,variable:"outA_out"},{name:"outB",elem:"vec4",format:"",location:1,variable:"outB_out"}],uniforms:[{name:"ev",block:"OceanEvolveParams_ubo",struct:"OceanEvolveParams",binding:0}]}},oceanAssemble:{wgsl:`struct OceanAssembleParams {
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
uniform uint fieldA_w;
uniform highp sampler2D fieldB_tex;
uniform uint fieldB_len;
uniform uint fieldB_w;
layout(location = 0) out vec4 disp_out;
layout(location = 1) out vec4 slope_out;

vec4 fieldA_at(uint i) {
  return texelFetch(fieldA_tex, ivec2(int(i % fieldA_w), int(i / fieldA_w)), 0);
}

vec4 fieldB_at(uint i) {
  return texelFetch(fieldB_tex, ivec2(int(i % fieldB_w), int(i / fieldB_w)), 0);
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
`,gatherLayout:{kernel:"ocean_assemble",inputs:[{name:"fieldA",kind:"buffer",elem:"vec4",sampler:"fieldA_tex",length:"fieldA_len",width:"fieldA_w",binding:1},{name:"fieldB",kind:"buffer",elem:"vec4",sampler:"fieldB_tex",length:"fieldB_len",width:"fieldB_w",binding:2}],outputs:[{name:"disp",elem:"vec4",format:"rgba16float",location:0,variable:"disp_out"},{name:"slope",elem:"vec4",format:"rgba16float",location:1,variable:"slope_out"}],uniforms:[{name:"assem",block:"OceanAssembleParams_ubo",struct:"OceanAssembleParams",binding:0}]}},oceanMip:{wgsl:`struct OceanMipParams {
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
`,gatherLayout:{kernel:"ocean_mip",inputs:[{name:"src",kind:"texture",elem:"vec4",sampler:"src",length:"",width:"",binding:1}],outputs:[{name:"dst",elem:"vec4",format:"rgba16float",location:0,variable:"dst_out"}],uniforms:[{name:"mip",block:"OceanMipParams_ubo",struct:"OceanMipParams",binding:0}]}},oceanFoam:{wgsl:`struct OceanFoamParams {
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
  wakeWidth : u32,
  wakeGain : f32,
  wakeCell : f32,
  _pad4 : f32,
  wakeRelX : f32,
  wakeRelZ : f32,
  wakeOriginX : i32,
  wakeOriginZ : i32,
};

@group(0) @binding(0) var<uniform> foam : OceanFoamParams;
@group(0) @binding(1) var<storage, read> foamIn : array<f32>;
@group(0) @binding(2) var<storage, read> fieldB0 : array<vec4<f32>>;
@group(0) @binding(3) var<storage, read> fieldB1 : array<vec4<f32>>;
@group(0) @binding(4) var<storage, read> fieldB2 : array<vec4<f32>>;
@group(0) @binding(5) var<storage, read> fieldB3 : array<vec4<f32>>;
@group(0) @binding(6) var<storage, read_write> foamOut : array<f32>;
@group(0) @binding(7) var foamTex : texture_storage_2d<rgba16float, write>;
@group(0) @binding(8) var<storage, read> wakeIn : array<vec4<f32>>;

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
  var wf = 0.0;
  if ((foam.wakeWidth > 0u)) {
    if ((foam.wakeGain > 0.0)) {
      let ww = i32(foam.wakeWidth);
      let px = (((foam.wakeRelX + ((f32(ru) + 0.5) * foam.cell)) / foam.wakeCell) - 0.5);
      let pz = (((foam.wakeRelZ + ((f32(rv) + 0.5) * foam.cell)) / foam.wakeCell) - 0.5);
      let x0 = floor(px);
      let z0 = floor(pz);
      let fx = (px - x0);
      let fz = (pz - z0);
      var turb = 0.0;
      var theta = 0.0;
      for (var c : u32 = 0u; (c < 4u); c = (c + 1u)) {
        var cu = i32(x0);
        var cv = i32(z0);
        var wgt = ((1.0 - fx) * (1.0 - fz));
        if ((c == 1u)) {
          cu = (cu + 1);
          wgt = (fx * (1.0 - fz));
        }
        if ((c == 2u)) {
          cv = (cv + 1);
          wgt = ((1.0 - fx) * fz);
        }
        if ((c == 3u)) {
          cu = (cu + 1);
          cv = (cv + 1);
          wgt = (fx * fz);
        }
        if ((cu >= 0)) {
          if ((cu < ww)) {
            if ((cv >= 0)) {
              if ((cv < ww)) {
                let bu = ((cu + foam.wakeOriginX) % ww);
                let bv = ((cv + foam.wakeOriginZ) % ww);
                let wc = wakeIn[u32(((bv * ww) + bu))];
                turb = (turb + (wgt * wc.z));
                if ((wgt >= 0.25)) {
                  theta = wc.w;
                }
              }
            }
          }
        }
      }
      let fwx = sin(theta);
      let fwz = cos(theta);
      let gx = ((f32((foam.originX + i32(ru))) + 0.5) * foam.cell);
      let gz = ((f32((foam.originZ + i32(rv))) + 0.5) * foam.cell);
      let ga = ((gx * fwx) + (gz * fwz));
      let gb = ((gx * fwz) - (gz * fwx));
      var nsum = 0.0;
      for (var o : u32 = 0u; (o < 3u); o = (o + 1u)) {
        var sa = 16.0;
        var sb = 1.6;
        var amp = 0.35;
        if ((o == 1u)) {
          sa = 6.0;
          sb = 3.5;
          amp = 0.4;
        }
        if ((o == 2u)) {
          sa = 1.4;
          sb = 1.4;
          amp = 0.25;
        }
        let qa = (ga / sa);
        let qb = (gb / sb);
        let ia = floor(qa);
        let ib = floor(qb);
        let ta = (qa - ia);
        let tb = (qb - ib);
        let ea = ((ta * ta) * (3.0 - (2.0 * ta)));
        let eb = ((tb * tb) * (3.0 - (2.0 * tb)));
        let ma = (ia - (289.0 * floor((ia / 289.0))));
        let mb = (ib - (289.0 * floor((ib / 289.0))));
        let ph = (37.719 * f32(o));
        let h00 = fract((sin((((12.9898 * ma) + (78.233 * mb)) + ph)) * 43758.5453));
        let h10 = fract((sin((((12.9898 * (ma + 1.0)) + (78.233 * mb)) + ph)) * 43758.5453));
        let h01 = fract((sin((((12.9898 * ma) + (78.233 * (mb + 1.0))) + ph)) * 43758.5453));
        let h11 = fract((sin((((12.9898 * (ma + 1.0)) + (78.233 * (mb + 1.0))) + ph)) * 43758.5453));
        nsum = (nsum + (amp * mix(mix(h00, h10, ea), mix(h01, h11, ea), eb)));
      }
      let nn = clamp(((nsum - 0.25) / 0.5), 0.0, 1.0);
      wf = clamp(((foam.wakeGain * turb) * (0.55 + (0.9 * nn))), 0.0, 1.0);
    }
  }
  foamOut[i] = f;
  textureStore(foamTex, vec2<i32>(vec2<u32>((i % w), (i / w))), vec4<f32>(f, jac, wf, 1.0));
}
`,entry:"ocean_foam",workgroup:[64,1,1],bindings:[{group:0,binding:0,name:"foam",kind:"uniform",elementType:"OceanFoamParams"},{group:0,binding:1,name:"foamIn",kind:"storage-read",elementType:"f32"},{group:0,binding:2,name:"fieldB0",kind:"storage-read",elementType:"vec4<f32>"},{group:0,binding:3,name:"fieldB1",kind:"storage-read",elementType:"vec4<f32>"},{group:0,binding:4,name:"fieldB2",kind:"storage-read",elementType:"vec4<f32>"},{group:0,binding:5,name:"fieldB3",kind:"storage-read",elementType:"vec4<f32>"},{group:0,binding:6,name:"foamOut",kind:"storage-rw",elementType:"f32"},{group:0,binding:7,name:"foamTex",kind:"storage-texture",format:"rgba16float",elementType:"vec4<f32>"},{group:0,binding:8,name:"wakeIn",kind:"storage-read",elementType:"vec4<f32>"}],uniforms:[{group:0,binding:0,name:"foam",struct:"OceanFoamParams",size:160,fields:[{name:"width",type:"u32",offset:0,size:4},{name:"cascades",type:"u32",offset:4,size:4},{name:"_pad0",type:"u32",offset:8,size:4},{name:"_pad1",type:"u32",offset:12,size:4},{name:"originX",type:"i32",offset:16,size:4},{name:"originZ",type:"i32",offset:20,size:4},{name:"shiftX",type:"i32",offset:24,size:4},{name:"shiftZ",type:"i32",offset:28,size:4},{name:"dt",type:"f32",offset:32,size:4},{name:"tau",type:"f32",offset:36,size:4},{name:"threshold",type:"f32",offset:40,size:4},{name:"gain",type:"f32",offset:44,size:4},{name:"lambda",type:"f32",offset:48,size:4},{name:"cell",type:"f32",offset:52,size:4},{name:"_pad2",type:"f32",offset:56,size:4},{name:"_pad3",type:"f32",offset:60,size:4},{name:"n0",type:"u32",offset:64,size:4},{name:"n1",type:"u32",offset:68,size:4},{name:"n2",type:"u32",offset:72,size:4},{name:"n3",type:"u32",offset:76,size:4},{name:"size0",type:"f32",offset:80,size:4},{name:"size1",type:"f32",offset:84,size:4},{name:"size2",type:"f32",offset:88,size:4},{name:"size3",type:"f32",offset:92,size:4},{name:"omX0",type:"f32",offset:96,size:4},{name:"omX1",type:"f32",offset:100,size:4},{name:"omX2",type:"f32",offset:104,size:4},{name:"omX3",type:"f32",offset:108,size:4},{name:"omZ0",type:"f32",offset:112,size:4},{name:"omZ1",type:"f32",offset:116,size:4},{name:"omZ2",type:"f32",offset:120,size:4},{name:"omZ3",type:"f32",offset:124,size:4},{name:"wakeWidth",type:"u32",offset:128,size:4},{name:"wakeGain",type:"f32",offset:132,size:4},{name:"wakeCell",type:"f32",offset:136,size:4},{name:"_pad4",type:"f32",offset:140,size:4},{name:"wakeRelX",type:"f32",offset:144,size:4},{name:"wakeRelZ",type:"f32",offset:148,size:4},{name:"wakeOriginX",type:"i32",offset:152,size:4},{name:"wakeOriginZ",type:"i32",offset:156,size:4}]}],glsl:`#version 300 es
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
  uint wakeWidth;
  float wakeGain;
  float wakeCell;
  float _pad4;
  float wakeRelX;
  float wakeRelZ;
  int wakeOriginX;
  int wakeOriginZ;
} foam;
uniform highp sampler2D foamIn_tex;
uniform uint foamIn_len;
uniform uint foamIn_w;
uniform highp sampler2D fieldB0_tex;
uniform uint fieldB0_len;
uniform uint fieldB0_w;
uniform highp sampler2D fieldB1_tex;
uniform uint fieldB1_len;
uniform uint fieldB1_w;
uniform highp sampler2D fieldB2_tex;
uniform uint fieldB2_len;
uniform uint fieldB2_w;
uniform highp sampler2D fieldB3_tex;
uniform uint fieldB3_len;
uniform uint fieldB3_w;
layout(location = 0) out vec4 foamOut_out;
layout(location = 1) out vec4 foamTex_out;
uniform highp sampler2D wakeIn_tex;
uniform uint wakeIn_len;
uniform uint wakeIn_w;

float foamIn_at(uint i) {
  return texelFetch(foamIn_tex, ivec2(int(i % foamIn_w), int(i / foamIn_w)), 0).x;
}

vec4 fieldB0_at(uint i) {
  return texelFetch(fieldB0_tex, ivec2(int(i % fieldB0_w), int(i / fieldB0_w)), 0);
}

vec4 fieldB1_at(uint i) {
  return texelFetch(fieldB1_tex, ivec2(int(i % fieldB1_w), int(i / fieldB1_w)), 0);
}

vec4 fieldB2_at(uint i) {
  return texelFetch(fieldB2_tex, ivec2(int(i % fieldB2_w), int(i / fieldB2_w)), 0);
}

vec4 fieldB3_at(uint i) {
  return texelFetch(fieldB3_tex, ivec2(int(i % fieldB3_w), int(i / fieldB3_w)), 0);
}

vec4 wakeIn_at(uint i) {
  return texelFetch(wakeIn_tex, ivec2(int(i % wakeIn_w), int(i / wakeIn_w)), 0);
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
  float wf = 0.0;
  if ((foam.wakeWidth > 0u)) {
    if ((foam.wakeGain > 0.0)) {
      int ww = int(foam.wakeWidth);
      float px = (((foam.wakeRelX + ((float(ru) + 0.5) * foam.cell)) / foam.wakeCell) - 0.5);
      float pz = (((foam.wakeRelZ + ((float(rv) + 0.5) * foam.cell)) / foam.wakeCell) - 0.5);
      float x0 = floor(px);
      float z0 = floor(pz);
      float fx = (px - x0);
      float fz = (pz - z0);
      float turb = 0.0;
      float theta = 0.0;
      for (uint c = 0u; (c < 4u); c = (c + 1u)) {
        int cu = int(x0);
        int cv = int(z0);
        float wgt = ((1.0 - fx) * (1.0 - fz));
        if ((c == 1u)) {
          cu = (cu + 1);
          wgt = (fx * (1.0 - fz));
        }
        if ((c == 2u)) {
          cv = (cv + 1);
          wgt = ((1.0 - fx) * fz);
        }
        if ((c == 3u)) {
          cu = (cu + 1);
          cv = (cv + 1);
          wgt = (fx * fz);
        }
        if ((cu >= 0)) {
          if ((cu < ww)) {
            if ((cv >= 0)) {
              if ((cv < ww)) {
                int bu = ((cu + foam.wakeOriginX) % ww);
                int bv = ((cv + foam.wakeOriginZ) % ww);
                vec4 wc = wakeIn_at(uint(((bv * ww) + bu)));
                turb = (turb + (wgt * wc.z));
                if ((wgt >= 0.25)) {
                  theta = wc.w;
                }
              }
            }
          }
        }
      }
      float fwx = sin(theta);
      float fwz = cos(theta);
      float gx = ((float((foam.originX + int(ru))) + 0.5) * foam.cell);
      float gz = ((float((foam.originZ + int(rv))) + 0.5) * foam.cell);
      float ga = ((gx * fwx) + (gz * fwz));
      float gb = ((gx * fwz) - (gz * fwx));
      float nsum = 0.0;
      for (uint o = 0u; (o < 3u); o = (o + 1u)) {
        float sa = 16.0;
        float sb = 1.6;
        float amp = 0.35;
        if ((o == 1u)) {
          sa = 6.0;
          sb = 3.5;
          amp = 0.4;
        }
        if ((o == 2u)) {
          sa = 1.4;
          sb = 1.4;
          amp = 0.25;
        }
        float qa = (ga / sa);
        float qb = (gb / sb);
        float ia = floor(qa);
        float ib = floor(qb);
        float ta = (qa - ia);
        float tb = (qb - ib);
        float ea = ((ta * ta) * (3.0 - (2.0 * ta)));
        float eb = ((tb * tb) * (3.0 - (2.0 * tb)));
        float ma = (ia - (289.0 * floor((ia / 289.0))));
        float mb = (ib - (289.0 * floor((ib / 289.0))));
        float ph = (37.719 * float(o));
        float h00 = fract((sin((((12.9898 * ma) + (78.233 * mb)) + ph)) * 43758.5453));
        float h10 = fract((sin((((12.9898 * (ma + 1.0)) + (78.233 * mb)) + ph)) * 43758.5453));
        float h01 = fract((sin((((12.9898 * ma) + (78.233 * (mb + 1.0))) + ph)) * 43758.5453));
        float h11 = fract((sin((((12.9898 * (ma + 1.0)) + (78.233 * (mb + 1.0))) + ph)) * 43758.5453));
        nsum = (nsum + (amp * mix(mix(h00, h10, ea), mix(h01, h11, ea), eb)));
      }
      float nn = clamp(((nsum - 0.25) / 0.5), 0.0, 1.0);
      wf = clamp(((foam.wakeGain * turb) * (0.55 + (0.9 * nn))), 0.0, 1.0);
    }
  }
  foamOut_out = vec4(f, 0.0, 0.0, 0.0);
  foamTex_out = vec4(f, jac, wf, 1.0);
}
`,gatherLayout:{kernel:"ocean_foam",inputs:[{name:"foamIn",kind:"buffer",elem:"f32",sampler:"foamIn_tex",length:"foamIn_len",width:"foamIn_w",binding:1},{name:"fieldB0",kind:"buffer",elem:"vec4",sampler:"fieldB0_tex",length:"fieldB0_len",width:"fieldB0_w",binding:2},{name:"fieldB1",kind:"buffer",elem:"vec4",sampler:"fieldB1_tex",length:"fieldB1_len",width:"fieldB1_w",binding:3},{name:"fieldB2",kind:"buffer",elem:"vec4",sampler:"fieldB2_tex",length:"fieldB2_len",width:"fieldB2_w",binding:4},{name:"fieldB3",kind:"buffer",elem:"vec4",sampler:"fieldB3_tex",length:"fieldB3_len",width:"fieldB3_w",binding:5},{name:"wakeIn",kind:"buffer",elem:"vec4",sampler:"wakeIn_tex",length:"wakeIn_len",width:"wakeIn_w",binding:8}],outputs:[{name:"foamOut",elem:"f32",format:"",location:0,variable:"foamOut_out"},{name:"foamTex",elem:"vec4",format:"rgba16float",location:1,variable:"foamTex_out"}],uniforms:[{name:"foam",block:"OceanFoamParams_ubo",struct:"OceanFoamParams",binding:0}]}},oceanProbe:{wgsl:`struct OceanProbeParams {
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
`,entry:"ocean_probe",workgroup:[64,1,1],bindings:[{group:0,binding:0,name:"probe",kind:"uniform",elementType:"OceanProbeParams"},{group:0,binding:1,name:"points",kind:"storage-read",elementType:"vec4<f32>"},{group:0,binding:2,name:"disp0",kind:"storage-read",elementType:"vec4<f32>"},{group:0,binding:3,name:"disp1",kind:"storage-read",elementType:"vec4<f32>"},{group:0,binding:4,name:"disp2",kind:"storage-read",elementType:"vec4<f32>"},{group:0,binding:5,name:"disp3",kind:"storage-read",elementType:"vec4<f32>"},{group:0,binding:6,name:"out",kind:"storage-rw",elementType:"vec4<f32>"}],uniforms:[{group:0,binding:0,name:"probe",struct:"OceanProbeParams",size:96,fields:[{name:"count",type:"u32",offset:0,size:4},{name:"cascades",type:"u32",offset:4,size:4},{name:"_pad0",type:"u32",offset:8,size:4},{name:"_pad1",type:"u32",offset:12,size:4},{name:"n0",type:"u32",offset:16,size:4},{name:"n1",type:"u32",offset:20,size:4},{name:"n2",type:"u32",offset:24,size:4},{name:"n3",type:"u32",offset:28,size:4},{name:"size0",type:"f32",offset:32,size:4},{name:"size1",type:"f32",offset:36,size:4},{name:"size2",type:"f32",offset:40,size:4},{name:"size3",type:"f32",offset:44,size:4},{name:"t",type:"f32",offset:48,size:4},{name:"normalStep",type:"f32",offset:52,size:4},{name:"lambda",type:"f32",offset:56,size:4},{name:"_pad3",type:"f32",offset:60,size:4},{name:"omX0",type:"f32",offset:64,size:4},{name:"omX1",type:"f32",offset:68,size:4},{name:"omX2",type:"f32",offset:72,size:4},{name:"omX3",type:"f32",offset:76,size:4},{name:"omZ0",type:"f32",offset:80,size:4},{name:"omZ1",type:"f32",offset:84,size:4},{name:"omZ2",type:"f32",offset:88,size:4},{name:"omZ3",type:"f32",offset:92,size:4}]}]},wakeSource:{wgsl:`struct WakeSourceParams {
  width : u32,
  reset : u32,
  _pad0 : u32,
  _pad1 : u32,
  originX : i32,
  originZ : i32,
  shiftX : i32,
  shiftZ : i32,
  cell : f32,
  dt : f32,
  tau : f32,
  depth : f32,
  hullU : f32,
  hullV : f32,
  heading : f32,
  speed : f32,
  hullLen : f32,
  hullBeam : f32,
  srcLength : f32,
  srcBeam : f32,
  fullSpeed : f32,
  foamSpeed : f32,
  diffuse : f32,
  jitter : f32,
  bowGain : f32,
  bowFoam : f32,
  _pad4 : f32,
  _pad5 : f32,
};

@group(0) @binding(0) var<uniform> wake : WakeSourceParams;
@group(0) @binding(1) var<storage, read> wakeIn : array<vec4<f32>>;
@group(0) @binding(2) var<storage, read_write> fftOut : array<vec4<f32>>;
@group(0) @binding(3) var<storage, read_write> cleanOut : array<vec4<f32>>;

@compute @workgroup_size(64)
fn wake_source(@builtin(global_invocation_id) gid : vec3<u32>) {
  let i = gid.x;
  let w = wake.width;
  if ((i >= (w * w))) {
    return;
  }
  let wi = i32(w);
  let ru = ((((i32((i % w)) - wake.originX) % wi) + wi) % wi);
  let rv = ((((i32((i / w)) - wake.originZ) % wi) + wi) % wi);
  var entered = (wake.reset != 0u);
  if ((wake.shiftX > 0)) {
    if ((ru >= (wi - wake.shiftX))) {
      entered = true;
    }
  }
  if ((wake.shiftX < 0)) {
    if ((ru < (0 - wake.shiftX))) {
      entered = true;
    }
  }
  if ((wake.shiftZ > 0)) {
    if ((rv >= (wi - wake.shiftZ))) {
      entered = true;
    }
  }
  if ((wake.shiftZ < 0)) {
    if ((rv < (0 - wake.shiftZ))) {
      entered = true;
    }
  }
  var st = wakeIn[i];
  if (entered) {
    st = vec4<f32>(0.0, 0.0, 0.0, 0.0);
  }
  let px = (((f32(ru) + 0.5) * wake.cell) - wake.hullU);
  let pz = (((f32(rv) + 0.5) * wake.cell) - wake.hullV);
  let fx = sin(wake.heading);
  let fz = cos(wake.heading);
  let along = ((px * fx) + (pz * fz));
  let across = ((px * fz) - (pz * fx));
  let bowShift = max(((0.5 * wake.hullLen) - (0.5 * wake.srcLength)), 0.0);
  let pa = ((along - bowShift) / (0.5 * wake.srcLength));
  let pb = (across / (0.5 * wake.srcBeam));
  let qs = ((pa * pa) + (pb * pb));
  var head = 0.0;
  if ((qs < 1.0)) {
    let fade = clamp((wake.speed / wake.fullSpeed), 0.0, 1.0);
    head = ((((wake.depth * fade) * fade) * (1.0 - qs)) * (1.0 - qs));
  }
  let bowLen = max((0.18 * wake.hullLen), (2.0 * wake.cell));
  let bowBeam = max((0.6 * wake.hullBeam), (1.5 * wake.cell));
  let ba = (((along - (0.5 * wake.hullLen)) + (0.5 * bowLen)) / (0.5 * bowLen));
  let bb = (across / (0.5 * bowBeam));
  let qbw = ((ba * ba) + (bb * bb));
  if ((qbw < 1.0)) {
    head = (head - (((((wake.bowGain * wake.speed) * wake.speed) / 19.62) * (1.0 - qbw)) * (1.0 - qbw)));
  }
  let uh = (along / (0.5 * wake.hullLen));
  let vh = (across / (0.5 * wake.hullBeam));
  let qh = ((uh * uh) + (vh * vh));
  var inj = 0.0;
  if ((qh < 1.0)) {
    inj = ((0.4 * (1.0 - qh)) * (1.0 - qh));
  }
  let su = ((uh + 0.9) / 0.4);
  let sv = (vh / 1.2);
  let qw = ((su * su) + (sv * sv));
  if ((qw < 1.0)) {
    inj = (inj + smoothstep(0.0, 1.0, ((1.0 - qw) * 1.7)));
  }
  let qx = (qw * 0.4);
  if ((qx < 1.0)) {
    inj = (inj + ((0.3 * (1.0 - qx)) * (1.0 - qx)));
  }
  let bu = ((uh - 0.8) / 0.35);
  let bv = (vh / 1.0);
  let qb = ((bu * bu) + (bv * bv));
  if ((qb < 1.0)) {
    inj = (inj + (((wake.bowFoam * smoothstep(4.5, 9.0, wake.speed)) * (1.0 - qb)) * (1.0 - qb)));
  }
  inj = min((inj * clamp((wake.speed / wake.foamSpeed), 0.0, 1.0)), 1.0);
  let cx = (i % w);
  let cz = (i / w);
  let tl = wakeIn[((cz * w) + (((cx + w) - 1u) % w))].z;
  let tr = wakeIn[((cz * w) + ((cx + 1u) % w))].z;
  let tu = wakeIn[(((((cz + w) - 1u) % w) * w) + cx)].z;
  let td = wakeIn[((((cz + 1u) % w) * w) + cx)].z;
  let ka = min(((wake.diffuse * wake.dt) / (wake.cell * wake.cell)), 0.2);
  var told = st.z;
  if (entered) {
    told = 0.0;
  }
  var tdiff = (told + (ka * ((((tl + tr) + tu) + td) - (4.0 * told))));
  if (entered) {
    tdiff = 0.0;
  }
  let wx = f32(((((wake.originX + i32(ru)) % 289) + 289) % 289));
  let wz = f32(((((wake.originZ + i32(rv)) % 289) + 289) % 289));
  let nz = fract((sin(((12.9898 * wx) + (78.233 * wz))) * 43758.5453));
  let decayed = (max(tdiff, 0.0) * exp((0.0 - ((wake.dt * (1.0 + (wake.jitter * nz))) / wake.tau))));
  let turb = max(decayed, inj);
  var theta = st.w;
  if ((inj > decayed)) {
    theta = (wake.heading - (3.141592653589793 * floor((wake.heading / 3.141592653589793))));
  }
  fftOut[i] = vec4<f32>((st.x + head), 0.0, 0.0, 0.0);
  cleanOut[i] = vec4<f32>(st.x, st.y, turb, theta);
}
`,entry:"wake_source",workgroup:[64,1,1],bindings:[{group:0,binding:0,name:"wake",kind:"uniform",elementType:"WakeSourceParams"},{group:0,binding:1,name:"wakeIn",kind:"storage-read",elementType:"vec4<f32>"},{group:0,binding:2,name:"fftOut",kind:"storage-rw",elementType:"vec4<f32>"},{group:0,binding:3,name:"cleanOut",kind:"storage-rw",elementType:"vec4<f32>"}],uniforms:[{group:0,binding:0,name:"wake",struct:"WakeSourceParams",size:112,fields:[{name:"width",type:"u32",offset:0,size:4},{name:"reset",type:"u32",offset:4,size:4},{name:"_pad0",type:"u32",offset:8,size:4},{name:"_pad1",type:"u32",offset:12,size:4},{name:"originX",type:"i32",offset:16,size:4},{name:"originZ",type:"i32",offset:20,size:4},{name:"shiftX",type:"i32",offset:24,size:4},{name:"shiftZ",type:"i32",offset:28,size:4},{name:"cell",type:"f32",offset:32,size:4},{name:"dt",type:"f32",offset:36,size:4},{name:"tau",type:"f32",offset:40,size:4},{name:"depth",type:"f32",offset:44,size:4},{name:"hullU",type:"f32",offset:48,size:4},{name:"hullV",type:"f32",offset:52,size:4},{name:"heading",type:"f32",offset:56,size:4},{name:"speed",type:"f32",offset:60,size:4},{name:"hullLen",type:"f32",offset:64,size:4},{name:"hullBeam",type:"f32",offset:68,size:4},{name:"srcLength",type:"f32",offset:72,size:4},{name:"srcBeam",type:"f32",offset:76,size:4},{name:"fullSpeed",type:"f32",offset:80,size:4},{name:"foamSpeed",type:"f32",offset:84,size:4},{name:"diffuse",type:"f32",offset:88,size:4},{name:"jitter",type:"f32",offset:92,size:4},{name:"bowGain",type:"f32",offset:96,size:4},{name:"bowFoam",type:"f32",offset:100,size:4},{name:"_pad4",type:"f32",offset:104,size:4},{name:"_pad5",type:"f32",offset:108,size:4}]}],glsl:`#version 300 es
precision highp float;
precision highp int;
precision highp sampler2D;

uniform uint elio_width;

layout(std140) uniform WakeSourceParams_ubo {
  uint width;
  uint reset;
  uint _pad0;
  uint _pad1;
  int originX;
  int originZ;
  int shiftX;
  int shiftZ;
  float cell;
  float dt;
  float tau;
  float depth;
  float hullU;
  float hullV;
  float heading;
  float speed;
  float hullLen;
  float hullBeam;
  float srcLength;
  float srcBeam;
  float fullSpeed;
  float foamSpeed;
  float diffuse;
  float jitter;
  float bowGain;
  float bowFoam;
  float _pad4;
  float _pad5;
} wake;
uniform highp sampler2D wakeIn_tex;
uniform uint wakeIn_len;
uniform uint wakeIn_w;
layout(location = 0) out vec4 fftOut_out;
layout(location = 1) out vec4 cleanOut_out;

vec4 wakeIn_at(uint i) {
  return texelFetch(wakeIn_tex, ivec2(int(i % wakeIn_w), int(i / wakeIn_w)), 0);
}

void main() {
  fftOut_out = vec4(0.0);
  cleanOut_out = vec4(0.0);
  uint i = (uint(gl_FragCoord.y) * elio_width + uint(gl_FragCoord.x));
  uint w = wake.width;
  if ((i >= (w * w))) {
    return;
  }
  int wi = int(w);
  int ru = ((((int((i % w)) - wake.originX) % wi) + wi) % wi);
  int rv = ((((int((i / w)) - wake.originZ) % wi) + wi) % wi);
  bool entered = (wake.reset != 0u);
  if ((wake.shiftX > 0)) {
    if ((ru >= (wi - wake.shiftX))) {
      entered = true;
    }
  }
  if ((wake.shiftX < 0)) {
    if ((ru < (0 - wake.shiftX))) {
      entered = true;
    }
  }
  if ((wake.shiftZ > 0)) {
    if ((rv >= (wi - wake.shiftZ))) {
      entered = true;
    }
  }
  if ((wake.shiftZ < 0)) {
    if ((rv < (0 - wake.shiftZ))) {
      entered = true;
    }
  }
  vec4 st = wakeIn_at(i);
  if (entered) {
    st = vec4(0.0, 0.0, 0.0, 0.0);
  }
  float px = (((float(ru) + 0.5) * wake.cell) - wake.hullU);
  float pz = (((float(rv) + 0.5) * wake.cell) - wake.hullV);
  float fx = sin(wake.heading);
  float fz = cos(wake.heading);
  float along = ((px * fx) + (pz * fz));
  float across = ((px * fz) - (pz * fx));
  float bowShift = max(((0.5 * wake.hullLen) - (0.5 * wake.srcLength)), 0.0);
  float pa = ((along - bowShift) / (0.5 * wake.srcLength));
  float pb = (across / (0.5 * wake.srcBeam));
  float qs = ((pa * pa) + (pb * pb));
  float head = 0.0;
  if ((qs < 1.0)) {
    float fade = clamp((wake.speed / wake.fullSpeed), 0.0, 1.0);
    head = ((((wake.depth * fade) * fade) * (1.0 - qs)) * (1.0 - qs));
  }
  float bowLen = max((0.18 * wake.hullLen), (2.0 * wake.cell));
  float bowBeam = max((0.6 * wake.hullBeam), (1.5 * wake.cell));
  float ba = (((along - (0.5 * wake.hullLen)) + (0.5 * bowLen)) / (0.5 * bowLen));
  float bb = (across / (0.5 * bowBeam));
  float qbw = ((ba * ba) + (bb * bb));
  if ((qbw < 1.0)) {
    head = (head - (((((wake.bowGain * wake.speed) * wake.speed) / 19.62) * (1.0 - qbw)) * (1.0 - qbw)));
  }
  float uh = (along / (0.5 * wake.hullLen));
  float vh = (across / (0.5 * wake.hullBeam));
  float qh = ((uh * uh) + (vh * vh));
  float inj = 0.0;
  if ((qh < 1.0)) {
    inj = ((0.4 * (1.0 - qh)) * (1.0 - qh));
  }
  float su = ((uh + 0.9) / 0.4);
  float sv = (vh / 1.2);
  float qw = ((su * su) + (sv * sv));
  if ((qw < 1.0)) {
    inj = (inj + smoothstep(0.0, 1.0, ((1.0 - qw) * 1.7)));
  }
  float qx = (qw * 0.4);
  if ((qx < 1.0)) {
    inj = (inj + ((0.3 * (1.0 - qx)) * (1.0 - qx)));
  }
  float bu = ((uh - 0.8) / 0.35);
  float bv = (vh / 1.0);
  float qb = ((bu * bu) + (bv * bv));
  if ((qb < 1.0)) {
    inj = (inj + (((wake.bowFoam * smoothstep(4.5, 9.0, wake.speed)) * (1.0 - qb)) * (1.0 - qb)));
  }
  inj = min((inj * clamp((wake.speed / wake.foamSpeed), 0.0, 1.0)), 1.0);
  uint cx = (i % w);
  uint cz = (i / w);
  float tl = wakeIn_at(((cz * w) + (((cx + w) - 1u) % w))).z;
  float tr = wakeIn_at(((cz * w) + ((cx + 1u) % w))).z;
  float tu = wakeIn_at((((((cz + w) - 1u) % w) * w) + cx)).z;
  float td = wakeIn_at(((((cz + 1u) % w) * w) + cx)).z;
  float ka = min(((wake.diffuse * wake.dt) / (wake.cell * wake.cell)), 0.2);
  float told = st.z;
  if (entered) {
    told = 0.0;
  }
  float tdiff = (told + (ka * ((((tl + tr) + tu) + td) - (4.0 * told))));
  if (entered) {
    tdiff = 0.0;
  }
  float wx = float(((((wake.originX + int(ru)) % 289) + 289) % 289));
  float wz = float(((((wake.originZ + int(rv)) % 289) + 289) % 289));
  float nz = fract((sin(((12.9898 * wx) + (78.233 * wz))) * 43758.5453));
  float decayed = (max(tdiff, 0.0) * exp((0.0 - ((wake.dt * (1.0 + (wake.jitter * nz))) / wake.tau))));
  float turb = max(decayed, inj);
  float theta = st.w;
  if ((inj > decayed)) {
    theta = (wake.heading - (3.141592653589793 * floor((wake.heading / 3.141592653589793))));
  }
  fftOut_out = vec4((st.x + head), 0.0, 0.0, 0.0);
  cleanOut_out = vec4(st.x, st.y, turb, theta);
}
`,gatherLayout:{kernel:"wake_source",inputs:[{name:"wakeIn",kind:"buffer",elem:"vec4",sampler:"wakeIn_tex",length:"wakeIn_len",width:"wakeIn_w",binding:1}],outputs:[{name:"fftOut",elem:"vec4",format:"",location:0,variable:"fftOut_out"},{name:"cleanOut",elem:"vec4",format:"",location:1,variable:"cleanOut_out"}],uniforms:[{name:"wake",block:"WakeSourceParams_ubo",struct:"WakeSourceParams",binding:0}]}},wakeSymbol:{wgsl:`struct WakeSymbolParams {
  n : u32,
  _pad0 : u32,
  _pad1 : u32,
  _pad2 : u32,
  window : f32,
  _pad3 : f32,
  _pad4 : f32,
  _pad5 : f32,
};

@group(0) @binding(0) var<uniform> sym : WakeSymbolParams;
@group(0) @binding(1) var<storage, read> fftIn : array<vec4<f32>>;
@group(0) @binding(2) var<storage, read_write> fftOut : array<vec4<f32>>;

@compute @workgroup_size(64)
fn wake_symbol(@builtin(global_invocation_id) gid : vec3<u32>) {
  let i = gid.x;
  let n = sym.n;
  if ((i >= (n * n))) {
    return;
  }
  let ix = (i % n);
  let iz = (i / n);
  var sx = i32(ix);
  var sz = i32(iz);
  if ((ix > (n / 2u))) {
    sx = (sx - i32(n));
  }
  if ((iz > (n / 2u))) {
    sz = (sz - i32(n));
  }
  var gain = 0.0;
  if ((ix != (n / 2u))) {
    if ((iz != (n / 2u))) {
      let kx = ((6.283185307179586 * f32(sx)) / sym.window);
      let kz = ((6.283185307179586 * f32(sz)) / sym.window);
      gain = (sqrt(((kx * kx) + (kz * kz))) / (f32(n) * f32(n)));
    }
  }
  let u = fftIn[i];
  fftOut[i] = vec4<f32>((gain * u.x), (0.0 - (gain * u.y)), 0.0, 0.0);
}
`,entry:"wake_symbol",workgroup:[64,1,1],bindings:[{group:0,binding:0,name:"sym",kind:"uniform",elementType:"WakeSymbolParams"},{group:0,binding:1,name:"fftIn",kind:"storage-read",elementType:"vec4<f32>"},{group:0,binding:2,name:"fftOut",kind:"storage-rw",elementType:"vec4<f32>"}],uniforms:[{group:0,binding:0,name:"sym",struct:"WakeSymbolParams",size:32,fields:[{name:"n",type:"u32",offset:0,size:4},{name:"_pad0",type:"u32",offset:4,size:4},{name:"_pad1",type:"u32",offset:8,size:4},{name:"_pad2",type:"u32",offset:12,size:4},{name:"window",type:"f32",offset:16,size:4},{name:"_pad3",type:"f32",offset:20,size:4},{name:"_pad4",type:"f32",offset:24,size:4},{name:"_pad5",type:"f32",offset:28,size:4}]}],glsl:`#version 300 es
precision highp float;
precision highp int;
precision highp sampler2D;

uniform uint elio_width;

layout(std140) uniform WakeSymbolParams_ubo {
  uint n;
  uint _pad0;
  uint _pad1;
  uint _pad2;
  float window;
  float _pad3;
  float _pad4;
  float _pad5;
} sym;
uniform highp sampler2D fftIn_tex;
uniform uint fftIn_len;
uniform uint fftIn_w;
layout(location = 0) out vec4 fftOut_out;

vec4 fftIn_at(uint i) {
  return texelFetch(fftIn_tex, ivec2(int(i % fftIn_w), int(i / fftIn_w)), 0);
}

void main() {
  fftOut_out = vec4(0.0);
  uint i = (uint(gl_FragCoord.y) * elio_width + uint(gl_FragCoord.x));
  uint n = sym.n;
  if ((i >= (n * n))) {
    return;
  }
  uint ix = (i % n);
  uint iz = (i / n);
  int sx = int(ix);
  int sz = int(iz);
  if ((ix > (n / 2u))) {
    sx = (sx - int(n));
  }
  if ((iz > (n / 2u))) {
    sz = (sz - int(n));
  }
  float gain = 0.0;
  if ((ix != (n / 2u))) {
    if ((iz != (n / 2u))) {
      float kx = ((6.283185307179586 * float(sx)) / sym.window);
      float kz = ((6.283185307179586 * float(sz)) / sym.window);
      gain = (sqrt(((kx * kx) + (kz * kz))) / (float(n) * float(n)));
    }
  }
  vec4 u = fftIn_at(i);
  fftOut_out = vec4((gain * u.x), (0.0 - (gain * u.y)), 0.0, 0.0);
}
`,gatherLayout:{kernel:"wake_symbol",inputs:[{name:"fftIn",kind:"buffer",elem:"vec4",sampler:"fftIn_tex",length:"fftIn_len",width:"fftIn_w",binding:1}],outputs:[{name:"fftOut",elem:"vec4",format:"",location:0,variable:"fftOut_out"}],uniforms:[{name:"sym",block:"WakeSymbolParams_ubo",struct:"WakeSymbolParams",binding:0}]}},wakeStep:{wgsl:`struct WakeStepParams {
  width : u32,
  _pad0 : u32,
  _pad1 : u32,
  _pad2 : u32,
  originX : i32,
  originZ : i32,
  sponge : f32,
  spongeRate : f32,
  dt : f32,
  gravity : f32,
  alpha : f32,
  cell : f32,
};

@group(0) @binding(0) var<uniform> wstep : WakeStepParams;
@group(0) @binding(1) var<storage, read> qIn : array<vec4<f32>>;
@group(0) @binding(2) var<storage, read> cleanIn : array<vec4<f32>>;
@group(0) @binding(3) var<storage, read_write> stateOut : array<vec4<f32>>;
@group(0) @binding(4) var wakeTex : texture_storage_2d<rgba16float, write>;

@compute @workgroup_size(64)
fn wake_step(@builtin(global_invocation_id) gid : vec3<u32>) {
  let i = gid.x;
  let w = wstep.width;
  if ((i >= (w * w))) {
    return;
  }
  let wi = i32(w);
  let cx = i32((i % w));
  let cz = i32((i / w));
  var h0 = 0.0;
  var h1 = 0.0;
  var h2 = 0.0;
  var h3 = 0.0;
  var h4 = 0.0;
  for (var c : u32 = 0u; (c < 5u); c = (c + 1u)) {
    var ox : i32 = 0;
    var oz : i32 = 0;
    if ((c == 1u)) {
      ox = 1;
    }
    if ((c == 2u)) {
      ox = (0 - 1);
    }
    if ((c == 3u)) {
      oz = 1;
    }
    if ((c == 4u)) {
      oz = (0 - 1);
    }
    let xi = (((cx + ox) + wi) % wi);
    let zi = (((cz + oz) + wi) % wi);
    let j = u32(((zi * wi) + xi));
    let ru = ((((xi - wstep.originX) % wi) + wi) % wi);
    let rv = ((((zi - wstep.originZ) % wi) + wi) % wi);
    let edge = min(min(ru, rv), min(((wi - 1) - ru), ((wi - 1) - rv)));
    let t = clamp(((wstep.sponge - f32(edge)) / max(wstep.sponge, 0.001)), 0.0, 1.0);
    let beta = max((1.0 - ((wstep.alpha + ((wstep.spongeRate * t) * t)) * wstep.dt)), 0.0);
    let s = cleanIn[j];
    let hn = ((s.x + (beta * (s.x - s.y))) - (((wstep.dt * wstep.dt) * wstep.gravity) * qIn[j].x));
    if ((c == 0u)) {
      h0 = hn;
    }
    if ((c == 1u)) {
      h1 = hn;
    }
    if ((c == 2u)) {
      h2 = hn;
    }
    if ((c == 3u)) {
      h3 = hn;
    }
    if ((c == 4u)) {
      h4 = hn;
    }
  }
  let own = cleanIn[i];
  stateOut[i] = vec4<f32>(h0, own.x, own.z, own.w);
  textureStore(wakeTex, vec2<i32>(vec2<u32>((i % w), (i / w))), vec4<f32>(h0, ((h1 - h2) / (2.0 * wstep.cell)), ((h3 - h4) / (2.0 * wstep.cell)), own.z));
}
`,entry:"wake_step",workgroup:[64,1,1],bindings:[{group:0,binding:0,name:"wstep",kind:"uniform",elementType:"WakeStepParams"},{group:0,binding:1,name:"qIn",kind:"storage-read",elementType:"vec4<f32>"},{group:0,binding:2,name:"cleanIn",kind:"storage-read",elementType:"vec4<f32>"},{group:0,binding:3,name:"stateOut",kind:"storage-rw",elementType:"vec4<f32>"},{group:0,binding:4,name:"wakeTex",kind:"storage-texture",format:"rgba16float",elementType:"vec4<f32>"}],uniforms:[{group:0,binding:0,name:"wstep",struct:"WakeStepParams",size:48,fields:[{name:"width",type:"u32",offset:0,size:4},{name:"_pad0",type:"u32",offset:4,size:4},{name:"_pad1",type:"u32",offset:8,size:4},{name:"_pad2",type:"u32",offset:12,size:4},{name:"originX",type:"i32",offset:16,size:4},{name:"originZ",type:"i32",offset:20,size:4},{name:"sponge",type:"f32",offset:24,size:4},{name:"spongeRate",type:"f32",offset:28,size:4},{name:"dt",type:"f32",offset:32,size:4},{name:"gravity",type:"f32",offset:36,size:4},{name:"alpha",type:"f32",offset:40,size:4},{name:"cell",type:"f32",offset:44,size:4}]}],glsl:`#version 300 es
precision highp float;
precision highp int;
precision highp sampler2D;

uniform uint elio_width;

layout(std140) uniform WakeStepParams_ubo {
  uint width;
  uint _pad0;
  uint _pad1;
  uint _pad2;
  int originX;
  int originZ;
  float sponge;
  float spongeRate;
  float dt;
  float gravity;
  float alpha;
  float cell;
} wstep;
uniform highp sampler2D qIn_tex;
uniform uint qIn_len;
uniform uint qIn_w;
uniform highp sampler2D cleanIn_tex;
uniform uint cleanIn_len;
uniform uint cleanIn_w;
layout(location = 0) out vec4 stateOut_out;
layout(location = 1) out vec4 wakeTex_out;

vec4 qIn_at(uint i) {
  return texelFetch(qIn_tex, ivec2(int(i % qIn_w), int(i / qIn_w)), 0);
}

vec4 cleanIn_at(uint i) {
  return texelFetch(cleanIn_tex, ivec2(int(i % cleanIn_w), int(i / cleanIn_w)), 0);
}

void main() {
  stateOut_out = vec4(0.0);
  wakeTex_out = vec4(0.0);
  uint i = (uint(gl_FragCoord.y) * elio_width + uint(gl_FragCoord.x));
  uint w = wstep.width;
  if ((i >= (w * w))) {
    return;
  }
  int wi = int(w);
  int cx = int((i % w));
  int cz = int((i / w));
  float h0 = 0.0;
  float h1 = 0.0;
  float h2 = 0.0;
  float h3 = 0.0;
  float h4 = 0.0;
  for (uint c = 0u; (c < 5u); c = (c + 1u)) {
    int ox = 0;
    int oz = 0;
    if ((c == 1u)) {
      ox = 1;
    }
    if ((c == 2u)) {
      ox = (0 - 1);
    }
    if ((c == 3u)) {
      oz = 1;
    }
    if ((c == 4u)) {
      oz = (0 - 1);
    }
    int xi = (((cx + ox) + wi) % wi);
    int zi = (((cz + oz) + wi) % wi);
    uint j = uint(((zi * wi) + xi));
    int ru = ((((xi - wstep.originX) % wi) + wi) % wi);
    int rv = ((((zi - wstep.originZ) % wi) + wi) % wi);
    int edge = min(min(ru, rv), min(((wi - 1) - ru), ((wi - 1) - rv)));
    float t = clamp(((wstep.sponge - float(edge)) / max(wstep.sponge, 0.001)), 0.0, 1.0);
    float beta = max((1.0 - ((wstep.alpha + ((wstep.spongeRate * t) * t)) * wstep.dt)), 0.0);
    vec4 s = cleanIn_at(j);
    float hn = ((s.x + (beta * (s.x - s.y))) - (((wstep.dt * wstep.dt) * wstep.gravity) * qIn_at(j).x));
    if ((c == 0u)) {
      h0 = hn;
    }
    if ((c == 1u)) {
      h1 = hn;
    }
    if ((c == 2u)) {
      h2 = hn;
    }
    if ((c == 3u)) {
      h3 = hn;
    }
    if ((c == 4u)) {
      h4 = hn;
    }
  }
  vec4 own = cleanIn_at(i);
  stateOut_out = vec4(h0, own.x, own.z, own.w);
  wakeTex_out = vec4(h0, ((h1 - h2) / (2.0 * wstep.cell)), ((h3 - h4) / (2.0 * wstep.cell)), own.z);
}
`,gatherLayout:{kernel:"wake_step",inputs:[{name:"qIn",kind:"buffer",elem:"vec4",sampler:"qIn_tex",length:"qIn_len",width:"qIn_w",binding:1},{name:"cleanIn",kind:"buffer",elem:"vec4",sampler:"cleanIn_tex",length:"cleanIn_len",width:"cleanIn_w",binding:2}],outputs:[{name:"stateOut",elem:"vec4",format:"",location:0,variable:"stateOut_out"},{name:"wakeTex",elem:"vec4",format:"rgba16float",location:1,variable:"wakeTex_out"}],uniforms:[{name:"wstep",block:"WakeStepParams_ubo",struct:"WakeStepParams",binding:0}]}},fftCompute64:{wgsl:`@group(0) @binding(0) var<storage, read_write> data : array<vec4<f32>>;

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
uniform uint src_w;
layout(location = 0) out vec4 dst_out;

vec4 src_at(uint i) {
  return texelFetch(src_tex, ivec2(int(i % src_w), int(i / src_w)), 0);
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
`,gatherLayout:{kernel:"fft_stage",inputs:[{name:"src",kind:"buffer",elem:"vec4",sampler:"src_tex",length:"src_len",width:"src_w",binding:1}],outputs:[{name:"dst",elem:"vec4",format:"",location:0,variable:"dst_out"}],uniforms:[{name:"fft",block:"FFTStage_ubo",struct:"FFTStage",binding:0}]}},fftCompute128:{wgsl:`@group(0) @binding(0) var<storage, read_write> data : array<vec4<f32>>;

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
uniform uint src_w;
layout(location = 0) out vec4 dst_out;

vec4 src_at(uint i) {
  return texelFetch(src_tex, ivec2(int(i % src_w), int(i / src_w)), 0);
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
`,gatherLayout:{kernel:"fft_stage",inputs:[{name:"src",kind:"buffer",elem:"vec4",sampler:"src_tex",length:"src_len",width:"src_w",binding:1}],outputs:[{name:"dst",elem:"vec4",format:"",location:0,variable:"dst_out"}],uniforms:[{name:"fft",block:"FFTStage_ubo",struct:"FFTStage",binding:0}]}},fftCompute256:{wgsl:`@group(0) @binding(0) var<storage, read_write> data : array<vec4<f32>>;

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
uniform uint src_w;
layout(location = 0) out vec4 dst_out;

vec4 src_at(uint i) {
  return texelFetch(src_tex, ivec2(int(i % src_w), int(i / src_w)), 0);
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
`,gatherLayout:{kernel:"fft_stage",inputs:[{name:"src",kind:"buffer",elem:"vec4",sampler:"src_tex",length:"src_len",width:"src_w",binding:1}],outputs:[{name:"dst",elem:"vec4",format:"",location:0,variable:"dst_out"}],uniforms:[{name:"fft",block:"FFTStage_ubo",struct:"FFTStage",binding:0}]}}},graphs:{"webgpu/high":{name:"ocean",backend:"webgpu",tier:"high",version:1,params:[{name:"windSpeed",type:"f32",source:{kind:"value",value:9},tween:20},{name:"fetch",type:"f32",source:{kind:"value",value:300},tween:20},{name:"gamma",type:"f32",source:{kind:"value",value:3.3},tween:20},{name:"spread",type:"f32",source:{kind:"value",value:8},tween:20},{name:"swell",type:"f32",source:{kind:"value",value:0},tween:20},{name:"windDirDeg",type:"f32",source:{kind:"value",value:0},tween:20},{name:"swellDirDeg",type:"f32",source:{kind:"value",value:0},tween:20},{name:"seed",type:"u32",source:{kind:"value",value:0}},{name:"loopPeriod",type:"f32",source:{kind:"value",value:1e3}},{name:"lambda",type:"f32",source:{kind:"value",value:1},tween:20},{name:"timeScale",type:"f32",source:{kind:"value",value:1}},{name:"driftX",type:"f32",source:{kind:"value",value:0}},{name:"driftZ",type:"f32",source:{kind:"value",value:0}},{name:"foamTau",type:"f32",source:{kind:"value",value:4}},{name:"foamThreshold",type:"f32",source:{kind:"value",value:.9}},{name:"foamGain",type:"f32",source:{kind:"value",value:2}},{name:"probeCount",type:"u32",source:{kind:"value",value:0}},{name:"probeOriginX",type:"f32",source:{kind:"value",value:0}},{name:"probeOriginZ",type:"f32",source:{kind:"value",value:0}},{name:"probePoints",type:"vec4<f32>[]",count:64,source:{kind:"value",value:0}},{name:"dt",type:"f32",source:{kind:"frame",name:"dt"}},{name:"cameraX",type:"f32",source:{kind:"frame",name:"cameraX"}},{name:"cameraZ",type:"f32",source:{kind:"frame",name:"cameraZ"}},{name:"windDir",type:"f32",source:{args:[1.5707963267948966,{op:"mul",args:["param:windDirDeg",.017453292519943295]}],kind:"expr",op:"sub"}},{name:"swellDir",type:"f32",source:{args:[1.5707963267948966,{op:"mul",args:["param:swellDirDeg",.017453292519943295]}],kind:"expr",op:"sub"}},{name:"simTime",type:"f32",source:{kind:"accumulate",of:"param:timeScale"}},{name:"phaseTime",type:"f32",source:{args:["param:simTime","param:loopPeriod"],kind:"expr",op:"mod"}},{name:"waterX",type:"f32",source:{kind:"accumulate",of:"param:driftX"}},{name:"waterZ",type:"f32",source:{kind:"accumulate",of:"param:driftZ"}},{name:"waterModX0",type:"f32",source:{args:["param:waterX",1600],kind:"expr",op:"mod"}},{name:"waterModZ0",type:"f32",source:{args:["param:waterZ",1600],kind:"expr",op:"mod"}},{name:"waterModX1",type:"f32",source:{args:["param:waterX",250],kind:"expr",op:"mod"}},{name:"waterModZ1",type:"f32",source:{args:["param:waterZ",250],kind:"expr",op:"mod"}},{name:"waterModX2",type:"f32",source:{args:["param:waterX",37],kind:"expr",op:"mod"}},{name:"waterModZ2",type:"f32",source:{args:["param:waterZ",37],kind:"expr",op:"mod"}},{name:"waterModX3",type:"f32",source:{args:["param:waterX",5.3],kind:"expr",op:"mod"}},{name:"waterModZ3",type:"f32",source:{args:["param:waterZ",5.3],kind:"expr",op:"mod"}},{name:"foamOriginX",type:"i32",source:{args:[{op:"floor",args:[{op:"div",args:[{op:"sub",args:["param:cameraX","param:waterX"]},.5]}]},512],kind:"expr",op:"sub"}},{name:"foamShiftX",type:"i32",source:{kind:"delta",of:"param:foamOriginX"}},{name:"foamOriginMod0X",type:"f32",source:{args:[{op:"mul",args:["param:foamOriginX",.5]},1600],kind:"expr",op:"mod"}},{name:"foamOriginMod1X",type:"f32",source:{args:[{op:"mul",args:["param:foamOriginX",.5]},250],kind:"expr",op:"mod"}},{name:"foamOriginMod2X",type:"f32",source:{args:[{op:"mul",args:["param:foamOriginX",.5]},37],kind:"expr",op:"mod"}},{name:"foamOriginMod3X",type:"f32",source:{args:[{op:"mul",args:["param:foamOriginX",.5]},5.3],kind:"expr",op:"mod"}},{name:"probeOriginMod0X",type:"f32",source:{args:[{op:"sub",args:["param:probeOriginX","param:waterX"]},1600],kind:"expr",op:"mod"}},{name:"probeOriginMod1X",type:"f32",source:{args:[{op:"sub",args:["param:probeOriginX","param:waterX"]},250],kind:"expr",op:"mod"}},{name:"probeOriginMod2X",type:"f32",source:{args:[{op:"sub",args:["param:probeOriginX","param:waterX"]},37],kind:"expr",op:"mod"}},{name:"probeOriginMod3X",type:"f32",source:{args:[{op:"sub",args:["param:probeOriginX","param:waterX"]},5.3],kind:"expr",op:"mod"}},{name:"foamOriginZ",type:"i32",source:{args:[{op:"floor",args:[{op:"div",args:[{op:"sub",args:["param:cameraZ","param:waterZ"]},.5]}]},512],kind:"expr",op:"sub"}},{name:"foamShiftZ",type:"i32",source:{kind:"delta",of:"param:foamOriginZ"}},{name:"foamOriginMod0Z",type:"f32",source:{args:[{op:"mul",args:["param:foamOriginZ",.5]},1600],kind:"expr",op:"mod"}},{name:"foamOriginMod1Z",type:"f32",source:{args:[{op:"mul",args:["param:foamOriginZ",.5]},250],kind:"expr",op:"mod"}},{name:"foamOriginMod2Z",type:"f32",source:{args:[{op:"mul",args:["param:foamOriginZ",.5]},37],kind:"expr",op:"mod"}},{name:"foamOriginMod3Z",type:"f32",source:{args:[{op:"mul",args:["param:foamOriginZ",.5]},5.3],kind:"expr",op:"mod"}},{name:"probeOriginMod0Z",type:"f32",source:{args:[{op:"sub",args:["param:probeOriginZ","param:waterZ"]},1600],kind:"expr",op:"mod"}},{name:"probeOriginMod1Z",type:"f32",source:{args:[{op:"sub",args:["param:probeOriginZ","param:waterZ"]},250],kind:"expr",op:"mod"}},{name:"probeOriginMod2Z",type:"f32",source:{args:[{op:"sub",args:["param:probeOriginZ","param:waterZ"]},37],kind:"expr",op:"mod"}},{name:"probeOriginMod3Z",type:"f32",source:{args:[{op:"sub",args:["param:probeOriginZ","param:waterZ"]},5.3],kind:"expr",op:"mod"}},{name:"hullX",type:"f32",source:{kind:"value",value:0}},{name:"hullZ",type:"f32",source:{kind:"value",value:0}},{name:"hullHeading",type:"f32",source:{kind:"value",value:0}},{name:"hullSpeed",type:"f32",source:{kind:"value",value:0}},{name:"wakeEnabled",type:"bool",source:{kind:"value",value:0}},{name:"hullLength",type:"f32",source:{kind:"value",value:200}},{name:"hullBeam",type:"f32",source:{kind:"value",value:30}},{name:"wakeCell",type:"f32",source:{kind:"value",value:4}},{name:"wakeDepth",type:"f32",source:{kind:"value",value:.4}},{name:"wakeGravity",type:"f32",source:{kind:"value",value:9.81}},{name:"wakeAlpha",type:"f32",source:{kind:"value",value:.02}},{name:"wakeMinLambdaCells",type:"f32",source:{kind:"value",value:8}},{name:"wakeFoamGain",type:"f32",source:{kind:"value",value:1}},{name:"wakeFoamTau",type:"f32",source:{kind:"value",value:48}},{name:"wakeDiffuse",type:"f32",source:{kind:"value",value:1.5}},{name:"wakeJitter",type:"f32",source:{kind:"value",value:1}},{name:"wakeBowGain",type:"f32",source:{kind:"value",value:.2}},{name:"wakeBowFoam",type:"f32",source:{kind:"value",value:.9}},{name:"wakeOn",type:"f32",source:{args:["param:wakeEnabled",1],kind:"expr",op:"mul"}},{name:"wakeFoamGainEff",type:"f32",source:{args:["param:wakeEnabled","param:wakeFoamGain"],kind:"expr",op:"mul"}},{name:"wakeEdge",type:"i32",source:{kind:"delta",of:"param:wakeEnabled"}},{name:"wakeReset",type:"u32",source:{args:["param:wakeEdge",0],kind:"expr",op:"max"}},{name:"wakeLength",type:"f32",source:{args:["param:wakeCell",256],kind:"expr",op:"mul"}},{name:"hullWaterX",type:"f32",source:{args:["param:hullX","param:waterX"],kind:"expr",op:"sub"}},{name:"wakeOriginX",type:"i32",source:{args:[{op:"div",args:[{op:"sub",args:[{op:"sub",args:["param:hullWaterX",{op:"mul",args:[{op:"sin",args:["param:hullHeading"]},{op:"mul",args:["param:wakeCell",51.2]}]}]},{op:"mul",args:["param:wakeCell",128]}]},"param:wakeCell"]}],kind:"expr",op:"floor"}},{name:"wakeShiftX",type:"i32",source:{kind:"delta",of:"param:wakeOriginX"}},{name:"wakeHullU",type:"f32",source:{args:["param:hullWaterX",{op:"mul",args:["param:wakeOriginX","param:wakeCell"]}],kind:"expr",op:"sub"}},{name:"wakeOriginModX",type:"i32",source:{args:["param:wakeOriginX",256],kind:"expr",op:"mod"}},{name:"wakeRelX",type:"f32",source:{args:[{op:"mul",args:["param:foamOriginX",.5]},{op:"mul",args:["param:wakeOriginX","param:wakeCell"]}],kind:"expr",op:"sub"}},{name:"wakeCamRelX",type:"f32",source:{args:[{op:"add",args:["param:waterX",{op:"mul",args:["param:wakeOriginX","param:wakeCell"]}]},"param:cameraX"],kind:"expr",op:"sub"}},{name:"hullWaterZ",type:"f32",source:{args:["param:hullZ","param:waterZ"],kind:"expr",op:"sub"}},{name:"wakeOriginZ",type:"i32",source:{args:[{op:"div",args:[{op:"sub",args:[{op:"sub",args:["param:hullWaterZ",{op:"mul",args:[{op:"cos",args:["param:hullHeading"]},{op:"mul",args:["param:wakeCell",51.2]}]}]},{op:"mul",args:["param:wakeCell",128]}]},"param:wakeCell"]}],kind:"expr",op:"floor"}},{name:"wakeShiftZ",type:"i32",source:{kind:"delta",of:"param:wakeOriginZ"}},{name:"wakeHullV",type:"f32",source:{args:["param:hullWaterZ",{op:"mul",args:["param:wakeOriginZ","param:wakeCell"]}],kind:"expr",op:"sub"}},{name:"wakeOriginModZ",type:"i32",source:{args:["param:wakeOriginZ",256],kind:"expr",op:"mod"}},{name:"wakeRelZ",type:"f32",source:{args:[{op:"mul",args:["param:foamOriginZ",.5]},{op:"mul",args:["param:wakeOriginZ","param:wakeCell"]}],kind:"expr",op:"sub"}},{name:"wakeCamRelZ",type:"f32",source:{args:[{op:"add",args:["param:waterZ",{op:"mul",args:["param:wakeOriginZ","param:wakeCell"]}]},"param:cameraZ"],kind:"expr",op:"sub"}},{name:"wakeGEff",type:"f32",source:{args:[{op:"min",args:[{op:"min",args:["param:wakeGravity",{op:"mul",args:[{op:"mul",args:["param:hullSpeed","param:hullSpeed"]},{op:"div",args:[6.283185307179586,{op:"mul",args:["param:wakeMinLambdaCells","param:wakeCell"]}]}]}]},{op:"mul",args:["param:wakeCell",16.39001041697031]}]},.05],kind:"expr",op:"max"}},{name:"wakeDt",type:"f32",source:{args:[{op:"mul",args:["param:dt","param:timeScale"]},.2],kind:"expr",op:"min"}},{name:"wakeSrcLength",type:"f32",source:{args:[{op:"min",args:["param:hullLength",{op:"div",args:[{op:"mul",args:[{op:"mul",args:["param:hullSpeed","param:hullSpeed"]},3.141592653589793]},"param:wakeGEff"]}]},{op:"mul",args:["param:wakeCell",2]}],kind:"expr",op:"max"}},{name:"wakeSrcBeam",type:"f32",source:{args:[{op:"min",args:["param:hullBeam",{op:"mul",args:["param:wakeSrcLength",.67]}]},{op:"mul",args:["param:wakeCell",1.5]}],kind:"expr",op:"max"}}],resources:[{name:"h0_0",kind:"buffer",elementType:"vec4<f32>",count:4096},{name:"omega_0",kind:"buffer",elementType:"f32",count:4096},{name:"fftA_0",kind:"buffer",elementType:"vec4<f32>",count:4096},{name:"fftB_0",kind:"buffer",elementType:"vec4<f32>",count:4096},{name:"dispTex0",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"slopeTex0",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"h0_1",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"omega_1",kind:"buffer",elementType:"f32",count:65536},{name:"fftA_1",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"fftB_1",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"dispTex1",kind:"texture",format:"rgba16float",width:256,height:256,mipLevels:9},{name:"slopeTex1",kind:"texture",format:"rgba16float",width:256,height:256,mipLevels:9},{name:"h0_2",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"omega_2",kind:"buffer",elementType:"f32",count:65536},{name:"fftA_2",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"fftB_2",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"dispTex2",kind:"texture",format:"rgba16float",width:256,height:256,mipLevels:9},{name:"slopeTex2",kind:"texture",format:"rgba16float",width:256,height:256,mipLevels:9},{name:"h0_3",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"omega_3",kind:"buffer",elementType:"f32",count:65536},{name:"fftA_3",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"fftB_3",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"dispTex3",kind:"texture",format:"rgba16float",width:256,height:256,mipLevels:9},{name:"slopeTex3",kind:"texture",format:"rgba16float",width:256,height:256,mipLevels:9},{name:"wakeA",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"wakeB",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"wakeF",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"wakeG",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"wakeC",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"wakeTex",kind:"texture",format:"rgba16float",width:256,height:256,mipLevels:9},{name:"foamA",kind:"buffer",elementType:"f32",count:1048576},{name:"foamB",kind:"buffer",elementType:"f32",count:1048576},{name:"foamTex",kind:"texture",format:"rgba16float",width:1024,height:1024,mipLevels:11},{name:"probePoints",kind:"buffer",elementType:"vec4<f32>",count:64,feed:"probePoints"},{name:"probeOut",kind:"buffer",elementType:"vec4<f32>",count:64}],passes:[{name:"c0.spectrum",kernel:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",dispatch:[64,1,1],bindings:[{name:"h0",resource:"h0_0"},{name:"omega",resource:"omega_0"}],uniforms:[{name:"spec",values:{cascade:0,fetchKm:"param:fetch",gamma:"param:gamma",kMax:.12,kMin:.003926990816987242,loopPeriod:"param:loopPeriod",n:64,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:1600,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c0.evolve",kernel:"ocean_evolve",dispatch:[64,1,1],bindings:[{name:"h0",resource:"h0_0"},{name:"omega",resource:"omega_0"},{name:"outA",resource:"fftA_0"},{name:"outB",resource:"fftB_0"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:64,t:"param:phaseTime",tile:1600}}]},{name:"c0.fftA.fft_rows",kernel:"fft_compute_64",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftA_0"}]},{name:"c0.fftA.fft_cols",kernel:"fft_compute_64",entry:"fft_cols",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftA_0"}]},{name:"c0.fftB.fft_rows",kernel:"fft_compute_64",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftB_0"}]},{name:"c0.fftB.fft_cols",kernel:"fft_compute_64",entry:"fft_cols",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftB_0"}]},{name:"c0.assemble",kernel:"ocean_assemble",dispatch:[64,1,1],bindings:[{name:"fieldA",resource:"fftA_0"},{name:"fieldB",resource:"fftB_0"},{name:"disp",resource:"dispTex0"},{name:"slope",resource:"slopeTex0"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:64}}]},{name:"c0.dispTex0.mips",type:"repeat",count:6,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[16,1,1],bindings:[{name:"src",resource:"dispTex0",levelAt:{index:"l"}},{name:"dst",resource:"dispTex0",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:64},dstWidth:{index:"l",offset:1,shr:64},pad:0,srcLevel:0}}]}]},{name:"c0.slopeTex0.mips",type:"repeat",count:6,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[16,1,1],bindings:[{name:"src",resource:"slopeTex0",levelAt:{index:"l"}},{name:"dst",resource:"slopeTex0",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:64},dstWidth:{index:"l",offset:1,shr:64},pad:0,srcLevel:0}}]}]},{name:"c1.spectrum",kernel:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",dispatch:[1024,1,1],bindings:[{name:"h0",resource:"h0_1"},{name:"omega",resource:"omega_1"}],uniforms:[{name:"spec",values:{cascade:1,fetchKm:"param:fetch",gamma:"param:gamma",kMax:.8,kMin:.12,loopPeriod:"param:loopPeriod",n:256,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:250,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c1.evolve",kernel:"ocean_evolve",dispatch:[1024,1,1],bindings:[{name:"h0",resource:"h0_1"},{name:"omega",resource:"omega_1"},{name:"outA",resource:"fftA_1"},{name:"outB",resource:"fftB_1"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:256,t:"param:phaseTime",tile:250}}]},{name:"c1.fftA.fft_rows",kernel:"fft_compute_256",dispatch:[256,1,1],bindings:[{name:"data",resource:"fftA_1"}]},{name:"c1.fftA.fft_cols",kernel:"fft_compute_256",entry:"fft_cols",dispatch:[256,1,1],bindings:[{name:"data",resource:"fftA_1"}]},{name:"c1.fftB.fft_rows",kernel:"fft_compute_256",dispatch:[256,1,1],bindings:[{name:"data",resource:"fftB_1"}]},{name:"c1.fftB.fft_cols",kernel:"fft_compute_256",entry:"fft_cols",dispatch:[256,1,1],bindings:[{name:"data",resource:"fftB_1"}]},{name:"c1.assemble",kernel:"ocean_assemble",dispatch:[1024,1,1],bindings:[{name:"fieldA",resource:"fftA_1"},{name:"fieldB",resource:"fftB_1"},{name:"disp",resource:"dispTex1"},{name:"slope",resource:"slopeTex1"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:256}}]},{name:"c1.dispTex1.mips",type:"repeat",count:8,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[256,1,1],bindings:[{name:"src",resource:"dispTex1",levelAt:{index:"l"}},{name:"dst",resource:"dispTex1",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:256},dstWidth:{index:"l",offset:1,shr:256},pad:0,srcLevel:0}}]}]},{name:"c1.slopeTex1.mips",type:"repeat",count:8,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[256,1,1],bindings:[{name:"src",resource:"slopeTex1",levelAt:{index:"l"}},{name:"dst",resource:"slopeTex1",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:256},dstWidth:{index:"l",offset:1,shr:256},pad:0,srcLevel:0}}]}]},{name:"c2.spectrum",kernel:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",dispatch:[1024,1,1],bindings:[{name:"h0",resource:"h0_2"},{name:"omega",resource:"omega_2"}],uniforms:[{name:"spec",values:{cascade:2,fetchKm:"param:fetch",gamma:"param:gamma",kMax:5.4,kMin:.8,loopPeriod:"param:loopPeriod",n:256,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:37,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c2.evolve",kernel:"ocean_evolve",dispatch:[1024,1,1],bindings:[{name:"h0",resource:"h0_2"},{name:"omega",resource:"omega_2"},{name:"outA",resource:"fftA_2"},{name:"outB",resource:"fftB_2"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:256,t:"param:phaseTime",tile:37}}]},{name:"c2.fftA.fft_rows",kernel:"fft_compute_256",dispatch:[256,1,1],bindings:[{name:"data",resource:"fftA_2"}]},{name:"c2.fftA.fft_cols",kernel:"fft_compute_256",entry:"fft_cols",dispatch:[256,1,1],bindings:[{name:"data",resource:"fftA_2"}]},{name:"c2.fftB.fft_rows",kernel:"fft_compute_256",dispatch:[256,1,1],bindings:[{name:"data",resource:"fftB_2"}]},{name:"c2.fftB.fft_cols",kernel:"fft_compute_256",entry:"fft_cols",dispatch:[256,1,1],bindings:[{name:"data",resource:"fftB_2"}]},{name:"c2.assemble",kernel:"ocean_assemble",dispatch:[1024,1,1],bindings:[{name:"fieldA",resource:"fftA_2"},{name:"fieldB",resource:"fftB_2"},{name:"disp",resource:"dispTex2"},{name:"slope",resource:"slopeTex2"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:256}}]},{name:"c2.dispTex2.mips",type:"repeat",count:8,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[256,1,1],bindings:[{name:"src",resource:"dispTex2",levelAt:{index:"l"}},{name:"dst",resource:"dispTex2",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:256},dstWidth:{index:"l",offset:1,shr:256},pad:0,srcLevel:0}}]}]},{name:"c2.slopeTex2.mips",type:"repeat",count:8,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[256,1,1],bindings:[{name:"src",resource:"slopeTex2",levelAt:{index:"l"}},{name:"dst",resource:"slopeTex2",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:256},dstWidth:{index:"l",offset:1,shr:256},pad:0,srcLevel:0}}]}]},{name:"c3.spectrum",kernel:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",dispatch:[1024,1,1],bindings:[{name:"h0",resource:"h0_3"},{name:"omega",resource:"omega_3"}],uniforms:[{name:"spec",values:{cascade:3,fetchKm:"param:fetch",gamma:"param:gamma",kMax:37,kMin:5.4,loopPeriod:"param:loopPeriod",n:256,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:5.3,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c3.evolve",kernel:"ocean_evolve",dispatch:[1024,1,1],bindings:[{name:"h0",resource:"h0_3"},{name:"omega",resource:"omega_3"},{name:"outA",resource:"fftA_3"},{name:"outB",resource:"fftB_3"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:256,t:"param:phaseTime",tile:5.3}}]},{name:"c3.fftA.fft_rows",kernel:"fft_compute_256",dispatch:[256,1,1],bindings:[{name:"data",resource:"fftA_3"}]},{name:"c3.fftA.fft_cols",kernel:"fft_compute_256",entry:"fft_cols",dispatch:[256,1,1],bindings:[{name:"data",resource:"fftA_3"}]},{name:"c3.fftB.fft_rows",kernel:"fft_compute_256",dispatch:[256,1,1],bindings:[{name:"data",resource:"fftB_3"}]},{name:"c3.fftB.fft_cols",kernel:"fft_compute_256",entry:"fft_cols",dispatch:[256,1,1],bindings:[{name:"data",resource:"fftB_3"}]},{name:"c3.assemble",kernel:"ocean_assemble",dispatch:[1024,1,1],bindings:[{name:"fieldA",resource:"fftA_3"},{name:"fieldB",resource:"fftB_3"},{name:"disp",resource:"dispTex3"},{name:"slope",resource:"slopeTex3"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:256}}]},{name:"c3.dispTex3.mips",type:"repeat",count:8,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[256,1,1],bindings:[{name:"src",resource:"dispTex3",levelAt:{index:"l"}},{name:"dst",resource:"dispTex3",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:256},dstWidth:{index:"l",offset:1,shr:256},pad:0,srcLevel:0}}]}]},{name:"c3.slopeTex3.mips",type:"repeat",count:8,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[256,1,1],bindings:[{name:"src",resource:"slopeTex3",levelAt:{index:"l"}},{name:"dst",resource:"slopeTex3",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:256},dstWidth:{index:"l",offset:1,shr:256},pad:0,srcLevel:0}}]}]},{name:"wake.source",kernel:"wake_source",when:"param:wakeEnabled",dispatch:[1024,1,1],bindings:[{name:"wakeIn",resource:"wakeA"},{name:"fftOut",resource:"wakeF"},{name:"cleanOut",resource:"wakeC"}],uniforms:[{name:"wake",values:{bowFoam:"param:wakeBowFoam",bowGain:"param:wakeBowGain",cell:"param:wakeCell",depth:"param:wakeDepth",diffuse:"param:wakeDiffuse",dt:"param:wakeDt",foamSpeed:5,fullSpeed:3,heading:"param:hullHeading",hullBeam:"param:hullBeam",hullLen:"param:hullLength",hullU:"param:wakeHullU",hullV:"param:wakeHullV",jitter:"param:wakeJitter",originX:"param:wakeOriginX",originZ:"param:wakeOriginZ",reset:"param:wakeReset",shiftX:"param:wakeShiftX",shiftZ:"param:wakeShiftZ",speed:"param:hullSpeed",srcBeam:"param:wakeSrcBeam",srcLength:"param:wakeSrcLength",tau:"param:wakeFoamTau",width:256}}]},{name:"wake.fft1.fft_rows",kernel:"fft_compute_256",when:"param:wakeEnabled",dispatch:[256,1,1],bindings:[{name:"data",resource:"wakeF"}]},{name:"wake.fft1.fft_cols",kernel:"fft_compute_256",entry:"fft_cols",when:"param:wakeEnabled",dispatch:[256,1,1],bindings:[{name:"data",resource:"wakeF"}]},{name:"wake.symbol",kernel:"wake_symbol",when:"param:wakeEnabled",dispatch:[1024,1,1],bindings:[{name:"fftIn",resource:"wakeF"},{name:"fftOut",resource:"wakeG"}],uniforms:[{name:"sym",values:{n:256,window:"param:wakeLength"}}]},{name:"wake.fft2.fft_rows",kernel:"fft_compute_256",when:"param:wakeEnabled",dispatch:[256,1,1],bindings:[{name:"data",resource:"wakeG"}]},{name:"wake.fft2.fft_cols",kernel:"fft_compute_256",entry:"fft_cols",when:"param:wakeEnabled",dispatch:[256,1,1],bindings:[{name:"data",resource:"wakeG"}]},{name:"wake.step",kernel:"wake_step",when:"param:wakeEnabled",dispatch:[1024,1,1],bindings:[{name:"qIn",resource:"wakeG"},{name:"cleanIn",resource:"wakeC"},{name:"stateOut",resource:"wakeB"},{name:"wakeTex",resource:"wakeTex"}],uniforms:[{name:"wstep",values:{alpha:"param:wakeAlpha",cell:"param:wakeCell",dt:"param:wakeDt",gravity:"param:wakeGEff",originX:"param:wakeOriginX",originZ:"param:wakeOriginZ",sponge:24,spongeRate:.8,width:256}}]},{name:"wake.wakeTex.mips",type:"repeat",when:"param:wakeEnabled",count:8,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[256,1,1],bindings:[{name:"src",resource:"wakeTex",levelAt:{index:"l"}},{name:"dst",resource:"wakeTex",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:256},dstWidth:{index:"l",offset:1,shr:256},pad:0,srcLevel:0}}]}]},{name:"foam",kernel:"ocean_foam",dispatch:[16384,1,1],bindings:[{name:"foamIn",resource:"foamA"},{name:"fieldB0",resource:"fftB_0"},{name:"fieldB1",resource:"fftB_1"},{name:"fieldB2",resource:"fftB_2"},{name:"fieldB3",resource:"fftB_3"},{name:"foamOut",resource:"foamB"},{name:"foamTex",resource:"foamTex"},{name:"wakeIn",resource:"wakeB"}],uniforms:[{name:"foam",values:{cascades:4,cell:.5,dt:"param:dt",gain:"param:foamGain",lambda:"param:lambda",n0:64,n1:256,n2:256,n3:256,omX0:"param:foamOriginMod0X",omX1:"param:foamOriginMod1X",omX2:"param:foamOriginMod2X",omX3:"param:foamOriginMod3X",omZ0:"param:foamOriginMod0Z",omZ1:"param:foamOriginMod1Z",omZ2:"param:foamOriginMod2Z",omZ3:"param:foamOriginMod3Z",originX:"param:foamOriginX",originZ:"param:foamOriginZ",shiftX:"param:foamShiftX",shiftZ:"param:foamShiftZ",size0:1600,size1:250,size2:37,size3:5.3,tau:"param:foamTau",threshold:"param:foamThreshold",wakeCell:"param:wakeCell",wakeGain:"param:wakeFoamGainEff",wakeOriginX:"param:wakeOriginModX",wakeOriginZ:"param:wakeOriginModZ",wakeRelX:"param:wakeRelX",wakeRelZ:"param:wakeRelZ",wakeWidth:256,width:1024}}]},{name:"foamTex.mips",type:"repeat",count:10,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[4096,1,1],bindings:[{name:"src",resource:"foamTex",levelAt:{index:"l"}},{name:"dst",resource:"foamTex",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:1024},dstWidth:{index:"l",offset:1,shr:1024},pad:0,srcLevel:0}}]}]},{name:"probe",kernel:"ocean_probe",dispatch:[1,1,1],bindings:[{name:"points",resource:"probePoints"},{name:"disp0",resource:"fftA_0"},{name:"disp1",resource:"fftA_1"},{name:"disp2",resource:"fftA_2"},{name:"disp3",resource:"fftA_3"},{name:"out",resource:"probeOut"}],uniforms:[{name:"probe",values:{cascades:4,count:"param:probeCount",lambda:"param:lambda",n0:64,n1:256,n2:256,n3:256,normalStep:.5,omX0:"param:probeOriginMod0X",omX1:"param:probeOriginMod1X",omX2:"param:probeOriginMod2X",omX3:"param:probeOriginMod3X",omZ0:"param:probeOriginMod0Z",omZ1:"param:probeOriginMod1Z",omZ2:"param:probeOriginMod2Z",omZ3:"param:probeOriginMod3Z",size0:1600,size1:250,size2:37,size3:5.3,t:"param:simTime"}}]}],frameSwaps:[["wakeA","wakeB"],["foamA","foamB"]],exports:{disp0:"dispTex0",disp1:"dispTex1",disp2:"dispTex2",disp3:"dispTex3",foam:"foamTex",probe:"probeOut",slope0:"slopeTex0",slope1:"slopeTex1",slope2:"slopeTex2",slope3:"slopeTex3",wake:"wakeTex"},exportParams:["waterModX0","waterModZ0","waterModX1","waterModZ1","waterModX2","waterModZ2","waterModX3","waterModZ3","wakeCamRelX","wakeCamRelZ","wakeOriginModX","wakeOriginModZ","wakeCell","wakeOn"]},"webgpu/medium":{name:"ocean",backend:"webgpu",tier:"medium",version:1,resources:[{name:"h0_0",kind:"buffer",elementType:"vec4<f32>",count:4096},{name:"omega_0",kind:"buffer",elementType:"f32",count:4096},{name:"fftA_0",kind:"buffer",elementType:"vec4<f32>",count:4096},{name:"fftB_0",kind:"buffer",elementType:"vec4<f32>",count:4096},{name:"dispTex0",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"slopeTex0",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"h0_1",kind:"buffer",elementType:"vec4<f32>",count:16384},{name:"omega_1",kind:"buffer",elementType:"f32",count:16384},{name:"fftA_1",kind:"buffer",elementType:"vec4<f32>",count:16384},{name:"fftB_1",kind:"buffer",elementType:"vec4<f32>",count:16384},{name:"dispTex1",kind:"texture",format:"rgba16float",width:128,height:128,mipLevels:8},{name:"slopeTex1",kind:"texture",format:"rgba16float",width:128,height:128,mipLevels:8},{name:"h0_2",kind:"buffer",elementType:"vec4<f32>",count:16384},{name:"omega_2",kind:"buffer",elementType:"f32",count:16384},{name:"fftA_2",kind:"buffer",elementType:"vec4<f32>",count:16384},{name:"fftB_2",kind:"buffer",elementType:"vec4<f32>",count:16384},{name:"dispTex2",kind:"texture",format:"rgba16float",width:128,height:128,mipLevels:8},{name:"slopeTex2",kind:"texture",format:"rgba16float",width:128,height:128,mipLevels:8},{name:"h0_3",kind:"buffer",elementType:"vec4<f32>",count:16384},{name:"omega_3",kind:"buffer",elementType:"f32",count:16384},{name:"fftA_3",kind:"buffer",elementType:"vec4<f32>",count:16384},{name:"fftB_3",kind:"buffer",elementType:"vec4<f32>",count:16384},{name:"dispTex3",kind:"texture",format:"rgba16float",width:128,height:128,mipLevels:8},{name:"slopeTex3",kind:"texture",format:"rgba16float",width:128,height:128,mipLevels:8},{name:"wakeA",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"wakeB",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"wakeF",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"wakeG",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"wakeC",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"wakeTex",kind:"texture",format:"rgba16float",width:256,height:256,mipLevels:9},{name:"foamA",kind:"buffer",elementType:"f32",count:1048576},{name:"foamB",kind:"buffer",elementType:"f32",count:1048576},{name:"foamTex",kind:"texture",format:"rgba16float",width:1024,height:1024,mipLevels:11},{name:"probePoints",kind:"buffer",elementType:"vec4<f32>",count:64,feed:"probePoints"},{name:"probeOut",kind:"buffer",elementType:"vec4<f32>",count:64}],passes:[{name:"c0.spectrum",kernel:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",dispatch:[64,1,1],bindings:[{name:"h0",resource:"h0_0"},{name:"omega",resource:"omega_0"}],uniforms:[{name:"spec",values:{cascade:0,fetchKm:"param:fetch",gamma:"param:gamma",kMax:.12,kMin:.003926990816987242,loopPeriod:"param:loopPeriod",n:64,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:1600,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c0.evolve",kernel:"ocean_evolve",dispatch:[64,1,1],bindings:[{name:"h0",resource:"h0_0"},{name:"omega",resource:"omega_0"},{name:"outA",resource:"fftA_0"},{name:"outB",resource:"fftB_0"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:64,t:"param:phaseTime",tile:1600}}]},{name:"c0.fftA.fft_rows",kernel:"fft_compute_64",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftA_0"}]},{name:"c0.fftA.fft_cols",kernel:"fft_compute_64",entry:"fft_cols",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftA_0"}]},{name:"c0.fftB.fft_rows",kernel:"fft_compute_64",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftB_0"}]},{name:"c0.fftB.fft_cols",kernel:"fft_compute_64",entry:"fft_cols",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftB_0"}]},{name:"c0.assemble",kernel:"ocean_assemble",dispatch:[64,1,1],bindings:[{name:"fieldA",resource:"fftA_0"},{name:"fieldB",resource:"fftB_0"},{name:"disp",resource:"dispTex0"},{name:"slope",resource:"slopeTex0"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:64}}]},{name:"c0.dispTex0.mips",type:"repeat",count:6,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[16,1,1],bindings:[{name:"src",resource:"dispTex0",levelAt:{index:"l"}},{name:"dst",resource:"dispTex0",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:64},dstWidth:{index:"l",offset:1,shr:64},pad:0,srcLevel:0}}]}]},{name:"c0.slopeTex0.mips",type:"repeat",count:6,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[16,1,1],bindings:[{name:"src",resource:"slopeTex0",levelAt:{index:"l"}},{name:"dst",resource:"slopeTex0",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:64},dstWidth:{index:"l",offset:1,shr:64},pad:0,srcLevel:0}}]}]},{name:"c1.spectrum",kernel:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",dispatch:[256,1,1],bindings:[{name:"h0",resource:"h0_1"},{name:"omega",resource:"omega_1"}],uniforms:[{name:"spec",values:{cascade:1,fetchKm:"param:fetch",gamma:"param:gamma",kMax:.8,kMin:.12,loopPeriod:"param:loopPeriod",n:128,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:250,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c1.evolve",kernel:"ocean_evolve",dispatch:[256,1,1],bindings:[{name:"h0",resource:"h0_1"},{name:"omega",resource:"omega_1"},{name:"outA",resource:"fftA_1"},{name:"outB",resource:"fftB_1"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:128,t:"param:phaseTime",tile:250}}]},{name:"c1.fftA.fft_rows",kernel:"fft_compute_128",dispatch:[128,1,1],bindings:[{name:"data",resource:"fftA_1"}]},{name:"c1.fftA.fft_cols",kernel:"fft_compute_128",entry:"fft_cols",dispatch:[128,1,1],bindings:[{name:"data",resource:"fftA_1"}]},{name:"c1.fftB.fft_rows",kernel:"fft_compute_128",dispatch:[128,1,1],bindings:[{name:"data",resource:"fftB_1"}]},{name:"c1.fftB.fft_cols",kernel:"fft_compute_128",entry:"fft_cols",dispatch:[128,1,1],bindings:[{name:"data",resource:"fftB_1"}]},{name:"c1.assemble",kernel:"ocean_assemble",dispatch:[256,1,1],bindings:[{name:"fieldA",resource:"fftA_1"},{name:"fieldB",resource:"fftB_1"},{name:"disp",resource:"dispTex1"},{name:"slope",resource:"slopeTex1"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:128}}]},{name:"c1.dispTex1.mips",type:"repeat",count:7,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[64,1,1],bindings:[{name:"src",resource:"dispTex1",levelAt:{index:"l"}},{name:"dst",resource:"dispTex1",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:128},dstWidth:{index:"l",offset:1,shr:128},pad:0,srcLevel:0}}]}]},{name:"c1.slopeTex1.mips",type:"repeat",count:7,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[64,1,1],bindings:[{name:"src",resource:"slopeTex1",levelAt:{index:"l"}},{name:"dst",resource:"slopeTex1",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:128},dstWidth:{index:"l",offset:1,shr:128},pad:0,srcLevel:0}}]}]},{name:"c2.spectrum",kernel:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",dispatch:[256,1,1],bindings:[{name:"h0",resource:"h0_2"},{name:"omega",resource:"omega_2"}],uniforms:[{name:"spec",values:{cascade:2,fetchKm:"param:fetch",gamma:"param:gamma",kMax:5.4,kMin:.8,loopPeriod:"param:loopPeriod",n:128,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:37,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c2.evolve",kernel:"ocean_evolve",dispatch:[256,1,1],bindings:[{name:"h0",resource:"h0_2"},{name:"omega",resource:"omega_2"},{name:"outA",resource:"fftA_2"},{name:"outB",resource:"fftB_2"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:128,t:"param:phaseTime",tile:37}}]},{name:"c2.fftA.fft_rows",kernel:"fft_compute_128",dispatch:[128,1,1],bindings:[{name:"data",resource:"fftA_2"}]},{name:"c2.fftA.fft_cols",kernel:"fft_compute_128",entry:"fft_cols",dispatch:[128,1,1],bindings:[{name:"data",resource:"fftA_2"}]},{name:"c2.fftB.fft_rows",kernel:"fft_compute_128",dispatch:[128,1,1],bindings:[{name:"data",resource:"fftB_2"}]},{name:"c2.fftB.fft_cols",kernel:"fft_compute_128",entry:"fft_cols",dispatch:[128,1,1],bindings:[{name:"data",resource:"fftB_2"}]},{name:"c2.assemble",kernel:"ocean_assemble",dispatch:[256,1,1],bindings:[{name:"fieldA",resource:"fftA_2"},{name:"fieldB",resource:"fftB_2"},{name:"disp",resource:"dispTex2"},{name:"slope",resource:"slopeTex2"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:128}}]},{name:"c2.dispTex2.mips",type:"repeat",count:7,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[64,1,1],bindings:[{name:"src",resource:"dispTex2",levelAt:{index:"l"}},{name:"dst",resource:"dispTex2",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:128},dstWidth:{index:"l",offset:1,shr:128},pad:0,srcLevel:0}}]}]},{name:"c2.slopeTex2.mips",type:"repeat",count:7,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[64,1,1],bindings:[{name:"src",resource:"slopeTex2",levelAt:{index:"l"}},{name:"dst",resource:"slopeTex2",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:128},dstWidth:{index:"l",offset:1,shr:128},pad:0,srcLevel:0}}]}]},{name:"c3.spectrum",kernel:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",dispatch:[256,1,1],bindings:[{name:"h0",resource:"h0_3"},{name:"omega",resource:"omega_3"}],uniforms:[{name:"spec",values:{cascade:3,fetchKm:"param:fetch",gamma:"param:gamma",kMax:37,kMin:5.4,loopPeriod:"param:loopPeriod",n:128,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:5.3,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c3.evolve",kernel:"ocean_evolve",dispatch:[256,1,1],bindings:[{name:"h0",resource:"h0_3"},{name:"omega",resource:"omega_3"},{name:"outA",resource:"fftA_3"},{name:"outB",resource:"fftB_3"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:128,t:"param:phaseTime",tile:5.3}}]},{name:"c3.fftA.fft_rows",kernel:"fft_compute_128",dispatch:[128,1,1],bindings:[{name:"data",resource:"fftA_3"}]},{name:"c3.fftA.fft_cols",kernel:"fft_compute_128",entry:"fft_cols",dispatch:[128,1,1],bindings:[{name:"data",resource:"fftA_3"}]},{name:"c3.fftB.fft_rows",kernel:"fft_compute_128",dispatch:[128,1,1],bindings:[{name:"data",resource:"fftB_3"}]},{name:"c3.fftB.fft_cols",kernel:"fft_compute_128",entry:"fft_cols",dispatch:[128,1,1],bindings:[{name:"data",resource:"fftB_3"}]},{name:"c3.assemble",kernel:"ocean_assemble",dispatch:[256,1,1],bindings:[{name:"fieldA",resource:"fftA_3"},{name:"fieldB",resource:"fftB_3"},{name:"disp",resource:"dispTex3"},{name:"slope",resource:"slopeTex3"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:128}}]},{name:"c3.dispTex3.mips",type:"repeat",count:7,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[64,1,1],bindings:[{name:"src",resource:"dispTex3",levelAt:{index:"l"}},{name:"dst",resource:"dispTex3",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:128},dstWidth:{index:"l",offset:1,shr:128},pad:0,srcLevel:0}}]}]},{name:"c3.slopeTex3.mips",type:"repeat",count:7,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[64,1,1],bindings:[{name:"src",resource:"slopeTex3",levelAt:{index:"l"}},{name:"dst",resource:"slopeTex3",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:128},dstWidth:{index:"l",offset:1,shr:128},pad:0,srcLevel:0}}]}]},{name:"wake.source",kernel:"wake_source",when:"param:wakeEnabled",dispatch:[1024,1,1],bindings:[{name:"wakeIn",resource:"wakeA"},{name:"fftOut",resource:"wakeF"},{name:"cleanOut",resource:"wakeC"}],uniforms:[{name:"wake",values:{bowFoam:"param:wakeBowFoam",bowGain:"param:wakeBowGain",cell:"param:wakeCell",depth:"param:wakeDepth",diffuse:"param:wakeDiffuse",dt:"param:wakeDt",foamSpeed:5,fullSpeed:3,heading:"param:hullHeading",hullBeam:"param:hullBeam",hullLen:"param:hullLength",hullU:"param:wakeHullU",hullV:"param:wakeHullV",jitter:"param:wakeJitter",originX:"param:wakeOriginX",originZ:"param:wakeOriginZ",reset:"param:wakeReset",shiftX:"param:wakeShiftX",shiftZ:"param:wakeShiftZ",speed:"param:hullSpeed",srcBeam:"param:wakeSrcBeam",srcLength:"param:wakeSrcLength",tau:"param:wakeFoamTau",width:256}}]},{name:"wake.fft1.fft_rows",kernel:"fft_compute_256",when:"param:wakeEnabled",dispatch:[256,1,1],bindings:[{name:"data",resource:"wakeF"}]},{name:"wake.fft1.fft_cols",kernel:"fft_compute_256",entry:"fft_cols",when:"param:wakeEnabled",dispatch:[256,1,1],bindings:[{name:"data",resource:"wakeF"}]},{name:"wake.symbol",kernel:"wake_symbol",when:"param:wakeEnabled",dispatch:[1024,1,1],bindings:[{name:"fftIn",resource:"wakeF"},{name:"fftOut",resource:"wakeG"}],uniforms:[{name:"sym",values:{n:256,window:"param:wakeLength"}}]},{name:"wake.fft2.fft_rows",kernel:"fft_compute_256",when:"param:wakeEnabled",dispatch:[256,1,1],bindings:[{name:"data",resource:"wakeG"}]},{name:"wake.fft2.fft_cols",kernel:"fft_compute_256",entry:"fft_cols",when:"param:wakeEnabled",dispatch:[256,1,1],bindings:[{name:"data",resource:"wakeG"}]},{name:"wake.step",kernel:"wake_step",when:"param:wakeEnabled",dispatch:[1024,1,1],bindings:[{name:"qIn",resource:"wakeG"},{name:"cleanIn",resource:"wakeC"},{name:"stateOut",resource:"wakeB"},{name:"wakeTex",resource:"wakeTex"}],uniforms:[{name:"wstep",values:{alpha:"param:wakeAlpha",cell:"param:wakeCell",dt:"param:wakeDt",gravity:"param:wakeGEff",originX:"param:wakeOriginX",originZ:"param:wakeOriginZ",sponge:24,spongeRate:.8,width:256}}]},{name:"wake.wakeTex.mips",type:"repeat",when:"param:wakeEnabled",count:8,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[256,1,1],bindings:[{name:"src",resource:"wakeTex",levelAt:{index:"l"}},{name:"dst",resource:"wakeTex",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:256},dstWidth:{index:"l",offset:1,shr:256},pad:0,srcLevel:0}}]}]},{name:"foam",kernel:"ocean_foam",dispatch:[16384,1,1],bindings:[{name:"foamIn",resource:"foamA"},{name:"fieldB0",resource:"fftB_0"},{name:"fieldB1",resource:"fftB_1"},{name:"fieldB2",resource:"fftB_2"},{name:"fieldB3",resource:"fftB_3"},{name:"foamOut",resource:"foamB"},{name:"foamTex",resource:"foamTex"},{name:"wakeIn",resource:"wakeB"}],uniforms:[{name:"foam",values:{cascades:4,cell:.5,dt:"param:dt",gain:"param:foamGain",lambda:"param:lambda",n0:64,n1:128,n2:128,n3:128,omX0:"param:foamOriginMod0X",omX1:"param:foamOriginMod1X",omX2:"param:foamOriginMod2X",omX3:"param:foamOriginMod3X",omZ0:"param:foamOriginMod0Z",omZ1:"param:foamOriginMod1Z",omZ2:"param:foamOriginMod2Z",omZ3:"param:foamOriginMod3Z",originX:"param:foamOriginX",originZ:"param:foamOriginZ",shiftX:"param:foamShiftX",shiftZ:"param:foamShiftZ",size0:1600,size1:250,size2:37,size3:5.3,tau:"param:foamTau",threshold:"param:foamThreshold",wakeCell:"param:wakeCell",wakeGain:"param:wakeFoamGainEff",wakeOriginX:"param:wakeOriginModX",wakeOriginZ:"param:wakeOriginModZ",wakeRelX:"param:wakeRelX",wakeRelZ:"param:wakeRelZ",wakeWidth:256,width:1024}}]},{name:"foamTex.mips",type:"repeat",count:10,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[4096,1,1],bindings:[{name:"src",resource:"foamTex",levelAt:{index:"l"}},{name:"dst",resource:"foamTex",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:1024},dstWidth:{index:"l",offset:1,shr:1024},pad:0,srcLevel:0}}]}]},{name:"probe",kernel:"ocean_probe",dispatch:[1,1,1],bindings:[{name:"points",resource:"probePoints"},{name:"disp0",resource:"fftA_0"},{name:"disp1",resource:"fftA_1"},{name:"disp2",resource:"fftA_2"},{name:"disp3",resource:"fftA_3"},{name:"out",resource:"probeOut"}],uniforms:[{name:"probe",values:{cascades:4,count:"param:probeCount",lambda:"param:lambda",n0:64,n1:128,n2:128,n3:128,normalStep:.5,omX0:"param:probeOriginMod0X",omX1:"param:probeOriginMod1X",omX2:"param:probeOriginMod2X",omX3:"param:probeOriginMod3X",omZ0:"param:probeOriginMod0Z",omZ1:"param:probeOriginMod1Z",omZ2:"param:probeOriginMod2Z",omZ3:"param:probeOriginMod3Z",size0:1600,size1:250,size2:37,size3:5.3,t:"param:simTime"}}]}],frameSwaps:[["wakeA","wakeB"],["foamA","foamB"]],exports:{disp0:"dispTex0",disp1:"dispTex1",disp2:"dispTex2",disp3:"dispTex3",foam:"foamTex",probe:"probeOut",slope0:"slopeTex0",slope1:"slopeTex1",slope2:"slopeTex2",slope3:"slopeTex3",wake:"wakeTex"},paramsFrom:"webgpu/high"},"webgpu/low":{name:"ocean",backend:"webgpu",tier:"low",version:1,resources:[{name:"h0_0",kind:"buffer",elementType:"vec4<f32>",count:4096},{name:"omega_0",kind:"buffer",elementType:"f32",count:4096},{name:"fftA_0",kind:"buffer",elementType:"vec4<f32>",count:4096},{name:"fftB_0",kind:"buffer",elementType:"vec4<f32>",count:4096},{name:"dispTex0",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"slopeTex0",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"h0_1",kind:"buffer",elementType:"vec4<f32>",count:4096},{name:"omega_1",kind:"buffer",elementType:"f32",count:4096},{name:"fftA_1",kind:"buffer",elementType:"vec4<f32>",count:4096},{name:"fftB_1",kind:"buffer",elementType:"vec4<f32>",count:4096},{name:"dispTex1",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"slopeTex1",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"h0_2",kind:"buffer",elementType:"vec4<f32>",count:4096},{name:"omega_2",kind:"buffer",elementType:"f32",count:4096},{name:"fftA_2",kind:"buffer",elementType:"vec4<f32>",count:4096},{name:"fftB_2",kind:"buffer",elementType:"vec4<f32>",count:4096},{name:"dispTex2",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"slopeTex2",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"wakeA",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"wakeB",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"wakeF",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"wakeG",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"wakeC",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"wakeTex",kind:"texture",format:"rgba16float",width:256,height:256,mipLevels:9},{name:"foamA",kind:"buffer",elementType:"f32",count:1048576},{name:"foamB",kind:"buffer",elementType:"f32",count:1048576},{name:"foamTex",kind:"texture",format:"rgba16float",width:1024,height:1024,mipLevels:11},{name:"probePoints",kind:"buffer",elementType:"vec4<f32>",count:64,feed:"probePoints"},{name:"probeOut",kind:"buffer",elementType:"vec4<f32>",count:64}],passes:[{name:"c0.spectrum",kernel:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",dispatch:[64,1,1],bindings:[{name:"h0",resource:"h0_0"},{name:"omega",resource:"omega_0"}],uniforms:[{name:"spec",values:{cascade:0,fetchKm:"param:fetch",gamma:"param:gamma",kMax:.12,kMin:.003926990816987242,loopPeriod:"param:loopPeriod",n:64,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:1600,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c0.evolve",kernel:"ocean_evolve",dispatch:[64,1,1],bindings:[{name:"h0",resource:"h0_0"},{name:"omega",resource:"omega_0"},{name:"outA",resource:"fftA_0"},{name:"outB",resource:"fftB_0"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:64,t:"param:phaseTime",tile:1600}}]},{name:"c0.fftA.fft_rows",kernel:"fft_compute_64",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftA_0"}]},{name:"c0.fftA.fft_cols",kernel:"fft_compute_64",entry:"fft_cols",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftA_0"}]},{name:"c0.fftB.fft_rows",kernel:"fft_compute_64",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftB_0"}]},{name:"c0.fftB.fft_cols",kernel:"fft_compute_64",entry:"fft_cols",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftB_0"}]},{name:"c0.assemble",kernel:"ocean_assemble",dispatch:[64,1,1],bindings:[{name:"fieldA",resource:"fftA_0"},{name:"fieldB",resource:"fftB_0"},{name:"disp",resource:"dispTex0"},{name:"slope",resource:"slopeTex0"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:64}}]},{name:"c0.dispTex0.mips",type:"repeat",count:6,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[16,1,1],bindings:[{name:"src",resource:"dispTex0",levelAt:{index:"l"}},{name:"dst",resource:"dispTex0",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:64},dstWidth:{index:"l",offset:1,shr:64},pad:0,srcLevel:0}}]}]},{name:"c0.slopeTex0.mips",type:"repeat",count:6,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[16,1,1],bindings:[{name:"src",resource:"slopeTex0",levelAt:{index:"l"}},{name:"dst",resource:"slopeTex0",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:64},dstWidth:{index:"l",offset:1,shr:64},pad:0,srcLevel:0}}]}]},{name:"c1.spectrum",kernel:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",dispatch:[64,1,1],bindings:[{name:"h0",resource:"h0_1"},{name:"omega",resource:"omega_1"}],uniforms:[{name:"spec",values:{cascade:1,fetchKm:"param:fetch",gamma:"param:gamma",kMax:.8,kMin:.12,loopPeriod:"param:loopPeriod",n:64,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:250,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c1.evolve",kernel:"ocean_evolve",dispatch:[64,1,1],bindings:[{name:"h0",resource:"h0_1"},{name:"omega",resource:"omega_1"},{name:"outA",resource:"fftA_1"},{name:"outB",resource:"fftB_1"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:64,t:"param:phaseTime",tile:250}}]},{name:"c1.fftA.fft_rows",kernel:"fft_compute_64",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftA_1"}]},{name:"c1.fftA.fft_cols",kernel:"fft_compute_64",entry:"fft_cols",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftA_1"}]},{name:"c1.fftB.fft_rows",kernel:"fft_compute_64",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftB_1"}]},{name:"c1.fftB.fft_cols",kernel:"fft_compute_64",entry:"fft_cols",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftB_1"}]},{name:"c1.assemble",kernel:"ocean_assemble",dispatch:[64,1,1],bindings:[{name:"fieldA",resource:"fftA_1"},{name:"fieldB",resource:"fftB_1"},{name:"disp",resource:"dispTex1"},{name:"slope",resource:"slopeTex1"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:64}}]},{name:"c1.dispTex1.mips",type:"repeat",count:6,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[16,1,1],bindings:[{name:"src",resource:"dispTex1",levelAt:{index:"l"}},{name:"dst",resource:"dispTex1",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:64},dstWidth:{index:"l",offset:1,shr:64},pad:0,srcLevel:0}}]}]},{name:"c1.slopeTex1.mips",type:"repeat",count:6,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[16,1,1],bindings:[{name:"src",resource:"slopeTex1",levelAt:{index:"l"}},{name:"dst",resource:"slopeTex1",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:64},dstWidth:{index:"l",offset:1,shr:64},pad:0,srcLevel:0}}]}]},{name:"c2.spectrum",kernel:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",dispatch:[64,1,1],bindings:[{name:"h0",resource:"h0_2"},{name:"omega",resource:"omega_2"}],uniforms:[{name:"spec",values:{cascade:2,fetchKm:"param:fetch",gamma:"param:gamma",kMax:5.4,kMin:.8,loopPeriod:"param:loopPeriod",n:64,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:37,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c2.evolve",kernel:"ocean_evolve",dispatch:[64,1,1],bindings:[{name:"h0",resource:"h0_2"},{name:"omega",resource:"omega_2"},{name:"outA",resource:"fftA_2"},{name:"outB",resource:"fftB_2"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:64,t:"param:phaseTime",tile:37}}]},{name:"c2.fftA.fft_rows",kernel:"fft_compute_64",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftA_2"}]},{name:"c2.fftA.fft_cols",kernel:"fft_compute_64",entry:"fft_cols",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftA_2"}]},{name:"c2.fftB.fft_rows",kernel:"fft_compute_64",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftB_2"}]},{name:"c2.fftB.fft_cols",kernel:"fft_compute_64",entry:"fft_cols",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftB_2"}]},{name:"c2.assemble",kernel:"ocean_assemble",dispatch:[64,1,1],bindings:[{name:"fieldA",resource:"fftA_2"},{name:"fieldB",resource:"fftB_2"},{name:"disp",resource:"dispTex2"},{name:"slope",resource:"slopeTex2"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:64}}]},{name:"c2.dispTex2.mips",type:"repeat",count:6,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[16,1,1],bindings:[{name:"src",resource:"dispTex2",levelAt:{index:"l"}},{name:"dst",resource:"dispTex2",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:64},dstWidth:{index:"l",offset:1,shr:64},pad:0,srcLevel:0}}]}]},{name:"c2.slopeTex2.mips",type:"repeat",count:6,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[16,1,1],bindings:[{name:"src",resource:"slopeTex2",levelAt:{index:"l"}},{name:"dst",resource:"slopeTex2",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:64},dstWidth:{index:"l",offset:1,shr:64},pad:0,srcLevel:0}}]}]},{name:"wake.source",kernel:"wake_source",when:"param:wakeEnabled",dispatch:[1024,1,1],bindings:[{name:"wakeIn",resource:"wakeA"},{name:"fftOut",resource:"wakeF"},{name:"cleanOut",resource:"wakeC"}],uniforms:[{name:"wake",values:{bowFoam:"param:wakeBowFoam",bowGain:"param:wakeBowGain",cell:"param:wakeCell",depth:"param:wakeDepth",diffuse:"param:wakeDiffuse",dt:"param:wakeDt",foamSpeed:5,fullSpeed:3,heading:"param:hullHeading",hullBeam:"param:hullBeam",hullLen:"param:hullLength",hullU:"param:wakeHullU",hullV:"param:wakeHullV",jitter:"param:wakeJitter",originX:"param:wakeOriginX",originZ:"param:wakeOriginZ",reset:"param:wakeReset",shiftX:"param:wakeShiftX",shiftZ:"param:wakeShiftZ",speed:"param:hullSpeed",srcBeam:"param:wakeSrcBeam",srcLength:"param:wakeSrcLength",tau:"param:wakeFoamTau",width:256}}]},{name:"wake.fft1.fft_rows",kernel:"fft_compute_256",when:"param:wakeEnabled",dispatch:[256,1,1],bindings:[{name:"data",resource:"wakeF"}]},{name:"wake.fft1.fft_cols",kernel:"fft_compute_256",entry:"fft_cols",when:"param:wakeEnabled",dispatch:[256,1,1],bindings:[{name:"data",resource:"wakeF"}]},{name:"wake.symbol",kernel:"wake_symbol",when:"param:wakeEnabled",dispatch:[1024,1,1],bindings:[{name:"fftIn",resource:"wakeF"},{name:"fftOut",resource:"wakeG"}],uniforms:[{name:"sym",values:{n:256,window:"param:wakeLength"}}]},{name:"wake.fft2.fft_rows",kernel:"fft_compute_256",when:"param:wakeEnabled",dispatch:[256,1,1],bindings:[{name:"data",resource:"wakeG"}]},{name:"wake.fft2.fft_cols",kernel:"fft_compute_256",entry:"fft_cols",when:"param:wakeEnabled",dispatch:[256,1,1],bindings:[{name:"data",resource:"wakeG"}]},{name:"wake.step",kernel:"wake_step",when:"param:wakeEnabled",dispatch:[1024,1,1],bindings:[{name:"qIn",resource:"wakeG"},{name:"cleanIn",resource:"wakeC"},{name:"stateOut",resource:"wakeB"},{name:"wakeTex",resource:"wakeTex"}],uniforms:[{name:"wstep",values:{alpha:"param:wakeAlpha",cell:"param:wakeCell",dt:"param:wakeDt",gravity:"param:wakeGEff",originX:"param:wakeOriginX",originZ:"param:wakeOriginZ",sponge:24,spongeRate:.8,width:256}}]},{name:"wake.wakeTex.mips",type:"repeat",when:"param:wakeEnabled",count:8,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[256,1,1],bindings:[{name:"src",resource:"wakeTex",levelAt:{index:"l"}},{name:"dst",resource:"wakeTex",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:256},dstWidth:{index:"l",offset:1,shr:256},pad:0,srcLevel:0}}]}]},{name:"foam",kernel:"ocean_foam",dispatch:[16384,1,1],bindings:[{name:"foamIn",resource:"foamA"},{name:"fieldB0",resource:"fftB_0"},{name:"fieldB1",resource:"fftB_1"},{name:"fieldB2",resource:"fftB_2"},{name:"fieldB3",resource:"fftB_2"},{name:"foamOut",resource:"foamB"},{name:"foamTex",resource:"foamTex"},{name:"wakeIn",resource:"wakeB"}],uniforms:[{name:"foam",values:{cascades:3,cell:.5,dt:"param:dt",gain:"param:foamGain",lambda:"param:lambda",n0:64,n1:64,n2:64,n3:64,omX0:"param:foamOriginMod0X",omX1:"param:foamOriginMod1X",omX2:"param:foamOriginMod2X",omX3:"param:foamOriginMod2X",omZ0:"param:foamOriginMod0Z",omZ1:"param:foamOriginMod1Z",omZ2:"param:foamOriginMod2Z",omZ3:"param:foamOriginMod2Z",originX:"param:foamOriginX",originZ:"param:foamOriginZ",shiftX:"param:foamShiftX",shiftZ:"param:foamShiftZ",size0:1600,size1:250,size2:37,size3:37,tau:"param:foamTau",threshold:"param:foamThreshold",wakeCell:"param:wakeCell",wakeGain:"param:wakeFoamGainEff",wakeOriginX:"param:wakeOriginModX",wakeOriginZ:"param:wakeOriginModZ",wakeRelX:"param:wakeRelX",wakeRelZ:"param:wakeRelZ",wakeWidth:256,width:1024}}]},{name:"foamTex.mips",type:"repeat",count:10,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[4096,1,1],bindings:[{name:"src",resource:"foamTex",levelAt:{index:"l"}},{name:"dst",resource:"foamTex",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:1024},dstWidth:{index:"l",offset:1,shr:1024},pad:0,srcLevel:0}}]}]},{name:"probe",kernel:"ocean_probe",dispatch:[1,1,1],bindings:[{name:"points",resource:"probePoints"},{name:"disp0",resource:"fftA_0"},{name:"disp1",resource:"fftA_1"},{name:"disp2",resource:"fftA_2"},{name:"disp3",resource:"fftA_2"},{name:"out",resource:"probeOut"}],uniforms:[{name:"probe",values:{cascades:3,count:"param:probeCount",lambda:"param:lambda",n0:64,n1:64,n2:64,n3:64,normalStep:.5,omX0:"param:probeOriginMod0X",omX1:"param:probeOriginMod1X",omX2:"param:probeOriginMod2X",omX3:"param:probeOriginMod2X",omZ0:"param:probeOriginMod0Z",omZ1:"param:probeOriginMod1Z",omZ2:"param:probeOriginMod2Z",omZ3:"param:probeOriginMod2Z",size0:1600,size1:250,size2:37,size3:37,t:"param:simTime"}}]}],frameSwaps:[["wakeA","wakeB"],["foamA","foamB"]],exports:{disp0:"dispTex0",disp1:"dispTex1",disp2:"dispTex2",foam:"foamTex",probe:"probeOut",slope0:"slopeTex0",slope1:"slopeTex1",slope2:"slopeTex2",wake:"wakeTex"},paramsFrom:"webgpu/high"},"webgl2/high":{name:"ocean",backend:"webgl2",tier:"high",version:1,params:[{name:"windSpeed",type:"f32",source:{kind:"value",value:9},tween:20},{name:"fetch",type:"f32",source:{kind:"value",value:300},tween:20},{name:"gamma",type:"f32",source:{kind:"value",value:3.3},tween:20},{name:"spread",type:"f32",source:{kind:"value",value:8},tween:20},{name:"swell",type:"f32",source:{kind:"value",value:0},tween:20},{name:"windDirDeg",type:"f32",source:{kind:"value",value:0},tween:20},{name:"swellDirDeg",type:"f32",source:{kind:"value",value:0},tween:20},{name:"seed",type:"u32",source:{kind:"value",value:0}},{name:"loopPeriod",type:"f32",source:{kind:"value",value:1e3}},{name:"lambda",type:"f32",source:{kind:"value",value:1},tween:20},{name:"timeScale",type:"f32",source:{kind:"value",value:1}},{name:"driftX",type:"f32",source:{kind:"value",value:0}},{name:"driftZ",type:"f32",source:{kind:"value",value:0}},{name:"foamTau",type:"f32",source:{kind:"value",value:4}},{name:"foamThreshold",type:"f32",source:{kind:"value",value:.9}},{name:"foamGain",type:"f32",source:{kind:"value",value:2}},{name:"probeCount",type:"u32",source:{kind:"value",value:0}},{name:"dt",type:"f32",source:{kind:"frame",name:"dt"}},{name:"cameraX",type:"f32",source:{kind:"frame",name:"cameraX"}},{name:"cameraZ",type:"f32",source:{kind:"frame",name:"cameraZ"}},{name:"windDir",type:"f32",source:{args:[1.5707963267948966,{op:"mul",args:["param:windDirDeg",.017453292519943295]}],kind:"expr",op:"sub"}},{name:"swellDir",type:"f32",source:{args:[1.5707963267948966,{op:"mul",args:["param:swellDirDeg",.017453292519943295]}],kind:"expr",op:"sub"}},{name:"simTime",type:"f32",source:{kind:"accumulate",of:"param:timeScale"}},{name:"phaseTime",type:"f32",source:{args:["param:simTime","param:loopPeriod"],kind:"expr",op:"mod"}},{name:"waterX",type:"f32",source:{kind:"accumulate",of:"param:driftX"}},{name:"waterZ",type:"f32",source:{kind:"accumulate",of:"param:driftZ"}},{name:"waterModX0",type:"f32",source:{args:["param:waterX",1600],kind:"expr",op:"mod"}},{name:"waterModZ0",type:"f32",source:{args:["param:waterZ",1600],kind:"expr",op:"mod"}},{name:"waterModX1",type:"f32",source:{args:["param:waterX",250],kind:"expr",op:"mod"}},{name:"waterModZ1",type:"f32",source:{args:["param:waterZ",250],kind:"expr",op:"mod"}},{name:"waterModX2",type:"f32",source:{args:["param:waterX",37],kind:"expr",op:"mod"}},{name:"waterModZ2",type:"f32",source:{args:["param:waterZ",37],kind:"expr",op:"mod"}},{name:"waterModX3",type:"f32",source:{args:["param:waterX",5.3],kind:"expr",op:"mod"}},{name:"waterModZ3",type:"f32",source:{args:["param:waterZ",5.3],kind:"expr",op:"mod"}},{name:"foamOriginX",type:"i32",source:{args:[{op:"floor",args:[{op:"div",args:[{op:"sub",args:["param:cameraX","param:waterX"]},.5]}]},512],kind:"expr",op:"sub"}},{name:"foamShiftX",type:"i32",source:{kind:"delta",of:"param:foamOriginX"}},{name:"foamOriginMod0X",type:"f32",source:{args:[{op:"mul",args:["param:foamOriginX",.5]},1600],kind:"expr",op:"mod"}},{name:"foamOriginMod1X",type:"f32",source:{args:[{op:"mul",args:["param:foamOriginX",.5]},250],kind:"expr",op:"mod"}},{name:"foamOriginMod2X",type:"f32",source:{args:[{op:"mul",args:["param:foamOriginX",.5]},37],kind:"expr",op:"mod"}},{name:"foamOriginMod3X",type:"f32",source:{args:[{op:"mul",args:["param:foamOriginX",.5]},5.3],kind:"expr",op:"mod"}},{name:"foamOriginZ",type:"i32",source:{args:[{op:"floor",args:[{op:"div",args:[{op:"sub",args:["param:cameraZ","param:waterZ"]},.5]}]},512],kind:"expr",op:"sub"}},{name:"foamShiftZ",type:"i32",source:{kind:"delta",of:"param:foamOriginZ"}},{name:"foamOriginMod0Z",type:"f32",source:{args:[{op:"mul",args:["param:foamOriginZ",.5]},1600],kind:"expr",op:"mod"}},{name:"foamOriginMod1Z",type:"f32",source:{args:[{op:"mul",args:["param:foamOriginZ",.5]},250],kind:"expr",op:"mod"}},{name:"foamOriginMod2Z",type:"f32",source:{args:[{op:"mul",args:["param:foamOriginZ",.5]},37],kind:"expr",op:"mod"}},{name:"foamOriginMod3Z",type:"f32",source:{args:[{op:"mul",args:["param:foamOriginZ",.5]},5.3],kind:"expr",op:"mod"}},{name:"hullX",type:"f32",source:{kind:"value",value:0}},{name:"hullZ",type:"f32",source:{kind:"value",value:0}},{name:"hullHeading",type:"f32",source:{kind:"value",value:0}},{name:"hullSpeed",type:"f32",source:{kind:"value",value:0}},{name:"wakeEnabled",type:"bool",source:{kind:"value",value:0}},{name:"hullLength",type:"f32",source:{kind:"value",value:200}},{name:"hullBeam",type:"f32",source:{kind:"value",value:30}},{name:"wakeCell",type:"f32",source:{kind:"value",value:4}},{name:"wakeDepth",type:"f32",source:{kind:"value",value:.4}},{name:"wakeGravity",type:"f32",source:{kind:"value",value:9.81}},{name:"wakeAlpha",type:"f32",source:{kind:"value",value:.02}},{name:"wakeMinLambdaCells",type:"f32",source:{kind:"value",value:8}},{name:"wakeFoamGain",type:"f32",source:{kind:"value",value:1}},{name:"wakeFoamTau",type:"f32",source:{kind:"value",value:48}},{name:"wakeDiffuse",type:"f32",source:{kind:"value",value:1.5}},{name:"wakeJitter",type:"f32",source:{kind:"value",value:1}},{name:"wakeBowGain",type:"f32",source:{kind:"value",value:.2}},{name:"wakeBowFoam",type:"f32",source:{kind:"value",value:.9}},{name:"wakeOn",type:"f32",source:{args:["param:wakeEnabled",1],kind:"expr",op:"mul"}},{name:"wakeFoamGainEff",type:"f32",source:{args:["param:wakeEnabled","param:wakeFoamGain"],kind:"expr",op:"mul"}},{name:"wakeEdge",type:"i32",source:{kind:"delta",of:"param:wakeEnabled"}},{name:"wakeReset",type:"u32",source:{args:["param:wakeEdge",0],kind:"expr",op:"max"}},{name:"wakeLength",type:"f32",source:{args:["param:wakeCell",256],kind:"expr",op:"mul"}},{name:"hullWaterX",type:"f32",source:{args:["param:hullX","param:waterX"],kind:"expr",op:"sub"}},{name:"wakeOriginX",type:"i32",source:{args:[{op:"div",args:[{op:"sub",args:[{op:"sub",args:["param:hullWaterX",{op:"mul",args:[{op:"sin",args:["param:hullHeading"]},{op:"mul",args:["param:wakeCell",51.2]}]}]},{op:"mul",args:["param:wakeCell",128]}]},"param:wakeCell"]}],kind:"expr",op:"floor"}},{name:"wakeShiftX",type:"i32",source:{kind:"delta",of:"param:wakeOriginX"}},{name:"wakeHullU",type:"f32",source:{args:["param:hullWaterX",{op:"mul",args:["param:wakeOriginX","param:wakeCell"]}],kind:"expr",op:"sub"}},{name:"wakeOriginModX",type:"i32",source:{args:["param:wakeOriginX",256],kind:"expr",op:"mod"}},{name:"wakeRelX",type:"f32",source:{args:[{op:"mul",args:["param:foamOriginX",.5]},{op:"mul",args:["param:wakeOriginX","param:wakeCell"]}],kind:"expr",op:"sub"}},{name:"wakeCamRelX",type:"f32",source:{args:[{op:"add",args:["param:waterX",{op:"mul",args:["param:wakeOriginX","param:wakeCell"]}]},"param:cameraX"],kind:"expr",op:"sub"}},{name:"hullWaterZ",type:"f32",source:{args:["param:hullZ","param:waterZ"],kind:"expr",op:"sub"}},{name:"wakeOriginZ",type:"i32",source:{args:[{op:"div",args:[{op:"sub",args:[{op:"sub",args:["param:hullWaterZ",{op:"mul",args:[{op:"cos",args:["param:hullHeading"]},{op:"mul",args:["param:wakeCell",51.2]}]}]},{op:"mul",args:["param:wakeCell",128]}]},"param:wakeCell"]}],kind:"expr",op:"floor"}},{name:"wakeShiftZ",type:"i32",source:{kind:"delta",of:"param:wakeOriginZ"}},{name:"wakeHullV",type:"f32",source:{args:["param:hullWaterZ",{op:"mul",args:["param:wakeOriginZ","param:wakeCell"]}],kind:"expr",op:"sub"}},{name:"wakeOriginModZ",type:"i32",source:{args:["param:wakeOriginZ",256],kind:"expr",op:"mod"}},{name:"wakeRelZ",type:"f32",source:{args:[{op:"mul",args:["param:foamOriginZ",.5]},{op:"mul",args:["param:wakeOriginZ","param:wakeCell"]}],kind:"expr",op:"sub"}},{name:"wakeCamRelZ",type:"f32",source:{args:[{op:"add",args:["param:waterZ",{op:"mul",args:["param:wakeOriginZ","param:wakeCell"]}]},"param:cameraZ"],kind:"expr",op:"sub"}},{name:"wakeGEff",type:"f32",source:{args:[{op:"min",args:[{op:"min",args:["param:wakeGravity",{op:"mul",args:[{op:"mul",args:["param:hullSpeed","param:hullSpeed"]},{op:"div",args:[6.283185307179586,{op:"mul",args:["param:wakeMinLambdaCells","param:wakeCell"]}]}]}]},{op:"mul",args:["param:wakeCell",16.39001041697031]}]},.05],kind:"expr",op:"max"}},{name:"wakeDt",type:"f32",source:{args:[{op:"mul",args:["param:dt","param:timeScale"]},.2],kind:"expr",op:"min"}},{name:"wakeSrcLength",type:"f32",source:{args:[{op:"min",args:["param:hullLength",{op:"div",args:[{op:"mul",args:[{op:"mul",args:["param:hullSpeed","param:hullSpeed"]},3.141592653589793]},"param:wakeGEff"]}]},{op:"mul",args:["param:wakeCell",2]}],kind:"expr",op:"max"}},{name:"wakeSrcBeam",type:"f32",source:{args:[{op:"min",args:["param:hullBeam",{op:"mul",args:["param:wakeSrcLength",.67]}]},{op:"mul",args:["param:wakeCell",1.5]}],kind:"expr",op:"max"}}],resources:[{name:"h0_0",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"omega_0",kind:"buffer",elementType:"f32",count:4096,width:64},{name:"fftA_0",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"fftB_0",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"fftA2_0",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"fftB2_0",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"dispTex0",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"slopeTex0",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"h0_1",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"omega_1",kind:"buffer",elementType:"f32",count:65536,width:256},{name:"fftA_1",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"fftB_1",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"fftA2_1",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"fftB2_1",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"dispTex1",kind:"texture",format:"rgba16float",width:256,height:256,mipLevels:9},{name:"slopeTex1",kind:"texture",format:"rgba16float",width:256,height:256,mipLevels:9},{name:"h0_2",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"omega_2",kind:"buffer",elementType:"f32",count:65536,width:256},{name:"fftA_2",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"fftB_2",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"fftA2_2",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"fftB2_2",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"dispTex2",kind:"texture",format:"rgba16float",width:256,height:256,mipLevels:9},{name:"slopeTex2",kind:"texture",format:"rgba16float",width:256,height:256,mipLevels:9},{name:"h0_3",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"omega_3",kind:"buffer",elementType:"f32",count:65536,width:256},{name:"fftA_3",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"fftB_3",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"fftA2_3",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"fftB2_3",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"dispTex3",kind:"texture",format:"rgba16float",width:256,height:256,mipLevels:9},{name:"slopeTex3",kind:"texture",format:"rgba16float",width:256,height:256,mipLevels:9},{name:"wakeA",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"wakeB",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"wakeF",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"wakeG",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"wakeC",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"wakeTex",kind:"texture",format:"rgba16float",width:256,height:256,mipLevels:9},{name:"foamA",kind:"buffer",elementType:"f32",count:1048576,width:1024},{name:"foamB",kind:"buffer",elementType:"f32",count:1048576,width:1024},{name:"foamTex",kind:"texture",format:"rgba16float",width:1024,height:1024,mipLevels:11}],passes:[{name:"c0.spectrum",kernel:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",target:{width:64,height:64,elioWidth:64},bindings:[{name:"h0",resource:"h0_0"},{name:"omega",resource:"omega_0"}],uniforms:[{name:"spec",values:{cascade:0,fetchKm:"param:fetch",gamma:"param:gamma",kMax:.12,kMin:.003926990816987242,loopPeriod:"param:loopPeriod",n:64,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:1600,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c0.evolve",kernel:"ocean_evolve",target:{width:64,height:64,elioWidth:64},bindings:[{name:"h0",resource:"h0_0"},{name:"omega",resource:"omega_0"},{name:"outA",resource:"fftA_0"},{name:"outB",resource:"fftB_0"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:64,t:"param:phaseTime",tile:1600}}]},{name:"c0.fft.rows",type:"repeat",count:6,index:"s",swap:[["fftA_0","fftA2_0"],["fftB_0","fftB2_0"]],body:[{name:"A",kernel:"fft_gather_64",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftA_0"},{name:"dst",resource:"fftA2_0"}],uniforms:[{name:"fft",values:{axis:0,n:64,stage:"param:s"}}]},{name:"B",kernel:"fft_gather_64",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftB_0"},{name:"dst",resource:"fftB2_0"}],uniforms:[{name:"fft",values:{axis:0,n:64,stage:"param:s"}}]}]},{name:"c0.fft.cols",type:"repeat",count:6,index:"s",swap:[["fftA_0","fftA2_0"],["fftB_0","fftB2_0"]],body:[{name:"A",kernel:"fft_gather_64",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftA_0"},{name:"dst",resource:"fftA2_0"}],uniforms:[{name:"fft",values:{axis:1,n:64,stage:"param:s"}}]},{name:"B",kernel:"fft_gather_64",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftB_0"},{name:"dst",resource:"fftB2_0"}],uniforms:[{name:"fft",values:{axis:1,n:64,stage:"param:s"}}]}]},{name:"c0.assemble",kernel:"ocean_assemble",target:{width:64,height:64,elioWidth:64},bindings:[{name:"fieldA",resource:"fftA_0"},{name:"fieldB",resource:"fftB_0"},{name:"disp",resource:"dispTex0"},{name:"slope",resource:"slopeTex0"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:64}}]},{name:"c0.dispTex0.mips",type:"mipmap",resource:"dispTex0"},{name:"c0.slopeTex0.mips",type:"mipmap",resource:"slopeTex0"},{name:"c1.spectrum",kernel:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",target:{width:256,height:256,elioWidth:256},bindings:[{name:"h0",resource:"h0_1"},{name:"omega",resource:"omega_1"}],uniforms:[{name:"spec",values:{cascade:1,fetchKm:"param:fetch",gamma:"param:gamma",kMax:.8,kMin:.12,loopPeriod:"param:loopPeriod",n:256,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:250,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c1.evolve",kernel:"ocean_evolve",target:{width:256,height:256,elioWidth:256},bindings:[{name:"h0",resource:"h0_1"},{name:"omega",resource:"omega_1"},{name:"outA",resource:"fftA_1"},{name:"outB",resource:"fftB_1"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:256,t:"param:phaseTime",tile:250}}]},{name:"c1.fft.rows",type:"repeat",count:8,index:"s",swap:[["fftA_1","fftA2_1"],["fftB_1","fftB2_1"]],body:[{name:"A",kernel:"fft_gather_256",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"fftA_1"},{name:"dst",resource:"fftA2_1"}],uniforms:[{name:"fft",values:{axis:0,n:256,stage:"param:s"}}]},{name:"B",kernel:"fft_gather_256",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"fftB_1"},{name:"dst",resource:"fftB2_1"}],uniforms:[{name:"fft",values:{axis:0,n:256,stage:"param:s"}}]}]},{name:"c1.fft.cols",type:"repeat",count:8,index:"s",swap:[["fftA_1","fftA2_1"],["fftB_1","fftB2_1"]],body:[{name:"A",kernel:"fft_gather_256",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"fftA_1"},{name:"dst",resource:"fftA2_1"}],uniforms:[{name:"fft",values:{axis:1,n:256,stage:"param:s"}}]},{name:"B",kernel:"fft_gather_256",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"fftB_1"},{name:"dst",resource:"fftB2_1"}],uniforms:[{name:"fft",values:{axis:1,n:256,stage:"param:s"}}]}]},{name:"c1.assemble",kernel:"ocean_assemble",target:{width:256,height:256,elioWidth:256},bindings:[{name:"fieldA",resource:"fftA_1"},{name:"fieldB",resource:"fftB_1"},{name:"disp",resource:"dispTex1"},{name:"slope",resource:"slopeTex1"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:256}}]},{name:"c1.dispTex1.mips",type:"mipmap",resource:"dispTex1"},{name:"c1.slopeTex1.mips",type:"mipmap",resource:"slopeTex1"},{name:"c2.spectrum",kernel:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",target:{width:256,height:256,elioWidth:256},bindings:[{name:"h0",resource:"h0_2"},{name:"omega",resource:"omega_2"}],uniforms:[{name:"spec",values:{cascade:2,fetchKm:"param:fetch",gamma:"param:gamma",kMax:5.4,kMin:.8,loopPeriod:"param:loopPeriod",n:256,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:37,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c2.evolve",kernel:"ocean_evolve",target:{width:256,height:256,elioWidth:256},bindings:[{name:"h0",resource:"h0_2"},{name:"omega",resource:"omega_2"},{name:"outA",resource:"fftA_2"},{name:"outB",resource:"fftB_2"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:256,t:"param:phaseTime",tile:37}}]},{name:"c2.fft.rows",type:"repeat",count:8,index:"s",swap:[["fftA_2","fftA2_2"],["fftB_2","fftB2_2"]],body:[{name:"A",kernel:"fft_gather_256",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"fftA_2"},{name:"dst",resource:"fftA2_2"}],uniforms:[{name:"fft",values:{axis:0,n:256,stage:"param:s"}}]},{name:"B",kernel:"fft_gather_256",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"fftB_2"},{name:"dst",resource:"fftB2_2"}],uniforms:[{name:"fft",values:{axis:0,n:256,stage:"param:s"}}]}]},{name:"c2.fft.cols",type:"repeat",count:8,index:"s",swap:[["fftA_2","fftA2_2"],["fftB_2","fftB2_2"]],body:[{name:"A",kernel:"fft_gather_256",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"fftA_2"},{name:"dst",resource:"fftA2_2"}],uniforms:[{name:"fft",values:{axis:1,n:256,stage:"param:s"}}]},{name:"B",kernel:"fft_gather_256",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"fftB_2"},{name:"dst",resource:"fftB2_2"}],uniforms:[{name:"fft",values:{axis:1,n:256,stage:"param:s"}}]}]},{name:"c2.assemble",kernel:"ocean_assemble",target:{width:256,height:256,elioWidth:256},bindings:[{name:"fieldA",resource:"fftA_2"},{name:"fieldB",resource:"fftB_2"},{name:"disp",resource:"dispTex2"},{name:"slope",resource:"slopeTex2"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:256}}]},{name:"c2.dispTex2.mips",type:"mipmap",resource:"dispTex2"},{name:"c2.slopeTex2.mips",type:"mipmap",resource:"slopeTex2"},{name:"c3.spectrum",kernel:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",target:{width:256,height:256,elioWidth:256},bindings:[{name:"h0",resource:"h0_3"},{name:"omega",resource:"omega_3"}],uniforms:[{name:"spec",values:{cascade:3,fetchKm:"param:fetch",gamma:"param:gamma",kMax:37,kMin:5.4,loopPeriod:"param:loopPeriod",n:256,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:5.3,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c3.evolve",kernel:"ocean_evolve",target:{width:256,height:256,elioWidth:256},bindings:[{name:"h0",resource:"h0_3"},{name:"omega",resource:"omega_3"},{name:"outA",resource:"fftA_3"},{name:"outB",resource:"fftB_3"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:256,t:"param:phaseTime",tile:5.3}}]},{name:"c3.fft.rows",type:"repeat",count:8,index:"s",swap:[["fftA_3","fftA2_3"],["fftB_3","fftB2_3"]],body:[{name:"A",kernel:"fft_gather_256",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"fftA_3"},{name:"dst",resource:"fftA2_3"}],uniforms:[{name:"fft",values:{axis:0,n:256,stage:"param:s"}}]},{name:"B",kernel:"fft_gather_256",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"fftB_3"},{name:"dst",resource:"fftB2_3"}],uniforms:[{name:"fft",values:{axis:0,n:256,stage:"param:s"}}]}]},{name:"c3.fft.cols",type:"repeat",count:8,index:"s",swap:[["fftA_3","fftA2_3"],["fftB_3","fftB2_3"]],body:[{name:"A",kernel:"fft_gather_256",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"fftA_3"},{name:"dst",resource:"fftA2_3"}],uniforms:[{name:"fft",values:{axis:1,n:256,stage:"param:s"}}]},{name:"B",kernel:"fft_gather_256",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"fftB_3"},{name:"dst",resource:"fftB2_3"}],uniforms:[{name:"fft",values:{axis:1,n:256,stage:"param:s"}}]}]},{name:"c3.assemble",kernel:"ocean_assemble",target:{width:256,height:256,elioWidth:256},bindings:[{name:"fieldA",resource:"fftA_3"},{name:"fieldB",resource:"fftB_3"},{name:"disp",resource:"dispTex3"},{name:"slope",resource:"slopeTex3"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:256}}]},{name:"c3.dispTex3.mips",type:"mipmap",resource:"dispTex3"},{name:"c3.slopeTex3.mips",type:"mipmap",resource:"slopeTex3"},{name:"wake.source",kernel:"wake_source",when:"param:wakeEnabled",target:{width:256,height:256,elioWidth:256},bindings:[{name:"wakeIn",resource:"wakeA"},{name:"fftOut",resource:"wakeF"},{name:"cleanOut",resource:"wakeC"}],uniforms:[{name:"wake",values:{bowFoam:"param:wakeBowFoam",bowGain:"param:wakeBowGain",cell:"param:wakeCell",depth:"param:wakeDepth",diffuse:"param:wakeDiffuse",dt:"param:wakeDt",foamSpeed:5,fullSpeed:3,heading:"param:hullHeading",hullBeam:"param:hullBeam",hullLen:"param:hullLength",hullU:"param:wakeHullU",hullV:"param:wakeHullV",jitter:"param:wakeJitter",originX:"param:wakeOriginX",originZ:"param:wakeOriginZ",reset:"param:wakeReset",shiftX:"param:wakeShiftX",shiftZ:"param:wakeShiftZ",speed:"param:hullSpeed",srcBeam:"param:wakeSrcBeam",srcLength:"param:wakeSrcLength",tau:"param:wakeFoamTau",width:256}}]},{name:"wake.fft1.rows",type:"repeat",when:"param:wakeEnabled",count:8,index:"s",swap:[["wakeF","wakeG"]],body:[{name:"stage",kernel:"fft_gather_256",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"wakeF"},{name:"dst",resource:"wakeG"}],uniforms:[{name:"fft",values:{axis:0,n:256,stage:"param:s"}}]}]},{name:"wake.fft1.cols",type:"repeat",when:"param:wakeEnabled",count:8,index:"s",swap:[["wakeF","wakeG"]],body:[{name:"stage",kernel:"fft_gather_256",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"wakeF"},{name:"dst",resource:"wakeG"}],uniforms:[{name:"fft",values:{axis:1,n:256,stage:"param:s"}}]}]},{name:"wake.symbol",kernel:"wake_symbol",when:"param:wakeEnabled",target:{width:256,height:256,elioWidth:256},bindings:[{name:"fftIn",resource:"wakeF"},{name:"fftOut",resource:"wakeG"}],uniforms:[{name:"sym",values:{n:256,window:"param:wakeLength"}}]},{name:"wake.fft2.rows",type:"repeat",when:"param:wakeEnabled",count:8,index:"s",swap:[["wakeG","wakeF"]],body:[{name:"stage",kernel:"fft_gather_256",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"wakeG"},{name:"dst",resource:"wakeF"}],uniforms:[{name:"fft",values:{axis:0,n:256,stage:"param:s"}}]}]},{name:"wake.fft2.cols",type:"repeat",when:"param:wakeEnabled",count:8,index:"s",swap:[["wakeG","wakeF"]],body:[{name:"stage",kernel:"fft_gather_256",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"wakeG"},{name:"dst",resource:"wakeF"}],uniforms:[{name:"fft",values:{axis:1,n:256,stage:"param:s"}}]}]},{name:"wake.step",kernel:"wake_step",when:"param:wakeEnabled",target:{width:256,height:256,elioWidth:256},bindings:[{name:"qIn",resource:"wakeG"},{name:"cleanIn",resource:"wakeC"},{name:"stateOut",resource:"wakeB"},{name:"wakeTex",resource:"wakeTex"}],uniforms:[{name:"wstep",values:{alpha:"param:wakeAlpha",cell:"param:wakeCell",dt:"param:wakeDt",gravity:"param:wakeGEff",originX:"param:wakeOriginX",originZ:"param:wakeOriginZ",sponge:24,spongeRate:.8,width:256}}]},{name:"wake.wakeTex.mips",type:"mipmap",when:"param:wakeEnabled",resource:"wakeTex"},{name:"foam",kernel:"ocean_foam",target:{width:1024,height:1024,elioWidth:1024},bindings:[{name:"foamIn",resource:"foamA"},{name:"fieldB0",resource:"fftB_0"},{name:"fieldB1",resource:"fftB_1"},{name:"fieldB2",resource:"fftB_2"},{name:"fieldB3",resource:"fftB_3"},{name:"foamOut",resource:"foamB"},{name:"foamTex",resource:"foamTex"},{name:"wakeIn",resource:"wakeB"}],uniforms:[{name:"foam",values:{cascades:4,cell:.5,dt:"param:dt",gain:"param:foamGain",lambda:"param:lambda",n0:64,n1:256,n2:256,n3:256,omX0:"param:foamOriginMod0X",omX1:"param:foamOriginMod1X",omX2:"param:foamOriginMod2X",omX3:"param:foamOriginMod3X",omZ0:"param:foamOriginMod0Z",omZ1:"param:foamOriginMod1Z",omZ2:"param:foamOriginMod2Z",omZ3:"param:foamOriginMod3Z",originX:"param:foamOriginX",originZ:"param:foamOriginZ",shiftX:"param:foamShiftX",shiftZ:"param:foamShiftZ",size0:1600,size1:250,size2:37,size3:5.3,tau:"param:foamTau",threshold:"param:foamThreshold",wakeCell:"param:wakeCell",wakeGain:"param:wakeFoamGainEff",wakeOriginX:"param:wakeOriginModX",wakeOriginZ:"param:wakeOriginModZ",wakeRelX:"param:wakeRelX",wakeRelZ:"param:wakeRelZ",wakeWidth:256,width:1024}}]},{name:"foamTex.mips",type:"mipmap",resource:"foamTex"}],frameSwaps:[["wakeA","wakeB"],["foamA","foamB"]],exports:{disp0:"dispTex0",disp1:"dispTex1",disp2:"dispTex2",disp3:"dispTex3",foam:"foamTex",slope0:"slopeTex0",slope1:"slopeTex1",slope2:"slopeTex2",slope3:"slopeTex3",wake:"wakeTex"},exportParams:["waterModX0","waterModZ0","waterModX1","waterModZ1","waterModX2","waterModZ2","waterModX3","waterModZ3","wakeCamRelX","wakeCamRelZ","wakeOriginModX","wakeOriginModZ","wakeCell","wakeOn"]},"webgl2/medium":{name:"ocean",backend:"webgl2",tier:"medium",version:1,resources:[{name:"h0_0",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"omega_0",kind:"buffer",elementType:"f32",count:4096,width:64},{name:"fftA_0",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"fftB_0",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"fftA2_0",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"fftB2_0",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"dispTex0",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"slopeTex0",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"h0_1",kind:"buffer",elementType:"vec4<f32>",count:16384,width:128},{name:"omega_1",kind:"buffer",elementType:"f32",count:16384,width:128},{name:"fftA_1",kind:"buffer",elementType:"vec4<f32>",count:16384,width:128},{name:"fftB_1",kind:"buffer",elementType:"vec4<f32>",count:16384,width:128},{name:"fftA2_1",kind:"buffer",elementType:"vec4<f32>",count:16384,width:128},{name:"fftB2_1",kind:"buffer",elementType:"vec4<f32>",count:16384,width:128},{name:"dispTex1",kind:"texture",format:"rgba16float",width:128,height:128,mipLevels:8},{name:"slopeTex1",kind:"texture",format:"rgba16float",width:128,height:128,mipLevels:8},{name:"h0_2",kind:"buffer",elementType:"vec4<f32>",count:16384,width:128},{name:"omega_2",kind:"buffer",elementType:"f32",count:16384,width:128},{name:"fftA_2",kind:"buffer",elementType:"vec4<f32>",count:16384,width:128},{name:"fftB_2",kind:"buffer",elementType:"vec4<f32>",count:16384,width:128},{name:"fftA2_2",kind:"buffer",elementType:"vec4<f32>",count:16384,width:128},{name:"fftB2_2",kind:"buffer",elementType:"vec4<f32>",count:16384,width:128},{name:"dispTex2",kind:"texture",format:"rgba16float",width:128,height:128,mipLevels:8},{name:"slopeTex2",kind:"texture",format:"rgba16float",width:128,height:128,mipLevels:8},{name:"h0_3",kind:"buffer",elementType:"vec4<f32>",count:16384,width:128},{name:"omega_3",kind:"buffer",elementType:"f32",count:16384,width:128},{name:"fftA_3",kind:"buffer",elementType:"vec4<f32>",count:16384,width:128},{name:"fftB_3",kind:"buffer",elementType:"vec4<f32>",count:16384,width:128},{name:"fftA2_3",kind:"buffer",elementType:"vec4<f32>",count:16384,width:128},{name:"fftB2_3",kind:"buffer",elementType:"vec4<f32>",count:16384,width:128},{name:"dispTex3",kind:"texture",format:"rgba16float",width:128,height:128,mipLevels:8},{name:"slopeTex3",kind:"texture",format:"rgba16float",width:128,height:128,mipLevels:8},{name:"wakeA",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"wakeB",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"wakeF",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"wakeG",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"wakeC",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"wakeTex",kind:"texture",format:"rgba16float",width:256,height:256,mipLevels:9},{name:"foamA",kind:"buffer",elementType:"f32",count:1048576,width:1024},{name:"foamB",kind:"buffer",elementType:"f32",count:1048576,width:1024},{name:"foamTex",kind:"texture",format:"rgba16float",width:1024,height:1024,mipLevels:11}],passes:[{name:"c0.spectrum",kernel:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",target:{width:64,height:64,elioWidth:64},bindings:[{name:"h0",resource:"h0_0"},{name:"omega",resource:"omega_0"}],uniforms:[{name:"spec",values:{cascade:0,fetchKm:"param:fetch",gamma:"param:gamma",kMax:.12,kMin:.003926990816987242,loopPeriod:"param:loopPeriod",n:64,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:1600,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c0.evolve",kernel:"ocean_evolve",target:{width:64,height:64,elioWidth:64},bindings:[{name:"h0",resource:"h0_0"},{name:"omega",resource:"omega_0"},{name:"outA",resource:"fftA_0"},{name:"outB",resource:"fftB_0"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:64,t:"param:phaseTime",tile:1600}}]},{name:"c0.fft.rows",type:"repeat",count:6,index:"s",swap:[["fftA_0","fftA2_0"],["fftB_0","fftB2_0"]],body:[{name:"A",kernel:"fft_gather_64",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftA_0"},{name:"dst",resource:"fftA2_0"}],uniforms:[{name:"fft",values:{axis:0,n:64,stage:"param:s"}}]},{name:"B",kernel:"fft_gather_64",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftB_0"},{name:"dst",resource:"fftB2_0"}],uniforms:[{name:"fft",values:{axis:0,n:64,stage:"param:s"}}]}]},{name:"c0.fft.cols",type:"repeat",count:6,index:"s",swap:[["fftA_0","fftA2_0"],["fftB_0","fftB2_0"]],body:[{name:"A",kernel:"fft_gather_64",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftA_0"},{name:"dst",resource:"fftA2_0"}],uniforms:[{name:"fft",values:{axis:1,n:64,stage:"param:s"}}]},{name:"B",kernel:"fft_gather_64",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftB_0"},{name:"dst",resource:"fftB2_0"}],uniforms:[{name:"fft",values:{axis:1,n:64,stage:"param:s"}}]}]},{name:"c0.assemble",kernel:"ocean_assemble",target:{width:64,height:64,elioWidth:64},bindings:[{name:"fieldA",resource:"fftA_0"},{name:"fieldB",resource:"fftB_0"},{name:"disp",resource:"dispTex0"},{name:"slope",resource:"slopeTex0"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:64}}]},{name:"c0.dispTex0.mips",type:"mipmap",resource:"dispTex0"},{name:"c0.slopeTex0.mips",type:"mipmap",resource:"slopeTex0"},{name:"c1.spectrum",kernel:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",target:{width:128,height:128,elioWidth:128},bindings:[{name:"h0",resource:"h0_1"},{name:"omega",resource:"omega_1"}],uniforms:[{name:"spec",values:{cascade:1,fetchKm:"param:fetch",gamma:"param:gamma",kMax:.8,kMin:.12,loopPeriod:"param:loopPeriod",n:128,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:250,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c1.evolve",kernel:"ocean_evolve",target:{width:128,height:128,elioWidth:128},bindings:[{name:"h0",resource:"h0_1"},{name:"omega",resource:"omega_1"},{name:"outA",resource:"fftA_1"},{name:"outB",resource:"fftB_1"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:128,t:"param:phaseTime",tile:250}}]},{name:"c1.fft.rows",type:"repeat",count:7,index:"s",swap:[["fftA_1","fftA2_1"],["fftB_1","fftB2_1"]],body:[{name:"A",kernel:"fft_gather_128",target:{width:128,height:128,elioWidth:128},bindings:[{name:"src",resource:"fftA_1"},{name:"dst",resource:"fftA2_1"}],uniforms:[{name:"fft",values:{axis:0,n:128,stage:"param:s"}}]},{name:"B",kernel:"fft_gather_128",target:{width:128,height:128,elioWidth:128},bindings:[{name:"src",resource:"fftB_1"},{name:"dst",resource:"fftB2_1"}],uniforms:[{name:"fft",values:{axis:0,n:128,stage:"param:s"}}]}]},{name:"c1.fft.cols",type:"repeat",count:7,index:"s",swap:[["fftA_1","fftA2_1"],["fftB_1","fftB2_1"]],body:[{name:"A",kernel:"fft_gather_128",target:{width:128,height:128,elioWidth:128},bindings:[{name:"src",resource:"fftA_1"},{name:"dst",resource:"fftA2_1"}],uniforms:[{name:"fft",values:{axis:1,n:128,stage:"param:s"}}]},{name:"B",kernel:"fft_gather_128",target:{width:128,height:128,elioWidth:128},bindings:[{name:"src",resource:"fftB_1"},{name:"dst",resource:"fftB2_1"}],uniforms:[{name:"fft",values:{axis:1,n:128,stage:"param:s"}}]}]},{name:"c1.assemble",kernel:"ocean_assemble",target:{width:128,height:128,elioWidth:128},bindings:[{name:"fieldA",resource:"fftA_1"},{name:"fieldB",resource:"fftB_1"},{name:"disp",resource:"dispTex1"},{name:"slope",resource:"slopeTex1"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:128}}]},{name:"c1.dispTex1.mips",type:"mipmap",resource:"dispTex1"},{name:"c1.slopeTex1.mips",type:"mipmap",resource:"slopeTex1"},{name:"c2.spectrum",kernel:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",target:{width:128,height:128,elioWidth:128},bindings:[{name:"h0",resource:"h0_2"},{name:"omega",resource:"omega_2"}],uniforms:[{name:"spec",values:{cascade:2,fetchKm:"param:fetch",gamma:"param:gamma",kMax:5.4,kMin:.8,loopPeriod:"param:loopPeriod",n:128,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:37,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c2.evolve",kernel:"ocean_evolve",target:{width:128,height:128,elioWidth:128},bindings:[{name:"h0",resource:"h0_2"},{name:"omega",resource:"omega_2"},{name:"outA",resource:"fftA_2"},{name:"outB",resource:"fftB_2"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:128,t:"param:phaseTime",tile:37}}]},{name:"c2.fft.rows",type:"repeat",count:7,index:"s",swap:[["fftA_2","fftA2_2"],["fftB_2","fftB2_2"]],body:[{name:"A",kernel:"fft_gather_128",target:{width:128,height:128,elioWidth:128},bindings:[{name:"src",resource:"fftA_2"},{name:"dst",resource:"fftA2_2"}],uniforms:[{name:"fft",values:{axis:0,n:128,stage:"param:s"}}]},{name:"B",kernel:"fft_gather_128",target:{width:128,height:128,elioWidth:128},bindings:[{name:"src",resource:"fftB_2"},{name:"dst",resource:"fftB2_2"}],uniforms:[{name:"fft",values:{axis:0,n:128,stage:"param:s"}}]}]},{name:"c2.fft.cols",type:"repeat",count:7,index:"s",swap:[["fftA_2","fftA2_2"],["fftB_2","fftB2_2"]],body:[{name:"A",kernel:"fft_gather_128",target:{width:128,height:128,elioWidth:128},bindings:[{name:"src",resource:"fftA_2"},{name:"dst",resource:"fftA2_2"}],uniforms:[{name:"fft",values:{axis:1,n:128,stage:"param:s"}}]},{name:"B",kernel:"fft_gather_128",target:{width:128,height:128,elioWidth:128},bindings:[{name:"src",resource:"fftB_2"},{name:"dst",resource:"fftB2_2"}],uniforms:[{name:"fft",values:{axis:1,n:128,stage:"param:s"}}]}]},{name:"c2.assemble",kernel:"ocean_assemble",target:{width:128,height:128,elioWidth:128},bindings:[{name:"fieldA",resource:"fftA_2"},{name:"fieldB",resource:"fftB_2"},{name:"disp",resource:"dispTex2"},{name:"slope",resource:"slopeTex2"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:128}}]},{name:"c2.dispTex2.mips",type:"mipmap",resource:"dispTex2"},{name:"c2.slopeTex2.mips",type:"mipmap",resource:"slopeTex2"},{name:"c3.spectrum",kernel:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",target:{width:128,height:128,elioWidth:128},bindings:[{name:"h0",resource:"h0_3"},{name:"omega",resource:"omega_3"}],uniforms:[{name:"spec",values:{cascade:3,fetchKm:"param:fetch",gamma:"param:gamma",kMax:37,kMin:5.4,loopPeriod:"param:loopPeriod",n:128,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:5.3,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c3.evolve",kernel:"ocean_evolve",target:{width:128,height:128,elioWidth:128},bindings:[{name:"h0",resource:"h0_3"},{name:"omega",resource:"omega_3"},{name:"outA",resource:"fftA_3"},{name:"outB",resource:"fftB_3"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:128,t:"param:phaseTime",tile:5.3}}]},{name:"c3.fft.rows",type:"repeat",count:7,index:"s",swap:[["fftA_3","fftA2_3"],["fftB_3","fftB2_3"]],body:[{name:"A",kernel:"fft_gather_128",target:{width:128,height:128,elioWidth:128},bindings:[{name:"src",resource:"fftA_3"},{name:"dst",resource:"fftA2_3"}],uniforms:[{name:"fft",values:{axis:0,n:128,stage:"param:s"}}]},{name:"B",kernel:"fft_gather_128",target:{width:128,height:128,elioWidth:128},bindings:[{name:"src",resource:"fftB_3"},{name:"dst",resource:"fftB2_3"}],uniforms:[{name:"fft",values:{axis:0,n:128,stage:"param:s"}}]}]},{name:"c3.fft.cols",type:"repeat",count:7,index:"s",swap:[["fftA_3","fftA2_3"],["fftB_3","fftB2_3"]],body:[{name:"A",kernel:"fft_gather_128",target:{width:128,height:128,elioWidth:128},bindings:[{name:"src",resource:"fftA_3"},{name:"dst",resource:"fftA2_3"}],uniforms:[{name:"fft",values:{axis:1,n:128,stage:"param:s"}}]},{name:"B",kernel:"fft_gather_128",target:{width:128,height:128,elioWidth:128},bindings:[{name:"src",resource:"fftB_3"},{name:"dst",resource:"fftB2_3"}],uniforms:[{name:"fft",values:{axis:1,n:128,stage:"param:s"}}]}]},{name:"c3.assemble",kernel:"ocean_assemble",target:{width:128,height:128,elioWidth:128},bindings:[{name:"fieldA",resource:"fftA_3"},{name:"fieldB",resource:"fftB_3"},{name:"disp",resource:"dispTex3"},{name:"slope",resource:"slopeTex3"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:128}}]},{name:"c3.dispTex3.mips",type:"mipmap",resource:"dispTex3"},{name:"c3.slopeTex3.mips",type:"mipmap",resource:"slopeTex3"},{name:"wake.source",kernel:"wake_source",when:"param:wakeEnabled",target:{width:256,height:256,elioWidth:256},bindings:[{name:"wakeIn",resource:"wakeA"},{name:"fftOut",resource:"wakeF"},{name:"cleanOut",resource:"wakeC"}],uniforms:[{name:"wake",values:{bowFoam:"param:wakeBowFoam",bowGain:"param:wakeBowGain",cell:"param:wakeCell",depth:"param:wakeDepth",diffuse:"param:wakeDiffuse",dt:"param:wakeDt",foamSpeed:5,fullSpeed:3,heading:"param:hullHeading",hullBeam:"param:hullBeam",hullLen:"param:hullLength",hullU:"param:wakeHullU",hullV:"param:wakeHullV",jitter:"param:wakeJitter",originX:"param:wakeOriginX",originZ:"param:wakeOriginZ",reset:"param:wakeReset",shiftX:"param:wakeShiftX",shiftZ:"param:wakeShiftZ",speed:"param:hullSpeed",srcBeam:"param:wakeSrcBeam",srcLength:"param:wakeSrcLength",tau:"param:wakeFoamTau",width:256}}]},{name:"wake.fft1.rows",type:"repeat",when:"param:wakeEnabled",count:8,index:"s",swap:[["wakeF","wakeG"]],body:[{name:"stage",kernel:"fft_gather_256",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"wakeF"},{name:"dst",resource:"wakeG"}],uniforms:[{name:"fft",values:{axis:0,n:256,stage:"param:s"}}]}]},{name:"wake.fft1.cols",type:"repeat",when:"param:wakeEnabled",count:8,index:"s",swap:[["wakeF","wakeG"]],body:[{name:"stage",kernel:"fft_gather_256",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"wakeF"},{name:"dst",resource:"wakeG"}],uniforms:[{name:"fft",values:{axis:1,n:256,stage:"param:s"}}]}]},{name:"wake.symbol",kernel:"wake_symbol",when:"param:wakeEnabled",target:{width:256,height:256,elioWidth:256},bindings:[{name:"fftIn",resource:"wakeF"},{name:"fftOut",resource:"wakeG"}],uniforms:[{name:"sym",values:{n:256,window:"param:wakeLength"}}]},{name:"wake.fft2.rows",type:"repeat",when:"param:wakeEnabled",count:8,index:"s",swap:[["wakeG","wakeF"]],body:[{name:"stage",kernel:"fft_gather_256",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"wakeG"},{name:"dst",resource:"wakeF"}],uniforms:[{name:"fft",values:{axis:0,n:256,stage:"param:s"}}]}]},{name:"wake.fft2.cols",type:"repeat",when:"param:wakeEnabled",count:8,index:"s",swap:[["wakeG","wakeF"]],body:[{name:"stage",kernel:"fft_gather_256",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"wakeG"},{name:"dst",resource:"wakeF"}],uniforms:[{name:"fft",values:{axis:1,n:256,stage:"param:s"}}]}]},{name:"wake.step",kernel:"wake_step",when:"param:wakeEnabled",target:{width:256,height:256,elioWidth:256},bindings:[{name:"qIn",resource:"wakeG"},{name:"cleanIn",resource:"wakeC"},{name:"stateOut",resource:"wakeB"},{name:"wakeTex",resource:"wakeTex"}],uniforms:[{name:"wstep",values:{alpha:"param:wakeAlpha",cell:"param:wakeCell",dt:"param:wakeDt",gravity:"param:wakeGEff",originX:"param:wakeOriginX",originZ:"param:wakeOriginZ",sponge:24,spongeRate:.8,width:256}}]},{name:"wake.wakeTex.mips",type:"mipmap",when:"param:wakeEnabled",resource:"wakeTex"},{name:"foam",kernel:"ocean_foam",target:{width:1024,height:1024,elioWidth:1024},bindings:[{name:"foamIn",resource:"foamA"},{name:"fieldB0",resource:"fftB_0"},{name:"fieldB1",resource:"fftB_1"},{name:"fieldB2",resource:"fftB_2"},{name:"fieldB3",resource:"fftB_3"},{name:"foamOut",resource:"foamB"},{name:"foamTex",resource:"foamTex"},{name:"wakeIn",resource:"wakeB"}],uniforms:[{name:"foam",values:{cascades:4,cell:.5,dt:"param:dt",gain:"param:foamGain",lambda:"param:lambda",n0:64,n1:128,n2:128,n3:128,omX0:"param:foamOriginMod0X",omX1:"param:foamOriginMod1X",omX2:"param:foamOriginMod2X",omX3:"param:foamOriginMod3X",omZ0:"param:foamOriginMod0Z",omZ1:"param:foamOriginMod1Z",omZ2:"param:foamOriginMod2Z",omZ3:"param:foamOriginMod3Z",originX:"param:foamOriginX",originZ:"param:foamOriginZ",shiftX:"param:foamShiftX",shiftZ:"param:foamShiftZ",size0:1600,size1:250,size2:37,size3:5.3,tau:"param:foamTau",threshold:"param:foamThreshold",wakeCell:"param:wakeCell",wakeGain:"param:wakeFoamGainEff",wakeOriginX:"param:wakeOriginModX",wakeOriginZ:"param:wakeOriginModZ",wakeRelX:"param:wakeRelX",wakeRelZ:"param:wakeRelZ",wakeWidth:256,width:1024}}]},{name:"foamTex.mips",type:"mipmap",resource:"foamTex"}],frameSwaps:[["wakeA","wakeB"],["foamA","foamB"]],exports:{disp0:"dispTex0",disp1:"dispTex1",disp2:"dispTex2",disp3:"dispTex3",foam:"foamTex",slope0:"slopeTex0",slope1:"slopeTex1",slope2:"slopeTex2",slope3:"slopeTex3",wake:"wakeTex"},paramsFrom:"webgl2/high"},"webgl2/low":{name:"ocean",backend:"webgl2",tier:"low",version:1,resources:[{name:"h0_0",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"omega_0",kind:"buffer",elementType:"f32",count:4096,width:64},{name:"fftA_0",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"fftB_0",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"fftA2_0",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"fftB2_0",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"dispTex0",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"slopeTex0",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"h0_1",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"omega_1",kind:"buffer",elementType:"f32",count:4096,width:64},{name:"fftA_1",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"fftB_1",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"fftA2_1",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"fftB2_1",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"dispTex1",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"slopeTex1",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"h0_2",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"omega_2",kind:"buffer",elementType:"f32",count:4096,width:64},{name:"fftA_2",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"fftB_2",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"fftA2_2",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"fftB2_2",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"dispTex2",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"slopeTex2",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"wakeA",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"wakeB",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"wakeF",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"wakeG",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"wakeC",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"wakeTex",kind:"texture",format:"rgba16float",width:256,height:256,mipLevels:9},{name:"foamA",kind:"buffer",elementType:"f32",count:1048576,width:1024},{name:"foamB",kind:"buffer",elementType:"f32",count:1048576,width:1024},{name:"foamTex",kind:"texture",format:"rgba16float",width:1024,height:1024,mipLevels:11}],passes:[{name:"c0.spectrum",kernel:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",target:{width:64,height:64,elioWidth:64},bindings:[{name:"h0",resource:"h0_0"},{name:"omega",resource:"omega_0"}],uniforms:[{name:"spec",values:{cascade:0,fetchKm:"param:fetch",gamma:"param:gamma",kMax:.12,kMin:.003926990816987242,loopPeriod:"param:loopPeriod",n:64,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:1600,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c0.evolve",kernel:"ocean_evolve",target:{width:64,height:64,elioWidth:64},bindings:[{name:"h0",resource:"h0_0"},{name:"omega",resource:"omega_0"},{name:"outA",resource:"fftA_0"},{name:"outB",resource:"fftB_0"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:64,t:"param:phaseTime",tile:1600}}]},{name:"c0.fft.rows",type:"repeat",count:6,index:"s",swap:[["fftA_0","fftA2_0"],["fftB_0","fftB2_0"]],body:[{name:"A",kernel:"fft_gather_64",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftA_0"},{name:"dst",resource:"fftA2_0"}],uniforms:[{name:"fft",values:{axis:0,n:64,stage:"param:s"}}]},{name:"B",kernel:"fft_gather_64",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftB_0"},{name:"dst",resource:"fftB2_0"}],uniforms:[{name:"fft",values:{axis:0,n:64,stage:"param:s"}}]}]},{name:"c0.fft.cols",type:"repeat",count:6,index:"s",swap:[["fftA_0","fftA2_0"],["fftB_0","fftB2_0"]],body:[{name:"A",kernel:"fft_gather_64",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftA_0"},{name:"dst",resource:"fftA2_0"}],uniforms:[{name:"fft",values:{axis:1,n:64,stage:"param:s"}}]},{name:"B",kernel:"fft_gather_64",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftB_0"},{name:"dst",resource:"fftB2_0"}],uniforms:[{name:"fft",values:{axis:1,n:64,stage:"param:s"}}]}]},{name:"c0.assemble",kernel:"ocean_assemble",target:{width:64,height:64,elioWidth:64},bindings:[{name:"fieldA",resource:"fftA_0"},{name:"fieldB",resource:"fftB_0"},{name:"disp",resource:"dispTex0"},{name:"slope",resource:"slopeTex0"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:64}}]},{name:"c0.dispTex0.mips",type:"mipmap",resource:"dispTex0"},{name:"c0.slopeTex0.mips",type:"mipmap",resource:"slopeTex0"},{name:"c1.spectrum",kernel:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",target:{width:64,height:64,elioWidth:64},bindings:[{name:"h0",resource:"h0_1"},{name:"omega",resource:"omega_1"}],uniforms:[{name:"spec",values:{cascade:1,fetchKm:"param:fetch",gamma:"param:gamma",kMax:.8,kMin:.12,loopPeriod:"param:loopPeriod",n:64,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:250,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c1.evolve",kernel:"ocean_evolve",target:{width:64,height:64,elioWidth:64},bindings:[{name:"h0",resource:"h0_1"},{name:"omega",resource:"omega_1"},{name:"outA",resource:"fftA_1"},{name:"outB",resource:"fftB_1"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:64,t:"param:phaseTime",tile:250}}]},{name:"c1.fft.rows",type:"repeat",count:6,index:"s",swap:[["fftA_1","fftA2_1"],["fftB_1","fftB2_1"]],body:[{name:"A",kernel:"fft_gather_64",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftA_1"},{name:"dst",resource:"fftA2_1"}],uniforms:[{name:"fft",values:{axis:0,n:64,stage:"param:s"}}]},{name:"B",kernel:"fft_gather_64",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftB_1"},{name:"dst",resource:"fftB2_1"}],uniforms:[{name:"fft",values:{axis:0,n:64,stage:"param:s"}}]}]},{name:"c1.fft.cols",type:"repeat",count:6,index:"s",swap:[["fftA_1","fftA2_1"],["fftB_1","fftB2_1"]],body:[{name:"A",kernel:"fft_gather_64",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftA_1"},{name:"dst",resource:"fftA2_1"}],uniforms:[{name:"fft",values:{axis:1,n:64,stage:"param:s"}}]},{name:"B",kernel:"fft_gather_64",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftB_1"},{name:"dst",resource:"fftB2_1"}],uniforms:[{name:"fft",values:{axis:1,n:64,stage:"param:s"}}]}]},{name:"c1.assemble",kernel:"ocean_assemble",target:{width:64,height:64,elioWidth:64},bindings:[{name:"fieldA",resource:"fftA_1"},{name:"fieldB",resource:"fftB_1"},{name:"disp",resource:"dispTex1"},{name:"slope",resource:"slopeTex1"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:64}}]},{name:"c1.dispTex1.mips",type:"mipmap",resource:"dispTex1"},{name:"c1.slopeTex1.mips",type:"mipmap",resource:"slopeTex1"},{name:"c2.spectrum",kernel:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",target:{width:64,height:64,elioWidth:64},bindings:[{name:"h0",resource:"h0_2"},{name:"omega",resource:"omega_2"}],uniforms:[{name:"spec",values:{cascade:2,fetchKm:"param:fetch",gamma:"param:gamma",kMax:5.4,kMin:.8,loopPeriod:"param:loopPeriod",n:64,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:37,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c2.evolve",kernel:"ocean_evolve",target:{width:64,height:64,elioWidth:64},bindings:[{name:"h0",resource:"h0_2"},{name:"omega",resource:"omega_2"},{name:"outA",resource:"fftA_2"},{name:"outB",resource:"fftB_2"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:64,t:"param:phaseTime",tile:37}}]},{name:"c2.fft.rows",type:"repeat",count:6,index:"s",swap:[["fftA_2","fftA2_2"],["fftB_2","fftB2_2"]],body:[{name:"A",kernel:"fft_gather_64",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftA_2"},{name:"dst",resource:"fftA2_2"}],uniforms:[{name:"fft",values:{axis:0,n:64,stage:"param:s"}}]},{name:"B",kernel:"fft_gather_64",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftB_2"},{name:"dst",resource:"fftB2_2"}],uniforms:[{name:"fft",values:{axis:0,n:64,stage:"param:s"}}]}]},{name:"c2.fft.cols",type:"repeat",count:6,index:"s",swap:[["fftA_2","fftA2_2"],["fftB_2","fftB2_2"]],body:[{name:"A",kernel:"fft_gather_64",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftA_2"},{name:"dst",resource:"fftA2_2"}],uniforms:[{name:"fft",values:{axis:1,n:64,stage:"param:s"}}]},{name:"B",kernel:"fft_gather_64",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftB_2"},{name:"dst",resource:"fftB2_2"}],uniforms:[{name:"fft",values:{axis:1,n:64,stage:"param:s"}}]}]},{name:"c2.assemble",kernel:"ocean_assemble",target:{width:64,height:64,elioWidth:64},bindings:[{name:"fieldA",resource:"fftA_2"},{name:"fieldB",resource:"fftB_2"},{name:"disp",resource:"dispTex2"},{name:"slope",resource:"slopeTex2"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:64}}]},{name:"c2.dispTex2.mips",type:"mipmap",resource:"dispTex2"},{name:"c2.slopeTex2.mips",type:"mipmap",resource:"slopeTex2"},{name:"wake.source",kernel:"wake_source",when:"param:wakeEnabled",target:{width:256,height:256,elioWidth:256},bindings:[{name:"wakeIn",resource:"wakeA"},{name:"fftOut",resource:"wakeF"},{name:"cleanOut",resource:"wakeC"}],uniforms:[{name:"wake",values:{bowFoam:"param:wakeBowFoam",bowGain:"param:wakeBowGain",cell:"param:wakeCell",depth:"param:wakeDepth",diffuse:"param:wakeDiffuse",dt:"param:wakeDt",foamSpeed:5,fullSpeed:3,heading:"param:hullHeading",hullBeam:"param:hullBeam",hullLen:"param:hullLength",hullU:"param:wakeHullU",hullV:"param:wakeHullV",jitter:"param:wakeJitter",originX:"param:wakeOriginX",originZ:"param:wakeOriginZ",reset:"param:wakeReset",shiftX:"param:wakeShiftX",shiftZ:"param:wakeShiftZ",speed:"param:hullSpeed",srcBeam:"param:wakeSrcBeam",srcLength:"param:wakeSrcLength",tau:"param:wakeFoamTau",width:256}}]},{name:"wake.fft1.rows",type:"repeat",when:"param:wakeEnabled",count:8,index:"s",swap:[["wakeF","wakeG"]],body:[{name:"stage",kernel:"fft_gather_256",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"wakeF"},{name:"dst",resource:"wakeG"}],uniforms:[{name:"fft",values:{axis:0,n:256,stage:"param:s"}}]}]},{name:"wake.fft1.cols",type:"repeat",when:"param:wakeEnabled",count:8,index:"s",swap:[["wakeF","wakeG"]],body:[{name:"stage",kernel:"fft_gather_256",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"wakeF"},{name:"dst",resource:"wakeG"}],uniforms:[{name:"fft",values:{axis:1,n:256,stage:"param:s"}}]}]},{name:"wake.symbol",kernel:"wake_symbol",when:"param:wakeEnabled",target:{width:256,height:256,elioWidth:256},bindings:[{name:"fftIn",resource:"wakeF"},{name:"fftOut",resource:"wakeG"}],uniforms:[{name:"sym",values:{n:256,window:"param:wakeLength"}}]},{name:"wake.fft2.rows",type:"repeat",when:"param:wakeEnabled",count:8,index:"s",swap:[["wakeG","wakeF"]],body:[{name:"stage",kernel:"fft_gather_256",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"wakeG"},{name:"dst",resource:"wakeF"}],uniforms:[{name:"fft",values:{axis:0,n:256,stage:"param:s"}}]}]},{name:"wake.fft2.cols",type:"repeat",when:"param:wakeEnabled",count:8,index:"s",swap:[["wakeG","wakeF"]],body:[{name:"stage",kernel:"fft_gather_256",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"wakeG"},{name:"dst",resource:"wakeF"}],uniforms:[{name:"fft",values:{axis:1,n:256,stage:"param:s"}}]}]},{name:"wake.step",kernel:"wake_step",when:"param:wakeEnabled",target:{width:256,height:256,elioWidth:256},bindings:[{name:"qIn",resource:"wakeG"},{name:"cleanIn",resource:"wakeC"},{name:"stateOut",resource:"wakeB"},{name:"wakeTex",resource:"wakeTex"}],uniforms:[{name:"wstep",values:{alpha:"param:wakeAlpha",cell:"param:wakeCell",dt:"param:wakeDt",gravity:"param:wakeGEff",originX:"param:wakeOriginX",originZ:"param:wakeOriginZ",sponge:24,spongeRate:.8,width:256}}]},{name:"wake.wakeTex.mips",type:"mipmap",when:"param:wakeEnabled",resource:"wakeTex"},{name:"foam",kernel:"ocean_foam",target:{width:1024,height:1024,elioWidth:1024},bindings:[{name:"foamIn",resource:"foamA"},{name:"fieldB0",resource:"fftB_0"},{name:"fieldB1",resource:"fftB_1"},{name:"fieldB2",resource:"fftB_2"},{name:"fieldB3",resource:"fftB_2"},{name:"foamOut",resource:"foamB"},{name:"foamTex",resource:"foamTex"},{name:"wakeIn",resource:"wakeB"}],uniforms:[{name:"foam",values:{cascades:3,cell:.5,dt:"param:dt",gain:"param:foamGain",lambda:"param:lambda",n0:64,n1:64,n2:64,n3:64,omX0:"param:foamOriginMod0X",omX1:"param:foamOriginMod1X",omX2:"param:foamOriginMod2X",omX3:"param:foamOriginMod2X",omZ0:"param:foamOriginMod0Z",omZ1:"param:foamOriginMod1Z",omZ2:"param:foamOriginMod2Z",omZ3:"param:foamOriginMod2Z",originX:"param:foamOriginX",originZ:"param:foamOriginZ",shiftX:"param:foamShiftX",shiftZ:"param:foamShiftZ",size0:1600,size1:250,size2:37,size3:37,tau:"param:foamTau",threshold:"param:foamThreshold",wakeCell:"param:wakeCell",wakeGain:"param:wakeFoamGainEff",wakeOriginX:"param:wakeOriginModX",wakeOriginZ:"param:wakeOriginModZ",wakeRelX:"param:wakeRelX",wakeRelZ:"param:wakeRelZ",wakeWidth:256,width:1024}}]},{name:"foamTex.mips",type:"mipmap",resource:"foamTex"}],frameSwaps:[["wakeA","wakeB"],["foamA","foamB"]],exports:{disp0:"dispTex0",disp1:"dispTex1",disp2:"dispTex2",foam:"foamTex",slope0:"slopeTex0",slope1:"slopeTex1",slope2:"slopeTex2",wake:"wakeTex"},paramsFrom:"webgl2/high"}}})})(),(function(){function re(s){return s.replace(/_([a-z0-9])/g,function(x,B){return B.toUpperCase()})}function S(s){return typeof s=="boolean"?s?1:0:Number(s)}function L(s,x,B){if(typeof s=="string"){if(s.indexOf("param:")!==0)throw new Error("elio-graph: "+B+": bad value "+s);const F=s.slice(6);if(!(F in x))throw new Error("elio-graph: parameter "+F+" was not supplied");return S(x[F])}return S(s)}function C(s,x,B,F){const _=new ArrayBuffer(Math.max(16,Math.ceil(s.size/16)*16)),y=new DataView(_),k={};for(const P of s.fields){let T=x[P.name];if(T===void 0){if(!/^_pad/.test(P.name))throw new Error("elio-graph: "+F+": uniform "+s.struct+" lacks a value for "+P.name);T=0}k[P.name]=!0;const ne=L(T,B,F);if(P.type==="u32")y.setUint32(P.offset,ne>>>0,!0);else if(P.type==="i32")y.setInt32(P.offset,ne|0,!0);else if(P.type==="f32")y.setFloat32(P.offset,ne,!0);else throw new Error("elio-graph: "+F+": unsupported uniform type "+P.type)}for(const P of Object.keys(x))if(!k[P])throw new Error("elio-graph: "+F+": uniform "+s.struct+" has no field "+P);return _}function g(s){for(const x of Object.keys(s))if(typeof s[x]=="string")return!1;return!0}const a={x:1,z:1,heading:1,speed:1},me={time:1,dt:1,cameraX:1,cameraY:1,cameraZ:1},ie={add:function(s,x){return s+x},sub:function(s,x){return s-x},mul:function(s,x){return s*x},div:function(s,x){return x===0?0:s/x},mod:function(s,x){return x===0?0:s-Math.floor(s/x)*x},floor:function(s){return Math.floor(s)},sin:function(s){return Math.sin(s)},cos:function(s){return Math.cos(s)},min:function(s,x){return Math.min(s,x)},max:function(s,x){return Math.max(s,x)}};function le(s,x){return/\[\]$/.test(s)?Float32Array.from(x||[]):(x=Number(x),Number.isFinite(x)?s==="bool"?x?1:0:s==="u32"?Math.max(0,Math.floor(x))>>>0:s==="i32"?Math.trunc(x)|0:x:0)}function ce(s){const x={},B={},F={},_={},y={};let k=!0,P=null,T={};const ne={},be={},xe=function(v){return v.tween>0?P===null?v.tween:P:0};for(const v of s){const Z=v.source||{kind:"value",value:0};Z.kind==="value"&&(y[v.name]=le(v.type,/\[\]$/.test(v.type)?Z.value:Z.value||0))}const X=function(v,Z){const A=Z||x;if(typeof v=="number")return v;if(typeof v=="string"&&v.indexOf("param:")===0){const $=v.slice(6);return $ in A?A[$]:0}if(v&&typeof v=="object"&&typeof v.op=="string"){const $=ie[v.op];if(!$)throw new Error("elio-graph: unknown op "+v.op);return $(X(v.args[0],A),v.args.length>1?X(v.args[1],A):0)}throw new Error("elio-graph: bad argument "+v)},z={current:x,changed:{},setNodes:function(v){T=v||{}},setTween:function(v){P=Number.isFinite(v)&&v>=0?v:null},set:function(v,Z){const A=s.find(function(f){return f.name===v});if(!A||A.source&&A.source.kind!=="value")throw new Error("elio-graph: "+v+" is not a value parameter");const $=le(A.type,Z);if($!==y[v]){if(typeof $!="number"){y[v]=$;return}y[v]=$,xe(A)>0&&!k?F[v]={from:x[v],start:null}:delete F[v]}},snapshot:function(){return Object.assign({},x)},derive:function(v,Z){const A=Object.assign({},x);for(const $ of s){const f=$.source||{kind:"value",value:0};if(f.kind==="frame"&&f.name==="dt")A[$.name]=le($.type,v);else if(f.kind==="expr")A[$.name]=le($.type,ie[f.op](X(f.args[0],A),f.args.length>1?X(f.args[1],A):0));else if(f.kind==="delta"){const p=String(f.of).replace(/^param:/,"");A[$.name]=le($.type,Z?(A[p]||0)-(Z[p]||0):0)}}return A},evaluate:function(v){const Z=Number(v.time)||0,A=Number(v.dt)||0,$=Number.isFinite(v.accumulateDt)?Number(v.accumulateDt):A,f={},p=function(r){if(r in f)return f[r];const u=typeof v.nodePose=="function"?v.nodePose(r):null;if(!u)return f[r]=null;const D=ne[r];let R=Number(u.speed);return Number.isFinite(R)||(R=D&&A>0?Math.hypot(u.x-D.x,u.z-D.z)/A:0),f[r]={x:Number(u.x)||0,z:Number(u.z)||0,heading:Number(u.heading)||0,speed:R}};for(const r of s){const u=r.source||{kind:"value",value:0};let D=0;if(u.kind==="value"){D=y[r.name];const R=F[r.name];if(R){R.start===null&&(R.start=Z);const te=Math.min(1,Math.max(0,(Z-R.start)/xe(r)));te>=1?delete F[r.name]:D=R.from+(D-R.from)*te}}else if(u.kind==="frame"){if(!me[u.name])throw new Error("elio-graph: unknown frame source "+u.name);D=u.name==="time"?Z:u.name==="dt"?A:Number(v[u.name])||0}else if(u.kind==="expr"){const R=ie[u.op];if(!R)throw new Error("elio-graph: unknown op "+u.op);D=R(X(u.args[0]),u.args.length>1?X(u.args[1]):0)}else if(u.kind==="node"){if(!a[u.field])throw new Error("elio-graph: unknown node field "+u.field);const R=p(u.node in T?T[u.node]:u.node);D=R?R[u.field]:u.field==="speed"?0:be[r.name]||0,be[r.name]=D}else if(u.kind==="delta"){const R=String(u.of).replace(/^param:/,"");D=k?0:(x[R]||0)-(B[R]||0)}else if(u.kind==="accumulate")_[r.name]=(_[r.name]||0)+X(u.of)*$,D=_[r.name];else throw new Error("elio-graph: unknown parameter source "+u.kind);x[r.name]=/\[\]$/.test(r.type)?y[r.name]:le(r.type,D)}for(const r of Object.keys(f))f[r]&&(ne[r]={x:f[r].x,z:f[r].z});for(const r of s)z.changed[r.name]=k||x[r.name]!==B[r.name];for(const r of s)B[r.name]=x[r.name];k=!1}};return z}function t(s,x,B){if(!s||s==="always")return!0;if(s.indexOf("param:")===0)return!!x[s.slice(6)];if(s.indexOf("changed:")===0)return s.slice(8).split(",").some(function(F){return!!B[F.trim()]});throw new Error("elio-graph: bad when "+s)}function m(s,x){const B=Object.assign({},s);if(s.paramsFrom){const _=x&&x.graphs&&x.graphs[s.paramsFrom];if(!_||_.backend!==s.backend||_.paramsFrom)throw new Error("elio-graph: paramsFrom "+s.paramsFrom+" does not name a graph of "+s.backend+" that owns its parameters");B.params=_.params,B.exportParams=_.exportParams}delete B.paramsFrom,B.params=B.params||[],B.exportParams=B.exportParams||[],B.frameSwaps=B.frameSwaps||[],B.exports=B.exports||{};const F=function(_){const y=Object.assign({},_);if(y.type=_.type||"kernel",y.when=_.when||"always",y.type==="kernel"){const k=x&&x.kernels&&x.kernels[re(_.kernel)];!y.entry&&k&&(y.entry=k.entry),_.bindings&&(y.bindings=_.bindings.map(function(P){return Object.assign({level:0},P)})),y.uniforms||(y.uniforms=[])}return y.type==="repeat"&&(y.body=(_.body||[]).map(F)),y};return B.passes=(s.passes||[]).map(F),B}function h(s,x){const B=x+(s.offset||0);return s.shr?Math.max(s.shr>>B,1):B}function H(s){const x={},B={},F={};for(const T of s.resources)x[T.name]=T;for(const T of s.params)F[T.name]=!0;const _=function(T){return T in B?B[T]:T},y=function(T,ne,be,xe){const X=[];for(const z of T){if(z.type!=="repeat"){X.push(k(z,ne,be,xe));continue}const v=be+z.name;if(!(z.count>=1))throw new Error("elio-graph: repeat "+v+": count "+z.count+", want at least 1");if(!z.index||F[z.index])throw new Error("elio-graph: repeat "+v+": index "+z.index+" is empty or names a declared param");if(!z.body||!z.body.length)throw new Error("elio-graph: repeat "+v+": body is empty");if(z.index in ne)throw new Error("elio-graph: repeat "+v+": index "+z.index+" is already in use by an outer repeat");for(const A of z.swap||[])if(!x[A[0]]||!x[A[1]])throw new Error("elio-graph: repeat "+v+": swap names an unknown resource");const Z=z.when&&z.when!=="always"?z.when:xe;for(let A=0;A<z.count;A++){const $=Object.assign({},ne,{[z.index]:A});for(const f of y(z.body,$,v+"["+A+"].",Z))X.push(f);for(const f of z.swap||[]){const p=_(f[0]),r=_(f[1]);B[f[0]]=r,B[f[1]]=p}}}return X},k=function(T,ne,be,xe){const X=Object.assign({},T,{name:be+T.name});return xe&&(!T.when||T.when==="always")&&(X.when=xe),T.resource&&(X.resource=_(T.resource)),T.from&&(X.from=_(T.from)),T.to&&(X.to=_(T.to)),T.bindings&&(X.bindings=T.bindings.map(function(z){const v=Object.assign({},z,{resource:_(z.resource)});if(z.levelAt){if(!(z.levelAt.index in ne))throw new Error("elio-graph: pass "+X.name+": index "+z.levelAt.index+" is not in scope");v.level=h(z.levelAt,ne[z.levelAt.index]),delete v.levelAt}return v})),T.uniforms&&(X.uniforms=T.uniforms.map(function(z){const v={};for(const Z of Object.keys(z.values)){let A=z.values[Z];if(A&&typeof A=="object"){if(!(A.index in ne))throw new Error("elio-graph: pass "+X.name+": index "+A.index+" is not in scope");A=h(A,ne[A.index])}else typeof A=="string"&&A.indexOf("param:")===0&&A.slice(6)in ne&&(A=ne[A.slice(6)]);v[Z]=A}return Object.assign({},z,{values:v})})),X},P=y(s.passes,{},"","");for(const T of Object.keys(B))if(B[T]!==T)throw new Error("elio-graph: swaps are not restored: "+T+" still refers to "+B[T]+" at the end of the passes");return P}function pe(s,x,B){if(!s||s.version!==1)throw new Error("elio-graph: unsupported graph version");if(s.backend!==B)throw new Error("elio-graph: graph is for "+s.backend+", not "+B);const F={};for(const k of s.resources)F[k.name]=k;const _={};for(const k of s.params)_[k.name]=!0;const y=function(k){if(typeof k=="string"&&k.indexOf("param:")===0&&!_[k.slice(6)])throw new Error("elio-graph: undeclared parameter "+k.slice(6))};for(const k of H(s)){const P=k.when||"always";if(P.indexOf("param:")===0)y(P);else if(P.indexOf("changed:")===0){for(const T of P.slice(8).split(","))if(!_[T.trim()])throw new Error("elio-graph: undeclared parameter "+T.trim())}else if(P!=="always")throw new Error("elio-graph: pass "+k.name+": bad when "+P);if(k.type==="mipmap"){if(!F[k.resource])throw new Error("elio-graph: pass "+k.name+": unknown resource "+k.resource);continue}if(k.type==="copy"){if(B!=="webgl2")throw new Error("elio-graph: pass "+k.name+": copy passes exist only on webgl2");for(const T of[k.from,k.to])if(!F[T]||F[T].kind!=="buffer")throw new Error("elio-graph: pass "+k.name+": unknown resource "+T+" (a copy joins two buffers)");continue}if(!x.kernels[re(k.kernel)])throw new Error("elio-graph: pass "+k.name+": the module has no kernel "+k.kernel);for(const T of k.bindings)if(!F[T.resource])throw new Error("elio-graph: pass "+k.name+": unknown resource "+T.resource);for(const T of k.uniforms||[])for(const ne of Object.keys(T.values))y(T.values[ne])}for(const k of s.exportParams||[])if(!_[k])throw new Error("elio-graph: exportParams names undeclared parameter "+k);for(const k of s.resources)if(k.feed&&!_[k.feed])throw new Error("elio-graph: resource "+k.name+" is fed by undeclared parameter "+k.feed);for(const k of s.frameSwaps||[])for(const P of k)if(!F[P])throw new Error("elio-graph: frameSwaps names unknown resource "+P);for(const k of Object.keys(s.exports||{})){const P=F[s.exports[k]];if(!P||P.kind!=="texture"&&B!=="webgpu")throw new Error("elio-graph: export "+k+" must name a texture resource")}}function he(s){const x=s.backend,B=s.module,F=s.host||{},_={graph:null,backend:x,ready:!1,failed:null,disposed:!1,buildCount:0,run:Z,nodePose:ne,gpuTiming:be,setValue:v,setNodes:xe,setTween:X,value:z,hasParam:T,params:null,exportParams:A,export:$,dispose:p,fail:f};let y=null,k=null;const P=function(r){if(!_.failed){if(_.failed=r&&r.fallback||"error",_.error=r,y){try{y.dispose()}catch{}y=null}_.ready=!1,s.onFail&&s.onFail(_.failed,r),!(r&&r.fallback)&&typeof console<"u"&&console.warn&&console.warn("elio-graph: graph disabled: "+(r&&r.message?r.message:String(r)))}};try{let r=s.graph||B.graphs&&B.graphs[x+"/"+(s.tier||"high")];if(!r)throw Object.assign(new Error("elio-graph: no graph for "+x+"/"+s.tier),{fallback:"no-graph"});r=m(r,B),pe(r,B,x);const u=window.__gosx_elio_graph.executors[x];if(!u)throw Object.assign(new Error("elio-graph: no executor for "+x),{fallback:"no-executor"});_.graph=r,k=_.params=ce(r.params),y=u({steps:H(r).map(function(D){return{pass:D}}),graph:r,module:B,host:F,pack:C,isStatic:g,kernelKey:re,holds:t,label:s.label||r.name}),_.buildCount=1,_.ready=y.isReady()}catch(r){P(r)}function T(r){return!!(_.graph&&_.graph.params.some(function(u){return u.name===r}))}function ne(r){return F.nodePose?F.nodePose(r):null}function be(){return y&&y.gpuTiming?y.gpuTiming():null}function xe(r){k&&k.setNodes(r)}function X(r){k&&k.setTween(r)}function z(r){const u=y&&y.held?y.held(r):void 0;return u!==void 0?u:k&&r in k.current?k.current[r]:0}function v(r,u){if(!(!k||_.failed))try{k.set(r,u)}catch(D){P(D)}}function Z(r){if(!y||_.failed||_.disposed)return!1;try{if(_.ready=y.isReady(),!_.ready)return!1;const u=r&&!r.nodePose&&F.nodePose?Object.assign({},r,{nodePose:F.nodePose}):r||{};return k.evaluate(u),y.run(k.current,u,k.changed,k)!==!1}catch(u){return P(u),!1}}function A(){const r={};if(k&&_.graph)for(const u of _.graph.exportParams||[])r[u]=z(u);return r}function $(r){return y&&!_.failed?y.export(r):null}function f(r){P({message:r,fallback:r})}function p(){if(!_.disposed&&(_.disposed=!0,_.ready=!1,y)){try{y.dispose()}catch{}y=null}}return _}window.__gosx_elio_graph={create:he,expand:H,normalize:m,pack:C,kernelKey:re,check:pe,holds:t,createParams:ce,executors:{}}})(),(function(){const re=`#version 300 es
void main() { vec2 p = vec2(float((gl_VertexID << 1) & 2), float(gl_VertexID & 2)); gl_Position = vec4(p * 2. - 1., 0., 1.); }`;function S(g){return{draw:g.getParameter(g.DRAW_FRAMEBUFFER_BINDING),read:g.getParameter(g.READ_FRAMEBUFFER_BINDING),viewport:g.getParameter(g.VIEWPORT),program:g.getParameter(g.CURRENT_PROGRAM),vao:g.getParameter(g.VERTEX_ARRAY_BINDING),active:g.getParameter(g.ACTIVE_TEXTURE),blend:g.isEnabled(g.BLEND),depth:g.isEnabled(g.DEPTH_TEST),cull:g.isEnabled(g.CULL_FACE),scissor:g.isEnabled(g.SCISSOR_TEST),colorMask:g.getParameter(g.COLOR_WRITEMASK),depthMask:g.getParameter(g.DEPTH_WRITEMASK)}}function L(g,a){g.bindBuffer(g.UNIFORM_BUFFER,null),g.bindBufferBase(g.UNIFORM_BUFFER,0,null),g.bindBuffer(g.PIXEL_PACK_BUFFER,null),g.bindBuffer(g.PIXEL_UNPACK_BUFFER,null),g.bindFramebuffer(g.DRAW_FRAMEBUFFER,a.draw),g.bindFramebuffer(g.READ_FRAMEBUFFER,a.read),g.viewport(a.viewport[0],a.viewport[1],a.viewport[2],a.viewport[3]),g.useProgram(a.program),g.bindVertexArray(a.vao),g.activeTexture(a.active);const me=function(ie,le){le?g.enable(ie):g.disable(ie)};me(g.BLEND,a.blend),me(g.DEPTH_TEST,a.depth),me(g.CULL_FACE,a.cull),me(g.SCISSOR_TEST,a.scissor),g.colorMask(a.colorMask[0],a.colorMask[1],a.colorMask[2],a.colorMask[3]),g.depthMask(a.depthMask)}function C(g){const a=g.host.gl,me=g.graph,ie=g.module,le=ie.kernels,ce=g.host.firstUnit||4;if(!(g.host.caps?g.host.caps(a):{}).colorBufferFloat)throw Object.assign(new Error("EXT_color_buffer_float is not available"),{fallback:"EXT_color_buffer_float"});const m=function(i,l){const b=a.getParameter(i);return typeof b=="number"?b:l};if(m(a.MAX_DRAW_BUFFERS,0)<2||m(a.MAX_TEXTURE_IMAGE_UNITS,0)<ce+5)throw Object.assign(new Error("too few texture units or draw buffers"),{fallback:"texture-units"});for(const i of g.steps){const l=i.pass;if(!l||l.type!=="kernel")continue;const b=le[g.kernelKey(l.kernel)];if(!b.glsl||!b.gatherLayout)throw Object.assign(new Error("kernel "+l.kernel+" has no GLSL gather form"),{fallback:"kernel:"+l.kernel})}const h=[],H=[],pe=[],he=[],s=Object.create(null),x={};for(const i of Object.keys(me.exports||{}))x[me.exports[i]]=!0;const B=a.createVertexArray(),F={split:!1,disposed:!1};let _=null;const y=Object.create(null),k=new Map,P=new Map;let T=0;const ne={rgba16float:a.RGBA16F,rgba32float:a.RGBA32F,r32float:a.R32F};function be(i,l,b,c,V){const Q=a.createTexture();return h.push(Q),a.activeTexture(a.TEXTURE0+ce),a.bindTexture(a.TEXTURE_2D,Q),a.texStorage2D(a.TEXTURE_2D,c,b,i,l),a.texParameteri(a.TEXTURE_2D,a.TEXTURE_MIN_FILTER,V?c>1?a.LINEAR_MIPMAP_LINEAR:a.LINEAR:a.NEAREST),a.texParameteri(a.TEXTURE_2D,a.TEXTURE_MAG_FILTER,V?a.LINEAR:a.NEAREST),a.texParameteri(a.TEXTURE_2D,a.TEXTURE_WRAP_S,V?a.REPEAT:a.CLAMP_TO_EDGE),a.texParameteri(a.TEXTURE_2D,a.TEXTURE_WRAP_T,V?a.REPEAT:a.CLAMP_TO_EDGE),Q}function xe(i,l){const b=a.createShader(i);if(he.push(b),a.shaderSource(b,l),a.compileShader(b),!a.getShaderParameter(b,a.COMPILE_STATUS))throw new Error("elio-graph: shader did not compile: "+a.getShaderInfoLog(b));return b}function X(i){if(s[i])return s[i];const l=le[g.kernelKey(i)];_||(_=xe(a.VERTEX_SHADER,re));const b=xe(a.FRAGMENT_SHADER,l.glsl),c=a.createProgram();a.attachShader(c,_),a.attachShader(c,b),a.linkProgram(c);const V=l.gatherLayout,Q=s[i]={handle:c,layout:V,kernel:l};if(!a.getProgramParameter(c,a.LINK_STATUS))throw new Error("elio-graph: program "+i+" did not link: "+a.getProgramInfoLog(c));return a.useProgram(c),Q.width=a.getUniformLocation(c,"elio_width"),Q.lens=V.inputs.map(function(ae){return a.getUniformLocation(c,ae.length)}),Q.widths=V.inputs.map(function(ae){return ae.width?a.getUniformLocation(c,ae.width):null}),V.inputs.forEach(function(ae,M){a.uniform1i(a.getUniformLocation(c,ae.sampler),ce+M)}),V.uniforms.forEach(function(ae,M){a.uniformBlockBinding(c,a.getUniformBlockIndex(c,ae.block),M)}),Q}function z(i,l){const c=(i.uniformsStd140||i.uniforms).find(function(V){return V.name===l});if(!c)throw new Error("elio-graph: kernel has no uniform block "+l);return c}function v(){for(const i of me.resources){let l;if(i.kind==="buffer"){const b=Math.ceil(i.count/i.width);l={name:i.name,id:++T,kind:"buffer",version:0,w:i.width,h:b,levels:1,tex:be(i.width,b,a.RGBA32F,1,!1)}}else{const b=i.mipLevels||1,c=ne[i.format];if(!c)throw new Error("elio-graph: unsupported format "+i.format);const V=i.format==="rgba16float"&&(b>1||!!x[i.name]);l={name:i.name,id:++T,kind:"texture",version:0,w:i.width,h:i.height,levels:b,tex:be(i.width,i.height,c,b,V)}}y[i.name]=l}}function Z(i){const l=a.createFramebuffer();H.push(l),a.bindFramebuffer(a.FRAMEBUFFER,l);const b=[];for(const c of i){for(a.framebufferTexture2D(a.FRAMEBUFFER,a.COLOR_ATTACHMENT0+c.location,a.TEXTURE_2D,c.res.tex,c.level);b.length<=c.location;)b.push(a.NONE);b[c.location]=a.COLOR_ATTACHMENT0+c.location}return a.drawBuffers(b),{fbo:l,complete:a.checkFramebufferStatus(a.FRAMEBUFFER)===a.FRAMEBUFFER_COMPLETE}}function A(i){const l=i.map(function(Q){return Q.res.id+"@"+Q.level+":"+Q.location}).join(","),b=k.get(l+(F.split?"s":""));if(b)return b;let c;const V=function(Q){const ae=Z([Q]);if(!ae.complete)throw new Error("elio-graph: framebuffer incomplete");return ae};if(i.length===1)c=[V(i[0])];else{const Q=F.split?null:Z(i);Q&&Q.complete?c=[Q]:(F.split=!0,c=i.map(V))}return k.set(l+(F.split?"s":""),c),c}function $(){if(!F.disposed){F.disposed=!0;for(const i of h)a.deleteTexture(i);for(const i of H)a.deleteFramebuffer(i);for(const i of pe)a.deleteBuffer(i);for(const i of Object.keys(s))a.deleteProgram(s[i].handle);for(const i of he)a.deleteShader(i);a.deleteVertexArray(B),h.length=H.length=pe.length=0,k.clear(),P.clear()}}let f=[];const p=S(a);try{f=g.steps.map(function(i){const l=i.pass;if(l.type==="mipmap"||l.type==="copy")return{pass:l};const b=X(l.kernel),c=b.layout,V=c.inputs.map(function(M){const ke=l.bindings.find(function(e){return e.name===M.name});if(!ke)throw new Error("elio-graph: pass "+l.name+" does not bind input "+M.name);return ke}),Q=c.outputs.map(function(M){const ke=l.bindings.find(function(e){return e.name===M.name});if(!ke)throw new Error("elio-graph: pass "+l.name+" does not bind output "+M.name);return{binding:ke,location:M.location}}),ae=(l.uniforms||[]).map(function(M){const ke=c.uniforms.findIndex(function(d){return d.name===M.name});if(ke<0)throw new Error("elio-graph: pass "+l.name+" has no uniform block "+M.name);const e=z(b.kernel,M.name),n=a.createBuffer(),o=Math.max(16,Math.ceil(e.size/16)*16);return pe.push(n),a.bindBuffer(a.UNIFORM_BUFFER,n),a.bufferData(a.UNIFORM_BUFFER,o,a.DYNAMIC_DRAW),{index:ke,layout:e,values:M.values,buffer:n,size:o,written:!1,constant:g.isStatic(M.values)}});return{pass:l,p:b,inputs:V,outputs:Q,blocks:ae}}),v()}catch(i){throw $(),i}finally{L(a,p)}function r(i){const l=y[i.from],b=y[i.to];if(l.w*l.h!==b.w*b.h||b.w!==i.width)throw new Error("elio-graph: pass "+i.name+" cannot copy "+l.name+" into "+b.name+" at width "+i.width);if(b.copiedFrom===l&&b.copiedVersion===l.version)return;let c=P.get(b.id);c||(c=a.createBuffer(),pe.push(c),a.bindBuffer(a.PIXEL_PACK_BUFFER,c),a.bufferData(a.PIXEL_PACK_BUFFER,l.w*l.h*16,a.STREAM_COPY),a.bindBuffer(a.PIXEL_PACK_BUFFER,null),P.set(b.id,c)),a.bindFramebuffer(a.READ_FRAMEBUFFER,A([{res:l,level:0,location:0}])[0].fbo),a.bindBuffer(a.PIXEL_PACK_BUFFER,c),a.readPixels(0,0,l.w,l.h,a.RGBA,a.FLOAT,0),a.bindBuffer(a.PIXEL_PACK_BUFFER,null),a.bindBuffer(a.PIXEL_UNPACK_BUFFER,c),a.activeTexture(a.TEXTURE0+ce),a.bindTexture(a.TEXTURE_2D,b.tex),a.texSubImage2D(a.TEXTURE_2D,0,0,0,b.w,b.h,a.RGBA,a.FLOAT,0),a.bindBuffer(a.PIXEL_UNPACK_BUFFER,null),b.copiedFrom=l,b.copiedVersion=l.version,b.version++}function u(i){for(const l of i){const b=y[l[0]];y[l[0]]=y[l[1]],y[l[1]]=b}}function D(i){for(const l of me.resources){if(!l.feed)continue;const b=y[l.name],c=new Float32Array(b.w*b.h*4);c.set(i[l.feed].subarray(0,c.length)),a.activeTexture(a.TEXTURE0+ce),a.bindTexture(a.TEXTURE_2D,b.tex),a.texSubImage2D(a.TEXTURE_2D,0,0,0,b.w,b.h,a.RGBA,a.FLOAT,c)}}function R(i,l){const b=i.pass,c=i.p,V=b.target,Q=i.outputs.map(function(M){return{res:y[M.binding.resource],level:M.binding.level||0,location:M.location}}),ae=i.inputs.map(function(M){return y[M.resource]});i.inputs.forEach(function(M,ke){for(const e of Q)if(e.res===ae[ke]&&e.level===(M.level||0))throw new Error("elio-graph: pass "+b.name+" reads "+e.res.name+", which it writes")}),a.useProgram(c.handle),a.uniform1ui(c.width,V.elioWidth),ae.forEach(function(M,ke){a.activeTexture(a.TEXTURE0+ce+ke),a.bindTexture(a.TEXTURE_2D,M.tex),c.lens[ke]&&a.uniform1ui(c.lens[ke],M.w*M.h),c.widths[ke]&&a.uniform1ui(c.widths[ke],M.w)});for(const M of i.blocks)(!M.constant||!M.written)&&(a.bindBuffer(a.UNIFORM_BUFFER,M.buffer),a.bufferSubData(a.UNIFORM_BUFFER,0,new Uint8Array(g.pack(M.layout,M.values,l,b.name))),M.written=!0),a.bindBufferRange(a.UNIFORM_BUFFER,M.index,M.buffer,0,M.size);for(const M of Q)M.res.version++;a.viewport(0,0,V.width,V.height);for(const M of A(Q))a.bindFramebuffer(a.FRAMEBUFFER,M.fbo),a.drawArrays(a.TRIANGLES,0,3)}const te=g.host.profile,ee=function(i,l){i.pass.type==="copy"?r(i.pass):i.pass.type==="mipmap"?(a.activeTexture(a.TEXTURE0+ce),a.bindTexture(a.TEXTURE_2D,y[i.pass.resource].tex),a.generateMipmap(a.TEXTURE_2D)):R(i,l)},ue=[];for(const i of g.host.throttle||[]){const l=new RegExp(i.match),b=f.filter(function(V){return l.test(V.pass.name)&&!V.group});if(!b.length||!(i.every>1))continue;const c={every:Math.floor(i.every),hold:i.hold?new RegExp(i.hold):null,since:1/0,dt:0,snap:null,runs:!1,derived:null,plans:b};for(const V of b)V.group=c;ue.push(c)}const ye=function(i){let l=null,b=!1;for(const c of f){const V=(c.outputs||[]).map(function(Q){return Q.binding.resource}).concat(c.pass.resource?[c.pass.resource]:[]);V.indexOf(i[0])<0&&V.indexOf(i[1])<0||(!c.group||l&&l!==c.group?b=!0:l=c.group)}return b?null:l},ze=(me.frameSwaps||[]).map(function(i){return{pair:i,group:ue.length?ye(i):null}}),Ze=function(i){return i&&i!=="always"?i.replace(/^(param|changed):/,"").split(",").map(function(l){return l.trim()}):[]},Me=function(i,l,b){for(const c of ue){c.dt+=Number(i&&i.dt)||0,c.since++;const V=c.plans.some(function(Q){return Ze(Q.pass.when).some(function(ae){return l[ae]})});c.runs=V||c.since>=c.every,c.runs&&(c.derived=b.derive(c.dt,c.snap))}},Le=function(i){for(const l of ue)l.runs&&(l.since=0,l.dt=0,l.snap=i.snapshot())};return{isReady:function(){return!F.disposed},run:function(i,l,b,c){if(a.isContextLost&&a.isContextLost())return!1;const V=S(a);try{a.disable(a.BLEND),a.disable(a.DEPTH_TEST),a.disable(a.CULL_FACE),a.disable(a.SCISSOR_TEST),a.colorMask(!0,!0,!0,!0),a.bindVertexArray(B),D(i),ue.length&&Me(l,b,c);for(const ae of f){if(ae.group&&!ae.group.runs||!g.holds(ae.pass.when,i,b))continue;const M=ae.group?ae.group.derived:i;te?te(ae.pass.name,function(){ee(ae,M)}):ee(ae,M)}for(const ae of ze)(!ae.group||ae.group.runs)&&u([ae.pair]);ue.length&&Le(c)}finally{L(a,V)}const Q=g.host.textureCache;return Q&&(Q._sceneTextureEpoch=(Q._sceneTextureEpoch||0)+1),!0},export:function(i){const l=y[me.exports[i]];return l?{tex:l.tex,width:l.w,height:l.h,levels:l.levels}:null},held:function(i){for(const l of ue)if(l.hold&&l.hold.test(i)&&l.snap&&i in l.snap)return l.snap[i]},split:function(){return F.split},dispose:$}}window.__gosx_elio_graph.executors.webgl2=C,window.__gosx_elio_graph.glState={save:S,restore:L}})(),(function(){const re={f32:4,"vec2<f32>":8,"vec4<f32>":16};function S(L){const C=L.host.device,g=L.graph,a=L.module.kernels,me=L.host;if(!C)throw Object.assign(new Error("no device"),{fallback:"no-device"});const ie="gosx-elio-"+L.label+"-",le=[],ce=[],t=Object.create(null),m=Object.create(null),h=Object.create(null),H=new Map,pe=new Map,he={disposed:!1};let s=0;const x={};for(const f of Object.keys(g.exports||{}))x[g.exports[f]]=!0;const B=C.queue,F=GPUBufferUsage.STORAGE|GPUBufferUsage.COPY_DST|GPUBufferUsage.COPY_SRC,_=GPUBufferUsage.UNIFORM|GPUBufferUsage.COPY_DST;for(const f of g.resources)if(f.kind==="buffer"){const p=Math.max(16,Math.ceil(f.count*(re[f.elementType]||16)/4)*4),r=C.createBuffer({label:ie+f.name,size:p,usage:F});le.push(r),t[f.name]={name:f.name,id:++s,buffer:r}}else{const p=C.createTexture({label:ie+f.name,size:[f.width,f.height,1],format:f.format,mipLevelCount:f.mipLevels||1,usage:GPUTextureUsage.STORAGE_BINDING|GPUTextureUsage.TEXTURE_BINDING});ce.push(p),t[f.name]={name:f.name,id:++s,texture:p,w:f.width,h:f.height,levels:f.mipLevels||1,format:f.format}}function y(f){return f.map(function(p){const r={binding:p.binding,visibility:GPUShaderStage.COMPUTE};return p.kind==="uniform"?r.buffer={type:"uniform"}:p.kind==="storage-read"?r.buffer={type:"read-only-storage"}:p.kind==="storage-rw"?r.buffer={type:"storage"}:p.kind==="sampled-texture"||p.kind==="texture"?r.texture={sampleType:"unfilterable-float",viewDimension:"2d"}:r.storageTexture={access:"write-only",format:p.format,viewDimension:"2d"},r})}for(const f of L.steps){const p=f.pass;if(!p)continue;if(p.type!=="kernel")throw new Error("elio-graph: pass "+p.name+": "+p.type+" passes exist only on webgl2");const r=p.kernel+"/"+p.entry;if(m[r])continue;const u=a[L.kernelKey(p.kernel)],D=h[p.kernel]||(h[p.kernel]=C.createBindGroupLayout({label:ie+p.kernel,entries:y(u.bindings)})),R=C.createShaderModule({label:ie+p.kernel,code:u.wgsl}),te=me.create(C,"compute",{label:ie+p.kernel,layout:C.createPipelineLayout({bindGroupLayouts:[D]}),compute:{module:R,entryPoint:p.entry}});m[r]={handle:te,layout:D,kernel:u}}function k(f){const p=me.validation&&me.validation.get(f.handle);return!p||p.ready===!0?p&&p.pipeline||f.handle:null}let P=0;const T=L.steps.map(function(f){const p=f.pass,r=m[p.kernel+"/"+p.entry],u=(p.uniforms||[]).map(function(D){const R=r.kernel.uniforms.find(function(ue){return ue.name===D.name});if(!R)throw new Error("elio-graph: pass "+p.name+" has no uniform block "+D.name);const te=Math.max(16,Math.ceil(R.size/16)*16),ee=C.createBuffer({label:ie+p.name+"-"+D.name,size:te,usage:_});return le.push(ee),{name:D.name,layout:R,values:D.values,buffer:ee,constant:L.isStatic(D.values),written:!1}});return{pass:p,p:r,blocks:u,id:++P}});function ne(f){const p=f.pass,r=[],u=[p.name,f.id];for(const te of f.p.kernel.bindings){let ee;if(te.kind==="uniform")ee={buffer:f.blocks.find(function(ue){return ue.name===te.name}).buffer};else{const ue=p.bindings.find(function(ze){return ze.name===te.name});if(!ue)throw new Error("elio-graph: pass "+p.name+" does not bind "+te.name);const ye=t[ue.resource];u.push(ye.id+"@"+(ue.level||0)),te.kind==="storage-read"||te.kind==="storage-rw"?ee={buffer:ye.buffer}:ee=ye.texture.createView({baseMipLevel:ue.level||0,mipLevelCount:1})}r.push({binding:te.binding,resource:ee})}const D=u.join("|");let R=H.get(D);return R||(R=C.createBindGroup({label:ie+p.name,layout:f.p.layout,entries:r}),H.set(D,R)),R}function be(f){for(const p of f){const r=t[p[0]];t[p[0]]=t[p[1]],t[p[1]]=r}}const xe=3,X=120;let z=null;if(me.timing!==!1&&C.features&&C.features.has&&C.features.has("timestamp-query"))try{const f=[],p=C.createQuerySet({label:ie+"time",type:"timestamp",count:2}),r=C.createBuffer({label:ie+"time-resolve",size:16,usage:GPUBufferUsage.QUERY_RESOLVE|GPUBufferUsage.COPY_SRC});le.push(r);for(let u=0;u<xe;u++){const D=C.createBuffer({label:ie+"time-"+u,size:16,usage:GPUBufferUsage.MAP_READ|GPUBufferUsage.COPY_DST});le.push(D),f.push({buffer:D,state:"free"})}z={querySet:p,resolve:r,slots:f,samples:[],ms:null}}catch{z=null}function v(){if(z)for(const f of z.slots)f.state==="copied"&&(f.state="mapping",f.buffer.mapAsync(1).then(function(){if(he.disposed||f.state!=="mapping")return;const p=new BigUint64Array(f.buffer.getMappedRange().slice(0,16));f.buffer.unmap(),f.state="free",p[1]>p[0]&&(z.ms=Number(p[1]-p[0])/1e6,z.samples.push(z.ms),z.samples.length>X&&z.samples.shift())},function(){f.state="free"}))}function Z(f){const p=z.slots.find(function(r){return r.state==="free"});f.resolveQuerySet(z.querySet,0,2,z.resolve,0),p&&(f.copyBufferToBuffer(z.resolve,0,p.buffer,0,16),p.state="copied")}const A=C.lost&&C.lost.then?C.lost:null;A&&A.then(function(){$()},function(){$()});function $(){if(!he.disposed){he.disposed=!0;for(const f of le)f.destroy();for(const f of ce)f.destroy();le.length=ce.length=0,H.clear(),pe.clear(),z&&z.querySet.destroy&&z.querySet.destroy()}}return{isReady:function(){if(he.disposed)return!1;for(const f of Object.keys(m))if(!k(m[f]))return!1;return!0},run:function(f,p,r){const u=p.encoder;if(!u)throw new Error("elio-graph: a webgpu frame needs frame.encoder");for(const R of g.resources)R.feed&&B.writeBuffer(t[R.name].buffer,0,f[R.feed]);v();const D=T.filter(function(R){return L.holds(R.pass.when,f,r)});return D.forEach(function(R,te){const ee=R.pass;for(const ze of R.blocks)ze.constant&&ze.written||(B.writeBuffer(ze.buffer,0,new Uint8Array(L.pack(ze.layout,ze.values,f,ee.name))),ze.written=!0);const ue={label:ie+ee.name};z&&(te===0||te===D.length-1)&&(ue.timestampWrites={querySet:z.querySet},te===0&&(ue.timestampWrites.beginningOfPassWriteIndex=0),te===D.length-1&&(ue.timestampWrites.endOfPassWriteIndex=1));const ye=u.beginComputePass(ue);ye.setPipeline(k(R.p)),ye.setBindGroup(0,ne(R)),ye.dispatchWorkgroups(ee.dispatch[0],ee.dispatch[1]||1,ee.dispatch[2]||1),ye.end()}),z&&D.length&&Z(u),be(g.frameSwaps||[]),!0},export:function(f){const p=t[g.exports[f]];if(p&&p.buffer)return{buffer:p.buffer};if(!p)return null;let r=pe.get(p.texture);return r||(r=p.texture.createView(),pe.set(p.texture,r)),{texture:p.texture,view:r,width:p.w,height:p.h,levels:p.levels}},gpuTiming:function(){return z?{ms:z.ms,samples:z.samples.slice()}:null},dispose:$}}window.__gosx_elio_graph.executors.webgpu=S})(),(function(){const re={uniform:0,sampler:1,disp:2,slope:6,foam:10,wake:11},S={edgeCells:8,k:.2,slope:3,slickLo:.04,slickHi:.4,slickFine:.85,slickMid:.4,glow:.3,thinBase:.12,thinLace:.3,bodyBase:.32,bodyLace:.3,fadeNear:120,fadeFar:700};function L(t,m,h,H){const pe=t.indexOf(m);if(pe<0)throw new Error("ocean-fft: anchor not found: "+m.slice(0,48));const he=t.indexOf(h,pe);if(he<0)throw new Error("ocean-fft: anchor not found: "+h.slice(0,48));return t.slice(0,pe)+H+t.slice(he)}function C(t,m,h){if(t.indexOf(m)<0)throw new Error("ocean-fft: anchor not found: "+m.slice(0,48));return t.replace(m,()=>h)}function g(t){let m="const WAKE_EDGE_CELLS: f32 = "+S.edgeCells.toFixed(1)+`;
`;m+=`struct OceanSpec { c: array<vec4f, 4>, f: vec4f, m: vec4f, w: array<vec4f, 4>, k: array<vec4f, 2> };
`,m+="@group("+t+") @binding("+re.uniform+`) var<uniform> sp: OceanSpec;
`,m+="@group("+t+") @binding("+re.sampler+`) var spSampler: sampler;
`;for(let h=0;h<4;h++)m+="@group("+t+") @binding("+(re.disp+h)+") var spDisp"+h+`: texture_2d<f32>;
`;for(let h=0;h<4;h++)m+="@group("+t+") @binding("+(re.slope+h)+") var spSlope"+h+`: texture_2d<f32>;
`;return m+="@group("+t+") @binding("+re.foam+`) var spFoam: texture_2d<f32>;
`,m+="@group("+t+") @binding("+re.wake+`) var spWakeTex: texture_2d<f32>;
`,m+=`fn spWake(rest: vec2f, cam: vec2f) -> vec3f {
`,m+=`  let r = (rest - cam - sp.k[0].xy) / max(sp.k[1].x, 1e-3);
`,m+=`  let edge = min(min(r.x, sp.k[1].z - r.x), min(r.y, sp.k[1].z - r.y));
`,m+=`  return vec3f((r + sp.k[0].zw) / sp.k[1].z, smoothstep(0.0, WAKE_EDGE_CELLS, edge) * sp.k[1].y);
`,m+=`}
`,m+=`fn spLod(c: vec4f, spacing: f32) -> f32 { return max(log2(max(spacing, 1e-3) * c.z / c.x), 0.0); }
`,m}function a(){let t=`  var p = vec3f(xz.x, level, xz.y); var dx = vec3f(1.0, 0.0, 0.0); var dz = vec3f(0.0, 0.0, 1.0);
`;t+=`  var amp = sp.m.x;
`,t+=`  let spacing = max(ocean.p[8].z * ocean.p[8].w * exp(ocean.p[8].w * f32(ring)), r * 6.2831853 / f32(segs));
`;for(let m=0;m<4;m++)t+="  if (sp.c["+m+`].w > 0.5) {
`,t+="    let uv = (xz - sp.w["+m+"].xy) / sp.c["+m+"].x; let lod = spLod(sp.c["+m+"], spacing); let k = smoothstep(0.0, 1.2, depth0 * sp.c["+m+`].y);
`,t+="    let a = textureSampleLevel(spDisp"+m+", spSampler, uv, lod); let b = textureSampleLevel(spSlope"+m+`, spSampler, uv, lod);
`,t+=`    p += a.xyz * k; dx += vec3f(b.z, b.x, a.w) * k; dz += vec3f(a.w, b.y, b.w) * k;
`,t+=`  }
`;return t+=`  let swv = spWake(xz, ocean.p[7].xz);
`,t+=`  if (sp.k[1].w > 0.0) { p = vec3f(xz.x, level, xz.y); dx = vec3f(1.0, 0.0, 0.0); dz = vec3f(0.0, 0.0, 1.0); }
`,t+=`  if (swv.z > 0.0 && sp.k[1].w <= 0.0) {
`,t+=`    let swk = textureSampleLevel(spWakeTex, spSampler, swv.xy, max(log2(max(spacing, 1e-3) / max(sp.k[1].x, 1e-3)), 0.0));
`,t+="    let sws = swv.z * smoothstep(0.0, 1.2, depth0 * "+S.k.toFixed(2)+`);
`,t+=`    p.y += swk.x * sws; dx.y += swk.y * sws; dz.y += swk.z * sws;
`,t+=`  }
`,t}function me(){let t=`  var sGx = 0.0; var sGz = 0.0; var sXX = 0.0; var sZZ = 0.0; var sXZ = 0.0;
`;t+=`  let wxz = in.rest - sp.m.yz;
`,t+=`  let sFw = fwidth(wxz);
`,t+=`  var sOp = 0.0; var sAlb = 1.0;
`,t+=`  let swv = spWake(in.rest, ocean.p[7].xz);
`,t+=`  let swf = textureSample(spWakeTex, spSampler, swv.xy);
`,t+="  let sSlick = swv.z * smoothstep("+S.slickLo.toFixed(2)+", "+S.slickHi.toFixed(2)+`, swf.w);
`;for(let m=0;m<4;m++){const h=m>=2?S.slickFine:m===1?S.slickMid:0;t+="  if (sp.c["+m+`].w > 0.5) {
`,t+="    let uv = (in.rest - sp.w["+m+"].xy) / sp.c["+m+"].x; let k = smoothstep(0.0, 1.2, in.depth0 * sp.c["+m+"].y)"+(h?" * (1.0 - "+h.toFixed(2)+" * sSlick)":"")+`;
`,t+="    let a = textureSample(spDisp"+m+", spSampler, uv); let b = textureSample(spSlope"+m+`, spSampler, uv);
`,t+=`    sGx += b.x * k; sGz += b.y * k; sXX += b.z * k; sZZ += b.w * k; sXZ += a.w * k;
`,t+=`  }
`}return t+="  let swm = swv.z * smoothstep(0.0, 1.2, in.depth0 * "+S.k.toFixed(2)+`);
`,t+="  sGx += swf.y * swm * "+S.slope.toFixed(2)+"; sGz += swf.z * swm * "+S.slope.toFixed(2)+`;
`,t+=`  let sJ = (1.0 + sXX) * (1.0 + sZZ) - sXZ * sXZ;
`,t+=`  let sFoamTex = textureSampleLevel(spFoam, spSampler, wxz * sp.f.x, 0.0);
`,t+=`  let sPersist = sFoamTex.r;
`,t+=`  let N = normalize(vec3f(sGz * sXZ - (1.0 + sZZ) * sGx, max(sJ, 0.1), sXZ * sGx - sGz * (1.0 + sXX)));
`,t}const ie=`    let sNear = 1.0 - smoothstep(0.7 * sp.m.w, sp.m.w, length(in.world.xz - ocean.p[7].xz));
    let sJf = (1.0 + sp.f.y * sXX) * (1.0 + sp.f.y * sZZ) - sp.f.y * sp.f.y * sXZ * sXZ;
    let sStateless = clamp((sp.f.z - sJf) * sp.f.w, 0.0, 1.0) * lace * bu;
    let sKept = smoothstep(0.04, 0.6, sPersist) * (0.55 + 0.45 * lace);
    foam = mix(sStateless, sKept, sNear);
    let sDen = mix(smoothstep(0.1, 0.9, swf.w) * swv.z, sFoamTex.b, sNear);
    var sStr = mix(0.4, spLace(wxz, t, sFw), 1.0 - smoothstep(`+S.fadeNear.toFixed(1)+", "+S.fadeFar.toFixed(1)+`, dist));
    sStr *= mix(0.55, 1.0, smoothstep(0.1, 0.6, sDen));
    let sThin = smoothstep(0.02, 0.5, sDen); let sBody = smoothstep(0.5, 1.0, sDen);
    sOp = sThin * mix(`+S.thinBase.toFixed(2)+" + "+S.thinLace.toFixed(2)+" * sStr, "+S.bodyBase.toFixed(2)+" + "+S.bodyLace.toFixed(2)+` * sStr, sBody);
    sOp *= (1.0 - smoothstep(1500.0, 6000.0, dist)) * min(ocean.p[3].w, 1.0);
    sAlb = mix(0.8, 1.0, sStr);
`;function le(){let t="";return t+=`fn spLine(p: vec2f, sc: f32, w: f32, pw: f32, mean: f32, fw: vec2f) -> f32 {
`,t+=`  var q = p * sc; q += 0.8 * (vec2f(on(q * 0.8 + vec2f(3.1)), on(q * 0.8 + vec2f(9.7))) - vec2f(0.5));
`,t+=`  let i = floor(q); let f = fract(q); var d1 = 8.0; var d2 = 8.0;
`,t+=`  for (var y = -1; y <= 1; y++) { for (var x = -1; x <= 1; x++) {
`,t+=`    let g = vec2f(f32(x), f32(y)); let c = i + g; let r = g + vec2f(oh(c), oh(c + vec2f(37.7, 11.3))) - f;
`,t+=`    let d = dot(r, r); if (d < d1) { d2 = d1; d1 = d; } else if (d < d2) { d2 = d; }
`,t+=`  } }
`,t+=`  let line = pow(1.0 - smoothstep(0.0, w, sqrt(d2) - sqrt(d1)), pw);
`,t+=`  return mix(mean, line, 1.0 - smoothstep(0.35, 0.9, length(fw) * sc));
`,t+=`}
`,t+=`fn spStrand(q: vec2f, o: vec2f, fw: vec2f) -> f32 {
`,t+=`  let a = spLine(q + o, 3.3, 0.65, 2.2, 0.22, fw); let b = spLine(q + o * 1.7, 1.1, 0.12, 1.0, 0.15, fw); let c = spLine(q + o * 0.6, 0.4, 0.09, 1.0, 0.11, fw);
`,t+=`  return 1.0 - (1.0 - 0.5 * a) * (1.0 - 0.6 * b) * (1.0 - 0.4 * c);
`,t+=`}
`,t+=`fn spLace(p: vec2f, t: f32, fw: vec2f) -> f32 {
`,t+=`  let q = p + 0.7 * (vec2f(on(p * 0.55 + vec2f(t * 0.13, 3.1)), on(p * 0.55 + vec2f(8.7, -t * 0.11))) - vec2f(0.5));
`,t+=`  let k = floor(t / 4.0);
`,t+=`  return mix(spStrand(q, vec2f(17.3, 5.1) * k, fw), spStrand(q, vec2f(17.3, 5.1) * (k + 1.0), fw), smoothstep(0.0, 1.0, fract(t / 4.0)));
`,t+=`}
`,t}function ce(t,m){let h=t;return h=C(h,"@location(4) depth0: f32 };","@location(4) depth0: f32, @location(5) rest: vec2f };"),h=L(h,"  var p = vec3f(xz.x, level, xz.y);","  let ph = fract(t * ocean.p[0].w / 9.0",a()),h=C(h,"out.depth0 = depth0;","out.depth0 = depth0; out.rest = xz;"),h=C(h,`  let N = od(normalize(in.normal), in.world.xz, t, ocean.p[9].xy);
`,me()),h=C(h,"  if (dist < 250.0) {","  if (dist < 6000.0) {"),h=C(h,`    foam = (1.0 - smoothstep(0.12, 0.35, in.jacobian)) * lace * bu;
`,ie),h=C(h,"(1.0 - smoothstep(80.0, 250.0, dist))","(1.0 - smoothstep(1500.0, 6000.0, dist))"),h=C(h,"  return vec4f(color, alpha);",`  if (sp.k[1].w > 0.0) { let hh = clamp(swf.x / sp.k[1].w, -1.0, 1.0); color = select(vec3f(0.25), select(mix(vec3f(1.0), vec3f(1.0, 0.15, 0.1), hh), mix(vec3f(1.0), vec3f(0.1, 0.3, 1.0), -hh), hh < 0.0), swv.z > 0.0); alpha = 1.0; }
  return vec4f(color, alpha);`),h=C(h,"  color = mix(color, fl, foam); alpha = mix(alpha, 1.0, foam);",`  let sAer = swv.z * smoothstep(0.45, 1.0, swf.w);
  let sTurq = vec3f(0.05, 0.42, 0.44) * (ambient * 0.9 + sunCol * 0.05);
  color = mix(color, sTurq, sAer * `+S.glow.toFixed(2)+` * (1.0 - foam)); alpha = mix(alpha, 1.0, sAer * 0.5);
  color = mix(color, fl, foam); alpha = mix(alpha, 1.0, foam);
  let sSun = smoothstep(0.0, 0.55, dot(N, L));
  let sWfl = fl * mix(vec3f(0.66, 0.76, 0.9), vec3f(1.0), sSun) * sAlb + ocean.p[4].xyz * sunCol * 0.2 * max(dot(N, L), 0.0) * sAlb;
  color = mix(color, sWfl, sOp) + spec * sOp * 0.25; alpha = mix(alpha, 1.0, sOp);`),h=C(h,"(ambient * 0.55 + sunCol * 0.04 * max(dot(N, L), 0.0))","(ambient * 1.4 + sunCol * 0.2 * max(dot(N, L), 0.0) + vec3f(0.05))"),h+`
`+g(m)+`
`+le()}window.__gosx_scene3d_ocean_fft_surface={wgsl:ce,SLOT:re}})(),(function(){const re={disp:4,slope:8,foam:12,wake:13},S={edgeCells:8,k:.2,slope:3,slickLo:.04,slickHi:.4,slickFine:.85,slickMid:.4,glow:.3,thinBase:.12,thinLace:.3,bodyBase:.32,bodyLace:.3,fadeNear:120,fadeFar:700};function L(t,m,h){if(t.indexOf(m)<0)throw new Error("ocean-fft: anchor not found: "+m.slice(0,48));return t.replace(m,()=>h)}function C(t,m,h,H){const pe=t.indexOf(m);if(pe<0)throw new Error("ocean-fft: anchor not found: "+m.slice(0,48));const he=t.indexOf(h,pe);if(he<0)throw new Error("ocean-fft: anchor not found: "+h.slice(0,48));return t.slice(0,pe)+H+t.slice(he)}function g(){let t=`uniform vec4 u_sp[12];
`;for(let m=0;m<4;m++)t+="uniform sampler2D u_spDisp"+m+`;
`;for(let m=0;m<4;m++)t+="uniform sampler2D u_spSlope"+m+`;
`;return t+=`uniform sampler2D u_spFoam;
`,t+=`uniform sampler2D u_spWake;
`,t+=`vec3 spWake(vec2 rest, vec2 cam) {
`,t+=`  vec2 r = (rest - cam - u_sp[10].xy) / max(u_sp[11].x, 1e-3);
`,t+=`  float edge = min(min(r.x, u_sp[11].z - r.x), min(r.y, u_sp[11].z - r.y));
`,t+="  return vec3((r + u_sp[10].zw) / u_sp[11].z, smoothstep(0., "+S.edgeCells.toFixed(1)+`, edge) * u_sp[11].y);
`,t+=`}
`,t+=`float spLod(vec4 c, float spacing) { return max(log2(max(spacing, 1e-3) * c.z / c.x), 0.); }
`,t}function a(){let t=`  vec3 p = vec3(xz.x, level, xz.y), dx = vec3(1., 0., 0.), dz = vec3(0., 0., 1.);
`;t+=`  float amp = u_sp[5].x;
`,t+=`  float spacing = max(u_ocean[8].z * u_ocean[8].w * exp(u_ocean[8].w * float(ring)), r * 6.2831853 / float(segs));
`;for(let m=0;m<4;m++)t+="  if (u_sp["+m+`].w > 0.5) {
`,t+="    vec2 suv = (xz - u_sp["+(6+m)+"].xy) / u_sp["+m+"].x; float slod = spLod(u_sp["+m+"], spacing); float sk = smoothstep(0., 1.2, depth0 * u_sp["+m+`].y);
`,t+="    vec4 sa = textureLod(u_spDisp"+m+", suv, slod), sb = textureLod(u_spSlope"+m+`, suv, slod);
`,t+=`    p += sa.xyz * sk; dx += vec3(sb.z, sb.x, sa.w) * sk; dz += vec3(sa.w, sb.y, sb.w) * sk;
`,t+=`  }
`;return t+=`  vec3 swv = spWake(xz, u_ocean[7].xz);
`,t+=`  if (u_sp[11].w > 0.) { p = vec3(xz.x, level, xz.y); dx = vec3(1., 0., 0.); dz = vec3(0., 0., 1.); }
`,t+=`  if (swv.z > 0. && u_sp[11].w <= 0.) {
`,t+=`    vec4 swk = textureLod(u_spWake, swv.xy, max(log2(max(spacing, 1e-3) / max(u_sp[11].x, 1e-3)), 0.));
`,t+="    float sws = swv.z * smoothstep(0., 1.2, depth0 * "+S.k.toFixed(2)+`);
`,t+=`    p.y += swk.x * sws; dx.y += swk.y * sws; dz.y += swk.z * sws;
`,t+=`  }
`,t}function me(){let t=`  float sGx = 0., sGz = 0., sXX = 0., sZZ = 0., sXZ = 0.;
`;t+=`  vec2 wxz = v_rest - u_sp[5].yz;
`,t+=`  vec2 sFw = fwidth(wxz);
`,t+=`  float sOp = 0., sAlb = 1.;
`,t+=`  vec3 swv = spWake(v_rest, u_ocean[7].xz);
`,t+=`  vec4 swf = texture(u_spWake, swv.xy);
`,t+="  float sSlick = swv.z * smoothstep("+S.slickLo.toFixed(2)+", "+S.slickHi.toFixed(2)+`, swf.w);
`;for(let m=0;m<4;m++){const h=m>=2?S.slickFine:m===1?S.slickMid:0;t+="  if (u_sp["+m+`].w > 0.5) {
`,t+="    vec2 suv = (v_rest - u_sp["+(6+m)+"].xy) / u_sp["+m+"].x; float sk = smoothstep(0., 1.2, v_d0 * u_sp["+m+"].y)"+(h?" * (1. - "+h.toFixed(2)+" * sSlick)":"")+`;
`,t+="    vec4 sa = texture(u_spDisp"+m+", suv), sb = texture(u_spSlope"+m+`, suv);
`,t+=`    sGx += sb.x * sk; sGz += sb.y * sk; sXX += sb.z * sk; sZZ += sb.w * sk; sXZ += sa.w * sk;
`,t+=`  }
`}return t+="  float swm = swv.z * smoothstep(0., 1.2, v_d0 * "+S.k.toFixed(2)+`);
`,t+="  sGx += swf.y * swm * "+S.slope.toFixed(2)+"; sGz += swf.z * swm * "+S.slope.toFixed(2)+`;
`,t+=`  float sJ = (1. + sXX) * (1. + sZZ) - sXZ * sXZ;
`,t+=`  vec4 sFoamTex = textureLod(u_spFoam, wxz * u_sp[4].x, 0.);
`,t+=`  float sPersist = sFoamTex.r;
`,t+=`  vec3 N = normalize(vec3(sGz * sXZ - (1. + sZZ) * sGx, max(sJ, 0.1), sXZ * sGx - sGz * (1. + sXX)));
`,t}const ie=`    float sNear = 1. - smoothstep(0.7 * u_sp[5].w, u_sp[5].w, length(v_w.xz - u_ocean[7].xz));
    float sJf = (1. + u_sp[4].y * sXX) * (1. + u_sp[4].y * sZZ) - u_sp[4].y * u_sp[4].y * sXZ * sXZ;
    float sStateless = clamp((u_sp[4].z - sJf) * u_sp[4].w, 0., 1.) * lace * bu;
    float sKept = smoothstep(0.04, 0.6, sPersist) * (0.55 + 0.45 * lace);
    foam = mix(sStateless, sKept, sNear);
    float sDen = mix(smoothstep(0.1, 0.9, swf.w) * swv.z, sFoamTex.b, sNear);
    float sStr = mix(0.4, spLace(wxz, t, sFw), 1. - smoothstep(`+S.fadeNear.toFixed(1)+", "+S.fadeFar.toFixed(1)+`, dist));
    sStr *= mix(0.55, 1., smoothstep(0.1, 0.6, sDen));
    float sThin = smoothstep(0.02, 0.5, sDen), sBody = smoothstep(0.5, 1., sDen);
    sOp = sThin * mix(`+S.thinBase.toFixed(2)+" + "+S.thinLace.toFixed(2)+" * sStr, "+S.bodyBase.toFixed(2)+" + "+S.bodyLace.toFixed(2)+` * sStr, sBody);
    sOp *= (1. - smoothstep(1500., 6000., dist)) * min(u_ocean[3].w, 1.);
    sAlb = mix(0.8, 1., sStr);
`;function le(){let t="";return t+=`float spLine(vec2 p, float sc, float w, float pw, float mean, vec2 fw) {
`,t+=`  vec2 q = p * sc; q += 0.8 * (vec2(on(q * 0.8 + 3.1), on(q * 0.8 + 9.7)) - 0.5);
`,t+=`  vec2 i = floor(q), f = fract(q); float d1 = 8., d2 = 8.;
`,t+=`  for (int y = -1; y <= 1; y++) for (int x = -1; x <= 1; x++) {
`,t+=`    vec2 g = vec2(float(x), float(y)), c = i + g; vec2 r = g + vec2(oh(c), oh(c + vec2(37.7, 11.3))) - f;
`,t+=`    float d = dot(r, r); if (d < d1) { d2 = d1; d1 = d; } else if (d < d2) { d2 = d; }
`,t+=`  }
`,t+=`  float line = pow(1. - smoothstep(0., w, sqrt(d2) - sqrt(d1)), pw);
`,t+=`  return mix(mean, line, 1. - smoothstep(0.35, 0.9, length(fw) * sc));
`,t+=`}
`,t+=`float spStrand(vec2 q, vec2 o, vec2 fw) {
`,t+=`  float a = spLine(q + o, 3.3, 0.65, 2.2, 0.22, fw), b = spLine(q + o * 1.7, 1.1, 0.12, 1., 0.15, fw), c = spLine(q + o * 0.6, 0.4, 0.09, 1., 0.11, fw);
`,t+=`  return 1. - (1. - 0.5 * a) * (1. - 0.6 * b) * (1. - 0.4 * c);
`,t+=`}
`,t+=`float spLace(vec2 p, float t, vec2 fw) {
`,t+=`  vec2 q = p + 0.7 * (vec2(on(p * 0.55 + vec2(t * 0.13, 3.1)), on(p * 0.55 + vec2(8.7, -t * 0.11))) - 0.5);
`,t+=`  float k = floor(t / 4.);
`,t+=`  return mix(spStrand(q, vec2(17.3, 5.1) * k, fw), spStrand(q, vec2(17.3, 5.1) * (k + 1.), fw), smoothstep(0., 1., fract(t / 4.)));
`,t+=`}
`,t}function ce(t,m){let h=t;h=L(h,"out float v_d0;","out float v_d0; out vec2 v_rest;"),h=L(h,"void main() {",g()+"void main() {"),h=C(h,"  vec3 p = vec3(xz.x, level, xz.y), dx","  float ph = fract(t * u_ocean[0].w / 9.",a()),h=L(h,"v_d0 = depth0;","v_d0 = depth0; v_rest = xz;");let H=m;return H=L(H,"in float v_d0;","in float v_d0; in vec2 v_rest;"),H=L(H,"void main() {",g()+le()+"void main() {"),H=L(H,`  vec3 N = od(normalize(v_n), v_w.xz, t, u_ocean[9].xy);
`,me()),H=L(H,"  if (dist < 250.) {","  if (dist < 6000.) {"),H=L(H,`    foam = (1. - smoothstep(0.12, 0.35, v_j)) * lace * bu;
`,ie),H=L(H,"(1. - smoothstep(80., 250., dist))","(1. - smoothstep(1500., 6000., dist))"),H=L(H,"  fragColor = vec4(color, alpha);",`  if (u_sp[11].w > 0.) { float hh = clamp(swf.x / u_sp[11].w, -1., 1.); color = swv.z > 0. ? (hh < 0. ? mix(vec3(1.), vec3(.1, .3, 1.), -hh) : mix(vec3(1.), vec3(1., .15, .1), hh)) : vec3(.25); alpha = 1.; }
  fragColor = vec4(color, alpha);`),H=L(H,"  color = mix(color, fl, foam); alpha = mix(alpha, 1., foam);",`  float sAer = swv.z * smoothstep(0.45, 1.0, swf.w);
  vec3 sTurq = vec3(0.05, 0.42, 0.44) * (ambient * 0.9 + sunCol * 0.05);
  color = mix(color, sTurq, sAer * `+S.glow.toFixed(2)+` * (1. - foam)); alpha = mix(alpha, 1., sAer * 0.5);
  color = mix(color, fl, foam); alpha = mix(alpha, 1., foam);
  float sSun = smoothstep(0., 0.55, dot(N, L));
  vec3 sWfl = fl * mix(vec3(0.66, 0.76, 0.9), vec3(1.), sSun) * sAlb + u_ocean[4].xyz * sunCol * 0.2 * max(dot(N, L), 0.) * sAlb;
  color = mix(color, sWfl, sOp) + spec * sOp * 0.25; alpha = mix(alpha, 1., sOp);`),H=L(H,"(ambient * 0.55 + sunCol * 0.04 * max(dot(N, L), 0.))","(ambient * 1.4 + sunCol * 0.2 * max(dot(N, L), 0.) + vec3(0.05))"),{vertex:h,fragment:H}}window.__gosx_scene3d_ocean_fft_surface_gl={glsl:ce,UNIT:re}})(),(function(){const re=window.__gosx_elio_graph,S=window.__gosx_scene3d_elio_ocean,L=window.__gosx_scene3d_ocean_fft_surface,C=window.__gosx_scene3d_ocean_fft_surface_gl,g=.25,a=3.5,me=240,ie=.3,le=function(e){return Math.max(.3,.14*e*e/9.81)},ce=48,t=4;let m=null,h=null;function H(e){return e==="low"||e==="medium"?e:"high"}function pe(e){const n=[];let o={cell:.5,width:1024};for(const w of re.expand(e)){const d=w.uniforms&&w.uniforms[0]&&w.uniforms[0].values;w.kernel==="ocean_spectrum"&&d?n.push({tile:d.tile,kMin:d.kMin,kMax:d.kMax,n:d.n}):w.kernel==="ocean_foam"&&d&&(o={cell:d.cell,width:d.width})}return{cascades:n.slice(0,t),foam:o}}function he(e,n){const o=n.graphParams;if(o)for(const w of Object.keys(o))Number.isFinite(o[w])&&e.hasParam(w)&&e.setValue(w,o[w]);Number.isFinite(n.graphTween)&&e.setTween(n.graphTween),e.setNodes(n.graphNodes||{}),x(e,n)}const s=[["hullX","x"],["hullZ","z"],["hullHeading","heading"],["hullSpeed","speed"]];function x(e,n){const o=n.graphNodes&&n.graphNodes.hull;if(!o)return;const w=e.nodePose?e.nodePose(o):null;if(w)for(const d of s){const W=e.graph.params.find(function(O){return O.name===d[0]});W&&W.source.kind==="value"&&e.setValue(d[0],w[d[1]])}}function B(e,n,o,w,d){o.fill(0);const W=e.exportParams(),O=n.foam;n.cascades.forEach(function(K,se){o[se*4]=K.tile,o[se*4+1]=Math.sqrt(Math.max(K.kMin,1e-6)*K.kMax),o[se*4+2]=K.n,o[se*4+3]=1,o[24+se*4]=W["waterModX"+se]||0,o[24+se*4+1]=W["waterModZ"+se]||0});const U=O.cell*O.width,de=function(K){return K-Math.floor(K/U)*U},G=e.export("wake");if(G){const K=d?e.value("cameraX")-(d.x||0):0,se=d?e.value("cameraZ")-(d.z||0):0;o[40]=(W.wakeCamRelX||0)+K,o[41]=(W.wakeCamRelZ||0)+se,o[42]=W.wakeOriginModX||0,o[43]=W.wakeOriginModZ||0,o[44]=W.wakeCell||0,o[45]=W.wakeOn||0,o[46]=G.width,o[47]=w&&w.wake&&w.wake.debug?ie:0}o[16]=1/U,o[17]=1,o[18]=e.value("foamThreshold"),o[19]=a,o[20]=le(e.value("windSpeed")),o[21]=de(e.value("waterX")),o[22]=de(e.value("waterZ")),o[23]=me}function F(e,n){const o=Number.isFinite(n.time)?n.time:0,w=e.last===null?0:Math.max(0,o-e.last),d=Math.min(g,w);e.last=o;const W=n.camera||{};return{time:o,dt:d,accumulateDt:w,cameraX:W.x||0,cameraY:W.y||0,cameraZ:W.z||0}}function _(e){const n=window.__gosx_scene3d_node_poses;return n&&n[e]?n[e]:null}function y(e,n){const o=e&&n();if(!o||!(Math.abs(o.value("driftX"))+Math.abs(o.value("driftZ"))>0))return e;const w=e.speed*Math.sin(e.heading)-o.value("driftX"),d=e.speed*Math.cos(e.heading)-o.value("driftZ");return{x:e.x,z:e.z,heading:e.heading,speed:Math.hypot(w,d)}}function k(){return h}function P(){return m&&!m.failed?m.gpuTiming():null}const T={x:0,z:0,time:0,scale:1,driftX:0,driftZ:0,gen:0};let ne=0;function be(e){if(e.failed){h=null;return}T.x=e.value("waterX"),T.z=e.value("waterZ"),T.time=e.value("simTime"),T.gen=e.waterGen,T.scale=e.value("timeScale"),T.driftX=e.value("driftX"),T.driftZ=e.value("driftZ"),h=T}function xe(e){const n=h;return Z.arr=e,n?(e[1]=n.time,e[2]=n.x,e[3]=n.z,e[4]=1,e[5]=n.scale,e[6]=n.driftX,e[7]=n.driftZ,e[9]=n.gen,$(e,n),!0):!1}const X={COUNT:10,SEQ:11,GEN:12,FLAGS:13,N:14,LAT:15,AT:16,TICK:17,PTS:18,OUT:146},z=1,v=4,Z={arr:null,pending:null,ready:null,delivered:null,failed:"",reported:""};function A(){const e=Z.arr;return e?Math.max(0,Math.min(u,Math.floor(e[X.COUNT])||0)):0}function $(e,n){const o=Z,w=e[0]+1;o.pending&&o.pending.seq===void 0&&(o.pending.seq=w,o.pending.gen=n.gen);const d=o.ready;if(d&&d!==o.delivered&&d.meta.seq!==void 0){const W=d.from,O=d.meta.host.n;for(let U=0;U<O;U++)e[X.OUT+3*U]=d.meta.level+d.heights[W+U],e[X.OUT+3*U+1]=d.nx[W+U],e[X.OUT+3*U+2]=d.nz[W+U];e[X.SEQ]=d.meta.seq,e[X.GEN]=d.meta.gen,e[X.FLAGS]=z,e[X.N]=O,e[X.LAT]=w-d.meta.seq,e[X.AT]=performance.timeOrigin+performance.now(),e[X.TICK]++,o.delivered=d}else o.failed&&o.reported!==o.failed&&A()&&(e[X.SEQ]=w,e[X.GEN]=n.gen,e[X.FLAGS]=v,e[X.N]=0,e[X.LAT]=0,e[X.AT]=performance.timeOrigin+performance.now(),e[X.TICK]++,o.reported=o.failed);o.failed||(o.reported="")}function f(){Z.pending=Z.ready=Z.delivered=null}const p=3,r=250,u=64,D=1,R=8,te=16,ee={collect:[],k:0,latest:null,previous:null};function ue(){ee.collect=[],ee.k=0}function ye(e,n,o,w){const d=ee.k++;ee.collect.length<u&&ee.collect.push({x:e,z:n});const W=Number.isFinite(w)?w:typeof performance<"u"?performance.now():0,O=ee.latest;if(!O||d>=O.count||W-O.at>r||Math.abs(O.points[d].x-e)>8||Math.abs(O.points[d].z-n)>8)return null;const U=ee.previous&&ee.previous.count>d?ee.previous:null,de=U&&O.at>U.at?(O.heights[d]-U.heights[d])/((O.at-U.at)/1e3):0,G=O.heights[d]+de*((W-O.at)/1e3),K=O.nx[d],se=O.nz[d],Y=Math.sqrt(Math.max(0,1-K*K-se*se));return{x:e,y:o+G,z:n,normal:{x:K,y:Y,z:se},velocity:{x:0,y:de,z:0}}}function ze(e,n){const o=[];for(let d=0;d<p;d++)o.push({buffer:e.createBuffer({label:"gosx-ocean-fft-probe-"+d,size:u*te,usage:D|R}),state:"free",meta:null});const w=function(d){const W=d.buffer,O=d.meta;d.state="mapping",W.mapAsync(D).then(function(){if(d.state!=="mapping")return;const U=new Float32Array(W.getMappedRange().slice(0,O.count*te));W.unmap();const de=[],G=[],K=[];for(let se=0;se<O.count;se++)de.push(U[se*4]),G.push(U[se*4+1]),K.push(U[se*4+2]);ee.previous=ee.latest,ee.latest={at:O.at,count:O.count,points:O.points,heights:de,nx:G,nz:K},d.state="free"},function(){d.state="free"})};return{poll:function(){for(const d of o)d.state==="copied"&&w(d)},copy:function(d,W,O){const U=o.find(function(de){return de.state==="free"});U&&(d.copyBufferToBuffer(W,0,U.buffer,0,u*te),U.state="copied",U.meta={at:n(),count:O.length,points:O})},dispose:function(){for(const d of o)d.state="disposed",d.buffer.destroy()}}}function Ze(e,n){if(!e.hasParam("probePoints"))return!1;const o=Math.min(n.length,u);if(!o)return e.setValue("probeCount",0),!1;let w=0,d=0;for(let O=0;O<o;O++)w+=n[O].x,d+=n[O].z;w/=o,d/=o;const W=new Float32Array(u*4);for(let O=0;O<o;O++)W[O*4]=n[O].x-w,W[O*4+1]=n[O].z-d;return e.setValue("probeOriginX",w),e.setValue("probeOriginZ",d),e.setValue("probeCount",o),e.setValue("probePoints",W),!0}function Me(e,n,o){const w=function(d,W){const O=h;if(!O)return e.sample(n,d,W,o);const U=e.sample(n,d-O.x,W-O.z,O.time);return U.x+=O.x,U.z+=O.z,U};return{sample:w,probed:function(d,W){return ye(d,W,n.data[0])||w(d,W)},begin:ue}}function Le(e){const n=e.device,o="gosx-ocean-fft-",w=function(){const I=GPUShaderStage.VERTEX|GPUShaderStage.FRAGMENT,q=[];q.push({binding:L.SLOT.uniform,visibility:I,buffer:{type:"uniform"}},{binding:L.SLOT.sampler,visibility:I,sampler:{type:"filtering"}});for(let E=0;E<t;E++)q.push({binding:L.SLOT.disp+E,visibility:I,texture:{sampleType:"float"}},{binding:L.SLOT.slope+E,visibility:I,texture:{sampleType:"float"}});return q.push({binding:L.SLOT.foam,visibility:I,texture:{sampleType:"float"}},{binding:L.SLOT.wake,visibility:I,texture:{sampleType:"float"}}),q},d=n.createBindGroupLayout({label:o+"surface",entries:w()}),W=n.createSampler({label:o+"sampler",magFilter:"linear",minFilter:"linear",mipmapFilter:"linear",addressModeU:"repeat",addressModeV:"repeat"}),O=n.createBuffer({label:o+"surface",size:ce*4,usage:GPUBufferUsage.UNIFORM|GPUBufferUsage.COPY_DST}),U=n.createTexture({label:o+"dummy",size:[1,1,1],format:"rgba16float",usage:GPUTextureUsage.TEXTURE_BINDING|GPUTextureUsage.COPY_DST}),de=U.createView(),G=new Float32Array(ce),K={last:null,tier:"",runner:null,info:null,viewList:null},se=ze(n,function(){return e.now?e.now():typeof performance<"u"?performance.now():0}),Y={pipeline:null,handle:null,bgl:null,params:null,pts:null,out:null,slots:[],issued:0,f32:new Float32Array(20),u32:null,ptsData:new Float32Array(u*4)};Y.u32=new Uint32Array(Y.f32.buffer);const N={update:function(I,q,E){for(const j of Y.slots)j.state==="copied"&&(j.state="mapping",j.buffer.mapAsync(D).then(function(){if(j.state!=="mapping")return;const ve=new Float32Array(j.buffer.getMappedRange().slice(0)),Se=j.meta.host.n,Fe=[],Ae=[],Pe=[];j.buffer.unmap(),j.state="free";for(let Oe=0;Oe<Se;Oe++)Fe.push(ve[Oe*4]),Ae.push(ve[Oe*4+1]),Pe.push(ve[Oe*4+2]);(!Z.ready||Z.ready.meta.issued<j.meta.issued)&&(Z.ready={meta:j.meta,heights:Fe,nx:Ae,nz:Pe,from:0})},function(){j.state="free"}));const oe=A();if(!oe||Z.failed||!E)return;let ge=Y.slots.find(function(j){return j.state==="free"});if(!(!ge&&Y.slots.length>=p))try{Y.handle||(Y.bgl=n.createBindGroupLayout({label:o+"probe",entries:[{binding:0,visibility:GPUShaderStage.COMPUTE,buffer:{type:"uniform"}},{binding:1,visibility:GPUShaderStage.COMPUTE,buffer:{type:"read-only-storage"}},{binding:2,visibility:GPUShaderStage.COMPUTE,buffer:{type:"storage"}},{binding:3,visibility:GPUShaderStage.COMPUTE,sampler:{type:"filtering"}}].concat([4,5,6,7,8,9,10,11].map(function(_e){return{binding:_e,visibility:GPUShaderStage.COMPUTE,texture:{sampleType:"float"}}}))}),Y.handle=e.create(n,"compute",{label:o+"probe",layout:n.createPipelineLayout({bindGroupLayouts:[Y.bgl]}),compute:{module:n.createShaderModule({label:o+"probe",code:V()}),entryPoint:"main"}}),Y.params=n.createBuffer({label:o+"probe-params",size:80,usage:GPUBufferUsage.UNIFORM|GPUBufferUsage.COPY_DST}),Y.pts=n.createBuffer({label:o+"probe-points",size:u*te,usage:GPUBufferUsage.STORAGE|GPUBufferUsage.COPY_DST}),Y.out=n.createBuffer({label:o+"probe-out",size:u*te,usage:GPUBufferUsage.STORAGE|GPUBufferUsage.COPY_SRC}));const j=e.validation&&e.validation.get(Y.handle),ve=!j||j.ready===!0?j&&j.pipeline||Y.handle:null;if(!ve)return;ge||(ge={state:"free",meta:null,buffer:n.createBuffer({label:o+"probe-staging",size:u*te,usage:D|R})},Y.slots.push(ge));const Se=Z.arr,Fe=q.exportParams(),Ae=Y.f32;Ae.fill(0),Y.u32[0]=oe,Ae[1]=Number(I.ocean&&I.ocean.level)||0,E.cascades.forEach(function(_e,Ee){Ae[4+Ee*4]=_e.tile,Ae[5+Ee*4]=1,Ae[6+Ee*4]=Fe["waterModX"+Ee]||0,Ae[7+Ee*4]=Fe["waterModZ"+Ee]||0}),Y.ptsData.fill(0);for(let _e=0;_e<oe;_e++)Y.ptsData[_e*4]=Se[X.PTS+2*_e],Y.ptsData[_e*4+1]=Se[X.PTS+2*_e+1];n.queue.writeBuffer(Y.params,0,Ae),n.queue.writeBuffer(Y.pts,0,Y.ptsData);const Pe=[{binding:0,resource:{buffer:Y.params}},{binding:1,resource:{buffer:Y.pts}},{binding:2,resource:{buffer:Y.out}},{binding:3,resource:W}];for(let _e=0;_e<t;_e++){const Ee=q.export("disp"+_e),De=q.export("slope"+_e);Pe.push({binding:4+_e,resource:Ee?Ee.view:de},{binding:8+_e,resource:De?De.view:de})}const Oe=I.encoder.beginComputePass({label:o+"probe"});Oe.setPipeline(ve),Oe.setBindGroup(0,n.createBindGroup({label:o+"probe",layout:Y.bgl,entries:Pe})),Oe.dispatchWorkgroups(1),Oe.end(),I.encoder.copyBufferToBuffer(Y.out,0,ge.buffer,0,u*te),ge.state="copied",ge.meta={host:{n:oe},level:Ae[1],issued:++Y.issued},Z.pending=ge.meta}catch(j){Z.failed=String(j&&j.message||j)}},dispose:function(){for(const I of Y.slots)I.state="disposed",I.buffer.destroy();for(const I of[Y.params,Y.pts,Y.out])I&&I.destroy()}},fe={device:n,ready:!1,layout:d,group:null,disposed:!1,tier:"",update:Be,dispose:J,fail:Te};function Te(I){const q="ocean-fft: "+(I&&I.message?I.message:String(I));e.guard&&e.guard.fail("gosx-ocean-fft-surface",q)}function Xe(I){K.runner&&K.runner.dispose();const q=re.create({module:S,backend:"webgpu",tier:I,host:{device:n,create:e.create,validation:e.validation,nodePose:function(E){return y(_(E),function(){return K.runner})}}});q.waterGen=++ne,K.runner=q,K.tier=I,K.last=null,K.viewList=null,fe.group=null,fe.ready=!1,K.info=q.graph?pe(q.graph):null}function we(){const I=K.runner,q=[],E=[{binding:L.SLOT.uniform,resource:{buffer:O}},{binding:L.SLOT.sampler,resource:W}];for(let j=0;j<t;j++){const ve=I.export("disp"+j),Se=I.export("slope"+j);E.push({binding:L.SLOT.disp+j,resource:ve?ve.view:de},{binding:L.SLOT.slope+j,resource:Se?Se.view:de}),q.push(ve?ve.view:de,Se?Se.view:de)}const oe=I.export("foam");E.push({binding:L.SLOT.foam,resource:oe?oe.view:de}),q.push(oe?oe.view:de);const ge=I.export("wake");E.push({binding:L.SLOT.wake,resource:ge?ge.view:de}),q.push(ge?ge.view:de),!(K.viewList&&q.every(function(j,ve){return j===K.viewList[ve]}))&&(K.viewList=q,fe.group=n.createBindGroup({label:o+"surface",layout:d,entries:E}))}function Be(I){const q=H(I.tier);(!K.runner||q!==K.tier)&&Xe(q);const E=K.runner;if(E.failed)return fe.ready=!1,fe.group=null,!1;he(E,I.ocean),se.poll();const oe=ee.collect.slice(0,u),ge=Ze(E,oe),j=E.run(Object.assign(F(K,I),{encoder:I.encoder}));if(E.failed)return fe.ready=!1,fe.group=null,h=null,e.guard&&e.guard.fail("gosx-elio-ocean","ocean graph failed: "+E.failed),!1;if(!j)return fe.ready;const ve=ge?E.export("probe"):null;return ve&&ve.buffer&&se.copy(I.encoder,ve.buffer,oe),we(),B(E,K.info,G,I.ocean,I.camera),n.queue.writeBuffer(O,0,G),be(E),m=E,N.update(I,E,K.info),fe.ready=!0,!0}function J(){fe.disposed||(fe.disposed=!0,fe.ready=!1,fe.group=null,K.runner&&K.runner.dispose(),h=null,m=null,ee.latest=ee.previous=null,f(),se.dispose(),N.dispose(),O.destroy(),U.destroy())}return Object.defineProperty(fe,"runner",{get:function(){return K.runner}}),fe}function i(e,n){const o=n.environment&&n.environment.ocean;let w=e.fft;const d=n.guard&&(n.guard.disabled.has("oceanFFT")||n.guard.disabled.has("elioGraph"));return!o||!o.spectrum||d||w&&w.device!==n.device?(w&&(w.dispose(),e.fft=null),null):(w||(w=e.fft=Le(n)),w.update({ocean:o,tier:n.tier,time:n.time,camera:n.camera,encoder:n.encoder}),w)}const l=[{match:"^wake\\.",every:2,hold:"^(wake|camera[XZ])"}];function b(){return typeof window<"u"&&window.__gosx_elio_diagnostics||{}}function c(){let e=`#version 300 es
precision highp float;
uniform vec4 pts[`+u+`], cs[4]; uniform float level;
`;for(let n=0;n<4;n++)e+="uniform sampler2D d"+n+", s"+n+`;
`;e+=`out vec4 o;
void ev(vec2 q, out vec3 s, out vec3 dx, out vec3 dz) {
  s = vec3(0.); dx = vec3(1., 0., 0.); dz = vec3(0., 0., 1.);
`;for(let n=0;n<4;n++)e+="  if (cs["+n+"].y > 0.5) { vec2 uv = (q - cs["+n+"].zw) / cs["+n+"].x; vec4 a = textureLod(d"+n+", uv, 0.), b = textureLod(s"+n+`, uv, 0.); s += a.xyz; dx += vec3(b.z, b.x, a.w); dz += vec3(a.w, b.y, b.w); }
`;return e+=`}
void main() {
  vec2 t = pts[int(gl_FragCoord.x)].xy, q = t; vec3 s, dx, dz;
`,e+=`  for (int k = 0; k < 8; k++) {
    ev(q, s, dx, dz);
    vec2 e = q + s.xz - t; float det = dx.x * dz.z - dz.x * dx.z;
`,e+=`    if (abs(det) > 1e-4) q -= vec2(dz.z * e.x - dz.x * e.y, dx.x * e.y - dx.z * e.x) / det; else q -= e * 0.5;
  }
`,e+=`  ev(q, s, dx, dz); vec3 n = normalize(cross(dz, dx)); o = vec4(level + s.y, n.x, n.z, 1.);
}
`,e}function V(){let e=`struct E { s: vec3<f32>, dx: vec3<f32>, dz: vec3<f32> };
struct P { count: u32, level: f32, a: f32, b: f32, cs: array<vec4<f32>, 4> };
`;e+=`@group(0) @binding(0) var<uniform> P0: P;
@group(0) @binding(1) var<storage, read> pts: array<vec4<f32>>;
@group(0) @binding(2) var<storage, read_write> outp: array<vec4<f32>>;
@group(0) @binding(3) var smp: sampler;
`;for(let n=0;n<4;n++)e+="@group(0) @binding("+(4+n)+") var d"+n+`: texture_2d<f32>;
@group(0) @binding(`+(8+n)+") var s"+n+`: texture_2d<f32>;
`;e+=`fn ev(q: vec2<f32>) -> E {
  var e: E; e.s = vec3<f32>(0.0); e.dx = vec3<f32>(1.0, 0.0, 0.0); e.dz = vec3<f32>(0.0, 0.0, 1.0);
`;for(let n=0;n<4;n++)e+="  if (P0.cs["+n+"].y > 0.5) { let uv = (q - P0.cs["+n+"].zw) / P0.cs["+n+"].x; let a = textureSampleLevel(d"+n+", smp, uv, 0.0); let b = textureSampleLevel(s"+n+`, smp, uv, 0.0); e.s += a.xyz; e.dx += vec3<f32>(b.z, b.x, a.w); e.dz += vec3<f32>(a.w, b.y, b.w); }
`;return e+=`  return e;
}
@compute @workgroup_size(64) fn main(@builtin(global_invocation_id) g: vec3<u32>) {
  let i = g.x; if (i >= P0.count) { return; }
  let t = pts[i].xy; var q = t; var e = ev(q);
`,e+=`  for (var k = 0; k < 8; k = k + 1) {
    e = ev(q); let ex = q.x + e.s.x - t.x; let ez = q.y + e.s.z - t.y; let det = e.dx.x * e.dz.z - e.dz.x * e.dx.z;
`,e+=`    if (abs(det) > 1e-4) { q = q - vec2<f32>(e.dz.z * ex - e.dz.x * ez, e.dx.x * ez - e.dx.z * ex) / det; } else { q = q - vec2<f32>(ex, ez) * 0.5; }
  }
`,e+=`  e = ev(q); let n = normalize(cross(e.dz, e.dx)); outp[i] = vec4<f32>(P0.level + e.s.y, n.x, n.z, 1.0);
}
`,e}const Q=`#version 300 es
void main() { vec2 p = vec2(float((gl_VertexID << 1) & 2), float(gl_VertexID & 2)); gl_Position = vec4(p * 2. - 1., 0., 1.); }`;function ae(e,n){const o={last:null},w=re.create({module:S,backend:"webgl2",tier:H(n.tier),host:{gl:e,caps:n.caps,textureCache:n.textureCache,profile:n.profile||b().profile,throttle:n.throttle||b().throttle||l,nodePose:function(N){return y(_(N),function(){return U.runner})}}}),d=new Float32Array(ce),W=new Map;w.waterGen=++ne;const O=w.graph?pe(w.graph):null,U={gl:e,runner:w,tier:H(n.tier),disposed:!1,update:de,bind:se,dispose:Y,fail:function(N){w.fail(N)}};function de(N){return w.failed||(he(w,N.ocean),!w.run(F(o,N)))?!1:(B(w,O,d,N.ocean,N.camera),be(w),K(N),!0)}const G={program:null,fbo:null,tex:null,slots:[],issued:0};function K(N){for(const J of G.slots){if(J.state!=="fenced")continue;const I=e.clientWaitSync(J.sync,0,0);if(I!==e.ALREADY_SIGNALED&&I!==e.CONDITION_SATISFIED){I===e.WAIT_FAILED&&(J.state="free",Z.failed="probe-fence");continue}e.bindBuffer(e.PIXEL_PACK_BUFFER,J.pbo),e.getBufferSubData(e.PIXEL_PACK_BUFFER,0,J.dst),e.bindBuffer(e.PIXEL_PACK_BUFFER,null),e.deleteSync(J.sync),J.sync=null,J.state="free";const q=J.meta.host.n,E=[],oe=[],ge=[];for(let j=0;j<q;j++)E.push(J.dst[j*4]),oe.push(J.dst[j*4+1]),ge.push(J.dst[j*4+2]);(!Z.ready||Z.ready.meta.issued<J.meta.issued)&&(Z.ready={meta:J.meta,heights:E,nx:oe,nz:ge,from:0})}const fe=A();if(!fe)return;const Te=G.slots.find(function(J){return J.state==="free"})||(G.slots.length<p?null:void 0);if(Te===void 0||Z.failed)return;const Xe=re.glState.save(e),we=O.cascades,Be=w.exportParams();try{if(!G.program){const q=function(oe,ge){const j=e.createShader(oe);if(e.shaderSource(j,ge),e.compileShader(j),!e.getShaderParameter(j,e.COMPILE_STATUS))throw new Error("probe-compile");return j},E=e.createProgram();if(e.attachShader(E,q(e.VERTEX_SHADER,Q)),e.attachShader(E,q(e.FRAGMENT_SHADER,c())),e.linkProgram(E),!e.getProgramParameter(E,e.LINK_STATUS))throw new Error("probe-link");if(G.program=E,G.tex=e.createTexture(),e.bindTexture(e.TEXTURE_2D,G.tex),e.texStorage2D(e.TEXTURE_2D,1,e.RGBA32F,u,1),G.fbo=e.createFramebuffer(),e.bindFramebuffer(e.FRAMEBUFFER,G.fbo),e.framebufferTexture2D(e.FRAMEBUFFER,e.COLOR_ATTACHMENT0,e.TEXTURE_2D,G.tex,0),e.checkFramebufferStatus(e.FRAMEBUFFER)!==e.FRAMEBUFFER_COMPLETE)throw new Error("probe-target");e.useProgram(E);for(let oe=0;oe<4;oe++)e.uniform1i(e.getUniformLocation(E,"d"+oe),C.UNIT.disp+oe),e.uniform1i(e.getUniformLocation(E,"s"+oe),C.UNIT.slope+oe);G.loc={pts:e.getUniformLocation(E,"pts[0]"),cs:e.getUniformLocation(E,"cs[0]"),level:e.getUniformLocation(E,"level")},G.pts=new Float32Array(u*4),G.cs=new Float32Array(16)}let J=Te;J||(J={state:"free",pbo:e.createBuffer(),dst:new Float32Array(u*4),sync:null,meta:null},e.bindBuffer(e.PIXEL_PACK_BUFFER,J.pbo),e.bufferData(e.PIXEL_PACK_BUFFER,u*te,e.STREAM_READ),G.slots.push(J));const I=Z.arr;G.pts.fill(0),G.cs.fill(0);for(let q=0;q<fe;q++)G.pts[q*4]=I[X.PTS+2*q],G.pts[q*4+1]=I[X.PTS+2*q+1];we.forEach(function(q,E){G.cs[E*4]=q.tile,G.cs[E*4+1]=1,G.cs[E*4+2]=Be["waterModX"+E]||0,G.cs[E*4+3]=Be["waterModZ"+E]||0}),e.bindFramebuffer(e.FRAMEBUFFER,G.fbo),e.viewport(0,0,fe,1),e.disable(e.BLEND),e.disable(e.DEPTH_TEST),e.disable(e.CULL_FACE),e.disable(e.SCISSOR_TEST),e.colorMask(!0,!0,!0,!0),e.useProgram(G.program),e.uniform4fv(G.loc.pts,G.pts),e.uniform4fv(G.loc.cs,G.cs),e.uniform1f(G.loc.level,Number(N.ocean&&N.ocean.level)||0);for(let q=0;q<4;q++){const E=w.export("disp"+q),oe=w.export("slope"+q);e.activeTexture(e.TEXTURE0+C.UNIT.disp+q),e.bindTexture(e.TEXTURE_2D,E?E.tex:null),e.activeTexture(e.TEXTURE0+C.UNIT.slope+q),e.bindTexture(e.TEXTURE_2D,oe?oe.tex:null)}e.drawArrays(e.TRIANGLES,0,3),e.bindBuffer(e.PIXEL_PACK_BUFFER,J.pbo),e.readPixels(0,0,fe,1,e.RGBA,e.FLOAT,0),e.bindBuffer(e.PIXEL_PACK_BUFFER,null),J.sync=e.fenceSync(e.SYNC_GPU_COMMANDS_COMPLETE,0),e.flush(),J.state="fenced",J.meta={host:{n:fe},level:Number(N.ocean&&N.ocean.level)||0,issued:++G.issued},Z.pending=J.meta}catch(J){Z.failed=String(J&&J.message||J)}finally{re.glState.restore(e,Xe)}}function se(N,fe){let Te=W.get(fe);if(!Te){const we=function(Be){return N.getUniformLocation(fe,Be)};Te={sp:we("u_sp[0]"),disp:[0,1,2,3].map(function(Be){return we("u_spDisp"+Be)}),slope:[0,1,2,3].map(function(Be){return we("u_spSlope"+Be)}),foam:we("u_spFoam"),wake:we("u_spWake")},W.set(fe,Te)}const Xe=function(we,Be,J){N.activeTexture(N.TEXTURE0+we),N.bindTexture(N.TEXTURE_2D,J?J.tex:U.dummy),N.uniform1i(Be,we)};U.dummy||(U.dummy=N.createTexture(),N.bindTexture(N.TEXTURE_2D,U.dummy),N.texStorage2D(N.TEXTURE_2D,1,N.RGBA16F,1,1),N.texParameteri(N.TEXTURE_2D,N.TEXTURE_MIN_FILTER,N.LINEAR),N.texParameteri(N.TEXTURE_2D,N.TEXTURE_MAG_FILTER,N.LINEAR));for(let we=0;we<t;we++)Xe(C.UNIT.disp+we,Te.disp[we],w.export("disp"+we)),Xe(C.UNIT.slope+we,Te.slope[we],w.export("slope"+we));Xe(C.UNIT.foam,Te.foam,w.export("foam")),Xe(C.UNIT.wake,Te.wake,w.export("wake")),N.uniform4fv(Te.sp,d)}function Y(){if(!U.disposed){U.disposed=!0,h=null,f();for(const N of G.slots)N.sync&&e.deleteSync(N.sync),e.deleteBuffer(N.pbo);G.program&&(e.deleteProgram(G.program),e.deleteFramebuffer(G.fbo),e.deleteTexture(G.tex)),w.dispose(),U.dummy&&e.deleteTexture(U.dummy)}}return U}function M(e,n,o){let w=e.fft;if(w&&(w.gl!==n||w.disposed||w.tier!==H(o.tier))&&(w.dispose(),w=e.fft=null),e.fftDisabled)return null;if(n.isContextLost&&n.isContextLost())return h=null,null;w||(w=e.fft=ae(n,o));const d=w.update({ocean:o.ocean,time:o.time,camera:o.camera});return w.runner.failed?(e.fftDisabled=w.runner.failed,w.dispose(),e.fft=null,h=null,o.mount&&o.mount.setAttribute&&o.mount.setAttribute("data-gosx-scene3d-ocean-fft-fallback",e.fftDisabled),null):d?w:null}function ke(e,n,o){e.fft&&e.fft.dispose(),e.fft=null,e.fftDisabled=o,h=null,n&&n.mount&&n.mount.setAttribute&&n.mount.setAttribute("data-gosx-scene3d-ocean-fft-fallback",o)}window.__gosx_scene3d_ocean_fft_api={frame:i,water:k,fill:xe,timing:P,nodePose:_,waterRelative:y,sampler:Me,probe:{begin:ue,sample:ye},probeState:ee,describe:pe,WAKE_DEBUG_RANGE:ie,feed:he,surfaceData:B},window.__gosx_scene3d_ocean_fft_gl_api={run:M,fail:ke}})();
