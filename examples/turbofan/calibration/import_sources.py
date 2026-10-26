#!/usr/bin/env python3
"""Rebuild map tables and an optional ICAO row using only Python's stdlib.

The pinned MATLAB inputs are data: they are parsed, never executed.
Download the EASA workbook from the URL in data/jt9d_icao.json, then pass
--edb /path/to/workbook.xlsx to reproduce that observation snapshot too.
"""
import argparse
import hashlib
import json
import re
from pathlib import Path
from xml.etree import ElementTree as ET
from zipfile import ZipFile

DATA = Path(__file__).resolve().parents[1] / "cycle" / "data"
EXPECTED_EDB_SHA = "57a9ff572458ad3a3141afc1aea932b5faa5796d279f0ac74600b27869302530"


def maps():
    result = {}
    for name in ("Fan", "LPC", "HPC", "HPT", "LPT"):
        path = DATA / "tmats" / f"setup_{name}.m"
        source = re.sub(r"%[^\n]*", "", path.read_text()).replace("...", " ")
        arrays = {}
        for key, body in re.findall(r"MWS\.\w+\.(\w+)\s*=\s*\[(.*?)\];", source, re.S):
            arrays[key] = [float(x) for x in re.findall(r"[-+]?(?:\d*\.\d+|\d+)(?:[eE][-+]?\d+)?", body)]
        speed = arrays["NcVec"]
        coordinate = arrays.get("RlineVec", arrays.get("PRVec"))
        def matrix(key):
            if key not in arrays:
                return None
            values = arrays[key]
            width = len(coordinate)
            assert len(values) == len(speed) * width, (name, key)
            return [values[i:i+width] for i in range(0, len(values), width)]
        scalars = {}
        for key, value in re.findall(r"MWS\.\w+\.(s_\w+)\s*=\s*([^;]+);", source):
            assert re.fullmatch(r"[\d.eE+\- /\s]+", value), value
            terms = [float(x.strip()) for x in value.split("/")]
            assert len(terms) in (1, 2)
            scalars[key] = terms[0] if len(terms) == 1 else terms[0] / terms[1]
        result[name] = dict(name=name, speed=speed, coordinate=coordinate,
                            flow=matrix("WcArray"), efficiency=matrix("EffArray"),
                            pressure=matrix("PRArray"), scale=scalars,
                            sourceSHA256=hashlib.sha256(path.read_bytes()).hexdigest())
    (DATA / "maps.json").write_text(json.dumps(result, separators=(",", ":")) + "\n")


def observations(path):
    digest = hashlib.sha256(path.read_bytes()).hexdigest()
    if digest != EXPECTED_EDB_SHA:
        raise ValueError("workbook differs from the pinned 03/2026 source; review a new edition explicitly")
    ns = {"s": "http://schemas.openxmlformats.org/spreadsheetml/2006/main"}
    with ZipFile(path) as z:
        shared = ["".join(e.itertext()) for e in ET.fromstring(z.read("xl/sharedStrings.xml")).findall("s:si", ns)]
        rows = ET.fromstring(z.read("xl/worksheets/sheet3.xml")).findall("s:sheetData/s:row", ns)
        def values(row):
            out = {}
            for c in row:
                v = c.find("s:v", ns)
                if v is not None:
                    column = re.sub(r"\d", "", c.attrib["r"])
                    out[column] = shared[int(v.text)] if c.attrib.get("t") == "s" else float(v.text)
            return out
        header = values(rows[0])
        row = next(r for r in rows if values(r).get("A") == "1PW026")
        v = values(row)
        columns = "A C D E F G H I J K N M O BZ CA CB CC CE CG CI CJ CK CL CO CP CQ CR CT CU".split()
        result = dict(sourceURL="https://www.easa.europa.eu/en/downloads/131424/en",
                      sourcePage="https://www.easa.europa.eu/en/domains/environment/icao-aircraft-engine-emissions-databank",
                      edition="03/2026", workbookSHA256=digest,
                      sheet="Gaseous Emissions and Smoke", row=int(row.attrib["r"]),
                      cells={c+row.attrib["r"]: dict(column=header.get(c), value=v[c]) for c in columns if c in v},
                      engine="JT9D-7R4D / -7R4D1", uid="1PW026", ratedThrustN=v["I"]*1000,
                      pressureRatio=v["H"], bypassRatio=v["G"],
                      points=[dict(mode=mode, thrustFraction=fraction, fuelFlowKGS=v[c],
                                   role="calibration" if mode in ("takeoff", "approach", "idle") else "unfitted check")
                              for mode, fraction, c in [("takeoff", 1, "BZ"), ("climbout", .85, "CA"), ("approach", .3, "CB"), ("idle", .07, "CC")]],
                      attribution="ICAO Aircraft Engine Emissions Databank, hosted by EASA; manufacturer supplied data. Reproduced with source acknowledgement.")
    (DATA / "jt9d_icao.json").write_text(json.dumps(result, indent=2) + "\n")


def thermodynamics():
    path = DATA / "air-species.yaml"
    weights = {"N2": .78, "O2": .21, "Ar": .01}
    species = {}
    for block in re.split(r"(?=^- name: )", path.read_text(), flags=re.M):
        name = re.match(r"- name: (\w+)", block)
        if not name:
            continue
        ranges = [float(x) for x in re.search(r"temperature-ranges: \[([^]]+)\]", block).group(1).split(",")]
        arrays = [[float(x.strip()) for x in body.split(",")]
                  for body in re.findall(r"^    - \[([^]]+)\]", block, re.M | re.S)]
        assert ranges[0] <= 200 and ranges[-1] >= 3500
        species[name.group(1)] = dict(ranges=ranges, data=arrays)
    coefficients = [[sum(weights[n] * species[n]["data"][min(phase, len(species[n]["data"])-1)][j]
                         for n in weights) for j in range(7)] for phase in range(2)]
    result = dict(sourceURL="https://github.com/Cantera/cantera/blob/6853993ed60ffb212671aa7a1d6b79714f07c2db/data/nasa_gas.yaml",
                  speciesSnapshotSHA256=hashlib.sha256(path.read_bytes()).hexdigest(),
                  mixtureMoleFractions=weights, speciesRangesK={n: species[n]["ranges"] for n in weights},
                  temperatureRangeK=[200, 1000, 3500], coefficients=coefficients,
                  gasConstantJPerKGK=287.05,
                  note="Frozen air composition; air-equivalent combustion products. Not chemical equilibrium.")
    (DATA / "air.json").write_text(json.dumps(result, indent=2) + "\n")


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--edb", type=Path)
    args = parser.parse_args()
    maps()
    thermodynamics()
    if args.edb:
        observations(args.edb)
