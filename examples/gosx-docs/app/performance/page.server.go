package performance

import (
	"fmt"
	"strconv"
	"strings"

	docsapp "m31labs.dev/gosx/examples/gosx-docs/app"
	"m31labs.dev/gosx/route"
)

type lighthousePageView struct {
	Path                string
	Median              LighthouseScore
	PerformanceTarget   string
	PerformanceStatus   string
	AccessibilityTarget string
	AccessibilityStatus string
	BestPracticesTarget string
	BestPracticesStatus string
	SEOTarget           string
	SEOStatus           string
	CLSTarget           string
	CLSStatus           string
}

func lighthousePageViews(pages []LighthousePage) []lighthousePageView {
	views := make([]lighthousePageView, 0, len(pages))
	for _, page := range pages {
		view := lighthousePageView{Path: page.Path, Median: page.Median}
		switch page.Path {
		case "/demos/":
			view.PerformanceTarget, view.PerformanceStatus = targetScore(page.Median.Performance, 90)
			view.AccessibilityTarget, view.AccessibilityStatus = targetScore(page.Median.Accessibility, 100)
			view.BestPracticesTarget, view.BestPracticesStatus = targetScore(page.Median.BestPractices, 100)
			view.SEOTarget, view.SEOStatus = targetScore(page.Median.SEO, 100)
			view.CLSTarget, view.CLSStatus = targetCLS(page.Median.CLS, 0.01)
		case "/capabilities/", "/performance/":
			view.PerformanceTarget, view.PerformanceStatus = targetScore(page.Median.Performance, 95)
			view.AccessibilityTarget, view.AccessibilityStatus = targetScore(page.Median.Accessibility, 100)
		}
		views = append(views, view)
	}
	return views
}

func targetScore(measured, target int) (string, string) {
	status := "misses"
	if measured >= target {
		status = "meets"
	}
	return fmt.Sprintf("≥ %d", target), status
}

func targetCLS(measured, target float64) (string, string) {
	status := "misses"
	if measured <= target {
		status = "meets"
	}
	return fmt.Sprintf("≤ %.2f", target), status
}

func lighthouseLoadDescription(start, end string) string {
	if start == "" || end == "" {
		return "The Lighthouse batch load average was not recorded for this receipt."
	}
	firstStart := strings.Fields(start)
	firstEnd := strings.Fields(end)
	startAverage, endAverage := "unknown", "unknown"
	if len(firstStart) > 0 {
		startAverage = firstStart[0]
	}
	if len(firstEnd) > 0 {
		endAverage = firstEnd[0]
	}
	startValue, _ := strconv.ParseFloat(startAverage, 64)
	endValue, _ := strconv.ParseFloat(endAverage, 64)
	if startValue >= 30 || endValue >= 30 {
		average := startAverage
		if endValue > startValue {
			average = endAverage
		}
		return fmt.Sprintf("Measured under load average %s (1 minute); load averages (1, 5, and 15 minutes, before / after): %s / %s", average, start, end)
	}
	return fmt.Sprintf("Load averages (1, 5, and 15 minutes, before / after): %s / %s", start, end)
}

func init() {
	docsapp.RegisterStaticDocsPage(
		"Performance receipts",
		"Measured page scores, renderer frame times, download sizes, and quickstart timings.",
		route.FileModuleOptions{
			Load: func(_ *route.RouteContext, _ route.FilePage) (any, error) {
				receipts, err := Read()
				if err != nil {
					return nil, err
				}
				hasMeasurements := receipts.Validate() == nil
				measuredDate := ""
				measuredLabel := ""
				provenanceLabel := ""
				machineDescription := ""
				lighthouseDescriptor := ""
				gpuDescriptor := ""
				bundleDescriptor := ""
				quickstartDescriptor := ""
				lighthousePages := []lighthousePageView{}
				if hasMeasurements {
					lighthousePages = lighthousePageViews(receipts.Lighthouse.Pages)
					measuredDate = receipts.MeasuredAt.Format("2 January 2006")
					measuredLabel = "Measured on " + measuredDate
					provenanceLabel = fmt.Sprintf("Measured %s on %s. Load average (1, 5, and 15 minute averages; start / end): ", measuredDate, receipts.Machine.Name)
					machineDescription = fmt.Sprintf("%s · %s · %.2f GiB RAM · %s", receipts.Machine.OS, receipts.Machine.CPU, receipts.Machine.MemoryGiB, receipts.Machine.Name)
					lighthouseDescriptor = fmt.Sprintf("Measured %s. %s. %s. Machine: %s. %s.", measuredDate, receipts.Lighthouse.Browser, receipts.Lighthouse.Method, receipts.Machine.Name, lighthouseLoadDescription(receipts.Lighthouse.LoadAverageStart, receipts.Lighthouse.LoadAverageEnd))
					gpuDescriptor = fmt.Sprintf("Measured %s. %s. %s. %s. Collection-host load average: %s.", measuredDate, receipts.GPU.Label, receipts.GPU.Browser, receipts.GPU.Method, receipts.Machine.LoadAverage)
					bundleDescriptor = fmt.Sprintf("Measured %s. Raw bytes from the local production build; Brotli bytes use quality 11. Browser: not applicable. Machine: %s. Load average: %s.", measuredDate, receipts.Machine.Name, receipts.Machine.LoadAverage)
					quickstartDescriptor = fmt.Sprintf("Measured %s. %s Browser: not applicable to the Go CLI install. Machine: %s; load average: %s.", measuredDate, receipts.Quickstart.Method, receipts.Machine.Name, receipts.Machine.LoadAverage)
				}
				return map[string]any{
					"receipts":             receipts,
					"lighthousePages":      lighthousePages,
					"hasMeasurements":      hasMeasurements,
					"measuredLabel":        measuredLabel,
					"lighthouseDescriptor": lighthouseDescriptor,
					"gpuDescriptor":        gpuDescriptor,
					"bundleDescriptor":     bundleDescriptor,
					"quickstartDescriptor": quickstartDescriptor,
					"provenanceLabel":      provenanceLabel,
					"machineDescription":   machineDescription,
				}, nil
			},
			Bindings: func(_ *route.RouteContext, _ route.FilePage, _ any) route.FileTemplateBindings {
				return route.FileTemplateBindings{Funcs: map[string]any{
					"millisecondsLabel": func(value float64) string { return fmt.Sprintf("%.2f ms", value) },
					"secondsLabel":      func(value float64) string { return fmt.Sprintf("%.2f s", value) },
				}}
			},
		},
	)
}
