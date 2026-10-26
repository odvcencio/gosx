# Turbofan cycle model

This is a steady, one-dimensional **design-point** model of an unmixed,
two-spool turbofan. It computes thermodynamic states, fuel flow, both shaft
loads, two convergent nozzles and net thrust. It supports cycle comparisons
and energy accounting, without claiming calibrated engine performance.
The browser and native tests execute the same Go package, `cycle`.

The 3D geometry and slowed rotor speeds are anatomical illustrations. No
calculated RPM, shaft inertia or relationship between those shapes and the
calculated flow areas is asserted. Changing a slider defines a new design
point; nozzle areas resize at each point. This is not fixed-engine throttle
response or an off-design component-matching solution.

## Inputs and assumptions

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

## Equations

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

## Verification and limits

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

This establishes equation and implementation checks. **No experimental or
manufacturer data calibration has been performed.** Matching an actual engine
requires suitable gas properties, component maps, fixed flow areas, cooling
and bleed, independent operating points and an off-design matching solver.
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
