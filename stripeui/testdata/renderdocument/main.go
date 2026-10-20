package main

import (
	"fmt"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/server"
	"m31labs.dev/gosx/stripeui"
)

func main() {
	page := server.NewPageState()
	page.SetNonce("strict-csp-nonce")
	stripeui.Require(page, stripeui.RuntimeConfig{Preconnect: true})
	body := stripeui.Elements(stripeui.ElementsSurfaceProps{
		RuntimeOptions: stripeui.RuntimeOptions{PublishableKey: "pk_test_public"},
		SessionAction:  "/checkout/__actions/payment-intent",
	}, stripeui.PaymentElement(stripeui.ElementProps{}))
	document := server.HTMLDocument(&server.DocumentContext{
		Title: "Stripe runtime order fixture",
		Head:  page.Head(),
		Body:  body,
		Nonce: page.Nonce(),
	})
	fmt.Print(gosx.RenderHTML(document))
}
