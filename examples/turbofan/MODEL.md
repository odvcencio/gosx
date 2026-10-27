# Turbofan thermodynamic models

The workbench has two steady component-cycle models: a generic **cycle design** calculator and a **JT9D mapped ground profile** calibrated to public data. The browser, server and native checks execute the same Go package, `cycle`.

The mapped model is a numerical component solver: it iterates seven unknowns to close seven flow, shaft-power and fixed-nozzle constraints. It does not solve a three-dimensional fluid field. Its schematic geometry supplies no aerodynamic coefficients. The renderer receives solved N1/N2 speeds at a common **1/800 speed reduction**, preserving their ratio, and a gas-temperature color tint. That tint is not a prediction of metal temperature or flame appearance. Changing fuel preserves the current rotor pose, camera, canvas and pause state. Neither model integrates shaft inertia or predicts acceleration transients. Generic design-mode rotor speeds remain illustrative.

## JT9D mapped ground profile

Open `/?model=jt9d` or select **JT9D maps**. The profile targets the grouped **JT9D-7R4D / -7R4D1** entry, ICAO UID **1PW026**, in the EASA-hosted 03/2026 workbook. Rated thrust is 213.5 kN, published pressure ratio 23.4 and bypass ratio 5. Reported fuel flows are 2.055, 1.678, 0.6593 and 0.2054 kg/s at 100%, 85%, 30% and 7% rated static thrust. Exact source cells, workbook hash, licenses and import instructions are in [data/SOURCES.md](cycle/data/SOURCES.md).

Five bilinear component maps come from NASA's public T-MATS JT9D example: fan, booster/LPC, HPC, HPT and LPT. These are **NASA model shapes**, not proprietary manufacturer maps for the databank variant. The retained tables are unchanged. Corrected speed uses rows; compressor R-line or turbine pressure coordinate uses columns. Table interpolation rejects extrapolation. Separately identified, bounded engineering extensions provide missing low-speed coverage; dashed plot curves and per-component `extended` flags distinguish them.

Rated fuel/thrust and published OPR/BPR reconstruct one design point. The heat/work equations admit two airflow roots; the model selects the core nozzle area closest in log space to NASA's 862.88 in² geometry prior. It then fixes both nozzle areas and map scales. Rated flow is approximately 650.895 kg/s, with core/bypass areas of 0.572401 / 1.435810 m².

At each fuel command, a damped Newton/Levenberg–Marquardt iteration solves **N1, N2, three compressor R-lines and two turbine pressure coordinates**. Seven normalized residuals enforce HPC/HPT/LPT flow matching, HP/LP shaft balance and the two fixed-area nozzle capacities. The maximum residual must be below **1e-9**. Fuel continuation initializes each solve; numerical outputs are never replaced by interpolation between calibration thrusts.

Gas properties use NASA7 frozen air: 78% N2, 21% O2 and 1% Ar by mole, R = 287.05 J/(kg K), 200–3500 K. Compression/expansion use entropy and enthalpy changes; convergent nozzles include variable heat capacity, choking, mass capacity, momentum and pressure thrust. Combustion products remain air-equivalent gas rather than chemical-equilibrium products. Compressor cooling bleed is 9%: 5.5% rejoins at HPT inlet, 3.5% after HPT, conserving enthalpy. Burner recovery is 0.945; combustion and shaft efficiencies are 0.99; fuel LHV is 43 MJ/kg. These remain declared priors, with no identified accessory or variable-geometry schedule.

### Low-speed coverage and calibration

