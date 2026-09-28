package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"m31labs.dev/gosx/desktop"
)

var appVersion = "0.0.0"

func main() {
	muteAudio := flag.Bool("mute-audio", true, "mute HTML media in the smoke window")
	flag.Parse()
	if evidence := os.Getenv("GOSX_INSTALLER_TEST_EVIDENCE"); evidence != "" {
		if err := os.MkdirAll(evidence, 0700); err != nil {
			fatal(err)
		}
		if err := os.WriteFile(filepath.Join(evidence, "launched-version.txt"), []byte(appVersion+"\n"), 0600); err != nil {
			fatal(err)
		}
	}
	if err := desktop.Run(desktop.Options{
		Title:       "GoSX installer smoke " + appVersion,
		HTML:        fmt.Sprintf("<!doctype html><meta charset=utf-8><title>Installer smoke</title><main>GoSX desktop installer smoke: %s</main>", appVersion),
		AppID:       "dev.gosx.wbinstaller.smoke",
		Version:     appVersion,
		UserDataDir: os.Getenv("GOSX_INSTALLER_TEST_WEBVIEW"),
		MuteAudio:   *muteAudio,
	}); err != nil {
		fatal(err)
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
