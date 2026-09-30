package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"m31labs.dev/gosx/cmd/gosx/installerhost"
)

const azureArtifactSigningProvider = "azure-artifact-signing"
const artifactSigningTimestampURL = "http://timestamp.acs.microsoft.com"
const artifactSigningTokenEnv = "GOSX_ARTIFACT_SIGNING_TOKEN"

func desktopSigningFlags(fs *flag.FlagSet, options *desktopPackageFlags) {
	fs.StringVar(&options.signProvider, "sign-provider", "", "signing provider: azure-artifact-signing; conflicts with --sign-cmd")
	fs.StringVar(&options.signing.Endpoint, "sign-endpoint", "", "Artifact Signing HTTPS endpoint")
	fs.StringVar(&options.signing.Account, "sign-account", "", "Artifact Signing account name")
	fs.StringVar(&options.signing.Profile, "sign-profile", "", "Artifact Signing certificate profile name")
	fs.StringVar(&options.signing.Tool, "sign-tool", "", "Artifact Signing tool: signtool (Windows) or jsign")
	fs.StringVar(&options.signing.DlibPath, "dlib-path", "", "path to Azure.CodeSigning.Dlib.dll")
	fs.StringVar(&options.signing.SigntoolPath, "signtool-path", "", "path to signtool.exe (default PATH)")
	fs.StringVar(&options.signing.JsignPath, "jsign-path", "", "path to jsign (default PATH); also used for verification")
	fs.BoolVar(&options.verifySignatures, "verify-signatures", false, "verify every signed file before packaging")
	fs.StringVar(&options.expectSubject, "expect-subject", "", "required substring of the verified signer subject")
}

func resolveArtifactSigningConfig(flags, config installerhost.SigningConfig, getenv func(string) string, host string) (installerhost.SigningConfig, error) {
	pick := func(flag, env, value string) string {
		environment := ""
		if env != "" {
			environment = getenv(env)
		}
		for _, candidate := range []string{flag, environment, value} {
			if candidate = strings.TrimSpace(candidate); candidate != "" {
				return candidate
			}
		}
		return ""
	}
	resolved := installerhost.SigningConfig{
		Endpoint:     pick(flags.Endpoint, "GOSX_ARTIFACT_SIGNING_ENDPOINT", config.Endpoint),
		Account:      pick(flags.Account, "GOSX_ARTIFACT_SIGNING_ACCOUNT", config.Account),
		Profile:      pick(flags.Profile, "GOSX_ARTIFACT_SIGNING_PROFILE", config.Profile),
		Tool:         pick(flags.Tool, "GOSX_ARTIFACT_SIGNING_TOOL", config.Tool),
		DlibPath:     pick(flags.DlibPath, "GOSX_ARTIFACT_SIGNING_DLIB", config.DlibPath),
		SigntoolPath: pick(flags.SigntoolPath, "", config.SigntoolPath),
		JsignPath:    pick(flags.JsignPath, "", config.JsignPath),
	}
	if resolved.Tool == "" {
		resolved.Tool = "jsign"
		if host == "windows" {
			resolved.Tool = "signtool"
		}
	}
	var missing []string
	for _, field := range []struct{ name, value string }{
		{"endpoint (--sign-endpoint / GOSX_ARTIFACT_SIGNING_ENDPOINT)", resolved.Endpoint},
		{"account (--sign-account / GOSX_ARTIFACT_SIGNING_ACCOUNT)", resolved.Account},
		{"profile (--sign-profile / GOSX_ARTIFACT_SIGNING_PROFILE)", resolved.Profile},
	} {
		if field.value == "" {
			missing = append(missing, field.name)
		}
	}
	if resolved.Tool == "signtool" && resolved.DlibPath == "" {
		missing = append(missing, "dlib_path (--dlib-path / GOSX_ARTIFACT_SIGNING_DLIB)")
	}
	if len(missing) > 0 {
		return resolved, fmt.Errorf("Artifact Signing is missing: %s", strings.Join(missing, ", "))
	}
	endpoint, err := url.Parse(resolved.Endpoint)
	if err != nil || endpoint.Scheme != "https" || endpoint.Hostname() == "" || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" {
		return resolved, fmt.Errorf("Artifact Signing endpoint must be an HTTPS URL without credentials, query or fragment")
	}
	if strings.ContainsAny(resolved.Account+resolved.Profile, "/\\\x00\r\n") {
		return resolved, fmt.Errorf("Artifact Signing account and profile must be plain names")
	}
	switch resolved.Tool {
	case "signtool":
		if host != "windows" {
			return resolved, fmt.Errorf("Artifact Signing signtool requires a Windows host; choose --sign-tool jsign")
		}
	case "jsign":
	default:
		return resolved, fmt.Errorf("Artifact Signing tool must be signtool or jsign")
	}
	return resolved, nil
}

