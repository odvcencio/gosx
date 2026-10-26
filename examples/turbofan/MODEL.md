# Turbofan thermodynamic models

The workbench has two steady, one-dimensional models: a generic **cycle design** calculator and a **JT9D mapped ground profile** calibrated to public data. The browser, server and native checks execute the same Go package, `cycle`.

The 3D geometry and slowed rotor speeds remain anatomical illustrations. The mapped model estimates shaft RPM from its maps; those estimates are not used as animation speeds and have no independent RPM validation. Neither model predicts shaft transients. In generic cycle design, every change sizes a new engine point. In JT9D mode, geometry and map scalars stay fixed while fuel command changes.

## JT9D mapped ground profile

Open `/?model=jt9d` or select **JT9D maps**. This profile targets the grouped
**JT9D-7R4D / -7R4D1** entry, ICAO UID **1PW026**, in the EASA-hosted 03/2026
workbook. The rated thrust is 213.5 kN, published pressure ratio 23.4 and bypass
ratio 5. The reported fuel flows are 2.055, 1.678, 0.6593 and 0.2054 kg/s at
100%, 85%, 30% and 7% rated static thrust. Source cells, workbook hash,
licenses and reproducible import steps are in [data/SOURCES.md](cycle/data/SOURCES.md).

Five bilinear component maps come from the public NASA T-MATS JT9D example:
fan, booster/LPC, HPC, HPT and LPT. These are **NASA model map shapes scaled to
a public engine target**, not the manufacturer's proprietary maps. Corrected
flow uses `Wc = W sqrt(Tt/288.15)/(Pt/101325)` and corrected speed uses
`Nc = N/sqrt(Tt/288.15)`. Speed and flow are scaled to the reconstructed rated
point; pressure rise scales as `PR = 1 + sPR (PRmap − 1)`. Efficiency uses the
published NASA efficiency scale. Both interpolation coordinates must remain
inside the supplied tables; extrapolation is rejected.

The rated reconstruction uses published thrust/fuel, pressure and bypass
ratios. Fan/booster pressure ratios and component efficiencies are NASA-model
priors at map corrected speed 1 and R-line 2. HPT/LPT reference coordinates
are speed 100 and pressure coordinates 5 / 6.4. The shaft-speed references
are model priors, not measured engine RPM. For fixed fuel, a scalar airflow
solve closes rated thrust. Two mathematical roots occur; the root whose core
area is closest in log space to NASA's 862.88 in² core-nozzle prior is selected.
This yields about **650.89 kg/s airflow**, **0.57240 m² core area** and
**1.43581 m² bypass area**. The areas then remain fixed.

At each fuel command, seven unknowns are solved together: both shaft speeds,
fan/LPC/HPC R-lines, and both turbine pressure coordinates. Seven residuals
close HPC/HPT/LPT flow, both nozzle capacities, and both shaft powers.
Fan and booster flow follow their maps; the bypass split is an output.
A damped least-squares Newton method with fuel continuation stops at maximum
normalized matching residual 1e-9. A failed or out-of-map solution is cleared
from the UI, never presented as a plausible operating point.

Gas enthalpy and entropy use temperature-dependent NASA7 frozen-air
properties, with small continuity offsets at 1000 K. The mixture is 78% N2,
21% O2 and 1% Ar by mole, with R = 287.05 J/(kg K), over 200–3500 K.
Combustion products remain **air-equivalent gas**, not chemical-equilibrium
products. Compression and expansion use isentropic entropy changes and
enthalpy-based efficiencies. Nozzles solve the sonic energy condition using
the local heat capacity; fixed-area mass capacity, momentum and pressure
thrust are all included.

The compressor supplies 9% cooling bleed: 5.5% rejoins at the HPT inlet,
3.5% after the HPT. Enthalpy is conserved through both mixing steps. Burner
pressure recovery is 0.945, combustion and shaft efficiencies 0.99, LHV
43 MJ/kg. These are declared model priors. No accessory extraction or
variable-geometry schedule is identified from the public observations.

The first rated-only fit overpredicted approach thrust by 23%. Approach is
therefore a **second calibration point**, fitting a single shared low-speed
fan/booster efficiency correction:

`eta = eta_map sEta [1 − k max(0, 1 − Nc_map)²]`, with `k = 1.0665884689`.

It leaves the rated point unchanged. This aggregate correction can absorb
multiple model discrepancies; it is not a measurement of real fan loss,
booster loss, deterioration or variable geometry. `data/profile.json` records
the fit protocol. No climbout or idle fuel observation enters the fit.

