//go:build !js || !wasm

package telemetry

import (
	"testing"
	"time"

	"m31labs.dev/gosx/auth"
	"m31labs.dev/gosx/server"
)

func BenchmarkRequestAggregate(b *testing.B) {
	tel := requestInventory(b, 0)
	tel.observeCatalog([]server.ObservationPattern{{Kind: "page", Pattern: "/match/{code}", Methods: []string{"GET"}}})
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tel.observeRequest("page", "/match/{code}", "GET", 200, 64, time.Millisecond, false)
	}
}

func BenchmarkOperationAndAuthAggregate(b *testing.B) {
	for _, name := range []string{"operation", "auth"} {
		b.Run(name, func(b *testing.B) {
			tel := requestInventory(b, 0)
			if name == "operation" {
				if err := tel.initializeOperations(); err != nil {
					b.Fatal(err)
				}
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if name == "operation" {
					tel.ObserveOperation(server.OperationEvent{Component: "isr", Operation: "refresh", Status: "ok", Duration: time.Millisecond})
				} else {
					tel.observeAuth(auth.AuthEvent{Type: "sign_in", Success: true})
				}
			}
		})
	}
}
