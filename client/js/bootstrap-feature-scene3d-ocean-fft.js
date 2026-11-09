(function(){"use strict";function ie(X){if(X&&typeof X=="object"){for(const M of Object.keys(X))ie(X[M]);Object.freeze(X)}return X}window.__gosx_scene3d_elio_ocean=ie({elio:"v0.6.0",set:"ocean",sha256:"08c0b504826d61cc6b8340869602cb894b36fb2a6eacf74c0d4251c977d21905",kernels:{oceanSpectrum:{wgsl:`struct OceanSpectrumParams {
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
        let fxs = (((u - floor(u)) * f32(nc)) - 0.5);
        let fzs = (((v - floor(v)) * f32(nc)) - 0.5);
        let fxf = floor(fxs);
        let fzf = floor(fzs);
        let x0 = (u32((fxf + f32(nc))) % nc);
        let z0 = (u32((fzf + f32(nc))) % nc);
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
`,gatherLayout:{kernel:"fft_stage",inputs:[{name:"src",kind:"buffer",elem:"vec4",sampler:"src_tex",length:"src_len",width:"src_w",binding:1}],outputs:[{name:"dst",elem:"vec4",format:"",location:0,variable:"dst_out"}],uniforms:[{name:"fft",block:"FFTStage_ubo",struct:"FFTStage",binding:0}]}}},graphs:{"webgpu/high":{name:"ocean",backend:"webgpu",tier:"high",version:1,params:[{name:"windSpeed",type:"f32",source:{kind:"value",value:9},tween:20},{name:"fetch",type:"f32",source:{kind:"value",value:300},tween:20},{name:"gamma",type:"f32",source:{kind:"value",value:3.3},tween:20},{name:"spread",type:"f32",source:{kind:"value",value:8},tween:20},{name:"swell",type:"f32",source:{kind:"value",value:0},tween:20},{name:"windDirDeg",type:"f32",source:{kind:"value",value:0},tween:20},{name:"swellDirDeg",type:"f32",source:{kind:"value",value:0},tween:20},{name:"seed",type:"u32",source:{kind:"value",value:0}},{name:"loopPeriod",type:"f32",source:{kind:"value",value:1e3}},{name:"lambda",type:"f32",source:{kind:"value",value:1},tween:20},{name:"timeScale",type:"f32",source:{kind:"value",value:1}},{name:"driftX",type:"f32",source:{kind:"value",value:0}},{name:"driftZ",type:"f32",source:{kind:"value",value:0}},{name:"foamTau",type:"f32",source:{kind:"value",value:4}},{name:"foamThreshold",type:"f32",source:{kind:"value",value:.9}},{name:"foamGain",type:"f32",source:{kind:"value",value:2}},{name:"probeCount",type:"u32",source:{kind:"value",value:0}},{name:"probeOriginX",type:"f32",source:{kind:"value",value:0}},{name:"probeOriginZ",type:"f32",source:{kind:"value",value:0}},{name:"probePoints",type:"vec4<f32>[]",count:64,source:{kind:"value",value:0}},{name:"dt",type:"f32",source:{kind:"frame",name:"dt"}},{name:"cameraX",type:"f32",source:{kind:"frame",name:"cameraX"}},{name:"cameraZ",type:"f32",source:{kind:"frame",name:"cameraZ"}},{name:"windDir",type:"f32",source:{args:[1.5707963267948966,{op:"mul",args:["param:windDirDeg",.017453292519943295]}],kind:"expr",op:"sub"}},{name:"swellDir",type:"f32",source:{args:[1.5707963267948966,{op:"mul",args:["param:swellDirDeg",.017453292519943295]}],kind:"expr",op:"sub"}},{name:"simTime",type:"f32",source:{kind:"accumulate",of:"param:timeScale"}},{name:"phaseTime",type:"f32",source:{args:["param:simTime","param:loopPeriod"],kind:"expr",op:"mod"}},{name:"waterX",type:"f32",source:{kind:"accumulate",of:"param:driftX"}},{name:"waterZ",type:"f32",source:{kind:"accumulate",of:"param:driftZ"}},{name:"waterModX0",type:"f32",source:{args:["param:waterX",1600],kind:"expr",op:"mod"}},{name:"waterModZ0",type:"f32",source:{args:["param:waterZ",1600],kind:"expr",op:"mod"}},{name:"waterModX1",type:"f32",source:{args:["param:waterX",250],kind:"expr",op:"mod"}},{name:"waterModZ1",type:"f32",source:{args:["param:waterZ",250],kind:"expr",op:"mod"}},{name:"waterModX2",type:"f32",source:{args:["param:waterX",37],kind:"expr",op:"mod"}},{name:"waterModZ2",type:"f32",source:{args:["param:waterZ",37],kind:"expr",op:"mod"}},{name:"waterModX3",type:"f32",source:{args:["param:waterX",5.3],kind:"expr",op:"mod"}},{name:"waterModZ3",type:"f32",source:{args:["param:waterZ",5.3],kind:"expr",op:"mod"}},{name:"foamOriginX",type:"i32",source:{args:[{op:"floor",args:[{op:"div",args:[{op:"sub",args:["param:cameraX","param:waterX"]},.5]}]},512],kind:"expr",op:"sub"}},{name:"foamShiftX",type:"i32",source:{kind:"delta",of:"param:foamOriginX"}},{name:"foamOriginMod0X",type:"f32",source:{args:[{op:"mul",args:["param:foamOriginX",.5]},1600],kind:"expr",op:"mod"}},{name:"foamOriginMod1X",type:"f32",source:{args:[{op:"mul",args:["param:foamOriginX",.5]},250],kind:"expr",op:"mod"}},{name:"foamOriginMod2X",type:"f32",source:{args:[{op:"mul",args:["param:foamOriginX",.5]},37],kind:"expr",op:"mod"}},{name:"foamOriginMod3X",type:"f32",source:{args:[{op:"mul",args:["param:foamOriginX",.5]},5.3],kind:"expr",op:"mod"}},{name:"probeOriginMod0X",type:"f32",source:{args:[{op:"sub",args:["param:probeOriginX","param:waterX"]},1600],kind:"expr",op:"mod"}},{name:"probeOriginMod1X",type:"f32",source:{args:[{op:"sub",args:["param:probeOriginX","param:waterX"]},250],kind:"expr",op:"mod"}},{name:"probeOriginMod2X",type:"f32",source:{args:[{op:"sub",args:["param:probeOriginX","param:waterX"]},37],kind:"expr",op:"mod"}},{name:"probeOriginMod3X",type:"f32",source:{args:[{op:"sub",args:["param:probeOriginX","param:waterX"]},5.3],kind:"expr",op:"mod"}},{name:"foamOriginZ",type:"i32",source:{args:[{op:"floor",args:[{op:"div",args:[{op:"sub",args:["param:cameraZ","param:waterZ"]},.5]}]},512],kind:"expr",op:"sub"}},{name:"foamShiftZ",type:"i32",source:{kind:"delta",of:"param:foamOriginZ"}},{name:"foamOriginMod0Z",type:"f32",source:{args:[{op:"mul",args:["param:foamOriginZ",.5]},1600],kind:"expr",op:"mod"}},{name:"foamOriginMod1Z",type:"f32",source:{args:[{op:"mul",args:["param:foamOriginZ",.5]},250],kind:"expr",op:"mod"}},{name:"foamOriginMod2Z",type:"f32",source:{args:[{op:"mul",args:["param:foamOriginZ",.5]},37],kind:"expr",op:"mod"}},{name:"foamOriginMod3Z",type:"f32",source:{args:[{op:"mul",args:["param:foamOriginZ",.5]},5.3],kind:"expr",op:"mod"}},{name:"probeOriginMod0Z",type:"f32",source:{args:[{op:"sub",args:["param:probeOriginZ","param:waterZ"]},1600],kind:"expr",op:"mod"}},{name:"probeOriginMod1Z",type:"f32",source:{args:[{op:"sub",args:["param:probeOriginZ","param:waterZ"]},250],kind:"expr",op:"mod"}},{name:"probeOriginMod2Z",type:"f32",source:{args:[{op:"sub",args:["param:probeOriginZ","param:waterZ"]},37],kind:"expr",op:"mod"}},{name:"probeOriginMod3Z",type:"f32",source:{args:[{op:"sub",args:["param:probeOriginZ","param:waterZ"]},5.3],kind:"expr",op:"mod"}},{name:"hullX",type:"f32",source:{kind:"value",value:0}},{name:"hullZ",type:"f32",source:{kind:"value",value:0}},{name:"hullHeading",type:"f32",source:{kind:"value",value:0}},{name:"hullSpeed",type:"f32",source:{kind:"value",value:0}},{name:"wakeEnabled",type:"bool",source:{kind:"value",value:0}},{name:"hullLength",type:"f32",source:{kind:"value",value:200}},{name:"hullBeam",type:"f32",source:{kind:"value",value:30}},{name:"wakeCell",type:"f32",source:{kind:"value",value:4}},{name:"wakeDepth",type:"f32",source:{kind:"value",value:.4}},{name:"wakeGravity",type:"f32",source:{kind:"value",value:9.81}},{name:"wakeAlpha",type:"f32",source:{kind:"value",value:.02}},{name:"wakeMinLambdaCells",type:"f32",source:{kind:"value",value:8}},{name:"wakeFoamGain",type:"f32",source:{kind:"value",value:1}},{name:"wakeFoamTau",type:"f32",source:{kind:"value",value:48}},{name:"wakeDiffuse",type:"f32",source:{kind:"value",value:1.5}},{name:"wakeJitter",type:"f32",source:{kind:"value",value:1}},{name:"wakeBowGain",type:"f32",source:{kind:"value",value:.2}},{name:"wakeBowFoam",type:"f32",source:{kind:"value",value:.9}},{name:"wakeOn",type:"f32",source:{args:["param:wakeEnabled",1],kind:"expr",op:"mul"}},{name:"wakeFoamGainEff",type:"f32",source:{args:["param:wakeEnabled","param:wakeFoamGain"],kind:"expr",op:"mul"}},{name:"wakeEdge",type:"i32",source:{kind:"delta",of:"param:wakeEnabled"}},{name:"wakeReset",type:"u32",source:{args:["param:wakeEdge",0],kind:"expr",op:"max"}},{name:"wakeLength",type:"f32",source:{args:["param:wakeCell",256],kind:"expr",op:"mul"}},{name:"hullWaterX",type:"f32",source:{args:["param:hullX","param:waterX"],kind:"expr",op:"sub"}},{name:"wakeOriginX",type:"i32",source:{args:[{op:"div",args:[{op:"sub",args:[{op:"sub",args:["param:hullWaterX",{op:"mul",args:[{op:"sin",args:["param:hullHeading"]},{op:"mul",args:["param:wakeCell",51.2]}]}]},{op:"mul",args:["param:wakeCell",128]}]},"param:wakeCell"]}],kind:"expr",op:"floor"}},{name:"wakeShiftX",type:"i32",source:{kind:"delta",of:"param:wakeOriginX"}},{name:"wakeHullU",type:"f32",source:{args:["param:hullWaterX",{op:"mul",args:["param:wakeOriginX","param:wakeCell"]}],kind:"expr",op:"sub"}},{name:"wakeOriginModX",type:"i32",source:{args:["param:wakeOriginX",256],kind:"expr",op:"mod"}},{name:"wakeRelX",type:"f32",source:{args:[{op:"mul",args:["param:foamOriginX",.5]},{op:"mul",args:["param:wakeOriginX","param:wakeCell"]}],kind:"expr",op:"sub"}},{name:"wakeCamRelX",type:"f32",source:{args:[{op:"add",args:["param:waterX",{op:"mul",args:["param:wakeOriginX","param:wakeCell"]}]},"param:cameraX"],kind:"expr",op:"sub"}},{name:"hullWaterZ",type:"f32",source:{args:["param:hullZ","param:waterZ"],kind:"expr",op:"sub"}},{name:"wakeOriginZ",type:"i32",source:{args:[{op:"div",args:[{op:"sub",args:[{op:"sub",args:["param:hullWaterZ",{op:"mul",args:[{op:"cos",args:["param:hullHeading"]},{op:"mul",args:["param:wakeCell",51.2]}]}]},{op:"mul",args:["param:wakeCell",128]}]},"param:wakeCell"]}],kind:"expr",op:"floor"}},{name:"wakeShiftZ",type:"i32",source:{kind:"delta",of:"param:wakeOriginZ"}},{name:"wakeHullV",type:"f32",source:{args:["param:hullWaterZ",{op:"mul",args:["param:wakeOriginZ","param:wakeCell"]}],kind:"expr",op:"sub"}},{name:"wakeOriginModZ",type:"i32",source:{args:["param:wakeOriginZ",256],kind:"expr",op:"mod"}},{name:"wakeRelZ",type:"f32",source:{args:[{op:"mul",args:["param:foamOriginZ",.5]},{op:"mul",args:["param:wakeOriginZ","param:wakeCell"]}],kind:"expr",op:"sub"}},{name:"wakeCamRelZ",type:"f32",source:{args:[{op:"add",args:["param:waterZ",{op:"mul",args:["param:wakeOriginZ","param:wakeCell"]}]},"param:cameraZ"],kind:"expr",op:"sub"}},{name:"wakeGEff",type:"f32",source:{args:[{op:"min",args:[{op:"min",args:["param:wakeGravity",{op:"mul",args:[{op:"mul",args:["param:hullSpeed","param:hullSpeed"]},{op:"div",args:[6.283185307179586,{op:"mul",args:["param:wakeMinLambdaCells","param:wakeCell"]}]}]}]},{op:"mul",args:["param:wakeCell",16.39001041697031]}]},.05],kind:"expr",op:"max"}},{name:"wakeDt",type:"f32",source:{args:[{op:"mul",args:["param:dt","param:timeScale"]},.2],kind:"expr",op:"min"}},{name:"wakeSrcLength",type:"f32",source:{args:[{op:"min",args:["param:hullLength",{op:"div",args:[{op:"mul",args:[{op:"mul",args:["param:hullSpeed","param:hullSpeed"]},3.141592653589793]},"param:wakeGEff"]}]},{op:"mul",args:["param:wakeCell",2]}],kind:"expr",op:"max"}},{name:"wakeSrcBeam",type:"f32",source:{args:[{op:"min",args:["param:hullBeam",{op:"mul",args:["param:wakeSrcLength",.67]}]},{op:"mul",args:["param:wakeCell",1.5]}],kind:"expr",op:"max"}}],resources:[{name:"h0_0",kind:"buffer",elementType:"vec4<f32>",count:4096},{name:"omega_0",kind:"buffer",elementType:"f32",count:4096},{name:"fftA_0",kind:"buffer",elementType:"vec4<f32>",count:4096},{name:"fftB_0",kind:"buffer",elementType:"vec4<f32>",count:4096},{name:"dispTex0",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"slopeTex0",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"h0_1",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"omega_1",kind:"buffer",elementType:"f32",count:65536},{name:"fftA_1",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"fftB_1",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"dispTex1",kind:"texture",format:"rgba16float",width:256,height:256,mipLevels:9},{name:"slopeTex1",kind:"texture",format:"rgba16float",width:256,height:256,mipLevels:9},{name:"h0_2",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"omega_2",kind:"buffer",elementType:"f32",count:65536},{name:"fftA_2",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"fftB_2",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"dispTex2",kind:"texture",format:"rgba16float",width:256,height:256,mipLevels:9},{name:"slopeTex2",kind:"texture",format:"rgba16float",width:256,height:256,mipLevels:9},{name:"h0_3",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"omega_3",kind:"buffer",elementType:"f32",count:65536},{name:"fftA_3",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"fftB_3",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"dispTex3",kind:"texture",format:"rgba16float",width:256,height:256,mipLevels:9},{name:"slopeTex3",kind:"texture",format:"rgba16float",width:256,height:256,mipLevels:9},{name:"wakeA",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"wakeB",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"wakeF",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"wakeG",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"wakeC",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"wakeTex",kind:"texture",format:"rgba16float",width:256,height:256,mipLevels:9},{name:"foamA",kind:"buffer",elementType:"f32",count:1048576},{name:"foamB",kind:"buffer",elementType:"f32",count:1048576},{name:"foamTex",kind:"texture",format:"rgba16float",width:1024,height:1024,mipLevels:11},{name:"probePoints",kind:"buffer",elementType:"vec4<f32>",count:64,feed:"probePoints"},{name:"probeOut",kind:"buffer",elementType:"vec4<f32>",count:64}],passes:[{name:"c0.spectrum",kernel:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",dispatch:[64,1,1],bindings:[{name:"h0",resource:"h0_0"},{name:"omega",resource:"omega_0"}],uniforms:[{name:"spec",values:{cascade:0,fetchKm:"param:fetch",gamma:"param:gamma",kMax:.12,kMin:.003926990816987242,loopPeriod:"param:loopPeriod",n:64,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:1600,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c0.evolve",kernel:"ocean_evolve",dispatch:[64,1,1],bindings:[{name:"h0",resource:"h0_0"},{name:"omega",resource:"omega_0"},{name:"outA",resource:"fftA_0"},{name:"outB",resource:"fftB_0"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:64,t:"param:phaseTime",tile:1600}}]},{name:"c0.fftA.fft_rows",kernel:"fft_compute_64",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftA_0"}]},{name:"c0.fftA.fft_cols",kernel:"fft_compute_64",entry:"fft_cols",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftA_0"}]},{name:"c0.fftB.fft_rows",kernel:"fft_compute_64",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftB_0"}]},{name:"c0.fftB.fft_cols",kernel:"fft_compute_64",entry:"fft_cols",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftB_0"}]},{name:"c0.assemble",kernel:"ocean_assemble",dispatch:[64,1,1],bindings:[{name:"fieldA",resource:"fftA_0"},{name:"fieldB",resource:"fftB_0"},{name:"disp",resource:"dispTex0"},{name:"slope",resource:"slopeTex0"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:64}}]},{name:"c0.dispTex0.mips",type:"repeat",count:6,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[16,1,1],bindings:[{name:"src",resource:"dispTex0",levelAt:{index:"l"}},{name:"dst",resource:"dispTex0",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:64},dstWidth:{index:"l",offset:1,shr:64},pad:0,srcLevel:0}}]}]},{name:"c0.slopeTex0.mips",type:"repeat",count:6,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[16,1,1],bindings:[{name:"src",resource:"slopeTex0",levelAt:{index:"l"}},{name:"dst",resource:"slopeTex0",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:64},dstWidth:{index:"l",offset:1,shr:64},pad:0,srcLevel:0}}]}]},{name:"c1.spectrum",kernel:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",dispatch:[1024,1,1],bindings:[{name:"h0",resource:"h0_1"},{name:"omega",resource:"omega_1"}],uniforms:[{name:"spec",values:{cascade:1,fetchKm:"param:fetch",gamma:"param:gamma",kMax:.8,kMin:.12,loopPeriod:"param:loopPeriod",n:256,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:250,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c1.evolve",kernel:"ocean_evolve",dispatch:[1024,1,1],bindings:[{name:"h0",resource:"h0_1"},{name:"omega",resource:"omega_1"},{name:"outA",resource:"fftA_1"},{name:"outB",resource:"fftB_1"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:256,t:"param:phaseTime",tile:250}}]},{name:"c1.fftA.fft_rows",kernel:"fft_compute_256",dispatch:[256,1,1],bindings:[{name:"data",resource:"fftA_1"}]},{name:"c1.fftA.fft_cols",kernel:"fft_compute_256",entry:"fft_cols",dispatch:[256,1,1],bindings:[{name:"data",resource:"fftA_1"}]},{name:"c1.fftB.fft_rows",kernel:"fft_compute_256",dispatch:[256,1,1],bindings:[{name:"data",resource:"fftB_1"}]},{name:"c1.fftB.fft_cols",kernel:"fft_compute_256",entry:"fft_cols",dispatch:[256,1,1],bindings:[{name:"data",resource:"fftB_1"}]},{name:"c1.assemble",kernel:"ocean_assemble",dispatch:[1024,1,1],bindings:[{name:"fieldA",resource:"fftA_1"},{name:"fieldB",resource:"fftB_1"},{name:"disp",resource:"dispTex1"},{name:"slope",resource:"slopeTex1"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:256}}]},{name:"c1.dispTex1.mips",type:"repeat",count:8,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[256,1,1],bindings:[{name:"src",resource:"dispTex1",levelAt:{index:"l"}},{name:"dst",resource:"dispTex1",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:256},dstWidth:{index:"l",offset:1,shr:256},pad:0,srcLevel:0}}]}]},{name:"c1.slopeTex1.mips",type:"repeat",count:8,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[256,1,1],bindings:[{name:"src",resource:"slopeTex1",levelAt:{index:"l"}},{name:"dst",resource:"slopeTex1",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:256},dstWidth:{index:"l",offset:1,shr:256},pad:0,srcLevel:0}}]}]},{name:"c2.spectrum",kernel:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",dispatch:[1024,1,1],bindings:[{name:"h0",resource:"h0_2"},{name:"omega",resource:"omega_2"}],uniforms:[{name:"spec",values:{cascade:2,fetchKm:"param:fetch",gamma:"param:gamma",kMax:5.4,kMin:.8,loopPeriod:"param:loopPeriod",n:256,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:37,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c2.evolve",kernel:"ocean_evolve",dispatch:[1024,1,1],bindings:[{name:"h0",resource:"h0_2"},{name:"omega",resource:"omega_2"},{name:"outA",resource:"fftA_2"},{name:"outB",resource:"fftB_2"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:256,t:"param:phaseTime",tile:37}}]},{name:"c2.fftA.fft_rows",kernel:"fft_compute_256",dispatch:[256,1,1],bindings:[{name:"data",resource:"fftA_2"}]},{name:"c2.fftA.fft_cols",kernel:"fft_compute_256",entry:"fft_cols",dispatch:[256,1,1],bindings:[{name:"data",resource:"fftA_2"}]},{name:"c2.fftB.fft_rows",kernel:"fft_compute_256",dispatch:[256,1,1],bindings:[{name:"data",resource:"fftB_2"}]},{name:"c2.fftB.fft_cols",kernel:"fft_compute_256",entry:"fft_cols",dispatch:[256,1,1],bindings:[{name:"data",resource:"fftB_2"}]},{name:"c2.assemble",kernel:"ocean_assemble",dispatch:[1024,1,1],bindings:[{name:"fieldA",resource:"fftA_2"},{name:"fieldB",resource:"fftB_2"},{name:"disp",resource:"dispTex2"},{name:"slope",resource:"slopeTex2"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:256}}]},{name:"c2.dispTex2.mips",type:"repeat",count:8,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[256,1,1],bindings:[{name:"src",resource:"dispTex2",levelAt:{index:"l"}},{name:"dst",resource:"dispTex2",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:256},dstWidth:{index:"l",offset:1,shr:256},pad:0,srcLevel:0}}]}]},{name:"c2.slopeTex2.mips",type:"repeat",count:8,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[256,1,1],bindings:[{name:"src",resource:"slopeTex2",levelAt:{index:"l"}},{name:"dst",resource:"slopeTex2",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:256},dstWidth:{index:"l",offset:1,shr:256},pad:0,srcLevel:0}}]}]},{name:"c3.spectrum",kernel:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",dispatch:[1024,1,1],bindings:[{name:"h0",resource:"h0_3"},{name:"omega",resource:"omega_3"}],uniforms:[{name:"spec",values:{cascade:3,fetchKm:"param:fetch",gamma:"param:gamma",kMax:37,kMin:5.4,loopPeriod:"param:loopPeriod",n:256,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:5.3,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c3.evolve",kernel:"ocean_evolve",dispatch:[1024,1,1],bindings:[{name:"h0",resource:"h0_3"},{name:"omega",resource:"omega_3"},{name:"outA",resource:"fftA_3"},{name:"outB",resource:"fftB_3"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:256,t:"param:phaseTime",tile:5.3}}]},{name:"c3.fftA.fft_rows",kernel:"fft_compute_256",dispatch:[256,1,1],bindings:[{name:"data",resource:"fftA_3"}]},{name:"c3.fftA.fft_cols",kernel:"fft_compute_256",entry:"fft_cols",dispatch:[256,1,1],bindings:[{name:"data",resource:"fftA_3"}]},{name:"c3.fftB.fft_rows",kernel:"fft_compute_256",dispatch:[256,1,1],bindings:[{name:"data",resource:"fftB_3"}]},{name:"c3.fftB.fft_cols",kernel:"fft_compute_256",entry:"fft_cols",dispatch:[256,1,1],bindings:[{name:"data",resource:"fftB_3"}]},{name:"c3.assemble",kernel:"ocean_assemble",dispatch:[1024,1,1],bindings:[{name:"fieldA",resource:"fftA_3"},{name:"fieldB",resource:"fftB_3"},{name:"disp",resource:"dispTex3"},{name:"slope",resource:"slopeTex3"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:256}}]},{name:"c3.dispTex3.mips",type:"repeat",count:8,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[256,1,1],bindings:[{name:"src",resource:"dispTex3",levelAt:{index:"l"}},{name:"dst",resource:"dispTex3",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:256},dstWidth:{index:"l",offset:1,shr:256},pad:0,srcLevel:0}}]}]},{name:"c3.slopeTex3.mips",type:"repeat",count:8,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[256,1,1],bindings:[{name:"src",resource:"slopeTex3",levelAt:{index:"l"}},{name:"dst",resource:"slopeTex3",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:256},dstWidth:{index:"l",offset:1,shr:256},pad:0,srcLevel:0}}]}]},{name:"wake.source",kernel:"wake_source",when:"param:wakeEnabled",dispatch:[1024,1,1],bindings:[{name:"wakeIn",resource:"wakeA"},{name:"fftOut",resource:"wakeF"},{name:"cleanOut",resource:"wakeC"}],uniforms:[{name:"wake",values:{bowFoam:"param:wakeBowFoam",bowGain:"param:wakeBowGain",cell:"param:wakeCell",depth:"param:wakeDepth",diffuse:"param:wakeDiffuse",dt:"param:wakeDt",foamSpeed:5,fullSpeed:3,heading:"param:hullHeading",hullBeam:"param:hullBeam",hullLen:"param:hullLength",hullU:"param:wakeHullU",hullV:"param:wakeHullV",jitter:"param:wakeJitter",originX:"param:wakeOriginX",originZ:"param:wakeOriginZ",reset:"param:wakeReset",shiftX:"param:wakeShiftX",shiftZ:"param:wakeShiftZ",speed:"param:hullSpeed",srcBeam:"param:wakeSrcBeam",srcLength:"param:wakeSrcLength",tau:"param:wakeFoamTau",width:256}}]},{name:"wake.fft1.fft_rows",kernel:"fft_compute_256",when:"param:wakeEnabled",dispatch:[256,1,1],bindings:[{name:"data",resource:"wakeF"}]},{name:"wake.fft1.fft_cols",kernel:"fft_compute_256",entry:"fft_cols",when:"param:wakeEnabled",dispatch:[256,1,1],bindings:[{name:"data",resource:"wakeF"}]},{name:"wake.symbol",kernel:"wake_symbol",when:"param:wakeEnabled",dispatch:[1024,1,1],bindings:[{name:"fftIn",resource:"wakeF"},{name:"fftOut",resource:"wakeG"}],uniforms:[{name:"sym",values:{n:256,window:"param:wakeLength"}}]},{name:"wake.fft2.fft_rows",kernel:"fft_compute_256",when:"param:wakeEnabled",dispatch:[256,1,1],bindings:[{name:"data",resource:"wakeG"}]},{name:"wake.fft2.fft_cols",kernel:"fft_compute_256",entry:"fft_cols",when:"param:wakeEnabled",dispatch:[256,1,1],bindings:[{name:"data",resource:"wakeG"}]},{name:"wake.step",kernel:"wake_step",when:"param:wakeEnabled",dispatch:[1024,1,1],bindings:[{name:"qIn",resource:"wakeG"},{name:"cleanIn",resource:"wakeC"},{name:"stateOut",resource:"wakeB"},{name:"wakeTex",resource:"wakeTex"}],uniforms:[{name:"wstep",values:{alpha:"param:wakeAlpha",cell:"param:wakeCell",dt:"param:wakeDt",gravity:"param:wakeGEff",originX:"param:wakeOriginX",originZ:"param:wakeOriginZ",sponge:24,spongeRate:.8,width:256}}]},{name:"wake.wakeTex.mips",type:"repeat",when:"param:wakeEnabled",count:8,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[256,1,1],bindings:[{name:"src",resource:"wakeTex",levelAt:{index:"l"}},{name:"dst",resource:"wakeTex",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:256},dstWidth:{index:"l",offset:1,shr:256},pad:0,srcLevel:0}}]}]},{name:"foam",kernel:"ocean_foam",dispatch:[16384,1,1],bindings:[{name:"foamIn",resource:"foamA"},{name:"fieldB0",resource:"fftB_0"},{name:"fieldB1",resource:"fftB_1"},{name:"fieldB2",resource:"fftB_2"},{name:"fieldB3",resource:"fftB_3"},{name:"foamOut",resource:"foamB"},{name:"foamTex",resource:"foamTex"},{name:"wakeIn",resource:"wakeB"}],uniforms:[{name:"foam",values:{cascades:4,cell:.5,dt:"param:dt",gain:"param:foamGain",lambda:"param:lambda",n0:64,n1:256,n2:256,n3:256,omX0:"param:foamOriginMod0X",omX1:"param:foamOriginMod1X",omX2:"param:foamOriginMod2X",omX3:"param:foamOriginMod3X",omZ0:"param:foamOriginMod0Z",omZ1:"param:foamOriginMod1Z",omZ2:"param:foamOriginMod2Z",omZ3:"param:foamOriginMod3Z",originX:"param:foamOriginX",originZ:"param:foamOriginZ",shiftX:"param:foamShiftX",shiftZ:"param:foamShiftZ",size0:1600,size1:250,size2:37,size3:5.3,tau:"param:foamTau",threshold:"param:foamThreshold",wakeCell:"param:wakeCell",wakeGain:"param:wakeFoamGainEff",wakeOriginX:"param:wakeOriginModX",wakeOriginZ:"param:wakeOriginModZ",wakeRelX:"param:wakeRelX",wakeRelZ:"param:wakeRelZ",wakeWidth:256,width:1024}}]},{name:"foamTex.mips",type:"repeat",count:10,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[4096,1,1],bindings:[{name:"src",resource:"foamTex",levelAt:{index:"l"}},{name:"dst",resource:"foamTex",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:1024},dstWidth:{index:"l",offset:1,shr:1024},pad:0,srcLevel:0}}]}]},{name:"probe",kernel:"ocean_probe",dispatch:[1,1,1],bindings:[{name:"points",resource:"probePoints"},{name:"disp0",resource:"fftA_0"},{name:"disp1",resource:"fftA_1"},{name:"disp2",resource:"fftA_2"},{name:"disp3",resource:"fftA_3"},{name:"out",resource:"probeOut"}],uniforms:[{name:"probe",values:{cascades:4,count:"param:probeCount",lambda:"param:lambda",n0:64,n1:256,n2:256,n3:256,normalStep:.5,omX0:"param:probeOriginMod0X",omX1:"param:probeOriginMod1X",omX2:"param:probeOriginMod2X",omX3:"param:probeOriginMod3X",omZ0:"param:probeOriginMod0Z",omZ1:"param:probeOriginMod1Z",omZ2:"param:probeOriginMod2Z",omZ3:"param:probeOriginMod3Z",size0:1600,size1:250,size2:37,size3:5.3,t:"param:simTime"}}]}],frameSwaps:[["wakeA","wakeB"],["foamA","foamB"]],exports:{disp0:"dispTex0",disp1:"dispTex1",disp2:"dispTex2",disp3:"dispTex3",foam:"foamTex",probe:"probeOut",slope0:"slopeTex0",slope1:"slopeTex1",slope2:"slopeTex2",slope3:"slopeTex3",wake:"wakeTex"},exportParams:["waterModX0","waterModZ0","waterModX1","waterModZ1","waterModX2","waterModZ2","waterModX3","waterModZ3","wakeCamRelX","wakeCamRelZ","wakeOriginModX","wakeOriginModZ","wakeCell","wakeOn"]},"webgpu/medium":{name:"ocean",backend:"webgpu",tier:"medium",version:1,resources:[{name:"h0_0",kind:"buffer",elementType:"vec4<f32>",count:4096},{name:"omega_0",kind:"buffer",elementType:"f32",count:4096},{name:"fftA_0",kind:"buffer",elementType:"vec4<f32>",count:4096},{name:"fftB_0",kind:"buffer",elementType:"vec4<f32>",count:4096},{name:"dispTex0",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"slopeTex0",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"h0_1",kind:"buffer",elementType:"vec4<f32>",count:16384},{name:"omega_1",kind:"buffer",elementType:"f32",count:16384},{name:"fftA_1",kind:"buffer",elementType:"vec4<f32>",count:16384},{name:"fftB_1",kind:"buffer",elementType:"vec4<f32>",count:16384},{name:"dispTex1",kind:"texture",format:"rgba16float",width:128,height:128,mipLevels:8},{name:"slopeTex1",kind:"texture",format:"rgba16float",width:128,height:128,mipLevels:8},{name:"h0_2",kind:"buffer",elementType:"vec4<f32>",count:16384},{name:"omega_2",kind:"buffer",elementType:"f32",count:16384},{name:"fftA_2",kind:"buffer",elementType:"vec4<f32>",count:16384},{name:"fftB_2",kind:"buffer",elementType:"vec4<f32>",count:16384},{name:"dispTex2",kind:"texture",format:"rgba16float",width:128,height:128,mipLevels:8},{name:"slopeTex2",kind:"texture",format:"rgba16float",width:128,height:128,mipLevels:8},{name:"h0_3",kind:"buffer",elementType:"vec4<f32>",count:16384},{name:"omega_3",kind:"buffer",elementType:"f32",count:16384},{name:"fftA_3",kind:"buffer",elementType:"vec4<f32>",count:16384},{name:"fftB_3",kind:"buffer",elementType:"vec4<f32>",count:16384},{name:"dispTex3",kind:"texture",format:"rgba16float",width:128,height:128,mipLevels:8},{name:"slopeTex3",kind:"texture",format:"rgba16float",width:128,height:128,mipLevels:8},{name:"wakeA",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"wakeB",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"wakeF",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"wakeG",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"wakeC",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"wakeTex",kind:"texture",format:"rgba16float",width:256,height:256,mipLevels:9},{name:"foamA",kind:"buffer",elementType:"f32",count:1048576},{name:"foamB",kind:"buffer",elementType:"f32",count:1048576},{name:"foamTex",kind:"texture",format:"rgba16float",width:1024,height:1024,mipLevels:11},{name:"probePoints",kind:"buffer",elementType:"vec4<f32>",count:64,feed:"probePoints"},{name:"probeOut",kind:"buffer",elementType:"vec4<f32>",count:64}],passes:[{name:"c0.spectrum",kernel:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",dispatch:[64,1,1],bindings:[{name:"h0",resource:"h0_0"},{name:"omega",resource:"omega_0"}],uniforms:[{name:"spec",values:{cascade:0,fetchKm:"param:fetch",gamma:"param:gamma",kMax:.12,kMin:.003926990816987242,loopPeriod:"param:loopPeriod",n:64,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:1600,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c0.evolve",kernel:"ocean_evolve",dispatch:[64,1,1],bindings:[{name:"h0",resource:"h0_0"},{name:"omega",resource:"omega_0"},{name:"outA",resource:"fftA_0"},{name:"outB",resource:"fftB_0"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:64,t:"param:phaseTime",tile:1600}}]},{name:"c0.fftA.fft_rows",kernel:"fft_compute_64",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftA_0"}]},{name:"c0.fftA.fft_cols",kernel:"fft_compute_64",entry:"fft_cols",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftA_0"}]},{name:"c0.fftB.fft_rows",kernel:"fft_compute_64",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftB_0"}]},{name:"c0.fftB.fft_cols",kernel:"fft_compute_64",entry:"fft_cols",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftB_0"}]},{name:"c0.assemble",kernel:"ocean_assemble",dispatch:[64,1,1],bindings:[{name:"fieldA",resource:"fftA_0"},{name:"fieldB",resource:"fftB_0"},{name:"disp",resource:"dispTex0"},{name:"slope",resource:"slopeTex0"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:64}}]},{name:"c0.dispTex0.mips",type:"repeat",count:6,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[16,1,1],bindings:[{name:"src",resource:"dispTex0",levelAt:{index:"l"}},{name:"dst",resource:"dispTex0",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:64},dstWidth:{index:"l",offset:1,shr:64},pad:0,srcLevel:0}}]}]},{name:"c0.slopeTex0.mips",type:"repeat",count:6,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[16,1,1],bindings:[{name:"src",resource:"slopeTex0",levelAt:{index:"l"}},{name:"dst",resource:"slopeTex0",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:64},dstWidth:{index:"l",offset:1,shr:64},pad:0,srcLevel:0}}]}]},{name:"c1.spectrum",kernel:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",dispatch:[256,1,1],bindings:[{name:"h0",resource:"h0_1"},{name:"omega",resource:"omega_1"}],uniforms:[{name:"spec",values:{cascade:1,fetchKm:"param:fetch",gamma:"param:gamma",kMax:.8,kMin:.12,loopPeriod:"param:loopPeriod",n:128,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:250,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c1.evolve",kernel:"ocean_evolve",dispatch:[256,1,1],bindings:[{name:"h0",resource:"h0_1"},{name:"omega",resource:"omega_1"},{name:"outA",resource:"fftA_1"},{name:"outB",resource:"fftB_1"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:128,t:"param:phaseTime",tile:250}}]},{name:"c1.fftA.fft_rows",kernel:"fft_compute_128",dispatch:[128,1,1],bindings:[{name:"data",resource:"fftA_1"}]},{name:"c1.fftA.fft_cols",kernel:"fft_compute_128",entry:"fft_cols",dispatch:[128,1,1],bindings:[{name:"data",resource:"fftA_1"}]},{name:"c1.fftB.fft_rows",kernel:"fft_compute_128",dispatch:[128,1,1],bindings:[{name:"data",resource:"fftB_1"}]},{name:"c1.fftB.fft_cols",kernel:"fft_compute_128",entry:"fft_cols",dispatch:[128,1,1],bindings:[{name:"data",resource:"fftB_1"}]},{name:"c1.assemble",kernel:"ocean_assemble",dispatch:[256,1,1],bindings:[{name:"fieldA",resource:"fftA_1"},{name:"fieldB",resource:"fftB_1"},{name:"disp",resource:"dispTex1"},{name:"slope",resource:"slopeTex1"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:128}}]},{name:"c1.dispTex1.mips",type:"repeat",count:7,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[64,1,1],bindings:[{name:"src",resource:"dispTex1",levelAt:{index:"l"}},{name:"dst",resource:"dispTex1",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:128},dstWidth:{index:"l",offset:1,shr:128},pad:0,srcLevel:0}}]}]},{name:"c1.slopeTex1.mips",type:"repeat",count:7,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[64,1,1],bindings:[{name:"src",resource:"slopeTex1",levelAt:{index:"l"}},{name:"dst",resource:"slopeTex1",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:128},dstWidth:{index:"l",offset:1,shr:128},pad:0,srcLevel:0}}]}]},{name:"c2.spectrum",kernel:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",dispatch:[256,1,1],bindings:[{name:"h0",resource:"h0_2"},{name:"omega",resource:"omega_2"}],uniforms:[{name:"spec",values:{cascade:2,fetchKm:"param:fetch",gamma:"param:gamma",kMax:5.4,kMin:.8,loopPeriod:"param:loopPeriod",n:128,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:37,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c2.evolve",kernel:"ocean_evolve",dispatch:[256,1,1],bindings:[{name:"h0",resource:"h0_2"},{name:"omega",resource:"omega_2"},{name:"outA",resource:"fftA_2"},{name:"outB",resource:"fftB_2"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:128,t:"param:phaseTime",tile:37}}]},{name:"c2.fftA.fft_rows",kernel:"fft_compute_128",dispatch:[128,1,1],bindings:[{name:"data",resource:"fftA_2"}]},{name:"c2.fftA.fft_cols",kernel:"fft_compute_128",entry:"fft_cols",dispatch:[128,1,1],bindings:[{name:"data",resource:"fftA_2"}]},{name:"c2.fftB.fft_rows",kernel:"fft_compute_128",dispatch:[128,1,1],bindings:[{name:"data",resource:"fftB_2"}]},{name:"c2.fftB.fft_cols",kernel:"fft_compute_128",entry:"fft_cols",dispatch:[128,1,1],bindings:[{name:"data",resource:"fftB_2"}]},{name:"c2.assemble",kernel:"ocean_assemble",dispatch:[256,1,1],bindings:[{name:"fieldA",resource:"fftA_2"},{name:"fieldB",resource:"fftB_2"},{name:"disp",resource:"dispTex2"},{name:"slope",resource:"slopeTex2"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:128}}]},{name:"c2.dispTex2.mips",type:"repeat",count:7,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[64,1,1],bindings:[{name:"src",resource:"dispTex2",levelAt:{index:"l"}},{name:"dst",resource:"dispTex2",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:128},dstWidth:{index:"l",offset:1,shr:128},pad:0,srcLevel:0}}]}]},{name:"c2.slopeTex2.mips",type:"repeat",count:7,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[64,1,1],bindings:[{name:"src",resource:"slopeTex2",levelAt:{index:"l"}},{name:"dst",resource:"slopeTex2",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:128},dstWidth:{index:"l",offset:1,shr:128},pad:0,srcLevel:0}}]}]},{name:"c3.spectrum",kernel:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",dispatch:[256,1,1],bindings:[{name:"h0",resource:"h0_3"},{name:"omega",resource:"omega_3"}],uniforms:[{name:"spec",values:{cascade:3,fetchKm:"param:fetch",gamma:"param:gamma",kMax:37,kMin:5.4,loopPeriod:"param:loopPeriod",n:128,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:5.3,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c3.evolve",kernel:"ocean_evolve",dispatch:[256,1,1],bindings:[{name:"h0",resource:"h0_3"},{name:"omega",resource:"omega_3"},{name:"outA",resource:"fftA_3"},{name:"outB",resource:"fftB_3"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:128,t:"param:phaseTime",tile:5.3}}]},{name:"c3.fftA.fft_rows",kernel:"fft_compute_128",dispatch:[128,1,1],bindings:[{name:"data",resource:"fftA_3"}]},{name:"c3.fftA.fft_cols",kernel:"fft_compute_128",entry:"fft_cols",dispatch:[128,1,1],bindings:[{name:"data",resource:"fftA_3"}]},{name:"c3.fftB.fft_rows",kernel:"fft_compute_128",dispatch:[128,1,1],bindings:[{name:"data",resource:"fftB_3"}]},{name:"c3.fftB.fft_cols",kernel:"fft_compute_128",entry:"fft_cols",dispatch:[128,1,1],bindings:[{name:"data",resource:"fftB_3"}]},{name:"c3.assemble",kernel:"ocean_assemble",dispatch:[256,1,1],bindings:[{name:"fieldA",resource:"fftA_3"},{name:"fieldB",resource:"fftB_3"},{name:"disp",resource:"dispTex3"},{name:"slope",resource:"slopeTex3"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:128}}]},{name:"c3.dispTex3.mips",type:"repeat",count:7,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[64,1,1],bindings:[{name:"src",resource:"dispTex3",levelAt:{index:"l"}},{name:"dst",resource:"dispTex3",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:128},dstWidth:{index:"l",offset:1,shr:128},pad:0,srcLevel:0}}]}]},{name:"c3.slopeTex3.mips",type:"repeat",count:7,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[64,1,1],bindings:[{name:"src",resource:"slopeTex3",levelAt:{index:"l"}},{name:"dst",resource:"slopeTex3",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:128},dstWidth:{index:"l",offset:1,shr:128},pad:0,srcLevel:0}}]}]},{name:"wake.source",kernel:"wake_source",when:"param:wakeEnabled",dispatch:[1024,1,1],bindings:[{name:"wakeIn",resource:"wakeA"},{name:"fftOut",resource:"wakeF"},{name:"cleanOut",resource:"wakeC"}],uniforms:[{name:"wake",values:{bowFoam:"param:wakeBowFoam",bowGain:"param:wakeBowGain",cell:"param:wakeCell",depth:"param:wakeDepth",diffuse:"param:wakeDiffuse",dt:"param:wakeDt",foamSpeed:5,fullSpeed:3,heading:"param:hullHeading",hullBeam:"param:hullBeam",hullLen:"param:hullLength",hullU:"param:wakeHullU",hullV:"param:wakeHullV",jitter:"param:wakeJitter",originX:"param:wakeOriginX",originZ:"param:wakeOriginZ",reset:"param:wakeReset",shiftX:"param:wakeShiftX",shiftZ:"param:wakeShiftZ",speed:"param:hullSpeed",srcBeam:"param:wakeSrcBeam",srcLength:"param:wakeSrcLength",tau:"param:wakeFoamTau",width:256}}]},{name:"wake.fft1.fft_rows",kernel:"fft_compute_256",when:"param:wakeEnabled",dispatch:[256,1,1],bindings:[{name:"data",resource:"wakeF"}]},{name:"wake.fft1.fft_cols",kernel:"fft_compute_256",entry:"fft_cols",when:"param:wakeEnabled",dispatch:[256,1,1],bindings:[{name:"data",resource:"wakeF"}]},{name:"wake.symbol",kernel:"wake_symbol",when:"param:wakeEnabled",dispatch:[1024,1,1],bindings:[{name:"fftIn",resource:"wakeF"},{name:"fftOut",resource:"wakeG"}],uniforms:[{name:"sym",values:{n:256,window:"param:wakeLength"}}]},{name:"wake.fft2.fft_rows",kernel:"fft_compute_256",when:"param:wakeEnabled",dispatch:[256,1,1],bindings:[{name:"data",resource:"wakeG"}]},{name:"wake.fft2.fft_cols",kernel:"fft_compute_256",entry:"fft_cols",when:"param:wakeEnabled",dispatch:[256,1,1],bindings:[{name:"data",resource:"wakeG"}]},{name:"wake.step",kernel:"wake_step",when:"param:wakeEnabled",dispatch:[1024,1,1],bindings:[{name:"qIn",resource:"wakeG"},{name:"cleanIn",resource:"wakeC"},{name:"stateOut",resource:"wakeB"},{name:"wakeTex",resource:"wakeTex"}],uniforms:[{name:"wstep",values:{alpha:"param:wakeAlpha",cell:"param:wakeCell",dt:"param:wakeDt",gravity:"param:wakeGEff",originX:"param:wakeOriginX",originZ:"param:wakeOriginZ",sponge:24,spongeRate:.8,width:256}}]},{name:"wake.wakeTex.mips",type:"repeat",when:"param:wakeEnabled",count:8,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[256,1,1],bindings:[{name:"src",resource:"wakeTex",levelAt:{index:"l"}},{name:"dst",resource:"wakeTex",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:256},dstWidth:{index:"l",offset:1,shr:256},pad:0,srcLevel:0}}]}]},{name:"foam",kernel:"ocean_foam",dispatch:[16384,1,1],bindings:[{name:"foamIn",resource:"foamA"},{name:"fieldB0",resource:"fftB_0"},{name:"fieldB1",resource:"fftB_1"},{name:"fieldB2",resource:"fftB_2"},{name:"fieldB3",resource:"fftB_3"},{name:"foamOut",resource:"foamB"},{name:"foamTex",resource:"foamTex"},{name:"wakeIn",resource:"wakeB"}],uniforms:[{name:"foam",values:{cascades:4,cell:.5,dt:"param:dt",gain:"param:foamGain",lambda:"param:lambda",n0:64,n1:128,n2:128,n3:128,omX0:"param:foamOriginMod0X",omX1:"param:foamOriginMod1X",omX2:"param:foamOriginMod2X",omX3:"param:foamOriginMod3X",omZ0:"param:foamOriginMod0Z",omZ1:"param:foamOriginMod1Z",omZ2:"param:foamOriginMod2Z",omZ3:"param:foamOriginMod3Z",originX:"param:foamOriginX",originZ:"param:foamOriginZ",shiftX:"param:foamShiftX",shiftZ:"param:foamShiftZ",size0:1600,size1:250,size2:37,size3:5.3,tau:"param:foamTau",threshold:"param:foamThreshold",wakeCell:"param:wakeCell",wakeGain:"param:wakeFoamGainEff",wakeOriginX:"param:wakeOriginModX",wakeOriginZ:"param:wakeOriginModZ",wakeRelX:"param:wakeRelX",wakeRelZ:"param:wakeRelZ",wakeWidth:256,width:1024}}]},{name:"foamTex.mips",type:"repeat",count:10,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[4096,1,1],bindings:[{name:"src",resource:"foamTex",levelAt:{index:"l"}},{name:"dst",resource:"foamTex",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:1024},dstWidth:{index:"l",offset:1,shr:1024},pad:0,srcLevel:0}}]}]},{name:"probe",kernel:"ocean_probe",dispatch:[1,1,1],bindings:[{name:"points",resource:"probePoints"},{name:"disp0",resource:"fftA_0"},{name:"disp1",resource:"fftA_1"},{name:"disp2",resource:"fftA_2"},{name:"disp3",resource:"fftA_3"},{name:"out",resource:"probeOut"}],uniforms:[{name:"probe",values:{cascades:4,count:"param:probeCount",lambda:"param:lambda",n0:64,n1:128,n2:128,n3:128,normalStep:.5,omX0:"param:probeOriginMod0X",omX1:"param:probeOriginMod1X",omX2:"param:probeOriginMod2X",omX3:"param:probeOriginMod3X",omZ0:"param:probeOriginMod0Z",omZ1:"param:probeOriginMod1Z",omZ2:"param:probeOriginMod2Z",omZ3:"param:probeOriginMod3Z",size0:1600,size1:250,size2:37,size3:5.3,t:"param:simTime"}}]}],frameSwaps:[["wakeA","wakeB"],["foamA","foamB"]],exports:{disp0:"dispTex0",disp1:"dispTex1",disp2:"dispTex2",disp3:"dispTex3",foam:"foamTex",probe:"probeOut",slope0:"slopeTex0",slope1:"slopeTex1",slope2:"slopeTex2",slope3:"slopeTex3",wake:"wakeTex"},paramsFrom:"webgpu/high"},"webgpu/low":{name:"ocean",backend:"webgpu",tier:"low",version:1,resources:[{name:"h0_0",kind:"buffer",elementType:"vec4<f32>",count:4096},{name:"omega_0",kind:"buffer",elementType:"f32",count:4096},{name:"fftA_0",kind:"buffer",elementType:"vec4<f32>",count:4096},{name:"fftB_0",kind:"buffer",elementType:"vec4<f32>",count:4096},{name:"dispTex0",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"slopeTex0",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"h0_1",kind:"buffer",elementType:"vec4<f32>",count:4096},{name:"omega_1",kind:"buffer",elementType:"f32",count:4096},{name:"fftA_1",kind:"buffer",elementType:"vec4<f32>",count:4096},{name:"fftB_1",kind:"buffer",elementType:"vec4<f32>",count:4096},{name:"dispTex1",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"slopeTex1",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"h0_2",kind:"buffer",elementType:"vec4<f32>",count:4096},{name:"omega_2",kind:"buffer",elementType:"f32",count:4096},{name:"fftA_2",kind:"buffer",elementType:"vec4<f32>",count:4096},{name:"fftB_2",kind:"buffer",elementType:"vec4<f32>",count:4096},{name:"dispTex2",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"slopeTex2",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"wakeA",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"wakeB",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"wakeF",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"wakeG",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"wakeC",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"wakeTex",kind:"texture",format:"rgba16float",width:256,height:256,mipLevels:9},{name:"foamA",kind:"buffer",elementType:"f32",count:1048576},{name:"foamB",kind:"buffer",elementType:"f32",count:1048576},{name:"foamTex",kind:"texture",format:"rgba16float",width:1024,height:1024,mipLevels:11},{name:"probePoints",kind:"buffer",elementType:"vec4<f32>",count:64,feed:"probePoints"},{name:"probeOut",kind:"buffer",elementType:"vec4<f32>",count:64}],passes:[{name:"c0.spectrum",kernel:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",dispatch:[64,1,1],bindings:[{name:"h0",resource:"h0_0"},{name:"omega",resource:"omega_0"}],uniforms:[{name:"spec",values:{cascade:0,fetchKm:"param:fetch",gamma:"param:gamma",kMax:.12,kMin:.003926990816987242,loopPeriod:"param:loopPeriod",n:64,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:1600,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c0.evolve",kernel:"ocean_evolve",dispatch:[64,1,1],bindings:[{name:"h0",resource:"h0_0"},{name:"omega",resource:"omega_0"},{name:"outA",resource:"fftA_0"},{name:"outB",resource:"fftB_0"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:64,t:"param:phaseTime",tile:1600}}]},{name:"c0.fftA.fft_rows",kernel:"fft_compute_64",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftA_0"}]},{name:"c0.fftA.fft_cols",kernel:"fft_compute_64",entry:"fft_cols",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftA_0"}]},{name:"c0.fftB.fft_rows",kernel:"fft_compute_64",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftB_0"}]},{name:"c0.fftB.fft_cols",kernel:"fft_compute_64",entry:"fft_cols",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftB_0"}]},{name:"c0.assemble",kernel:"ocean_assemble",dispatch:[64,1,1],bindings:[{name:"fieldA",resource:"fftA_0"},{name:"fieldB",resource:"fftB_0"},{name:"disp",resource:"dispTex0"},{name:"slope",resource:"slopeTex0"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:64}}]},{name:"c0.dispTex0.mips",type:"repeat",count:6,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[16,1,1],bindings:[{name:"src",resource:"dispTex0",levelAt:{index:"l"}},{name:"dst",resource:"dispTex0",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:64},dstWidth:{index:"l",offset:1,shr:64},pad:0,srcLevel:0}}]}]},{name:"c0.slopeTex0.mips",type:"repeat",count:6,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[16,1,1],bindings:[{name:"src",resource:"slopeTex0",levelAt:{index:"l"}},{name:"dst",resource:"slopeTex0",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:64},dstWidth:{index:"l",offset:1,shr:64},pad:0,srcLevel:0}}]}]},{name:"c1.spectrum",kernel:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",dispatch:[64,1,1],bindings:[{name:"h0",resource:"h0_1"},{name:"omega",resource:"omega_1"}],uniforms:[{name:"spec",values:{cascade:1,fetchKm:"param:fetch",gamma:"param:gamma",kMax:.8,kMin:.12,loopPeriod:"param:loopPeriod",n:64,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:250,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c1.evolve",kernel:"ocean_evolve",dispatch:[64,1,1],bindings:[{name:"h0",resource:"h0_1"},{name:"omega",resource:"omega_1"},{name:"outA",resource:"fftA_1"},{name:"outB",resource:"fftB_1"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:64,t:"param:phaseTime",tile:250}}]},{name:"c1.fftA.fft_rows",kernel:"fft_compute_64",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftA_1"}]},{name:"c1.fftA.fft_cols",kernel:"fft_compute_64",entry:"fft_cols",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftA_1"}]},{name:"c1.fftB.fft_rows",kernel:"fft_compute_64",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftB_1"}]},{name:"c1.fftB.fft_cols",kernel:"fft_compute_64",entry:"fft_cols",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftB_1"}]},{name:"c1.assemble",kernel:"ocean_assemble",dispatch:[64,1,1],bindings:[{name:"fieldA",resource:"fftA_1"},{name:"fieldB",resource:"fftB_1"},{name:"disp",resource:"dispTex1"},{name:"slope",resource:"slopeTex1"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:64}}]},{name:"c1.dispTex1.mips",type:"repeat",count:6,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[16,1,1],bindings:[{name:"src",resource:"dispTex1",levelAt:{index:"l"}},{name:"dst",resource:"dispTex1",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:64},dstWidth:{index:"l",offset:1,shr:64},pad:0,srcLevel:0}}]}]},{name:"c1.slopeTex1.mips",type:"repeat",count:6,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[16,1,1],bindings:[{name:"src",resource:"slopeTex1",levelAt:{index:"l"}},{name:"dst",resource:"slopeTex1",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:64},dstWidth:{index:"l",offset:1,shr:64},pad:0,srcLevel:0}}]}]},{name:"c2.spectrum",kernel:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",dispatch:[64,1,1],bindings:[{name:"h0",resource:"h0_2"},{name:"omega",resource:"omega_2"}],uniforms:[{name:"spec",values:{cascade:2,fetchKm:"param:fetch",gamma:"param:gamma",kMax:5.4,kMin:.8,loopPeriod:"param:loopPeriod",n:64,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:37,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c2.evolve",kernel:"ocean_evolve",dispatch:[64,1,1],bindings:[{name:"h0",resource:"h0_2"},{name:"omega",resource:"omega_2"},{name:"outA",resource:"fftA_2"},{name:"outB",resource:"fftB_2"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:64,t:"param:phaseTime",tile:37}}]},{name:"c2.fftA.fft_rows",kernel:"fft_compute_64",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftA_2"}]},{name:"c2.fftA.fft_cols",kernel:"fft_compute_64",entry:"fft_cols",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftA_2"}]},{name:"c2.fftB.fft_rows",kernel:"fft_compute_64",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftB_2"}]},{name:"c2.fftB.fft_cols",kernel:"fft_compute_64",entry:"fft_cols",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftB_2"}]},{name:"c2.assemble",kernel:"ocean_assemble",dispatch:[64,1,1],bindings:[{name:"fieldA",resource:"fftA_2"},{name:"fieldB",resource:"fftB_2"},{name:"disp",resource:"dispTex2"},{name:"slope",resource:"slopeTex2"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:64}}]},{name:"c2.dispTex2.mips",type:"repeat",count:6,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[16,1,1],bindings:[{name:"src",resource:"dispTex2",levelAt:{index:"l"}},{name:"dst",resource:"dispTex2",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:64},dstWidth:{index:"l",offset:1,shr:64},pad:0,srcLevel:0}}]}]},{name:"c2.slopeTex2.mips",type:"repeat",count:6,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[16,1,1],bindings:[{name:"src",resource:"slopeTex2",levelAt:{index:"l"}},{name:"dst",resource:"slopeTex2",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:64},dstWidth:{index:"l",offset:1,shr:64},pad:0,srcLevel:0}}]}]},{name:"wake.source",kernel:"wake_source",when:"param:wakeEnabled",dispatch:[1024,1,1],bindings:[{name:"wakeIn",resource:"wakeA"},{name:"fftOut",resource:"wakeF"},{name:"cleanOut",resource:"wakeC"}],uniforms:[{name:"wake",values:{bowFoam:"param:wakeBowFoam",bowGain:"param:wakeBowGain",cell:"param:wakeCell",depth:"param:wakeDepth",diffuse:"param:wakeDiffuse",dt:"param:wakeDt",foamSpeed:5,fullSpeed:3,heading:"param:hullHeading",hullBeam:"param:hullBeam",hullLen:"param:hullLength",hullU:"param:wakeHullU",hullV:"param:wakeHullV",jitter:"param:wakeJitter",originX:"param:wakeOriginX",originZ:"param:wakeOriginZ",reset:"param:wakeReset",shiftX:"param:wakeShiftX",shiftZ:"param:wakeShiftZ",speed:"param:hullSpeed",srcBeam:"param:wakeSrcBeam",srcLength:"param:wakeSrcLength",tau:"param:wakeFoamTau",width:256}}]},{name:"wake.fft1.fft_rows",kernel:"fft_compute_256",when:"param:wakeEnabled",dispatch:[256,1,1],bindings:[{name:"data",resource:"wakeF"}]},{name:"wake.fft1.fft_cols",kernel:"fft_compute_256",entry:"fft_cols",when:"param:wakeEnabled",dispatch:[256,1,1],bindings:[{name:"data",resource:"wakeF"}]},{name:"wake.symbol",kernel:"wake_symbol",when:"param:wakeEnabled",dispatch:[1024,1,1],bindings:[{name:"fftIn",resource:"wakeF"},{name:"fftOut",resource:"wakeG"}],uniforms:[{name:"sym",values:{n:256,window:"param:wakeLength"}}]},{name:"wake.fft2.fft_rows",kernel:"fft_compute_256",when:"param:wakeEnabled",dispatch:[256,1,1],bindings:[{name:"data",resource:"wakeG"}]},{name:"wake.fft2.fft_cols",kernel:"fft_compute_256",entry:"fft_cols",when:"param:wakeEnabled",dispatch:[256,1,1],bindings:[{name:"data",resource:"wakeG"}]},{name:"wake.step",kernel:"wake_step",when:"param:wakeEnabled",dispatch:[1024,1,1],bindings:[{name:"qIn",resource:"wakeG"},{name:"cleanIn",resource:"wakeC"},{name:"stateOut",resource:"wakeB"},{name:"wakeTex",resource:"wakeTex"}],uniforms:[{name:"wstep",values:{alpha:"param:wakeAlpha",cell:"param:wakeCell",dt:"param:wakeDt",gravity:"param:wakeGEff",originX:"param:wakeOriginX",originZ:"param:wakeOriginZ",sponge:24,spongeRate:.8,width:256}}]},{name:"wake.wakeTex.mips",type:"repeat",when:"param:wakeEnabled",count:8,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[256,1,1],bindings:[{name:"src",resource:"wakeTex",levelAt:{index:"l"}},{name:"dst",resource:"wakeTex",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:256},dstWidth:{index:"l",offset:1,shr:256},pad:0,srcLevel:0}}]}]},{name:"foam",kernel:"ocean_foam",dispatch:[16384,1,1],bindings:[{name:"foamIn",resource:"foamA"},{name:"fieldB0",resource:"fftB_0"},{name:"fieldB1",resource:"fftB_1"},{name:"fieldB2",resource:"fftB_2"},{name:"fieldB3",resource:"fftB_2"},{name:"foamOut",resource:"foamB"},{name:"foamTex",resource:"foamTex"},{name:"wakeIn",resource:"wakeB"}],uniforms:[{name:"foam",values:{cascades:3,cell:.5,dt:"param:dt",gain:"param:foamGain",lambda:"param:lambda",n0:64,n1:64,n2:64,n3:64,omX0:"param:foamOriginMod0X",omX1:"param:foamOriginMod1X",omX2:"param:foamOriginMod2X",omX3:"param:foamOriginMod2X",omZ0:"param:foamOriginMod0Z",omZ1:"param:foamOriginMod1Z",omZ2:"param:foamOriginMod2Z",omZ3:"param:foamOriginMod2Z",originX:"param:foamOriginX",originZ:"param:foamOriginZ",shiftX:"param:foamShiftX",shiftZ:"param:foamShiftZ",size0:1600,size1:250,size2:37,size3:37,tau:"param:foamTau",threshold:"param:foamThreshold",wakeCell:"param:wakeCell",wakeGain:"param:wakeFoamGainEff",wakeOriginX:"param:wakeOriginModX",wakeOriginZ:"param:wakeOriginModZ",wakeRelX:"param:wakeRelX",wakeRelZ:"param:wakeRelZ",wakeWidth:256,width:1024}}]},{name:"foamTex.mips",type:"repeat",count:10,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[4096,1,1],bindings:[{name:"src",resource:"foamTex",levelAt:{index:"l"}},{name:"dst",resource:"foamTex",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:1024},dstWidth:{index:"l",offset:1,shr:1024},pad:0,srcLevel:0}}]}]},{name:"probe",kernel:"ocean_probe",dispatch:[1,1,1],bindings:[{name:"points",resource:"probePoints"},{name:"disp0",resource:"fftA_0"},{name:"disp1",resource:"fftA_1"},{name:"disp2",resource:"fftA_2"},{name:"disp3",resource:"fftA_2"},{name:"out",resource:"probeOut"}],uniforms:[{name:"probe",values:{cascades:3,count:"param:probeCount",lambda:"param:lambda",n0:64,n1:64,n2:64,n3:64,normalStep:.5,omX0:"param:probeOriginMod0X",omX1:"param:probeOriginMod1X",omX2:"param:probeOriginMod2X",omX3:"param:probeOriginMod2X",omZ0:"param:probeOriginMod0Z",omZ1:"param:probeOriginMod1Z",omZ2:"param:probeOriginMod2Z",omZ3:"param:probeOriginMod2Z",size0:1600,size1:250,size2:37,size3:37,t:"param:simTime"}}]}],frameSwaps:[["wakeA","wakeB"],["foamA","foamB"]],exports:{disp0:"dispTex0",disp1:"dispTex1",disp2:"dispTex2",foam:"foamTex",probe:"probeOut",slope0:"slopeTex0",slope1:"slopeTex1",slope2:"slopeTex2",wake:"wakeTex"},paramsFrom:"webgpu/high"},"webgl2/high":{name:"ocean",backend:"webgl2",tier:"high",version:1,params:[{name:"windSpeed",type:"f32",source:{kind:"value",value:9},tween:20},{name:"fetch",type:"f32",source:{kind:"value",value:300},tween:20},{name:"gamma",type:"f32",source:{kind:"value",value:3.3},tween:20},{name:"spread",type:"f32",source:{kind:"value",value:8},tween:20},{name:"swell",type:"f32",source:{kind:"value",value:0},tween:20},{name:"windDirDeg",type:"f32",source:{kind:"value",value:0},tween:20},{name:"swellDirDeg",type:"f32",source:{kind:"value",value:0},tween:20},{name:"seed",type:"u32",source:{kind:"value",value:0}},{name:"loopPeriod",type:"f32",source:{kind:"value",value:1e3}},{name:"lambda",type:"f32",source:{kind:"value",value:1},tween:20},{name:"timeScale",type:"f32",source:{kind:"value",value:1}},{name:"driftX",type:"f32",source:{kind:"value",value:0}},{name:"driftZ",type:"f32",source:{kind:"value",value:0}},{name:"foamTau",type:"f32",source:{kind:"value",value:4}},{name:"foamThreshold",type:"f32",source:{kind:"value",value:.9}},{name:"foamGain",type:"f32",source:{kind:"value",value:2}},{name:"probeCount",type:"u32",source:{kind:"value",value:0}},{name:"dt",type:"f32",source:{kind:"frame",name:"dt"}},{name:"cameraX",type:"f32",source:{kind:"frame",name:"cameraX"}},{name:"cameraZ",type:"f32",source:{kind:"frame",name:"cameraZ"}},{name:"windDir",type:"f32",source:{args:[1.5707963267948966,{op:"mul",args:["param:windDirDeg",.017453292519943295]}],kind:"expr",op:"sub"}},{name:"swellDir",type:"f32",source:{args:[1.5707963267948966,{op:"mul",args:["param:swellDirDeg",.017453292519943295]}],kind:"expr",op:"sub"}},{name:"simTime",type:"f32",source:{kind:"accumulate",of:"param:timeScale"}},{name:"phaseTime",type:"f32",source:{args:["param:simTime","param:loopPeriod"],kind:"expr",op:"mod"}},{name:"waterX",type:"f32",source:{kind:"accumulate",of:"param:driftX"}},{name:"waterZ",type:"f32",source:{kind:"accumulate",of:"param:driftZ"}},{name:"waterModX0",type:"f32",source:{args:["param:waterX",1600],kind:"expr",op:"mod"}},{name:"waterModZ0",type:"f32",source:{args:["param:waterZ",1600],kind:"expr",op:"mod"}},{name:"waterModX1",type:"f32",source:{args:["param:waterX",250],kind:"expr",op:"mod"}},{name:"waterModZ1",type:"f32",source:{args:["param:waterZ",250],kind:"expr",op:"mod"}},{name:"waterModX2",type:"f32",source:{args:["param:waterX",37],kind:"expr",op:"mod"}},{name:"waterModZ2",type:"f32",source:{args:["param:waterZ",37],kind:"expr",op:"mod"}},{name:"waterModX3",type:"f32",source:{args:["param:waterX",5.3],kind:"expr",op:"mod"}},{name:"waterModZ3",type:"f32",source:{args:["param:waterZ",5.3],kind:"expr",op:"mod"}},{name:"foamOriginX",type:"i32",source:{args:[{op:"floor",args:[{op:"div",args:[{op:"sub",args:["param:cameraX","param:waterX"]},.5]}]},512],kind:"expr",op:"sub"}},{name:"foamShiftX",type:"i32",source:{kind:"delta",of:"param:foamOriginX"}},{name:"foamOriginMod0X",type:"f32",source:{args:[{op:"mul",args:["param:foamOriginX",.5]},1600],kind:"expr",op:"mod"}},{name:"foamOriginMod1X",type:"f32",source:{args:[{op:"mul",args:["param:foamOriginX",.5]},250],kind:"expr",op:"mod"}},{name:"foamOriginMod2X",type:"f32",source:{args:[{op:"mul",args:["param:foamOriginX",.5]},37],kind:"expr",op:"mod"}},{name:"foamOriginMod3X",type:"f32",source:{args:[{op:"mul",args:["param:foamOriginX",.5]},5.3],kind:"expr",op:"mod"}},{name:"foamOriginZ",type:"i32",source:{args:[{op:"floor",args:[{op:"div",args:[{op:"sub",args:["param:cameraZ","param:waterZ"]},.5]}]},512],kind:"expr",op:"sub"}},{name:"foamShiftZ",type:"i32",source:{kind:"delta",of:"param:foamOriginZ"}},{name:"foamOriginMod0Z",type:"f32",source:{args:[{op:"mul",args:["param:foamOriginZ",.5]},1600],kind:"expr",op:"mod"}},{name:"foamOriginMod1Z",type:"f32",source:{args:[{op:"mul",args:["param:foamOriginZ",.5]},250],kind:"expr",op:"mod"}},{name:"foamOriginMod2Z",type:"f32",source:{args:[{op:"mul",args:["param:foamOriginZ",.5]},37],kind:"expr",op:"mod"}},{name:"foamOriginMod3Z",type:"f32",source:{args:[{op:"mul",args:["param:foamOriginZ",.5]},5.3],kind:"expr",op:"mod"}},{name:"hullX",type:"f32",source:{kind:"value",value:0}},{name:"hullZ",type:"f32",source:{kind:"value",value:0}},{name:"hullHeading",type:"f32",source:{kind:"value",value:0}},{name:"hullSpeed",type:"f32",source:{kind:"value",value:0}},{name:"wakeEnabled",type:"bool",source:{kind:"value",value:0}},{name:"hullLength",type:"f32",source:{kind:"value",value:200}},{name:"hullBeam",type:"f32",source:{kind:"value",value:30}},{name:"wakeCell",type:"f32",source:{kind:"value",value:4}},{name:"wakeDepth",type:"f32",source:{kind:"value",value:.4}},{name:"wakeGravity",type:"f32",source:{kind:"value",value:9.81}},{name:"wakeAlpha",type:"f32",source:{kind:"value",value:.02}},{name:"wakeMinLambdaCells",type:"f32",source:{kind:"value",value:8}},{name:"wakeFoamGain",type:"f32",source:{kind:"value",value:1}},{name:"wakeFoamTau",type:"f32",source:{kind:"value",value:48}},{name:"wakeDiffuse",type:"f32",source:{kind:"value",value:1.5}},{name:"wakeJitter",type:"f32",source:{kind:"value",value:1}},{name:"wakeBowGain",type:"f32",source:{kind:"value",value:.2}},{name:"wakeBowFoam",type:"f32",source:{kind:"value",value:.9}},{name:"wakeOn",type:"f32",source:{args:["param:wakeEnabled",1],kind:"expr",op:"mul"}},{name:"wakeFoamGainEff",type:"f32",source:{args:["param:wakeEnabled","param:wakeFoamGain"],kind:"expr",op:"mul"}},{name:"wakeEdge",type:"i32",source:{kind:"delta",of:"param:wakeEnabled"}},{name:"wakeReset",type:"u32",source:{args:["param:wakeEdge",0],kind:"expr",op:"max"}},{name:"wakeLength",type:"f32",source:{args:["param:wakeCell",256],kind:"expr",op:"mul"}},{name:"hullWaterX",type:"f32",source:{args:["param:hullX","param:waterX"],kind:"expr",op:"sub"}},{name:"wakeOriginX",type:"i32",source:{args:[{op:"div",args:[{op:"sub",args:[{op:"sub",args:["param:hullWaterX",{op:"mul",args:[{op:"sin",args:["param:hullHeading"]},{op:"mul",args:["param:wakeCell",51.2]}]}]},{op:"mul",args:["param:wakeCell",128]}]},"param:wakeCell"]}],kind:"expr",op:"floor"}},{name:"wakeShiftX",type:"i32",source:{kind:"delta",of:"param:wakeOriginX"}},{name:"wakeHullU",type:"f32",source:{args:["param:hullWaterX",{op:"mul",args:["param:wakeOriginX","param:wakeCell"]}],kind:"expr",op:"sub"}},{name:"wakeOriginModX",type:"i32",source:{args:["param:wakeOriginX",256],kind:"expr",op:"mod"}},{name:"wakeRelX",type:"f32",source:{args:[{op:"mul",args:["param:foamOriginX",.5]},{op:"mul",args:["param:wakeOriginX","param:wakeCell"]}],kind:"expr",op:"sub"}},{name:"wakeCamRelX",type:"f32",source:{args:[{op:"add",args:["param:waterX",{op:"mul",args:["param:wakeOriginX","param:wakeCell"]}]},"param:cameraX"],kind:"expr",op:"sub"}},{name:"hullWaterZ",type:"f32",source:{args:["param:hullZ","param:waterZ"],kind:"expr",op:"sub"}},{name:"wakeOriginZ",type:"i32",source:{args:[{op:"div",args:[{op:"sub",args:[{op:"sub",args:["param:hullWaterZ",{op:"mul",args:[{op:"cos",args:["param:hullHeading"]},{op:"mul",args:["param:wakeCell",51.2]}]}]},{op:"mul",args:["param:wakeCell",128]}]},"param:wakeCell"]}],kind:"expr",op:"floor"}},{name:"wakeShiftZ",type:"i32",source:{kind:"delta",of:"param:wakeOriginZ"}},{name:"wakeHullV",type:"f32",source:{args:["param:hullWaterZ",{op:"mul",args:["param:wakeOriginZ","param:wakeCell"]}],kind:"expr",op:"sub"}},{name:"wakeOriginModZ",type:"i32",source:{args:["param:wakeOriginZ",256],kind:"expr",op:"mod"}},{name:"wakeRelZ",type:"f32",source:{args:[{op:"mul",args:["param:foamOriginZ",.5]},{op:"mul",args:["param:wakeOriginZ","param:wakeCell"]}],kind:"expr",op:"sub"}},{name:"wakeCamRelZ",type:"f32",source:{args:[{op:"add",args:["param:waterZ",{op:"mul",args:["param:wakeOriginZ","param:wakeCell"]}]},"param:cameraZ"],kind:"expr",op:"sub"}},{name:"wakeGEff",type:"f32",source:{args:[{op:"min",args:[{op:"min",args:["param:wakeGravity",{op:"mul",args:[{op:"mul",args:["param:hullSpeed","param:hullSpeed"]},{op:"div",args:[6.283185307179586,{op:"mul",args:["param:wakeMinLambdaCells","param:wakeCell"]}]}]}]},{op:"mul",args:["param:wakeCell",16.39001041697031]}]},.05],kind:"expr",op:"max"}},{name:"wakeDt",type:"f32",source:{args:[{op:"mul",args:["param:dt","param:timeScale"]},.2],kind:"expr",op:"min"}},{name:"wakeSrcLength",type:"f32",source:{args:[{op:"min",args:["param:hullLength",{op:"div",args:[{op:"mul",args:[{op:"mul",args:["param:hullSpeed","param:hullSpeed"]},3.141592653589793]},"param:wakeGEff"]}]},{op:"mul",args:["param:wakeCell",2]}],kind:"expr",op:"max"}},{name:"wakeSrcBeam",type:"f32",source:{args:[{op:"min",args:["param:hullBeam",{op:"mul",args:["param:wakeSrcLength",.67]}]},{op:"mul",args:["param:wakeCell",1.5]}],kind:"expr",op:"max"}}],resources:[{name:"h0_0",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"omega_0",kind:"buffer",elementType:"f32",count:4096,width:64},{name:"fftA_0",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"fftB_0",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"fftA2_0",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"fftB2_0",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"dispTex0",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"slopeTex0",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"h0_1",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"omega_1",kind:"buffer",elementType:"f32",count:65536,width:256},{name:"fftA_1",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"fftB_1",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"fftA2_1",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"fftB2_1",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"dispTex1",kind:"texture",format:"rgba16float",width:256,height:256,mipLevels:9},{name:"slopeTex1",kind:"texture",format:"rgba16float",width:256,height:256,mipLevels:9},{name:"h0_2",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"omega_2",kind:"buffer",elementType:"f32",count:65536,width:256},{name:"fftA_2",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"fftB_2",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"fftA2_2",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"fftB2_2",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"dispTex2",kind:"texture",format:"rgba16float",width:256,height:256,mipLevels:9},{name:"slopeTex2",kind:"texture",format:"rgba16float",width:256,height:256,mipLevels:9},{name:"h0_3",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"omega_3",kind:"buffer",elementType:"f32",count:65536,width:256},{name:"fftA_3",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"fftB_3",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"fftA2_3",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"fftB2_3",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"dispTex3",kind:"texture",format:"rgba16float",width:256,height:256,mipLevels:9},{name:"slopeTex3",kind:"texture",format:"rgba16float",width:256,height:256,mipLevels:9},{name:"wakeA",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"wakeB",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"wakeF",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"wakeG",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"wakeC",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"wakeTex",kind:"texture",format:"rgba16float",width:256,height:256,mipLevels:9},{name:"foamA",kind:"buffer",elementType:"f32",count:1048576,width:1024},{name:"foamB",kind:"buffer",elementType:"f32",count:1048576,width:1024},{name:"foamTex",kind:"texture",format:"rgba16float",width:1024,height:1024,mipLevels:11}],passes:[{name:"c0.spectrum",kernel:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",target:{width:64,height:64,elioWidth:64},bindings:[{name:"h0",resource:"h0_0"},{name:"omega",resource:"omega_0"}],uniforms:[{name:"spec",values:{cascade:0,fetchKm:"param:fetch",gamma:"param:gamma",kMax:.12,kMin:.003926990816987242,loopPeriod:"param:loopPeriod",n:64,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:1600,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c0.evolve",kernel:"ocean_evolve",target:{width:64,height:64,elioWidth:64},bindings:[{name:"h0",resource:"h0_0"},{name:"omega",resource:"omega_0"},{name:"outA",resource:"fftA_0"},{name:"outB",resource:"fftB_0"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:64,t:"param:phaseTime",tile:1600}}]},{name:"c0.fft.rows",type:"repeat",count:6,index:"s",swap:[["fftA_0","fftA2_0"],["fftB_0","fftB2_0"]],body:[{name:"A",kernel:"fft_gather_64",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftA_0"},{name:"dst",resource:"fftA2_0"}],uniforms:[{name:"fft",values:{axis:0,n:64,stage:"param:s"}}]},{name:"B",kernel:"fft_gather_64",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftB_0"},{name:"dst",resource:"fftB2_0"}],uniforms:[{name:"fft",values:{axis:0,n:64,stage:"param:s"}}]}]},{name:"c0.fft.cols",type:"repeat",count:6,index:"s",swap:[["fftA_0","fftA2_0"],["fftB_0","fftB2_0"]],body:[{name:"A",kernel:"fft_gather_64",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftA_0"},{name:"dst",resource:"fftA2_0"}],uniforms:[{name:"fft",values:{axis:1,n:64,stage:"param:s"}}]},{name:"B",kernel:"fft_gather_64",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftB_0"},{name:"dst",resource:"fftB2_0"}],uniforms:[{name:"fft",values:{axis:1,n:64,stage:"param:s"}}]}]},{name:"c0.assemble",kernel:"ocean_assemble",target:{width:64,height:64,elioWidth:64},bindings:[{name:"fieldA",resource:"fftA_0"},{name:"fieldB",resource:"fftB_0"},{name:"disp",resource:"dispTex0"},{name:"slope",resource:"slopeTex0"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:64}}]},{name:"c0.dispTex0.mips",type:"mipmap",resource:"dispTex0"},{name:"c0.slopeTex0.mips",type:"mipmap",resource:"slopeTex0"},{name:"c1.spectrum",kernel:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",target:{width:256,height:256,elioWidth:256},bindings:[{name:"h0",resource:"h0_1"},{name:"omega",resource:"omega_1"}],uniforms:[{name:"spec",values:{cascade:1,fetchKm:"param:fetch",gamma:"param:gamma",kMax:.8,kMin:.12,loopPeriod:"param:loopPeriod",n:256,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:250,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c1.evolve",kernel:"ocean_evolve",target:{width:256,height:256,elioWidth:256},bindings:[{name:"h0",resource:"h0_1"},{name:"omega",resource:"omega_1"},{name:"outA",resource:"fftA_1"},{name:"outB",resource:"fftB_1"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:256,t:"param:phaseTime",tile:250}}]},{name:"c1.fft.rows",type:"repeat",count:8,index:"s",swap:[["fftA_1","fftA2_1"],["fftB_1","fftB2_1"]],body:[{name:"A",kernel:"fft_gather_256",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"fftA_1"},{name:"dst",resource:"fftA2_1"}],uniforms:[{name:"fft",values:{axis:0,n:256,stage:"param:s"}}]},{name:"B",kernel:"fft_gather_256",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"fftB_1"},{name:"dst",resource:"fftB2_1"}],uniforms:[{name:"fft",values:{axis:0,n:256,stage:"param:s"}}]}]},{name:"c1.fft.cols",type:"repeat",count:8,index:"s",swap:[["fftA_1","fftA2_1"],["fftB_1","fftB2_1"]],body:[{name:"A",kernel:"fft_gather_256",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"fftA_1"},{name:"dst",resource:"fftA2_1"}],uniforms:[{name:"fft",values:{axis:1,n:256,stage:"param:s"}}]},{name:"B",kernel:"fft_gather_256",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"fftB_1"},{name:"dst",resource:"fftB2_1"}],uniforms:[{name:"fft",values:{axis:1,n:256,stage:"param:s"}}]}]},{name:"c1.assemble",kernel:"ocean_assemble",target:{width:256,height:256,elioWidth:256},bindings:[{name:"fieldA",resource:"fftA_1"},{name:"fieldB",resource:"fftB_1"},{name:"disp",resource:"dispTex1"},{name:"slope",resource:"slopeTex1"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:256}}]},{name:"c1.dispTex1.mips",type:"mipmap",resource:"dispTex1"},{name:"c1.slopeTex1.mips",type:"mipmap",resource:"slopeTex1"},{name:"c2.spectrum",kernel:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",target:{width:256,height:256,elioWidth:256},bindings:[{name:"h0",resource:"h0_2"},{name:"omega",resource:"omega_2"}],uniforms:[{name:"spec",values:{cascade:2,fetchKm:"param:fetch",gamma:"param:gamma",kMax:5.4,kMin:.8,loopPeriod:"param:loopPeriod",n:256,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:37,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c2.evolve",kernel:"ocean_evolve",target:{width:256,height:256,elioWidth:256},bindings:[{name:"h0",resource:"h0_2"},{name:"omega",resource:"omega_2"},{name:"outA",resource:"fftA_2"},{name:"outB",resource:"fftB_2"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:256,t:"param:phaseTime",tile:37}}]},{name:"c2.fft.rows",type:"repeat",count:8,index:"s",swap:[["fftA_2","fftA2_2"],["fftB_2","fftB2_2"]],body:[{name:"A",kernel:"fft_gather_256",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"fftA_2"},{name:"dst",resource:"fftA2_2"}],uniforms:[{name:"fft",values:{axis:0,n:256,stage:"param:s"}}]},{name:"B",kernel:"fft_gather_256",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"fftB_2"},{name:"dst",resource:"fftB2_2"}],uniforms:[{name:"fft",values:{axis:0,n:256,stage:"param:s"}}]}]},{name:"c2.fft.cols",type:"repeat",count:8,index:"s",swap:[["fftA_2","fftA2_2"],["fftB_2","fftB2_2"]],body:[{name:"A",kernel:"fft_gather_256",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"fftA_2"},{name:"dst",resource:"fftA2_2"}],uniforms:[{name:"fft",values:{axis:1,n:256,stage:"param:s"}}]},{name:"B",kernel:"fft_gather_256",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"fftB_2"},{name:"dst",resource:"fftB2_2"}],uniforms:[{name:"fft",values:{axis:1,n:256,stage:"param:s"}}]}]},{name:"c2.assemble",kernel:"ocean_assemble",target:{width:256,height:256,elioWidth:256},bindings:[{name:"fieldA",resource:"fftA_2"},{name:"fieldB",resource:"fftB_2"},{name:"disp",resource:"dispTex2"},{name:"slope",resource:"slopeTex2"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:256}}]},{name:"c2.dispTex2.mips",type:"mipmap",resource:"dispTex2"},{name:"c2.slopeTex2.mips",type:"mipmap",resource:"slopeTex2"},{name:"c3.spectrum",kernel:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",target:{width:256,height:256,elioWidth:256},bindings:[{name:"h0",resource:"h0_3"},{name:"omega",resource:"omega_3"}],uniforms:[{name:"spec",values:{cascade:3,fetchKm:"param:fetch",gamma:"param:gamma",kMax:37,kMin:5.4,loopPeriod:"param:loopPeriod",n:256,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:5.3,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c3.evolve",kernel:"ocean_evolve",target:{width:256,height:256,elioWidth:256},bindings:[{name:"h0",resource:"h0_3"},{name:"omega",resource:"omega_3"},{name:"outA",resource:"fftA_3"},{name:"outB",resource:"fftB_3"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:256,t:"param:phaseTime",tile:5.3}}]},{name:"c3.fft.rows",type:"repeat",count:8,index:"s",swap:[["fftA_3","fftA2_3"],["fftB_3","fftB2_3"]],body:[{name:"A",kernel:"fft_gather_256",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"fftA_3"},{name:"dst",resource:"fftA2_3"}],uniforms:[{name:"fft",values:{axis:0,n:256,stage:"param:s"}}]},{name:"B",kernel:"fft_gather_256",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"fftB_3"},{name:"dst",resource:"fftB2_3"}],uniforms:[{name:"fft",values:{axis:0,n:256,stage:"param:s"}}]}]},{name:"c3.fft.cols",type:"repeat",count:8,index:"s",swap:[["fftA_3","fftA2_3"],["fftB_3","fftB2_3"]],body:[{name:"A",kernel:"fft_gather_256",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"fftA_3"},{name:"dst",resource:"fftA2_3"}],uniforms:[{name:"fft",values:{axis:1,n:256,stage:"param:s"}}]},{name:"B",kernel:"fft_gather_256",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"fftB_3"},{name:"dst",resource:"fftB2_3"}],uniforms:[{name:"fft",values:{axis:1,n:256,stage:"param:s"}}]}]},{name:"c3.assemble",kernel:"ocean_assemble",target:{width:256,height:256,elioWidth:256},bindings:[{name:"fieldA",resource:"fftA_3"},{name:"fieldB",resource:"fftB_3"},{name:"disp",resource:"dispTex3"},{name:"slope",resource:"slopeTex3"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:256}}]},{name:"c3.dispTex3.mips",type:"mipmap",resource:"dispTex3"},{name:"c3.slopeTex3.mips",type:"mipmap",resource:"slopeTex3"},{name:"wake.source",kernel:"wake_source",when:"param:wakeEnabled",target:{width:256,height:256,elioWidth:256},bindings:[{name:"wakeIn",resource:"wakeA"},{name:"fftOut",resource:"wakeF"},{name:"cleanOut",resource:"wakeC"}],uniforms:[{name:"wake",values:{bowFoam:"param:wakeBowFoam",bowGain:"param:wakeBowGain",cell:"param:wakeCell",depth:"param:wakeDepth",diffuse:"param:wakeDiffuse",dt:"param:wakeDt",foamSpeed:5,fullSpeed:3,heading:"param:hullHeading",hullBeam:"param:hullBeam",hullLen:"param:hullLength",hullU:"param:wakeHullU",hullV:"param:wakeHullV",jitter:"param:wakeJitter",originX:"param:wakeOriginX",originZ:"param:wakeOriginZ",reset:"param:wakeReset",shiftX:"param:wakeShiftX",shiftZ:"param:wakeShiftZ",speed:"param:hullSpeed",srcBeam:"param:wakeSrcBeam",srcLength:"param:wakeSrcLength",tau:"param:wakeFoamTau",width:256}}]},{name:"wake.fft1.rows",type:"repeat",when:"param:wakeEnabled",count:8,index:"s",swap:[["wakeF","wakeG"]],body:[{name:"stage",kernel:"fft_gather_256",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"wakeF"},{name:"dst",resource:"wakeG"}],uniforms:[{name:"fft",values:{axis:0,n:256,stage:"param:s"}}]}]},{name:"wake.fft1.cols",type:"repeat",when:"param:wakeEnabled",count:8,index:"s",swap:[["wakeF","wakeG"]],body:[{name:"stage",kernel:"fft_gather_256",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"wakeF"},{name:"dst",resource:"wakeG"}],uniforms:[{name:"fft",values:{axis:1,n:256,stage:"param:s"}}]}]},{name:"wake.symbol",kernel:"wake_symbol",when:"param:wakeEnabled",target:{width:256,height:256,elioWidth:256},bindings:[{name:"fftIn",resource:"wakeF"},{name:"fftOut",resource:"wakeG"}],uniforms:[{name:"sym",values:{n:256,window:"param:wakeLength"}}]},{name:"wake.fft2.rows",type:"repeat",when:"param:wakeEnabled",count:8,index:"s",swap:[["wakeG","wakeF"]],body:[{name:"stage",kernel:"fft_gather_256",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"wakeG"},{name:"dst",resource:"wakeF"}],uniforms:[{name:"fft",values:{axis:0,n:256,stage:"param:s"}}]}]},{name:"wake.fft2.cols",type:"repeat",when:"param:wakeEnabled",count:8,index:"s",swap:[["wakeG","wakeF"]],body:[{name:"stage",kernel:"fft_gather_256",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"wakeG"},{name:"dst",resource:"wakeF"}],uniforms:[{name:"fft",values:{axis:1,n:256,stage:"param:s"}}]}]},{name:"wake.step",kernel:"wake_step",when:"param:wakeEnabled",target:{width:256,height:256,elioWidth:256},bindings:[{name:"qIn",resource:"wakeG"},{name:"cleanIn",resource:"wakeC"},{name:"stateOut",resource:"wakeB"},{name:"wakeTex",resource:"wakeTex"}],uniforms:[{name:"wstep",values:{alpha:"param:wakeAlpha",cell:"param:wakeCell",dt:"param:wakeDt",gravity:"param:wakeGEff",originX:"param:wakeOriginX",originZ:"param:wakeOriginZ",sponge:24,spongeRate:.8,width:256}}]},{name:"wake.wakeTex.mips",type:"mipmap",when:"param:wakeEnabled",resource:"wakeTex"},{name:"foam",kernel:"ocean_foam",target:{width:1024,height:1024,elioWidth:1024},bindings:[{name:"foamIn",resource:"foamA"},{name:"fieldB0",resource:"fftB_0"},{name:"fieldB1",resource:"fftB_1"},{name:"fieldB2",resource:"fftB_2"},{name:"fieldB3",resource:"fftB_3"},{name:"foamOut",resource:"foamB"},{name:"foamTex",resource:"foamTex"},{name:"wakeIn",resource:"wakeB"}],uniforms:[{name:"foam",values:{cascades:4,cell:.5,dt:"param:dt",gain:"param:foamGain",lambda:"param:lambda",n0:64,n1:256,n2:256,n3:256,omX0:"param:foamOriginMod0X",omX1:"param:foamOriginMod1X",omX2:"param:foamOriginMod2X",omX3:"param:foamOriginMod3X",omZ0:"param:foamOriginMod0Z",omZ1:"param:foamOriginMod1Z",omZ2:"param:foamOriginMod2Z",omZ3:"param:foamOriginMod3Z",originX:"param:foamOriginX",originZ:"param:foamOriginZ",shiftX:"param:foamShiftX",shiftZ:"param:foamShiftZ",size0:1600,size1:250,size2:37,size3:5.3,tau:"param:foamTau",threshold:"param:foamThreshold",wakeCell:"param:wakeCell",wakeGain:"param:wakeFoamGainEff",wakeOriginX:"param:wakeOriginModX",wakeOriginZ:"param:wakeOriginModZ",wakeRelX:"param:wakeRelX",wakeRelZ:"param:wakeRelZ",wakeWidth:256,width:1024}}]},{name:"foamTex.mips",type:"mipmap",resource:"foamTex"}],frameSwaps:[["wakeA","wakeB"],["foamA","foamB"]],exports:{disp0:"dispTex0",disp1:"dispTex1",disp2:"dispTex2",disp3:"dispTex3",foam:"foamTex",slope0:"slopeTex0",slope1:"slopeTex1",slope2:"slopeTex2",slope3:"slopeTex3",wake:"wakeTex"},exportParams:["waterModX0","waterModZ0","waterModX1","waterModZ1","waterModX2","waterModZ2","waterModX3","waterModZ3","wakeCamRelX","wakeCamRelZ","wakeOriginModX","wakeOriginModZ","wakeCell","wakeOn"]},"webgl2/medium":{name:"ocean",backend:"webgl2",tier:"medium",version:1,resources:[{name:"h0_0",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"omega_0",kind:"buffer",elementType:"f32",count:4096,width:64},{name:"fftA_0",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"fftB_0",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"fftA2_0",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"fftB2_0",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"dispTex0",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"slopeTex0",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"h0_1",kind:"buffer",elementType:"vec4<f32>",count:16384,width:128},{name:"omega_1",kind:"buffer",elementType:"f32",count:16384,width:128},{name:"fftA_1",kind:"buffer",elementType:"vec4<f32>",count:16384,width:128},{name:"fftB_1",kind:"buffer",elementType:"vec4<f32>",count:16384,width:128},{name:"fftA2_1",kind:"buffer",elementType:"vec4<f32>",count:16384,width:128},{name:"fftB2_1",kind:"buffer",elementType:"vec4<f32>",count:16384,width:128},{name:"dispTex1",kind:"texture",format:"rgba16float",width:128,height:128,mipLevels:8},{name:"slopeTex1",kind:"texture",format:"rgba16float",width:128,height:128,mipLevels:8},{name:"h0_2",kind:"buffer",elementType:"vec4<f32>",count:16384,width:128},{name:"omega_2",kind:"buffer",elementType:"f32",count:16384,width:128},{name:"fftA_2",kind:"buffer",elementType:"vec4<f32>",count:16384,width:128},{name:"fftB_2",kind:"buffer",elementType:"vec4<f32>",count:16384,width:128},{name:"fftA2_2",kind:"buffer",elementType:"vec4<f32>",count:16384,width:128},{name:"fftB2_2",kind:"buffer",elementType:"vec4<f32>",count:16384,width:128},{name:"dispTex2",kind:"texture",format:"rgba16float",width:128,height:128,mipLevels:8},{name:"slopeTex2",kind:"texture",format:"rgba16float",width:128,height:128,mipLevels:8},{name:"h0_3",kind:"buffer",elementType:"vec4<f32>",count:16384,width:128},{name:"omega_3",kind:"buffer",elementType:"f32",count:16384,width:128},{name:"fftA_3",kind:"buffer",elementType:"vec4<f32>",count:16384,width:128},{name:"fftB_3",kind:"buffer",elementType:"vec4<f32>",count:16384,width:128},{name:"fftA2_3",kind:"buffer",elementType:"vec4<f32>",count:16384,width:128},{name:"fftB2_3",kind:"buffer",elementType:"vec4<f32>",count:16384,width:128},{name:"dispTex3",kind:"texture",format:"rgba16float",width:128,height:128,mipLevels:8},{name:"slopeTex3",kind:"texture",format:"rgba16float",width:128,height:128,mipLevels:8},{name:"wakeA",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"wakeB",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"wakeF",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"wakeG",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"wakeC",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"wakeTex",kind:"texture",format:"rgba16float",width:256,height:256,mipLevels:9},{name:"foamA",kind:"buffer",elementType:"f32",count:1048576,width:1024},{name:"foamB",kind:"buffer",elementType:"f32",count:1048576,width:1024},{name:"foamTex",kind:"texture",format:"rgba16float",width:1024,height:1024,mipLevels:11}],passes:[{name:"c0.spectrum",kernel:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",target:{width:64,height:64,elioWidth:64},bindings:[{name:"h0",resource:"h0_0"},{name:"omega",resource:"omega_0"}],uniforms:[{name:"spec",values:{cascade:0,fetchKm:"param:fetch",gamma:"param:gamma",kMax:.12,kMin:.003926990816987242,loopPeriod:"param:loopPeriod",n:64,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:1600,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c0.evolve",kernel:"ocean_evolve",target:{width:64,height:64,elioWidth:64},bindings:[{name:"h0",resource:"h0_0"},{name:"omega",resource:"omega_0"},{name:"outA",resource:"fftA_0"},{name:"outB",resource:"fftB_0"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:64,t:"param:phaseTime",tile:1600}}]},{name:"c0.fft.rows",type:"repeat",count:6,index:"s",swap:[["fftA_0","fftA2_0"],["fftB_0","fftB2_0"]],body:[{name:"A",kernel:"fft_gather_64",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftA_0"},{name:"dst",resource:"fftA2_0"}],uniforms:[{name:"fft",values:{axis:0,n:64,stage:"param:s"}}]},{name:"B",kernel:"fft_gather_64",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftB_0"},{name:"dst",resource:"fftB2_0"}],uniforms:[{name:"fft",values:{axis:0,n:64,stage:"param:s"}}]}]},{name:"c0.fft.cols",type:"repeat",count:6,index:"s",swap:[["fftA_0","fftA2_0"],["fftB_0","fftB2_0"]],body:[{name:"A",kernel:"fft_gather_64",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftA_0"},{name:"dst",resource:"fftA2_0"}],uniforms:[{name:"fft",values:{axis:1,n:64,stage:"param:s"}}]},{name:"B",kernel:"fft_gather_64",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftB_0"},{name:"dst",resource:"fftB2_0"}],uniforms:[{name:"fft",values:{axis:1,n:64,stage:"param:s"}}]}]},{name:"c0.assemble",kernel:"ocean_assemble",target:{width:64,height:64,elioWidth:64},bindings:[{name:"fieldA",resource:"fftA_0"},{name:"fieldB",resource:"fftB_0"},{name:"disp",resource:"dispTex0"},{name:"slope",resource:"slopeTex0"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:64}}]},{name:"c0.dispTex0.mips",type:"mipmap",resource:"dispTex0"},{name:"c0.slopeTex0.mips",type:"mipmap",resource:"slopeTex0"},{name:"c1.spectrum",kernel:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",target:{width:128,height:128,elioWidth:128},bindings:[{name:"h0",resource:"h0_1"},{name:"omega",resource:"omega_1"}],uniforms:[{name:"spec",values:{cascade:1,fetchKm:"param:fetch",gamma:"param:gamma",kMax:.8,kMin:.12,loopPeriod:"param:loopPeriod",n:128,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:250,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c1.evolve",kernel:"ocean_evolve",target:{width:128,height:128,elioWidth:128},bindings:[{name:"h0",resource:"h0_1"},{name:"omega",resource:"omega_1"},{name:"outA",resource:"fftA_1"},{name:"outB",resource:"fftB_1"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:128,t:"param:phaseTime",tile:250}}]},{name:"c1.fft.rows",type:"repeat",count:7,index:"s",swap:[["fftA_1","fftA2_1"],["fftB_1","fftB2_1"]],body:[{name:"A",kernel:"fft_gather_128",target:{width:128,height:128,elioWidth:128},bindings:[{name:"src",resource:"fftA_1"},{name:"dst",resource:"fftA2_1"}],uniforms:[{name:"fft",values:{axis:0,n:128,stage:"param:s"}}]},{name:"B",kernel:"fft_gather_128",target:{width:128,height:128,elioWidth:128},bindings:[{name:"src",resource:"fftB_1"},{name:"dst",resource:"fftB2_1"}],uniforms:[{name:"fft",values:{axis:0,n:128,stage:"param:s"}}]}]},{name:"c1.fft.cols",type:"repeat",count:7,index:"s",swap:[["fftA_1","fftA2_1"],["fftB_1","fftB2_1"]],body:[{name:"A",kernel:"fft_gather_128",target:{width:128,height:128,elioWidth:128},bindings:[{name:"src",resource:"fftA_1"},{name:"dst",resource:"fftA2_1"}],uniforms:[{name:"fft",values:{axis:1,n:128,stage:"param:s"}}]},{name:"B",kernel:"fft_gather_128",target:{width:128,height:128,elioWidth:128},bindings:[{name:"src",resource:"fftB_1"},{name:"dst",resource:"fftB2_1"}],uniforms:[{name:"fft",values:{axis:1,n:128,stage:"param:s"}}]}]},{name:"c1.assemble",kernel:"ocean_assemble",target:{width:128,height:128,elioWidth:128},bindings:[{name:"fieldA",resource:"fftA_1"},{name:"fieldB",resource:"fftB_1"},{name:"disp",resource:"dispTex1"},{name:"slope",resource:"slopeTex1"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:128}}]},{name:"c1.dispTex1.mips",type:"mipmap",resource:"dispTex1"},{name:"c1.slopeTex1.mips",type:"mipmap",resource:"slopeTex1"},{name:"c2.spectrum",kernel:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",target:{width:128,height:128,elioWidth:128},bindings:[{name:"h0",resource:"h0_2"},{name:"omega",resource:"omega_2"}],uniforms:[{name:"spec",values:{cascade:2,fetchKm:"param:fetch",gamma:"param:gamma",kMax:5.4,kMin:.8,loopPeriod:"param:loopPeriod",n:128,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:37,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c2.evolve",kernel:"ocean_evolve",target:{width:128,height:128,elioWidth:128},bindings:[{name:"h0",resource:"h0_2"},{name:"omega",resource:"omega_2"},{name:"outA",resource:"fftA_2"},{name:"outB",resource:"fftB_2"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:128,t:"param:phaseTime",tile:37}}]},{name:"c2.fft.rows",type:"repeat",count:7,index:"s",swap:[["fftA_2","fftA2_2"],["fftB_2","fftB2_2"]],body:[{name:"A",kernel:"fft_gather_128",target:{width:128,height:128,elioWidth:128},bindings:[{name:"src",resource:"fftA_2"},{name:"dst",resource:"fftA2_2"}],uniforms:[{name:"fft",values:{axis:0,n:128,stage:"param:s"}}]},{name:"B",kernel:"fft_gather_128",target:{width:128,height:128,elioWidth:128},bindings:[{name:"src",resource:"fftB_2"},{name:"dst",resource:"fftB2_2"}],uniforms:[{name:"fft",values:{axis:0,n:128,stage:"param:s"}}]}]},{name:"c2.fft.cols",type:"repeat",count:7,index:"s",swap:[["fftA_2","fftA2_2"],["fftB_2","fftB2_2"]],body:[{name:"A",kernel:"fft_gather_128",target:{width:128,height:128,elioWidth:128},bindings:[{name:"src",resource:"fftA_2"},{name:"dst",resource:"fftA2_2"}],uniforms:[{name:"fft",values:{axis:1,n:128,stage:"param:s"}}]},{name:"B",kernel:"fft_gather_128",target:{width:128,height:128,elioWidth:128},bindings:[{name:"src",resource:"fftB_2"},{name:"dst",resource:"fftB2_2"}],uniforms:[{name:"fft",values:{axis:1,n:128,stage:"param:s"}}]}]},{name:"c2.assemble",kernel:"ocean_assemble",target:{width:128,height:128,elioWidth:128},bindings:[{name:"fieldA",resource:"fftA_2"},{name:"fieldB",resource:"fftB_2"},{name:"disp",resource:"dispTex2"},{name:"slope",resource:"slopeTex2"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:128}}]},{name:"c2.dispTex2.mips",type:"mipmap",resource:"dispTex2"},{name:"c2.slopeTex2.mips",type:"mipmap",resource:"slopeTex2"},{name:"c3.spectrum",kernel:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",target:{width:128,height:128,elioWidth:128},bindings:[{name:"h0",resource:"h0_3"},{name:"omega",resource:"omega_3"}],uniforms:[{name:"spec",values:{cascade:3,fetchKm:"param:fetch",gamma:"param:gamma",kMax:37,kMin:5.4,loopPeriod:"param:loopPeriod",n:128,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:5.3,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c3.evolve",kernel:"ocean_evolve",target:{width:128,height:128,elioWidth:128},bindings:[{name:"h0",resource:"h0_3"},{name:"omega",resource:"omega_3"},{name:"outA",resource:"fftA_3"},{name:"outB",resource:"fftB_3"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:128,t:"param:phaseTime",tile:5.3}}]},{name:"c3.fft.rows",type:"repeat",count:7,index:"s",swap:[["fftA_3","fftA2_3"],["fftB_3","fftB2_3"]],body:[{name:"A",kernel:"fft_gather_128",target:{width:128,height:128,elioWidth:128},bindings:[{name:"src",resource:"fftA_3"},{name:"dst",resource:"fftA2_3"}],uniforms:[{name:"fft",values:{axis:0,n:128,stage:"param:s"}}]},{name:"B",kernel:"fft_gather_128",target:{width:128,height:128,elioWidth:128},bindings:[{name:"src",resource:"fftB_3"},{name:"dst",resource:"fftB2_3"}],uniforms:[{name:"fft",values:{axis:0,n:128,stage:"param:s"}}]}]},{name:"c3.fft.cols",type:"repeat",count:7,index:"s",swap:[["fftA_3","fftA2_3"],["fftB_3","fftB2_3"]],body:[{name:"A",kernel:"fft_gather_128",target:{width:128,height:128,elioWidth:128},bindings:[{name:"src",resource:"fftA_3"},{name:"dst",resource:"fftA2_3"}],uniforms:[{name:"fft",values:{axis:1,n:128,stage:"param:s"}}]},{name:"B",kernel:"fft_gather_128",target:{width:128,height:128,elioWidth:128},bindings:[{name:"src",resource:"fftB_3"},{name:"dst",resource:"fftB2_3"}],uniforms:[{name:"fft",values:{axis:1,n:128,stage:"param:s"}}]}]},{name:"c3.assemble",kernel:"ocean_assemble",target:{width:128,height:128,elioWidth:128},bindings:[{name:"fieldA",resource:"fftA_3"},{name:"fieldB",resource:"fftB_3"},{name:"disp",resource:"dispTex3"},{name:"slope",resource:"slopeTex3"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:128}}]},{name:"c3.dispTex3.mips",type:"mipmap",resource:"dispTex3"},{name:"c3.slopeTex3.mips",type:"mipmap",resource:"slopeTex3"},{name:"wake.source",kernel:"wake_source",when:"param:wakeEnabled",target:{width:256,height:256,elioWidth:256},bindings:[{name:"wakeIn",resource:"wakeA"},{name:"fftOut",resource:"wakeF"},{name:"cleanOut",resource:"wakeC"}],uniforms:[{name:"wake",values:{bowFoam:"param:wakeBowFoam",bowGain:"param:wakeBowGain",cell:"param:wakeCell",depth:"param:wakeDepth",diffuse:"param:wakeDiffuse",dt:"param:wakeDt",foamSpeed:5,fullSpeed:3,heading:"param:hullHeading",hullBeam:"param:hullBeam",hullLen:"param:hullLength",hullU:"param:wakeHullU",hullV:"param:wakeHullV",jitter:"param:wakeJitter",originX:"param:wakeOriginX",originZ:"param:wakeOriginZ",reset:"param:wakeReset",shiftX:"param:wakeShiftX",shiftZ:"param:wakeShiftZ",speed:"param:hullSpeed",srcBeam:"param:wakeSrcBeam",srcLength:"param:wakeSrcLength",tau:"param:wakeFoamTau",width:256}}]},{name:"wake.fft1.rows",type:"repeat",when:"param:wakeEnabled",count:8,index:"s",swap:[["wakeF","wakeG"]],body:[{name:"stage",kernel:"fft_gather_256",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"wakeF"},{name:"dst",resource:"wakeG"}],uniforms:[{name:"fft",values:{axis:0,n:256,stage:"param:s"}}]}]},{name:"wake.fft1.cols",type:"repeat",when:"param:wakeEnabled",count:8,index:"s",swap:[["wakeF","wakeG"]],body:[{name:"stage",kernel:"fft_gather_256",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"wakeF"},{name:"dst",resource:"wakeG"}],uniforms:[{name:"fft",values:{axis:1,n:256,stage:"param:s"}}]}]},{name:"wake.symbol",kernel:"wake_symbol",when:"param:wakeEnabled",target:{width:256,height:256,elioWidth:256},bindings:[{name:"fftIn",resource:"wakeF"},{name:"fftOut",resource:"wakeG"}],uniforms:[{name:"sym",values:{n:256,window:"param:wakeLength"}}]},{name:"wake.fft2.rows",type:"repeat",when:"param:wakeEnabled",count:8,index:"s",swap:[["wakeG","wakeF"]],body:[{name:"stage",kernel:"fft_gather_256",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"wakeG"},{name:"dst",resource:"wakeF"}],uniforms:[{name:"fft",values:{axis:0,n:256,stage:"param:s"}}]}]},{name:"wake.fft2.cols",type:"repeat",when:"param:wakeEnabled",count:8,index:"s",swap:[["wakeG","wakeF"]],body:[{name:"stage",kernel:"fft_gather_256",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"wakeG"},{name:"dst",resource:"wakeF"}],uniforms:[{name:"fft",values:{axis:1,n:256,stage:"param:s"}}]}]},{name:"wake.step",kernel:"wake_step",when:"param:wakeEnabled",target:{width:256,height:256,elioWidth:256},bindings:[{name:"qIn",resource:"wakeG"},{name:"cleanIn",resource:"wakeC"},{name:"stateOut",resource:"wakeB"},{name:"wakeTex",resource:"wakeTex"}],uniforms:[{name:"wstep",values:{alpha:"param:wakeAlpha",cell:"param:wakeCell",dt:"param:wakeDt",gravity:"param:wakeGEff",originX:"param:wakeOriginX",originZ:"param:wakeOriginZ",sponge:24,spongeRate:.8,width:256}}]},{name:"wake.wakeTex.mips",type:"mipmap",when:"param:wakeEnabled",resource:"wakeTex"},{name:"foam",kernel:"ocean_foam",target:{width:1024,height:1024,elioWidth:1024},bindings:[{name:"foamIn",resource:"foamA"},{name:"fieldB0",resource:"fftB_0"},{name:"fieldB1",resource:"fftB_1"},{name:"fieldB2",resource:"fftB_2"},{name:"fieldB3",resource:"fftB_3"},{name:"foamOut",resource:"foamB"},{name:"foamTex",resource:"foamTex"},{name:"wakeIn",resource:"wakeB"}],uniforms:[{name:"foam",values:{cascades:4,cell:.5,dt:"param:dt",gain:"param:foamGain",lambda:"param:lambda",n0:64,n1:128,n2:128,n3:128,omX0:"param:foamOriginMod0X",omX1:"param:foamOriginMod1X",omX2:"param:foamOriginMod2X",omX3:"param:foamOriginMod3X",omZ0:"param:foamOriginMod0Z",omZ1:"param:foamOriginMod1Z",omZ2:"param:foamOriginMod2Z",omZ3:"param:foamOriginMod3Z",originX:"param:foamOriginX",originZ:"param:foamOriginZ",shiftX:"param:foamShiftX",shiftZ:"param:foamShiftZ",size0:1600,size1:250,size2:37,size3:5.3,tau:"param:foamTau",threshold:"param:foamThreshold",wakeCell:"param:wakeCell",wakeGain:"param:wakeFoamGainEff",wakeOriginX:"param:wakeOriginModX",wakeOriginZ:"param:wakeOriginModZ",wakeRelX:"param:wakeRelX",wakeRelZ:"param:wakeRelZ",wakeWidth:256,width:1024}}]},{name:"foamTex.mips",type:"mipmap",resource:"foamTex"}],frameSwaps:[["wakeA","wakeB"],["foamA","foamB"]],exports:{disp0:"dispTex0",disp1:"dispTex1",disp2:"dispTex2",disp3:"dispTex3",foam:"foamTex",slope0:"slopeTex0",slope1:"slopeTex1",slope2:"slopeTex2",slope3:"slopeTex3",wake:"wakeTex"},paramsFrom:"webgl2/high"},"webgl2/low":{name:"ocean",backend:"webgl2",tier:"low",version:1,resources:[{name:"h0_0",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"omega_0",kind:"buffer",elementType:"f32",count:4096,width:64},{name:"fftA_0",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"fftB_0",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"fftA2_0",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"fftB2_0",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"dispTex0",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"slopeTex0",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"h0_1",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"omega_1",kind:"buffer",elementType:"f32",count:4096,width:64},{name:"fftA_1",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"fftB_1",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"fftA2_1",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"fftB2_1",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"dispTex1",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"slopeTex1",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"h0_2",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"omega_2",kind:"buffer",elementType:"f32",count:4096,width:64},{name:"fftA_2",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"fftB_2",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"fftA2_2",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"fftB2_2",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"dispTex2",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"slopeTex2",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"wakeA",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"wakeB",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"wakeF",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"wakeG",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"wakeC",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"wakeTex",kind:"texture",format:"rgba16float",width:256,height:256,mipLevels:9},{name:"foamA",kind:"buffer",elementType:"f32",count:1048576,width:1024},{name:"foamB",kind:"buffer",elementType:"f32",count:1048576,width:1024},{name:"foamTex",kind:"texture",format:"rgba16float",width:1024,height:1024,mipLevels:11}],passes:[{name:"c0.spectrum",kernel:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",target:{width:64,height:64,elioWidth:64},bindings:[{name:"h0",resource:"h0_0"},{name:"omega",resource:"omega_0"}],uniforms:[{name:"spec",values:{cascade:0,fetchKm:"param:fetch",gamma:"param:gamma",kMax:.12,kMin:.003926990816987242,loopPeriod:"param:loopPeriod",n:64,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:1600,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c0.evolve",kernel:"ocean_evolve",target:{width:64,height:64,elioWidth:64},bindings:[{name:"h0",resource:"h0_0"},{name:"omega",resource:"omega_0"},{name:"outA",resource:"fftA_0"},{name:"outB",resource:"fftB_0"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:64,t:"param:phaseTime",tile:1600}}]},{name:"c0.fft.rows",type:"repeat",count:6,index:"s",swap:[["fftA_0","fftA2_0"],["fftB_0","fftB2_0"]],body:[{name:"A",kernel:"fft_gather_64",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftA_0"},{name:"dst",resource:"fftA2_0"}],uniforms:[{name:"fft",values:{axis:0,n:64,stage:"param:s"}}]},{name:"B",kernel:"fft_gather_64",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftB_0"},{name:"dst",resource:"fftB2_0"}],uniforms:[{name:"fft",values:{axis:0,n:64,stage:"param:s"}}]}]},{name:"c0.fft.cols",type:"repeat",count:6,index:"s",swap:[["fftA_0","fftA2_0"],["fftB_0","fftB2_0"]],body:[{name:"A",kernel:"fft_gather_64",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftA_0"},{name:"dst",resource:"fftA2_0"}],uniforms:[{name:"fft",values:{axis:1,n:64,stage:"param:s"}}]},{name:"B",kernel:"fft_gather_64",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftB_0"},{name:"dst",resource:"fftB2_0"}],uniforms:[{name:"fft",values:{axis:1,n:64,stage:"param:s"}}]}]},{name:"c0.assemble",kernel:"ocean_assemble",target:{width:64,height:64,elioWidth:64},bindings:[{name:"fieldA",resource:"fftA_0"},{name:"fieldB",resource:"fftB_0"},{name:"disp",resource:"dispTex0"},{name:"slope",resource:"slopeTex0"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:64}}]},{name:"c0.dispTex0.mips",type:"mipmap",resource:"dispTex0"},{name:"c0.slopeTex0.mips",type:"mipmap",resource:"slopeTex0"},{name:"c1.spectrum",kernel:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",target:{width:64,height:64,elioWidth:64},bindings:[{name:"h0",resource:"h0_1"},{name:"omega",resource:"omega_1"}],uniforms:[{name:"spec",values:{cascade:1,fetchKm:"param:fetch",gamma:"param:gamma",kMax:.8,kMin:.12,loopPeriod:"param:loopPeriod",n:64,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:250,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c1.evolve",kernel:"ocean_evolve",target:{width:64,height:64,elioWidth:64},bindings:[{name:"h0",resource:"h0_1"},{name:"omega",resource:"omega_1"},{name:"outA",resource:"fftA_1"},{name:"outB",resource:"fftB_1"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:64,t:"param:phaseTime",tile:250}}]},{name:"c1.fft.rows",type:"repeat",count:6,index:"s",swap:[["fftA_1","fftA2_1"],["fftB_1","fftB2_1"]],body:[{name:"A",kernel:"fft_gather_64",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftA_1"},{name:"dst",resource:"fftA2_1"}],uniforms:[{name:"fft",values:{axis:0,n:64,stage:"param:s"}}]},{name:"B",kernel:"fft_gather_64",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftB_1"},{name:"dst",resource:"fftB2_1"}],uniforms:[{name:"fft",values:{axis:0,n:64,stage:"param:s"}}]}]},{name:"c1.fft.cols",type:"repeat",count:6,index:"s",swap:[["fftA_1","fftA2_1"],["fftB_1","fftB2_1"]],body:[{name:"A",kernel:"fft_gather_64",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftA_1"},{name:"dst",resource:"fftA2_1"}],uniforms:[{name:"fft",values:{axis:1,n:64,stage:"param:s"}}]},{name:"B",kernel:"fft_gather_64",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftB_1"},{name:"dst",resource:"fftB2_1"}],uniforms:[{name:"fft",values:{axis:1,n:64,stage:"param:s"}}]}]},{name:"c1.assemble",kernel:"ocean_assemble",target:{width:64,height:64,elioWidth:64},bindings:[{name:"fieldA",resource:"fftA_1"},{name:"fieldB",resource:"fftB_1"},{name:"disp",resource:"dispTex1"},{name:"slope",resource:"slopeTex1"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:64}}]},{name:"c1.dispTex1.mips",type:"mipmap",resource:"dispTex1"},{name:"c1.slopeTex1.mips",type:"mipmap",resource:"slopeTex1"},{name:"c2.spectrum",kernel:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",target:{width:64,height:64,elioWidth:64},bindings:[{name:"h0",resource:"h0_2"},{name:"omega",resource:"omega_2"}],uniforms:[{name:"spec",values:{cascade:2,fetchKm:"param:fetch",gamma:"param:gamma",kMax:5.4,kMin:.8,loopPeriod:"param:loopPeriod",n:64,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:37,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c2.evolve",kernel:"ocean_evolve",target:{width:64,height:64,elioWidth:64},bindings:[{name:"h0",resource:"h0_2"},{name:"omega",resource:"omega_2"},{name:"outA",resource:"fftA_2"},{name:"outB",resource:"fftB_2"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:64,t:"param:phaseTime",tile:37}}]},{name:"c2.fft.rows",type:"repeat",count:6,index:"s",swap:[["fftA_2","fftA2_2"],["fftB_2","fftB2_2"]],body:[{name:"A",kernel:"fft_gather_64",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftA_2"},{name:"dst",resource:"fftA2_2"}],uniforms:[{name:"fft",values:{axis:0,n:64,stage:"param:s"}}]},{name:"B",kernel:"fft_gather_64",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftB_2"},{name:"dst",resource:"fftB2_2"}],uniforms:[{name:"fft",values:{axis:0,n:64,stage:"param:s"}}]}]},{name:"c2.fft.cols",type:"repeat",count:6,index:"s",swap:[["fftA_2","fftA2_2"],["fftB_2","fftB2_2"]],body:[{name:"A",kernel:"fft_gather_64",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftA_2"},{name:"dst",resource:"fftA2_2"}],uniforms:[{name:"fft",values:{axis:1,n:64,stage:"param:s"}}]},{name:"B",kernel:"fft_gather_64",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftB_2"},{name:"dst",resource:"fftB2_2"}],uniforms:[{name:"fft",values:{axis:1,n:64,stage:"param:s"}}]}]},{name:"c2.assemble",kernel:"ocean_assemble",target:{width:64,height:64,elioWidth:64},bindings:[{name:"fieldA",resource:"fftA_2"},{name:"fieldB",resource:"fftB_2"},{name:"disp",resource:"dispTex2"},{name:"slope",resource:"slopeTex2"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:64}}]},{name:"c2.dispTex2.mips",type:"mipmap",resource:"dispTex2"},{name:"c2.slopeTex2.mips",type:"mipmap",resource:"slopeTex2"},{name:"wake.source",kernel:"wake_source",when:"param:wakeEnabled",target:{width:256,height:256,elioWidth:256},bindings:[{name:"wakeIn",resource:"wakeA"},{name:"fftOut",resource:"wakeF"},{name:"cleanOut",resource:"wakeC"}],uniforms:[{name:"wake",values:{bowFoam:"param:wakeBowFoam",bowGain:"param:wakeBowGain",cell:"param:wakeCell",depth:"param:wakeDepth",diffuse:"param:wakeDiffuse",dt:"param:wakeDt",foamSpeed:5,fullSpeed:3,heading:"param:hullHeading",hullBeam:"param:hullBeam",hullLen:"param:hullLength",hullU:"param:wakeHullU",hullV:"param:wakeHullV",jitter:"param:wakeJitter",originX:"param:wakeOriginX",originZ:"param:wakeOriginZ",reset:"param:wakeReset",shiftX:"param:wakeShiftX",shiftZ:"param:wakeShiftZ",speed:"param:hullSpeed",srcBeam:"param:wakeSrcBeam",srcLength:"param:wakeSrcLength",tau:"param:wakeFoamTau",width:256}}]},{name:"wake.fft1.rows",type:"repeat",when:"param:wakeEnabled",count:8,index:"s",swap:[["wakeF","wakeG"]],body:[{name:"stage",kernel:"fft_gather_256",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"wakeF"},{name:"dst",resource:"wakeG"}],uniforms:[{name:"fft",values:{axis:0,n:256,stage:"param:s"}}]}]},{name:"wake.fft1.cols",type:"repeat",when:"param:wakeEnabled",count:8,index:"s",swap:[["wakeF","wakeG"]],body:[{name:"stage",kernel:"fft_gather_256",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"wakeF"},{name:"dst",resource:"wakeG"}],uniforms:[{name:"fft",values:{axis:1,n:256,stage:"param:s"}}]}]},{name:"wake.symbol",kernel:"wake_symbol",when:"param:wakeEnabled",target:{width:256,height:256,elioWidth:256},bindings:[{name:"fftIn",resource:"wakeF"},{name:"fftOut",resource:"wakeG"}],uniforms:[{name:"sym",values:{n:256,window:"param:wakeLength"}}]},{name:"wake.fft2.rows",type:"repeat",when:"param:wakeEnabled",count:8,index:"s",swap:[["wakeG","wakeF"]],body:[{name:"stage",kernel:"fft_gather_256",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"wakeG"},{name:"dst",resource:"wakeF"}],uniforms:[{name:"fft",values:{axis:0,n:256,stage:"param:s"}}]}]},{name:"wake.fft2.cols",type:"repeat",when:"param:wakeEnabled",count:8,index:"s",swap:[["wakeG","wakeF"]],body:[{name:"stage",kernel:"fft_gather_256",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"wakeG"},{name:"dst",resource:"wakeF"}],uniforms:[{name:"fft",values:{axis:1,n:256,stage:"param:s"}}]}]},{name:"wake.step",kernel:"wake_step",when:"param:wakeEnabled",target:{width:256,height:256,elioWidth:256},bindings:[{name:"qIn",resource:"wakeG"},{name:"cleanIn",resource:"wakeC"},{name:"stateOut",resource:"wakeB"},{name:"wakeTex",resource:"wakeTex"}],uniforms:[{name:"wstep",values:{alpha:"param:wakeAlpha",cell:"param:wakeCell",dt:"param:wakeDt",gravity:"param:wakeGEff",originX:"param:wakeOriginX",originZ:"param:wakeOriginZ",sponge:24,spongeRate:.8,width:256}}]},{name:"wake.wakeTex.mips",type:"mipmap",when:"param:wakeEnabled",resource:"wakeTex"},{name:"foam",kernel:"ocean_foam",target:{width:1024,height:1024,elioWidth:1024},bindings:[{name:"foamIn",resource:"foamA"},{name:"fieldB0",resource:"fftB_0"},{name:"fieldB1",resource:"fftB_1"},{name:"fieldB2",resource:"fftB_2"},{name:"fieldB3",resource:"fftB_2"},{name:"foamOut",resource:"foamB"},{name:"foamTex",resource:"foamTex"},{name:"wakeIn",resource:"wakeB"}],uniforms:[{name:"foam",values:{cascades:3,cell:.5,dt:"param:dt",gain:"param:foamGain",lambda:"param:lambda",n0:64,n1:64,n2:64,n3:64,omX0:"param:foamOriginMod0X",omX1:"param:foamOriginMod1X",omX2:"param:foamOriginMod2X",omX3:"param:foamOriginMod2X",omZ0:"param:foamOriginMod0Z",omZ1:"param:foamOriginMod1Z",omZ2:"param:foamOriginMod2Z",omZ3:"param:foamOriginMod2Z",originX:"param:foamOriginX",originZ:"param:foamOriginZ",shiftX:"param:foamShiftX",shiftZ:"param:foamShiftZ",size0:1600,size1:250,size2:37,size3:37,tau:"param:foamTau",threshold:"param:foamThreshold",wakeCell:"param:wakeCell",wakeGain:"param:wakeFoamGainEff",wakeOriginX:"param:wakeOriginModX",wakeOriginZ:"param:wakeOriginModZ",wakeRelX:"param:wakeRelX",wakeRelZ:"param:wakeRelZ",wakeWidth:256,width:1024}}]},{name:"foamTex.mips",type:"mipmap",resource:"foamTex"}],frameSwaps:[["wakeA","wakeB"],["foamA","foamB"]],exports:{disp0:"dispTex0",disp1:"dispTex1",disp2:"dispTex2",foam:"foamTex",slope0:"slopeTex0",slope1:"slopeTex1",slope2:"slopeTex2",wake:"wakeTex"},paramsFrom:"webgl2/high"}}})})(),(function(){function ie(o){return o.replace(/_([a-z0-9])/g,function(b,A){return A.toUpperCase()})}function X(o){return typeof o=="boolean"?o?1:0:Number(o)}function M(o,b,A){if(typeof o=="string"){if(o.indexOf("param:")!==0)throw new Error("elio-graph: "+A+": bad value "+o);const F=o.slice(6);if(!(F in b))throw new Error("elio-graph: parameter "+F+" was not supplied");return X(b[F])}return X(o)}function D(o,b,A,F){const v=new ArrayBuffer(Math.max(16,Math.ceil(o.size/16)*16)),_=new DataView(v),h={};for(const Z of o.fields){let y=b[Z.name];if(y===void 0){if(!/^_pad/.test(Z.name))throw new Error("elio-graph: "+F+": uniform "+o.struct+" lacks a value for "+Z.name);y=0}h[Z.name]=!0;const re=M(y,A,F);if(Z.type==="u32")_.setUint32(Z.offset,re>>>0,!0);else if(Z.type==="i32")_.setInt32(Z.offset,re|0,!0);else if(Z.type==="f32")_.setFloat32(Z.offset,re,!0);else throw new Error("elio-graph: "+F+": unsupported uniform type "+Z.type)}for(const Z of Object.keys(b))if(!h[Z])throw new Error("elio-graph: "+F+": uniform "+o.struct+" has no field "+Z);return v}function d(o){for(const b of Object.keys(o))if(typeof o[b]=="string")return!1;return!0}const a={x:1,z:1,heading:1,speed:1},le={time:1,dt:1,cameraX:1,cameraY:1,cameraZ:1},te={add:function(o,b){return o+b},sub:function(o,b){return o-b},mul:function(o,b){return o*b},div:function(o,b){return b===0?0:o/b},mod:function(o,b){return b===0?0:o-Math.floor(o/b)*b},floor:function(o){return Math.floor(o)},sin:function(o){return Math.sin(o)},cos:function(o){return Math.cos(o)},min:function(o,b){return Math.min(o,b)},max:function(o,b){return Math.max(o,b)}};function ue(o,b){return/\[\]$/.test(o)?Float32Array.from(b||[]):(b=Number(b),Number.isFinite(b)?o==="bool"?b?1:0:o==="u32"?Math.max(0,Math.floor(b))>>>0:o==="i32"?Math.trunc(b)|0:b:0)}function we(o){const b={},A={},F={},v={},_={};let h=!0,Z=null,y={};const re={},Te={},ze=function(x){return x.tween>0?Z===null?x.tween:Z:0};for(const x of o){const P=x.source||{kind:"value",value:0};P.kind==="value"&&(_[x.name]=ue(x.type,/\[\]$/.test(x.type)?P.value:P.value||0))}const S=function(x,P){const O=P||b;if(typeof x=="number")return x;if(typeof x=="string"&&x.indexOf("param:")===0){const V=x.slice(6);return V in O?O[V]:0}if(x&&typeof x=="object"&&typeof x.op=="string"){const V=te[x.op];if(!V)throw new Error("elio-graph: unknown op "+x.op);return V(S(x.args[0],O),x.args.length>1?S(x.args[1],O):0)}throw new Error("elio-graph: bad argument "+x)},z={current:b,changed:{},setNodes:function(x){y=x||{}},setTween:function(x){Z=Number.isFinite(x)&&x>=0?x:null},set:function(x,P){const O=o.find(function(I){return I.name===x});if(!O||O.source&&O.source.kind!=="value")throw new Error("elio-graph: "+x+" is not a value parameter");const V=ue(O.type,P);if(V!==_[x]){if(typeof V!="number"){_[x]=V;return}_[x]=V,ze(O)>0&&!h?F[x]={from:b[x],start:null}:delete F[x]}},snapshot:function(){return Object.assign({},b)},derive:function(x,P){const O=Object.assign({},b);for(const V of o){const I=V.source||{kind:"value",value:0};if(I.kind==="frame"&&I.name==="dt")O[V.name]=ue(V.type,x);else if(I.kind==="expr")O[V.name]=ue(V.type,te[I.op](S(I.args[0],O),I.args.length>1?S(I.args[1],O):0));else if(I.kind==="delta"){const w=String(I.of).replace(/^param:/,"");O[V.name]=ue(V.type,P?(O[w]||0)-(P[w]||0):0)}}return O},evaluate:function(x){const P=Number(x.time)||0,O=Number(x.dt)||0,V=Number.isFinite(x.accumulateDt)?Number(x.accumulateDt):O,I={},w=function(n){if(n in I)return I[n];const l=typeof x.nodePose=="function"?x.nodePose(n):null;if(!l)return I[n]=null;const q=re[n];let R=Number(l.speed);return Number.isFinite(R)||(R=q&&O>0?Math.hypot(l.x-q.x,l.z-q.z)/O:0),I[n]={x:Number(l.x)||0,z:Number(l.z)||0,heading:Number(l.heading)||0,speed:R}};for(const n of o){const l=n.source||{kind:"value",value:0};let q=0;if(l.kind==="value"){q=_[n.name];const R=F[n.name];if(R){R.start===null&&(R.start=P);const ge=Math.min(1,Math.max(0,(P-R.start)/ze(n)));ge>=1?delete F[n.name]:q=R.from+(q-R.from)*ge}}else if(l.kind==="frame"){if(!le[l.name])throw new Error("elio-graph: unknown frame source "+l.name);q=l.name==="time"?P:l.name==="dt"?O:Number(x[l.name])||0}else if(l.kind==="expr"){const R=te[l.op];if(!R)throw new Error("elio-graph: unknown op "+l.op);q=R(S(l.args[0]),l.args.length>1?S(l.args[1]):0)}else if(l.kind==="node"){if(!a[l.field])throw new Error("elio-graph: unknown node field "+l.field);const R=w(l.node in y?y[l.node]:l.node);q=R?R[l.field]:l.field==="speed"?0:Te[n.name]||0,Te[n.name]=q}else if(l.kind==="delta"){const R=String(l.of).replace(/^param:/,"");q=h?0:(b[R]||0)-(A[R]||0)}else if(l.kind==="accumulate")v[n.name]=(v[n.name]||0)+S(l.of)*V,q=v[n.name];else throw new Error("elio-graph: unknown parameter source "+l.kind);b[n.name]=/\[\]$/.test(n.type)?_[n.name]:ue(n.type,q)}for(const n of Object.keys(I))I[n]&&(re[n]={x:I[n].x,z:I[n].z});for(const n of o)z.changed[n.name]=h||b[n.name]!==A[n.name];for(const n of o)A[n.name]=b[n.name];h=!1}};return z}function t(o,b,A){if(!o||o==="always")return!0;if(o.indexOf("param:")===0)return!!b[o.slice(6)];if(o.indexOf("changed:")===0)return o.slice(8).split(",").some(function(F){return!!A[F.trim()]});throw new Error("elio-graph: bad when "+o)}function f(o,b){const A=Object.assign({},o);if(o.paramsFrom){const v=b&&b.graphs&&b.graphs[o.paramsFrom];if(!v||v.backend!==o.backend||v.paramsFrom)throw new Error("elio-graph: paramsFrom "+o.paramsFrom+" does not name a graph of "+o.backend+" that owns its parameters");A.params=v.params,A.exportParams=v.exportParams}delete A.paramsFrom,A.params=A.params||[],A.exportParams=A.exportParams||[],A.frameSwaps=A.frameSwaps||[],A.exports=A.exports||{};const F=function(v){const _=Object.assign({},v);if(_.type=v.type||"kernel",_.when=v.when||"always",_.type==="kernel"){const h=b&&b.kernels&&b.kernels[ie(v.kernel)];!_.entry&&h&&(_.entry=h.entry),v.bindings&&(_.bindings=v.bindings.map(function(Z){return Object.assign({level:0},Z)})),_.uniforms||(_.uniforms=[])}return _.type==="repeat"&&(_.body=(v.body||[]).map(F)),_};return A.passes=(o.passes||[]).map(F),A}function g(o,b){const A=b+(o.offset||0);return o.shr?Math.max(o.shr>>A,1):A}function W(o){const b={},A={},F={};for(const y of o.resources)b[y.name]=y;for(const y of o.params)F[y.name]=!0;const v=function(y){return y in A?A[y]:y},_=function(y,re,Te,ze){const S=[];for(const z of y){if(z.type!=="repeat"){S.push(h(z,re,Te,ze));continue}const x=Te+z.name;if(!(z.count>=1))throw new Error("elio-graph: repeat "+x+": count "+z.count+", want at least 1");if(!z.index||F[z.index])throw new Error("elio-graph: repeat "+x+": index "+z.index+" is empty or names a declared param");if(!z.body||!z.body.length)throw new Error("elio-graph: repeat "+x+": body is empty");if(z.index in re)throw new Error("elio-graph: repeat "+x+": index "+z.index+" is already in use by an outer repeat");for(const O of z.swap||[])if(!b[O[0]]||!b[O[1]])throw new Error("elio-graph: repeat "+x+": swap names an unknown resource");const P=z.when&&z.when!=="always"?z.when:ze;for(let O=0;O<z.count;O++){const V=Object.assign({},re,{[z.index]:O});for(const I of _(z.body,V,x+"["+O+"].",P))S.push(I);for(const I of z.swap||[]){const w=v(I[0]),n=v(I[1]);A[I[0]]=n,A[I[1]]=w}}}return S},h=function(y,re,Te,ze){const S=Object.assign({},y,{name:Te+y.name});return ze&&(!y.when||y.when==="always")&&(S.when=ze),y.resource&&(S.resource=v(y.resource)),y.from&&(S.from=v(y.from)),y.to&&(S.to=v(y.to)),y.bindings&&(S.bindings=y.bindings.map(function(z){const x=Object.assign({},z,{resource:v(z.resource)});if(z.levelAt){if(!(z.levelAt.index in re))throw new Error("elio-graph: pass "+S.name+": index "+z.levelAt.index+" is not in scope");x.level=g(z.levelAt,re[z.levelAt.index]),delete x.levelAt}return x})),y.uniforms&&(S.uniforms=y.uniforms.map(function(z){const x={};for(const P of Object.keys(z.values)){let O=z.values[P];if(O&&typeof O=="object"){if(!(O.index in re))throw new Error("elio-graph: pass "+S.name+": index "+O.index+" is not in scope");O=g(O,re[O.index])}else typeof O=="string"&&O.indexOf("param:")===0&&O.slice(6)in re&&(O=re[O.slice(6)]);x[P]=O}return Object.assign({},z,{values:x})})),S},Z=_(o.passes,{},"","");for(const y of Object.keys(A))if(A[y]!==y)throw new Error("elio-graph: swaps are not restored: "+y+" still refers to "+A[y]+" at the end of the passes");return Z}function de(o,b,A){if(!o||o.version!==1)throw new Error("elio-graph: unsupported graph version");if(o.backend!==A)throw new Error("elio-graph: graph is for "+o.backend+", not "+A);const F={};for(const h of o.resources)F[h.name]=h;const v={};for(const h of o.params)v[h.name]=!0;const _=function(h){if(typeof h=="string"&&h.indexOf("param:")===0&&!v[h.slice(6)])throw new Error("elio-graph: undeclared parameter "+h.slice(6))};for(const h of W(o)){const Z=h.when||"always";if(Z.indexOf("param:")===0)_(Z);else if(Z.indexOf("changed:")===0){for(const y of Z.slice(8).split(","))if(!v[y.trim()])throw new Error("elio-graph: undeclared parameter "+y.trim())}else if(Z!=="always")throw new Error("elio-graph: pass "+h.name+": bad when "+Z);if(h.type==="mipmap"){if(!F[h.resource])throw new Error("elio-graph: pass "+h.name+": unknown resource "+h.resource);continue}if(h.type==="copy"){if(A!=="webgl2")throw new Error("elio-graph: pass "+h.name+": copy passes exist only on webgl2");for(const y of[h.from,h.to])if(!F[y]||F[y].kind!=="buffer")throw new Error("elio-graph: pass "+h.name+": unknown resource "+y+" (a copy joins two buffers)");continue}if(!b.kernels[ie(h.kernel)])throw new Error("elio-graph: pass "+h.name+": the module has no kernel "+h.kernel);for(const y of h.bindings)if(!F[y.resource])throw new Error("elio-graph: pass "+h.name+": unknown resource "+y.resource);for(const y of h.uniforms||[])for(const re of Object.keys(y.values))_(y.values[re])}for(const h of o.exportParams||[])if(!v[h])throw new Error("elio-graph: exportParams names undeclared parameter "+h);for(const h of o.resources)if(h.feed&&!v[h.feed])throw new Error("elio-graph: resource "+h.name+" is fed by undeclared parameter "+h.feed);for(const h of o.frameSwaps||[])for(const Z of h)if(!F[Z])throw new Error("elio-graph: frameSwaps names unknown resource "+Z);for(const h of Object.keys(o.exports||{})){const Z=F[o.exports[h]];if(!Z||Z.kind!=="texture"&&A!=="webgpu")throw new Error("elio-graph: export "+h+" must name a texture resource")}}function be(o){const b=o.backend,A=o.module,F=o.host||{},v={graph:null,backend:b,ready:!1,failed:null,disposed:!1,buildCount:0,run:P,nodePose:re,gpuTiming:Te,setValue:x,setNodes:ze,setTween:S,value:z,hasParam:y,params:null,exportParams:O,export:V,dispose:w,fail:I};let _=null,h=null;const Z=function(n){if(!v.failed){if(v.failed=n&&n.fallback||"error",v.error=n,_){try{_.dispose()}catch{}_=null}v.ready=!1,o.onFail&&o.onFail(v.failed,n),!(n&&n.fallback)&&typeof console<"u"&&console.warn&&console.warn("elio-graph: graph disabled: "+(n&&n.message?n.message:String(n)))}};try{let n=o.graph||A.graphs&&A.graphs[b+"/"+(o.tier||"high")];if(!n)throw Object.assign(new Error("elio-graph: no graph for "+b+"/"+o.tier),{fallback:"no-graph"});n=f(n,A),de(n,A,b);const l=window.__gosx_elio_graph.executors[b];if(!l)throw Object.assign(new Error("elio-graph: no executor for "+b),{fallback:"no-executor"});v.graph=n,h=v.params=we(n.params),_=l({steps:W(n).map(function(q){return{pass:q}}),graph:n,module:A,host:F,pack:D,isStatic:d,kernelKey:ie,holds:t,label:o.label||n.name}),v.buildCount=1,v.ready=_.isReady()}catch(n){Z(n)}function y(n){return!!(v.graph&&v.graph.params.some(function(l){return l.name===n}))}function re(n){return F.nodePose?F.nodePose(n):null}function Te(){return _&&_.gpuTiming?_.gpuTiming():null}function ze(n){h&&h.setNodes(n)}function S(n){h&&h.setTween(n)}function z(n){const l=_&&_.held?_.held(n):void 0;return l!==void 0?l:h&&n in h.current?h.current[n]:0}function x(n,l){if(!(!h||v.failed))try{h.set(n,l)}catch(q){Z(q)}}function P(n){if(!_||v.failed||v.disposed)return!1;try{if(v.ready=_.isReady(),!v.ready)return!1;const l=n&&!n.nodePose&&F.nodePose?Object.assign({},n,{nodePose:F.nodePose}):n||{};return h.evaluate(l),_.run(h.current,l,h.changed,h)!==!1}catch(l){return Z(l),!1}}function O(){const n={};if(h&&v.graph)for(const l of v.graph.exportParams||[])n[l]=z(l);return n}function V(n){return _&&!v.failed?_.export(n):null}function I(n){Z({message:n,fallback:n})}function w(){if(!v.disposed&&(v.disposed=!0,v.ready=!1,_)){try{_.dispose()}catch{}_=null}}return v}window.__gosx_elio_graph={create:be,expand:W,normalize:f,pack:D,kernelKey:ie,check:de,holds:t,createParams:we,executors:{}}})(),(function(){const ie=`#version 300 es
void main() { vec2 p = vec2(float((gl_VertexID << 1) & 2), float(gl_VertexID & 2)); gl_Position = vec4(p * 2. - 1., 0., 1.); }`;function X(d){return{draw:d.getParameter(d.DRAW_FRAMEBUFFER_BINDING),read:d.getParameter(d.READ_FRAMEBUFFER_BINDING),viewport:d.getParameter(d.VIEWPORT),program:d.getParameter(d.CURRENT_PROGRAM),vao:d.getParameter(d.VERTEX_ARRAY_BINDING),active:d.getParameter(d.ACTIVE_TEXTURE),blend:d.isEnabled(d.BLEND),depth:d.isEnabled(d.DEPTH_TEST),cull:d.isEnabled(d.CULL_FACE),scissor:d.isEnabled(d.SCISSOR_TEST),colorMask:d.getParameter(d.COLOR_WRITEMASK),depthMask:d.getParameter(d.DEPTH_WRITEMASK)}}function M(d,a){d.bindBuffer(d.UNIFORM_BUFFER,null),d.bindBufferBase(d.UNIFORM_BUFFER,0,null),d.bindBuffer(d.PIXEL_PACK_BUFFER,null),d.bindBuffer(d.PIXEL_UNPACK_BUFFER,null),d.bindFramebuffer(d.DRAW_FRAMEBUFFER,a.draw),d.bindFramebuffer(d.READ_FRAMEBUFFER,a.read),d.viewport(a.viewport[0],a.viewport[1],a.viewport[2],a.viewport[3]),d.useProgram(a.program),d.bindVertexArray(a.vao),d.activeTexture(a.active);const le=function(te,ue){ue?d.enable(te):d.disable(te)};le(d.BLEND,a.blend),le(d.DEPTH_TEST,a.depth),le(d.CULL_FACE,a.cull),le(d.SCISSOR_TEST,a.scissor),d.colorMask(a.colorMask[0],a.colorMask[1],a.colorMask[2],a.colorMask[3]),d.depthMask(a.depthMask)}function D(d){const a=d.host.gl,le=d.graph,te=d.module,ue=te.kernels,we=d.host.firstUnit||4;if(!(d.host.caps?d.host.caps(a):{}).colorBufferFloat)throw Object.assign(new Error("EXT_color_buffer_float is not available"),{fallback:"EXT_color_buffer_float"});const f=function(i,m){const k=a.getParameter(i);return typeof k=="number"?k:m};if(f(a.MAX_DRAW_BUFFERS,0)<2||f(a.MAX_TEXTURE_IMAGE_UNITS,0)<we+5)throw Object.assign(new Error("too few texture units or draw buffers"),{fallback:"texture-units"});for(const i of d.steps){const m=i.pass;if(!m||m.type!=="kernel")continue;const k=ue[d.kernelKey(m.kernel)];if(!k.glsl||!k.gatherLayout)throw Object.assign(new Error("kernel "+m.kernel+" has no GLSL gather form"),{fallback:"kernel:"+m.kernel})}const g=[],W=[],de=[],be=[],o=Object.create(null),b={};for(const i of Object.keys(le.exports||{}))b[le.exports[i]]=!0;const A=a.createVertexArray(),F={split:!1,disposed:!1};let v=null;const _=Object.create(null),h=new Map,Z=new Map;let y=0;const re={rgba16float:a.RGBA16F,rgba32float:a.RGBA32F,r32float:a.R32F};function Te(i,m,k,p,N){const J=a.createTexture();return g.push(J),a.activeTexture(a.TEXTURE0+we),a.bindTexture(a.TEXTURE_2D,J),a.texStorage2D(a.TEXTURE_2D,p,k,i,m),a.texParameteri(a.TEXTURE_2D,a.TEXTURE_MIN_FILTER,N?p>1?a.LINEAR_MIPMAP_LINEAR:a.LINEAR:a.NEAREST),a.texParameteri(a.TEXTURE_2D,a.TEXTURE_MAG_FILTER,N?a.LINEAR:a.NEAREST),a.texParameteri(a.TEXTURE_2D,a.TEXTURE_WRAP_S,N?a.REPEAT:a.CLAMP_TO_EDGE),a.texParameteri(a.TEXTURE_2D,a.TEXTURE_WRAP_T,N?a.REPEAT:a.CLAMP_TO_EDGE),J}function ze(i,m){const k=a.createShader(i);if(be.push(k),a.shaderSource(k,m),a.compileShader(k),!a.getShaderParameter(k,a.COMPILE_STATUS))throw new Error("elio-graph: shader did not compile: "+a.getShaderInfoLog(k));return k}function S(i){if(o[i])return o[i];const m=ue[d.kernelKey(i)];v||(v=ze(a.VERTEX_SHADER,ie));const k=ze(a.FRAGMENT_SHADER,m.glsl),p=a.createProgram();a.attachShader(p,v),a.attachShader(p,k),a.linkProgram(p);const N=m.gatherLayout,J=o[i]={handle:p,layout:N,kernel:m};if(!a.getProgramParameter(p,a.LINK_STATUS))throw new Error("elio-graph: program "+i+" did not link: "+a.getProgramInfoLog(p));return a.useProgram(p),J.width=a.getUniformLocation(p,"elio_width"),J.lens=N.inputs.map(function(Q){return a.getUniformLocation(p,Q.length)}),J.widths=N.inputs.map(function(Q){return Q.width?a.getUniformLocation(p,Q.width):null}),N.inputs.forEach(function(Q,L){a.uniform1i(a.getUniformLocation(p,Q.sampler),we+L)}),N.uniforms.forEach(function(Q,L){a.uniformBlockBinding(p,a.getUniformBlockIndex(p,Q.block),L)}),J}function z(i,m){const p=(i.uniformsStd140||i.uniforms).find(function(N){return N.name===m});if(!p)throw new Error("elio-graph: kernel has no uniform block "+m);return p}function x(){for(const i of le.resources){let m;if(i.kind==="buffer"){const k=Math.ceil(i.count/i.width);m={name:i.name,id:++y,kind:"buffer",version:0,w:i.width,h:k,levels:1,tex:Te(i.width,k,a.RGBA32F,1,!1)}}else{const k=i.mipLevels||1,p=re[i.format];if(!p)throw new Error("elio-graph: unsupported format "+i.format);const N=i.format==="rgba16float"&&(k>1||!!b[i.name]);m={name:i.name,id:++y,kind:"texture",version:0,w:i.width,h:i.height,levels:k,tex:Te(i.width,i.height,p,k,N)}}_[i.name]=m}}function P(i){const m=a.createFramebuffer();W.push(m),a.bindFramebuffer(a.FRAMEBUFFER,m);const k=[];for(const p of i){for(a.framebufferTexture2D(a.FRAMEBUFFER,a.COLOR_ATTACHMENT0+p.location,a.TEXTURE_2D,p.res.tex,p.level);k.length<=p.location;)k.push(a.NONE);k[p.location]=a.COLOR_ATTACHMENT0+p.location}return a.drawBuffers(k),{fbo:m,complete:a.checkFramebufferStatus(a.FRAMEBUFFER)===a.FRAMEBUFFER_COMPLETE}}function O(i){const m=i.map(function(J){return J.res.id+"@"+J.level+":"+J.location}).join(","),k=h.get(m+(F.split?"s":""));if(k)return k;let p;const N=function(J){const Q=P([J]);if(!Q.complete)throw new Error("elio-graph: framebuffer incomplete");return Q};if(i.length===1)p=[N(i[0])];else{const J=F.split?null:P(i);J&&J.complete?p=[J]:(F.split=!0,p=i.map(N))}return h.set(m+(F.split?"s":""),p),p}function V(){if(!F.disposed){F.disposed=!0;for(const i of g)a.deleteTexture(i);for(const i of W)a.deleteFramebuffer(i);for(const i of de)a.deleteBuffer(i);for(const i of Object.keys(o))a.deleteProgram(o[i].handle);for(const i of be)a.deleteShader(i);a.deleteVertexArray(A),g.length=W.length=de.length=0,h.clear(),Z.clear()}}let I=[];const w=X(a);try{I=d.steps.map(function(i){const m=i.pass;if(m.type==="mipmap"||m.type==="copy")return{pass:m};const k=S(m.kernel),p=k.layout,N=p.inputs.map(function(L){const ye=m.bindings.find(function(Se){return Se.name===L.name});if(!ye)throw new Error("elio-graph: pass "+m.name+" does not bind input "+L.name);return ye}),J=p.outputs.map(function(L){const ye=m.bindings.find(function(Se){return Se.name===L.name});if(!ye)throw new Error("elio-graph: pass "+m.name+" does not bind output "+L.name);return{binding:ye,location:L.location}}),Q=(m.uniforms||[]).map(function(L){const ye=p.uniforms.findIndex(function(e){return e.name===L.name});if(ye<0)throw new Error("elio-graph: pass "+m.name+" has no uniform block "+L.name);const Se=z(k.kernel,L.name),Ze=a.createBuffer(),Re=Math.max(16,Math.ceil(Se.size/16)*16);return de.push(Ze),a.bindBuffer(a.UNIFORM_BUFFER,Ze),a.bufferData(a.UNIFORM_BUFFER,Re,a.DYNAMIC_DRAW),{index:ye,layout:Se,values:L.values,buffer:Ze,size:Re,written:!1,constant:d.isStatic(L.values)}});return{pass:m,p:k,inputs:N,outputs:J,blocks:Q}}),x()}catch(i){throw V(),i}finally{M(a,w)}function n(i){const m=_[i.from],k=_[i.to];if(m.w*m.h!==k.w*k.h||k.w!==i.width)throw new Error("elio-graph: pass "+i.name+" cannot copy "+m.name+" into "+k.name+" at width "+i.width);if(k.copiedFrom===m&&k.copiedVersion===m.version)return;let p=Z.get(k.id);p||(p=a.createBuffer(),de.push(p),a.bindBuffer(a.PIXEL_PACK_BUFFER,p),a.bufferData(a.PIXEL_PACK_BUFFER,m.w*m.h*16,a.STREAM_COPY),a.bindBuffer(a.PIXEL_PACK_BUFFER,null),Z.set(k.id,p)),a.bindFramebuffer(a.READ_FRAMEBUFFER,O([{res:m,level:0,location:0}])[0].fbo),a.bindBuffer(a.PIXEL_PACK_BUFFER,p),a.readPixels(0,0,m.w,m.h,a.RGBA,a.FLOAT,0),a.bindBuffer(a.PIXEL_PACK_BUFFER,null),a.bindBuffer(a.PIXEL_UNPACK_BUFFER,p),a.activeTexture(a.TEXTURE0+we),a.bindTexture(a.TEXTURE_2D,k.tex),a.texSubImage2D(a.TEXTURE_2D,0,0,0,k.w,k.h,a.RGBA,a.FLOAT,0),a.bindBuffer(a.PIXEL_UNPACK_BUFFER,null),k.copiedFrom=m,k.copiedVersion=m.version,k.version++}function l(i){for(const m of i){const k=_[m[0]];_[m[0]]=_[m[1]],_[m[1]]=k}}function q(i){for(const m of le.resources){if(!m.feed)continue;const k=_[m.name],p=new Float32Array(k.w*k.h*4);p.set(i[m.feed].subarray(0,p.length)),a.activeTexture(a.TEXTURE0+we),a.bindTexture(a.TEXTURE_2D,k.tex),a.texSubImage2D(a.TEXTURE_2D,0,0,0,k.w,k.h,a.RGBA,a.FLOAT,p)}}function R(i,m){const k=i.pass,p=i.p,N=k.target,J=i.outputs.map(function(L){return{res:_[L.binding.resource],level:L.binding.level||0,location:L.location}}),Q=i.inputs.map(function(L){return _[L.resource]});i.inputs.forEach(function(L,ye){for(const Se of J)if(Se.res===Q[ye]&&Se.level===(L.level||0))throw new Error("elio-graph: pass "+k.name+" reads "+Se.res.name+", which it writes")}),a.useProgram(p.handle),a.uniform1ui(p.width,N.elioWidth),Q.forEach(function(L,ye){a.activeTexture(a.TEXTURE0+we+ye),a.bindTexture(a.TEXTURE_2D,L.tex),p.lens[ye]&&a.uniform1ui(p.lens[ye],L.w*L.h),p.widths[ye]&&a.uniform1ui(p.widths[ye],L.w)});for(const L of i.blocks)(!L.constant||!L.written)&&(a.bindBuffer(a.UNIFORM_BUFFER,L.buffer),a.bufferSubData(a.UNIFORM_BUFFER,0,new Uint8Array(d.pack(L.layout,L.values,m,k.name))),L.written=!0),a.bindBufferRange(a.UNIFORM_BUFFER,L.index,L.buffer,0,L.size);for(const L of J)L.res.version++;a.viewport(0,0,N.width,N.height);for(const L of O(J))a.bindFramebuffer(a.FRAMEBUFFER,L.fbo),a.drawArrays(a.TRIANGLES,0,3)}const ge=d.host.profile,K=function(i,m){i.pass.type==="copy"?n(i.pass):i.pass.type==="mipmap"?(a.activeTexture(a.TEXTURE0+we),a.bindTexture(a.TEXTURE_2D,_[i.pass.resource].tex),a.generateMipmap(a.TEXTURE_2D)):R(i,m)},Y=[];for(const i of d.host.throttle||[]){const m=new RegExp(i.match),k=I.filter(function(N){return m.test(N.pass.name)&&!N.group});if(!k.length||!(i.every>1))continue;const p={every:Math.floor(i.every),hold:i.hold?new RegExp(i.hold):null,since:1/0,dt:0,snap:null,runs:!1,derived:null,plans:k};for(const N of k)N.group=p;Y.push(p)}const xe=function(i){let m=null,k=!1;for(const p of I){const N=(p.outputs||[]).map(function(J){return J.binding.resource}).concat(p.pass.resource?[p.pass.resource]:[]);N.indexOf(i[0])<0&&N.indexOf(i[1])<0||(!p.group||m&&m!==p.group?k=!0:m=p.group)}return k?null:m},ve=(le.frameSwaps||[]).map(function(i){return{pair:i,group:Y.length?xe(i):null}}),oe=function(i){return i&&i!=="always"?i.replace(/^(param|changed):/,"").split(",").map(function(m){return m.trim()}):[]},Me=function(i,m,k){for(const p of Y){p.dt+=Number(i&&i.dt)||0,p.since++;const N=p.plans.some(function(J){return oe(J.pass.when).some(function(Q){return m[Q]})});p.runs=N||p.since>=p.every,p.runs&&(p.derived=k.derive(p.dt,p.snap))}},De=function(i){for(const m of Y)m.runs&&(m.since=0,m.dt=0,m.snap=i.snapshot())};return{isReady:function(){return!F.disposed},run:function(i,m,k,p){if(a.isContextLost&&a.isContextLost())return!1;const N=X(a);try{a.disable(a.BLEND),a.disable(a.DEPTH_TEST),a.disable(a.CULL_FACE),a.disable(a.SCISSOR_TEST),a.colorMask(!0,!0,!0,!0),a.bindVertexArray(A),q(i),Y.length&&Me(m,k,p);for(const Q of I){if(Q.group&&!Q.group.runs||!d.holds(Q.pass.when,i,k))continue;const L=Q.group?Q.group.derived:i;ge?ge(Q.pass.name,function(){K(Q,L)}):K(Q,L)}for(const Q of ve)(!Q.group||Q.group.runs)&&l([Q.pair]);Y.length&&De(p)}finally{M(a,N)}const J=d.host.textureCache;return J&&(J._sceneTextureEpoch=(J._sceneTextureEpoch||0)+1),!0},export:function(i){const m=_[le.exports[i]];return m?{tex:m.tex,width:m.w,height:m.h,levels:m.levels}:null},held:function(i){for(const m of Y)if(m.hold&&m.hold.test(i)&&m.snap&&i in m.snap)return m.snap[i]},split:function(){return F.split},dispose:V}}window.__gosx_elio_graph.executors.webgl2=D,window.__gosx_elio_graph.glState={save:X,restore:M}})(),(function(){const ie={f32:4,"vec2<f32>":8,"vec4<f32>":16};function X(M){const D=M.host.device,d=M.graph,a=M.module.kernels,le=M.host;if(!D)throw Object.assign(new Error("no device"),{fallback:"no-device"});const te="gosx-elio-"+M.label+"-",ue=[],we=[],t=Object.create(null),f=Object.create(null),g=Object.create(null),W=new Map,de=new Map,be={disposed:!1};let o=0;const b={};for(const w of Object.keys(d.exports||{}))b[d.exports[w]]=!0;const A=D.queue,F=GPUBufferUsage.STORAGE|GPUBufferUsage.COPY_DST|GPUBufferUsage.COPY_SRC,v=GPUBufferUsage.UNIFORM|GPUBufferUsage.COPY_DST;for(const w of d.resources)if(w.kind==="buffer"){const n=Math.max(16,Math.ceil(w.count*(ie[w.elementType]||16)/4)*4),l=D.createBuffer({label:te+w.name,size:n,usage:F});ue.push(l),t[w.name]={name:w.name,id:++o,buffer:l}}else{const n=D.createTexture({label:te+w.name,size:[w.width,w.height,1],format:w.format,mipLevelCount:w.mipLevels||1,usage:GPUTextureUsage.STORAGE_BINDING|GPUTextureUsage.TEXTURE_BINDING});we.push(n),t[w.name]={name:w.name,id:++o,texture:n,w:w.width,h:w.height,levels:w.mipLevels||1,format:w.format}}function _(w){return w.map(function(n){const l={binding:n.binding,visibility:GPUShaderStage.COMPUTE};return n.kind==="uniform"?l.buffer={type:"uniform"}:n.kind==="storage-read"?l.buffer={type:"read-only-storage"}:n.kind==="storage-rw"?l.buffer={type:"storage"}:n.kind==="sampled-texture"||n.kind==="texture"?l.texture={sampleType:"unfilterable-float",viewDimension:"2d"}:l.storageTexture={access:"write-only",format:n.format,viewDimension:"2d"},l})}for(const w of M.steps){const n=w.pass;if(!n)continue;if(n.type!=="kernel")throw new Error("elio-graph: pass "+n.name+": "+n.type+" passes exist only on webgl2");const l=n.kernel+"/"+n.entry;if(f[l])continue;const q=a[M.kernelKey(n.kernel)],R=g[n.kernel]||(g[n.kernel]=D.createBindGroupLayout({label:te+n.kernel,entries:_(q.bindings)})),ge=D.createShaderModule({label:te+n.kernel,code:q.wgsl}),K=le.create(D,"compute",{label:te+n.kernel,layout:D.createPipelineLayout({bindGroupLayouts:[R]}),compute:{module:ge,entryPoint:n.entry}});f[l]={handle:K,layout:R,kernel:q}}function h(w){const n=le.validation&&le.validation.get(w.handle);return!n||n.ready===!0?n&&n.pipeline||w.handle:null}let Z=0;const y=M.steps.map(function(w){const n=w.pass,l=f[n.kernel+"/"+n.entry],q=(n.uniforms||[]).map(function(R){const ge=l.kernel.uniforms.find(function(xe){return xe.name===R.name});if(!ge)throw new Error("elio-graph: pass "+n.name+" has no uniform block "+R.name);const K=Math.max(16,Math.ceil(ge.size/16)*16),Y=D.createBuffer({label:te+n.name+"-"+R.name,size:K,usage:v});return ue.push(Y),{name:R.name,layout:ge,values:R.values,buffer:Y,constant:M.isStatic(R.values),written:!1}});return{pass:n,p:l,blocks:q,id:++Z}});function re(w){const n=w.pass,l=[],q=[n.name,w.id];for(const K of w.p.kernel.bindings){let Y;if(K.kind==="uniform")Y={buffer:w.blocks.find(function(xe){return xe.name===K.name}).buffer};else{const xe=n.bindings.find(function(oe){return oe.name===K.name});if(!xe)throw new Error("elio-graph: pass "+n.name+" does not bind "+K.name);const ve=t[xe.resource];q.push(ve.id+"@"+(xe.level||0)),K.kind==="storage-read"||K.kind==="storage-rw"?Y={buffer:ve.buffer}:Y=ve.texture.createView({baseMipLevel:xe.level||0,mipLevelCount:1})}l.push({binding:K.binding,resource:Y})}const R=q.join("|");let ge=W.get(R);return ge||(ge=D.createBindGroup({label:te+n.name,layout:w.p.layout,entries:l}),W.set(R,ge)),ge}function Te(w){for(const n of w){const l=t[n[0]];t[n[0]]=t[n[1]],t[n[1]]=l}}const ze=3,S=120;let z=null;if(le.timing!==!1&&D.features&&D.features.has&&D.features.has("timestamp-query"))try{const w=[],n=D.createQuerySet({label:te+"time",type:"timestamp",count:2}),l=D.createBuffer({label:te+"time-resolve",size:16,usage:GPUBufferUsage.QUERY_RESOLVE|GPUBufferUsage.COPY_SRC});ue.push(l);for(let q=0;q<ze;q++){const R=D.createBuffer({label:te+"time-"+q,size:16,usage:GPUBufferUsage.MAP_READ|GPUBufferUsage.COPY_DST});ue.push(R),w.push({buffer:R,state:"free"})}z={querySet:n,resolve:l,slots:w,samples:[],ms:null}}catch{z=null}function x(){if(z)for(const w of z.slots)w.state==="copied"&&(w.state="mapping",w.buffer.mapAsync(1).then(function(){if(be.disposed||w.state!=="mapping")return;const n=new BigUint64Array(w.buffer.getMappedRange().slice(0,16));w.buffer.unmap(),w.state="free",n[1]>n[0]&&(z.ms=Number(n[1]-n[0])/1e6,z.samples.push(z.ms),z.samples.length>S&&z.samples.shift())},function(){w.state="free"}))}function P(w,n,l){w.beginComputePass({label:te+"time-"+n,timestampWrites:Object.assign({querySet:z.querySet},l)}).end()}function O(w){const n=z.slots.find(function(l){return l.state==="free"});w.resolveQuerySet(z.querySet,0,2,z.resolve,0),n&&(w.copyBufferToBuffer(z.resolve,0,n.buffer,0,16),n.state="copied")}const V=D.lost&&D.lost.then?D.lost:null;V&&V.then(function(){I()},function(){I()});function I(){if(!be.disposed){be.disposed=!0;for(const w of ue)w.destroy();for(const w of we)w.destroy();ue.length=we.length=0,W.clear(),de.clear(),z&&z.querySet.destroy&&z.querySet.destroy()}}return{isReady:function(){if(be.disposed)return!1;for(const w of Object.keys(f))if(!h(f[w]))return!1;return!0},run:function(w,n,l){const q=n.encoder;if(!q)throw new Error("elio-graph: a webgpu frame needs frame.encoder");for(const Y of d.resources)Y.feed&&A.writeBuffer(t[Y.name].buffer,0,w[Y.feed]);x();const R=y.filter(function(Y){return M.holds(Y.pass.when,w,l)}),ge=!!z&&R.length>0,K=ge?D.createCommandEncoder({label:te+"graph"}):q;if(ge){const Y=D.createCommandEncoder({label:te+"time-begin"});P(Y,"begin",{beginningOfPassWriteIndex:0}),A.submit([Y.finish()])}return R.forEach(function(Y){const xe=Y.pass;for(const oe of Y.blocks)oe.constant&&oe.written||(A.writeBuffer(oe.buffer,0,new Uint8Array(M.pack(oe.layout,oe.values,w,xe.name))),oe.written=!0);const ve=K.beginComputePass({label:te+xe.name});ve.setPipeline(h(Y.p)),ve.setBindGroup(0,re(Y)),ve.dispatchWorkgroups(xe.dispatch[0],xe.dispatch[1]||1,xe.dispatch[2]||1),ve.end()}),ge&&(A.submit([K.finish()]),P(q,"end",{endOfPassWriteIndex:1})),z&&R.length&&O(q),Te(d.frameSwaps||[]),!0},export:function(w){const n=t[d.exports[w]];if(n&&n.buffer)return{buffer:n.buffer};if(!n)return null;let l=de.get(n.texture);return l||(l=n.texture.createView(),de.set(n.texture,l)),{texture:n.texture,view:l,width:n.w,height:n.h,levels:n.levels}},gpuTiming:function(){return z?{ms:z.ms,samples:z.samples.slice()}:null},dispose:I}}window.__gosx_elio_graph.executors.webgpu=X})(),(function(){const ie={uniform:0,sampler:1,disp:2,slope:6,foam:10,wake:11},X={edgeCells:8,k:.2,slope:3,slickLo:.04,slickHi:.4,slickFine:.85,slickMid:.4,glow:.3,thinBase:.12,thinLace:.3,bodyBase:.32,bodyLace:.3,fadeNear:120,fadeFar:700};function M(t,f,g,W){const de=t.indexOf(f);if(de<0)throw new Error("ocean-fft: anchor not found: "+f.slice(0,48));const be=t.indexOf(g,de);if(be<0)throw new Error("ocean-fft: anchor not found: "+g.slice(0,48));return t.slice(0,de)+W+t.slice(be)}function D(t,f,g){if(t.indexOf(f)<0)throw new Error("ocean-fft: anchor not found: "+f.slice(0,48));return t.replace(f,()=>g)}function d(t){let f="const WAKE_EDGE_CELLS: f32 = "+X.edgeCells.toFixed(1)+`;
`;f+=`struct OceanSpec { c: array<vec4f, 4>, f: vec4f, m: vec4f, w: array<vec4f, 4>, k: array<vec4f, 2> };
`,f+="@group("+t+") @binding("+ie.uniform+`) var<uniform> sp: OceanSpec;
`,f+="@group("+t+") @binding("+ie.sampler+`) var spSampler: sampler;
`;for(let g=0;g<4;g++)f+="@group("+t+") @binding("+(ie.disp+g)+") var spDisp"+g+`: texture_2d<f32>;
`;for(let g=0;g<4;g++)f+="@group("+t+") @binding("+(ie.slope+g)+") var spSlope"+g+`: texture_2d<f32>;
`;return f+="@group("+t+") @binding("+ie.foam+`) var spFoam: texture_2d<f32>;
`,f+="@group("+t+") @binding("+ie.wake+`) var spWakeTex: texture_2d<f32>;
`,f+=`fn spWake(rest: vec2f, cam: vec2f) -> vec3f {
`,f+=`  let r = (rest - cam - sp.k[0].xy) / max(sp.k[1].x, 1e-3);
`,f+=`  let edge = min(min(r.x, sp.k[1].z - r.x), min(r.y, sp.k[1].z - r.y));
`,f+=`  return vec3f((r + sp.k[0].zw) / sp.k[1].z, smoothstep(0.0, WAKE_EDGE_CELLS, edge) * sp.k[1].y);
`,f+=`}
`,f+=`fn spLod(c: vec4f, spacing: f32) -> f32 { return max(log2(max(spacing, 1e-3) * c.z / c.x), 0.0); }
`,f}function a(){let t=`  var p = vec3f(xz.x, level, xz.y); var dx = vec3f(1.0, 0.0, 0.0); var dz = vec3f(0.0, 0.0, 1.0);
`;t+=`  var amp = sp.m.x;
`,t+=`  let spacing = max(ocean.p[8].z * ocean.p[8].w * exp(ocean.p[8].w * f32(ring)), r * 6.2831853 / f32(segs));
`;for(let f=0;f<4;f++)t+="  if (sp.c["+f+`].w > 0.5) {
`,t+="    let uv = (xz - sp.w["+f+"].xy) / sp.c["+f+"].x; let lod = spLod(sp.c["+f+"], spacing); let k = smoothstep(0.0, 1.2, depth0 * sp.c["+f+`].y);
`,t+="    let a = textureSampleLevel(spDisp"+f+", spSampler, uv, lod); let b = textureSampleLevel(spSlope"+f+`, spSampler, uv, lod);
`,t+=`    p += a.xyz * k; dx += vec3f(b.z, b.x, a.w) * k; dz += vec3f(a.w, b.y, b.w) * k;
`,t+=`  }
`;return t+=`  let swv = spWake(xz, ocean.p[7].xz);
`,t+=`  if (sp.k[1].w > 0.0) { p = vec3f(xz.x, level, xz.y); dx = vec3f(1.0, 0.0, 0.0); dz = vec3f(0.0, 0.0, 1.0); }
`,t+=`  if (swv.z > 0.0 && sp.k[1].w <= 0.0) {
`,t+=`    let swk = textureSampleLevel(spWakeTex, spSampler, swv.xy, max(log2(max(spacing, 1e-3) / max(sp.k[1].x, 1e-3)), 0.0));
`,t+="    let sws = swv.z * smoothstep(0.0, 1.2, depth0 * "+X.k.toFixed(2)+`);
`,t+=`    p.y += swk.x * sws; dx.y += swk.y * sws; dz.y += swk.z * sws;
`,t+=`  }
`,t}function le(){let t=`  var sGx = 0.0; var sGz = 0.0; var sXX = 0.0; var sZZ = 0.0; var sXZ = 0.0;
`;t+=`  let wxz = in.rest - sp.m.yz;
`,t+=`  let sFw = fwidth(wxz);
`,t+=`  var sOp = 0.0; var sAlb = 1.0;
`,t+=`  let swv = spWake(in.rest, ocean.p[7].xz);
`,t+=`  let swf = textureSample(spWakeTex, spSampler, swv.xy);
`,t+="  let sSlick = swv.z * smoothstep("+X.slickLo.toFixed(2)+", "+X.slickHi.toFixed(2)+`, swf.w);
`;for(let f=0;f<4;f++){const g=f>=2?X.slickFine:f===1?X.slickMid:0;t+="  if (sp.c["+f+`].w > 0.5) {
`,t+="    let uv = (in.rest - sp.w["+f+"].xy) / sp.c["+f+"].x; let k = smoothstep(0.0, 1.2, in.depth0 * sp.c["+f+"].y)"+(g?" * (1.0 - "+g.toFixed(2)+" * sSlick)":"")+`;
`,t+="    let a = textureSample(spDisp"+f+", spSampler, uv); let b = textureSample(spSlope"+f+`, spSampler, uv);
`,t+=`    sGx += b.x * k; sGz += b.y * k; sXX += b.z * k; sZZ += b.w * k; sXZ += a.w * k;
`,t+=`  }
`}return t+="  let swm = swv.z * smoothstep(0.0, 1.2, in.depth0 * "+X.k.toFixed(2)+`);
`,t+="  sGx += swf.y * swm * "+X.slope.toFixed(2)+"; sGz += swf.z * swm * "+X.slope.toFixed(2)+`;
`,t+=`  let sJ = (1.0 + sXX) * (1.0 + sZZ) - sXZ * sXZ;
`,t+=`  let sFoamTex = textureSampleLevel(spFoam, spSampler, wxz * sp.f.x, 0.0);
`,t+=`  let sPersist = sFoamTex.r;
`,t+=`  let N = normalize(vec3f(sGz * sXZ - (1.0 + sZZ) * sGx, max(sJ, 0.1), sXZ * sGx - sGz * (1.0 + sXX)));
`,t}const te=`    let sNear = 1.0 - smoothstep(0.7 * sp.m.w, sp.m.w, length(in.world.xz - ocean.p[7].xz));
    let sJf = (1.0 + sp.f.y * sXX) * (1.0 + sp.f.y * sZZ) - sp.f.y * sp.f.y * sXZ * sXZ;
    let sStateless = clamp((sp.f.z - sJf) * sp.f.w, 0.0, 1.0) * lace * bu;
    let sKept = smoothstep(0.04, 0.6, sPersist) * (0.55 + 0.45 * lace);
    foam = mix(sStateless, sKept, sNear);
    let sDen = mix(smoothstep(0.1, 0.9, swf.w) * swv.z, sFoamTex.b, sNear);
    var sStr = mix(0.4, spLace(wxz, t, sFw), 1.0 - smoothstep(`+X.fadeNear.toFixed(1)+", "+X.fadeFar.toFixed(1)+`, dist));
    sStr *= mix(0.55, 1.0, smoothstep(0.1, 0.6, sDen));
    let sThin = smoothstep(0.02, 0.5, sDen); let sBody = smoothstep(0.5, 1.0, sDen);
    sOp = sThin * mix(`+X.thinBase.toFixed(2)+" + "+X.thinLace.toFixed(2)+" * sStr, "+X.bodyBase.toFixed(2)+" + "+X.bodyLace.toFixed(2)+` * sStr, sBody);
    sOp *= (1.0 - smoothstep(1500.0, 6000.0, dist)) * min(ocean.p[3].w, 1.0);
    sAlb = mix(0.8, 1.0, sStr);
`;function ue(){let t="";return t+=`fn spLine(p: vec2f, sc: f32, w: f32, pw: f32, mean: f32, fw: vec2f) -> f32 {
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
`,t}function we(t,f){let g=t;return g=D(g,"@location(4) depth0: f32 };","@location(4) depth0: f32, @location(5) rest: vec2f };"),g=M(g,"  var p = vec3f(xz.x, level, xz.y);","  let ph = fract(t * ocean.p[0].w / 9.0",a()),g=D(g,"out.depth0 = depth0;","out.depth0 = depth0; out.rest = xz;"),g=D(g,`  let N = od(normalize(in.normal), in.world.xz, t, ocean.p[9].xy);
`,le()),g=D(g,"  if (dist < 250.0) {","  if (dist < 6000.0) {"),g=D(g,`    foam = (1.0 - smoothstep(0.12, 0.35, in.jacobian)) * lace * bu;
`,te),g=D(g,"(1.0 - smoothstep(80.0, 250.0, dist))","(1.0 - smoothstep(1500.0, 6000.0, dist))"),g=D(g,"  return vec4f(color, alpha);",`  if (sp.k[1].w > 0.0) { let hh = clamp(swf.x / sp.k[1].w, -1.0, 1.0); color = select(vec3f(0.25), select(mix(vec3f(1.0), vec3f(1.0, 0.15, 0.1), hh), mix(vec3f(1.0), vec3f(0.1, 0.3, 1.0), -hh), hh < 0.0), swv.z > 0.0); alpha = 1.0; }
  return vec4f(color, alpha);`),g=D(g,"  color = mix(color, fl, foam); alpha = mix(alpha, 1.0, foam);",`  let sAer = swv.z * smoothstep(0.45, 1.0, swf.w);
  let sTurq = vec3f(0.05, 0.42, 0.44) * (ambient * 0.9 + sunCol * 0.05);
  color = mix(color, sTurq, sAer * `+X.glow.toFixed(2)+` * (1.0 - foam)); alpha = mix(alpha, 1.0, sAer * 0.5);
  color = mix(color, fl, foam); alpha = mix(alpha, 1.0, foam);
  let sSun = smoothstep(0.0, 0.55, dot(N, L));
  let sWfl = fl * mix(vec3f(0.66, 0.76, 0.9), vec3f(1.0), sSun) * sAlb + ocean.p[4].xyz * sunCol * 0.2 * max(dot(N, L), 0.0) * sAlb;
  color = mix(color, sWfl, sOp) + spec * sOp * 0.25; alpha = mix(alpha, 1.0, sOp);`),g=D(g,"(ambient * 0.55 + sunCol * 0.04 * max(dot(N, L), 0.0))","(ambient * 1.4 + sunCol * 0.2 * max(dot(N, L), 0.0) + vec3f(0.05))"),g+`
`+d(f)+`
`+ue()}window.__gosx_scene3d_ocean_fft_surface={wgsl:we,SLOT:ie}})(),(function(){const ie={disp:4,slope:8,foam:12,wake:13},X={edgeCells:8,k:.2,slope:3,slickLo:.04,slickHi:.4,slickFine:.85,slickMid:.4,glow:.3,thinBase:.12,thinLace:.3,bodyBase:.32,bodyLace:.3,fadeNear:120,fadeFar:700};function M(t,f,g){if(t.indexOf(f)<0)throw new Error("ocean-fft: anchor not found: "+f.slice(0,48));return t.replace(f,()=>g)}function D(t,f,g,W){const de=t.indexOf(f);if(de<0)throw new Error("ocean-fft: anchor not found: "+f.slice(0,48));const be=t.indexOf(g,de);if(be<0)throw new Error("ocean-fft: anchor not found: "+g.slice(0,48));return t.slice(0,de)+W+t.slice(be)}function d(){let t=`uniform vec4 u_sp[12];
`;for(let f=0;f<4;f++)t+="uniform sampler2D u_spDisp"+f+`;
`;for(let f=0;f<4;f++)t+="uniform sampler2D u_spSlope"+f+`;
`;return t+=`uniform sampler2D u_spFoam;
`,t+=`uniform sampler2D u_spWake;
`,t+=`vec3 spWake(vec2 rest, vec2 cam) {
`,t+=`  vec2 r = (rest - cam - u_sp[10].xy) / max(u_sp[11].x, 1e-3);
`,t+=`  float edge = min(min(r.x, u_sp[11].z - r.x), min(r.y, u_sp[11].z - r.y));
`,t+="  return vec3((r + u_sp[10].zw) / u_sp[11].z, smoothstep(0., "+X.edgeCells.toFixed(1)+`, edge) * u_sp[11].y);
`,t+=`}
`,t+=`float spLod(vec4 c, float spacing) { return max(log2(max(spacing, 1e-3) * c.z / c.x), 0.); }
`,t}function a(){let t=`  vec3 p = vec3(xz.x, level, xz.y), dx = vec3(1., 0., 0.), dz = vec3(0., 0., 1.);
`;t+=`  float amp = u_sp[5].x;
`,t+=`  float spacing = max(u_ocean[8].z * u_ocean[8].w * exp(u_ocean[8].w * float(ring)), r * 6.2831853 / float(segs));
`;for(let f=0;f<4;f++)t+="  if (u_sp["+f+`].w > 0.5) {
`,t+="    vec2 suv = (xz - u_sp["+(6+f)+"].xy) / u_sp["+f+"].x; float slod = spLod(u_sp["+f+"], spacing); float sk = smoothstep(0., 1.2, depth0 * u_sp["+f+`].y);
`,t+="    vec4 sa = textureLod(u_spDisp"+f+", suv, slod), sb = textureLod(u_spSlope"+f+`, suv, slod);
`,t+=`    p += sa.xyz * sk; dx += vec3(sb.z, sb.x, sa.w) * sk; dz += vec3(sa.w, sb.y, sb.w) * sk;
`,t+=`  }
`;return t+=`  vec3 swv = spWake(xz, u_ocean[7].xz);
`,t+=`  if (u_sp[11].w > 0.) { p = vec3(xz.x, level, xz.y); dx = vec3(1., 0., 0.); dz = vec3(0., 0., 1.); }
`,t+=`  if (swv.z > 0. && u_sp[11].w <= 0.) {
`,t+=`    vec4 swk = textureLod(u_spWake, swv.xy, max(log2(max(spacing, 1e-3) / max(u_sp[11].x, 1e-3)), 0.));
`,t+="    float sws = swv.z * smoothstep(0., 1.2, depth0 * "+X.k.toFixed(2)+`);
`,t+=`    p.y += swk.x * sws; dx.y += swk.y * sws; dz.y += swk.z * sws;
`,t+=`  }
`,t}function le(){let t=`  float sGx = 0., sGz = 0., sXX = 0., sZZ = 0., sXZ = 0.;
`;t+=`  vec2 wxz = v_rest - u_sp[5].yz;
`,t+=`  vec2 sFw = fwidth(wxz);
`,t+=`  float sOp = 0., sAlb = 1.;
`,t+=`  vec3 swv = spWake(v_rest, u_ocean[7].xz);
`,t+=`  vec4 swf = texture(u_spWake, swv.xy);
`,t+="  float sSlick = swv.z * smoothstep("+X.slickLo.toFixed(2)+", "+X.slickHi.toFixed(2)+`, swf.w);
`;for(let f=0;f<4;f++){const g=f>=2?X.slickFine:f===1?X.slickMid:0;t+="  if (u_sp["+f+`].w > 0.5) {
`,t+="    vec2 suv = (v_rest - u_sp["+(6+f)+"].xy) / u_sp["+f+"].x; float sk = smoothstep(0., 1.2, v_d0 * u_sp["+f+"].y)"+(g?" * (1. - "+g.toFixed(2)+" * sSlick)":"")+`;
`,t+="    vec4 sa = texture(u_spDisp"+f+", suv), sb = texture(u_spSlope"+f+`, suv);
`,t+=`    sGx += sb.x * sk; sGz += sb.y * sk; sXX += sb.z * sk; sZZ += sb.w * sk; sXZ += sa.w * sk;
`,t+=`  }
`}return t+="  float swm = swv.z * smoothstep(0., 1.2, v_d0 * "+X.k.toFixed(2)+`);
`,t+="  sGx += swf.y * swm * "+X.slope.toFixed(2)+"; sGz += swf.z * swm * "+X.slope.toFixed(2)+`;
`,t+=`  float sJ = (1. + sXX) * (1. + sZZ) - sXZ * sXZ;
`,t+=`  vec4 sFoamTex = textureLod(u_spFoam, wxz * u_sp[4].x, 0.);
`,t+=`  float sPersist = sFoamTex.r;
`,t+=`  vec3 N = normalize(vec3(sGz * sXZ - (1. + sZZ) * sGx, max(sJ, 0.1), sXZ * sGx - sGz * (1. + sXX)));
`,t}const te=`    float sNear = 1. - smoothstep(0.7 * u_sp[5].w, u_sp[5].w, length(v_w.xz - u_ocean[7].xz));
    float sJf = (1. + u_sp[4].y * sXX) * (1. + u_sp[4].y * sZZ) - u_sp[4].y * u_sp[4].y * sXZ * sXZ;
    float sStateless = clamp((u_sp[4].z - sJf) * u_sp[4].w, 0., 1.) * lace * bu;
    float sKept = smoothstep(0.04, 0.6, sPersist) * (0.55 + 0.45 * lace);
    foam = mix(sStateless, sKept, sNear);
    float sDen = mix(smoothstep(0.1, 0.9, swf.w) * swv.z, sFoamTex.b, sNear);
    float sStr = mix(0.4, spLace(wxz, t, sFw), 1. - smoothstep(`+X.fadeNear.toFixed(1)+", "+X.fadeFar.toFixed(1)+`, dist));
    sStr *= mix(0.55, 1., smoothstep(0.1, 0.6, sDen));
    float sThin = smoothstep(0.02, 0.5, sDen), sBody = smoothstep(0.5, 1., sDen);
    sOp = sThin * mix(`+X.thinBase.toFixed(2)+" + "+X.thinLace.toFixed(2)+" * sStr, "+X.bodyBase.toFixed(2)+" + "+X.bodyLace.toFixed(2)+` * sStr, sBody);
    sOp *= (1. - smoothstep(1500., 6000., dist)) * min(u_ocean[3].w, 1.);
    sAlb = mix(0.8, 1., sStr);
`;function ue(){let t="";return t+=`float spLine(vec2 p, float sc, float w, float pw, float mean, vec2 fw) {
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
`,t}function we(t,f){let g=t;g=M(g,"out float v_d0;","out float v_d0; out vec2 v_rest;"),g=M(g,"void main() {",d()+"void main() {"),g=D(g,"  vec3 p = vec3(xz.x, level, xz.y), dx","  float ph = fract(t * u_ocean[0].w / 9.",a()),g=M(g,"v_d0 = depth0;","v_d0 = depth0; v_rest = xz;");let W=f;return W=M(W,"in float v_d0;","in float v_d0; in vec2 v_rest;"),W=M(W,"void main() {",d()+ue()+"void main() {"),W=M(W,`  vec3 N = od(normalize(v_n), v_w.xz, t, u_ocean[9].xy);
`,le()),W=M(W,"  if (dist < 250.) {","  if (dist < 6000.) {"),W=M(W,`    foam = (1. - smoothstep(0.12, 0.35, v_j)) * lace * bu;
`,te),W=M(W,"(1. - smoothstep(80., 250., dist))","(1. - smoothstep(1500., 6000., dist))"),W=M(W,"  fragColor = vec4(color, alpha);",`  if (u_sp[11].w > 0.) { float hh = clamp(swf.x / u_sp[11].w, -1., 1.); color = swv.z > 0. ? (hh < 0. ? mix(vec3(1.), vec3(.1, .3, 1.), -hh) : mix(vec3(1.), vec3(1., .15, .1), hh)) : vec3(.25); alpha = 1.; }
  fragColor = vec4(color, alpha);`),W=M(W,"  color = mix(color, fl, foam); alpha = mix(alpha, 1., foam);",`  float sAer = swv.z * smoothstep(0.45, 1.0, swf.w);
  vec3 sTurq = vec3(0.05, 0.42, 0.44) * (ambient * 0.9 + sunCol * 0.05);
  color = mix(color, sTurq, sAer * `+X.glow.toFixed(2)+` * (1. - foam)); alpha = mix(alpha, 1., sAer * 0.5);
  color = mix(color, fl, foam); alpha = mix(alpha, 1., foam);
  float sSun = smoothstep(0., 0.55, dot(N, L));
  vec3 sWfl = fl * mix(vec3(0.66, 0.76, 0.9), vec3(1.), sSun) * sAlb + u_ocean[4].xyz * sunCol * 0.2 * max(dot(N, L), 0.) * sAlb;
  color = mix(color, sWfl, sOp) + spec * sOp * 0.25; alpha = mix(alpha, 1., sOp);`),W=M(W,"(ambient * 0.55 + sunCol * 0.04 * max(dot(N, L), 0.))","(ambient * 1.4 + sunCol * 0.2 * max(dot(N, L), 0.) + vec3(0.05))"),{vertex:g,fragment:W}}window.__gosx_scene3d_ocean_fft_surface_gl={glsl:we,UNIT:ie}})(),(function(){const ie=window.__gosx_elio_graph,X=window.__gosx_scene3d_elio_ocean,M=window.__gosx_scene3d_ocean_fft_surface,D=window.__gosx_scene3d_ocean_fft_surface_gl,d=.25,a=3.5,le=240,te=.3,ue=function(e){return Math.max(.3,.14*e*e/9.81)},we=48,t=4;let f=null,g=null;function W(e){return e==="low"||e==="medium"?e:"high"}function de(e){const r=[];let s={cell:.5,width:1024};for(const u of ie.expand(e)){const c=u.uniforms&&u.uniforms[0]&&u.uniforms[0].values;u.kernel==="ocean_spectrum"&&c?r.push({tile:c.tile,kMin:c.kMin,kMax:c.kMax,n:c.n}):u.kernel==="ocean_foam"&&c&&(s={cell:c.cell,width:c.width})}return{cascades:r.slice(0,t),foam:s}}function be(e,r){const s=r.graphParams;if(s)for(const u of Object.keys(s))Number.isFinite(s[u])&&e.hasParam(u)&&e.setValue(u,s[u]);Number.isFinite(r.graphTween)&&e.setTween(r.graphTween),e.setNodes(r.graphNodes||{}),b(e,r)}const o=[["hullX","x"],["hullZ","z"],["hullHeading","heading"],["hullSpeed","speed"]];function b(e,r){const s=r.graphNodes&&r.graphNodes.hull;if(!s)return;const u=e.nodePose?e.nodePose(s):null;if(u)for(const c of o){const E=e.graph.params.find(function(B){return B.name===c[0]});E&&E.source.kind==="value"&&e.setValue(c[0],u[c[1]])}}function A(e,r,s,u,c){s.fill(0);const E=e.exportParams(),B=r.foam;r.cascades.forEach(function(H,ke){s[ke*4]=H.tile,s[ke*4+1]=Math.sqrt(Math.max(H.kMin,1e-6)*H.kMax),s[ke*4+2]=H.n,s[ke*4+3]=1,s[24+ke*4]=E["waterModX"+ke]||0,s[24+ke*4+1]=E["waterModZ"+ke]||0});const $=B.cell*B.width,pe=function(H){return H-Math.floor(H/$)*$},G=e.export("wake");if(G){const H=c?e.value("cameraX")-(c.x||0):0,ke=c?e.value("cameraZ")-(c.z||0):0;s[40]=(E.wakeCamRelX||0)+H,s[41]=(E.wakeCamRelZ||0)+ke,s[42]=E.wakeOriginModX||0,s[43]=E.wakeOriginModZ||0,s[44]=E.wakeCell||0,s[45]=E.wakeOn||0,s[46]=G.width,s[47]=u&&u.wake&&u.wake.debug?te:0}s[16]=1/$,s[17]=1,s[18]=e.value("foamThreshold"),s[19]=a,s[20]=ue(e.value("windSpeed")),s[21]=pe(e.value("waterX")),s[22]=pe(e.value("waterZ")),s[23]=le}function F(e,r){const s=Number.isFinite(r.time)?r.time:0,u=e.last===null?0:Math.max(0,s-e.last),c=Math.min(d,u);e.last=s;const E=r.camera||{};return{time:s,dt:c,accumulateDt:u,cameraX:E.x||0,cameraY:E.y||0,cameraZ:E.z||0}}function v(e){const r=window.__gosx_scene3d_node_poses;return r&&r[e]?r[e]:null}function _(e,r){const s=e&&r();if(!s||!(Math.abs(s.value("driftX"))+Math.abs(s.value("driftZ"))>0))return e;const u=e.speed*Math.sin(e.heading)-s.value("driftX"),c=e.speed*Math.cos(e.heading)-s.value("driftZ");return{x:e.x,z:e.z,heading:e.heading,speed:Math.hypot(u,c)}}function h(){return g}function Z(){return f&&!f.failed?f.gpuTiming():null}const y={x:0,z:0,time:0,scale:1,driftX:0,driftZ:0,gen:0};let re=0;function Te(e){if(e.failed){g=null;return}y.x=e.value("waterX"),y.z=e.value("waterZ"),y.time=e.value("simTime"),y.gen=e.waterGen,y.scale=e.value("timeScale"),y.driftX=e.value("driftX"),y.driftZ=e.value("driftZ"),g=y}function ze(e){const r=g;return P.arr=e,r?(e[1]=r.time,e[2]=r.x,e[3]=r.z,e[4]=1,e[5]=r.scale,e[6]=r.driftX,e[7]=r.driftZ,e[9]=r.gen,l(e,r),!0):!1}const S={COUNT:10,SEQ:11,GEN:12,FLAGS:13,N:14,LAT:15,AT:16,TICK:17,PTS:18,OUT:146,REQ:338,ECHO:339},z=1,x=4,P={arr:null,pending:null,ready:null,delivered:null,failed:"",reported:"",tick:0};let O=0;function V(){const e=P.arr;return e?Math.max(0,Math.min(K,Math.floor(e[S.COUNT])||0)):0}function I(e,r){const s={heights:[],nx:[],nz:[]};for(let u=0;u<r;u++)s.heights.push(e[u*4]),s.nx.push(e[u*4+1]),s.nz.push(e[u*4+2]);return s}function w(e){e.tick=P.tick,e.request=P.arr[S.REQ],P.pending=e}function n(e,r){e.seq!==void 0&&(!P.ready||P.ready.meta.seq<e.seq)&&(P.ready=I(r,e.n),P.ready.meta=e)}function l(e,r){const s=P,u=e[0]+1,c=s.pending;c&&(s.pending=null,c.tick===s.tick&&c.seq===void 0?(c.seq=u,c.gen=r.gen,c.start&&c.start()):c.drop&&c.drop());const E=s.ready;if(E&&E!==s.delivered){for(let B=0;B<E.meta.n;B++)e[S.OUT+3*B]=E.heights[B],e[S.OUT+3*B+1]=E.nx[B],e[S.OUT+3*B+2]=E.nz[B];e[S.SEQ]=E.meta.seq,e[S.GEN]=E.meta.gen,e[S.FLAGS]=z,e[S.N]=E.meta.n,e[S.LAT]=u-E.meta.seq,e[S.ECHO]=E.meta.request,e[S.AT]=performance.timeOrigin+performance.now(),e[S.TICK]++,s.delivered=E}else s.failed&&s.reported!==s.failed&&V()&&(e[S.SEQ]=u,e[S.GEN]=r.gen,e[S.FLAGS]=x,e[S.N]=0,e[S.LAT]=0,e[S.ECHO]=e[S.REQ],e[S.AT]=performance.timeOrigin+performance.now(),e[S.TICK]++,s.reported=s.failed);s.failed||(s.reported="")}function q(){Object.assign(P,{arr:null,pending:null,ready:null,delivered:null,failed:"",reported:""})}const R=3,ge=250,K=64,Y=1,xe=8,ve=16,oe={collect:[],k:0,latest:null,previous:null};function Me(){oe.collect=[],oe.k=0}function De(e,r,s,u){const c=oe.k++;oe.collect.length<K&&oe.collect.push({x:e,z:r});const E=Number.isFinite(u)?u:typeof performance<"u"?performance.now():0,B=oe.latest;if(!B||c>=B.count||E-B.at>ge||Math.abs(B.points[c].x-e)>8||Math.abs(B.points[c].z-r)>8)return null;const $=oe.previous&&oe.previous.count>c?oe.previous:null,pe=$&&B.at>$.at?(B.heights[c]-$.heights[c])/((B.at-$.at)/1e3):0,G=B.heights[c]+pe*((E-B.at)/1e3),H=B.nx[c],ke=B.nz[c],j=Math.sqrt(Math.max(0,1-H*H-ke*ke));return{x:e,y:s+G,z:r,normal:{x:H,y:j,z:ke},velocity:{x:0,y:pe,z:0}}}function i(e,r){const s=[];for(let c=0;c<R;c++)s.push({buffer:e.createBuffer({label:"gosx-ocean-fft-probe-"+c,size:K*ve,usage:Y|xe}),state:"free",meta:null});const u=function(c){const E=c.buffer,B=c.meta;c.state="mapping",E.mapAsync(Y).then(function(){if(c.state!=="mapping")return;const $=new Float32Array(E.getMappedRange().slice(0,B.count*ve));E.unmap();const pe=I($,B.count);oe.previous=oe.latest,oe.latest={at:B.at,count:B.count,points:B.points,heights:pe.heights,nx:pe.nx,nz:pe.nz},c.state="free"},function(){c.state="free"})};return{poll:function(){for(const c of s)c.state==="copied"&&u(c)},copy:function(c,E,B){const $=s.find(function(pe){return pe.state==="free"});$&&(c.copyBufferToBuffer(E,0,$.buffer,0,K*ve),$.state="copied",$.meta={at:r(),count:B.length,points:B})},dispose:function(){for(const c of s)c.state="disposed",c.buffer.destroy()}}}function m(e,r){if(!e.hasParam("probePoints"))return!1;const s=Math.min(r.length,K);if(!s)return e.setValue("probeCount",0),!1;let u=0,c=0;for(let B=0;B<s;B++)u+=r[B].x,c+=r[B].z;u/=s,c/=s;const E=new Float32Array(K*4);for(let B=0;B<s;B++)E[B*4]=r[B].x-u,E[B*4+1]=r[B].z-c;return e.setValue("probeOriginX",u),e.setValue("probeOriginZ",c),e.setValue("probeCount",s),e.setValue("probePoints",E),!0}function k(e,r,s){const u=function(c,E){const B=g;if(!B)return e.sample(r,c,E,s);const $=e.sample(r,c-B.x,E-B.z,B.time);return $.x+=B.x,$.z+=B.z,$};return{sample:u,probed:function(c,E){return De(c,E,r.data[0])||u(c,E)},begin:Me}}function p(e){const r=e.device,s="gosx-ocean-fft-",u=function(){const T=GPUShaderStage.VERTEX|GPUShaderStage.FRAGMENT,ee=[];ee.push({binding:M.SLOT.uniform,visibility:T,buffer:{type:"uniform"}},{binding:M.SLOT.sampler,visibility:T,sampler:{type:"filtering"}});for(let C=0;C<t;C++)ee.push({binding:M.SLOT.disp+C,visibility:T,texture:{sampleType:"float"}},{binding:M.SLOT.slope+C,visibility:T,texture:{sampleType:"float"}});return ee.push({binding:M.SLOT.foam,visibility:T,texture:{sampleType:"float"}},{binding:M.SLOT.wake,visibility:T,texture:{sampleType:"float"}}),ee},c=r.createBindGroupLayout({label:s+"surface",entries:u()}),E=r.createSampler({label:s+"sampler",magFilter:"linear",minFilter:"linear",mipmapFilter:"linear",addressModeU:"repeat",addressModeV:"repeat"}),B=r.createBuffer({label:s+"surface",size:we*4,usage:GPUBufferUsage.UNIFORM|GPUBufferUsage.COPY_DST}),$=r.createTexture({label:s+"dummy",size:[1,1,1],format:"rgba16float",usage:GPUTextureUsage.TEXTURE_BINDING|GPUTextureUsage.COPY_DST}),pe=$.createView(),G=new Float32Array(we),H={last:null,tier:"",runner:null,info:null,viewList:null},ke=i(r,function(){return e.now?e.now():typeof performance<"u"?performance.now():0}),j={pipeline:null,building:!1,bgl:null,params:null,pts:null,out:null,slots:[],f32:new Float32Array(20),ptsData:new Float32Array(K*4),group:null,views:null},U={update:function(T,ee,C){const fe=V();if(!fe||P.failed||!C)return;let ne=j.slots.find(function(ae){return ae.state==="free"});if(!(!ne&&j.slots.length>=R))try{if(j.bgl||(j.bgl=r.createBindGroupLayout({label:s+"probe",entries:[{binding:0,visibility:GPUShaderStage.COMPUTE,buffer:{type:"uniform"}},{binding:1,visibility:GPUShaderStage.COMPUTE,buffer:{type:"read-only-storage"}},{binding:2,visibility:GPUShaderStage.COMPUTE,buffer:{type:"storage"}},{binding:3,visibility:GPUShaderStage.COMPUTE,sampler:{type:"filtering"}}].concat([4,5,6,7,8,9,10,11].map(function(me){return{binding:me,visibility:GPUShaderStage.COMPUTE,texture:{sampleType:"float"}}}))}),j.params=r.createBuffer({label:s+"probe-params",size:80,usage:GPUBufferUsage.UNIFORM|GPUBufferUsage.COPY_DST}),j.pts=r.createBuffer({label:s+"probe-points",size:K*ve,usage:GPUBufferUsage.STORAGE|GPUBufferUsage.COPY_DST}),j.out=r.createBuffer({label:s+"probe-out",size:K*ve,usage:GPUBufferUsage.STORAGE|GPUBufferUsage.COPY_SRC})),!j.pipeline){if(!j.building){j.building=!0;const me=typeof r.pushErrorScope=="function",ce=function(Ae){P.failed="probe-pipeline: "+String(Ae&&Ae.message||Ae)};me&&r.pushErrorScope("validation");try{const Ae={label:"gosx-probe-ocean",layout:r.createPipelineLayout({bindGroupLayouts:[j.bgl]}),compute:{module:r.createShaderModule({label:"gosx-probe-ocean",code:ye()}),entryPoint:"main"}};(r.createComputePipelineAsync?r.createComputePipelineAsync(Ae):Promise.resolve(r.createComputePipeline(Ae))).then(function(Ue){j.pipeline=Ue},ce)}catch(Ae){ce(Ae)}me&&r.popErrorScope().then(function(Ae){Ae&&ce(Ae)},ce)}return}ne||(ne={state:"free",buffer:r.createBuffer({label:s+"probe-staging",size:K*ve,usage:Y|xe})},j.slots.push(ne));const ae=P.arr,Be=ee.exportParams(),_e=j.f32,qe=Number(T.ocean&&T.ocean.level)||0,Le=[];_e.fill(0),new Uint32Array(_e.buffer)[0]=fe,_e[1]=qe,C.cascades.forEach(function(me,ce){_e[4+ce*4]=me.tile,_e[5+ce*4]=1,_e[6+ce*4]=Be["waterModX"+ce]||0,_e[7+ce*4]=Be["waterModZ"+ce]||0}),j.ptsData.fill(0);for(let me=0;me<fe;me++)j.ptsData[me*4]=ae[S.PTS+2*me],j.ptsData[me*4+1]=ae[S.PTS+2*me+1];r.queue.writeBuffer(j.params,0,_e),r.queue.writeBuffer(j.pts,0,j.ptsData);for(let me=0;me<t;me++){const ce=ee.export("disp"+me),Ae=ee.export("slope"+me);Le.push(ce?ce.view:pe,Ae?Ae.view:pe)}if(!j.group||Le.some(function(me,ce){return me!==j.views[ce]})){const me=[{binding:0,resource:{buffer:j.params}},{binding:1,resource:{buffer:j.pts}},{binding:2,resource:{buffer:j.out}},{binding:3,resource:E}];for(let ce=0;ce<t;ce++)me.push({binding:4+ce,resource:Le[2*ce]},{binding:8+ce,resource:Le[2*ce+1]});j.group=r.createBindGroup({label:s+"probe",layout:j.bgl,entries:me}),j.views=Le}const Ce=T.encoder.beginComputePass({label:s+"probe"});Ce.setPipeline(j.pipeline),Ce.setBindGroup(0,j.group),Ce.dispatchWorkgroups(1),Ce.end(),T.encoder.copyBufferToBuffer(j.out,0,ne.buffer,0,K*ve);const Fe=ne,Ge={n:fe};ne.state="copied",Ge.start=function(){Fe.state="mapping",Fe.buffer.mapAsync(Y).then(function(){if(Fe.state!=="mapping")return;const me=new Float32Array(Fe.buffer.getMappedRange().slice(0));Fe.buffer.unmap(),Fe.state="free",n(Ge,me)},function(){Fe.state="free"})},Ge.drop=function(){Fe.state="free"},w(Ge)}catch(ae){P.failed=String(ae&&ae.message||ae)}},dispose:function(){for(const T of j.slots)T.state="disposed",T.buffer.destroy();for(const T of[j.params,j.pts,j.out])T&&T.destroy()}},se={device:r,ready:!1,layout:c,group:null,disposed:!1,tier:"",update:Ee,dispose:Pe,fail:Oe};function Oe(T){const ee="ocean-fft: "+(T&&T.message?T.message:String(T));e.guard&&e.guard.fail("gosx-ocean-fft-surface",ee)}function Xe(T){H.runner&&H.runner.dispose();const ee=ie.create({module:X,backend:"webgpu",tier:T,host:{device:r,create:e.create,validation:e.validation,nodePose:function(C){return _(v(C),function(){return H.runner})}}});ee.waterGen=++re,H.runner=ee,H.tier=T,H.last=null,H.viewList=null,se.group=null,se.ready=!1,H.info=ee.graph?de(ee.graph):null}function he(){const T=H.runner,ee=[],C=[{binding:M.SLOT.uniform,resource:{buffer:B}},{binding:M.SLOT.sampler,resource:E}];for(let ae=0;ae<t;ae++){const Be=T.export("disp"+ae),_e=T.export("slope"+ae);C.push({binding:M.SLOT.disp+ae,resource:Be?Be.view:pe},{binding:M.SLOT.slope+ae,resource:_e?_e.view:pe}),ee.push(Be?Be.view:pe,_e?_e.view:pe)}const fe=T.export("foam");C.push({binding:M.SLOT.foam,resource:fe?fe.view:pe}),ee.push(fe?fe.view:pe);const ne=T.export("wake");C.push({binding:M.SLOT.wake,resource:ne?ne.view:pe}),ee.push(ne?ne.view:pe),!(H.viewList&&ee.every(function(ae,Be){return ae===H.viewList[Be]}))&&(H.viewList=ee,se.group=r.createBindGroup({label:s+"surface",layout:c,entries:C}))}function Ee(T){const ee=W(T.tier);P.tick=++O,(!H.runner||ee!==H.tier)&&Xe(ee);const C=H.runner;if(C.failed)return se.ready=!1,se.group=null,!1;be(C,T.ocean),ke.poll();const fe=oe.collect.slice(0,K),ne=m(C,fe),ae=C.run(Object.assign(F(H,T),{encoder:T.encoder}));if(C.failed)return se.ready=!1,se.group=null,g=null,e.guard&&e.guard.fail("gosx-elio-ocean","ocean graph failed: "+C.failed),!1;if(!ae)return se.ready;const Be=ne?C.export("probe"):null;return Be&&Be.buffer&&ke.copy(T.encoder,Be.buffer,fe),he(),A(C,H.info,G,T.ocean,T.camera),r.queue.writeBuffer(B,0,G),Te(C),f=C,U.update(T,C,H.info),se.ready=!0,!0}function Pe(){se.disposed||(se.disposed=!0,se.ready=!1,se.group=null,H.runner&&H.runner.dispose(),g=null,f=null,oe.latest=oe.previous=null,q(),ke.dispose(),U.dispose(),B.destroy(),$.destroy())}return Object.defineProperty(se,"runner",{get:function(){return H.runner}}),se}function N(e,r){const s=r.environment&&r.environment.ocean;let u=e.fft;const c=r.guard&&(r.guard.disabled.has("oceanFFT")||r.guard.disabled.has("elioGraph"));return!s||!s.spectrum||c||u&&u.device!==r.device?(u&&(u.dispose(),e.fft=null),null):(u||(u=e.fft=p(r)),u.update({ocean:s,tier:r.tier,time:r.time,camera:r.camera,encoder:r.encoder}),u)}const J=[{match:"^wake\\.",every:2,hold:"^(wake|camera[XZ])"}];function Q(){return typeof window<"u"&&window.__gosx_elio_diagnostics||{}}function L(){let e=`#version 300 es
precision highp float;
precision highp sampler2D;
uniform vec4 pts[`+K+`], cs[4]; uniform float level;
`;for(let r=0;r<4;r++)e+="uniform sampler2D d"+r+", s"+r+`;
`;e+=`out vec4 o;
void ev(vec2 q, out vec3 s, out vec3 dx, out vec3 dz) {
  s = vec3(0.); dx = vec3(1., 0., 0.); dz = vec3(0., 0., 1.);
`;for(let r=0;r<4;r++)e+="  if (cs["+r+"].y > 0.5) { vec2 uv = (q - cs["+r+"].zw) / cs["+r+"].x; vec4 a = textureLod(d"+r+", uv, 0.), b = textureLod(s"+r+`, uv, 0.); s += a.xyz; dx += vec3(b.z, b.x, a.w); dz += vec3(a.w, b.y, b.w); }
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
`,e}function ye(){let e=`struct E { s: vec3<f32>, dx: vec3<f32>, dz: vec3<f32> };
struct P { count: u32, level: f32, a: f32, b: f32, cs: array<vec4<f32>, 4> };
`;e+=`@group(0) @binding(0) var<uniform> P0: P;
@group(0) @binding(1) var<storage, read> pts: array<vec4<f32>>;
@group(0) @binding(2) var<storage, read_write> outp: array<vec4<f32>>;
@group(0) @binding(3) var smp: sampler;
`;for(let r=0;r<4;r++)e+="@group(0) @binding("+(4+r)+") var d"+r+`: texture_2d<f32>;
@group(0) @binding(`+(8+r)+") var s"+r+`: texture_2d<f32>;
`;e+=`fn ev(q: vec2<f32>) -> E {
  var e: E; e.s = vec3<f32>(0.0); e.dx = vec3<f32>(1.0, 0.0, 0.0); e.dz = vec3<f32>(0.0, 0.0, 1.0);
`;for(let r=0;r<4;r++)e+="  if (P0.cs["+r+"].y > 0.5) { let uv = (q - P0.cs["+r+"].zw) / P0.cs["+r+"].x; let a = textureSampleLevel(d"+r+", smp, uv, 0.0); let b = textureSampleLevel(s"+r+`, smp, uv, 0.0); e.s += a.xyz; e.dx += vec3<f32>(b.z, b.x, a.w); e.dz += vec3<f32>(a.w, b.y, b.w); }
`;return e+=`  return e;
}
@compute @workgroup_size(64) fn main(@builtin(global_invocation_id) g: vec3<u32>) {
  let i = g.x; if (i >= P0.count) { return; }
  let t = pts[i].xy; var q = t; var e: E;
`,e+=`  for (var k = 0; k < 8; k = k + 1) {
    e = ev(q); let ex = q.x + e.s.x - t.x; let ez = q.y + e.s.z - t.y; let det = e.dx.x * e.dz.z - e.dz.x * e.dx.z;
`,e+=`    if (abs(det) > 1e-4) { q = q - vec2<f32>(e.dz.z * ex - e.dz.x * ez, e.dx.x * ez - e.dx.z * ex) / det; } else { q = q - vec2<f32>(ex, ez) * 0.5; }
  }
`,e+=`  e = ev(q); let n = normalize(cross(e.dz, e.dx)); outp[i] = vec4<f32>(P0.level + e.s.y, n.x, n.z, 1.0);
}
`,e}const Se=`#version 300 es
void main() { vec2 p = vec2(float((gl_VertexID << 1) & 2), float(gl_VertexID & 2)); gl_Position = vec4(p * 2. - 1., 0., 1.); }`;function Ze(e,r){const s={last:null},u=ie.create({module:X,backend:"webgl2",tier:W(r.tier),host:{gl:e,caps:r.caps,textureCache:r.textureCache,profile:r.profile||Q().profile,throttle:r.throttle||Q().throttle||J,nodePose:function(U){return _(v(U),function(){return $.runner})}}}),c=new Float32Array(we),E=new Map;u.waterGen=++re;const B=u.graph?de(u.graph):null,$={gl:e,runner:u,tier:W(r.tier),disposed:!1,update:pe,bind:ke,dispose:j,fail:function(U){u.fail(U)}};function pe(U){return u.failed||(P.tick=++O,be(u,U.ocean),!u.run(F(s,U)))?!1:(A(u,B,c,U.ocean,U.camera),Te(u),H(U),!0)}const G={program:null,fbo:null,tex:null,slots:[]};function H(U){for(const T of G.slots){if(T.state!=="fenced")continue;const ee=e.clientWaitSync(T.sync,0,0);if(!(ee!==e.ALREADY_SIGNALED&&ee!==e.CONDITION_SATISFIED&&ee!==e.WAIT_FAILED)){if(T.state="free",e.deleteSync(T.sync),T.sync=null,ee===e.WAIT_FAILED){P.failed="probe-fence";continue}T.meta.seq!==void 0&&(e.bindBuffer(e.PIXEL_PACK_BUFFER,T.pbo),e.getBufferSubData(e.PIXEL_PACK_BUFFER,0,T.dst),e.bindBuffer(e.PIXEL_PACK_BUFFER,null),n(T.meta,T.dst))}}const se=V();if(!se||P.failed)return;const Oe=G.slots.find(function(T){return T.state==="free"})||(G.slots.length<R?null:void 0);if(Oe===void 0)return;const Xe=ie.glState.save(e),he=B.cascades,Ee=u.exportParams(),Pe=Number(U.ocean&&U.ocean.level)||0;try{if(!G.program){const C=[],fe=function(ae,Be){const _e=e.createShader(ae);if(C.push(_e),e.shaderSource(_e,Be),e.compileShader(_e),!e.getShaderParameter(_e,e.COMPILE_STATUS))throw new Error("probe-compile");return _e},ne=e.createProgram();try{if(e.attachShader(ne,fe(e.VERTEX_SHADER,Se)),e.attachShader(ne,fe(e.FRAGMENT_SHADER,L())),e.linkProgram(ne),!e.getProgramParameter(ne,e.LINK_STATUS))throw new Error("probe-link")}catch(ae){throw e.deleteProgram(ne),ae}finally{for(const ae of C)e.deleteShader(ae)}if(G.program=ne,G.tex=e.createTexture(),e.bindTexture(e.TEXTURE_2D,G.tex),e.texStorage2D(e.TEXTURE_2D,1,e.RGBA32F,K,1),G.fbo=e.createFramebuffer(),e.bindFramebuffer(e.FRAMEBUFFER,G.fbo),e.framebufferTexture2D(e.FRAMEBUFFER,e.COLOR_ATTACHMENT0,e.TEXTURE_2D,G.tex,0),e.checkFramebufferStatus(e.FRAMEBUFFER)!==e.FRAMEBUFFER_COMPLETE)throw new Error("probe-target");e.useProgram(ne);for(let ae=0;ae<4;ae++)e.uniform1i(e.getUniformLocation(ne,"d"+ae),D.UNIT.disp+ae),e.uniform1i(e.getUniformLocation(ne,"s"+ae),D.UNIT.slope+ae);G.loc={pts:e.getUniformLocation(ne,"pts[0]"),cs:e.getUniformLocation(ne,"cs[0]"),level:e.getUniformLocation(ne,"level")},G.pts=new Float32Array(K*4),G.cs=new Float32Array(16)}let T=Oe;T||(T={state:"free",pbo:e.createBuffer(),dst:new Float32Array(K*4),sync:null,meta:null},e.bindBuffer(e.PIXEL_PACK_BUFFER,T.pbo),e.bufferData(e.PIXEL_PACK_BUFFER,K*ve,e.STREAM_READ),G.slots.push(T));const ee=P.arr;G.pts.fill(0),G.cs.fill(0);for(let C=0;C<se;C++)G.pts[C*4]=ee[S.PTS+2*C],G.pts[C*4+1]=ee[S.PTS+2*C+1];he.forEach(function(C,fe){G.cs[fe*4]=C.tile,G.cs[fe*4+1]=1,G.cs[fe*4+2]=Ee["waterModX"+fe]||0,G.cs[fe*4+3]=Ee["waterModZ"+fe]||0}),e.bindFramebuffer(e.FRAMEBUFFER,G.fbo),e.viewport(0,0,se,1),e.disable(e.BLEND),e.disable(e.DEPTH_TEST),e.disable(e.CULL_FACE),e.disable(e.SCISSOR_TEST),e.colorMask(!0,!0,!0,!0),e.useProgram(G.program),e.uniform4fv(G.loc.pts,G.pts),e.uniform4fv(G.loc.cs,G.cs),e.uniform1f(G.loc.level,Pe);for(let C=0;C<4;C++){const fe=u.export("disp"+C),ne=u.export("slope"+C);e.activeTexture(e.TEXTURE0+D.UNIT.disp+C),e.bindTexture(e.TEXTURE_2D,fe?fe.tex:null),e.activeTexture(e.TEXTURE0+D.UNIT.slope+C),e.bindTexture(e.TEXTURE_2D,ne?ne.tex:null)}e.drawArrays(e.TRIANGLES,0,3),e.bindBuffer(e.PIXEL_PACK_BUFFER,T.pbo),e.readPixels(0,0,se,1,e.RGBA,e.FLOAT,0),e.bindBuffer(e.PIXEL_PACK_BUFFER,null),T.sync=e.fenceSync(e.SYNC_GPU_COMMANDS_COMPLETE,0),e.flush(),T.state="fenced",T.meta={n:se},w(T.meta)}catch(T){P.failed=String(T&&T.message||T)}finally{ie.glState.restore(e,Xe)}}function ke(U,se){let Oe=E.get(se);if(!Oe){const he=function(Ee){return U.getUniformLocation(se,Ee)};Oe={sp:he("u_sp[0]"),disp:[0,1,2,3].map(function(Ee){return he("u_spDisp"+Ee)}),slope:[0,1,2,3].map(function(Ee){return he("u_spSlope"+Ee)}),foam:he("u_spFoam"),wake:he("u_spWake")},E.set(se,Oe)}const Xe=function(he,Ee,Pe){U.activeTexture(U.TEXTURE0+he),U.bindTexture(U.TEXTURE_2D,Pe?Pe.tex:$.dummy),U.uniform1i(Ee,he)};$.dummy||($.dummy=U.createTexture(),U.bindTexture(U.TEXTURE_2D,$.dummy),U.texStorage2D(U.TEXTURE_2D,1,U.RGBA16F,1,1),U.texParameteri(U.TEXTURE_2D,U.TEXTURE_MIN_FILTER,U.LINEAR),U.texParameteri(U.TEXTURE_2D,U.TEXTURE_MAG_FILTER,U.LINEAR));for(let he=0;he<t;he++)Xe(D.UNIT.disp+he,Oe.disp[he],u.export("disp"+he)),Xe(D.UNIT.slope+he,Oe.slope[he],u.export("slope"+he));Xe(D.UNIT.foam,Oe.foam,u.export("foam")),Xe(D.UNIT.wake,Oe.wake,u.export("wake")),U.uniform4fv(Oe.sp,c)}function j(){if(!$.disposed){$.disposed=!0,g=null,q();for(const U of G.slots)U.sync&&e.deleteSync(U.sync),e.deleteBuffer(U.pbo);G.program&&(e.deleteProgram(G.program),e.deleteFramebuffer(G.fbo),e.deleteTexture(G.tex)),u.dispose(),$.dummy&&e.deleteTexture($.dummy)}}return $}function Re(e,r,s){let u=e.fft;if(u&&(u.gl!==r||u.disposed||u.tier!==W(s.tier))&&(u.dispose(),u=e.fft=null),e.fftDisabled)return null;if(r.isContextLost&&r.isContextLost())return g=null,null;u||(u=e.fft=Ze(r,s));const c=u.update({ocean:s.ocean,time:s.time,camera:s.camera});return u.runner.failed?(e.fftDisabled=u.runner.failed,u.dispose(),e.fft=null,g=null,s.mount&&s.mount.setAttribute&&s.mount.setAttribute("data-gosx-scene3d-ocean-fft-fallback",e.fftDisabled),null):c?u:null}function Ie(e,r,s){e.fft&&e.fft.dispose(),e.fft=null,e.fftDisabled=s,g=null,r&&r.mount&&r.mount.setAttribute&&r.mount.setAttribute("data-gosx-scene3d-ocean-fft-fallback",s)}window.__gosx_scene3d_ocean_fft_api={frame:N,water:h,fill:ze,timing:Z,nodePose:v,waterRelative:_,sampler:k,probe:{begin:Me,sample:De},probeState:oe,describe:de,WAKE_DEBUG_RANGE:te,feed:be,surfaceData:A},window.__gosx_scene3d_ocean_fft_gl_api={run:Re,fail:Ie}})();
