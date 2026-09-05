package main

import (
	"encoding/json"
	"fmt"
	"golang.org/x/net/html"
	runtimehost "m31labs.dev/gosx/client/runtime/host"
	"m31labs.dev/gosx/hydrate"
	"strings"
)

func validateExportServerSlotHost(root *html.Node) error {
	if root.Type == html.ElementNode && root.Data == "script" && nodeAttrMap(root.Attr)["id"] == "gosx-manifest" {
		var body strings.Builder
		for c := root.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == html.TextNode {
				body.WriteString(c.Data)
			}
		}
		var manifest hydrate.Manifest
		if err := json.Unmarshal([]byte(body.String()), &manifest); err != nil {
			if strings.Contains(body.String(), "serverSlotHost") || strings.Contains(body.String(), "server-slots-v1") {
				return fmt.Errorf("invalid ServerSlot export manifest: %w", err)
			}
		} else {
			required := manifest.ServerSlotHost != nil
			for _, entry := range manifest.Islands {
				required = required || entry.Requires == "server-slots-v1"
			}
			if required {
				r := manifest.ServerSlotHost
				if r == nil || r.Version != 1 || r.Hash != runtimehost.ServerSlotRuntimeHash() || r.Integrity != runtimehost.ServerSlotRuntimeIntegrity() || r.Path != "/gosx/server-slots.js?v="+r.Hash {
					return fmt.Errorf("stale ServerSlot export host identity; rebuild the exporting app")
				}
			}
		}
	}
	for c := root.FirstChild; c != nil; c = c.NextSibling {
		if err := validateExportServerSlotHost(c); err != nil {
			return err
		}
	}
	return nil
}