type artifactSigner struct {
	config      installerhost.SigningConfig
	toolPath    string
	metadataDir string
	token       string
}

func prepareArtifactSigner(options desktopPackageFlags, config installerhost.SigningConfig) (*artifactSigner, error) {
	resolved, err := resolveArtifactSigningConfig(options.signing, config, os.Getenv, runtime.GOOS)
	if err != nil {
		return nil, err
	}
	signer := &artifactSigner{config: resolved}
	path := resolved.JsignPath
	if resolved.Tool == "signtool" {
		path = resolved.SigntoolPath
	}
	if path == "" {
		path = resolved.Tool
	}
	signer.toolPath, err = exec.LookPath(path)
	if err != nil {
		return nil, fmt.Errorf("Artifact Signing %s tool is unavailable; set --%s-path or PATH", resolved.Tool, resolved.Tool)
	}
	if resolved.Tool == "jsign" {
		signer.token = strings.TrimSpace(os.Getenv(artifactSigningTokenEnv))
		if signer.token == "" {
			// Keep stdout in memory and discard stderr: Azure CLI errors may
			// contain credentials, and a failed command may still emit a token.
			output, tokenErr := exec.Command("az", "account", "get-access-token", "--resource", "https://codesigning.azure.net", "--query", "accessToken", "--output", "tsv").Output()
			if tokenErr != nil {
				return nil, fmt.Errorf("Artifact Signing needs GOSX_ARTIFACT_SIGNING_TOKEN or a successful az account get-access-token (run az login first)")
			}
			signer.token = strings.TrimSpace(string(output))
			if signer.token == "" {
				return nil, fmt.Errorf("az account get-access-token returned no Artifact Signing token")
			}
		}
		return signer, nil
	}
	info, err := os.Stat(resolved.DlibPath)
	if err != nil || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("Artifact Signing --dlib-path must point to Azure.CodeSigning.Dlib.dll")
	}
	if err := signer.writeMetadata(); err != nil {
		return nil, err
	}
	return signer, nil
}

func (signer *artifactSigner) writeMetadata() error {
	var err error
	signer.metadataDir, err = os.MkdirTemp("", "gosx-artifact-signing-")
	if err != nil {
		return err
	}
	metadata, err := json.Marshal(struct {
		Endpoint               string
		CodeSigningAccountName string
		CertificateProfileName string
	}{signer.config.Endpoint, signer.config.Account, signer.config.Profile})
	if err == nil {
		err = os.WriteFile(filepath.Join(signer.metadataDir, "metadata.json"), metadata, 0600)
	}
	if err != nil {
		signer.close()
		return err
	}
	return nil
}

func (signer *artifactSigner) close() {
	if signer.metadataDir != "" {
		_ = os.RemoveAll(signer.metadataDir)
	}
}

func (signer *artifactSigner) command(file string) *exec.Cmd {
	if signer.config.Tool == "signtool" {
		return exec.Command(signer.toolPath, "sign", "/v", "/fd", "SHA256", "/tr", artifactSigningTimestampURL, "/td", "SHA256", "/dlib", signer.config.DlibPath, "/dmdf", filepath.Join(signer.metadataDir, "metadata.json"), file)
	}
	command := exec.Command(signer.toolPath, "--storetype", "TRUSTEDSIGNING", "--keystore", signer.config.Endpoint, "--alias", signer.config.Account+"/"+signer.config.Profile, "--storepass", "env:"+artifactSigningTokenEnv, "--alg", "SHA-256", "--tsaurl", artifactSigningTimestampURL, "--tsmode", "RFC3161", file)
	command.Env = setCommandEnvironment(os.Environ(), map[string]string{artifactSigningTokenEnv: signer.token})
	return command
}

func redactSigningText(value string, secrets ...string) string {
	secrets = append(secrets, os.Getenv(artifactSigningTokenEnv), os.Getenv("AZURE_CLIENT_SECRET"))
	for _, secret := range secrets {
		if secret != "" {
			value = strings.ReplaceAll(value, secret, "[REDACTED]")
		}
	}
	return value
}

func (signer *artifactSigner) sign(file string) error {
	output, err := signer.command(file).CombinedOutput()
	if err != nil {
		return fmt.Errorf("Artifact Signing %s failed: %s", signer.config.Tool, redactSigningText(fmt.Sprintf("%v\n%s", err, output), signer.token))
	}
	return nil
}
