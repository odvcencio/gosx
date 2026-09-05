package perf

import "strings"

const measurementStatusVersion = 1

type MeasurementState string

const (
	MeasurementOK          MeasurementState = "ok"
	MeasurementFailed      MeasurementState = "failed"
	MeasurementUnsupported MeasurementState = "unsupported"
	MeasurementUnobserved  MeasurementState = "unobserved"
	MeasurementSkipped     MeasurementState = "skipped"
	MeasurementUnknown     MeasurementState = "unknown"
)

type MeasurementStatus struct {
	Version int                        `json:"version"`
	Groups  map[string]CollectorStatus `json:"groups,omitempty"`
}

type CollectorStatus struct {
	State  MeasurementState `json:"state"`
	Reason string           `json:"reason,omitempty"`
}

func (p *PageReport) setMeasurementStatus(group string, state MeasurementState, reason string) {
	p.setMeasurementStatusValue(group, CollectorStatus{State: state, Reason: reason})
}

func (p *PageReport) setMeasurementStatusValue(group string, status CollectorStatus) {
	if p.Measurements == nil {
		p.Measurements = &MeasurementStatus{Version: measurementStatusVersion}
	}
	if p.Measurements.Version == 0 {
		p.Measurements.Version = measurementStatusVersion
	}
	if p.Measurements.Groups == nil {
		p.Measurements.Groups = map[string]CollectorStatus{}
	}
	p.Measurements.Groups[group] = status
}

func (p *PageReport) measurementStatus(group string) CollectorStatus {
	if p == nil || p.Measurements == nil || p.Measurements.Groups == nil {
		return CollectorStatus{State: MeasurementUnknown, Reason: "report predates optional measurement status metadata; recapture before gating this metric"}
	}
	if p.Measurements.Version != measurementStatusVersion {
		return CollectorStatus{State: MeasurementUnknown, Reason: "unsupported optional measurement status metadata version; recapture before gating this metric"}
	}
	status, ok := p.Measurements.Groups[group]
	if !ok || status.State == "" {
		return CollectorStatus{State: MeasurementUnknown, Reason: "optional measurement status not recorded; recapture before gating this metric"}
	}
	return status
}

func measurementGroupForMetric(name string) string {
	switch name {
	case "network_kb", "requests":
		return "resource-waterfall"
	case "lcp", "lcp_ms":
		return "lcp"
	case "cls":
		return "cls"
	case "fid", "fid_ms":
		return "fid"
	case "long_tasks", "long_task_total", "long_task_total_ms", "tbt", "tbt_ms":
		return "long-tasks"
	case "hub_messages", "hub_bytes", "hub_sends", "signal_writes", "signal_reads":
		return "runtime-counters"
	}
	if strings.HasPrefix(name, "webgl_") {
		return "webgl-info"
	}
	return ""
}

func metricAvailableForStatus(status CollectorStatus) bool {
	return status.State == MeasurementOK
}

func metricStatusReason(status CollectorStatus) string {
	if status.Reason != "" {
		return status.Reason
	}
	switch status.State {
	case MeasurementFailed:
		return "collector failed"
	case MeasurementUnsupported:
		return "browser or instrumentation does not support this metric"
	case MeasurementUnobserved:
		return "metric was supported but no sample was observed"
	case MeasurementSkipped:
		return "collector skipped"
	case MeasurementUnknown, "":
		return "optional measurement status unknown; recapture before gating this metric"
	default:
		return string(status.State)
	}
}

type browserObserverStatus struct {
	Supported bool   `json:"supported"`
	Installed bool   `json:"installed"`
	Observed  bool   `json:"observed"`
	Error     string `json:"error"`
}

type browserMeasurementStatus struct {
	Version         int                   `json:"version"`
	LCP             browserObserverStatus `json:"lcp"`
	CLS             browserObserverStatus `json:"cls"`
	FID             browserObserverStatus `json:"fid"`
	LongTasks       browserObserverStatus `json:"longTasks"`
	RuntimeCounters browserRuntimeStatus  `json:"runtimeCounters"`
}

type browserRuntimeStatus struct {
	Available bool   `json:"available"`
	Error     string `json:"error"`
}

func statusFromObserver(entryType string, status browserObserverStatus, requiresSample bool) CollectorStatus {
	if !status.Supported {
		return CollectorStatus{State: MeasurementUnsupported, Reason: "PerformanceObserver entry type " + entryType + " is unsupported"}
	}
	if !status.Installed {
		reason := status.Error
		if reason == "" {
			reason = "PerformanceObserver for " + entryType + " was not installed"
		}
		return CollectorStatus{State: MeasurementFailed, Reason: reason}
	}
	if requiresSample && !status.Observed {
		return CollectorStatus{State: MeasurementUnobserved, Reason: "PerformanceObserver for " + entryType + " installed but observed no entries"}
	}
	return CollectorStatus{State: MeasurementOK}
}

func statusFromBrowserObserver(entryType string, all browserMeasurementStatus, status browserObserverStatus, requiresSample bool) CollectorStatus {
	if all.Version != measurementStatusVersion {
		return CollectorStatus{State: MeasurementUnknown, Reason: "browser optional measurement status metadata is absent or unsupported"}
	}
	return statusFromObserver(entryType, status, requiresSample)
}

func statusFromRuntimeCounters(status browserRuntimeStatus) CollectorStatus {
	if status.Error != "" {
		return CollectorStatus{State: MeasurementFailed, Reason: status.Error}
	}
	if !status.Available {
		return CollectorStatus{State: MeasurementUnsupported, Reason: "window.__gosx_perf runtime counters are unavailable"}
	}
	return CollectorStatus{State: MeasurementOK}
}

func statusFromBrowserRuntimeCounters(all browserMeasurementStatus) CollectorStatus {
	if all.Version != measurementStatusVersion {
		return CollectorStatus{State: MeasurementUnknown, Reason: "browser optional measurement status metadata is absent or unsupported"}
	}
	return statusFromRuntimeCounters(all.RuntimeCounters)
}
