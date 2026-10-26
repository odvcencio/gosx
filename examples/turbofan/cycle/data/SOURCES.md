# Public source snapshots

NASA's JT9D maps are model shapes, not identified manufacturer maps for the
ICAO variant. The observations and map priors have separate provenance.

- **Map tables and cycle priors:** [NASA T-MATS](https://github.com/nasa/T-MATS/tree/ad6e4d5d0d76c229db4eb72ca40ef58d5ddc4014/Trunk/TMATS_Examples/Example_JT9D/Simsetup), commit `ad6e4d5d0d76c229db4eb72ca40ef58d5ddc4014`. Source `.m` files and Apache-2.0 license are retained in `tmats/`; trailing blank lines were normalized. The generated `maps.json` records each retained file's SHA-256. Tables are imported row-major, with corrected speed as rows and R-line / turbine pressure coordinate as columns. A bracketed bilinear interpolator rejects extrapolation. MATLAB source is never executed.
- **Engine observations:** [ICAO Aircraft Engine Emissions Databank, hosted by EASA](https://www.easa.europa.eu/en/domains/environment/icao-aircraft-engine-emissions-databank), edition 03/2026, [workbook](https://www.easa.europa.eu/en/downloads/131424/en), SHA-256 `57a9ff572458ad3a3141afc1aea932b5faa5796d279f0ac74600b27869302530`. Sheet `Gaseous Emissions and Smoke`, row **555**, UID **1PW026**, **JT9D-7R4D, -7R4D1**. `jt9d_icao.json` retains exact cells, headers, values, data status and test metadata. Status C means certification data; the entry is marked corrected as Annex 16. This is a grouped engine entry, not telemetry from one serial-numbered engine. Source acknowledgement follows [EASA's reproduction terms](https://www.easa.europa.eu/en/copyright-disclaimer). The complete workbook is not vendored.
- **LTO settings:** Takeoff 100%, climbout 85%, approach 30%, idle 7% of rated static thrust, following the [FAA AEDT technical manual](https://rosap.ntl.bts.gov/view/dot/9986/dot_9986_DS1.pdf). Target thrusts are these prescribed fractions times published rated thrust; fuel flows are databank observations. They are not four independent instrumented force measurements with supplied uncertainty intervals.
- **Gas properties:** [Cantera `data/nasa_gas.yaml`](https://github.com/Cantera/cantera/blob/6853993ed60ffb212671aa7a1d6b79714f07c2db/data/nasa_gas.yaml), commit `6853993ed60ffb212671aa7a1d6b79714f07c2db`. `air-species.yaml` retains the selected N2/O2/Ar species blocks, each valid from 200 K; `air.json` retains mole fractions and their weighted NASA7 coefficients. `CANTERA-LICENSE.txt` retains the BSD license. The mixture is frozen 78% N2 / 21% O2 / 1% Ar. The model uses R = 287.05 J/(kg K), 200–3500 K, and treats combustion products as air-equivalent gas. It does not implement CEA, dissociation or changing composition.

To regenerate the observations after downloading the pinned workbook:

```sh
python3 examples/turbofan/calibration/import_sources.py --edb /path/to/workbook.xlsx
```

Without `--edb`, maps and gas coefficients are regenerated from the retained source data.
The importer rejects a workbook with a different hash; new editions need
explicit review. No third-party Python packages are required.

To reproduce the fit and every check, without network access:

```sh
GOWORK=off go run ./examples/turbofan/calibration --fit
```

Takeoff plus published pressure/bypass ratios reconstruct the rated cycle;
approach fits one common low-speed efficiency correction. Idle fits a second correction below map speed 0.5. Climbout
is excluded from parameter fitting. The public check values were inspected
during development: this is an unfitted comparison, not blind validation.

## Modeled idle extension

The retained NASA tables do not cover the matched idle point. NASA's
[T-MATS workshop proceedings](https://ntrs.nasa.gov/citations/20180002976),
NASA/CP-2018-219785, printed p.56 (PDF p.62), identify missing idle fan-map
coverage. The public [NASA map-extension study](https://ntrs.nasa.gov/api/citations/20160010065/downloads/20160010065.pdf),
section III.A, discusses extensions toward unity pressure ratio and zero flow.

The equations in `../low_speed.go` are this example's bounded engineering
assumptions anchored to those public tables. They are not additional NASA test
measurements or an implementation of the papers' complete procedures. Fan/LPC
low-speed similarity and LPT swallowing/efficiency assumptions are documented in
[MODEL.md](../../MODEL.md). At calibrated idle, Fan and LPT use these extensions.
The ICAO idle fuel observation and 7% reference-thrust setting explicitly enter
the fit. Idle is calibration evidence; climbout is the sole unfitted LTO check.
Manufacturer maps, actual idle RPM, bleed and variable-vane schedules remain
unidentified by these public data.
