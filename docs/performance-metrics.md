# Performance measurement availability

`gosx perf` activates its profiling tab before navigation, including when connected to an existing browser through `CHROME_WS_URL`. A background tab can load successfully while the browser withholds paint and animation-frame samples. Keep the profiling tab visible during collection.

JSON page reports include `metricAvailability`. Each boolean records whether the associated measurement was collected, independently of its numeric value:

- `lcp` and `fid` require an actual paint or input entry. A page without input has no FID measurement.
- `cls` and `longTasks` require a supported observer and a visible measurement window. Zero shifts, long tasks, or blocking time are valid measurements when collection succeeded.
- `network` requires a successful resource-timing query. An empty resource list can be a measured zero.
- `runtimeCounters` and `sceneFrames` identify available instrumentation counters. A counter can be available even when its value is zero.

Scene render percentiles and first-submit duration additionally require a positive `frameStats.count`. Dropped-frame estimates require recorded animation-frame intervals. These CPU and display-opportunity samples do not certify hardware GPU performance.

Required budget assertions fail when their measurement is unavailable. An explicitly optional metric, such as `gpu_total_p95?`, can be skipped. Renderer recovery does not exempt missing evidence; the existing recovery policy only applies to measurements that were actually recorded. Budget thresholds are unchanged.

Older reports lack `metricAvailability`. Nonzero measurements and recorded samples remain usable, but an isolated default zero is insufficient evidence. Recollect the report when a required metric is unknown. In Go-created reports, set availability only for measurements that were actually captured, including legitimate zeros.

Human-readable output marks unavailable measurements. `gosx perf compare` includes a metric only when both reports contain evidence for it; its `unavailable` list identifies metrics recorded in only one report. Missing candidate data therefore cannot appear as a 100% improvement.