| ICAO point | Role | Fuel kg/s | Reference kN | Predicted kN | Thrust error |
| --- | --- | ---: | ---: | ---: | ---: |
| Takeoff 100% | Calibration | 2.0550 | 213.500 | 213.500 | ≈0% |
| Climbout 85% | Unfitted check | 1.6780 | 181.475 | 180.484 | −0.546% |
| Approach 30% | Calibration | 0.6593 | 64.050 | 64.050 | ≈0% |
| Idle 7% | Unfitted check | 0.2054 | 14.945 | Unavailable | No converged mapped point |

Errors compare **predicted thrust at the reported fuel flow** with the
prescribed ICAO thrust fraction times rated thrust. The source provides no
uncertainty interval for these aggregate fuel values. Public check values
were inspected during development: this is an unfitted check, not blind
validation. The initial 23% error is retained here to make the calibration
scope explicit. A sparse fit does not uniquely identify five component maps.

The UI locks mapped mode to sea-level static conditions. There is no
experimental validation of flight conditions, station measurements, spool RPM,
surge margin, cooling schedules or transients. Idle remains unsupported;
there is no synthetic replacement point. The API's altitude/Mach capability
is an unvalidated model extrapolation and is not exposed as a calibrated
flight simulation.

Reproduce the fit and all checks, without network access:

```sh
GOWORK=off go run ./examples/turbofan/calibration --fit
GOWORK=off go test ./examples/turbofan/cycle
```

The stored profile is checked against recomputation. An operating-line test
checks 21 points between approach and takeoff for monotonic thrust, map-domain
membership, fixed nozzle areas, mass/energy conservation and shaft balances.
The [calibration report](calibration/report.json) retains the numerical
results. Browser checks compare native and WASM predictions and CSV exports.

## Generic cycle design

This mode keeps prescribed pressure/bypass ratios and corrected airflow.
It is useful for design comparisons, without a real-engine calibration.

### Inputs and assumptions

| Quantity | Default | Supported range |
| --- | ---: | ---: |
| Geopotential altitude | 0 m | 0–11,000 m |
| Flight Mach | 0 | 0–0.85 |
| Burner exit / turbine inlet total temperature | 1600 K | 1000–1800 K |
| Overall pressure ratio, including fan | 30 | 5–40 |
| Fan pressure ratio | 1.5 | 1.1–1.8 |
| Bypass/core air mass ratio | 8 | 1–12 |

`DefaultModel()` prescribes fan/compressor/turbine isentropic efficiencies of
0.90/0.88/0.91, shaft and burner efficiencies of 0.99, inlet and burner total
pressure recoveries of 0.99 and 0.95, and fuel lower heating value of 43 MJ/kg.
Corrected **core** airflow is 40 kg/s at 288.15 K and 101325 Pa, referenced to
inlet total conditions. It is a boundary condition, rather than a predicted
compressor-map flow. `Model` exposes these assumptions for native studies.

The gas is calorically perfect throughout: gamma = 1.4, R = 287.05 J/(kg K)
and cp = gamma R / (gamma − 1) = 1004.675 J/(kg K), including the hot stream.
This omits temperature-dependent properties, dissociation, cooling, bleed,
accessory loads and duct losses beyond the stated recoveries. There are no
component maps, surge margin, stall, blade stress, acoustics, or CFD.

### Equations

All internal units are SI. Temperatures and pressures below are stagnation
(total) quantities unless stated otherwise. Stations: 2 inlet, 13 fan exit,
3 compressor exit, 4 burner exit, 45 HP turbine exit, 5 LP turbine exit,
19 bypass nozzle exit and 9 core nozzle exit.

For ISA tropospheric geopotential altitude h, ambient temperature is
`Ta = 288.15 − 0.0065 h` and pressure is
`pa = 101325 (Ta/288.15)^[9.80665/(R × 0.0065)]`.
Ram temperature ratio is `tau = 1 + (gamma − 1) M²/2`.
The inlet has `T2 = Ta tau` and `p2 = pa tau^[gamma/(gamma − 1)] × recovery`.
Corrected flow gives `mc = mc_corrected (p2/101325)/sqrt(T2/288.15)`;
bypass flow is `mb = BPR mc`. This flow prescription is held throughout a
design study and does not enforce a fixed inlet capture area.

For either fan or compressor, with pressure ratio pi and efficiency eta,
`Tout/Tin = 1 + [pi^((gamma − 1)/gamma) − 1]/eta`.
The core compressor ratio is OPR/FPR. The fan acts on **both** air streams;
the compressor acts only on core air.

Fuel mass is included. Burner energy conservation gives
`f = cp (T4 − T3)/(eta_b LHV − cp T4)`, and `mf = f mc`.
The HP shaft load is `W_HP = mc cp (T3 − T13)`.
The LP shaft load is `W_LP = (mc + mb) cp (T13 − T2)`.
For each turbine, `deltaT = W_load/[eta_shaft (mc + mf) cp]` and
`pout/pin = [1 − deltaT/(eta_t Tin)]^[gamma/(gamma − 1)]`.
Both shaft work balances therefore close, including mechanical losses.

