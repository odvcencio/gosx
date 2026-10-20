#!/usr/bin/env python3
"""Build the small, deterministic glTF meshes for the Blackglass Coast demo.

Coordinates follow the Studio SceneDoc. GoSX applies the water-zone offset
when it mounts each model. The assets use only core glTF 2.0 PBR materials.
"""

import json
import math
import pathlib
import random
import struct


ROOT = pathlib.Path(__file__).resolve().parents[1]
OUT = ROOT / "examples/gosx-docs/public/models/blackglass"
RNG = random.Random(31109)

MATERIALS = {
    "sand": ("#82674d", 0.96, 0.0, None),
    "wet-sand": ("#3c4040", 0.78, 0.0, None),
    "basalt": ("#0e171a", 0.68, 0.06, None),
    "basalt-edge": ("#232b2d", 0.8, 0.02, None),
    "ruin": ("#675847", 0.86, 0.0, None),
    "ruin-dark": ("#3a3632", 0.9, 0.0, None),
    "blackglass": ("#18272c", 0.28, 0.72, None),
    "bronze": ("#8d6742", 0.39, 0.66, None),
    "ember": ("#ff9d4e", 0.3, 0.05, "#ff6529"),
}


def rgba(hex_color):
    return [int(hex_color[i:i + 2], 16) / 255 for i in (1, 3, 5)] + [1]


def normal(a, b, c):
    u = [b[i] - a[i] for i in range(3)]
    v = [c[i] - a[i] for i in range(3)]
    n = [u[1] * v[2] - u[2] * v[1], u[2] * v[0] - u[0] * v[2], u[0] * v[1] - u[1] * v[0]]
    length = math.sqrt(sum(x * x for x in n)) or 1
    return [x / length for x in n]


class Mesh:
    def __init__(self):
        self.groups = {}

    def tri(self, material, a, b, c):
        group = self.groups.setdefault(material, {"positions": [], "normals": [], "indices": []})
        n = normal(a, b, c)
        start = len(group["positions"]) // 3
        for point in (a, b, c):
            group["positions"].extend(point)
            group["normals"].extend(n)
        group["indices"].extend((start, start + 1, start + 2))

    def quad(self, material, a, b, c, d):
        self.tri(material, a, b, c)
        self.tri(material, a, c, d)

    def rings(self, material, rings, cap_top=True):
        for lower, upper in zip(rings, rings[1:]):
            for i in range(len(lower)):
                j = (i + 1) % len(lower)
                self.quad(material, lower[i], lower[j], upper[j], upper[i])
        if cap_top:
            top = rings[-1]
            center = tuple(sum(p[i] for p in top) / len(top) for i in range(3))
            for i in range(len(top)):
                self.tri(material, center, top[i], top[(i + 1) % len(top)])

    def write(self, path):
        blob = bytearray()
        views, accessors, primitives = [], [], []
        used_materials = [key for key in MATERIALS if key in self.groups]

        def accessor(values, component_type, kind, count, extent=False):
            while len(blob) % 4:
                blob.append(0)
            offset = len(blob)
            fmt = "f" if component_type == 5126 else "I"
            blob.extend(struct.pack("<" + fmt * len(values), *values))
            view = len(views)
            views.append({"buffer": 0, "byteOffset": offset, "byteLength": len(blob) - offset, "target": 34962 if kind == "VEC3" else 34963})
            entry = {"bufferView": view, "componentType": component_type, "count": count, "type": kind}
            if extent:
                entry["min"] = [min(values[i::3]) for i in range(3)]
                entry["max"] = [max(values[i::3]) for i in range(3)]
            accessors.append(entry)
            return len(accessors) - 1

        for material in used_materials:
            group = self.groups[material]
            count = len(group["positions"]) // 3
            position = accessor(group["positions"], 5126, "VEC3", count, True)
            normals = accessor(group["normals"], 5126, "VEC3", count)
            indices = accessor(group["indices"], 5125, "SCALAR", len(group["indices"]))
            primitives.append({"attributes": {"POSITION": position, "NORMAL": normals}, "indices": indices, "material": used_materials.index(material), "mode": 4})

        materials = []
        for name in used_materials:
            color, roughness, metalness, emissive = MATERIALS[name]
            entry = {"name": name, "pbrMetallicRoughness": {"baseColorFactor": rgba(color), "metallicFactor": metalness, "roughnessFactor": roughness}, "doubleSided": True}
            if emissive:
                entry["emissiveFactor"] = rgba(emissive)[:3]
            materials.append(entry)
        document = {"asset": {"version": "2.0", "generator": "gosx Blackglass Coast deterministic mesh source"}, "scene": 0, "scenes": [{"nodes": [0]}], "nodes": [{"mesh": 0, "name": path.stem}], "meshes": [{"name": path.stem, "primitives": primitives}], "materials": materials, "buffers": [{"byteLength": len(blob)}], "bufferViews": views, "accessors": accessors}
        encoded = json.dumps(document, separators=(",", ":")).encode()
        encoded += b" " * (-len(encoded) % 4)
        blob.extend(b"\0" * (-len(blob) % 4))
        glb = struct.pack("<III", 0x46546C67, 2, 12 + 8 + len(encoded) + 8 + len(blob))
        glb += struct.pack("<I4s", len(encoded), b"JSON") + encoded
        glb += struct.pack("<I4s", len(blob), b"BIN\0") + blob
        path.write_bytes(glb)
        print(f"{path.relative_to(ROOT)}: {len(glb)} bytes, {sum(len(g['indices']) // 3 for g in self.groups.values())} triangles")


