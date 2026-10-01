"""Execute the shipped TAA fragment shader with Mesa's CPU rasterizer."""
import ctypes as c
import ctypes.util
import json
import sys

library = ctypes.util.find_library("OSMesa")
if not library:
    print(json.dumps({"skip": "OSMesa CPU rasterizer is unavailable"}))
    sys.exit(0)
gl = c.CDLL(library)


def api(name, result, *arguments):
    function = getattr(gl, name)
    function.restype, function.argtypes = result, arguments
    return function


uint, integer, floating, pointer = c.c_uint, c.c_int, c.c_float, c.c_void_p
create = api("OSMesaCreateContextExt", pointer, uint, integer, integer, integer, pointer)
context = create(0x1908, 0, 0, 0, None)
width, height = 32, 8
pixels = (c.c_ubyte * (width * height * 4))()
assert api("OSMesaMakeCurrent", integer, pointer, pointer, uint, integer, integer)(context, pixels, 0x1401, width, height)
renderer = api("glGetString", c.c_char_p, uint)(0x1F01).decode()
assert "llvmpipe" in renderer.lower() or "softpipe" in renderer.lower(), renderer
shader_source = api("glShaderSource", None, uint, integer, c.POINTER(c.c_char_p), pointer)
compile_shader = api("glCompileShader", None, uint)
shader_status = api("glGetShaderiv", None, uint, uint, c.POINTER(integer))


def shader(kind, source):
    handle = api("glCreateShader", uint, uint)(kind)
    shader_source(handle, 1, (c.c_char_p * 1)(source.encode()), None)
    compile_shader(handle)
    status = integer()
    shader_status(handle, 0x8B81, c.byref(status))
    log = c.create_string_buffer(8192)
    api("glGetShaderInfoLog", None, uint, integer, pointer, pointer)(handle, len(log), None, log)
    assert status.value, log.value.decode()
    return handle


vertex = """#version 300 es
precision highp float;
out vec2 v_uv;
void main() {
    vec2 p = vec2(float((gl_VertexID << 1) & 2), float(gl_VertexID & 2));
    v_uv = p; gl_Position = vec4(p * 2.0 - 1.0, 0.0, 1.0);
}
"""
program = api("glCreateProgram", uint)()
attach = api("glAttachShader", None, uint, uint)
attach(program, shader(0x8B31, vertex))
attach(program, shader(0x8B30, sys.stdin.read()))
api("glLinkProgram", None, uint)(program)
status = integer()
api("glGetProgramiv", None, uint, uint, c.POINTER(integer))(program, 0x8B82, c.byref(status))
assert status.value, "TAA program did not link"
api("glUseProgram", None, uint)(program)
api("glViewport", None, integer, integer, integer, integer)(0, 0, width, height)
location = api("glGetUniformLocation", integer, uint, c.c_char_p)
matrix = api("glUniformMatrix4fv", None, integer, integer, c.c_ubyte, c.POINTER(floating))
identity = (floating * 16)(1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1)
projection = (floating * 16)(1, 0, 0, 0, 0, 1, 0, 0, 0, 0, -1, 0, 0, 0, 0, 1)
for name in [b"u_inverseView", b"u_previousView"]:
    matrix(location(program, name), 1, 0, identity)
for name in [b"u_projection", b"u_previousProjection"]:
    matrix(location(program, name), 1, 0, projection)
textures = (uint * 4)()
api("glGenTextures", None, integer, c.POINTER(uint))(4, textures)
active = api("glActiveTexture", None, uint)
bind = api("glBindTexture", None, uint, uint)
parameter = api("glTexParameteri", None, uint, uint, integer)
upload = api("glTexImage2D", None, uint, integer, integer, integer, integer, integer, uint, uint, pointer)
for unit, name in enumerate([b"u_texture", b"u_depthTexture", b"u_history", b"u_historyDepth"]):
    active(0x84C0 + unit)
    bind(0x0DE1, textures[unit])
    for option, value in [(0x2801, 0x2600), (0x2800, 0x2600), (0x2802, 0x812F), (0x2803, 0x812F)]:
        parameter(0x0DE1, option, value)
    api("glUniform1i", None, integer, integer)(location(program, name), unit)
temporal = api("glUniform4f", None, integer, floating, floating, floating, floating)
read = api("glReadPixels", None, integer, integer, integer, integer, uint, uint, pointer)


def resolve(current, history, depth=0.5, old_depth=0.5, valid=True):
    for unit, values in enumerate([current, [depth] * width, history, [old_depth] * width]):
        data = (floating * (width * height * 4))(*[v for _ in range(height) for value in values for v in [value, value, value, 1]])
        active(0x84C0 + unit)
        bind(0x0DE1, textures[unit])
        upload(0x0DE1, 0, 0x8814, width, height, 0, 0x1908, 0x1406, data)
    temporal(location(program, b"u_temporalParams"), 0.9, 1.25, 0.01, int(valid))
    api("glDrawArrays", None, uint, integer, integer)(0x0004, 0, 3)
    result = (floating * (width * height * 4))()
    read(0, 0, width, height, 0x1908, 0x1406, result)
    assert api("glGetError", uint)() == 0, "software TAA draw failed"
    return list(result)[0:width * 4:4]


def edge(coverage):
    return [0] * 15 + [coverage] + [1] * 16


history = edge(0.5)
stabilized, raw = [], []
for coverage in [0.4, 0.6] * 4:
    current = edge(coverage)
    history = resolve(current, history)
    raw.append(coverage)
    stabilized.append(history[15])
    assert max(abs(history[x] - current[x]) for x in list(range(14)) + list(range(17, width))) < 0.006
raw_range = max(raw) - min(raw)
resolved_range = max(stabilized) - min(stabilized)
assert resolved_range < raw_range * 0.8, (raw_range, resolved_range)
current, old = edge(0.6), edge(0.4)
accepted = resolve(current, old)
rejected = resolve(current, old, depth=0.2, old_depth=0.8)
reset = resolve(current, old, valid=False)
disocclusion_error = max(abs(a - b) for a, b in zip(current, rejected))
reset_error = max(abs(a - b) for a, b in zip(current, reset))
assert abs(accepted[15] - current[15]) > 0.02, "fixture must expose stale blending"
assert disocclusion_error < 0.006 and reset_error < 0.006
print(json.dumps({"renderer": renderer, "rawEdgeRange": raw_range, "resolvedEdgeRange": resolved_range,
                  "disocclusionError": disocclusion_error, "resetError": reset_error}))
api("OSMesaDestroyContext", None, pointer)(context)
