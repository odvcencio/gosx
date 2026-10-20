//go:build ignore

// This fake signing tool is used only by packaging tests. Its marker is not an
// Authenticode signature and must never be used for a release.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const marker = "\nGOSX-TEST-SIGNED\n"

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	args := os.Args[1:]
	if os.Getenv("GOSX_TEST_TOOL_FAIL") == "1" {
		return fmt.Errorf("fake tool failure: %s", os.Getenv("GOSX_ARTIFACT_SIGNING_TOKEN"))
	}
	if len(args) > 1 && args[0] == "account" {
		want := []string{"account", "get-access-token", "--resource", "https://codesigning.azure.net", "--query", "accessToken", "--output", "tsv"}
		if fmt.Sprint(args) != fmt.Sprint(want) {
			return fmt.Errorf("unexpected Azure CLI arguments")
		}
		fmt.Println("test-access-token")
		return nil
	}
	if len(args) == 0 {
		return fmt.Errorf("missing fake tool arguments")
	}
	file := args[len(args)-1]
	op := "sign"
	if args[0] == "verify" || args[0] == "-NoProfile" {
		op = "verify"
	}
	if args[0] == "-NoProfile" {
		file = os.Getenv("GOSX_AUTHENTICODE_FILE")
	}
	if log := os.Getenv("GOSX_TEST_SIGN_LOG"); log != "" {
		f, err := os.OpenFile(log, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
		if err != nil {
			return err
		}
		err = json.NewEncoder(f).Encode(struct{ Op, File string }{op, filepath.Base(file)})
		closeErr := f.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
	}
	data, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	if op == "verify" {
		if !bytes.HasSuffix(data, []byte(marker)) {
			return fmt.Errorf("file has no fake signature")
		}
		if args[0] == "-NoProfile" {
			fmt.Println(`{"Status":"Valid","Subject":"CN=Example Publisher"}`)
		} else if len(args) > 1 && args[1] == "-in" {
			fmt.Println("Signature Index: 0\nSigner's certificate:\nSubject: CN=Example Publisher\nSignature verification: ok")
		} else {
			fmt.Println("Signature #1 (SHA256 with RSA 4096) by Example Publisher is valid")
		}
		return nil
	}
	if args[0] == "--storetype" {
		found := false
		for i := 0; i+1 < len(args); i++ {
			if args[i] == "--storepass" && args[i+1] == "env:GOSX_ARTIFACT_SIGNING_TOKEN" {
				found = os.Getenv("GOSX_ARTIFACT_SIGNING_TOKEN") != ""
			}
		}
		if !found {
			return fmt.Errorf("jsign must receive an environment token reference")
		}
	}
	return os.WriteFile(file, append(data, []byte(marker)...), 0644)
}
