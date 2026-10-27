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

## External RPM and station references

`public_validation.json` is a separate, manually transcribed reference snapshot.
It records the download SHA-256, PDF page/table location, engine variant, units
and applicability of each source. The numerical tables were checked against the
PDF pages, including OCR errors in the NASA pressure and idle-temperature cells.
None of these references changes the fitted profile or NASA map scaling.

- **FAA TCDS E3NE, revision 16, 25 July 2019:** [archived FAA PDF](https://web.archive.org/web/20200326073836id_/https://rgl.faa.gov/Regulatory_and_Guidance_Library/rgMakeModel.nsf/0/1f744c3819e0622a86258443003da885/$FILE/E3NE_Rev16.pdf), PDF p.5, Notes 1 and 2. Standard -7R4D N1/N2 maxima are 3770/8000 rpm; -7R4D1 maxima are 3810/8000 rpm. The source separately describes converted (DM)/(EM) variants. Note 2 also gives a 625 C takeoff total Tt7 maximum immediately after the LPT, mapped to model station 5. These are historical limit checks, not measured operating-speed observations or a claim about the latest operational limitations.
- **Osaka Metropolitan University Archives Newsletter No.14:** [repository PDF](https://omu.repo.nii.ac.jp/record/2001582/files/2024000887.pdf), PDF p.3, Figure 2, exhibit panel for JAL-donated JT9D-7R4D P717001. It gives gauge 100% RPM (3600/7807), nominal ground/flight idle N2 (4370/5465), and combustor-outlet/turbine-inlet temperatures (1271.1/1185.0 C) at 33.3 C and 213.5 kN uninstalled thrust. The installed 208.0 kN value includes a stated nacelle loss and is not substituted for the uninstalled rating. These are exhibit specifications, without a sensor/uncertainty record. The mappings to solver stations 4/41 are provisional. Nominal idle lacks a coupled ambient/fuel/bleed/load condition and is not assigned a validation error.
- **NASA/Pratt & Whitney Experimental Clean Combustor Program: Engine Test Results**, Roberts, Fiorentino and Greene: [NASA PDF](https://ntrs.nasa.gov/api/citations/19780003125/downloads/19780003125.pdf), Table V-3, printed p.111 / PDF p.21. Standard-day **JT9D-7A** generator reference conditions supply four fuel/thrust/T/P points, not measurements of the current -7R4D/D1 variant. Source combustor-inlet Tt4 maps to solver station 3. Use printed kg/h divided by 3600 and the SI pressure column (all rows x1e5 Pa). Idle temperature is **447 K**, not the OCR rendering 441 K; approach thrust is **61585 N**, not 81585 N. At takeoff, 21.68e5 Pa disagrees with the printed 323 psia; the mismatch is retained and disclosed.

The frozen hot-day comparison solves fuel at the stated temperature/thrust;
it does not tune parameters using the temperatures. Its fuel lies beyond the
interactive ground interval, and its RPM exceeds the reported maxima. This
is a model discrepancy with qualified specifications, not independent empirical
validation. NASA -7A comparisons are tagged as a variant mismatch. No full
matched measured RPM/station dataset with complete conditions was identified.

Reproduce the audit without network access:

```sh
GOWORK=off go run ./examples/turbofan/calibration --validate-public
```

JSON is emitted even when limits fail; the current expected exit status is 1.
The audit rejects `--fit` in the same run. Its generated report is retained at
`../../calibration/public-validation.json`. Teaching diagrams, the already-used
T-MATS priors and the investigated clearance/case-metal reports are recorded as
excluded candidates, with their reasons. They do not count as matched measured
validation points.
