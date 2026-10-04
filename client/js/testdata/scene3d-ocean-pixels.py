"""Rasterize the shipped ocean fragment shader with Mesa's CPU rasterizer."""
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
width, height = 64, 64
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


sources = json.loads(sys.stdin.read())
vertex = """#version 300 es
precision highp float;
out vec3 v_world; out vec3 v_normal; out float v_jacobian; out float v_crest; out float v_depth0;
void main() {
 vec2 p = vec2(float((gl_VertexID << 1) & 2), float(gl_VertexID & 2));
 gl_Position = vec4(p * 2.0 - 1.0, 0.0, 1.0);
 v_world = vec3(p.x * 4.0 - 2.0, 0.0, p.y * 4.0 - 2.0);
 v_normal = vec3(0.0, 1.0, 0.0); v_jacobian = 1.0; v_crest = 0.0; v_depth0 = 1.0;
}
"""
program = api("glCreateProgram", uint)()
attach = api("glAttachShader", None, uint, uint)
attach(program, shader(0x8B31, vertex))
attach(program, shader(0x8B30, sources["fragment"]))
api("glLinkProgram", None, uint)(program)
status = integer()
api("glGetProgramiv", None, uint, uint, c.POINTER(integer))(program, 0x8B82, c.byref(status))
assert status.value, "Ocean shader did not link"
api("glUseProgram", None, uint)(program)
api("glViewport", None, integer, integer, integer, integer)(0, 0, width, height)
location = api("glGetUniformLocation", integer, uint, c.c_char_p)
texture = uint()
api("glGenTextures", None, integer, c.POINTER(uint))(1, c.byref(texture))
api("glBindTexture", None, uint, uint)(0x0DE1, texture)
api("glTexParameteri", None, uint, uint, integer)(0x0DE1, 0x2801, 0x2600)
api("glTexParameteri", None, uint, uint, integer)(0x0DE1, 0x2800, 0x2600)
texel = (c.c_ubyte * 4)(255, 255, 255, 255)
api("glTexImage2D", None, uint, integer, integer, integer, integer, integer, uint, uint, pointer)(0x0DE1, 0, 0x1908, 1, 1, 0, 0x1908, 0x1401, texel)
api("glUniform1i", None, integer, integer)(location(program, b"u_bathymetry"), 0)
counts = {}
for name, values in sources["uniforms"].items():
 data = (floating * len(values))(*values)
 api("glUniform4fv", None, integer, integer, c.POINTER(floating))(location(program, b"u_ocean[0]"), 35, data)
 api("glClearColor", None, floating, floating, floating, floating)(0, 0, 0, 0)
 api("glClear", None, uint)(0x4000)
 api("glDrawArrays", None, uint, integer, integer)(0x0004, 0, 3)
 api("glFinish", None)()
 api("glReadPixels", None, integer, integer, integer, integer, uint, uint, pointer)(0, 0, width, height, 0x1908, 0x1401, pixels)
 counts[name] = sum(pixels[i] != 0 for i in range(3, len(pixels), 4))
assert counts["dry"] == 0, f"Dry bathymetry regained coverage: {counts}"
assert counts["wet"] == width * height, f"Wet control did not draw: {counts}"
api("glDeleteProgram", None, uint)(program)
api("OSMesaDestroyContext", None, pointer)(context)
print(json.dumps(counts))
