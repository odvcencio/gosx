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
sources = json.loads(sys.stdin.read())
attach(program, shader(0x8B30, sources["taa"]))
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
    filtering = 0x2601 if unit in (0, 2) else 0x2600
    for option, value in [(0x2801, filtering), (0x2800, filtering), (0x2802, 0x812F), (0x2803, 0x812F)]:
        parameter(0x0DE1, option, value)
    api("glUniform1i", None, integer, integer)(location(program, name), unit)
temporal = api("glUniform4f", None, integer, floating, floating, floating, floating)
read = api("glReadPixels", None, integer, integer, integer, integer, uint, uint, pointer)


def resolve(current, history, depth=0.5, old_depth=0.5, valid=True, transparent=False, alpha=False):
    depth = [depth] * width if isinstance(depth, (float, int)) else depth
    old_depth = [old_depth] * width if isinstance(old_depth, (float, int)) else old_depth
    for unit, values in enumerate([current, depth, history, old_depth]):
        data = (floating * (width * height * 4))(*[v for _ in range(height) for value in values for v in [value, value, value, value if transparent and unit in (0, 2) else 1]])
        active(0x84C0 + unit)
        bind(0x0DE1, textures[unit])
        upload(0x0DE1, 0, 0x8814, width, height, 0, 0x1908, 0x1406, data)
    temporal(location(program, b"u_temporalParams"), 0.9, 1.25, 0.01, int(valid))
    api("glDrawArrays", None, uint, integer, integer)(0x0004, 0, 3)
    result = (floating * (width * height * 4))()
    read(0, 0, width, height, 0x1908, 0x1406, result)
    assert api("glGetError", uint)() == 0, "software TAA draw failed"
    return list(result)[3 if alpha else 0:width * 4:4]


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

# Rasterize a stationary half-plane with the same eight clip-space Halton
# translations as the renderer. Coverage and depth come from real geometry,
# rather than the uniform foreground-depth fixture above.
geometry_program = api("glCreateProgram", uint)()
attach(geometry_program, shader(0x8B31, """#version 300 es
precision highp float;
uniform mat4 u_projection;
uniform float u_edge;
void main() {
    vec2 corners[6] = vec2[6](vec2(0,-1),vec2(1,-1),vec2(1,1),vec2(0,-1),vec2(1,1),vec2(0,1));
    vec2 p = corners[gl_VertexID];
    gl_Position = u_projection * vec4(mix(u_edge, 2.0, p.x), p.y * 2.0, -2.0, 1);
}
"""))
attach(geometry_program, shader(0x8B30, """#version 300 es
precision highp float;
out vec4 color;
void main() { color = vec4(1); }
"""))
api("glLinkProgram", None, uint)(geometry_program)
api("glGetProgramiv", None, uint, uint, c.POINTER(integer))(geometry_program, 0x8B82, c.byref(status))
assert status.value, "coverage geometry did not link"
framebuffer = uint()
api("glGenFramebuffers", None, integer, c.POINTER(uint))(1, c.byref(framebuffer))
bind_framebuffer = api("glBindFramebuffer", None, uint, uint)
bind_framebuffer(0x8D40, framebuffer)
attachments = (uint * 2)()
api("glGenTextures", None, integer, c.POINTER(uint))(2, attachments)
for index, attachment in enumerate(attachments):
    bind(0x0DE1, attachment)
    parameter(0x0DE1, 0x2801, 0x2600)
    parameter(0x0DE1, 0x2800, 0x2600)
    upload(0x0DE1, 0, 0x8814 if index == 0 else 0x8CAC, width, height, 0,
           0x1908 if index == 0 else 0x1902, 0x1406, None)
    api("glFramebufferTexture2D", None, uint, uint, uint, uint, integer)(
        0x8D40, 0x8CE0 if index == 0 else 0x8D00, 0x0DE1, attachment, 0)
assert api("glCheckFramebufferStatus", uint, uint)(0x8D40) == 0x8CD5
bind_framebuffer(0x8D40, 0)


def halton(index, base):
    value, fraction = 0, 1
    while index:
        fraction /= base
        value += fraction * (index % base)
        index //= base
    return value


def jittered_projection(index):
    data = [1, 0, 0, 0, 0, 1, 0, 0, 0, 0, -11 / 9, -1, 0, 0, -20 / 9, 0]
    for column in range(4):
        data[column * 4] += (halton(index % 8 + 1, 2) - 0.5) * 2 / width * data[column * 4 + 3]
        data[column * 4 + 1] += (halton(index % 8 + 1, 3) - 0.5) * 2 / height * data[column * 4 + 3]
    return (floating * 16)(*data)


def rasterize(proj, edge_position=-2 / width):
    bind_framebuffer(0x8D40, framebuffer)
    api("glEnable", None, uint)(0x0B71)
    api("glClearColor", None, floating, floating, floating, floating)(0, 0, 0, 1)
    api("glClearDepth", None, c.c_double)(1)
    api("glClear", None, uint)(0x4000 | 0x0100)
    api("glUseProgram", None, uint)(geometry_program)
    matrix(location(geometry_program, b"u_projection"), 1, 0, proj)
    api("glUniform1f", None, integer, floating)(location(geometry_program, b"u_edge"), edge_position)
    api("glDrawArrays", None, uint, integer, integer)(0x0004, 0, 6)
    color = (floating * (width * height * 4))()
    depths = (floating * (width * height))()
    read(0, 0, width, height, 0x1908, 0x1406, color)
    read(0, 0, width, height, 0x1902, 0x1406, depths)
    api("glDisable", None, uint)(0x0B71)
    bind_framebuffer(0x8D40, 0)
    api("glUseProgram", None, uint)(program)
    return list(color)[0:width * 4:4], list(depths)[:width]


