(function(){"use strict";var X=window.__gosx_scene3d_api;if(!X){console.warn("[gosx] scene3d-postfx-webgl chunk loaded without main scene3d bundle");return}var l=X.sceneNumber||function(e,a){var o=Number(e);return Number.isFinite(o)?o:a},G=X.sceneMat4MultiplyInto,te=X.SCENE_POST_CUSTOM_POST||"customPost",k,q,J,A,z,M,Y,Q,Z,U,K,y;function $(e){e&&(k=e.SCENE_POST_VIEW_POSITION_GLSL,q=e.createSceneFullscreenQuad,J=e.createScenePostFBO,A=e.createScenePostProgram,z=e.disposeScenePostFBO,M=e.drawSceneFullscreenQuad,Y=e.objectIsSkinned,Q=e.sceneInvertOrthonormalView,Z=e.scenePBRCreateProgramInfo,U=e.scenePBRProgramReady,K=e.scenePBRRenderTruth,y=e.sceneWaterUniformLocation)}function oe(){return`#version 300 es
precision highp float;
precision highp sampler2D;
in vec2 v_uv;
uniform sampler2D u_texture;
uniform sampler2D u_history;
uniform sampler2D u_historyDepth;
uniform sampler2D u_velocity;
uniform mat4 u_inverseView;
uniform mat4 u_previousView;
uniform mat4 u_previousProjection;
uniform vec4 u_temporalParams;
uniform vec4 u_temporalJitter;
uniform vec4 u_motion;
uniform vec4 u_ocean;
uniform vec4 u_pools[4];
out vec4 fragColor;
${k}
vec3 toYCoCg(vec3 c) { return vec3(dot(c, vec3(0.25, 0.5, 0.25)), c.r * 0.5 - c.b * 0.5, -c.r * 0.25 + c.g * 0.5 - c.b * 0.25); }
vec3 fromYCoCg(vec3 c) { return vec3(c.x + c.y - c.z, c.x + c.z, c.x - c.y - c.z); }
bool waterAt(vec3 world) {
    if (u_ocean.z > 0.5 && abs(world.y - u_ocean.x) <= u_ocean.y) return true;
    for (int k = 0; k < 4; k++) {
        vec4 pool = u_pools[k];
        if (pool.w > 0.5 && abs(world.x) <= pool.x && abs(world.z) <= pool.y && abs(world.y) <= pool.z) return true;
    }
    return false;
}
void main() {
    vec4 current = texture(u_texture, v_uv);
    float depth = texture(u_depthTexture, v_uv).r;
    vec2 texel = 1.0 / vec2(textureSize(u_texture, 0));
    if (u_temporalParams.w < 0.5) { fragColor = current; return; }
    float nearDepth = depth, farDepth = depth;
    for (int y = -1; y <= 1; y++) for (int x = -1; x <= 1; x++) {
        float d = texture(u_depthTexture, v_uv + vec2(x, y) * texel).r;
        nearDepth = min(nearDepth, d); farDepth = max(farDepth, d);
    }
    if (nearDepth >= 0.999999) { fragColor = current; return; }
    bool coverageEdge = farDepth >= 0.999999;
    if (coverageEdge) depth = nearDepth;
    vec3 viewPosition = postViewPosition(v_uv, depth);
    vec4 world = u_inverseView * vec4(viewPosition, 1);
    vec4 previousPosition = u_previousView * world;
    vec4 clip = u_previousProjection * previousPosition;
    if (clip.w <= 0.0) { fragColor = current; return; }
    vec2 uv = clip.xy / clip.w * 0.5 + 0.5;
    bool mover = false;
    if (u_motion.x > 0.5) {
        vec4 velocity = texture(u_velocity, v_uv);
        if (velocity.a > 0.5 && abs(velocity.b + viewPosition.z) <= 0.02 * abs(viewPosition.z) + 0.02) { uv = v_uv + velocity.xy; mover = true; }
    }
    if (any(lessThan(uv, vec2(0))) || any(greaterThan(uv, vec2(1)))) { fragColor = current; return; }
    if (!mover) {
        float oldDepth = texture(u_historyDepth, uv).r;
        if (coverageEdge) for (int y = -1; y <= 1; y++) for (int x = -1; x <= 1; x++)
            oldDepth = min(oldDepth, texture(u_historyDepth, uv + vec2(x, y) * texel).r);
        oldDepth = oldDepth * 2.0 - 1.0;
        float oldZ = (u_previousProjection[3][2] - oldDepth * u_previousProjection[3][3]) /
                     (oldDepth * u_previousProjection[2][3] - u_previousProjection[2][2]);
        if (abs(oldZ - previousPosition.z) > u_temporalParams.z * max(1.0, abs(previousPosition.z))) { fragColor = current; return; }
    }
    vec4 low = vec4(1e20), high = vec4(-1e20), mean = vec4(0), square = vec4(0);
    for (int y = -1; y <= 1; y++) for (int x = -1; x <= 1; x++) {
        vec4 sampleColor = texture(u_texture, v_uv + vec2(x, y) * texel);
        vec4 c = vec4(toYCoCg(sampleColor.rgb), sampleColor.a);
        low = min(low, c); high = max(high, c); mean += c / 9.0; square += c * c / 9.0;
    }
    vec4 sigma = sqrt(max(vec4(0), square - mean * mean));
    low = max(low, mean - sigma * u_temporalParams.y);
    high = min(high, mean + sigma * u_temporalParams.y);
    vec2 historyUV = coverageEdge ? uv + u_temporalJitter.xy - u_temporalJitter.zw : uv;
    if (any(lessThan(historyUV, vec2(0))) || any(greaterThan(historyUV, vec2(1)))) { fragColor = current; return; }
    vec4 historySample = texture(u_history, historyUV);
    vec4 bounded = clamp(vec4(toYCoCg(historySample.rgb), historySample.a), low, high);
    vec3 history = fromYCoCg(bounded.rgb);
    float motion = length((historyUV - v_uv) / texel);
    float change = abs(toYCoCg(history).x - toYCoCg(current.rgb).x);
    float reactive = waterAt(world.xyz) ? 0.75 : 0.0;
    float weight = u_temporalParams.x * exp(-motion * 0.05) / (coverageEdge ? 1.0 : 1.0 + change * 8.0) * (1.0 - reactive * 0.85);
    fragColor = vec4(mix(current.rgb, history, weight), mix(current.a, bounded.a, weight));
}`}const ae=`
layout(location=0) in vec3 a_position;
layout(location=1) in vec4 a_joints;
layout(location=2) in vec4 a_weights;
uniform mat4 u_current;
uniform mat4 u_previous;
#ifdef GOSX_SKINNED
uniform mat4 u_joints[64];
uniform mat4 u_previousJoints[64];
#endif
out vec4 v_current;
out vec4 v_previous;
void main() {
    vec4 p = vec4(a_position, 1.0), q = p;
#ifdef GOSX_SKINNED
    ivec4 j = ivec4(a_joints);
    p = (a_weights.x * u_joints[j.x] + a_weights.y * u_joints[j.y] + a_weights.z * u_joints[j.z] + a_weights.w * u_joints[j.w]) * p;
    q = (a_weights.x * u_previousJoints[j.x] + a_weights.y * u_previousJoints[j.y] + a_weights.z * u_previousJoints[j.z] + a_weights.w * u_previousJoints[j.w]) * q;
#endif
    v_current = u_current * p;
    v_previous = u_previous * q;
    gl_Position = v_current;
}`,ne=`#version 300 es
precision highp float;
in vec4 v_current;
in vec4 v_previous;
out vec4 fragColor;
void main() { fragColor = vec4((v_previous.xy / v_previous.w - v_current.xy / v_current.w) * 0.5, v_current.w, 1.0); }`;function ie(e,a){var o=e&&e.environment&&e.environment.ocean;a[0]=o?l(o.level,0):0,a[1]=o?Math.max(.05,l(o.waveHeight,.8)*.75):0,a[2]=o?1:0,a[3]=0;for(var r=Array.isArray(e&&e.waterSystems)?e.waterSystems:[],n=0;n<4;n++){var t=r[n],u=4+n*4;a[u]=t?Math.max(.001,l(t.poolWidth,1)):0,a[u+1]=t?Math.max(.001,l(t.poolLength,1)):0,a[u+2]=t?.2*Math.max(.001,l(t.poolHeight,1))+.02:0,a[u+3]=t?1:0}}function ue(e,a){var o=e.gl,r=a?"skinned":"rigid";if(e.programs[r]!==void 0)return e.programs[r];if(a&&!(o.getParameter(o.MAX_VERTEX_UNIFORM_VECTORS)>=528))return e.programs[r]=null;var n=`#version 300 es
`+(a?`#define GOSX_SKINNED 1
`:"")+ae;return e.programs[r]=Z(o,n,ne,"Motion vector shader",function(t){return{uniforms:{current:o.getUniformLocation(t,"u_current"),previous:o.getUniformLocation(t,"u_previous"),joints:o.getUniformLocation(t,"u_joints[0]"),previousJoints:o.getUniformLocation(t,"u_previousJoints[0]")}}})}function ee(e){var a=e.gl,o=e.target;o&&(a.deleteFramebuffer(o.fbo),a.deleteTexture(o.texture),a.deleteRenderbuffer(o.depth)),e.target=null,e.width=e.height=0}function se(e,a,o){if(e.target&&e.width===a&&e.height===o)return e.target;ee(e);var r=e.gl,n=r.createTexture(),t=r.createRenderbuffer(),u=r.createFramebuffer();e.width=a,e.height=o,r.bindTexture(r.TEXTURE_2D,n),r.texStorage2D(r.TEXTURE_2D,1,r.RGBA16F,a,o);for(var i of[r.TEXTURE_MIN_FILTER,r.TEXTURE_MAG_FILTER])r.texParameteri(r.TEXTURE_2D,i,r.NEAREST);for(var s of[r.TEXTURE_WRAP_S,r.TEXTURE_WRAP_T])r.texParameteri(r.TEXTURE_2D,s,r.CLAMP_TO_EDGE);return r.bindRenderbuffer(r.RENDERBUFFER,t),r.renderbufferStorage(r.RENDERBUFFER,r.DEPTH_COMPONENT24,a,o),r.bindFramebuffer(r.FRAMEBUFFER,u),r.framebufferTexture2D(r.FRAMEBUFFER,r.COLOR_ATTACHMENT0,r.TEXTURE_2D,n,0),r.framebufferRenderbuffer(r.FRAMEBUFFER,r.DEPTH_ATTACHMENT,r.RENDERBUFFER,t),e.target={texture:n,depth:t,fbo:u}}function ce(e,a,o,r){var n=r?Math.min(r.length,1024):0,t=e.records.get(a.id);if(!t)return e.records.set(a.id,{model:Float32Array.from(Array.prototype.slice.call(o,0,16)),previous:new Float32Array(16),joints:r?r.slice(0,n):null,previousJoints:null,seen:e.frame}),null;t.seen=e.frame,t.previous.set(t.model);for(var u=0;u<16;u++)t.model[u]=o[u];var i=t.previous.some(function(f,m){return f!==t.model[m]});if(!r)return t.previousJoints=null,i?t:null;var s=t.previousJoints&&t.previousJoints.length===n?t.previousJoints:new Float32Array(n);return t.previousJoints=t.joints&&t.joints.length===n?t.joints:r.slice(0,n),s.set(r.subarray(0,n)),t.joints=s,i||t.joints.some(function(f,m){return f!==t.previousJoints[m]})?t:null}function fe(e,a){var o=[];e.frame+=1;for(var r=0;r<a.length;r++){var n=a[r];if(!(!n||n.viewCulled||!n.id||!n.directVertices)){var t=Y(n)&&n.skin.jointMatrices instanceof Float32Array&&n.skin.jointMatrices.length>=16?n.skin.jointMatrices:null;if(!(!n.retainedGeometry&&!t)){var u=ce(e,n,n.modelMatrix&&n.modelMatrix.length>=16?n.modelMatrix:_e,t);u&&o.push({obj:n,record:u})}}}return e.records.forEach(function(i,s){i.seen!==e.frame&&e.records.delete(s)}),o}function ve(e,a,o){var r=e.gl,n=a.obj,t=a.record,u=!!t.previousJoints,i=ue(e,u),s=Math.max(0,Math.floor(l(n.vertexCount,0)));if(!i||!U(r,i.program)||!o.attribute(n,"positions",0,3,o.data(n.vertices,"positions",s,3))||u&&!(o.attribute(n,"joints",1,4,o.data(n.vertices,"joints",s,4))&&o.attribute(n,"weights",2,4,o.data(n.vertices,"weights",s,4))))return!1;r.useProgram(i.program),G(e.current,e.currentVP,t.model),G(e.previous,e.previousVP,t.previous),r.uniformMatrix4fv(i.uniforms.current,!1,e.current),r.uniformMatrix4fv(i.uniforms.previous,!1,e.previous),u&&(r.uniformMatrix4fv(i.uniforms.joints,!1,t.joints),r.uniformMatrix4fv(i.uniforms.previousJoints,!1,t.previousJoints));var f=o.indices(n);return f>0?r.drawElements(r.TRIANGLES,f,r.UNSIGNED_INT,0):r.drawArrays(r.TRIANGLES,0,s),!0}function me(e,a,o,r,n){var t=e.gl,u=se(e,r,n),i=0,s=t.isEnabled(t.BLEND),f=t.isEnabled(t.CULL_FACE),m=t.isEnabled(t.DEPTH_TEST);t.bindFramebuffer(t.FRAMEBUFFER,u.fbo),t.viewport(0,0,r,n),t.disable(t.BLEND),t.disable(t.CULL_FACE),t.enable(t.DEPTH_TEST),t.depthFunc(t.LESS),t.depthMask(!0),t.clearColor(0,0,0,0),t.clearDepth(1),t.clear(t.COLOR_BUFFER_BIT|t.DEPTH_BUFFER_BIT),e.vao||(e.vao=t.createVertexArray()),t.bindVertexArray(e.vao);for(var F=0;F<a.length;F++)ve(e,a[F],o)&&(i+=1);return t.bindVertexArray(null),s&&t.enable(t.BLEND),f&&t.enable(t.CULL_FACE),m||t.disable(t.DEPTH_TEST),i}function pe(e,a,o,r,n,t,u,i){ie(a,e.water);var s=fe(e,a&&a.meshObjects||[]);G(e.currentVP,r,n),G(e.previousVP,t,u);var f=s.length&&o?me(e,s,o,i[0],i[1]):0;e.active=f>0;var m=e.gl;K().enabled()&&m.canvas&&m.canvas.parentNode&&m.canvas.parentNode.setAttribute("data-gosx-scene3d-render-taa-movers",String(f))}function le(e,a){var o=e.gl;o.activeTexture(o.TEXTURE0+4),o.bindTexture(o.TEXTURE_2D,e.active&&e.target?e.target.texture:null),o.uniform1i(y(o,a,"u_velocity"),4),o.uniform4f(y(o,a,"u_motion"),e.active?1:0,0,0,0),o.uniform4fv(y(o,a,"u_ocean"),e.ocean),o.uniform4fv(y(o,a,"u_pools[0]"),e.pools)}const _e=new Float32Array([1,0,0,0,0,1,0,0,0,0,1,0,0,0,0,1]);function de(e){var a=new Float32Array(20),o={gl:e,records:new Map,programs:{},target:null,vao:null,width:0,height:0,frame:0,active:!1,current:new Float32Array(16),previous:new Float32Array(16),currentVP:new Float32Array(16),previousVP:new Float32Array(16),water:a,ocean:a.subarray(0,4),pools:a.subarray(4,20)};return{begin:function(){o.active=!1},update:function(r,n,t,u,i,s,f){pe(o,r,n,t,u,i,s,f)},bindResolve:function(r){le(o,r)},reset:function(){o.records.clear(),o.active=!1},dispose:function(){ee(o),o.records.clear(),o.vao&&e.deleteVertexArray(o.vao),o.vao=null,o.programs={}}}}function re(e,a){for(var o=0,r=1;e>0;)r/=a,o+=r*(e%a),e=Math.floor(e/a);return o}function Ee(e,a,o,r,n){var t=(re(r%8+1,2)-.5)*2/a,u=(re(r%8+1,3)-.5)*2/o;n[0]=t*.5,n[1]=u*.5;for(var i=0;i<4;i++)e[i*4]+=t*e[i*4+3],e[i*4+1]+=u*e[i*4+3]}function xe(e,a){var o=null,r=null,n=!1,t=0,u=!1,i=0,s=0,f="",m=0,F=new Float32Array(16),w=new Float32Array(16),_=new Float32Array(16),c=null,v=new Float32Array(4),d=de(e),h=null,E=null;function p(){o&&(z(e,o[0]),z(e,o[1])),o=null,u=!1,t=0,d.reset()}function O(R,x,T,b,g,V){var C=R.find(I=>I.kind==="taa"),N=C?R.slice(0,R.indexOf(C)+1):[];if(!C||!g||n||N.some(I=>I.kind===te))return p(),!1;if(!e.getExtension("EXT_color_buffer_float")||!e.blitFramebuffer||!e.checkFramebufferStatus)return!1;if(r||(r=A(e,oe())),!r)return n=!0,!1;if(!o||i!==x.width||s!==x.height){p(),i=x.width,s=x.height,o=[J(e,i,s,!0),J(e,i,s,!0)];for(var L=0;L<2;L++)if(e.bindFramebuffer(e.FRAMEBUFFER,o[L].fbo),e.checkFramebufferStatus(e.FRAMEBUFFER)!==e.FRAMEBUFFER_COMPLETE)return p(),n=!0,!1}var D=JSON.stringify([N,N.some(I=>I.kind==="contactShadows")?V:null]),W=performance.now();c=Q(b);var H=Math.hypot(c[12]-_[12],c[13]-_[13],c[14]-_[14])>2,S=c[8]*_[8]+c[9]*_[9]+c[10]*_[10]<.5,B=Math.abs(T[0]-w[0])>1e-4||Math.abs(T[5]-w[5])>1e-4||T[10]!==w[10]||T[11]!==w[11];return(f!==D||W-m>250||H||S||B)&&(u=!1),f=D,m=W,v[0]=0,v[1]=0,u&&Ee(T,i,s,t,v),h=T,E=b,d.begin(),u}function P(R,x){d.update(R,u?x:null,h,E,w,F,[i,s])}function j(R,x,T,b){if(!o||!r||!b)return R;for(var g=o[t%2],V=o[(t+1)%2],C=0;C<5;C++)e.activeTexture(e.TEXTURE0+C),e.bindTexture(e.TEXTURE_2D,null);e.bindFramebuffer(e.FRAMEBUFFER,g.fbo),e.viewport(0,0,i,s),e.useProgram(r.program);for(var N=[R,x.depthTex,V.colorTex,V.depthTex],L=["u_texture","u_depthTexture","u_history","u_historyDepth"],D=0;D<4;D++)e.activeTexture(e.TEXTURE0+D),e.bindTexture(e.TEXTURE_2D,N[D]),e.uniform1i(y(e,r.program,L[D]),D);for(var W=[b.projection,c,F,w],H=["u_projection","u_inverseView","u_previousView","u_previousProjection"],S=0;S<4;S++)e.uniformMatrix4fv(y(e,r.program,H[S]),!1,W[S]);e.uniform4f(y(e,r.program,"u_temporalParams"),Math.max(0,Math.min(.95,l(T.historyWeight,.9))),Math.max(.5,Math.min(3,l(T.clampGamma,1))),Math.max(1e-4,Math.min(.1,l(T.depthThreshold,.01))),u?1:0),e.uniform4f(y(e,r.program,"u_temporalJitter"),v[0],v[1],v[2],v[3]),d.bindResolve(r.program),M(e,a.vao);for(var B=0;B<5;B++)e.activeTexture(e.TEXTURE0+B),e.bindTexture(e.TEXTURE_2D,null);return e.bindFramebuffer(e.READ_FRAMEBUFFER,x.fbo),e.bindFramebuffer(e.DRAW_FRAMEBUFFER,g.fbo),e.blitFramebuffer(0,0,i,s,0,0,i,s,e.DEPTH_BUFFER_BIT,e.NEAREST),F.set(b.view),w.set(b.projection),_.set(c),v[2]=v[0],v[3]=v[1],u=!0,t++,g.colorTex}return{prepare:O,motion:P,resolve:j,reset:p,ready:function(){return!!o&&!n},dispose:function(){p(),d.dispose(),r&&(e.deleteProgram(r.program),e.deleteShader(r.vertexShader),e.deleteShader(r.fragmentShader)),r=null},valid:function(){return u}}}const Te=`#version 300 es
precision highp float;
in vec2 v_uv;
uniform sampler2D u_texture;
uniform sampler2D u_depthTexture;
uniform float u_focusDistance;
uniform float u_aperture;
uniform float u_maxBlur;
uniform float u_near;
uniform float u_far;
out vec4 fragColor;
const float GOLDEN_ANGLE = 2.39996323;
const int DOF_TAPS = 48;
float linearDepth(float depth) {
    float z = depth * 2.0 - 1.0;
    return (2.0 * u_near * u_far) / max(0.0001, u_far + u_near - z * (u_far - u_near));
}
float cocOf(float depth, float maxBlur) { return clamp(abs(depth - u_focusDistance) * u_aperture * maxBlur, 0.0, maxBlur); }
void main() {
    vec2 texel = 1.0 / vec2(textureSize(u_texture, 0));
    float maxBlur = clamp(u_maxBlur, 0.0, 64.0);
    float centerDepth = linearDepth(texture(u_depthTexture, v_uv).r);
    float centerCoC = cocOf(centerDepth, maxBlur);
    vec3 color = texture(u_texture, v_uv).rgb;
    float total = 1.0;
    // Squared radius grows by 2 * stride per tap, so DOF_TAPS taps reach maxBlur.
    float stride = maxBlur * maxBlur / float(2 * DOF_TAPS);
    float radius = stride, angle = 0.0;
    for (int i = 0; i < DOF_TAPS; i++) {
        if (radius >= maxBlur || maxBlur < 0.5) break;
        vec2 uv = v_uv + vec2(cos(angle), sin(angle)) * texel * radius;
        vec3 sampleColor = texture(u_texture, uv).rgb;
        float sampleDepth = linearDepth(texture(u_depthTexture, uv).r);
        float sampleCoC = cocOf(sampleDepth, maxBlur);
        if (sampleDepth > centerDepth) sampleCoC = min(sampleCoC, centerCoC * 2.0);
        color += mix(color / total, sampleColor, smoothstep(radius - 0.5, radius + 0.5, sampleCoC));
        total += 1.0;
        radius += stride / radius;
        angle += GOLDEN_ANGLE;
    }
    fragColor = vec4(color / total, 1.0);
}`,he=`#version 300 es
precision highp float;
in vec2 v_uv;
uniform sampler2D u_scene;
out vec4 fragColor;
void main() {
    vec3 c = texture(u_scene, v_uv).rgb;
    fragColor = vec4(log2(max(dot(c, vec3(0.2126, 0.7152, 0.0722)), 0.0001)), 0.0, 0.0, 1.0);
}`,Re=`#version 300 es
precision highp float;
in vec2 v_uv;
uniform sampler2D u_luminance;
uniform sampler2D u_previous;
uniform vec4 u_settings;
uniform vec4 u_rates;
uniform float u_level;
out vec4 fragColor;
void main() {
    float average = exp2(textureLod(u_luminance, vec2(0.5), u_level).r);
    float target = clamp(u_settings.z / max(average, 0.0001) * exp2(u_settings.w), u_settings.x, u_settings.y);
    float previous = texture(u_previous, vec2(0.5)).r;
    float current = (u_rates.w > 0.5 || !(previous > 0.0)) ? target : previous;
    float rate = target > current ? u_rates.x : u_rates.y;
    fragColor = vec4(current + (target - current) * (1.0 - exp(-max(u_rates.z, 0.0) * rate)), average, 0.0, 1.0);
}`,be=`#version 300 es
precision highp float;
in vec2 v_uv;
uniform sampler2D u_exposure;
out vec4 fragColor;
void main() {
    fragColor = vec4(vec3(texture(u_exposure, vec2(0.5)).r), 1.0);
}`;function ye(e){if(!e||typeof e!="object")return null;var a=function(t,u){var i=Number(t);return isFinite(i)&&i>0?i:u},o=a(e.minExposure,.1),r=a(e.maxExposure,8),n=Number(e.compensation);return{minExposure:Math.min(o,r),maxExposure:Math.max(o,r),key:a(e.key,.18),speedUp:a(e.speedUp,3),speedDown:a(e.speedDown,1),compensation:isFinite(n)?Math.max(-8,Math.min(8,n)):0}}function Fe(e){var a=null,o=!1,r=null,n=null,t=[],u=0,i=-1,s=null,f=!1;function m(c,v,d){var h=e.createTexture();e.bindTexture(e.TEXTURE_2D,h),e.texStorage2D(e.TEXTURE_2D,d,e.R16F,c,v),e.texParameteri(e.TEXTURE_2D,e.TEXTURE_MIN_FILTER,d>1?e.LINEAR_MIPMAP_LINEAR:e.NEAREST),e.texParameteri(e.TEXTURE_2D,e.TEXTURE_MAG_FILTER,e.NEAREST);var E=e.createFramebuffer();return e.bindFramebuffer(e.FRAMEBUFFER,E),e.framebufferTexture2D(e.FRAMEBUFFER,e.COLOR_ATTACHMENT0,e.TEXTURE_2D,h,0),{texture:h,fbo:E}}function F(){return a||o?!!a:!e.getExtension("EXT_color_buffer_float")||typeof e.texStorage2D!="function"?(o=!0,!1):(a={luminance:A(e,he),adapt:A(e,Re),apply:A(e,be)},!a.luminance||!a.adapt||!a.apply?(a=null,o=!0,!1):(r=q(e),n=m(64,64,7),t=[m(1,1,1),m(1,1,1)],!0))}function w(){return U(e,a.luminance.program)&&U(e,a.adapt.program)&&U(e,a.apply.program)}function _(c,v,d,h){e.activeTexture(e.TEXTURE0+c),e.bindTexture(e.TEXTURE_2D,v),e.uniform1i(e.getUniformLocation(d,h),c)}return{apply:function(c,v,d,h,E){var p=ye(h&&h.autoExposure);if(!p||!c||!c.framebuffer||!c.colorTexture||!F()||!w())return f=!1,!1;var O=typeof performance<"u"?performance.now():Date.now(),P=[l(E&&E.x,0),l(E&&E.y,0),l(E&&E.z,0)],j=!f||!s||Math.hypot(P[0]-s[0],P[1]-s[1],P[2]-s[2])>2,R=i<0||O-i>250?0:(O-i)/1e3;s=P,i=O,f=!0,e.disable(e.BLEND),e.disable(e.DEPTH_TEST),e.bindFramebuffer(e.FRAMEBUFFER,n.fbo),e.viewport(0,0,64,64),e.useProgram(a.luminance.program),_(0,c.colorTexture,a.luminance.program,"u_scene"),M(e,r.vao),e.bindTexture(e.TEXTURE_2D,n.texture),e.generateMipmap(e.TEXTURE_2D);var x=t[u%2],T=t[(u+1)%2];e.bindFramebuffer(e.FRAMEBUFFER,x.fbo),e.viewport(0,0,1,1),e.useProgram(a.adapt.program),_(0,n.texture,a.adapt.program,"u_luminance"),_(1,T.texture,a.adapt.program,"u_previous"),e.uniform4f(e.getUniformLocation(a.adapt.program,"u_settings"),p.minExposure,p.maxExposure,p.key,p.compensation),e.uniform4f(e.getUniformLocation(a.adapt.program,"u_rates"),p.speedUp,p.speedDown,R,j?1:0),e.uniform1f(e.getUniformLocation(a.adapt.program,"u_level"),6),M(e,r.vao),e.bindFramebuffer(e.FRAMEBUFFER,c.framebuffer),e.viewport(0,0,v,d);var b=typeof e.getParameter=="function"&&e.getParameter(e.DRAW_BUFFER1)===e.COLOR_ATTACHMENT1;return b&&e.drawBuffers([e.COLOR_ATTACHMENT0,e.NONE]),e.useProgram(a.apply.program),_(0,x.texture,a.apply.program,"u_exposure"),e.enable(e.BLEND),e.blendFuncSeparate(e.ZERO,e.SRC_COLOR,e.ZERO,e.ONE),M(e,r.vao),e.disable(e.BLEND),b&&e.drawBuffers([e.COLOR_ATTACHMENT0,e.COLOR_ATTACHMENT1]),e.activeTexture(e.TEXTURE0),u+=1,!0},dispose:function(){for(var c of[n].concat(t))c&&(e.deleteTexture(c.texture),e.deleteFramebuffer(c.fbo));if(a)for(var v in a)e.deleteProgram(a[v].program);r&&(e.deleteVertexArray(r.vao),r.vbo&&e.deleteBuffer(r.vbo)),a=null,n=null,t=[],r=null,o=!1,f=!1}}}window.__gosx_scene3d_postfx_webgl_api={temporal:function(e,a,o){return $(o),xe(e,a)},autoExposure:function(e,a){return $(a),Fe(e)},dofSource:Te}})();
