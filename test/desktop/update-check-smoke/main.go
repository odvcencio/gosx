package main

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"m31labs.dev/gosx/desktop"
)

type report struct {
	Status       desktop.SignedUpdateStatus `json:"status"`
	CheckedAt    string                     `json:"checked_at,omitempty"`
	NextCheckAt  string                     `json:"next_check_at,omitempty"`
	Version      string                     `json:"version,omitempty"`
	Released     string                     `json:"released,omitempty"`
	Notes        string                     `json:"notes,omitempty"`
	DownloadPage string                     `json:"download_page,omitempty"`
	Error        string                     `json:"error,omitempty"`
}

func main() {
	var options desktop.SignedUpdateCheckOptions
	var publicKey string
	flag.StringVar(&options.ManifestURL, "manifest", "", "latest.json URL")
	flag.StringVar(&options.App, "app", "", "application ID")
	flag.StringVar(&options.Channel, "channel", "stable", "release channel")
	flag.StringVar(&options.CurrentVersion, "version", "", "current application version")
	flag.StringVar(&publicKey, "public-key", "", "compiled test public key in base64 or hex")
	flag.StringVar(&options.StateFile, "state-file", "", "persistent update state file")
	flag.BoolVar(&options.Enabled, "enabled", true, "allow update checks")
	flag.BoolVar(&options.StartupComplete, "startup-complete", true, "startup has completed")
	flag.BoolVar(&options.Online, "online", true, "caller reports a working network")
	flag.BoolVar(&options.AllowLoopbackHTTPForTests, "allow-loopback-http-for-tests", true, "allow local test server HTTP")
	flag.Parse()

	decoded, err := base64.StdEncoding.DecodeString(publicKey)
	if err != nil {
		decoded, err = hex.DecodeString(publicKey)
	}
	if err != nil || len(decoded) != ed25519.PublicKeySize {
		writeReport(report{Error: "invalid public key"})
		return
	}
	options.PublicKey = ed25519.PublicKey(decoded)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	result, checkErr := desktop.CheckSignedUpdate(ctx, options)
	output := report{
		Status: result.Status, Version: result.Version, Released: result.Released,
		Notes: result.Notes, DownloadPage: result.DownloadPage,
	}
	if !result.CheckedAt.IsZero() {
		output.CheckedAt = result.CheckedAt.UTC().Format("2006-01-02T15:04:05Z07:00")
		output.NextCheckAt = result.NextCheckAt.UTC().Format("2006-01-02T15:04:05Z07:00")
	}
	if checkErr != nil {
		output.Error = checkErr.Error()
	}
	writeReport(output)
}

func writeReport(output report) {
	encoded, err := json.Marshal(output)
	if err != nil {
		fmt.Fprintf(os.Stderr, "encode update report: %v\n", err)
		return
	}
	fmt.Println(strings.TrimSpace(string(encoded)))
}
