(function(){"use strict";function q(W){if(W&&typeof W=="object"){for(const S of Object.keys(W))q(W[S]);Object.freeze(W)}return W}window.__gosx_scene3d_elio_ocean=q({elio:"v0.4.3+ocean",set:"ocean",sha256:"e46e6e37557772f654a205cc027621f31842e34c634763713ac9b37f33f600ca",kernels:{oceanSpectrum:{wgsl:`struct OceanSpectrumParams {
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
`,gatherLayout:{kernel:"fft_stage",inputs:[{name:"src",kind:"buffer",elem:"vec4",sampler:"src_tex",length:"src_len",binding:1}],outputs:[{name:"dst",elem:"vec4",format:"",location:0,variable:"dst_out"}],uniforms:[{name:"fft",block:"FFTStage_ubo",struct:"FFTStage",binding:0}]}}},graphs:{"webgpu/high":{name:"ocean",backend:"webgpu",tier:"high",version:1,params:[{name:"windSpeed",type:"f32",source:{kind:"value",value:9},tween:20},{name:"fetch",type:"f32",source:{kind:"value",value:300},tween:20},{name:"gamma",type:"f32",source:{kind:"value",value:3.3},tween:20},{name:"spread",type:"f32",source:{kind:"value",value:8},tween:20},{name:"swell",type:"f32",source:{kind:"value",value:0},tween:20},{name:"windDirDeg",type:"f32",source:{kind:"value",value:0},tween:20},{name:"swellDirDeg",type:"f32",source:{kind:"value",value:0},tween:20},{name:"seed",type:"u32",source:{kind:"value",value:0}},{name:"loopPeriod",type:"f32",source:{kind:"value",value:1e3}},{name:"lambda",type:"f32",source:{kind:"value",value:1},tween:20},{name:"timeScale",type:"f32",source:{kind:"value",value:1}},{name:"driftX",type:"f32",source:{kind:"value",value:0}},{name:"driftZ",type:"f32",source:{kind:"value",value:0}},{name:"foamTau",type:"f32",source:{kind:"value",value:4}},{name:"foamThreshold",type:"f32",source:{kind:"value",value:.9}},{name:"foamGain",type:"f32",source:{kind:"value",value:2}},{name:"probeCount",type:"u32",source:{kind:"value",value:0}},{name:"probeOriginX",type:"f32",source:{kind:"value",value:0}},{name:"probeOriginZ",type:"f32",source:{kind:"value",value:0}},{name:"probePoints",type:"vec4<f32>[]",count:64,source:{kind:"value",value:0}},{name:"dt",type:"f32",source:{kind:"frame",name:"dt"}},{name:"cameraX",type:"f32",source:{kind:"frame",name:"cameraX"}},{name:"cameraZ",type:"f32",source:{kind:"frame",name:"cameraZ"}},{name:"windDirRad",type:"f32",source:{args:["param:windDirDeg",.017453292519943295],kind:"expr",op:"mul"}},{name:"windDir",type:"f32",source:{args:[1.5707963267948966,"param:windDirRad"],kind:"expr",op:"sub"}},{name:"swellDirRad",type:"f32",source:{args:["param:swellDirDeg",.017453292519943295],kind:"expr",op:"mul"}},{name:"swellDir",type:"f32",source:{args:[1.5707963267948966,"param:swellDirRad"],kind:"expr",op:"sub"}},{name:"simTime",type:"f32",source:{kind:"accumulate",of:"param:timeScale"}},{name:"phaseTime",type:"f32",source:{args:["param:simTime","param:loopPeriod"],kind:"expr",op:"mod"}},{name:"waterX",type:"f32",source:{kind:"accumulate",of:"param:driftX"}},{name:"waterZ",type:"f32",source:{kind:"accumulate",of:"param:driftZ"}},{name:"waterModX0",type:"f32",source:{args:["param:waterX",1600],kind:"expr",op:"mod"}},{name:"waterModZ0",type:"f32",source:{args:["param:waterZ",1600],kind:"expr",op:"mod"}},{name:"waterModX1",type:"f32",source:{args:["param:waterX",250],kind:"expr",op:"mod"}},{name:"waterModZ1",type:"f32",source:{args:["param:waterZ",250],kind:"expr",op:"mod"}},{name:"waterModX2",type:"f32",source:{args:["param:waterX",37],kind:"expr",op:"mod"}},{name:"waterModZ2",type:"f32",source:{args:["param:waterZ",37],kind:"expr",op:"mod"}},{name:"waterModX3",type:"f32",source:{args:["param:waterX",5.3],kind:"expr",op:"mod"}},{name:"waterModZ3",type:"f32",source:{args:["param:waterZ",5.3],kind:"expr",op:"mod"}},{name:"foamRelX",type:"f32",source:{args:["param:cameraX","param:waterX"],kind:"expr",op:"sub"}},{name:"foamTexelX",type:"f32",source:{args:["param:foamRelX",.5],kind:"expr",op:"div"}},{name:"foamCellX",type:"f32",source:{args:["param:foamTexelX"],kind:"expr",op:"floor"}},{name:"foamOriginX",type:"i32",source:{args:["param:foamCellX",512],kind:"expr",op:"sub"}},{name:"foamShiftX",type:"i32",source:{kind:"delta",of:"param:foamOriginX"}},{name:"foamOriginMetersX",type:"f32",source:{args:["param:foamOriginX",.5],kind:"expr",op:"mul"}},{name:"foamOriginMod0X",type:"f32",source:{args:["param:foamOriginMetersX",1600],kind:"expr",op:"mod"}},{name:"foamOriginMod1X",type:"f32",source:{args:["param:foamOriginMetersX",250],kind:"expr",op:"mod"}},{name:"foamOriginMod2X",type:"f32",source:{args:["param:foamOriginMetersX",37],kind:"expr",op:"mod"}},{name:"foamOriginMod3X",type:"f32",source:{args:["param:foamOriginMetersX",5.3],kind:"expr",op:"mod"}},{name:"probeLocalX",type:"f32",source:{args:["param:probeOriginX","param:waterX"],kind:"expr",op:"sub"}},{name:"probeOriginMod0X",type:"f32",source:{args:["param:probeLocalX",1600],kind:"expr",op:"mod"}},{name:"probeOriginMod1X",type:"f32",source:{args:["param:probeLocalX",250],kind:"expr",op:"mod"}},{name:"probeOriginMod2X",type:"f32",source:{args:["param:probeLocalX",37],kind:"expr",op:"mod"}},{name:"probeOriginMod3X",type:"f32",source:{args:["param:probeLocalX",5.3],kind:"expr",op:"mod"}},{name:"foamRelZ",type:"f32",source:{args:["param:cameraZ","param:waterZ"],kind:"expr",op:"sub"}},{name:"foamTexelZ",type:"f32",source:{args:["param:foamRelZ",.5],kind:"expr",op:"div"}},{name:"foamCellZ",type:"f32",source:{args:["param:foamTexelZ"],kind:"expr",op:"floor"}},{name:"foamOriginZ",type:"i32",source:{args:["param:foamCellZ",512],kind:"expr",op:"sub"}},{name:"foamShiftZ",type:"i32",source:{kind:"delta",of:"param:foamOriginZ"}},{name:"foamOriginMetersZ",type:"f32",source:{args:["param:foamOriginZ",.5],kind:"expr",op:"mul"}},{name:"foamOriginMod0Z",type:"f32",source:{args:["param:foamOriginMetersZ",1600],kind:"expr",op:"mod"}},{name:"foamOriginMod1Z",type:"f32",source:{args:["param:foamOriginMetersZ",250],kind:"expr",op:"mod"}},{name:"foamOriginMod2Z",type:"f32",source:{args:["param:foamOriginMetersZ",37],kind:"expr",op:"mod"}},{name:"foamOriginMod3Z",type:"f32",source:{args:["param:foamOriginMetersZ",5.3],kind:"expr",op:"mod"}},{name:"probeLocalZ",type:"f32",source:{args:["param:probeOriginZ","param:waterZ"],kind:"expr",op:"sub"}},{name:"probeOriginMod0Z",type:"f32",source:{args:["param:probeLocalZ",1600],kind:"expr",op:"mod"}},{name:"probeOriginMod1Z",type:"f32",source:{args:["param:probeLocalZ",250],kind:"expr",op:"mod"}},{name:"probeOriginMod2Z",type:"f32",source:{args:["param:probeLocalZ",37],kind:"expr",op:"mod"}},{name:"probeOriginMod3Z",type:"f32",source:{args:["param:probeLocalZ",5.3],kind:"expr",op:"mod"}}],resources:[{name:"h0_0",kind:"buffer",elementType:"vec4<f32>",count:4096},{name:"omega_0",kind:"buffer",elementType:"f32",count:4096},{name:"fftA_0",kind:"buffer",elementType:"vec4<f32>",count:4096},{name:"fftB_0",kind:"buffer",elementType:"vec4<f32>",count:4096},{name:"dispTex0",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"slopeTex0",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"h0_1",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"omega_1",kind:"buffer",elementType:"f32",count:65536},{name:"fftA_1",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"fftB_1",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"dispTex1",kind:"texture",format:"rgba16float",width:256,height:256,mipLevels:9},{name:"slopeTex1",kind:"texture",format:"rgba16float",width:256,height:256,mipLevels:9},{name:"h0_2",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"omega_2",kind:"buffer",elementType:"f32",count:65536},{name:"fftA_2",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"fftB_2",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"dispTex2",kind:"texture",format:"rgba16float",width:256,height:256,mipLevels:9},{name:"slopeTex2",kind:"texture",format:"rgba16float",width:256,height:256,mipLevels:9},{name:"h0_3",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"omega_3",kind:"buffer",elementType:"f32",count:65536},{name:"fftA_3",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"fftB_3",kind:"buffer",elementType:"vec4<f32>",count:65536},{name:"dispTex3",kind:"texture",format:"rgba16float",width:256,height:256,mipLevels:9},{name:"slopeTex3",kind:"texture",format:"rgba16float",width:256,height:256,mipLevels:9},{name:"foamA",kind:"buffer",elementType:"f32",count:1048576},{name:"foamB",kind:"buffer",elementType:"f32",count:1048576},{name:"foamTex",kind:"texture",format:"rgba16float",width:1024,height:1024,mipLevels:11},{name:"probePoints",kind:"buffer",elementType:"vec4<f32>",count:64,feed:"probePoints"},{name:"probeOut",kind:"buffer",elementType:"vec4<f32>",count:64}],passes:[{name:"c0.spectrum",type:"kernel",kernel:"ocean_spectrum",entry:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",dispatch:[64,1,1],bindings:[{name:"h0",resource:"h0_0"},{name:"omega",resource:"omega_0"}],uniforms:[{name:"spec",values:{cascade:0,fetchKm:"param:fetch",gamma:"param:gamma",kMax:.12,kMin:.003926990816987242,loopPeriod:"param:loopPeriod",n:64,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:1600,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c0.evolve",type:"kernel",kernel:"ocean_evolve",entry:"ocean_evolve",dispatch:[64,1,1],bindings:[{name:"h0",resource:"h0_0"},{name:"omega",resource:"omega_0"},{name:"outA",resource:"fftA_0"},{name:"outB",resource:"fftB_0"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:64,t:"param:phaseTime",tile:1600}}]},{name:"c0.fftA.fft_rows",type:"kernel",kernel:"fft_compute_64",entry:"fft_rows",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftA_0"}]},{name:"c0.fftA.fft_cols",type:"kernel",kernel:"fft_compute_64",entry:"fft_cols",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftA_0"}]},{name:"c0.fftB.fft_rows",type:"kernel",kernel:"fft_compute_64",entry:"fft_rows",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftB_0"}]},{name:"c0.fftB.fft_cols",type:"kernel",kernel:"fft_compute_64",entry:"fft_cols",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftB_0"}]},{name:"c0.assemble",type:"kernel",kernel:"ocean_assemble",entry:"ocean_assemble",dispatch:[64,1,1],bindings:[{name:"fieldA",resource:"fftA_0"},{name:"fieldB",resource:"fftB_0"},{name:"disp",resource:"dispTex0"},{name:"slope",resource:"slopeTex0"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:64}}]},{name:"c0.dispTex0.mips",type:"repeat",count:6,index:"l",body:[{name:"mip",type:"kernel",kernel:"ocean_mip",entry:"ocean_mip",dispatch:[16,1,1],bindings:[{name:"src",resource:"dispTex0",levelAt:{index:"l"}},{name:"dst",resource:"dispTex0",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:64},dstWidth:{index:"l",offset:1,shr:64},pad:0,srcLevel:0}}]}]},{name:"c0.slopeTex0.mips",type:"repeat",count:6,index:"l",body:[{name:"mip",type:"kernel",kernel:"ocean_mip",entry:"ocean_mip",dispatch:[16,1,1],bindings:[{name:"src",resource:"slopeTex0",levelAt:{index:"l"}},{name:"dst",resource:"slopeTex0",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:64},dstWidth:{index:"l",offset:1,shr:64},pad:0,srcLevel:0}}]}]},{name:"c1.spectrum",type:"kernel",kernel:"ocean_spectrum",entry:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",dispatch:[1024,1,1],bindings:[{name:"h0",resource:"h0_1"},{name:"omega",resource:"omega_1"}],uniforms:[{name:"spec",values:{cascade:1,fetchKm:"param:fetch",gamma:"param:gamma",kMax:.8,kMin:.12,loopPeriod:"param:loopPeriod",n:256,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:250,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c1.evolve",type:"kernel",kernel:"ocean_evolve",entry:"ocean_evolve",dispatch:[1024,1,1],bindings:[{name:"h0",resource:"h0_1"},{name:"omega",resource:"omega_1"},{name:"outA",resource:"fftA_1"},{name:"outB",resource:"fftB_1"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:256,t:"param:phaseTime",tile:250}}]},{name:"c1.fftA.fft_rows",type:"kernel",kernel:"fft_compute_256",entry:"fft_rows",dispatch:[256,1,1],bindings:[{name:"data",resource:"fftA_1"}]},{name:"c1.fftA.fft_cols",type:"kernel",kernel:"fft_compute_256",entry:"fft_cols",dispatch:[256,1,1],bindings:[{name:"data",resource:"fftA_1"}]},{name:"c1.fftB.fft_rows",type:"kernel",kernel:"fft_compute_256",entry:"fft_rows",dispatch:[256,1,1],bindings:[{name:"data",resource:"fftB_1"}]},{name:"c1.fftB.fft_cols",type:"kernel",kernel:"fft_compute_256",entry:"fft_cols",dispatch:[256,1,1],bindings:[{name:"data",resource:"fftB_1"}]},{name:"c1.assemble",type:"kernel",kernel:"ocean_assemble",entry:"ocean_assemble",dispatch:[1024,1,1],bindings:[{name:"fieldA",resource:"fftA_1"},{name:"fieldB",resource:"fftB_1"},{name:"disp",resource:"dispTex1"},{name:"slope",resource:"slopeTex1"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:256}}]},{name:"c1.dispTex1.mips",type:"repeat",count:8,index:"l",body:[{name:"mip",type:"kernel",kernel:"ocean_mip",entry:"ocean_mip",dispatch:[256,1,1],bindings:[{name:"src",resource:"dispTex1",levelAt:{index:"l"}},{name:"dst",resource:"dispTex1",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:256},dstWidth:{index:"l",offset:1,shr:256},pad:0,srcLevel:0}}]}]},{name:"c1.slopeTex1.mips",type:"repeat",count:8,index:"l",body:[{name:"mip",type:"kernel",kernel:"ocean_mip",entry:"ocean_mip",dispatch:[256,1,1],bindings:[{name:"src",resource:"slopeTex1",levelAt:{index:"l"}},{name:"dst",resource:"slopeTex1",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:256},dstWidth:{index:"l",offset:1,shr:256},pad:0,srcLevel:0}}]}]},{name:"c2.spectrum",type:"kernel",kernel:"ocean_spectrum",entry:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",dispatch:[1024,1,1],bindings:[{name:"h0",resource:"h0_2"},{name:"omega",resource:"omega_2"}],uniforms:[{name:"spec",values:{cascade:2,fetchKm:"param:fetch",gamma:"param:gamma",kMax:5.4,kMin:.8,loopPeriod:"param:loopPeriod",n:256,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:37,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c2.evolve",type:"kernel",kernel:"ocean_evolve",entry:"ocean_evolve",dispatch:[1024,1,1],bindings:[{name:"h0",resource:"h0_2"},{name:"omega",resource:"omega_2"},{name:"outA",resource:"fftA_2"},{name:"outB",resource:"fftB_2"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:256,t:"param:phaseTime",tile:37}}]},{name:"c2.fftA.fft_rows",type:"kernel",kernel:"fft_compute_256",entry:"fft_rows",dispatch:[256,1,1],bindings:[{name:"data",resource:"fftA_2"}]},{name:"c2.fftA.fft_cols",type:"kernel",kernel:"fft_compute_256",entry:"fft_cols",dispatch:[256,1,1],bindings:[{name:"data",resource:"fftA_2"}]},{name:"c2.fftB.fft_rows",type:"kernel",kernel:"fft_compute_256",entry:"fft_rows",dispatch:[256,1,1],bindings:[{name:"data",resource:"fftB_2"}]},{name:"c2.fftB.fft_cols",type:"kernel",kernel:"fft_compute_256",entry:"fft_cols",dispatch:[256,1,1],bindings:[{name:"data",resource:"fftB_2"}]},{name:"c2.assemble",type:"kernel",kernel:"ocean_assemble",entry:"ocean_assemble",dispatch:[1024,1,1],bindings:[{name:"fieldA",resource:"fftA_2"},{name:"fieldB",resource:"fftB_2"},{name:"disp",resource:"dispTex2"},{name:"slope",resource:"slopeTex2"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:256}}]},{name:"c2.dispTex2.mips",type:"repeat",count:8,index:"l",body:[{name:"mip",type:"kernel",kernel:"ocean_mip",entry:"ocean_mip",dispatch:[256,1,1],bindings:[{name:"src",resource:"dispTex2",levelAt:{index:"l"}},{name:"dst",resource:"dispTex2",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:256},dstWidth:{index:"l",offset:1,shr:256},pad:0,srcLevel:0}}]}]},{name:"c2.slopeTex2.mips",type:"repeat",count:8,index:"l",body:[{name:"mip",type:"kernel",kernel:"ocean_mip",entry:"ocean_mip",dispatch:[256,1,1],bindings:[{name:"src",resource:"slopeTex2",levelAt:{index:"l"}},{name:"dst",resource:"slopeTex2",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:256},dstWidth:{index:"l",offset:1,shr:256},pad:0,srcLevel:0}}]}]},{name:"c3.spectrum",type:"kernel",kernel:"ocean_spectrum",entry:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",dispatch:[1024,1,1],bindings:[{name:"h0",resource:"h0_3"},{name:"omega",resource:"omega_3"}],uniforms:[{name:"spec",values:{cascade:3,fetchKm:"param:fetch",gamma:"param:gamma",kMax:37,kMin:5.4,loopPeriod:"param:loopPeriod",n:256,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:5.3,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c3.evolve",type:"kernel",kernel:"ocean_evolve",entry:"ocean_evolve",dispatch:[1024,1,1],bindings:[{name:"h0",resource:"h0_3"},{name:"omega",resource:"omega_3"},{name:"outA",resource:"fftA_3"},{name:"outB",resource:"fftB_3"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:256,t:"param:phaseTime",tile:5.3}}]},{name:"c3.fftA.fft_rows",type:"kernel",kernel:"fft_compute_256",entry:"fft_rows",dispatch:[256,1,1],bindings:[{name:"data",resource:"fftA_3"}]},{name:"c3.fftA.fft_cols",type:"kernel",kernel:"fft_compute_256",entry:"fft_cols",dispatch:[256,1,1],bindings:[{name:"data",resource:"fftA_3"}]},{name:"c3.fftB.fft_rows",type:"kernel",kernel:"fft_compute_256",entry:"fft_rows",dispatch:[256,1,1],bindings:[{name:"data",resource:"fftB_3"}]},{name:"c3.fftB.fft_cols",type:"kernel",kernel:"fft_compute_256",entry:"fft_cols",dispatch:[256,1,1],bindings:[{name:"data",resource:"fftB_3"}]},{name:"c3.assemble",type:"kernel",kernel:"ocean_assemble",entry:"ocean_assemble",dispatch:[1024,1,1],bindings:[{name:"fieldA",resource:"fftA_3"},{name:"fieldB",resource:"fftB_3"},{name:"disp",resource:"dispTex3"},{name:"slope",resource:"slopeTex3"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:256}}]},{name:"c3.dispTex3.mips",type:"repeat",count:8,index:"l",body:[{name:"mip",type:"kernel",kernel:"ocean_mip",entry:"ocean_mip",dispatch:[256,1,1],bindings:[{name:"src",resource:"dispTex3",levelAt:{index:"l"}},{name:"dst",resource:"dispTex3",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:256},dstWidth:{index:"l",offset:1,shr:256},pad:0,srcLevel:0}}]}]},{name:"c3.slopeTex3.mips",type:"repeat",count:8,index:"l",body:[{name:"mip",type:"kernel",kernel:"ocean_mip",entry:"ocean_mip",dispatch:[256,1,1],bindings:[{name:"src",resource:"slopeTex3",levelAt:{index:"l"}},{name:"dst",resource:"slopeTex3",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:256},dstWidth:{index:"l",offset:1,shr:256},pad:0,srcLevel:0}}]}]},{name:"foam",type:"kernel",kernel:"ocean_foam",entry:"ocean_foam",dispatch:[16384,1,1],bindings:[{name:"foamIn",resource:"foamA"},{name:"fieldB0",resource:"fftB_0"},{name:"fieldB1",resource:"fftB_1"},{name:"fieldB2",resource:"fftB_2"},{name:"fieldB3",resource:"fftB_3"},{name:"foamOut",resource:"foamB"},{name:"foamTex",resource:"foamTex"}],uniforms:[{name:"foam",values:{cascades:4,cell:.5,dt:"param:dt",gain:"param:foamGain",lambda:"param:lambda",n0:64,n1:256,n2:256,n3:256,omX0:"param:foamOriginMod0X",omX1:"param:foamOriginMod1X",omX2:"param:foamOriginMod2X",omX3:"param:foamOriginMod3X",omZ0:"param:foamOriginMod0Z",omZ1:"param:foamOriginMod1Z",omZ2:"param:foamOriginMod2Z",omZ3:"param:foamOriginMod3Z",originX:"param:foamOriginX",originZ:"param:foamOriginZ",shiftX:"param:foamShiftX",shiftZ:"param:foamShiftZ",size0:1600,size1:250,size2:37,size3:5.3,tau:"param:foamTau",threshold:"param:foamThreshold",width:1024}}]},{name:"foamTex.mips",type:"repeat",count:10,index:"l",body:[{name:"mip",type:"kernel",kernel:"ocean_mip",entry:"ocean_mip",dispatch:[4096,1,1],bindings:[{name:"src",resource:"foamTex",levelAt:{index:"l"}},{name:"dst",resource:"foamTex",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:1024},dstWidth:{index:"l",offset:1,shr:1024},pad:0,srcLevel:0}}]}]},{name:"probe",type:"kernel",kernel:"ocean_probe",entry:"ocean_probe",dispatch:[1,1,1],bindings:[{name:"points",resource:"probePoints"},{name:"disp0",resource:"fftA_0"},{name:"disp1",resource:"fftA_1"},{name:"disp2",resource:"fftA_2"},{name:"disp3",resource:"fftA_3"},{name:"out",resource:"probeOut"}],uniforms:[{name:"probe",values:{cascades:4,count:"param:probeCount",lambda:"param:lambda",n0:64,n1:256,n2:256,n3:256,normalStep:.5,omX0:"param:probeOriginMod0X",omX1:"param:probeOriginMod1X",omX2:"param:probeOriginMod2X",omX3:"param:probeOriginMod3X",omZ0:"param:probeOriginMod0Z",omZ1:"param:probeOriginMod1Z",omZ2:"param:probeOriginMod2Z",omZ3:"param:probeOriginMod3Z",size0:1600,size1:250,size2:37,size3:5.3,t:"param:simTime"}}]}],frameSwaps:[["foamA","foamB"]],exports:{disp0:"dispTex0",disp1:"dispTex1",disp2:"dispTex2",disp3:"dispTex3",foam:"foamTex",probe:"probeOut",slope0:"slopeTex0",slope1:"slopeTex1",slope2:"slopeTex2",slope3:"slopeTex3"},exportParams:["waterModX0","waterModZ0","waterModX1","waterModZ1","waterModX2","waterModZ2","waterModX3","waterModZ3"]},"webgpu/medium":{name:"ocean",backend:"webgpu",tier:"medium",version:1,params:[{name:"windSpeed",type:"f32",source:{kind:"value",value:9},tween:20},{name:"fetch",type:"f32",source:{kind:"value",value:300},tween:20},{name:"gamma",type:"f32",source:{kind:"value",value:3.3},tween:20},{name:"spread",type:"f32",source:{kind:"value",value:8},tween:20},{name:"swell",type:"f32",source:{kind:"value",value:0},tween:20},{name:"windDirDeg",type:"f32",source:{kind:"value",value:0},tween:20},{name:"swellDirDeg",type:"f32",source:{kind:"value",value:0},tween:20},{name:"seed",type:"u32",source:{kind:"value",value:0}},{name:"loopPeriod",type:"f32",source:{kind:"value",value:1e3}},{name:"lambda",type:"f32",source:{kind:"value",value:1},tween:20},{name:"timeScale",type:"f32",source:{kind:"value",value:1}},{name:"driftX",type:"f32",source:{kind:"value",value:0}},{name:"driftZ",type:"f32",source:{kind:"value",value:0}},{name:"foamTau",type:"f32",source:{kind:"value",value:4}},{name:"foamThreshold",type:"f32",source:{kind:"value",value:.9}},{name:"foamGain",type:"f32",source:{kind:"value",value:2}},{name:"probeCount",type:"u32",source:{kind:"value",value:0}},{name:"probeOriginX",type:"f32",source:{kind:"value",value:0}},{name:"probeOriginZ",type:"f32",source:{kind:"value",value:0}},{name:"probePoints",type:"vec4<f32>[]",count:64,source:{kind:"value",value:0}},{name:"dt",type:"f32",source:{kind:"frame",name:"dt"}},{name:"cameraX",type:"f32",source:{kind:"frame",name:"cameraX"}},{name:"cameraZ",type:"f32",source:{kind:"frame",name:"cameraZ"}},{name:"windDirRad",type:"f32",source:{args:["param:windDirDeg",.017453292519943295],kind:"expr",op:"mul"}},{name:"windDir",type:"f32",source:{args:[1.5707963267948966,"param:windDirRad"],kind:"expr",op:"sub"}},{name:"swellDirRad",type:"f32",source:{args:["param:swellDirDeg",.017453292519943295],kind:"expr",op:"mul"}},{name:"swellDir",type:"f32",source:{args:[1.5707963267948966,"param:swellDirRad"],kind:"expr",op:"sub"}},{name:"simTime",type:"f32",source:{kind:"accumulate",of:"param:timeScale"}},{name:"phaseTime",type:"f32",source:{args:["param:simTime","param:loopPeriod"],kind:"expr",op:"mod"}},{name:"waterX",type:"f32",source:{kind:"accumulate",of:"param:driftX"}},{name:"waterZ",type:"f32",source:{kind:"accumulate",of:"param:driftZ"}},{name:"waterModX0",type:"f32",source:{args:["param:waterX",1600],kind:"expr",op:"mod"}},{name:"waterModZ0",type:"f32",source:{args:["param:waterZ",1600],kind:"expr",op:"mod"}},{name:"waterModX1",type:"f32",source:{args:["param:waterX",250],kind:"expr",op:"mod"}},{name:"waterModZ1",type:"f32",source:{args:["param:waterZ",250],kind:"expr",op:"mod"}},{name:"waterModX2",type:"f32",source:{args:["param:waterX",37],kind:"expr",op:"mod"}},{name:"waterModZ2",type:"f32",source:{args:["param:waterZ",37],kind:"expr",op:"mod"}},{name:"waterModX3",type:"f32",source:{args:["param:waterX",5.3],kind:"expr",op:"mod"}},{name:"waterModZ3",type:"f32",source:{args:["param:waterZ",5.3],kind:"expr",op:"mod"}},{name:"foamRelX",type:"f32",source:{args:["param:cameraX","param:waterX"],kind:"expr",op:"sub"}},{name:"foamTexelX",type:"f32",source:{args:["param:foamRelX",.5],kind:"expr",op:"div"}},{name:"foamCellX",type:"f32",source:{args:["param:foamTexelX"],kind:"expr",op:"floor"}},{name:"foamOriginX",type:"i32",source:{args:["param:foamCellX",512],kind:"expr",op:"sub"}},{name:"foamShiftX",type:"i32",source:{kind:"delta",of:"param:foamOriginX"}},{name:"foamOriginMetersX",type:"f32",source:{args:["param:foamOriginX",.5],kind:"expr",op:"mul"}},{name:"foamOriginMod0X",type:"f32",source:{args:["param:foamOriginMetersX",1600],kind:"expr",op:"mod"}},{name:"foamOriginMod1X",type:"f32",source:{args:["param:foamOriginMetersX",250],kind:"expr",op:"mod"}},{name:"foamOriginMod2X",type:"f32",source:{args:["param:foamOriginMetersX",37],kind:"expr",op:"mod"}},{name:"foamOriginMod3X",type:"f32",source:{args:["param:foamOriginMetersX",5.3],kind:"expr",op:"mod"}},{name:"probeLocalX",type:"f32",source:{args:["param:probeOriginX","param:waterX"],kind:"expr",op:"sub"}},{name:"probeOriginMod0X",type:"f32",source:{args:["param:probeLocalX",1600],kind:"expr",op:"mod"}},{name:"probeOriginMod1X",type:"f32",source:{args:["param:probeLocalX",250],kind:"expr",op:"mod"}},{name:"probeOriginMod2X",type:"f32",source:{args:["param:probeLocalX",37],kind:"expr",op:"mod"}},{name:"probeOriginMod3X",type:"f32",source:{args:["param:probeLocalX",5.3],kind:"expr",op:"mod"}},{name:"foamRelZ",type:"f32",source:{args:["param:cameraZ","param:waterZ"],kind:"expr",op:"sub"}},{name:"foamTexelZ",type:"f32",source:{args:["param:foamRelZ",.5],kind:"expr",op:"div"}},{name:"foamCellZ",type:"f32",source:{args:["param:foamTexelZ"],kind:"expr",op:"floor"}},{name:"foamOriginZ",type:"i32",source:{args:["param:foamCellZ",512],kind:"expr",op:"sub"}},{name:"foamShiftZ",type:"i32",source:{kind:"delta",of:"param:foamOriginZ"}},{name:"foamOriginMetersZ",type:"f32",source:{args:["param:foamOriginZ",.5],kind:"expr",op:"mul"}},{name:"foamOriginMod0Z",type:"f32",source:{args:["param:foamOriginMetersZ",1600],kind:"expr",op:"mod"}},{name:"foamOriginMod1Z",type:"f32",source:{args:["param:foamOriginMetersZ",250],kind:"expr",op:"mod"}},{name:"foamOriginMod2Z",type:"f32",source:{args:["param:foamOriginMetersZ",37],kind:"expr",op:"mod"}},{name:"foamOriginMod3Z",type:"f32",source:{args:["param:foamOriginMetersZ",5.3],kind:"expr",op:"mod"}},{name:"probeLocalZ",type:"f32",source:{args:["param:probeOriginZ","param:waterZ"],kind:"expr",op:"sub"}},{name:"probeOriginMod0Z",type:"f32",source:{args:["param:probeLocalZ",1600],kind:"expr",op:"mod"}},{name:"probeOriginMod1Z",type:"f32",source:{args:["param:probeLocalZ",250],kind:"expr",op:"mod"}},{name:"probeOriginMod2Z",type:"f32",source:{args:["param:probeLocalZ",37],kind:"expr",op:"mod"}},{name:"probeOriginMod3Z",type:"f32",source:{args:["param:probeLocalZ",5.3],kind:"expr",op:"mod"}}],resources:[{name:"h0_0",kind:"buffer",elementType:"vec4<f32>",count:4096},{name:"omega_0",kind:"buffer",elementType:"f32",count:4096},{name:"fftA_0",kind:"buffer",elementType:"vec4<f32>",count:4096},{name:"fftB_0",kind:"buffer",elementType:"vec4<f32>",count:4096},{name:"dispTex0",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"slopeTex0",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"h0_1",kind:"buffer",elementType:"vec4<f32>",count:16384},{name:"omega_1",kind:"buffer",elementType:"f32",count:16384},{name:"fftA_1",kind:"buffer",elementType:"vec4<f32>",count:16384},{name:"fftB_1",kind:"buffer",elementType:"vec4<f32>",count:16384},{name:"dispTex1",kind:"texture",format:"rgba16float",width:128,height:128,mipLevels:8},{name:"slopeTex1",kind:"texture",format:"rgba16float",width:128,height:128,mipLevels:8},{name:"h0_2",kind:"buffer",elementType:"vec4<f32>",count:16384},{name:"omega_2",kind:"buffer",elementType:"f32",count:16384},{name:"fftA_2",kind:"buffer",elementType:"vec4<f32>",count:16384},{name:"fftB_2",kind:"buffer",elementType:"vec4<f32>",count:16384},{name:"dispTex2",kind:"texture",format:"rgba16float",width:128,height:128,mipLevels:8},{name:"slopeTex2",kind:"texture",format:"rgba16float",width:128,height:128,mipLevels:8},{name:"h0_3",kind:"buffer",elementType:"vec4<f32>",count:16384},{name:"omega_3",kind:"buffer",elementType:"f32",count:16384},{name:"fftA_3",kind:"buffer",elementType:"vec4<f32>",count:16384},{name:"fftB_3",kind:"buffer",elementType:"vec4<f32>",count:16384},{name:"dispTex3",kind:"texture",format:"rgba16float",width:128,height:128,mipLevels:8},{name:"slopeTex3",kind:"texture",format:"rgba16float",width:128,height:128,mipLevels:8},{name:"foamA",kind:"buffer",elementType:"f32",count:1048576},{name:"foamB",kind:"buffer",elementType:"f32",count:1048576},{name:"foamTex",kind:"texture",format:"rgba16float",width:1024,height:1024,mipLevels:11},{name:"probePoints",kind:"buffer",elementType:"vec4<f32>",count:64,feed:"probePoints"},{name:"probeOut",kind:"buffer",elementType:"vec4<f32>",count:64}],passes:[{name:"c0.spectrum",type:"kernel",kernel:"ocean_spectrum",entry:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",dispatch:[64,1,1],bindings:[{name:"h0",resource:"h0_0"},{name:"omega",resource:"omega_0"}],uniforms:[{name:"spec",values:{cascade:0,fetchKm:"param:fetch",gamma:"param:gamma",kMax:.12,kMin:.003926990816987242,loopPeriod:"param:loopPeriod",n:64,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:1600,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c0.evolve",type:"kernel",kernel:"ocean_evolve",entry:"ocean_evolve",dispatch:[64,1,1],bindings:[{name:"h0",resource:"h0_0"},{name:"omega",resource:"omega_0"},{name:"outA",resource:"fftA_0"},{name:"outB",resource:"fftB_0"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:64,t:"param:phaseTime",tile:1600}}]},{name:"c0.fftA.fft_rows",type:"kernel",kernel:"fft_compute_64",entry:"fft_rows",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftA_0"}]},{name:"c0.fftA.fft_cols",type:"kernel",kernel:"fft_compute_64",entry:"fft_cols",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftA_0"}]},{name:"c0.fftB.fft_rows",type:"kernel",kernel:"fft_compute_64",entry:"fft_rows",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftB_0"}]},{name:"c0.fftB.fft_cols",type:"kernel",kernel:"fft_compute_64",entry:"fft_cols",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftB_0"}]},{name:"c0.assemble",type:"kernel",kernel:"ocean_assemble",entry:"ocean_assemble",dispatch:[64,1,1],bindings:[{name:"fieldA",resource:"fftA_0"},{name:"fieldB",resource:"fftB_0"},{name:"disp",resource:"dispTex0"},{name:"slope",resource:"slopeTex0"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:64}}]},{name:"c0.dispTex0.mips",type:"repeat",count:6,index:"l",body:[{name:"mip",type:"kernel",kernel:"ocean_mip",entry:"ocean_mip",dispatch:[16,1,1],bindings:[{name:"src",resource:"dispTex0",levelAt:{index:"l"}},{name:"dst",resource:"dispTex0",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:64},dstWidth:{index:"l",offset:1,shr:64},pad:0,srcLevel:0}}]}]},{name:"c0.slopeTex0.mips",type:"repeat",count:6,index:"l",body:[{name:"mip",type:"kernel",kernel:"ocean_mip",entry:"ocean_mip",dispatch:[16,1,1],bindings:[{name:"src",resource:"slopeTex0",levelAt:{index:"l"}},{name:"dst",resource:"slopeTex0",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:64},dstWidth:{index:"l",offset:1,shr:64},pad:0,srcLevel:0}}]}]},{name:"c1.spectrum",type:"kernel",kernel:"ocean_spectrum",entry:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",dispatch:[256,1,1],bindings:[{name:"h0",resource:"h0_1"},{name:"omega",resource:"omega_1"}],uniforms:[{name:"spec",values:{cascade:1,fetchKm:"param:fetch",gamma:"param:gamma",kMax:.8,kMin:.12,loopPeriod:"param:loopPeriod",n:128,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:250,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c1.evolve",type:"kernel",kernel:"ocean_evolve",entry:"ocean_evolve",dispatch:[256,1,1],bindings:[{name:"h0",resource:"h0_1"},{name:"omega",resource:"omega_1"},{name:"outA",resource:"fftA_1"},{name:"outB",resource:"fftB_1"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:128,t:"param:phaseTime",tile:250}}]},{name:"c1.fftA.fft_rows",type:"kernel",kernel:"fft_compute_128",entry:"fft_rows",dispatch:[128,1,1],bindings:[{name:"data",resource:"fftA_1"}]},{name:"c1.fftA.fft_cols",type:"kernel",kernel:"fft_compute_128",entry:"fft_cols",dispatch:[128,1,1],bindings:[{name:"data",resource:"fftA_1"}]},{name:"c1.fftB.fft_rows",type:"kernel",kernel:"fft_compute_128",entry:"fft_rows",dispatch:[128,1,1],bindings:[{name:"data",resource:"fftB_1"}]},{name:"c1.fftB.fft_cols",type:"kernel",kernel:"fft_compute_128",entry:"fft_cols",dispatch:[128,1,1],bindings:[{name:"data",resource:"fftB_1"}]},{name:"c1.assemble",type:"kernel",kernel:"ocean_assemble",entry:"ocean_assemble",dispatch:[256,1,1],bindings:[{name:"fieldA",resource:"fftA_1"},{name:"fieldB",resource:"fftB_1"},{name:"disp",resource:"dispTex1"},{name:"slope",resource:"slopeTex1"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:128}}]},{name:"c1.dispTex1.mips",type:"repeat",count:7,index:"l",body:[{name:"mip",type:"kernel",kernel:"ocean_mip",entry:"ocean_mip",dispatch:[64,1,1],bindings:[{name:"src",resource:"dispTex1",levelAt:{index:"l"}},{name:"dst",resource:"dispTex1",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:128},dstWidth:{index:"l",offset:1,shr:128},pad:0,srcLevel:0}}]}]},{name:"c1.slopeTex1.mips",type:"repeat",count:7,index:"l",body:[{name:"mip",type:"kernel",kernel:"ocean_mip",entry:"ocean_mip",dispatch:[64,1,1],bindings:[{name:"src",resource:"slopeTex1",levelAt:{index:"l"}},{name:"dst",resource:"slopeTex1",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:128},dstWidth:{index:"l",offset:1,shr:128},pad:0,srcLevel:0}}]}]},{name:"c2.spectrum",type:"kernel",kernel:"ocean_spectrum",entry:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",dispatch:[256,1,1],bindings:[{name:"h0",resource:"h0_2"},{name:"omega",resource:"omega_2"}],uniforms:[{name:"spec",values:{cascade:2,fetchKm:"param:fetch",gamma:"param:gamma",kMax:5.4,kMin:.8,loopPeriod:"param:loopPeriod",n:128,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:37,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c2.evolve",type:"kernel",kernel:"ocean_evolve",entry:"ocean_evolve",dispatch:[256,1,1],bindings:[{name:"h0",resource:"h0_2"},{name:"omega",resource:"omega_2"},{name:"outA",resource:"fftA_2"},{name:"outB",resource:"fftB_2"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:128,t:"param:phaseTime",tile:37}}]},{name:"c2.fftA.fft_rows",type:"kernel",kernel:"fft_compute_128",entry:"fft_rows",dispatch:[128,1,1],bindings:[{name:"data",resource:"fftA_2"}]},{name:"c2.fftA.fft_cols",type:"kernel",kernel:"fft_compute_128",entry:"fft_cols",dispatch:[128,1,1],bindings:[{name:"data",resource:"fftA_2"}]},{name:"c2.fftB.fft_rows",type:"kernel",kernel:"fft_compute_128",entry:"fft_rows",dispatch:[128,1,1],bindings:[{name:"data",resource:"fftB_2"}]},{name:"c2.fftB.fft_cols",type:"kernel",kernel:"fft_compute_128",entry:"fft_cols",dispatch:[128,1,1],bindings:[{name:"data",resource:"fftB_2"}]},{name:"c2.assemble",type:"kernel",kernel:"ocean_assemble",entry:"ocean_assemble",dispatch:[256,1,1],bindings:[{name:"fieldA",resource:"fftA_2"},{name:"fieldB",resource:"fftB_2"},{name:"disp",resource:"dispTex2"},{name:"slope",resource:"slopeTex2"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:128}}]},{name:"c2.dispTex2.mips",type:"repeat",count:7,index:"l",body:[{name:"mip",type:"kernel",kernel:"ocean_mip",entry:"ocean_mip",dispatch:[64,1,1],bindings:[{name:"src",resource:"dispTex2",levelAt:{index:"l"}},{name:"dst",resource:"dispTex2",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:128},dstWidth:{index:"l",offset:1,shr:128},pad:0,srcLevel:0}}]}]},{name:"c2.slopeTex2.mips",type:"repeat",count:7,index:"l",body:[{name:"mip",type:"kernel",kernel:"ocean_mip",entry:"ocean_mip",dispatch:[64,1,1],bindings:[{name:"src",resource:"slopeTex2",levelAt:{index:"l"}},{name:"dst",resource:"slopeTex2",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:128},dstWidth:{index:"l",offset:1,shr:128},pad:0,srcLevel:0}}]}]},{name:"c3.spectrum",type:"kernel",kernel:"ocean_spectrum",entry:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",dispatch:[256,1,1],bindings:[{name:"h0",resource:"h0_3"},{name:"omega",resource:"omega_3"}],uniforms:[{name:"spec",values:{cascade:3,fetchKm:"param:fetch",gamma:"param:gamma",kMax:37,kMin:5.4,loopPeriod:"param:loopPeriod",n:128,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:5.3,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c3.evolve",type:"kernel",kernel:"ocean_evolve",entry:"ocean_evolve",dispatch:[256,1,1],bindings:[{name:"h0",resource:"h0_3"},{name:"omega",resource:"omega_3"},{name:"outA",resource:"fftA_3"},{name:"outB",resource:"fftB_3"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:128,t:"param:phaseTime",tile:5.3}}]},{name:"c3.fftA.fft_rows",type:"kernel",kernel:"fft_compute_128",entry:"fft_rows",dispatch:[128,1,1],bindings:[{name:"data",resource:"fftA_3"}]},{name:"c3.fftA.fft_cols",type:"kernel",kernel:"fft_compute_128",entry:"fft_cols",dispatch:[128,1,1],bindings:[{name:"data",resource:"fftA_3"}]},{name:"c3.fftB.fft_rows",type:"kernel",kernel:"fft_compute_128",entry:"fft_rows",dispatch:[128,1,1],bindings:[{name:"data",resource:"fftB_3"}]},{name:"c3.fftB.fft_cols",type:"kernel",kernel:"fft_compute_128",entry:"fft_cols",dispatch:[128,1,1],bindings:[{name:"data",resource:"fftB_3"}]},{name:"c3.assemble",type:"kernel",kernel:"ocean_assemble",entry:"ocean_assemble",dispatch:[256,1,1],bindings:[{name:"fieldA",resource:"fftA_3"},{name:"fieldB",resource:"fftB_3"},{name:"disp",resource:"dispTex3"},{name:"slope",resource:"slopeTex3"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:128}}]},{name:"c3.dispTex3.mips",type:"repeat",count:7,index:"l",body:[{name:"mip",type:"kernel",kernel:"ocean_mip",entry:"ocean_mip",dispatch:[64,1,1],bindings:[{name:"src",resource:"dispTex3",levelAt:{index:"l"}},{name:"dst",resource:"dispTex3",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:128},dstWidth:{index:"l",offset:1,shr:128},pad:0,srcLevel:0}}]}]},{name:"c3.slopeTex3.mips",type:"repeat",count:7,index:"l",body:[{name:"mip",type:"kernel",kernel:"ocean_mip",entry:"ocean_mip",dispatch:[64,1,1],bindings:[{name:"src",resource:"slopeTex3",levelAt:{index:"l"}},{name:"dst",resource:"slopeTex3",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:128},dstWidth:{index:"l",offset:1,shr:128},pad:0,srcLevel:0}}]}]},{name:"foam",type:"kernel",kernel:"ocean_foam",entry:"ocean_foam",dispatch:[16384,1,1],bindings:[{name:"foamIn",resource:"foamA"},{name:"fieldB0",resource:"fftB_0"},{name:"fieldB1",resource:"fftB_1"},{name:"fieldB2",resource:"fftB_2"},{name:"fieldB3",resource:"fftB_3"},{name:"foamOut",resource:"foamB"},{name:"foamTex",resource:"foamTex"}],uniforms:[{name:"foam",values:{cascades:4,cell:.5,dt:"param:dt",gain:"param:foamGain",lambda:"param:lambda",n0:64,n1:128,n2:128,n3:128,omX0:"param:foamOriginMod0X",omX1:"param:foamOriginMod1X",omX2:"param:foamOriginMod2X",omX3:"param:foamOriginMod3X",omZ0:"param:foamOriginMod0Z",omZ1:"param:foamOriginMod1Z",omZ2:"param:foamOriginMod2Z",omZ3:"param:foamOriginMod3Z",originX:"param:foamOriginX",originZ:"param:foamOriginZ",shiftX:"param:foamShiftX",shiftZ:"param:foamShiftZ",size0:1600,size1:250,size2:37,size3:5.3,tau:"param:foamTau",threshold:"param:foamThreshold",width:1024}}]},{name:"foamTex.mips",type:"repeat",count:10,index:"l",body:[{name:"mip",type:"kernel",kernel:"ocean_mip",entry:"ocean_mip",dispatch:[4096,1,1],bindings:[{name:"src",resource:"foamTex",levelAt:{index:"l"}},{name:"dst",resource:"foamTex",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:1024},dstWidth:{index:"l",offset:1,shr:1024},pad:0,srcLevel:0}}]}]},{name:"probe",type:"kernel",kernel:"ocean_probe",entry:"ocean_probe",dispatch:[1,1,1],bindings:[{name:"points",resource:"probePoints"},{name:"disp0",resource:"fftA_0"},{name:"disp1",resource:"fftA_1"},{name:"disp2",resource:"fftA_2"},{name:"disp3",resource:"fftA_3"},{name:"out",resource:"probeOut"}],uniforms:[{name:"probe",values:{cascades:4,count:"param:probeCount",lambda:"param:lambda",n0:64,n1:128,n2:128,n3:128,normalStep:.5,omX0:"param:probeOriginMod0X",omX1:"param:probeOriginMod1X",omX2:"param:probeOriginMod2X",omX3:"param:probeOriginMod3X",omZ0:"param:probeOriginMod0Z",omZ1:"param:probeOriginMod1Z",omZ2:"param:probeOriginMod2Z",omZ3:"param:probeOriginMod3Z",size0:1600,size1:250,size2:37,size3:5.3,t:"param:simTime"}}]}],frameSwaps:[["foamA","foamB"]],exports:{disp0:"dispTex0",disp1:"dispTex1",disp2:"dispTex2",disp3:"dispTex3",foam:"foamTex",probe:"probeOut",slope0:"slopeTex0",slope1:"slopeTex1",slope2:"slopeTex2",slope3:"slopeTex3"},exportParams:["waterModX0","waterModZ0","waterModX1","waterModZ1","waterModX2","waterModZ2","waterModX3","waterModZ3"]},"webgpu/low":{name:"ocean",backend:"webgpu",tier:"low",version:1,params:[{name:"windSpeed",type:"f32",source:{kind:"value",value:9},tween:20},{name:"fetch",type:"f32",source:{kind:"value",value:300},tween:20},{name:"gamma",type:"f32",source:{kind:"value",value:3.3},tween:20},{name:"spread",type:"f32",source:{kind:"value",value:8},tween:20},{name:"swell",type:"f32",source:{kind:"value",value:0},tween:20},{name:"windDirDeg",type:"f32",source:{kind:"value",value:0},tween:20},{name:"swellDirDeg",type:"f32",source:{kind:"value",value:0},tween:20},{name:"seed",type:"u32",source:{kind:"value",value:0}},{name:"loopPeriod",type:"f32",source:{kind:"value",value:1e3}},{name:"lambda",type:"f32",source:{kind:"value",value:1},tween:20},{name:"timeScale",type:"f32",source:{kind:"value",value:1}},{name:"driftX",type:"f32",source:{kind:"value",value:0}},{name:"driftZ",type:"f32",source:{kind:"value",value:0}},{name:"foamTau",type:"f32",source:{kind:"value",value:4}},{name:"foamThreshold",type:"f32",source:{kind:"value",value:.9}},{name:"foamGain",type:"f32",source:{kind:"value",value:2}},{name:"probeCount",type:"u32",source:{kind:"value",value:0}},{name:"probeOriginX",type:"f32",source:{kind:"value",value:0}},{name:"probeOriginZ",type:"f32",source:{kind:"value",value:0}},{name:"probePoints",type:"vec4<f32>[]",count:64,source:{kind:"value",value:0}},{name:"dt",type:"f32",source:{kind:"frame",name:"dt"}},{name:"cameraX",type:"f32",source:{kind:"frame",name:"cameraX"}},{name:"cameraZ",type:"f32",source:{kind:"frame",name:"cameraZ"}},{name:"windDirRad",type:"f32",source:{args:["param:windDirDeg",.017453292519943295],kind:"expr",op:"mul"}},{name:"windDir",type:"f32",source:{args:[1.5707963267948966,"param:windDirRad"],kind:"expr",op:"sub"}},{name:"swellDirRad",type:"f32",source:{args:["param:swellDirDeg",.017453292519943295],kind:"expr",op:"mul"}},{name:"swellDir",type:"f32",source:{args:[1.5707963267948966,"param:swellDirRad"],kind:"expr",op:"sub"}},{name:"simTime",type:"f32",source:{kind:"accumulate",of:"param:timeScale"}},{name:"phaseTime",type:"f32",source:{args:["param:simTime","param:loopPeriod"],kind:"expr",op:"mod"}},{name:"waterX",type:"f32",source:{kind:"accumulate",of:"param:driftX"}},{name:"waterZ",type:"f32",source:{kind:"accumulate",of:"param:driftZ"}},{name:"waterModX0",type:"f32",source:{args:["param:waterX",1600],kind:"expr",op:"mod"}},{name:"waterModZ0",type:"f32",source:{args:["param:waterZ",1600],kind:"expr",op:"mod"}},{name:"waterModX1",type:"f32",source:{args:["param:waterX",250],kind:"expr",op:"mod"}},{name:"waterModZ1",type:"f32",source:{args:["param:waterZ",250],kind:"expr",op:"mod"}},{name:"waterModX2",type:"f32",source:{args:["param:waterX",37],kind:"expr",op:"mod"}},{name:"waterModZ2",type:"f32",source:{args:["param:waterZ",37],kind:"expr",op:"mod"}},{name:"foamRelX",type:"f32",source:{args:["param:cameraX","param:waterX"],kind:"expr",op:"sub"}},{name:"foamTexelX",type:"f32",source:{args:["param:foamRelX",.5],kind:"expr",op:"div"}},{name:"foamCellX",type:"f32",source:{args:["param:foamTexelX"],kind:"expr",op:"floor"}},{name:"foamOriginX",type:"i32",source:{args:["param:foamCellX",512],kind:"expr",op:"sub"}},{name:"foamShiftX",type:"i32",source:{kind:"delta",of:"param:foamOriginX"}},{name:"foamOriginMetersX",type:"f32",source:{args:["param:foamOriginX",.5],kind:"expr",op:"mul"}},{name:"foamOriginMod0X",type:"f32",source:{args:["param:foamOriginMetersX",1600],kind:"expr",op:"mod"}},{name:"foamOriginMod1X",type:"f32",source:{args:["param:foamOriginMetersX",250],kind:"expr",op:"mod"}},{name:"foamOriginMod2X",type:"f32",source:{args:["param:foamOriginMetersX",37],kind:"expr",op:"mod"}},{name:"probeLocalX",type:"f32",source:{args:["param:probeOriginX","param:waterX"],kind:"expr",op:"sub"}},{name:"probeOriginMod0X",type:"f32",source:{args:["param:probeLocalX",1600],kind:"expr",op:"mod"}},{name:"probeOriginMod1X",type:"f32",source:{args:["param:probeLocalX",250],kind:"expr",op:"mod"}},{name:"probeOriginMod2X",type:"f32",source:{args:["param:probeLocalX",37],kind:"expr",op:"mod"}},{name:"foamRelZ",type:"f32",source:{args:["param:cameraZ","param:waterZ"],kind:"expr",op:"sub"}},{name:"foamTexelZ",type:"f32",source:{args:["param:foamRelZ",.5],kind:"expr",op:"div"}},{name:"foamCellZ",type:"f32",source:{args:["param:foamTexelZ"],kind:"expr",op:"floor"}},{name:"foamOriginZ",type:"i32",source:{args:["param:foamCellZ",512],kind:"expr",op:"sub"}},{name:"foamShiftZ",type:"i32",source:{kind:"delta",of:"param:foamOriginZ"}},{name:"foamOriginMetersZ",type:"f32",source:{args:["param:foamOriginZ",.5],kind:"expr",op:"mul"}},{name:"foamOriginMod0Z",type:"f32",source:{args:["param:foamOriginMetersZ",1600],kind:"expr",op:"mod"}},{name:"foamOriginMod1Z",type:"f32",source:{args:["param:foamOriginMetersZ",250],kind:"expr",op:"mod"}},{name:"foamOriginMod2Z",type:"f32",source:{args:["param:foamOriginMetersZ",37],kind:"expr",op:"mod"}},{name:"probeLocalZ",type:"f32",source:{args:["param:probeOriginZ","param:waterZ"],kind:"expr",op:"sub"}},{name:"probeOriginMod0Z",type:"f32",source:{args:["param:probeLocalZ",1600],kind:"expr",op:"mod"}},{name:"probeOriginMod1Z",type:"f32",source:{args:["param:probeLocalZ",250],kind:"expr",op:"mod"}},{name:"probeOriginMod2Z",type:"f32",source:{args:["param:probeLocalZ",37],kind:"expr",op:"mod"}}],resources:[{name:"h0_0",kind:"buffer",elementType:"vec4<f32>",count:4096},{name:"omega_0",kind:"buffer",elementType:"f32",count:4096},{name:"fftA_0",kind:"buffer",elementType:"vec4<f32>",count:4096},{name:"fftB_0",kind:"buffer",elementType:"vec4<f32>",count:4096},{name:"dispTex0",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"slopeTex0",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"h0_1",kind:"buffer",elementType:"vec4<f32>",count:4096},{name:"omega_1",kind:"buffer",elementType:"f32",count:4096},{name:"fftA_1",kind:"buffer",elementType:"vec4<f32>",count:4096},{name:"fftB_1",kind:"buffer",elementType:"vec4<f32>",count:4096},{name:"dispTex1",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"slopeTex1",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"h0_2",kind:"buffer",elementType:"vec4<f32>",count:4096},{name:"omega_2",kind:"buffer",elementType:"f32",count:4096},{name:"fftA_2",kind:"buffer",elementType:"vec4<f32>",count:4096},{name:"fftB_2",kind:"buffer",elementType:"vec4<f32>",count:4096},{name:"dispTex2",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"slopeTex2",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"foamA",kind:"buffer",elementType:"f32",count:1048576},{name:"foamB",kind:"buffer",elementType:"f32",count:1048576},{name:"foamTex",kind:"texture",format:"rgba16float",width:1024,height:1024,mipLevels:11},{name:"probePoints",kind:"buffer",elementType:"vec4<f32>",count:64,feed:"probePoints"},{name:"probeOut",kind:"buffer",elementType:"vec4<f32>",count:64}],passes:[{name:"c0.spectrum",type:"kernel",kernel:"ocean_spectrum",entry:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",dispatch:[64,1,1],bindings:[{name:"h0",resource:"h0_0"},{name:"omega",resource:"omega_0"}],uniforms:[{name:"spec",values:{cascade:0,fetchKm:"param:fetch",gamma:"param:gamma",kMax:.12,kMin:.003926990816987242,loopPeriod:"param:loopPeriod",n:64,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:1600,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c0.evolve",type:"kernel",kernel:"ocean_evolve",entry:"ocean_evolve",dispatch:[64,1,1],bindings:[{name:"h0",resource:"h0_0"},{name:"omega",resource:"omega_0"},{name:"outA",resource:"fftA_0"},{name:"outB",resource:"fftB_0"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:64,t:"param:phaseTime",tile:1600}}]},{name:"c0.fftA.fft_rows",type:"kernel",kernel:"fft_compute_64",entry:"fft_rows",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftA_0"}]},{name:"c0.fftA.fft_cols",type:"kernel",kernel:"fft_compute_64",entry:"fft_cols",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftA_0"}]},{name:"c0.fftB.fft_rows",type:"kernel",kernel:"fft_compute_64",entry:"fft_rows",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftB_0"}]},{name:"c0.fftB.fft_cols",type:"kernel",kernel:"fft_compute_64",entry:"fft_cols",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftB_0"}]},{name:"c0.assemble",type:"kernel",kernel:"ocean_assemble",entry:"ocean_assemble",dispatch:[64,1,1],bindings:[{name:"fieldA",resource:"fftA_0"},{name:"fieldB",resource:"fftB_0"},{name:"disp",resource:"dispTex0"},{name:"slope",resource:"slopeTex0"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:64}}]},{name:"c0.dispTex0.mips",type:"repeat",count:6,index:"l",body:[{name:"mip",type:"kernel",kernel:"ocean_mip",entry:"ocean_mip",dispatch:[16,1,1],bindings:[{name:"src",resource:"dispTex0",levelAt:{index:"l"}},{name:"dst",resource:"dispTex0",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:64},dstWidth:{index:"l",offset:1,shr:64},pad:0,srcLevel:0}}]}]},{name:"c0.slopeTex0.mips",type:"repeat",count:6,index:"l",body:[{name:"mip",type:"kernel",kernel:"ocean_mip",entry:"ocean_mip",dispatch:[16,1,1],bindings:[{name:"src",resource:"slopeTex0",levelAt:{index:"l"}},{name:"dst",resource:"slopeTex0",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:64},dstWidth:{index:"l",offset:1,shr:64},pad:0,srcLevel:0}}]}]},{name:"c1.spectrum",type:"kernel",kernel:"ocean_spectrum",entry:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",dispatch:[64,1,1],bindings:[{name:"h0",resource:"h0_1"},{name:"omega",resource:"omega_1"}],uniforms:[{name:"spec",values:{cascade:1,fetchKm:"param:fetch",gamma:"param:gamma",kMax:.8,kMin:.12,loopPeriod:"param:loopPeriod",n:64,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:250,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c1.evolve",type:"kernel",kernel:"ocean_evolve",entry:"ocean_evolve",dispatch:[64,1,1],bindings:[{name:"h0",resource:"h0_1"},{name:"omega",resource:"omega_1"},{name:"outA",resource:"fftA_1"},{name:"outB",resource:"fftB_1"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:64,t:"param:phaseTime",tile:250}}]},{name:"c1.fftA.fft_rows",type:"kernel",kernel:"fft_compute_64",entry:"fft_rows",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftA_1"}]},{name:"c1.fftA.fft_cols",type:"kernel",kernel:"fft_compute_64",entry:"fft_cols",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftA_1"}]},{name:"c1.fftB.fft_rows",type:"kernel",kernel:"fft_compute_64",entry:"fft_rows",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftB_1"}]},{name:"c1.fftB.fft_cols",type:"kernel",kernel:"fft_compute_64",entry:"fft_cols",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftB_1"}]},{name:"c1.assemble",type:"kernel",kernel:"ocean_assemble",entry:"ocean_assemble",dispatch:[64,1,1],bindings:[{name:"fieldA",resource:"fftA_1"},{name:"fieldB",resource:"fftB_1"},{name:"disp",resource:"dispTex1"},{name:"slope",resource:"slopeTex1"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:64}}]},{name:"c1.dispTex1.mips",type:"repeat",count:6,index:"l",body:[{name:"mip",type:"kernel",kernel:"ocean_mip",entry:"ocean_mip",dispatch:[16,1,1],bindings:[{name:"src",resource:"dispTex1",levelAt:{index:"l"}},{name:"dst",resource:"dispTex1",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:64},dstWidth:{index:"l",offset:1,shr:64},pad:0,srcLevel:0}}]}]},{name:"c1.slopeTex1.mips",type:"repeat",count:6,index:"l",body:[{name:"mip",type:"kernel",kernel:"ocean_mip",entry:"ocean_mip",dispatch:[16,1,1],bindings:[{name:"src",resource:"slopeTex1",levelAt:{index:"l"}},{name:"dst",resource:"slopeTex1",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:64},dstWidth:{index:"l",offset:1,shr:64},pad:0,srcLevel:0}}]}]},{name:"c2.spectrum",type:"kernel",kernel:"ocean_spectrum",entry:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",dispatch:[64,1,1],bindings:[{name:"h0",resource:"h0_2"},{name:"omega",resource:"omega_2"}],uniforms:[{name:"spec",values:{cascade:2,fetchKm:"param:fetch",gamma:"param:gamma",kMax:5.4,kMin:.8,loopPeriod:"param:loopPeriod",n:64,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:37,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c2.evolve",type:"kernel",kernel:"ocean_evolve",entry:"ocean_evolve",dispatch:[64,1,1],bindings:[{name:"h0",resource:"h0_2"},{name:"omega",resource:"omega_2"},{name:"outA",resource:"fftA_2"},{name:"outB",resource:"fftB_2"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:64,t:"param:phaseTime",tile:37}}]},{name:"c2.fftA.fft_rows",type:"kernel",kernel:"fft_compute_64",entry:"fft_rows",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftA_2"}]},{name:"c2.fftA.fft_cols",type:"kernel",kernel:"fft_compute_64",entry:"fft_cols",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftA_2"}]},{name:"c2.fftB.fft_rows",type:"kernel",kernel:"fft_compute_64",entry:"fft_rows",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftB_2"}]},{name:"c2.fftB.fft_cols",type:"kernel",kernel:"fft_compute_64",entry:"fft_cols",dispatch:[64,1,1],bindings:[{name:"data",resource:"fftB_2"}]},{name:"c2.assemble",type:"kernel",kernel:"ocean_assemble",entry:"ocean_assemble",dispatch:[64,1,1],bindings:[{name:"fieldA",resource:"fftA_2"},{name:"fieldB",resource:"fftB_2"},{name:"disp",resource:"dispTex2"},{name:"slope",resource:"slopeTex2"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:64}}]},{name:"c2.dispTex2.mips",type:"repeat",count:6,index:"l",body:[{name:"mip",type:"kernel",kernel:"ocean_mip",entry:"ocean_mip",dispatch:[16,1,1],bindings:[{name:"src",resource:"dispTex2",levelAt:{index:"l"}},{name:"dst",resource:"dispTex2",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:64},dstWidth:{index:"l",offset:1,shr:64},pad:0,srcLevel:0}}]}]},{name:"c2.slopeTex2.mips",type:"repeat",count:6,index:"l",body:[{name:"mip",type:"kernel",kernel:"ocean_mip",entry:"ocean_mip",dispatch:[16,1,1],bindings:[{name:"src",resource:"slopeTex2",levelAt:{index:"l"}},{name:"dst",resource:"slopeTex2",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:64},dstWidth:{index:"l",offset:1,shr:64},pad:0,srcLevel:0}}]}]},{name:"foam",type:"kernel",kernel:"ocean_foam",entry:"ocean_foam",dispatch:[16384,1,1],bindings:[{name:"foamIn",resource:"foamA"},{name:"fieldB0",resource:"fftB_0"},{name:"fieldB1",resource:"fftB_1"},{name:"fieldB2",resource:"fftB_2"},{name:"fieldB3",resource:"fftB_2"},{name:"foamOut",resource:"foamB"},{name:"foamTex",resource:"foamTex"}],uniforms:[{name:"foam",values:{cascades:3,cell:.5,dt:"param:dt",gain:"param:foamGain",lambda:"param:lambda",n0:64,n1:64,n2:64,n3:64,omX0:"param:foamOriginMod0X",omX1:"param:foamOriginMod1X",omX2:"param:foamOriginMod2X",omX3:"param:foamOriginMod2X",omZ0:"param:foamOriginMod0Z",omZ1:"param:foamOriginMod1Z",omZ2:"param:foamOriginMod2Z",omZ3:"param:foamOriginMod2Z",originX:"param:foamOriginX",originZ:"param:foamOriginZ",shiftX:"param:foamShiftX",shiftZ:"param:foamShiftZ",size0:1600,size1:250,size2:37,size3:37,tau:"param:foamTau",threshold:"param:foamThreshold",width:1024}}]},{name:"foamTex.mips",type:"repeat",count:10,index:"l",body:[{name:"mip",type:"kernel",kernel:"ocean_mip",entry:"ocean_mip",dispatch:[4096,1,1],bindings:[{name:"src",resource:"foamTex",levelAt:{index:"l"}},{name:"dst",resource:"foamTex",levelAt:{index:"l",offset:1}}],uniforms:[{name:"mip",values:{dstHeight:{index:"l",offset:1,shr:1024},dstWidth:{index:"l",offset:1,shr:1024},pad:0,srcLevel:0}}]}]},{name:"probe",type:"kernel",kernel:"ocean_probe",entry:"ocean_probe",dispatch:[1,1,1],bindings:[{name:"points",resource:"probePoints"},{name:"disp0",resource:"fftA_0"},{name:"disp1",resource:"fftA_1"},{name:"disp2",resource:"fftA_2"},{name:"disp3",resource:"fftA_2"},{name:"out",resource:"probeOut"}],uniforms:[{name:"probe",values:{cascades:3,count:"param:probeCount",lambda:"param:lambda",n0:64,n1:64,n2:64,n3:64,normalStep:.5,omX0:"param:probeOriginMod0X",omX1:"param:probeOriginMod1X",omX2:"param:probeOriginMod2X",omX3:"param:probeOriginMod2X",omZ0:"param:probeOriginMod0Z",omZ1:"param:probeOriginMod1Z",omZ2:"param:probeOriginMod2Z",omZ3:"param:probeOriginMod2Z",size0:1600,size1:250,size2:37,size3:37,t:"param:simTime"}}]}],frameSwaps:[["foamA","foamB"]],exports:{disp0:"dispTex0",disp1:"dispTex1",disp2:"dispTex2",foam:"foamTex",probe:"probeOut",slope0:"slopeTex0",slope1:"slopeTex1",slope2:"slopeTex2"},exportParams:["waterModX0","waterModZ0","waterModX1","waterModZ1","waterModX2","waterModZ2"]},"webgl2/high":{name:"ocean",backend:"webgl2",tier:"high",version:1,params:[{name:"windSpeed",type:"f32",source:{kind:"value",value:9},tween:20},{name:"fetch",type:"f32",source:{kind:"value",value:300},tween:20},{name:"gamma",type:"f32",source:{kind:"value",value:3.3},tween:20},{name:"spread",type:"f32",source:{kind:"value",value:8},tween:20},{name:"swell",type:"f32",source:{kind:"value",value:0},tween:20},{name:"windDirDeg",type:"f32",source:{kind:"value",value:0},tween:20},{name:"swellDirDeg",type:"f32",source:{kind:"value",value:0},tween:20},{name:"seed",type:"u32",source:{kind:"value",value:0}},{name:"loopPeriod",type:"f32",source:{kind:"value",value:1e3}},{name:"lambda",type:"f32",source:{kind:"value",value:1},tween:20},{name:"timeScale",type:"f32",source:{kind:"value",value:1}},{name:"driftX",type:"f32",source:{kind:"value",value:0}},{name:"driftZ",type:"f32",source:{kind:"value",value:0}},{name:"foamTau",type:"f32",source:{kind:"value",value:4}},{name:"foamThreshold",type:"f32",source:{kind:"value",value:.9}},{name:"foamGain",type:"f32",source:{kind:"value",value:2}},{name:"probeCount",type:"u32",source:{kind:"value",value:0}},{name:"dt",type:"f32",source:{kind:"frame",name:"dt"}},{name:"cameraX",type:"f32",source:{kind:"frame",name:"cameraX"}},{name:"cameraZ",type:"f32",source:{kind:"frame",name:"cameraZ"}},{name:"windDirRad",type:"f32",source:{args:["param:windDirDeg",.017453292519943295],kind:"expr",op:"mul"}},{name:"windDir",type:"f32",source:{args:[1.5707963267948966,"param:windDirRad"],kind:"expr",op:"sub"}},{name:"swellDirRad",type:"f32",source:{args:["param:swellDirDeg",.017453292519943295],kind:"expr",op:"mul"}},{name:"swellDir",type:"f32",source:{args:[1.5707963267948966,"param:swellDirRad"],kind:"expr",op:"sub"}},{name:"simTime",type:"f32",source:{kind:"accumulate",of:"param:timeScale"}},{name:"phaseTime",type:"f32",source:{args:["param:simTime","param:loopPeriod"],kind:"expr",op:"mod"}},{name:"waterX",type:"f32",source:{kind:"accumulate",of:"param:driftX"}},{name:"waterZ",type:"f32",source:{kind:"accumulate",of:"param:driftZ"}},{name:"waterModX0",type:"f32",source:{args:["param:waterX",1600],kind:"expr",op:"mod"}},{name:"waterModZ0",type:"f32",source:{args:["param:waterZ",1600],kind:"expr",op:"mod"}},{name:"waterModX1",type:"f32",source:{args:["param:waterX",250],kind:"expr",op:"mod"}},{name:"waterModZ1",type:"f32",source:{args:["param:waterZ",250],kind:"expr",op:"mod"}},{name:"waterModX2",type:"f32",source:{args:["param:waterX",37],kind:"expr",op:"mod"}},{name:"waterModZ2",type:"f32",source:{args:["param:waterZ",37],kind:"expr",op:"mod"}},{name:"waterModX3",type:"f32",source:{args:["param:waterX",5.3],kind:"expr",op:"mod"}},{name:"waterModZ3",type:"f32",source:{args:["param:waterZ",5.3],kind:"expr",op:"mod"}},{name:"foamRelX",type:"f32",source:{args:["param:cameraX","param:waterX"],kind:"expr",op:"sub"}},{name:"foamTexelX",type:"f32",source:{args:["param:foamRelX",.5],kind:"expr",op:"div"}},{name:"foamCellX",type:"f32",source:{args:["param:foamTexelX"],kind:"expr",op:"floor"}},{name:"foamOriginX",type:"i32",source:{args:["param:foamCellX",512],kind:"expr",op:"sub"}},{name:"foamShiftX",type:"i32",source:{kind:"delta",of:"param:foamOriginX"}},{name:"foamOriginMetersX",type:"f32",source:{args:["param:foamOriginX",.5],kind:"expr",op:"mul"}},{name:"foamOriginMod0X",type:"f32",source:{args:["param:foamOriginMetersX",1600],kind:"expr",op:"mod"}},{name:"foamOriginMod1X",type:"f32",source:{args:["param:foamOriginMetersX",250],kind:"expr",op:"mod"}},{name:"foamOriginMod2X",type:"f32",source:{args:["param:foamOriginMetersX",37],kind:"expr",op:"mod"}},{name:"foamOriginMod3X",type:"f32",source:{args:["param:foamOriginMetersX",5.3],kind:"expr",op:"mod"}},{name:"foamRelZ",type:"f32",source:{args:["param:cameraZ","param:waterZ"],kind:"expr",op:"sub"}},{name:"foamTexelZ",type:"f32",source:{args:["param:foamRelZ",.5],kind:"expr",op:"div"}},{name:"foamCellZ",type:"f32",source:{args:["param:foamTexelZ"],kind:"expr",op:"floor"}},{name:"foamOriginZ",type:"i32",source:{args:["param:foamCellZ",512],kind:"expr",op:"sub"}},{name:"foamShiftZ",type:"i32",source:{kind:"delta",of:"param:foamOriginZ"}},{name:"foamOriginMetersZ",type:"f32",source:{args:["param:foamOriginZ",.5],kind:"expr",op:"mul"}},{name:"foamOriginMod0Z",type:"f32",source:{args:["param:foamOriginMetersZ",1600],kind:"expr",op:"mod"}},{name:"foamOriginMod1Z",type:"f32",source:{args:["param:foamOriginMetersZ",250],kind:"expr",op:"mod"}},{name:"foamOriginMod2Z",type:"f32",source:{args:["param:foamOriginMetersZ",37],kind:"expr",op:"mod"}},{name:"foamOriginMod3Z",type:"f32",source:{args:["param:foamOriginMetersZ",5.3],kind:"expr",op:"mod"}}],resources:[{name:"h0_0",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"omega_0",kind:"buffer",elementType:"f32",count:4096,width:64},{name:"fftA_0",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"fftB_0",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"fftA2_0",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"fftB2_0",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"dispTex0",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"slopeTex0",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"h0_1",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"omega_1",kind:"buffer",elementType:"f32",count:65536,width:256},{name:"fftA_1",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"fftB_1",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"fftA2_1",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"fftB2_1",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"dispTex1",kind:"texture",format:"rgba16float",width:256,height:256,mipLevels:9},{name:"slopeTex1",kind:"texture",format:"rgba16float",width:256,height:256,mipLevels:9},{name:"h0_2",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"omega_2",kind:"buffer",elementType:"f32",count:65536,width:256},{name:"fftA_2",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"fftB_2",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"fftA2_2",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"fftB2_2",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"dispTex2",kind:"texture",format:"rgba16float",width:256,height:256,mipLevels:9},{name:"slopeTex2",kind:"texture",format:"rgba16float",width:256,height:256,mipLevels:9},{name:"h0_3",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"omega_3",kind:"buffer",elementType:"f32",count:65536,width:256},{name:"fftA_3",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"fftB_3",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"fftA2_3",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"fftB2_3",kind:"buffer",elementType:"vec4<f32>",count:65536,width:256},{name:"dispTex3",kind:"texture",format:"rgba16float",width:256,height:256,mipLevels:9},{name:"slopeTex3",kind:"texture",format:"rgba16float",width:256,height:256,mipLevels:9},{name:"foamA",kind:"buffer",elementType:"f32",count:1048576,width:1024},{name:"foamB",kind:"buffer",elementType:"f32",count:1048576,width:1024},{name:"foamTex",kind:"texture",format:"rgba16float",width:1024,height:1024,mipLevels:11}],passes:[{name:"c0.spectrum",type:"kernel",kernel:"ocean_spectrum",entry:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",target:{width:64,height:64,elioWidth:64},bindings:[{name:"h0",resource:"h0_0"},{name:"omega",resource:"omega_0"}],uniforms:[{name:"spec",values:{cascade:0,fetchKm:"param:fetch",gamma:"param:gamma",kMax:.12,kMin:.003926990816987242,loopPeriod:"param:loopPeriod",n:64,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:1600,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c0.evolve",type:"kernel",kernel:"ocean_evolve",entry:"ocean_evolve",target:{width:64,height:64,elioWidth:64},bindings:[{name:"h0",resource:"h0_0"},{name:"omega",resource:"omega_0"},{name:"outA",resource:"fftA_0"},{name:"outB",resource:"fftB_0"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:64,t:"param:phaseTime",tile:1600}}]},{name:"c0.fft.rows",type:"repeat",count:6,index:"s",swap:[["fftA_0","fftA2_0"],["fftB_0","fftB2_0"]],body:[{name:"A",type:"kernel",kernel:"fft_gather_64",entry:"fft_stage",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftA_0"},{name:"dst",resource:"fftA2_0"}],uniforms:[{name:"fft",values:{axis:0,n:64,stage:"param:s"}}]},{name:"B",type:"kernel",kernel:"fft_gather_64",entry:"fft_stage",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftB_0"},{name:"dst",resource:"fftB2_0"}],uniforms:[{name:"fft",values:{axis:0,n:64,stage:"param:s"}}]}]},{name:"c0.fft.cols",type:"repeat",count:6,index:"s",swap:[["fftA_0","fftA2_0"],["fftB_0","fftB2_0"]],body:[{name:"A",type:"kernel",kernel:"fft_gather_64",entry:"fft_stage",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftA_0"},{name:"dst",resource:"fftA2_0"}],uniforms:[{name:"fft",values:{axis:1,n:64,stage:"param:s"}}]},{name:"B",type:"kernel",kernel:"fft_gather_64",entry:"fft_stage",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftB_0"},{name:"dst",resource:"fftB2_0"}],uniforms:[{name:"fft",values:{axis:1,n:64,stage:"param:s"}}]}]},{name:"c0.assemble",type:"kernel",kernel:"ocean_assemble",entry:"ocean_assemble",target:{width:64,height:64,elioWidth:64},bindings:[{name:"fieldA",resource:"fftA_0"},{name:"fieldB",resource:"fftB_0"},{name:"disp",resource:"dispTex0"},{name:"slope",resource:"slopeTex0"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:64}}]},{name:"c0.dispTex0.mips",type:"mipmap",resource:"dispTex0"},{name:"c0.slopeTex0.mips",type:"mipmap",resource:"slopeTex0"},{name:"c1.spectrum",type:"kernel",kernel:"ocean_spectrum",entry:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",target:{width:256,height:256,elioWidth:256},bindings:[{name:"h0",resource:"h0_1"},{name:"omega",resource:"omega_1"}],uniforms:[{name:"spec",values:{cascade:1,fetchKm:"param:fetch",gamma:"param:gamma",kMax:.8,kMin:.12,loopPeriod:"param:loopPeriod",n:256,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:250,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c1.evolve",type:"kernel",kernel:"ocean_evolve",entry:"ocean_evolve",target:{width:256,height:256,elioWidth:256},bindings:[{name:"h0",resource:"h0_1"},{name:"omega",resource:"omega_1"},{name:"outA",resource:"fftA_1"},{name:"outB",resource:"fftB_1"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:256,t:"param:phaseTime",tile:250}}]},{name:"c1.fft.rows",type:"repeat",count:8,index:"s",swap:[["fftA_1","fftA2_1"],["fftB_1","fftB2_1"]],body:[{name:"A",type:"kernel",kernel:"fft_gather_256",entry:"fft_stage",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"fftA_1"},{name:"dst",resource:"fftA2_1"}],uniforms:[{name:"fft",values:{axis:0,n:256,stage:"param:s"}}]},{name:"B",type:"kernel",kernel:"fft_gather_256",entry:"fft_stage",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"fftB_1"},{name:"dst",resource:"fftB2_1"}],uniforms:[{name:"fft",values:{axis:0,n:256,stage:"param:s"}}]}]},{name:"c1.fft.cols",type:"repeat",count:8,index:"s",swap:[["fftA_1","fftA2_1"],["fftB_1","fftB2_1"]],body:[{name:"A",type:"kernel",kernel:"fft_gather_256",entry:"fft_stage",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"fftA_1"},{name:"dst",resource:"fftA2_1"}],uniforms:[{name:"fft",values:{axis:1,n:256,stage:"param:s"}}]},{name:"B",type:"kernel",kernel:"fft_gather_256",entry:"fft_stage",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"fftB_1"},{name:"dst",resource:"fftB2_1"}],uniforms:[{name:"fft",values:{axis:1,n:256,stage:"param:s"}}]}]},{name:"c1.assemble",type:"kernel",kernel:"ocean_assemble",entry:"ocean_assemble",target:{width:256,height:256,elioWidth:256},bindings:[{name:"fieldA",resource:"fftA_1"},{name:"fieldB",resource:"fftB_1"},{name:"disp",resource:"dispTex1"},{name:"slope",resource:"slopeTex1"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:256}}]},{name:"c1.dispTex1.mips",type:"mipmap",resource:"dispTex1"},{name:"c1.slopeTex1.mips",type:"mipmap",resource:"slopeTex1"},{name:"c2.spectrum",type:"kernel",kernel:"ocean_spectrum",entry:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",target:{width:256,height:256,elioWidth:256},bindings:[{name:"h0",resource:"h0_2"},{name:"omega",resource:"omega_2"}],uniforms:[{name:"spec",values:{cascade:2,fetchKm:"param:fetch",gamma:"param:gamma",kMax:5.4,kMin:.8,loopPeriod:"param:loopPeriod",n:256,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:37,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c2.evolve",type:"kernel",kernel:"ocean_evolve",entry:"ocean_evolve",target:{width:256,height:256,elioWidth:256},bindings:[{name:"h0",resource:"h0_2"},{name:"omega",resource:"omega_2"},{name:"outA",resource:"fftA_2"},{name:"outB",resource:"fftB_2"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:256,t:"param:phaseTime",tile:37}}]},{name:"c2.fft.rows",type:"repeat",count:8,index:"s",swap:[["fftA_2","fftA2_2"],["fftB_2","fftB2_2"]],body:[{name:"A",type:"kernel",kernel:"fft_gather_256",entry:"fft_stage",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"fftA_2"},{name:"dst",resource:"fftA2_2"}],uniforms:[{name:"fft",values:{axis:0,n:256,stage:"param:s"}}]},{name:"B",type:"kernel",kernel:"fft_gather_256",entry:"fft_stage",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"fftB_2"},{name:"dst",resource:"fftB2_2"}],uniforms:[{name:"fft",values:{axis:0,n:256,stage:"param:s"}}]}]},{name:"c2.fft.cols",type:"repeat",count:8,index:"s",swap:[["fftA_2","fftA2_2"],["fftB_2","fftB2_2"]],body:[{name:"A",type:"kernel",kernel:"fft_gather_256",entry:"fft_stage",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"fftA_2"},{name:"dst",resource:"fftA2_2"}],uniforms:[{name:"fft",values:{axis:1,n:256,stage:"param:s"}}]},{name:"B",type:"kernel",kernel:"fft_gather_256",entry:"fft_stage",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"fftB_2"},{name:"dst",resource:"fftB2_2"}],uniforms:[{name:"fft",values:{axis:1,n:256,stage:"param:s"}}]}]},{name:"c2.assemble",type:"kernel",kernel:"ocean_assemble",entry:"ocean_assemble",target:{width:256,height:256,elioWidth:256},bindings:[{name:"fieldA",resource:"fftA_2"},{name:"fieldB",resource:"fftB_2"},{name:"disp",resource:"dispTex2"},{name:"slope",resource:"slopeTex2"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:256}}]},{name:"c2.dispTex2.mips",type:"mipmap",resource:"dispTex2"},{name:"c2.slopeTex2.mips",type:"mipmap",resource:"slopeTex2"},{name:"c3.spectrum",type:"kernel",kernel:"ocean_spectrum",entry:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",target:{width:256,height:256,elioWidth:256},bindings:[{name:"h0",resource:"h0_3"},{name:"omega",resource:"omega_3"}],uniforms:[{name:"spec",values:{cascade:3,fetchKm:"param:fetch",gamma:"param:gamma",kMax:37,kMin:5.4,loopPeriod:"param:loopPeriod",n:256,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:5.3,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c3.evolve",type:"kernel",kernel:"ocean_evolve",entry:"ocean_evolve",target:{width:256,height:256,elioWidth:256},bindings:[{name:"h0",resource:"h0_3"},{name:"omega",resource:"omega_3"},{name:"outA",resource:"fftA_3"},{name:"outB",resource:"fftB_3"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:256,t:"param:phaseTime",tile:5.3}}]},{name:"c3.fft.rows",type:"repeat",count:8,index:"s",swap:[["fftA_3","fftA2_3"],["fftB_3","fftB2_3"]],body:[{name:"A",type:"kernel",kernel:"fft_gather_256",entry:"fft_stage",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"fftA_3"},{name:"dst",resource:"fftA2_3"}],uniforms:[{name:"fft",values:{axis:0,n:256,stage:"param:s"}}]},{name:"B",type:"kernel",kernel:"fft_gather_256",entry:"fft_stage",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"fftB_3"},{name:"dst",resource:"fftB2_3"}],uniforms:[{name:"fft",values:{axis:0,n:256,stage:"param:s"}}]}]},{name:"c3.fft.cols",type:"repeat",count:8,index:"s",swap:[["fftA_3","fftA2_3"],["fftB_3","fftB2_3"]],body:[{name:"A",type:"kernel",kernel:"fft_gather_256",entry:"fft_stage",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"fftA_3"},{name:"dst",resource:"fftA2_3"}],uniforms:[{name:"fft",values:{axis:1,n:256,stage:"param:s"}}]},{name:"B",type:"kernel",kernel:"fft_gather_256",entry:"fft_stage",target:{width:256,height:256,elioWidth:256},bindings:[{name:"src",resource:"fftB_3"},{name:"dst",resource:"fftB2_3"}],uniforms:[{name:"fft",values:{axis:1,n:256,stage:"param:s"}}]}]},{name:"c3.assemble",type:"kernel",kernel:"ocean_assemble",entry:"ocean_assemble",target:{width:256,height:256,elioWidth:256},bindings:[{name:"fieldA",resource:"fftA_3"},{name:"fieldB",resource:"fftB_3"},{name:"disp",resource:"dispTex3"},{name:"slope",resource:"slopeTex3"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:256}}]},{name:"c3.dispTex3.mips",type:"mipmap",resource:"dispTex3"},{name:"c3.slopeTex3.mips",type:"mipmap",resource:"slopeTex3"},{name:"foam",type:"kernel",kernel:"ocean_foam",entry:"ocean_foam",target:{width:1024,height:1024,elioWidth:1024},bindings:[{name:"foamIn",resource:"foamA"},{name:"fieldB0",resource:"fftB_0"},{name:"fieldB1",resource:"fftB_1"},{name:"fieldB2",resource:"fftB_2"},{name:"fieldB3",resource:"fftB_3"},{name:"foamOut",resource:"foamB"},{name:"foamTex",resource:"foamTex"}],uniforms:[{name:"foam",values:{cascades:4,cell:.5,dt:"param:dt",gain:"param:foamGain",lambda:"param:lambda",n0:64,n1:256,n2:256,n3:256,omX0:"param:foamOriginMod0X",omX1:"param:foamOriginMod1X",omX2:"param:foamOriginMod2X",omX3:"param:foamOriginMod3X",omZ0:"param:foamOriginMod0Z",omZ1:"param:foamOriginMod1Z",omZ2:"param:foamOriginMod2Z",omZ3:"param:foamOriginMod3Z",originX:"param:foamOriginX",originZ:"param:foamOriginZ",shiftX:"param:foamShiftX",shiftZ:"param:foamShiftZ",size0:1600,size1:250,size2:37,size3:5.3,tau:"param:foamTau",threshold:"param:foamThreshold",width:1024}}]},{name:"foamTex.mips",type:"mipmap",resource:"foamTex"}],frameSwaps:[["foamA","foamB"]],exports:{disp0:"dispTex0",disp1:"dispTex1",disp2:"dispTex2",disp3:"dispTex3",foam:"foamTex",slope0:"slopeTex0",slope1:"slopeTex1",slope2:"slopeTex2",slope3:"slopeTex3"},exportParams:["waterModX0","waterModZ0","waterModX1","waterModZ1","waterModX2","waterModZ2","waterModX3","waterModZ3"]},"webgl2/medium":{name:"ocean",backend:"webgl2",tier:"medium",version:1,params:[{name:"windSpeed",type:"f32",source:{kind:"value",value:9},tween:20},{name:"fetch",type:"f32",source:{kind:"value",value:300},tween:20},{name:"gamma",type:"f32",source:{kind:"value",value:3.3},tween:20},{name:"spread",type:"f32",source:{kind:"value",value:8},tween:20},{name:"swell",type:"f32",source:{kind:"value",value:0},tween:20},{name:"windDirDeg",type:"f32",source:{kind:"value",value:0},tween:20},{name:"swellDirDeg",type:"f32",source:{kind:"value",value:0},tween:20},{name:"seed",type:"u32",source:{kind:"value",value:0}},{name:"loopPeriod",type:"f32",source:{kind:"value",value:1e3}},{name:"lambda",type:"f32",source:{kind:"value",value:1},tween:20},{name:"timeScale",type:"f32",source:{kind:"value",value:1}},{name:"driftX",type:"f32",source:{kind:"value",value:0}},{name:"driftZ",type:"f32",source:{kind:"value",value:0}},{name:"foamTau",type:"f32",source:{kind:"value",value:4}},{name:"foamThreshold",type:"f32",source:{kind:"value",value:.9}},{name:"foamGain",type:"f32",source:{kind:"value",value:2}},{name:"probeCount",type:"u32",source:{kind:"value",value:0}},{name:"dt",type:"f32",source:{kind:"frame",name:"dt"}},{name:"cameraX",type:"f32",source:{kind:"frame",name:"cameraX"}},{name:"cameraZ",type:"f32",source:{kind:"frame",name:"cameraZ"}},{name:"windDirRad",type:"f32",source:{args:["param:windDirDeg",.017453292519943295],kind:"expr",op:"mul"}},{name:"windDir",type:"f32",source:{args:[1.5707963267948966,"param:windDirRad"],kind:"expr",op:"sub"}},{name:"swellDirRad",type:"f32",source:{args:["param:swellDirDeg",.017453292519943295],kind:"expr",op:"mul"}},{name:"swellDir",type:"f32",source:{args:[1.5707963267948966,"param:swellDirRad"],kind:"expr",op:"sub"}},{name:"simTime",type:"f32",source:{kind:"accumulate",of:"param:timeScale"}},{name:"phaseTime",type:"f32",source:{args:["param:simTime","param:loopPeriod"],kind:"expr",op:"mod"}},{name:"waterX",type:"f32",source:{kind:"accumulate",of:"param:driftX"}},{name:"waterZ",type:"f32",source:{kind:"accumulate",of:"param:driftZ"}},{name:"waterModX0",type:"f32",source:{args:["param:waterX",1600],kind:"expr",op:"mod"}},{name:"waterModZ0",type:"f32",source:{args:["param:waterZ",1600],kind:"expr",op:"mod"}},{name:"waterModX1",type:"f32",source:{args:["param:waterX",250],kind:"expr",op:"mod"}},{name:"waterModZ1",type:"f32",source:{args:["param:waterZ",250],kind:"expr",op:"mod"}},{name:"waterModX2",type:"f32",source:{args:["param:waterX",37],kind:"expr",op:"mod"}},{name:"waterModZ2",type:"f32",source:{args:["param:waterZ",37],kind:"expr",op:"mod"}},{name:"waterModX3",type:"f32",source:{args:["param:waterX",5.3],kind:"expr",op:"mod"}},{name:"waterModZ3",type:"f32",source:{args:["param:waterZ",5.3],kind:"expr",op:"mod"}},{name:"foamRelX",type:"f32",source:{args:["param:cameraX","param:waterX"],kind:"expr",op:"sub"}},{name:"foamTexelX",type:"f32",source:{args:["param:foamRelX",.5],kind:"expr",op:"div"}},{name:"foamCellX",type:"f32",source:{args:["param:foamTexelX"],kind:"expr",op:"floor"}},{name:"foamOriginX",type:"i32",source:{args:["param:foamCellX",512],kind:"expr",op:"sub"}},{name:"foamShiftX",type:"i32",source:{kind:"delta",of:"param:foamOriginX"}},{name:"foamOriginMetersX",type:"f32",source:{args:["param:foamOriginX",.5],kind:"expr",op:"mul"}},{name:"foamOriginMod0X",type:"f32",source:{args:["param:foamOriginMetersX",1600],kind:"expr",op:"mod"}},{name:"foamOriginMod1X",type:"f32",source:{args:["param:foamOriginMetersX",250],kind:"expr",op:"mod"}},{name:"foamOriginMod2X",type:"f32",source:{args:["param:foamOriginMetersX",37],kind:"expr",op:"mod"}},{name:"foamOriginMod3X",type:"f32",source:{args:["param:foamOriginMetersX",5.3],kind:"expr",op:"mod"}},{name:"foamRelZ",type:"f32",source:{args:["param:cameraZ","param:waterZ"],kind:"expr",op:"sub"}},{name:"foamTexelZ",type:"f32",source:{args:["param:foamRelZ",.5],kind:"expr",op:"div"}},{name:"foamCellZ",type:"f32",source:{args:["param:foamTexelZ"],kind:"expr",op:"floor"}},{name:"foamOriginZ",type:"i32",source:{args:["param:foamCellZ",512],kind:"expr",op:"sub"}},{name:"foamShiftZ",type:"i32",source:{kind:"delta",of:"param:foamOriginZ"}},{name:"foamOriginMetersZ",type:"f32",source:{args:["param:foamOriginZ",.5],kind:"expr",op:"mul"}},{name:"foamOriginMod0Z",type:"f32",source:{args:["param:foamOriginMetersZ",1600],kind:"expr",op:"mod"}},{name:"foamOriginMod1Z",type:"f32",source:{args:["param:foamOriginMetersZ",250],kind:"expr",op:"mod"}},{name:"foamOriginMod2Z",type:"f32",source:{args:["param:foamOriginMetersZ",37],kind:"expr",op:"mod"}},{name:"foamOriginMod3Z",type:"f32",source:{args:["param:foamOriginMetersZ",5.3],kind:"expr",op:"mod"}}],resources:[{name:"h0_0",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"omega_0",kind:"buffer",elementType:"f32",count:4096,width:64},{name:"fftA_0",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"fftB_0",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"fftA2_0",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"fftB2_0",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"dispTex0",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"slopeTex0",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"h0_1",kind:"buffer",elementType:"vec4<f32>",count:16384,width:128},{name:"omega_1",kind:"buffer",elementType:"f32",count:16384,width:128},{name:"fftA_1",kind:"buffer",elementType:"vec4<f32>",count:16384,width:128},{name:"fftB_1",kind:"buffer",elementType:"vec4<f32>",count:16384,width:128},{name:"fftA2_1",kind:"buffer",elementType:"vec4<f32>",count:16384,width:128},{name:"fftB2_1",kind:"buffer",elementType:"vec4<f32>",count:16384,width:128},{name:"dispTex1",kind:"texture",format:"rgba16float",width:128,height:128,mipLevels:8},{name:"slopeTex1",kind:"texture",format:"rgba16float",width:128,height:128,mipLevels:8},{name:"h0_2",kind:"buffer",elementType:"vec4<f32>",count:16384,width:128},{name:"omega_2",kind:"buffer",elementType:"f32",count:16384,width:128},{name:"fftA_2",kind:"buffer",elementType:"vec4<f32>",count:16384,width:128},{name:"fftB_2",kind:"buffer",elementType:"vec4<f32>",count:16384,width:128},{name:"fftA2_2",kind:"buffer",elementType:"vec4<f32>",count:16384,width:128},{name:"fftB2_2",kind:"buffer",elementType:"vec4<f32>",count:16384,width:128},{name:"dispTex2",kind:"texture",format:"rgba16float",width:128,height:128,mipLevels:8},{name:"slopeTex2",kind:"texture",format:"rgba16float",width:128,height:128,mipLevels:8},{name:"h0_3",kind:"buffer",elementType:"vec4<f32>",count:16384,width:128},{name:"omega_3",kind:"buffer",elementType:"f32",count:16384,width:128},{name:"fftA_3",kind:"buffer",elementType:"vec4<f32>",count:16384,width:128},{name:"fftB_3",kind:"buffer",elementType:"vec4<f32>",count:16384,width:128},{name:"fftA2_3",kind:"buffer",elementType:"vec4<f32>",count:16384,width:128},{name:"fftB2_3",kind:"buffer",elementType:"vec4<f32>",count:16384,width:128},{name:"dispTex3",kind:"texture",format:"rgba16float",width:128,height:128,mipLevels:8},{name:"slopeTex3",kind:"texture",format:"rgba16float",width:128,height:128,mipLevels:8},{name:"foamA",kind:"buffer",elementType:"f32",count:1048576,width:1024},{name:"foamB",kind:"buffer",elementType:"f32",count:1048576,width:1024},{name:"foamTex",kind:"texture",format:"rgba16float",width:1024,height:1024,mipLevels:11}],passes:[{name:"c0.spectrum",type:"kernel",kernel:"ocean_spectrum",entry:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",target:{width:64,height:64,elioWidth:64},bindings:[{name:"h0",resource:"h0_0"},{name:"omega",resource:"omega_0"}],uniforms:[{name:"spec",values:{cascade:0,fetchKm:"param:fetch",gamma:"param:gamma",kMax:.12,kMin:.003926990816987242,loopPeriod:"param:loopPeriod",n:64,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:1600,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c0.evolve",type:"kernel",kernel:"ocean_evolve",entry:"ocean_evolve",target:{width:64,height:64,elioWidth:64},bindings:[{name:"h0",resource:"h0_0"},{name:"omega",resource:"omega_0"},{name:"outA",resource:"fftA_0"},{name:"outB",resource:"fftB_0"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:64,t:"param:phaseTime",tile:1600}}]},{name:"c0.fft.rows",type:"repeat",count:6,index:"s",swap:[["fftA_0","fftA2_0"],["fftB_0","fftB2_0"]],body:[{name:"A",type:"kernel",kernel:"fft_gather_64",entry:"fft_stage",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftA_0"},{name:"dst",resource:"fftA2_0"}],uniforms:[{name:"fft",values:{axis:0,n:64,stage:"param:s"}}]},{name:"B",type:"kernel",kernel:"fft_gather_64",entry:"fft_stage",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftB_0"},{name:"dst",resource:"fftB2_0"}],uniforms:[{name:"fft",values:{axis:0,n:64,stage:"param:s"}}]}]},{name:"c0.fft.cols",type:"repeat",count:6,index:"s",swap:[["fftA_0","fftA2_0"],["fftB_0","fftB2_0"]],body:[{name:"A",type:"kernel",kernel:"fft_gather_64",entry:"fft_stage",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftA_0"},{name:"dst",resource:"fftA2_0"}],uniforms:[{name:"fft",values:{axis:1,n:64,stage:"param:s"}}]},{name:"B",type:"kernel",kernel:"fft_gather_64",entry:"fft_stage",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftB_0"},{name:"dst",resource:"fftB2_0"}],uniforms:[{name:"fft",values:{axis:1,n:64,stage:"param:s"}}]}]},{name:"c0.assemble",type:"kernel",kernel:"ocean_assemble",entry:"ocean_assemble",target:{width:64,height:64,elioWidth:64},bindings:[{name:"fieldA",resource:"fftA_0"},{name:"fieldB",resource:"fftB_0"},{name:"disp",resource:"dispTex0"},{name:"slope",resource:"slopeTex0"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:64}}]},{name:"c0.dispTex0.mips",type:"mipmap",resource:"dispTex0"},{name:"c0.slopeTex0.mips",type:"mipmap",resource:"slopeTex0"},{name:"c1.spectrum",type:"kernel",kernel:"ocean_spectrum",entry:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",target:{width:128,height:128,elioWidth:128},bindings:[{name:"h0",resource:"h0_1"},{name:"omega",resource:"omega_1"}],uniforms:[{name:"spec",values:{cascade:1,fetchKm:"param:fetch",gamma:"param:gamma",kMax:.8,kMin:.12,loopPeriod:"param:loopPeriod",n:128,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:250,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c1.evolve",type:"kernel",kernel:"ocean_evolve",entry:"ocean_evolve",target:{width:128,height:128,elioWidth:128},bindings:[{name:"h0",resource:"h0_1"},{name:"omega",resource:"omega_1"},{name:"outA",resource:"fftA_1"},{name:"outB",resource:"fftB_1"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:128,t:"param:phaseTime",tile:250}}]},{name:"c1.fft.rows",type:"repeat",count:7,index:"s",swap:[["fftA_1","fftA2_1"],["fftB_1","fftB2_1"]],body:[{name:"A",type:"kernel",kernel:"fft_gather_128",entry:"fft_stage",target:{width:128,height:128,elioWidth:128},bindings:[{name:"src",resource:"fftA_1"},{name:"dst",resource:"fftA2_1"}],uniforms:[{name:"fft",values:{axis:0,n:128,stage:"param:s"}}]},{name:"B",type:"kernel",kernel:"fft_gather_128",entry:"fft_stage",target:{width:128,height:128,elioWidth:128},bindings:[{name:"src",resource:"fftB_1"},{name:"dst",resource:"fftB2_1"}],uniforms:[{name:"fft",values:{axis:0,n:128,stage:"param:s"}}]}]},{name:"c1.fft.cols",type:"repeat",count:7,index:"s",swap:[["fftA_1","fftA2_1"],["fftB_1","fftB2_1"]],body:[{name:"A",type:"kernel",kernel:"fft_gather_128",entry:"fft_stage",target:{width:128,height:128,elioWidth:128},bindings:[{name:"src",resource:"fftA_1"},{name:"dst",resource:"fftA2_1"}],uniforms:[{name:"fft",values:{axis:1,n:128,stage:"param:s"}}]},{name:"B",type:"kernel",kernel:"fft_gather_128",entry:"fft_stage",target:{width:128,height:128,elioWidth:128},bindings:[{name:"src",resource:"fftB_1"},{name:"dst",resource:"fftB2_1"}],uniforms:[{name:"fft",values:{axis:1,n:128,stage:"param:s"}}]}]},{name:"c1.assemble",type:"kernel",kernel:"ocean_assemble",entry:"ocean_assemble",target:{width:128,height:128,elioWidth:128},bindings:[{name:"fieldA",resource:"fftA_1"},{name:"fieldB",resource:"fftB_1"},{name:"disp",resource:"dispTex1"},{name:"slope",resource:"slopeTex1"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:128}}]},{name:"c1.dispTex1.mips",type:"mipmap",resource:"dispTex1"},{name:"c1.slopeTex1.mips",type:"mipmap",resource:"slopeTex1"},{name:"c2.spectrum",type:"kernel",kernel:"ocean_spectrum",entry:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",target:{width:128,height:128,elioWidth:128},bindings:[{name:"h0",resource:"h0_2"},{name:"omega",resource:"omega_2"}],uniforms:[{name:"spec",values:{cascade:2,fetchKm:"param:fetch",gamma:"param:gamma",kMax:5.4,kMin:.8,loopPeriod:"param:loopPeriod",n:128,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:37,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c2.evolve",type:"kernel",kernel:"ocean_evolve",entry:"ocean_evolve",target:{width:128,height:128,elioWidth:128},bindings:[{name:"h0",resource:"h0_2"},{name:"omega",resource:"omega_2"},{name:"outA",resource:"fftA_2"},{name:"outB",resource:"fftB_2"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:128,t:"param:phaseTime",tile:37}}]},{name:"c2.fft.rows",type:"repeat",count:7,index:"s",swap:[["fftA_2","fftA2_2"],["fftB_2","fftB2_2"]],body:[{name:"A",type:"kernel",kernel:"fft_gather_128",entry:"fft_stage",target:{width:128,height:128,elioWidth:128},bindings:[{name:"src",resource:"fftA_2"},{name:"dst",resource:"fftA2_2"}],uniforms:[{name:"fft",values:{axis:0,n:128,stage:"param:s"}}]},{name:"B",type:"kernel",kernel:"fft_gather_128",entry:"fft_stage",target:{width:128,height:128,elioWidth:128},bindings:[{name:"src",resource:"fftB_2"},{name:"dst",resource:"fftB2_2"}],uniforms:[{name:"fft",values:{axis:0,n:128,stage:"param:s"}}]}]},{name:"c2.fft.cols",type:"repeat",count:7,index:"s",swap:[["fftA_2","fftA2_2"],["fftB_2","fftB2_2"]],body:[{name:"A",type:"kernel",kernel:"fft_gather_128",entry:"fft_stage",target:{width:128,height:128,elioWidth:128},bindings:[{name:"src",resource:"fftA_2"},{name:"dst",resource:"fftA2_2"}],uniforms:[{name:"fft",values:{axis:1,n:128,stage:"param:s"}}]},{name:"B",type:"kernel",kernel:"fft_gather_128",entry:"fft_stage",target:{width:128,height:128,elioWidth:128},bindings:[{name:"src",resource:"fftB_2"},{name:"dst",resource:"fftB2_2"}],uniforms:[{name:"fft",values:{axis:1,n:128,stage:"param:s"}}]}]},{name:"c2.assemble",type:"kernel",kernel:"ocean_assemble",entry:"ocean_assemble",target:{width:128,height:128,elioWidth:128},bindings:[{name:"fieldA",resource:"fftA_2"},{name:"fieldB",resource:"fftB_2"},{name:"disp",resource:"dispTex2"},{name:"slope",resource:"slopeTex2"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:128}}]},{name:"c2.dispTex2.mips",type:"mipmap",resource:"dispTex2"},{name:"c2.slopeTex2.mips",type:"mipmap",resource:"slopeTex2"},{name:"c3.spectrum",type:"kernel",kernel:"ocean_spectrum",entry:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",target:{width:128,height:128,elioWidth:128},bindings:[{name:"h0",resource:"h0_3"},{name:"omega",resource:"omega_3"}],uniforms:[{name:"spec",values:{cascade:3,fetchKm:"param:fetch",gamma:"param:gamma",kMax:37,kMin:5.4,loopPeriod:"param:loopPeriod",n:128,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:5.3,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c3.evolve",type:"kernel",kernel:"ocean_evolve",entry:"ocean_evolve",target:{width:128,height:128,elioWidth:128},bindings:[{name:"h0",resource:"h0_3"},{name:"omega",resource:"omega_3"},{name:"outA",resource:"fftA_3"},{name:"outB",resource:"fftB_3"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:128,t:"param:phaseTime",tile:5.3}}]},{name:"c3.fft.rows",type:"repeat",count:7,index:"s",swap:[["fftA_3","fftA2_3"],["fftB_3","fftB2_3"]],body:[{name:"A",type:"kernel",kernel:"fft_gather_128",entry:"fft_stage",target:{width:128,height:128,elioWidth:128},bindings:[{name:"src",resource:"fftA_3"},{name:"dst",resource:"fftA2_3"}],uniforms:[{name:"fft",values:{axis:0,n:128,stage:"param:s"}}]},{name:"B",type:"kernel",kernel:"fft_gather_128",entry:"fft_stage",target:{width:128,height:128,elioWidth:128},bindings:[{name:"src",resource:"fftB_3"},{name:"dst",resource:"fftB2_3"}],uniforms:[{name:"fft",values:{axis:0,n:128,stage:"param:s"}}]}]},{name:"c3.fft.cols",type:"repeat",count:7,index:"s",swap:[["fftA_3","fftA2_3"],["fftB_3","fftB2_3"]],body:[{name:"A",type:"kernel",kernel:"fft_gather_128",entry:"fft_stage",target:{width:128,height:128,elioWidth:128},bindings:[{name:"src",resource:"fftA_3"},{name:"dst",resource:"fftA2_3"}],uniforms:[{name:"fft",values:{axis:1,n:128,stage:"param:s"}}]},{name:"B",type:"kernel",kernel:"fft_gather_128",entry:"fft_stage",target:{width:128,height:128,elioWidth:128},bindings:[{name:"src",resource:"fftB_3"},{name:"dst",resource:"fftB2_3"}],uniforms:[{name:"fft",values:{axis:1,n:128,stage:"param:s"}}]}]},{name:"c3.assemble",type:"kernel",kernel:"ocean_assemble",entry:"ocean_assemble",target:{width:128,height:128,elioWidth:128},bindings:[{name:"fieldA",resource:"fftA_3"},{name:"fieldB",resource:"fftB_3"},{name:"disp",resource:"dispTex3"},{name:"slope",resource:"slopeTex3"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:128}}]},{name:"c3.dispTex3.mips",type:"mipmap",resource:"dispTex3"},{name:"c3.slopeTex3.mips",type:"mipmap",resource:"slopeTex3"},{name:"foam",type:"kernel",kernel:"ocean_foam",entry:"ocean_foam",target:{width:1024,height:1024,elioWidth:1024},bindings:[{name:"foamIn",resource:"foamA"},{name:"fieldB0",resource:"fftB_0"},{name:"fieldB1",resource:"fftB_1"},{name:"fieldB2",resource:"fftB_2"},{name:"fieldB3",resource:"fftB_3"},{name:"foamOut",resource:"foamB"},{name:"foamTex",resource:"foamTex"}],uniforms:[{name:"foam",values:{cascades:4,cell:.5,dt:"param:dt",gain:"param:foamGain",lambda:"param:lambda",n0:64,n1:128,n2:128,n3:128,omX0:"param:foamOriginMod0X",omX1:"param:foamOriginMod1X",omX2:"param:foamOriginMod2X",omX3:"param:foamOriginMod3X",omZ0:"param:foamOriginMod0Z",omZ1:"param:foamOriginMod1Z",omZ2:"param:foamOriginMod2Z",omZ3:"param:foamOriginMod3Z",originX:"param:foamOriginX",originZ:"param:foamOriginZ",shiftX:"param:foamShiftX",shiftZ:"param:foamShiftZ",size0:1600,size1:250,size2:37,size3:5.3,tau:"param:foamTau",threshold:"param:foamThreshold",width:1024}}]},{name:"foamTex.mips",type:"mipmap",resource:"foamTex"}],frameSwaps:[["foamA","foamB"]],exports:{disp0:"dispTex0",disp1:"dispTex1",disp2:"dispTex2",disp3:"dispTex3",foam:"foamTex",slope0:"slopeTex0",slope1:"slopeTex1",slope2:"slopeTex2",slope3:"slopeTex3"},exportParams:["waterModX0","waterModZ0","waterModX1","waterModZ1","waterModX2","waterModZ2","waterModX3","waterModZ3"]},"webgl2/low":{name:"ocean",backend:"webgl2",tier:"low",version:1,params:[{name:"windSpeed",type:"f32",source:{kind:"value",value:9},tween:20},{name:"fetch",type:"f32",source:{kind:"value",value:300},tween:20},{name:"gamma",type:"f32",source:{kind:"value",value:3.3},tween:20},{name:"spread",type:"f32",source:{kind:"value",value:8},tween:20},{name:"swell",type:"f32",source:{kind:"value",value:0},tween:20},{name:"windDirDeg",type:"f32",source:{kind:"value",value:0},tween:20},{name:"swellDirDeg",type:"f32",source:{kind:"value",value:0},tween:20},{name:"seed",type:"u32",source:{kind:"value",value:0}},{name:"loopPeriod",type:"f32",source:{kind:"value",value:1e3}},{name:"lambda",type:"f32",source:{kind:"value",value:1},tween:20},{name:"timeScale",type:"f32",source:{kind:"value",value:1}},{name:"driftX",type:"f32",source:{kind:"value",value:0}},{name:"driftZ",type:"f32",source:{kind:"value",value:0}},{name:"foamTau",type:"f32",source:{kind:"value",value:4}},{name:"foamThreshold",type:"f32",source:{kind:"value",value:.9}},{name:"foamGain",type:"f32",source:{kind:"value",value:2}},{name:"probeCount",type:"u32",source:{kind:"value",value:0}},{name:"dt",type:"f32",source:{kind:"frame",name:"dt"}},{name:"cameraX",type:"f32",source:{kind:"frame",name:"cameraX"}},{name:"cameraZ",type:"f32",source:{kind:"frame",name:"cameraZ"}},{name:"windDirRad",type:"f32",source:{args:["param:windDirDeg",.017453292519943295],kind:"expr",op:"mul"}},{name:"windDir",type:"f32",source:{args:[1.5707963267948966,"param:windDirRad"],kind:"expr",op:"sub"}},{name:"swellDirRad",type:"f32",source:{args:["param:swellDirDeg",.017453292519943295],kind:"expr",op:"mul"}},{name:"swellDir",type:"f32",source:{args:[1.5707963267948966,"param:swellDirRad"],kind:"expr",op:"sub"}},{name:"simTime",type:"f32",source:{kind:"accumulate",of:"param:timeScale"}},{name:"phaseTime",type:"f32",source:{args:["param:simTime","param:loopPeriod"],kind:"expr",op:"mod"}},{name:"waterX",type:"f32",source:{kind:"accumulate",of:"param:driftX"}},{name:"waterZ",type:"f32",source:{kind:"accumulate",of:"param:driftZ"}},{name:"waterModX0",type:"f32",source:{args:["param:waterX",1600],kind:"expr",op:"mod"}},{name:"waterModZ0",type:"f32",source:{args:["param:waterZ",1600],kind:"expr",op:"mod"}},{name:"waterModX1",type:"f32",source:{args:["param:waterX",250],kind:"expr",op:"mod"}},{name:"waterModZ1",type:"f32",source:{args:["param:waterZ",250],kind:"expr",op:"mod"}},{name:"waterModX2",type:"f32",source:{args:["param:waterX",37],kind:"expr",op:"mod"}},{name:"waterModZ2",type:"f32",source:{args:["param:waterZ",37],kind:"expr",op:"mod"}},{name:"foamRelX",type:"f32",source:{args:["param:cameraX","param:waterX"],kind:"expr",op:"sub"}},{name:"foamTexelX",type:"f32",source:{args:["param:foamRelX",.5],kind:"expr",op:"div"}},{name:"foamCellX",type:"f32",source:{args:["param:foamTexelX"],kind:"expr",op:"floor"}},{name:"foamOriginX",type:"i32",source:{args:["param:foamCellX",512],kind:"expr",op:"sub"}},{name:"foamShiftX",type:"i32",source:{kind:"delta",of:"param:foamOriginX"}},{name:"foamOriginMetersX",type:"f32",source:{args:["param:foamOriginX",.5],kind:"expr",op:"mul"}},{name:"foamOriginMod0X",type:"f32",source:{args:["param:foamOriginMetersX",1600],kind:"expr",op:"mod"}},{name:"foamOriginMod1X",type:"f32",source:{args:["param:foamOriginMetersX",250],kind:"expr",op:"mod"}},{name:"foamOriginMod2X",type:"f32",source:{args:["param:foamOriginMetersX",37],kind:"expr",op:"mod"}},{name:"foamRelZ",type:"f32",source:{args:["param:cameraZ","param:waterZ"],kind:"expr",op:"sub"}},{name:"foamTexelZ",type:"f32",source:{args:["param:foamRelZ",.5],kind:"expr",op:"div"}},{name:"foamCellZ",type:"f32",source:{args:["param:foamTexelZ"],kind:"expr",op:"floor"}},{name:"foamOriginZ",type:"i32",source:{args:["param:foamCellZ",512],kind:"expr",op:"sub"}},{name:"foamShiftZ",type:"i32",source:{kind:"delta",of:"param:foamOriginZ"}},{name:"foamOriginMetersZ",type:"f32",source:{args:["param:foamOriginZ",.5],kind:"expr",op:"mul"}},{name:"foamOriginMod0Z",type:"f32",source:{args:["param:foamOriginMetersZ",1600],kind:"expr",op:"mod"}},{name:"foamOriginMod1Z",type:"f32",source:{args:["param:foamOriginMetersZ",250],kind:"expr",op:"mod"}},{name:"foamOriginMod2Z",type:"f32",source:{args:["param:foamOriginMetersZ",37],kind:"expr",op:"mod"}}],resources:[{name:"h0_0",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"omega_0",kind:"buffer",elementType:"f32",count:4096,width:64},{name:"fftA_0",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"fftB_0",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"fftA2_0",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"fftB2_0",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"dispTex0",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"slopeTex0",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"h0_1",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"omega_1",kind:"buffer",elementType:"f32",count:4096,width:64},{name:"fftA_1",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"fftB_1",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"fftA2_1",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"fftB2_1",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"dispTex1",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"slopeTex1",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"h0_2",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"omega_2",kind:"buffer",elementType:"f32",count:4096,width:64},{name:"fftA_2",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"fftB_2",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"fftA2_2",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"fftB2_2",kind:"buffer",elementType:"vec4<f32>",count:4096,width:64},{name:"dispTex2",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"slopeTex2",kind:"texture",format:"rgba16float",width:64,height:64,mipLevels:7},{name:"foamA",kind:"buffer",elementType:"f32",count:1048576,width:1024},{name:"foamB",kind:"buffer",elementType:"f32",count:1048576,width:1024},{name:"foamTex",kind:"texture",format:"rgba16float",width:1024,height:1024,mipLevels:11}],passes:[{name:"c0.spectrum",type:"kernel",kernel:"ocean_spectrum",entry:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",target:{width:64,height:64,elioWidth:64},bindings:[{name:"h0",resource:"h0_0"},{name:"omega",resource:"omega_0"}],uniforms:[{name:"spec",values:{cascade:0,fetchKm:"param:fetch",gamma:"param:gamma",kMax:.12,kMin:.003926990816987242,loopPeriod:"param:loopPeriod",n:64,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:1600,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c0.evolve",type:"kernel",kernel:"ocean_evolve",entry:"ocean_evolve",target:{width:64,height:64,elioWidth:64},bindings:[{name:"h0",resource:"h0_0"},{name:"omega",resource:"omega_0"},{name:"outA",resource:"fftA_0"},{name:"outB",resource:"fftB_0"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:64,t:"param:phaseTime",tile:1600}}]},{name:"c0.fft.rows",type:"repeat",count:6,index:"s",swap:[["fftA_0","fftA2_0"],["fftB_0","fftB2_0"]],body:[{name:"A",type:"kernel",kernel:"fft_gather_64",entry:"fft_stage",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftA_0"},{name:"dst",resource:"fftA2_0"}],uniforms:[{name:"fft",values:{axis:0,n:64,stage:"param:s"}}]},{name:"B",type:"kernel",kernel:"fft_gather_64",entry:"fft_stage",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftB_0"},{name:"dst",resource:"fftB2_0"}],uniforms:[{name:"fft",values:{axis:0,n:64,stage:"param:s"}}]}]},{name:"c0.fft.cols",type:"repeat",count:6,index:"s",swap:[["fftA_0","fftA2_0"],["fftB_0","fftB2_0"]],body:[{name:"A",type:"kernel",kernel:"fft_gather_64",entry:"fft_stage",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftA_0"},{name:"dst",resource:"fftA2_0"}],uniforms:[{name:"fft",values:{axis:1,n:64,stage:"param:s"}}]},{name:"B",type:"kernel",kernel:"fft_gather_64",entry:"fft_stage",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftB_0"},{name:"dst",resource:"fftB2_0"}],uniforms:[{name:"fft",values:{axis:1,n:64,stage:"param:s"}}]}]},{name:"c0.assemble",type:"kernel",kernel:"ocean_assemble",entry:"ocean_assemble",target:{width:64,height:64,elioWidth:64},bindings:[{name:"fieldA",resource:"fftA_0"},{name:"fieldB",resource:"fftB_0"},{name:"disp",resource:"dispTex0"},{name:"slope",resource:"slopeTex0"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:64}}]},{name:"c0.dispTex0.mips",type:"mipmap",resource:"dispTex0"},{name:"c0.slopeTex0.mips",type:"mipmap",resource:"slopeTex0"},{name:"c1.spectrum",type:"kernel",kernel:"ocean_spectrum",entry:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",target:{width:64,height:64,elioWidth:64},bindings:[{name:"h0",resource:"h0_1"},{name:"omega",resource:"omega_1"}],uniforms:[{name:"spec",values:{cascade:1,fetchKm:"param:fetch",gamma:"param:gamma",kMax:.8,kMin:.12,loopPeriod:"param:loopPeriod",n:64,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:250,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c1.evolve",type:"kernel",kernel:"ocean_evolve",entry:"ocean_evolve",target:{width:64,height:64,elioWidth:64},bindings:[{name:"h0",resource:"h0_1"},{name:"omega",resource:"omega_1"},{name:"outA",resource:"fftA_1"},{name:"outB",resource:"fftB_1"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:64,t:"param:phaseTime",tile:250}}]},{name:"c1.fft.rows",type:"repeat",count:6,index:"s",swap:[["fftA_1","fftA2_1"],["fftB_1","fftB2_1"]],body:[{name:"A",type:"kernel",kernel:"fft_gather_64",entry:"fft_stage",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftA_1"},{name:"dst",resource:"fftA2_1"}],uniforms:[{name:"fft",values:{axis:0,n:64,stage:"param:s"}}]},{name:"B",type:"kernel",kernel:"fft_gather_64",entry:"fft_stage",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftB_1"},{name:"dst",resource:"fftB2_1"}],uniforms:[{name:"fft",values:{axis:0,n:64,stage:"param:s"}}]}]},{name:"c1.fft.cols",type:"repeat",count:6,index:"s",swap:[["fftA_1","fftA2_1"],["fftB_1","fftB2_1"]],body:[{name:"A",type:"kernel",kernel:"fft_gather_64",entry:"fft_stage",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftA_1"},{name:"dst",resource:"fftA2_1"}],uniforms:[{name:"fft",values:{axis:1,n:64,stage:"param:s"}}]},{name:"B",type:"kernel",kernel:"fft_gather_64",entry:"fft_stage",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftB_1"},{name:"dst",resource:"fftB2_1"}],uniforms:[{name:"fft",values:{axis:1,n:64,stage:"param:s"}}]}]},{name:"c1.assemble",type:"kernel",kernel:"ocean_assemble",entry:"ocean_assemble",target:{width:64,height:64,elioWidth:64},bindings:[{name:"fieldA",resource:"fftA_1"},{name:"fieldB",resource:"fftB_1"},{name:"disp",resource:"dispTex1"},{name:"slope",resource:"slopeTex1"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:64}}]},{name:"c1.dispTex1.mips",type:"mipmap",resource:"dispTex1"},{name:"c1.slopeTex1.mips",type:"mipmap",resource:"slopeTex1"},{name:"c2.spectrum",type:"kernel",kernel:"ocean_spectrum",entry:"ocean_spectrum",when:"changed:windSpeed,fetch,gamma,spread,swell,windDir,swellDir,seed,loopPeriod",target:{width:64,height:64,elioWidth:64},bindings:[{name:"h0",resource:"h0_2"},{name:"omega",resource:"omega_2"}],uniforms:[{name:"spec",values:{cascade:2,fetchKm:"param:fetch",gamma:"param:gamma",kMax:5.4,kMin:.8,loopPeriod:"param:loopPeriod",n:64,seed:"param:seed",spread:"param:spread",swell:"param:swell",swellDir:"param:swellDir",tile:37,windDir:"param:windDir",windSpeed:"param:windSpeed"}}]},{name:"c2.evolve",type:"kernel",kernel:"ocean_evolve",entry:"ocean_evolve",target:{width:64,height:64,elioWidth:64},bindings:[{name:"h0",resource:"h0_2"},{name:"omega",resource:"omega_2"},{name:"outA",resource:"fftA_2"},{name:"outB",resource:"fftB_2"}],uniforms:[{name:"ev",values:{loopPeriod:"param:loopPeriod",n:64,t:"param:phaseTime",tile:37}}]},{name:"c2.fft.rows",type:"repeat",count:6,index:"s",swap:[["fftA_2","fftA2_2"],["fftB_2","fftB2_2"]],body:[{name:"A",type:"kernel",kernel:"fft_gather_64",entry:"fft_stage",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftA_2"},{name:"dst",resource:"fftA2_2"}],uniforms:[{name:"fft",values:{axis:0,n:64,stage:"param:s"}}]},{name:"B",type:"kernel",kernel:"fft_gather_64",entry:"fft_stage",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftB_2"},{name:"dst",resource:"fftB2_2"}],uniforms:[{name:"fft",values:{axis:0,n:64,stage:"param:s"}}]}]},{name:"c2.fft.cols",type:"repeat",count:6,index:"s",swap:[["fftA_2","fftA2_2"],["fftB_2","fftB2_2"]],body:[{name:"A",type:"kernel",kernel:"fft_gather_64",entry:"fft_stage",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftA_2"},{name:"dst",resource:"fftA2_2"}],uniforms:[{name:"fft",values:{axis:1,n:64,stage:"param:s"}}]},{name:"B",type:"kernel",kernel:"fft_gather_64",entry:"fft_stage",target:{width:64,height:64,elioWidth:64},bindings:[{name:"src",resource:"fftB_2"},{name:"dst",resource:"fftB2_2"}],uniforms:[{name:"fft",values:{axis:1,n:64,stage:"param:s"}}]}]},{name:"c2.assemble",type:"kernel",kernel:"ocean_assemble",entry:"ocean_assemble",target:{width:64,height:64,elioWidth:64},bindings:[{name:"fieldA",resource:"fftA_2"},{name:"fieldB",resource:"fftB_2"},{name:"disp",resource:"dispTex2"},{name:"slope",resource:"slopeTex2"}],uniforms:[{name:"assem",values:{lambda:"param:lambda",n:64}}]},{name:"c2.dispTex2.mips",type:"mipmap",resource:"dispTex2"},{name:"c2.slopeTex2.mips",type:"mipmap",resource:"slopeTex2"},{name:"foam",type:"kernel",kernel:"ocean_foam",entry:"ocean_foam",target:{width:1024,height:1024,elioWidth:1024},bindings:[{name:"foamIn",resource:"foamA"},{name:"fieldB0",resource:"fftB_0"},{name:"fieldB1",resource:"fftB_1"},{name:"fieldB2",resource:"fftB_2"},{name:"fieldB3",resource:"fftB_2"},{name:"foamOut",resource:"foamB"},{name:"foamTex",resource:"foamTex"}],uniforms:[{name:"foam",values:{cascades:3,cell:.5,dt:"param:dt",gain:"param:foamGain",lambda:"param:lambda",n0:64,n1:64,n2:64,n3:64,omX0:"param:foamOriginMod0X",omX1:"param:foamOriginMod1X",omX2:"param:foamOriginMod2X",omX3:"param:foamOriginMod2X",omZ0:"param:foamOriginMod0Z",omZ1:"param:foamOriginMod1Z",omZ2:"param:foamOriginMod2Z",omZ3:"param:foamOriginMod2Z",originX:"param:foamOriginX",originZ:"param:foamOriginZ",shiftX:"param:foamShiftX",shiftZ:"param:foamShiftZ",size0:1600,size1:250,size2:37,size3:37,tau:"param:foamTau",threshold:"param:foamThreshold",width:1024}}]},{name:"foamTex.mips",type:"mipmap",resource:"foamTex"}],frameSwaps:[["foamA","foamB"]],exports:{disp0:"dispTex0",disp1:"dispTex1",disp2:"dispTex2",foam:"foamTex",slope0:"slopeTex0",slope1:"slopeTex1",slope2:"slopeTex2"},exportParams:["waterModX0","waterModZ0","waterModX1","waterModZ1","waterModX2","waterModZ2"]}}})})(),(function(){function q(n){return n.replace(/_([a-z0-9])/g,function(_,O){return O.toUpperCase()})}function W(n){return typeof n=="boolean"?n?1:0:Number(n)}function S(n,_,O){if(typeof n=="string"){if(n.indexOf("param:")!==0)throw new Error("elio-graph: "+O+": bad value "+n);const F=n.slice(6);if(!(F in _))throw new Error("elio-graph: parameter "+F+" was not supplied");return W(_[F])}return W(n)}function C(n,_,O,F){const B=new ArrayBuffer(Math.max(16,Math.ceil(n.size/16)*16)),Z=new DataView(B),h={};for(const b of n.fields){let k=_[b.name];if(k===void 0){if(!/^_pad/.test(b.name))throw new Error("elio-graph: "+F+": uniform "+n.struct+" lacks a value for "+b.name);k=0}h[b.name]=!0;const R=S(k,O,F);if(b.type==="u32")Z.setUint32(b.offset,R>>>0,!0);else if(b.type==="i32")Z.setInt32(b.offset,R|0,!0);else if(b.type==="f32")Z.setFloat32(b.offset,R,!0);else throw new Error("elio-graph: "+F+": unsupported uniform type "+b.type)}for(const b of Object.keys(_))if(!h[b])throw new Error("elio-graph: "+F+": uniform "+n.struct+" has no field "+b);return B}function d(n){for(const _ of Object.keys(n))if(typeof n[_]=="string")return!1;return!0}const e={time:1,dt:1,cameraX:1,cameraY:1,cameraZ:1},K={add:function(n,_){return n+_},sub:function(n,_){return n-_},mul:function(n,_){return n*_},div:function(n,_){return _===0?0:n/_},mod:function(n,_){return _===0?0:n-Math.floor(n/_)*_},floor:function(n){return Math.floor(n)},min:function(n,_){return Math.min(n,_)},max:function(n,_){return Math.max(n,_)}};function H(n,_){return/\[\]$/.test(n)?Float32Array.from(_||[]):(_=Number(_),Number.isFinite(_)?n==="bool"?_?1:0:n==="u32"?Math.max(0,Math.floor(_))>>>0:n==="i32"?Math.trunc(_)|0:_:0)}function f(n){const _={},O={},F={},B={},Z={};let h=!0,b=null;const k=function(M){return M.tween>0?b===null?M.tween:b:0};for(const M of n){const L=M.source||{kind:"value",value:0};L.kind==="value"&&(Z[M.name]=H(M.type,/\[\]$/.test(M.type)?L.value:L.value||0))}const R=function(M){if(typeof M=="number")return M;if(typeof M=="string"&&M.indexOf("param:")===0){const L=M.slice(6);return L in _?_[L]:0}throw new Error("elio-graph: bad argument "+M)},U={current:_,changed:{},setTween:function(M){b=Number.isFinite(M)&&M>=0?M:null},set:function(M,L){const T=n.find(function(l){return l.name===M});if(!T||T.source&&T.source.kind!=="value")throw new Error("elio-graph: "+M+" is not a value parameter");const X=H(T.type,L);if(X!==Z[M]){if(typeof X!="number"){Z[M]=X;return}Z[M]=X,k(T)>0&&!h?F[M]={from:_[M],start:null}:delete F[M]}},evaluate:function(M){const L=Number(M.time)||0,T=Number(M.dt)||0;for(const X of n){const l=X.source||{kind:"value",value:0};let t=0;if(l.kind==="value"){t=Z[X.name];const m=F[X.name];if(m){m.start===null&&(m.start=L);const D=Math.min(1,Math.max(0,(L-m.start)/k(X)));D>=1?delete F[X.name]:t=m.from+(t-m.from)*D}}else if(l.kind==="frame"){if(!e[l.name])throw new Error("elio-graph: unknown frame source "+l.name);t=l.name==="time"?L:l.name==="dt"?T:Number(M[l.name])||0}else if(l.kind==="expr"){const m=K[l.op];if(!m)throw new Error("elio-graph: unknown op "+l.op);t=m(R(l.args[0]),l.args.length>1?R(l.args[1]):0)}else if(l.kind==="delta"){const m=String(l.of).replace(/^param:/,"");t=h?0:(_[m]||0)-(O[m]||0)}else if(l.kind==="accumulate")B[X.name]=(B[X.name]||0)+R(l.of)*T,t=B[X.name];else throw new Error("elio-graph: unknown parameter source "+l.kind);_[X.name]=/\[\]$/.test(X.type)?Z[X.name]:H(X.type,t)}for(const X of n)U.changed[X.name]=h||_[X.name]!==O[X.name];for(const X of n)O[X.name]=_[X.name];h=!1}};return U}function s(n,_,O){if(!n||n==="always")return!0;if(n.indexOf("param:")===0)return!!_[n.slice(6)];if(n.indexOf("changed:")===0)return n.slice(8).split(",").some(function(F){return!!O[F.trim()]});throw new Error("elio-graph: bad when "+n)}function w(n,_){const O=_+(n.offset||0);return n.shr?Math.max(n.shr>>O,1):O}function E(n){const _={},O={},F={};for(const k of n.resources)_[k.name]=k;for(const k of n.params)F[k.name]=!0;const B=function(k){return k in O?O[k]:k},Z=function(k,R,U,M){const L=[];for(const T of k){if(T.type!=="repeat"){L.push(h(T,R,U,M));continue}const X=U+T.name;if(!(T.count>=1))throw new Error("elio-graph: repeat "+X+": count "+T.count+", want at least 1");if(!T.index||F[T.index])throw new Error("elio-graph: repeat "+X+": index "+T.index+" is empty or names a declared param");if(!T.body||!T.body.length)throw new Error("elio-graph: repeat "+X+": body is empty");if(T.index in R)throw new Error("elio-graph: repeat "+X+": index "+T.index+" is already in use by an outer repeat");for(const t of T.swap||[])if(!_[t[0]]||!_[t[1]])throw new Error("elio-graph: repeat "+X+": swap names an unknown resource");const l=T.when&&T.when!=="always"?T.when:M;for(let t=0;t<T.count;t++){const m=Object.assign({},R,{[T.index]:t});for(const D of Z(T.body,m,X+"["+t+"].",l))L.push(D);for(const D of T.swap||[]){const j=B(D[0]),N=B(D[1]);O[D[0]]=N,O[D[1]]=j}}}return L},h=function(k,R,U,M){const L=Object.assign({},k,{name:U+k.name});return M&&(!k.when||k.when==="always")&&(L.when=M),k.resource&&(L.resource=B(k.resource)),k.bindings&&(L.bindings=k.bindings.map(function(T){const X=Object.assign({},T,{resource:B(T.resource)});if(T.levelAt){if(!(T.levelAt.index in R))throw new Error("elio-graph: pass "+L.name+": index "+T.levelAt.index+" is not in scope");X.level=w(T.levelAt,R[T.levelAt.index]),delete X.levelAt}return X})),k.uniforms&&(L.uniforms=k.uniforms.map(function(T){const X={};for(const l of Object.keys(T.values)){let t=T.values[l];if(t&&typeof t=="object"){if(!(t.index in R))throw new Error("elio-graph: pass "+L.name+": index "+t.index+" is not in scope");t=w(t,R[t.index])}else typeof t=="string"&&t.indexOf("param:")===0&&t.slice(6)in R&&(t=R[t.slice(6)]);X[l]=t}return Object.assign({},T,{values:X})})),L},b=Z(n.passes,{},"","");for(const k of Object.keys(O))if(O[k]!==k)throw new Error("elio-graph: swaps are not restored: "+k+" still refers to "+O[k]+" at the end of the passes");return b}function V(n,_,O){if(!n||n.version!==1)throw new Error("elio-graph: unsupported graph version");if(n.backend!==O)throw new Error("elio-graph: graph is for "+n.backend+", not "+O);const F={};for(const h of n.resources)F[h.name]=h;const B={};for(const h of n.params)B[h.name]=!0;const Z=function(h){if(typeof h=="string"&&h.indexOf("param:")===0&&!B[h.slice(6)])throw new Error("elio-graph: undeclared parameter "+h.slice(6))};for(const h of E(n)){const b=h.when||"always";if(b.indexOf("param:")===0)Z(b);else if(b.indexOf("changed:")===0){for(const k of b.slice(8).split(","))if(!B[k.trim()])throw new Error("elio-graph: undeclared parameter "+k.trim())}else if(b!=="always")throw new Error("elio-graph: pass "+h.name+": bad when "+b);if(h.type==="mipmap"){if(!F[h.resource])throw new Error("elio-graph: pass "+h.name+": unknown resource "+h.resource);continue}if(!_.kernels[q(h.kernel)])throw new Error("elio-graph: pass "+h.name+": the module has no kernel "+h.kernel);for(const k of h.bindings)if(!F[k.resource])throw new Error("elio-graph: pass "+h.name+": unknown resource "+k.resource);for(const k of h.uniforms||[])for(const R of Object.keys(k.values))Z(k.values[R])}for(const h of n.exportParams||[])if(!B[h])throw new Error("elio-graph: exportParams names undeclared parameter "+h);for(const h of n.resources)if(h.feed&&!B[h.feed])throw new Error("elio-graph: resource "+h.name+" is fed by undeclared parameter "+h.feed);for(const h of n.frameSwaps||[])for(const b of h)if(!F[b])throw new Error("elio-graph: frameSwaps names unknown resource "+b);for(const h of Object.keys(n.exports||{})){const b=F[n.exports[h]];if(!b||b.kind!=="texture"&&O!=="webgpu")throw new Error("elio-graph: export "+h+" must name a texture resource")}}function Y(n){const _=n.backend,O=n.module,F=n.host||{},B={graph:null,backend:_,ready:!1,failed:null,disposed:!1,buildCount:0,run:L,setValue:M,setTween:R,value:U,hasParam:k,params:null,exportParams:T,export:X,dispose:t,fail:l};let Z=null,h=null;const b=function(m){if(!B.failed){if(B.failed=m&&m.fallback||"error",B.error=m,Z){try{Z.dispose()}catch{}Z=null}B.ready=!1,n.onFail&&n.onFail(B.failed,m),!(m&&m.fallback)&&typeof console<"u"&&console.warn&&console.warn("elio-graph: graph disabled: "+(m&&m.message?m.message:String(m)))}};try{const m=n.graph||O.graphs&&O.graphs[_+"/"+(n.tier||"high")];if(!m)throw Object.assign(new Error("elio-graph: no graph for "+_+"/"+n.tier),{fallback:"no-graph"});V(m,O,_);const D=window.__gosx_elio_graph.executors[_];if(!D)throw Object.assign(new Error("elio-graph: no executor for "+_),{fallback:"no-executor"});B.graph=m,h=B.params=f(m.params),Z=D({steps:E(m).map(function(j){return{pass:j}}),graph:m,module:O,host:F,pack:C,isStatic:d,kernelKey:q,holds:s,label:n.label||m.name}),B.buildCount=1,B.ready=Z.isReady()}catch(m){b(m)}function k(m){return!!(B.graph&&B.graph.params.some(function(D){return D.name===m}))}function R(m){h&&h.setTween(m)}function U(m){return h&&m in h.current?h.current[m]:0}function M(m,D){if(!(!h||B.failed))try{h.set(m,D)}catch(j){b(j)}}function L(m){if(!Z||B.failed||B.disposed)return!1;try{if(B.ready=Z.isReady(),!B.ready)return!1;const D=m||{};return h.evaluate(D),Z.run(h.current,D,h.changed)!==!1}catch(D){return b(D),!1}}function T(){const m={};if(h&&B.graph)for(const D of B.graph.exportParams||[])m[D]=h.current[D];return m}function X(m){return Z&&!B.failed?Z.export(m):null}function l(m){b({message:m,fallback:m})}function t(){if(!B.disposed&&(B.disposed=!0,B.ready=!1,Z)){try{Z.dispose()}catch{}Z=null}}return B}window.__gosx_elio_graph={create:Y,expand:E,pack:C,kernelKey:q,check:V,holds:s,createParams:f,executors:{}}})(),(function(){const q=`#version 300 es
void main() { vec2 p = vec2(float((gl_VertexID << 1) & 2), float(gl_VertexID & 2)); gl_Position = vec4(p * 2. - 1., 0., 1.); }`;function W(d){return{draw:d.getParameter(d.DRAW_FRAMEBUFFER_BINDING),read:d.getParameter(d.READ_FRAMEBUFFER_BINDING),viewport:d.getParameter(d.VIEWPORT),program:d.getParameter(d.CURRENT_PROGRAM),vao:d.getParameter(d.VERTEX_ARRAY_BINDING),active:d.getParameter(d.ACTIVE_TEXTURE),blend:d.isEnabled(d.BLEND),depth:d.isEnabled(d.DEPTH_TEST),cull:d.isEnabled(d.CULL_FACE),scissor:d.isEnabled(d.SCISSOR_TEST),colorMask:d.getParameter(d.COLOR_WRITEMASK),depthMask:d.getParameter(d.DEPTH_WRITEMASK)}}function S(d,e){d.bindBuffer(d.UNIFORM_BUFFER,null),d.bindBufferBase(d.UNIFORM_BUFFER,0,null),d.bindBuffer(d.PIXEL_PACK_BUFFER,null),d.bindBuffer(d.PIXEL_UNPACK_BUFFER,null),d.bindFramebuffer(d.DRAW_FRAMEBUFFER,e.draw),d.bindFramebuffer(d.READ_FRAMEBUFFER,e.read),d.viewport(e.viewport[0],e.viewport[1],e.viewport[2],e.viewport[3]),d.useProgram(e.program),d.bindVertexArray(e.vao),d.activeTexture(e.active);const K=function(H,f){f?d.enable(H):d.disable(H)};K(d.BLEND,e.blend),K(d.DEPTH_TEST,e.depth),K(d.CULL_FACE,e.cull),K(d.SCISSOR_TEST,e.scissor),d.colorMask(e.colorMask[0],e.colorMask[1],e.colorMask[2],e.colorMask[3]),d.depthMask(e.depthMask)}function C(d){const e=d.host.gl,K=d.graph,H=d.module,f=H.kernels,s=d.host.firstUnit||4;if(!(d.host.caps?d.host.caps(e):{}).colorBufferFloat)throw Object.assign(new Error("EXT_color_buffer_float is not available"),{fallback:"EXT_color_buffer_float"});const E=function(r,a){const o=e.getParameter(r);return typeof o=="number"?o:a};if(E(e.MAX_DRAW_BUFFERS,0)<2||E(e.MAX_TEXTURE_IMAGE_UNITS,0)<s+5)throw Object.assign(new Error("too few texture units or draw buffers"),{fallback:"texture-units"});for(const r of d.steps){const a=r.pass;if(!a||a.type!=="kernel")continue;const o=f[d.kernelKey(a.kernel)];if(!o.glsl||!o.gatherLayout)throw Object.assign(new Error("kernel "+a.kernel+" has no GLSL gather form"),{fallback:"kernel:"+a.kernel})}const V=[],Y=[],n=[],_=[],O=Object.create(null),F={};for(const r of Object.keys(K.exports||{}))F[K.exports[r]]=!0;const B=e.createVertexArray(),Z={split:!1,disposed:!1};let h=null;const b=Object.create(null),k=new Map,R=new Map;let U=0;const M={rgba16float:e.RGBA16F,rgba32float:e.RGBA32F,r32float:e.R32F};function L(r,a,o,c,z){const v=e.createTexture();return V.push(v),e.activeTexture(e.TEXTURE0+s),e.bindTexture(e.TEXTURE_2D,v),e.texStorage2D(e.TEXTURE_2D,c,o,r,a),e.texParameteri(e.TEXTURE_2D,e.TEXTURE_MIN_FILTER,z?c>1?e.LINEAR_MIPMAP_LINEAR:e.LINEAR:e.NEAREST),e.texParameteri(e.TEXTURE_2D,e.TEXTURE_MAG_FILTER,z?e.LINEAR:e.NEAREST),e.texParameteri(e.TEXTURE_2D,e.TEXTURE_WRAP_S,z?e.REPEAT:e.CLAMP_TO_EDGE),e.texParameteri(e.TEXTURE_2D,e.TEXTURE_WRAP_T,z?e.REPEAT:e.CLAMP_TO_EDGE),v}function T(r,a){const o=e.createShader(r);if(_.push(o),e.shaderSource(o,a),e.compileShader(o),!e.getShaderParameter(o,e.COMPILE_STATUS))throw new Error("elio-graph: shader did not compile: "+e.getShaderInfoLog(o));return o}function X(r){if(O[r])return O[r];const a=f[d.kernelKey(r)];h||(h=T(e.VERTEX_SHADER,q));const o=T(e.FRAGMENT_SHADER,a.glsl),c=e.createProgram();e.attachShader(c,h),e.attachShader(c,o),e.linkProgram(c);const z=a.gatherLayout,v=O[r]={handle:c,layout:z,kernel:a};if(!e.getProgramParameter(c,e.LINK_STATUS))throw new Error("elio-graph: program "+r+" did not link: "+e.getProgramInfoLog(c));return e.useProgram(c),v.width=e.getUniformLocation(c,"elio_width"),v.lens=z.inputs.map(function(y){return e.getUniformLocation(c,y.length)}),z.inputs.forEach(function(y,P){e.uniform1i(e.getUniformLocation(c,y.sampler),s+P)}),z.uniforms.forEach(function(y,P){e.uniformBlockBinding(c,e.getUniformBlockIndex(c,y.block),P)}),v}function l(r,a){const c=(r.uniformsStd140||r.uniforms).find(function(z){return z.name===a});if(!c)throw new Error("elio-graph: kernel has no uniform block "+a);return c}function t(){for(const r of K.resources){let a;if(r.kind==="buffer"){const o=Math.ceil(r.count/r.width);a={name:r.name,id:++U,kind:"buffer",version:0,w:r.width,h:o,levels:1,tex:L(r.width,o,e.RGBA32F,1,!1)}}else{const o=r.mipLevels||1,c=M[r.format];if(!c)throw new Error("elio-graph: unsupported format "+r.format);const z=r.format==="rgba16float"&&(o>1||!!F[r.name]);a={name:r.name,id:++U,kind:"texture",version:0,w:r.width,h:r.height,levels:o,tex:L(r.width,r.height,c,o,z)}}b[r.name]=a}}function m(r){const a=e.createFramebuffer();Y.push(a),e.bindFramebuffer(e.FRAMEBUFFER,a);const o=[];for(const c of r){for(e.framebufferTexture2D(e.FRAMEBUFFER,e.COLOR_ATTACHMENT0+c.location,e.TEXTURE_2D,c.res.tex,c.level);o.length<=c.location;)o.push(e.NONE);o[c.location]=e.COLOR_ATTACHMENT0+c.location}return e.drawBuffers(o),{fbo:a,complete:e.checkFramebufferStatus(e.FRAMEBUFFER)===e.FRAMEBUFFER_COMPLETE}}function D(r){const a=r.map(function(v){return v.res.id+"@"+v.level+":"+v.location}).join(","),o=k.get(a+(Z.split?"s":""));if(o)return o;let c;const z=function(v){const y=m([v]);if(!y.complete)throw new Error("elio-graph: framebuffer incomplete");return y};if(r.length===1)c=[z(r[0])];else{const v=Z.split?null:m(r);v&&v.complete?c=[v]:(Z.split=!0,c=r.map(z))}return k.set(a+(Z.split?"s":""),c),c}function j(){if(!Z.disposed){Z.disposed=!0;for(const r of V)e.deleteTexture(r);for(const r of Y)e.deleteFramebuffer(r);for(const r of n)e.deleteBuffer(r);for(const r of Object.keys(O))e.deleteProgram(O[r].handle);for(const r of _)e.deleteShader(r);e.deleteVertexArray(B),V.length=Y.length=n.length=0,k.clear(),R.clear()}}let N=[];const i=W(e);try{N=d.steps.map(function(r){const a=r.pass;if(a.type==="mipmap")return{pass:a};const o=X(a.kernel),c=o.layout,z=c.inputs.map(function(P){const A=a.bindings.find(function(J){return J.name===P.name});if(!A)throw new Error("elio-graph: pass "+a.name+" does not bind input "+P.name);return A}),v=c.outputs.map(function(P){const A=a.bindings.find(function(J){return J.name===P.name});if(!A)throw new Error("elio-graph: pass "+a.name+" does not bind output "+P.name);return{binding:A,location:P.location}}),y=(a.uniforms||[]).map(function(P){const A=c.uniforms.findIndex(function(I){return I.name===P.name});if(A<0)throw new Error("elio-graph: pass "+a.name+" has no uniform block "+P.name);const J=l(o.kernel,P.name),G=e.createBuffer(),ee=Math.max(16,Math.ceil(J.size/16)*16);return n.push(G),e.bindBuffer(e.UNIFORM_BUFFER,G),e.bufferData(e.UNIFORM_BUFFER,ee,e.DYNAMIC_DRAW),{index:A,layout:J,values:P.values,buffer:G,size:ee,written:!1,constant:d.isStatic(P.values)}});return{pass:a,p:o,inputs:z,outputs:v,blocks:y}}),t()}catch(r){throw j(),r}finally{S(e,i)}function p(r,a){const o=r.w*r.h;if(o%a)throw new Error("elio-graph: buffer "+r.name+" of "+o+" elements cannot be viewed "+a+" wide");const c=r.id+"@"+a;let z=R.get(c);if(!z){const v=e.createBuffer();n.push(v),e.bindBuffer(e.PIXEL_PACK_BUFFER,v),e.bufferData(e.PIXEL_PACK_BUFFER,o*16,e.STREAM_COPY),e.bindBuffer(e.PIXEL_PACK_BUFFER,null),z={name:r.name,w:a,h:o/a,tex:L(a,o/a,e.RGBA32F,1,!1),pbo:v,version:-1},R.set(c,z)}return z.version!==r.version&&(e.bindFramebuffer(e.READ_FRAMEBUFFER,D([{res:r,level:0,location:0}])[0].fbo),e.bindBuffer(e.PIXEL_PACK_BUFFER,z.pbo),e.readPixels(0,0,r.w,r.h,e.RGBA,e.FLOAT,0),e.bindBuffer(e.PIXEL_PACK_BUFFER,null),e.bindBuffer(e.PIXEL_UNPACK_BUFFER,z.pbo),e.activeTexture(e.TEXTURE0+s),e.bindTexture(e.TEXTURE_2D,z.tex),e.texSubImage2D(e.TEXTURE_2D,0,0,0,z.w,z.h,e.RGBA,e.FLOAT,0),e.bindBuffer(e.PIXEL_UNPACK_BUFFER,null),z.version=r.version),z}function u(r){for(const a of r){const o=b[a[0]];b[a[0]]=b[a[1]],b[a[1]]=o}}function g(r){for(const a of K.resources){if(!a.feed)continue;const o=b[a.name],c=new Float32Array(o.w*o.h*4);c.set(r[a.feed].subarray(0,c.length)),e.activeTexture(e.TEXTURE0+s),e.bindTexture(e.TEXTURE_2D,o.tex),e.texSubImage2D(e.TEXTURE_2D,0,0,0,o.w,o.h,e.RGBA,e.FLOAT,c)}}function x(r,a){const o=r.pass,c=r.p,z=o.target,v=r.outputs.map(function(A){return{res:b[A.binding.resource],level:A.binding.level||0,location:A.location}}),y=r.inputs.map(function(A){return b[A.resource]}),P=y.map(function(A){return A.kind==="buffer"&&A.w!==z.elioWidth?p(A,z.elioWidth):A});r.inputs.forEach(function(A,J){for(const G of v)if(G.res===y[J]&&G.level===(A.level||0))throw new Error("elio-graph: pass "+o.name+" reads "+G.res.name+", which it writes")}),e.useProgram(c.handle),e.uniform1ui(c.width,z.elioWidth),P.forEach(function(A,J){e.activeTexture(e.TEXTURE0+s+J),e.bindTexture(e.TEXTURE_2D,A.tex),c.lens[J]&&e.uniform1ui(c.lens[J],A.w*A.h)});for(const A of r.blocks)(!A.constant||!A.written)&&(e.bindBuffer(e.UNIFORM_BUFFER,A.buffer),e.bufferSubData(e.UNIFORM_BUFFER,0,new Uint8Array(d.pack(A.layout,A.values,a,o.name))),A.written=!0),e.bindBufferRange(e.UNIFORM_BUFFER,A.index,A.buffer,0,A.size);for(const A of v)A.res.version++;e.viewport(0,0,z.width,z.height);for(const A of D(v))e.bindFramebuffer(e.FRAMEBUFFER,A.fbo),e.drawArrays(e.TRIANGLES,0,3)}return{isReady:function(){return!Z.disposed},run:function(r,a,o){if(e.isContextLost&&e.isContextLost())return!1;const c=W(e);try{e.disable(e.BLEND),e.disable(e.DEPTH_TEST),e.disable(e.CULL_FACE),e.disable(e.SCISSOR_TEST),e.colorMask(!0,!0,!0,!0),e.bindVertexArray(B),g(r);for(const v of N)d.holds(v.pass.when,r,o)&&(v.pass.type==="mipmap"?(e.activeTexture(e.TEXTURE0+s),e.bindTexture(e.TEXTURE_2D,b[v.pass.resource].tex),e.generateMipmap(e.TEXTURE_2D)):x(v,r));u(K.frameSwaps||[])}finally{S(e,c)}const z=d.host.textureCache;return z&&(z._sceneTextureEpoch=(z._sceneTextureEpoch||0)+1),!0},export:function(r){const a=b[K.exports[r]];return a?{tex:a.tex,width:a.w,height:a.h,levels:a.levels}:null},split:function(){return Z.split},dispose:j}}window.__gosx_elio_graph.executors.webgl2=C})(),(function(){const q={f32:4,"vec2<f32>":8,"vec4<f32>":16};function W(S){const C=S.host.device,d=S.graph,e=S.module.kernels,K=S.host;if(!C)throw Object.assign(new Error("no device"),{fallback:"no-device"});const H="gosx-elio-"+S.label+"-",f=[],s=[],w=Object.create(null),E=Object.create(null),V=Object.create(null),Y=new Map,n=new Map,_={disposed:!1};let O=0;const F={};for(const l of Object.keys(d.exports||{}))F[d.exports[l]]=!0;const B=C.queue,Z=GPUBufferUsage.STORAGE|GPUBufferUsage.COPY_DST|GPUBufferUsage.COPY_SRC,h=GPUBufferUsage.UNIFORM|GPUBufferUsage.COPY_DST;for(const l of d.resources)if(l.kind==="buffer"){const t=Math.max(16,Math.ceil(l.count*(q[l.elementType]||16)/4)*4),m=C.createBuffer({label:H+l.name,size:t,usage:Z});f.push(m),w[l.name]={name:l.name,id:++O,buffer:m}}else{const t=C.createTexture({label:H+l.name,size:[l.width,l.height,1],format:l.format,mipLevelCount:l.mipLevels||1,usage:GPUTextureUsage.STORAGE_BINDING|GPUTextureUsage.TEXTURE_BINDING});s.push(t),w[l.name]={name:l.name,id:++O,texture:t,w:l.width,h:l.height,levels:l.mipLevels||1,format:l.format}}function b(l){return l.map(function(t){const m={binding:t.binding,visibility:GPUShaderStage.COMPUTE};return t.kind==="uniform"?m.buffer={type:"uniform"}:t.kind==="storage-read"?m.buffer={type:"read-only-storage"}:t.kind==="storage-rw"?m.buffer={type:"storage"}:t.kind==="sampled-texture"||t.kind==="texture"?m.texture={sampleType:"unfilterable-float",viewDimension:"2d"}:m.storageTexture={access:"write-only",format:t.format,viewDimension:"2d"},m})}for(const l of S.steps){const t=l.pass;if(!t)continue;if(t.type!=="kernel")throw new Error("elio-graph: pass "+t.name+": "+t.type+" passes exist only on webgl2");const m=t.kernel+"/"+t.entry;if(E[m])continue;const D=e[S.kernelKey(t.kernel)],j=V[t.kernel]||(V[t.kernel]=C.createBindGroupLayout({label:H+t.kernel,entries:b(D.bindings)})),N=C.createShaderModule({label:H+t.kernel,code:D.wgsl}),i=K.create(C,"compute",{label:H+t.kernel,layout:C.createPipelineLayout({bindGroupLayouts:[j]}),compute:{module:N,entryPoint:t.entry}});E[m]={handle:i,layout:j,kernel:D}}function k(l){const t=K.validation&&K.validation.get(l.handle);return!t||t.ready===!0?t&&t.pipeline||l.handle:null}let R=0;const U=S.steps.map(function(l){const t=l.pass,m=E[t.kernel+"/"+t.entry],D=(t.uniforms||[]).map(function(j){const N=m.kernel.uniforms.find(function(u){return u.name===j.name});if(!N)throw new Error("elio-graph: pass "+t.name+" has no uniform block "+j.name);const i=Math.max(16,Math.ceil(N.size/16)*16),p=C.createBuffer({label:H+t.name+"-"+j.name,size:i,usage:h});return f.push(p),{name:j.name,layout:N,values:j.values,buffer:p,constant:S.isStatic(j.values),written:!1}});return{pass:t,p:m,blocks:D,id:++R}});function M(l){const t=l.pass,m=[],D=[t.name,l.id];for(const i of l.p.kernel.bindings){let p;if(i.kind==="uniform")p={buffer:l.blocks.find(function(u){return u.name===i.name}).buffer};else{const u=t.bindings.find(function(x){return x.name===i.name});if(!u)throw new Error("elio-graph: pass "+t.name+" does not bind "+i.name);const g=w[u.resource];D.push(g.id+"@"+(u.level||0)),i.kind==="storage-read"||i.kind==="storage-rw"?p={buffer:g.buffer}:p=g.texture.createView({baseMipLevel:u.level||0,mipLevelCount:1})}m.push({binding:i.binding,resource:p})}const j=D.join("|");let N=Y.get(j);return N||(N=C.createBindGroup({label:H+t.name,layout:l.p.layout,entries:m}),Y.set(j,N)),N}function L(l){for(const t of l){const m=w[t[0]];w[t[0]]=w[t[1]],w[t[1]]=m}}const T=C.lost&&C.lost.then?C.lost:null;T&&T.then(function(){X()},function(){X()});function X(){if(!_.disposed){_.disposed=!0;for(const l of f)l.destroy();for(const l of s)l.destroy();f.length=s.length=0,Y.clear(),n.clear()}}return{isReady:function(){if(_.disposed)return!1;for(const l of Object.keys(E))if(!k(E[l]))return!1;return!0},run:function(l,t,m){const D=t.encoder;if(!D)throw new Error("elio-graph: a webgpu frame needs frame.encoder");for(const j of d.resources)j.feed&&B.writeBuffer(w[j.name].buffer,0,l[j.feed]);for(const j of U){const N=j.pass;if(!S.holds(N.when,l,m))continue;for(const p of j.blocks)p.constant&&p.written||(B.writeBuffer(p.buffer,0,new Uint8Array(S.pack(p.layout,p.values,l,N.name))),p.written=!0);const i=D.beginComputePass({label:H+N.name});i.setPipeline(k(j.p)),i.setBindGroup(0,M(j)),i.dispatchWorkgroups(N.dispatch[0],N.dispatch[1]||1,N.dispatch[2]||1),i.end()}return L(d.frameSwaps||[]),!0},export:function(l){const t=w[d.exports[l]];if(t&&t.buffer)return{buffer:t.buffer};if(!t)return null;let m=n.get(t.texture);return m||(m=t.texture.createView(),n.set(t.texture,m)),{texture:t.texture,view:m,width:t.w,height:t.h,levels:t.levels}},dispose:X}}window.__gosx_elio_graph.executors.webgpu=W})(),(function(){const q={uniform:0,sampler:1,disp:2,slope:6,foam:10};function W(f,s,w,E){const V=f.indexOf(s);if(V<0)throw new Error("ocean-fft: anchor not found: "+s.slice(0,48));const Y=f.indexOf(w,V);if(Y<0)throw new Error("ocean-fft: anchor not found: "+w.slice(0,48));return f.slice(0,V)+E+f.slice(Y)}function S(f,s,w){if(f.indexOf(s)<0)throw new Error("ocean-fft: anchor not found: "+s.slice(0,48));return f.replace(s,()=>w)}function C(f){let s=`struct OceanSpec { c: array<vec4f, 4>, f: vec4f, m: vec4f, w: array<vec4f, 4> };
`;s+="@group("+f+") @binding("+q.uniform+`) var<uniform> sp: OceanSpec;
`,s+="@group("+f+") @binding("+q.sampler+`) var spSampler: sampler;
`;for(let w=0;w<4;w++)s+="@group("+f+") @binding("+(q.disp+w)+") var spDisp"+w+`: texture_2d<f32>;
`;for(let w=0;w<4;w++)s+="@group("+f+") @binding("+(q.slope+w)+") var spSlope"+w+`: texture_2d<f32>;
`;return s+="@group("+f+") @binding("+q.foam+`) var spFoam: texture_2d<f32>;
`,s+=`fn spLod(c: vec4f, spacing: f32) -> f32 { return max(log2(max(spacing, 1e-3) * c.z / c.x), 0.0); }
`,s}function d(){let f=`  var p = vec3f(xz.x, level, xz.y); var dx = vec3f(1.0, 0.0, 0.0); var dz = vec3f(0.0, 0.0, 1.0);
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
`,f}const K=`    let sNear = 1.0 - smoothstep(0.7 * sp.m.w, sp.m.w, length(in.world.xz - ocean.p[7].xz));
    let sJf = (1.0 + sp.f.y * sXX) * (1.0 + sp.f.y * sZZ) - sp.f.y * sp.f.y * sXZ * sXZ;
    let sStateless = clamp((sp.f.z - sJf) * sp.f.w, 0.0, 1.0) * lace * bu;
    let sKept = smoothstep(0.04, 0.6, sPersist) * (0.55 + 0.45 * lace);
    foam = mix(sStateless, sKept, sNear);
`;function H(f,s){let w=f;return w=S(w,"@location(4) depth0: f32 };","@location(4) depth0: f32, @location(5) rest: vec2f };"),w=W(w,"  var p = vec3f(xz.x, level, xz.y);","  let ph = fract(t * ocean.p[0].w / 9.0",d()),w=S(w,"out.depth0 = depth0;","out.depth0 = depth0; out.rest = xz;"),w=S(w,`  let N = od(normalize(in.normal), in.world.xz, t, ocean.p[9].xy);
`,e()),w=S(w,"  if (dist < 250.0) {","  if (dist < 6000.0) {"),w=S(w,`    foam = (1.0 - smoothstep(0.12, 0.35, in.jacobian)) * lace * bu;
`,K),w=S(w,"(1.0 - smoothstep(80.0, 250.0, dist))","(1.0 - smoothstep(1500.0, 6000.0, dist))"),w=S(w,"(ambient * 0.55 + sunCol * 0.04 * max(dot(N, L), 0.0))","(ambient * 1.4 + sunCol * 0.2 * max(dot(N, L), 0.0) + vec3f(0.05))"),w+`
`+C(s)}window.__gosx_scene3d_ocean_fft_surface={wgsl:H,SLOT:q}})(),(function(){const q={disp:4,slope:8,foam:12};function W(f,s,w){if(f.indexOf(s)<0)throw new Error("ocean-fft: anchor not found: "+s.slice(0,48));return f.replace(s,()=>w)}function S(f,s,w,E){const V=f.indexOf(s);if(V<0)throw new Error("ocean-fft: anchor not found: "+s.slice(0,48));const Y=f.indexOf(w,V);if(Y<0)throw new Error("ocean-fft: anchor not found: "+w.slice(0,48));return f.slice(0,V)+E+f.slice(Y)}function C(){let f=`uniform vec4 u_sp[10];
`;for(let s=0;s<4;s++)f+="uniform sampler2D u_spDisp"+s+`;
`;for(let s=0;s<4;s++)f+="uniform sampler2D u_spSlope"+s+`;
`;return f+=`uniform sampler2D u_spFoam;
`,f+=`float spLod(vec4 c, float spacing) { return max(log2(max(spacing, 1e-3) * c.z / c.x), 0.); }
`,f}function d(){let f=`  vec3 p = vec3(xz.x, level, xz.y), dx = vec3(1., 0., 0.), dz = vec3(0., 0., 1.);
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
`,f}const K=`    float sNear = 1. - smoothstep(0.7 * u_sp[5].w, u_sp[5].w, length(v_w.xz - u_ocean[7].xz));
    float sJf = (1. + u_sp[4].y * sXX) * (1. + u_sp[4].y * sZZ) - u_sp[4].y * u_sp[4].y * sXZ * sXZ;
    float sStateless = clamp((u_sp[4].z - sJf) * u_sp[4].w, 0., 1.) * lace * bu;
    float sKept = smoothstep(0.04, 0.6, sPersist) * (0.55 + 0.45 * lace);
    foam = mix(sStateless, sKept, sNear);
`;function H(f,s){let w=f;w=W(w,"out float v_d0;","out float v_d0; out vec2 v_rest;"),w=W(w,"void main() {",C()+"void main() {"),w=S(w,"  vec3 p = vec3(xz.x, level, xz.y), dx","  float ph = fract(t * u_ocean[0].w / 9.",d()),w=W(w,"v_d0 = depth0;","v_d0 = depth0; v_rest = xz;");let E=s;return E=W(E,"in float v_d0;","in float v_d0; in vec2 v_rest;"),E=W(E,"void main() {",C()+"void main() {"),E=W(E,`  vec3 N = od(normalize(v_n), v_w.xz, t, u_ocean[9].xy);
`,e()),E=W(E,"  if (dist < 250.) {","  if (dist < 6000.) {"),E=W(E,`    foam = (1. - smoothstep(0.12, 0.35, v_j)) * lace * bu;
`,K),E=W(E,"(1. - smoothstep(80., 250., dist))","(1. - smoothstep(1500., 6000., dist))"),E=W(E,"(ambient * 0.55 + sunCol * 0.04 * max(dot(N, L), 0.))","(ambient * 1.4 + sunCol * 0.2 * max(dot(N, L), 0.) + vec3(0.05))"),{vertex:w,fragment:E}}window.__gosx_scene3d_ocean_fft_surface_gl={glsl:H,UNIT:q}})(),(function(){const q=window.__gosx_elio_graph,W=window.__gosx_scene3d_elio_ocean,S=window.__gosx_scene3d_ocean_fft_surface,C=window.__gosx_scene3d_ocean_fft_surface_gl,d=.25,e=3.5,K=240,H=function(i){return Math.max(.3,.14*i*i/9.81)},f=40,s=4;let w=null;function E(i){return i==="low"||i==="medium"?i:"high"}function V(i){const p=[];let u={cell:.5,width:1024};for(const g of q.expand(i)){const x=g.uniforms&&g.uniforms[0]&&g.uniforms[0].values;g.kernel==="ocean_spectrum"&&x?p.push({tile:x.tile,kMin:x.kMin,kMax:x.kMax,n:x.n}):g.kernel==="ocean_foam"&&x&&(u={cell:x.cell,width:x.width})}return{cascades:p.slice(0,s),foam:u}}function Y(i,p){const u=p.graphParams;if(u)for(const g of Object.keys(u))Number.isFinite(u[g])&&i.hasParam(g)&&i.setValue(g,u[g]);Number.isFinite(p.graphTween)&&i.setTween(p.graphTween)}function n(i,p,u){u.fill(0);const g=i.exportParams(),x=p.foam;p.cascades.forEach(function(o,c){u[c*4]=o.tile,u[c*4+1]=Math.sqrt(Math.max(o.kMin,1e-6)*o.kMax),u[c*4+2]=o.n,u[c*4+3]=1,u[24+c*4]=g["waterModX"+c]||0,u[24+c*4+1]=g["waterModZ"+c]||0});const r=x.cell*x.width,a=function(o){return o-Math.floor(o/r)*r};u[16]=1/r,u[17]=1,u[18]=i.value("foamThreshold"),u[19]=e,u[20]=H(i.value("windSpeed")),u[21]=a(i.value("waterX")),u[22]=a(i.value("waterZ")),u[23]=K}function _(i,p){const u=Number.isFinite(p.time)?p.time:0,g=i.last===null?0:Math.min(d,Math.max(0,u-i.last));i.last=u;const x=p.camera||{};return{time:u,dt:g,cameraX:x.x||0,cameraY:x.y||0,cameraZ:x.z||0}}function O(){return w}function F(i){w=i.failed?null:{x:i.value("waterX"),z:i.value("waterZ"),time:i.value("simTime")}}const B=3,Z=250,h=64,b=1,k=8,R=16,U={collect:[],k:0,latest:null,previous:null};function M(){U.collect=[],U.k=0}function L(i,p,u,g){const x=U.k++;U.collect.length<h&&U.collect.push({x:i,z:p});const r=Number.isFinite(g)?g:typeof performance<"u"?performance.now():0,a=U.latest;if(!a||x>=a.count||r-a.at>Z||Math.abs(a.points[x].x-i)>8||Math.abs(a.points[x].z-p)>8)return null;const o=U.previous&&U.previous.count>x?U.previous:null,c=o&&a.at>o.at?(a.heights[x]-o.heights[x])/((a.at-o.at)/1e3):0,z=a.heights[x]+c*((r-a.at)/1e3),v=a.nx[x],y=a.nz[x],P=Math.sqrt(Math.max(0,1-v*v-y*y));return{x:i,y:u+z,z:p,normal:{x:v,y:P,z:y},velocity:{x:0,y:c,z:0}}}function T(i,p){const u=[];for(let x=0;x<B;x++)u.push({buffer:i.createBuffer({label:"gosx-ocean-fft-probe-"+x,size:h*R,usage:b|k}),state:"free",meta:null});const g=function(x){const r=x.buffer,a=x.meta;x.state="mapping",r.mapAsync(b).then(function(){if(x.state!=="mapping")return;const o=new Float32Array(r.getMappedRange().slice(0,a.count*R));r.unmap();const c=[],z=[],v=[];for(let y=0;y<a.count;y++)c.push(o[y*4]),z.push(o[y*4+1]),v.push(o[y*4+2]);U.previous=U.latest,U.latest={at:a.at,count:a.count,points:a.points,heights:c,nx:z,nz:v},x.state="free"},function(){x.state="free"})};return{poll:function(){for(const x of u)x.state==="copied"&&g(x)},copy:function(x,r,a){const o=u.find(function(c){return c.state==="free"});o&&(x.copyBufferToBuffer(r,0,o.buffer,0,h*R),o.state="copied",o.meta={at:p(),count:a.length,points:a})},dispose:function(){for(const x of u)x.state="disposed",x.buffer.destroy()}}}function X(i,p){if(!i.hasParam("probePoints"))return!1;const u=Math.min(p.length,h);if(!u)return i.setValue("probeCount",0),!1;let g=0,x=0;for(let a=0;a<u;a++)g+=p[a].x,x+=p[a].z;g/=u,x/=u;const r=new Float32Array(h*4);for(let a=0;a<u;a++)r[a*4]=p[a].x-g,r[a*4+1]=p[a].z-x;return i.setValue("probeOriginX",g),i.setValue("probeOriginZ",x),i.setValue("probeCount",u),i.setValue("probePoints",r),!0}function l(i,p,u){const g=function(x,r){const a=w;if(!a)return i.sample(p,x,r,u);const o=i.sample(p,x-a.x,r-a.z,a.time);return o.x+=a.x,o.z+=a.z,o};return{sample:g,probed:function(x,r){return L(x,r,p.data[0])||g(x,r)},begin:M}}function t(i){const p=i.device,u="gosx-ocean-fft-",g=function(){const I=GPUShaderStage.VERTEX|GPUShaderStage.FRAGMENT,$=[];$.push({binding:S.SLOT.uniform,visibility:I,buffer:{type:"uniform"}},{binding:S.SLOT.sampler,visibility:I,sampler:{type:"filtering"}});for(let Q=0;Q<s;Q++)$.push({binding:S.SLOT.disp+Q,visibility:I,texture:{sampleType:"float"}},{binding:S.SLOT.slope+Q,visibility:I,texture:{sampleType:"float"}});return $.push({binding:S.SLOT.foam,visibility:I,texture:{sampleType:"float"}}),$},x=p.createBindGroupLayout({label:u+"surface",entries:g()}),r=p.createSampler({label:u+"sampler",magFilter:"linear",minFilter:"linear",mipmapFilter:"linear",addressModeU:"repeat",addressModeV:"repeat"}),a=p.createBuffer({label:u+"surface",size:f*4,usage:GPUBufferUsage.UNIFORM|GPUBufferUsage.COPY_DST}),o=p.createTexture({label:u+"dummy",size:[1,1,1],format:"rgba16float",usage:GPUTextureUsage.TEXTURE_BINDING|GPUTextureUsage.COPY_DST}),c=o.createView(),z=new Float32Array(f),v={last:null,tier:"",runner:null,info:null,viewList:null},y=T(p,function(){return i.now?i.now():typeof performance<"u"?performance.now():0}),P={device:p,ready:!1,layout:x,group:null,disposed:!1,tier:"",update:ee,dispose:oe,fail:A};function A(I){const $="ocean-fft: "+(I&&I.message?I.message:String(I));i.guard&&i.guard.fail("gosx-ocean-fft-surface",$)}function J(I){v.runner&&v.runner.dispose();const $=q.create({module:W,backend:"webgpu",tier:I,host:{device:p,create:i.create,validation:i.validation}});v.runner=$,v.tier=I,v.last=null,v.viewList=null,P.group=null,P.ready=!1,v.info=$.graph?V($.graph):null}function G(){const I=v.runner,$=[],Q=[{binding:S.SLOT.uniform,resource:{buffer:a}},{binding:S.SLOT.sampler,resource:r}];for(let ae=0;ae<s;ae++){const re=I.export("disp"+ae),ne=I.export("slope"+ae);Q.push({binding:S.SLOT.disp+ae,resource:re?re.view:c},{binding:S.SLOT.slope+ae,resource:ne?ne.view:c}),$.push(re?re.view:c,ne?ne.view:c)}const te=I.export("foam");Q.push({binding:S.SLOT.foam,resource:te?te.view:c}),$.push(te?te.view:c),!(v.viewList&&$.every(function(ae,re){return ae===v.viewList[re]}))&&(v.viewList=$,P.group=p.createBindGroup({label:u+"surface",layout:x,entries:Q}))}function ee(I){const $=E(I.tier);(!v.runner||$!==v.tier)&&J($);const Q=v.runner;if(Q.failed)return P.ready=!1,P.group=null,!1;Y(Q,I.ocean),y.poll();const te=U.collect.slice(0,h),ae=X(Q,te),re=Q.run(Object.assign(_(v,I),{encoder:I.encoder}));if(Q.failed)return P.ready=!1,P.group=null,w=null,i.guard&&i.guard.fail("gosx-elio-ocean","ocean graph failed: "+Q.failed),!1;if(!re)return P.ready;const ne=ae?Q.export("probe"):null;return ne&&ne.buffer&&y.copy(I.encoder,ne.buffer,te),G(),n(Q,v.info,z),p.queue.writeBuffer(a,0,z),F(Q),P.ready=!0,!0}function oe(){P.disposed||(P.disposed=!0,P.ready=!1,P.group=null,v.runner&&v.runner.dispose(),w=null,U.latest=U.previous=null,y.dispose(),a.destroy(),o.destroy())}return Object.defineProperty(P,"runner",{get:function(){return v.runner}}),P}function m(i,p){const u=p.environment&&p.environment.ocean;let g=i.fft;const x=p.guard&&(p.guard.disabled.has("oceanFFT")||p.guard.disabled.has("elioGraph"));return!u||!u.spectrum||x||g&&g.device!==p.device?(g&&(g.dispose(),i.fft=null),null):(g||(g=i.fft=t(p)),g.update({ocean:u,tier:p.tier,time:p.time,camera:p.camera,encoder:p.encoder}),g)}function D(i,p){const u={last:null},g=q.create({module:W,backend:"webgl2",tier:E(p.tier),host:{gl:i,caps:p.caps,textureCache:p.textureCache}}),x=new Float32Array(f),r=new Map,a=g.graph?V(g.graph):null,o={gl:i,runner:g,tier:E(p.tier),disposed:!1,update:c,bind:z,dispose:v,fail:function(y){g.fail(y)}};function c(y){return g.failed||(Y(g,y.ocean),!g.run(_(u,y)))?!1:(n(g,a,x),F(g),!0)}function z(y,P){let A=r.get(P);if(!A){const G=function(ee){return y.getUniformLocation(P,ee)};A={sp:G("u_sp[0]"),disp:[0,1,2,3].map(function(ee){return G("u_spDisp"+ee)}),slope:[0,1,2,3].map(function(ee){return G("u_spSlope"+ee)}),foam:G("u_spFoam")},r.set(P,A)}const J=function(G,ee,oe){y.activeTexture(y.TEXTURE0+G),y.bindTexture(y.TEXTURE_2D,oe?oe.tex:o.dummy),y.uniform1i(ee,G)};o.dummy||(o.dummy=y.createTexture(),y.bindTexture(y.TEXTURE_2D,o.dummy),y.texStorage2D(y.TEXTURE_2D,1,y.RGBA16F,1,1),y.texParameteri(y.TEXTURE_2D,y.TEXTURE_MIN_FILTER,y.LINEAR),y.texParameteri(y.TEXTURE_2D,y.TEXTURE_MAG_FILTER,y.LINEAR));for(let G=0;G<s;G++)J(C.UNIT.disp+G,A.disp[G],g.export("disp"+G)),J(C.UNIT.slope+G,A.slope[G],g.export("slope"+G));J(C.UNIT.foam,A.foam,g.export("foam")),y.uniform4fv(A.sp,x)}function v(){o.disposed||(o.disposed=!0,g.dispose(),o.dummy&&i.deleteTexture(o.dummy))}return o}function j(i,p,u){let g=i.fft;if(g&&(g.gl!==p||g.disposed||g.tier!==E(u.tier))&&(g.dispose(),g=i.fft=null),i.fftDisabled||p.isContextLost&&p.isContextLost())return null;g||(g=i.fft=D(p,u));const x=g.update({ocean:u.ocean,time:u.time,camera:u.camera});return g.runner.failed?(i.fftDisabled=g.runner.failed,g.dispose(),i.fft=null,w=null,u.mount&&u.mount.setAttribute&&u.mount.setAttribute("data-gosx-scene3d-ocean-fft-fallback",i.fftDisabled),null):x?g:null}function N(i,p,u){i.fft&&i.fft.dispose(),i.fft=null,i.fftDisabled=u,w=null,p&&p.mount&&p.mount.setAttribute&&p.mount.setAttribute("data-gosx-scene3d-ocean-fft-fallback",u)}window.__gosx_scene3d_ocean_fft_api={frame:m,water:O,sampler:l,probe:{begin:M,sample:L},probeState:U,describe:V,feed:Y,surfaceData:n},window.__gosx_scene3d_ocean_fft_gl_api={run:j,fail:N}})();
