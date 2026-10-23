(function(){"use strict";function N(C){if(C&&typeof C=="object"){for(const P of Object.keys(C))N(C[P]);Object.freeze(C)}return C}window.__gosx_scene3d_elio_ocean=N({elio:"v0.4.3+ocean",set:"ocean",sha256:"17730dd3cd0d3abad52c12d545e6d673a1c1ff7b508dbc3446503ca469189d47",kernels:{oceanSpectrum:{wgsl:`struct OceanSpectrumParams {
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
                turb = (turb + (wgt * wakeIn[u32(((bv * ww) + bu))].z));
              }
            }
          }
        }
      }
      wf = clamp((foam.wakeGain * turb), 0.0, 1.0);
      f = max(f, wf);
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
uniform highp sampler2D wakeIn_tex;
uniform uint wakeIn_len;

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

vec4 wakeIn_at(uint i) {
  return texelFetch(wakeIn_tex, ivec2(int(i % elio_width), int(i / elio_width)), 0);
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
                turb = (turb + (wgt * wakeIn_at(uint(((bv * ww) + bu))).z));
              }
            }
          }
        }
      }
      wf = clamp((foam.wakeGain * turb), 0.0, 1.0);
      f = max(f, wf);
    }
  }
  foamOut_out = vec4(f, 0.0, 0.0, 0.0);
  foamTex_out = vec4(f, jac, wf, 1.0);
}
`,gatherLayout:{kernel:"ocean_foam",inputs:[{name:"foamIn",kind:"buffer",elem:"f32",sampler:"foamIn_tex",length:"foamIn_len",binding:1},{name:"fieldB0",kind:"buffer",elem:"vec4",sampler:"fieldB0_tex",length:"fieldB0_len",binding:2},{name:"fieldB1",kind:"buffer",elem:"vec4",sampler:"fieldB1_tex",length:"fieldB1_len",binding:3},{name:"fieldB2",kind:"buffer",elem:"vec4",sampler:"fieldB2_tex",length:"fieldB2_len",binding:4},{name:"fieldB3",kind:"buffer",elem:"vec4",sampler:"fieldB3_tex",length:"fieldB3_len",binding:5},{name:"wakeIn",kind:"buffer",elem:"vec4",sampler:"wakeIn_tex",length:"wakeIn_len",binding:8}],outputs:[{name:"foamOut",elem:"f32",format:"",location:0,variable:"foamOut_out"},{name:"foamTex",elem:"vec4",format:"rgba16float",location:1,variable:"foamTex_out"}],uniforms:[{name:"foam",block:"OceanFoamParams_ubo",struct:"OceanFoamParams",binding:0}]}},oceanProbe:{wgsl:`struct OceanProbeParams {
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
  _pad2 : f32,
  _pad3 : f32,
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
  let qs = (((along / (0.5 * wake.srcLength)) * (along / (0.5 * wake.srcLength))) + ((across / (0.5 * wake.srcBeam)) * (across / (0.5 * wake.srcBeam))));
  var head = 0.0;
  if ((qs < 1.0)) {
    let fade = clamp((wake.speed / wake.fullSpeed), 0.0, 1.0);
    head = ((((wake.depth * fade) * fade) * (1.0 - qs)) * (1.0 - qs));
  }
  let uh = (along / (0.5 * wake.hullLen));
  let vh = (across / (0.5 * wake.hullBeam));
  let qh = ((uh * uh) + (vh * vh));
  var inj = 0.0;
  if ((qh < 1.0)) {
    inj = (((0.8 * (1.0 - qh)) * (1.0 - qh)) * (0.5 - (0.5 * clamp(uh, -1.0, 1.0))));
  }
  let bu = ((uh - 0.85) / 0.25);
  let bv = (vh / 0.45);
  let qb = ((bu * bu) + (bv * bv));
  if ((qb < 1.0)) {
    inj = (inj + ((0.6 * (1.0 - qb)) * (1.0 - qb)));
  }
  inj = min((inj * clamp((wake.speed / wake.foamSpeed), 0.0, 1.0)), 1.0);
  let turb = max((st.z * exp((0.0 - (wake.dt / wake.tau)))), inj);
  fftOut[i] = vec4<f32>((st.x + head), 0.0, 0.0, 0.0);
  cleanOut[i] = vec4<f32>(st.x, st.y, turb, 0.0);
}
`,entry:"wake_source",workgroup:[64,1,1],bindings:[{group:0,binding:0,name:"wake",kind:"uniform",elementType:"WakeSourceParams"},{group:0,binding:1,name:"wakeIn",kind:"storage-read",elementType:"vec4<f32>"},{group:0,binding:2,name:"fftOut",kind:"storage-rw",elementType:"vec4<f32>"},{group:0,binding:3,name:"cleanOut",kind:"storage-rw",elementType:"vec4<f32>"}],uniforms:[{group:0,binding:0,name:"wake",struct:"WakeSourceParams",size:96,fields:[{name:"width",type:"u32",offset:0,size:4},{name:"reset",type:"u32",offset:4,size:4},{name:"_pad0",type:"u32",offset:8,size:4},{name:"_pad1",type:"u32",offset:12,size:4},{name:"originX",type:"i32",offset:16,size:4},{name:"originZ",type:"i32",offset:20,size:4},{name:"shiftX",type:"i32",offset:24,size:4},{name:"shiftZ",type:"i32",offset:28,size:4},{name:"cell",type:"f32",offset:32,size:4},{name:"dt",type:"f32",offset:36,size:4},{name:"tau",type:"f32",offset:40,size:4},{name:"depth",type:"f32",offset:44,size:4},{name:"hullU",type:"f32",offset:48,size:4},{name:"hullV",type:"f32",offset:52,size:4},{name:"heading",type:"f32",offset:56,size:4},{name:"speed",type:"f32",offset:60,size:4},{name:"hullLen",type:"f32",offset:64,size:4},{name:"hullBeam",type:"f32",offset:68,size:4},{name:"srcLength",type:"f32",offset:72,size:4},{name:"srcBeam",type:"f32",offset:76,size:4},{name:"fullSpeed",type:"f32",offset:80,size:4},{name:"foamSpeed",type:"f32",offset:84,size:4},{name:"_pad2",type:"f32",offset:88,size:4},{name:"_pad3",type:"f32",offset:92,size:4}]}],glsl:`#version 300 es
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
  float _pad2;
  float _pad3;
} wake;
uniform highp sampler2D wakeIn_tex;
uniform uint wakeIn_len;
layout(location = 0) out vec4 fftOut_out;
layout(location = 1) out vec4 cleanOut_out;

vec4 wakeIn_at(uint i) {
  return texelFetch(wakeIn_tex, ivec2(int(i % elio_width), int(i / elio_width)), 0);
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
  float qs = (((along / (0.5 * wake.srcLength)) * (along / (0.5 * wake.srcLength))) + ((across / (0.5 * wake.srcBeam)) * (across / (0.5 * wake.srcBeam))));
  float head = 0.0;
  if ((qs < 1.0)) {
    float fade = clamp((wake.speed / wake.fullSpeed), 0.0, 1.0);
    head = ((((wake.depth * fade) * fade) * (1.0 - qs)) * (1.0 - qs));
  }
  float uh = (along / (0.5 * wake.hullLen));
  float vh = (across / (0.5 * wake.hullBeam));
  float qh = ((uh * uh) + (vh * vh));
  float inj = 0.0;
  if ((qh < 1.0)) {
    inj = (((0.8 * (1.0 - qh)) * (1.0 - qh)) * (0.5 - (0.5 * clamp(uh, -1.0, 1.0))));
  }
  float bu = ((uh - 0.85) / 0.25);
  float bv = (vh / 0.45);
  float qb = ((bu * bu) + (bv * bv));
  if ((qb < 1.0)) {
    inj = (inj + ((0.6 * (1.0 - qb)) * (1.0 - qb)));
  }
  inj = min((inj * clamp((wake.speed / wake.foamSpeed), 0.0, 1.0)), 1.0);
  float turb = max((st.z * exp((0.0 - (wake.dt / wake.tau)))), inj);
  fftOut_out = vec4((st.x + head), 0.0, 0.0, 0.0);
  cleanOut_out = vec4(st.x, st.y, turb, 0.0);
}
`,gatherLayout:{kernel:"wake_source",inputs:[{name:"wakeIn",kind:"buffer",elem:"vec4",sampler:"wakeIn_tex",length:"wakeIn_len",binding:1}],outputs:[{name:"fftOut",elem:"vec4",format:"",location:0,variable:"fftOut_out"},{name:"cleanOut",elem:"vec4",format:"",location:1,variable:"cleanOut_out"}],uniforms:[{name:"wake",block:"WakeSourceParams_ubo",struct:"WakeSourceParams",binding:0}]}},wakeSymbol:{wgsl:`struct WakeSymbolParams {
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
layout(location = 0) out vec4 fftOut_out;

vec4 fftIn_at(uint i) {
  return texelFetch(fftIn_tex, ivec2(int(i % elio_width), int(i / elio_width)), 0);
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
`,gatherLayout:{kernel:"wake_symbol",inputs:[{name:"fftIn",kind:"buffer",elem:"vec4",sampler:"fftIn_tex",length:"fftIn_len",binding:1}],outputs:[{name:"fftOut",elem:"vec4",format:"",location:0,variable:"fftOut_out"}],uniforms:[{name:"sym",block:"WakeSymbolParams_ubo",struct:"WakeSymbolParams",binding:0}]}},wakeStep:{wgsl:`struct WakeStepParams {
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
  stateOut[i] = vec4<f32>(h0, own.x, own.z, 0.0);
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
uniform highp sampler2D cleanIn_tex;
uniform uint cleanIn_len;
layout(location = 0) out vec4 stateOut_out;
layout(location = 1) out vec4 wakeTex_out;

vec4 qIn_at(uint i) {
  return texelFetch(qIn_tex, ivec2(int(i % elio_width), int(i / elio_width)), 0);
}

vec4 cleanIn_at(uint i) {
  return texelFetch(cleanIn_tex, ivec2(int(i % elio_width), int(i / elio_width)), 0);
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
  stateOut_out = vec4(h0, own.x, own.z, 0.0);
  wakeTex_out = vec4(h0, ((h1 - h2) / (2.0 * wstep.cell)), ((h3 - h4) / (2.0 * wstep.cell)), own.z);
}
`,gatherLayout:{kernel:"wake_step",inputs:[{name:"qIn",kind:"buffer",elem:"vec4",sampler:"qIn_tex",length:"qIn_len",binding:1},{name:"cleanIn",kind:"buffer",elem:"vec4",sampler:"cleanIn_tex",length:"cleanIn_len",binding:2}],outputs:[{name:"stateOut",elem:"vec4",format:"",location:0,variable:"stateOut_out"},{name:"wakeTex",elem:"vec4",format:"rgba16float",location:1,variable:"wakeTex_out"}],uniforms:[{name:"wstep",block:"WakeStepParams_ubo",struct:"WakeStepParams",binding:0}]}},fftCompute64:{wgsl:`@group(0) @binding(0) var<storage, read_write> data : array<vec4<f32>>;

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
`,gatherLayout:{kernel:"fft_stage",inputs:[{name:"src",kind:"buffer",elem:"vec4",sampler:"src_tex",length:"src_len",binding:1}],outputs:[{name:"dst",elem:"vec4",format:"",location:0,variable:"dst_out"}],uniforms:[{name:"fft",block:"FFTStage_ubo",struct:"FFTStage",binding:0}]}}},graphs:{"webgpu/high":{name:"ocean",backend:"webgpu",tier:"high",version:1,params:[{name:"windSpeed",type:"f32",source:{kind:"value",value:9},tween:20},{name:"fetch",type:"f32",source:{kind:"value",value:300},tween:20},{name:"gamma",type:"f32",source:{kind:"value",value:3.3},tween:20},{name:"spread",type:"f32",source:{kind:"value",value:8},tween:20},{name:"swell",type:"f32",source:{kind:"value",value:0},tween:20},{name:"windDirDeg",type:"f32",source:{kind:"value",value:0},tween:20},{name:"swellDirDeg",type:"f32",source:{kind:"value",value:0},tween:20},{name:"seed",type:"u32",source:{kind:"value",value:0}},{name:"loopPeriod",type:"f32",source:{kind:"value",value:1e3}},{name:"lambda",type:"f32",source:{kind:"value",value:1},tween:20},{name:"timeScale",type:"f32",source:{kind:"value",value:1}},{name:"driftX",type:"f32",source:{kind:"value",value:0}},{name:"driftZ",type:"f32",source:{kind:"value",value:0}},{name:"foamTau",type:"f32",source:{kind:"value",value:4}},{name:"foamThreshold",type:"f32",source:{kind:"value",value:.9}},{name:"foamGain",type:"f32",source:{kind:"value",value:2}},{name:"probeCount",type:"u32",source:{kind:"value",value:0}},{name:"probeOriginX",type:"f32",source:{kind:"value",value:0}},{name:"probeOriginZ",type:"f32",source:{kind:"value",value:0}},{name:"probePoints",type:"vec4<f32>[]",count:64,source:{kind:"value",value:0}},{name:"dt",type:"f32",source:{kind:"frame",name:"dt"}},{name:"cameraX",type:"f32",source:{kind:"frame",name:"cameraX"}},{name:"cameraZ",type:"f32",source:{kind:"frame",name:"cameraZ"}},{name:"windDir",type:"f32",source:{args:[1.5707963267948966,{op:"mul",args:["param:windDirDeg",.017453292519943295]}],kind:"expr",op:"sub"}},{name:"swellDir",type:"f32",source:{args:[1.5707963267948966,{op:"mul",args:["param:swellDirDeg",.017453292519943295]}],kind:"expr",op:"sub"}},{name:"simTime",type:"f32",source:{kind:"accumulate",of:"param:timeScale"}},{name:"phaseTime",type:"f32",source:{args:["param:simTime","param:loopPeriod"],kind:"expr",op:"mod"}},{name:"waterX",type:"f32",source:{kind:"accumulate",of:"param:driftX"}},{name:"waterZ",type:"f32",source:{kind:"accumulate",of:"param:driftZ"}},{name:"waterModX0",type:"f32",source:{args:["param:waterX",1600],kind:"expr",op:"mod"}},{name:"waterModZ0",type:"f32",source:{args:["param:waterZ",1600],kind:"expr",op:"mod"}},{name:"waterModX1",type:"f32",source:{args:["param:waterX",250],kind:"expr",op:"mod"}},{name:"waterModZ1",type:"f32",source:{args:["param:waterZ",250],kind:"expr",op:"mod"}},{name:"waterModX2",type:"f32",source:{args:["param:waterX",37],kind:"expr",op:"mod"}},{name:"waterModZ2",type:"f32",source:{args:["param:waterZ",37],kind:"expr",op:"mod"}},{name:"waterModX3",type:"f32",source:{args:["param:waterX",5.3],kind:"expr",op:"mod"}},{name:"waterModZ3",type:"f32",source:{args:["param:waterZ",5.3],kind:"expr",op:"mod"}},{name:"foamOriginX",type:"i32",source:{args:[{op:"floor",args:[{op:"div",args:[{op:"sub",args:["param:cameraX","param:waterX"]},.5]}]},512],kind:"expr",op:"sub"}},{name:"foamShiftX",type:"i32",source:{kind:"delta",of:"param:foamOriginX"}},{name:"foamOriginMod0X",type:"f32",source:{args:[{op:"mul",args:["param:foamOriginX",.5]},1600],kind:"expr",op:"mod"}},{name:"foamOriginMod1X",type:"f32",source:{args:[{op:"mul",args:["param:foamOriginX",.5]},250],kind:"expr",op:"mod"}},{name:"foamOriginMod2X",type:"f32",source:{args:[{op:"mul",args:["param:foamOriginX",.5]},37],kind:"expr",op:"mod"}},{name:"foamOriginMod3X",type:"f32",source:{args:[{op:"mul",args:["param:foamOriginX",.5]},5.3],kind:"expr",op:"mod"}},{name:"probeOriginMod0X",type:"f32",source:{args:[{op:"sub",args:["param:probeOriginX","param:waterX"]},1600],kind:"expr",op:"mod"}},{name:"probeOriginMod1X",type:"f32",source:{args:[{op:"sub",args:["param:probeOriginX","param:waterX"]},250],kind:"expr",op:"mod"}},{name:"probeOriginMod2X",type:"f32",source:{args:[{op:"sub",args:["param:probeOriginX","param:waterX"]},37],kind:"expr",op:"mod"}},{name:"probeOriginMod3X",type:"f32",source:{args:[{op:"sub",args:["param:probeOriginX","param:waterX"]},5.3],kind:"expr",op:"mod"}},{name:"foamOriginZ",type:"i32",source:{args:[{op:"floor",args:[{op:"div",args:[{op:"sub",args:["param:cameraZ","param:waterZ"]},.5]}]},512],kind:"expr",op:"sub"}},{name:"foamShiftZ",type:"i32",source:{kind:"delta",of:"param:foamOriginZ"}},{name:"foamOriginMod0Z",type:"f32",source:{args:[{op:"mul",args:["param:foamOriginZ",.5]},1600],kind:"expr",op:"mod"}},{name:"foamOriginMod1Z",type:"f32",source:{args:[{op:"mul",args:["param:foamOriginZ",.5]},250],kind:"expr",op:"mod"}},{name:"foamOriginMod2Z",type:"f32",source:{args:[{op:"mul",args:["param:foamOriginZ",.5]},37],kind:"expr",op:"mod"}},{name:"foamOriginMod3Z",type:"f32",source:{args:[{op:"mul",args:["param:foamOriginZ",.5]},5.3],kind:"expr",op:"mod"}},{name:"probeOriginMod0Z",type:"f32",source:{args:[{op:"sub",args:["param:probeOriginZ","param:waterZ"]},1600],kind:"expr",op:"mod"}},{name:"probeOriginMod1Z",type:"f32",source:{args:[{op:"sub",args:["param:probeOriginZ","param:waterZ"]},250],kind:"expr",op:"mod"}},{name:"probeOriginMod2Z",type:"f32",source:{args:[{op:"sub",args:["param:probeOriginZ","param:waterZ"]},37],kind:"expr",op:"mod"}},{name:"probeOriginMod3Z",type:"f32",source:{args:[{op:"sub",args:["param:probeOriginZ","param:waterZ"]},5.3],kind:"expr",op:"mod"}},{name:"hullX",type:"f32",source:{kind:"value",value:0}},{name:"hullZ",type:"f32",source:{kind:"value",value:0}},{name:"hullHeading",type:"f32",source:{kind:"value",value:0}},{name:"hullSpeed",type:"f32",source:{kind:"value",value:0}},{name:"wakeEnabled",type:"bool",source:{kind:"value",value:0}},{name:"hullLength",type:"f32",source:{kind:"value",value:200}},{name:"hullBeam",type:"f32",source:{kind:"value",value:30}},{name:"wakeCell",type:"f32",source:{kind:"value",value:4}},{name:"wakeDepth",type:"f32",source:{kind:"value",value:.4}},{name:"wakeGravity",type:"f32",source:{kind:"value",value:9.81}},{name:"wakeAlpha",type:"f32",source:{kind:"value",value:.02}},{name:"wakeMinLambdaCells",type:"f32",source:{kind:"value",value:8}},{name:"wakeFoamGain",type:"f32",source:{kind:"value",value:1}},{name:"wakeFoamTau",type:"f32",source:{kind:"value",value:45}},{name:"wakeOn",type:"f32",source:{args:["param:wakeEnabled",1],kind:"expr",op:"mul"}},{name:"wakeFoamGainEff",type:"f32",source:{args:["param:wakeEnabled","param:wakeFoamGain"],kind:"expr",op:"mul"}},{name:"wakeEdge",type:"i32",source:{kind:"delta",of:"param:wakeEnabled"}},{name:"wakeReset",type:"u32",source:{args:["param:wakeEdge",0],kind:"expr",op:"max"}},{name:"wakeLength",type:"f32",source:{args:["param:wakeCell",256],kind:"expr",op:"mul"}},{name:"hullWaterX",type:"f32",source:{args:["param:hullX","param:waterX"],kind:"expr",op:"sub"}},{name:"wakeOriginX",type:"i32",source:{args:[{op:"div",args:[{op:"sub",args:[{op:"sub",args:["param:hullWaterX",{op:"mul",args:[{op:"sin",args:["param:hullHeading"]},{op:"mul",args:["param:wakeCell",51.2]}]}]},{op:"mul",args:["param:wakeCell",128]}]},"param:wakeCell"]}],kind:"expr",op:"floor"}},{name:"wakeShiftX",type:"i32",source:{kind:"delta",of:"param:wakeOriginX"}},{name:"wakeHullU",type:"f32",source:{args:["param:hullWaterX",{op:"mul",args:["param:wakeOriginX","param:wakeCell"]}],kind:"expr",op:"sub"}},{name:"wakeOriginModX",type:"i32",source:{args:["param:wakeOriginX",256],kind:"expr",op:"mod"}},{name:"wakeRelX",type:"f32",source:{args:[{op:"mul",args:["param:foamOriginX",.5]},{op:"mul",args:["param:wakeOriginX","param:wakeCell"]}],kind:"expr",op:"sub"}},{name:"wakeCamRelX",type:"f32",source:{args:[{op:"add",args:["param:waterX",{op:"mul",args:["param:wakeOriginX","param:wakeCell"]}]},"param:cameraX"],kind:"expr",op:"sub"}},{name:"hullWaterZ",type:"f32",source:{args:["param:hullZ","param:waterZ"],kind:"expr",op:"sub"}},{name:"wakeOriginZ",type:"i32",source:{args:[{op:"div",args:[{op:"sub",args:[{op:"sub",args:["param:hullWaterZ",{op:"mul",args:[{op:"cos",args:["param:hullHeading"]},{op:"mul",args:["param:wakeCell",51.2]}]}]},{op:"mul",args:["param:wakeCell",128]}]},"param:wakeCell"]}],kind:"expr",op:"floor"}},{name:"wakeShiftZ",type:"i32",source:{kind:"delta",of:"param:wakeOriginZ"}},{name:"wakeHullV",type:"f32",source:{args:["param:hullWaterZ",{op:"mul",args:["param:wakeOriginZ","param:wakeCell"]}],kind:"expr",op:"sub"}},{name:"wakeOriginModZ",type:"i32",source:{args:["param:wakeOriginZ",256],kind:"expr",op:"mod"}},{name:"wakeRelZ",type:"f32",source:{args:[{op:"mul",args:["param:foamOriginZ",.5]},{op:"mul",args:["param:wakeOriginZ","param:wakeCell"]}],kind:"expr",op:"sub"}},{name:"wakeCamRelZ",type:"f32",source:{args:[{op:"add",args:["param:waterZ",{op:"mul",args:["param:wakeOriginZ","param:wakeCell"]}]},"param:cameraZ"],kind:"expr",op:"sub"}},{name:"wakeGEff",type:"f32",source:{args:[{op:"min",args:[{op:"min",args:["param:wakeGravity",{op:"mul",args:[{op:"mul",args:["param:hullSpeed","param:hullSpeed"]},{op:"div",args:[6.283185307179586,{op:"mul",args:["param:wakeMinLambdaCells","param:wakeCell"]}]}]}]},{op:"mul",args:["param:wakeCell",16.39001041697031]}]},.05],kind:"expr",op:"max"}},{name:"wakeDt",type:"f32",source:{args:[{op:"mul",args:["param:dt","param:timeScale"]},.2],kind:"expr",op:"min"}},{name:"wakeSrcLength",type:"f32",source:{args:[{op:"min",args:["param:hullLength",{op:"div",args:[{op:"mul",args:[{op:"mul",args:["param:hullSpeed","param:hullSpeed"]},3.141592653589793]},"param:wakeGEff"]}]},{op:"mul",args:["param:wakeCell",2]}],kind:"expr",op:"max"}},{name:"wakeSrcBeam",type:"f32",source:{args:[{op:"min",args:["param:hullBeam",{op:"mul",args:["param:wakeSrcLength",.67]}]},{op:"mul",args:["param:wakeCell",1.5]}],kind:"expr",op:"max"}}],resources:[{name:"h0_0",kind:"buffer",elementType:"vec4<f32>",count:4096},{name:"omega_0",kind:"buffer",elementType:"f32",count:4096},{name:"fftA_0",kind:"buffer",elementType:"vec4<f32>",count:4096},{name:"fftB_0",kind:"buffer",elementType:"vec4<f32>",count:4096},{name:"dispTex0",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"slopeTex0",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"h0_1",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"omega_1",kind:"buffer",elementType:"f32",count:65536},{name:"fftA_1",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"fftB_1",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"dispTex1",kind:"texture",format:"rgba16float",width:256,height:256,mipLevels:9},{name:"slopeTex1",kind:"texture",format:"rgba16float",width:256,height:256,mipLevels:9},{name:"h0_2",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"omega_2",kind:"buffer",elementType:"f32",count:65536},{name:"fftA_2",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"fftB_2",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"dispTex2",kind:"texture",format:"rgba16float",width:256,height:256,mipLevels:9},{name:"slopeTex2",kind:"texture",format:"rgba16float",width:256,height:256,mipLevels:9},{name:"h0_3",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"omega_3",kind:"buffer",elementType:"f32",count:65536},{name:"fftA_3",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"fftB_3",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"dispTex3",kind:"texture",format:"rgba16float",width:256,height:256,mipLevels:9},{name:"slopeTex3",kind:"texture",format:"rgba16float",width:256,height:256,mipLevels:9},{name:"wakeA",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"wakeB",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"wakeF",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"wakeG",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"wakeC",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"wakeTex",kind:"texture",format:"rgba16float",width:256,height:256,mipLevels:9},{name:"foamA",kind:"buffer",elementType:"f32",count:1048576},{name:"foamB",kind:"buffer",elementType:"f32",count:1048576},{name:"foamTex",kind:"texture",format:"rgba16float",width:1024,height:1024,mipLevels:11},{name:"probePoints",kind:"buffer",elementType:"vec4<f32>",count:64,feed:"probePoints"},{name:"probeOut",kind:"buffer",elementType:"vec4<f32>",count:64}],passes:[{name:"c0.spectrum",kernel:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",dispatch:[64,1,1],bindings:[{name:"h0",resource:"h0_0"},{name:"omega",resource:"omega_0"}],uniforms:[{name:"spec",values:{cascade:0,fetchKm:"param:fetch",gamma:"param:gamma",kMax:.12,kMin:.003926990816987242,loopPeriod:"param:loopPeriod",n:64,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:1600,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c0.evolve",kernel:"ocean_evolve",dispatch:[64,1,1],bindings:[{name:"h0",resource:"h0_0"},{name:"omega",resource:"omega_0"},{name:"outA",resource:"fftA_0"},{name:"outB",resource:"fftB_0"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:64,t:"param:phaseTime",tile:1600}}]},{name:"c0.fftA.fft_rows",kernel:"fft_compute_64",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftA_0"}]},{name:"c0.fftA.fft_cols",kernel:"fft_compute_64",entry:"fft_cols",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftA_0"}]},{name:"c0.fftB.fft_rows",kernel:"fft_compute_64",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftB_0"}]},{name:"c0.fftB.fft_cols",kernel:"fft_compute_64",entry:"fft_cols",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftB_0"}]},{name:"c0.assemble",kernel:"ocean_assemble",dispatch:[64,1,1],bindings:[{name:"fieldA",resource:"fftA_0"},{name:"fieldB",resource:"fftB_0"},{name:"disp",resource:"dispTex0"},{name:"slope",resource:"slopeTex0"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:64}}]},{name:"c0.dispTex0.mips",type:"repeat",count:6,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[16,1,1],bindings:[{name:"src",resource:"dispTex0",levelAt:{index:"l"}},{name:"dst",resource:"dispTex0",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:64},dstWidth:{index:"l",offset:1,shr:64},pad:0,srcLevel:0}}]}]},{name:"c0.slopeTex0.mips",type:"repeat",count:6,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[16,1,1],bindings:[{name:"src",resource:"slopeTex0",levelAt:{index:"l"}},{name:"dst",resource:"slopeTex0",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:64},dstWidth:{index:"l",offset:1,shr:64},pad:0,srcLevel:0}}]}]},{name:"c1.spectrum",kernel:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",dispatch:[1024,1,1],bindings:[{name:"h0",resource:"h0_1"},{name:"omega",resource:"omega_1"}],uniforms:[{name:"spec",values:{cascade:1,fetchKm:"param:fetch",gamma:"param:gamma",kMax:.8,kMin:.12,loopPeriod:"param:loopPeriod",n:256,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:250,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c1.evolve",kernel:"ocean_evolve",dispatch:[1024,1,1],bindings:[{name:"h0",resource:"h0_1"},{name:"omega",resource:"omega_1"},{name:"outA",resource:"fftA_1"},{name:"outB",resource:"fftB_1"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:256,t:"param:phaseTime",tile:250}}]},{name:"c1.fftA.fft_rows",kernel:"fft_compute_256",dispatch:[256,1,1],bindings:[{name:"data",resource:"fftA_1"}]},{name:"c1.fftA.fft_cols",kernel:"fft_compute_256",entry:"fft_cols",dispatch:[256,1,1],bindings:[{name:"data",resource:"fftA_1"}]},{name:"c1.fftB.fft_rows",kernel:"fft_compute_256",dispatch:[256,1,1],bindings:[{name:"data",resource:"fftB_1"}]},{name:"c1.fftB.fft_cols",kernel:"fft_compute_256",entry:"fft_cols",dispatch:[256,1,1],bindings:[{name:"data",resource:"fftB_1"}]},{name:"c1.assemble",kernel:"ocean_assemble",dispatch:[1024,1,1],bindings:[{name:"fieldA",resource:"fftA_1"},{name:"fieldB",resource:"fftB_1"},{name:"disp",resource:"dispTex1"},{name:"slope",resource:"slopeTex1"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:256}}]},{name:"c1.dispTex1.mips",type:"repeat",count:8,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[256,1,1],bindings:[{name:"src",resource:"dispTex1",levelAt:{index:"l"}},{name:"dst",resource:"dispTex1",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:256},dstWidth:{index:"l",offset:1,shr:256},pad:0,srcLevel:0}}]}]},{name:"c1.slopeTex1.mips",type:"repeat",count:8,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[256,1,1],bindings:[{name:"src",resource:"slopeTex1",levelAt:{index:"l"}},{name:"dst",resource:"slopeTex1",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:256},dstWidth:{index:"l",offset:1,shr:256},pad:0,srcLevel:0}}]}]},{name:"c2.spectrum",kernel:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",dispatch:[1024,1,1],bindings:[{name:"h0",resource:"h0_2"},{name:"omega",resource:"omega_2"}],uniforms:[{name:"spec",values:{cascade:2,fetchKm:"param:fetch",gamma:"param:gamma",kMax:5.4,kMin:.8,loopPeriod:"param:loopPeriod",n:256,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:37,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c2.evolve",kernel:"ocean_evolve",dispatch:[1024,1,1],bindings:[{name:"h0",resource:"h0_2"},{name:"omega",resource:"omega_2"},{name:"outA",resource:"fftA_2"},{name:"outB",resource:"fftB_2"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:256,t:"param:phaseTime",tile:37}}]},{name:"c2.fftA.fft_rows",kernel:"fft_compute_256",dispatch:[256,1,1],bindings:[{name:"data",resource:"fftA_2"}]},{name:"c2.fftA.fft_cols",kernel:"fft_compute_256",entry:"fft_cols",dispatch:[256,1,1],bindings:[{name:"data",resource:"fftA_2"}]},{name:"c2.fftB.fft_rows",kernel:"fft_compute_256",dispatch:[256,1,1],bindings:[{name:"data",resource:"fftB_2"}]},{name:"c2.fftB.fft_cols",kernel:"fft_compute_256",entry:"fft_cols",dispatch:[256,1,1],bindings:[{name:"data",resource:"fftB_2"}]},{name:"c2.assemble",kernel:"ocean_assemble",dispatch:[1024,1,1],bindings:[{name:"fieldA",resource:"fftA_2"},{name:"fieldB",resource:"fftB_2"},{name:"disp",resource:"dispTex2"},{name:"slope",resource:"slopeTex2"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:256}}]},{name:"c2.dispTex2.mips",type:"repeat",count:8,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[256,1,1],bindings:[{name:"src",resource:"dispTex2",levelAt:{index:"l"}},{name:"dst",resource:"dispTex2",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:256},dstWidth:{index:"l",offset:1,shr:256},pad:0,srcLevel:0}}]}]},{name:"c2.slopeTex2.mips",type:"repeat",count:8,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[256,1,1],bindings:[{name:"src",resource:"slopeTex2",levelAt:{index:"l"}},{name:"dst",resource:"slopeTex2",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:256},dstWidth:{index:"l",offset:1,shr:256},pad:0,srcLevel:0}}]}]},{name:"c3.spectrum",kernel:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",dispatch:[1024,1,1],bindings:[{name:"h0",resource:"h0_3"},{name:"omega",resource:"omega_3"}],uniforms:[{name:"spec",values:{cascade:3,fetchKm:"param:fetch",gamma:"param:gamma",kMax:37,kMin:5.4,loopPeriod:"param:loopPeriod",n:256,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:5.3,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c3.evolve",kernel:"ocean_evolve",dispatch:[1024,1,1],bindings:[{name:"h0",resource:"h0_3"},{name:"omega",resource:"omega_3"},{name:"outA",resource:"fftA_3"},{name:"outB",resource:"fftB_3"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:256,t:"param:phaseTime",tile:5.3}}]},{name:"c3.fftA.fft_rows",kernel:"fft_compute_256",dispatch:[256,1,1],bindings:[{name:"data",resource:"fftA_3"}]},{name:"c3.fftA.fft_cols",kernel:"fft_compute_256",entry:"fft_cols",dispatch:[256,1,1],bindings:[{name:"data",resource:"fftA_3"}]},{name:"c3.fftB.fft_rows",kernel:"fft_compute_256",dispatch:[256,1,1],bindings:[{name:"data",resource:"fftB_3"}]},{name:"c3.fftB.fft_cols",kernel:"fft_compute_256",entry:"fft_cols",dispatch:[256,1,1],bindings:[{name:"data",resource:"fftB_3"}]},{name:"c3.assemble",kernel:"ocean_assemble",dispatch:[1024,1,1],bindings:[{name:"fieldA",resource:"fftA_3"},{name:"fieldB",resource:"fftB_3"},{name:"disp",resource:"dispTex3"},{name:"slope",resource:"slopeTex3"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:256}}]},{name:"c3.dispTex3.mips",type:"repeat",count:8,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[256,1,1],bindings:[{name:"src",resource:"dispTex3",levelAt:{index:"l"}},{name:"dst",resource:"dispTex3",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:256},dstWidth:{index:"l",offset:1,shr:256},pad:0,srcLevel:0}}]}]},{name:"c3.slopeTex3.mips",type:"repeat",count:8,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[256,1,1],bindings:[{name:"src",resource:"slopeTex3",levelAt:{index:"l"}},{name:"dst",resource:"slopeTex3",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:256},dstWidth:{index:"l",offset:1,shr:256},pad:0,srcLevel:0}}]}]},{name:"wake.source",kernel:"wake_source",when:"param:wakeEnabled",dispatch:[1024,1,1],bindings:[{name:"wakeIn",resource:"wakeA"},{name:"fftOut",resource:"wakeF"},{name:"cleanOut",resource:"wakeC"}],uniforms:[{name:"wake",values:{cell:"param:wakeCell",depth:"param:wakeDepth",dt:"param:wakeDt",foamSpeed:5,fullSpeed:3,heading:"param:hullHeading",hullBeam:"param:hullBeam",hullLen:"param:hullLength",hullU:"param:wakeHullU",hullV:"param:wakeHullV",originX:"param:wakeOriginX",originZ:"param:wakeOriginZ",reset:"param:wakeReset",shiftX:"param:wakeShiftX",shiftZ:"param:wakeShiftZ",speed:"param:hullSpeed",srcBeam:"param:wakeSrcBeam",srcLength:"param:wakeSrcLength",tau:"param:wakeFoamTau",width:256}}]},{name:"wake.fft1.fft_rows",kernel:"fft_compute_256",when:"param:wakeEnabled",dispatch:[256,1,1],bindings:[{name:"data",resource:"wakeF"}]},{name:"wake.fft1.fft_cols",kernel:"fft_compute_256",entry:"fft_cols",when:"param:wakeEnabled",dispatch:[256,1,1],bindings:[{name:"data",resource:"wakeF"}]},{name:"wake.symbol",kernel:"wake_symbol",when:"param:wakeEnabled",dispatch:[1024,1,1],bindings:[{name:"fftIn",resource:"wakeF"},{name:"fftOut",resource:"wakeG"}],uniforms:[{name:"sym",values:{n:256,window:"param:wakeLength"}}]},{name:"wake.fft2.fft_rows",kernel:"fft_compute_256",when:"param:wakeEnabled",dispatch:[256,1,1],bindings:[{name:"data",resource:"wakeG"}]},{name:"wake.fft2.fft_cols",kernel:"fft_compute_256",entry:"fft_cols",when:"param:wakeEnabled",dispatch:[256,1,1],bindings:[{name:"data",resource:"wakeG"}]},{name:"wake.step",kernel:"wake_step",when:"param:wakeEnabled",dispatch:[1024,1,1],bindings:[{name:"qIn",resource:"wakeG"},{name:"cleanIn",resource:"wakeC"},{name:"stateOut",resource:"wakeB"},{name:"wakeTex",resource:"wakeTex"}],uniforms:[{name:"wstep",values:{alpha:"param:wakeAlpha",cell:"param:wakeCell",dt:"param:wakeDt",gravity:"param:wakeGEff",originX:"param:wakeOriginX",originZ:"param:wakeOriginZ",sponge:24,spongeRate:.8,width:256}}]},{name:"wake.wakeTex.mips",type:"repeat",when:"param:wakeEnabled",count:8,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[256,1,1],bindings:[{name:"src",resource:"wakeTex",levelAt:{index:"l"}},{name:"dst",resource:"wakeTex",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:256},dstWidth:{index:"l",offset:1,shr:256},pad:0,srcLevel:0}}]}]},{name:"foam",kernel:"ocean_foam",dispatch:[16384,1,1],bindings:[{name:"foamIn",resource:"foamA"},{name:"fieldB0",resource:"fftB_0"},{name:"fieldB1",resource:"fftB_1"},{name:"fieldB2",resource:"fftB_2"},{name:"fieldB3",resource:"fftB_3"},{name:"foamOut",resource:"foamB"},{name:"foamTex",resource:"foamTex"},{name:"wakeIn",resource:"wakeB"}],uniforms:[{name:"foam",values:{cascades:4,cell:.5,dt:"param:dt",gain:"param:foamGain",lambda:"param:lambda",n0:64,n1:256,n2:256,n3:256,omX0:"param:foamOriginMod0X",omX1:"param:foamOriginMod1X",omX2:"param:foamOriginMod2X",omX3:"param:foamOriginMod3X",omZ0:"param:foamOriginMod0Z",omZ1:"param:foamOriginMod1Z",omZ2:"param:foamOriginMod2Z",omZ3:"param:foamOriginMod3Z",originX:"param:foamOriginX",originZ:"param:foamOriginZ",shiftX:"param:foamShiftX",shiftZ:"param:foamShiftZ",size0:1600,size1:250,size2:37,size3:5.3,tau:"param:foamTau",threshold:"param:foamThreshold",wakeCell:"param:wakeCell",wakeGain:"param:wakeFoamGainEff",wakeOriginX:"param:wakeOriginModX",wakeOriginZ:"param:wakeOriginModZ",wakeRelX:"param:wakeRelX",wakeRelZ:"param:wakeRelZ",wakeWidth:256,width:1024}}]},{name:"foamTex.mips",type:"repeat",count:10,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[4096,1,1],bindings:[{name:"src",resource:"foamTex",levelAt:{index:"l"}},{name:"dst",resource:"foamTex",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:1024},dstWidth:{index:"l",offset:1,shr:1024},pad:0,srcLevel:0}}]}]},{name:"probe",kernel:"ocean_probe",dispatch:[1,1,1],bindings:[{name:"points",resource:"probePoints"},{name:"disp0",resource:"fftA_0"},{name:"disp1",resource:"fftA_1"},{name:"disp2",resource:"fftA_2"},{name:"disp3",resource:"fftA_3"},{name:"out",resource:"probeOut"}],uniforms:[{name:"probe",values:{cascades:4,count:"param:probeCount",lambda:"param:lambda",n0:64,n1:256,n2:256,n3:256,normalStep:.5,omX0:"param:probeOriginMod0X",omX1:"param:probeOriginMod1X",omX2:"param:probeOriginMod2X",omX3:"param:probeOriginMod3X",omZ0:"param:probeOriginMod0Z",omZ1:"param:probeOriginMod1Z",omZ2:"param:probeOriginMod2Z",omZ3:"param:probeOriginMod3Z",size0:1600,size1:250,size2:37,size3:5.3,t:"param:simTime"}}]}],frameSwaps:[["wakeA","wakeB"],["foamA","foamB"]],exports:{disp0:"dispTex0",disp1:"dispTex1",disp2:"dispTex2",disp3:"dispTex3",foam:"foamTex",probe:"probeOut",slope0:"slopeTex0",slope1:"slopeTex1",slope2:"slopeTex2",slope3:"slopeTex3",wake:"wakeTex"},exportParams:["waterModX0","waterModZ0","waterModX1","waterModZ1","waterModX2","waterModZ2","waterModX3","waterModZ3","wakeCamRelX","wakeCamRelZ","wakeOriginModX","wakeOriginModZ","wakeCell","wakeOn"]},"webgpu/medium":{name:"ocean",backend:"webgpu",tier:"medium",version:1,resources:[{name:"h0_0",kind:"buffer",elementType:"vec4<f32>",count:4096},{name:"omega_0",kind:"buffer",elementType:"f32",count:4096},{name:"fftA_0",kind:"buffer",elementType:"vec4<f32>",count:4096},{name:"fftB_0",kind:"buffer",elementType:"vec4<f32>",count:4096},{name:"dispTex0",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"slopeTex0",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"h0_1",kind:"buffer",elementType:"vec4<f32>",count:16384},{name:"omega_1",kind:"buffer",elementType:"f32",count:16384},{name:"fftA_1",kind:"buffer",elementType:"vec4<f32>",count:16384},{name:"fftB_1",kind:"buffer",elementType:"vec4<f32>",count:16384},{name:"dispTex1",kind:"texture",format:"rgba16float",width:128,height:128,mipLevels:8},{name:"slopeTex1",kind:"texture",format:"rgba16float",width:128,height:128,mipLevels:8},{name:"h0_2",kind:"buffer",elementType:"vec4<f32>",count:16384},{name:"omega_2",kind:"buffer",elementType:"f32",count:16384},{name:"fftA_2",kind:"buffer",elementType:"vec4<f32>",count:16384},{name:"fftB_2",kind:"buffer",elementType:"vec4<f32>",count:16384},{name:"dispTex2",kind:"texture",format:"rgba16float",width:128,height:128,mipLevels:8},{name:"slopeTex2",kind:"texture",format:"rgba16float",width:128,height:128,mipLevels:8},{name:"h0_3",kind:"buffer",elementType:"vec4<f32>",count:16384},{name:"omega_3",kind:"buffer",elementType:"f32",count:16384},{name:"fftA_3",kind:"buffer",elementType:"vec4<f32>",count:16384},{name:"fftB_3",kind:"buffer",elementType:"vec4<f32>",count:16384},{name:"dispTex3",kind:"texture",format:"rgba16float",width:128,height:128,mipLevels:8},{name:"slopeTex3",kind:"texture",format:"rgba16float",width:128,height:128,mipLevels:8},{name:"wakeA",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"wakeB",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"wakeF",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"wakeG",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"wakeC",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"wakeTex",kind:"texture",format:"rgba16float",width:256,height:256,mipLevels:9},{name:"foamA",kind:"buffer",elementType:"f32",count:1048576},{name:"foamB",kind:"buffer",elementType:"f32",count:1048576},{name:"foamTex",kind:"texture",format:"rgba16float",width:1024,height:1024,mipLevels:11},{name:"probePoints",kind:"buffer",elementType:"vec4<f32>",count:64,feed:"probePoints"},{name:"probeOut",kind:"buffer",elementType:"vec4<f32>",count:64}],passes:[{name:"c0.spectrum",kernel:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",dispatch:[64,1,1],bindings:[{name:"h0",resource:"h0_0"},{name:"omega",resource:"omega_0"}],uniforms:[{name:"spec",values:{cascade:0,fetchKm:"param:fetch",gamma:"param:gamma",kMax:.12,kMin:.003926990816987242,loopPeriod:"param:loopPeriod",n:64,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:1600,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c0.evolve",kernel:"ocean_evolve",dispatch:[64,1,1],bindings:[{name:"h0",resource:"h0_0"},{name:"omega",resource:"omega_0"},{name:"outA",resource:"fftA_0"},{name:"outB",resource:"fftB_0"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:64,t:"param:phaseTime",tile:1600}}]},{name:"c0.fftA.fft_rows",kernel:"fft_compute_64",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftA_0"}]},{name:"c0.fftA.fft_cols",kernel:"fft_compute_64",entry:"fft_cols",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftA_0"}]},{name:"c0.fftB.fft_rows",kernel:"fft_compute_64",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftB_0"}]},{name:"c0.fftB.fft_cols",kernel:"fft_compute_64",entry:"fft_cols",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftB_0"}]},{name:"c0.assemble",kernel:"ocean_assemble",dispatch:[64,1,1],bindings:[{name:"fieldA",resource:"fftA_0"},{name:"fieldB",resource:"fftB_0"},{name:"disp",resource:"dispTex0"},{name:"slope",resource:"slopeTex0"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:64}}]},{name:"c0.dispTex0.mips",type:"repeat",count:6,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[16,1,1],bindings:[{name:"src",resource:"dispTex0",levelAt:{index:"l"}},{name:"dst",resource:"dispTex0",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:64},dstWidth:{index:"l",offset:1,shr:64},pad:0,srcLevel:0}}]}]},{name:"c0.slopeTex0.mips",type:"repeat",count:6,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[16,1,1],bindings:[{name:"src",resource:"slopeTex0",levelAt:{index:"l"}},{name:"dst",resource:"slopeTex0",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:64},dstWidth:{index:"l",offset:1,shr:64},pad:0,srcLevel:0}}]}]},{name:"c1.spectrum",kernel:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",dispatch:[256,1,1],bindings:[{name:"h0",resource:"h0_1"},{name:"omega",resource:"omega_1"}],uniforms:[{name:"spec",values:{cascade:1,fetchKm:"param:fetch",gamma:"param:gamma",kMax:.8,kMin:.12,loopPeriod:"param:loopPeriod",n:128,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:250,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c1.evolve",kernel:"ocean_evolve",dispatch:[256,1,1],bindings:[{name:"h0",resource:"h0_1"},{name:"omega",resource:"omega_1"},{name:"outA",resource:"fftA_1"},{name:"outB",resource:"fftB_1"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:128,t:"param:phaseTime",tile:250}}]},{name:"c1.fftA.fft_rows",kernel:"fft_compute_128",dispatch:[128,1,1],bindings:[{name:"data",resource:"fftA_1"}]},{name:"c1.fftA.fft_cols",kernel:"fft_compute_128",entry:"fft_cols",dispatch:[128,1,1],bindings:[{name:"data",resource:"fftA_1"}]},{name:"c1.fftB.fft_rows",kernel:"fft_compute_128",dispatch:[128,1,1],bindings:[{name:"data",resource:"fftB_1"}]},{name:"c1.fftB.fft_cols",kernel:"fft_compute_128",entry:"fft_cols",dispatch:[128,1,1],bindings:[{name:"data",resource:"fftB_1"}]},{name:"c1.assemble",kernel:"ocean_assemble",dispatch:[256,1,1],bindings:[{name:"fieldA",resource:"fftA_1"},{name:"fieldB",resource:"fftB_1"},{name:"disp",resource:"dispTex1"},{name:"slope",resource:"slopeTex1"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:128}}]},{name:"c1.dispTex1.mips",type:"repeat",count:7,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[64,1,1],bindings:[{name:"src",resource:"dispTex1",levelAt:{index:"l"}},{name:"dst",resource:"dispTex1",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:128},dstWidth:{index:"l",offset:1,shr:128},pad:0,srcLevel:0}}]}]},{name:"c1.slopeTex1.mips",type:"repeat",count:7,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[64,1,1],bindings:[{name:"src",resource:"slopeTex1",levelAt:{index:"l"}},{name:"dst",resource:"slopeTex1",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:128},dstWidth:{index:"l",offset:1,shr:128},pad:0,srcLevel:0}}]}]},{name:"c2.spectrum",kernel:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",dispatch:[256,1,1],bindings:[{name:"h0",resource:"h0_2"},{name:"omega",resource:"omega_2"}],uniforms:[{name:"spec",values:{cascade:2,fetchKm:"param:fetch",gamma:"param:gamma",kMax:5.4,kMin:.8,loopPeriod:"param:loopPeriod",n:128,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:37,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c2.evolve",kernel:"ocean_evolve",dispatch:[256,1,1],bindings:[{name:"h0",resource:"h0_2"},{name:"omega",resource:"omega_2"},{name:"outA",resource:"fftA_2"},{name:"outB",resource:"fftB_2"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:128,t:"param:phaseTime",tile:37}}]},{name:"c2.fftA.fft_rows",kernel:"fft_compute_128",dispatch:[128,1,1],bindings:[{name:"data",resource:"fftA_2"}]},{name:"c2.fftA.fft_cols",kernel:"fft_compute_128",entry:"fft_cols",dispatch:[128,1,1],bindings:[{name:"data",resource:"fftA_2"}]},{name:"c2.fftB.fft_rows",kernel:"fft_compute_128",dispatch:[128,1,1],bindings:[{name:"data",resource:"fftB_2"}]},{name:"c2.fftB.fft_cols",kernel:"fft_compute_128",entry:"fft_cols",dispatch:[128,1,1],bindings:[{name:"data",resource:"fftB_2"}]},{name:"c2.assemble",kernel:"ocean_assemble",dispatch:[256,1,1],bindings:[{name:"fieldA",resource:"fftA_2"},{name:"fieldB",resource:"fftB_2"},{name:"disp",resource:"dispTex2"},{name:"slope",resource:"slopeTex2"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:128}}]},{name:"c2.dispTex2.mips",type:"repeat",count:7,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[64,1,1],bindings:[{name:"src",resource:"dispTex2",levelAt:{index:"l"}},{name:"dst",resource:"dispTex2",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:128},dstWidth:{index:"l",offset:1,shr:128},pad:0,srcLevel:0}}]}]},{name:"c2.slopeTex2.mips",type:"repeat",count:7,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[64,1,1],bindings:[{name:"src",resource:"slopeTex2",levelAt:{index:"l"}},{name:"dst",resource:"slopeTex2",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:128},dstWidth:{index:"l",offset:1,shr:128},pad:0,srcLevel:0}}]}]},{name:"c3.spectrum",kernel:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",dispatch:[256,1,1],bindings:[{name:"h0",resource:"h0_3"},{name:"omega",resource:"omega_3"}],uniforms:[{name:"spec",values:{cascade:3,fetchKm:"param:fetch",gamma:"param:gamma",kMax:37,kMin:5.4,loopPeriod:"param:loopPeriod",n:128,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:5.3,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c3.evolve",kernel:"ocean_evolve",dispatch:[256,1,1],bindings:[{name:"h0",resource:"h0_3"},{name:"omega",resource:"omega_3"},{name:"outA",resource:"fftA_3"},{name:"outB",resource:"fftB_3"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:128,t:"param:phaseTime",tile:5.3}}]},{name:"c3.fftA.fft_rows",kernel:"fft_compute_128",dispatch:[128,1,1],bindings:[{name:"data",resource:"fftA_3"}]},{name:"c3.fftA.fft_cols",kernel:"fft_compute_128",entry:"fft_cols",dispatch:[128,1,1],bindings:[{name:"data",resource:"fftA_3"}]},{name:"c3.fftB.fft_rows",kernel:"fft_compute_128",dispatch:[128,1,1],bindings:[{name:"data",resource:"fftB_3"}]},{name:"c3.fftB.fft_cols",kernel:"fft_compute_128",entry:"fft_cols",dispatch:[128,1,1],bindings:[{name:"data",resource:"fftB_3"}]},{name:"c3.assemble",kernel:"ocean_assemble",dispatch:[256,1,1],bindings:[{name:"fieldA",resource:"fftA_3"},{name:"fieldB",resource:"fftB_3"},{name:"disp",resource:"dispTex3"},{name:"slope",resource:"slopeTex3"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:128}}]},{name:"c3.dispTex3.mips",type:"repeat",count:7,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[64,1,1],bindings:[{name:"src",resource:"dispTex3",levelAt:{index:"l"}},{name:"dst",resource:"dispTex3",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:128},dstWidth:{index:"l",offset:1,shr:128},pad:0,srcLevel:0}}]}]},{name:"c3.slopeTex3.mips",type:"repeat",count:7,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[64,1,1],bindings:[{name:"src",resource:"slopeTex3",levelAt:{index:"l"}},{name:"dst",resource:"slopeTex3",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:128},dstWidth:{index:"l",offset:1,shr:128},pad:0,srcLevel:0}}]}]},{name:"wake.source",kernel:"wake_source",when:"param:wakeEnabled",dispatch:[1024,1,1],bindings:[{name:"wakeIn",resource:"wakeA"},{name:"fftOut",resource:"wakeF"},{name:"cleanOut",resource:"wakeC"}],uniforms:[{name:"wake",values:{cell:"param:wakeCell",depth:"param:wakeDepth",dt:"param:wakeDt",foamSpeed:5,fullSpeed:3,heading:"param:hullHeading",hullBeam:"param:hullBeam",hullLen:"param:hullLength",hullU:"param:wakeHullU",hullV:"param:wakeHullV",originX:"param:wakeOriginX",originZ:"param:wakeOriginZ",reset:"param:wakeReset",shiftX:"param:wakeShiftX",shiftZ:"param:wakeShiftZ",speed:"param:hullSpeed",srcBeam:"param:wakeSrcBeam",srcLength:"param:wakeSrcLength",tau:"param:wakeFoamTau",width:256}}]},{name:"wake.fft1.fft_rows",kernel:"fft_compute_256",when:"param:wakeEnabled",dispatch:[256,1,1],bindings:[{name:"data",resource:"wakeF"}]},{name:"wake.fft1.fft_cols",kernel:"fft_compute_256",entry:"fft_cols",when:"param:wakeEnabled",dispatch:[256,1,1],bindings:[{name:"data",resource:"wakeF"}]},{name:"wake.symbol",kernel:"wake_symbol",when:"param:wakeEnabled",dispatch:[1024,1,1],bindings:[{name:"fftIn",resource:"wakeF"},{name:"fftOut",resource:"wakeG"}],uniforms:[{name:"sym",values:{n:256,window:"param:wakeLength"}}]},{name:"wake.fft2.fft_rows",kernel:"fft_compute_256",when:"param:wakeEnabled",dispatch:[256,1,1],bindings:[{name:"data",resource:"wakeG"}]},{name:"wake.fft2.fft_cols",kernel:"fft_compute_256",entry:"fft_cols",when:"param:wakeEnabled",dispatch:[256,1,1],bindings:[{name:"data",resource:"wakeG"}]},{name:"wake.step",kernel:"wake_step",when:"param:wakeEnabled",dispatch:[1024,1,1],bindings:[{name:"qIn",resource:"wakeG"},{name:"cleanIn",resource:"wakeC"},{name:"stateOut",resource:"wakeB"},{name:"wakeTex",resource:"wakeTex"}],uniforms:[{name:"wstep",values:{alpha:"param:wakeAlpha",cell:"param:wakeCell",dt:"param:wakeDt",gravity:"param:wakeGEff",originX:"param:wakeOriginX",originZ:"param:wakeOriginZ",sponge:24,spongeRate:.8,width:256}}]},{name:"wake.wakeTex.mips",type:"repeat",when:"param:wakeEnabled",count:8,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[256,1,1],bindings:[{name:"src",resource:"wakeTex",levelAt:{index:"l"}},{name:"dst",resource:"wakeTex",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:256},dstWidth:{index:"l",offset:1,shr:256},pad:0,srcLevel:0}}]}]},{name:"foam",kernel:"ocean_foam",dispatch:[16384,1,1],bindings:[{name:"foamIn",resource:"foamA"},{name:"fieldB0",resource:"fftB_0"},{name:"fieldB1",resource:"fftB_1"},{name:"fieldB2",resource:"fftB_2"},{name:"fieldB3",resource:"fftB_3"},{name:"foamOut",resource:"foamB"},{name:"foamTex",resource:"foamTex"},{name:"wakeIn",resource:"wakeB"}],uniforms:[{name:"foam",values:{cascades:4,cell:.5,dt:"param:dt",gain:"param:foamGain",lambda:"param:lambda",n0:64,n1:128,n2:128,n3:128,omX0:"param:foamOriginMod0X",omX1:"param:foamOriginMod1X",omX2:"param:foamOriginMod2X",omX3:"param:foamOriginMod3X",omZ0:"param:foamOriginMod0Z",omZ1:"param:foamOriginMod1Z",omZ2:"param:foamOriginMod2Z",omZ3:"param:foamOriginMod3Z",originX:"param:foamOriginX",originZ:"param:foamOriginZ",shiftX:"param:foamShiftX",shiftZ:"param:foamShiftZ",size0:1600,size1:250,size2:37,size3:5.3,tau:"param:foamTau",threshold:"param:foamThreshold",wakeCell:"param:wakeCell",wakeGain:"param:wakeFoamGainEff",wakeOriginX:"param:wakeOriginModX",wakeOriginZ:"param:wakeOriginModZ",wakeRelX:"param:wakeRelX",wakeRelZ:"param:wakeRelZ",wakeWidth:256,width:1024}}]},{name:"foamTex.mips",type:"repeat",count:10,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[4096,1,1],bindings:[{name:"src",resource:"foamTex",levelAt:{index:"l"}},{name:"dst",resource:"foamTex",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:1024},dstWidth:{index:"l",offset:1,shr:1024},pad:0,srcLevel:0}}]}]},{name:"probe",kernel:"ocean_probe",dispatch:[1,1,1],bindings:[{name:"points",resource:"probePoints"},{name:"disp0",resource:"fftA_0"},{name:"disp1",resource:"fftA_1"},{name:"disp2",resource:"fftA_2"},{name:"disp3",resource:"fftA_3"},{name:"out",resource:"probeOut"}],uniforms:[{name:"probe",values:{cascades:4,count:"param:probeCount",lambda:"param:lambda",n0:64,n1:128,n2:128,n3:128,normalStep:.5,omX0:"param:probeOriginMod0X",omX1:"param:probeOriginMod1X",omX2:"param:probeOriginMod2X",omX3:"param:probeOriginMod3X",omZ0:"param:probeOriginMod0Z",omZ1:"param:probeOriginMod1Z",omZ2:"param:probeOriginMod2Z",omZ3:"param:probeOriginMod3Z",size0:1600,size1:250,size2:37,size3:5.3,t:"param:simTime"}}]}],frameSwaps:[["wakeA","wakeB"],["foamA","foamB"]],exports:{disp0:"dispTex0",disp1:"dispTex1",disp2:"dispTex2",disp3:"dispTex3",foam:"foamTex",probe:"probeOut",slope0:"slopeTex0",slope1:"slopeTex1",slope2:"slopeTex2",slope3:"slopeTex3",wake:"wakeTex"},paramsFrom:"webgpu/high"},"webgpu/low":{name:"ocean",backend:"webgpu",tier:"low",version:1,resources:[{name:"h0_0",kind:"buffer",elementType:"vec4<f32>",count:4096},{name:"omega_0",kind:"buffer",elementType:"f32",count:4096},{name:"fftA_0",kind:"buffer",elementType:"vec4<f32>",count:4096},{name:"fftB_0",kind:"buffer",elementType:"vec4<f32>",count:4096},{name:"dispTex0",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"slopeTex0",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"h0_1",kind:"buffer",elementType:"vec4<f32>",count:4096},{name:"omega_1",kind:"buffer",elementType:"f32",count:4096},{name:"fftA_1",kind:"buffer",elementType:"vec4<f32>",count:4096},{name:"fftB_1",kind:"buffer",elementType:"vec4<f32>",count:4096},{name:"dispTex1",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"slopeTex1",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"h0_2",kind:"buffer",elementType:"vec4<f32>",count:4096},{name:"omega_2",kind:"buffer",elementType:"f32",count:4096},{name:"fftA_2",kind:"buffer",elementType:"vec4<f32>",count:4096},{name:"fftB_2",kind:"buffer",elementType:"vec4<f32>",count:4096},{name:"dispTex2",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"slopeTex2",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"wakeA",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"wakeB",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"wakeF",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"wakeG",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"wakeC",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"wakeTex",kind:"texture",format:"rgba16float",width:256,height:256,mipLevels:9},{name:"foamA",kind:"buffer",elementType:"f32",count:1048576},{name:"foamB",kind:"buffer",elementType:"f32",count:1048576},{name:"foamTex",kind:"texture",format:"rgba16float",width:1024,height:1024,mipLevels:11},{name:"probePoints",kind:"buffer",elementType:"vec4<f32>",count:64,feed:"probePoints"},{name:"probeOut",kind:"buffer",elementType:"vec4<f32>",count:64}],passes:[{name:"c0.spectrum",kernel:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",dispatch:[64,1,1],bindings:[{name:"h0",resource:"h0_0"},{name:"omega",resource:"omega_0"}],uniforms:[{name:"spec",values:{cascade:0,fetchKm:"param:fetch",gamma:"param:gamma",kMax:.12,kMin:.003926990816987242,loopPeriod:"param:loopPeriod",n:64,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:1600,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c0.evolve",kernel:"ocean_evolve",dispatch:[64,1,1],bindings:[{name:"h0",resource:"h0_0"},{name:"omega",resource:"omega_0"},{name:"outA",resource:"fftA_0"},{name:"outB",resource:"fftB_0"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:64,t:"param:phaseTime",tile:1600}}]},{name:"c0.fftA.fft_rows",kernel:"fft_compute_64",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftA_0"}]},{name:"c0.fftA.fft_cols",kernel:"fft_compute_64",entry:"fft_cols",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftA_0"}]},{name:"c0.fftB.fft_rows",kernel:"fft_compute_64",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftB_0"}]},{name:"c0.fftB.fft_cols",kernel:"fft_compute_64",entry:"fft_cols",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftB_0"}]},{name:"c0.assemble",kernel:"ocean_assemble",dispatch:[64,1,1],bindings:[{name:"fieldA",resource:"fftA_0"},{name:"fieldB",resource:"fftB_0"},{name:"disp",resource:"dispTex0"},{name:"slope",resource:"slopeTex0"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:64}}]},{name:"c0.dispTex0.mips",type:"repeat",count:6,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[16,1,1],bindings:[{name:"src",resource:"dispTex0",levelAt:{index:"l"}},{name:"dst",resource:"dispTex0",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:64},dstWidth:{index:"l",offset:1,shr:64},pad:0,srcLevel:0}}]}]},{name:"c0.slopeTex0.mips",type:"repeat",count:6,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[16,1,1],bindings:[{name:"src",resource:"slopeTex0",levelAt:{index:"l"}},{name:"dst",resource:"slopeTex0",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:64},dstWidth:{index:"l",offset:1,shr:64},pad:0,srcLevel:0}}]}]},{name:"c1.spectrum",kernel:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",dispatch:[64,1,1],bindings:[{name:"h0",resource:"h0_1"},{name:"omega",resource:"omega_1"}],uniforms:[{name:"spec",values:{cascade:1,fetchKm:"param:fetch",gamma:"param:gamma",kMax:.8,kMin:.12,loopPeriod:"param:loopPeriod",n:64,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:250,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c1.evolve",kernel:"ocean_evolve",dispatch:[64,1,1],bindings:[{name:"h0",resource:"h0_1"},{name:"omega",resource:"omega_1"},{name:"outA",resource:"fftA_1"},{name:"outB",resource:"fftB_1"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:64,t:"param:phaseTime",tile:250}}]},{name:"c1.fftA.fft_rows",kernel:"fft_compute_64",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftA_1"}]},{name:"c1.fftA.fft_cols",kernel:"fft_compute_64",entry:"fft_cols",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftA_1"}]},{name:"c1.fftB.fft_rows",kernel:"fft_compute_64",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftB_1"}]},{name:"c1.fftB.fft_cols",kernel:"fft_compute_64",entry:"fft_cols",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftB_1"}]},{name:"c1.assemble",kernel:"ocean_assemble",dispatch:[64,1,1],bindings:[{name:"fieldA",resource:"fftA_1"},{name:"fieldB",resource:"fftB_1"},{name:"disp",resource:"dispTex1"},{name:"slope",resource:"slopeTex1"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:64}}]},{name:"c1.dispTex1.mips",type:"repeat",count:6,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[16,1,1],bindings:[{name:"src",resource:"dispTex1",levelAt:{index:"l"}},{name:"dst",resource:"dispTex1",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:64},dstWidth:{index:"l",offset:1,shr:64},pad:0,srcLevel:0}}]}]},{name:"c1.slopeTex1.mips",type:"repeat",count:6,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[16,1,1],bindings:[{name:"src",resource:"slopeTex1",levelAt:{index:"l"}},{name:"dst",resource:"slopeTex1",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:64},dstWidth:{index:"l",offset:1,shr:64},pad:0,srcLevel:0}}]}]},{name:"c2.spectrum",kernel:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",dispatch:[64,1,1],bindings:[{name:"h0",resource:"h0_2"},{name:"omega",resource:"omega_2"}],uniforms:[{name:"spec",values:{cascade:2,fetchKm:"param:fetch",gamma:"param:gamma",kMax:5.4,kMin:.8,loopPeriod:"param:loopPeriod",n:64,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:37,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c2.evolve",kernel:"ocean_evolve",dispatch:[64,1,1],bindings:[{name:"h0",resource:"h0_2"},{name:"omega",resource:"omega_2"},{name:"outA",resource:"fftA_2"},{name:"outB",resource:"fftB_2"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:64,t:"param:phaseTime",tile:37}}]},{name:"c2.fftA.fft_rows",kernel:"fft_compute_64",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftA_2"}]},{name:"c2.fftA.fft_cols",kernel:"fft_compute_64",entry:"fft_cols",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftA_2"}]},{name:"c2.fftB.fft_rows",kernel:"fft_compute_64",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftB_2"}]},{name:"c2.fftB.fft_cols",kernel:"fft_compute_64",entry:"fft_cols",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftB_2"}]},{name:"c2.assemble",kernel:"ocean_assemble",dispatch:[64,1,1],bindings:[{name:"fieldA",resource:"fftA_2"},{name:"fieldB",resource:"fftB_2"},{name:"disp",resource:"dispTex2"},{name:"slope",resource:"slopeTex2"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:64}}]},{name:"c2.dispTex2.mips",type:"repeat",count:6,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[16,1,1],bindings:[{name:"src",resource:"dispTex2",levelAt:{index:"l"}},{name:"dst",resource:"dispTex2",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:64},dstWidth:{index:"l",offset:1,shr:64},pad:0,srcLevel:0}}]}]},{name:"c2.slopeTex2.mips",type:"repeat",count:6,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[16,1,1],bindings:[{name:"src",resource:"slopeTex2",levelAt:{index:"l"}},{name:"dst",resource:"slopeTex2",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:64},dstWidth:{index:"l",offset:1,shr:64},pad:0,srcLevel:0}}]}]},{name:"wake.source",kernel:"wake_source",when:"param:wakeEnabled",dispatch:[1024,1,1],bindings:[{name:"wakeIn",resource:"wakeA"},{name:"fftOut",resource:"wakeF"},{name:"cleanOut",resource:"wakeC"}],uniforms:[{name:"wake",values:{cell:"param:wakeCell",depth:"param:wakeDepth",dt:"param:wakeDt",foamSpeed:5,fullSpeed:3,heading:"param:hullHeading",hullBeam:"param:hullBeam",hullLen:"param:hullLength",hullU:"param:wakeHullU",hullV:"param:wakeHullV",originX:"param:wakeOriginX",originZ:"param:wakeOriginZ",reset:"param:wakeReset",shiftX:"param:wakeShiftX",shiftZ:"param:wakeShiftZ",speed:"param:hullSpeed",srcBeam:"param:wakeSrcBeam",srcLength:"param:wakeSrcLength",tau:"param:wakeFoamTau",width:256}}]},{name:"wake.fft1.fft_rows",kernel:"fft_compute_256",when:"param:wakeEnabled",dispatch:[256,1,1],bindings:[{name:"data",resource:"wakeF"}]},{name:"wake.fft1.fft_cols",kernel:"fft_compute_256",entry:"fft_cols",when:"param:wakeEnabled",dispatch:[256,1,1],bindings:[{name:"data",resource:"wakeF"}]},{name:"wake.symbol",kernel:"wake_symbol",when:"param:wakeEnabled",dispatch:[1024,1,1],bindings:[{name:"fftIn",resource:"wakeF"},{name:"fftOut",resource:"wakeG"}],uniforms:[{name:"sym",values:{n:256,window:"param:wakeLength"}}]},{name:"wake.fft2.fft_rows",kernel:"fft_compute_256",when:"param:wakeEnabled",dispatch:[256,1,1],bindings:[{name:"data",resource:"wakeG"}]},{name:"wake.fft2.fft_cols",kernel:"fft_compute_256",entry:"fft_cols",when:"param:wakeEnabled",dispatch:[256,1,1],bindings:[{name:"data",resource:"wakeG"}]},{name:"wake.step",kernel:"wake_step",when:"param:wakeEnabled",dispatch:[1024,1,1],bindings:[{name:"qIn",resource:"wakeG"},{name:"cleanIn",resource:"wakeC"},{name:"stateOut",resource:"wakeB"},{name:"wakeTex",resource:"wakeTex"}],uniforms:[{name:"wstep",values:{alpha:"param:wakeAlpha",cell:"param:wakeCell",dt:"param:wakeDt",gravity:"param:wakeGEff",originX:"param:wakeOriginX",originZ:"param:wakeOriginZ",sponge:24,spongeRate:.8,width:256}}]},{name:"wake.wakeTex.mips",type:"repeat",when:"param:wakeEnabled",count:8,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[256,1,1],bindings:[{name:"src",resource:"wakeTex",levelAt:{index:"l"}},{name:"dst",resource:"wakeTex",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:256},dstWidth:{index:"l",offset:1,shr:256},pad:0,srcLevel:0}}]}]},{name:"foam",kernel:"ocean_foam",dispatch:[16384,1,1],bindings:[{name:"foamIn",resource:"foamA"},{name:"fieldB0",resource:"fftB_0"},{name:"fieldB1",resource:"fftB_1"},{name:"fieldB2",resource:"fftB_2"},{name:"fieldB3",resource:"fftB_2"},{name:"foamOut",resource:"foamB"},{name:"foamTex",resource:"foamTex"},{name:"wakeIn",resource:"wakeB"}],uniforms:[{name:"foam",values:{cascades:3,cell:.5,dt:"param:dt",gain:"param:foamGain",lambda:"param:lambda",n0:64,n1:64,n2:64,n3:64,omX0:"param:foamOriginMod0X",omX1:"param:foamOriginMod1X",omX2:"param:foamOriginMod2X",omX3:"param:foamOriginMod2X",omZ0:"param:foamOriginMod0Z",omZ1:"param:foamOriginMod1Z",omZ2:"param:foamOriginMod2Z",omZ3:"param:foamOriginMod2Z",originX:"param:foamOriginX",originZ:"param:foamOriginZ",shiftX:"param:foamShiftX",shiftZ:"param:foamShiftZ",size0:1600,size1:250,size2:37,size3:37,tau:"param:foamTau",threshold:"param:foamThreshold",wakeCell:"param:wakeCell",wakeGain:"param:wakeFoamGainEff",wakeOriginX:"param:wakeOriginModX",wakeOriginZ:"param:wakeOriginModZ",wakeRelX:"param:wakeRelX",wakeRelZ:"param:wakeRelZ",wakeWidth:256,width:1024}}]},{name:"foamTex.mips",type:"repeat",count:10,index:"l",body:[{name:"mip",kernel:"ocean_mip",dispatch:[4096,1,1],bindings:[{name:"src",resource:"foamTex",levelAt:{index:"l"}},{name:"dst",resource:"foamTex",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:1024},dstWidth:{index:"l",offset:1,shr:1024},pad:0,srcLevel:0}}]}]},{name:"probe",kernel:"ocean_probe",dispatch:[1,1,1],bindings:[{name:"points",resource:"probePoints"},{name:"disp0",resource:"fftA_0"},{name:"disp1",resource:"fftA_1"},{name:"disp2",resource:"fftA_2"},{name:"disp3",resource:"fftA_2"},{name:"out",resource:"probeOut"}],uniforms:[{name:"probe",values:{cascades:3,count:"param:probeCount",lambda:"param:lambda",n0:64,n1:64,n2:64,n3:64,normalStep:.5,omX0:"param:probeOriginMod0X",omX1:"param:probeOriginMod1X",omX2:"param:probeOriginMod2X",omX3:"param:probeOriginMod2X",omZ0:"param:probeOriginMod0Z",omZ1:"param:probeOriginMod1Z",omZ2:"param:probeOriginMod2Z",omZ3:"param:probeOriginMod2Z",size0:1600,size1:250,size2:37,size3:37,t:"param:simTime"}}]}],frameSwaps:[["wakeA","wakeB"],["foamA","foamB"]],exports:{disp0:"dispTex0",disp1:"dispTex1",disp2:"dispTex2",foam:"foamTex",probe:"probeOut",slope0:"slopeTex0",slope1:"slopeTex1",slope2:"slopeTex2",wake:"wakeTex"},paramsFrom:"webgpu/high"},"webgl2/high":{name:"ocean",backend:"webgl2",tier:"high",version:1,params:[{name:"windSpeed",type:"f32",source:{kind:"value",value:9},tween:20},{name:"fetch",type:"f32",source:{kind:"value",value:300},tween:20},{name:"gamma",type:"f32",source:{kind:"value",value:3.3},tween:20},{name:"spread",type:"f32",source:{kind:"value",value:8},tween:20},{name:"swell",type:"f32",source:{kind:"value",value:0},tween:20},{name:"windDirDeg",type:"f32",source:{kind:"value",value:0},tween:20},{name:"swellDirDeg",type:"f32",source:{kind:"value",value:0},tween:20},{name:"seed",type:"u32",source:{kind:"value",value:0}},{name:"loopPeriod",type:"f32",source:{kind:"value",value:1e3}},{name:"lambda",type:"f32",source:{kind:"value",value:1},tween:20},{name:"timeScale",type:"f32",source:{kind:"value",value:1}},{name:"driftX",type:"f32",source:{kind:"value",value:0}},{name:"driftZ",type:"f32",source:{kind:"value",value:0}},{name:"foamTau",type:"f32",source:{kind:"value",value:4}},{name:"foamThreshold",type:"f32",source:{kind:"value",value:.9}},{name:"foamGain",type:"f32",source:{kind:"value",value:2}},{name:"probeCount",type:"u32",source:{kind:"value",value:0}},{name:"dt",type:"f32",source:{kind:"frame",name:"dt"}},{name:"cameraX",type:"f32",source:{kind:"frame",name:"cameraX"}},{name:"cameraZ",type:"f32",source:{kind:"frame",name:"cameraZ"}},{name:"windDir",type:"f32",source:{args:[1.5707963267948966,{op:"mul",args:["param:windDirDeg",.017453292519943295]}],kind:"expr",op:"sub"}},{name:"swellDir",type:"f32",source:{args:[1.5707963267948966,{op:"mul",args:["param:swellDirDeg",.017453292519943295]}],kind:"expr",op:"sub"}},{name:"simTime",type:"f32",source:{kind:"accumulate",of:"param:timeScale"}},{name:"phaseTime",type:"f32",source:{args:["param:simTime","param:loopPeriod"],kind:"expr",op:"mod"}},{name:"waterX",type:"f32",source:{kind:"accumulate",of:"param:driftX"}},{name:"waterZ",type:"f32",source:{kind:"accumulate",of:"param:driftZ"}},{name:"waterModX0",type:"f32",source:{args:["param:waterX",1600],kind:"expr",op:"mod"}},{name:"waterModZ0",type:"f32",source:{args:["param:waterZ",1600],kind:"expr",op:"mod"}},{name:"waterModX1",type:"f32",source:{args:["param:waterX",250],kind:"expr",op:"mod"}},{name:"waterModZ1",type:"f32",source:{args:["param:waterZ",250],kind:"expr",op:"mod"}},{name:"waterModX2",type:"f32",source:{args:["param:waterX",37],kind:"expr",op:"mod"}},{name:"waterModZ2",type:"f32",source:{args:["param:waterZ",37],kind:"expr",op:"mod"}},{name:"waterModX3",type:"f32",source:{args:["param:waterX",5.3],kind:"expr",op:"mod"}},{name:"waterModZ3",type:"f32",source:{args:["param:waterZ",5.3],kind:"expr",op:"mod"}},{name:"foamOriginX",type:"i32",source:{args:[{op:"floor",args:[{op:"div",args:[{op:"sub",args:["param:cameraX","param:waterX"]},.5]}]},512],kind:"expr",op:"sub"}},{name:"foamShiftX",type:"i32",source:{kind:"delta",of:"param:foamOriginX"}},{name:"foamOriginMod0X",type:"f32",source:{args:[{op:"mul",args:["param:foamOriginX",.5]},1600],kind:"expr",op:"mod"}},{name:"foamOriginMod1X",type:"f32",source:{args:[{op:"mul",args:["param:foamOriginX",.5]},250],kind:"expr",op:"mod"}},{name:"foamOriginMod2X",type:"f32",source:{args:[{op:"mul",args:["param:foamOriginX",.5]},37],kind:"expr",op:"mod"}},{name:"foamOriginMod3X",type:"f32",source:{args:[{op:"mul",args:["param:foamOriginX",.5]},5.3],kind:"expr",op:"mod"}},{name:"foamOriginZ",type:"i32",source:{args:[{op:"floor",args:[{op:"div",args:[{op:"sub",args:["param:cameraZ","param:waterZ"]},.5]}]},512],kind:"expr",op:"sub"}},{name:"foamShiftZ",type:"i32",source:{kind:"delta",of:"param:foamOriginZ"}},{name:"foamOriginMod0Z",type:"f32",source:{args:[{op:"mul",args:["param:foamOriginZ",.5]},1600],kind:"expr",op:"mod"}},{name:"foamOriginMod1Z",type:"f32",source:{args:[{op:"mul",args:["param:foamOriginZ",.5]},250],kind:"expr",op:"mod"}},{name:"foamOriginMod2Z",type:"f32",source:{args:[{op:"mul",args:["param:foamOriginZ",.5]},37],kind:"expr",op:"mod"}},{name:"foamOriginMod3Z",type:"f32",source:{args:[{op:"mul",args:["param:foamOriginZ",.5]},5.3],kind:"expr",op:"mod"}},{name:"hullX",type:"f32",source:{kind:"value",value:0}},{name:"hullZ",type:"f32",source:{kind:"value",value:0}},{name:"hullHeading",type:"f32",source:{kind:"value",value:0}},{name:"hullSpeed",type:"f32",source:{kind:"value",value:0}},{name:"wakeEnabled",type:"bool",source:{kind:"value",value:0}},{name:"hullLength",type:"f32",source:{kind:"value",value:200}},{name:"hullBeam",type:"f32",source:{kind:"value",value:30}},{name:"wakeCell",type:"f32",source:{kind:"value",value:4}},{name:"wakeDepth",type:"f32",source:{kind:"value",value:.4}},{name:"wakeGravity",type:"f32",source:{kind:"value",value:9.81}},{name:"wakeAlpha",type:"f32",source:{kind:"value",value:.02}},{name:"wakeMinLambdaCells",type:"f32",source:{kind:"value",value:8}},{name:"wakeFoamGain",type:"f32",source:{kind:"value",value:1}},{name:"wakeFoamTau",type:"f32",source:{kind:"value",value:45}},{name:"wakeOn",type:"f32",source:{args:["param:wakeEnabled",1],kind:"expr",op:"mul"}},{name:"wakeFoamGainEff",type:"f32",source:{args:["param:wakeEnabled","param:wakeFoamGain"],kind:"expr",op:"mul"}},{name:"wakeEdge",type:"i32",source:{kind:"delta",of:"param:wakeEnabled"}},{name:"wakeReset",type:"u32",source:{args:["param:wakeEdge",0],kind:"expr",op:"max"}},{name:"wakeLength",type:"f32",source:{args:["param:wakeCell",256],kind:"expr",op:"mul"}},{name:"hullWaterX",type:"f32",source:{args:["param:hullX","param:waterX"],kind:"expr",op:"sub"}},{name:"wakeOriginX",type:"i32",source:{args:[{op:"div",args:[{op:"sub",args:[{op:"sub",args:["param:hullWaterX",{op:"mul",args:[{op:"sin",args:["param:hullHeading"]},{op:"mul",args:["param:wakeCell",51.2]}]}]},{op:"mul",args:["param:wakeCell",128]}]},"param:wakeCell"]}],kind:"expr",op:"floor"}},{name:"wakeShiftX",type:"i32",source:{kind:"delta",of:"param:wakeOriginX"}},{name:"wakeHullU",type:"f32",source:{args:["param:hullWaterX",{op:"mul",args:["param:wakeOriginX","param:wakeCell"]}],kind:"expr",op:"sub"}},{name:"wakeOriginModX",type:"i32",source:{args:["param:wakeOriginX",256],kind:"expr",op:"mod"}},{name:"wakeRelX",type:"f32",source:{args:[{op:"mul",args:["param:foamOriginX",.5]},{op:"mul",args:["param:wakeOriginX","param:wakeCell"]}],kind:"expr",op:"sub"}},{name:"wakeCamRelX",type:"f32",source:{args:[{op:"add",args:["param:waterX",{op:"mul",args:["param:wakeOriginX","param:wakeCell"]}]},"param:cameraX"],kind:"expr",op:"sub"}},{name:"hullWaterZ",type:"f32",source:{args:["param:hullZ","param:waterZ"],kind:"expr",op:"sub"}},{name:"wakeOriginZ",type:"i32",source:{args:[{op:"div",args:[{op:"sub",args:[{op:"sub",args:["param:hullWaterZ",{op:"mul",args:[{op:"cos",args:["param:hullHeading"]},{op:"mul",args:["param:wakeCell",51.2]}]}]},{op:"mul",args:["param:wakeCell",128]}]},"param:wakeCell"]}],kind:"expr",op:"floor"}},{name:"wakeShiftZ",type:"i32",source:{kind:"delta",of:"param:wakeOriginZ"}},{name:"wakeHullV",type:"f32",source:{args:["param:hullWaterZ",{op:"mul",args:["param:wakeOriginZ","param:wakeCell"]}],kind:"expr",op:"sub"}},{name:"wakeOriginModZ",type:"i32",source:{args:["param:wakeOriginZ",256],kind:"expr",op:"mod"}},{name:"wakeRelZ",type:"f32",source:{args:[{op:"mul",args:["param:foamOriginZ",.5]},{op:"mul",args:["param:wakeOriginZ","param:wakeCell"]}],kind:"expr",op:"sub"}},{name:"wakeCamRelZ",type:"f32",source:{args:[{op:"add",args:["param:waterZ",{op:"mul",args:["param:wakeOriginZ","param:wakeCell"]}]},"param:cameraZ"],kind:"expr",op:"sub"}},{name:"wakeGEff",type:"f32",source:{args:[{op:"min",args:[{op:"min",args:["param:wakeGravity",{op:"mul",args:[{op:"mul",args:["param:hullSpeed","param:hullSpeed"]},{op:"div",args:[6.283185307179586,{op:"mul",args:["param:wakeMinLambdaCells","param:wakeCell"]}]}]}]},{op:"mul",args:["param:wakeCell",16.39001041697031]}]},.05],kind:"expr",op:"max"}},{name:"wakeDt",type:"f32",source:{args:[{op:"mul",args:["param:dt","param:timeScale"]},.2],kind:"expr",op:"min"}},{name:"wakeSrcLength",type:"f32",source:{args:[{op:"min",args:["param:hullLength",{op:"div",args:[{op:"mul",args:[{op:"mul",args:["param:hullSpeed","param:hullSpeed"]},3.141592653589793]},"param:wakeGEff"]}]},{op:"mul",args:["param:wakeCell",2]}],kind:"expr",op:"max"}},{name:"wakeSrcBeam",type:"f32",source:{args:[{op:"min",args:["param:hullBeam",{op:"mul",args:["param:wakeSrcLength",.67]}]},{op:"mul",args:["param:wakeCell",1.5]}],kind:"expr",op:"max"}}],resources:[{name:"h0_0",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"omega_0",kind:"buffer",elementType:"f32",count:4096,width:64},{name:"fftA_0",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"fftB_0",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"fftA2_0",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"fftB2_0",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"dispTex0",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"slopeTex0",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"h0_1",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"omega_1",kind:"buffer",elementType:"f32",count:65536,width:256},{name:"fftA_1",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"fftB_1",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"fftA2_1",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"fftB2_1",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"dispTex1",kind:"texture",format:"rgba16float",width:256,height:256,mipLevels:9},{name:"slopeTex1",kind:"texture",format:"rgba16float",width:256,height:256,mipLevels:9},{name:"h0_2",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"omega_2",kind:"buffer",elementType:"f32",count:65536,width:256},{name:"fftA_2",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"fftB_2",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"fftA2_2",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"fftB2_2",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"dispTex2",kind:"texture",format:"rgba16float",width:256,height:256,mipLevels:9},{name:"slopeTex2",kind:"texture",format:"rgba16float",width:256,height:256,mipLevels:9},{name:"h0_3",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"omega_3",kind:"buffer",elementType:"f32",count:65536,width:256},{name:"fftA_3",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"fftB_3",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"fftA2_3",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"fftB2_3",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"dispTex3",kind:"texture",format:"rgba16float",width:256,height:256,mipLevels:9},{name:"slopeTex3",kind:"texture",format:"rgba16float",width:256,height:256,mipLevels:9},{name:"wakeA",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"wakeB",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"wakeF",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"wakeG",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"wakeC",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"wakeTex",kind:"texture",format:"rgba16float",width:256,height:256,mipLevels:9},{name:"foamA",kind:"buffer",elementType:"f32",count:1048576,width:1024},{name:"foamB",kind:"buffer",elementType:"f32",count:1048576,width:1024},{name:"foamTex",kind:"texture",format:"rgba16float",width:1024,height:1024,mipLevels:11},{name:"fftB_0_w",kind:"buffer",elementType:"vec4<f32>",count:4096,width:1024},{name:"fftB_1_w",kind:"buffer",elementType:"vec4<f32>",count:65536,width:1024},{name:"fftB_2_w",kind:"buffer",elementType:"vec4<f32>",count:65536,width:1024},{name:"fftB_3_w",kind:"buffer",elementType:"vec4<f32>",count:65536,width:1024},{name:"wakeB_w",kind:"buffer",elementType:"vec4<f32>",count:65536,width:1024}],passes:[{name:"c0.spectrum",kernel:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",target:{width:64,height:64,elioWidth:64},bindings:[{name:"h0",resource:"h0_0"},{name:"omega",resource:"omega_0"}],uniforms:[{name:"spec",values:{cascade:0,fetchKm:"param:fetch",gamma:"param:gamma",kMax:.12,kMin:.003926990816987242,loopPeriod:"param:loopPeriod",n:64,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:1600,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c0.evolve",kernel:"ocean_evolve",target:{width:64,height:64,elioWidth:64},bindings:[{name:"h0",resource:"h0_0"},{name:"omega",resource:"omega_0"},{name:"outA",resource:"fftA_0"},{name:"outB",resource:"fftB_0"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:64,t:"param:phaseTime",tile:1600}}]},{name:"c0.fft.rows",type:"repeat",count:6,index:"s",swap:[["fftA_0","fftA2_0"],["fftB_0","fftB2_0"]],body:[{name:"A",kernel:"fft_gather_64",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftA_0"},{name:"dst",resource:"fftA2_0"}],uniforms:[{name:"fft",values:{axis:0,n:64,stage:"param:s"}}]},{name:"B",kernel:"fft_gather_64",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftB_0"},{name:"dst",resource:"fftB2_0"}],uniforms:[{name:"fft",values:{axis:0,n:64,stage:"param:s"}}]}]},{name:"c0.fft.cols",type:"repeat",count:6,index:"s",swap:[["fftA_0","fftA2_0"],["fftB_0","fftB2_0"]],body:[{name:"A",kernel:"fft_gather_64",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftA_0"},{name:"dst",resource:"fftA2_0"}],uniforms:[{name:"fft",values:{axis:1,n:64,stage:"param:s"}}]},{name:"B",kernel:"fft_gather_64",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftB_0"},{name:"dst",resource:"fftB2_0"}],uniforms:[{name:"fft",values:{axis:1,n:64,stage:"param:s"}}]}]},{name:"c0.assemble",kernel:"ocean_assemble",target:{width:64,height:64,elioWidth:64},bindings:[{name:"fieldA",resource:"fftA_0"},{name:"fieldB",resource:"fftB_0"},{name:"disp",resource:"dispTex0"},{name:"slope",resource:"slopeTex0"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:64}}]},{name:"c0.dispTex0.mips",type:"mipmap",resource:"dispTex0"},{name:"c0.slopeTex0.mips",type:"mipmap",resource:"slopeTex0"},{name:"c1.spectrum",kernel:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",target:{width:256,height:256,elioWidth:256},bindings:[{name:"h0",resource:"h0_1"},{name:"omega",resource:"omega_1"}],uniforms:[{name:"spec",values:{cascade:1,fetchKm:"param:fetch",gamma:"param:gamma",kMax:.8,kMin:.12,loopPeriod:"param:loopPeriod",n:256,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:250,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c1.evolve",kernel:"ocean_evolve",target:{width:256,height:256,elioWidth:256},bindings:[{name:"h0",resource:"h0_1"},{name:"omega",resource:"omega_1"},{name:"outA",resource:"fftA_1"},{name:"outB",resource:"fftB_1"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:256,t:"param:phaseTime",tile:250}}]},{name:"c1.fft.rows",type:"repeat",count:8,index:"s",swap:[["fftA_1","fftA2_1"],["fftB_1","fftB2_1"]],body:[{name:"A",kernel:"fft_gather_256",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"fftA_1"},{name:"dst",resource:"fftA2_1"}],uniforms:[{name:"fft",values:{axis:0,n:256,stage:"param:s"}}]},{name:"B",kernel:"fft_gather_256",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"fftB_1"},{name:"dst",resource:"fftB2_1"}],uniforms:[{name:"fft",values:{axis:0,n:256,stage:"param:s"}}]}]},{name:"c1.fft.cols",type:"repeat",count:8,index:"s",swap:[["fftA_1","fftA2_1"],["fftB_1","fftB2_1"]],body:[{name:"A",kernel:"fft_gather_256",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"fftA_1"},{name:"dst",resource:"fftA2_1"}],uniforms:[{name:"fft",values:{axis:1,n:256,stage:"param:s"}}]},{name:"B",kernel:"fft_gather_256",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"fftB_1"},{name:"dst",resource:"fftB2_1"}],uniforms:[{name:"fft",values:{axis:1,n:256,stage:"param:s"}}]}]},{name:"c1.assemble",kernel:"ocean_assemble",target:{width:256,height:256,elioWidth:256},bindings:[{name:"fieldA",resource:"fftA_1"},{name:"fieldB",resource:"fftB_1"},{name:"disp",resource:"dispTex1"},{name:"slope",resource:"slopeTex1"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:256}}]},{name:"c1.dispTex1.mips",type:"mipmap",resource:"dispTex1"},{name:"c1.slopeTex1.mips",type:"mipmap",resource:"slopeTex1"},{name:"c2.spectrum",kernel:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",target:{width:256,height:256,elioWidth:256},bindings:[{name:"h0",resource:"h0_2"},{name:"omega",resource:"omega_2"}],uniforms:[{name:"spec",values:{cascade:2,fetchKm:"param:fetch",gamma:"param:gamma",kMax:5.4,kMin:.8,loopPeriod:"param:loopPeriod",n:256,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:37,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c2.evolve",kernel:"ocean_evolve",target:{width:256,height:256,elioWidth:256},bindings:[{name:"h0",resource:"h0_2"},{name:"omega",resource:"omega_2"},{name:"outA",resource:"fftA_2"},{name:"outB",resource:"fftB_2"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:256,t:"param:phaseTime",tile:37}}]},{name:"c2.fft.rows",type:"repeat",count:8,index:"s",swap:[["fftA_2","fftA2_2"],["fftB_2","fftB2_2"]],body:[{name:"A",kernel:"fft_gather_256",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"fftA_2"},{name:"dst",resource:"fftA2_2"}],uniforms:[{name:"fft",values:{axis:0,n:256,stage:"param:s"}}]},{name:"B",kernel:"fft_gather_256",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"fftB_2"},{name:"dst",resource:"fftB2_2"}],uniforms:[{name:"fft",values:{axis:0,n:256,stage:"param:s"}}]}]},{name:"c2.fft.cols",type:"repeat",count:8,index:"s",swap:[["fftA_2","fftA2_2"],["fftB_2","fftB2_2"]],body:[{name:"A",kernel:"fft_gather_256",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"fftA_2"},{name:"dst",resource:"fftA2_2"}],uniforms:[{name:"fft",values:{axis:1,n:256,stage:"param:s"}}]},{name:"B",kernel:"fft_gather_256",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"fftB_2"},{name:"dst",resource:"fftB2_2"}],uniforms:[{name:"fft",values:{axis:1,n:256,stage:"param:s"}}]}]},{name:"c2.assemble",kernel:"ocean_assemble",target:{width:256,height:256,elioWidth:256},bindings:[{name:"fieldA",resource:"fftA_2"},{name:"fieldB",resource:"fftB_2"},{name:"disp",resource:"dispTex2"},{name:"slope",resource:"slopeTex2"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:256}}]},{name:"c2.dispTex2.mips",type:"mipmap",resource:"dispTex2"},{name:"c2.slopeTex2.mips",type:"mipmap",resource:"slopeTex2"},{name:"c3.spectrum",kernel:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",target:{width:256,height:256,elioWidth:256},bindings:[{name:"h0",resource:"h0_3"},{name:"omega",resource:"omega_3"}],uniforms:[{name:"spec",values:{cascade:3,fetchKm:"param:fetch",gamma:"param:gamma",kMax:37,kMin:5.4,loopPeriod:"param:loopPeriod",n:256,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:5.3,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c3.evolve",kernel:"ocean_evolve",target:{width:256,height:256,elioWidth:256},bindings:[{name:"h0",resource:"h0_3"},{name:"omega",resource:"omega_3"},{name:"outA",resource:"fftA_3"},{name:"outB",resource:"fftB_3"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:256,t:"param:phaseTime",tile:5.3}}]},{name:"c3.fft.rows",type:"repeat",count:8,index:"s",swap:[["fftA_3","fftA2_3"],["fftB_3","fftB2_3"]],body:[{name:"A",kernel:"fft_gather_256",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"fftA_3"},{name:"dst",resource:"fftA2_3"}],uniforms:[{name:"fft",values:{axis:0,n:256,stage:"param:s"}}]},{name:"B",kernel:"fft_gather_256",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"fftB_3"},{name:"dst",resource:"fftB2_3"}],uniforms:[{name:"fft",values:{axis:0,n:256,stage:"param:s"}}]}]},{name:"c3.fft.cols",type:"repeat",count:8,index:"s",swap:[["fftA_3","fftA2_3"],["fftB_3","fftB2_3"]],body:[{name:"A",kernel:"fft_gather_256",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"fftA_3"},{name:"dst",resource:"fftA2_3"}],uniforms:[{name:"fft",values:{axis:1,n:256,stage:"param:s"}}]},{name:"B",kernel:"fft_gather_256",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"fftB_3"},{name:"dst",resource:"fftB2_3"}],uniforms:[{name:"fft",values:{axis:1,n:256,stage:"param:s"}}]}]},{name:"c3.assemble",kernel:"ocean_assemble",target:{width:256,height:256,elioWidth:256},bindings:[{name:"fieldA",resource:"fftA_3"},{name:"fieldB",resource:"fftB_3"},{name:"disp",resource:"dispTex3"},{name:"slope",resource:"slopeTex3"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:256}}]},{name:"c3.dispTex3.mips",type:"mipmap",resource:"dispTex3"},{name:"c3.slopeTex3.mips",type:"mipmap",resource:"slopeTex3"},{name:"wake.source",kernel:"wake_source",when:"param:wakeEnabled",target:{width:256,height:256,elioWidth:256},bindings:[{name:"wakeIn",resource:"wakeA"},{name:"fftOut",resource:"wakeF"},{name:"cleanOut",resource:"wakeC"}],uniforms:[{name:"wake",values:{cell:"param:wakeCell",depth:"param:wakeDepth",dt:"param:wakeDt",foamSpeed:5,fullSpeed:3,heading:"param:hullHeading",hullBeam:"param:hullBeam",hullLen:"param:hullLength",hullU:"param:wakeHullU",hullV:"param:wakeHullV",originX:"param:wakeOriginX",originZ:"param:wakeOriginZ",reset:"param:wakeReset",shiftX:"param:wakeShiftX",shiftZ:"param:wakeShiftZ",speed:"param:hullSpeed",srcBeam:"param:wakeSrcBeam",srcLength:"param:wakeSrcLength",tau:"param:wakeFoamTau",width:256}}]},{name:"wake.fft1.rows",type:"repeat",when:"param:wakeEnabled",count:8,index:"s",swap:[["wakeF","wakeG"]],body:[{name:"stage",kernel:"fft_gather_256",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"wakeF"},{name:"dst",resource:"wakeG"}],uniforms:[{name:"fft",values:{axis:0,n:256,stage:"param:s"}}]}]},{name:"wake.fft1.cols",type:"repeat",when:"param:wakeEnabled",count:8,index:"s",swap:[["wakeF","wakeG"]],body:[{name:"stage",kernel:"fft_gather_256",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"wakeF"},{name:"dst",resource:"wakeG"}],uniforms:[{name:"fft",values:{axis:1,n:256,stage:"param:s"}}]}]},{name:"wake.symbol",kernel:"wake_symbol",when:"param:wakeEnabled",target:{width:256,height:256,elioWidth:256},bindings:[{name:"fftIn",resource:"wakeF"},{name:"fftOut",resource:"wakeG"}],uniforms:[{name:"sym",values:{n:256,window:"param:wakeLength"}}]},{name:"wake.fft2.rows",type:"repeat",when:"param:wakeEnabled",count:8,index:"s",swap:[["wakeG","wakeF"]],body:[{name:"stage",kernel:"fft_gather_256",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"wakeG"},{name:"dst",resource:"wakeF"}],uniforms:[{name:"fft",values:{axis:0,n:256,stage:"param:s"}}]}]},{name:"wake.fft2.cols",type:"repeat",when:"param:wakeEnabled",count:8,index:"s",swap:[["wakeG","wakeF"]],body:[{name:"stage",kernel:"fft_gather_256",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"wakeG"},{name:"dst",resource:"wakeF"}],uniforms:[{name:"fft",values:{axis:1,n:256,stage:"param:s"}}]}]},{name:"wake.step",kernel:"wake_step",when:"param:wakeEnabled",target:{width:256,height:256,elioWidth:256},bindings:[{name:"qIn",resource:"wakeG"},{name:"cleanIn",resource:"wakeC"},{name:"stateOut",resource:"wakeB"},{name:"wakeTex",resource:"wakeTex"}],uniforms:[{name:"wstep",values:{alpha:"param:wakeAlpha",cell:"param:wakeCell",dt:"param:wakeDt",gravity:"param:wakeGEff",originX:"param:wakeOriginX",originZ:"param:wakeOriginZ",sponge:24,spongeRate:.8,width:256}}]},{name:"wake.wakeTex.mips",type:"mipmap",when:"param:wakeEnabled",resource:"wakeTex"},{name:"foam.copy.fftB_0",type:"copy",from:"fftB_0",to:"fftB_0_w",width:1024},{name:"foam.copy.fftB_1",type:"copy",from:"fftB_1",to:"fftB_1_w",width:1024},{name:"foam.copy.fftB_2",type:"copy",from:"fftB_2",to:"fftB_2_w",width:1024},{name:"foam.copy.fftB_3",type:"copy",from:"fftB_3",to:"fftB_3_w",width:1024},{name:"foam.copy.wakeB",type:"copy",from:"wakeB",to:"wakeB_w",width:1024},{name:"foam",kernel:"ocean_foam",target:{width:1024,height:1024,elioWidth:1024},bindings:[{name:"foamIn",resource:"foamA"},{name:"fieldB0",resource:"fftB_0_w"},{name:"fieldB1",resource:"fftB_1_w"},{name:"fieldB2",resource:"fftB_2_w"},{name:"fieldB3",resource:"fftB_3_w"},{name:"foamOut",resource:"foamB"},{name:"foamTex",resource:"foamTex"},{name:"wakeIn",resource:"wakeB_w"}],uniforms:[{name:"foam",values:{cascades:4,cell:.5,dt:"param:dt",gain:"param:foamGain",lambda:"param:lambda",n0:64,n1:256,n2:256,n3:256,omX0:"param:foamOriginMod0X",omX1:"param:foamOriginMod1X",omX2:"param:foamOriginMod2X",omX3:"param:foamOriginMod3X",omZ0:"param:foamOriginMod0Z",omZ1:"param:foamOriginMod1Z",omZ2:"param:foamOriginMod2Z",omZ3:"param:foamOriginMod3Z",originX:"param:foamOriginX",originZ:"param:foamOriginZ",shiftX:"param:foamShiftX",shiftZ:"param:foamShiftZ",size0:1600,size1:250,size2:37,size3:5.3,tau:"param:foamTau",threshold:"param:foamThreshold",wakeCell:"param:wakeCell",wakeGain:"param:wakeFoamGainEff",wakeOriginX:"param:wakeOriginModX",wakeOriginZ:"param:wakeOriginModZ",wakeRelX:"param:wakeRelX",wakeRelZ:"param:wakeRelZ",wakeWidth:256,width:1024}}]},{name:"foamTex.mips",type:"mipmap",resource:"foamTex"}],frameSwaps:[["wakeA","wakeB"],["foamA","foamB"]],exports:{disp0:"dispTex0",disp1:"dispTex1",disp2:"dispTex2",disp3:"dispTex3",foam:"foamTex",slope0:"slopeTex0",slope1:"slopeTex1",slope2:"slopeTex2",slope3:"slopeTex3",wake:"wakeTex"},exportParams:["waterModX0","waterModZ0","waterModX1","waterModZ1","waterModX2","waterModZ2","waterModX3","waterModZ3","wakeCamRelX","wakeCamRelZ","wakeOriginModX","wakeOriginModZ","wakeCell","wakeOn"]},"webgl2/medium":{name:"ocean",backend:"webgl2",tier:"medium",version:1,resources:[{name:"h0_0",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"omega_0",kind:"buffer",elementType:"f32",count:4096,width:64},{name:"fftA_0",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"fftB_0",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"fftA2_0",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"fftB2_0",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"dispTex0",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"slopeTex0",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"h0_1",kind:"buffer",elementType:"vec4<f32>",count:16384,width:128},{name:"omega_1",kind:"buffer",elementType:"f32",count:16384,width:128},{name:"fftA_1",kind:"buffer",elementType:"vec4<f32>",count:16384,width:128},{name:"fftB_1",kind:"buffer",elementType:"vec4<f32>",count:16384,width:128},{name:"fftA2_1",kind:"buffer",elementType:"vec4<f32>",count:16384,width:128},{name:"fftB2_1",kind:"buffer",elementType:"vec4<f32>",count:16384,width:128},{name:"dispTex1",kind:"texture",format:"rgba16float",width:128,height:128,mipLevels:8},{name:"slopeTex1",kind:"texture",format:"rgba16float",width:128,height:128,mipLevels:8},{name:"h0_2",kind:"buffer",elementType:"vec4<f32>",count:16384,width:128},{name:"omega_2",kind:"buffer",elementType:"f32",count:16384,width:128},{name:"fftA_2",kind:"buffer",elementType:"vec4<f32>",count:16384,width:128},{name:"fftB_2",kind:"buffer",elementType:"vec4<f32>",count:16384,width:128},{name:"fftA2_2",kind:"buffer",elementType:"vec4<f32>",count:16384,width:128},{name:"fftB2_2",kind:"buffer",elementType:"vec4<f32>",count:16384,width:128},{name:"dispTex2",kind:"texture",format:"rgba16float",width:128,height:128,mipLevels:8},{name:"slopeTex2",kind:"texture",format:"rgba16float",width:128,height:128,mipLevels:8},{name:"h0_3",kind:"buffer",elementType:"vec4<f32>",count:16384,width:128},{name:"omega_3",kind:"buffer",elementType:"f32",count:16384,width:128},{name:"fftA_3",kind:"buffer",elementType:"vec4<f32>",count:16384,width:128},{name:"fftB_3",kind:"buffer",elementType:"vec4<f32>",count:16384,width:128},{name:"fftA2_3",kind:"buffer",elementType:"vec4<f32>",count:16384,width:128},{name:"fftB2_3",kind:"buffer",elementType:"vec4<f32>",count:16384,width:128},{name:"dispTex3",kind:"texture",format:"rgba16float",width:128,height:128,mipLevels:8},{name:"slopeTex3",kind:"texture",format:"rgba16float",width:128,height:128,mipLevels:8},{name:"wakeA",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"wakeB",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"wakeF",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"wakeG",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"wakeC",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"wakeTex",kind:"texture",format:"rgba16float",width:256,height:256,mipLevels:9},{name:"foamA",kind:"buffer",elementType:"f32",count:1048576,width:1024},{name:"foamB",kind:"buffer",elementType:"f32",count:1048576,width:1024},{name:"foamTex",kind:"texture",format:"rgba16float",width:1024,height:1024,mipLevels:11},{name:"fftB_0_w",kind:"buffer",elementType:"vec4<f32>",count:4096,width:1024},{name:"fftB_1_w",kind:"buffer",elementType:"vec4<f32>",count:16384,width:1024},{name:"fftB_2_w",kind:"buffer",elementType:"vec4<f32>",count:16384,width:1024},{name:"fftB_3_w",kind:"buffer",elementType:"vec4<f32>",count:16384,width:1024},{name:"wakeB_w",kind:"buffer",elementType:"vec4<f32>",count:65536,width:1024}],passes:[{name:"c0.spectrum",kernel:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",target:{width:64,height:64,elioWidth:64},bindings:[{name:"h0",resource:"h0_0"},{name:"omega",resource:"omega_0"}],uniforms:[{name:"spec",values:{cascade:0,fetchKm:"param:fetch",gamma:"param:gamma",kMax:.12,kMin:.003926990816987242,loopPeriod:"param:loopPeriod",n:64,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:1600,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c0.evolve",kernel:"ocean_evolve",target:{width:64,height:64,elioWidth:64},bindings:[{name:"h0",resource:"h0_0"},{name:"omega",resource:"omega_0"},{name:"outA",resource:"fftA_0"},{name:"outB",resource:"fftB_0"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:64,t:"param:phaseTime",tile:1600}}]},{name:"c0.fft.rows",type:"repeat",count:6,index:"s",swap:[["fftA_0","fftA2_0"],["fftB_0","fftB2_0"]],body:[{name:"A",kernel:"fft_gather_64",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftA_0"},{name:"dst",resource:"fftA2_0"}],uniforms:[{name:"fft",values:{axis:0,n:64,stage:"param:s"}}]},{name:"B",kernel:"fft_gather_64",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftB_0"},{name:"dst",resource:"fftB2_0"}],uniforms:[{name:"fft",values:{axis:0,n:64,stage:"param:s"}}]}]},{name:"c0.fft.cols",type:"repeat",count:6,index:"s",swap:[["fftA_0","fftA2_0"],["fftB_0","fftB2_0"]],body:[{name:"A",kernel:"fft_gather_64",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftA_0"},{name:"dst",resource:"fftA2_0"}],uniforms:[{name:"fft",values:{axis:1,n:64,stage:"param:s"}}]},{name:"B",kernel:"fft_gather_64",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftB_0"},{name:"dst",resource:"fftB2_0"}],uniforms:[{name:"fft",values:{axis:1,n:64,stage:"param:s"}}]}]},{name:"c0.assemble",kernel:"ocean_assemble",target:{width:64,height:64,elioWidth:64},bindings:[{name:"fieldA",resource:"fftA_0"},{name:"fieldB",resource:"fftB_0"},{name:"disp",resource:"dispTex0"},{name:"slope",resource:"slopeTex0"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:64}}]},{name:"c0.dispTex0.mips",type:"mipmap",resource:"dispTex0"},{name:"c0.slopeTex0.mips",type:"mipmap",resource:"slopeTex0"},{name:"c1.spectrum",kernel:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",target:{width:128,height:128,elioWidth:128},bindings:[{name:"h0",resource:"h0_1"},{name:"omega",resource:"omega_1"}],uniforms:[{name:"spec",values:{cascade:1,fetchKm:"param:fetch",gamma:"param:gamma",kMax:.8,kMin:.12,loopPeriod:"param:loopPeriod",n:128,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:250,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c1.evolve",kernel:"ocean_evolve",target:{width:128,height:128,elioWidth:128},bindings:[{name:"h0",resource:"h0_1"},{name:"omega",resource:"omega_1"},{name:"outA",resource:"fftA_1"},{name:"outB",resource:"fftB_1"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:128,t:"param:phaseTime",tile:250}}]},{name:"c1.fft.rows",type:"repeat",count:7,index:"s",swap:[["fftA_1","fftA2_1"],["fftB_1","fftB2_1"]],body:[{name:"A",kernel:"fft_gather_128",target:{width:128,height:128,elioWidth:128},bindings:[{name:"src",resource:"fftA_1"},{name:"dst",resource:"fftA2_1"}],uniforms:[{name:"fft",values:{axis:0,n:128,stage:"param:s"}}]},{name:"B",kernel:"fft_gather_128",target:{width:128,height:128,elioWidth:128},bindings:[{name:"src",resource:"fftB_1"},{name:"dst",resource:"fftB2_1"}],uniforms:[{name:"fft",values:{axis:0,n:128,stage:"param:s"}}]}]},{name:"c1.fft.cols",type:"repeat",count:7,index:"s",swap:[["fftA_1","fftA2_1"],["fftB_1","fftB2_1"]],body:[{name:"A",kernel:"fft_gather_128",target:{width:128,height:128,elioWidth:128},bindings:[{name:"src",resource:"fftA_1"},{name:"dst",resource:"fftA2_1"}],uniforms:[{name:"fft",values:{axis:1,n:128,stage:"param:s"}}]},{name:"B",kernel:"fft_gather_128",target:{width:128,height:128,elioWidth:128},bindings:[{name:"src",resource:"fftB_1"},{name:"dst",resource:"fftB2_1"}],uniforms:[{name:"fft",values:{axis:1,n:128,stage:"param:s"}}]}]},{name:"c1.assemble",kernel:"ocean_assemble",target:{width:128,height:128,elioWidth:128},bindings:[{name:"fieldA",resource:"fftA_1"},{name:"fieldB",resource:"fftB_1"},{name:"disp",resource:"dispTex1"},{name:"slope",resource:"slopeTex1"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:128}}]},{name:"c1.dispTex1.mips",type:"mipmap",resource:"dispTex1"},{name:"c1.slopeTex1.mips",type:"mipmap",resource:"slopeTex1"},{name:"c2.spectrum",kernel:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",target:{width:128,height:128,elioWidth:128},bindings:[{name:"h0",resource:"h0_2"},{name:"omega",resource:"omega_2"}],uniforms:[{name:"spec",values:{cascade:2,fetchKm:"param:fetch",gamma:"param:gamma",kMax:5.4,kMin:.8,loopPeriod:"param:loopPeriod",n:128,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:37,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c2.evolve",kernel:"ocean_evolve",target:{width:128,height:128,elioWidth:128},bindings:[{name:"h0",resource:"h0_2"},{name:"omega",resource:"omega_2"},{name:"outA",resource:"fftA_2"},{name:"outB",resource:"fftB_2"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:128,t:"param:phaseTime",tile:37}}]},{name:"c2.fft.rows",type:"repeat",count:7,index:"s",swap:[["fftA_2","fftA2_2"],["fftB_2","fftB2_2"]],body:[{name:"A",kernel:"fft_gather_128",target:{width:128,height:128,elioWidth:128},bindings:[{name:"src",resource:"fftA_2"},{name:"dst",resource:"fftA2_2"}],uniforms:[{name:"fft",values:{axis:0,n:128,stage:"param:s"}}]},{name:"B",kernel:"fft_gather_128",target:{width:128,height:128,elioWidth:128},bindings:[{name:"src",resource:"fftB_2"},{name:"dst",resource:"fftB2_2"}],uniforms:[{name:"fft",values:{axis:0,n:128,stage:"param:s"}}]}]},{name:"c2.fft.cols",type:"repeat",count:7,index:"s",swap:[["fftA_2","fftA2_2"],["fftB_2","fftB2_2"]],body:[{name:"A",kernel:"fft_gather_128",target:{width:128,height:128,elioWidth:128},bindings:[{name:"src",resource:"fftA_2"},{name:"dst",resource:"fftA2_2"}],uniforms:[{name:"fft",values:{axis:1,n:128,stage:"param:s"}}]},{name:"B",kernel:"fft_gather_128",target:{width:128,height:128,elioWidth:128},bindings:[{name:"src",resource:"fftB_2"},{name:"dst",resource:"fftB2_2"}],uniforms:[{name:"fft",values:{axis:1,n:128,stage:"param:s"}}]}]},{name:"c2.assemble",kernel:"ocean_assemble",target:{width:128,height:128,elioWidth:128},bindings:[{name:"fieldA",resource:"fftA_2"},{name:"fieldB",resource:"fftB_2"},{name:"disp",resource:"dispTex2"},{name:"slope",resource:"slopeTex2"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:128}}]},{name:"c2.dispTex2.mips",type:"mipmap",resource:"dispTex2"},{name:"c2.slopeTex2.mips",type:"mipmap",resource:"slopeTex2"},{name:"c3.spectrum",kernel:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",target:{width:128,height:128,elioWidth:128},bindings:[{name:"h0",resource:"h0_3"},{name:"omega",resource:"omega_3"}],uniforms:[{name:"spec",values:{cascade:3,fetchKm:"param:fetch",gamma:"param:gamma",kMax:37,kMin:5.4,loopPeriod:"param:loopPeriod",n:128,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:5.3,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c3.evolve",kernel:"ocean_evolve",target:{width:128,height:128,elioWidth:128},bindings:[{name:"h0",resource:"h0_3"},{name:"omega",resource:"omega_3"},{name:"outA",resource:"fftA_3"},{name:"outB",resource:"fftB_3"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:128,t:"param:phaseTime",tile:5.3}}]},{name:"c3.fft.rows",type:"repeat",count:7,index:"s",swap:[["fftA_3","fftA2_3"],["fftB_3","fftB2_3"]],body:[{name:"A",kernel:"fft_gather_128",target:{width:128,height:128,elioWidth:128},bindings:[{name:"src",resource:"fftA_3"},{name:"dst",resource:"fftA2_3"}],uniforms:[{name:"fft",values:{axis:0,n:128,stage:"param:s"}}]},{name:"B",kernel:"fft_gather_128",target:{width:128,height:128,elioWidth:128},bindings:[{name:"src",resource:"fftB_3"},{name:"dst",resource:"fftB2_3"}],uniforms:[{name:"fft",values:{axis:0,n:128,stage:"param:s"}}]}]},{name:"c3.fft.cols",type:"repeat",count:7,index:"s",swap:[["fftA_3","fftA2_3"],["fftB_3","fftB2_3"]],body:[{name:"A",kernel:"fft_gather_128",target:{width:128,height:128,elioWidth:128},bindings:[{name:"src",resource:"fftA_3"},{name:"dst",resource:"fftA2_3"}],uniforms:[{name:"fft",values:{axis:1,n:128,stage:"param:s"}}]},{name:"B",kernel:"fft_gather_128",target:{width:128,height:128,elioWidth:128},bindings:[{name:"src",resource:"fftB_3"},{name:"dst",resource:"fftB2_3"}],uniforms:[{name:"fft",values:{axis:1,n:128,stage:"param:s"}}]}]},{name:"c3.assemble",kernel:"ocean_assemble",target:{width:128,height:128,elioWidth:128},bindings:[{name:"fieldA",resource:"fftA_3"},{name:"fieldB",resource:"fftB_3"},{name:"disp",resource:"dispTex3"},{name:"slope",resource:"slopeTex3"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:128}}]},{name:"c3.dispTex3.mips",type:"mipmap",resource:"dispTex3"},{name:"c3.slopeTex3.mips",type:"mipmap",resource:"slopeTex3"},{name:"wake.source",kernel:"wake_source",when:"param:wakeEnabled",target:{width:256,height:256,elioWidth:256},bindings:[{name:"wakeIn",resource:"wakeA"},{name:"fftOut",resource:"wakeF"},{name:"cleanOut",resource:"wakeC"}],uniforms:[{name:"wake",values:{cell:"param:wakeCell",depth:"param:wakeDepth",dt:"param:wakeDt",foamSpeed:5,fullSpeed:3,heading:"param:hullHeading",hullBeam:"param:hullBeam",hullLen:"param:hullLength",hullU:"param:wakeHullU",hullV:"param:wakeHullV",originX:"param:wakeOriginX",originZ:"param:wakeOriginZ",reset:"param:wakeReset",shiftX:"param:wakeShiftX",shiftZ:"param:wakeShiftZ",speed:"param:hullSpeed",srcBeam:"param:wakeSrcBeam",srcLength:"param:wakeSrcLength",tau:"param:wakeFoamTau",width:256}}]},{name:"wake.fft1.rows",type:"repeat",when:"param:wakeEnabled",count:8,index:"s",swap:[["wakeF","wakeG"]],body:[{name:"stage",kernel:"fft_gather_256",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"wakeF"},{name:"dst",resource:"wakeG"}],uniforms:[{name:"fft",values:{axis:0,n:256,stage:"param:s"}}]}]},{name:"wake.fft1.cols",type:"repeat",when:"param:wakeEnabled",count:8,index:"s",swap:[["wakeF","wakeG"]],body:[{name:"stage",kernel:"fft_gather_256",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"wakeF"},{name:"dst",resource:"wakeG"}],uniforms:[{name:"fft",values:{axis:1,n:256,stage:"param:s"}}]}]},{name:"wake.symbol",kernel:"wake_symbol",when:"param:wakeEnabled",target:{width:256,height:256,elioWidth:256},bindings:[{name:"fftIn",resource:"wakeF"},{name:"fftOut",resource:"wakeG"}],uniforms:[{name:"sym",values:{n:256,window:"param:wakeLength"}}]},{name:"wake.fft2.rows",type:"repeat",when:"param:wakeEnabled",count:8,index:"s",swap:[["wakeG","wakeF"]],body:[{name:"stage",kernel:"fft_gather_256",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"wakeG"},{name:"dst",resource:"wakeF"}],uniforms:[{name:"fft",values:{axis:0,n:256,stage:"param:s"}}]}]},{name:"wake.fft2.cols",type:"repeat",when:"param:wakeEnabled",count:8,index:"s",swap:[["wakeG","wakeF"]],body:[{name:"stage",kernel:"fft_gather_256",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"wakeG"},{name:"dst",resource:"wakeF"}],uniforms:[{name:"fft",values:{axis:1,n:256,stage:"param:s"}}]}]},{name:"wake.step",kernel:"wake_step",when:"param:wakeEnabled",target:{width:256,height:256,elioWidth:256},bindings:[{name:"qIn",resource:"wakeG"},{name:"cleanIn",resource:"wakeC"},{name:"stateOut",resource:"wakeB"},{name:"wakeTex",resource:"wakeTex"}],uniforms:[{name:"wstep",values:{alpha:"param:wakeAlpha",cell:"param:wakeCell",dt:"param:wakeDt",gravity:"param:wakeGEff",originX:"param:wakeOriginX",originZ:"param:wakeOriginZ",sponge:24,spongeRate:.8,width:256}}]},{name:"wake.wakeTex.mips",type:"mipmap",when:"param:wakeEnabled",resource:"wakeTex"},{name:"foam.copy.fftB_0",type:"copy",from:"fftB_0",to:"fftB_0_w",width:1024},{name:"foam.copy.fftB_1",type:"copy",from:"fftB_1",to:"fftB_1_w",width:1024},{name:"foam.copy.fftB_2",type:"copy",from:"fftB_2",to:"fftB_2_w",width:1024},{name:"foam.copy.fftB_3",type:"copy",from:"fftB_3",to:"fftB_3_w",width:1024},{name:"foam.copy.wakeB",type:"copy",from:"wakeB",to:"wakeB_w",width:1024},{name:"foam",kernel:"ocean_foam",target:{width:1024,height:1024,elioWidth:1024},bindings:[{name:"foamIn",resource:"foamA"},{name:"fieldB0",resource:"fftB_0_w"},{name:"fieldB1",resource:"fftB_1_w"},{name:"fieldB2",resource:"fftB_2_w"},{name:"fieldB3",resource:"fftB_3_w"},{name:"foamOut",resource:"foamB"},{name:"foamTex",resource:"foamTex"},{name:"wakeIn",resource:"wakeB_w"}],uniforms:[{name:"foam",values:{cascades:4,cell:.5,dt:"param:dt",gain:"param:foamGain",lambda:"param:lambda",n0:64,n1:128,n2:128,n3:128,omX0:"param:foamOriginMod0X",omX1:"param:foamOriginMod1X",omX2:"param:foamOriginMod2X",omX3:"param:foamOriginMod3X",omZ0:"param:foamOriginMod0Z",omZ1:"param:foamOriginMod1Z",omZ2:"param:foamOriginMod2Z",omZ3:"param:foamOriginMod3Z",originX:"param:foamOriginX",originZ:"param:foamOriginZ",shiftX:"param:foamShiftX",shiftZ:"param:foamShiftZ",size0:1600,size1:250,size2:37,size3:5.3,tau:"param:foamTau",threshold:"param:foamThreshold",wakeCell:"param:wakeCell",wakeGain:"param:wakeFoamGainEff",wakeOriginX:"param:wakeOriginModX",wakeOriginZ:"param:wakeOriginModZ",wakeRelX:"param:wakeRelX",wakeRelZ:"param:wakeRelZ",wakeWidth:256,width:1024}}]},{name:"foamTex.mips",type:"mipmap",resource:"foamTex"}],frameSwaps:[["wakeA","wakeB"],["foamA","foamB"]],exports:{disp0:"dispTex0",disp1:"dispTex1",disp2:"dispTex2",disp3:"dispTex3",foam:"foamTex",slope0:"slopeTex0",slope1:"slopeTex1",slope2:"slopeTex2",slope3:"slopeTex3",wake:"wakeTex"},paramsFrom:"webgl2/high"},"webgl2/low":{name:"ocean",backend:"webgl2",tier:"low",version:1,resources:[{name:"h0_0",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"omega_0",kind:"buffer",elementType:"f32",count:4096,width:64},{name:"fftA_0",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"fftB_0",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"fftA2_0",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"fftB2_0",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"dispTex0",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"slopeTex0",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"h0_1",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"omega_1",kind:"buffer",elementType:"f32",count:4096,width:64},{name:"fftA_1",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"fftB_1",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"fftA2_1",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"fftB2_1",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"dispTex1",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"slopeTex1",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"h0_2",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"omega_2",kind:"buffer",elementType:"f32",count:4096,width:64},{name:"fftA_2",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"fftB_2",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"fftA2_2",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"fftB2_2",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"dispTex2",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"slopeTex2",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"wakeA",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"wakeB",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"wakeF",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"wakeG",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"wakeC",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"wakeTex",kind:"texture",format:"rgba16float",width:256,height:256,mipLevels:9},{name:"foamA",kind:"buffer",elementType:"f32",count:1048576,width:1024},{name:"foamB",kind:"buffer",elementType:"f32",count:1048576,width:1024},{name:"foamTex",kind:"texture",format:"rgba16float",width:1024,height:1024,mipLevels:11},{name:"fftB_0_w",kind:"buffer",elementType:"vec4<f32>",count:4096,width:1024},{name:"fftB_1_w",kind:"buffer",elementType:"vec4<f32>",count:4096,width:1024},{name:"fftB_2_w",kind:"buffer",elementType:"vec4<f32>",count:4096,width:1024},{name:"wakeB_w",kind:"buffer",elementType:"vec4<f32>",count:65536,width:1024}],passes:[{name:"c0.spectrum",kernel:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",target:{width:64,height:64,elioWidth:64},bindings:[{name:"h0",resource:"h0_0"},{name:"omega",resource:"omega_0"}],uniforms:[{name:"spec",values:{cascade:0,fetchKm:"param:fetch",gamma:"param:gamma",kMax:.12,kMin:.003926990816987242,loopPeriod:"param:loopPeriod",n:64,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:1600,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c0.evolve",kernel:"ocean_evolve",target:{width:64,height:64,elioWidth:64},bindings:[{name:"h0",resource:"h0_0"},{name:"omega",resource:"omega_0"},{name:"outA",resource:"fftA_0"},{name:"outB",resource:"fftB_0"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:64,t:"param:phaseTime",tile:1600}}]},{name:"c0.fft.rows",type:"repeat",count:6,index:"s",swap:[["fftA_0","fftA2_0"],["fftB_0","fftB2_0"]],body:[{name:"A",kernel:"fft_gather_64",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftA_0"},{name:"dst",resource:"fftA2_0"}],uniforms:[{name:"fft",values:{axis:0,n:64,stage:"param:s"}}]},{name:"B",kernel:"fft_gather_64",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftB_0"},{name:"dst",resource:"fftB2_0"}],uniforms:[{name:"fft",values:{axis:0,n:64,stage:"param:s"}}]}]},{name:"c0.fft.cols",type:"repeat",count:6,index:"s",swap:[["fftA_0","fftA2_0"],["fftB_0","fftB2_0"]],body:[{name:"A",kernel:"fft_gather_64",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftA_0"},{name:"dst",resource:"fftA2_0"}],uniforms:[{name:"fft",values:{axis:1,n:64,stage:"param:s"}}]},{name:"B",kernel:"fft_gather_64",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftB_0"},{name:"dst",resource:"fftB2_0"}],uniforms:[{name:"fft",values:{axis:1,n:64,stage:"param:s"}}]}]},{name:"c0.assemble",kernel:"ocean_assemble",target:{width:64,height:64,elioWidth:64},bindings:[{name:"fieldA",resource:"fftA_0"},{name:"fieldB",resource:"fftB_0"},{name:"disp",resource:"dispTex0"},{name:"slope",resource:"slopeTex0"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:64}}]},{name:"c0.dispTex0.mips",type:"mipmap",resource:"dispTex0"},{name:"c0.slopeTex0.mips",type:"mipmap",resource:"slopeTex0"},{name:"c1.spectrum",kernel:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",target:{width:64,height:64,elioWidth:64},bindings:[{name:"h0",resource:"h0_1"},{name:"omega",resource:"omega_1"}],uniforms:[{name:"spec",values:{cascade:1,fetchKm:"param:fetch",gamma:"param:gamma",kMax:.8,kMin:.12,loopPeriod:"param:loopPeriod",n:64,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:250,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c1.evolve",kernel:"ocean_evolve",target:{width:64,height:64,elioWidth:64},bindings:[{name:"h0",resource:"h0_1"},{name:"omega",resource:"omega_1"},{name:"outA",resource:"fftA_1"},{name:"outB",resource:"fftB_1"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:64,t:"param:phaseTime",tile:250}}]},{name:"c1.fft.rows",type:"repeat",count:6,index:"s",swap:[["fftA_1","fftA2_1"],["fftB_1","fftB2_1"]],body:[{name:"A",kernel:"fft_gather_64",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftA_1"},{name:"dst",resource:"fftA2_1"}],uniforms:[{name:"fft",values:{axis:0,n:64,stage:"param:s"}}]},{name:"B",kernel:"fft_gather_64",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftB_1"},{name:"dst",resource:"fftB2_1"}],uniforms:[{name:"fft",values:{axis:0,n:64,stage:"param:s"}}]}]},{name:"c1.fft.cols",type:"repeat",count:6,index:"s",swap:[["fftA_1","fftA2_1"],["fftB_1","fftB2_1"]],body:[{name:"A",kernel:"fft_gather_64",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftA_1"},{name:"dst",resource:"fftA2_1"}],uniforms:[{name:"fft",values:{axis:1,n:64,stage:"param:s"}}]},{name:"B",kernel:"fft_gather_64",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftB_1"},{name:"dst",resource:"fftB2_1"}],uniforms:[{name:"fft",values:{axis:1,n:64,stage:"param:s"}}]}]},{name:"c1.assemble",kernel:"ocean_assemble",target:{width:64,height:64,elioWidth:64},bindings:[{name:"fieldA",resource:"fftA_1"},{name:"fieldB",resource:"fftB_1"},{name:"disp",resource:"dispTex1"},{name:"slope",resource:"slopeTex1"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:64}}]},{name:"c1.dispTex1.mips",type:"mipmap",resource:"dispTex1"},{name:"c1.slopeTex1.mips",type:"mipmap",resource:"slopeTex1"},{name:"c2.spectrum",kernel:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",target:{width:64,height:64,elioWidth:64},bindings:[{name:"h0",resource:"h0_2"},{name:"omega",resource:"omega_2"}],uniforms:[{name:"spec",values:{cascade:2,fetchKm:"param:fetch",gamma:"param:gamma",kMax:5.4,kMin:.8,loopPeriod:"param:loopPeriod",n:64,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:37,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c2.evolve",kernel:"ocean_evolve",target:{width:64,height:64,elioWidth:64},bindings:[{name:"h0",resource:"h0_2"},{name:"omega",resource:"omega_2"},{name:"outA",resource:"fftA_2"},{name:"outB",resource:"fftB_2"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:64,t:"param:phaseTime",tile:37}}]},{name:"c2.fft.rows",type:"repeat",count:6,index:"s",swap:[["fftA_2","fftA2_2"],["fftB_2","fftB2_2"]],body:[{name:"A",kernel:"fft_gather_64",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftA_2"},{name:"dst",resource:"fftA2_2"}],uniforms:[{name:"fft",values:{axis:0,n:64,stage:"param:s"}}]},{name:"B",kernel:"fft_gather_64",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftB_2"},{name:"dst",resource:"fftB2_2"}],uniforms:[{name:"fft",values:{axis:0,n:64,stage:"param:s"}}]}]},{name:"c2.fft.cols",type:"repeat",count:6,index:"s",swap:[["fftA_2","fftA2_2"],["fftB_2","fftB2_2"]],body:[{name:"A",kernel:"fft_gather_64",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftA_2"},{name:"dst",resource:"fftA2_2"}],uniforms:[{name:"fft",values:{axis:1,n:64,stage:"param:s"}}]},{name:"B",kernel:"fft_gather_64",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftB_2"},{name:"dst",resource:"fftB2_2"}],uniforms:[{name:"fft",values:{axis:1,n:64,stage:"param:s"}}]}]},{name:"c2.assemble",kernel:"ocean_assemble",target:{width:64,height:64,elioWidth:64},bindings:[{name:"fieldA",resource:"fftA_2"},{name:"fieldB",resource:"fftB_2"},{name:"disp",resource:"dispTex2"},{name:"slope",resource:"slopeTex2"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:64}}]},{name:"c2.dispTex2.mips",type:"mipmap",resource:"dispTex2"},{name:"c2.slopeTex2.mips",type:"mipmap",resource:"slopeTex2"},{name:"wake.source",kernel:"wake_source",when:"param:wakeEnabled",target:{width:256,height:256,elioWidth:256},bindings:[{name:"wakeIn",resource:"wakeA"},{name:"fftOut",resource:"wakeF"},{name:"cleanOut",resource:"wakeC"}],uniforms:[{name:"wake",values:{cell:"param:wakeCell",depth:"param:wakeDepth",dt:"param:wakeDt",foamSpeed:5,fullSpeed:3,heading:"param:hullHeading",hullBeam:"param:hullBeam",hullLen:"param:hullLength",hullU:"param:wakeHullU",hullV:"param:wakeHullV",originX:"param:wakeOriginX",originZ:"param:wakeOriginZ",reset:"param:wakeReset",shiftX:"param:wakeShiftX",shiftZ:"param:wakeShiftZ",speed:"param:hullSpeed",srcBeam:"param:wakeSrcBeam",srcLength:"param:wakeSrcLength",tau:"param:wakeFoamTau",width:256}}]},{name:"wake.fft1.rows",type:"repeat",when:"param:wakeEnabled",count:8,index:"s",swap:[["wakeF","wakeG"]],body:[{name:"stage",kernel:"fft_gather_256",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"wakeF"},{name:"dst",resource:"wakeG"}],uniforms:[{name:"fft",values:{axis:0,n:256,stage:"param:s"}}]}]},{name:"wake.fft1.cols",type:"repeat",when:"param:wakeEnabled",count:8,index:"s",swap:[["wakeF","wakeG"]],body:[{name:"stage",kernel:"fft_gather_256",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"wakeF"},{name:"dst",resource:"wakeG"}],uniforms:[{name:"fft",values:{axis:1,n:256,stage:"param:s"}}]}]},{name:"wake.symbol",kernel:"wake_symbol",when:"param:wakeEnabled",target:{width:256,height:256,elioWidth:256},bindings:[{name:"fftIn",resource:"wakeF"},{name:"fftOut",resource:"wakeG"}],uniforms:[{name:"sym",values:{n:256,window:"param:wakeLength"}}]},{name:"wake.fft2.rows",type:"repeat",when:"param:wakeEnabled",count:8,index:"s",swap:[["wakeG","wakeF"]],body:[{name:"stage",kernel:"fft_gather_256",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"wakeG"},{name:"dst",resource:"wakeF"}],uniforms:[{name:"fft",values:{axis:0,n:256,stage:"param:s"}}]}]},{name:"wake.fft2.cols",type:"repeat",when:"param:wakeEnabled",count:8,index:"s",swap:[["wakeG","wakeF"]],body:[{name:"stage",kernel:"fft_gather_256",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"wakeG"},{name:"dst",resource:"wakeF"}],uniforms:[{name:"fft",values:{axis:1,n:256,stage:"param:s"}}]}]},{name:"wake.step",kernel:"wake_step",when:"param:wakeEnabled",target:{width:256,height:256,elioWidth:256},bindings:[{name:"qIn",resource:"wakeG"},{name:"cleanIn",resource:"wakeC"},{name:"stateOut",resource:"wakeB"},{name:"wakeTex",resource:"wakeTex"}],uniforms:[{name:"wstep",values:{alpha:"param:wakeAlpha",cell:"param:wakeCell",dt:"param:wakeDt",gravity:"param:wakeGEff",originX:"param:wakeOriginX",originZ:"param:wakeOriginZ",sponge:24,spongeRate:.8,width:256}}]},{name:"wake.wakeTex.mips",type:"mipmap",when:"param:wakeEnabled",resource:"wakeTex"},{name:"foam.copy.fftB_0",type:"copy",from:"fftB_0",to:"fftB_0_w",width:1024},{name:"foam.copy.fftB_1",type:"copy",from:"fftB_1",to:"fftB_1_w",width:1024},{name:"foam.copy.fftB_2",type:"copy",from:"fftB_2",to:"fftB_2_w",width:1024},{name:"foam.copy.wakeB",type:"copy",from:"wakeB",to:"wakeB_w",width:1024},{name:"foam",kernel:"ocean_foam",target:{width:1024,height:1024,elioWidth:1024},bindings:[{name:"foamIn",resource:"foamA"},{name:"fieldB0",resource:"fftB_0_w"},{name:"fieldB1",resource:"fftB_1_w"},{name:"fieldB2",resource:"fftB_2_w"},{name:"fieldB3",resource:"fftB_2_w"},{name:"foamOut",resource:"foamB"},{name:"foamTex",resource:"foamTex"},{name:"wakeIn",resource:"wakeB_w"}],uniforms:[{name:"foam",values:{cascades:3,cell:.5,dt:"param:dt",gain:"param:foamGain",lambda:"param:lambda",n0:64,n1:64,n2:64,n3:64,omX0:"param:foamOriginMod0X",omX1:"param:foamOriginMod1X",omX2:"param:foamOriginMod2X",omX3:"param:foamOriginMod2X",omZ0:"param:foamOriginMod0Z",omZ1:"param:foamOriginMod1Z",omZ2:"param:foamOriginMod2Z",omZ3:"param:foamOriginMod2Z",originX:"param:foamOriginX",originZ:"param:foamOriginZ",shiftX:"param:foamShiftX",shiftZ:"param:foamShiftZ",size0:1600,size1:250,size2:37,size3:37,tau:"param:foamTau",threshold:"param:foamThreshold",wakeCell:"param:wakeCell",wakeGain:"param:wakeFoamGainEff",wakeOriginX:"param:wakeOriginModX",wakeOriginZ:"param:wakeOriginModZ",wakeRelX:"param:wakeRelX",wakeRelZ:"param:wakeRelZ",wakeWidth:256,width:1024}}]},{name:"foamTex.mips",type:"mipmap",resource:"foamTex"}],frameSwaps:[["wakeA","wakeB"],["foamA","foamB"]],exports:{disp0:"dispTex0",disp1:"dispTex1",disp2:"dispTex2",foam:"foamTex",slope0:"slopeTex0",slope1:"slopeTex1",slope2:"slopeTex2",wake:"wakeTex"},paramsFrom:"webgl2/high"}}})})(),(function(){function N(f){return f.replace(/_([a-z0-9])/g,function(k,A){return A.toUpperCase()})}function C(f){return typeof f=="boolean"?f?1:0:Number(f)}function P(f,k,A){if(typeof f=="string"){if(f.indexOf("param:")!==0)throw new Error("elio-graph: "+A+": bad value "+f);const S=f.slice(6);if(!(S in k))throw new Error("elio-graph: parameter "+S+" was not supplied");return C(k[S])}return C(f)}function L(f,k,A,S){const b=new ArrayBuffer(Math.max(16,Math.ceil(f.size/16)*16)),y=new DataView(b),g={};for(const Z of f.fields){let z=k[Z.name];if(z===void 0){if(!/^_pad/.test(Z.name))throw new Error("elio-graph: "+S+": uniform "+f.struct+" lacks a value for "+Z.name);z=0}g[Z.name]=!0;const U=P(z,A,S);if(Z.type==="u32")y.setUint32(Z.offset,U>>>0,!0);else if(Z.type==="i32")y.setInt32(Z.offset,U|0,!0);else if(Z.type==="f32")y.setFloat32(Z.offset,U,!0);else throw new Error("elio-graph: "+S+": unsupported uniform type "+Z.type)}for(const Z of Object.keys(k))if(!g[Z])throw new Error("elio-graph: "+S+": uniform "+f.struct+" has no field "+Z);return b}function c(f){for(const k of Object.keys(f))if(typeof f[k]=="string")return!1;return!0}const e={x:1,z:1,heading:1,speed:1},H={time:1,dt:1,cameraX:1,cameraY:1,cameraZ:1},j={add:function(f,k){return f+k},sub:function(f,k){return f-k},mul:function(f,k){return f*k},div:function(f,k){return k===0?0:f/k},mod:function(f,k){return k===0?0:f-Math.floor(f/k)*k},floor:function(f){return Math.floor(f)},sin:function(f){return Math.sin(f)},cos:function(f){return Math.cos(f)},min:function(f,k){return Math.min(f,k)},max:function(f,k){return Math.max(f,k)}};function K(f,k){return/\[\]$/.test(f)?Float32Array.from(k||[]):(k=Number(k),Number.isFinite(k)?f==="bool"?k?1:0:f==="u32"?Math.max(0,Math.floor(k))>>>0:f==="i32"?Math.trunc(k)|0:k:0)}function i(f){const k={},A={},S={},b={},y={};let g=!0,Z=null,z={};const U={},ie={},J=function(_){return _.tween>0?Z===null?_.tween:Z:0};for(const _ of f){const F=_.source||{kind:"value",value:0};F.kind==="value"&&(y[_.name]=K(_.type,/\[\]$/.test(_.type)?F.value:F.value||0))}const G=function(_,F){const O=F||k;if(typeof _=="number")return _;if(typeof _=="string"&&_.indexOf("param:")===0){const D=_.slice(6);return D in O?O[D]:0}if(_&&typeof _=="object"&&typeof _.op=="string"){const D=j[_.op];if(!D)throw new Error("elio-graph: unknown op "+_.op);return D(G(_.args[0],O),_.args.length>1?G(_.args[1],O):0)}throw new Error("elio-graph: bad argument "+_)},T={current:k,changed:{},setNodes:function(_){z=_||{}},setTween:function(_){Z=Number.isFinite(_)&&_>=0?_:null},set:function(_,F){const O=f.find(function(u){return u.name===_});if(!O||O.source&&O.source.kind!=="value")throw new Error("elio-graph: "+_+" is not a value parameter");const D=K(O.type,F);if(D!==y[_]){if(typeof D!="number"){y[_]=D;return}y[_]=D,J(O)>0&&!g?S[_]={from:k[_],start:null}:delete S[_]}},snapshot:function(){return Object.assign({},k)},derive:function(_,F){const O=Object.assign({},k);for(const D of f){const u=D.source||{kind:"value",value:0};if(u.kind==="frame"&&u.name==="dt")O[D.name]=K(D.type,_);else if(u.kind==="expr")O[D.name]=K(D.type,j[u.op](G(u.args[0],O),u.args.length>1?G(u.args[1],O):0));else if(u.kind==="delta"){const o=String(u.of).replace(/^param:/,"");O[D.name]=K(D.type,F?(O[o]||0)-(F[o]||0):0)}}return O},evaluate:function(_){const F=Number(_.time)||0,O=Number(_.dt)||0,D={},u=function(o){if(o in D)return D[o];const r=typeof _.nodePose=="function"?_.nodePose(o):null;if(!r)return D[o]=null;const X=U[o];let E=Number(r.speed);return Number.isFinite(E)||(E=X&&O>0?Math.hypot(r.x-X.x,r.z-X.z)/O:0),D[o]={x:Number(r.x)||0,z:Number(r.z)||0,heading:Number(r.heading)||0,speed:E}};for(const o of f){const r=o.source||{kind:"value",value:0};let X=0;if(r.kind==="value"){X=y[o.name];const E=S[o.name];if(E){E.start===null&&(E.start=F);const I=Math.min(1,Math.max(0,(F-E.start)/J(o)));I>=1?delete S[o.name]:X=E.from+(X-E.from)*I}}else if(r.kind==="frame"){if(!H[r.name])throw new Error("elio-graph: unknown frame source "+r.name);X=r.name==="time"?F:r.name==="dt"?O:Number(_[r.name])||0}else if(r.kind==="expr"){const E=j[r.op];if(!E)throw new Error("elio-graph: unknown op "+r.op);X=E(G(r.args[0]),r.args.length>1?G(r.args[1]):0)}else if(r.kind==="node"){if(!e[r.field])throw new Error("elio-graph: unknown node field "+r.field);const E=u(r.node in z?z[r.node]:r.node);X=E?E[r.field]:r.field==="speed"?0:ie[o.name]||0,ie[o.name]=X}else if(r.kind==="delta"){const E=String(r.of).replace(/^param:/,"");X=g?0:(k[E]||0)-(A[E]||0)}else if(r.kind==="accumulate")b[o.name]=(b[o.name]||0)+G(r.of)*O,X=b[o.name];else throw new Error("elio-graph: unknown parameter source "+r.kind);k[o.name]=/\[\]$/.test(o.type)?y[o.name]:K(o.type,X)}for(const o of Object.keys(D))D[o]&&(U[o]={x:D[o].x,z:D[o].z});for(const o of f)T.changed[o.name]=g||k[o.name]!==A[o.name];for(const o of f)A[o.name]=k[o.name];g=!1}};return T}function p(f,k,A){if(!f||f==="always")return!0;if(f.indexOf("param:")===0)return!!k[f.slice(6)];if(f.indexOf("changed:")===0)return f.slice(8).split(",").some(function(S){return!!A[S.trim()]});throw new Error("elio-graph: bad when "+f)}function x(f,k){const A=Object.assign({},f);if(f.paramsFrom){const b=k&&k.graphs&&k.graphs[f.paramsFrom];if(!b||b.backend!==f.backend||b.paramsFrom)throw new Error("elio-graph: paramsFrom "+f.paramsFrom+" does not name a graph of "+f.backend+" that owns its parameters");A.params=b.params,A.exportParams=b.exportParams}delete A.paramsFrom,A.params=A.params||[],A.exportParams=A.exportParams||[],A.frameSwaps=A.frameSwaps||[],A.exports=A.exports||{};const S=function(b){const y=Object.assign({},b);if(y.type=b.type||"kernel",y.when=b.when||"always",y.type==="kernel"){const g=k&&k.kernels&&k.kernels[N(b.kernel)];!y.entry&&g&&(y.entry=g.entry),b.bindings&&(y.bindings=b.bindings.map(function(Z){return Object.assign({level:0},Z)})),y.uniforms||(y.uniforms=[])}return y.type==="repeat"&&(y.body=(b.body||[]).map(S)),y};return A.passes=(f.passes||[]).map(S),A}function R(f,k){const A=k+(f.offset||0);return f.shr?Math.max(f.shr>>A,1):A}function V(f){const k={},A={},S={};for(const z of f.resources)k[z.name]=z;for(const z of f.params)S[z.name]=!0;const b=function(z){return z in A?A[z]:z},y=function(z,U,ie,J){const G=[];for(const T of z){if(T.type!=="repeat"){G.push(g(T,U,ie,J));continue}const _=ie+T.name;if(!(T.count>=1))throw new Error("elio-graph: repeat "+_+": count "+T.count+", want at least 1");if(!T.index||S[T.index])throw new Error("elio-graph: repeat "+_+": index "+T.index+" is empty or names a declared param");if(!T.body||!T.body.length)throw new Error("elio-graph: repeat "+_+": body is empty");if(T.index in U)throw new Error("elio-graph: repeat "+_+": index "+T.index+" is already in use by an outer repeat");for(const O of T.swap||[])if(!k[O[0]]||!k[O[1]])throw new Error("elio-graph: repeat "+_+": swap names an unknown resource");const F=T.when&&T.when!=="always"?T.when:J;for(let O=0;O<T.count;O++){const D=Object.assign({},U,{[T.index]:O});for(const u of y(T.body,D,_+"["+O+"].",F))G.push(u);for(const u of T.swap||[]){const o=b(u[0]),r=b(u[1]);A[u[0]]=r,A[u[1]]=o}}}return G},g=function(z,U,ie,J){const G=Object.assign({},z,{name:ie+z.name});return J&&(!z.when||z.when==="always")&&(G.when=J),z.resource&&(G.resource=b(z.resource)),z.from&&(G.from=b(z.from)),z.to&&(G.to=b(z.to)),z.bindings&&(G.bindings=z.bindings.map(function(T){const _=Object.assign({},T,{resource:b(T.resource)});if(T.levelAt){if(!(T.levelAt.index in U))throw new Error("elio-graph: pass "+G.name+": index "+T.levelAt.index+" is not in scope");_.level=R(T.levelAt,U[T.levelAt.index]),delete _.levelAt}return _})),z.uniforms&&(G.uniforms=z.uniforms.map(function(T){const _={};for(const F of Object.keys(T.values)){let O=T.values[F];if(O&&typeof O=="object"){if(!(O.index in U))throw new Error("elio-graph: pass "+G.name+": index "+O.index+" is not in scope");O=R(O,U[O.index])}else typeof O=="string"&&O.indexOf("param:")===0&&O.slice(6)in U&&(O=U[O.slice(6)]);_[F]=O}return Object.assign({},T,{values:_})})),G},Z=y(f.passes,{},"","");for(const z of Object.keys(A))if(A[z]!==z)throw new Error("elio-graph: swaps are not restored: "+z+" still refers to "+A[z]+" at the end of the passes");return Z}function ae(f,k,A){if(!f||f.version!==1)throw new Error("elio-graph: unsupported graph version");if(f.backend!==A)throw new Error("elio-graph: graph is for "+f.backend+", not "+A);const S={};for(const g of f.resources)S[g.name]=g;const b={};for(const g of f.params)b[g.name]=!0;const y=function(g){if(typeof g=="string"&&g.indexOf("param:")===0&&!b[g.slice(6)])throw new Error("elio-graph: undeclared parameter "+g.slice(6))};for(const g of V(f)){const Z=g.when||"always";if(Z.indexOf("param:")===0)y(Z);else if(Z.indexOf("changed:")===0){for(const z of Z.slice(8).split(","))if(!b[z.trim()])throw new Error("elio-graph: undeclared parameter "+z.trim())}else if(Z!=="always")throw new Error("elio-graph: pass "+g.name+": bad when "+Z);if(g.type==="mipmap"){if(!S[g.resource])throw new Error("elio-graph: pass "+g.name+": unknown resource "+g.resource);continue}if(g.type==="copy"){if(A!=="webgl2")throw new Error("elio-graph: pass "+g.name+": copy passes exist only on webgl2");for(const z of[g.from,g.to])if(!S[z]||S[z].kind!=="buffer")throw new Error("elio-graph: pass "+g.name+": unknown resource "+z+" (a copy joins two buffers)");continue}if(!k.kernels[N(g.kernel)])throw new Error("elio-graph: pass "+g.name+": the module has no kernel "+g.kernel);for(const z of g.bindings)if(!S[z.resource])throw new Error("elio-graph: pass "+g.name+": unknown resource "+z.resource);for(const z of g.uniforms||[])for(const U of Object.keys(z.values))y(z.values[U])}for(const g of f.exportParams||[])if(!b[g])throw new Error("elio-graph: exportParams names undeclared parameter "+g);for(const g of f.resources)if(g.feed&&!b[g.feed])throw new Error("elio-graph: resource "+g.name+" is fed by undeclared parameter "+g.feed);for(const g of f.frameSwaps||[])for(const Z of g)if(!S[Z])throw new Error("elio-graph: frameSwaps names unknown resource "+Z);for(const g of Object.keys(f.exports||{})){const Z=S[f.exports[g]];if(!Z||Z.kind!=="texture"&&A!=="webgpu")throw new Error("elio-graph: export "+g+" must name a texture resource")}}function oe(f){const k=f.backend,A=f.module,S=f.host||{},b={graph:null,backend:k,ready:!1,failed:null,disposed:!1,buildCount:0,run:F,nodePose:U,gpuTiming:ie,setValue:_,setNodes:J,setTween:G,value:T,hasParam:z,params:null,exportParams:O,export:D,dispose:o,fail:u};let y=null,g=null;const Z=function(r){if(!b.failed){if(b.failed=r&&r.fallback||"error",b.error=r,y){try{y.dispose()}catch{}y=null}b.ready=!1,f.onFail&&f.onFail(b.failed,r),!(r&&r.fallback)&&typeof console<"u"&&console.warn&&console.warn("elio-graph: graph disabled: "+(r&&r.message?r.message:String(r)))}};try{let r=f.graph||A.graphs&&A.graphs[k+"/"+(f.tier||"high")];if(!r)throw Object.assign(new Error("elio-graph: no graph for "+k+"/"+f.tier),{fallback:"no-graph"});r=x(r,A),ae(r,A,k);const X=window.__gosx_elio_graph.executors[k];if(!X)throw Object.assign(new Error("elio-graph: no executor for "+k),{fallback:"no-executor"});b.graph=r,g=b.params=i(r.params),y=X({steps:V(r).map(function(E){return{pass:E}}),graph:r,module:A,host:S,pack:L,isStatic:c,kernelKey:N,holds:p,label:f.label||r.name}),b.buildCount=1,b.ready=y.isReady()}catch(r){Z(r)}function z(r){return!!(b.graph&&b.graph.params.some(function(X){return X.name===r}))}function U(r){return S.nodePose?S.nodePose(r):null}function ie(){return y&&y.gpuTiming?y.gpuTiming():null}function J(r){g&&g.setNodes(r)}function G(r){g&&g.setTween(r)}function T(r){const X=y&&y.held?y.held(r):void 0;return X!==void 0?X:g&&r in g.current?g.current[r]:0}function _(r,X){if(!(!g||b.failed))try{g.set(r,X)}catch(E){Z(E)}}function F(r){if(!y||b.failed||b.disposed)return!1;try{if(b.ready=y.isReady(),!b.ready)return!1;const X=r&&!r.nodePose&&S.nodePose?Object.assign({},r,{nodePose:S.nodePose}):r||{};return g.evaluate(X),y.run(g.current,X,g.changed,g)!==!1}catch(X){return Z(X),!1}}function O(){const r={};if(g&&b.graph)for(const X of b.graph.exportParams||[])r[X]=T(X);return r}function D(r){return y&&!b.failed?y.export(r):null}function u(r){Z({message:r,fallback:r})}function o(){if(!b.disposed&&(b.disposed=!0,b.ready=!1,y)){try{y.dispose()}catch{}y=null}}return b}window.__gosx_elio_graph={create:oe,expand:V,normalize:x,pack:L,kernelKey:N,check:ae,holds:p,createParams:i,executors:{}}})(),(function(){const N=`#version 300 es
void main() { vec2 p = vec2(float((gl_VertexID << 1) & 2), float(gl_VertexID & 2)); gl_Position = vec4(p * 2. - 1., 0., 1.); }`;function C(c){return{draw:c.getParameter(c.DRAW_FRAMEBUFFER_BINDING),read:c.getParameter(c.READ_FRAMEBUFFER_BINDING),viewport:c.getParameter(c.VIEWPORT),program:c.getParameter(c.CURRENT_PROGRAM),vao:c.getParameter(c.VERTEX_ARRAY_BINDING),active:c.getParameter(c.ACTIVE_TEXTURE),blend:c.isEnabled(c.BLEND),depth:c.isEnabled(c.DEPTH_TEST),cull:c.isEnabled(c.CULL_FACE),scissor:c.isEnabled(c.SCISSOR_TEST),colorMask:c.getParameter(c.COLOR_WRITEMASK),depthMask:c.getParameter(c.DEPTH_WRITEMASK)}}function P(c,e){c.bindBuffer(c.UNIFORM_BUFFER,null),c.bindBufferBase(c.UNIFORM_BUFFER,0,null),c.bindBuffer(c.PIXEL_PACK_BUFFER,null),c.bindBuffer(c.PIXEL_UNPACK_BUFFER,null),c.bindFramebuffer(c.DRAW_FRAMEBUFFER,e.draw),c.bindFramebuffer(c.READ_FRAMEBUFFER,e.read),c.viewport(e.viewport[0],e.viewport[1],e.viewport[2],e.viewport[3]),c.useProgram(e.program),c.bindVertexArray(e.vao),c.activeTexture(e.active);const H=function(j,K){K?c.enable(j):c.disable(j)};H(c.BLEND,e.blend),H(c.DEPTH_TEST,e.depth),H(c.CULL_FACE,e.cull),H(c.SCISSOR_TEST,e.scissor),c.colorMask(e.colorMask[0],e.colorMask[1],e.colorMask[2],e.colorMask[3]),c.depthMask(e.depthMask)}function L(c){const e=c.host.gl,H=c.graph,j=c.module,K=j.kernels,i=c.host.firstUnit||4;if(!(c.host.caps?c.host.caps(e):{}).colorBufferFloat)throw Object.assign(new Error("EXT_color_buffer_float is not available"),{fallback:"EXT_color_buffer_float"});const x=function(a,t){const n=e.getParameter(a);return typeof n=="number"?n:t};if(x(e.MAX_DRAW_BUFFERS,0)<2||x(e.MAX_TEXTURE_IMAGE_UNITS,0)<i+5)throw Object.assign(new Error("too few texture units or draw buffers"),{fallback:"texture-units"});for(const a of c.steps){const t=a.pass;if(!t||t.type!=="kernel")continue;const n=K[c.kernelKey(t.kernel)];if(!n.glsl||!n.gatherLayout)throw Object.assign(new Error("kernel "+t.kernel+" has no GLSL gather form"),{fallback:"kernel:"+t.kernel})}const R=[],V=[],ae=[],oe=[],f=Object.create(null),k={};for(const a of Object.keys(H.exports||{}))k[H.exports[a]]=!0;const A=e.createVertexArray(),S={split:!1,disposed:!1};let b=null;const y=Object.create(null),g=new Map,Z=new Map;let z=0;const U={rgba16float:e.RGBA16F,rgba32float:e.RGBA32F,r32float:e.R32F};function ie(a,t,n,s,B){const M=e.createTexture();return R.push(M),e.activeTexture(e.TEXTURE0+i),e.bindTexture(e.TEXTURE_2D,M),e.texStorage2D(e.TEXTURE_2D,s,n,a,t),e.texParameteri(e.TEXTURE_2D,e.TEXTURE_MIN_FILTER,B?s>1?e.LINEAR_MIPMAP_LINEAR:e.LINEAR:e.NEAREST),e.texParameteri(e.TEXTURE_2D,e.TEXTURE_MAG_FILTER,B?e.LINEAR:e.NEAREST),e.texParameteri(e.TEXTURE_2D,e.TEXTURE_WRAP_S,B?e.REPEAT:e.CLAMP_TO_EDGE),e.texParameteri(e.TEXTURE_2D,e.TEXTURE_WRAP_T,B?e.REPEAT:e.CLAMP_TO_EDGE),M}function J(a,t){const n=e.createShader(a);if(oe.push(n),e.shaderSource(n,t),e.compileShader(n),!e.getShaderParameter(n,e.COMPILE_STATUS))throw new Error("elio-graph: shader did not compile: "+e.getShaderInfoLog(n));return n}function G(a){if(f[a])return f[a];const t=K[c.kernelKey(a)];b||(b=J(e.VERTEX_SHADER,N));const n=J(e.FRAGMENT_SHADER,t.glsl),s=e.createProgram();e.attachShader(s,b),e.attachShader(s,n),e.linkProgram(s);const B=t.gatherLayout,M=f[a]={handle:s,layout:B,kernel:t};if(!e.getProgramParameter(s,e.LINK_STATUS))throw new Error("elio-graph: program "+a+" did not link: "+e.getProgramInfoLog(s));return e.useProgram(s),M.width=e.getUniformLocation(s,"elio_width"),M.lens=B.inputs.map(function(v){return e.getUniformLocation(s,v.length)}),B.inputs.forEach(function(v,l){e.uniform1i(e.getUniformLocation(s,v.sampler),i+l)}),B.uniforms.forEach(function(v,l){e.uniformBlockBinding(s,e.getUniformBlockIndex(s,v.block),l)}),M}function T(a,t){const s=(a.uniformsStd140||a.uniforms).find(function(B){return B.name===t});if(!s)throw new Error("elio-graph: kernel has no uniform block "+t);return s}function _(){for(const a of H.resources){let t;if(a.kind==="buffer"){const n=Math.ceil(a.count/a.width);t={name:a.name,id:++z,kind:"buffer",version:0,w:a.width,h:n,levels:1,tex:ie(a.width,n,e.RGBA32F,1,!1)}}else{const n=a.mipLevels||1,s=U[a.format];if(!s)throw new Error("elio-graph: unsupported format "+a.format);const B=a.format==="rgba16float"&&(n>1||!!k[a.name]);t={name:a.name,id:++z,kind:"texture",version:0,w:a.width,h:a.height,levels:n,tex:ie(a.width,a.height,s,n,B)}}y[a.name]=t}}function F(a){const t=e.createFramebuffer();V.push(t),e.bindFramebuffer(e.FRAMEBUFFER,t);const n=[];for(const s of a){for(e.framebufferTexture2D(e.FRAMEBUFFER,e.COLOR_ATTACHMENT0+s.location,e.TEXTURE_2D,s.res.tex,s.level);n.length<=s.location;)n.push(e.NONE);n[s.location]=e.COLOR_ATTACHMENT0+s.location}return e.drawBuffers(n),{fbo:t,complete:e.checkFramebufferStatus(e.FRAMEBUFFER)===e.FRAMEBUFFER_COMPLETE}}function O(a){const t=a.map(function(M){return M.res.id+"@"+M.level+":"+M.location}).join(","),n=g.get(t+(S.split?"s":""));if(n)return n;let s;const B=function(M){const v=F([M]);if(!v.complete)throw new Error("elio-graph: framebuffer incomplete");return v};if(a.length===1)s=[B(a[0])];else{const M=S.split?null:F(a);M&&M.complete?s=[M]:(S.split=!0,s=a.map(B))}return g.set(t+(S.split?"s":""),s),s}function D(){if(!S.disposed){S.disposed=!0;for(const a of R)e.deleteTexture(a);for(const a of V)e.deleteFramebuffer(a);for(const a of ae)e.deleteBuffer(a);for(const a of Object.keys(f))e.deleteProgram(f[a].handle);for(const a of oe)e.deleteShader(a);e.deleteVertexArray(A),R.length=V.length=ae.length=0,g.clear(),Z.clear()}}let u=[];const o=C(e);try{u=c.steps.map(function(a){const t=a.pass;if(t.type==="mipmap"||t.type==="copy")return{pass:t};const n=G(t.kernel),s=n.layout,B=s.inputs.map(function(l){const W=t.bindings.find(function(te){return te.name===l.name});if(!W)throw new Error("elio-graph: pass "+t.name+" does not bind input "+l.name);return W}),M=s.outputs.map(function(l){const W=t.bindings.find(function(te){return te.name===l.name});if(!W)throw new Error("elio-graph: pass "+t.name+" does not bind output "+l.name);return{binding:W,location:l.location}}),v=(t.uniforms||[]).map(function(l){const W=s.uniforms.findIndex(function(de){return de.name===l.name});if(W<0)throw new Error("elio-graph: pass "+t.name+" has no uniform block "+l.name);const te=T(n.kernel,l.name),fe=e.createBuffer(),$=Math.max(16,Math.ceil(te.size/16)*16);return ae.push(fe),e.bindBuffer(e.UNIFORM_BUFFER,fe),e.bufferData(e.UNIFORM_BUFFER,$,e.DYNAMIC_DRAW),{index:W,layout:te,values:l.values,buffer:fe,size:$,written:!1,constant:c.isStatic(l.values)}});return{pass:t,p:n,inputs:B,outputs:M,blocks:v}}),_()}catch(a){throw D(),a}finally{P(e,o)}function r(a){const t=y[a.from],n=y[a.to];if(t.w*t.h!==n.w*n.h||n.w!==a.width)throw new Error("elio-graph: pass "+a.name+" cannot copy "+t.name+" into "+n.name+" at width "+a.width);if(n.copiedFrom===t&&n.copiedVersion===t.version)return;let s=Z.get(n.id);s||(s=e.createBuffer(),ae.push(s),e.bindBuffer(e.PIXEL_PACK_BUFFER,s),e.bufferData(e.PIXEL_PACK_BUFFER,t.w*t.h*16,e.STREAM_COPY),e.bindBuffer(e.PIXEL_PACK_BUFFER,null),Z.set(n.id,s)),e.bindFramebuffer(e.READ_FRAMEBUFFER,O([{res:t,level:0,location:0}])[0].fbo),e.bindBuffer(e.PIXEL_PACK_BUFFER,s),e.readPixels(0,0,t.w,t.h,e.RGBA,e.FLOAT,0),e.bindBuffer(e.PIXEL_PACK_BUFFER,null),e.bindBuffer(e.PIXEL_UNPACK_BUFFER,s),e.activeTexture(e.TEXTURE0+i),e.bindTexture(e.TEXTURE_2D,n.tex),e.texSubImage2D(e.TEXTURE_2D,0,0,0,n.w,n.h,e.RGBA,e.FLOAT,0),e.bindBuffer(e.PIXEL_UNPACK_BUFFER,null),n.copiedFrom=t,n.copiedVersion=t.version,n.version++}function X(a){for(const t of a){const n=y[t[0]];y[t[0]]=y[t[1]],y[t[1]]=n}}function E(a){for(const t of H.resources){if(!t.feed)continue;const n=y[t.name],s=new Float32Array(n.w*n.h*4);s.set(a[t.feed].subarray(0,s.length)),e.activeTexture(e.TEXTURE0+i),e.bindTexture(e.TEXTURE_2D,n.tex),e.texSubImage2D(e.TEXTURE_2D,0,0,0,n.w,n.h,e.RGBA,e.FLOAT,s)}}function I(a,t){const n=a.pass,s=a.p,B=n.target,M=a.outputs.map(function(l){return{res:y[l.binding.resource],level:l.binding.level||0,location:l.location}}),v=a.inputs.map(function(l){return y[l.resource]});for(const l of v)if(l.kind==="buffer"&&l.w!==B.elioWidth)throw new Error("elio-graph: pass "+n.name+" reads buffer "+l.name+" "+l.w+" wide at elio_width "+B.elioWidth);a.inputs.forEach(function(l,W){for(const te of M)if(te.res===v[W]&&te.level===(l.level||0))throw new Error("elio-graph: pass "+n.name+" reads "+te.res.name+", which it writes")}),e.useProgram(s.handle),e.uniform1ui(s.width,B.elioWidth),v.forEach(function(l,W){e.activeTexture(e.TEXTURE0+i+W),e.bindTexture(e.TEXTURE_2D,l.tex),s.lens[W]&&e.uniform1ui(s.lens[W],l.w*l.h)});for(const l of a.blocks)(!l.constant||!l.written)&&(e.bindBuffer(e.UNIFORM_BUFFER,l.buffer),e.bufferSubData(e.UNIFORM_BUFFER,0,new Uint8Array(c.pack(l.layout,l.values,t,n.name))),l.written=!0),e.bindBufferRange(e.UNIFORM_BUFFER,l.index,l.buffer,0,l.size);for(const l of M)l.res.version++;e.viewport(0,0,B.width,B.height);for(const l of O(M))e.bindFramebuffer(e.FRAMEBUFFER,l.fbo),e.drawArrays(e.TRIANGLES,0,3)}const Q=c.host.profile,re=function(a,t){a.pass.type==="copy"?r(a.pass):a.pass.type==="mipmap"?(e.activeTexture(e.TEXTURE0+i),e.bindTexture(e.TEXTURE_2D,y[a.pass.resource].tex),e.generateMipmap(e.TEXTURE_2D)):I(a,t)},Y=[];for(const a of c.host.throttle||[]){const t=new RegExp(a.match),n=u.filter(function(B){return t.test(B.pass.name)&&!B.group});if(!n.length||!(a.every>1))continue;const s={every:Math.floor(a.every),hold:a.hold?new RegExp(a.hold):null,since:1/0,dt:0,snap:null,runs:!1,derived:null,plans:n};for(const B of n)B.group=s;Y.push(s)}const se=function(a){let t=null,n=!1;for(const s of u){const B=(s.inputs||[]).map(function(M){return M.resource}).concat((s.outputs||[]).map(function(M){return M.binding.resource}),s.pass.resource?[s.pass.resource]:[]);B.indexOf(a[0])<0&&B.indexOf(a[1])<0||(!s.group||t&&t!==s.group?n=!0:t=s.group)}return n?null:t},m=(H.frameSwaps||[]).map(function(a){return{pair:a,group:Y.length?se(a):null}}),w=function(a){return a&&a!=="always"?a.replace(/^(param|changed):/,"").split(",").map(function(t){return t.trim()}):[]},d=function(a,t,n){for(const s of Y){s.dt+=Number(a&&a.dt)||0,s.since++;const B=s.plans.some(function(M){return w(M.pass.when).some(function(v){return t[v]})});s.runs=B||s.since>=s.every,s.runs&&(s.derived=n.derive(s.dt,s.snap))}},h=function(a){for(const t of Y)t.runs&&(t.since=0,t.dt=0,t.snap=a.snapshot())};return{isReady:function(){return!S.disposed},run:function(a,t,n,s){if(e.isContextLost&&e.isContextLost())return!1;const B=C(e);try{e.disable(e.BLEND),e.disable(e.DEPTH_TEST),e.disable(e.CULL_FACE),e.disable(e.SCISSOR_TEST),e.colorMask(!0,!0,!0,!0),e.bindVertexArray(A),E(a),Y.length&&d(t,n,s);for(const v of u){if(v.group&&!v.group.runs||!c.holds(v.pass.when,a,n))continue;const l=v.group?v.group.derived:a;Q?Q(v.pass.name,function(){re(v,l)}):re(v,l)}for(const v of m)(!v.group||v.group.runs)&&X([v.pair]);Y.length&&h(s)}finally{P(e,B)}const M=c.host.textureCache;return M&&(M._sceneTextureEpoch=(M._sceneTextureEpoch||0)+1),!0},export:function(a){const t=y[H.exports[a]];return t?{tex:t.tex,width:t.w,height:t.h,levels:t.levels}:null},held:function(a){for(const t of Y)if(t.hold&&t.hold.test(a)&&t.snap&&a in t.snap)return t.snap[a]},split:function(){return S.split},dispose:D}}window.__gosx_elio_graph.executors.webgl2=L})(),(function(){const N={f32:4,"vec2<f32>":8,"vec4<f32>":16};function C(P){const L=P.host.device,c=P.graph,e=P.module.kernels,H=P.host;if(!L)throw Object.assign(new Error("no device"),{fallback:"no-device"});const j="gosx-elio-"+P.label+"-",K=[],i=[],p=Object.create(null),x=Object.create(null),R=Object.create(null),V=new Map,ae=new Map,oe={disposed:!1};let f=0;const k={};for(const u of Object.keys(c.exports||{}))k[c.exports[u]]=!0;const A=L.queue,S=GPUBufferUsage.STORAGE|GPUBufferUsage.COPY_DST|GPUBufferUsage.COPY_SRC,b=GPUBufferUsage.UNIFORM|GPUBufferUsage.COPY_DST;for(const u of c.resources)if(u.kind==="buffer"){const o=Math.max(16,Math.ceil(u.count*(N[u.elementType]||16)/4)*4),r=L.createBuffer({label:j+u.name,size:o,usage:S});K.push(r),p[u.name]={name:u.name,id:++f,buffer:r}}else{const o=L.createTexture({label:j+u.name,size:[u.width,u.height,1],format:u.format,mipLevelCount:u.mipLevels||1,usage:GPUTextureUsage.STORAGE_BINDING|GPUTextureUsage.TEXTURE_BINDING});i.push(o),p[u.name]={name:u.name,id:++f,texture:o,w:u.width,h:u.height,levels:u.mipLevels||1,format:u.format}}function y(u){return u.map(function(o){const r={binding:o.binding,visibility:GPUShaderStage.COMPUTE};return o.kind==="uniform"?r.buffer={type:"uniform"}:o.kind==="storage-read"?r.buffer={type:"read-only-storage"}:o.kind==="storage-rw"?r.buffer={type:"storage"}:o.kind==="sampled-texture"||o.kind==="texture"?r.texture={sampleType:"unfilterable-float",viewDimension:"2d"}:r.storageTexture={access:"write-only",format:o.format,viewDimension:"2d"},r})}for(const u of P.steps){const o=u.pass;if(!o)continue;if(o.type!=="kernel")throw new Error("elio-graph: pass "+o.name+": "+o.type+" passes exist only on webgl2");const r=o.kernel+"/"+o.entry;if(x[r])continue;const X=e[P.kernelKey(o.kernel)],E=R[o.kernel]||(R[o.kernel]=L.createBindGroupLayout({label:j+o.kernel,entries:y(X.bindings)})),I=L.createShaderModule({label:j+o.kernel,code:X.wgsl}),Q=H.create(L,"compute",{label:j+o.kernel,layout:L.createPipelineLayout({bindGroupLayouts:[E]}),compute:{module:I,entryPoint:o.entry}});x[r]={handle:Q,layout:E,kernel:X}}function g(u){const o=H.validation&&H.validation.get(u.handle);return!o||o.ready===!0?o&&o.pipeline||u.handle:null}let Z=0;const z=P.steps.map(function(u){const o=u.pass,r=x[o.kernel+"/"+o.entry],X=(o.uniforms||[]).map(function(E){const I=r.kernel.uniforms.find(function(Y){return Y.name===E.name});if(!I)throw new Error("elio-graph: pass "+o.name+" has no uniform block "+E.name);const Q=Math.max(16,Math.ceil(I.size/16)*16),re=L.createBuffer({label:j+o.name+"-"+E.name,size:Q,usage:b});return K.push(re),{name:E.name,layout:I,values:E.values,buffer:re,constant:P.isStatic(E.values),written:!1}});return{pass:o,p:r,blocks:X,id:++Z}});function U(u){const o=u.pass,r=[],X=[o.name,u.id];for(const Q of u.p.kernel.bindings){let re;if(Q.kind==="uniform")re={buffer:u.blocks.find(function(Y){return Y.name===Q.name}).buffer};else{const Y=o.bindings.find(function(m){return m.name===Q.name});if(!Y)throw new Error("elio-graph: pass "+o.name+" does not bind "+Q.name);const se=p[Y.resource];X.push(se.id+"@"+(Y.level||0)),Q.kind==="storage-read"||Q.kind==="storage-rw"?re={buffer:se.buffer}:re=se.texture.createView({baseMipLevel:Y.level||0,mipLevelCount:1})}r.push({binding:Q.binding,resource:re})}const E=X.join("|");let I=V.get(E);return I||(I=L.createBindGroup({label:j+o.name,layout:u.p.layout,entries:r}),V.set(E,I)),I}function ie(u){for(const o of u){const r=p[o[0]];p[o[0]]=p[o[1]],p[o[1]]=r}}const J=3,G=120;let T=null;if(H.timing!==!1&&L.features&&L.features.has&&L.features.has("timestamp-query"))try{const u=[],o=L.createQuerySet({label:j+"time",type:"timestamp",count:2}),r=L.createBuffer({label:j+"time-resolve",size:16,usage:GPUBufferUsage.QUERY_RESOLVE|GPUBufferUsage.COPY_SRC});K.push(r);for(let X=0;X<J;X++){const E=L.createBuffer({label:j+"time-"+X,size:16,usage:GPUBufferUsage.MAP_READ|GPUBufferUsage.COPY_DST});K.push(E),u.push({buffer:E,state:"free"})}T={querySet:o,resolve:r,slots:u,samples:[],ms:null}}catch{T=null}function _(){if(T)for(const u of T.slots)u.state==="copied"&&(u.state="mapping",u.buffer.mapAsync(1).then(function(){if(oe.disposed||u.state!=="mapping")return;const o=new BigUint64Array(u.buffer.getMappedRange().slice(0,16));u.buffer.unmap(),u.state="free",o[1]>o[0]&&(T.ms=Number(o[1]-o[0])/1e6,T.samples.push(T.ms),T.samples.length>G&&T.samples.shift())},function(){u.state="free"}))}function F(u){const o=T.slots.find(function(r){return r.state==="free"});u.resolveQuerySet(T.querySet,0,2,T.resolve,0),o&&(u.copyBufferToBuffer(T.resolve,0,o.buffer,0,16),o.state="copied")}const O=L.lost&&L.lost.then?L.lost:null;O&&O.then(function(){D()},function(){D()});function D(){if(!oe.disposed){oe.disposed=!0;for(const u of K)u.destroy();for(const u of i)u.destroy();K.length=i.length=0,V.clear(),ae.clear(),T&&T.querySet.destroy&&T.querySet.destroy()}}return{isReady:function(){if(oe.disposed)return!1;for(const u of Object.keys(x))if(!g(x[u]))return!1;return!0},run:function(u,o,r){const X=o.encoder;if(!X)throw new Error("elio-graph: a webgpu frame needs frame.encoder");for(const I of c.resources)I.feed&&A.writeBuffer(p[I.name].buffer,0,u[I.feed]);_();const E=z.filter(function(I){return P.holds(I.pass.when,u,r)});return E.forEach(function(I,Q){const re=I.pass;for(const m of I.blocks)m.constant&&m.written||(A.writeBuffer(m.buffer,0,new Uint8Array(P.pack(m.layout,m.values,u,re.name))),m.written=!0);const Y={label:j+re.name};T&&(Q===0||Q===E.length-1)&&(Y.timestampWrites={querySet:T.querySet},Q===0&&(Y.timestampWrites.beginningOfPassWriteIndex=0),Q===E.length-1&&(Y.timestampWrites.endOfPassWriteIndex=1));const se=X.beginComputePass(Y);se.setPipeline(g(I.p)),se.setBindGroup(0,U(I)),se.dispatchWorkgroups(re.dispatch[0],re.dispatch[1]||1,re.dispatch[2]||1),se.end()}),T&&E.length&&F(X),ie(c.frameSwaps||[]),!0},export:function(u){const o=p[c.exports[u]];if(o&&o.buffer)return{buffer:o.buffer};if(!o)return null;let r=ae.get(o.texture);return r||(r=o.texture.createView(),ae.set(o.texture,r)),{texture:o.texture,view:r,width:o.w,height:o.h,levels:o.levels}},gpuTiming:function(){return T?{ms:T.ms,samples:T.samples.slice()}:null},dispose:D}}window.__gosx_elio_graph.executors.webgpu=C})(),(function(){const N={uniform:0,sampler:1,disp:2,slope:6,foam:10,wake:11},C={edgeCells:8,k:.2,foam:1.5,slope:3};function P(i,p,x,R){const V=i.indexOf(p);if(V<0)throw new Error("ocean-fft: anchor not found: "+p.slice(0,48));const ae=i.indexOf(x,V);if(ae<0)throw new Error("ocean-fft: anchor not found: "+x.slice(0,48));return i.slice(0,V)+R+i.slice(ae)}function L(i,p,x){if(i.indexOf(p)<0)throw new Error("ocean-fft: anchor not found: "+p.slice(0,48));return i.replace(p,()=>x)}function c(i){let p="const WAKE_EDGE_CELLS: f32 = "+C.edgeCells.toFixed(1)+`;
`;p+=`struct OceanSpec { c: array<vec4f, 4>, f: vec4f, m: vec4f, w: array<vec4f, 4>, k: array<vec4f, 2> };
`,p+="@group("+i+") @binding("+N.uniform+`) var<uniform> sp: OceanSpec;
`,p+="@group("+i+") @binding("+N.sampler+`) var spSampler: sampler;
`;for(let x=0;x<4;x++)p+="@group("+i+") @binding("+(N.disp+x)+") var spDisp"+x+`: texture_2d<f32>;
`;for(let x=0;x<4;x++)p+="@group("+i+") @binding("+(N.slope+x)+") var spSlope"+x+`: texture_2d<f32>;
`;return p+="@group("+i+") @binding("+N.foam+`) var spFoam: texture_2d<f32>;
`,p+="@group("+i+") @binding("+N.wake+`) var spWakeTex: texture_2d<f32>;
`,p+=`fn spWake(rest: vec2f, cam: vec2f) -> vec3f {
`,p+=`  let r = (rest - cam - sp.k[0].xy) / max(sp.k[1].x, 1e-3);
`,p+=`  let edge = min(min(r.x, sp.k[1].z - r.x), min(r.y, sp.k[1].z - r.y));
`,p+=`  return vec3f((r + sp.k[0].zw) / sp.k[1].z, smoothstep(0.0, WAKE_EDGE_CELLS, edge) * sp.k[1].y);
`,p+=`}
`,p+=`fn spLod(c: vec4f, spacing: f32) -> f32 { return max(log2(max(spacing, 1e-3) * c.z / c.x), 0.0); }
`,p}function e(){let i=`  var p = vec3f(xz.x, level, xz.y); var dx = vec3f(1.0, 0.0, 0.0); var dz = vec3f(0.0, 0.0, 1.0);
`;i+=`  var amp = sp.m.x;
`,i+=`  let spacing = max(ocean.p[8].z * ocean.p[8].w * exp(ocean.p[8].w * f32(ring)), r * 6.2831853 / f32(segs));
`;for(let p=0;p<4;p++)i+="  if (sp.c["+p+`].w > 0.5) {
`,i+="    let uv = (xz - sp.w["+p+"].xy) / sp.c["+p+"].x; let lod = spLod(sp.c["+p+"], spacing); let k = smoothstep(0.0, 1.2, depth0 * sp.c["+p+`].y);
`,i+="    let a = textureSampleLevel(spDisp"+p+", spSampler, uv, lod); let b = textureSampleLevel(spSlope"+p+`, spSampler, uv, lod);
`,i+=`    p += a.xyz * k; dx += vec3f(b.z, b.x, a.w) * k; dz += vec3f(a.w, b.y, b.w) * k;
`,i+=`  }
`;return i+=`  let swv = spWake(xz, ocean.p[7].xz);
`,i+=`  if (sp.k[1].w > 0.0) { p = vec3f(xz.x, level, xz.y); dx = vec3f(1.0, 0.0, 0.0); dz = vec3f(0.0, 0.0, 1.0); }
`,i+=`  if (swv.z > 0.0 && sp.k[1].w <= 0.0) {
`,i+=`    let swk = textureSampleLevel(spWakeTex, spSampler, swv.xy, max(log2(max(spacing, 1e-3) / max(sp.k[1].x, 1e-3)), 0.0));
`,i+="    let sws = swv.z * smoothstep(0.0, 1.2, depth0 * "+C.k.toFixed(2)+`);
`,i+=`    p.y += swk.x * sws; dx.y += swk.y * sws; dz.y += swk.z * sws;
`,i+=`  }
`,i}function H(){let i=`  var sGx = 0.0; var sGz = 0.0; var sXX = 0.0; var sZZ = 0.0; var sXZ = 0.0;
`;i+=`  let wxz = in.rest - sp.m.yz;
`;for(let p=0;p<4;p++)i+="  if (sp.c["+p+`].w > 0.5) {
`,i+="    let uv = (in.rest - sp.w["+p+"].xy) / sp.c["+p+"].x; let k = smoothstep(0.0, 1.2, in.depth0 * sp.c["+p+`].y);
`,i+="    let a = textureSample(spDisp"+p+", spSampler, uv); let b = textureSample(spSlope"+p+`, spSampler, uv);
`,i+=`    sGx += b.x * k; sGz += b.y * k; sXX += b.z * k; sZZ += b.w * k; sXZ += a.w * k;
`,i+=`  }
`;return i+=`  let swv = spWake(in.rest, ocean.p[7].xz);
`,i+=`  let swf = textureSample(spWakeTex, spSampler, swv.xy);
`,i+="  let swm = swv.z * smoothstep(0.0, 1.2, in.depth0 * "+C.k.toFixed(2)+`);
`,i+="  sGx += swf.y * swm * "+C.slope.toFixed(2)+"; sGz += swf.z * swm * "+C.slope.toFixed(2)+`;
`,i+=`  let sJ = (1.0 + sXX) * (1.0 + sZZ) - sXZ * sXZ;
`,i+=`  let sPersist = textureSampleLevel(spFoam, spSampler, wxz * sp.f.x, 0.0).r;
`,i+=`  let N = normalize(vec3f(sGz * sXZ - (1.0 + sZZ) * sGx, max(sJ, 0.1), sXZ * sGx - sGz * (1.0 + sXX)));
`,i}const j=`    let sNear = 1.0 - smoothstep(0.7 * sp.m.w, sp.m.w, length(in.world.xz - ocean.p[7].xz));
    let sJf = (1.0 + sp.f.y * sXX) * (1.0 + sp.f.y * sZZ) - sp.f.y * sp.f.y * sXZ * sXZ;
    let sStateless = clamp((sp.f.z - sJf) * sp.f.w, 0.0, 1.0) * lace * bu;
    let sKept = smoothstep(0.04, 0.6, sPersist) * (0.55 + 0.45 * lace);
    foam = mix(sStateless, sKept, sNear);
    foam = max(foam, clamp(swf.w * swv.z * `+C.foam.toFixed(2)+`, 0.0, 1.0) * (0.6 + 0.4 * lace));
`;function K(i,p){let x=i;return x=L(x,"@location(4) depth0: f32 };","@location(4) depth0: f32, @location(5) rest: vec2f };"),x=P(x,"  var p = vec3f(xz.x, level, xz.y);","  let ph = fract(t * ocean.p[0].w / 9.0",e()),x=L(x,"out.depth0 = depth0;","out.depth0 = depth0; out.rest = xz;"),x=L(x,`  let N = od(normalize(in.normal), in.world.xz, t, ocean.p[9].xy);
`,H()),x=L(x,"  if (dist < 250.0) {","  if (dist < 6000.0) {"),x=L(x,`    foam = (1.0 - smoothstep(0.12, 0.35, in.jacobian)) * lace * bu;
`,j),x=L(x,"(1.0 - smoothstep(80.0, 250.0, dist))","(1.0 - smoothstep(1500.0, 6000.0, dist))"),x=L(x,"  return vec4f(color, alpha);",`  if (sp.k[1].w > 0.0) { let hh = clamp(swf.x / sp.k[1].w, -1.0, 1.0); color = select(vec3f(0.25), select(mix(vec3f(1.0), vec3f(1.0, 0.15, 0.1), hh), mix(vec3f(1.0), vec3f(0.1, 0.3, 1.0), -hh), hh < 0.0), swv.z > 0.0); alpha = 1.0; }
  return vec4f(color, alpha);`),x=L(x,"(ambient * 0.55 + sunCol * 0.04 * max(dot(N, L), 0.0))","(ambient * 1.4 + sunCol * 0.2 * max(dot(N, L), 0.0) + vec3f(0.05))"),x+`
`+c(p)}window.__gosx_scene3d_ocean_fft_surface={wgsl:K,SLOT:N}})(),(function(){const N={disp:4,slope:8,foam:12,wake:13},C={edgeCells:8,k:.2,foam:1.5,slope:3};function P(i,p,x){if(i.indexOf(p)<0)throw new Error("ocean-fft: anchor not found: "+p.slice(0,48));return i.replace(p,()=>x)}function L(i,p,x,R){const V=i.indexOf(p);if(V<0)throw new Error("ocean-fft: anchor not found: "+p.slice(0,48));const ae=i.indexOf(x,V);if(ae<0)throw new Error("ocean-fft: anchor not found: "+x.slice(0,48));return i.slice(0,V)+R+i.slice(ae)}function c(){let i=`uniform vec4 u_sp[12];
`;for(let p=0;p<4;p++)i+="uniform sampler2D u_spDisp"+p+`;
`;for(let p=0;p<4;p++)i+="uniform sampler2D u_spSlope"+p+`;
`;return i+=`uniform sampler2D u_spFoam;
`,i+=`uniform sampler2D u_spWake;
`,i+=`vec3 spWake(vec2 rest, vec2 cam) {
`,i+=`  vec2 r = (rest - cam - u_sp[10].xy) / max(u_sp[11].x, 1e-3);
`,i+=`  float edge = min(min(r.x, u_sp[11].z - r.x), min(r.y, u_sp[11].z - r.y));
`,i+="  return vec3((r + u_sp[10].zw) / u_sp[11].z, smoothstep(0., "+C.edgeCells.toFixed(1)+`, edge) * u_sp[11].y);
`,i+=`}
`,i+=`float spLod(vec4 c, float spacing) { return max(log2(max(spacing, 1e-3) * c.z / c.x), 0.); }
`,i}function e(){let i=`  vec3 p = vec3(xz.x, level, xz.y), dx = vec3(1., 0., 0.), dz = vec3(0., 0., 1.);
`;i+=`  float amp = u_sp[5].x;
`,i+=`  float spacing = max(u_ocean[8].z * u_ocean[8].w * exp(u_ocean[8].w * float(ring)), r * 6.2831853 / float(segs));
`;for(let p=0;p<4;p++)i+="  if (u_sp["+p+`].w > 0.5) {
`,i+="    vec2 suv = (xz - u_sp["+(6+p)+"].xy) / u_sp["+p+"].x; float slod = spLod(u_sp["+p+"], spacing); float sk = smoothstep(0., 1.2, depth0 * u_sp["+p+`].y);
`,i+="    vec4 sa = textureLod(u_spDisp"+p+", suv, slod), sb = textureLod(u_spSlope"+p+`, suv, slod);
`,i+=`    p += sa.xyz * sk; dx += vec3(sb.z, sb.x, sa.w) * sk; dz += vec3(sa.w, sb.y, sb.w) * sk;
`,i+=`  }
`;return i+=`  vec3 swv = spWake(xz, u_ocean[7].xz);
`,i+=`  if (u_sp[11].w > 0.) { p = vec3(xz.x, level, xz.y); dx = vec3(1., 0., 0.); dz = vec3(0., 0., 1.); }
`,i+=`  if (swv.z > 0. && u_sp[11].w <= 0.) {
`,i+=`    vec4 swk = textureLod(u_spWake, swv.xy, max(log2(max(spacing, 1e-3) / max(u_sp[11].x, 1e-3)), 0.));
`,i+="    float sws = swv.z * smoothstep(0., 1.2, depth0 * "+C.k.toFixed(2)+`);
`,i+=`    p.y += swk.x * sws; dx.y += swk.y * sws; dz.y += swk.z * sws;
`,i+=`  }
`,i}function H(){let i=`  float sGx = 0., sGz = 0., sXX = 0., sZZ = 0., sXZ = 0.;
`;i+=`  vec2 wxz = v_rest - u_sp[5].yz;
`;for(let p=0;p<4;p++)i+="  if (u_sp["+p+`].w > 0.5) {
`,i+="    vec2 suv = (v_rest - u_sp["+(6+p)+"].xy) / u_sp["+p+"].x; float sk = smoothstep(0., 1.2, v_d0 * u_sp["+p+`].y);
`,i+="    vec4 sa = texture(u_spDisp"+p+", suv), sb = texture(u_spSlope"+p+`, suv);
`,i+=`    sGx += sb.x * sk; sGz += sb.y * sk; sXX += sb.z * sk; sZZ += sb.w * sk; sXZ += sa.w * sk;
`,i+=`  }
`;return i+=`  vec3 swv = spWake(v_rest, u_ocean[7].xz);
`,i+=`  vec4 swf = texture(u_spWake, swv.xy);
`,i+="  float swm = swv.z * smoothstep(0., 1.2, v_d0 * "+C.k.toFixed(2)+`);
`,i+="  sGx += swf.y * swm * "+C.slope.toFixed(2)+"; sGz += swf.z * swm * "+C.slope.toFixed(2)+`;
`,i+=`  float sJ = (1. + sXX) * (1. + sZZ) - sXZ * sXZ;
`,i+=`  float sPersist = textureLod(u_spFoam, wxz * u_sp[4].x, 0.).r;
`,i+=`  vec3 N = normalize(vec3(sGz * sXZ - (1. + sZZ) * sGx, max(sJ, 0.1), sXZ * sGx - sGz * (1. + sXX)));
`,i}const j=`    float sNear = 1. - smoothstep(0.7 * u_sp[5].w, u_sp[5].w, length(v_w.xz - u_ocean[7].xz));
    float sJf = (1. + u_sp[4].y * sXX) * (1. + u_sp[4].y * sZZ) - u_sp[4].y * u_sp[4].y * sXZ * sXZ;
    float sStateless = clamp((u_sp[4].z - sJf) * u_sp[4].w, 0., 1.) * lace * bu;
    float sKept = smoothstep(0.04, 0.6, sPersist) * (0.55 + 0.45 * lace);
    foam = mix(sStateless, sKept, sNear);
    foam = max(foam, clamp(swf.w * swv.z * `+C.foam.toFixed(2)+`, 0., 1.) * (0.6 + 0.4 * lace));
`;function K(i,p){let x=i;x=P(x,"out float v_d0;","out float v_d0; out vec2 v_rest;"),x=P(x,"void main() {",c()+"void main() {"),x=L(x,"  vec3 p = vec3(xz.x, level, xz.y), dx","  float ph = fract(t * u_ocean[0].w / 9.",e()),x=P(x,"v_d0 = depth0;","v_d0 = depth0; v_rest = xz;");let R=p;return R=P(R,"in float v_d0;","in float v_d0; in vec2 v_rest;"),R=P(R,"void main() {",c()+"void main() {"),R=P(R,`  vec3 N = od(normalize(v_n), v_w.xz, t, u_ocean[9].xy);
`,H()),R=P(R,"  if (dist < 250.) {","  if (dist < 6000.) {"),R=P(R,`    foam = (1. - smoothstep(0.12, 0.35, v_j)) * lace * bu;
`,j),R=P(R,"(1. - smoothstep(80., 250., dist))","(1. - smoothstep(1500., 6000., dist))"),R=P(R,"  fragColor = vec4(color, alpha);",`  if (u_sp[11].w > 0.) { float hh = clamp(swf.x / u_sp[11].w, -1., 1.); color = swv.z > 0. ? (hh < 0. ? mix(vec3(1.), vec3(.1, .3, 1.), -hh) : mix(vec3(1.), vec3(1., .15, .1), hh)) : vec3(.25); alpha = 1.; }
  fragColor = vec4(color, alpha);`),R=P(R,"(ambient * 0.55 + sunCol * 0.04 * max(dot(N, L), 0.))","(ambient * 1.4 + sunCol * 0.2 * max(dot(N, L), 0.) + vec3(0.05))"),{vertex:x,fragment:R}}window.__gosx_scene3d_ocean_fft_surface_gl={glsl:K,UNIT:N}})(),(function(){const N=window.__gosx_elio_graph,C=window.__gosx_scene3d_elio_ocean,P=window.__gosx_scene3d_ocean_fft_surface,L=window.__gosx_scene3d_ocean_fft_surface_gl,c=.25,e=3.5,H=240,j=.3,K=function(m){return Math.max(.3,.14*m*m/9.81)},i=48,p=4;let x=null,R=null;function V(m){return m==="low"||m==="medium"?m:"high"}function ae(m){const w=[];let d={cell:.5,width:1024};for(const h of N.expand(m)){const a=h.uniforms&&h.uniforms[0]&&h.uniforms[0].values;h.kernel==="ocean_spectrum"&&a?w.push({tile:a.tile,kMin:a.kMin,kMax:a.kMax,n:a.n}):h.kernel==="ocean_foam"&&a&&(d={cell:a.cell,width:a.width})}return{cascades:w.slice(0,p),foam:d}}function oe(m,w){const d=w.graphParams;if(d)for(const h of Object.keys(d))Number.isFinite(d[h])&&m.hasParam(h)&&m.setValue(h,d[h]);Number.isFinite(w.graphTween)&&m.setTween(w.graphTween),m.setNodes(w.graphNodes||{}),k(m,w)}const f=[["hullX","x"],["hullZ","z"],["hullHeading","heading"],["hullSpeed","speed"]];function k(m,w){const d=w.graphNodes&&w.graphNodes.hull;if(!d)return;const h=m.nodePose?m.nodePose(d):null;if(h)for(const a of f){const t=m.graph.params.find(function(n){return n.name===a[0]});t&&t.source.kind==="value"&&m.setValue(a[0],h[a[1]])}}function A(m,w,d,h,a){d.fill(0);const t=m.exportParams(),n=w.foam;w.cascades.forEach(function(v,l){d[l*4]=v.tile,d[l*4+1]=Math.sqrt(Math.max(v.kMin,1e-6)*v.kMax),d[l*4+2]=v.n,d[l*4+3]=1,d[24+l*4]=t["waterModX"+l]||0,d[24+l*4+1]=t["waterModZ"+l]||0});const s=n.cell*n.width,B=function(v){return v-Math.floor(v/s)*s},M=m.export("wake");if(M){const v=a?m.value("cameraX")-(a.x||0):0,l=a?m.value("cameraZ")-(a.z||0):0;d[40]=(t.wakeCamRelX||0)+v,d[41]=(t.wakeCamRelZ||0)+l,d[42]=t.wakeOriginModX||0,d[43]=t.wakeOriginModZ||0,d[44]=t.wakeCell||0,d[45]=t.wakeOn||0,d[46]=M.width,d[47]=h&&h.wake&&h.wake.debug?j:0}d[16]=1/s,d[17]=1,d[18]=m.value("foamThreshold"),d[19]=e,d[20]=K(m.value("windSpeed")),d[21]=B(m.value("waterX")),d[22]=B(m.value("waterZ")),d[23]=H}function S(m,w){const d=Number.isFinite(w.time)?w.time:0,h=m.last===null?0:Math.min(c,Math.max(0,d-m.last));m.last=d;const a=w.camera||{};return{time:d,dt:h,cameraX:a.x||0,cameraY:a.y||0,cameraZ:a.z||0}}function b(m){const w=window.__gosx_scene3d_node_poses;return w&&w[m]?w[m]:null}function y(m,w){const d=m&&w();if(!d||!(Math.abs(d.value("driftX"))+Math.abs(d.value("driftZ"))>0))return m;const h=m.speed*Math.sin(m.heading)-d.value("driftX"),a=m.speed*Math.cos(m.heading)-d.value("driftZ");return{x:m.x,z:m.z,heading:m.heading,speed:Math.hypot(h,a)}}function g(){return R}function Z(){return x&&!x.failed?x.gpuTiming():null}function z(m){R=m.failed?null:{x:m.value("waterX"),z:m.value("waterZ"),time:m.value("simTime")}}const U=3,ie=250,J=64,G=1,T=8,_=16,F={collect:[],k:0,latest:null,previous:null};function O(){F.collect=[],F.k=0}function D(m,w,d,h){const a=F.k++;F.collect.length<J&&F.collect.push({x:m,z:w});const t=Number.isFinite(h)?h:typeof performance<"u"?performance.now():0,n=F.latest;if(!n||a>=n.count||t-n.at>ie||Math.abs(n.points[a].x-m)>8||Math.abs(n.points[a].z-w)>8)return null;const s=F.previous&&F.previous.count>a?F.previous:null,B=s&&n.at>s.at?(n.heights[a]-s.heights[a])/((n.at-s.at)/1e3):0,M=n.heights[a]+B*((t-n.at)/1e3),v=n.nx[a],l=n.nz[a],W=Math.sqrt(Math.max(0,1-v*v-l*l));return{x:m,y:d+M,z:w,normal:{x:v,y:W,z:l},velocity:{x:0,y:B,z:0}}}function u(m,w){const d=[];for(let a=0;a<U;a++)d.push({buffer:m.createBuffer({label:"gosx-ocean-fft-probe-"+a,size:J*_,usage:G|T}),state:"free",meta:null});const h=function(a){const t=a.buffer,n=a.meta;a.state="mapping",t.mapAsync(G).then(function(){if(a.state!=="mapping")return;const s=new Float32Array(t.getMappedRange().slice(0,n.count*_));t.unmap();const B=[],M=[],v=[];for(let l=0;l<n.count;l++)B.push(s[l*4]),M.push(s[l*4+1]),v.push(s[l*4+2]);F.previous=F.latest,F.latest={at:n.at,count:n.count,points:n.points,heights:B,nx:M,nz:v},a.state="free"},function(){a.state="free"})};return{poll:function(){for(const a of d)a.state==="copied"&&h(a)},copy:function(a,t,n){const s=d.find(function(B){return B.state==="free"});s&&(a.copyBufferToBuffer(t,0,s.buffer,0,J*_),s.state="copied",s.meta={at:w(),count:n.length,points:n})},dispose:function(){for(const a of d)a.state="disposed",a.buffer.destroy()}}}function o(m,w){if(!m.hasParam("probePoints"))return!1;const d=Math.min(w.length,J);if(!d)return m.setValue("probeCount",0),!1;let h=0,a=0;for(let n=0;n<d;n++)h+=w[n].x,a+=w[n].z;h/=d,a/=d;const t=new Float32Array(J*4);for(let n=0;n<d;n++)t[n*4]=w[n].x-h,t[n*4+1]=w[n].z-a;return m.setValue("probeOriginX",h),m.setValue("probeOriginZ",a),m.setValue("probeCount",d),m.setValue("probePoints",t),!0}function r(m,w,d){const h=function(a,t){const n=R;if(!n)return m.sample(w,a,t,d);const s=m.sample(w,a-n.x,t-n.z,n.time);return s.x+=n.x,s.z+=n.z,s};return{sample:h,probed:function(a,t){return D(a,t,w.data[0])||h(a,t)},begin:O}}function X(m){const w=m.device,d="gosx-ocean-fft-",h=function(){const q=GPUShaderStage.VERTEX|GPUShaderStage.FRAGMENT,ne=[];ne.push({binding:P.SLOT.uniform,visibility:q,buffer:{type:"uniform"}},{binding:P.SLOT.sampler,visibility:q,sampler:{type:"filtering"}});for(let ee=0;ee<p;ee++)ne.push({binding:P.SLOT.disp+ee,visibility:q,texture:{sampleType:"float"}},{binding:P.SLOT.slope+ee,visibility:q,texture:{sampleType:"float"}});return ne.push({binding:P.SLOT.foam,visibility:q,texture:{sampleType:"float"}},{binding:P.SLOT.wake,visibility:q,texture:{sampleType:"float"}}),ne},a=w.createBindGroupLayout({label:d+"surface",entries:h()}),t=w.createSampler({label:d+"sampler",magFilter:"linear",minFilter:"linear",mipmapFilter:"linear",addressModeU:"repeat",addressModeV:"repeat"}),n=w.createBuffer({label:d+"surface",size:i*4,usage:GPUBufferUsage.UNIFORM|GPUBufferUsage.COPY_DST}),s=w.createTexture({label:d+"dummy",size:[1,1,1],format:"rgba16float",usage:GPUTextureUsage.TEXTURE_BINDING|GPUTextureUsage.COPY_DST}),B=s.createView(),M=new Float32Array(i),v={last:null,tier:"",runner:null,info:null,viewList:null},l=u(w,function(){return m.now?m.now():typeof performance<"u"?performance.now():0}),W={device:w,ready:!1,layout:a,group:null,disposed:!1,tier:"",update:me,dispose:de,fail:te};function te(q){const ne="ocean-fft: "+(q&&q.message?q.message:String(q));m.guard&&m.guard.fail("gosx-ocean-fft-surface",ne)}function fe(q){v.runner&&v.runner.dispose();const ne=N.create({module:C,backend:"webgpu",tier:q,host:{device:w,create:m.create,validation:m.validation,nodePose:function(ee){return y(b(ee),function(){return v.runner})}}});v.runner=ne,v.tier=q,v.last=null,v.viewList=null,W.group=null,W.ready=!1,v.info=ne.graph?ae(ne.graph):null}function $(){const q=v.runner,ne=[],ee=[{binding:P.SLOT.uniform,resource:{buffer:n}},{binding:P.SLOT.sampler,resource:t}];for(let le=0;le<p;le++){const pe=q.export("disp"+le),we=q.export("slope"+le);ee.push({binding:P.SLOT.disp+le,resource:pe?pe.view:B},{binding:P.SLOT.slope+le,resource:we?we.view:B}),ne.push(pe?pe.view:B,we?we.view:B)}const ue=q.export("foam");ee.push({binding:P.SLOT.foam,resource:ue?ue.view:B}),ne.push(ue?ue.view:B);const ce=q.export("wake");ee.push({binding:P.SLOT.wake,resource:ce?ce.view:B}),ne.push(ce?ce.view:B),!(v.viewList&&ne.every(function(le,pe){return le===v.viewList[pe]}))&&(v.viewList=ne,W.group=w.createBindGroup({label:d+"surface",layout:a,entries:ee}))}function me(q){const ne=V(q.tier);(!v.runner||ne!==v.tier)&&fe(ne);const ee=v.runner;if(ee.failed)return W.ready=!1,W.group=null,!1;oe(ee,q.ocean),l.poll();const ue=F.collect.slice(0,J),ce=o(ee,ue),le=ee.run(Object.assign(S(v,q),{encoder:q.encoder}));if(ee.failed)return W.ready=!1,W.group=null,R=null,m.guard&&m.guard.fail("gosx-elio-ocean","ocean graph failed: "+ee.failed),!1;if(!le)return W.ready;const pe=ce?ee.export("probe"):null;return pe&&pe.buffer&&l.copy(q.encoder,pe.buffer,ue),$(),A(ee,v.info,M,q.ocean,q.camera),w.queue.writeBuffer(n,0,M),z(ee),x=ee,W.ready=!0,!0}function de(){W.disposed||(W.disposed=!0,W.ready=!1,W.group=null,v.runner&&v.runner.dispose(),R=null,x=null,F.latest=F.previous=null,l.dispose(),n.destroy(),s.destroy())}return Object.defineProperty(W,"runner",{get:function(){return v.runner}}),W}function E(m,w){const d=w.environment&&w.environment.ocean;let h=m.fft;const a=w.guard&&(w.guard.disabled.has("oceanFFT")||w.guard.disabled.has("elioGraph"));return!d||!d.spectrum||a||h&&h.device!==w.device?(h&&(h.dispose(),m.fft=null),null):(h||(h=m.fft=X(w)),h.update({ocean:d,tier:w.tier,time:w.time,camera:w.camera,encoder:w.encoder}),h)}const I=[{match:"^wake\\.",every:2,hold:"^(wake|camera[XZ])"}];function Q(){return typeof window<"u"&&window.__gosx_elio_diagnostics||{}}function re(m,w){const d={last:null},h=N.create({module:C,backend:"webgl2",tier:V(w.tier),host:{gl:m,caps:w.caps,textureCache:w.textureCache,profile:w.profile||Q().profile,throttle:w.throttle||Q().throttle||I,nodePose:function(l){return y(b(l),function(){return s.runner})}}}),a=new Float32Array(i),t=new Map,n=h.graph?ae(h.graph):null,s={gl:m,runner:h,tier:V(w.tier),disposed:!1,update:B,bind:M,dispose:v,fail:function(l){h.fail(l)}};function B(l){return h.failed||(oe(h,l.ocean),!h.run(S(d,l)))?!1:(A(h,n,a,l.ocean,l.camera),z(h),!0)}function M(l,W){let te=t.get(W);if(!te){const $=function(me){return l.getUniformLocation(W,me)};te={sp:$("u_sp[0]"),disp:[0,1,2,3].map(function(me){return $("u_spDisp"+me)}),slope:[0,1,2,3].map(function(me){return $("u_spSlope"+me)}),foam:$("u_spFoam"),wake:$("u_spWake")},t.set(W,te)}const fe=function($,me,de){l.activeTexture(l.TEXTURE0+$),l.bindTexture(l.TEXTURE_2D,de?de.tex:s.dummy),l.uniform1i(me,$)};s.dummy||(s.dummy=l.createTexture(),l.bindTexture(l.TEXTURE_2D,s.dummy),l.texStorage2D(l.TEXTURE_2D,1,l.RGBA16F,1,1),l.texParameteri(l.TEXTURE_2D,l.TEXTURE_MIN_FILTER,l.LINEAR),l.texParameteri(l.TEXTURE_2D,l.TEXTURE_MAG_FILTER,l.LINEAR));for(let $=0;$<p;$++)fe(L.UNIT.disp+$,te.disp[$],h.export("disp"+$)),fe(L.UNIT.slope+$,te.slope[$],h.export("slope"+$));fe(L.UNIT.foam,te.foam,h.export("foam")),fe(L.UNIT.wake,te.wake,h.export("wake")),l.uniform4fv(te.sp,a)}function v(){s.disposed||(s.disposed=!0,h.dispose(),s.dummy&&m.deleteTexture(s.dummy))}return s}function Y(m,w,d){let h=m.fft;if(h&&(h.gl!==w||h.disposed||h.tier!==V(d.tier))&&(h.dispose(),h=m.fft=null),m.fftDisabled||w.isContextLost&&w.isContextLost())return null;h||(h=m.fft=re(w,d));const a=h.update({ocean:d.ocean,time:d.time,camera:d.camera});return h.runner.failed?(m.fftDisabled=h.runner.failed,h.dispose(),m.fft=null,R=null,d.mount&&d.mount.setAttribute&&d.mount.setAttribute("data-gosx-scene3d-ocean-fft-fallback",m.fftDisabled),null):a?h:null}function se(m,w,d){m.fft&&m.fft.dispose(),m.fft=null,m.fftDisabled=d,R=null,w&&w.mount&&w.mount.setAttribute&&w.mount.setAttribute("data-gosx-scene3d-ocean-fft-fallback",d)}window.__gosx_scene3d_ocean_fft_api={frame:E,water:g,timing:Z,nodePose:b,waterRelative:y,sampler:r,probe:{begin:O,sample:D},probeState:F,describe:ae,WAKE_DEBUG_RANGE:j,feed:oe,surfaceData:A},window.__gosx_scene3d_ocean_fft_gl_api={run:Y,fail:se}})();