NASA's [T-MATS workshop report](https://ntrs.nasa.gov/citations/20180002976), printed p.56 / PDF p.62, explicitly identifies missing idle fan-map coverage. NASA's [map-extension study](https://ntrs.nasa.gov/api/citations/20160010065/downloads/20160010065.pdf), section III.A, describes extension toward unity pressure ratio and zero flow. Those sources motivate the approach; they do **not** supply the new map entries or the formulas chosen here.

The implemented engineering assumptions are:

- **Fan/LPC lower-speed lines:** hold the lowest published line's efficiency prior, scale corrected flow by `q = Nc/Nc_min`, and pressure-ratio excess by `q²`. Existing R-line limits stay in force. The modeled speed floor is 0.2; Fan/LPC published floors are 0.5/0.3.
- **LPT:** below its published speed floor of 60, retain boundary swallowing capacity. Below its pressure-coordinate floor of 3, scale flow by the Stodola factor `sqrt[(1 − PR⁻²)/(1 − PR_boundary⁻²)]`, using the scaled physical pressure ratios. Efficiency uses a bounded blade-speed/ideal-jet-speed parabola, `eta_boundary × clamp[u(2−u), 0.05, 1]`. Here `u = min(1,Nc/60)`, multiplied below the PR boundary by `sqrt[(1−PR_boundary⁻⁰·²⁵)/(1−PR⁻⁰·²⁵)]`. These are shape priors, not reconstructed blade geometry. The modeled domain is `20 ≤ Nc ≤ 120`, `1.05 ≤ pressure coordinate ≤ 8`.
- HPC and HPT retain their original domains. No below-unity compressor operation, startup, shutdown or windmilling is supported. Fuel commands are restricted to 0.2054–2.055 kg/s.

Approach fits the common fan/booster efficiency correction `1 − k max(0,1−Nc_map)²`, with **k = 1.0665884689267835**. Idle then fits one additional factor `1 + a max(0,1−Nc_map/0.5)²`, with **a = 1.8115930073145137**. The second correction term and its first derivative vanish at map speed 0.5, leaving approach, climbout and takeoff unchanged. These corrections absorb aggregate model discrepancy; they do not identify measured component degradation, variable vanes, bleed schedules or manufacturer efficiency curves.

At calibrated idle, Fan and LPT use modeled extensions; LPC/HPC/HPT remain inside their published table domains. The data do not independently identify this choice of low-speed shapes or the inferred internal states. Idle is now explicitly a **third fitted point**.

| ICAO point | Role | Fuel kg/s | Reference kN | Predicted kN | Thrust error |
| --- | --- | ---: | ---: | ---: | ---: |
| Takeoff 100% | Calibration | 2.0550 | 213.500 | 213.500 | ≈0% |
| Climbout 85% | Unfitted check | 1.6780 | 181.475 | 180.484 | −0.546% |
| Approach 30% | Calibration | 0.6593 | 64.050 | 64.050 | ≈0% |
| Idle 7% | Calibration; modeled low-speed maps | 0.2054 | 14.945 | 14.945 | ≈0% |

Errors compare predicted thrust at the reported fuel flow with prescribed ICAO thrust fractions times rated static thrust. They are not four independent force measurements with supplied uncertainty intervals. Climbout is excluded from parameter fitting; its public value was inspected during development, so this is an **unfitted check, not blind validation**. A three-point calibration does not uniquely identify five maps or establish accuracy between the fitted points.

The UI locks mapped mode to sea-level static conditions. Flight conditions, station measurements, actual shaft RPM, surge margin, cooling schedules and transients have no experimental validation. The API's altitude/Mach behavior remains unvalidated and is not exposed as calibrated flight operation.

### Reproduction and numerical checks

```sh
GOWORK=off go run ./examples/turbofan/calibration --fit
GOWORK=off go test ./examples/turbofan/cycle
```

The stored profile is checked against recomputation. An **81-point idle-to-takeoff sweep** checks monotonic thrust, declared map domains, fixed nozzle areas and mass/energy/shaft closure. A geometry-perturbation check rematches an intermediate 0.4 kg/s fuel command after a 2% bypass-nozzle-area change: thrust changes from approximately 33.525 to 33.821 kN while the matching residual still closes. This distinguishes a component solve from a fuel-to-thrust lookup. It proves numerical response to the equations, not real-engine accuracy.

The [calibration report](calibration/report.json) retains predicted states, map-region flags and residuals. Browser checks compare native/WASM outputs, idle CSV, control synchronization, actual rendered spool motion, temperature tint, retained pose/camera/canvas and reduced-motion behavior.

## Engineering inspector

The live inspector exposes station thermodynamics, component loading, map coordinates,
nozzle discharge, fuel and shaft-work ledgers, thrust decomposition and all seven matching
residuals. It derives quantities from the current solved point. These are model estimates,
with SI units and conventional shaft speeds in rpm. Public observations remain separate in
the calibration comparison.

- Station enthalpy uses the solver's gas model relative to 288.15 K. Specific entropy is
  `s(T) − s(288.15 K) − R ln(P/101325 Pa)`. Static nozzle exit states are identified;
  corrected total flow is omitted there. Station 45a identifies the modeled HPT outlet
  before the second cooling mix; 45 is the mixed LPT inlet.
- Signed component gas power is `ṁ [h_out − h_in]`: positive for compression, negative for
  expansion. HP/LP load torque is shaft power divided by `2π N/60`; its accuracy depends
  on the unvalidated model-estimated RPM. Shaft work is internal transfer, not an additional
  energy output to add to exhaust energy.
- Whole-engine energy closure uses inlet and exhaust **total** enthalpy, heat delivered
  by combustion and shaft loss. Exhaust static enthalpy plus jet kinetic power reproduces
  exhaust total enthalpy within the matching residual. The kinetic-power readout is not
  a claimed thermal efficiency. Thrust power `F_net V_flight` is zero at static conditions.
- Stream thrust is `ṁ_exit V_exit − ṁ_air V_flight + (P_exit−P_ambient) A`. Fuel mass is
  included in core exit momentum. Nozzle diagnostics report NPR, sonic critical NPR,
  their ratio, Mach, velocity, density, sound speed and mass flux. Variable gas properties
  determine the sonic threshold in mapped mode.
- The temperature–entropy graphic follows an open, air-equivalent stream with fuel and
  cooling addition; it is not a closed-cycle area/efficiency construction. The pressure
  diagram uses a logarithmic pressure/ambient axis and schematic station spacing.
- The ground operating line contains **81 actual fixed-geometry model solves**. Thrust,
  N1/N2 reference-speed fractions, OPR and TSFC curves are predictions, not an engine test
  sweep. Local sensitivities are secants between neighboring stored model points. The
  displayed fuel bracket identifies their sampling interval; no exact derivative or
  independent accuracy is claimed.
- Numerical closure is shown separately from measured-data agreement. Small residuals
  establish that the equations balance, rather than that the component maps are identified.

The engineering CSV retains unrounded derived values, units and map-region labels. The
separate operating-line CSV retains all 81 points and modeled-extension flags. Plot pixel
coordinates are excluded from exported engineering quantities. The initial diagrams and
station tables are server-rendered; subsequent controls update the same mounted document.

## Public RPM and station audit

`calibration --validate-public` uses the frozen fitted model and the separate
`cycle/data/public_validation.json` references. It emits all twenty-two comparisons
and exits 1 when a reported speed bound is exceeded. `--fit` is rejected in the
same invocation. A test confirms that the audit does not change model parameters.

FAA TCDS E3NE revision 16 (2019), Note 1, gives the standard -7R4D limits
N1 <= 3770 and N2 <= 8000 rpm, and -7R4D1 limits N1 <= 3810 and N2 <= 8000 rpm.
The grouped ICAO entry does not identify a converted (DM)/(EM) engine, whose
separate limits are stated in the source. Rated model N1 = 3943.959 rpm exceeds
both standard-variant N1 limits. N2 = 7940.896 rpm lies below those maxima, which
does not validate its actual operating speed. Limits are inequalities, not RPM
measurements to fit as equalities. Note 2 gives takeoff (5-minute) total exhaust Tt7 <= 625 C / 898.15 K for both variants. It maps to model total station 5, 783.385 K at rated fuel: within the maximum, without validating actual temperature. The inspector also shows cockpit percentage
normalization (3600 and 7807 rpm), separately from rated-model normalization.

The Osaka Metropolitan University exhibit for a JAL-donated -7R4D publishes
combustor-outlet 1271.1 C and turbine-inlet 1185.0 C at 33.3 C ambient and
213.5 kN uninstalled thrust. These convert to 1544.25 K and 1458.15 K. The source
does not supply measurement uncertainty or probe/cooling locations. The
provisional mappings are solver station 4 and station 41 after the first cooling
mix; they do not assert identical instrumentation definitions.

An optional `MappedInputs.AmbientTemperatureK` overrides ISA temperature at
unchanged ISA pressure; zero retains the previous ISA behavior. The ambient
command must be 200–330 K. Continuation ramps temperature along with other
commands, and tests verify inlet stagnation enthalpy/entropy and cycle closure.
The UI and public `Solve` fuel interval remain 0.2054–2.055 kg/s.

The separate hot-reference audit brackets fuel within 0.90–1.15 times the
original rated fuel, using the same seven-equation matcher and strict component
map boundaries. It conditions fuel on the published thrust instead of using
thrust as an output to validate. It finds 2.130823 kg/s, beyond the UI interval,
with station 4 = 1599.858 K and station 41 = 1557.844 K: +3.60% and +6.84%.
The point also exceeds reported speed maxima, which is explicitly recorded.
No new map, efficiency, cooling, RPM or gas parameters are fitted. These
differences are model discrepancies, not accuracy certifications or tolerances.

The nominal ground-idle N2 reference of 4370 rpm is displayed beside the ICAO
idle model's 6006.03 rpm without assigning a validation error: ambient, fuel,
bleed and loads are missing. The 5465 rpm flight-idle reference has no modeled
comparison. NASA Table V-3 gives standard-day -7A fuel/thrust and combustor-inlet
conditions; the twelve corresponding thrust/T/P comparisons are explicitly
earlier-variant context. Source Tt4 means this model's HPC exit, station 3.
The table's takeoff SI/psia columns disagree; the audit retains 21.68e5 Pa from
the SI column and flags the 323 psia discrepancy instead of silently resolving it.

No fully matched measured RPM/station dataset was identified. The current model
fails speed compatibility and remains unvalidated for station accuracy.
See [source provenance and exclusions](cycle/data/SOURCES.md).

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

## Surface rendering

The default materials have fixed authored finishes under studio lighting. The optional
gas-temperature tint is a display diagnostic; enabling it changes the liner color,
not the cycle inputs, computed temperatures, RPM or rotor clock. No metal-temperature,
emissivity or thermal-inertia model is implied. Reflection and surface maps are
generated by `materials/main.go`; the environment is LDR and the finishes are
illustrative. Geometry and solver parameters are unchanged by the material pass.
