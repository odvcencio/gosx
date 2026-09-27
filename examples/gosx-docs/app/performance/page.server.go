package performance

import (
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
				return map[string]any{
					"receipts":        receipts,
					"hasMeasurements": !receipts.MeasuredAt.IsZero() && receipts.Commit != "",
					"measuredDate":    receipts.MeasuredAt.Format("2 January 2006"),
				}, nil
			},
		},
	)
}
