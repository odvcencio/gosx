(function(){"use strict";function ne(F){if(F&&typeof F=="object"){for(const D of Object.keys(F))ne(F[D]);Object.freeze(F)}return F}window.__gosx_scene3d_elio_ocean=ne({elio:"v0.5.0",set:"ocean",sha256:"3164f8e7d5544d0d8812b023cf2baa4774c1f688e98e9935fa8b702a90bd3125",kernels:{oceanSpectrum:{wgsl:`struct OceanSpectrumParams {
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
`,gatherLayout:{kernel:"fft_stage",inputs:[{name:"src",kind:"buffer",elem:"vec4",sampler:"src_tex",length:"src_len",width:"src_w",binding:1}],outputs:[{name:"dst",elem:"vec4",format:"",location:0,variable:"dst_out"}],uniforms:[{name:"fft",block:"FFTStage_ubo",struct:"FFTStage",binding:0}]}}},graphs:{"webgpu/high":{name:"ocean",backend:"webgpu",tier:"high",version:1,params:[{name:"windSpeed",type:"f32",source:{kind:"value",value:9},tween:20},{name:"fetch",type:"f32",source:{kind:"value",value:300},tween:20},{name:"gamma",type:"f32",source:{kind:"value",value:3.3},tween:20},{name:"spread",type:"f32",source:{kind:"value",value:8},tween:20},{name:"swell",type:"f32",source:{kind:"value",value:0},tween:20},{name:"windDirDeg",type:"f32",source:{kind:"value",value:0},tween:20},{name:"swellDirDeg",type:"f32",source:{kind:"value",value:0},tween:20},{name:"seed",type:"u32",source:{kind:"value",value:0}},{name:"loopPeriod",type:"f32",source:{kind:"value",value:1e3}},{name:"lambda",type:"f32",source:{kind:"value",value:1},tween:20},{name:"timeScale",type:"f32",source:{kind:"value",value:1}},{name:"driftX",type:"f32",source:{kind:"value",value:0}},{name:"driftZ",type:"f32",source:{kind:"value",value:0}},{name:"foamTau",type:"f32",source:{kind:"value",value:4}},{name:"foamThreshold",type:"f32",source:{kind:"value",value:.9}},{name:"foamGain",type:"f32",source:{kind:"value",value:2}},{name:"probeCount",type:"u32",source:{kind:"value",value:0}},{name:"probeOriginX",type:"f32",source:{kind:"value",value:0}},{name:"probeOriginZ",type:"f32",source:{kind:"value",value:0}},{name:"probePoints",type:"vec4<f32>[]",count:64,source:{kind:"value",value:0}},{name:"dt",type:"f32",source:{kind:"frame",name:"dt"}},{name:"cameraX",type:"f32",source:{kind:"frame",name:"cameraX"}},{name:"cameraZ",type:"f32",source:{kind:"frame",name:"cameraZ"}},{name:"windDir",type:"f32",source:{args:[1.5707963267948966,{op:"mul",args:["param:windDirDeg",.017453292519943295]}],kind:"expr",op:"sub"}},{name:"swellDir",type:"f32",source:{args:[1.5707963267948966,{op:"mul",args:["param:swellDirDeg",.017453292519943295]}],kind:"expr",op:"sub"}},{name:"simTime",type:"f32",source:{kind:"accumulate",of:"param:timeScale"}},{name:"phaseTime",type:"f32",source:{args:["param:simTime","param:loopPeriod"],kind:"expr",op:"mod"}},{name:"waterX",type:"f32",source:{kind:"accumulate",of:"param:driftX"}},{name:"waterZ",type:"f32",source:{kind:"accumulate",of:"param:driftZ"}},{name:"waterModX0",type:"f32",source:{args:["param:waterX",1600],kind:"expr",op:"mod"}},{name:"waterModZ0",type:"f32",source:{args:["param:waterZ",1600],kind:"expr",op:"mod"}},{name:"waterModX1",type:"f32",source:{args:["param:waterX",250],kind:"expr",op:"mod"}},{name:"waterModZ1",type:"f32",source:{args:["param:waterZ",250],kind:"expr",op:"mod"}},{name:"waterModX2",type:"f32",source:{args:["param:waterX",37],kind:"expr",op:"mod"}},{name:"waterModZ2",type:"f32",source:{args:["param:waterZ",37],kind:"expr",op:"mod"}},{name:"waterModX3",type:"f32",source:{args:["param:waterX",5.3],kind:"expr",op:"mod"}},{name:"waterModZ3",type:"f32",source:{args:["param:waterZ",5.3],kind:"expr",op:"mod"}},{name:"foamOriginX",type:"i32",source:{args:[{op:"floor",args:[{op:"div",args:[{op:"sub",args:["param:cameraX","param:waterX"]},.5]}]},512],kind:"expr",op:"sub"}},{name:"foamShiftX",type:"i32",source:{kind:"delta",of:"param:foamOriginX"}},{name:"foamOriginMod0X",type:"f32",source:{args:[{op:"mul",args:["param:foamOriginX",.5]},1600],kind:"expr",op:"mod"}},{name:"foamOriginMod1X",type:"f32",source:{args:[{op:"mul",args:["param:foamOriginX",.5]},250],kind:"expr",op:"mod"}},{name:"foamOriginMod2X",type:"f32",source:{args:[{op:"mul",args:["param:foamOriginX",.5]},37],kind:"expr",op:"mod"}},{name:"foamOriginMod3X",type:"f32",source:{args:[{op:"mul",args:["param:foamOriginX",.5]},5.3],kind:"expr",op:"mod"}},{name:"probeOriginMod0X",type:"f32",source:{args:[{op:"sub",args:["param:probeOriginX","param:waterX"]},1600],kind:"expr",op:"mod"}},{name:"probeOriginMod1X",type:"f32",source:{args:[{op:"sub",args:["param:probeOriginX","param:waterX"]},250],kind:"expr",op:"mod"}},{name:"probeOriginMod2X",type:"f32",source:{args:[{op:"sub",args:["param:probeOriginX","param:waterX"]},37],kind:"expr",op:"mod"}},{name:"probeOriginMod3X",type:"f32",source:{args:[{op:"sub",args:["param:probeOriginX","param:waterX"]},5.3],kind:"expr",op:"mod"}},{name:"foamOriginZ",type:"i32",source:{args:[{op:"floor",args:[{op:"div",args:[{op:"sub",args:["param:cameraZ","param:waterZ"]},.5]}]},512],kind:"expr",op:"sub"}},{name:"foamShiftZ",type:"i32",source:{kind:"delta",of:"param:foamOriginZ"}},{name:"foamOriginMod0Z",type:"f32",source:{args:[{op:"mul",args:["param:foamOriginZ",.5]},1600],kind:"expr",op:"mod"}},{name:"foamOriginMod1Z",type:"f32",source:{args:[{op:"mul",args:["param:foamOriginZ",.5]},250],kind:"expr",op:"mod"}},{name:"foamOriginMod2Z",type:"f32",source:{args:[{op:"mul",args:["param:foamOriginZ",.5]},37],kind:"expr",op:"mod"}},{name:"foamOriginMod3Z",type:"f32",source:{args:[{op:"mul",args:["param:foamOriginZ",.5]},5.3],kind:"expr",op:"mod"}},{name:"probeOriginMod0Z",type:"f32",source:{args:[{op:"sub",args:["param:probeOriginZ","param:waterZ"]},1600],kind:"expr",op:"mod"}},{name:"probeOriginMod1Z",type:"f32",source:{args:[{op:"sub",args:["param:probeOriginZ","param:waterZ"]},250],kind:"expr",op:"mod"}},{name:"probeOriginMod2Z",type:"f32",source:{args:[{op:"sub",args:["param:probeOriginZ","param:waterZ"]},37],kind:"expr",op:"mod"}},{name:"probeOriginMod3Z",type:"f32",source:{args:[{op:"sub",args:["param:probeOriginZ","param:waterZ"]},5.3],kind:"expr",op:"mod"}},{name:"hullX",type:"f32",source:{kind:"value",value:0}},{name:"hullZ",type:"f32",source:{kind:"value",value:0}},{name:"hullHeading",type:"f32",source:{kind:"value",value:0}},{name:"hullSpeed",type:"f32",source:{kind:"value",value:0}},{name:"wakeEnabled",type:"bool",source:{kind:"value",value:0}},{name:"hullLength",type:"f32",source:{kind:"value",value:200}},{name:"hullBeam",type:"f32",source:{kind:"value",value:30}},{name:"wakeCell",type:"f32",source:{kind:"value",value:4}},{name:"wakeDepth",type:"f32",source:{kind:"value",value:.4}},{name:"wakeGravity",type:"f32",source:{kind:"value",value:9.81}},{name:"wakeAlpha",type:"f32",source:{kind:"value",value:.02}},{name:"wakeMinLambdaCells",type:"f32",source:{kind:"value",value:8}},{name:"wakeFoamGain",type:"f32",source:{kind:"value",value:1}},{name:"wakeFoamTau",type:"f32",source:{kind:"value",value:48}},{name:"wakeDiffuse",type:"f32",source:{kind:"value",value:1.5}},{name:"wakeJitter",type:"f32",source:{kind:"value",value:1}},{name:"wakeBowGain",type:"f32",source:{kind:"value",value:.2}},{name:"wakeBowFoam",type:"f32",source:{kind:"value",value:.9}},{name:"wakeOn",type:"f32",source:{args:["param:wakeEnabled",1],kind:"expr",op:"mul"}},{name:"wakeFoamGainEff",type:"f32",source:{args:["param:wakeEnabled","param:wakeFoamGain"],kind:"expr",op:"mul"}},{name:"wakeEdge",type:"i32",source:{kind:"delta",of:"param:wakeEnabled"}},{name:"wakeReset",type:"u32",source:{args:["param:wakeEdge",0],kind:"expr",op:"max"}},{name:"wakeLength",type:"f32",source:{args:["param:wakeCell",256],kind:"expr",op:"mul"}},{name:"hullWaterX",type:"f32",source:{args:["param:hullX","param:waterX"],kind:"expr",op:"sub"}},{name:"wakeOriginX",type:"i32",source:{args:[{op:"div",args:[{op:"sub",args:[{op:"sub",args:["param:hullWaterX",{op:"mul",args:[{op:"sin",args:["param:hullHeading"]},{op:"mul",args:["param:wakeCell",51.2]}]}]},{op:"mul",args:["param:wakeCell",128]}]},"param:wakeCell"]}],kind:"expr",op:"floor"}},{name:"wakeShiftX",type:"i32",source:{kind:"delta",of:"param:wakeOriginX"}},{name:"wakeHullU",type:"f32",source:{args:["param:hullWaterX",{op:"mul",args:["param:wakeOriginX","param:wakeCell"]}],kind:"expr",op:"sub"}},{name:"wakeOriginModX",type:"i32",source:{args:["param:wakeOriginX",256],kind:"expr",op:"mod"}},{name:"wakeRelX",type:"f32",source:{args:[{op:"mul",args:["param:foamOriginX",.5]},{op:"mul",args:["param:wakeOriginX","param:wakeCell"]}],kind:"expr",op:"sub"}},{name:"wakeCamRelX",type:"f32",source:{args:[{op:"add",args:["param:waterX",{op:"mul",args:["param:wakeOriginX","param:wakeCell"]}]},"param:cameraX"],kind:"expr",op:"sub"}},{name:"hullWaterZ",type:"f32",source:{args:["param:hullZ","param:waterZ"],kind:"expr",op:"sub"}},{name:"wakeOriginZ",type:"i32",source:{args:[{op:"div",args:[{op:"sub",args:[{op:"sub",args:["param:hullWaterZ",{op:"mul",args:[{op:"cos",args:["param:hullHeading"]},{op:"mul",args:["param:wakeCell",51.2]}]}]},{op:"mul",args:["param:wakeCell",128]}]},"param:wakeCell"]}],kind:"expr",op:"floor"}},{name:"wakeShiftZ",type:"i32",source:{kind:"delta",of:"param:wakeOriginZ"}},{name:"wakeHullV",type:"f32",source:{args:["param:hullWaterZ",{op:"mul",args:["param:wakeOriginZ","param:wakeCell"]}],kind:"expr",op:"sub"}},{name:"wakeOriginModZ",type:"i32",source:{args:["param:wakeOriginZ",256],kind:"expr",op:"mod"}},{name:"wakeRelZ",type:"f32",source:{args:[{op:"mul",args:["param:foamOriginZ",.5]},{op:"mul",args:["param:wakeOriginZ","param:wakeCell"]}],kind:"expr",op:"sub"}},{name:"wakeCamRelZ",type:"f32",source:{args:[{op:"add",args:["param:waterZ",{op:"mul",args:["param:wakeOriginZ","param:wakeCell"]}]},"param:cameraZ"],kind:"expr",op:"sub"}},{name:"wakeGEff",type:"f32",source:{args:[{op:"min",args:[{op:"min",args:["param:wakeGravity",{op:"mul",args:[{op:"mul",args:["param:hullSpeed","param:hullSpeed"]},{op:"div",args:[6.283185307179586,{op:"mul",args:["param:wakeMinLambdaCells","param:wakeCell"]}]}]}]},{op:"mul",args:["param:wakeCell",16.39001041697031]}]},.05],kind:"expr",op:"max"}},{name:"wakeDt",type:"f32",source:{args:[{op:"mul",args:["param:dt","param:timeScale"]},.2],kind:"expr",op:"min"}},{name:"wakeSrcLength",type:"f32",source:{args:[{op:"min",args:["param:hullLength",{op:"div",args:[{op:"mul",args:[{op:"mul",args:["param:hullSpeed","param:hullSpeed"]},3.141592653589793]},"param:wakeGEff"]}]},{op:"mul",args:["param:wakeCell",2]}],kind:"expr",op:"max"}},{name:"wakeSrcBeam",type:"f32",source:{args:[{op:"min",args:["param:hullBeam",{op:"mul",args:["param:wakeSrcLength",.67]}]},{op:"mul",args:["param:wakeCell",1.5]}],kind:"expr",op:"max"}}],resources:[{name:"h0_0",kind:"buffer",elementType:"vec4<f32>",count:4096},{name:"omega_0",kind:"buffer",elementType:"f32",count:4096},{name:"fftA_0",kind:"buffer",elementType:"vec4<f32>",count:4096},{name:"fftB_0",kind:"buffer",elementType:"vec4<f32>",count:4096},{name:"dispTex0",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"slopeTex0",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"h0_1",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"omega_1",kind:"buffer",elementType:"f32",count:65536},{name:"fftA_1",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"fftB_1",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"dispTex1",kind:"texture",format:"rgba16float",width:256,height:256,mipLevels:9},{name:"slopeTex1",kind:"texture",format:"rgba16float",width:256,height:256,mipLevels:9},{name:"h0_2",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"omega_2",kind:"buffer",elementType:"f32",count:65536},{name:"fftA_2",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"fftB_2",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"dispTex2",kind:"texture",format:"rgba16float",width:256,height:256,mipLevels:9},{name:"slopeTex2",kind:"texture",format:"rgba16float",width:256,height:256,mipLevels:9},{name:"h0_3",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"omega_3",kind:"buffer",elementType:"f32",count:65536},{name:"fftA_3",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"fftB_3",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"dispTex3",kind:"texture",format:"rgba16float",width:256,height:256,mipLevels:9},{name:"slopeTex3",kind:"texture",format:"rgba16float",width:256,height:256,mipLevels:9},{name:"wakeA",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"wakeB",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"wakeF",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"wakeG",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"wakeC",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"wakeTex",kind:"texture",format:"rgba16float",width:256,height:256,mipLevels:9},{name:"foamA",kind:"buffer",elementType:"f32",count:1048576},{name:"foamB",kind:"buffer",elementType:"f32",count:1048576},{name:"foamTex",kind:"texture",format:"rgba16float",width:1024,height:1024,mipLevels:11},{name:"probePoints",kind:"buffer",elementType:"vec4<f32>",count:64,feed:"probePoints"},{name:"probeOut",kind:"buffer",elementType:"vec4<f32>",count:64}],passes:[{name:"c0.spectrum",kernel:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",dispatch:[64,1,1],bindings:[{name:"h0",resource:"h0_0"},{name:"omega",resource:"omega_0"}],uniforms:[{name:"spec",values:{cascade:0,fetchKm:"param:fetch",gamma:"param:gamma",kMax:.12,kMin:.003926990816987242,loopPeriod:"param:loopPeriod",n:64,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:1600,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c0.evolve",kernel:"ocean_evolve",dispatch:[64,1,1],bindings:[{name:"h0",resource:"h0_0"},{name:"omega",resource:"omega_0"},{name:"outA",resource:"fftA_0"},{name:"outB",resource:"fftB_0"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:64,t:"param:phaseTime",tile:1600}}]},{name:"c0.fftA.fft_rows",kernel:"fft_compute_64",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftA_0"}]},{name:"c0.fftA.fft_cols",kernel:"fft_compute_64",entry:"fft_cols",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftA_0"}]},{name:"c0.fftB.fft_rows",kernel:"fft_compute_64",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftB_0"}]},{name:"c0.fftB.fft_cols",kernel:"fft_compute_64",entry:"fft_cols",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftB_0"}]},{name:"c0.assemble",kernel:"ocean_assemble",dispatch:[64,1,1],bindings:[{name:"fieldA",resource:"fftA_0"},{name:"fieldB",resource:"fftB_0"},{name:"disp",resource:"dispTex0"},{name:"slope",resource:"slopeTex0"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:64}}]},{name:"c0.dispTex0.mips",type:"repeat",count:6,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[16,1,1],bindings:[{name:"src",resource:"dispTex0",levelAt:{index:"l"}},{name:"dst",resource:"dispTex0",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:64},dstWidth:{index:"l",offset:1,shr:64},pad:0,srcLevel:0}}]}]},{name:"c0.slopeTex0.mips",type:"repeat",count:6,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[16,1,1],bindings:[{name:"src",resource:"slopeTex0",levelAt:{index:"l"}},{name:"dst",resource:"slopeTex0",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:64},dstWidth:{index:"l",offset:1,shr:64},pad:0,srcLevel:0}}]}]},{name:"c1.spectrum",kernel:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",dispatch:[1024,1,1],bindings:[{name:"h0",resource:"h0_1"},{name:"omega",resource:"omega_1"}],uniforms:[{name:"spec",values:{cascade:1,fetchKm:"param:fetch",gamma:"param:gamma",kMax:.8,kMin:.12,loopPeriod:"param:loopPeriod",n:256,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:250,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c1.evolve",kernel:"ocean_evolve",dispatch:[1024,1,1],bindings:[{name:"h0",resource:"h0_1"},{name:"omega",resource:"omega_1"},{name:"outA",resource:"fftA_1"},{name:"outB",resource:"fftB_1"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:256,t:"param:phaseTime",tile:250}}]},{name:"c1.fftA.fft_rows",kernel:"fft_compute_256",dispatch:[256,1,1],bindings:[{name:"data",resource:"fftA_1"}]},{name:"c1.fftA.fft_cols",kernel:"fft_compute_256",entry:"fft_cols",dispatch:[256,1,1],bindings:[{name:"data",resource:"fftA_1"}]},{name:"c1.fftB.fft_rows",kernel:"fft_compute_256",dispatch:[256,1,1],bindings:[{name:"data",resource:"fftB_1"}]},{name:"c1.fftB.fft_cols",kernel:"fft_compute_256",entry:"fft_cols",dispatch:[256,1,1],bindings:[{name:"data",resource:"fftB_1"}]},{name:"c1.assemble",kernel:"ocean_assemble",dispatch:[1024,1,1],bindings:[{name:"fieldA",resource:"fftA_1"},{name:"fieldB",resource:"fftB_1"},{name:"disp",resource:"dispTex1"},{name:"slope",resource:"slopeTex1"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:256}}]},{name:"c1.dispTex1.mips",type:"repeat",count:8,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[256,1,1],bindings:[{name:"src",resource:"dispTex1",levelAt:{index:"l"}},{name:"dst",resource:"dispTex1",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:256},dstWidth:{index:"l",offset:1,shr:256},pad:0,srcLevel:0}}]}]},{name:"c1.slopeTex1.mips",type:"repeat",count:8,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[256,1,1],bindings:[{name:"src",resource:"slopeTex1",levelAt:{index:"l"}},{name:"dst",resource:"slopeTex1",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:256},dstWidth:{index:"l",offset:1,shr:256},pad:0,srcLevel:0}}]}]},{name:"c2.spectrum",kernel:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",dispatch:[1024,1,1],bindings:[{name:"h0",resource:"h0_2"},{name:"omega",resource:"omega_2"}],uniforms:[{name:"spec",values:{cascade:2,fetchKm:"param:fetch",gamma:"param:gamma",kMax:5.4,kMin:.8,loopPeriod:"param:loopPeriod",n:256,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:37,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c2.evolve",kernel:"ocean_evolve",dispatch:[1024,1,1],bindings:[{name:"h0",resource:"h0_2"},{name:"omega",resource:"omega_2"},{name:"outA",resource:"fftA_2"},{name:"outB",resource:"fftB_2"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:256,t:"param:phaseTime",tile:37}}]},{name:"c2.fftA.fft_rows",kernel:"fft_compute_256",dispatch:[256,1,1],bindings:[{name:"data",resource:"fftA_2"}]},{name:"c2.fftA.fft_cols",kernel:"fft_compute_256",entry:"fft_cols",dispatch:[256,1,1],bindings:[{name:"data",resource:"fftA_2"}]},{name:"c2.fftB.fft_rows",kernel:"fft_compute_256",dispatch:[256,1,1],bindings:[{name:"data",resource:"fftB_2"}]},{name:"c2.fftB.fft_cols",kernel:"fft_compute_256",entry:"fft_cols",dispatch:[256,1,1],bindings:[{name:"data",resource:"fftB_2"}]},{name:"c2.assemble",kernel:"ocean_assemble",dispatch:[1024,1,1],bindings:[{name:"fieldA",resource:"fftA_2"},{name:"fieldB",resource:"fftB_2"},{name:"disp",resource:"dispTex2"},{name:"slope",resource:"slopeTex2"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:256}}]},{name:"c2.dispTex2.mips",type:"repeat",count:8,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[256,1,1],bindings:[{name:"src",resource:"dispTex2",levelAt:{index:"l"}},{name:"dst",resource:"dispTex2",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:256},dstWidth:{index:"l",offset:1,shr:256},pad:0,srcLevel:0}}]}]},{name:"c2.slopeTex2.mips",type:"repeat",count:8,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[256,1,1],bindings:[{name:"src",resource:"slopeTex2",levelAt:{index:"l"}},{name:"dst",resource:"slopeTex2",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:256},dstWidth:{index:"l",offset:1,shr:256},pad:0,srcLevel:0}}]}]},{name:"c3.spectrum",kernel:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",dispatch:[1024,1,1],bindings:[{name:"h0",resource:"h0_3"},{name:"omega",resource:"omega_3"}],uniforms:[{name:"spec",values:{cascade:3,fetchKm:"param:fetch",gamma:"param:gamma",kMax:37,kMin:5.4,loopPeriod:"param:loopPeriod",n:256,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:5.3,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c3.evolve",kernel:"ocean_evolve",dispatch:[1024,1,1],bindings:[{name:"h0",resource:"h0_3"},{name:"omega",resource:"omega_3"},{name:"outA",resource:"fftA_3"},{name:"outB",resource:"fftB_3"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:256,t:"param:phaseTime",tile:5.3}}]},{name:"c3.fftA.fft_rows",kernel:"fft_compute_256",dispatch:[256,1,1],bindings:[{name:"data",resource:"fftA_3"}]},{name:"c3.fftA.fft_cols",kernel:"fft_compute_256",entry:"fft_cols",dispatch:[256,1,1],bindings:[{name:"data",resource:"fftA_3"}]},{name:"c3.fftB.fft_rows",kernel:"fft_compute_256",dispatch:[256,1,1],bindings:[{name:"data",resource:"fftB_3"}]},{name:"c3.fftB.fft_cols",kernel:"fft_compute_256",entry:"fft_cols",dispatch:[256,1,1],bindings:[{name:"data",resource:"fftB_3"}]},{name:"c3.assemble",kernel:"ocean_assemble",dispatch:[1024,1,1],bindings:[{name:"fieldA",resource:"fftA_3"},{name:"fieldB",resource:"fftB_3"},{name:"disp",resource:"dispTex3"},{name:"slope",resource:"slopeTex3"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:256}}]},{name:"c3.dispTex3.mips",type:"repeat",count:8,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[256,1,1],bindings:[{name:"src",resource:"dispTex3",levelAt:{index:"l"}},{name:"dst",resource:"dispTex3",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:256},dstWidth:{index:"l",offset:1,shr:256},pad:0,srcLevel:0}}]}]},{name:"c3.slopeTex3.mips",type:"repeat",count:8,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[256,1,1],bindings:[{name:"src",resource:"slopeTex3",levelAt:{index:"l"}},{name:"dst",resource:"slopeTex3",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:256},dstWidth:{index:"l",offset:1,shr:256},pad:0,srcLevel:0}}]}]},{name:"wake.source",kernel:"wake_source",when:"param:wakeEnabled",dispatch:[1024,1,1],bindings:[{name:"wakeIn",resource:"wakeA"},{name:"fftOut",resource:"wakeF"},{name:"cleanOut",resource:"wakeC"}],uniforms:[{name:"wake",values:{bowFoam:"param:wakeBowFoam",bowGain:"param:wakeBowGain",cell:"param:wakeCell",depth:"param:wakeDepth",diffuse:"param:wakeDiffuse",dt:"param:wakeDt",foamSpeed:5,fullSpeed:3,heading:"param:hullHeading",hullBeam:"param:hullBeam",hullLen:"param:hullLength",hullU:"param:wakeHullU",hullV:"param:wakeHullV",jitter:"param:wakeJitter",originX:"param:wakeOriginX",originZ:"param:wakeOriginZ",reset:"param:wakeReset",shiftX:"param:wakeShiftX",shiftZ:"param:wakeShiftZ",speed:"param:hullSpeed",srcBeam:"param:wakeSrcBeam",srcLength:"param:wakeSrcLength",tau:"param:wakeFoamTau",width:256}}]},{name:"wake.fft1.fft_rows",kernel:"fft_compute_256",when:"param:wakeEnabled",dispatch:[256,1,1],bindings:[{name:"data",resource:"wakeF"}]},{name:"wake.fft1.fft_cols",kernel:"fft_compute_256",entry:"fft_cols",when:"param:wakeEnabled",dispatch:[256,1,1],bindings:[{name:"data",resource:"wakeF"}]},{name:"wake.symbol",kernel:"wake_symbol",when:"param:wakeEnabled",dispatch:[1024,1,1],bindings:[{name:"fftIn",resource:"wakeF"},{name:"fftOut",resource:"wakeG"}],uniforms:[{name:"sym",values:{n:256,window:"param:wakeLength"}}]},{name:"wake.fft2.fft_rows",kernel:"fft_compute_256",when:"param:wakeEnabled",dispatch:[256,1,1],bindings:[{name:"data",resource:"wakeG"}]},{name:"wake.fft2.fft_cols",kernel:"fft_compute_256",entry:"fft_cols",when:"param:wakeEnabled",dispatch:[256,1,1],bindings:[{name:"data",resource:"wakeG"}]},{name:"wake.step",kernel:"wake_step",when:"param:wakeEnabled",dispatch:[1024,1,1],bindings:[{name:"qIn",resource:"wakeG"},{name:"cleanIn",resource:"wakeC"},{name:"stateOut",resource:"wakeB"},{name:"wakeTex",resource:"wakeTex"}],uniforms:[{name:"wstep",values:{alpha:"param:wakeAlpha",cell:"param:wakeCell",dt:"param:wakeDt",gravity:"param:wakeGEff",originX:"param:wakeOriginX",originZ:"param:wakeOriginZ",sponge:24,spongeRate:.8,width:256}}]},{name:"wake.wakeTex.mips",type:"repeat",when:"param:wakeEnabled",count:8,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[256,1,1],bindings:[{name:"src",resource:"wakeTex",levelAt:{index:"l"}},{name:"dst",resource:"wakeTex",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:256},dstWidth:{index:"l",offset:1,shr:256},pad:0,srcLevel:0}}]}]},{name:"foam",kernel:"ocean_foam",dispatch:[16384,1,1],bindings:[{name:"foamIn",resource:"foamA"},{name:"fieldB0",resource:"fftB_0"},{name:"fieldB1",resource:"fftB_1"},{name:"fieldB2",resource:"fftB_2"},{name:"fieldB3",resource:"fftB_3"},{name:"foamOut",resource:"foamB"},{name:"foamTex",resource:"foamTex"},{name:"wakeIn",resource:"wakeB"}],uniforms:[{name:"foam",values:{cascades:4,cell:.5,dt:"param:dt",gain:"param:foamGain",lambda:"param:lambda",n0:64,n1:256,n2:256,n3:256,omX0:"param:foamOriginMod0X",omX1:"param:foamOriginMod1X",omX2:"param:foamOriginMod2X",omX3:"param:foamOriginMod3X",omZ0:"param:foamOriginMod0Z",omZ1:"param:foamOriginMod1Z",omZ2:"param:foamOriginMod2Z",omZ3:"param:foamOriginMod3Z",originX:"param:foamOriginX",originZ:"param:foamOriginZ",shiftX:"param:foamShiftX",shiftZ:"param:foamShiftZ",size0:1600,size1:250,size2:37,size3:5.3,tau:"param:foamTau",threshold:"param:foamThreshold",wakeCell:"param:wakeCell",wakeGain:"param:wakeFoamGainEff",wakeOriginX:"param:wakeOriginModX",wakeOriginZ:"param:wakeOriginModZ",wakeRelX:"param:wakeRelX",wakeRelZ:"param:wakeRelZ",wakeWidth:256,width:1024}}]},{name:"foamTex.mips",type:"repeat",count:10,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[4096,1,1],bindings:[{name:"src",resource:"foamTex",levelAt:{index:"l"}},{name:"dst",resource:"foamTex",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:1024},dstWidth:{index:"l",offset:1,shr:1024},pad:0,srcLevel:0}}]}]},{name:"probe",kernel:"ocean_probe",dispatch:[1,1,1],bindings:[{name:"points",resource:"probePoints"},{name:"disp0",resource:"fftA_0"},{name:"disp1",resource:"fftA_1"},{name:"disp2",resource:"fftA_2"},{name:"disp3",resource:"fftA_3"},{name:"out",resource:"probeOut"}],uniforms:[{name:"probe",values:{cascades:4,count:"param:probeCount",lambda:"param:lambda",n0:64,n1:256,n2:256,n3:256,normalStep:.5,omX0:"param:probeOriginMod0X",omX1:"param:probeOriginMod1X",omX2:"param:probeOriginMod2X",omX3:"param:probeOriginMod3X",omZ0:"param:probeOriginMod0Z",omZ1:"param:probeOriginMod1Z",omZ2:"param:probeOriginMod2Z",omZ3:"param:probeOriginMod3Z",size0:1600,size1:250,size2:37,size3:5.3,t:"param:simTime"}}]}],frameSwaps:[["wakeA","wakeB"],["foamA","foamB"]],exports:{disp0:"dispTex0",disp1:"dispTex1",disp2:"dispTex2",disp3:"dispTex3",foam:"foamTex",probe:"probeOut",slope0:"slopeTex0",slope1:"slopeTex1",slope2:"slopeTex2",slope3:"slopeTex3",wake:"wakeTex"},exportParams:["waterModX0","waterModZ0","waterModX1","waterModZ1","waterModX2","waterModZ2","waterModX3","waterModZ3","wakeCamRelX","wakeCamRelZ","wakeOriginModX","wakeOriginModZ","wakeCell","wakeOn"]},"webgpu/medium":{name:"ocean",backend:"webgpu",tier:"medium",version:1,resources:[{name:"h0_0",kind:"buffer",elementType:"vec4<f32>",count:4096},{name:"omega_0",kind:"buffer",elementType:"f32",count:4096},{name:"fftA_0",kind:"buffer",elementType:"vec4<f32>",count:4096},{name:"fftB_0",kind:"buffer",elementType:"vec4<f32>",count:4096},{name:"dispTex0",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"slopeTex0",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"h0_1",kind:"buffer",elementType:"vec4<f32>",count:16384},{name:"omega_1",kind:"buffer",elementType:"f32",count:16384},{name:"fftA_1",kind:"buffer",elementType:"vec4<f32>",count:16384},{name:"fftB_1",kind:"buffer",elementType:"vec4<f32>",count:16384},{name:"dispTex1",kind:"texture",format:"rgba16float",width:128,height:128,mipLevels:8},{name:"slopeTex1",kind:"texture",format:"rgba16float",width:128,height:128,mipLevels:8},{name:"h0_2",kind:"buffer",elementType:"vec4<f32>",count:16384},{name:"omega_2",kind:"buffer",elementType:"f32",count:16384},{name:"fftA_2",kind:"buffer",elementType:"vec4<f32>",count:16384},{name:"fftB_2",kind:"buffer",elementType:"vec4<f32>",count:16384},{name:"dispTex2",kind:"texture",format:"rgba16float",width:128,height:128,mipLevels:8},{name:"slopeTex2",kind:"texture",format:"rgba16float",width:128,height:128,mipLevels:8},{name:"h0_3",kind:"buffer",elementType:"vec4<f32>",count:16384},{name:"omega_3",kind:"buffer",elementType:"f32",count:16384},{name:"fftA_3",kind:"buffer",elementType:"vec4<f32>",count:16384},{name:"fftB_3",kind:"buffer",elementType:"vec4<f32>",count:16384},{name:"dispTex3",kind:"texture",format:"rgba16float",width:128,height:128,mipLevels:8},{name:"slopeTex3",kind:"texture",format:"rgba16float",width:128,height:128,mipLevels:8},{name:"wakeA",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"wakeB",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"wakeF",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"wakeG",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"wakeC",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"wakeTex",kind:"texture",format:"rgba16float",width:256,height:256,mipLevels:9},{name:"foamA",kind:"buffer",elementType:"f32",count:1048576},{name:"foamB",kind:"buffer",elementType:"f32",count:1048576},{name:"foamTex",kind:"texture",format:"rgba16float",width:1024,height:1024,mipLevels:11},{name:"probePoints",kind:"buffer",elementType:"vec4<f32>",count:64,feed:"probePoints"},{name:"probeOut",kind:"buffer",elementType:"vec4<f32>",count:64}],passes:[{name:"c0.spectrum",kernel:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",dispatch:[64,1,1],bindings:[{name:"h0",resource:"h0_0"},{name:"omega",resource:"omega_0"}],uniforms:[{name:"spec",values:{cascade:0,fetchKm:"param:fetch",gamma:"param:gamma",kMax:.12,kMin:.003926990816987242,loopPeriod:"param:loopPeriod",n:64,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:1600,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c0.evolve",kernel:"ocean_evolve",dispatch:[64,1,1],bindings:[{name:"h0",resource:"h0_0"},{name:"omega",resource:"omega_0"},{name:"outA",resource:"fftA_0"},{name:"outB",resource:"fftB_0"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:64,t:"param:phaseTime",tile:1600}}]},{name:"c0.fftA.fft_rows",kernel:"fft_compute_64",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftA_0"}]},{name:"c0.fftA.fft_cols",kernel:"fft_compute_64",entry:"fft_cols",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftA_0"}]},{name:"c0.fftB.fft_rows",kernel:"fft_compute_64",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftB_0"}]},{name:"c0.fftB.fft_cols",kernel:"fft_compute_64",entry:"fft_cols",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftB_0"}]},{name:"c0.assemble",kernel:"ocean_assemble",dispatch:[64,1,1],bindings:[{name:"fieldA",resource:"fftA_0"},{name:"fieldB",resource:"fftB_0"},{name:"disp",resource:"dispTex0"},{name:"slope",resource:"slopeTex0"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:64}}]},{name:"c0.dispTex0.mips",type:"repeat",count:6,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[16,1,1],bindings:[{name:"src",resource:"dispTex0",levelAt:{index:"l"}},{name:"dst",resource:"dispTex0",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:64},dstWidth:{index:"l",offset:1,shr:64},pad:0,srcLevel:0}}]}]},{name:"c0.slopeTex0.mips",type:"repeat",count:6,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[16,1,1],bindings:[{name:"src",resource:"slopeTex0",levelAt:{index:"l"}},{name:"dst",resource:"slopeTex0",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:64},dstWidth:{index:"l",offset:1,shr:64},pad:0,srcLevel:0}}]}]},{name:"c1.spectrum",kernel:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",dispatch:[256,1,1],bindings:[{name:"h0",resource:"h0_1"},{name:"omega",resource:"omega_1"}],uniforms:[{name:"spec",values:{cascade:1,fetchKm:"param:fetch",gamma:"param:gamma",kMax:.8,kMin:.12,loopPeriod:"param:loopPeriod",n:128,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:250,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c1.evolve",kernel:"ocean_evolve",dispatch:[256,1,1],bindings:[{name:"h0",resource:"h0_1"},{name:"omega",resource:"omega_1"},{name:"outA",resource:"fftA_1"},{name:"outB",resource:"fftB_1"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:128,t:"param:phaseTime",tile:250}}]},{name:"c1.fftA.fft_rows",kernel:"fft_compute_128",dispatch:[128,1,1],bindings:[{name:"data",resource:"fftA_1"}]},{name:"c1.fftA.fft_cols",kernel:"fft_compute_128",entry:"fft_cols",dispatch:[128,1,1],bindings:[{name:"data",resource:"fftA_1"}]},{name:"c1.fftB.fft_rows",kernel:"fft_compute_128",dispatch:[128,1,1],bindings:[{name:"data",resource:"fftB_1"}]},{name:"c1.fftB.fft_cols",kernel:"fft_compute_128",entry:"fft_cols",dispatch:[128,1,1],bindings:[{name:"data",resource:"fftB_1"}]},{name:"c1.assemble",kernel:"ocean_assemble",dispatch:[256,1,1],bindings:[{name:"fieldA",resource:"fftA_1"},{name:"fieldB",resource:"fftB_1"},{name:"disp",resource:"dispTex1"},{name:"slope",resource:"slopeTex1"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:128}}]},{name:"c1.dispTex1.mips",type:"repeat",count:7,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[64,1,1],bindings:[{name:"src",resource:"dispTex1",levelAt:{index:"l"}},{name:"dst",resource:"dispTex1",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:128},dstWidth:{index:"l",offset:1,shr:128},pad:0,srcLevel:0}}]}]},{name:"c1.slopeTex1.mips",type:"repeat",count:7,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[64,1,1],bindings:[{name:"src",resource:"slopeTex1",levelAt:{index:"l"}},{name:"dst",resource:"slopeTex1",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:128},dstWidth:{index:"l",offset:1,shr:128},pad:0,srcLevel:0}}]}]},{name:"c2.spectrum",kernel:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",dispatch:[256,1,1],bindings:[{name:"h0",resource:"h0_2"},{name:"omega",resource:"omega_2"}],uniforms:[{name:"spec",values:{cascade:2,fetchKm:"param:fetch",gamma:"param:gamma",kMax:5.4,kMin:.8,loopPeriod:"param:loopPeriod",n:128,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:37,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c2.evolve",kernel:"ocean_evolve",dispatch:[256,1,1],bindings:[{name:"h0",resource:"h0_2"},{name:"omega",resource:"omega_2"},{name:"outA",resource:"fftA_2"},{name:"outB",resource:"fftB_2"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:128,t:"param:phaseTime",tile:37}}]},{name:"c2.fftA.fft_rows",kernel:"fft_compute_128",dispatch:[128,1,1],bindings:[{name:"data",resource:"fftA_2"}]},{name:"c2.fftA.fft_cols",kernel:"fft_compute_128",entry:"fft_cols",dispatch:[128,1,1],bindings:[{name:"data",resource:"fftA_2"}]},{name:"c2.fftB.fft_rows",kernel:"fft_compute_128",dispatch:[128,1,1],bindings:[{name:"data",resource:"fftB_2"}]},{name:"c2.fftB.fft_cols",kernel:"fft_compute_128",entry:"fft_cols",dispatch:[128,1,1],bindings:[{name:"data",resource:"fftB_2"}]},{name:"c2.assemble",kernel:"ocean_assemble",dispatch:[256,1,1],bindings:[{name:"fieldA",resource:"fftA_2"},{name:"fieldB",resource:"fftB_2"},{name:"disp",resource:"dispTex2"},{name:"slope",resource:"slopeTex2"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:128}}]},{name:"c2.dispTex2.mips",type:"repeat",count:7,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[64,1,1],bindings:[{name:"src",resource:"dispTex2",levelAt:{index:"l"}},{name:"dst",resource:"dispTex2",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:128},dstWidth:{index:"l",offset:1,shr:128},pad:0,srcLevel:0}}]}]},{name:"c2.slopeTex2.mips",type:"repeat",count:7,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[64,1,1],bindings:[{name:"src",resource:"slopeTex2",levelAt:{index:"l"}},{name:"dst",resource:"slopeTex2",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:128},dstWidth:{index:"l",offset:1,shr:128},pad:0,srcLevel:0}}]}]},{name:"c3.spectrum",kernel:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",dispatch:[256,1,1],bindings:[{name:"h0",resource:"h0_3"},{name:"omega",resource:"omega_3"}],uniforms:[{name:"spec",values:{cascade:3,fetchKm:"param:fetch",gamma:"param:gamma",kMax:37,kMin:5.4,loopPeriod:"param:loopPeriod",n:128,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:5.3,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c3.evolve",kernel:"ocean_evolve",dispatch:[256,1,1],bindings:[{name:"h0",resource:"h0_3"},{name:"omega",resource:"omega_3"},{name:"outA",resource:"fftA_3"},{name:"outB",resource:"fftB_3"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:128,t:"param:phaseTime",tile:5.3}}]},{name:"c3.fftA.fft_rows",kernel:"fft_compute_128",dispatch:[128,1,1],bindings:[{name:"data",resource:"fftA_3"}]},{name:"c3.fftA.fft_cols",kernel:"fft_compute_128",entry:"fft_cols",dispatch:[128,1,1],bindings:[{name:"data",resource:"fftA_3"}]},{name:"c3.fftB.fft_rows",kernel:"fft_compute_128",dispatch:[128,1,1],bindings:[{name:"data",resource:"fftB_3"}]},{name:"c3.fftB.fft_cols",kernel:"fft_compute_128",entry:"fft_cols",dispatch:[128,1,1],bindings:[{name:"data",resource:"fftB_3"}]},{name:"c3.assemble",kernel:"ocean_assemble",dispatch:[256,1,1],bindings:[{name:"fieldA",resource:"fftA_3"},{name:"fieldB",resource:"fftB_3"},{name:"disp",resource:"dispTex3"},{name:"slope",resource:"slopeTex3"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:128}}]},{name:"c3.dispTex3.mips",type:"repeat",count:7,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[64,1,1],bindings:[{name:"src",resource:"dispTex3",levelAt:{index:"l"}},{name:"dst",resource:"dispTex3",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:128},dstWidth:{index:"l",offset:1,shr:128},pad:0,srcLevel:0}}]}]},{name:"c3.slopeTex3.mips",type:"repeat",count:7,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[64,1,1],bindings:[{name:"src",resource:"slopeTex3",levelAt:{index:"l"}},{name:"dst",resource:"slopeTex3",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:128},dstWidth:{index:"l",offset:1,shr:128},pad:0,srcLevel:0}}]}]},{name:"wake.source",kernel:"wake_source",when:"param:wakeEnabled",dispatch:[1024,1,1],bindings:[{name:"wakeIn",resource:"wakeA"},{name:"fftOut",resource:"wakeF"},{name:"cleanOut",resource:"wakeC"}],uniforms:[{name:"wake",values:{bowFoam:"param:wakeBowFoam",bowGain:"param:wakeBowGain",cell:"param:wakeCell",depth:"param:wakeDepth",diffuse:"param:wakeDiffuse",dt:"param:wakeDt",foamSpeed:5,fullSpeed:3,heading:"param:hullHeading",hullBeam:"param:hullBeam",hullLen:"param:hullLength",hullU:"param:wakeHullU",hullV:"param:wakeHullV",jitter:"param:wakeJitter",originX:"param:wakeOriginX",originZ:"param:wakeOriginZ",reset:"param:wakeReset",shiftX:"param:wakeShiftX",shiftZ:"param:wakeShiftZ",speed:"param:hullSpeed",srcBeam:"param:wakeSrcBeam",srcLength:"param:wakeSrcLength",tau:"param:wakeFoamTau",width:256}}]},{name:"wake.fft1.fft_rows",kernel:"fft_compute_256",when:"param:wakeEnabled",dispatch:[256,1,1],bindings:[{name:"data",resource:"wakeF"}]},{name:"wake.fft1.fft_cols",kernel:"fft_compute_256",entry:"fft_cols",when:"param:wakeEnabled",dispatch:[256,1,1],bindings:[{name:"data",resource:"wakeF"}]},{name:"wake.symbol",kernel:"wake_symbol",when:"param:wakeEnabled",dispatch:[1024,1,1],bindings:[{name:"fftIn",resource:"wakeF"},{name:"fftOut",resource:"wakeG"}],uniforms:[{name:"sym",values:{n:256,window:"param:wakeLength"}}]},{name:"wake.fft2.fft_rows",kernel:"fft_compute_256",when:"param:wakeEnabled",dispatch:[256,1,1],bindings:[{name:"data",resource:"wakeG"}]},{name:"wake.fft2.fft_cols",kernel:"fft_compute_256",entry:"fft_cols",when:"param:wakeEnabled",dispatch:[256,1,1],bindings:[{name:"data",resource:"wakeG"}]},{name:"wake.step",kernel:"wake_step",when:"param:wakeEnabled",dispatch:[1024,1,1],bindings:[{name:"qIn",resource:"wakeG"},{name:"cleanIn",resource:"wakeC"},{name:"stateOut",resource:"wakeB"},{name:"wakeTex",resource:"wakeTex"}],uniforms:[{name:"wstep",values:{alpha:"param:wakeAlpha",cell:"param:wakeCell",dt:"param:wakeDt",gravity:"param:wakeGEff",originX:"param:wakeOriginX",originZ:"param:wakeOriginZ",sponge:24,spongeRate:.8,width:256}}]},{name:"wake.wakeTex.mips",type:"repeat",when:"param:wakeEnabled",count:8,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[256,1,1],bindings:[{name:"src",resource:"wakeTex",levelAt:{index:"l"}},{name:"dst",resource:"wakeTex",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:256},dstWidth:{index:"l",offset:1,shr:256},pad:0,srcLevel:0}}]}]},{name:"foam",kernel:"ocean_foam",dispatch:[16384,1,1],bindings:[{name:"foamIn",resource:"foamA"},{name:"fieldB0",resource:"fftB_0"},{name:"fieldB1",resource:"fftB_1"},{name:"fieldB2",resource:"fftB_2"},{name:"fieldB3",resource:"fftB_3"},{name:"foamOut",resource:"foamB"},{name:"foamTex",resource:"foamTex"},{name:"wakeIn",resource:"wakeB"}],uniforms:[{name:"foam",values:{cascades:4,cell:.5,dt:"param:dt",gain:"param:foamGain",lambda:"param:lambda",n0:64,n1:128,n2:128,n3:128,omX0:"param:foamOriginMod0X",omX1:"param:foamOriginMod1X",omX2:"param:foamOriginMod2X",omX3:"param:foamOriginMod3X",omZ0:"param:foamOriginMod0Z",omZ1:"param:foamOriginMod1Z",omZ2:"param:foamOriginMod2Z",omZ3:"param:foamOriginMod3Z",originX:"param:foamOriginX",originZ:"param:foamOriginZ",shiftX:"param:foamShiftX",shiftZ:"param:foamShiftZ",size0:1600,size1:250,size2:37,size3:5.3,tau:"param:foamTau",threshold:"param:foamThreshold",wakeCell:"param:wakeCell",wakeGain:"param:wakeFoamGainEff",wakeOriginX:"param:wakeOriginModX",wakeOriginZ:"param:wakeOriginModZ",wakeRelX:"param:wakeRelX",wakeRelZ:"param:wakeRelZ",wakeWidth:256,width:1024}}]},{name:"foamTex.mips",type:"repeat",count:10,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[4096,1,1],bindings:[{name:"src",resource:"foamTex",levelAt:{index:"l"}},{name:"dst",resource:"foamTex",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:1024},dstWidth:{index:"l",offset:1,shr:1024},pad:0,srcLevel:0}}]}]},{name:"probe",kernel:"ocean_probe",dispatch:[1,1,1],bindings:[{name:"points",resource:"probePoints"},{name:"disp0",resource:"fftA_0"},{name:"disp1",resource:"fftA_1"},{name:"disp2",resource:"fftA_2"},{name:"disp3",resource:"fftA_3"},{name:"out",resource:"probeOut"}],uniforms:[{name:"probe",values:{cascades:4,count:"param:probeCount",lambda:"param:lambda",n0:64,n1:128,n2:128,n3:128,normalStep:.5,omX0:"param:probeOriginMod0X",omX1:"param:probeOriginMod1X",omX2:"param:probeOriginMod2X",omX3:"param:probeOriginMod3X",omZ0:"param:probeOriginMod0Z",omZ1:"param:probeOriginMod1Z",omZ2:"param:probeOriginMod2Z",omZ3:"param:probeOriginMod3Z",size0:1600,size1:250,size2:37,size3:5.3,t:"param:simTime"}}]}],frameSwaps:[["wakeA","wakeB"],["foamA","foamB"]],exports:{disp0:"dispTex0",disp1:"dispTex1",disp2:"dispTex2",disp3:"dispTex3",foam:"foamTex",probe:"probeOut",slope0:"slopeTex0",slope1:"slopeTex1",slope2:"slopeTex2",slope3:"slopeTex3",wake:"wakeTex"},paramsFrom:"webgpu/high"},"webgpu/low":{name:"ocean",backend:"webgpu",tier:"low",version:1,resources:[{name:"h0_0",kind:"buffer",elementType:"vec4<f32>",count:4096},{name:"omega_0",kind:"buffer",elementType:"f32",count:4096},{name:"fftA_0",kind:"buffer",elementType:"vec4<f32>",count:4096},{name:"fftB_0",kind:"buffer",elementType:"vec4<f32>",count:4096},{name:"dispTex0",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"slopeTex0",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"h0_1",kind:"buffer",elementType:"vec4<f32>",count:4096},{name:"omega_1",kind:"buffer",elementType:"f32",count:4096},{name:"fftA_1",kind:"buffer",elementType:"vec4<f32>",count:4096},{name:"fftB_1",kind:"buffer",elementType:"vec4<f32>",count:4096},{name:"dispTex1",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"slopeTex1",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"h0_2",kind:"buffer",elementType:"vec4<f32>",count:4096},{name:"omega_2",kind:"buffer",elementType:"f32",count:4096},{name:"fftA_2",kind:"buffer",elementType:"vec4<f32>",count:4096},{name:"fftB_2",kind:"buffer",elementType:"vec4<f32>",count:4096},{name:"dispTex2",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"slopeTex2",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"wakeA",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"wakeB",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"wakeF",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"wakeG",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"wakeC",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"wakeTex",kind:"texture",format:"rgba16float",width:256,height:256,mipLevels:9},{name:"foamA",kind:"buffer",elementType:"f32",count:1048576},{name:"foamB",kind:"buffer",elementType:"f32",count:1048576},{name:"foamTex",kind:"texture",format:"rgba16float",width:1024,height:1024,mipLevels:11},{name:"probePoints",kind:"buffer",elementType:"vec4<f32>",count:64,feed:"probePoints"},{name:"probeOut",kind:"buffer",elementType:"vec4<f32>",count:64}],passes:[{name:"c0.spectrum",kernel:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",dispatch:[64,1,1],bindings:[{name:"h0",resource:"h0_0"},{name:"omega",resource:"omega_0"}],uniforms:[{name:"spec",values:{cascade:0,fetchKm:"param:fetch",gamma:"param:gamma",kMax:.12,kMin:.003926990816987242,loopPeriod:"param:loopPeriod",n:64,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:1600,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c0.evolve",kernel:"ocean_evolve",dispatch:[64,1,1],bindings:[{name:"h0",resource:"h0_0"},{name:"omega",resource:"omega_0"},{name:"outA",resource:"fftA_0"},{name:"outB",resource:"fftB_0"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:64,t:"param:phaseTime",tile:1600}}]},{name:"c0.fftA.fft_rows",kernel:"fft_compute_64",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftA_0"}]},{name:"c0.fftA.fft_cols",kernel:"fft_compute_64",entry:"fft_cols",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftA_0"}]},{name:"c0.fftB.fft_rows",kernel:"fft_compute_64",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftB_0"}]},{name:"c0.fftB.fft_cols",kernel:"fft_compute_64",entry:"fft_cols",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftB_0"}]},{name:"c0.assemble",kernel:"ocean_assemble",dispatch:[64,1,1],bindings:[{name:"fieldA",resource:"fftA_0"},{name:"fieldB",resource:"fftB_0"},{name:"disp",resource:"dispTex0"},{name:"slope",resource:"slopeTex0"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:64}}]},{name:"c0.dispTex0.mips",type:"repeat",count:6,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[16,1,1],bindings:[{name:"src",resource:"dispTex0",levelAt:{index:"l"}},{name:"dst",resource:"dispTex0",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:64},dstWidth:{index:"l",offset:1,shr:64},pad:0,srcLevel:0}}]}]},{name:"c0.slopeTex0.mips",type:"repeat",count:6,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[16,1,1],bindings:[{name:"src",resource:"slopeTex0",levelAt:{index:"l"}},{name:"dst",resource:"slopeTex0",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:64},dstWidth:{index:"l",offset:1,shr:64},pad:0,srcLevel:0}}]}]},{name:"c1.spectrum",kernel:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",dispatch:[64,1,1],bindings:[{name:"h0",resource:"h0_1"},{name:"omega",resource:"omega_1"}],uniforms:[{name:"spec",values:{cascade:1,fetchKm:"param:fetch",gamma:"param:gamma",kMax:.8,kMin:.12,loopPeriod:"param:loopPeriod",n:64,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:250,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c1.evolve",kernel:"ocean_evolve",dispatch:[64,1,1],bindings:[{name:"h0",resource:"h0_1"},{name:"omega",resource:"omega_1"},{name:"outA",resource:"fftA_1"},{name:"outB",resource:"fftB_1"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:64,t:"param:phaseTime",tile:250}}]},{name:"c1.fftA.fft_rows",kernel:"fft_compute_64",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftA_1"}]},{name:"c1.fftA.fft_cols",kernel:"fft_compute_64",entry:"fft_cols",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftA_1"}]},{name:"c1.fftB.fft_rows",kernel:"fft_compute_64",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftB_1"}]},{name:"c1.fftB.fft_cols",kernel:"fft_compute_64",entry:"fft_cols",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftB_1"}]},{name:"c1.assemble",kernel:"ocean_assemble",dispatch:[64,1,1],bindings:[{name:"fieldA",resource:"fftA_1"},{name:"fieldB",resource:"fftB_1"},{name:"disp",resource:"dispTex1"},{name:"slope",resource:"slopeTex1"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:64}}]},{name:"c1.dispTex1.mips",type:"repeat",count:6,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[16,1,1],bindings:[{name:"src",resource:"dispTex1",levelAt:{index:"l"}},{name:"dst",resource:"dispTex1",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:64},dstWidth:{index:"l",offset:1,shr:64},pad:0,srcLevel:0}}]}]},{name:"c1.slopeTex1.mips",type:"repeat",count:6,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[16,1,1],bindings:[{name:"src",resource:"slopeTex1",levelAt:{index:"l"}},{name:"dst",resource:"slopeTex1",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:64},dstWidth:{index:"l",offset:1,shr:64},pad:0,srcLevel:0}}]}]},{name:"c2.spectrum",kernel:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",dispatch:[64,1,1],bindings:[{name:"h0",resource:"h0_2"},{name:"omega",resource:"omega_2"}],uniforms:[{name:"spec",values:{cascade:2,fetchKm:"param:fetch",gamma:"param:gamma",kMax:5.4,kMin:.8,loopPeriod:"param:loopPeriod",n:64,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:37,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c2.evolve",kernel:"ocean_evolve",dispatch:[64,1,1],bindings:[{name:"h0",resource:"h0_2"},{name:"omega",resource:"omega_2"},{name:"outA",resource:"fftA_2"},{name:"outB",resource:"fftB_2"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:64,t:"param:phaseTime",tile:37}}]},{name:"c2.fftA.fft_rows",kernel:"fft_compute_64",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftA_2"}]},{name:"c2.fftA.fft_cols",kernel:"fft_compute_64",entry:"fft_cols",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftA_2"}]},{name:"c2.fftB.fft_rows",kernel:"fft_compute_64",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftB_2"}]},{name:"c2.fftB.fft_cols",kernel:"fft_compute_64",entry:"fft_cols",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftB_2"}]},{name:"c2.assemble",kernel:"ocean_assemble",dispatch:[64,1,1],bindings:[{name:"fieldA",resource:"fftA_2"},{name:"fieldB",resource:"fftB_2"},{name:"disp",resource:"dispTex2"},{name:"slope",resource:"slopeTex2"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:64}}]},{name:"c2.dispTex2.mips",type:"repeat",count:6,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[16,1,1],bindings:[{name:"src",resource:"dispTex2",levelAt:{index:"l"}},{name:"dst",resource:"dispTex2",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:64},dstWidth:{index:"l",offset:1,shr:64},pad:0,srcLevel:0}}]}]},{name:"c2.slopeTex2.mips",type:"repeat",count:6,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[16,1,1],bindings:[{name:"src",resource:"slopeTex2",levelAt:{index:"l"}},{name:"dst",resource:"slopeTex2",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:64},dstWidth:{index:"l",offset:1,shr:64},pad:0,srcLevel:0}}]}]},{name:"wake.source",kernel:"wake_source",when:"param:wakeEnabled",dispatch:[1024,1,1],bindings:[{name:"wakeIn",resource:"wakeA"},{name:"fftOut",resource:"wakeF"},{name:"cleanOut",resource:"wakeC"}],uniforms:[{name:"wake",values:{bowFoam:"param:wakeBowFoam",bowGain:"param:wakeBowGain",cell:"param:wakeCell",depth:"param:wakeDepth",diffuse:"param:wakeDiffuse",dt:"param:wakeDt",foamSpeed:5,fullSpeed:3,heading:"param:hullHeading",hullBeam:"param:hullBeam",hullLen:"param:hullLength",hullU:"param:wakeHullU",hullV:"param:wakeHullV",jitter:"param:wakeJitter",originX:"param:wakeOriginX",originZ:"param:wakeOriginZ",reset:"param:wakeReset",shiftX:"param:wakeShiftX",shiftZ:"param:wakeShiftZ",speed:"param:hullSpeed",srcBeam:"param:wakeSrcBeam",srcLength:"param:wakeSrcLength",tau:"param:wakeFoamTau",width:256}}]},{name:"wake.fft1.fft_rows",kernel:"fft_compute_256",when:"param:wakeEnabled",dispatch:[256,1,1],bindings:[{name:"data",resource:"wakeF"}]},{name:"wake.fft1.fft_cols",kernel:"fft_compute_256",entry:"fft_cols",when:"param:wakeEnabled",dispatch:[256,1,1],bindings:[{name:"data",resource:"wakeF"}]},{name:"wake.symbol",kernel:"wake_symbol",when:"param:wakeEnabled",dispatch:[1024,1,1],bindings:[{name:"fftIn",resource:"wakeF"},{name:"fftOut",resource:"wakeG"}],uniforms:[{name:"sym",values:{n:256,window:"param:wakeLength"}}]},{name:"wake.fft2.fft_rows",kernel:"fft_compute_256",when:"param:wakeEnabled",dispatch:[256,1,1],bindings:[{name:"data",resource:"wakeG"}]},{name:"wake.fft2.fft_cols",kernel:"fft_compute_256",entry:"fft_cols",when:"param:wakeEnabled",dispatch:[256,1,1],bindings:[{name:"data",resource:"wakeG"}]},{name:"wake.step",kernel:"wake_step",when:"param:wakeEnabled",dispatch:[1024,1,1],bindings:[{name:"qIn",resource:"wakeG"},{name:"cleanIn",resource:"wakeC"},{name:"stateOut",resource:"wakeB"},{name:"wakeTex",resource:"wakeTex"}],uniforms:[{name:"wstep",values:{alpha:"param:wakeAlpha",cell:"param:wakeCell",dt:"param:wakeDt",gravity:"param:wakeGEff",originX:"param:wakeOriginX",originZ:"param:wakeOriginZ",sponge:24,spongeRate:.8,width:256}}]},{name:"wake.wakeTex.mips",type:"repeat",when:"param:wakeEnabled",count:8,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[256,1,1],bindings:[{name:"src",resource:"wakeTex",levelAt:{index:"l"}},{name:"dst",resource:"wakeTex",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:256},dstWidth:{index:"l",offset:1,shr:256},pad:0,srcLevel:0}}]}]},{name:"foam",kernel:"ocean_foam",dispatch:[16384,1,1],bindings:[{name:"foamIn",resource:"foamA"},{name:"fieldB0",resource:"fftB_0"},{name:"fieldB1",resource:"fftB_1"},{name:"fieldB2",resource:"fftB_2"},{name:"fieldB3",resource:"fftB_2"},{name:"foamOut",resource:"foamB"},{name:"foamTex",resource:"foamTex"},{name:"wakeIn",resource:"wakeB"}],uniforms:[{name:"foam",values:{cascades:3,cell:.5,dt:"param:dt",gain:"param:foamGain",lambda:"param:lambda",n0:64,n1:64,n2:64,n3:64,omX0:"param:foamOriginMod0X",omX1:"param:foamOriginMod1X",omX2:"param:foamOriginMod2X",omX3:"param:foamOriginMod2X",omZ0:"param:foamOriginMod0Z",omZ1:"param:foamOriginMod1Z",omZ2:"param:foamOriginMod2Z",omZ3:"param:foamOriginMod2Z",originX:"param:foamOriginX",originZ:"param:foamOriginZ",shiftX:"param:foamShiftX",shiftZ:"param:foamShiftZ",size0:1600,size1:250,size2:37,size3:37,tau:"param:foamTau",threshold:"param:foamThreshold",wakeCell:"param:wakeCell",wakeGain:"param:wakeFoamGainEff",wakeOriginX:"param:wakeOriginModX",wakeOriginZ:"param:wakeOriginModZ",wakeRelX:"param:wakeRelX",wakeRelZ:"param:wakeRelZ",wakeWidth:256,width:1024}}]},{name:"foamTex.mips",type:"repeat",count:10,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[4096,1,1],bindings:[{name:"src",resource:"foamTex",levelAt:{index:"l"}},{name:"dst",resource:"foamTex",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:1024},dstWidth:{index:"l",offset:1,shr:1024},pad:0,srcLevel:0}}]}]},{name:"probe",kernel:"ocean_probe",dispatch:[1,1,1],bindings:[{name:"points",resource:"probePoints"},{name:"disp0",resource:"fftA_0"},{name:"disp1",resource:"fftA_1"},{name:"disp2",resource:"fftA_2"},{name:"disp3",resource:"fftA_2"},{name:"out",resource:"probeOut"}],uniforms:[{name:"probe",values:{cascades:3,count:"param:probeCount",lambda:"param:lambda",n0:64,n1:64,n2:64,n3:64,normalStep:.5,omX0:"param:probeOriginMod0X",omX1:"param:probeOriginMod1X",omX2:"param:probeOriginMod2X",omX3:"param:probeOriginMod2X",omZ0:"param:probeOriginMod0Z",omZ1:"param:probeOriginMod1Z",omZ2:"param:probeOriginMod2Z",omZ3:"param:probeOriginMod2Z",size0:1600,size1:250,size2:37,size3:37,t:"param:simTime"}}]}],frameSwaps:[["wakeA","wakeB"],["foamA","foamB"]],exports:{disp0:"dispTex0",disp1:"dispTex1",disp2:"dispTex2",foam:"foamTex",probe:"probeOut",slope0:"slopeTex0",slope1:"slopeTex1",slope2:"slopeTex2",wake:"wakeTex"},paramsFrom:"webgpu/high"},"webgl2/high":{name:"ocean",backend:"webgl2",tier:"high",version:1,params:[{name:"windSpeed",type:"f32",source:{kind:"value",value:9},tween:20},{name:"fetch",type:"f32",source:{kind:"value",value:300},tween:20},{name:"gamma",type:"f32",source:{kind:"value",value:3.3},tween:20},{name:"spread",type:"f32",source:{kind:"value",value:8},tween:20},{name:"swell",type:"f32",source:{kind:"value",value:0},tween:20},{name:"windDirDeg",type:"f32",source:{kind:"value",value:0},tween:20},{name:"swellDirDeg",type:"f32",source:{kind:"value",value:0},tween:20},{name:"seed",type:"u32",source:{kind:"value",value:0}},{name:"loopPeriod",type:"f32",source:{kind:"value",value:1e3}},{name:"lambda",type:"f32",source:{kind:"value",value:1},tween:20},{name:"timeScale",type:"f32",source:{kind:"value",value:1}},{name:"driftX",type:"f32",source:{kind:"value",value:0}},{name:"driftZ",type:"f32",source:{kind:"value",value:0}},{name:"foamTau",type:"f32",source:{kind:"value",value:4}},{name:"foamThreshold",type:"f32",source:{kind:"value",value:.9}},{name:"foamGain",type:"f32",source:{kind:"value",value:2}},{name:"probeCount",type:"u32",source:{kind:"value",value:0}},{name:"dt",type:"f32",source:{kind:"frame",name:"dt"}},{name:"cameraX",type:"f32",source:{kind:"frame",name:"cameraX"}},{name:"cameraZ",type:"f32",source:{kind:"frame",name:"cameraZ"}},{name:"windDir",type:"f32",source:{args:[1.5707963267948966,{op:"mul",args:["param:windDirDeg",.017453292519943295]}],kind:"expr",op:"sub"}},{name:"swellDir",type:"f32",source:{args:[1.5707963267948966,{op:"mul",args:["param:swellDirDeg",.017453292519943295]}],kind:"expr",op:"sub"}},{name:"simTime",type:"f32",source:{kind:"accumulate",of:"param:timeScale"}},{name:"phaseTime",type:"f32",source:{args:["param:simTime","param:loopPeriod"],kind:"expr",op:"mod"}},{name:"waterX",type:"f32",source:{kind:"accumulate",of:"param:driftX"}},{name:"waterZ",type:"f32",source:{kind:"accumulate",of:"param:driftZ"}},{name:"waterModX0",type:"f32",source:{args:["param:waterX",1600],kind:"expr",op:"mod"}},{name:"waterModZ0",type:"f32",source:{args:["param:waterZ",1600],kind:"expr",op:"mod"}},{name:"waterModX1",type:"f32",source:{args:["param:waterX",250],kind:"expr",op:"mod"}},{name:"waterModZ1",type:"f32",source:{args:["param:waterZ",250],kind:"expr",op:"mod"}},{name:"waterModX2",type:"f32",source:{args:["param:waterX",37],kind:"expr",op:"mod"}},{name:"waterModZ2",type:"f32",source:{args:["param:waterZ",37],kind:"expr",op:"mod"}},{name:"waterModX3",type:"f32",source:{args:["param:waterX",5.3],kind:"expr",op:"mod"}},{name:"waterModZ3",type:"f32",source:{args:["param:waterZ",5.3],kind:"expr",op:"mod"}},{name:"foamOriginX",type:"i32",source:{args:[{op:"floor",args:[{op:"div",args:[{op:"sub",args:["param:cameraX","param:waterX"]},.5]}]},512],kind:"expr",op:"sub"}},{name:"foamShiftX",type:"i32",source:{kind:"delta",of:"param:foamOriginX"}},{name:"foamOriginMod0X",type:"f32",source:{args:[{op:"mul",args:["param:foamOriginX",.5]},1600],kind:"expr",op:"mod"}},{name:"foamOriginMod1X",type:"f32",source:{args:[{op:"mul",args:["param:foamOriginX",.5]},250],kind:"expr",op:"mod"}},{name:"foamOriginMod2X",type:"f32",source:{args:[{op:"mul",args:["param:foamOriginX",.5]},37],kind:"expr",op:"mod"}},{name:"foamOriginMod3X",type:"f32",source:{args:[{op:"mul",args:["param:foamOriginX",.5]},5.3],kind:"expr",op:"mod"}},{name:"foamOriginZ",type:"i32",source:{args:[{op:"floor",args:[{op:"div",args:[{op:"sub",args:["param:cameraZ","param:waterZ"]},.5]}]},512],kind:"expr",op:"sub"}},{name:"foamShiftZ",type:"i32",source:{kind:"delta",of:"param:foamOriginZ"}},{name:"foamOriginMod0Z",type:"f32",source:{args:[{op:"mul",args:["param:foamOriginZ",.5]},1600],kind:"expr",op:"mod"}},{name:"foamOriginMod1Z",type:"f32",source:{args:[{op:"mul",args:["param:foamOriginZ",.5]},250],kind:"expr",op:"mod"}},{name:"foamOriginMod2Z",type:"f32",source:{args:[{op:"mul",args:["param:foamOriginZ",.5]},37],kind:"expr",op:"mod"}},{name:"foamOriginMod3Z",type:"f32",source:{args:[{op:"mul",args:["param:foamOriginZ",.5]},5.3],kind:"expr",op:"mod"}},{name:"hullX",type:"f32",source:{kind:"value",value:0}},{name:"hullZ",type:"f32",source:{kind:"value",value:0}},{name:"hullHeading",type:"f32",source:{kind:"value",value:0}},{name:"hullSpeed",type:"f32",source:{kind:"value",value:0}},{name:"wakeEnabled",type:"bool",source:{kind:"value",value:0}},{name:"hullLength",type:"f32",source:{kind:"value",value:200}},{name:"hullBeam",type:"f32",source:{kind:"value",value:30}},{name:"wakeCell",type:"f32",source:{kind:"value",value:4}},{name:"wakeDepth",type:"f32",source:{kind:"value",value:.4}},{name:"wakeGravity",type:"f32",source:{kind:"value",value:9.81}},{name:"wakeAlpha",type:"f32",source:{kind:"value",value:.02}},{name:"wakeMinLambdaCells",type:"f32",source:{kind:"value",value:8}},{name:"wakeFoamGain",type:"f32",source:{kind:"value",value:1}},{name:"wakeFoamTau",type:"f32",source:{kind:"value",value:48}},{name:"wakeDiffuse",type:"f32",source:{kind:"value",value:1.5}},{name:"wakeJitter",type:"f32",source:{kind:"value",value:1}},{name:"wakeBowGain",type:"f32",source:{kind:"value",value:.2}},{name:"wakeBowFoam",type:"f32",source:{kind:"value",value:.9}},{name:"wakeOn",type:"f32",source:{args:["param:wakeEnabled",1],kind:"expr",op:"mul"}},{name:"wakeFoamGainEff",type:"f32",source:{args:["param:wakeEnabled","param:wakeFoamGain"],kind:"expr",op:"mul"}},{name:"wakeEdge",type:"i32",source:{kind:"delta",of:"param:wakeEnabled"}},{name:"wakeReset",type:"u32",source:{args:["param:wakeEdge",0],kind:"expr",op:"max"}},{name:"wakeLength",type:"f32",source:{args:["param:wakeCell",256],kind:"expr",op:"mul"}},{name:"hullWaterX",type:"f32",source:{args:["param:hullX","param:waterX"],kind:"expr",op:"sub"}},{name:"wakeOriginX",type:"i32",source:{args:[{op:"div",args:[{op:"sub",args:[{op:"sub",args:["param:hullWaterX",{op:"mul",args:[{op:"sin",args:["param:hullHeading"]},{op:"mul",args:["param:wakeCell",51.2]}]}]},{op:"mul",args:["param:wakeCell",128]}]},"param:wakeCell"]}],kind:"expr",op:"floor"}},{name:"wakeShiftX",type:"i32",source:{kind:"delta",of:"param:wakeOriginX"}},{name:"wakeHullU",type:"f32",source:{args:["param:hullWaterX",{op:"mul",args:["param:wakeOriginX","param:wakeCell"]}],kind:"expr",op:"sub"}},{name:"wakeOriginModX",type:"i32",source:{args:["param:wakeOriginX",256],kind:"expr",op:"mod"}},{name:"wakeRelX",type:"f32",source:{args:[{op:"mul",args:["param:foamOriginX",.5]},{op:"mul",args:["param:wakeOriginX","param:wakeCell"]}],kind:"expr",op:"sub"}},{name:"wakeCamRelX",type:"f32",source:{args:[{op:"add",args:["param:waterX",{op:"mul",args:["param:wakeOriginX","param:wakeCell"]}]},"param:cameraX"],kind:"expr",op:"sub"}},{name:"hullWaterZ",type:"f32",source:{args:["param:hullZ","param:waterZ"],kind:"expr",op:"sub"}},{name:"wakeOriginZ",type:"i32",source:{args:[{op:"div",args:[{op:"sub",args:[{op:"sub",args:["param:hullWaterZ",{op:"mul",args:[{op:"cos",args:["param:hullHeading"]},{op:"mul",args:["param:wakeCell",51.2]}]}]},{op:"mul",args:["param:wakeCell",128]}]},"param:wakeCell"]}],kind:"expr",op:"floor"}},{name:"wakeShiftZ",type:"i32",source:{kind:"delta",of:"param:wakeOriginZ"}},{name:"wakeHullV",type:"f32",source:{args:["param:hullWaterZ",{op:"mul",args:["param:wakeOriginZ","param:wakeCell"]}],kind:"expr",op:"sub"}},{name:"wakeOriginModZ",type:"i32",source:{args:["param:wakeOriginZ",256],kind:"expr",op:"mod"}},{name:"wakeRelZ",type:"f32",source:{args:[{op:"mul",args:["param:foamOriginZ",.5]},{op:"mul",args:["param:wakeOriginZ","param:wakeCell"]}],kind:"expr",op:"sub"}},{name:"wakeCamRelZ",type:"f32",source:{args:[{op:"add",args:["param:waterZ",{op:"mul",args:["param:wakeOriginZ","param:wakeCell"]}]},"param:cameraZ"],kind:"expr",op:"sub"}},{name:"wakeGEff",type:"f32",source:{args:[{op:"min",args:[{op:"min",args:["param:wakeGravity",{op:"mul",args:[{op:"mul",args:["param:hullSpeed","param:hullSpeed"]},{op:"div",args:[6.283185307179586,{op:"mul",args:["param:wakeMinLambdaCells","param:wakeCell"]}]}]}]},{op:"mul",args:["param:wakeCell",16.39001041697031]}]},.05],kind:"expr",op:"max"}},{name:"wakeDt",type:"f32",source:{args:[{op:"mul",args:["param:dt","param:timeScale"]},.2],kind:"expr",op:"min"}},{name:"wakeSrcLength",type:"f32",source:{args:[{op:"min",args:["param:hullLength",{op:"div",args:[{op:"mul",args:[{op:"mul",args:["param:hullSpeed","param:hullSpeed"]},3.141592653589793]},"param:wakeGEff"]}]},{op:"mul",args:["param:wakeCell",2]}],kind:"expr",op:"max"}},{name:"wakeSrcBeam",type:"f32",source:{args:[{op:"min",args:["param:hullBeam",{op:"mul",args:["param:wakeSrcLength",.67]}]},{op:"mul",args:["param:wakeCell",1.5]}],kind:"expr",op:"max"}}],resources:[{name:"h0_0",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"omega_0",kind:"buffer",elementType:"f32",count:4096,width:64},{name:"fftA_0",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"fftB_0",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"fftA2_0",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"fftB2_0",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"dispTex0",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"slopeTex0",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"h0_1",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"omega_1",kind:"buffer",elementType:"f32",count:65536,width:256},{name:"fftA_1",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"fftB_1",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"fftA2_1",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"fftB2_1",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"dispTex1",kind:"texture",format:"rgba16float",width:256,height:256,mipLevels:9},{name:"slopeTex1",kind:"texture",format:"rgba16float",width:256,height:256,mipLevels:9},{name:"h0_2",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"omega_2",kind:"buffer",elementType:"f32",count:65536,width:256},{name:"fftA_2",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"fftB_2",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"fftA2_2",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"fftB2_2",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"dispTex2",kind:"texture",format:"rgba16float",width:256,height:256,mipLevels:9},{name:"slopeTex2",kind:"texture",format:"rgba16float",width:256,height:256,mipLevels:9},{name:"h0_3",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"omega_3",kind:"buffer",elementType:"f32",count:65536,width:256},{name:"fftA_3",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"fftB_3",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"fftA2_3",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"fftB2_3",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"dispTex3",kind:"texture",format:"rgba16float",width:256,height:256,mipLevels:9},{name:"slopeTex3",kind:"texture",format:"rgba16float",width:256,height:256,mipLevels:9},{name:"wakeA",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"wakeB",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"wakeF",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"wakeG",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"wakeC",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"wakeTex",kind:"texture",format:"rgba16float",width:256,height:256,mipLevels:9},{name:"foamA",kind:"buffer",elementType:"f32",count:1048576,width:1024},{name:"foamB",kind:"buffer",elementType:"f32",count:1048576,width:1024},{name:"foamTex",kind:"texture",format:"rgba16float",width:1024,height:1024,mipLevels:11}],passes:[{name:"c0.spectrum",kernel:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",target:{width:64,height:64,elioWidth:64},bindings:[{name:"h0",resource:"h0_0"},{name:"omega",resource:"omega_0"}],uniforms:[{name:"spec",values:{cascade:0,fetchKm:"param:fetch",gamma:"param:gamma",kMax:.12,kMin:.003926990816987242,loopPeriod:"param:loopPeriod",n:64,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:1600,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c0.evolve",kernel:"ocean_evolve",target:{width:64,height:64,elioWidth:64},bindings:[{name:"h0",resource:"h0_0"},{name:"omega",resource:"omega_0"},{name:"outA",resource:"fftA_0"},{name:"outB",resource:"fftB_0"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:64,t:"param:phaseTime",tile:1600}}]},{name:"c0.fft.rows",type:"repeat",count:6,index:"s",swap:[["fftA_0","fftA2_0"],["fftB_0","fftB2_0"]],body:[{name:"A",kernel:"fft_gather_64",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftA_0"},{name:"dst",resource:"fftA2_0"}],uniforms:[{name:"fft",values:{axis:0,n:64,stage:"param:s"}}]},{name:"B",kernel:"fft_gather_64",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftB_0"},{name:"dst",resource:"fftB2_0"}],uniforms:[{name:"fft",values:{axis:0,n:64,stage:"param:s"}}]}]},{name:"c0.fft.cols",type:"repeat",count:6,index:"s",swap:[["fftA_0","fftA2_0"],["fftB_0","fftB2_0"]],body:[{name:"A",kernel:"fft_gather_64",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftA_0"},{name:"dst",resource:"fftA2_0"}],uniforms:[{name:"fft",values:{axis:1,n:64,stage:"param:s"}}]},{name:"B",kernel:"fft_gather_64",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftB_0"},{name:"dst",resource:"fftB2_0"}],uniforms:[{name:"fft",values:{axis:1,n:64,stage:"param:s"}}]}]},{name:"c0.assemble",kernel:"ocean_assemble",target:{width:64,height:64,elioWidth:64},bindings:[{name:"fieldA",resource:"fftA_0"},{name:"fieldB",resource:"fftB_0"},{name:"disp",resource:"dispTex0"},{name:"slope",resource:"slopeTex0"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:64}}]},{name:"c0.dispTex0.mips",type:"mipmap",resource:"dispTex0"},{name:"c0.slopeTex0.mips",type:"mipmap",resource:"slopeTex0"},{name:"c1.spectrum",kernel:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",target:{width:256,height:256,elioWidth:256},bindings:[{name:"h0",resource:"h0_1"},{name:"omega",resource:"omega_1"}],uniforms:[{name:"spec",values:{cascade:1,fetchKm:"param:fetch",gamma:"param:gamma",kMax:.8,kMin:.12,loopPeriod:"param:loopPeriod",n:256,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:250,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c1.evolve",kernel:"ocean_evolve",target:{width:256,height:256,elioWidth:256},bindings:[{name:"h0",resource:"h0_1"},{name:"omega",resource:"omega_1"},{name:"outA",resource:"fftA_1"},{name:"outB",resource:"fftB_1"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:256,t:"param:phaseTime",tile:250}}]},{name:"c1.fft.rows",type:"repeat",count:8,index:"s",swap:[["fftA_1","fftA2_1"],["fftB_1","fftB2_1"]],body:[{name:"A",kernel:"fft_gather_256",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"fftA_1"},{name:"dst",resource:"fftA2_1"}],uniforms:[{name:"fft",values:{axis:0,n:256,stage:"param:s"}}]},{name:"B",kernel:"fft_gather_256",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"fftB_1"},{name:"dst",resource:"fftB2_1"}],uniforms:[{name:"fft",values:{axis:0,n:256,stage:"param:s"}}]}]},{name:"c1.fft.cols",type:"repeat",count:8,index:"s",swap:[["fftA_1","fftA2_1"],["fftB_1","fftB2_1"]],body:[{name:"A",kernel:"fft_gather_256",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"fftA_1"},{name:"dst",resource:"fftA2_1"}],uniforms:[{name:"fft",values:{axis:1,n:256,stage:"param:s"}}]},{name:"B",kernel:"fft_gather_256",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"fftB_1"},{name:"dst",resource:"fftB2_1"}],uniforms:[{name:"fft",values:{axis:1,n:256,stage:"param:s"}}]}]},{name:"c1.assemble",kernel:"ocean_assemble",target:{width:256,height:256,elioWidth:256},bindings:[{name:"fieldA",resource:"fftA_1"},{name:"fieldB",resource:"fftB_1"},{name:"disp",resource:"dispTex1"},{name:"slope",resource:"slopeTex1"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:256}}]},{name:"c1.dispTex1.mips",type:"mipmap",resource:"dispTex1"},{name:"c1.slopeTex1.mips",type:"mipmap",resource:"slopeTex1"},{name:"c2.spectrum",kernel:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",target:{width:256,height:256,elioWidth:256},bindings:[{name:"h0",resource:"h0_2"},{name:"omega",resource:"omega_2"}],uniforms:[{name:"spec",values:{cascade:2,fetchKm:"param:fetch",gamma:"param:gamma",kMax:5.4,kMin:.8,loopPeriod:"param:loopPeriod",n:256,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:37,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c2.evolve",kernel:"ocean_evolve",target:{width:256,height:256,elioWidth:256},bindings:[{name:"h0",resource:"h0_2"},{name:"omega",resource:"omega_2"},{name:"outA",resource:"fftA_2"},{name:"outB",resource:"fftB_2"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:256,t:"param:phaseTime",tile:37}}]},{name:"c2.fft.rows",type:"repeat",count:8,index:"s",swap:[["fftA_2","fftA2_2"],["fftB_2","fftB2_2"]],body:[{name:"A",kernel:"fft_gather_256",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"fftA_2"},{name:"dst",resource:"fftA2_2"}],uniforms:[{name:"fft",values:{axis:0,n:256,stage:"param:s"}}]},{name:"B",kernel:"fft_gather_256",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"fftB_2"},{name:"dst",resource:"fftB2_2"}],uniforms:[{name:"fft",values:{axis:0,n:256,stage:"param:s"}}]}]},{name:"c2.fft.cols",type:"repeat",count:8,index:"s",swap:[["fftA_2","fftA2_2"],["fftB_2","fftB2_2"]],body:[{name:"A",kernel:"fft_gather_256",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"fftA_2"},{name:"dst",resource:"fftA2_2"}],uniforms:[{name:"fft",values:{axis:1,n:256,stage:"param:s"}}]},{name:"B",kernel:"fft_gather_256",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"fftB_2"},{name:"dst",resource:"fftB2_2"}],uniforms:[{name:"fft",values:{axis:1,n:256,stage:"param:s"}}]}]},{name:"c2.assemble",kernel:"ocean_assemble",target:{width:256,height:256,elioWidth:256},bindings:[{name:"fieldA",resource:"fftA_2"},{name:"fieldB",resource:"fftB_2"},{name:"disp",resource:"dispTex2"},{name:"slope",resource:"slopeTex2"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:256}}]},{name:"c2.dispTex2.mips",type:"mipmap",resource:"dispTex2"},{name:"c2.slopeTex2.mips",type:"mipmap",resource:"slopeTex2"},{name:"c3.spectrum",kernel:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",target:{width:256,height:256,elioWidth:256},bindings:[{name:"h0",resource:"h0_3"},{name:"omega",resource:"omega_3"}],uniforms:[{name:"spec",values:{cascade:3,fetchKm:"param:fetch",gamma:"param:gamma",kMax:37,kMin:5.4,loopPeriod:"param:loopPeriod",n:256,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:5.3,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c3.evolve",kernel:"ocean_evolve",target:{width:256,height:256,elioWidth:256},bindings:[{name:"h0",resource:"h0_3"},{name:"omega",resource:"omega_3"},{name:"outA",resource:"fftA_3"},{name:"outB",resource:"fftB_3"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:256,t:"param:phaseTime",tile:5.3}}]},{name:"c3.fft.rows",type:"repeat",count:8,index:"s",swap:[["fftA_3","fftA2_3"],["fftB_3","fftB2_3"]],body:[{name:"A",kernel:"fft_gather_256",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"fftA_3"},{name:"dst",resource:"fftA2_3"}],uniforms:[{name:"fft",values:{axis:0,n:256,stage:"param:s"}}]},{name:"B",kernel:"fft_gather_256",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"fftB_3"},{name:"dst",resource:"fftB2_3"}],uniforms:[{name:"fft",values:{axis:0,n:256,stage:"param:s"}}]}]},{name:"c3.fft.cols",type:"repeat",count:8,index:"s",swap:[["fftA_3","fftA2_3"],["fftB_3","fftB2_3"]],body:[{name:"A",kernel:"fft_gather_256",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"fftA_3"},{name:"dst",resource:"fftA2_3"}],uniforms:[{name:"fft",values:{axis:1,n:256,stage:"param:s"}}]},{name:"B",kernel:"fft_gather_256",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"fftB_3"},{name:"dst",resource:"fftB2_3"}],uniforms:[{name:"fft",values:{axis:1,n:256,stage:"param:s"}}]}]},{name:"c3.assemble",kernel:"ocean_assemble",target:{width:256,height:256,elioWidth:256},bindings:[{name:"fieldA",resource:"fftA_3"},{name:"fieldB",resource:"fftB_3"},{name:"disp",resource:"dispTex3"},{name:"slope",resource:"slopeTex3"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:256}}]},{name:"c3.dispTex3.mips",type:"mipmap",resource:"dispTex3"},{name:"c3.slopeTex3.mips",type:"mipmap",resource:"slopeTex3"},{name:"wake.source",kernel:"wake_source",when:"param:wakeEnabled",target:{width:256,height:256,elioWidth:256},bindings:[{name:"wakeIn",resource:"wakeA"},{name:"fftOut",resource:"wakeF"},{name:"cleanOut",resource:"wakeC"}],uniforms:[{name:"wake",values:{bowFoam:"param:wakeBowFoam",bowGain:"param:wakeBowGain",cell:"param:wakeCell",depth:"param:wakeDepth",diffuse:"param:wakeDiffuse",dt:"param:wakeDt",foamSpeed:5,fullSpeed:3,heading:"param:hullHeading",hullBeam:"param:hullBeam",hullLen:"param:hullLength",hullU:"param:wakeHullU",hullV:"param:wakeHullV",jitter:"param:wakeJitter",originX:"param:wakeOriginX",originZ:"param:wakeOriginZ",reset:"param:wakeReset",shiftX:"param:wakeShiftX",shiftZ:"param:wakeShiftZ",speed:"param:hullSpeed",srcBeam:"param:wakeSrcBeam",srcLength:"param:wakeSrcLength",tau:"param:wakeFoamTau",width:256}}]},{name:"wake.fft1.rows",type:"repeat",when:"param:wakeEnabled",count:8,index:"s",swap:[["wakeF","wakeG"]],body:[{name:"stage",kernel:"fft_gather_256",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"wakeF"},{name:"dst",resource:"wakeG"}],uniforms:[{name:"fft",values:{axis:0,n:256,stage:"param:s"}}]}]},{name:"wake.fft1.cols",type:"repeat",when:"param:wakeEnabled",count:8,index:"s",swap:[["wakeF","wakeG"]],body:[{name:"stage",kernel:"fft_gather_256",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"wakeF"},{name:"dst",resource:"wakeG"}],uniforms:[{name:"fft",values:{axis:1,n:256,stage:"param:s"}}]}]},{name:"wake.symbol",kernel:"wake_symbol",when:"param:wakeEnabled",target:{width:256,height:256,elioWidth:256},bindings:[{name:"fftIn",resource:"wakeF"},{name:"fftOut",resource:"wakeG"}],uniforms:[{name:"sym",values:{n:256,window:"param:wakeLength"}}]},{name:"wake.fft2.rows",type:"repeat",when:"param:wakeEnabled",count:8,index:"s",swap:[["wakeG","wakeF"]],body:[{name:"stage",kernel:"fft_gather_256",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"wakeG"},{name:"dst",resource:"wakeF"}],uniforms:[{name:"fft",values:{axis:0,n:256,stage:"param:s"}}]}]},{name:"wake.fft2.cols",type:"repeat",when:"param:wakeEnabled",count:8,index:"s",swap:[["wakeG","wakeF"]],body:[{name:"stage",kernel:"fft_gather_256",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"wakeG"},{name:"dst",resource:"wakeF"}],uniforms:[{name:"fft",values:{axis:1,n:256,stage:"param:s"}}]}]},{name:"wake.step",kernel:"wake_step",when:"param:wakeEnabled",target:{width:256,height:256,elioWidth:256},bindings:[{name:"qIn",resource:"wakeG"},{name:"cleanIn",resource:"wakeC"},{name:"stateOut",resource:"wakeB"},{name:"wakeTex",resource:"wakeTex"}],uniforms:[{name:"wstep",values:{alpha:"param:wakeAlpha",cell:"param:wakeCell",dt:"param:wakeDt",gravity:"param:wakeGEff",originX:"param:wakeOriginX",originZ:"param:wakeOriginZ",sponge:24,spongeRate:.8,width:256}}]},{name:"wake.wakeTex.mips",type:"mipmap",when:"param:wakeEnabled",resource:"wakeTex"},{name:"foam",kernel:"ocean_foam",target:{width:1024,height:1024,elioWidth:1024},bindings:[{name:"foamIn",resource:"foamA"},{name:"fieldB0",resource:"fftB_0"},{name:"fieldB1",resource:"fftB_1"},{name:"fieldB2",resource:"fftB_2"},{name:"fieldB3",resource:"fftB_3"},{name:"foamOut",resource:"foamB"},{name:"foamTex",resource:"foamTex"},{name:"wakeIn",resource:"wakeB"}],uniforms:[{name:"foam",values:{cascades:4,cell:.5,dt:"param:dt",gain:"param:foamGain",lambda:"param:lambda",n0:64,n1:256,n2:256,n3:256,omX0:"param:foamOriginMod0X",omX1:"param:foamOriginMod1X",omX2:"param:foamOriginMod2X",omX3:"param:foamOriginMod3X",omZ0:"param:foamOriginMod0Z",omZ1:"param:foamOriginMod1Z",omZ2:"param:foamOriginMod2Z",omZ3:"param:foamOriginMod3Z",originX:"param:foamOriginX",originZ:"param:foamOriginZ",shiftX:"param:foamShiftX",shiftZ:"param:foamShiftZ",size0:1600,size1:250,size2:37,size3:5.3,tau:"param:foamTau",threshold:"param:foamThreshold",wakeCell:"param:wakeCell",wakeGain:"param:wakeFoamGainEff",wakeOriginX:"param:wakeOriginModX",wakeOriginZ:"param:wakeOriginModZ",wakeRelX:"param:wakeRelX",wakeRelZ:"param:wakeRelZ",wakeWidth:256,width:1024}}]},{name:"foamTex.mips",type:"mipmap",resource:"foamTex"}],frameSwaps:[["wakeA","wakeB"],["foamA","foamB"]],exports:{disp0:"dispTex0",disp1:"dispTex1",disp2:"dispTex2",disp3:"dispTex3",foam:"foamTex",slope0:"slopeTex0",slope1:"slopeTex1",slope2:"slopeTex2",slope3:"slopeTex3",wake:"wakeTex"},exportParams:["waterModX0","waterModZ0","waterModX1","waterModZ1","waterModX2","waterModZ2","waterModX3","waterModZ3","wakeCamRelX","wakeCamRelZ","wakeOriginModX","wakeOriginModZ","wakeCell","wakeOn"]},"webgl2/medium":{name:"ocean",backend:"webgl2",tier:"medium",version:1,resources:[{name:"h0_0",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"omega_0",kind:"buffer",elementType:"f32",count:4096,width:64},{name:"fftA_0",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"fftB_0",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"fftA2_0",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"fftB2_0",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"dispTex0",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"slopeTex0",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"h0_1",kind:"buffer",elementType:"vec4<f32>",count:16384,width:128},{name:"omega_1",kind:"buffer",elementType:"f32",count:16384,width:128},{name:"fftA_1",kind:"buffer",elementType:"vec4<f32>",count:16384,width:128},{name:"fftB_1",kind:"buffer",elementType:"vec4<f32>",count:16384,width:128},{name:"fftA2_1",kind:"buffer",elementType:"vec4<f32>",count:16384,width:128},{name:"fftB2_1",kind:"buffer",elementType:"vec4<f32>",count:16384,width:128},{name:"dispTex1",kind:"texture",format:"rgba16float",width:128,height:128,mipLevels:8},{name:"slopeTex1",kind:"texture",format:"rgba16float",width:128,height:128,mipLevels:8},{name:"h0_2",kind:"buffer",elementType:"vec4<f32>",count:16384,width:128},{name:"omega_2",kind:"buffer",elementType:"f32",count:16384,width:128},{name:"fftA_2",kind:"buffer",elementType:"vec4<f32>",count:16384,width:128},{name:"fftB_2",kind:"buffer",elementType:"vec4<f32>",count:16384,width:128},{name:"fftA2_2",kind:"buffer",elementType:"vec4<f32>",count:16384,width:128},{name:"fftB2_2",kind:"buffer",elementType:"vec4<f32>",count:16384,width:128},{name:"dispTex2",kind:"texture",format:"rgba16float",width:128,height:128,mipLevels:8},{name:"slopeTex2",kind:"texture",format:"rgba16float",width:128,height:128,mipLevels:8},{name:"h0_3",kind:"buffer",elementType:"vec4<f32>",count:16384,width:128},{name:"omega_3",kind:"buffer",elementType:"f32",count:16384,width:128},{name:"fftA_3",kind:"buffer",elementType:"vec4<f32>",count:16384,width:128},{name:"fftB_3",kind:"buffer",elementType:"vec4<f32>",count:16384,width:128},{name:"fftA2_3",kind:"buffer",elementType:"vec4<f32>",count:16384,width:128},{name:"fftB2_3",kind:"buffer",elementType:"vec4<f32>",count:16384,width:128},{name:"dispTex3",kind:"texture",format:"rgba16float",width:128,height:128,mipLevels:8},{name:"slopeTex3",kind:"texture",format:"rgba16float",width:128,height:128,mipLevels:8},{name:"wakeA",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"wakeB",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"wakeF",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"wakeG",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"wakeC",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"wakeTex",kind:"texture",format:"rgba16float",width:256,height:256,mipLevels:9},{name:"foamA",kind:"buffer",elementType:"f32",count:1048576,width:1024},{name:"foamB",kind:"buffer",elementType:"f32",count:1048576,width:1024},{name:"foamTex",kind:"texture",format:"rgba16float",width:1024,height:1024,mipLevels:11}],passes:[{name:"c0.spectrum",kernel:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",target:{width:64,height:64,elioWidth:64},bindings:[{name:"h0",resource:"h0_0"},{name:"omega",resource:"omega_0"}],uniforms:[{name:"spec",values:{cascade:0,fetchKm:"param:fetch",gamma:"param:gamma",kMax:.12,kMin:.003926990816987242,loopPeriod:"param:loopPeriod",n:64,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:1600,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c0.evolve",kernel:"ocean_evolve",target:{width:64,height:64,elioWidth:64},bindings:[{name:"h0",resource:"h0_0"},{name:"omega",resource:"omega_0"},{name:"outA",resource:"fftA_0"},{name:"outB",resource:"fftB_0"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:64,t:"param:phaseTime",tile:1600}}]},{name:"c0.fft.rows",type:"repeat",count:6,index:"s",swap:[["fftA_0","fftA2_0"],["fftB_0","fftB2_0"]],body:[{name:"A",kernel:"fft_gather_64",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftA_0"},{name:"dst",resource:"fftA2_0"}],uniforms:[{name:"fft",values:{axis:0,n:64,stage:"param:s"}}]},{name:"B",kernel:"fft_gather_64",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftB_0"},{name:"dst",resource:"fftB2_0"}],uniforms:[{name:"fft",values:{axis:0,n:64,stage:"param:s"}}]}]},{name:"c0.fft.cols",type:"repeat",count:6,index:"s",swap:[["fftA_0","fftA2_0"],["fftB_0","fftB2_0"]],body:[{name:"A",kernel:"fft_gather_64",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftA_0"},{name:"dst",resource:"fftA2_0"}],uniforms:[{name:"fft",values:{axis:1,n:64,stage:"param:s"}}]},{name:"B",kernel:"fft_gather_64",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftB_0"},{name:"dst",resource:"fftB2_0"}],uniforms:[{name:"fft",values:{axis:1,n:64,stage:"param:s"}}]}]},{name:"c0.assemble",kernel:"ocean_assemble",target:{width:64,height:64,elioWidth:64},bindings:[{name:"fieldA",resource:"fftA_0"},{name:"fieldB",resource:"fftB_0"},{name:"disp",resource:"dispTex0"},{name:"slope",resource:"slopeTex0"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:64}}]},{name:"c0.dispTex0.mips",type:"mipmap",resource:"dispTex0"},{name:"c0.slopeTex0.mips",type:"mipmap",resource:"slopeTex0"},{name:"c1.spectrum",kernel:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",target:{width:128,height:128,elioWidth:128},bindings:[{name:"h0",resource:"h0_1"},{name:"omega",resource:"omega_1"}],uniforms:[{name:"spec",values:{cascade:1,fetchKm:"param:fetch",gamma:"param:gamma",kMax:.8,kMin:.12,loopPeriod:"param:loopPeriod",n:128,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:250,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c1.evolve",kernel:"ocean_evolve",target:{width:128,height:128,elioWidth:128},bindings:[{name:"h0",resource:"h0_1"},{name:"omega",resource:"omega_1"},{name:"outA",resource:"fftA_1"},{name:"outB",resource:"fftB_1"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:128,t:"param:phaseTime",tile:250}}]},{name:"c1.fft.rows",type:"repeat",count:7,index:"s",swap:[["fftA_1","fftA2_1"],["fftB_1","fftB2_1"]],body:[{name:"A",kernel:"fft_gather_128",target:{width:128,height:128,elioWidth:128},bindings:[{name:"src",resource:"fftA_1"},{name:"dst",resource:"fftA2_1"}],uniforms:[{name:"fft",values:{axis:0,n:128,stage:"param:s"}}]},{name:"B",kernel:"fft_gather_128",target:{width:128,height:128,elioWidth:128},bindings:[{name:"src",resource:"fftB_1"},{name:"dst",resource:"fftB2_1"}],uniforms:[{name:"fft",values:{axis:0,n:128,stage:"param:s"}}]}]},{name:"c1.fft.cols",type:"repeat",count:7,index:"s",swap:[["fftA_1","fftA2_1"],["fftB_1","fftB2_1"]],body:[{name:"A",kernel:"fft_gather_128",target:{width:128,height:128,elioWidth:128},bindings:[{name:"src",resource:"fftA_1"},{name:"dst",resource:"fftA2_1"}],uniforms:[{name:"fft",values:{axis:1,n:128,stage:"param:s"}}]},{name:"B",kernel:"fft_gather_128",target:{width:128,height:128,elioWidth:128},bindings:[{name:"src",resource:"fftB_1"},{name:"dst",resource:"fftB2_1"}],uniforms:[{name:"fft",values:{axis:1,n:128,stage:"param:s"}}]}]},{name:"c1.assemble",kernel:"ocean_assemble",target:{width:128,height:128,elioWidth:128},bindings:[{name:"fieldA",resource:"fftA_1"},{name:"fieldB",resource:"fftB_1"},{name:"disp",resource:"dispTex1"},{name:"slope",resource:"slopeTex1"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:128}}]},{name:"c1.dispTex1.mips",type:"mipmap",resource:"dispTex1"},{name:"c1.slopeTex1.mips",type:"mipmap",resource:"slopeTex1"},{name:"c2.spectrum",kernel:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",target:{width:128,height:128,elioWidth:128},bindings:[{name:"h0",resource:"h0_2"},{name:"omega",resource:"omega_2"}],uniforms:[{name:"spec",values:{cascade:2,fetchKm:"param:fetch",gamma:"param:gamma",kMax:5.4,kMin:.8,loopPeriod:"param:loopPeriod",n:128,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:37,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c2.evolve",kernel:"ocean_evolve",target:{width:128,height:128,elioWidth:128},bindings:[{name:"h0",resource:"h0_2"},{name:"omega",resource:"omega_2"},{name:"outA",resource:"fftA_2"},{name:"outB",resource:"fftB_2"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:128,t:"param:phaseTime",tile:37}}]},{name:"c2.fft.rows",type:"repeat",count:7,index:"s",swap:[["fftA_2","fftA2_2"],["fftB_2","fftB2_2"]],body:[{name:"A",kernel:"fft_gather_128",target:{width:128,height:128,elioWidth:128},bindings:[{name:"src",resource:"fftA_2"},{name:"dst",resource:"fftA2_2"}],uniforms:[{name:"fft",values:{axis:0,n:128,stage:"param:s"}}]},{name:"B",kernel:"fft_gather_128",target:{width:128,height:128,elioWidth:128},bindings:[{name:"src",resource:"fftB_2"},{name:"dst",resource:"fftB2_2"}],uniforms:[{name:"fft",values:{axis:0,n:128,stage:"param:s"}}]}]},{name:"c2.fft.cols",type:"repeat",count:7,index:"s",swap:[["fftA_2","fftA2_2"],["fftB_2","fftB2_2"]],body:[{name:"A",kernel:"fft_gather_128",target:{width:128,height:128,elioWidth:128},bindings:[{name:"src",resource:"fftA_2"},{name:"dst",resource:"fftA2_2"}],uniforms:[{name:"fft",values:{axis:1,n:128,stage:"param:s"}}]},{name:"B",kernel:"fft_gather_128",target:{width:128,height:128,elioWidth:128},bindings:[{name:"src",resource:"fftB_2"},{name:"dst",resource:"fftB2_2"}],uniforms:[{name:"fft",values:{axis:1,n:128,stage:"param:s"}}]}]},{name:"c2.assemble",kernel:"ocean_assemble",target:{width:128,height:128,elioWidth:128},bindings:[{name:"fieldA",resource:"fftA_2"},{name:"fieldB",resource:"fftB_2"},{name:"disp",resource:"dispTex2"},{name:"slope",resource:"slopeTex2"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:128}}]},{name:"c2.dispTex2.mips",type:"mipmap",resource:"dispTex2"},{name:"c2.slopeTex2.mips",type:"mipmap",resource:"slopeTex2"},{name:"c3.spectrum",kernel:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",target:{width:128,height:128,elioWidth:128},bindings:[{name:"h0",resource:"h0_3"},{name:"omega",resource:"omega_3"}],uniforms:[{name:"spec",values:{cascade:3,fetchKm:"param:fetch",gamma:"param:gamma",kMax:37,kMin:5.4,loopPeriod:"param:loopPeriod",n:128,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:5.3,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c3.evolve",kernel:"ocean_evolve",target:{width:128,height:128,elioWidth:128},bindings:[{name:"h0",resource:"h0_3"},{name:"omega",resource:"omega_3"},{name:"outA",resource:"fftA_3"},{name:"outB",resource:"fftB_3"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:128,t:"param:phaseTime",tile:5.3}}]},{name:"c3.fft.rows",type:"repeat",count:7,index:"s",swap:[["fftA_3","fftA2_3"],["fftB_3","fftB2_3"]],body:[{name:"A",kernel:"fft_gather_128",target:{width:128,height:128,elioWidth:128},bindings:[{name:"src",resource:"fftA_3"},{name:"dst",resource:"fftA2_3"}],uniforms:[{name:"fft",values:{axis:0,n:128,stage:"param:s"}}]},{name:"B",kernel:"fft_gather_128",target:{width:128,height:128,elioWidth:128},bindings:[{name:"src",resource:"fftB_3"},{name:"dst",resource:"fftB2_3"}],uniforms:[{name:"fft",values:{axis:0,n:128,stage:"param:s"}}]}]},{name:"c3.fft.cols",type:"repeat",count:7,index:"s",swap:[["fftA_3","fftA2_3"],["fftB_3","fftB2_3"]],body:[{name:"A",kernel:"fft_gather_128",target:{width:128,height:128,elioWidth:128},bindings:[{name:"src",resource:"fftA_3"},{name:"dst",resource:"fftA2_3"}],uniforms:[{name:"fft",values:{axis:1,n:128,stage:"param:s"}}]},{name:"B",kernel:"fft_gather_128",target:{width:128,height:128,elioWidth:128},bindings:[{name:"src",resource:"fftB_3"},{name:"dst",resource:"fftB2_3"}],uniforms:[{name:"fft",values:{axis:1,n:128,stage:"param:s"}}]}]},{name:"c3.assemble",kernel:"ocean_assemble",target:{width:128,height:128,elioWidth:128},bindings:[{name:"fieldA",resource:"fftA_3"},{name:"fieldB",resource:"fftB_3"},{name:"disp",resource:"dispTex3"},{name:"slope",resource:"slopeTex3"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:128}}]},{name:"c3.dispTex3.mips",type:"mipmap",resource:"dispTex3"},{name:"c3.slopeTex3.mips",type:"mipmap",resource:"slopeTex3"},{name:"wake.source",kernel:"wake_source",when:"param:wakeEnabled",target:{width:256,height:256,elioWidth:256},bindings:[{name:"wakeIn",resource:"wakeA"},{name:"fftOut",resource:"wakeF"},{name:"cleanOut",resource:"wakeC"}],uniforms:[{name:"wake",values:{bowFoam:"param:wakeBowFoam",bowGain:"param:wakeBowGain",cell:"param:wakeCell",depth:"param:wakeDepth",diffuse:"param:wakeDiffuse",dt:"param:wakeDt",foamSpeed:5,fullSpeed:3,heading:"param:hullHeading",hullBeam:"param:hullBeam",hullLen:"param:hullLength",hullU:"param:wakeHullU",hullV:"param:wakeHullV",jitter:"param:wakeJitter",originX:"param:wakeOriginX",originZ:"param:wakeOriginZ",reset:"param:wakeReset",shiftX:"param:wakeShiftX",shiftZ:"param:wakeShiftZ",speed:"param:hullSpeed",srcBeam:"param:wakeSrcBeam",srcLength:"param:wakeSrcLength",tau:"param:wakeFoamTau",width:256}}]},{name:"wake.fft1.rows",type:"repeat",when:"param:wakeEnabled",count:8,index:"s",swap:[["wakeF","wakeG"]],body:[{name:"stage",kernel:"fft_gather_256",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"wakeF"},{name:"dst",resource:"wakeG"}],uniforms:[{name:"fft",values:{axis:0,n:256,stage:"param:s"}}]}]},{name:"wake.fft1.cols",type:"repeat",when:"param:wakeEnabled",count:8,index:"s",swap:[["wakeF","wakeG"]],body:[{name:"stage",kernel:"fft_gather_256",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"wakeF"},{name:"dst",resource:"wakeG"}],uniforms:[{name:"fft",values:{axis:1,n:256,stage:"param:s"}}]}]},{name:"wake.symbol",kernel:"wake_symbol",when:"param:wakeEnabled",target:{width:256,height:256,elioWidth:256},bindings:[{name:"fftIn",resource:"wakeF"},{name:"fftOut",resource:"wakeG"}],uniforms:[{name:"sym",values:{n:256,window:"param:wakeLength"}}]},{name:"wake.fft2.rows",type:"repeat",when:"param:wakeEnabled",count:8,index:"s",swap:[["wakeG","wakeF"]],body:[{name:"stage",kernel:"fft_gather_256",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"wakeG"},{name:"dst",resource:"wakeF"}],uniforms:[{name:"fft",values:{axis:0,n:256,stage:"param:s"}}]}]},{name:"wake.fft2.cols",type:"repeat",when:"param:wakeEnabled",count:8,index:"s",swap:[["wakeG","wakeF"]],body:[{name:"stage",kernel:"fft_gather_256",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"wakeG"},{name:"dst",resource:"wakeF"}],uniforms:[{name:"fft",values:{axis:1,n:256,stage:"param:s"}}]}]},{name:"wake.step",kernel:"wake_step",when:"param:wakeEnabled",target:{width:256,height:256,elioWidth:256},bindings:[{name:"qIn",resource:"wakeG"},{name:"cleanIn",resource:"wakeC"},{name:"stateOut",resource:"wakeB"},{name:"wakeTex",resource:"wakeTex"}],uniforms:[{name:"wstep",values:{alpha:"param:wakeAlpha",cell:"param:wakeCell",dt:"param:wakeDt",gravity:"param:wakeGEff",originX:"param:wakeOriginX",originZ:"param:wakeOriginZ",sponge:24,spongeRate:.8,width:256}}]},{name:"wake.wakeTex.mips",type:"mipmap",when:"param:wakeEnabled",resource:"wakeTex"},{name:"foam",kernel:"ocean_foam",target:{width:1024,height:1024,elioWidth:1024},bindings:[{name:"foamIn",resource:"foamA"},{name:"fieldB0",resource:"fftB_0"},{name:"fieldB1",resource:"fftB_1"},{name:"fieldB2",resource:"fftB_2"},{name:"fieldB3",resource:"fftB_3"},{name:"foamOut",resource:"foamB"},{name:"foamTex",resource:"foamTex"},{name:"wakeIn",resource:"wakeB"}],uniforms:[{name:"foam",values:{cascades:4,cell:.5,dt:"param:dt",gain:"param:foamGain",lambda:"param:lambda",n0:64,n1:128,n2:128,n3:128,omX0:"param:foamOriginMod0X",omX1:"param:foamOriginMod1X",omX2:"param:foamOriginMod2X",omX3:"param:foamOriginMod3X",omZ0:"param:foamOriginMod0Z",omZ1:"param:foamOriginMod1Z",omZ2:"param:foamOriginMod2Z",omZ3:"param:foamOriginMod3Z",originX:"param:foamOriginX",originZ:"param:foamOriginZ",shiftX:"param:foamShiftX",shiftZ:"param:foamShiftZ",size0:1600,size1:250,size2:37,size3:5.3,tau:"param:foamTau",threshold:"param:foamThreshold",wakeCell:"param:wakeCell",wakeGain:"param:wakeFoamGainEff",wakeOriginX:"param:wakeOriginModX",wakeOriginZ:"param:wakeOriginModZ",wakeRelX:"param:wakeRelX",wakeRelZ:"param:wakeRelZ",wakeWidth:256,width:1024}}]},{name:"foamTex.mips",type:"mipmap",resource:"foamTex"}],frameSwaps:[["wakeA","wakeB"],["foamA","foamB"]],exports:{disp0:"dispTex0",disp1:"dispTex1",disp2:"dispTex2",disp3:"dispTex3",foam:"foamTex",slope0:"slopeTex0",slope1:"slopeTex1",slope2:"slopeTex2",slope3:"slopeTex3",wake:"wakeTex"},paramsFrom:"webgl2/high"},"webgl2/low":{name:"ocean",backend:"webgl2",tier:"low",version:1,resources:[{name:"h0_0",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"omega_0",kind:"buffer",elementType:"f32",count:4096,width:64},{name:"fftA_0",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"fftB_0",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"fftA2_0",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"fftB2_0",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"dispTex0",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"slopeTex0",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"h0_1",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"omega_1",kind:"buffer",elementType:"f32",count:4096,width:64},{name:"fftA_1",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"fftB_1",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"fftA2_1",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"fftB2_1",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"dispTex1",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"slopeTex1",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"h0_2",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"omega_2",kind:"buffer",elementType:"f32",count:4096,width:64},{name:"fftA_2",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"fftB_2",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"fftA2_2",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"fftB2_2",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"dispTex2",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"slopeTex2",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"wakeA",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"wakeB",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"wakeF",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"wakeG",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"wakeC",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"wakeTex",kind:"texture",format:"rgba16float",width:256,height:256,mipLevels:9},{name:"foamA",kind:"buffer",elementType:"f32",count:1048576,width:1024},{name:"foamB",kind:"buffer",elementType:"f32",count:1048576,width:1024},{name:"foamTex",kind:"texture",format:"rgba16float",width:1024,height:1024,mipLevels:11}],passes:[{name:"c0.spectrum",kernel:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",target:{width:64,height:64,elioWidth:64},bindings:[{name:"h0",resource:"h0_0"},{name:"omega",resource:"omega_0"}],uniforms:[{name:"spec",values:{cascade:0,fetchKm:"param:fetch",gamma:"param:gamma",kMax:.12,kMin:.003926990816987242,loopPeriod:"param:loopPeriod",n:64,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:1600,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c0.evolve",kernel:"ocean_evolve",target:{width:64,height:64,elioWidth:64},bindings:[{name:"h0",resource:"h0_0"},{name:"omega",resource:"omega_0"},{name:"outA",resource:"fftA_0"},{name:"outB",resource:"fftB_0"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:64,t:"param:phaseTime",tile:1600}}]},{name:"c0.fft.rows",type:"repeat",count:6,index:"s",swap:[["fftA_0","fftA2_0"],["fftB_0","fftB2_0"]],body:[{name:"A",kernel:"fft_gather_64",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftA_0"},{name:"dst",resource:"fftA2_0"}],uniforms:[{name:"fft",values:{axis:0,n:64,stage:"param:s"}}]},{name:"B",kernel:"fft_gather_64",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftB_0"},{name:"dst",resource:"fftB2_0"}],uniforms:[{name:"fft",values:{axis:0,n:64,stage:"param:s"}}]}]},{name:"c0.fft.cols",type:"repeat",count:6,index:"s",swap:[["fftA_0","fftA2_0"],["fftB_0","fftB2_0"]],body:[{name:"A",kernel:"fft_gather_64",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftA_0"},{name:"dst",resource:"fftA2_0"}],uniforms:[{name:"fft",values:{axis:1,n:64,stage:"param:s"}}]},{name:"B",kernel:"fft_gather_64",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftB_0"},{name:"dst",resource:"fftB2_0"}],uniforms:[{name:"fft",values:{axis:1,n:64,stage:"param:s"}}]}]},{name:"c0.assemble",kernel:"ocean_assemble",target:{width:64,height:64,elioWidth:64},bindings:[{name:"fieldA",resource:"fftA_0"},{name:"fieldB",resource:"fftB_0"},{name:"disp",resource:"dispTex0"},{name:"slope",resource:"slopeTex0"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:64}}]},{name:"c0.dispTex0.mips",type:"mipmap",resource:"dispTex0"},{name:"c0.slopeTex0.mips",type:"mipmap",resource:"slopeTex0"},{name:"c1.spectrum",kernel:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",target:{width:64,height:64,elioWidth:64},bindings:[{name:"h0",resource:"h0_1"},{name:"omega",resource:"omega_1"}],uniforms:[{name:"spec",values:{cascade:1,fetchKm:"param:fetch",gamma:"param:gamma",kMax:.8,kMin:.12,loopPeriod:"param:loopPeriod",n:64,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:250,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c1.evolve",kernel:"ocean_evolve",target:{width:64,height:64,elioWidth:64},bindings:[{name:"h0",resource:"h0_1"},{name:"omega",resource:"omega_1"},{name:"outA",resource:"fftA_1"},{name:"outB",resource:"fftB_1"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:64,t:"param:phaseTime",tile:250}}]},{name:"c1.fft.rows",type:"repeat",count:6,index:"s",swap:[["fftA_1","fftA2_1"],["fftB_1","fftB2_1"]],body:[{name:"A",kernel:"fft_gather_64",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftA_1"},{name:"dst",resource:"fftA2_1"}],uniforms:[{name:"fft",values:{axis:0,n:64,stage:"param:s"}}]},{name:"B",kernel:"fft_gather_64",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftB_1"},{name:"dst",resource:"fftB2_1"}],uniforms:[{name:"fft",values:{axis:0,n:64,stage:"param:s"}}]}]},{name:"c1.fft.cols",type:"repeat",count:6,index:"s",swap:[["fftA_1","fftA2_1"],["fftB_1","fftB2_1"]],body:[{name:"A",kernel:"fft_gather_64",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftA_1"},{name:"dst",resource:"fftA2_1"}],uniforms:[{name:"fft",values:{axis:1,n:64,stage:"param:s"}}]},{name:"B",kernel:"fft_gather_64",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftB_1"},{name:"dst",resource:"fftB2_1"}],uniforms:[{name:"fft",values:{axis:1,n:64,stage:"param:s"}}]}]},{name:"c1.assemble",kernel:"ocean_assemble",target:{width:64,height:64,elioWidth:64},bindings:[{name:"fieldA",resource:"fftA_1"},{name:"fieldB",resource:"fftB_1"},{name:"disp",resource:"dispTex1"},{name:"slope",resource:"slopeTex1"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:64}}]},{name:"c1.dispTex1.mips",type:"mipmap",resource:"dispTex1"},{name:"c1.slopeTex1.mips",type:"mipmap",resource:"slopeTex1"},{name:"c2.spectrum",kernel:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",target:{width:64,height:64,elioWidth:64},bindings:[{name:"h0",resource:"h0_2"},{name:"omega",resource:"omega_2"}],uniforms:[{name:"spec",values:{cascade:2,fetchKm:"param:fetch",gamma:"param:gamma",kMax:5.4,kMin:.8,loopPeriod:"param:loopPeriod",n:64,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:37,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c2.evolve",kernel:"ocean_evolve",target:{width:64,height:64,elioWidth:64},bindings:[{name:"h0",resource:"h0_2"},{name:"omega",resource:"omega_2"},{name:"outA",resource:"fftA_2"},{name:"outB",resource:"fftB_2"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:64,t:"param:phaseTime",tile:37}}]},{name:"c2.fft.rows",type:"repeat",count:6,index:"s",swap:[["fftA_2","fftA2_2"],["fftB_2","fftB2_2"]],body:[{name:"A",kernel:"fft_gather_64",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftA_2"},{name:"dst",resource:"fftA2_2"}],uniforms:[{name:"fft",values:{axis:0,n:64,stage:"param:s"}}]},{name:"B",kernel:"fft_gather_64",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftB_2"},{name:"dst",resource:"fftB2_2"}],uniforms:[{name:"fft",values:{axis:0,n:64,stage:"param:s"}}]}]},{name:"c2.fft.cols",type:"repeat",count:6,index:"s",swap:[["fftA_2","fftA2_2"],["fftB_2","fftB2_2"]],body:[{name:"A",kernel:"fft_gather_64",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftA_2"},{name:"dst",resource:"fftA2_2"}],uniforms:[{name:"fft",values:{axis:1,n:64,stage:"param:s"}}]},{name:"B",kernel:"fft_gather_64",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftB_2"},{name:"dst",resource:"fftB2_2"}],uniforms:[{name:"fft",values:{axis:1,n:64,stage:"param:s"}}]}]},{name:"c2.assemble",kernel:"ocean_assemble",target:{width:64,height:64,elioWidth:64},bindings:[{name:"fieldA",resource:"fftA_2"},{name:"fieldB",resource:"fftB_2"},{name:"disp",resource:"dispTex2"},{name:"slope",resource:"slopeTex2"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:64}}]},{name:"c2.dispTex2.mips",type:"mipmap",resource:"dispTex2"},{name:"c2.slopeTex2.mips",type:"mipmap",resource:"slopeTex2"},{name:"wake.source",kernel:"wake_source",when:"param:wakeEnabled",target:{width:256,height:256,elioWidth:256},bindings:[{name:"wakeIn",resource:"wakeA"},{name:"fftOut",resource:"wakeF"},{name:"cleanOut",resource:"wakeC"}],uniforms:[{name:"wake",values:{bowFoam:"param:wakeBowFoam",bowGain:"param:wakeBowGain",cell:"param:wakeCell",depth:"param:wakeDepth",diffuse:"param:wakeDiffuse",dt:"param:wakeDt",foamSpeed:5,fullSpeed:3,heading:"param:hullHeading",hullBeam:"param:hullBeam",hullLen:"param:hullLength",hullU:"param:wakeHullU",hullV:"param:wakeHullV",jitter:"param:wakeJitter",originX:"param:wakeOriginX",originZ:"param:wakeOriginZ",reset:"param:wakeReset",shiftX:"param:wakeShiftX",shiftZ:"param:wakeShiftZ",speed:"param:hullSpeed",srcBeam:"param:wakeSrcBeam",srcLength:"param:wakeSrcLength",tau:"param:wakeFoamTau",width:256}}]},{name:"wake.fft1.rows",type:"repeat",when:"param:wakeEnabled",count:8,index:"s",swap:[["wakeF","wakeG"]],body:[{name:"stage",kernel:"fft_gather_256",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"wakeF"},{name:"dst",resource:"wakeG"}],uniforms:[{name:"fft",values:{axis:0,n:256,stage:"param:s"}}]}]},{name:"wake.fft1.cols",type:"repeat",when:"param:wakeEnabled",count:8,index:"s",swap:[["wakeF","wakeG"]],body:[{name:"stage",kernel:"fft_gather_256",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"wakeF"},{name:"dst",resource:"wakeG"}],uniforms:[{name:"fft",values:{axis:1,n:256,stage:"param:s"}}]}]},{name:"wake.symbol",kernel:"wake_symbol",when:"param:wakeEnabled",target:{width:256,height:256,elioWidth:256},bindings:[{name:"fftIn",resource:"wakeF"},{name:"fftOut",resource:"wakeG"}],uniforms:[{name:"sym",values:{n:256,window:"param:wakeLength"}}]},{name:"wake.fft2.rows",type:"repeat",when:"param:wakeEnabled",count:8,index:"s",swap:[["wakeG","wakeF"]],body:[{name:"stage",kernel:"fft_gather_256",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"wakeG"},{name:"dst",resource:"wakeF"}],uniforms:[{name:"fft",values:{axis:0,n:256,stage:"param:s"}}]}]},{name:"wake.fft2.cols",type:"repeat",when:"param:wakeEnabled",count:8,index:"s",swap:[["wakeG","wakeF"]],body:[{name:"stage",kernel:"fft_gather_256",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"wakeG"},{name:"dst",resource:"wakeF"}],uniforms:[{name:"fft",values:{axis:1,n:256,stage:"param:s"}}]}]},{name:"wake.step",kernel:"wake_step",when:"param:wakeEnabled",target:{width:256,height:256,elioWidth:256},bindings:[{name:"qIn",resource:"wakeG"},{name:"cleanIn",resource:"wakeC"},{name:"stateOut",resource:"wakeB"},{name:"wakeTex",resource:"wakeTex"}],uniforms:[{name:"wstep",values:{alpha:"param:wakeAlpha",cell:"param:wakeCell",dt:"param:wakeDt",gravity:"param:wakeGEff",originX:"param:wakeOriginX",originZ:"param:wakeOriginZ",sponge:24,spongeRate:.8,width:256}}]},{name:"wake.wakeTex.mips",type:"mipmap",when:"param:wakeEnabled",resource:"wakeTex"},{name:"foam",kernel:"ocean_foam",target:{width:1024,height:1024,elioWidth:1024},bindings:[{name:"foamIn",resource:"foamA"},{name:"fieldB0",resource:"fftB_0"},{name:"fieldB1",resource:"fftB_1"},{name:"fieldB2",resource:"fftB_2"},{name:"fieldB3",resource:"fftB_2"},{name:"foamOut",resource:"foamB"},{name:"foamTex",resource:"foamTex"},{name:"wakeIn",resource:"wakeB"}],uniforms:[{name:"foam",values:{cascades:3,cell:.5,dt:"param:dt",gain:"param:foamGain",lambda:"param:lambda",n0:64,n1:64,n2:64,n3:64,omX0:"param:foamOriginMod0X",omX1:"param:foamOriginMod1X",omX2:"param:foamOriginMod2X",omX3:"param:foamOriginMod2X",omZ0:"param:foamOriginMod0Z",omZ1:"param:foamOriginMod1Z",omZ2:"param:foamOriginMod2Z",omZ3:"param:foamOriginMod2Z",originX:"param:foamOriginX",originZ:"param:foamOriginZ",shiftX:"param:foamShiftX",shiftZ:"param:foamShiftZ",size0:1600,size1:250,size2:37,size3:37,tau:"param:foamTau",threshold:"param:foamThreshold",wakeCell:"param:wakeCell",wakeGain:"param:wakeFoamGainEff",wakeOriginX:"param:wakeOriginModX",wakeOriginZ:"param:wakeOriginModZ",wakeRelX:"param:wakeRelX",wakeRelZ:"param:wakeRelZ",wakeWidth:256,width:1024}}]},{name:"foamTex.mips",type:"mipmap",resource:"foamTex"}],frameSwaps:[["wakeA","wakeB"],["foamA","foamB"]],exports:{disp0:"dispTex0",disp1:"dispTex1",disp2:"dispTex2",foam:"foamTex",slope0:"slopeTex0",slope1:"slopeTex1",slope2:"slopeTex2",wake:"wakeTex"},paramsFrom:"webgl2/high"}}})})(),(function(){function ne(o){return o.replace(/_([a-z0-9])/g,function(b,O){return O.toUpperCase()})}function F(o){return typeof o=="boolean"?o?1:0:Number(o)}function D(o,b,O){if(typeof o=="string"){if(o.indexOf("param:")!==0)throw new Error("elio-graph: "+O+": bad value "+o);const P=o.slice(6);if(!(P in b))throw new Error("elio-graph: parameter "+P+" was not supplied");return F(b[P])}return F(o)}function G(o,b,O,P){const _=new ArrayBuffer(Math.max(16,Math.ceil(o.size/16)*16)),y=new DataView(_),h={};for(const Z of o.fields){let T=b[Z.name];if(T===void 0){if(!/^_pad/.test(Z.name))throw new Error("elio-graph: "+P+": uniform "+o.struct+" lacks a value for "+Z.name);T=0}h[Z.name]=!0;const te=D(T,O,P);if(Z.type==="u32")y.setUint32(Z.offset,te>>>0,!0);else if(Z.type==="i32")y.setInt32(Z.offset,te|0,!0);else if(Z.type==="f32")y.setFloat32(Z.offset,te,!0);else throw new Error("elio-graph: "+P+": unsupported uniform type "+Z.type)}for(const Z of Object.keys(b))if(!h[Z])throw new Error("elio-graph: "+P+": uniform "+o.struct+" has no field "+Z);return _}function c(o){for(const b of Object.keys(o))if(typeof o[b]=="string")return!1;return!0}const a={x:1,z:1,heading:1,speed:1},me={time:1,dt:1,cameraX:1,cameraY:1,cameraZ:1},re={add:function(o,b){return o+b},sub:function(o,b){return o-b},mul:function(o,b){return o*b},div:function(o,b){return b===0?0:o/b},mod:function(o,b){return b===0?0:o-Math.floor(o/b)*b},floor:function(o){return Math.floor(o)},sin:function(o){return Math.sin(o)},cos:function(o){return Math.cos(o)},min:function(o,b){return Math.min(o,b)},max:function(o,b){return Math.max(o,b)}};function le(o,b){return/\[\]$/.test(o)?Float32Array.from(b||[]):(b=Number(b),Number.isFinite(b)?o==="bool"?b?1:0:o==="u32"?Math.max(0,Math.floor(b))>>>0:o==="i32"?Math.trunc(b)|0:b:0)}function we(o){const b={},O={},P={},_={},y={};let h=!0,Z=null,T={};const te={},ye={},Te=function(v){return v.tween>0?Z===null?v.tween:Z:0};for(const v of o){const L=v.source||{kind:"value",value:0};L.kind==="value"&&(y[v.name]=le(v.type,/\[\]$/.test(v.type)?L.value:L.value||0))}const E=function(v,L){const S=L||b;if(typeof v=="number")return v;if(typeof v=="string"&&v.indexOf("param:")===0){const K=v.slice(6);return K in S?S[K]:0}if(v&&typeof v=="object"&&typeof v.op=="string"){const K=re[v.op];if(!K)throw new Error("elio-graph: unknown op "+v.op);return K(E(v.args[0],S),v.args.length>1?E(v.args[1],S):0)}throw new Error("elio-graph: bad argument "+v)},B={current:b,changed:{},setNodes:function(v){T=v||{}},setTween:function(v){Z=Number.isFinite(v)&&v>=0?v:null},set:function(v,L){const S=o.find(function(f){return f.name===v});if(!S||S.source&&S.source.kind!=="value")throw new Error("elio-graph: "+v+" is not a value parameter");const K=le(S.type,L);if(K!==y[v]){if(typeof K!="number"){y[v]=K;return}y[v]=K,Te(S)>0&&!h?P[v]={from:b[v],start:null}:delete P[v]}},snapshot:function(){return Object.assign({},b)},derive:function(v,L){const S=Object.assign({},b);for(const K of o){const f=K.source||{kind:"value",value:0};if(f.kind==="frame"&&f.name==="dt")S[K.name]=le(K.type,v);else if(f.kind==="expr")S[K.name]=le(K.type,re[f.op](E(f.args[0],S),f.args.length>1?E(f.args[1],S):0));else if(f.kind==="delta"){const p=String(f.of).replace(/^param:/,"");S[K.name]=le(K.type,L?(S[p]||0)-(L[p]||0):0)}}return S},evaluate:function(v){const L=Number(v.time)||0,S=Number(v.dt)||0,K=Number.isFinite(v.accumulateDt)?Number(v.accumulateDt):S,f={},p=function(r){if(r in f)return f[r];const x=typeof v.nodePose=="function"?v.nodePose(r):null;if(!x)return f[r]=null;const I=te[r];let R=Number(x.speed);return Number.isFinite(R)||(R=I&&S>0?Math.hypot(x.x-I.x,x.z-I.z)/S:0),f[r]={x:Number(x.x)||0,z:Number(x.z)||0,heading:Number(x.heading)||0,speed:R}};for(const r of o){const x=r.source||{kind:"value",value:0};let I=0;if(x.kind==="value"){I=y[r.name];const R=P[r.name];if(R){R.start===null&&(R.start=L);const ue=Math.min(1,Math.max(0,(L-R.start)/Te(r)));ue>=1?delete P[r.name]:I=R.from+(I-R.from)*ue}}else if(x.kind==="frame"){if(!me[x.name])throw new Error("elio-graph: unknown frame source "+x.name);I=x.name==="time"?L:x.name==="dt"?S:Number(v[x.name])||0}else if(x.kind==="expr"){const R=re[x.op];if(!R)throw new Error("elio-graph: unknown op "+x.op);I=R(E(x.args[0]),x.args.length>1?E(x.args[1]):0)}else if(x.kind==="node"){if(!a[x.field])throw new Error("elio-graph: unknown node field "+x.field);const R=p(x.node in T?T[x.node]:x.node);I=R?R[x.field]:x.field==="speed"?0:ye[r.name]||0,ye[r.name]=I}else if(x.kind==="delta"){const R=String(x.of).replace(/^param:/,"");I=h?0:(b[R]||0)-(O[R]||0)}else if(x.kind==="accumulate")_[r.name]=(_[r.name]||0)+E(x.of)*K,I=_[r.name];else throw new Error("elio-graph: unknown parameter source "+x.kind);b[r.name]=/\[\]$/.test(r.type)?y[r.name]:le(r.type,I)}for(const r of Object.keys(f))f[r]&&(te[r]={x:f[r].x,z:f[r].z});for(const r of o)B.changed[r.name]=h||b[r.name]!==O[r.name];for(const r of o)O[r.name]=b[r.name];h=!1}};return B}function t(o,b,O){if(!o||o==="always")return!0;if(o.indexOf("param:")===0)return!!b[o.slice(6)];if(o.indexOf("changed:")===0)return o.slice(8).split(",").some(function(P){return!!O[P.trim()]});throw new Error("elio-graph: bad when "+o)}function m(o,b){const O=Object.assign({},o);if(o.paramsFrom){const _=b&&b.graphs&&b.graphs[o.paramsFrom];if(!_||_.backend!==o.backend||_.paramsFrom)throw new Error("elio-graph: paramsFrom "+o.paramsFrom+" does not name a graph of "+o.backend+" that owns its parameters");O.params=_.params,O.exportParams=_.exportParams}delete O.paramsFrom,O.params=O.params||[],O.exportParams=O.exportParams||[],O.frameSwaps=O.frameSwaps||[],O.exports=O.exports||{};const P=function(_){const y=Object.assign({},_);if(y.type=_.type||"kernel",y.when=_.when||"always",y.type==="kernel"){const h=b&&b.kernels&&b.kernels[ne(_.kernel)];!y.entry&&h&&(y.entry=h.entry),_.bindings&&(y.bindings=_.bindings.map(function(Z){return Object.assign({level:0},Z)})),y.uniforms||(y.uniforms=[])}return y.type==="repeat"&&(y.body=(_.body||[]).map(P)),y};return O.passes=(o.passes||[]).map(P),O}function g(o,b){const O=b+(o.offset||0);return o.shr?Math.max(o.shr>>O,1):O}function U(o){const b={},O={},P={};for(const T of o.resources)b[T.name]=T;for(const T of o.params)P[T.name]=!0;const _=function(T){return T in O?O[T]:T},y=function(T,te,ye,Te){const E=[];for(const B of T){if(B.type!=="repeat"){E.push(h(B,te,ye,Te));continue}const v=ye+B.name;if(!(B.count>=1))throw new Error("elio-graph: repeat "+v+": count "+B.count+", want at least 1");if(!B.index||P[B.index])throw new Error("elio-graph: repeat "+v+": index "+B.index+" is empty or names a declared param");if(!B.body||!B.body.length)throw new Error("elio-graph: repeat "+v+": body is empty");if(B.index in te)throw new Error("elio-graph: repeat "+v+": index "+B.index+" is already in use by an outer repeat");for(const S of B.swap||[])if(!b[S[0]]||!b[S[1]])throw new Error("elio-graph: repeat "+v+": swap names an unknown resource");const L=B.when&&B.when!=="always"?B.when:Te;for(let S=0;S<B.count;S++){const K=Object.assign({},te,{[B.index]:S});for(const f of y(B.body,K,v+"["+S+"].",L))E.push(f);for(const f of B.swap||[]){const p=_(f[0]),r=_(f[1]);O[f[0]]=r,O[f[1]]=p}}}return E},h=function(T,te,ye,Te){const E=Object.assign({},T,{name:ye+T.name});return Te&&(!T.when||T.when==="always")&&(E.when=Te),T.resource&&(E.resource=_(T.resource)),T.from&&(E.from=_(T.from)),T.to&&(E.to=_(T.to)),T.bindings&&(E.bindings=T.bindings.map(function(B){const v=Object.assign({},B,{resource:_(B.resource)});if(B.levelAt){if(!(B.levelAt.index in te))throw new Error("elio-graph: pass "+E.name+": index "+B.levelAt.index+" is not in scope");v.level=g(B.levelAt,te[B.levelAt.index]),delete v.levelAt}return v})),T.uniforms&&(E.uniforms=T.uniforms.map(function(B){const v={};for(const L of Object.keys(B.values)){let S=B.values[L];if(S&&typeof S=="object"){if(!(S.index in te))throw new Error("elio-graph: pass "+E.name+": index "+S.index+" is not in scope");S=g(S,te[S.index])}else typeof S=="string"&&S.indexOf("param:")===0&&S.slice(6)in te&&(S=te[S.slice(6)]);v[L]=S}return Object.assign({},B,{values:v})})),E},Z=y(o.passes,{},"","");for(const T of Object.keys(O))if(O[T]!==T)throw new Error("elio-graph: swaps are not restored: "+T+" still refers to "+O[T]+" at the end of the passes");return Z}function de(o,b,O){if(!o||o.version!==1)throw new Error("elio-graph: unsupported graph version");if(o.backend!==O)throw new Error("elio-graph: graph is for "+o.backend+", not "+O);const P={};for(const h of o.resources)P[h.name]=h;const _={};for(const h of o.params)_[h.name]=!0;const y=function(h){if(typeof h=="string"&&h.indexOf("param:")===0&&!_[h.slice(6)])throw new Error("elio-graph: undeclared parameter "+h.slice(6))};for(const h of U(o)){const Z=h.when||"always";if(Z.indexOf("param:")===0)y(Z);else if(Z.indexOf("changed:")===0){for(const T of Z.slice(8).split(","))if(!_[T.trim()])throw new Error("elio-graph: undeclared parameter "+T.trim())}else if(Z!=="always")throw new Error("elio-graph: pass "+h.name+": bad when "+Z);if(h.type==="mipmap"){if(!P[h.resource])throw new Error("elio-graph: pass "+h.name+": unknown resource "+h.resource);continue}if(h.type==="copy"){if(O!=="webgl2")throw new Error("elio-graph: pass "+h.name+": copy passes exist only on webgl2");for(const T of[h.from,h.to])if(!P[T]||P[T].kind!=="buffer")throw new Error("elio-graph: pass "+h.name+": unknown resource "+T+" (a copy joins two buffers)");continue}if(!b.kernels[ne(h.kernel)])throw new Error("elio-graph: pass "+h.name+": the module has no kernel "+h.kernel);for(const T of h.bindings)if(!P[T.resource])throw new Error("elio-graph: pass "+h.name+": unknown resource "+T.resource);for(const T of h.uniforms||[])for(const te of Object.keys(T.values))y(T.values[te])}for(const h of o.exportParams||[])if(!_[h])throw new Error("elio-graph: exportParams names undeclared parameter "+h);for(const h of o.resources)if(h.feed&&!_[h.feed])throw new Error("elio-graph: resource "+h.name+" is fed by undeclared parameter "+h.feed);for(const h of o.frameSwaps||[])for(const Z of h)if(!P[Z])throw new Error("elio-graph: frameSwaps names unknown resource "+Z);for(const h of Object.keys(o.exports||{})){const Z=P[o.exports[h]];if(!Z||Z.kind!=="texture"&&O!=="webgpu")throw new Error("elio-graph: export "+h+" must name a texture resource")}}function ke(o){const b=o.backend,O=o.module,P=o.host||{},_={graph:null,backend:b,ready:!1,failed:null,disposed:!1,buildCount:0,run:L,nodePose:te,gpuTiming:ye,setValue:v,setNodes:Te,setTween:E,value:B,hasParam:T,params:null,exportParams:S,export:K,dispose:p,fail:f};let y=null,h=null;const Z=function(r){if(!_.failed){if(_.failed=r&&r.fallback||"error",_.error=r,y){try{y.dispose()}catch{}y=null}_.ready=!1,o.onFail&&o.onFail(_.failed,r),!(r&&r.fallback)&&typeof console<"u"&&console.warn&&console.warn("elio-graph: graph disabled: "+(r&&r.message?r.message:String(r)))}};try{let r=o.graph||O.graphs&&O.graphs[b+"/"+(o.tier||"high")];if(!r)throw Object.assign(new Error("elio-graph: no graph for "+b+"/"+o.tier),{fallback:"no-graph"});r=m(r,O),de(r,O,b);const x=window.__gosx_elio_graph.executors[b];if(!x)throw Object.assign(new Error("elio-graph: no executor for "+b),{fallback:"no-executor"});_.graph=r,h=_.params=we(r.params),y=x({steps:U(r).map(function(I){return{pass:I}}),graph:r,module:O,host:P,pack:G,isStatic:c,kernelKey:ne,holds:t,label:o.label||r.name}),_.buildCount=1,_.ready=y.isReady()}catch(r){Z(r)}function T(r){return!!(_.graph&&_.graph.params.some(function(x){return x.name===r}))}function te(r){return P.nodePose?P.nodePose(r):null}function ye(){return y&&y.gpuTiming?y.gpuTiming():null}function Te(r){h&&h.setNodes(r)}function E(r){h&&h.setTween(r)}function B(r){const x=y&&y.held?y.held(r):void 0;return x!==void 0?x:h&&r in h.current?h.current[r]:0}function v(r,x){if(!(!h||_.failed))try{h.set(r,x)}catch(I){Z(I)}}function L(r){if(!y||_.failed||_.disposed)return!1;try{if(_.ready=y.isReady(),!_.ready)return!1;const x=r&&!r.nodePose&&P.nodePose?Object.assign({},r,{nodePose:P.nodePose}):r||{};return h.evaluate(x),y.run(h.current,x,h.changed,h)!==!1}catch(x){return Z(x),!1}}function S(){const r={};if(h&&_.graph)for(const x of _.graph.exportParams||[])r[x]=B(x);return r}function K(r){return y&&!_.failed?y.export(r):null}function f(r){Z({message:r,fallback:r})}function p(){if(!_.disposed&&(_.disposed=!0,_.ready=!1,y)){try{y.dispose()}catch{}y=null}}return _}window.__gosx_elio_graph={create:ke,expand:U,normalize:m,pack:G,kernelKey:ne,check:de,holds:t,createParams:we,executors:{}}})(),(function(){const ne=`#version 300 es
void main() { vec2 p = vec2(float((gl_VertexID << 1) & 2), float(gl_VertexID & 2)); gl_Position = vec4(p * 2. - 1., 0., 1.); }`;function F(c){return{draw:c.getParameter(c.DRAW_FRAMEBUFFER_BINDING),read:c.getParameter(c.READ_FRAMEBUFFER_BINDING),viewport:c.getParameter(c.VIEWPORT),program:c.getParameter(c.CURRENT_PROGRAM),vao:c.getParameter(c.VERTEX_ARRAY_BINDING),active:c.getParameter(c.ACTIVE_TEXTURE),blend:c.isEnabled(c.BLEND),depth:c.isEnabled(c.DEPTH_TEST),cull:c.isEnabled(c.CULL_FACE),scissor:c.isEnabled(c.SCISSOR_TEST),colorMask:c.getParameter(c.COLOR_WRITEMASK),depthMask:c.getParameter(c.DEPTH_WRITEMASK)}}function D(c,a){c.bindBuffer(c.UNIFORM_BUFFER,null),c.bindBufferBase(c.UNIFORM_BUFFER,0,null),c.bindBuffer(c.PIXEL_PACK_BUFFER,null),c.bindBuffer(c.PIXEL_UNPACK_BUFFER,null),c.bindFramebuffer(c.DRAW_FRAMEBUFFER,a.draw),c.bindFramebuffer(c.READ_FRAMEBUFFER,a.read),c.viewport(a.viewport[0],a.viewport[1],a.viewport[2],a.viewport[3]),c.useProgram(a.program),c.bindVertexArray(a.vao),c.activeTexture(a.active);const me=function(re,le){le?c.enable(re):c.disable(re)};me(c.BLEND,a.blend),me(c.DEPTH_TEST,a.depth),me(c.CULL_FACE,a.cull),me(c.SCISSOR_TEST,a.scissor),c.colorMask(a.colorMask[0],a.colorMask[1],a.colorMask[2],a.colorMask[3]),c.depthMask(a.depthMask)}function G(c){const a=c.host.gl,me=c.graph,re=c.module,le=re.kernels,we=c.host.firstUnit||4;if(!(c.host.caps?c.host.caps(a):{}).colorBufferFloat)throw Object.assign(new Error("EXT_color_buffer_float is not available"),{fallback:"EXT_color_buffer_float"});const m=function(i,l){const k=a.getParameter(i);return typeof k=="number"?k:l};if(m(a.MAX_DRAW_BUFFERS,0)<2||m(a.MAX_TEXTURE_IMAGE_UNITS,0)<we+5)throw Object.assign(new Error("too few texture units or draw buffers"),{fallback:"texture-units"});for(const i of c.steps){const l=i.pass;if(!l||l.type!=="kernel")continue;const k=le[c.kernelKey(l.kernel)];if(!k.glsl||!k.gatherLayout)throw Object.assign(new Error("kernel "+l.kernel+" has no GLSL gather form"),{fallback:"kernel:"+l.kernel})}const g=[],U=[],de=[],ke=[],o=Object.create(null),b={};for(const i of Object.keys(me.exports||{}))b[me.exports[i]]=!0;const O=a.createVertexArray(),P={split:!1,disposed:!1};let _=null;const y=Object.create(null),h=new Map,Z=new Map;let T=0;const te={rgba16float:a.RGBA16F,rgba32float:a.RGBA32F,r32float:a.R32F};function ye(i,l,k,d,N){const Y=a.createTexture();return g.push(Y),a.activeTexture(a.TEXTURE0+we),a.bindTexture(a.TEXTURE_2D,Y),a.texStorage2D(a.TEXTURE_2D,d,k,i,l),a.texParameteri(a.TEXTURE_2D,a.TEXTURE_MIN_FILTER,N?d>1?a.LINEAR_MIPMAP_LINEAR:a.LINEAR:a.NEAREST),a.texParameteri(a.TEXTURE_2D,a.TEXTURE_MAG_FILTER,N?a.LINEAR:a.NEAREST),a.texParameteri(a.TEXTURE_2D,a.TEXTURE_WRAP_S,N?a.REPEAT:a.CLAMP_TO_EDGE),a.texParameteri(a.TEXTURE_2D,a.TEXTURE_WRAP_T,N?a.REPEAT:a.CLAMP_TO_EDGE),Y}function Te(i,l){const k=a.createShader(i);if(ke.push(k),a.shaderSource(k,l),a.compileShader(k),!a.getShaderParameter(k,a.COMPILE_STATUS))throw new Error("elio-graph: shader did not compile: "+a.getShaderInfoLog(k));return k}function E(i){if(o[i])return o[i];const l=le[c.kernelKey(i)];_||(_=Te(a.VERTEX_SHADER,ne));const k=Te(a.FRAGMENT_SHADER,l.glsl),d=a.createProgram();a.attachShader(d,_),a.attachShader(d,k),a.linkProgram(d);const N=l.gatherLayout,Y=o[i]={handle:d,layout:N,kernel:l};if(!a.getProgramParameter(d,a.LINK_STATUS))throw new Error("elio-graph: program "+i+" did not link: "+a.getProgramInfoLog(d));return a.useProgram(d),Y.width=a.getUniformLocation(d,"elio_width"),Y.lens=N.inputs.map(function(J){return a.getUniformLocation(d,J.length)}),Y.widths=N.inputs.map(function(J){return J.width?a.getUniformLocation(d,J.width):null}),N.inputs.forEach(function(J,M){a.uniform1i(a.getUniformLocation(d,J.sampler),we+M)}),N.uniforms.forEach(function(J,M){a.uniformBlockBinding(d,a.getUniformBlockIndex(d,J.block),M)}),Y}function B(i,l){const d=(i.uniformsStd140||i.uniforms).find(function(N){return N.name===l});if(!d)throw new Error("elio-graph: kernel has no uniform block "+l);return d}function v(){for(const i of me.resources){let l;if(i.kind==="buffer"){const k=Math.ceil(i.count/i.width);l={name:i.name,id:++T,kind:"buffer",version:0,w:i.width,h:k,levels:1,tex:ye(i.width,k,a.RGBA32F,1,!1)}}else{const k=i.mipLevels||1,d=te[i.format];if(!d)throw new Error("elio-graph: unsupported format "+i.format);const N=i.format==="rgba16float"&&(k>1||!!b[i.name]);l={name:i.name,id:++T,kind:"texture",version:0,w:i.width,h:i.height,levels:k,tex:ye(i.width,i.height,d,k,N)}}y[i.name]=l}}function L(i){const l=a.createFramebuffer();U.push(l),a.bindFramebuffer(a.FRAMEBUFFER,l);const k=[];for(const d of i){for(a.framebufferTexture2D(a.FRAMEBUFFER,a.COLOR_ATTACHMENT0+d.location,a.TEXTURE_2D,d.res.tex,d.level);k.length<=d.location;)k.push(a.NONE);k[d.location]=a.COLOR_ATTACHMENT0+d.location}return a.drawBuffers(k),{fbo:l,complete:a.checkFramebufferStatus(a.FRAMEBUFFER)===a.FRAMEBUFFER_COMPLETE}}function S(i){const l=i.map(function(Y){return Y.res.id+"@"+Y.level+":"+Y.location}).join(","),k=h.get(l+(P.split?"s":""));if(k)return k;let d;const N=function(Y){const J=L([Y]);if(!J.complete)throw new Error("elio-graph: framebuffer incomplete");return J};if(i.length===1)d=[N(i[0])];else{const Y=P.split?null:L(i);Y&&Y.complete?d=[Y]:(P.split=!0,d=i.map(N))}return h.set(l+(P.split?"s":""),d),d}function K(){if(!P.disposed){P.disposed=!0;for(const i of g)a.deleteTexture(i);for(const i of U)a.deleteFramebuffer(i);for(const i of de)a.deleteBuffer(i);for(const i of Object.keys(o))a.deleteProgram(o[i].handle);for(const i of ke)a.deleteShader(i);a.deleteVertexArray(O),g.length=U.length=de.length=0,h.clear(),Z.clear()}}let f=[];const p=F(a);try{f=c.steps.map(function(i){const l=i.pass;if(l.type==="mipmap"||l.type==="copy")return{pass:l};const k=E(l.kernel),d=k.layout,N=d.inputs.map(function(M){const _e=l.bindings.find(function(Se){return Se.name===M.name});if(!_e)throw new Error("elio-graph: pass "+l.name+" does not bind input "+M.name);return _e}),Y=d.outputs.map(function(M){const _e=l.bindings.find(function(Se){return Se.name===M.name});if(!_e)throw new Error("elio-graph: pass "+l.name+" does not bind output "+M.name);return{binding:_e,location:M.location}}),J=(l.uniforms||[]).map(function(M){const _e=d.uniforms.findIndex(function(e){return e.name===M.name});if(_e<0)throw new Error("elio-graph: pass "+l.name+" has no uniform block "+M.name);const Se=B(k.kernel,M.name),Ze=a.createBuffer(),Re=Math.max(16,Math.ceil(Se.size/16)*16);return de.push(Ze),a.bindBuffer(a.UNIFORM_BUFFER,Ze),a.bufferData(a.UNIFORM_BUFFER,Re,a.DYNAMIC_DRAW),{index:_e,layout:Se,values:M.values,buffer:Ze,size:Re,written:!1,constant:c.isStatic(M.values)}});return{pass:l,p:k,inputs:N,outputs:Y,blocks:J}}),v()}catch(i){throw K(),i}finally{D(a,p)}function r(i){const l=y[i.from],k=y[i.to];if(l.w*l.h!==k.w*k.h||k.w!==i.width)throw new Error("elio-graph: pass "+i.name+" cannot copy "+l.name+" into "+k.name+" at width "+i.width);if(k.copiedFrom===l&&k.copiedVersion===l.version)return;let d=Z.get(k.id);d||(d=a.createBuffer(),de.push(d),a.bindBuffer(a.PIXEL_PACK_BUFFER,d),a.bufferData(a.PIXEL_PACK_BUFFER,l.w*l.h*16,a.STREAM_COPY),a.bindBuffer(a.PIXEL_PACK_BUFFER,null),Z.set(k.id,d)),a.bindFramebuffer(a.READ_FRAMEBUFFER,S([{res:l,level:0,location:0}])[0].fbo),a.bindBuffer(a.PIXEL_PACK_BUFFER,d),a.readPixels(0,0,l.w,l.h,a.RGBA,a.FLOAT,0),a.bindBuffer(a.PIXEL_PACK_BUFFER,null),a.bindBuffer(a.PIXEL_UNPACK_BUFFER,d),a.activeTexture(a.TEXTURE0+we),a.bindTexture(a.TEXTURE_2D,k.tex),a.texSubImage2D(a.TEXTURE_2D,0,0,0,k.w,k.h,a.RGBA,a.FLOAT,0),a.bindBuffer(a.PIXEL_UNPACK_BUFFER,null),k.copiedFrom=l,k.copiedVersion=l.version,k.version++}function x(i){for(const l of i){const k=y[l[0]];y[l[0]]=y[l[1]],y[l[1]]=k}}function I(i){for(const l of me.resources){if(!l.feed)continue;const k=y[l.name],d=new Float32Array(k.w*k.h*4);d.set(i[l.feed].subarray(0,d.length)),a.activeTexture(a.TEXTURE0+we),a.bindTexture(a.TEXTURE_2D,k.tex),a.texSubImage2D(a.TEXTURE_2D,0,0,0,k.w,k.h,a.RGBA,a.FLOAT,d)}}function R(i,l){const k=i.pass,d=i.p,N=k.target,Y=i.outputs.map(function(M){return{res:y[M.binding.resource],level:M.binding.level||0,location:M.location}}),J=i.inputs.map(function(M){return y[M.resource]});i.inputs.forEach(function(M,_e){for(const Se of Y)if(Se.res===J[_e]&&Se.level===(M.level||0))throw new Error("elio-graph: pass "+k.name+" reads "+Se.res.name+", which it writes")}),a.useProgram(d.handle),a.uniform1ui(d.width,N.elioWidth),J.forEach(function(M,_e){a.activeTexture(a.TEXTURE0+we+_e),a.bindTexture(a.TEXTURE_2D,M.tex),d.lens[_e]&&a.uniform1ui(d.lens[_e],M.w*M.h),d.widths[_e]&&a.uniform1ui(d.widths[_e],M.w)});for(const M of i.blocks)(!M.constant||!M.written)&&(a.bindBuffer(a.UNIFORM_BUFFER,M.buffer),a.bufferSubData(a.UNIFORM_BUFFER,0,new Uint8Array(c.pack(M.layout,M.values,l,k.name))),M.written=!0),a.bindBufferRange(a.UNIFORM_BUFFER,M.index,M.buffer,0,M.size);for(const M of Y)M.res.version++;a.viewport(0,0,N.width,N.height);for(const M of S(Y))a.bindFramebuffer(a.FRAMEBUFFER,M.fbo),a.drawArrays(a.TRIANGLES,0,3)}const ue=c.host.profile,V=function(i,l){i.pass.type==="copy"?r(i.pass):i.pass.type==="mipmap"?(a.activeTexture(a.TEXTURE0+we),a.bindTexture(a.TEXTURE_2D,y[i.pass.resource].tex),a.generateMipmap(a.TEXTURE_2D)):R(i,l)},ie=[];for(const i of c.host.throttle||[]){const l=new RegExp(i.match),k=f.filter(function(N){return l.test(N.pass.name)&&!N.group});if(!k.length||!(i.every>1))continue;const d={every:Math.floor(i.every),hold:i.hold?new RegExp(i.hold):null,since:1/0,dt:0,snap:null,runs:!1,derived:null,plans:k};for(const N of k)N.group=d;ie.push(d)}const Ae=function(i){let l=null,k=!1;for(const d of f){const N=(d.outputs||[]).map(function(Y){return Y.binding.resource}).concat(d.pass.resource?[d.pass.resource]:[]);N.indexOf(i[0])<0&&N.indexOf(i[1])<0||(!d.group||l&&l!==d.group?k=!0:l=d.group)}return k?null:l},be=(me.frameSwaps||[]).map(function(i){return{pair:i,group:ie.length?Ae(i):null}}),ve=function(i){return i&&i!=="always"?i.replace(/^(param|changed):/,"").split(",").map(function(l){return l.trim()}):[]},Me=function(i,l,k){for(const d of ie){d.dt+=Number(i&&i.dt)||0,d.since++;const N=d.plans.some(function(Y){return ve(Y.pass.when).some(function(J){return l[J]})});d.runs=N||d.since>=d.every,d.runs&&(d.derived=k.derive(d.dt,d.snap))}},De=function(i){for(const l of ie)l.runs&&(l.since=0,l.dt=0,l.snap=i.snapshot())};return{isReady:function(){return!P.disposed},run:function(i,l,k,d){if(a.isContextLost&&a.isContextLost())return!1;const N=F(a);try{a.disable(a.BLEND),a.disable(a.DEPTH_TEST),a.disable(a.CULL_FACE),a.disable(a.SCISSOR_TEST),a.colorMask(!0,!0,!0,!0),a.bindVertexArray(O),I(i),ie.length&&Me(l,k,d);for(const J of f){if(J.group&&!J.group.runs||!c.holds(J.pass.when,i,k))continue;const M=J.group?J.group.derived:i;ue?ue(J.pass.name,function(){V(J,M)}):V(J,M)}for(const J of be)(!J.group||J.group.runs)&&x([J.pair]);ie.length&&De(d)}finally{D(a,N)}const Y=c.host.textureCache;return Y&&(Y._sceneTextureEpoch=(Y._sceneTextureEpoch||0)+1),!0},export:function(i){const l=y[me.exports[i]];return l?{tex:l.tex,width:l.w,height:l.h,levels:l.levels}:null},held:function(i){for(const l of ie)if(l.hold&&l.hold.test(i)&&l.snap&&i in l.snap)return l.snap[i]},split:function(){return P.split},dispose:K}}window.__gosx_elio_graph.executors.webgl2=G,window.__gosx_elio_graph.glState={save:F,restore:D}})(),(function(){const ne={f32:4,"vec2<f32>":8,"vec4<f32>":16};function F(D){const G=D.host.device,c=D.graph,a=D.module.kernels,me=D.host;if(!G)throw Object.assign(new Error("no device"),{fallback:"no-device"});const re="gosx-elio-"+D.label+"-",le=[],we=[],t=Object.create(null),m=Object.create(null),g=Object.create(null),U=new Map,de=new Map,ke={disposed:!1};let o=0;const b={};for(const f of Object.keys(c.exports||{}))b[c.exports[f]]=!0;const O=G.queue,P=GPUBufferUsage.STORAGE|GPUBufferUsage.COPY_DST|GPUBufferUsage.COPY_SRC,_=GPUBufferUsage.UNIFORM|GPUBufferUsage.COPY_DST;for(const f of c.resources)if(f.kind==="buffer"){const p=Math.max(16,Math.ceil(f.count*(ne[f.elementType]||16)/4)*4),r=G.createBuffer({label:re+f.name,size:p,usage:P});le.push(r),t[f.name]={name:f.name,id:++o,buffer:r}}else{const p=G.createTexture({label:re+f.name,size:[f.width,f.height,1],format:f.format,mipLevelCount:f.mipLevels||1,usage:GPUTextureUsage.STORAGE_BINDING|GPUTextureUsage.TEXTURE_BINDING});we.push(p),t[f.name]={name:f.name,id:++o,texture:p,w:f.width,h:f.height,levels:f.mipLevels||1,format:f.format}}function y(f){return f.map(function(p){const r={binding:p.binding,visibility:GPUShaderStage.COMPUTE};return p.kind==="uniform"?r.buffer={type:"uniform"}:p.kind==="storage-read"?r.buffer={type:"read-only-storage"}:p.kind==="storage-rw"?r.buffer={type:"storage"}:p.kind==="sampled-texture"||p.kind==="texture"?r.texture={sampleType:"unfilterable-float",viewDimension:"2d"}:r.storageTexture={access:"write-only",format:p.format,viewDimension:"2d"},r})}for(const f of D.steps){const p=f.pass;if(!p)continue;if(p.type!=="kernel")throw new Error("elio-graph: pass "+p.name+": "+p.type+" passes exist only on webgl2");const r=p.kernel+"/"+p.entry;if(m[r])continue;const x=a[D.kernelKey(p.kernel)],I=g[p.kernel]||(g[p.kernel]=G.createBindGroupLayout({label:re+p.kernel,entries:y(x.bindings)})),R=G.createShaderModule({label:re+p.kernel,code:x.wgsl}),ue=me.create(G,"compute",{label:re+p.kernel,layout:G.createPipelineLayout({bindGroupLayouts:[I]}),compute:{module:R,entryPoint:p.entry}});m[r]={handle:ue,layout:I,kernel:x}}function h(f){const p=me.validation&&me.validation.get(f.handle);return!p||p.ready===!0?p&&p.pipeline||f.handle:null}let Z=0;const T=D.steps.map(function(f){const p=f.pass,r=m[p.kernel+"/"+p.entry],x=(p.uniforms||[]).map(function(I){const R=r.kernel.uniforms.find(function(ie){return ie.name===I.name});if(!R)throw new Error("elio-graph: pass "+p.name+" has no uniform block "+I.name);const ue=Math.max(16,Math.ceil(R.size/16)*16),V=G.createBuffer({label:re+p.name+"-"+I.name,size:ue,usage:_});return le.push(V),{name:I.name,layout:R,values:I.values,buffer:V,constant:D.isStatic(I.values),written:!1}});return{pass:p,p:r,blocks:x,id:++Z}});function te(f){const p=f.pass,r=[],x=[p.name,f.id];for(const ue of f.p.kernel.bindings){let V;if(ue.kind==="uniform")V={buffer:f.blocks.find(function(ie){return ie.name===ue.name}).buffer};else{const ie=p.bindings.find(function(be){return be.name===ue.name});if(!ie)throw new Error("elio-graph: pass "+p.name+" does not bind "+ue.name);const Ae=t[ie.resource];x.push(Ae.id+"@"+(ie.level||0)),ue.kind==="storage-read"||ue.kind==="storage-rw"?V={buffer:Ae.buffer}:V=Ae.texture.createView({baseMipLevel:ie.level||0,mipLevelCount:1})}r.push({binding:ue.binding,resource:V})}const I=x.join("|");let R=U.get(I);return R||(R=G.createBindGroup({label:re+p.name,layout:f.p.layout,entries:r}),U.set(I,R)),R}function ye(f){for(const p of f){const r=t[p[0]];t[p[0]]=t[p[1]],t[p[1]]=r}}const Te=3,E=120;let B=null;if(me.timing!==!1&&G.features&&G.features.has&&G.features.has("timestamp-query"))try{const f=[],p=G.createQuerySet({label:re+"time",type:"timestamp",count:2}),r=G.createBuffer({label:re+"time-resolve",size:16,usage:GPUBufferUsage.QUERY_RESOLVE|GPUBufferUsage.COPY_SRC});le.push(r);for(let x=0;x<Te;x++){const I=G.createBuffer({label:re+"time-"+x,size:16,usage:GPUBufferUsage.MAP_READ|GPUBufferUsage.COPY_DST});le.push(I),f.push({buffer:I,state:"free"})}B={querySet:p,resolve:r,slots:f,samples:[],ms:null}}catch{B=null}function v(){if(B)for(const f of B.slots)f.state==="copied"&&(f.state="mapping",f.buffer.mapAsync(1).then(function(){if(ke.disposed||f.state!=="mapping")return;const p=new BigUint64Array(f.buffer.getMappedRange().slice(0,16));f.buffer.unmap(),f.state="free",p[1]>p[0]&&(B.ms=Number(p[1]-p[0])/1e6,B.samples.push(B.ms),B.samples.length>E&&B.samples.shift())},function(){f.state="free"}))}function L(f){const p=B.slots.find(function(r){return r.state==="free"});f.resolveQuerySet(B.querySet,0,2,B.resolve,0),p&&(f.copyBufferToBuffer(B.resolve,0,p.buffer,0,16),p.state="copied")}const S=G.lost&&G.lost.then?G.lost:null;S&&S.then(function(){K()},function(){K()});function K(){if(!ke.disposed){ke.disposed=!0;for(const f of le)f.destroy();for(const f of we)f.destroy();le.length=we.length=0,U.clear(),de.clear(),B&&B.querySet.destroy&&B.querySet.destroy()}}return{isReady:function(){if(ke.disposed)return!1;for(const f of Object.keys(m))if(!h(m[f]))return!1;return!0},run:function(f,p,r){const x=p.encoder;if(!x)throw new Error("elio-graph: a webgpu frame needs frame.encoder");for(const R of c.resources)R.feed&&O.writeBuffer(t[R.name].buffer,0,f[R.feed]);v();const I=T.filter(function(R){return D.holds(R.pass.when,f,r)});return I.forEach(function(R,ue){const V=R.pass;for(const be of R.blocks)be.constant&&be.written||(O.writeBuffer(be.buffer,0,new Uint8Array(D.pack(be.layout,be.values,f,V.name))),be.written=!0);const ie={label:re+V.name};B&&(ue===0||ue===I.length-1)&&(ie.timestampWrites={querySet:B.querySet},ue===0&&(ie.timestampWrites.beginningOfPassWriteIndex=0),ue===I.length-1&&(ie.timestampWrites.endOfPassWriteIndex=1));const Ae=x.beginComputePass(ie);Ae.setPipeline(h(R.p)),Ae.setBindGroup(0,te(R)),Ae.dispatchWorkgroups(V.dispatch[0],V.dispatch[1]||1,V.dispatch[2]||1),Ae.end()}),B&&I.length&&L(x),ye(c.frameSwaps||[]),!0},export:function(f){const p=t[c.exports[f]];if(p&&p.buffer)return{buffer:p.buffer};if(!p)return null;let r=de.get(p.texture);return r||(r=p.texture.createView(),de.set(p.texture,r)),{texture:p.texture,view:r,width:p.w,height:p.h,levels:p.levels}},gpuTiming:function(){return B?{ms:B.ms,samples:B.samples.slice()}:null},dispose:K}}window.__gosx_elio_graph.executors.webgpu=F})(),(function(){const ne={uniform:0,sampler:1,disp:2,slope:6,foam:10,wake:11},F={edgeCells:8,k:.2,slope:3,slickLo:.04,slickHi:.4,slickFine:.85,slickMid:.4,glow:.3,thinBase:.12,thinLace:.3,bodyBase:.32,bodyLace:.3,fadeNear:120,fadeFar:700};function D(t,m,g,U){const de=t.indexOf(m);if(de<0)throw new Error("ocean-fft: anchor not found: "+m.slice(0,48));const ke=t.indexOf(g,de);if(ke<0)throw new Error("ocean-fft: anchor not found: "+g.slice(0,48));return t.slice(0,de)+U+t.slice(ke)}function G(t,m,g){if(t.indexOf(m)<0)throw new Error("ocean-fft: anchor not found: "+m.slice(0,48));return t.replace(m,()=>g)}function c(t){let m="const WAKE_EDGE_CELLS: f32 = "+F.edgeCells.toFixed(1)+`;
`;m+=`struct OceanSpec { c: array<vec4f, 4>, f: vec4f, m: vec4f, w: array<vec4f, 4>, k: array<vec4f, 2> };
`,m+="@group("+t+") @binding("+ne.uniform+`) var<uniform> sp: OceanSpec;
`,m+="@group("+t+") @binding("+ne.sampler+`) var spSampler: sampler;
`;for(let g=0;g<4;g++)m+="@group("+t+") @binding("+(ne.disp+g)+") var spDisp"+g+`: texture_2d<f32>;
`;for(let g=0;g<4;g++)m+="@group("+t+") @binding("+(ne.slope+g)+") var spSlope"+g+`: texture_2d<f32>;
`;return m+="@group("+t+") @binding("+ne.foam+`) var spFoam: texture_2d<f32>;
`,m+="@group("+t+") @binding("+ne.wake+`) var spWakeTex: texture_2d<f32>;
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
`,t+="    let sws = swv.z * smoothstep(0.0, 1.2, depth0 * "+F.k.toFixed(2)+`);
`,t+=`    p.y += swk.x * sws; dx.y += swk.y * sws; dz.y += swk.z * sws;
`,t+=`  }
`,t}function me(){let t=`  var sGx = 0.0; var sGz = 0.0; var sXX = 0.0; var sZZ = 0.0; var sXZ = 0.0;
`;t+=`  let wxz = in.rest - sp.m.yz;
`,t+=`  let sFw = fwidth(wxz);
`,t+=`  var sOp = 0.0; var sAlb = 1.0;
`,t+=`  let swv = spWake(in.rest, ocean.p[7].xz);
`,t+=`  let swf = textureSample(spWakeTex, spSampler, swv.xy);
`,t+="  let sSlick = swv.z * smoothstep("+F.slickLo.toFixed(2)+", "+F.slickHi.toFixed(2)+`, swf.w);
`;for(let m=0;m<4;m++){const g=m>=2?F.slickFine:m===1?F.slickMid:0;t+="  if (sp.c["+m+`].w > 0.5) {
`,t+="    let uv = (in.rest - sp.w["+m+"].xy) / sp.c["+m+"].x; let k = smoothstep(0.0, 1.2, in.depth0 * sp.c["+m+"].y)"+(g?" * (1.0 - "+g.toFixed(2)+" * sSlick)":"")+`;
`,t+="    let a = textureSample(spDisp"+m+", spSampler, uv); let b = textureSample(spSlope"+m+`, spSampler, uv);
`,t+=`    sGx += b.x * k; sGz += b.y * k; sXX += b.z * k; sZZ += b.w * k; sXZ += a.w * k;
`,t+=`  }
`}return t+="  let swm = swv.z * smoothstep(0.0, 1.2, in.depth0 * "+F.k.toFixed(2)+`);
`,t+="  sGx += swf.y * swm * "+F.slope.toFixed(2)+"; sGz += swf.z * swm * "+F.slope.toFixed(2)+`;
`,t+=`  let sJ = (1.0 + sXX) * (1.0 + sZZ) - sXZ * sXZ;
`,t+=`  let sFoamTex = textureSampleLevel(spFoam, spSampler, wxz * sp.f.x, 0.0);
`,t+=`  let sPersist = sFoamTex.r;
`,t+=`  let N = normalize(vec3f(sGz * sXZ - (1.0 + sZZ) * sGx, max(sJ, 0.1), sXZ * sGx - sGz * (1.0 + sXX)));
`,t}const re=`    let sNear = 1.0 - smoothstep(0.7 * sp.m.w, sp.m.w, length(in.world.xz - ocean.p[7].xz));
    let sJf = (1.0 + sp.f.y * sXX) * (1.0 + sp.f.y * sZZ) - sp.f.y * sp.f.y * sXZ * sXZ;
    let sStateless = clamp((sp.f.z - sJf) * sp.f.w, 0.0, 1.0) * lace * bu;
    let sKept = smoothstep(0.04, 0.6, sPersist) * (0.55 + 0.45 * lace);
    foam = mix(sStateless, sKept, sNear);
    let sDen = mix(smoothstep(0.1, 0.9, swf.w) * swv.z, sFoamTex.b, sNear);
    var sStr = mix(0.4, spLace(wxz, t, sFw), 1.0 - smoothstep(`+F.fadeNear.toFixed(1)+", "+F.fadeFar.toFixed(1)+`, dist));
    sStr *= mix(0.55, 1.0, smoothstep(0.1, 0.6, sDen));
    let sThin = smoothstep(0.02, 0.5, sDen); let sBody = smoothstep(0.5, 1.0, sDen);
    sOp = sThin * mix(`+F.thinBase.toFixed(2)+" + "+F.thinLace.toFixed(2)+" * sStr, "+F.bodyBase.toFixed(2)+" + "+F.bodyLace.toFixed(2)+` * sStr, sBody);
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
`,t}function we(t,m){let g=t;return g=G(g,"@location(4) depth0: f32 };","@location(4) depth0: f32, @location(5) rest: vec2f };"),g=D(g,"  var p = vec3f(xz.x, level, xz.y);","  let ph = fract(t * ocean.p[0].w / 9.0",a()),g=G(g,"out.depth0 = depth0;","out.depth0 = depth0; out.rest = xz;"),g=G(g,`  let N = od(normalize(in.normal), in.world.xz, t, ocean.p[9].xy);
`,me()),g=G(g,"  if (dist < 250.0) {","  if (dist < 6000.0) {"),g=G(g,`    foam = (1.0 - smoothstep(0.12, 0.35, in.jacobian)) * lace * bu;
`,re),g=G(g,"(1.0 - smoothstep(80.0, 250.0, dist))","(1.0 - smoothstep(1500.0, 6000.0, dist))"),g=G(g,"  return vec4f(color, alpha);",`  if (sp.k[1].w > 0.0) { let hh = clamp(swf.x / sp.k[1].w, -1.0, 1.0); color = select(vec3f(0.25), select(mix(vec3f(1.0), vec3f(1.0, 0.15, 0.1), hh), mix(vec3f(1.0), vec3f(0.1, 0.3, 1.0), -hh), hh < 0.0), swv.z > 0.0); alpha = 1.0; }
  return vec4f(color, alpha);`),g=G(g,"  color = mix(color, fl, foam); alpha = mix(alpha, 1.0, foam);",`  let sAer = swv.z * smoothstep(0.45, 1.0, swf.w);
  let sTurq = vec3f(0.05, 0.42, 0.44) * (ambient * 0.9 + sunCol * 0.05);
  color = mix(color, sTurq, sAer * `+F.glow.toFixed(2)+` * (1.0 - foam)); alpha = mix(alpha, 1.0, sAer * 0.5);
  color = mix(color, fl, foam); alpha = mix(alpha, 1.0, foam);
  let sSun = smoothstep(0.0, 0.55, dot(N, L));
  let sWfl = fl * mix(vec3f(0.66, 0.76, 0.9), vec3f(1.0), sSun) * sAlb + ocean.p[4].xyz * sunCol * 0.2 * max(dot(N, L), 0.0) * sAlb;
  color = mix(color, sWfl, sOp) + spec * sOp * 0.25; alpha = mix(alpha, 1.0, sOp);`),g=G(g,"(ambient * 0.55 + sunCol * 0.04 * max(dot(N, L), 0.0))","(ambient * 1.4 + sunCol * 0.2 * max(dot(N, L), 0.0) + vec3f(0.05))"),g+`
`+c(m)+`
`+le()}window.__gosx_scene3d_ocean_fft_surface={wgsl:we,SLOT:ne}})(),(function(){const ne={disp:4,slope:8,foam:12,wake:13},F={edgeCells:8,k:.2,slope:3,slickLo:.04,slickHi:.4,slickFine:.85,slickMid:.4,glow:.3,thinBase:.12,thinLace:.3,bodyBase:.32,bodyLace:.3,fadeNear:120,fadeFar:700};function D(t,m,g){if(t.indexOf(m)<0)throw new Error("ocean-fft: anchor not found: "+m.slice(0,48));return t.replace(m,()=>g)}function G(t,m,g,U){const de=t.indexOf(m);if(de<0)throw new Error("ocean-fft: anchor not found: "+m.slice(0,48));const ke=t.indexOf(g,de);if(ke<0)throw new Error("ocean-fft: anchor not found: "+g.slice(0,48));return t.slice(0,de)+U+t.slice(ke)}function c(){let t=`uniform vec4 u_sp[12];
`;for(let m=0;m<4;m++)t+="uniform sampler2D u_spDisp"+m+`;
`;for(let m=0;m<4;m++)t+="uniform sampler2D u_spSlope"+m+`;
`;return t+=`uniform sampler2D u_spFoam;
`,t+=`uniform sampler2D u_spWake;
`,t+=`vec3 spWake(vec2 rest, vec2 cam) {
`,t+=`  vec2 r = (rest - cam - u_sp[10].xy) / max(u_sp[11].x, 1e-3);
`,t+=`  float edge = min(min(r.x, u_sp[11].z - r.x), min(r.y, u_sp[11].z - r.y));
`,t+="  return vec3((r + u_sp[10].zw) / u_sp[11].z, smoothstep(0., "+F.edgeCells.toFixed(1)+`, edge) * u_sp[11].y);
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
`,t+="    float sws = swv.z * smoothstep(0., 1.2, depth0 * "+F.k.toFixed(2)+`);
`,t+=`    p.y += swk.x * sws; dx.y += swk.y * sws; dz.y += swk.z * sws;
`,t+=`  }
`,t}function me(){let t=`  float sGx = 0., sGz = 0., sXX = 0., sZZ = 0., sXZ = 0.;
`;t+=`  vec2 wxz = v_rest - u_sp[5].yz;
`,t+=`  vec2 sFw = fwidth(wxz);
`,t+=`  float sOp = 0., sAlb = 1.;
`,t+=`  vec3 swv = spWake(v_rest, u_ocean[7].xz);
`,t+=`  vec4 swf = texture(u_spWake, swv.xy);
`,t+="  float sSlick = swv.z * smoothstep("+F.slickLo.toFixed(2)+", "+F.slickHi.toFixed(2)+`, swf.w);
`;for(let m=0;m<4;m++){const g=m>=2?F.slickFine:m===1?F.slickMid:0;t+="  if (u_sp["+m+`].w > 0.5) {
`,t+="    vec2 suv = (v_rest - u_sp["+(6+m)+"].xy) / u_sp["+m+"].x; float sk = smoothstep(0., 1.2, v_d0 * u_sp["+m+"].y)"+(g?" * (1. - "+g.toFixed(2)+" * sSlick)":"")+`;
`,t+="    vec4 sa = texture(u_spDisp"+m+", suv), sb = texture(u_spSlope"+m+`, suv);
`,t+=`    sGx += sb.x * sk; sGz += sb.y * sk; sXX += sb.z * sk; sZZ += sb.w * sk; sXZ += sa.w * sk;
`,t+=`  }
`}return t+="  float swm = swv.z * smoothstep(0., 1.2, v_d0 * "+F.k.toFixed(2)+`);
`,t+="  sGx += swf.y * swm * "+F.slope.toFixed(2)+"; sGz += swf.z * swm * "+F.slope.toFixed(2)+`;
`,t+=`  float sJ = (1. + sXX) * (1. + sZZ) - sXZ * sXZ;
`,t+=`  vec4 sFoamTex = textureLod(u_spFoam, wxz * u_sp[4].x, 0.);
`,t+=`  float sPersist = sFoamTex.r;
`,t+=`  vec3 N = normalize(vec3(sGz * sXZ - (1. + sZZ) * sGx, max(sJ, 0.1), sXZ * sGx - sGz * (1. + sXX)));
`,t}const re=`    float sNear = 1. - smoothstep(0.7 * u_sp[5].w, u_sp[5].w, length(v_w.xz - u_ocean[7].xz));
    float sJf = (1. + u_sp[4].y * sXX) * (1. + u_sp[4].y * sZZ) - u_sp[4].y * u_sp[4].y * sXZ * sXZ;
    float sStateless = clamp((u_sp[4].z - sJf) * u_sp[4].w, 0., 1.) * lace * bu;
    float sKept = smoothstep(0.04, 0.6, sPersist) * (0.55 + 0.45 * lace);
    foam = mix(sStateless, sKept, sNear);
    float sDen = mix(smoothstep(0.1, 0.9, swf.w) * swv.z, sFoamTex.b, sNear);
    float sStr = mix(0.4, spLace(wxz, t, sFw), 1. - smoothstep(`+F.fadeNear.toFixed(1)+", "+F.fadeFar.toFixed(1)+`, dist));
    sStr *= mix(0.55, 1., smoothstep(0.1, 0.6, sDen));
    float sThin = smoothstep(0.02, 0.5, sDen), sBody = smoothstep(0.5, 1., sDen);
    sOp = sThin * mix(`+F.thinBase.toFixed(2)+" + "+F.thinLace.toFixed(2)+" * sStr, "+F.bodyBase.toFixed(2)+" + "+F.bodyLace.toFixed(2)+` * sStr, sBody);
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
`,t}function we(t,m){let g=t;g=D(g,"out float v_d0;","out float v_d0; out vec2 v_rest;"),g=D(g,"void main() {",c()+"void main() {"),g=G(g,"  vec3 p = vec3(xz.x, level, xz.y), dx","  float ph = fract(t * u_ocean[0].w / 9.",a()),g=D(g,"v_d0 = depth0;","v_d0 = depth0; v_rest = xz;");let U=m;return U=D(U,"in float v_d0;","in float v_d0; in vec2 v_rest;"),U=D(U,"void main() {",c()+le()+"void main() {"),U=D(U,`  vec3 N = od(normalize(v_n), v_w.xz, t, u_ocean[9].xy);
`,me()),U=D(U,"  if (dist < 250.) {","  if (dist < 6000.) {"),U=D(U,`    foam = (1. - smoothstep(0.12, 0.35, v_j)) * lace * bu;
`,re),U=D(U,"(1. - smoothstep(80., 250., dist))","(1. - smoothstep(1500., 6000., dist))"),U=D(U,"  fragColor = vec4(color, alpha);",`  if (u_sp[11].w > 0.) { float hh = clamp(swf.x / u_sp[11].w, -1., 1.); color = swv.z > 0. ? (hh < 0. ? mix(vec3(1.), vec3(.1, .3, 1.), -hh) : mix(vec3(1.), vec3(1., .15, .1), hh)) : vec3(.25); alpha = 1.; }
  fragColor = vec4(color, alpha);`),U=D(U,"  color = mix(color, fl, foam); alpha = mix(alpha, 1., foam);",`  float sAer = swv.z * smoothstep(0.45, 1.0, swf.w);
  vec3 sTurq = vec3(0.05, 0.42, 0.44) * (ambient * 0.9 + sunCol * 0.05);
  color = mix(color, sTurq, sAer * `+F.glow.toFixed(2)+` * (1. - foam)); alpha = mix(alpha, 1., sAer * 0.5);
  color = mix(color, fl, foam); alpha = mix(alpha, 1., foam);
  float sSun = smoothstep(0., 0.55, dot(N, L));
  vec3 sWfl = fl * mix(vec3(0.66, 0.76, 0.9), vec3(1.), sSun) * sAlb + u_ocean[4].xyz * sunCol * 0.2 * max(dot(N, L), 0.) * sAlb;
  color = mix(color, sWfl, sOp) + spec * sOp * 0.25; alpha = mix(alpha, 1., sOp);`),U=D(U,"(ambient * 0.55 + sunCol * 0.04 * max(dot(N, L), 0.))","(ambient * 1.4 + sunCol * 0.2 * max(dot(N, L), 0.) + vec3(0.05))"),{vertex:g,fragment:U}}window.__gosx_scene3d_ocean_fft_surface_gl={glsl:we,UNIT:ne}})(),(function(){const ne=window.__gosx_elio_graph,F=window.__gosx_scene3d_elio_ocean,D=window.__gosx_scene3d_ocean_fft_surface,G=window.__gosx_scene3d_ocean_fft_surface_gl,c=.25,a=3.5,me=240,re=.3,le=function(e){return Math.max(.3,.14*e*e/9.81)},we=48,t=4;let m=null,g=null;function U(e){return e==="low"||e==="medium"?e:"high"}function de(e){const n=[];let s={cell:.5,width:1024};for(const u of ne.expand(e)){const w=u.uniforms&&u.uniforms[0]&&u.uniforms[0].values;u.kernel==="ocean_spectrum"&&w?n.push({tile:w.tile,kMin:w.kMin,kMax:w.kMax,n:w.n}):u.kernel==="ocean_foam"&&w&&(s={cell:w.cell,width:w.width})}return{cascades:n.slice(0,t),foam:s}}function ke(e,n){const s=n.graphParams;if(s)for(const u of Object.keys(s))Number.isFinite(s[u])&&e.hasParam(u)&&e.setValue(u,s[u]);Number.isFinite(n.graphTween)&&e.setTween(n.graphTween),e.setNodes(n.graphNodes||{}),b(e,n)}const o=[["hullX","x"],["hullZ","z"],["hullHeading","heading"],["hullSpeed","speed"]];function b(e,n){const s=n.graphNodes&&n.graphNodes.hull;if(!s)return;const u=e.nodePose?e.nodePose(s):null;if(u)for(const w of o){const X=e.graph.params.find(function(A){return A.name===w[0]});X&&X.source.kind==="value"&&e.setValue(w[0],u[w[1]])}}function O(e,n,s,u,w){s.fill(0);const X=e.exportParams(),A=n.foam;n.cascades.forEach(function(H,he){s[he*4]=H.tile,s[he*4+1]=Math.sqrt(Math.max(H.kMin,1e-6)*H.kMax),s[he*4+2]=H.n,s[he*4+3]=1,s[24+he*4]=X["waterModX"+he]||0,s[24+he*4+1]=X["waterModZ"+he]||0});const Q=A.cell*A.width,pe=function(H){return H-Math.floor(H/Q)*Q},q=e.export("wake");if(q){const H=w?e.value("cameraX")-(w.x||0):0,he=w?e.value("cameraZ")-(w.z||0):0;s[40]=(X.wakeCamRelX||0)+H,s[41]=(X.wakeCamRelZ||0)+he,s[42]=X.wakeOriginModX||0,s[43]=X.wakeOriginModZ||0,s[44]=X.wakeCell||0,s[45]=X.wakeOn||0,s[46]=q.width,s[47]=u&&u.wake&&u.wake.debug?re:0}s[16]=1/Q,s[17]=1,s[18]=e.value("foamThreshold"),s[19]=a,s[20]=le(e.value("windSpeed")),s[21]=pe(e.value("waterX")),s[22]=pe(e.value("waterZ")),s[23]=me}function P(e,n){const s=Number.isFinite(n.time)?n.time:0,u=e.last===null?0:Math.max(0,s-e.last),w=Math.min(c,u);e.last=s;const X=n.camera||{};return{time:s,dt:w,accumulateDt:u,cameraX:X.x||0,cameraY:X.y||0,cameraZ:X.z||0}}function _(e){const n=window.__gosx_scene3d_node_poses;return n&&n[e]?n[e]:null}function y(e,n){const s=e&&n();if(!s||!(Math.abs(s.value("driftX"))+Math.abs(s.value("driftZ"))>0))return e;const u=e.speed*Math.sin(e.heading)-s.value("driftX"),w=e.speed*Math.cos(e.heading)-s.value("driftZ");return{x:e.x,z:e.z,heading:e.heading,speed:Math.hypot(u,w)}}function h(){return g}function Z(){return m&&!m.failed?m.gpuTiming():null}const T={x:0,z:0,time:0,scale:1,driftX:0,driftZ:0,gen:0};let te=0;function ye(e){if(e.failed){g=null;return}T.x=e.value("waterX"),T.z=e.value("waterZ"),T.time=e.value("simTime"),T.gen=e.waterGen,T.scale=e.value("timeScale"),T.driftX=e.value("driftX"),T.driftZ=e.value("driftZ"),g=T}function Te(e){const n=g;return L.arr=e,n?(e[1]=n.time,e[2]=n.x,e[3]=n.z,e[4]=1,e[5]=n.scale,e[6]=n.driftX,e[7]=n.driftZ,e[9]=n.gen,x(e,n),!0):!1}const E={COUNT:10,SEQ:11,GEN:12,FLAGS:13,N:14,LAT:15,AT:16,TICK:17,PTS:18,OUT:146,REQ:338,ECHO:339},B=1,v=4,L={arr:null,pending:null,ready:null,delivered:null,failed:"",reported:"",tick:0};let S=0;function K(){const e=L.arr;return e?Math.max(0,Math.min(V,Math.floor(e[E.COUNT])||0)):0}function f(e,n){const s={heights:[],nx:[],nz:[]};for(let u=0;u<n;u++)s.heights.push(e[u*4]),s.nx.push(e[u*4+1]),s.nz.push(e[u*4+2]);return s}function p(e){e.tick=L.tick,e.request=L.arr[E.REQ],L.pending=e}function r(e,n){e.seq!==void 0&&(!L.ready||L.ready.meta.seq<e.seq)&&(L.ready=f(n,e.n),L.ready.meta=e)}function x(e,n){const s=L,u=e[0]+1,w=s.pending;w&&(s.pending=null,w.tick===s.tick&&w.seq===void 0?(w.seq=u,w.gen=n.gen,w.start&&w.start()):w.drop&&w.drop());const X=s.ready;if(X&&X!==s.delivered){for(let A=0;A<X.meta.n;A++)e[E.OUT+3*A]=X.heights[A],e[E.OUT+3*A+1]=X.nx[A],e[E.OUT+3*A+2]=X.nz[A];e[E.SEQ]=X.meta.seq,e[E.GEN]=X.meta.gen,e[E.FLAGS]=B,e[E.N]=X.meta.n,e[E.LAT]=u-X.meta.seq,e[E.ECHO]=X.meta.request,e[E.AT]=performance.timeOrigin+performance.now(),e[E.TICK]++,s.delivered=X}else s.failed&&s.reported!==s.failed&&K()&&(e[E.SEQ]=u,e[E.GEN]=n.gen,e[E.FLAGS]=v,e[E.N]=0,e[E.LAT]=0,e[E.ECHO]=e[E.REQ],e[E.AT]=performance.timeOrigin+performance.now(),e[E.TICK]++,s.reported=s.failed);s.failed||(s.reported="")}function I(){Object.assign(L,{arr:null,pending:null,ready:null,delivered:null,failed:"",reported:""})}const R=3,ue=250,V=64,ie=1,Ae=8,be=16,ve={collect:[],k:0,latest:null,previous:null};function Me(){ve.collect=[],ve.k=0}function De(e,n,s,u){const w=ve.k++;ve.collect.length<V&&ve.collect.push({x:e,z:n});const X=Number.isFinite(u)?u:typeof performance<"u"?performance.now():0,A=ve.latest;if(!A||w>=A.count||X-A.at>ue||Math.abs(A.points[w].x-e)>8||Math.abs(A.points[w].z-n)>8)return null;const Q=ve.previous&&ve.previous.count>w?ve.previous:null,pe=Q&&A.at>Q.at?(A.heights[w]-Q.heights[w])/((A.at-Q.at)/1e3):0,q=A.heights[w]+pe*((X-A.at)/1e3),H=A.nx[w],he=A.nz[w],j=Math.sqrt(Math.max(0,1-H*H-he*he));return{x:e,y:s+q,z:n,normal:{x:H,y:j,z:he},velocity:{x:0,y:pe,z:0}}}function i(e,n){const s=[];for(let w=0;w<R;w++)s.push({buffer:e.createBuffer({label:"gosx-ocean-fft-probe-"+w,size:V*be,usage:ie|Ae}),state:"free",meta:null});const u=function(w){const X=w.buffer,A=w.meta;w.state="mapping",X.mapAsync(ie).then(function(){if(w.state!=="mapping")return;const Q=new Float32Array(X.getMappedRange().slice(0,A.count*be));X.unmap();const pe=f(Q,A.count);ve.previous=ve.latest,ve.latest={at:A.at,count:A.count,points:A.points,heights:pe.heights,nx:pe.nx,nz:pe.nz},w.state="free"},function(){w.state="free"})};return{poll:function(){for(const w of s)w.state==="copied"&&u(w)},copy:function(w,X,A){const Q=s.find(function(pe){return pe.state==="free"});Q&&(w.copyBufferToBuffer(X,0,Q.buffer,0,V*be),Q.state="copied",Q.meta={at:n(),count:A.length,points:A})},dispose:function(){for(const w of s)w.state="disposed",w.buffer.destroy()}}}function l(e,n){if(!e.hasParam("probePoints"))return!1;const s=Math.min(n.length,V);if(!s)return e.setValue("probeCount",0),!1;let u=0,w=0;for(let A=0;A<s;A++)u+=n[A].x,w+=n[A].z;u/=s,w/=s;const X=new Float32Array(V*4);for(let A=0;A<s;A++)X[A*4]=n[A].x-u,X[A*4+1]=n[A].z-w;return e.setValue("probeOriginX",u),e.setValue("probeOriginZ",w),e.setValue("probeCount",s),e.setValue("probePoints",X),!0}function k(e,n,s){const u=function(w,X){const A=g;if(!A)return e.sample(n,w,X,s);const Q=e.sample(n,w-A.x,X-A.z,A.time);return Q.x+=A.x,Q.z+=A.z,Q};return{sample:u,probed:function(w,X){return De(w,X,n.data[0])||u(w,X)},begin:Me}}function d(e){const n=e.device,s="gosx-ocean-fft-",u=function(){const z=GPUShaderStage.VERTEX|GPUShaderStage.FRAGMENT,$=[];$.push({binding:D.SLOT.uniform,visibility:z,buffer:{type:"uniform"}},{binding:D.SLOT.sampler,visibility:z,sampler:{type:"filtering"}});for(let C=0;C<t;C++)$.push({binding:D.SLOT.disp+C,visibility:z,texture:{sampleType:"float"}},{binding:D.SLOT.slope+C,visibility:z,texture:{sampleType:"float"}});return $.push({binding:D.SLOT.foam,visibility:z,texture:{sampleType:"float"}},{binding:D.SLOT.wake,visibility:z,texture:{sampleType:"float"}}),$},w=n.createBindGroupLayout({label:s+"surface",entries:u()}),X=n.createSampler({label:s+"sampler",magFilter:"linear",minFilter:"linear",mipmapFilter:"linear",addressModeU:"repeat",addressModeV:"repeat"}),A=n.createBuffer({label:s+"surface",size:we*4,usage:GPUBufferUsage.UNIFORM|GPUBufferUsage.COPY_DST}),Q=n.createTexture({label:s+"dummy",size:[1,1,1],format:"rgba16float",usage:GPUTextureUsage.TEXTURE_BINDING|GPUTextureUsage.COPY_DST}),pe=Q.createView(),q=new Float32Array(we),H={last:null,tier:"",runner:null,info:null,viewList:null},he=i(n,function(){return e.now?e.now():typeof performance<"u"?performance.now():0}),j={pipeline:null,building:!1,bgl:null,params:null,pts:null,out:null,slots:[],f32:new Float32Array(20),ptsData:new Float32Array(V*4),group:null,views:null},W={update:function(z,$,C){const se=K();if(!se||L.failed||!C)return;let ae=j.slots.find(function(ee){return ee.state==="free"});if(!(!ae&&j.slots.length>=R))try{if(j.bgl||(j.bgl=n.createBindGroupLayout({label:s+"probe",entries:[{binding:0,visibility:GPUShaderStage.COMPUTE,buffer:{type:"uniform"}},{binding:1,visibility:GPUShaderStage.COMPUTE,buffer:{type:"read-only-storage"}},{binding:2,visibility:GPUShaderStage.COMPUTE,buffer:{type:"storage"}},{binding:3,visibility:GPUShaderStage.COMPUTE,sampler:{type:"filtering"}}].concat([4,5,6,7,8,9,10,11].map(function(fe){return{binding:fe,visibility:GPUShaderStage.COMPUTE,texture:{sampleType:"float"}}}))}),j.params=n.createBuffer({label:s+"probe-params",size:80,usage:GPUBufferUsage.UNIFORM|GPUBufferUsage.COPY_DST}),j.pts=n.createBuffer({label:s+"probe-points",size:V*be,usage:GPUBufferUsage.STORAGE|GPUBufferUsage.COPY_DST}),j.out=n.createBuffer({label:s+"probe-out",size:V*be,usage:GPUBufferUsage.STORAGE|GPUBufferUsage.COPY_SRC})),!j.pipeline){if(!j.building){j.building=!0;const fe=typeof n.pushErrorScope=="function",ce=function(Be){L.failed="probe-pipeline: "+String(Be&&Be.message||Be)};fe&&n.pushErrorScope("validation");try{const Be={label:"gosx-probe-ocean",layout:n.createPipelineLayout({bindGroupLayouts:[j.bgl]}),compute:{module:n.createShaderModule({label:"gosx-probe-ocean",code:_e()}),entryPoint:"main"}};(n.createComputePipelineAsync?n.createComputePipelineAsync(Be):Promise.resolve(n.createComputePipeline(Be))).then(function(We){j.pipeline=We},ce)}catch(Be){ce(Be)}fe&&n.popErrorScope().then(function(Be){Be&&ce(Be)},ce)}return}ae||(ae={state:"free",buffer:n.createBuffer({label:s+"probe-staging",size:V*be,usage:ie|Ae})},j.slots.push(ae));const ee=L.arr,ze=$.exportParams(),xe=j.f32,qe=Number(z.ocean&&z.ocean.level)||0,Le=[];xe.fill(0),new Uint32Array(xe.buffer)[0]=se,xe[1]=qe,C.cascades.forEach(function(fe,ce){xe[4+ce*4]=fe.tile,xe[5+ce*4]=1,xe[6+ce*4]=ze["waterModX"+ce]||0,xe[7+ce*4]=ze["waterModZ"+ce]||0}),j.ptsData.fill(0);for(let fe=0;fe<se;fe++)j.ptsData[fe*4]=ee[E.PTS+2*fe],j.ptsData[fe*4+1]=ee[E.PTS+2*fe+1];n.queue.writeBuffer(j.params,0,xe),n.queue.writeBuffer(j.pts,0,j.ptsData);for(let fe=0;fe<t;fe++){const ce=$.export("disp"+fe),Be=$.export("slope"+fe);Le.push(ce?ce.view:pe,Be?Be.view:pe)}if(!j.group||Le.some(function(fe,ce){return fe!==j.views[ce]})){const fe=[{binding:0,resource:{buffer:j.params}},{binding:1,resource:{buffer:j.pts}},{binding:2,resource:{buffer:j.out}},{binding:3,resource:X}];for(let ce=0;ce<t;ce++)fe.push({binding:4+ce,resource:Le[2*ce]},{binding:8+ce,resource:Le[2*ce+1]});j.group=n.createBindGroup({label:s+"probe",layout:j.bgl,entries:fe}),j.views=Le}const Ge=z.encoder.beginComputePass({label:s+"probe"});Ge.setPipeline(j.pipeline),Ge.setBindGroup(0,j.group),Ge.dispatchWorkgroups(1),Ge.end(),z.encoder.copyBufferToBuffer(j.out,0,ae.buffer,0,V*be);const Fe=ae,Ce={n:se};ae.state="copied",Ce.start=function(){Fe.state="mapping",Fe.buffer.mapAsync(ie).then(function(){if(Fe.state!=="mapping")return;const fe=new Float32Array(Fe.buffer.getMappedRange().slice(0));Fe.buffer.unmap(),Fe.state="free",r(Ce,fe)},function(){Fe.state="free"})},Ce.drop=function(){Fe.state="free"},p(Ce)}catch(ee){L.failed=String(ee&&ee.message||ee)}},dispose:function(){for(const z of j.slots)z.state="disposed",z.buffer.destroy();for(const z of[j.params,j.pts,j.out])z&&z.destroy()}},oe={device:n,ready:!1,layout:w,group:null,disposed:!1,tier:"",update:Ee,dispose:Pe,fail:Oe};function Oe(z){const $="ocean-fft: "+(z&&z.message?z.message:String(z));e.guard&&e.guard.fail("gosx-ocean-fft-surface",$)}function Xe(z){H.runner&&H.runner.dispose();const $=ne.create({module:F,backend:"webgpu",tier:z,host:{device:n,create:e.create,validation:e.validation,nodePose:function(C){return y(_(C),function(){return H.runner})}}});$.waterGen=++te,H.runner=$,H.tier=z,H.last=null,H.viewList=null,oe.group=null,oe.ready=!1,H.info=$.graph?de($.graph):null}function ge(){const z=H.runner,$=[],C=[{binding:D.SLOT.uniform,resource:{buffer:A}},{binding:D.SLOT.sampler,resource:X}];for(let ee=0;ee<t;ee++){const ze=z.export("disp"+ee),xe=z.export("slope"+ee);C.push({binding:D.SLOT.disp+ee,resource:ze?ze.view:pe},{binding:D.SLOT.slope+ee,resource:xe?xe.view:pe}),$.push(ze?ze.view:pe,xe?xe.view:pe)}const se=z.export("foam");C.push({binding:D.SLOT.foam,resource:se?se.view:pe}),$.push(se?se.view:pe);const ae=z.export("wake");C.push({binding:D.SLOT.wake,resource:ae?ae.view:pe}),$.push(ae?ae.view:pe),!(H.viewList&&$.every(function(ee,ze){return ee===H.viewList[ze]}))&&(H.viewList=$,oe.group=n.createBindGroup({label:s+"surface",layout:w,entries:C}))}function Ee(z){const $=U(z.tier);L.tick=++S,(!H.runner||$!==H.tier)&&Xe($);const C=H.runner;if(C.failed)return oe.ready=!1,oe.group=null,!1;ke(C,z.ocean),he.poll();const se=ve.collect.slice(0,V),ae=l(C,se),ee=C.run(Object.assign(P(H,z),{encoder:z.encoder}));if(C.failed)return oe.ready=!1,oe.group=null,g=null,e.guard&&e.guard.fail("gosx-elio-ocean","ocean graph failed: "+C.failed),!1;if(!ee)return oe.ready;const ze=ae?C.export("probe"):null;return ze&&ze.buffer&&he.copy(z.encoder,ze.buffer,se),ge(),O(C,H.info,q,z.ocean,z.camera),n.queue.writeBuffer(A,0,q),ye(C),m=C,W.update(z,C,H.info),oe.ready=!0,!0}function Pe(){oe.disposed||(oe.disposed=!0,oe.ready=!1,oe.group=null,H.runner&&H.runner.dispose(),g=null,m=null,ve.latest=ve.previous=null,I(),he.dispose(),W.dispose(),A.destroy(),Q.destroy())}return Object.defineProperty(oe,"runner",{get:function(){return H.runner}}),oe}function N(e,n){const s=n.environment&&n.environment.ocean;let u=e.fft;const w=n.guard&&(n.guard.disabled.has("oceanFFT")||n.guard.disabled.has("elioGraph"));return!s||!s.spectrum||w||u&&u.device!==n.device?(u&&(u.dispose(),e.fft=null),null):(u||(u=e.fft=d(n)),u.update({ocean:s,tier:n.tier,time:n.time,camera:n.camera,encoder:n.encoder}),u)}const Y=[{match:"^wake\\.",every:2,hold:"^(wake|camera[XZ])"}];function J(){return typeof window<"u"&&window.__gosx_elio_diagnostics||{}}function M(){let e=`#version 300 es
precision highp float;
precision highp sampler2D;
uniform vec4 pts[`+V+`], cs[4]; uniform float level;
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
`,e}function _e(){let e=`struct E { s: vec3<f32>, dx: vec3<f32>, dz: vec3<f32> };
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
  let t = pts[i].xy; var q = t; var e: E;
`,e+=`  for (var k = 0; k < 8; k = k + 1) {
    e = ev(q); let ex = q.x + e.s.x - t.x; let ez = q.y + e.s.z - t.y; let det = e.dx.x * e.dz.z - e.dz.x * e.dx.z;
`,e+=`    if (abs(det) > 1e-4) { q = q - vec2<f32>(e.dz.z * ex - e.dz.x * ez, e.dx.x * ez - e.dx.z * ex) / det; } else { q = q - vec2<f32>(ex, ez) * 0.5; }
  }
`,e+=`  e = ev(q); let n = normalize(cross(e.dz, e.dx)); outp[i] = vec4<f32>(P0.level + e.s.y, n.x, n.z, 1.0);
}
`,e}const Se=`#version 300 es
void main() { vec2 p = vec2(float((gl_VertexID << 1) & 2), float(gl_VertexID & 2)); gl_Position = vec4(p * 2. - 1., 0., 1.); }`;function Ze(e,n){const s={last:null},u=ne.create({module:F,backend:"webgl2",tier:U(n.tier),host:{gl:e,caps:n.caps,textureCache:n.textureCache,profile:n.profile||J().profile,throttle:n.throttle||J().throttle||Y,nodePose:function(W){return y(_(W),function(){return Q.runner})}}}),w=new Float32Array(we),X=new Map;u.waterGen=++te;const A=u.graph?de(u.graph):null,Q={gl:e,runner:u,tier:U(n.tier),disposed:!1,update:pe,bind:he,dispose:j,fail:function(W){u.fail(W)}};function pe(W){return u.failed||(L.tick=++S,ke(u,W.ocean),!u.run(P(s,W)))?!1:(O(u,A,w,W.ocean,W.camera),ye(u),H(W),!0)}const q={program:null,fbo:null,tex:null,slots:[]};function H(W){for(const z of q.slots){if(z.state!=="fenced")continue;const $=e.clientWaitSync(z.sync,0,0);if(!($!==e.ALREADY_SIGNALED&&$!==e.CONDITION_SATISFIED&&$!==e.WAIT_FAILED)){if(z.state="free",e.deleteSync(z.sync),z.sync=null,$===e.WAIT_FAILED){L.failed="probe-fence";continue}z.meta.seq!==void 0&&(e.bindBuffer(e.PIXEL_PACK_BUFFER,z.pbo),e.getBufferSubData(e.PIXEL_PACK_BUFFER,0,z.dst),e.bindBuffer(e.PIXEL_PACK_BUFFER,null),r(z.meta,z.dst))}}const oe=K();if(!oe||L.failed)return;const Oe=q.slots.find(function(z){return z.state==="free"})||(q.slots.length<R?null:void 0);if(Oe===void 0)return;const Xe=ne.glState.save(e),ge=A.cascades,Ee=u.exportParams(),Pe=Number(W.ocean&&W.ocean.level)||0;try{if(!q.program){const C=[],se=function(ee,ze){const xe=e.createShader(ee);if(C.push(xe),e.shaderSource(xe,ze),e.compileShader(xe),!e.getShaderParameter(xe,e.COMPILE_STATUS))throw new Error("probe-compile");return xe},ae=e.createProgram();try{if(e.attachShader(ae,se(e.VERTEX_SHADER,Se)),e.attachShader(ae,se(e.FRAGMENT_SHADER,M())),e.linkProgram(ae),!e.getProgramParameter(ae,e.LINK_STATUS))throw new Error("probe-link")}catch(ee){throw e.deleteProgram(ae),ee}finally{for(const ee of C)e.deleteShader(ee)}if(q.program=ae,q.tex=e.createTexture(),e.bindTexture(e.TEXTURE_2D,q.tex),e.texStorage2D(e.TEXTURE_2D,1,e.RGBA32F,V,1),q.fbo=e.createFramebuffer(),e.bindFramebuffer(e.FRAMEBUFFER,q.fbo),e.framebufferTexture2D(e.FRAMEBUFFER,e.COLOR_ATTACHMENT0,e.TEXTURE_2D,q.tex,0),e.checkFramebufferStatus(e.FRAMEBUFFER)!==e.FRAMEBUFFER_COMPLETE)throw new Error("probe-target");e.useProgram(ae);for(let ee=0;ee<4;ee++)e.uniform1i(e.getUniformLocation(ae,"d"+ee),G.UNIT.disp+ee),e.uniform1i(e.getUniformLocation(ae,"s"+ee),G.UNIT.slope+ee);q.loc={pts:e.getUniformLocation(ae,"pts[0]"),cs:e.getUniformLocation(ae,"cs[0]"),level:e.getUniformLocation(ae,"level")},q.pts=new Float32Array(V*4),q.cs=new Float32Array(16)}let z=Oe;z||(z={state:"free",pbo:e.createBuffer(),dst:new Float32Array(V*4),sync:null,meta:null},e.bindBuffer(e.PIXEL_PACK_BUFFER,z.pbo),e.bufferData(e.PIXEL_PACK_BUFFER,V*be,e.STREAM_READ),q.slots.push(z));const $=L.arr;q.pts.fill(0),q.cs.fill(0);for(let C=0;C<oe;C++)q.pts[C*4]=$[E.PTS+2*C],q.pts[C*4+1]=$[E.PTS+2*C+1];ge.forEach(function(C,se){q.cs[se*4]=C.tile,q.cs[se*4+1]=1,q.cs[se*4+2]=Ee["waterModX"+se]||0,q.cs[se*4+3]=Ee["waterModZ"+se]||0}),e.bindFramebuffer(e.FRAMEBUFFER,q.fbo),e.viewport(0,0,oe,1),e.disable(e.BLEND),e.disable(e.DEPTH_TEST),e.disable(e.CULL_FACE),e.disable(e.SCISSOR_TEST),e.colorMask(!0,!0,!0,!0),e.useProgram(q.program),e.uniform4fv(q.loc.pts,q.pts),e.uniform4fv(q.loc.cs,q.cs),e.uniform1f(q.loc.level,Pe);for(let C=0;C<4;C++){const se=u.export("disp"+C),ae=u.export("slope"+C);e.activeTexture(e.TEXTURE0+G.UNIT.disp+C),e.bindTexture(e.TEXTURE_2D,se?se.tex:null),e.activeTexture(e.TEXTURE0+G.UNIT.slope+C),e.bindTexture(e.TEXTURE_2D,ae?ae.tex:null)}e.drawArrays(e.TRIANGLES,0,3),e.bindBuffer(e.PIXEL_PACK_BUFFER,z.pbo),e.readPixels(0,0,oe,1,e.RGBA,e.FLOAT,0),e.bindBuffer(e.PIXEL_PACK_BUFFER,null),z.sync=e.fenceSync(e.SYNC_GPU_COMMANDS_COMPLETE,0),e.flush(),z.state="fenced",z.meta={n:oe},p(z.meta)}catch(z){L.failed=String(z&&z.message||z)}finally{ne.glState.restore(e,Xe)}}function he(W,oe){let Oe=X.get(oe);if(!Oe){const ge=function(Ee){return W.getUniformLocation(oe,Ee)};Oe={sp:ge("u_sp[0]"),disp:[0,1,2,3].map(function(Ee){return ge("u_spDisp"+Ee)}),slope:[0,1,2,3].map(function(Ee){return ge("u_spSlope"+Ee)}),foam:ge("u_spFoam"),wake:ge("u_spWake")},X.set(oe,Oe)}const Xe=function(ge,Ee,Pe){W.activeTexture(W.TEXTURE0+ge),W.bindTexture(W.TEXTURE_2D,Pe?Pe.tex:Q.dummy),W.uniform1i(Ee,ge)};Q.dummy||(Q.dummy=W.createTexture(),W.bindTexture(W.TEXTURE_2D,Q.dummy),W.texStorage2D(W.TEXTURE_2D,1,W.RGBA16F,1,1),W.texParameteri(W.TEXTURE_2D,W.TEXTURE_MIN_FILTER,W.LINEAR),W.texParameteri(W.TEXTURE_2D,W.TEXTURE_MAG_FILTER,W.LINEAR));for(let ge=0;ge<t;ge++)Xe(G.UNIT.disp+ge,Oe.disp[ge],u.export("disp"+ge)),Xe(G.UNIT.slope+ge,Oe.slope[ge],u.export("slope"+ge));Xe(G.UNIT.foam,Oe.foam,u.export("foam")),Xe(G.UNIT.wake,Oe.wake,u.export("wake")),W.uniform4fv(Oe.sp,w)}function j(){if(!Q.disposed){Q.disposed=!0,g=null,I();for(const W of q.slots)W.sync&&e.deleteSync(W.sync),e.deleteBuffer(W.pbo);q.program&&(e.deleteProgram(q.program),e.deleteFramebuffer(q.fbo),e.deleteTexture(q.tex)),u.dispose(),Q.dummy&&e.deleteTexture(Q.dummy)}}return Q}function Re(e,n,s){let u=e.fft;if(u&&(u.gl!==n||u.disposed||u.tier!==U(s.tier))&&(u.dispose(),u=e.fft=null),e.fftDisabled)return null;if(n.isContextLost&&n.isContextLost())return g=null,null;u||(u=e.fft=Ze(n,s));const w=u.update({ocean:s.ocean,time:s.time,camera:s.camera});return u.runner.failed?(e.fftDisabled=u.runner.failed,u.dispose(),e.fft=null,g=null,s.mount&&s.mount.setAttribute&&s.mount.setAttribute("data-gosx-scene3d-ocean-fft-fallback",e.fftDisabled),null):w?u:null}function Ie(e,n,s){e.fft&&e.fft.dispose(),e.fft=null,e.fftDisabled=s,g=null,n&&n.mount&&n.mount.setAttribute&&n.mount.setAttribute("data-gosx-scene3d-ocean-fft-fallback",s)}window.__gosx_scene3d_ocean_fft_api={frame:N,water:h,fill:Te,timing:Z,nodePose:_,waterRelative:y,sampler:k,probe:{begin:Me,sample:De},probeState:ve,describe:de,WAKE_DEBUG_RANGE:re,feed:ke,surfaceData:O},window.__gosx_scene3d_ocean_fft_gl_api={run:Re,fail:Ie}})();
