package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

func desktopVerifySignatureUsage(w io.Writer) {
	fmt.Fprintln(w, "Usage: gosx desktop verify-signature [--expect-subject <CN substring>] [--jsign-path <file>] <file>...")
}

func cmdDesktopVerifySignature() {
	if len(os.Args) > 3 && isHelpArg(os.Args[3]) {
		desktopVerifySignatureUsage(os.Stdout)
		return
	}
	fs := flag.NewFlagSet("desktop verify-signature", flag.ExitOnError)
	var expectSubject, jsignPath string
	fs.StringVar(&expectSubject, "expect-subject", "", "required substring of the verified signer subject")
	fs.StringVar(&jsignPath, "jsign-path", "", "path to jsign with verify support (default PATH)")
	_ = fs.Parse(os.Args[3:])
	if fs.NArg() == 0 {
		desktopVerifySignatureUsage(os.Stderr)
		os.Exit(2)
	}
	verifier, err := prepareAuthenticodeVerifier(runtime.GOOS, jsignPath, exec.LookPath)
	if err == nil {
		for _, file := range fs.Args() {
			var subject string
			subject, err = verifier.verify(file, expectSubject)
			if err != nil {
				break
			}
			fmt.Printf("Valid Authenticode signature: %s (%s)\n", file, subject)
		}
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "gosx desktop verify-signature: %s\n", redactSigningText(err.Error()))
		os.Exit(1)
	}
}

type authenticodeVerifier struct {
	kind     string
	toolPath string
}

func prepareAuthenticodeVerifier(host, jsignPath string, lookPath func(string) (string, error)) (*authenticodeVerifier, error) {
	if host == "windows" {
		for _, tool := range []string{"powershell.exe", "pwsh.exe"} {
			if path, err := lookPath(tool); err == nil {
				return &authenticodeVerifier{kind: "powershell", toolPath: path}, nil
			}
		}
		return nil, fmt.Errorf("cannot verify on this host: install PowerShell with Get-AuthenticodeSignature")
	}
	if path, err := lookPath("osslsigncode"); err == nil {
		return &authenticodeVerifier{kind: "osslsigncode", toolPath: path}, nil
	}
	if jsignPath == "" {
		jsignPath = "jsign"
	}
	if path, err := lookPath(jsignPath); err == nil {
		return &authenticodeVerifier{kind: "jsign", toolPath: path}, nil
	}
	return nil, fmt.Errorf("cannot verify on this host: install osslsigncode or jsign with verify support (8.0 preview or later)")
}

// The path is passed through the environment; it is never interpolated into
// PowerShell source. Convert Status to a string for stable JSON output.
const authenticodePowerShell = `$ErrorActionPreference = 'Stop'; $s = Get-AuthenticodeSignature -LiteralPath $env:GOSX_AUTHENTICODE_FILE; @{Status=$s.Status.ToString(); Subject=$s.SignerCertificate.Subject} | ConvertTo-Json -Compress`

func (verifier *authenticodeVerifier) command(file string) *exec.Cmd {
	switch verifier.kind {
	case "powershell":
		command := exec.Command(verifier.toolPath, "-NoProfile", "-NonInteractive", "-Command", authenticodePowerShell)
		command.Env = setCommandEnvironment(os.Environ(), map[string]string{"GOSX_AUTHENTICODE_FILE": file})
		return command
	case "osslsigncode":
		return exec.Command(verifier.toolPath, "verify", "-in", file)
	default:
		return exec.Command(verifier.toolPath, "verify", "--verbose", file)
	}
}

func (verifier *authenticodeVerifier) verify(file, expected string) (string, error) {
	absolute, err := filepath.Abs(file)
	if err != nil {
		return "", err
	}
	output, err := verifier.command(absolute).CombinedOutput()
	if err != nil {
		if verifier.kind == "jsign" && (strings.Contains(strings.ToLower(string(output)), "unknown command") || strings.Contains(strings.ToLower(string(output)), "invalid command")) {
			return "", fmt.Errorf("cannot verify on this host: this jsign has no verify command; install osslsigncode or jsign 8.0 preview or later")
		}
		return "", fmt.Errorf("verify %s with %s: %s", file, verifier.kind, redactSigningText(fmt.Sprintf("%v\n%s", err, output)))
	}
	subject, err := verifiedAuthenticodeSubject(verifier.kind, string(output), expected)
	if err != nil {
		return "", fmt.Errorf("verify %s: %w", file, err)
	}
	return subject, nil
}

func verifiedAuthenticodeSubject(kind, output, expected string) (string, error) {
	var subjects []string
	switch kind {
	case "powershell":
		var signature struct{ Status, Subject string }
		if err := json.Unmarshal([]byte(strings.TrimPrefix(output, "\ufeff")), &signature); err != nil {
			return "", fmt.Errorf("PowerShell returned invalid Authenticode status JSON")
		}
		if signature.Status != "Valid" {
			return "", fmt.Errorf("Authenticode status is %s; require Valid", signature.Status)
		}
		subjects = append(subjects, signature.Subject)
	case "osslsigncode":
		// Bind the leaf subject to the result for that signature. Do not
		// accept a CA/timestamp subject or a subject from an invalid signature.
		inSigner := false
		subject := ""
		for _, line := range strings.Split(output, "\n") {
			line = strings.TrimSpace(line)
			switch {
			case strings.HasPrefix(line, "Signature Index:"):
				inSigner, subject = false, ""
			case line == "Signer's certificate:":
				inSigner = true
			case inSigner && strings.HasPrefix(line, "Subject:"):
				subject = strings.TrimSpace(strings.TrimPrefix(line, "Subject:"))
				inSigner = false
			case line == "Signature verification: ok":
				subjects = append(subjects, subject)
			}
		}
	case "jsign":
		// Jsign verify reports the verified leaf's common name in its
		// summary. Its show output alone does not establish validity.
		for _, line := range strings.Split(output, "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "Signature #") && strings.HasSuffix(line, " is valid") {
				if _, subject, ok := strings.Cut(line, " by "); ok {
					subjects = append(subjects, "CN="+strings.TrimSuffix(subject, " is valid"))
				}
			}
		}
	}
	for _, subject := range subjects {
		if subject != "" && (expected == "" || strings.Contains(subject, expected)) {
			return subject, nil
		}
	}
	if len(subjects) > 0 && expected != "" {
		return "", fmt.Errorf("verified signer subject does not contain the expected text")
	}
	return "", fmt.Errorf("verification did not report a valid signature and signer subject")
}
