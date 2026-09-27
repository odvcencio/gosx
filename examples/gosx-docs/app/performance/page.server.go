package performance

import (
	"fmt"

	docsapp "m31labs.dev/gosx/examples/gosx-docs/app"
	"m31labs.dev/gosx/route"
)

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
				if hasMeasurements {
					measuredDate = receipts.MeasuredAt.Format("2 January 2006")
					measuredLabel = "Measured on " + measuredDate
					provenanceLabel = fmt.Sprintf("Measured %s on %s. Load average (1, 5, and 15 minute averages; start / end): ", measuredDate, receipts.Machine.Name)
					machineDescription = fmt.Sprintf("%s · %s · %.2f GiB RAM · %s", receipts.Machine.OS, receipts.Machine.CPU, receipts.Machine.MemoryGiB, receipts.Machine.Name)
				}
				return map[string]any{
					"receipts":             receipts,
					"hasMeasurements":      hasMeasurements,
					"measuredLabel":        measuredLabel,
					"lighthouseDescriptor": receipts.Lighthouse.Browser + ". " + receipts.Lighthouse.Method,
					"gpuDescriptor":        receipts.GPU.Label + ". " + receipts.GPU.Browser + ". " + receipts.GPU.Method,
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