Each ideal convergent nozzle has exit pressure
`pe = max(pa, pt [2/(gamma + 1)]^[gamma/(gamma − 1)])`.
Exit temperature is `Te = Tt (pe/pt)^((gamma − 1)/gamma)` and velocity is
`Ve = sqrt[2 cp (Tt − Te)]`. Flow is sonic when choked, otherwise subsonic.
The necessary exit area is `Ae = mdot R Te/(pe Ve)`.
Choked flow retains its pressure thrust, `(pe − pa) Ae`; it is not
artificially expanded to ambient pressure in a convergent nozzle.

The net thrust is
`F = (mc + mf) V9 + mb V19 − (mc + mb) Vflight + (p9 − pa) A9 + (p19 − pa) A19`.
TSFC is `mf/F`, defined only when F is positive. UI units are mg/(N s).
The CSV also records the separate stream thrusts, model inputs, assumptions,
areas, station conditions and conservation residuals at numerical precision.

The model rejects insufficient heating, a turbine load that cannot be
supplied, or nozzle total pressure at/below ambient. The UI clears results
for infeasible points and disables export. It does not clamp those states
into plausible-looking positive thrust. Negative net thrust is allowed
when both nozzles can discharge; its TSFC is explicitly undefined.

### Generic verification and limits

An independent closed-form ideal-cycle benchmark sets all efficiencies and
recoveries to 1, zero flight speed, bypass ratio 4, fan temperature ratio
11/10, overall compression temperature ratio 2, and `T4/T2 = 4`.
Choosing `LHV = 104 cp T2` makes `f = 1/50`. The combined shaft loads are
`[0.9 + 5(0.1)] mc cp T2 = 1.4 mc cp T2`, giving
`T5/T2 = 4 − 1.4/1.02 = 134/51`. Ideal expansion then gives
`p5/pa = (67/51)^3.5`. The core nozzle is sonic with
`T9/T2 = (5/6)(134/51)` and `p9/pa = (335/306)^3.5`;
the bypass is unchoked with `V19 = sqrt(0.2 cp T2)`.
The test compares these results and the complete momentum/pressure thrust
against the component solver, without using its intermediate calculations.

The envelope test samples all 729 combinations of three values for each of
the six inputs: 506 points are feasible and 223 rejected. Feasible points
close mass, burner energy, both shaft balances and whole-cycle energy to
relative tolerance 1e-12, and satisfy nozzle continuity and momentum balance.
Additional checks cover choking continuity, heat-input trends, altitude,
airflow scaling, invalid inputs, and CSV contents. The default generic point
calculates 99.481 kN net thrust and 0.746732 kg/s fuel; these are model outputs,
not reference data for a manufactured engine.

These generic-mode checks establish equations and implementation only. The
generic mode has no engine calibration. A full-envelope mapped model needs
additional flight and station data, gas-composition effects, identified
variable-geometry/cooling schedules, and engine-specific component maps.
Transient shaft dynamics would then require torque/inertia and control
models, rather than a cosmetic change to animation speed.

## Primary references

The implementation is original Go code using public textbook relations,
not a port of NASA EngineSim or T-MATS.

- [NASA compressor thermodynamics](https://www.grc.nasa.gov/www/k-12/airplane/compress.html): pressure, temperature, efficiency and compression work.
- [NASA burner thermodynamics](https://www.grc.nasa.gov/www/k-12/airplane/burnth.html): energy balance including added fuel mass.
- [NASA power turbine thermodynamics](https://www.grc.nasa.gov/www/k-12/airplane/powtrbth.html): turbine work and pressure ratio.
- [NASA isentropic flow](https://www.grc.nasa.gov/www/k-12/airplane/isentrop.html) and [mass-flow choking](https://www.grc.nasa.gov/www/k-12/airplane/mflchk.html): total/static relations and sonic limit.
- [NASA thrust equation](https://www1.grc.nasa.gov/beginners-guide-to-aeronautics/thrust-force/) and [turbofan thrust](https://www.grc.nasa.gov/www/k-12/BGP/turbfan.html): momentum and pressure forces, separate flow streams.
- [NASA-hosted atmosphere derivation](https://ntrs.nasa.gov/api/citations/19950007312/downloads/19950007312.pdf): tropospheric lapse rate, sea-level conditions and hydrostatic pressure relation.
- [NASA T-MATS](https://github.com/nasa/T-MATS): a reference for the map-based matching and transient models that lie beyond this example.