history, old_depths, previous = [0] * width, [1] * width, jittered_projection(0)
silhouette_raw, silhouette_resolved, silhouette_depths = [], [], []
for index in range(16):
    proj = jittered_projection(index)
    current, depths = rasterize(proj)
    matrix(location(program, b"u_projection"), 1, 0, proj)
    matrix(location(program, b"u_previousProjection"), 1, 0, previous)
    api("glUniform4f", None, integer, floating, floating, floating, floating)(
        location(program, b"u_temporalJitter"),
        (halton(index % 8 + 1, 2) - 0.5) / width, (halton(index % 8 + 1, 3) - 0.5) / height,
        (halton((index - 1) % 8 + 1, 2) - 0.5) / width, (halton((index - 1) % 8 + 1, 3) - 0.5) / height)
    history = resolve(current, history, depths, old_depths, valid=index > 0)
    if index >= 8:
        silhouette_raw.append(current[15])
        silhouette_resolved.append(history[15])
        silhouette_depths.append(depths[15])
    assert max(abs(history[x] - current[x]) for x in list(range(12)) + list(range(20, width))) < 0.006
    old_depths, previous = depths, proj
silhouette_range = max(silhouette_resolved) - min(silhouette_resolved)
assert min(silhouette_depths) < 0.999999 <= max(silhouette_depths), silhouette_depths
assert max(silhouette_raw) - min(silhouette_raw) == 1, silhouette_raw
assert silhouette_range < 0.25, (silhouette_raw, silhouette_resolved, silhouette_range)
# Premultiplied white over a transparent page must resolve coverage with color.
transparent_history, old_depths, previous = [0] * width, [1] * width, jittered_projection(0)
transparent_alpha, transparent_rgb = [], []
for index in range(16):
    proj = jittered_projection(index)
    current, depths = rasterize(proj)
    matrix(location(program, b"u_projection"), 1, 0, proj)
    matrix(location(program, b"u_previousProjection"), 1, 0, previous)
    temporal(location(program, b"u_temporalJitter"),
             (halton(index % 8 + 1, 2) - 0.5) / width, (halton(index % 8 + 1, 3) - 0.5) / height,
             (halton((index - 1) % 8 + 1, 2) - 0.5) / width, (halton((index - 1) % 8 + 1, 3) - 0.5) / height)
    alpha_values = resolve(current, transparent_history, depths, old_depths, valid=index > 0, transparent=True, alpha=True)
    transparent_history = resolve(current, transparent_history, depths, old_depths, valid=index > 0, transparent=True)
    if index >= 8:
        transparent_alpha.append(alpha_values[15]); transparent_rgb.append(transparent_history[15])
    assert max(abs(a - rgb) for a, rgb in zip(alpha_values, transparent_history)) < 0.006, "premultiplied coverage diverged"
    old_depths, previous = depths, proj
transparent_range = max(transparent_alpha) - min(transparent_alpha)
assert transparent_range < 0.25, transparent_alpha
# A genuinely uncovered region must lose its foreground history immediately.
current, depths = rasterize(previous, edge_position=0.75)
uncovered = resolve(current, history, depths, old_depths)
uncovered_error = max(abs(uncovered[x] - current[x]) for x in range(15, 20))
background_error = max(abs(value) for value in resolve([0] * width, history, 1, old_depths))
assert uncovered_error < 0.006 and background_error < 0.006
# Run the actual spatial fallback on the same foreground/clear-depth boundary.
# With an unjittered projection, an identity live custom pass cannot move coverage.
fxaa = api("glCreateProgram", uint)()
attach(fxaa, shader(0x8B31, vertex)); attach(fxaa, shader(0x8B30, sources["fxaa"]))
api("glLinkProgram", None, uint)(fxaa)
api("glGetProgramiv", None, uint, uint, c.POINTER(integer))(fxaa, 0x8B82, c.byref(status))
assert status.value, "FXAA fallback did not link"
fallback_values = []
for _ in range(8):
    current, _ = rasterize(projection)
    api("glUseProgram", None, uint)(fxaa)
    api("glUniform1i", None, integer, integer)(location(fxaa, b"u_texture"), 0)
    active(0x84C0); bind(0x0DE1, textures[0])
    data = (floating * (width * height * 4))(*[v for _ in range(height) for value in current for v in [value] * 4])
    upload(0x0DE1, 0, 0x8814, width, height, 0, 0x1908, 0x1406, data)
    api("glDrawArrays", None, uint, integer, integer)(0x0004, 0, 3)
    result = (floating * (width * height * 4))()
    read(0, 0, width, height, 0x1908, 0x1406, result)
    assert api("glGetError", uint)() == 0
    assert max(abs(result[x * 4] - result[x * 4 + 3]) for x in range(width)) < 0.006, "FXAA lost transparent coverage"
    fallback_values.append(result[15 * 4])
fallback_range = max(fallback_values) - min(fallback_values)
assert fallback_range == 0, fallback_values
print(json.dumps({"renderer": renderer, "rawEdgeRange": raw_range, "resolvedEdgeRange": resolved_range,
                  "disocclusionError": disocclusion_error, "resetError": reset_error,
                  "silhouetteRaw": silhouette_raw, "silhouetteResolved": silhouette_resolved,
                  "silhouetteRange": silhouette_range, "uncoveredError": uncovered_error,
                  "backgroundError": background_error, "transparentAlphaRange": transparent_range, "unjitteredFXAARange": fallback_range}))
api("OSMesaDestroyContext", None, pointer)(context)