def beach():
    mesh = Mesh()
    columns, rows = 36, 14

    def point(i, j):
        x = -30 + i * 1.38
        t = j / rows
        shore = 4.3 + 1.1 * math.sin(x * 0.23) + 0.55 * math.sin(x * 0.47)
        z = shore + (25.5 - shore) * t
        y = -0.1 + 1.4 * t + 0.09 * math.sin(x * 0.41 + t * 7) * t
        return (x, y, z)

    for i in range(columns):
        for j in range(rows):
            a, b, c, d = point(i, j), point(i + 1, j), point(i + 1, j + 1), point(i, j + 1)
            mesh.quad("wet-sand" if j < 2 else "sand", a, b, c, d)
    for i in range(columns):
        a, b = point(i, 0), point(i + 1, 0)
        mesh.quad("wet-sand", (b[0], -1.1, b[2]), (a[0], -1.1, a[2]), a, b)
    return mesh


def mound(mesh, x, z, rx, rz, top, seed, material="basalt"):
    rng = random.Random(seed)
    segments = 16
    jitter = [rng.uniform(0.78, 1.12) for _ in range(segments)]
    heights = [rng.uniform(-0.38, 0.42) for _ in range(segments)]

    def ring(scale, base_y):
        return [(x + math.cos(2 * math.pi * i / segments) * rx * scale * jitter[i], base_y + heights[i], z + math.sin(2 * math.pi * i / segments) * rz * scale * jitter[i]) for i in range(segments)]

    mesh.rings(material, [ring(1.12, -2.2), ring(1.0, top * 0.5), ring(0.73, top)], False)
    crown = ring(0.73, top)
    peak = (x + rng.uniform(-0.4, 0.4), top + 0.24, z + rng.uniform(-0.4, 0.4))
    for i in range(segments):
        mesh.tri("basalt-edge" if i % 5 == 0 else material, peak, crown[i], crown[(i + 1) % segments])


def shelves():
    mesh = Mesh()
    mound(mesh, -21, -3, 9, 10, 3.9, 41)
    mound(mesh, 13, -9, 7, 8, 3.2, 43)
    # The camera starts above this ledge. Its low crest leaves the cove open.
    mound(mesh, 5, 13, 7.5, 4.3, 2.1, 47)
    for index, (x, y, z) in enumerate(((-23, 1.2, 4), (-20, 2.1, 7), (-16, 0.9, 9), (-8, 1.3, 13), (0, 1, 10), (13, 1.6, 2), (15, 1.2, -3), (10, 1, -11), (-3, 1.1, -14), (-13, 1.2, -16), (-22, 1, -10), (-26, 0.8, -2))):
        mound(mesh, x, z, 0.95 + (index % 3) * 0.25, 0.7 + (index % 4) * 0.18, max(0.1, y), 100 + index)
    rough_block(mesh, (-3.8, 0.34, 10.6), (2.8, 0.68, 1.4), 731, "basalt")
    return mesh


