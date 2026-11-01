(function(){"use strict";function U(X){if(X&&typeof X=="object"){for(const F of Object.keys(X))U(X[F]);Object.freeze(X)}return X}window.__gosx_scene3d_elio_ocean=U({elio:"v0.5.0",set:"ocean",sha256:"3164f8e7d5544d0d8812b023cf2baa4774c1f688e98e9935fa8b702a90bd3125",kernels:{oceanSpectrum:{wgsl:`struct OceanSpectrumParams {
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
`,gatherLayout:{kernel:"fft_stage",inputs:[{name:"src",kind:"buffer",elem:"vec4",sampler:"src_tex",length:"src_len",width:"src_w",binding:1}],outputs:[{name:"dst",elem:"vec4",format:"",location:0,variable:"dst_out"}],uniforms:[{name:"fft",block:"FFTStage_ubo",struct:"FFTStage",binding:0}]}}},graphs:{"webgpu/high":{name:"ocean",backend:"webgpu",tier:"high",version:1,params:[{name:"windSpeed",type:"f32",source:{kind:"value",value:9},tween:20},{name:"fetch",type:"f32",source:{kind:"value",value:300},tween:20},{name:"gamma",type:"f32",source:{kind:"value",value:3.3},tween:20},{name:"spread",type:"f32",source:{kind:"value",value:8},tween:20},{name:"swell",type:"f32",source:{kind:"value",value:0},tween:20},{name:"windDirDeg",type:"f32",source:{kind:"value",value:0},tween:20},{name:"swellDirDeg",type:"f32",source:{kind:"value",value:0},tween:20},{name:"seed",type:"u32",source:{kind:"value",value:0}},{name:"loopPeriod",type:"f32",source:{kind:"value",value:1e3}},{name:"lambda",type:"f32",source:{kind:"value",value:1},tween:20},{name:"timeScale",type:"f32",source:{kind:"value",value:1}},{name:"driftX",type:"f32",source:{kind:"value",value:0}},{name:"driftZ",type:"f32",source:{kind:"value",value:0}},{name:"foamTau",type:"f32",source:{kind:"value",value:4}},{name:"foamThreshold",type:"f32",source:{kind:"value",value:.9}},{name:"foamGain",type:"f32",source:{kind:"value",value:2}},{name:"probeCount",type:"u32",source:{kind:"value",value:0}},{name:"probeOriginX",type:"f32",source:{kind:"value",value:0}},{name:"probeOriginZ",type:"f32",source:{kind:"value",value:0}},{name:"probePoints",type:"vec4<f32>[]",count:64,source:{kind:"value",value:0}},{name:"dt",type:"f32",source:{kind:"frame",name:"dt"}},{name:"cameraX",type:"f32",source:{kind:"frame",name:"cameraX"}},{name:"cameraZ",type:"f32",source:{kind:"frame",name:"cameraZ"}},{name:"windDir",type:"f32",source:{args:[1.5707963267948966,{op:"mul",args:["param:windDirDeg",.017453292519943295]}],kind:"expr",op:"sub"}},{name:"swellDir",type:"f32",source:{args:[1.5707963267948966,{op:"mul",args:["param:swellDirDeg",.017453292519943295]}],kind:"expr",op:"sub"}},{name:"simTime",type:"f32",source:{kind:"accumulate",of:"param:timeScale"}},{name:"phaseTime",type:"f32",source:{args:["param:simTime","param:loopPeriod"],kind:"expr",op:"mod"}},{name:"waterX",type:"f32",source:{kind:"accumulate",of:"param:driftX"}},{name:"waterZ",type:"f32",source:{kind:"accumulate",of:"param:driftZ"}},{name:"waterModX0",type:"f32",source:{args:["param:waterX",1600],kind:"expr",op:"mod"}},{name:"waterModZ0",type:"f32",source:{args:["param:waterZ",1600],kind:"expr",op:"mod"}},{name:"waterModX1",type:"f32",source:{args:["param:waterX",250],kind:"expr",op:"mod"}},{name:"waterModZ1",type:"f32",source:{args:["param:waterZ",250],kind:"expr",op:"mod"}},{name:"waterModX2",type:"f32",source:{args:["param:waterX",37],kind:"expr",op:"mod"}},{name:"waterModZ2",type:"f32",source:{args:["param:waterZ",37],kind:"expr",op:"mod"}},{name:"waterModX3",type:"f32",source:{args:["param:waterX",5.3],kind:"expr",op:"mod"}},{name:"waterModZ3",type:"f32",source:{args:["param:waterZ",5.3],kind:"expr",op:"mod"}},{name:"foamOriginX",type:"i32",source:{args:[{op:"floor",args:[{op:"div",args:[{op:"sub",args:["param:cameraX","param:waterX"]},.5]}]},512],kind:"expr",op:"sub"}},{name:"foamShiftX",type:"i32",source:{kind:"delta",of:"param:foamOriginX"}},{name:"foamOriginMod0X",type:"f32",source:{args:[{op:"mul",args:["param:foamOriginX",.5]},1600],kind:"expr",op:"mod"}},{name:"foamOriginMod1X",type:"f32",source:{args:[{op:"mul",args:["param:foamOriginX",.5]},250],kind:"expr",op:"mod"}},{name:"foamOriginMod2X",type:"f32",source:{args:[{op:"mul",args:["param:foamOriginX",.5]},37],kind:"expr",op:"mod"}},{name:"foamOriginMod3X",type:"f32",source:{args:[{op:"mul",args:["param:foamOriginX",.5]},5.3],kind:"expr",op:"mod"}},{name:"probeOriginMod0X",type:"f32",source:{args:[{op:"sub",args:["param:probeOriginX","param:waterX"]},1600],kind:"expr",op:"mod"}},{name:"probeOriginMod1X",type:"f32",source:{args:[{op:"sub",args:["param:probeOriginX","param:waterX"]},250],kind:"expr",op:"mod"}},{name:"probeOriginMod2X",type:"f32",source:{args:[{op:"sub",args:["param:probeOriginX","param:waterX"]},37],kind:"expr",op:"mod"}},{name:"probeOriginMod3X",type:"f32",source:{args:[{op:"sub",args:["param:probeOriginX","param:waterX"]},5.3],kind:"expr",op:"mod"}},{name:"foamOriginZ",type:"i32",source:{args:[{op:"floor",args:[{op:"div",args:[{op:"sub",args:["param:cameraZ","param:waterZ"]},.5]}]},512],kind:"expr",op:"sub"}},{name:"foamShiftZ",type:"i32",source:{kind:"delta",of:"param:foamOriginZ"}},{name:"foamOriginMod0Z",type:"f32",source:{args:[{op:"mul",args:["param:foamOriginZ",.5]},1600],kind:"expr",op:"mod"}},{name:"foamOriginMod1Z",type:"f32",source:{args:[{op:"mul",args:["param:foamOriginZ",.5]},250],kind:"expr",op:"mod"}},{name:"foamOriginMod2Z",type:"f32",source:{args:[{op:"mul",args:["param:foamOriginZ",.5]},37],kind:"expr",op:"mod"}},{name:"foamOriginMod3Z",type:"f32",source:{args:[{op:"mul",args:["param:foamOriginZ",.5]},5.3],kind:"expr",op:"mod"}},{name:"probeOriginMod0Z",type:"f32",source:{args:[{op:"sub",args:["param:probeOriginZ","param:waterZ"]},1600],kind:"expr",op:"mod"}},{name:"probeOriginMod1Z",type:"f32",source:{args:[{op:"sub",args:["param:probeOriginZ","param:waterZ"]},250],kind:"expr",op:"mod"}},{name:"probeOriginMod2Z",type:"f32",source:{args:[{op:"sub",args:["param:probeOriginZ","param:waterZ"]},37],kind:"expr",op:"mod"}},{name:"probeOriginMod3Z",type:"f32",source:{args:[{op:"sub",args:["param:probeOriginZ","param:waterZ"]},5.3],kind:"expr",op:"mod"}},{name:"hullX",type:"f32",source:{kind:"value",value:0}},{name:"hullZ",type:"f32",source:{kind:"value",value:0}},{name:"hullHeading",type:"f32",source:{kind:"value",value:0}},{name:"hullSpeed",type:"f32",source:{kind:"value",value:0}},{name:"wakeEnabled",type:"bool",source:{kind:"value",value:0}},{name:"hullLength",type:"f32",source:{kind:"value",value:200}},{name:"hullBeam",type:"f32",source:{kind:"value",value:30}},{name:"wakeCell",type:"f32",source:{kind:"value",value:4}},{name:"wakeDepth",type:"f32",source:{kind:"value",value:.4}},{name:"wakeGravity",type:"f32",source:{kind:"value",value:9.81}},{name:"wakeAlpha",type:"f32",source:{kind:"value",value:.02}},{name:"wakeMinLambdaCells",type:"f32",source:{kind:"value",value:8}},{name:"wakeFoamGain",type:"f32",source:{kind:"value",value:1}},{name:"wakeFoamTau",type:"f32",source:{kind:"value",value:48}},{name:"wakeDiffuse",type:"f32",source:{kind:"value",value:1.5}},{name:"wakeJitter",type:"f32",source:{kind:"value",value:1}},{name:"wakeBowGain",type:"f32",source:{kind:"value",value:.2}},{name:"wakeBowFoam",type:"f32",source:{kind:"value",value:.9}},{name:"wakeOn",type:"f32",source:{args:["param:wakeEnabled",1],kind:"expr",op:"mul"}},{name:"wakeFoamGainEff",type:"f32",source:{args:["param:wakeEnabled","param:wakeFoamGain"],kind:"expr",op:"mul"}},{name:"wakeEdge",type:"i32",source:{kind:"delta",of:"param:wakeEnabled"}},{name:"wakeReset",type:"u32",source:{args:["param:wakeEdge",0],kind:"expr",op:"max"}},{name:"wakeLength",type:"f32",source:{args:["param:wakeCell",256],kind:"expr",op:"mul"}},{name:"hullWaterX",type:"f32",source:{args:["param:hullX","param:waterX"],kind:"expr",op:"sub"}},{name:"wakeOriginX",type:"i32",source:{args:[{op:"div",args:[{op:"sub",args:[{op:"sub",args:["param:hullWaterX",{op:"mul",args:[{op:"sin",args:["param:hullHeading"]},{op:"mul",args:["param:wakeCell",51.2]}]}]},{op:"mul",args:["param:wakeCell",128]}]},"param:wakeCell"]}],kind:"expr",op:"floor"}},{name:"wakeShiftX",type:"i32",source:{kind:"delta",of:"param:wakeOriginX"}},{name:"wakeHullU",type:"f32",source:{args:["param:hullWaterX",{op:"mul",args:["param:wakeOriginX","param:wakeCell"]}],kind:"expr",op:"sub"}},{name:"wakeOriginModX",type:"i32",source:{args:["param:wakeOriginX",256],kind:"expr",op:"mod"}},{name:"wakeRelX",type:"f32",source:{args:[{op:"mul",args:["param:foamOriginX",.5]},{op:"mul",args:["param:wakeOriginX","param:wakeCell"]}],kind:"expr",op:"sub"}},{name:"wakeCamRelX",type:"f32",source:{args:[{op:"add",args:["param:waterX",{op:"mul",args:["param:wakeOriginX","param:wakeCell"]}]},"param:cameraX"],kind:"expr",op:"sub"}},{name:"hullWaterZ",type:"f32",source:{args:["param:hullZ","param:waterZ"],kind:"expr",op:"sub"}},{name:"wakeOriginZ",type:"i32",source:{args:[{op:"div",args:[{op:"sub",args:[{op:"sub",args:["param:hullWaterZ",{op:"mul",args:[{op:"cos",args:["param:hullHeading"]},{op:"mul",args:["param:wakeCell",51.2]}]}]},{op:"mul",args:["param:wakeCell",128]}]},"param:wakeCell"]}],kind:"expr",op:"floor"}},{name:"wakeShiftZ",type:"i32",source:{kind:"delta",of:"param:wakeOriginZ"}},{name:"wakeHullV",type:"f32",source:{args:["param:hullWaterZ",{op:"mul",args:["param:wakeOriginZ","param:wakeCell"]}],kind:"expr",op:"sub"}},{name:"wakeOriginModZ",type:"i32",source:{args:["param:wakeOriginZ",256],kind:"expr",op:"mod"}},{name:"wakeRelZ",type:"f32",source:{args:[{op:"mul",args:["param:foamOriginZ",.5]},{op:"mul",args:["param:wakeOriginZ","param:wakeCell"]}],kind:"expr",op:"sub"}},{name:"wakeCamRelZ",type:"f32",source:{args:[{op:"add",args:["param:waterZ",{op:"mul",args:["param:wakeOriginZ","param:wakeCell"]}]},"param:cameraZ"],kind:"expr",op:"sub"}},{name:"wakeGEff",type:"f32",source:{args:[{op:"min",args:[{op:"min",args:["param:wakeGravity",{op:"mul",args:[{op:"mul",args:["param:hullSpeed","param:hullSpeed"]},{op:"div",args:[6.283185307179586,{op:"mul",args:["param:wakeMinLambdaCells","param:wakeCell"]}]}]}]},{op:"mul",args:["param:wakeCell",16.39001041697031]}]},.05],kind:"expr",op:"max"}},{name:"wakeDt",type:"f32",source:{args:[{op:"mul",args:["param:dt","param:timeScale"]},.2],kind:"expr",op:"min"}},{name:"wakeSrcLength",type:"f32",source:{args:[{op:"min",args:["param:hullLength",{op:"div",args:[{op:"mul",args:[{op:"mul",args:["param:hullSpeed","param:hullSpeed"]},3.141592653589793]},"param:wakeGEff"]}]},{op:"mul",args:["param:wakeCell",2]}],kind:"expr",op:"max"}},{name:"wakeSrcBeam",type:"f32",source:{args:[{op:"min",args:["param:hullBeam",{op:"mul",args:["param:wakeSrcLength",.67]}]},{op:"mul",args:["param:wakeCell",1.5]}],kind:"expr",op:"max"}}],resources:[{name:"h0_0",kind:"buffer",elementType:"vec4<f32>",count:4096},{name:"omega_0",kind:"buffer",elementType:"f32",count:4096},{name:"fftA_0",kind:"buffer",elementType:"vec4<f32>",count:4096},{name:"fftB_0",kind:"buffer",elementType:"vec4<f32>",count:4096},{name:"dispTex0",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"slopeTex0",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"h0_1",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"omega_1",kind:"buffer",elementType:"f32",count:65536},{name:"fftA_1",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"fftB_1",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"dispTex1",kind:"texture",format:"rgba16float",width:256,height:256,mipLevels:9},{name:"slopeTex1",kind:"texture",format:"rgba16float",width:256,height:256,mipLevels:9},{name:"h0_2",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"omega_2",kind:"buffer",elementType:"f32",count:65536},{name:"fftA_2",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"fftB_2",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"dispTex2",kind:"texture",format:"rgba16float",width:256,height:256,mipLevels:9},{name:"slopeTex2",kind:"texture",format:"rgba16float",width:256,height:256,mipLevels:9},{name:"h0_3",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"omega_3",kind:"buffer",elementType:"f32",count:65536},{name:"fftA_3",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"fftB_3",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"dispTex3",kind:"texture",format:"rgba16float",width:256,height:256,mipLevels:9},{name:"slopeTex3",kind:"texture",format:"rgba16float",width:256,height:256,mipLevels:9},{name:"wakeA",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"wakeB",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"wakeF",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"wakeG",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"wakeC",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"wakeTex",kind:"texture",format:"rgba16float",width:256,height:256,mipLevels:9},{name:"foamA",kind:"buffer",elementType:"f32",count:1048576},{name:"foamB",kind:"buffer",elementType:"f32",count:1048576},{name:"foamTex",kind:"texture",format:"rgba16float",width:1024,height:1024,mipLevels:11},{name:"probePoints",kind:"buffer",elementType:"vec4<f32>",count:64,feed:"probePoints"},{name:"probeOut",kind:"buffer",elementType:"vec4<f32>",count:64}],passes:[{name:"c0.spectrum",kernel:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",dispatch:[64,1,1],bindings:[{name:"h0",resource:"h0_0"},{name:"omega",resource:"omega_0"}],uniforms:[{name:"spec",values:{cascade:0,fetchKm:"param:fetch",gamma:"param:gamma",kMax:.12,kMin:.003926990816987242,loopPeriod:"param:loopPeriod",n:64,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:1600,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c0.evolve",kernel:"ocean_evolve",dispatch:[64,1,1],bindings:[{name:"h0",resource:"h0_0"},{name:"omega",resource:"omega_0"},{name:"outA",resource:"fftA_0"},{name:"outB",resource:"fftB_0"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:64,t:"param:phaseTime",tile:1600}}]},{name:"c0.fftA.fft_rows",kernel:"fft_compute_64",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftA_0"}]},{name:"c0.fftA.fft_cols",kernel:"fft_compute_64",entry:"fft_cols",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftA_0"}]},{name:"c0.fftB.fft_rows",kernel:"fft_compute_64",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftB_0"}]},{name:"c0.fftB.fft_cols",kernel:"fft_compute_64",entry:"fft_cols",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftB_0"}]},{name:"c0.assemble",kernel:"ocean_assemble",dispatch:[64,1,1],bindings:[{name:"fieldA",resource:"fftA_0"},{name:"fieldB",resource:"fftB_0"},{name:"disp",resource:"dispTex0"},{name:"slope",resource:"slopeTex0"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:64}}]},{name:"c0.dispTex0.mips",type:"repeat",count:6,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[16,1,1],bindings:[{name:"src",resource:"dispTex0",levelAt:{index:"l"}},{name:"dst",resource:"dispTex0",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:64},dstWidth:{index:"l",offset:1,shr:64},pad:0,srcLevel:0}}]}]},{name:"c0.slopeTex0.mips",type:"repeat",count:6,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[16,1,1],bindings:[{name:"src",resource:"slopeTex0",levelAt:{index:"l"}},{name:"dst",resource:"slopeTex0",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:64},dstWidth:{index:"l",offset:1,shr:64},pad:0,srcLevel:0}}]}]},{name:"c1.spectrum",kernel:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",dispatch:[1024,1,1],bindings:[{name:"h0",resource:"h0_1"},{name:"omega",resource:"omega_1"}],uniforms:[{name:"spec",values:{cascade:1,fetchKm:"param:fetch",gamma:"param:gamma",kMax:.8,kMin:.12,loopPeriod:"param:loopPeriod",n:256,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:250,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c1.evolve",kernel:"ocean_evolve",dispatch:[1024,1,1],bindings:[{name:"h0",resource:"h0_1"},{name:"omega",resource:"omega_1"},{name:"outA",resource:"fftA_1"},{name:"outB",resource:"fftB_1"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:256,t:"param:phaseTime",tile:250}}]},{name:"c1.fftA.fft_rows",kernel:"fft_compute_256",dispatch:[256,1,1],bindings:[{name:"data",resource:"fftA_1"}]},{name:"c1.fftA.fft_cols",kernel:"fft_compute_256",entry:"fft_cols",dispatch:[256,1,1],bindings:[{name:"data",resource:"fftA_1"}]},{name:"c1.fftB.fft_rows",kernel:"fft_compute_256",dispatch:[256,1,1],bindings:[{name:"data",resource:"fftB_1"}]},{name:"c1.fftB.fft_cols",kernel:"fft_compute_256",entry:"fft_cols",dispatch:[256,1,1],bindings:[{name:"data",resource:"fftB_1"}]},{name:"c1.assemble",kernel:"ocean_assemble",dispatch:[1024,1,1],bindings:[{name:"fieldA",resource:"fftA_1"},{name:"fieldB",resource:"fftB_1"},{name:"disp",resource:"dispTex1"},{name:"slope",resource:"slopeTex1"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:256}}]},{name:"c1.dispTex1.mips",type:"repeat",count:8,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[256,1,1],bindings:[{name:"src",resource:"dispTex1",levelAt:{index:"l"}},{name:"dst",resource:"dispTex1",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:256},dstWidth:{index:"l",offset:1,shr:256},pad:0,srcLevel:0}}]}]},{name:"c1.slopeTex1.mips",type:"repeat",count:8,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[256,1,1],bindings:[{name:"src",resource:"slopeTex1",levelAt:{index:"l"}},{name:"dst",resource:"slopeTex1",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:256},dstWidth:{index:"l",offset:1,shr:256},pad:0,srcLevel:0}}]}]},{name:"c2.spectrum",kernel:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",dispatch:[1024,1,1],bindings:[{name:"h0",resource:"h0_2"},{name:"omega",resource:"omega_2"}],uniforms:[{name:"spec",values:{cascade:2,fetchKm:"param:fetch",gamma:"param:gamma",kMax:5.4,kMin:.8,loopPeriod:"param:loopPeriod",n:256,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:37,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c2.evolve",kernel:"ocean_evolve",dispatch:[1024,1,1],bindings:[{name:"h0",resource:"h0_2"},{name:"omega",resource:"omega_2"},{name:"outA",resource:"fftA_2"},{name:"outB",resource:"fftB_2"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:256,t:"param:phaseTime",tile:37}}]},{name:"c2.fftA.fft_rows",kernel:"fft_compute_256",dispatch:[256,1,1],bindings:[{name:"data",resource:"fftA_2"}]},{name:"c2.fftA.fft_cols",kernel:"fft_compute_256",entry:"fft_cols",dispatch:[256,1,1],bindings:[{name:"data",resource:"fftA_2"}]},{name:"c2.fftB.fft_rows",kernel:"fft_compute_256",dispatch:[256,1,1],bindings:[{name:"data",resource:"fftB_2"}]},{name:"c2.fftB.fft_cols",kernel:"fft_compute_256",entry:"fft_cols",dispatch:[256,1,1],bindings:[{name:"data",resource:"fftB_2"}]},{name:"c2.assemble",kernel:"ocean_assemble",dispatch:[1024,1,1],bindings:[{name:"fieldA",resource:"fftA_2"},{name:"fieldB",resource:"fftB_2"},{name:"disp",resource:"dispTex2"},{name:"slope",resource:"slopeTex2"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:256}}]},{name:"c2.dispTex2.mips",type:"repeat",count:8,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[256,1,1],bindings:[{name:"src",resource:"dispTex2",levelAt:{index:"l"}},{name:"dst",resource:"dispTex2",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:256},dstWidth:{index:"l",offset:1,shr:256},pad:0,srcLevel:0}}]}]},{name:"c2.slopeTex2.mips",type:"repeat",count:8,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[256,1,1],bindings:[{name:"src",resource:"slopeTex2",levelAt:{index:"l"}},{name:"dst",resource:"slopeTex2",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:256},dstWidth:{index:"l",offset:1,shr:256},pad:0,srcLevel:0}}]}]},{name:"c3.spectrum",kernel:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",dispatch:[1024,1,1],bindings:[{name:"h0",resource:"h0_3"},{name:"omega",resource:"omega_3"}],uniforms:[{name:"spec",values:{cascade:3,fetchKm:"param:fetch",gamma:"param:gamma",kMax:37,kMin:5.4,loopPeriod:"param:loopPeriod",n:256,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:5.3,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c3.evolve",kernel:"ocean_evolve",dispatch:[1024,1,1],bindings:[{name:"h0",resource:"h0_3"},{name:"omega",resource:"omega_3"},{name:"outA",resource:"fftA_3"},{name:"outB",resource:"fftB_3"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:256,t:"param:phaseTime",tile:5.3}}]},{name:"c3.fftA.fft_rows",kernel:"fft_compute_256",dispatch:[256,1,1],bindings:[{name:"data",resource:"fftA_3"}]},{name:"c3.fftA.fft_cols",kernel:"fft_compute_256",entry:"fft_cols",dispatch:[256,1,1],bindings:[{name:"data",resource:"fftA_3"}]},{name:"c3.fftB.fft_rows",kernel:"fft_compute_256",dispatch:[256,1,1],bindings:[{name:"data",resource:"fftB_3"}]},{name:"c3.fftB.fft_cols",kernel:"fft_compute_256",entry:"fft_cols",dispatch:[256,1,1],bindings:[{name:"data",resource:"fftB_3"}]},{name:"c3.assemble",kernel:"ocean_assemble",dispatch:[1024,1,1],bindings:[{name:"fieldA",resource:"fftA_3"},{name:"fieldB",resource:"fftB_3"},{name:"disp",resource:"dispTex3"},{name:"slope",resource:"slopeTex3"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:256}}]},{name:"c3.dispTex3.mips",type:"repeat",count:8,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[256,1,1],bindings:[{name:"src",resource:"dispTex3",levelAt:{index:"l"}},{name:"dst",resource:"dispTex3",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:256},dstWidth:{index:"l",offset:1,shr:256},pad:0,srcLevel:0}}]}]},{name:"c3.slopeTex3.mips",type:"repeat",count:8,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[256,1,1],bindings:[{name:"src",resource:"slopeTex3",levelAt:{index:"l"}},{name:"dst",resource:"slopeTex3",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:256},dstWidth:{index:"l",offset:1,shr:256},pad:0,srcLevel:0}}]}]},{name:"wake.source",kernel:"wake_source",when:"param:wakeEnabled",dispatch:[1024,1,1],bindings:[{name:"wakeIn",resource:"wakeA"},{name:"fftOut",resource:"wakeF"},{name:"cleanOut",resource:"wakeC"}],uniforms:[{name:"wake",values:{bowFoam:"param:wakeBowFoam",bowGain:"param:wakeBowGain",cell:"param:wakeCell",depth:"param:wakeDepth",diffuse:"param:wakeDiffuse",dt:"param:wakeDt",foamSpeed:5,fullSpeed:3,heading:"param:hullHeading",hullBeam:"param:hullBeam",hullLen:"param:hullLength",hullU:"param:wakeHullU",hullV:"param:wakeHullV",jitter:"param:wakeJitter",originX:"param:wakeOriginX",originZ:"param:wakeOriginZ",reset:"param:wakeReset",shiftX:"param:wakeShiftX",shiftZ:"param:wakeShiftZ",speed:"param:hullSpeed",srcBeam:"param:wakeSrcBeam",srcLength:"param:wakeSrcLength",tau:"param:wakeFoamTau",width:256}}]},{name:"wake.fft1.fft_rows",kernel:"fft_compute_256",when:"param:wakeEnabled",dispatch:[256,1,1],bindings:[{name:"data",resource:"wakeF"}]},{name:"wake.fft1.fft_cols",kernel:"fft_compute_256",entry:"fft_cols",when:"param:wakeEnabled",dispatch:[256,1,1],bindings:[{name:"data",resource:"wakeF"}]},{name:"wake.symbol",kernel:"wake_symbol",when:"param:wakeEnabled",dispatch:[1024,1,1],bindings:[{name:"fftIn",resource:"wakeF"},{name:"fftOut",resource:"wakeG"}],uniforms:[{name:"sym",values:{n:256,window:"param:wakeLength"}}]},{name:"wake.fft2.fft_rows",kernel:"fft_compute_256",when:"param:wakeEnabled",dispatch:[256,1,1],bindings:[{name:"data",resource:"wakeG"}]},{name:"wake.fft2.fft_cols",kernel:"fft_compute_256",entry:"fft_cols",when:"param:wakeEnabled",dispatch:[256,1,1],bindings:[{name:"data",resource:"wakeG"}]},{name:"wake.step",kernel:"wake_step",when:"param:wakeEnabled",dispatch:[1024,1,1],bindings:[{name:"qIn",resource:"wakeG"},{name:"cleanIn",resource:"wakeC"},{name:"stateOut",resource:"wakeB"},{name:"wakeTex",resource:"wakeTex"}],uniforms:[{name:"wstep",values:{alpha:"param:wakeAlpha",cell:"param:wakeCell",dt:"param:wakeDt",gravity:"param:wakeGEff",originX:"param:wakeOriginX",originZ:"param:wakeOriginZ",sponge:24,spongeRate:.8,width:256}}]},{name:"wake.wakeTex.mips",type:"repeat",when:"param:wakeEnabled",count:8,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[256,1,1],bindings:[{name:"src",resource:"wakeTex",levelAt:{index:"l"}},{name:"dst",resource:"wakeTex",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:256},dstWidth:{index:"l",offset:1,shr:256},pad:0,srcLevel:0}}]}]},{name:"foam",kernel:"ocean_foam",dispatch:[16384,1,1],bindings:[{name:"foamIn",resource:"foamA"},{name:"fieldB0",resource:"fftB_0"},{name:"fieldB1",resource:"fftB_1"},{name:"fieldB2",resource:"fftB_2"},{name:"fieldB3",resource:"fftB_3"},{name:"foamOut",resource:"foamB"},{name:"foamTex",resource:"foamTex"},{name:"wakeIn",resource:"wakeB"}],uniforms:[{name:"foam",values:{cascades:4,cell:.5,dt:"param:dt",gain:"param:foamGain",lambda:"param:lambda",n0:64,n1:256,n2:256,n3:256,omX0:"param:foamOriginMod0X",omX1:"param:foamOriginMod1X",omX2:"param:foamOriginMod2X",omX3:"param:foamOriginMod3X",omZ0:"param:foamOriginMod0Z",omZ1:"param:foamOriginMod1Z",omZ2:"param:foamOriginMod2Z",omZ3:"param:foamOriginMod3Z",originX:"param:foamOriginX",originZ:"param:foamOriginZ",shiftX:"param:foamShiftX",shiftZ:"param:foamShiftZ",size0:1600,size1:250,size2:37,size3:5.3,tau:"param:foamTau",threshold:"param:foamThreshold",wakeCell:"param:wakeCell",wakeGain:"param:wakeFoamGainEff",wakeOriginX:"param:wakeOriginModX",wakeOriginZ:"param:wakeOriginModZ",wakeRelX:"param:wakeRelX",wakeRelZ:"param:wakeRelZ",wakeWidth:256,width:1024}}]},{name:"foamTex.mips",type:"repeat",count:10,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[4096,1,1],bindings:[{name:"src",resource:"foamTex",levelAt:{index:"l"}},{name:"dst",resource:"foamTex",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:1024},dstWidth:{index:"l",offset:1,shr:1024},pad:0,srcLevel:0}}]}]},{name:"probe",kernel:"ocean_probe",dispatch:[1,1,1],bindings:[{name:"points",resource:"probePoints"},{name:"disp0",resource:"fftA_0"},{name:"disp1",resource:"fftA_1"},{name:"disp2",resource:"fftA_2"},{name:"disp3",resource:"fftA_3"},{name:"out",resource:"probeOut"}],uniforms:[{name:"probe",values:{cascades:4,count:"param:probeCount",lambda:"param:lambda",n0:64,n1:256,n2:256,n3:256,normalStep:.5,omX0:"param:probeOriginMod0X",omX1:"param:probeOriginMod1X",omX2:"param:probeOriginMod2X",omX3:"param:probeOriginMod3X",omZ0:"param:probeOriginMod0Z",omZ1:"param:probeOriginMod1Z",omZ2:"param:probeOriginMod2Z",omZ3:"param:probeOriginMod3Z",size0:1600,size1:250,size2:37,size3:5.3,t:"param:simTime"}}]}],frameSwaps:[["wakeA","wakeB"],["foamA","foamB"]],exports:{disp0:"dispTex0",disp1:"dispTex1",disp2:"dispTex2",disp3:"dispTex3",foam:"foamTex",probe:"probeOut",slope0:"slopeTex0",slope1:"slopeTex1",slope2:"slopeTex2",slope3:"slopeTex3",wake:"wakeTex"},exportParams:["waterModX0","waterModZ0","waterModX1","waterModZ1","waterModX2","waterModZ2","waterModX3","waterModZ3","wakeCamRelX","wakeCamRelZ","wakeOriginModX","wakeOriginModZ","wakeCell","wakeOn"]},"webgpu/medium":{name:"ocean",backend:"webgpu",tier:"medium",version:1,resources:[{name:"h0_0",kind:"buffer",elementType:"vec4<f32>",count:4096},{name:"omega_0",kind:"buffer",elementType:"f32",count:4096},{name:"fftA_0",kind:"buffer",elementType:"vec4<f32>",count:4096},{name:"fftB_0",kind:"buffer",elementType:"vec4<f32>",count:4096},{name:"dispTex0",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"slopeTex0",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"h0_1",kind:"buffer",elementType:"vec4<f32>",count:16384},{name:"omega_1",kind:"buffer",elementType:"f32",count:16384},{name:"fftA_1",kind:"buffer",elementType:"vec4<f32>",count:16384},{name:"fftB_1",kind:"buffer",elementType:"vec4<f32>",count:16384},{name:"dispTex1",kind:"texture",format:"rgba16float",width:128,height:128,mipLevels:8},{name:"slopeTex1",kind:"texture",format:"rgba16float",width:128,height:128,mipLevels:8},{name:"h0_2",kind:"buffer",elementType:"vec4<f32>",count:16384},{name:"omega_2",kind:"buffer",elementType:"f32",count:16384},{name:"fftA_2",kind:"buffer",elementType:"vec4<f32>",count:16384},{name:"fftB_2",kind:"buffer",elementType:"vec4<f32>",count:16384},{name:"dispTex2",kind:"texture",format:"rgba16float",width:128,height:128,mipLevels:8},{name:"slopeTex2",kind:"texture",format:"rgba16float",width:128,height:128,mipLevels:8},{name:"h0_3",kind:"buffer",elementType:"vec4<f32>",count:16384},{name:"omega_3",kind:"buffer",elementType:"f32",count:16384},{name:"fftA_3",kind:"buffer",elementType:"vec4<f32>",count:16384},{name:"fftB_3",kind:"buffer",elementType:"vec4<f32>",count:16384},{name:"dispTex3",kind:"texture",format:"rgba16float",width:128,height:128,mipLevels:8},{name:"slopeTex3",kind:"texture",format:"rgba16float",width:128,height:128,mipLevels:8},{name:"wakeA",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"wakeB",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"wakeF",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"wakeG",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"wakeC",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"wakeTex",kind:"texture",format:"rgba16float",width:256,height:256,mipLevels:9},{name:"foamA",kind:"buffer",elementType:"f32",count:1048576},{name:"foamB",kind:"buffer",elementType:"f32",count:1048576},{name:"foamTex",kind:"texture",format:"rgba16float",width:1024,height:1024,mipLevels:11},{name:"probePoints",kind:"buffer",elementType:"vec4<f32>",count:64,feed:"probePoints"},{name:"probeOut",kind:"buffer",elementType:"vec4<f32>",count:64}],passes:[{name:"c0.spectrum",kernel:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",dispatch:[64,1,1],bindings:[{name:"h0",resource:"h0_0"},{name:"omega",resource:"omega_0"}],uniforms:[{name:"spec",values:{cascade:0,fetchKm:"param:fetch",gamma:"param:gamma",kMax:.12,kMin:.003926990816987242,loopPeriod:"param:loopPeriod",n:64,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:1600,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c0.evolve",kernel:"ocean_evolve",dispatch:[64,1,1],bindings:[{name:"h0",resource:"h0_0"},{name:"omega",resource:"omega_0"},{name:"outA",resource:"fftA_0"},{name:"outB",resource:"fftB_0"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:64,t:"param:phaseTime",tile:1600}}]},{name:"c0.fftA.fft_rows",kernel:"fft_compute_64",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftA_0"}]},{name:"c0.fftA.fft_cols",kernel:"fft_compute_64",entry:"fft_cols",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftA_0"}]},{name:"c0.fftB.fft_rows",kernel:"fft_compute_64",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftB_0"}]},{name:"c0.fftB.fft_cols",kernel:"fft_compute_64",entry:"fft_cols",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftB_0"}]},{name:"c0.assemble",kernel:"ocean_assemble",dispatch:[64,1,1],bindings:[{name:"fieldA",resource:"fftA_0"},{name:"fieldB",resource:"fftB_0"},{name:"disp",resource:"dispTex0"},{name:"slope",resource:"slopeTex0"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:64}}]},{name:"c0.dispTex0.mips",type:"repeat",count:6,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[16,1,1],bindings:[{name:"src",resource:"dispTex0",levelAt:{index:"l"}},{name:"dst",resource:"dispTex0",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:64},dstWidth:{index:"l",offset:1,shr:64},pad:0,srcLevel:0}}]}]},{name:"c0.slopeTex0.mips",type:"repeat",count:6,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[16,1,1],bindings:[{name:"src",resource:"slopeTex0",levelAt:{index:"l"}},{name:"dst",resource:"slopeTex0",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:64},dstWidth:{index:"l",offset:1,shr:64},pad:0,srcLevel:0}}]}]},{name:"c1.spectrum",kernel:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",dispatch:[256,1,1],bindings:[{name:"h0",resource:"h0_1"},{name:"omega",resource:"omega_1"}],uniforms:[{name:"spec",values:{cascade:1,fetchKm:"param:fetch",gamma:"param:gamma",kMax:.8,kMin:.12,loopPeriod:"param:loopPeriod",n:128,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:250,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c1.evolve",kernel:"ocean_evolve",dispatch:[256,1,1],bindings:[{name:"h0",resource:"h0_1"},{name:"omega",resource:"omega_1"},{name:"outA",resource:"fftA_1"},{name:"outB",resource:"fftB_1"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:128,t:"param:phaseTime",tile:250}}]},{name:"c1.fftA.fft_rows",kernel:"fft_compute_128",dispatch:[128,1,1],bindings:[{name:"data",resource:"fftA_1"}]},{name:"c1.fftA.fft_cols",kernel:"fft_compute_128",entry:"fft_cols",dispatch:[128,1,1],bindings:[{name:"data",resource:"fftA_1"}]},{name:"c1.fftB.fft_rows",kernel:"fft_compute_128",dispatch:[128,1,1],bindings:[{name:"data",resource:"fftB_1"}]},{name:"c1.fftB.fft_cols",kernel:"fft_compute_128",entry:"fft_cols",dispatch:[128,1,1],bindings:[{name:"data",resource:"fftB_1"}]},{name:"c1.assemble",kernel:"ocean_assemble",dispatch:[256,1,1],bindings:[{name:"fieldA",resource:"fftA_1"},{name:"fieldB",resource:"fftB_1"},{name:"disp",resource:"dispTex1"},{name:"slope",resource:"slopeTex1"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:128}}]},{name:"c1.dispTex1.mips",type:"repeat",count:7,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[64,1,1],bindings:[{name:"src",resource:"dispTex1",levelAt:{index:"l"}},{name:"dst",resource:"dispTex1",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:128},dstWidth:{index:"l",offset:1,shr:128},pad:0,srcLevel:0}}]}]},{name:"c1.slopeTex1.mips",type:"repeat",count:7,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[64,1,1],bindings:[{name:"src",resource:"slopeTex1",levelAt:{index:"l"}},{name:"dst",resource:"slopeTex1",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:128},dstWidth:{index:"l",offset:1,shr:128},pad:0,srcLevel:0}}]}]},{name:"c2.spectrum",kernel:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",dispatch:[256,1,1],bindings:[{name:"h0",resource:"h0_2"},{name:"omega",resource:"omega_2"}],uniforms:[{name:"spec",values:{cascade:2,fetchKm:"param:fetch",gamma:"param:gamma",kMax:5.4,kMin:.8,loopPeriod:"param:loopPeriod",n:128,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:37,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c2.evolve",kernel:"ocean_evolve",dispatch:[256,1,1],bindings:[{name:"h0",resource:"h0_2"},{name:"omega",resource:"omega_2"},{name:"outA",resource:"fftA_2"},{name:"outB",resource:"fftB_2"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:128,t:"param:phaseTime",tile:37}}]},{name:"c2.fftA.fft_rows",kernel:"fft_compute_128",dispatch:[128,1,1],bindings:[{name:"data",resource:"fftA_2"}]},{name:"c2.fftA.fft_cols",kernel:"fft_compute_128",entry:"fft_cols",dispatch:[128,1,1],bindings:[{name:"data",resource:"fftA_2"}]},{name:"c2.fftB.fft_rows",kernel:"fft_compute_128",dispatch:[128,1,1],bindings:[{name:"data",resource:"fftB_2"}]},{name:"c2.fftB.fft_cols",kernel:"fft_compute_128",entry:"fft_cols",dispatch:[128,1,1],bindings:[{name:"data",resource:"fftB_2"}]},{name:"c2.assemble",kernel:"ocean_assemble",dispatch:[256,1,1],bindings:[{name:"fieldA",resource:"fftA_2"},{name:"fieldB",resource:"fftB_2"},{name:"disp",resource:"dispTex2"},{name:"slope",resource:"slopeTex2"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:128}}]},{name:"c2.dispTex2.mips",type:"repeat",count:7,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[64,1,1],bindings:[{name:"src",resource:"dispTex2",levelAt:{index:"l"}},{name:"dst",resource:"dispTex2",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:128},dstWidth:{index:"l",offset:1,shr:128},pad:0,srcLevel:0}}]}]},{name:"c2.slopeTex2.mips",type:"repeat",count:7,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[64,1,1],bindings:[{name:"src",resource:"slopeTex2",levelAt:{index:"l"}},{name:"dst",resource:"slopeTex2",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:128},dstWidth:{index:"l",offset:1,shr:128},pad:0,srcLevel:0}}]}]},{name:"c3.spectrum",kernel:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",dispatch:[256,1,1],bindings:[{name:"h0",resource:"h0_3"},{name:"omega",resource:"omega_3"}],uniforms:[{name:"spec",values:{cascade:3,fetchKm:"param:fetch",gamma:"param:gamma",kMax:37,kMin:5.4,loopPeriod:"param:loopPeriod",n:128,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:5.3,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c3.evolve",kernel:"ocean_evolve",dispatch:[256,1,1],bindings:[{name:"h0",resource:"h0_3"},{name:"omega",resource:"omega_3"},{name:"outA",resource:"fftA_3"},{name:"outB",resource:"fftB_3"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:128,t:"param:phaseTime",tile:5.3}}]},{name:"c3.fftA.fft_rows",kernel:"fft_compute_128",dispatch:[128,1,1],bindings:[{name:"data",resource:"fftA_3"}]},{name:"c3.fftA.fft_cols",kernel:"fft_compute_128",entry:"fft_cols",dispatch:[128,1,1],bindings:[{name:"data",resource:"fftA_3"}]},{name:"c3.fftB.fft_rows",kernel:"fft_compute_128",dispatch:[128,1,1],bindings:[{name:"data",resource:"fftB_3"}]},{name:"c3.fftB.fft_cols",kernel:"fft_compute_128",entry:"fft_cols",dispatch:[128,1,1],bindings:[{name:"data",resource:"fftB_3"}]},{name:"c3.assemble",kernel:"ocean_assemble",dispatch:[256,1,1],bindings:[{name:"fieldA",resource:"fftA_3"},{name:"fieldB",resource:"fftB_3"},{name:"disp",resource:"dispTex3"},{name:"slope",resource:"slopeTex3"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:128}}]},{name:"c3.dispTex3.mips",type:"repeat",count:7,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[64,1,1],bindings:[{name:"src",resource:"dispTex3",levelAt:{index:"l"}},{name:"dst",resource:"dispTex3",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:128},dstWidth:{index:"l",offset:1,shr:128},pad:0,srcLevel:0}}]}]},{name:"c3.slopeTex3.mips",type:"repeat",count:7,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[64,1,1],bindings:[{name:"src",resource:"slopeTex3",levelAt:{index:"l"}},{name:"dst",resource:"slopeTex3",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:128},dstWidth:{index:"l",offset:1,shr:128},pad:0,srcLevel:0}}]}]},{name:"wake.source",kernel:"wake_source",when:"param:wakeEnabled",dispatch:[1024,1,1],bindings:[{name:"wakeIn",resource:"wakeA"},{name:"fftOut",resource:"wakeF"},{name:"cleanOut",resource:"wakeC"}],uniforms:[{name:"wake",values:{bowFoam:"param:wakeBowFoam",bowGain:"param:wakeBowGain",cell:"param:wakeCell",depth:"param:wakeDepth",diffuse:"param:wakeDiffuse",dt:"param:wakeDt",foamSpeed:5,fullSpeed:3,heading:"param:hullHeading",hullBeam:"param:hullBeam",hullLen:"param:hullLength",hullU:"param:wakeHullU",hullV:"param:wakeHullV",jitter:"param:wakeJitter",originX:"param:wakeOriginX",originZ:"param:wakeOriginZ",reset:"param:wakeReset",shiftX:"param:wakeShiftX",shiftZ:"param:wakeShiftZ",speed:"param:hullSpeed",srcBeam:"param:wakeSrcBeam",srcLength:"param:wakeSrcLength",tau:"param:wakeFoamTau",width:256}}]},{name:"wake.fft1.fft_rows",kernel:"fft_compute_256",when:"param:wakeEnabled",dispatch:[256,1,1],bindings:[{name:"data",resource:"wakeF"}]},{name:"wake.fft1.fft_cols",kernel:"fft_compute_256",entry:"fft_cols",when:"param:wakeEnabled",dispatch:[256,1,1],bindings:[{name:"data",resource:"wakeF"}]},{name:"wake.symbol",kernel:"wake_symbol",when:"param:wakeEnabled",dispatch:[1024,1,1],bindings:[{name:"fftIn",resource:"wakeF"},{name:"fftOut",resource:"wakeG"}],uniforms:[{name:"sym",values:{n:256,window:"param:wakeLength"}}]},{name:"wake.fft2.fft_rows",kernel:"fft_compute_256",when:"param:wakeEnabled",dispatch:[256,1,1],bindings:[{name:"data",resource:"wakeG"}]},{name:"wake.fft2.fft_cols",kernel:"fft_compute_256",entry:"fft_cols",when:"param:wakeEnabled",dispatch:[256,1,1],bindings:[{name:"data",resource:"wakeG"}]},{name:"wake.step",kernel:"wake_step",when:"param:wakeEnabled",dispatch:[1024,1,1],bindings:[{name:"qIn",resource:"wakeG"},{name:"cleanIn",resource:"wakeC"},{name:"stateOut",resource:"wakeB"},{name:"wakeTex",resource:"wakeTex"}],uniforms:[{name:"wstep",values:{alpha:"param:wakeAlpha",cell:"param:wakeCell",dt:"param:wakeDt",gravity:"param:wakeGEff",originX:"param:wakeOriginX",originZ:"param:wakeOriginZ",sponge:24,spongeRate:.8,width:256}}]},{name:"wake.wakeTex.mips",type:"repeat",when:"param:wakeEnabled",count:8,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[256,1,1],bindings:[{name:"src",resource:"wakeTex",levelAt:{index:"l"}},{name:"dst",resource:"wakeTex",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:256},dstWidth:{index:"l",offset:1,shr:256},pad:0,srcLevel:0}}]}]},{name:"foam",kernel:"ocean_foam",dispatch:[16384,1,1],bindings:[{name:"foamIn",resource:"foamA"},{name:"fieldB0",resource:"fftB_0"},{name:"fieldB1",resource:"fftB_1"},{name:"fieldB2",resource:"fftB_2"},{name:"fieldB3",resource:"fftB_3"},{name:"foamOut",resource:"foamB"},{name:"foamTex",resource:"foamTex"},{name:"wakeIn",resource:"wakeB"}],uniforms:[{name:"foam",values:{cascades:4,cell:.5,dt:"param:dt",gain:"param:foamGain",lambda:"param:lambda",n0:64,n1:128,n2:128,n3:128,omX0:"param:foamOriginMod0X",omX1:"param:foamOriginMod1X",omX2:"param:foamOriginMod2X",omX3:"param:foamOriginMod3X",omZ0:"param:foamOriginMod0Z",omZ1:"param:foamOriginMod1Z",omZ2:"param:foamOriginMod2Z",omZ3:"param:foamOriginMod3Z",originX:"param:foamOriginX",originZ:"param:foamOriginZ",shiftX:"param:foamShiftX",shiftZ:"param:foamShiftZ",size0:1600,size1:250,size2:37,size3:5.3,tau:"param:foamTau",threshold:"param:foamThreshold",wakeCell:"param:wakeCell",wakeGain:"param:wakeFoamGainEff",wakeOriginX:"param:wakeOriginModX",wakeOriginZ:"param:wakeOriginModZ",wakeRelX:"param:wakeRelX",wakeRelZ:"param:wakeRelZ",wakeWidth:256,width:1024}}]},{name:"foamTex.mips",type:"repeat",count:10,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[4096,1,1],bindings:[{name:"src",resource:"foamTex",levelAt:{index:"l"}},{name:"dst",resource:"foamTex",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:1024},dstWidth:{index:"l",offset:1,shr:1024},pad:0,srcLevel:0}}]}]},{name:"probe",kernel:"ocean_probe",dispatch:[1,1,1],bindings:[{name:"points",resource:"probePoints"},{name:"disp0",resource:"fftA_0"},{name:"disp1",resource:"fftA_1"},{name:"disp2",resource:"fftA_2"},{name:"disp3",resource:"fftA_3"},{name:"out",resource:"probeOut"}],uniforms:[{name:"probe",values:{cascades:4,count:"param:probeCount",lambda:"param:lambda",n0:64,n1:128,n2:128,n3:128,normalStep:.5,omX0:"param:probeOriginMod0X",omX1:"param:probeOriginMod1X",omX2:"param:probeOriginMod2X",omX3:"param:probeOriginMod3X",omZ0:"param:probeOriginMod0Z",omZ1:"param:probeOriginMod1Z",omZ2:"param:probeOriginMod2Z",omZ3:"param:probeOriginMod3Z",size0:1600,size1:250,size2:37,size3:5.3,t:"param:simTime"}}]}],frameSwaps:[["wakeA","wakeB"],["foamA","foamB"]],exports:{disp0:"dispTex0",disp1:"dispTex1",disp2:"dispTex2",disp3:"dispTex3",foam:"foamTex",probe:"probeOut",slope0:"slopeTex0",slope1:"slopeTex1",slope2:"slopeTex2",slope3:"slopeTex3",wake:"wakeTex"},paramsFrom:"webgpu/high"},"webgpu/low":{name:"ocean",backend:"webgpu",tier:"low",version:1,resources:[{name:"h0_0",kind:"buffer",elementType:"vec4<f32>",count:4096},{name:"omega_0",kind:"buffer",elementType:"f32",count:4096},{name:"fftA_0",kind:"buffer",elementType:"vec4<f32>",count:4096},{name:"fftB_0",kind:"buffer",elementType:"vec4<f32>",count:4096},{name:"dispTex0",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"slopeTex0",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"h0_1",kind:"buffer",elementType:"vec4<f32>",count:4096},{name:"omega_1",kind:"buffer",elementType:"f32",count:4096},{name:"fftA_1",kind:"buffer",elementType:"vec4<f32>",count:4096},{name:"fftB_1",kind:"buffer",elementType:"vec4<f32>",count:4096},{name:"dispTex1",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"slopeTex1",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"h0_2",kind:"buffer",elementType:"vec4<f32>",count:4096},{name:"omega_2",kind:"buffer",elementType:"f32",count:4096},{name:"fftA_2",kind:"buffer",elementType:"vec4<f32>",count:4096},{name:"fftB_2",kind:"buffer",elementType:"vec4<f32>",count:4096},{name:"dispTex2",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"slopeTex2",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"wakeA",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"wakeB",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"wakeF",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"wakeG",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"wakeC",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"wakeTex",kind:"texture",format:"rgba16float",width:256,height:256,mipLevels:9},{name:"foamA",kind:"buffer",elementType:"f32",count:1048576},{name:"foamB",kind:"buffer",elementType:"f32",count:1048576},{name:"foamTex",kind:"texture",format:"rgba16float",width:1024,height:1024,mipLevels:11},{name:"probePoints",kind:"buffer",elementType:"vec4<f32>",count:64,feed:"probePoints"},{name:"probeOut",kind:"buffer",elementType:"vec4<f32>",count:64}],passes:[{name:"c0.spectrum",kernel:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",dispatch:[64,1,1],bindings:[{name:"h0",resource:"h0_0"},{name:"omega",resource:"omega_0"}],uniforms:[{name:"spec",values:{cascade:0,fetchKm:"param:fetch",gamma:"param:gamma",kMax:.12,kMin:.003926990816987242,loopPeriod:"param:loopPeriod",n:64,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:1600,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c0.evolve",kernel:"ocean_evolve",dispatch:[64,1,1],bindings:[{name:"h0",resource:"h0_0"},{name:"omega",resource:"omega_0"},{name:"outA",resource:"fftA_0"},{name:"outB",resource:"fftB_0"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:64,t:"param:phaseTime",tile:1600}}]},{name:"c0.fftA.fft_rows",kernel:"fft_compute_64",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftA_0"}]},{name:"c0.fftA.fft_cols",kernel:"fft_compute_64",entry:"fft_cols",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftA_0"}]},{name:"c0.fftB.fft_rows",kernel:"fft_compute_64",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftB_0"}]},{name:"c0.fftB.fft_cols",kernel:"fft_compute_64",entry:"fft_cols",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftB_0"}]},{name:"c0.assemble",kernel:"ocean_assemble",dispatch:[64,1,1],bindings:[{name:"fieldA",resource:"fftA_0"},{name:"fieldB",resource:"fftB_0"},{name:"disp",resource:"dispTex0"},{name:"slope",resource:"slopeTex0"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:64}}]},{name:"c0.dispTex0.mips",type:"repeat",count:6,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[16,1,1],bindings:[{name:"src",resource:"dispTex0",levelAt:{index:"l"}},{name:"dst",resource:"dispTex0",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:64},dstWidth:{index:"l",offset:1,shr:64},pad:0,srcLevel:0}}]}]},{name:"c0.slopeTex0.mips",type:"repeat",count:6,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[16,1,1],bindings:[{name:"src",resource:"slopeTex0",levelAt:{index:"l"}},{name:"dst",resource:"slopeTex0",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:64},dstWidth:{index:"l",offset:1,shr:64},pad:0,srcLevel:0}}]}]},{name:"c1.spectrum",kernel:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",dispatch:[64,1,1],bindings:[{name:"h0",resource:"h0_1"},{name:"omega",resource:"omega_1"}],uniforms:[{name:"spec",values:{cascade:1,fetchKm:"param:fetch",gamma:"param:gamma",kMax:.8,kMin:.12,loopPeriod:"param:loopPeriod",n:64,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:250,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c1.evolve",kernel:"ocean_evolve",dispatch:[64,1,1],bindings:[{name:"h0",resource:"h0_1"},{name:"omega",resource:"omega_1"},{name:"outA",resource:"fftA_1"},{name:"outB",resource:"fftB_1"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:64,t:"param:phaseTime",tile:250}}]},{name:"c1.fftA.fft_rows",kernel:"fft_compute_64",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftA_1"}]},{name:"c1.fftA.fft_cols",kernel:"fft_compute_64",entry:"fft_cols",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftA_1"}]},{name:"c1.fftB.fft_rows",kernel:"fft_compute_64",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftB_1"}]},{name:"c1.fftB.fft_cols",kernel:"fft_compute_64",entry:"fft_cols",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftB_1"}]},{name:"c1.assemble",kernel:"ocean_assemble",dispatch:[64,1,1],bindings:[{name:"fieldA",resource:"fftA_1"},{name:"fieldB",resource:"fftB_1"},{name:"disp",resource:"dispTex1"},{name:"slope",resource:"slopeTex1"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:64}}]},{name:"c1.dispTex1.mips",type:"repeat",count:6,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[16,1,1],bindings:[{name:"src",resource:"dispTex1",levelAt:{index:"l"}},{name:"dst",resource:"dispTex1",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:64},dstWidth:{index:"l",offset:1,shr:64},pad:0,srcLevel:0}}]}]},{name:"c1.slopeTex1.mips",type:"repeat",count:6,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[16,1,1],bindings:[{name:"src",resource:"slopeTex1",levelAt:{index:"l"}},{name:"dst",resource:"slopeTex1",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:64},dstWidth:{index:"l",offset:1,shr:64},pad:0,srcLevel:0}}]}]},{name:"c2.spectrum",kernel:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",dispatch:[64,1,1],bindings:[{name:"h0",resource:"h0_2"},{name:"omega",resource:"omega_2"}],uniforms:[{name:"spec",values:{cascade:2,fetchKm:"param:fetch",gamma:"param:gamma",kMax:5.4,kMin:.8,loopPeriod:"param:loopPeriod",n:64,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:37,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c2.evolve",kernel:"ocean_evolve",dispatch:[64,1,1],bindings:[{name:"h0",resource:"h0_2"},{name:"omega",resource:"omega_2"},{name:"outA",resource:"fftA_2"},{name:"outB",resource:"fftB_2"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:64,t:"param:phaseTime",tile:37}}]},{name:"c2.fftA.fft_rows",kernel:"fft_compute_64",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftA_2"}]},{name:"c2.fftA.fft_cols",kernel:"fft_compute_64",entry:"fft_cols",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftA_2"}]},{name:"c2.fftB.fft_rows",kernel:"fft_compute_64",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftB_2"}]},{name:"c2.fftB.fft_cols",kernel:"fft_compute_64",entry:"fft_cols",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftB_2"}]},{name:"c2.assemble",kernel:"ocean_assemble",dispatch:[64,1,1],bindings:[{name:"fieldA",resource:"fftA_2"},{name:"fieldB",resource:"fftB_2"},{name:"disp",resource:"dispTex2"},{name:"slope",resource:"slopeTex2"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:64}}]},{name:"c2.dispTex2.mips",type:"repeat",count:6,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[16,1,1],bindings:[{name:"src",resource:"dispTex2",levelAt:{index:"l"}},{name:"dst",resource:"dispTex2",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:64},dstWidth:{index:"l",offset:1,shr:64},pad:0,srcLevel:0}}]}]},{name:"c2.slopeTex2.mips",type:"repeat",count:6,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[16,1,1],bindings:[{name:"src",resource:"slopeTex2",levelAt:{index:"l"}},{name:"dst",resource:"slopeTex2",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:64},dstWidth:{index:"l",offset:1,shr:64},pad:0,srcLevel:0}}]}]},{name:"wake.source",kernel:"wake_source",when:"param:wakeEnabled",dispatch:[1024,1,1],bindings:[{name:"wakeIn",resource:"wakeA"},{name:"fftOut",resource:"wakeF"},{name:"cleanOut",resource:"wakeC"}],uniforms:[{name:"wake",values:{bowFoam:"param:wakeBowFoam",bowGain:"param:wakeBowGain",cell:"param:wakeCell",depth:"param:wakeDepth",diffuse:"param:wakeDiffuse",dt:"param:wakeDt",foamSpeed:5,fullSpeed:3,heading:"param:hullHeading",hullBeam:"param:hullBeam",hullLen:"param:hullLength",hullU:"param:wakeHullU",hullV:"param:wakeHullV",jitter:"param:wakeJitter",originX:"param:wakeOriginX",originZ:"param:wakeOriginZ",reset:"param:wakeReset",shiftX:"param:wakeShiftX",shiftZ:"param:wakeShiftZ",speed:"param:hullSpeed",srcBeam:"param:wakeSrcBeam",srcLength:"param:wakeSrcLength",tau:"param:wakeFoamTau",width:256}}]},{name:"wake.fft1.fft_rows",kernel:"fft_compute_256",when:"param:wakeEnabled",dispatch:[256,1,1],bindings:[{name:"data",resource:"wakeF"}]},{name:"wake.fft1.fft_cols",kernel:"fft_compute_256",entry:"fft_cols",when:"param:wakeEnabled",dispatch:[256,1,1],bindings:[{name:"data",resource:"wakeF"}]},{name:"wake.symbol",kernel:"wake_symbol",when:"param:wakeEnabled",dispatch:[1024,1,1],bindings:[{name:"fftIn",resource:"wakeF"},{name:"fftOut",resource:"wakeG"}],uniforms:[{name:"sym",values:{n:256,window:"param:wakeLength"}}]},{name:"wake.fft2.fft_rows",kernel:"fft_compute_256",when:"param:wakeEnabled",dispatch:[256,1,1],bindings:[{name:"data",resource:"wakeG"}]},{name:"wake.fft2.fft_cols",kernel:"fft_compute_256",entry:"fft_cols",when:"param:wakeEnabled",dispatch:[256,1,1],bindings:[{name:"data",resource:"wakeG"}]},{name:"wake.step",kernel:"wake_step",when:"param:wakeEnabled",dispatch:[1024,1,1],bindings:[{name:"qIn",resource:"wakeG"},{name:"cleanIn",resource:"wakeC"},{name:"stateOut",resource:"wakeB"},{name:"wakeTex",resource:"wakeTex"}],uniforms:[{name:"wstep",values:{alpha:"param:wakeAlpha",cell:"param:wakeCell",dt:"param:wakeDt",gravity:"param:wakeGEff",originX:"param:wakeOriginX",originZ:"param:wakeOriginZ",sponge:24,spongeRate:.8,width:256}}]},{name:"wake.wakeTex.mips",type:"repeat",when:"param:wakeEnabled",count:8,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[256,1,1],bindings:[{name:"src",resource:"wakeTex",levelAt:{index:"l"}},{name:"dst",resource:"wakeTex",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:256},dstWidth:{index:"l",offset:1,shr:256},pad:0,srcLevel:0}}]}]},{name:"foam",kernel:"ocean_foam",dispatch:[16384,1,1],bindings:[{name:"foamIn",resource:"foamA"},{name:"fieldB0",resource:"fftB_0"},{name:"fieldB1",resource:"fftB_1"},{name:"fieldB2",resource:"fftB_2"},{name:"fieldB3",resource:"fftB_2"},{name:"foamOut",resource:"foamB"},{name:"foamTex",resource:"foamTex"},{name:"wakeIn",resource:"wakeB"}],uniforms:[{name:"foam",values:{cascades:3,cell:.5,dt:"param:dt",gain:"param:foamGain",lambda:"param:lambda",n0:64,n1:64,n2:64,n3:64,omX0:"param:foamOriginMod0X",omX1:"param:foamOriginMod1X",omX2:"param:foamOriginMod2X",omX3:"param:foamOriginMod2X",omZ0:"param:foamOriginMod0Z",omZ1:"param:foamOriginMod1Z",omZ2:"param:foamOriginMod2Z",omZ3:"param:foamOriginMod2Z",originX:"param:foamOriginX",originZ:"param:foamOriginZ",shiftX:"param:foamShiftX",shiftZ:"param:foamShiftZ",size0:1600,size1:250,size2:37,size3:37,tau:"param:foamTau",threshold:"param:foamThreshold",wakeCell:"param:wakeCell",wakeGain:"param:wakeFoamGainEff",wakeOriginX:"param:wakeOriginModX",wakeOriginZ:"param:wakeOriginModZ",wakeRelX:"param:wakeRelX",wakeRelZ:"param:wakeRelZ",wakeWidth:256,width:1024}}]},{name:"foamTex.mips",type:"repeat",count:10,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[4096,1,1],bindings:[{name:"src",resource:"foamTex",levelAt:{index:"l"}},{name:"dst",resource:"foamTex",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:1024},dstWidth:{index:"l",offset:1,shr:1024},pad:0,srcLevel:0}}]}]},{name:"probe",kernel:"ocean_probe",dispatch:[1,1,1],bindings:[{name:"points",resource:"probePoints"},{name:"disp0",resource:"fftA_0"},{name:"disp1",resource:"fftA_1"},{name:"disp2",resource:"fftA_2"},{name:"disp3",resource:"fftA_2"},{name:"out",resource:"probeOut"}],uniforms:[{name:"probe",values:{cascades:3,count:"param:probeCount",lambda:"param:lambda",n0:64,n1:64,n2:64,n3:64,normalStep:.5,omX0:"param:probeOriginMod0X",omX1:"param:probeOriginMod1X",omX2:"param:probeOriginMod2X",omX3:"param:probeOriginMod2X",omZ0:"param:probeOriginMod0Z",omZ1:"param:probeOriginMod1Z",omZ2:"param:probeOriginMod2Z",omZ3:"param:probeOriginMod2Z",size0:1600,size1:250,size2:37,size3:37,t:"param:simTime"}}]}],frameSwaps:[["wakeA","wakeB"],["foamA","foamB"]],exports:{disp0:"dispTex0",disp1:"dispTex1",disp2:"dispTex2",foam:"foamTex",probe:"probeOut",slope0:"slopeTex0",slope1:"slopeTex1",slope2:"slopeTex2",wake:"wakeTex"},paramsFrom:"webgpu/high"},"webgl2/high":{name:"ocean",backend:"webgl2",tier:"high",version:1,params:[{name:"windSpeed",type:"f32",source:{kind:"value",value:9},tween:20},{name:"fetch",type:"f32",source:{kind:"value",value:300},tween:20},{name:"gamma",type:"f32",source:{kind:"value",value:3.3},tween:20},{name:"spread",type:"f32",source:{kind:"value",value:8},tween:20},{name:"swell",type:"f32",source:{kind:"value",value:0},tween:20},{name:"windDirDeg",type:"f32",source:{kind:"value",value:0},tween:20},{name:"swellDirDeg",type:"f32",source:{kind:"value",value:0},tween:20},{name:"seed",type:"u32",source:{kind:"value",value:0}},{name:"loopPeriod",type:"f32",source:{kind:"value",value:1e3}},{name:"lambda",type:"f32",source:{kind:"value",value:1},tween:20},{name:"timeScale",type:"f32",source:{kind:"value",value:1}},{name:"driftX",type:"f32",source:{kind:"value",value:0}},{name:"driftZ",type:"f32",source:{kind:"value",value:0}},{name:"foamTau",type:"f32",source:{kind:"value",value:4}},{name:"foamThreshold",type:"f32",source:{kind:"value",value:.9}},{name:"foamGain",type:"f32",source:{kind:"value",value:2}},{name:"probeCount",type:"u32",source:{kind:"value",value:0}},{name:"dt",type:"f32",source:{kind:"frame",name:"dt"}},{name:"cameraX",type:"f32",source:{kind:"frame",name:"cameraX"}},{name:"cameraZ",type:"f32",source:{kind:"frame",name:"cameraZ"}},{name:"windDir",type:"f32",source:{args:[1.5707963267948966,{op:"mul",args:["param:windDirDeg",.017453292519943295]}],kind:"expr",op:"sub"}},{name:"swellDir",type:"f32",source:{args:[1.5707963267948966,{op:"mul",args:["param:swellDirDeg",.017453292519943295]}],kind:"expr",op:"sub"}},{name:"simTime",type:"f32",source:{kind:"accumulate",of:"param:timeScale"}},{name:"phaseTime",type:"f32",source:{args:["param:simTime","param:loopPeriod"],kind:"expr",op:"mod"}},{name:"waterX",type:"f32",source:{kind:"accumulate",of:"param:driftX"}},{name:"waterZ",type:"f32",source:{kind:"accumulate",of:"param:driftZ"}},{name:"waterModX0",type:"f32",source:{args:["param:waterX",1600],kind:"expr",op:"mod"}},{name:"waterModZ0",type:"f32",source:{args:["param:waterZ",1600],kind:"expr",op:"mod"}},{name:"waterModX1",type:"f32",source:{args:["param:waterX",250],kind:"expr",op:"mod"}},{name:"waterModZ1",type:"f32",source:{args:["param:waterZ",250],kind:"expr",op:"mod"}},{name:"waterModX2",type:"f32",source:{args:["param:waterX",37],kind:"expr",op:"mod"}},{name:"waterModZ2",type:"f32",source:{args:["param:waterZ",37],kind:"expr",op:"mod"}},{name:"waterModX3",type:"f32",source:{args:["param:waterX",5.3],kind:"expr",op:"mod"}},{name:"waterModZ3",type:"f32",source:{args:["param:waterZ",5.3],kind:"expr",op:"mod"}},{name:"foamOriginX",type:"i32",source:{args:[{op:"floor",args:[{op:"div",args:[{op:"sub",args:["param:cameraX","param:waterX"]},.5]}]},512],kind:"expr",op:"sub"}},{name:"foamShiftX",type:"i32",source:{kind:"delta",of:"param:foamOriginX"}},{name:"foamOriginMod0X",type:"f32",source:{args:[{op:"mul",args:["param:foamOriginX",.5]},1600],kind:"expr",op:"mod"}},{name:"foamOriginMod1X",type:"f32",source:{args:[{op:"mul",args:["param:foamOriginX",.5]},250],kind:"expr",op:"mod"}},{name:"foamOriginMod2X",type:"f32",source:{args:[{op:"mul",args:["param:foamOriginX",.5]},37],kind:"expr",op:"mod"}},{name:"foamOriginMod3X",type:"f32",source:{args:[{op:"mul",args:["param:foamOriginX",.5]},5.3],kind:"expr",op:"mod"}},{name:"foamOriginZ",type:"i32",source:{args:[{op:"floor",args:[{op:"div",args:[{op:"sub",args:["param:cameraZ","param:waterZ"]},.5]}]},512],kind:"expr",op:"sub"}},{name:"foamShiftZ",type:"i32",source:{kind:"delta",of:"param:foamOriginZ"}},{name:"foamOriginMod0Z",type:"f32",source:{args:[{op:"mul",args:["param:foamOriginZ",.5]},1600],kind:"expr",op:"mod"}},{name:"foamOriginMod1Z",type:"f32",source:{args:[{op:"mul",args:["param:foamOriginZ",.5]},250],kind:"expr",op:"mod"}},{name:"foamOriginMod2Z",type:"f32",source:{args:[{op:"mul",args:["param:foamOriginZ",.5]},37],kind:"expr",op:"mod"}},{name:"foamOriginMod3Z",type:"f32",source:{args:[{op:"mul",args:["param:foamOriginZ",.5]},5.3],kind:"expr",op:"mod"}},{name:"hullX",type:"f32",source:{kind:"value",value:0}},{name:"hullZ",type:"f32",source:{kind:"value",value:0}},{name:"hullHeading",type:"f32",source:{kind:"value",value:0}},{name:"hullSpeed",type:"f32",source:{kind:"value",value:0}},{name:"wakeEnabled",type:"bool",source:{kind:"value",value:0}},{name:"hullLength",type:"f32",source:{kind:"value",value:200}},{name:"hullBeam",type:"f32",source:{kind:"value",value:30}},{name:"wakeCell",type:"f32",source:{kind:"value",value:4}},{name:"wakeDepth",type:"f32",source:{kind:"value",value:.4}},{name:"wakeGravity",type:"f32",source:{kind:"value",value:9.81}},{name:"wakeAlpha",type:"f32",source:{kind:"value",value:.02}},{name:"wakeMinLambdaCells",type:"f32",source:{kind:"value",value:8}},{name:"wakeFoamGain",type:"f32",source:{kind:"value",value:1}},{name:"wakeFoamTau",type:"f32",source:{kind:"value",value:48}},{name:"wakeDiffuse",type:"f32",source:{kind:"value",value:1.5}},{name:"wakeJitter",type:"f32",source:{kind:"value",value:1}},{name:"wakeBowGain",type:"f32",source:{kind:"value",value:.2}},{name:"wakeBowFoam",type:"f32",source:{kind:"value",value:.9}},{name:"wakeOn",type:"f32",source:{args:["param:wakeEnabled",1],kind:"expr",op:"mul"}},{name:"wakeFoamGainEff",type:"f32",source:{args:["param:wakeEnabled","param:wakeFoamGain"],kind:"expr",op:"mul"}},{name:"wakeEdge",type:"i32",source:{kind:"delta",of:"param:wakeEnabled"}},{name:"wakeReset",type:"u32",source:{args:["param:wakeEdge",0],kind:"expr",op:"max"}},{name:"wakeLength",type:"f32",source:{args:["param:wakeCell",256],kind:"expr",op:"mul"}},{name:"hullWaterX",type:"f32",source:{args:["param:hullX","param:waterX"],kind:"expr",op:"sub"}},{name:"wakeOriginX",type:"i32",source:{args:[{op:"div",args:[{op:"sub",args:[{op:"sub",args:["param:hullWaterX",{op:"mul",args:[{op:"sin",args:["param:hullHeading"]},{op:"mul",args:["param:wakeCell",51.2]}]}]},{op:"mul",args:["param:wakeCell",128]}]},"param:wakeCell"]}],kind:"expr",op:"floor"}},{name:"wakeShiftX",type:"i32",source:{kind:"delta",of:"param:wakeOriginX"}},{name:"wakeHullU",type:"f32",source:{args:["param:hullWaterX",{op:"mul",args:["param:wakeOriginX","param:wakeCell"]}],kind:"expr",op:"sub"}},{name:"wakeOriginModX",type:"i32",source:{args:["param:wakeOriginX",256],kind:"expr",op:"mod"}},{name:"wakeRelX",type:"f32",source:{args:[{op:"mul",args:["param:foamOriginX",.5]},{op:"mul",args:["param:wakeOriginX","param:wakeCell"]}],kind:"expr",op:"sub"}},{name:"wakeCamRelX",type:"f32",source:{args:[{op:"add",args:["param:waterX",{op:"mul",args:["param:wakeOriginX","param:wakeCell"]}]},"param:cameraX"],kind:"expr",op:"sub"}},{name:"hullWaterZ",type:"f32",source:{args:["param:hullZ","param:waterZ"],kind:"expr",op:"sub"}},{name:"wakeOriginZ",type:"i32",source:{args:[{op:"div",args:[{op:"sub",args:[{op:"sub",args:["param:hullWaterZ",{op:"mul",args:[{op:"cos",args:["param:hullHeading"]},{op:"mul",args:["param:wakeCell",51.2]}]}]},{op:"mul",args:["param:wakeCell",128]}]},"param:wakeCell"]}],kind:"expr",op:"floor"}},{name:"wakeShiftZ",type:"i32",source:{kind:"delta",of:"param:wakeOriginZ"}},{name:"wakeHullV",type:"f32",source:{args:["param:hullWaterZ",{op:"mul",args:["param:wakeOriginZ","param:wakeCell"]}],kind:"expr",op:"sub"}},{name:"wakeOriginModZ",type:"i32",source:{args:["param:wakeOriginZ",256],kind:"expr",op:"mod"}},{name:"wakeRelZ",type:"f32",source:{args:[{op:"mul",args:["param:foamOriginZ",.5]},{op:"mul",args:["param:wakeOriginZ","param:wakeCell"]}],kind:"expr",op:"sub"}},{name:"wakeCamRelZ",type:"f32",source:{args:[{op:"add",args:["param:waterZ",{op:"mul",args:["param:wakeOriginZ","param:wakeCell"]}]},"param:cameraZ"],kind:"expr",op:"sub"}},{name:"wakeGEff",type:"f32",source:{args:[{op:"min",args:[{op:"min",args:["param:wakeGravity",{op:"mul",args:[{op:"mul",args:["param:hullSpeed","param:hullSpeed"]},{op:"div",args:[6.283185307179586,{op:"mul",args:["param:wakeMinLambdaCells","param:wakeCell"]}]}]}]},{op:"mul",args:["param:wakeCell",16.39001041697031]}]},.05],kind:"expr",op:"max"}},{name:"wakeDt",type:"f32",source:{args:[{op:"mul",args:["param:dt","param:timeScale"]},.2],kind:"expr",op:"min"}},{name:"wakeSrcLength",type:"f32",source:{args:[{op:"min",args:["param:hullLength",{op:"div",args:[{op:"mul",args:[{op:"mul",args:["param:hullSpeed","param:hullSpeed"]},3.141592653589793]},"param:wakeGEff"]}]},{op:"mul",args:["param:wakeCell",2]}],kind:"expr",op:"max"}},{name:"wakeSrcBeam",type:"f32",source:{args:[{op:"min",args:["param:hullBeam",{op:"mul",args:["param:wakeSrcLength",.67]}]},{op:"mul",args:["param:wakeCell",1.5]}],kind:"expr",op:"max"}}],resources:[{name:"h0_0",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"omega_0",kind:"buffer",elementType:"f32",count:4096,width:64},{name:"fftA_0",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"fftB_0",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"fftA2_0",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"fftB2_0",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"dispTex0",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"slopeTex0",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"h0_1",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"omega_1",kind:"buffer",elementType:"f32",count:65536,width:256},{name:"fftA_1",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"fftB_1",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"fftA2_1",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"fftB2_1",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"dispTex1",kind:"texture",format:"rgba16float",width:256,height:256,mipLevels:9},{name:"slopeTex1",kind:"texture",format:"rgba16float",width:256,height:256,mipLevels:9},{name:"h0_2",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"omega_2",kind:"buffer",elementType:"f32",count:65536,width:256},{name:"fftA_2",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"fftB_2",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"fftA2_2",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"fftB2_2",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"dispTex2",kind:"texture",format:"rgba16float",width:256,height:256,mipLevels:9},{name:"slopeTex2",kind:"texture",format:"rgba16float",width:256,height:256,mipLevels:9},{name:"h0_3",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"omega_3",kind:"buffer",elementType:"f32",count:65536,width:256},{name:"fftA_3",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"fftB_3",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"fftA2_3",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"fftB2_3",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"dispTex3",kind:"texture",format:"rgba16float",width:256,height:256,mipLevels:9},{name:"slopeTex3",kind:"texture",format:"rgba16float",width:256,height:256,mipLevels:9},{name:"wakeA",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"wakeB",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"wakeF",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"wakeG",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"wakeC",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"wakeTex",kind:"texture",format:"rgba16float",width:256,height:256,mipLevels:9},{name:"foamA",kind:"buffer",elementType:"f32",count:1048576,width:1024},{name:"foamB",kind:"buffer",elementType:"f32",count:1048576,width:1024},{name:"foamTex",kind:"texture",format:"rgba16float",width:1024,height:1024,mipLevels:11}],passes:[{name:"c0.spectrum",kernel:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",target:{width:64,height:64,elioWidth:64},bindings:[{name:"h0",resource:"h0_0"},{name:"omega",resource:"omega_0"}],uniforms:[{name:"spec",values:{cascade:0,fetchKm:"param:fetch",gamma:"param:gamma",kMax:.12,kMin:.003926990816987242,loopPeriod:"param:loopPeriod",n:64,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:1600,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c0.evolve",kernel:"ocean_evolve",target:{width:64,height:64,elioWidth:64},bindings:[{name:"h0",resource:"h0_0"},{name:"omega",resource:"omega_0"},{name:"outA",resource:"fftA_0"},{name:"outB",resource:"fftB_0"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:64,t:"param:phaseTime",tile:1600}}]},{name:"c0.fft.rows",type:"repeat",count:6,index:"s",swap:[["fftA_0","fftA2_0"],["fftB_0","fftB2_0"]],body:[{name:"A",kernel:"fft_gather_64",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftA_0"},{name:"dst",resource:"fftA2_0"}],uniforms:[{name:"fft",values:{axis:0,n:64,stage:"param:s"}}]},{name:"B",kernel:"fft_gather_64",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftB_0"},{name:"dst",resource:"fftB2_0"}],uniforms:[{name:"fft",values:{axis:0,n:64,stage:"param:s"}}]}]},{name:"c0.fft.cols",type:"repeat",count:6,index:"s",swap:[["fftA_0","fftA2_0"],["fftB_0","fftB2_0"]],body:[{name:"A",kernel:"fft_gather_64",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftA_0"},{name:"dst",resource:"fftA2_0"}],uniforms:[{name:"fft",values:{axis:1,n:64,stage:"param:s"}}]},{name:"B",kernel:"fft_gather_64",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftB_0"},{name:"dst",resource:"fftB2_0"}],uniforms:[{name:"fft",values:{axis:1,n:64,stage:"param:s"}}]}]},{name:"c0.assemble",kernel:"ocean_assemble",target:{width:64,height:64,elioWidth:64},bindings:[{name:"fieldA",resource:"fftA_0"},{name:"fieldB",resource:"fftB_0"},{name:"disp",resource:"dispTex0"},{name:"slope",resource:"slopeTex0"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:64}}]},{name:"c0.dispTex0.mips",type:"mipmap",resource:"dispTex0"},{name:"c0.slopeTex0.mips",type:"mipmap",resource:"slopeTex0"},{name:"c1.spectrum",kernel:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",target:{width:256,height:256,elioWidth:256},bindings:[{name:"h0",resource:"h0_1"},{name:"omega",resource:"omega_1"}],uniforms:[{name:"spec",values:{cascade:1,fetchKm:"param:fetch",gamma:"param:gamma",kMax:.8,kMin:.12,loopPeriod:"param:loopPeriod",n:256,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:250,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c1.evolve",kernel:"ocean_evolve",target:{width:256,height:256,elioWidth:256},bindings:[{name:"h0",resource:"h0_1"},{name:"omega",resource:"omega_1"},{name:"outA",resource:"fftA_1"},{name:"outB",resource:"fftB_1"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:256,t:"param:phaseTime",tile:250}}]},{name:"c1.fft.rows",type:"repeat",count:8,index:"s",swap:[["fftA_1","fftA2_1"],["fftB_1","fftB2_1"]],body:[{name:"A",kernel:"fft_gather_256",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"fftA_1"},{name:"dst",resource:"fftA2_1"}],uniforms:[{name:"fft",values:{axis:0,n:256,stage:"param:s"}}]},{name:"B",kernel:"fft_gather_256",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"fftB_1"},{name:"dst",resource:"fftB2_1"}],uniforms:[{name:"fft",values:{axis:0,n:256,stage:"param:s"}}]}]},{name:"c1.fft.cols",type:"repeat",count:8,index:"s",swap:[["fftA_1","fftA2_1"],["fftB_1","fftB2_1"]],body:[{name:"A",kernel:"fft_gather_256",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"fftA_1"},{name:"dst",resource:"fftA2_1"}],uniforms:[{name:"fft",values:{axis:1,n:256,stage:"param:s"}}]},{name:"B",kernel:"fft_gather_256",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"fftB_1"},{name:"dst",resource:"fftB2_1"}],uniforms:[{name:"fft",values:{axis:1,n:256,stage:"param:s"}}]}]},{name:"c1.assemble",kernel:"ocean_assemble",target:{width:256,height:256,elioWidth:256},bindings:[{name:"fieldA",resource:"fftA_1"},{name:"fieldB",resource:"fftB_1"},{name:"disp",resource:"dispTex1"},{name:"slope",resource:"slopeTex1"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:256}}]},{name:"c1.dispTex1.mips",type:"mipmap",resource:"dispTex1"},{name:"c1.slopeTex1.mips",type:"mipmap",resource:"slopeTex1"},{name:"c2.spectrum",kernel:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",target:{width:256,height:256,elioWidth:256},bindings:[{name:"h0",resource:"h0_2"},{name:"omega",resource:"omega_2"}],uniforms:[{name:"spec",values:{cascade:2,fetchKm:"param:fetch",gamma:"param:gamma",kMax:5.4,kMin:.8,loopPeriod:"param:loopPeriod",n:256,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:37,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c2.evolve",kernel:"ocean_evolve",target:{width:256,height:256,elioWidth:256},bindings:[{name:"h0",resource:"h0_2"},{name:"omega",resource:"omega_2"},{name:"outA",resource:"fftA_2"},{name:"outB",resource:"fftB_2"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:256,t:"param:phaseTime",tile:37}}]},{name:"c2.fft.rows",type:"repeat",count:8,index:"s",swap:[["fftA_2","fftA2_2"],["fftB_2","fftB2_2"]],body:[{name:"A",kernel:"fft_gather_256",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"fftA_2"},{name:"dst",resource:"fftA2_2"}],uniforms:[{name:"fft",values:{axis:0,n:256,stage:"param:s"}}]},{name:"B",kernel:"fft_gather_256",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"fftB_2"},{name:"dst",resource:"fftB2_2"}],uniforms:[{name:"fft",values:{axis:0,n:256,stage:"param:s"}}]}]},{name:"c2.fft.cols",type:"repeat",count:8,index:"s",swap:[["fftA_2","fftA2_2"],["fftB_2","fftB2_2"]],body:[{name:"A",kernel:"fft_gather_256",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"fftA_2"},{name:"dst",resource:"fftA2_2"}],uniforms:[{name:"fft",values:{axis:1,n:256,stage:"param:s"}}]},{name:"B",kernel:"fft_gather_256",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"fftB_2"},{name:"dst",resource:"fftB2_2"}],uniforms:[{name:"fft",values:{axis:1,n:256,stage:"param:s"}}]}]},{name:"c2.assemble",kernel:"ocean_assemble",target:{width:256,height:256,elioWidth:256},bindings:[{name:"fieldA",resource:"fftA_2"},{name:"fieldB",resource:"fftB_2"},{name:"disp",resource:"dispTex2"},{name:"slope",resource:"slopeTex2"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:256}}]},{name:"c2.dispTex2.mips",type:"mipmap",resource:"dispTex2"},{name:"c2.slopeTex2.mips",type:"mipmap",resource:"slopeTex2"},{name:"c3.spectrum",kernel:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",target:{width:256,height:256,elioWidth:256},bindings:[{name:"h0",resource:"h0_3"},{name:"omega",resource:"omega_3"}],uniforms:[{name:"spec",values:{cascade:3,fetchKm:"param:fetch",gamma:"param:gamma",kMax:37,kMin:5.4,loopPeriod:"param:loopPeriod",n:256,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:5.3,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c3.evolve",kernel:"ocean_evolve",target:{width:256,height:256,elioWidth:256},bindings:[{name:"h0",resource:"h0_3"},{name:"omega",resource:"omega_3"},{name:"outA",resource:"fftA_3"},{name:"outB",resource:"fftB_3"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:256,t:"param:phaseTime",tile:5.3}}]},{name:"c3.fft.rows",type:"repeat",count:8,index:"s",swap:[["fftA_3","fftA2_3"],["fftB_3","fftB2_3"]],body:[{name:"A",kernel:"fft_gather_256",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"fftA_3"},{name:"dst",resource:"fftA2_3"}],uniforms:[{name:"fft",values:{axis:0,n:256,stage:"param:s"}}]},{name:"B",kernel:"fft_gather_256",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"fftB_3"},{name:"dst",resource:"fftB2_3"}],uniforms:[{name:"fft",values:{axis:0,n:256,stage:"param:s"}}]}]},{name:"c3.fft.cols",type:"repeat",count:8,index:"s",swap:[["fftA_3","fftA2_3"],["fftB_3","fftB2_3"]],body:[{name:"A",kernel:"fft_gather_256",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"fftA_3"},{name:"dst",resource:"fftA2_3"}],uniforms:[{name:"fft",values:{axis:1,n:256,stage:"param:s"}}]},{name:"B",kernel:"fft_gather_256",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"fftB_3"},{name:"dst",resource:"fftB2_3"}],uniforms:[{name:"fft",values:{axis:1,n:256,stage:"param:s"}}]}]},{name:"c3.assemble",kernel:"ocean_assemble",target:{width:256,height:256,elioWidth:256},bindings:[{name:"fieldA",resource:"fftA_3"},{name:"fieldB",resource:"fftB_3"},{name:"disp",resource:"dispTex3"},{name:"slope",resource:"slopeTex3"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:256}}]},{name:"c3.dispTex3.mips",type:"mipmap",resource:"dispTex3"},{name:"c3.slopeTex3.mips",type:"mipmap",resource:"slopeTex3"},{name:"wake.source",kernel:"wake_source",when:"param:wakeEnabled",target:{width:256,height:256,elioWidth:256},bindings:[{name:"wakeIn",resource:"wakeA"},{name:"fftOut",resource:"wakeF"},{name:"cleanOut",resource:"wakeC"}],uniforms:[{name:"wake",values:{bowFoam:"param:wakeBowFoam",bowGain:"param:wakeBowGain",cell:"param:wakeCell",depth:"param:wakeDepth",diffuse:"param:wakeDiffuse",dt:"param:wakeDt",foamSpeed:5,fullSpeed:3,heading:"param:hullHeading",hullBeam:"param:hullBeam",hullLen:"param:hullLength",hullU:"param:wakeHullU",hullV:"param:wakeHullV",jitter:"param:wakeJitter",originX:"param:wakeOriginX",originZ:"param:wakeOriginZ",reset:"param:wakeReset",shiftX:"param:wakeShiftX",shiftZ:"param:wakeShiftZ",speed:"param:hullSpeed",srcBeam:"param:wakeSrcBeam",srcLength:"param:wakeSrcLength",tau:"param:wakeFoamTau",width:256}}]},{name:"wake.fft1.rows",type:"repeat",when:"param:wakeEnabled",count:8,index:"s",swap:[["wakeF","wakeG"]],body:[{name:"stage",kernel:"fft_gather_256",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"wakeF"},{name:"dst",resource:"wakeG"}],uniforms:[{name:"fft",values:{axis:0,n:256,stage:"param:s"}}]}]},{name:"wake.fft1.cols",type:"repeat",when:"param:wakeEnabled",count:8,index:"s",swap:[["wakeF","wakeG"]],body:[{name:"stage",kernel:"fft_gather_256",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"wakeF"},{name:"dst",resource:"wakeG"}],uniforms:[{name:"fft",values:{axis:1,n:256,stage:"param:s"}}]}]},{name:"wake.symbol",kernel:"wake_symbol",when:"param:wakeEnabled",target:{width:256,height:256,elioWidth:256},bindings:[{name:"fftIn",resource:"wakeF"},{name:"fftOut",resource:"wakeG"}],uniforms:[{name:"sym",values:{n:256,window:"param:wakeLength"}}]},{name:"wake.fft2.rows",type:"repeat",when:"param:wakeEnabled",count:8,index:"s",swap:[["wakeG","wakeF"]],body:[{name:"stage",kernel:"fft_gather_256",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"wakeG"},{name:"dst",resource:"wakeF"}],uniforms:[{name:"fft",values:{axis:0,n:256,stage:"param:s"}}]}]},{name:"wake.fft2.cols",type:"repeat",when:"param:wakeEnabled",count:8,index:"s",swap:[["wakeG","wakeF"]],body:[{name:"stage",kernel:"fft_gather_256",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"wakeG"},{name:"dst",resource:"wakeF"}],uniforms:[{name:"fft",values:{axis:1,n:256,stage:"param:s"}}]}]},{name:"wake.step",kernel:"wake_step",when:"param:wakeEnabled",target:{width:256,height:256,elioWidth:256},bindings:[{name:"qIn",resource:"wakeG"},{name:"cleanIn",resource:"wakeC"},{name:"stateOut",resource:"wakeB"},{name:"wakeTex",resource:"wakeTex"}],uniforms:[{name:"wstep",values:{alpha:"param:wakeAlpha",cell:"param:wakeCell",dt:"param:wakeDt",gravity:"param:wakeGEff",originX:"param:wakeOriginX",originZ:"param:wakeOriginZ",sponge:24,spongeRate:.8,width:256}}]},{name:"wake.wakeTex.mips",type:"mipmap",when:"param:wakeEnabled",resource:"wakeTex"},{name:"foam",kernel:"ocean_foam",target:{width:1024,height:1024,elioWidth:1024},bindings:[{name:"foamIn",resource:"foamA"},{name:"fieldB0",resource:"fftB_0"},{name:"fieldB1",resource:"fftB_1"},{name:"fieldB2",resource:"fftB_2"},{name:"fieldB3",resource:"fftB_3"},{name:"foamOut",resource:"foamB"},{name:"foamTex",resource:"foamTex"},{name:"wakeIn",resource:"wakeB"}],uniforms:[{name:"foam",values:{cascades:4,cell:.5,dt:"param:dt",gain:"param:foamGain",lambda:"param:lambda",n0:64,n1:256,n2:256,n3:256,omX0:"param:foamOriginMod0X",omX1:"param:foamOriginMod1X",omX2:"param:foamOriginMod2X",omX3:"param:foamOriginMod3X",omZ0:"param:foamOriginMod0Z",omZ1:"param:foamOriginMod1Z",omZ2:"param:foamOriginMod2Z",omZ3:"param:foamOriginMod3Z",originX:"param:foamOriginX",originZ:"param:foamOriginZ",shiftX:"param:foamShiftX",shiftZ:"param:foamShiftZ",size0:1600,size1:250,size2:37,size3:5.3,tau:"param:foamTau",threshold:"param:foamThreshold",wakeCell:"param:wakeCell",wakeGain:"param:wakeFoamGainEff",wakeOriginX:"param:wakeOriginModX",wakeOriginZ:"param:wakeOriginModZ",wakeRelX:"param:wakeRelX",wakeRelZ:"param:wakeRelZ",wakeWidth:256,width:1024}}]},{name:"foamTex.mips",type:"mipmap",resource:"foamTex"}],frameSwaps:[["wakeA","wakeB"],["foamA","foamB"]],exports:{disp0:"dispTex0",disp1:"dispTex1",disp2:"dispTex2",disp3:"dispTex3",foam:"foamTex",slope0:"slopeTex0",slope1:"slopeTex1",slope2:"slopeTex2",slope3:"slopeTex3",wake:"wakeTex"},exportParams:["waterModX0","waterModZ0","waterModX1","waterModZ1","waterModX2","waterModZ2","waterModX3","waterModZ3","wakeCamRelX","wakeCamRelZ","wakeOriginModX","wakeOriginModZ","wakeCell","wakeOn"]},"webgl2/medium":{name:"ocean",backend:"webgl2",tier:"medium",version:1,resources:[{name:"h0_0",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"omega_0",kind:"buffer",elementType:"f32",count:4096,width:64},{name:"fftA_0",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"fftB_0",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"fftA2_0",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"fftB2_0",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"dispTex0",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"slopeTex0",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"h0_1",kind:"buffer",elementType:"vec4<f32>",count:16384,width:128},{name:"omega_1",kind:"buffer",elementType:"f32",count:16384,width:128},{name:"fftA_1",kind:"buffer",elementType:"vec4<f32>",count:16384,width:128},{name:"fftB_1",kind:"buffer",elementType:"vec4<f32>",count:16384,width:128},{name:"fftA2_1",kind:"buffer",elementType:"vec4<f32>",count:16384,width:128},{name:"fftB2_1",kind:"buffer",elementType:"vec4<f32>",count:16384,width:128},{name:"dispTex1",kind:"texture",format:"rgba16float",width:128,height:128,mipLevels:8},{name:"slopeTex1",kind:"texture",format:"rgba16float",width:128,height:128,mipLevels:8},{name:"h0_2",kind:"buffer",elementType:"vec4<f32>",count:16384,width:128},{name:"omega_2",kind:"buffer",elementType:"f32",count:16384,width:128},{name:"fftA_2",kind:"buffer",elementType:"vec4<f32>",count:16384,width:128},{name:"fftB_2",kind:"buffer",elementType:"vec4<f32>",count:16384,width:128},{name:"fftA2_2",kind:"buffer",elementType:"vec4<f32>",count:16384,width:128},{name:"fftB2_2",kind:"buffer",elementType:"vec4<f32>",count:16384,width:128},{name:"dispTex2",kind:"texture",format:"rgba16float",width:128,height:128,mipLevels:8},{name:"slopeTex2",kind:"texture",format:"rgba16float",width:128,height:128,mipLevels:8},{name:"h0_3",kind:"buffer",elementType:"vec4<f32>",count:16384,width:128},{name:"omega_3",kind:"buffer",elementType:"f32",count:16384,width:128},{name:"fftA_3",kind:"buffer",elementType:"vec4<f32>",count:16384,width:128},{name:"fftB_3",kind:"buffer",elementType:"vec4<f32>",count:16384,width:128},{name:"fftA2_3",kind:"buffer",elementType:"vec4<f32>",count:16384,width:128},{name:"fftB2_3",kind:"buffer",elementType:"vec4<f32>",count:16384,width:128},{name:"dispTex3",kind:"texture",format:"rgba16float",width:128,height:128,mipLevels:8},{name:"slopeTex3",kind:"texture",format:"rgba16float",width:128,height:128,mipLevels:8},{name:"wakeA",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"wakeB",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"wakeF",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"wakeG",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"wakeC",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"wakeTex",kind:"texture",format:"rgba16float",width:256,height:256,mipLevels:9},{name:"foamA",kind:"buffer",elementType:"f32",count:1048576,width:1024},{name:"foamB",kind:"buffer",elementType:"f32",count:1048576,width:1024},{name:"foamTex",kind:"texture",format:"rgba16float",width:1024,height:1024,mipLevels:11}],passes:[{name:"c0.spectrum",kernel:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",target:{width:64,height:64,elioWidth:64},bindings:[{name:"h0",resource:"h0_0"},{name:"omega",resource:"omega_0"}],uniforms:[{name:"spec",values:{cascade:0,fetchKm:"param:fetch",gamma:"param:gamma",kMax:.12,kMin:.003926990816987242,loopPeriod:"param:loopPeriod",n:64,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:1600,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c0.evolve",kernel:"ocean_evolve",target:{width:64,height:64,elioWidth:64},bindings:[{name:"h0",resource:"h0_0"},{name:"omega",resource:"omega_0"},{name:"outA",resource:"fftA_0"},{name:"outB",resource:"fftB_0"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:64,t:"param:phaseTime",tile:1600}}]},{name:"c0.fft.rows",type:"repeat",count:6,index:"s",swap:[["fftA_0","fftA2_0"],["fftB_0","fftB2_0"]],body:[{name:"A",kernel:"fft_gather_64",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftA_0"},{name:"dst",resource:"fftA2_0"}],uniforms:[{name:"fft",values:{axis:0,n:64,stage:"param:s"}}]},{name:"B",kernel:"fft_gather_64",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftB_0"},{name:"dst",resource:"fftB2_0"}],uniforms:[{name:"fft",values:{axis:0,n:64,stage:"param:s"}}]}]},{name:"c0.fft.cols",type:"repeat",count:6,index:"s",swap:[["fftA_0","fftA2_0"],["fftB_0","fftB2_0"]],body:[{name:"A",kernel:"fft_gather_64",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftA_0"},{name:"dst",resource:"fftA2_0"}],uniforms:[{name:"fft",values:{axis:1,n:64,stage:"param:s"}}]},{name:"B",kernel:"fft_gather_64",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftB_0"},{name:"dst",resource:"fftB2_0"}],uniforms:[{name:"fft",values:{axis:1,n:64,stage:"param:s"}}]}]},{name:"c0.assemble",kernel:"ocean_assemble",target:{width:64,height:64,elioWidth:64},bindings:[{name:"fieldA",resource:"fftA_0"},{name:"fieldB",resource:"fftB_0"},{name:"disp",resource:"dispTex0"},{name:"slope",resource:"slopeTex0"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:64}}]},{name:"c0.dispTex0.mips",type:"mipmap",resource:"dispTex0"},{name:"c0.slopeTex0.mips",type:"mipmap",resource:"slopeTex0"},{name:"c1.spectrum",kernel:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",target:{width:128,height:128,elioWidth:128},bindings:[{name:"h0",resource:"h0_1"},{name:"omega",resource:"omega_1"}],uniforms:[{name:"spec",values:{cascade:1,fetchKm:"param:fetch",gamma:"param:gamma",kMax:.8,kMin:.12,loopPeriod:"param:loopPeriod",n:128,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:250,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c1.evolve",kernel:"ocean_evolve",target:{width:128,height:128,elioWidth:128},bindings:[{name:"h0",resource:"h0_1"},{name:"omega",resource:"omega_1"},{name:"outA",resource:"fftA_1"},{name:"outB",resource:"fftB_1"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:128,t:"param:phaseTime",tile:250}}]},{name:"c1.fft.rows",type:"repeat",count:7,index:"s",swap:[["fftA_1","fftA2_1"],["fftB_1","fftB2_1"]],body:[{name:"A",kernel:"fft_gather_128",target:{width:128,height:128,elioWidth:128},bindings:[{name:"src",resource:"fftA_1"},{name:"dst",resource:"fftA2_1"}],uniforms:[{name:"fft",values:{axis:0,n:128,stage:"param:s"}}]},{name:"B",kernel:"fft_gather_128",target:{width:128,height:128,elioWidth:128},bindings:[{name:"src",resource:"fftB_1"},{name:"dst",resource:"fftB2_1"}],uniforms:[{name:"fft",values:{axis:0,n:128,stage:"param:s"}}]}]},{name:"c1.fft.cols",type:"repeat",count:7,index:"s",swap:[["fftA_1","fftA2_1"],["fftB_1","fftB2_1"]],body:[{name:"A",kernel:"fft_gather_128",target:{width:128,height:128,elioWidth:128},bindings:[{name:"src",resource:"fftA_1"},{name:"dst",resource:"fftA2_1"}],uniforms:[{name:"fft",values:{axis:1,n:128,stage:"param:s"}}]},{name:"B",kernel:"fft_gather_128",target:{width:128,height:128,elioWidth:128},bindings:[{name:"src",resource:"fftB_1"},{name:"dst",resource:"fftB2_1"}],uniforms:[{name:"fft",values:{axis:1,n:128,stage:"param:s"}}]}]},{name:"c1.assemble",kernel:"ocean_assemble",target:{width:128,height:128,elioWidth:128},bindings:[{name:"fieldA",resource:"fftA_1"},{name:"fieldB",resource:"fftB_1"},{name:"disp",resource:"dispTex1"},{name:"slope",resource:"slopeTex1"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:128}}]},{name:"c1.dispTex1.mips",type:"mipmap",resource:"dispTex1"},{name:"c1.slopeTex1.mips",type:"mipmap",resource:"slopeTex1"},{name:"c2.spectrum",kernel:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",target:{width:128,height:128,elioWidth:128},bindings:[{name:"h0",resource:"h0_2"},{name:"omega",resource:"omega_2"}],uniforms:[{name:"spec",values:{cascade:2,fetchKm:"param:fetch",gamma:"param:gamma",kMax:5.4,kMin:.8,loopPeriod:"param:loopPeriod",n:128,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:37,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c2.evolve",kernel:"ocean_evolve",target:{width:128,height:128,elioWidth:128},bindings:[{name:"h0",resource:"h0_2"},{name:"omega",resource:"omega_2"},{name:"outA",resource:"fftA_2"},{name:"outB",resource:"fftB_2"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:128,t:"param:phaseTime",tile:37}}]},{name:"c2.fft.rows",type:"repeat",count:7,index:"s",swap:[["fftA_2","fftA2_2"],["fftB_2","fftB2_2"]],body:[{name:"A",kernel:"fft_gather_128",target:{width:128,height:128,elioWidth:128},bindings:[{name:"src",resource:"fftA_2"},{name:"dst",resource:"fftA2_2"}],uniforms:[{name:"fft",values:{axis:0,n:128,stage:"param:s"}}]},{name:"B",kernel:"fft_gather_128",target:{width:128,height:128,elioWidth:128},bindings:[{name:"src",resource:"fftB_2"},{name:"dst",resource:"fftB2_2"}],uniforms:[{name:"fft",values:{axis:0,n:128,stage:"param:s"}}]}]},{name:"c2.fft.cols",type:"repeat",count:7,index:"s",swap:[["fftA_2","fftA2_2"],["fftB_2","fftB2_2"]],body:[{name:"A",kernel:"fft_gather_128",target:{width:128,height:128,elioWidth:128},bindings:[{name:"src",resource:"fftA_2"},{name:"dst",resource:"fftA2_2"}],uniforms:[{name:"fft",values:{axis:1,n:128,stage:"param:s"}}]},{name:"B",kernel:"fft_gather_128",target:{width:128,height:128,elioWidth:128},bindings:[{name:"src",resource:"fftB_2"},{name:"dst",resource:"fftB2_2"}],uniforms:[{name:"fft",values:{axis:1,n:128,stage:"param:s"}}]}]},{name:"c2.assemble",kernel:"ocean_assemble",target:{width:128,height:128,elioWidth:128},bindings:[{name:"fieldA",resource:"fftA_2"},{name:"fieldB",resource:"fftB_2"},{name:"disp",resource:"dispTex2"},{name:"slope",resource:"slopeTex2"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:128}}]},{name:"c2.dispTex2.mips",type:"mipmap",resource:"dispTex2"},{name:"c2.slopeTex2.mips",type:"mipmap",resource:"slopeTex2"},{name:"c3.spectrum",kernel:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",target:{width:128,height:128,elioWidth:128},bindings:[{name:"h0",resource:"h0_3"},{name:"omega",resource:"omega_3"}],uniforms:[{name:"spec",values:{cascade:3,fetchKm:"param:fetch",gamma:"param:gamma",kMax:37,kMin:5.4,loopPeriod:"param:loopPeriod",n:128,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:5.3,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c3.evolve",kernel:"ocean_evolve",target:{width:128,height:128,elioWidth:128},bindings:[{name:"h0",resource:"h0_3"},{name:"omega",resource:"omega_3"},{name:"outA",resource:"fftA_3"},{name:"outB",resource:"fftB_3"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:128,t:"param:phaseTime",tile:5.3}}]},{name:"c3.fft.rows",type:"repeat",count:7,index:"s",swap:[["fftA_3","fftA2_3"],["fftB_3","fftB2_3"]],body:[{name:"A",kernel:"fft_gather_128",target:{width:128,height:128,elioWidth:128},bindings:[{name:"src",resource:"fftA_3"},{name:"dst",resource:"fftA2_3"}],uniforms:[{name:"fft",values:{axis:0,n:128,stage:"param:s"}}]},{name:"B",kernel:"fft_gather_128",target:{width:128,height:128,elioWidth:128},bindings:[{name:"src",resource:"fftB_3"},{name:"dst",resource:"fftB2_3"}],uniforms:[{name:"fft",values:{axis:0,n:128,stage:"param:s"}}]}]},{name:"c3.fft.cols",type:"repeat",count:7,index:"s",swap:[["fftA_3","fftA2_3"],["fftB_3","fftB2_3"]],body:[{name:"A",kernel:"fft_gather_128",target:{width:128,height:128,elioWidth:128},bindings:[{name:"src",resource:"fftA_3"},{name:"dst",resource:"fftA2_3"}],uniforms:[{name:"fft",values:{axis:1,n:128,stage:"param:s"}}]},{name:"B",kernel:"fft_gather_128",target:{width:128,height:128,elioWidth:128},bindings:[{name:"src",resource:"fftB_3"},{name:"dst",resource:"fftB2_3"}],uniforms:[{name:"fft",values:{axis:1,n:128,stage:"param:s"}}]}]},{name:"c3.assemble",kernel:"ocean_assemble",target:{width:128,height:128,elioWidth:128},bindings:[{name:"fieldA",resource:"fftA_3"},{name:"fieldB",resource:"fftB_3"},{name:"disp",resource:"dispTex3"},{name:"slope",resource:"slopeTex3"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:128}}]},{name:"c3.dispTex3.mips",type:"mipmap",resource:"dispTex3"},{name:"c3.slopeTex3.mips",type:"mipmap",resource:"slopeTex3"},{name:"wake.source",kernel:"wake_source",when:"param:wakeEnabled",target:{width:256,height:256,elioWidth:256},bindings:[{name:"wakeIn",resource:"wakeA"},{name:"fftOut",resource:"wakeF"},{name:"cleanOut",resource:"wakeC"}],uniforms:[{name:"wake",values:{bowFoam:"param:wakeBowFoam",bowGain:"param:wakeBowGain",cell:"param:wakeCell",depth:"param:wakeDepth",diffuse:"param:wakeDiffuse",dt:"param:wakeDt",foamSpeed:5,fullSpeed:3,heading:"param:hullHeading",hullBeam:"param:hullBeam",hullLen:"param:hullLength",hullU:"param:wakeHullU",hullV:"param:wakeHullV",jitter:"param:wakeJitter",originX:"param:wakeOriginX",originZ:"param:wakeOriginZ",reset:"param:wakeReset",shiftX:"param:wakeShiftX",shiftZ:"param:wakeShiftZ",speed:"param:hullSpeed",srcBeam:"param:wakeSrcBeam",srcLength:"param:wakeSrcLength",tau:"param:wakeFoamTau",width:256}}]},{name:"wake.fft1.rows",type:"repeat",when:"param:wakeEnabled",count:8,index:"s",swap:[["wakeF","wakeG"]],body:[{name:"stage",kernel:"fft_gather_256",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"wakeF"},{name:"dst",resource:"wakeG"}],uniforms:[{name:"fft",values:{axis:0,n:256,stage:"param:s"}}]}]},{name:"wake.fft1.cols",type:"repeat",when:"param:wakeEnabled",count:8,index:"s",swap:[["wakeF","wakeG"]],body:[{name:"stage",kernel:"fft_gather_256",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"wakeF"},{name:"dst",resource:"wakeG"}],uniforms:[{name:"fft",values:{axis:1,n:256,stage:"param:s"}}]}]},{name:"wake.symbol",kernel:"wake_symbol",when:"param:wakeEnabled",target:{width:256,height:256,elioWidth:256},bindings:[{name:"fftIn",resource:"wakeF"},{name:"fftOut",resource:"wakeG"}],uniforms:[{name:"sym",values:{n:256,window:"param:wakeLength"}}]},{name:"wake.fft2.rows",type:"repeat",when:"param:wakeEnabled",count:8,index:"s",swap:[["wakeG","wakeF"]],body:[{name:"stage",kernel:"fft_gather_256",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"wakeG"},{name:"dst",resource:"wakeF"}],uniforms:[{name:"fft",values:{axis:0,n:256,stage:"param:s"}}]}]},{name:"wake.fft2.cols",type:"repeat",when:"param:wakeEnabled",count:8,index:"s",swap:[["wakeG","wakeF"]],body:[{name:"stage",kernel:"fft_gather_256",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"wakeG"},{name:"dst",resource:"wakeF"}],uniforms:[{name:"fft",values:{axis:1,n:256,stage:"param:s"}}]}]},{name:"wake.step",kernel:"wake_step",when:"param:wakeEnabled",target:{width:256,height:256,elioWidth:256},bindings:[{name:"qIn",resource:"wakeG"},{name:"cleanIn",resource:"wakeC"},{name:"stateOut",resource:"wakeB"},{name:"wakeTex",resource:"wakeTex"}],uniforms:[{name:"wstep",values:{alpha:"param:wakeAlpha",cell:"param:wakeCell",dt:"param:wakeDt",gravity:"param:wakeGEff",originX:"param:wakeOriginX",originZ:"param:wakeOriginZ",sponge:24,spongeRate:.8,width:256}}]},{name:"wake.wakeTex.mips",type:"mipmap",when:"param:wakeEnabled",resource:"wakeTex"},{name:"foam",kernel:"ocean_foam",target:{width:1024,height:1024,elioWidth:1024},bindings:[{name:"foamIn",resource:"foamA"},{name:"fieldB0",resource:"fftB_0"},{name:"fieldB1",resource:"fftB_1"},{name:"fieldB2",resource:"fftB_2"},{name:"fieldB3",resource:"fftB_3"},{name:"foamOut",resource:"foamB"},{name:"foamTex",resource:"foamTex"},{name:"wakeIn",resource:"wakeB"}],uniforms:[{name:"foam",values:{cascades:4,cell:.5,dt:"param:dt",gain:"param:foamGain",lambda:"param:lambda",n0:64,n1:128,n2:128,n3:128,omX0:"param:foamOriginMod0X",omX1:"param:foamOriginMod1X",omX2:"param:foamOriginMod2X",omX3:"param:foamOriginMod3X",omZ0:"param:foamOriginMod0Z",omZ1:"param:foamOriginMod1Z",omZ2:"param:foamOriginMod2Z",omZ3:"param:foamOriginMod3Z",originX:"param:foamOriginX",originZ:"param:foamOriginZ",shiftX:"param:foamShiftX",shiftZ:"param:foamShiftZ",size0:1600,size1:250,size2:37,size3:5.3,tau:"param:foamTau",threshold:"param:foamThreshold",wakeCell:"param:wakeCell",wakeGain:"param:wakeFoamGainEff",wakeOriginX:"param:wakeOriginModX",wakeOriginZ:"param:wakeOriginModZ",wakeRelX:"param:wakeRelX",wakeRelZ:"param:wakeRelZ",wakeWidth:256,width:1024}}]},{name:"foamTex.mips",type:"mipmap",resource:"foamTex"}],frameSwaps:[["wakeA","wakeB"],["foamA","foamB"]],exports:{disp0:"dispTex0",disp1:"dispTex1",disp2:"dispTex2",disp3:"dispTex3",foam:"foamTex",slope0:"slopeTex0",slope1:"slopeTex1",slope2:"slopeTex2",slope3:"slopeTex3",wake:"wakeTex"},paramsFrom:"webgl2/high"},"webgl2/low":{name:"ocean",backend:"webgl2",tier:"low",version:1,resources:[{name:"h0_0",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"omega_0",kind:"buffer",elementType:"f32",count:4096,width:64},{name:"fftA_0",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"fftB_0",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"fftA2_0",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"fftB2_0",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"dispTex0",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"slopeTex0",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"h0_1",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"omega_1",kind:"buffer",elementType:"f32",count:4096,width:64},{name:"fftA_1",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"fftB_1",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"fftA2_1",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"fftB2_1",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"dispTex1",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"slopeTex1",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"h0_2",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"omega_2",kind:"buffer",elementType:"f32",count:4096,width:64},{name:"fftA_2",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"fftB_2",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"fftA2_2",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"fftB2_2",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"dispTex2",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"slopeTex2",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"wakeA",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"wakeB",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"wakeF",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"wakeG",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"wakeC",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"wakeTex",kind:"texture",format:"rgba16float",width:256,height:256,mipLevels:9},{name:"foamA",kind:"buffer",elementType:"f32",count:1048576,width:1024},{name:"foamB",kind:"buffer",elementType:"f32",count:1048576,width:1024},{name:"foamTex",kind:"texture",format:"rgba16float",width:1024,height:1024,mipLevels:11}],passes:[{name:"c0.spectrum",kernel:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",target:{width:64,height:64,elioWidth:64},bindings:[{name:"h0",resource:"h0_0"},{name:"omega",resource:"omega_0"}],uniforms:[{name:"spec",values:{cascade:0,fetchKm:"param:fetch",gamma:"param:gamma",kMax:.12,kMin:.003926990816987242,loopPeriod:"param:loopPeriod",n:64,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:1600,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c0.evolve",kernel:"ocean_evolve",target:{width:64,height:64,elioWidth:64},bindings:[{name:"h0",resource:"h0_0"},{name:"omega",resource:"omega_0"},{name:"outA",resource:"fftA_0"},{name:"outB",resource:"fftB_0"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:64,t:"param:phaseTime",tile:1600}}]},{name:"c0.fft.rows",type:"repeat",count:6,index:"s",swap:[["fftA_0","fftA2_0"],["fftB_0","fftB2_0"]],body:[{name:"A",kernel:"fft_gather_64",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftA_0"},{name:"dst",resource:"fftA2_0"}],uniforms:[{name:"fft",values:{axis:0,n:64,stage:"param:s"}}]},{name:"B",kernel:"fft_gather_64",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftB_0"},{name:"dst",resource:"fftB2_0"}],uniforms:[{name:"fft",values:{axis:0,n:64,stage:"param:s"}}]}]},{name:"c0.fft.cols",type:"repeat",count:6,index:"s",swap:[["fftA_0","fftA2_0"],["fftB_0","fftB2_0"]],body:[{name:"A",kernel:"fft_gather_64",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftA_0"},{name:"dst",resource:"fftA2_0"}],uniforms:[{name:"fft",values:{axis:1,n:64,stage:"param:s"}}]},{name:"B",kernel:"fft_gather_64",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftB_0"},{name:"dst",resource:"fftB2_0"}],uniforms:[{name:"fft",values:{axis:1,n:64,stage:"param:s"}}]}]},{name:"c0.assemble",kernel:"ocean_assemble",target:{width:64,height:64,elioWidth:64},bindings:[{name:"fieldA",resource:"fftA_0"},{name:"fieldB",resource:"fftB_0"},{name:"disp",resource:"dispTex0"},{name:"slope",resource:"slopeTex0"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:64}}]},{name:"c0.dispTex0.mips",type:"mipmap",resource:"dispTex0"},{name:"c0.slopeTex0.mips",type:"mipmap",resource:"slopeTex0"},{name:"c1.spectrum",kernel:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",target:{width:64,height:64,elioWidth:64},bindings:[{name:"h0",resource:"h0_1"},{name:"omega",resource:"omega_1"}],uniforms:[{name:"spec",values:{cascade:1,fetchKm:"param:fetch",gamma:"param:gamma",kMax:.8,kMin:.12,loopPeriod:"param:loopPeriod",n:64,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:250,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c1.evolve",kernel:"ocean_evolve",target:{width:64,height:64,elioWidth:64},bindings:[{name:"h0",resource:"h0_1"},{name:"omega",resource:"omega_1"},{name:"outA",resource:"fftA_1"},{name:"outB",resource:"fftB_1"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:64,t:"param:phaseTime",tile:250}}]},{name:"c1.fft.rows",type:"repeat",count:6,index:"s",swap:[["fftA_1","fftA2_1"],["fftB_1","fftB2_1"]],body:[{name:"A",kernel:"fft_gather_64",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftA_1"},{name:"dst",resource:"fftA2_1"}],uniforms:[{name:"fft",values:{axis:0,n:64,stage:"param:s"}}]},{name:"B",kernel:"fft_gather_64",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftB_1"},{name:"dst",resource:"fftB2_1"}],uniforms:[{name:"fft",values:{axis:0,n:64,stage:"param:s"}}]}]},{name:"c1.fft.cols",type:"repeat",count:6,index:"s",swap:[["fftA_1","fftA2_1"],["fftB_1","fftB2_1"]],body:[{name:"A",kernel:"fft_gather_64",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftA_1"},{name:"dst",resource:"fftA2_1"}],uniforms:[{name:"fft",values:{axis:1,n:64,stage:"param:s"}}]},{name:"B",kernel:"fft_gather_64",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftB_1"},{name:"dst",resource:"fftB2_1"}],uniforms:[{name:"fft",values:{axis:1,n:64,stage:"param:s"}}]}]},{name:"c1.assemble",kernel:"ocean_assemble",target:{width:64,height:64,elioWidth:64},bindings:[{name:"fieldA",resource:"fftA_1"},{name:"fieldB",resource:"fftB_1"},{name:"disp",resource:"dispTex1"},{name:"slope",resource:"slopeTex1"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:64}}]},{name:"c1.dispTex1.mips",type:"mipmap",resource:"dispTex1"},{name:"c1.slopeTex1.mips",type:"mipmap",resource:"slopeTex1"},{name:"c2.spectrum",kernel:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",target:{width:64,height:64,elioWidth:64},bindings:[{name:"h0",resource:"h0_2"},{name:"omega",resource:"omega_2"}],uniforms:[{name:"spec",values:{cascade:2,fetchKm:"param:fetch",gamma:"param:gamma",kMax:5.4,kMin:.8,loopPeriod:"param:loopPeriod",n:64,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:37,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c2.evolve",kernel:"ocean_evolve",target:{width:64,height:64,elioWidth:64},bindings:[{name:"h0",resource:"h0_2"},{name:"omega",resource:"omega_2"},{name:"outA",resource:"fftA_2"},{name:"outB",resource:"fftB_2"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:64,t:"param:phaseTime",tile:37}}]},{name:"c2.fft.rows",type:"repeat",count:6,index:"s",swap:[["fftA_2","fftA2_2"],["fftB_2","fftB2_2"]],body:[{name:"A",kernel:"fft_gather_64",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftA_2"},{name:"dst",resource:"fftA2_2"}],uniforms:[{name:"fft",values:{axis:0,n:64,stage:"param:s"}}]},{name:"B",kernel:"fft_gather_64",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftB_2"},{name:"dst",resource:"fftB2_2"}],uniforms:[{name:"fft",values:{axis:0,n:64,stage:"param:s"}}]}]},{name:"c2.fft.cols",type:"repeat",count:6,index:"s",swap:[["fftA_2","fftA2_2"],["fftB_2","fftB2_2"]],body:[{name:"A",kernel:"fft_gather_64",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftA_2"},{name:"dst",resource:"fftA2_2"}],uniforms:[{name:"fft",values:{axis:1,n:64,stage:"param:s"}}]},{name:"B",kernel:"fft_gather_64",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftB_2"},{name:"dst",resource:"fftB2_2"}],uniforms:[{name:"fft",values:{axis:1,n:64,stage:"param:s"}}]}]},{name:"c2.assemble",kernel:"ocean_assemble",target:{width:64,height:64,elioWidth:64},bindings:[{name:"fieldA",resource:"fftA_2"},{name:"fieldB",resource:"fftB_2"},{name:"disp",resource:"dispTex2"},{name:"slope",resource:"slopeTex2"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:64}}]},{name:"c2.dispTex2.mips",type:"mipmap",resource:"dispTex2"},{name:"c2.slopeTex2.mips",type:"mipmap",resource:"slopeTex2"},{name:"wake.source",kernel:"wake_source",when:"param:wakeEnabled",target:{width:256,height:256,elioWidth:256},bindings:[{name:"wakeIn",resource:"wakeA"},{name:"fftOut",resource:"wakeF"},{name:"cleanOut",resource:"wakeC"}],uniforms:[{name:"wake",values:{bowFoam:"param:wakeBowFoam",bowGain:"param:wakeBowGain",cell:"param:wakeCell",depth:"param:wakeDepth",diffuse:"param:wakeDiffuse",dt:"param:wakeDt",foamSpeed:5,fullSpeed:3,heading:"param:hullHeading",hullBeam:"param:hullBeam",hullLen:"param:hullLength",hullU:"param:wakeHullU",hullV:"param:wakeHullV",jitter:"param:wakeJitter",originX:"param:wakeOriginX",originZ:"param:wakeOriginZ",reset:"param:wakeReset",shiftX:"param:wakeShiftX",shiftZ:"param:wakeShiftZ",speed:"param:hullSpeed",srcBeam:"param:wakeSrcBeam",srcLength:"param:wakeSrcLength",tau:"param:wakeFoamTau",width:256}}]},{name:"wake.fft1.rows",type:"repeat",when:"param:wakeEnabled",count:8,index:"s",swap:[["wakeF","wakeG"]],body:[{name:"stage",kernel:"fft_gather_256",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"wakeF"},{name:"dst",resource:"wakeG"}],uniforms:[{name:"fft",values:{axis:0,n:256,stage:"param:s"}}]}]},{name:"wake.fft1.cols",type:"repeat",when:"param:wakeEnabled",count:8,index:"s",swap:[["wakeF","wakeG"]],body:[{name:"stage",kernel:"fft_gather_256",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"wakeF"},{name:"dst",resource:"wakeG"}],uniforms:[{name:"fft",values:{axis:1,n:256,stage:"param:s"}}]}]},{name:"wake.symbol",kernel:"wake_symbol",when:"param:wakeEnabled",target:{width:256,height:256,elioWidth:256},bindings:[{name:"fftIn",resource:"wakeF"},{name:"fftOut",resource:"wakeG"}],uniforms:[{name:"sym",values:{n:256,window:"param:wakeLength"}}]},{name:"wake.fft2.rows",type:"repeat",when:"param:wakeEnabled",count:8,index:"s",swap:[["wakeG","wakeF"]],body:[{name:"stage",kernel:"fft_gather_256",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"wakeG"},{name:"dst",resource:"wakeF"}],uniforms:[{name:"fft",values:{axis:0,n:256,stage:"param:s"}}]}]},{name:"wake.fft2.cols",type:"repeat",when:"param:wakeEnabled",count:8,index:"s",swap:[["wakeG","wakeF"]],body:[{name:"stage",kernel:"fft_gather_256",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"wakeG"},{name:"dst",resource:"wakeF"}],uniforms:[{name:"fft",values:{axis:1,n:256,stage:"param:s"}}]}]},{name:"wake.step",kernel:"wake_step",when:"param:wakeEnabled",target:{width:256,height:256,elioWidth:256},bindings:[{name:"qIn",resource:"wakeG"},{name:"cleanIn",resource:"wakeC"},{name:"stateOut",resource:"wakeB"},{name:"wakeTex",resource:"wakeTex"}],uniforms:[{name:"wstep",values:{alpha:"param:wakeAlpha",cell:"param:wakeCell",dt:"param:wakeDt",gravity:"param:wakeGEff",originX:"param:wakeOriginX",originZ:"param:wakeOriginZ",sponge:24,spongeRate:.8,width:256}}]},{name:"wake.wakeTex.mips",type:"mipmap",when:"param:wakeEnabled",resource:"wakeTex"},{name:"foam",kernel:"ocean_foam",target:{width:1024,height:1024,elioWidth:1024},bindings:[{name:"foamIn",resource:"foamA"},{name:"fieldB0",resource:"fftB_0"},{name:"fieldB1",resource:"fftB_1"},{name:"fieldB2",resource:"fftB_2"},{name:"fieldB3",resource:"fftB_2"},{name:"foamOut",resource:"foamB"},{name:"foamTex",resource:"foamTex"},{name:"wakeIn",resource:"wakeB"}],uniforms:[{name:"foam",values:{cascades:3,cell:.5,dt:"param:dt",gain:"param:foamGain",lambda:"param:lambda",n0:64,n1:64,n2:64,n3:64,omX0:"param:foamOriginMod0X",omX1:"param:foamOriginMod1X",omX2:"param:foamOriginMod2X",omX3:"param:foamOriginMod2X",omZ0:"param:foamOriginMod0Z",omZ1:"param:foamOriginMod1Z",omZ2:"param:foamOriginMod2Z",omZ3:"param:foamOriginMod2Z",originX:"param:foamOriginX",originZ:"param:foamOriginZ",shiftX:"param:foamShiftX",shiftZ:"param:foamShiftZ",size0:1600,size1:250,size2:37,size3:37,tau:"param:foamTau",threshold:"param:foamThreshold",wakeCell:"param:wakeCell",wakeGain:"param:wakeFoamGainEff",wakeOriginX:"param:wakeOriginModX",wakeOriginZ:"param:wakeOriginModZ",wakeRelX:"param:wakeRelX",wakeRelZ:"param:wakeRelZ",wakeWidth:256,width:1024}}]},{name:"foamTex.mips",type:"mipmap",resource:"foamTex"}],frameSwaps:[["wakeA","wakeB"],["foamA","foamB"]],exports:{disp0:"dispTex0",disp1:"dispTex1",disp2:"dispTex2",foam:"foamTex",slope0:"slopeTex0",slope1:"slopeTex1",slope2:"slopeTex2",wake:"wakeTex"},paramsFrom:"webgl2/high"}}})})(),(function(){function U(m){return m.replace(/_([a-z0-9])/g,function(g,A){return A.toUpperCase()})}function X(m){return typeof m=="boolean"?m?1:0:Number(m)}function F(m,g,A){if(typeof m=="string"){if(m.indexOf("param:")!==0)throw new Error("elio-graph: "+A+": bad value "+m);const Z=m.slice(6);if(!(Z in g))throw new Error("elio-graph: parameter "+Z+" was not supplied");return X(g[Z])}return X(m)}function L(m,g,A,Z){const _=new ArrayBuffer(Math.max(16,Math.ceil(m.size/16)*16)),v=new DataView(_),w={};for(const E of m.fields){let y=g[E.name];if(y===void 0){if(!/^_pad/.test(E.name))throw new Error("elio-graph: "+Z+": uniform "+m.struct+" lacks a value for "+E.name);y=0}w[E.name]=!0;const C=F(y,A,Z);if(E.type==="u32")v.setUint32(E.offset,C>>>0,!0);else if(E.type==="i32")v.setInt32(E.offset,C|0,!0);else if(E.type==="f32")v.setFloat32(E.offset,C,!0);else throw new Error("elio-graph: "+Z+": unsupported uniform type "+E.type)}for(const E of Object.keys(g))if(!w[E])throw new Error("elio-graph: "+Z+": uniform "+m.struct+" has no field "+E);return _}function d(m){for(const g of Object.keys(m))if(typeof m[g]=="string")return!1;return!0}const a={x:1,z:1,heading:1,speed:1},N={time:1,dt:1,cameraX:1,cameraY:1,cameraZ:1},q={add:function(m,g){return m+g},sub:function(m,g){return m-g},mul:function(m,g){return m*g},div:function(m,g){return g===0?0:m/g},mod:function(m,g){return g===0?0:m-Math.floor(m/g)*g},floor:function(m){return Math.floor(m)},sin:function(m){return Math.sin(m)},cos:function(m){return Math.cos(m)},min:function(m,g){return Math.min(m,g)},max:function(m,g){return Math.max(m,g)}};function H(m,g){return/\[\]$/.test(m)?Float32Array.from(g||[]):(g=Number(g),Number.isFinite(g)?m==="bool"?g?1:0:m==="u32"?Math.max(0,Math.floor(g))>>>0:m==="i32"?Math.trunc(g)|0:g:0)}function Q(m){const g={},A={},Z={},_={},v={};let w=!0,E=null,y={};const C={},re={},ie=function(k){return k.tween>0?E===null?k.tween:E:0};for(const k of m){const G=k.source||{kind:"value",value:0};G.kind==="value"&&(v[k.name]=H(k.type,/\[\]$/.test(k.type)?G.value:G.value||0))}const W=function(k,G){const O=G||g;if(typeof k=="number")return k;if(typeof k=="string"&&k.indexOf("param:")===0){const R=k.slice(6);return R in O?O[R]:0}if(k&&typeof k=="object"&&typeof k.op=="string"){const R=q[k.op];if(!R)throw new Error("elio-graph: unknown op "+k.op);return R(W(k.args[0],O),k.args.length>1?W(k.args[1],O):0)}throw new Error("elio-graph: bad argument "+k)},z={current:g,changed:{},setNodes:function(k){y=k||{}},setTween:function(k){E=Number.isFinite(k)&&k>=0?k:null},set:function(k,G){const O=m.find(function(s){return s.name===k});if(!O||O.source&&O.source.kind!=="value")throw new Error("elio-graph: "+k+" is not a value parameter");const R=H(O.type,G);if(R!==v[k]){if(typeof R!="number"){v[k]=R;return}v[k]=R,ie(O)>0&&!w?Z[k]={from:g[k],start:null}:delete Z[k]}},snapshot:function(){return Object.assign({},g)},derive:function(k,G){const O=Object.assign({},g);for(const R of m){const s=R.source||{kind:"value",value:0};if(s.kind==="frame"&&s.name==="dt")O[R.name]=H(R.type,k);else if(s.kind==="expr")O[R.name]=H(R.type,q[s.op](W(s.args[0],O),s.args.length>1?W(s.args[1],O):0));else if(s.kind==="delta"){const u=String(s.of).replace(/^param:/,"");O[R.name]=H(R.type,G?(O[u]||0)-(G[u]||0):0)}}return O},evaluate:function(k){const G=Number(k.time)||0,O=Number(k.dt)||0,R=Number.isFinite(k.accumulateDt)?Number(k.accumulateDt):O,s={},u=function(o){if(o in s)return s[o];const x=typeof k.nodePose=="function"?k.nodePose(o):null;if(!x)return s[o]=null;const P=C[o];let M=Number(x.speed);return Number.isFinite(M)||(M=P&&O>0?Math.hypot(x.x-P.x,x.z-P.z)/O:0),s[o]={x:Number(x.x)||0,z:Number(x.z)||0,heading:Number(x.heading)||0,speed:M}};for(const o of m){const x=o.source||{kind:"value",value:0};let P=0;if(x.kind==="value"){P=v[o.name];const M=Z[o.name];if(M){M.start===null&&(M.start=G);const K=Math.min(1,Math.max(0,(G-M.start)/ie(o)));K>=1?delete Z[o.name]:P=M.from+(P-M.from)*K}}else if(x.kind==="frame"){if(!N[x.name])throw new Error("elio-graph: unknown frame source "+x.name);P=x.name==="time"?G:x.name==="dt"?O:Number(k[x.name])||0}else if(x.kind==="expr"){const M=q[x.op];if(!M)throw new Error("elio-graph: unknown op "+x.op);P=M(W(x.args[0]),x.args.length>1?W(x.args[1]):0)}else if(x.kind==="node"){if(!a[x.field])throw new Error("elio-graph: unknown node field "+x.field);const M=u(x.node in y?y[x.node]:x.node);P=M?M[x.field]:x.field==="speed"?0:re[o.name]||0,re[o.name]=P}else if(x.kind==="delta"){const M=String(x.of).replace(/^param:/,"");P=w?0:(g[M]||0)-(A[M]||0)}else if(x.kind==="accumulate")_[o.name]=(_[o.name]||0)+W(x.of)*R,P=_[o.name];else throw new Error("elio-graph: unknown parameter source "+x.kind);g[o.name]=/\[\]$/.test(o.type)?v[o.name]:H(o.type,P)}for(const o of Object.keys(s))s[o]&&(C[o]={x:s[o].x,z:s[o].z});for(const o of m)z.changed[o.name]=w||g[o.name]!==A[o.name];for(const o of m)A[o.name]=g[o.name];w=!1}};return z}function n(m,g,A){if(!m||m==="always")return!0;if(m.indexOf("param:")===0)return!!g[m.slice(6)];if(m.indexOf("changed:")===0)return m.slice(8).split(",").some(function(Z){return!!A[Z.trim()]});throw new Error("elio-graph: bad when "+m)}function l(m,g){const A=Object.assign({},m);if(m.paramsFrom){const _=g&&g.graphs&&g.graphs[m.paramsFrom];if(!_||_.backend!==m.backend||_.paramsFrom)throw new Error("elio-graph: paramsFrom "+m.paramsFrom+" does not name a graph of "+m.backend+" that owns its parameters");A.params=_.params,A.exportParams=_.exportParams}delete A.paramsFrom,A.params=A.params||[],A.exportParams=A.exportParams||[],A.frameSwaps=A.frameSwaps||[],A.exports=A.exports||{};const Z=function(_){const v=Object.assign({},_);if(v.type=_.type||"kernel",v.when=_.when||"always",v.type==="kernel"){const w=g&&g.kernels&&g.kernels[U(_.kernel)];!v.entry&&w&&(v.entry=w.entry),_.bindings&&(v.bindings=_.bindings.map(function(E){return Object.assign({level:0},E)})),v.uniforms||(v.uniforms=[])}return v.type==="repeat"&&(v.body=(_.body||[]).map(Z)),v};return A.passes=(m.passes||[]).map(Z),A}function c(m,g){const A=g+(m.offset||0);return m.shr?Math.max(m.shr>>A,1):A}function D(m){const g={},A={},Z={};for(const y of m.resources)g[y.name]=y;for(const y of m.params)Z[y.name]=!0;const _=function(y){return y in A?A[y]:y},v=function(y,C,re,ie){const W=[];for(const z of y){if(z.type!=="repeat"){W.push(w(z,C,re,ie));continue}const k=re+z.name;if(!(z.count>=1))throw new Error("elio-graph: repeat "+k+": count "+z.count+", want at least 1");if(!z.index||Z[z.index])throw new Error("elio-graph: repeat "+k+": index "+z.index+" is empty or names a declared param");if(!z.body||!z.body.length)throw new Error("elio-graph: repeat "+k+": body is empty");if(z.index in C)throw new Error("elio-graph: repeat "+k+": index "+z.index+" is already in use by an outer repeat");for(const O of z.swap||[])if(!g[O[0]]||!g[O[1]])throw new Error("elio-graph: repeat "+k+": swap names an unknown resource");const G=z.when&&z.when!=="always"?z.when:ie;for(let O=0;O<z.count;O++){const R=Object.assign({},C,{[z.index]:O});for(const s of v(z.body,R,k+"["+O+"].",G))W.push(s);for(const s of z.swap||[]){const u=_(s[0]),o=_(s[1]);A[s[0]]=o,A[s[1]]=u}}}return W},w=function(y,C,re,ie){const W=Object.assign({},y,{name:re+y.name});return ie&&(!y.when||y.when==="always")&&(W.when=ie),y.resource&&(W.resource=_(y.resource)),y.from&&(W.from=_(y.from)),y.to&&(W.to=_(y.to)),y.bindings&&(W.bindings=y.bindings.map(function(z){const k=Object.assign({},z,{resource:_(z.resource)});if(z.levelAt){if(!(z.levelAt.index in C))throw new Error("elio-graph: pass "+W.name+": index "+z.levelAt.index+" is not in scope");k.level=c(z.levelAt,C[z.levelAt.index]),delete k.levelAt}return k})),y.uniforms&&(W.uniforms=y.uniforms.map(function(z){const k={};for(const G of Object.keys(z.values)){let O=z.values[G];if(O&&typeof O=="object"){if(!(O.index in C))throw new Error("elio-graph: pass "+W.name+": index "+O.index+" is not in scope");O=c(O,C[O.index])}else typeof O=="string"&&O.indexOf("param:")===0&&O.slice(6)in C&&(O=C[O.slice(6)]);k[G]=O}return Object.assign({},z,{values:k})})),W},E=v(m.passes,{},"","");for(const y of Object.keys(A))if(A[y]!==y)throw new Error("elio-graph: swaps are not restored: "+y+" still refers to "+A[y]+" at the end of the passes");return E}function J(m,g,A){if(!m||m.version!==1)throw new Error("elio-graph: unsupported graph version");if(m.backend!==A)throw new Error("elio-graph: graph is for "+m.backend+", not "+A);const Z={};for(const w of m.resources)Z[w.name]=w;const _={};for(const w of m.params)_[w.name]=!0;const v=function(w){if(typeof w=="string"&&w.indexOf("param:")===0&&!_[w.slice(6)])throw new Error("elio-graph: undeclared parameter "+w.slice(6))};for(const w of D(m)){const E=w.when||"always";if(E.indexOf("param:")===0)v(E);else if(E.indexOf("changed:")===0){for(const y of E.slice(8).split(","))if(!_[y.trim()])throw new Error("elio-graph: undeclared parameter "+y.trim())}else if(E!=="always")throw new Error("elio-graph: pass "+w.name+": bad when "+E);if(w.type==="mipmap"){if(!Z[w.resource])throw new Error("elio-graph: pass "+w.name+": unknown resource "+w.resource);continue}if(w.type==="copy"){if(A!=="webgl2")throw new Error("elio-graph: pass "+w.name+": copy passes exist only on webgl2");for(const y of[w.from,w.to])if(!Z[y]||Z[y].kind!=="buffer")throw new Error("elio-graph: pass "+w.name+": unknown resource "+y+" (a copy joins two buffers)");continue}if(!g.kernels[U(w.kernel)])throw new Error("elio-graph: pass "+w.name+": the module has no kernel "+w.kernel);for(const y of w.bindings)if(!Z[y.resource])throw new Error("elio-graph: pass "+w.name+": unknown resource "+y.resource);for(const y of w.uniforms||[])for(const C of Object.keys(y.values))v(y.values[C])}for(const w of m.exportParams||[])if(!_[w])throw new Error("elio-graph: exportParams names undeclared parameter "+w);for(const w of m.resources)if(w.feed&&!_[w.feed])throw new Error("elio-graph: resource "+w.name+" is fed by undeclared parameter "+w.feed);for(const w of m.frameSwaps||[])for(const E of w)if(!Z[E])throw new Error("elio-graph: frameSwaps names unknown resource "+E);for(const w of Object.keys(m.exports||{})){const E=Z[m.exports[w]];if(!E||E.kind!=="texture"&&A!=="webgpu")throw new Error("elio-graph: export "+w+" must name a texture resource")}}function ae(m){const g=m.backend,A=m.module,Z=m.host||{},_={graph:null,backend:g,ready:!1,failed:null,disposed:!1,buildCount:0,run:G,nodePose:C,gpuTiming:re,setValue:k,setNodes:ie,setTween:W,value:z,hasParam:y,params:null,exportParams:O,export:R,dispose:u,fail:s};let v=null,w=null;const E=function(o){if(!_.failed){if(_.failed=o&&o.fallback||"error",_.error=o,v){try{v.dispose()}catch{}v=null}_.ready=!1,m.onFail&&m.onFail(_.failed,o),!(o&&o.fallback)&&typeof console<"u"&&console.warn&&console.warn("elio-graph: graph disabled: "+(o&&o.message?o.message:String(o)))}};try{let o=m.graph||A.graphs&&A.graphs[g+"/"+(m.tier||"high")];if(!o)throw Object.assign(new Error("elio-graph: no graph for "+g+"/"+m.tier),{fallback:"no-graph"});o=l(o,A),J(o,A,g);const x=window.__gosx_elio_graph.executors[g];if(!x)throw Object.assign(new Error("elio-graph: no executor for "+g),{fallback:"no-executor"});_.graph=o,w=_.params=Q(o.params),v=x({steps:D(o).map(function(P){return{pass:P}}),graph:o,module:A,host:Z,pack:L,isStatic:d,kernelKey:U,holds:n,label:m.label||o.name}),_.buildCount=1,_.ready=v.isReady()}catch(o){E(o)}function y(o){return!!(_.graph&&_.graph.params.some(function(x){return x.name===o}))}function C(o){return Z.nodePose?Z.nodePose(o):null}function re(){return v&&v.gpuTiming?v.gpuTiming():null}function ie(o){w&&w.setNodes(o)}function W(o){w&&w.setTween(o)}function z(o){const x=v&&v.held?v.held(o):void 0;return x!==void 0?x:w&&o in w.current?w.current[o]:0}function k(o,x){if(!(!w||_.failed))try{w.set(o,x)}catch(P){E(P)}}function G(o){if(!v||_.failed||_.disposed)return!1;try{if(_.ready=v.isReady(),!_.ready)return!1;const x=o&&!o.nodePose&&Z.nodePose?Object.assign({},o,{nodePose:Z.nodePose}):o||{};return w.evaluate(x),v.run(w.current,x,w.changed,w)!==!1}catch(x){return E(x),!1}}function O(){const o={};if(w&&_.graph)for(const x of _.graph.exportParams||[])o[x]=z(x);return o}function R(o){return v&&!_.failed?v.export(o):null}function s(o){E({message:o,fallback:o})}function u(){if(!_.disposed&&(_.disposed=!0,_.ready=!1,v)){try{v.dispose()}catch{}v=null}}return _}window.__gosx_elio_graph={create:ae,expand:D,normalize:l,pack:L,kernelKey:U,check:J,holds:n,createParams:Q,executors:{}}})(),(function(){const U=`#version 300 es
void main() { vec2 p = vec2(float((gl_VertexID << 1) & 2), float(gl_VertexID & 2)); gl_Position = vec4(p * 2. - 1., 0., 1.); }`;function X(d){return{draw:d.getParameter(d.DRAW_FRAMEBUFFER_BINDING),read:d.getParameter(d.READ_FRAMEBUFFER_BINDING),viewport:d.getParameter(d.VIEWPORT),program:d.getParameter(d.CURRENT_PROGRAM),vao:d.getParameter(d.VERTEX_ARRAY_BINDING),active:d.getParameter(d.ACTIVE_TEXTURE),blend:d.isEnabled(d.BLEND),depth:d.isEnabled(d.DEPTH_TEST),cull:d.isEnabled(d.CULL_FACE),scissor:d.isEnabled(d.SCISSOR_TEST),colorMask:d.getParameter(d.COLOR_WRITEMASK),depthMask:d.getParameter(d.DEPTH_WRITEMASK)}}function F(d,a){d.bindBuffer(d.UNIFORM_BUFFER,null),d.bindBufferBase(d.UNIFORM_BUFFER,0,null),d.bindBuffer(d.PIXEL_PACK_BUFFER,null),d.bindBuffer(d.PIXEL_UNPACK_BUFFER,null),d.bindFramebuffer(d.DRAW_FRAMEBUFFER,a.draw),d.bindFramebuffer(d.READ_FRAMEBUFFER,a.read),d.viewport(a.viewport[0],a.viewport[1],a.viewport[2],a.viewport[3]),d.useProgram(a.program),d.bindVertexArray(a.vao),d.activeTexture(a.active);const N=function(q,H){H?d.enable(q):d.disable(q)};N(d.BLEND,a.blend),N(d.DEPTH_TEST,a.depth),N(d.CULL_FACE,a.cull),N(d.SCISSOR_TEST,a.scissor),d.colorMask(a.colorMask[0],a.colorMask[1],a.colorMask[2],a.colorMask[3]),d.depthMask(a.depthMask)}function L(d){const a=d.host.gl,N=d.graph,q=d.module,H=q.kernels,Q=d.host.firstUnit||4;if(!(d.host.caps?d.host.caps(a):{}).colorBufferFloat)throw Object.assign(new Error("EXT_color_buffer_float is not available"),{fallback:"EXT_color_buffer_float"});const l=function(e,t){const i=a.getParameter(e);return typeof i=="number"?i:t};if(l(a.MAX_DRAW_BUFFERS,0)<2||l(a.MAX_TEXTURE_IMAGE_UNITS,0)<Q+5)throw Object.assign(new Error("too few texture units or draw buffers"),{fallback:"texture-units"});for(const e of d.steps){const t=e.pass;if(!t||t.type!=="kernel")continue;const i=H[d.kernelKey(t.kernel)];if(!i.glsl||!i.gatherLayout)throw Object.assign(new Error("kernel "+t.kernel+" has no GLSL gather form"),{fallback:"kernel:"+t.kernel})}const c=[],D=[],J=[],ae=[],m=Object.create(null),g={};for(const e of Object.keys(N.exports||{}))g[N.exports[e]]=!0;const A=a.createVertexArray(),Z={split:!1,disposed:!1};let _=null;const v=Object.create(null),w=new Map,E=new Map;let y=0;const C={rgba16float:a.RGBA16F,rgba32float:a.RGBA32F,r32float:a.R32F};function re(e,t,i,r,h){const p=a.createTexture();return c.push(p),a.activeTexture(a.TEXTURE0+Q),a.bindTexture(a.TEXTURE_2D,p),a.texStorage2D(a.TEXTURE_2D,r,i,e,t),a.texParameteri(a.TEXTURE_2D,a.TEXTURE_MIN_FILTER,h?r>1?a.LINEAR_MIPMAP_LINEAR:a.LINEAR:a.NEAREST),a.texParameteri(a.TEXTURE_2D,a.TEXTURE_MAG_FILTER,h?a.LINEAR:a.NEAREST),a.texParameteri(a.TEXTURE_2D,a.TEXTURE_WRAP_S,h?a.REPEAT:a.CLAMP_TO_EDGE),a.texParameteri(a.TEXTURE_2D,a.TEXTURE_WRAP_T,h?a.REPEAT:a.CLAMP_TO_EDGE),p}function ie(e,t){const i=a.createShader(e);if(ae.push(i),a.shaderSource(i,t),a.compileShader(i),!a.getShaderParameter(i,a.COMPILE_STATUS))throw new Error("elio-graph: shader did not compile: "+a.getShaderInfoLog(i));return i}function W(e){if(m[e])return m[e];const t=H[d.kernelKey(e)];_||(_=ie(a.VERTEX_SHADER,U));const i=ie(a.FRAGMENT_SHADER,t.glsl),r=a.createProgram();a.attachShader(r,_),a.attachShader(r,i),a.linkProgram(r);const h=t.gatherLayout,p=m[e]={handle:r,layout:h,kernel:t};if(!a.getProgramParameter(r,a.LINK_STATUS))throw new Error("elio-graph: program "+e+" did not link: "+a.getProgramInfoLog(r));return a.useProgram(r),p.width=a.getUniformLocation(r,"elio_width"),p.lens=h.inputs.map(function(T){return a.getUniformLocation(r,T.length)}),p.widths=h.inputs.map(function(T){return T.width?a.getUniformLocation(r,T.width):null}),h.inputs.forEach(function(T,b){a.uniform1i(a.getUniformLocation(r,T.sampler),Q+b)}),h.uniforms.forEach(function(T,b){a.uniformBlockBinding(r,a.getUniformBlockIndex(r,T.block),b)}),p}function z(e,t){const r=(e.uniformsStd140||e.uniforms).find(function(h){return h.name===t});if(!r)throw new Error("elio-graph: kernel has no uniform block "+t);return r}function k(){for(const e of N.resources){let t;if(e.kind==="buffer"){const i=Math.ceil(e.count/e.width);t={name:e.name,id:++y,kind:"buffer",version:0,w:e.width,h:i,levels:1,tex:re(e.width,i,a.RGBA32F,1,!1)}}else{const i=e.mipLevels||1,r=C[e.format];if(!r)throw new Error("elio-graph: unsupported format "+e.format);const h=e.format==="rgba16float"&&(i>1||!!g[e.name]);t={name:e.name,id:++y,kind:"texture",version:0,w:e.width,h:e.height,levels:i,tex:re(e.width,e.height,r,i,h)}}v[e.name]=t}}function G(e){const t=a.createFramebuffer();D.push(t),a.bindFramebuffer(a.FRAMEBUFFER,t);const i=[];for(const r of e){for(a.framebufferTexture2D(a.FRAMEBUFFER,a.COLOR_ATTACHMENT0+r.location,a.TEXTURE_2D,r.res.tex,r.level);i.length<=r.location;)i.push(a.NONE);i[r.location]=a.COLOR_ATTACHMENT0+r.location}return a.drawBuffers(i),{fbo:t,complete:a.checkFramebufferStatus(a.FRAMEBUFFER)===a.FRAMEBUFFER_COMPLETE}}function O(e){const t=e.map(function(p){return p.res.id+"@"+p.level+":"+p.location}).join(","),i=w.get(t+(Z.split?"s":""));if(i)return i;let r;const h=function(p){const T=G([p]);if(!T.complete)throw new Error("elio-graph: framebuffer incomplete");return T};if(e.length===1)r=[h(e[0])];else{const p=Z.split?null:G(e);p&&p.complete?r=[p]:(Z.split=!0,r=e.map(h))}return w.set(t+(Z.split?"s":""),r),r}function R(){if(!Z.disposed){Z.disposed=!0;for(const e of c)a.deleteTexture(e);for(const e of D)a.deleteFramebuffer(e);for(const e of J)a.deleteBuffer(e);for(const e of Object.keys(m))a.deleteProgram(m[e].handle);for(const e of ae)a.deleteShader(e);a.deleteVertexArray(A),c.length=D.length=J.length=0,w.clear(),E.clear()}}let s=[];const u=X(a);try{s=d.steps.map(function(e){const t=e.pass;if(t.type==="mipmap"||t.type==="copy")return{pass:t};const i=W(t.kernel),r=i.layout,h=r.inputs.map(function(b){const I=t.bindings.find(function(S){return S.name===b.name});if(!I)throw new Error("elio-graph: pass "+t.name+" does not bind input "+b.name);return I}),p=r.outputs.map(function(b){const I=t.bindings.find(function(S){return S.name===b.name});if(!I)throw new Error("elio-graph: pass "+t.name+" does not bind output "+b.name);return{binding:I,location:b.location}}),T=(t.uniforms||[]).map(function(b){const I=r.uniforms.findIndex(function(pe){return pe.name===b.name});if(I<0)throw new Error("elio-graph: pass "+t.name+" has no uniform block "+b.name);const S=z(i.kernel,b.name),B=a.createBuffer(),V=Math.max(16,Math.ceil(S.size/16)*16);return J.push(B),a.bindBuffer(a.UNIFORM_BUFFER,B),a.bufferData(a.UNIFORM_BUFFER,V,a.DYNAMIC_DRAW),{index:I,layout:S,values:b.values,buffer:B,size:V,written:!1,constant:d.isStatic(b.values)}});return{pass:t,p:i,inputs:h,outputs:p,blocks:T}}),k()}catch(e){throw R(),e}finally{F(a,u)}function o(e){const t=v[e.from],i=v[e.to];if(t.w*t.h!==i.w*i.h||i.w!==e.width)throw new Error("elio-graph: pass "+e.name+" cannot copy "+t.name+" into "+i.name+" at width "+e.width);if(i.copiedFrom===t&&i.copiedVersion===t.version)return;let r=E.get(i.id);r||(r=a.createBuffer(),J.push(r),a.bindBuffer(a.PIXEL_PACK_BUFFER,r),a.bufferData(a.PIXEL_PACK_BUFFER,t.w*t.h*16,a.STREAM_COPY),a.bindBuffer(a.PIXEL_PACK_BUFFER,null),E.set(i.id,r)),a.bindFramebuffer(a.READ_FRAMEBUFFER,O([{res:t,level:0,location:0}])[0].fbo),a.bindBuffer(a.PIXEL_PACK_BUFFER,r),a.readPixels(0,0,t.w,t.h,a.RGBA,a.FLOAT,0),a.bindBuffer(a.PIXEL_PACK_BUFFER,null),a.bindBuffer(a.PIXEL_UNPACK_BUFFER,r),a.activeTexture(a.TEXTURE0+Q),a.bindTexture(a.TEXTURE_2D,i.tex),a.texSubImage2D(a.TEXTURE_2D,0,0,0,i.w,i.h,a.RGBA,a.FLOAT,0),a.bindBuffer(a.PIXEL_UNPACK_BUFFER,null),i.copiedFrom=t,i.copiedVersion=t.version,i.version++}function x(e){for(const t of e){const i=v[t[0]];v[t[0]]=v[t[1]],v[t[1]]=i}}function P(e){for(const t of N.resources){if(!t.feed)continue;const i=v[t.name],r=new Float32Array(i.w*i.h*4);r.set(e[t.feed].subarray(0,r.length)),a.activeTexture(a.TEXTURE0+Q),a.bindTexture(a.TEXTURE_2D,i.tex),a.texSubImage2D(a.TEXTURE_2D,0,0,0,i.w,i.h,a.RGBA,a.FLOAT,r)}}function M(e,t){const i=e.pass,r=e.p,h=i.target,p=e.outputs.map(function(b){return{res:v[b.binding.resource],level:b.binding.level||0,location:b.location}}),T=e.inputs.map(function(b){return v[b.resource]});e.inputs.forEach(function(b,I){for(const S of p)if(S.res===T[I]&&S.level===(b.level||0))throw new Error("elio-graph: pass "+i.name+" reads "+S.res.name+", which it writes")}),a.useProgram(r.handle),a.uniform1ui(r.width,h.elioWidth),T.forEach(function(b,I){a.activeTexture(a.TEXTURE0+Q+I),a.bindTexture(a.TEXTURE_2D,b.tex),r.lens[I]&&a.uniform1ui(r.lens[I],b.w*b.h),r.widths[I]&&a.uniform1ui(r.widths[I],b.w)});for(const b of e.blocks)(!b.constant||!b.written)&&(a.bindBuffer(a.UNIFORM_BUFFER,b.buffer),a.bufferSubData(a.UNIFORM_BUFFER,0,new Uint8Array(d.pack(b.layout,b.values,t,i.name))),b.written=!0),a.bindBufferRange(a.UNIFORM_BUFFER,b.index,b.buffer,0,b.size);for(const b of p)b.res.version++;a.viewport(0,0,h.width,h.height);for(const b of O(p))a.bindFramebuffer(a.FRAMEBUFFER,b.fbo),a.drawArrays(a.TRIANGLES,0,3)}const K=d.host.profile,ne=function(e,t){e.pass.type==="copy"?o(e.pass):e.pass.type==="mipmap"?(a.activeTexture(a.TEXTURE0+Q),a.bindTexture(a.TEXTURE_2D,v[e.pass.resource].tex),a.generateMipmap(a.TEXTURE_2D)):M(e,t)},Y=[];for(const e of d.host.throttle||[]){const t=new RegExp(e.match),i=s.filter(function(h){return t.test(h.pass.name)&&!h.group});if(!i.length||!(e.every>1))continue;const r={every:Math.floor(e.every),hold:e.hold?new RegExp(e.hold):null,since:1/0,dt:0,snap:null,runs:!1,derived:null,plans:i};for(const h of i)h.group=r;Y.push(r)}const oe=function(e){let t=null,i=!1;for(const r of s){const h=(r.outputs||[]).map(function(p){return p.binding.resource}).concat(r.pass.resource?[r.pass.resource]:[]);h.indexOf(e[0])<0&&h.indexOf(e[1])<0||(!r.group||t&&t!==r.group?i=!0:t=r.group)}return i?null:t},se=(N.frameSwaps||[]).map(function(e){return{pair:e,group:Y.length?oe(e):null}}),he=function(e){return e&&e!=="always"?e.replace(/^(param|changed):/,"").split(",").map(function(t){return t.trim()}):[]},ke=function(e,t,i){for(const r of Y){r.dt+=Number(e&&e.dt)||0,r.since++;const h=r.plans.some(function(p){return he(p.pass.when).some(function(T){return t[T]})});r.runs=h||r.since>=r.every,r.runs&&(r.derived=i.derive(r.dt,r.snap))}},f=function(e){for(const t of Y)t.runs&&(t.since=0,t.dt=0,t.snap=e.snapshot())};return{isReady:function(){return!Z.disposed},run:function(e,t,i,r){if(a.isContextLost&&a.isContextLost())return!1;const h=X(a);try{a.disable(a.BLEND),a.disable(a.DEPTH_TEST),a.disable(a.CULL_FACE),a.disable(a.SCISSOR_TEST),a.colorMask(!0,!0,!0,!0),a.bindVertexArray(A),P(e),Y.length&&ke(t,i,r);for(const T of s){if(T.group&&!T.group.runs||!d.holds(T.pass.when,e,i))continue;const b=T.group?T.group.derived:e;K?K(T.pass.name,function(){ne(T,b)}):ne(T,b)}for(const T of se)(!T.group||T.group.runs)&&x([T.pair]);Y.length&&f(r)}finally{F(a,h)}const p=d.host.textureCache;return p&&(p._sceneTextureEpoch=(p._sceneTextureEpoch||0)+1),!0},export:function(e){const t=v[N.exports[e]];return t?{tex:t.tex,width:t.w,height:t.h,levels:t.levels}:null},held:function(e){for(const t of Y)if(t.hold&&t.hold.test(e)&&t.snap&&e in t.snap)return t.snap[e]},split:function(){return Z.split},dispose:R}}window.__gosx_elio_graph.executors.webgl2=L})(),(function(){const U={f32:4,"vec2<f32>":8,"vec4<f32>":16};function X(F){const L=F.host.device,d=F.graph,a=F.module.kernels,N=F.host;if(!L)throw Object.assign(new Error("no device"),{fallback:"no-device"});const q="gosx-elio-"+F.label+"-",H=[],Q=[],n=Object.create(null),l=Object.create(null),c=Object.create(null),D=new Map,J=new Map,ae={disposed:!1};let m=0;const g={};for(const s of Object.keys(d.exports||{}))g[d.exports[s]]=!0;const A=L.queue,Z=GPUBufferUsage.STORAGE|GPUBufferUsage.COPY_DST|GPUBufferUsage.COPY_SRC,_=GPUBufferUsage.UNIFORM|GPUBufferUsage.COPY_DST;for(const s of d.resources)if(s.kind==="buffer"){const u=Math.max(16,Math.ceil(s.count*(U[s.elementType]||16)/4)*4),o=L.createBuffer({label:q+s.name,size:u,usage:Z});H.push(o),n[s.name]={name:s.name,id:++m,buffer:o}}else{const u=L.createTexture({label:q+s.name,size:[s.width,s.height,1],format:s.format,mipLevelCount:s.mipLevels||1,usage:GPUTextureUsage.STORAGE_BINDING|GPUTextureUsage.TEXTURE_BINDING});Q.push(u),n[s.name]={name:s.name,id:++m,texture:u,w:s.width,h:s.height,levels:s.mipLevels||1,format:s.format}}function v(s){return s.map(function(u){const o={binding:u.binding,visibility:GPUShaderStage.COMPUTE};return u.kind==="uniform"?o.buffer={type:"uniform"}:u.kind==="storage-read"?o.buffer={type:"read-only-storage"}:u.kind==="storage-rw"?o.buffer={type:"storage"}:u.kind==="sampled-texture"||u.kind==="texture"?o.texture={sampleType:"unfilterable-float",viewDimension:"2d"}:o.storageTexture={access:"write-only",format:u.format,viewDimension:"2d"},o})}for(const s of F.steps){const u=s.pass;if(!u)continue;if(u.type!=="kernel")throw new Error("elio-graph: pass "+u.name+": "+u.type+" passes exist only on webgl2");const o=u.kernel+"/"+u.entry;if(l[o])continue;const x=a[F.kernelKey(u.kernel)],P=c[u.kernel]||(c[u.kernel]=L.createBindGroupLayout({label:q+u.kernel,entries:v(x.bindings)})),M=L.createShaderModule({label:q+u.kernel,code:x.wgsl}),K=N.create(L,"compute",{label:q+u.kernel,layout:L.createPipelineLayout({bindGroupLayouts:[P]}),compute:{module:M,entryPoint:u.entry}});l[o]={handle:K,layout:P,kernel:x}}function w(s){const u=N.validation&&N.validation.get(s.handle);return!u||u.ready===!0?u&&u.pipeline||s.handle:null}let E=0;const y=F.steps.map(function(s){const u=s.pass,o=l[u.kernel+"/"+u.entry],x=(u.uniforms||[]).map(function(P){const M=o.kernel.uniforms.find(function(Y){return Y.name===P.name});if(!M)throw new Error("elio-graph: pass "+u.name+" has no uniform block "+P.name);const K=Math.max(16,Math.ceil(M.size/16)*16),ne=L.createBuffer({label:q+u.name+"-"+P.name,size:K,usage:_});return H.push(ne),{name:P.name,layout:M,values:P.values,buffer:ne,constant:F.isStatic(P.values),written:!1}});return{pass:u,p:o,blocks:x,id:++E}});function C(s){const u=s.pass,o=[],x=[u.name,s.id];for(const K of s.p.kernel.bindings){let ne;if(K.kind==="uniform")ne={buffer:s.blocks.find(function(Y){return Y.name===K.name}).buffer};else{const Y=u.bindings.find(function(se){return se.name===K.name});if(!Y)throw new Error("elio-graph: pass "+u.name+" does not bind "+K.name);const oe=n[Y.resource];x.push(oe.id+"@"+(Y.level||0)),K.kind==="storage-read"||K.kind==="storage-rw"?ne={buffer:oe.buffer}:ne=oe.texture.createView({baseMipLevel:Y.level||0,mipLevelCount:1})}o.push({binding:K.binding,resource:ne})}const P=x.join("|");let M=D.get(P);return M||(M=L.createBindGroup({label:q+u.name,layout:s.p.layout,entries:o}),D.set(P,M)),M}function re(s){for(const u of s){const o=n[u[0]];n[u[0]]=n[u[1]],n[u[1]]=o}}const ie=3,W=120;let z=null;if(N.timing!==!1&&L.features&&L.features.has&&L.features.has("timestamp-query"))try{const s=[],u=L.createQuerySet({label:q+"time",type:"timestamp",count:2}),o=L.createBuffer({label:q+"time-resolve",size:16,usage:GPUBufferUsage.QUERY_RESOLVE|GPUBufferUsage.COPY_SRC});H.push(o);for(let x=0;x<ie;x++){const P=L.createBuffer({label:q+"time-"+x,size:16,usage:GPUBufferUsage.MAP_READ|GPUBufferUsage.COPY_DST});H.push(P),s.push({buffer:P,state:"free"})}z={querySet:u,resolve:o,slots:s,samples:[],ms:null}}catch{z=null}function k(){if(z)for(const s of z.slots)s.state==="copied"&&(s.state="mapping",s.buffer.mapAsync(1).then(function(){if(ae.disposed||s.state!=="mapping")return;const u=new BigUint64Array(s.buffer.getMappedRange().slice(0,16));s.buffer.unmap(),s.state="free",u[1]>u[0]&&(z.ms=Number(u[1]-u[0])/1e6,z.samples.push(z.ms),z.samples.length>W&&z.samples.shift())},function(){s.state="free"}))}function G(s){const u=z.slots.find(function(o){return o.state==="free"});s.resolveQuerySet(z.querySet,0,2,z.resolve,0),u&&(s.copyBufferToBuffer(z.resolve,0,u.buffer,0,16),u.state="copied")}const O=L.lost&&L.lost.then?L.lost:null;O&&O.then(function(){R()},function(){R()});function R(){if(!ae.disposed){ae.disposed=!0;for(const s of H)s.destroy();for(const s of Q)s.destroy();H.length=Q.length=0,D.clear(),J.clear(),z&&z.querySet.destroy&&z.querySet.destroy()}}return{isReady:function(){if(ae.disposed)return!1;for(const s of Object.keys(l))if(!w(l[s]))return!1;return!0},run:function(s,u,o){const x=u.encoder;if(!x)throw new Error("elio-graph: a webgpu frame needs frame.encoder");for(const M of d.resources)M.feed&&A.writeBuffer(n[M.name].buffer,0,s[M.feed]);k();const P=y.filter(function(M){return F.holds(M.pass.when,s,o)});return P.forEach(function(M,K){const ne=M.pass;for(const se of M.blocks)se.constant&&se.written||(A.writeBuffer(se.buffer,0,new Uint8Array(F.pack(se.layout,se.values,s,ne.name))),se.written=!0);const Y={label:q+ne.name};z&&(K===0||K===P.length-1)&&(Y.timestampWrites={querySet:z.querySet},K===0&&(Y.timestampWrites.beginningOfPassWriteIndex=0),K===P.length-1&&(Y.timestampWrites.endOfPassWriteIndex=1));const oe=x.beginComputePass(Y);oe.setPipeline(w(M.p)),oe.setBindGroup(0,C(M)),oe.dispatchWorkgroups(ne.dispatch[0],ne.dispatch[1]||1,ne.dispatch[2]||1),oe.end()}),z&&P.length&&G(x),re(d.frameSwaps||[]),!0},export:function(s){const u=n[d.exports[s]];if(u&&u.buffer)return{buffer:u.buffer};if(!u)return null;let o=J.get(u.texture);return o||(o=u.texture.createView(),J.set(u.texture,o)),{texture:u.texture,view:o,width:u.w,height:u.h,levels:u.levels}},gpuTiming:function(){return z?{ms:z.ms,samples:z.samples.slice()}:null},dispose:R}}window.__gosx_elio_graph.executors.webgpu=X})(),(function(){const U={uniform:0,sampler:1,disp:2,slope:6,foam:10,wake:11},X={edgeCells:8,k:.2,slope:3,slickLo:.04,slickHi:.4,slickFine:.85,slickMid:.4,glow:.3,thinBase:.12,thinLace:.3,bodyBase:.32,bodyLace:.3,fadeNear:120,fadeFar:700};function F(n,l,c,D){const J=n.indexOf(l);if(J<0)throw new Error("ocean-fft: anchor not found: "+l.slice(0,48));const ae=n.indexOf(c,J);if(ae<0)throw new Error("ocean-fft: anchor not found: "+c.slice(0,48));return n.slice(0,J)+D+n.slice(ae)}function L(n,l,c){if(n.indexOf(l)<0)throw new Error("ocean-fft: anchor not found: "+l.slice(0,48));return n.replace(l,()=>c)}function d(n){let l="const WAKE_EDGE_CELLS: f32 = "+X.edgeCells.toFixed(1)+`;
`;l+=`struct OceanSpec { c: array<vec4f, 4>, f: vec4f, m: vec4f, w: array<vec4f, 4>, k: array<vec4f, 2> };
`,l+="@group("+n+") @binding("+U.uniform+`) var<uniform> sp: OceanSpec;
`,l+="@group("+n+") @binding("+U.sampler+`) var spSampler: sampler;
`;for(let c=0;c<4;c++)l+="@group("+n+") @binding("+(U.disp+c)+") var spDisp"+c+`: texture_2d<f32>;
`;for(let c=0;c<4;c++)l+="@group("+n+") @binding("+(U.slope+c)+") var spSlope"+c+`: texture_2d<f32>;
`;return l+="@group("+n+") @binding("+U.foam+`) var spFoam: texture_2d<f32>;
`,l+="@group("+n+") @binding("+U.wake+`) var spWakeTex: texture_2d<f32>;
`,l+=`fn spWake(rest: vec2f, cam: vec2f) -> vec3f {
`,l+=`  let r = (rest - cam - sp.k[0].xy) / max(sp.k[1].x, 1e-3);
`,l+=`  let edge = min(min(r.x, sp.k[1].z - r.x), min(r.y, sp.k[1].z - r.y));
`,l+=`  return vec3f((r + sp.k[0].zw) / sp.k[1].z, smoothstep(0.0, WAKE_EDGE_CELLS, edge) * sp.k[1].y);
`,l+=`}
`,l+=`fn spLod(c: vec4f, spacing: f32) -> f32 { return max(log2(max(spacing, 1e-3) * c.z / c.x), 0.0); }
`,l}function a(){let n=`  var p = vec3f(xz.x, level, xz.y); var dx = vec3f(1.0, 0.0, 0.0); var dz = vec3f(0.0, 0.0, 1.0);
`;n+=`  var amp = sp.m.x;
`,n+=`  let spacing = max(ocean.p[8].z * ocean.p[8].w * exp(ocean.p[8].w * f32(ring)), r * 6.2831853 / f32(segs));
`;for(let l=0;l<4;l++)n+="  if (sp.c["+l+`].w > 0.5) {
`,n+="    let uv = (xz - sp.w["+l+"].xy) / sp.c["+l+"].x; let lod = spLod(sp.c["+l+"], spacing); let k = smoothstep(0.0, 1.2, depth0 * sp.c["+l+`].y);
`,n+="    let a = textureSampleLevel(spDisp"+l+", spSampler, uv, lod); let b = textureSampleLevel(spSlope"+l+`, spSampler, uv, lod);
`,n+=`    p += a.xyz * k; dx += vec3f(b.z, b.x, a.w) * k; dz += vec3f(a.w, b.y, b.w) * k;
`,n+=`  }
`;return n+=`  let swv = spWake(xz, ocean.p[7].xz);
`,n+=`  if (sp.k[1].w > 0.0) { p = vec3f(xz.x, level, xz.y); dx = vec3f(1.0, 0.0, 0.0); dz = vec3f(0.0, 0.0, 1.0); }
`,n+=`  if (swv.z > 0.0 && sp.k[1].w <= 0.0) {
`,n+=`    let swk = textureSampleLevel(spWakeTex, spSampler, swv.xy, max(log2(max(spacing, 1e-3) / max(sp.k[1].x, 1e-3)), 0.0));
`,n+="    let sws = swv.z * smoothstep(0.0, 1.2, depth0 * "+X.k.toFixed(2)+`);
`,n+=`    p.y += swk.x * sws; dx.y += swk.y * sws; dz.y += swk.z * sws;
`,n+=`  }
`,n}function N(){let n=`  var sGx = 0.0; var sGz = 0.0; var sXX = 0.0; var sZZ = 0.0; var sXZ = 0.0;
`;n+=`  let wxz = in.rest - sp.m.yz;
`,n+=`  let sFw = fwidth(wxz);
`,n+=`  var sOp = 0.0; var sAlb = 1.0;
`,n+=`  let swv = spWake(in.rest, ocean.p[7].xz);
`,n+=`  let swf = textureSample(spWakeTex, spSampler, swv.xy);
`,n+="  let sSlick = swv.z * smoothstep("+X.slickLo.toFixed(2)+", "+X.slickHi.toFixed(2)+`, swf.w);
`;for(let l=0;l<4;l++){const c=l>=2?X.slickFine:l===1?X.slickMid:0;n+="  if (sp.c["+l+`].w > 0.5) {
`,n+="    let uv = (in.rest - sp.w["+l+"].xy) / sp.c["+l+"].x; let k = smoothstep(0.0, 1.2, in.depth0 * sp.c["+l+"].y)"+(c?" * (1.0 - "+c.toFixed(2)+" * sSlick)":"")+`;
`,n+="    let a = textureSample(spDisp"+l+", spSampler, uv); let b = textureSample(spSlope"+l+`, spSampler, uv);
`,n+=`    sGx += b.x * k; sGz += b.y * k; sXX += b.z * k; sZZ += b.w * k; sXZ += a.w * k;
`,n+=`  }
`}return n+="  let swm = swv.z * smoothstep(0.0, 1.2, in.depth0 * "+X.k.toFixed(2)+`);
`,n+="  sGx += swf.y * swm * "+X.slope.toFixed(2)+"; sGz += swf.z * swm * "+X.slope.toFixed(2)+`;
`,n+=`  let sJ = (1.0 + sXX) * (1.0 + sZZ) - sXZ * sXZ;
`,n+=`  let sFoamTex = textureSampleLevel(spFoam, spSampler, wxz * sp.f.x, 0.0);
`,n+=`  let sPersist = sFoamTex.r;
`,n+=`  let N = normalize(vec3f(sGz * sXZ - (1.0 + sZZ) * sGx, max(sJ, 0.1), sXZ * sGx - sGz * (1.0 + sXX)));
`,n}const q=`    let sNear = 1.0 - smoothstep(0.7 * sp.m.w, sp.m.w, length(in.world.xz - ocean.p[7].xz));
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
`;function H(){let n="";return n+=`fn spLine(p: vec2f, sc: f32, w: f32, pw: f32, mean: f32, fw: vec2f) -> f32 {
`,n+=`  var q = p * sc; q += 0.8 * (vec2f(on(q * 0.8 + vec2f(3.1)), on(q * 0.8 + vec2f(9.7))) - vec2f(0.5));
`,n+=`  let i = floor(q); let f = fract(q); var d1 = 8.0; var d2 = 8.0;
`,n+=`  for (var y = -1; y <= 1; y++) { for (var x = -1; x <= 1; x++) {
`,n+=`    let g = vec2f(f32(x), f32(y)); let c = i + g; let r = g + vec2f(oh(c), oh(c + vec2f(37.7, 11.3))) - f;
`,n+=`    let d = dot(r, r); if (d < d1) { d2 = d1; d1 = d; } else if (d < d2) { d2 = d; }
`,n+=`  } }
`,n+=`  let line = pow(1.0 - smoothstep(0.0, w, sqrt(d2) - sqrt(d1)), pw);
`,n+=`  return mix(mean, line, 1.0 - smoothstep(0.35, 0.9, length(fw) * sc));
`,n+=`}
`,n+=`fn spStrand(q: vec2f, o: vec2f, fw: vec2f) -> f32 {
`,n+=`  let a = spLine(q + o, 3.3, 0.65, 2.2, 0.22, fw); let b = spLine(q + o * 1.7, 1.1, 0.12, 1.0, 0.15, fw); let c = spLine(q + o * 0.6, 0.4, 0.09, 1.0, 0.11, fw);
`,n+=`  return 1.0 - (1.0 - 0.5 * a) * (1.0 - 0.6 * b) * (1.0 - 0.4 * c);
`,n+=`}
`,n+=`fn spLace(p: vec2f, t: f32, fw: vec2f) -> f32 {
`,n+=`  let q = p + 0.7 * (vec2f(on(p * 0.55 + vec2f(t * 0.13, 3.1)), on(p * 0.55 + vec2f(8.7, -t * 0.11))) - vec2f(0.5));
`,n+=`  let k = floor(t / 4.0);
`,n+=`  return mix(spStrand(q, vec2f(17.3, 5.1) * k, fw), spStrand(q, vec2f(17.3, 5.1) * (k + 1.0), fw), smoothstep(0.0, 1.0, fract(t / 4.0)));
`,n+=`}
`,n}function Q(n,l){let c=n;return c=L(c,"@location(4) depth0: f32 };","@location(4) depth0: f32, @location(5) rest: vec2f };"),c=F(c,"  var p = vec3f(xz.x, level, xz.y);","  let ph = fract(t * ocean.p[0].w / 9.0",a()),c=L(c,"out.depth0 = depth0;","out.depth0 = depth0; out.rest = xz;"),c=L(c,`  let N = od(normalize(in.normal), in.world.xz, t, ocean.p[9].xy);
`,N()),c=L(c,"  if (dist < 250.0) {","  if (dist < 6000.0) {"),c=L(c,`    foam = (1.0 - smoothstep(0.12, 0.35, in.jacobian)) * lace * bu;
`,q),c=L(c,"(1.0 - smoothstep(80.0, 250.0, dist))","(1.0 - smoothstep(1500.0, 6000.0, dist))"),c=L(c,"  return vec4f(color, alpha);",`  if (sp.k[1].w > 0.0) { let hh = clamp(swf.x / sp.k[1].w, -1.0, 1.0); color = select(vec3f(0.25), select(mix(vec3f(1.0), vec3f(1.0, 0.15, 0.1), hh), mix(vec3f(1.0), vec3f(0.1, 0.3, 1.0), -hh), hh < 0.0), swv.z > 0.0); alpha = 1.0; }
  return vec4f(color, alpha);`),c=L(c,"  color = mix(color, fl, foam); alpha = mix(alpha, 1.0, foam);",`  let sAer = swv.z * smoothstep(0.45, 1.0, swf.w);
  let sTurq = vec3f(0.05, 0.42, 0.44) * (ambient * 0.9 + sunCol * 0.05);
  color = mix(color, sTurq, sAer * `+X.glow.toFixed(2)+` * (1.0 - foam)); alpha = mix(alpha, 1.0, sAer * 0.5);
  color = mix(color, fl, foam); alpha = mix(alpha, 1.0, foam);
  let sSun = smoothstep(0.0, 0.55, dot(N, L));
  let sWfl = fl * mix(vec3f(0.66, 0.76, 0.9), vec3f(1.0), sSun) * sAlb + ocean.p[4].xyz * sunCol * 0.2 * max(dot(N, L), 0.0) * sAlb;
  color = mix(color, sWfl, sOp) + spec * sOp * 0.25; alpha = mix(alpha, 1.0, sOp);`),c=L(c,"(ambient * 0.55 + sunCol * 0.04 * max(dot(N, L), 0.0))","(ambient * 1.4 + sunCol * 0.2 * max(dot(N, L), 0.0) + vec3f(0.05))"),c+`
`+d(l)+`
`+H()}window.__gosx_scene3d_ocean_fft_surface={wgsl:Q,SLOT:U}})(),(function(){const U={disp:4,slope:8,foam:12,wake:13},X={edgeCells:8,k:.2,slope:3,slickLo:.04,slickHi:.4,slickFine:.85,slickMid:.4,glow:.3,thinBase:.12,thinLace:.3,bodyBase:.32,bodyLace:.3,fadeNear:120,fadeFar:700};function F(n,l,c){if(n.indexOf(l)<0)throw new Error("ocean-fft: anchor not found: "+l.slice(0,48));return n.replace(l,()=>c)}function L(n,l,c,D){const J=n.indexOf(l);if(J<0)throw new Error("ocean-fft: anchor not found: "+l.slice(0,48));const ae=n.indexOf(c,J);if(ae<0)throw new Error("ocean-fft: anchor not found: "+c.slice(0,48));return n.slice(0,J)+D+n.slice(ae)}function d(){let n=`uniform vec4 u_sp[12];
`;for(let l=0;l<4;l++)n+="uniform sampler2D u_spDisp"+l+`;
`;for(let l=0;l<4;l++)n+="uniform sampler2D u_spSlope"+l+`;
`;return n+=`uniform sampler2D u_spFoam;
`,n+=`uniform sampler2D u_spWake;
`,n+=`vec3 spWake(vec2 rest, vec2 cam) {
`,n+=`  vec2 r = (rest - cam - u_sp[10].xy) / max(u_sp[11].x, 1e-3);
`,n+=`  float edge = min(min(r.x, u_sp[11].z - r.x), min(r.y, u_sp[11].z - r.y));
`,n+="  return vec3((r + u_sp[10].zw) / u_sp[11].z, smoothstep(0., "+X.edgeCells.toFixed(1)+`, edge) * u_sp[11].y);
`,n+=`}
`,n+=`float spLod(vec4 c, float spacing) { return max(log2(max(spacing, 1e-3) * c.z / c.x), 0.); }
`,n}function a(){let n=`  vec3 p = vec3(xz.x, level, xz.y), dx = vec3(1., 0., 0.), dz = vec3(0., 0., 1.);
`;n+=`  float amp = u_sp[5].x;
`,n+=`  float spacing = max(u_ocean[8].z * u_ocean[8].w * exp(u_ocean[8].w * float(ring)), r * 6.2831853 / float(segs));
`;for(let l=0;l<4;l++)n+="  if (u_sp["+l+`].w > 0.5) {
`,n+="    vec2 suv = (xz - u_sp["+(6+l)+"].xy) / u_sp["+l+"].x; float slod = spLod(u_sp["+l+"], spacing); float sk = smoothstep(0., 1.2, depth0 * u_sp["+l+`].y);
`,n+="    vec4 sa = textureLod(u_spDisp"+l+", suv, slod), sb = textureLod(u_spSlope"+l+`, suv, slod);
`,n+=`    p += sa.xyz * sk; dx += vec3(sb.z, sb.x, sa.w) * sk; dz += vec3(sa.w, sb.y, sb.w) * sk;
`,n+=`  }
`;return n+=`  vec3 swv = spWake(xz, u_ocean[7].xz);
`,n+=`  if (u_sp[11].w > 0.) { p = vec3(xz.x, level, xz.y); dx = vec3(1., 0., 0.); dz = vec3(0., 0., 1.); }
`,n+=`  if (swv.z > 0. && u_sp[11].w <= 0.) {
`,n+=`    vec4 swk = textureLod(u_spWake, swv.xy, max(log2(max(spacing, 1e-3) / max(u_sp[11].x, 1e-3)), 0.));
`,n+="    float sws = swv.z * smoothstep(0., 1.2, depth0 * "+X.k.toFixed(2)+`);
`,n+=`    p.y += swk.x * sws; dx.y += swk.y * sws; dz.y += swk.z * sws;
`,n+=`  }
`,n}function N(){let n=`  float sGx = 0., sGz = 0., sXX = 0., sZZ = 0., sXZ = 0.;
`;n+=`  vec2 wxz = v_rest - u_sp[5].yz;
`,n+=`  vec2 sFw = fwidth(wxz);
`,n+=`  float sOp = 0., sAlb = 1.;
`,n+=`  vec3 swv = spWake(v_rest, u_ocean[7].xz);
`,n+=`  vec4 swf = texture(u_spWake, swv.xy);
`,n+="  float sSlick = swv.z * smoothstep("+X.slickLo.toFixed(2)+", "+X.slickHi.toFixed(2)+`, swf.w);
`;for(let l=0;l<4;l++){const c=l>=2?X.slickFine:l===1?X.slickMid:0;n+="  if (u_sp["+l+`].w > 0.5) {
`,n+="    vec2 suv = (v_rest - u_sp["+(6+l)+"].xy) / u_sp["+l+"].x; float sk = smoothstep(0., 1.2, v_d0 * u_sp["+l+"].y)"+(c?" * (1. - "+c.toFixed(2)+" * sSlick)":"")+`;
`,n+="    vec4 sa = texture(u_spDisp"+l+", suv), sb = texture(u_spSlope"+l+`, suv);
`,n+=`    sGx += sb.x * sk; sGz += sb.y * sk; sXX += sb.z * sk; sZZ += sb.w * sk; sXZ += sa.w * sk;
`,n+=`  }
`}return n+="  float swm = swv.z * smoothstep(0., 1.2, v_d0 * "+X.k.toFixed(2)+`);
`,n+="  sGx += swf.y * swm * "+X.slope.toFixed(2)+"; sGz += swf.z * swm * "+X.slope.toFixed(2)+`;
`,n+=`  float sJ = (1. + sXX) * (1. + sZZ) - sXZ * sXZ;
`,n+=`  vec4 sFoamTex = textureLod(u_spFoam, wxz * u_sp[4].x, 0.);
`,n+=`  float sPersist = sFoamTex.r;
`,n+=`  vec3 N = normalize(vec3(sGz * sXZ - (1. + sZZ) * sGx, max(sJ, 0.1), sXZ * sGx - sGz * (1. + sXX)));
`,n}const q=`    float sNear = 1. - smoothstep(0.7 * u_sp[5].w, u_sp[5].w, length(v_w.xz - u_ocean[7].xz));
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
`;function H(){let n="";return n+=`float spLine(vec2 p, float sc, float w, float pw, float mean, vec2 fw) {
`,n+=`  vec2 q = p * sc; q += 0.8 * (vec2(on(q * 0.8 + 3.1), on(q * 0.8 + 9.7)) - 0.5);
`,n+=`  vec2 i = floor(q), f = fract(q); float d1 = 8., d2 = 8.;
`,n+=`  for (int y = -1; y <= 1; y++) for (int x = -1; x <= 1; x++) {
`,n+=`    vec2 g = vec2(float(x), float(y)), c = i + g; vec2 r = g + vec2(oh(c), oh(c + vec2(37.7, 11.3))) - f;
`,n+=`    float d = dot(r, r); if (d < d1) { d2 = d1; d1 = d; } else if (d < d2) { d2 = d; }
`,n+=`  }
`,n+=`  float line = pow(1. - smoothstep(0., w, sqrt(d2) - sqrt(d1)), pw);
`,n+=`  return mix(mean, line, 1. - smoothstep(0.35, 0.9, length(fw) * sc));
`,n+=`}
`,n+=`float spStrand(vec2 q, vec2 o, vec2 fw) {
`,n+=`  float a = spLine(q + o, 3.3, 0.65, 2.2, 0.22, fw), b = spLine(q + o * 1.7, 1.1, 0.12, 1., 0.15, fw), c = spLine(q + o * 0.6, 0.4, 0.09, 1., 0.11, fw);
`,n+=`  return 1. - (1. - 0.5 * a) * (1. - 0.6 * b) * (1. - 0.4 * c);
`,n+=`}
`,n+=`float spLace(vec2 p, float t, vec2 fw) {
`,n+=`  vec2 q = p + 0.7 * (vec2(on(p * 0.55 + vec2(t * 0.13, 3.1)), on(p * 0.55 + vec2(8.7, -t * 0.11))) - 0.5);
`,n+=`  float k = floor(t / 4.);
`,n+=`  return mix(spStrand(q, vec2(17.3, 5.1) * k, fw), spStrand(q, vec2(17.3, 5.1) * (k + 1.), fw), smoothstep(0., 1., fract(t / 4.)));
`,n+=`}
`,n}function Q(n,l){let c=n;c=F(c,"out float v_d0;","out float v_d0; out vec2 v_rest;"),c=F(c,"void main() {",d()+"void main() {"),c=L(c,"  vec3 p = vec3(xz.x, level, xz.y), dx","  float ph = fract(t * u_ocean[0].w / 9.",a()),c=F(c,"v_d0 = depth0;","v_d0 = depth0; v_rest = xz;");let D=l;return D=F(D,"in float v_d0;","in float v_d0; in vec2 v_rest;"),D=F(D,"void main() {",d()+H()+"void main() {"),D=F(D,`  vec3 N = od(normalize(v_n), v_w.xz, t, u_ocean[9].xy);
`,N()),D=F(D,"  if (dist < 250.) {","  if (dist < 6000.) {"),D=F(D,`    foam = (1. - smoothstep(0.12, 0.35, v_j)) * lace * bu;
`,q),D=F(D,"(1. - smoothstep(80., 250., dist))","(1. - smoothstep(1500., 6000., dist))"),D=F(D,"  fragColor = vec4(color, alpha);",`  if (u_sp[11].w > 0.) { float hh = clamp(swf.x / u_sp[11].w, -1., 1.); color = swv.z > 0. ? (hh < 0. ? mix(vec3(1.), vec3(.1, .3, 1.), -hh) : mix(vec3(1.), vec3(1., .15, .1), hh)) : vec3(.25); alpha = 1.; }
  fragColor = vec4(color, alpha);`),D=F(D,"  color = mix(color, fl, foam); alpha = mix(alpha, 1., foam);",`  float sAer = swv.z * smoothstep(0.45, 1.0, swf.w);
  vec3 sTurq = vec3(0.05, 0.42, 0.44) * (ambient * 0.9 + sunCol * 0.05);
  color = mix(color, sTurq, sAer * `+X.glow.toFixed(2)+` * (1. - foam)); alpha = mix(alpha, 1., sAer * 0.5);
  color = mix(color, fl, foam); alpha = mix(alpha, 1., foam);
  float sSun = smoothstep(0., 0.55, dot(N, L));
  vec3 sWfl = fl * mix(vec3(0.66, 0.76, 0.9), vec3(1.), sSun) * sAlb + u_ocean[4].xyz * sunCol * 0.2 * max(dot(N, L), 0.) * sAlb;
  color = mix(color, sWfl, sOp) + spec * sOp * 0.25; alpha = mix(alpha, 1., sOp);`),D=F(D,"(ambient * 0.55 + sunCol * 0.04 * max(dot(N, L), 0.))","(ambient * 1.4 + sunCol * 0.2 * max(dot(N, L), 0.) + vec3(0.05))"),{vertex:c,fragment:D}}window.__gosx_scene3d_ocean_fft_surface_gl={glsl:Q,UNIT:U}})(),(function(){const U=window.__gosx_elio_graph,X=window.__gosx_scene3d_elio_ocean,F=window.__gosx_scene3d_ocean_fft_surface,L=window.__gosx_scene3d_ocean_fft_surface_gl,d=.25,a=3.5,N=240,q=.3,H=function(f){return Math.max(.3,.14*f*f/9.81)},Q=48,n=4;let l=null,c=null;function D(f){return f==="low"||f==="medium"?f:"high"}function J(f){const e=[];let t={cell:.5,width:1024};for(const i of U.expand(f)){const r=i.uniforms&&i.uniforms[0]&&i.uniforms[0].values;i.kernel==="ocean_spectrum"&&r?e.push({tile:r.tile,kMin:r.kMin,kMax:r.kMax,n:r.n}):i.kernel==="ocean_foam"&&r&&(t={cell:r.cell,width:r.width})}return{cascades:e.slice(0,n),foam:t}}function ae(f,e){const t=e.graphParams;if(t)for(const i of Object.keys(t))Number.isFinite(t[i])&&f.hasParam(i)&&f.setValue(i,t[i]);Number.isFinite(e.graphTween)&&f.setTween(e.graphTween),f.setNodes(e.graphNodes||{}),g(f,e)}const m=[["hullX","x"],["hullZ","z"],["hullHeading","heading"],["hullSpeed","speed"]];function g(f,e){const t=e.graphNodes&&e.graphNodes.hull;if(!t)return;const i=f.nodePose?f.nodePose(t):null;if(i)for(const r of m){const h=f.graph.params.find(function(p){return p.name===r[0]});h&&h.source.kind==="value"&&f.setValue(r[0],i[r[1]])}}function A(f,e,t,i,r){t.fill(0);const h=f.exportParams(),p=e.foam;e.cascades.forEach(function(S,B){t[B*4]=S.tile,t[B*4+1]=Math.sqrt(Math.max(S.kMin,1e-6)*S.kMax),t[B*4+2]=S.n,t[B*4+3]=1,t[24+B*4]=h["waterModX"+B]||0,t[24+B*4+1]=h["waterModZ"+B]||0});const T=p.cell*p.width,b=function(S){return S-Math.floor(S/T)*T},I=f.export("wake");if(I){const S=r?f.value("cameraX")-(r.x||0):0,B=r?f.value("cameraZ")-(r.z||0):0;t[40]=(h.wakeCamRelX||0)+S,t[41]=(h.wakeCamRelZ||0)+B,t[42]=h.wakeOriginModX||0,t[43]=h.wakeOriginModZ||0,t[44]=h.wakeCell||0,t[45]=h.wakeOn||0,t[46]=I.width,t[47]=i&&i.wake&&i.wake.debug?q:0}t[16]=1/T,t[17]=1,t[18]=f.value("foamThreshold"),t[19]=a,t[20]=H(f.value("windSpeed")),t[21]=b(f.value("waterX")),t[22]=b(f.value("waterZ")),t[23]=N}function Z(f,e){const t=Number.isFinite(e.time)?e.time:0,i=f.last===null?0:Math.max(0,t-f.last),r=Math.min(d,i);f.last=t;const h=e.camera||{};return{time:t,dt:r,accumulateDt:i,cameraX:h.x||0,cameraY:h.y||0,cameraZ:h.z||0}}function _(f){const e=window.__gosx_scene3d_node_poses;return e&&e[f]?e[f]:null}function v(f,e){const t=f&&e();if(!t||!(Math.abs(t.value("driftX"))+Math.abs(t.value("driftZ"))>0))return f;const i=f.speed*Math.sin(f.heading)-t.value("driftX"),r=f.speed*Math.cos(f.heading)-t.value("driftZ");return{x:f.x,z:f.z,heading:f.heading,speed:Math.hypot(i,r)}}function w(){return c}function E(){return l&&!l.failed?l.gpuTiming():null}const y={x:0,z:0,time:0,scale:1,driftX:0,driftZ:0,gen:0};let C=0;function re(f){if(f.failed){c=null;return}y.x=f.value("waterX"),y.z=f.value("waterZ"),y.time=f.value("simTime"),y.gen=f.waterGen,y.scale=f.value("timeScale"),y.driftX=f.value("driftX"),y.driftZ=f.value("driftZ"),c=y}function ie(f){const e=c;return e?(f[1]=e.time,f[2]=e.x,f[3]=e.z,f[4]=1,f[5]=e.scale,f[6]=e.driftX,f[7]=e.driftZ,f[9]=e.gen,!0):!1}const W=3,z=250,k=64,G=1,O=8,R=16,s={collect:[],k:0,latest:null,previous:null};function u(){s.collect=[],s.k=0}function o(f,e,t,i){const r=s.k++;s.collect.length<k&&s.collect.push({x:f,z:e});const h=Number.isFinite(i)?i:typeof performance<"u"?performance.now():0,p=s.latest;if(!p||r>=p.count||h-p.at>z||Math.abs(p.points[r].x-f)>8||Math.abs(p.points[r].z-e)>8)return null;const T=s.previous&&s.previous.count>r?s.previous:null,b=T&&p.at>T.at?(p.heights[r]-T.heights[r])/((p.at-T.at)/1e3):0,I=p.heights[r]+b*((h-p.at)/1e3),S=p.nx[r],B=p.nz[r],V=Math.sqrt(Math.max(0,1-S*S-B*B));return{x:f,y:t+I,z:e,normal:{x:S,y:V,z:B},velocity:{x:0,y:b,z:0}}}function x(f,e){const t=[];for(let r=0;r<W;r++)t.push({buffer:f.createBuffer({label:"gosx-ocean-fft-probe-"+r,size:k*R,usage:G|O}),state:"free",meta:null});const i=function(r){const h=r.buffer,p=r.meta;r.state="mapping",h.mapAsync(G).then(function(){if(r.state!=="mapping")return;const T=new Float32Array(h.getMappedRange().slice(0,p.count*R));h.unmap();const b=[],I=[],S=[];for(let B=0;B<p.count;B++)b.push(T[B*4]),I.push(T[B*4+1]),S.push(T[B*4+2]);s.previous=s.latest,s.latest={at:p.at,count:p.count,points:p.points,heights:b,nx:I,nz:S},r.state="free"},function(){r.state="free"})};return{poll:function(){for(const r of t)r.state==="copied"&&i(r)},copy:function(r,h,p){const T=t.find(function(b){return b.state==="free"});T&&(r.copyBufferToBuffer(h,0,T.buffer,0,k*R),T.state="copied",T.meta={at:e(),count:p.length,points:p})},dispose:function(){for(const r of t)r.state="disposed",r.buffer.destroy()}}}function P(f,e){if(!f.hasParam("probePoints"))return!1;const t=Math.min(e.length,k);if(!t)return f.setValue("probeCount",0),!1;let i=0,r=0;for(let p=0;p<t;p++)i+=e[p].x,r+=e[p].z;i/=t,r/=t;const h=new Float32Array(k*4);for(let p=0;p<t;p++)h[p*4]=e[p].x-i,h[p*4+1]=e[p].z-r;return f.setValue("probeOriginX",i),f.setValue("probeOriginZ",r),f.setValue("probeCount",t),f.setValue("probePoints",h),!0}function M(f,e,t){const i=function(r,h){const p=c;if(!p)return f.sample(e,r,h,t);const T=f.sample(e,r-p.x,h-p.z,p.time);return T.x+=p.x,T.z+=p.z,T};return{sample:i,probed:function(r,h){return o(r,h,e.data[0])||i(r,h)},begin:u}}function K(f){const e=f.device,t="gosx-ocean-fft-",i=function(){const j=GPUShaderStage.VERTEX|GPUShaderStage.FRAGMENT,ee=[];ee.push({binding:F.SLOT.uniform,visibility:j,buffer:{type:"uniform"}},{binding:F.SLOT.sampler,visibility:j,sampler:{type:"filtering"}});for(let $=0;$<n;$++)ee.push({binding:F.SLOT.disp+$,visibility:j,texture:{sampleType:"float"}},{binding:F.SLOT.slope+$,visibility:j,texture:{sampleType:"float"}});return ee.push({binding:F.SLOT.foam,visibility:j,texture:{sampleType:"float"}},{binding:F.SLOT.wake,visibility:j,texture:{sampleType:"float"}}),ee},r=e.createBindGroupLayout({label:t+"surface",entries:i()}),h=e.createSampler({label:t+"sampler",magFilter:"linear",minFilter:"linear",mipmapFilter:"linear",addressModeU:"repeat",addressModeV:"repeat"}),p=e.createBuffer({label:t+"surface",size:Q*4,usage:GPUBufferUsage.UNIFORM|GPUBufferUsage.COPY_DST}),T=e.createTexture({label:t+"dummy",size:[1,1,1],format:"rgba16float",usage:GPUTextureUsage.TEXTURE_BINDING|GPUTextureUsage.COPY_DST}),b=T.createView(),I=new Float32Array(Q),S={last:null,tier:"",runner:null,info:null,viewList:null},B=x(e,function(){return f.now?f.now():typeof performance<"u"?performance.now():0}),V={device:e,ready:!1,layout:r,group:null,disposed:!1,tier:"",update:ue,dispose:we,fail:fe};function fe(j){const ee="ocean-fft: "+(j&&j.message?j.message:String(j));f.guard&&f.guard.fail("gosx-ocean-fft-surface",ee)}function pe(j){S.runner&&S.runner.dispose();const ee=U.create({module:X,backend:"webgpu",tier:j,host:{device:e,create:f.create,validation:f.validation,nodePose:function($){return v(_($),function(){return S.runner})}}});ee.waterGen=++C,S.runner=ee,S.tier=j,S.last=null,S.viewList=null,V.group=null,V.ready=!1,S.info=ee.graph?J(ee.graph):null}function te(){const j=S.runner,ee=[],$=[{binding:F.SLOT.uniform,resource:{buffer:p}},{binding:F.SLOT.sampler,resource:h}];for(let me=0;me<n;me++){const le=j.export("disp"+me),ge=j.export("slope"+me);$.push({binding:F.SLOT.disp+me,resource:le?le.view:b},{binding:F.SLOT.slope+me,resource:ge?ge.view:b}),ee.push(le?le.view:b,ge?ge.view:b)}const de=j.export("foam");$.push({binding:F.SLOT.foam,resource:de?de.view:b}),ee.push(de?de.view:b);const ce=j.export("wake");$.push({binding:F.SLOT.wake,resource:ce?ce.view:b}),ee.push(ce?ce.view:b),!(S.viewList&&ee.every(function(me,le){return me===S.viewList[le]}))&&(S.viewList=ee,V.group=e.createBindGroup({label:t+"surface",layout:r,entries:$}))}function ue(j){const ee=D(j.tier);(!S.runner||ee!==S.tier)&&pe(ee);const $=S.runner;if($.failed)return V.ready=!1,V.group=null,!1;ae($,j.ocean),B.poll();const de=s.collect.slice(0,k),ce=P($,de),me=$.run(Object.assign(Z(S,j),{encoder:j.encoder}));if($.failed)return V.ready=!1,V.group=null,c=null,f.guard&&f.guard.fail("gosx-elio-ocean","ocean graph failed: "+$.failed),!1;if(!me)return V.ready;const le=ce?$.export("probe"):null;return le&&le.buffer&&B.copy(j.encoder,le.buffer,de),te(),A($,S.info,I,j.ocean,j.camera),e.queue.writeBuffer(p,0,I),re($),l=$,V.ready=!0,!0}function we(){V.disposed||(V.disposed=!0,V.ready=!1,V.group=null,S.runner&&S.runner.dispose(),c=null,l=null,s.latest=s.previous=null,B.dispose(),p.destroy(),T.destroy())}return Object.defineProperty(V,"runner",{get:function(){return S.runner}}),V}function ne(f,e){const t=e.environment&&e.environment.ocean;let i=f.fft;const r=e.guard&&(e.guard.disabled.has("oceanFFT")||e.guard.disabled.has("elioGraph"));return!t||!t.spectrum||r||i&&i.device!==e.device?(i&&(i.dispose(),f.fft=null),null):(i||(i=f.fft=K(e)),i.update({ocean:t,tier:e.tier,time:e.time,camera:e.camera,encoder:e.encoder}),i)}const Y=[{match:"^wake\\.",every:2,hold:"^(wake|camera[XZ])"}];function oe(){return typeof window<"u"&&window.__gosx_elio_diagnostics||{}}function se(f,e){const t={last:null},i=U.create({module:X,backend:"webgl2",tier:D(e.tier),host:{gl:f,caps:e.caps,textureCache:e.textureCache,profile:e.profile||oe().profile,throttle:e.throttle||oe().throttle||Y,nodePose:function(B){return v(_(B),function(){return T.runner})}}}),r=new Float32Array(Q),h=new Map;i.waterGen=++C;const p=i.graph?J(i.graph):null,T={gl:f,runner:i,tier:D(e.tier),disposed:!1,update:b,bind:I,dispose:S,fail:function(B){i.fail(B)}};function b(B){return i.failed||(ae(i,B.ocean),!i.run(Z(t,B)))?!1:(A(i,p,r,B.ocean,B.camera),re(i),!0)}function I(B,V){let fe=h.get(V);if(!fe){const te=function(ue){return B.getUniformLocation(V,ue)};fe={sp:te("u_sp[0]"),disp:[0,1,2,3].map(function(ue){return te("u_spDisp"+ue)}),slope:[0,1,2,3].map(function(ue){return te("u_spSlope"+ue)}),foam:te("u_spFoam"),wake:te("u_spWake")},h.set(V,fe)}const pe=function(te,ue,we){B.activeTexture(B.TEXTURE0+te),B.bindTexture(B.TEXTURE_2D,we?we.tex:T.dummy),B.uniform1i(ue,te)};T.dummy||(T.dummy=B.createTexture(),B.bindTexture(B.TEXTURE_2D,T.dummy),B.texStorage2D(B.TEXTURE_2D,1,B.RGBA16F,1,1),B.texParameteri(B.TEXTURE_2D,B.TEXTURE_MIN_FILTER,B.LINEAR),B.texParameteri(B.TEXTURE_2D,B.TEXTURE_MAG_FILTER,B.LINEAR));for(let te=0;te<n;te++)pe(L.UNIT.disp+te,fe.disp[te],i.export("disp"+te)),pe(L.UNIT.slope+te,fe.slope[te],i.export("slope"+te));pe(L.UNIT.foam,fe.foam,i.export("foam")),pe(L.UNIT.wake,fe.wake,i.export("wake")),B.uniform4fv(fe.sp,r)}function S(){T.disposed||(T.disposed=!0,c=null,i.dispose(),T.dummy&&f.deleteTexture(T.dummy))}return T}function he(f,e,t){let i=f.fft;if(i&&(i.gl!==e||i.disposed||i.tier!==D(t.tier))&&(i.dispose(),i=f.fft=null),f.fftDisabled)return null;if(e.isContextLost&&e.isContextLost())return c=null,null;i||(i=f.fft=se(e,t));const r=i.update({ocean:t.ocean,time:t.time,camera:t.camera});return i.runner.failed?(f.fftDisabled=i.runner.failed,i.dispose(),f.fft=null,c=null,t.mount&&t.mount.setAttribute&&t.mount.setAttribute("data-gosx-scene3d-ocean-fft-fallback",f.fftDisabled),null):r?i:null}function ke(f,e,t){f.fft&&f.fft.dispose(),f.fft=null,f.fftDisabled=t,c=null,e&&e.mount&&e.mount.setAttribute&&e.mount.setAttribute("data-gosx-scene3d-ocean-fft-fallback",t)}window.__gosx_scene3d_ocean_fft_api={frame:ne,water:w,fill:ie,timing:E,nodePose:_,waterRelative:v,sampler:M,probe:{begin:u,sample:o},probeState:s,describe:J,WAKE_DEBUG_RANGE:q,feed:ae,surfaceData:A},window.__gosx_scene3d_ocean_fft_gl_api={run:he,fail:ke}})();