def rough_block(mesh, center, size, seed, material="ruin"):
    rng = random.Random(seed)
    x, y, z = center
    sx, sy, sz = size
    segments = 8
    def ring(scale, h):
        corners = ((-1, -0.7), (-0.7, -1), (0.7, -1), (1, -0.7), (1, 0.7), (0.7, 1), (-0.7, 1), (-1, 0.7))
        return [(x + cx * sx * 0.5 * scale + rng.uniform(-0.04, 0.04), h + rng.uniform(-0.025, 0.025), z + cz * sz * 0.5 * scale + rng.uniform(-0.04, 0.04)) for cx, cz in corners]
    mesh.rings(material, [ring(0.93, y - sy / 2), ring(1, y - sy * 0.3), ring(0.98, y + sy * 0.3), ring(0.85, y + sy / 2)])


def ruins():
    mesh = Mesh()
    for x in (2.6, 6.4):
        for level in range(5):
            rough_block(mesh, (x + 0.035 * (level % 2), 0.39 + level * 0.78, 5.3), (0.86, 0.78, 0.9), int(x * 100) + level, "ruin" if level % 3 else "ruin-dark")
    for index in range(5):
        rough_block(mesh, (2.75 + index * 0.88, 4.64 + 0.035 * (index % 2), 5.3), (0.9, 0.78, 1.04), 500 + index)
    for index, (x, z, scale) in enumerate(((1.7, 6.6, 0.48), (7.25, 6.7, 0.6), (5.1, 7.3, 0.37))):
        rough_block(mesh, (x, 0.19, z), (scale * 1.8, scale, scale), 600 + index, "ruin-dark")
    return mesh


def circular_ring(x, z, y, radius, segments=16, phase=0):
    return [(x + radius * math.cos(2 * math.pi * i / segments + phase), y, z + radius * math.sin(2 * math.pi * i / segments + phase)) for i in range(segments)]


def beacon():
    mesh = Mesh()
    x, z = 8, -4
    mesh.rings("ruin-dark", [circular_ring(x, z, -0.05, 2.8), circular_ring(x, z, 0.35, 2.75), circular_ring(x, z, 1.6, 2.15)])
    mesh.rings("blackglass", [circular_ring(x, z, 1.6, 1.15), circular_ring(x, z, 2.15, 1.23), circular_ring(x, z, 7.48, 0.55), circular_ring(x, z, 7.75, 0.68)])
    for y, radius in ((2.15, 1.27), (5.6, 0.85), (7.55, 0.77), (8.63, 0.8)):
        mesh.rings("bronze", [circular_ring(x, z, y - 0.09, radius), circular_ring(x, z, y + 0.1, radius)])
    mesh.rings("blackglass", [circular_ring(x, z, 7.7, 0.52), circular_ring(x, z, 8.72, 0.52)])
    for i in range(8):
        phase = 2 * math.pi * i / 8
        rib_x, rib_z = x + 0.56 * math.cos(phase), z + 0.56 * math.sin(phase)
        mesh.rings("bronze", [circular_ring(rib_x, rib_z, 7.7, 0.075, 6), circular_ring(rib_x, rib_z, 8.72, 0.075, 6)], False)
    mesh.rings("blackglass", [circular_ring(x, z, 8.72, 0.84), circular_ring(x, z, 8.95, 0.72)])
    return mesh


def main():
    OUT.mkdir(parents=True, exist_ok=True)
    for name, build in (("shore", beach), ("basalt", shelves), ("ruins", ruins), ("beacon", beacon)):
        build().write(OUT / f"{name}-v1.glb")


if __name__ == "__main__":
    main()
