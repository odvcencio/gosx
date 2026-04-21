package main

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
)

const (
	msixManifestXMLNS       = "http://schemas.microsoft.com/appx/manifest/foundation/windows10"
	msixManifestUAPXMLNS    = "http://schemas.microsoft.com/appx/manifest/uap/windows10"
	msixManifestRescapXMLNS = "http://schemas.microsoft.com/appx/manifest/foundation/windows10/restrictedcapabilities"
	appInstallerXMLNS       = "http://schemas.microsoft.com/appx/appinstaller/2018"
)

type msixReleaseArtifacts struct {
	Package      string
	AppInstaller string
}

type msixPackageOptions struct {
	IdentityName         string
	Publisher            string
	PublisherDisplayName string
	DisplayName          string
	Description          string
	Version              string
	Executable           string
	Architecture         string
	PackageDir           string
	OutputPath           string
	AppInstallerURI      string
	MainPackageURI       string
	Capabilities         []string
}

func stageMSIXReleaseBundle(projectDir, distDir string, builtServer bool, serverBinaryPath string, opts BuildOptions) (msixReleaseArtifacts, error) {
	if !builtServer {
		return msixReleaseArtifacts{}, fmt.Errorf("MSIX packaging requires a runnable main package")
	}
	if targetGOOS() != "windows" {
		return msixReleaseArtifacts{}, fmt.Errorf("MSIX packaging requires GOOS=windows or a Windows host")
	}
	if opts.Sign {
		opts.MSIX = true
	}
	pkgOpts, err := resolveMSIXPackageOptions(projectDir, distDir, serverBinaryPath, opts)
	if err != nil {
		return msixReleaseArtifacts{}, err
	}
	if err := stageMSIXPackageDirectory(distDir, pkgOpts); err != nil {
		return msixReleaseArtifacts{}, err
	}

	artifacts := msixReleaseArtifacts{}
	if opts.MSIX {
		if err := packMSIXPackage(pkgOpts.PackageDir, pkgOpts.OutputPath); err != nil {
			return msixReleaseArtifacts{}, err
		}
		artifacts.Package = pkgOpts.OutputPath
		if opts.Sign {
			if err := signMSIXPackage(pkgOpts.OutputPath); err != nil {
				return msixReleaseArtifacts{}, err
			}
		}
	}
	if strings.TrimSpace(pkgOpts.AppInstallerURI) != "" {
		path := filepath.Join(distDir, "app.appinstaller")
		if err := writeAppInstallerManifest(path, pkgOpts); err != nil {
			return msixReleaseArtifacts{}, err
		}
		artifacts.AppInstaller = path
	}
	return artifacts, nil
}

func resolveMSIXPackageOptions(projectDir, distDir, serverBinaryPath string, opts BuildOptions) (msixPackageOptions, error) {
	base := filepath.Base(projectDir)
	if base == "." || base == string(filepath.Separator) || strings.TrimSpace(base) == "" {
		base = "gosx-app"
	}
	version := envOrDefault("GOSX_MSIX_VERSION", "0.1.0.0")
	if err := validateMSIXVersion(version); err != nil {
		return msixPackageOptions{}, err
	}
	appInstallerURI, mainPackageURI := resolveAppInstallerURIs(strings.TrimSpace(firstNonEmpty(opts.AppInstallerURI, os.Getenv("GOSX_APPINSTALLER_URI"))))
	capabilities := []string{"internetClient", "runFullTrust"}
	if envBool("GOSX_MSIX_BROAD_FILE_SYSTEM_ACCESS") {
		capabilities = append(capabilities, "broadFileSystemAccess")
	}
	sort.Strings(capabilities)

	executable := filepath.ToSlash(filepath.Join("server", filepath.Base(serverBinaryPath)))
	executable = strings.ReplaceAll(executable, "/", `\`)
	return msixPackageOptions{
		IdentityName:         sanitizeMSIXIdentity(envOrDefault("GOSX_MSIX_IDENTITY", "gosx."+base)),
		Publisher:            envOrDefault("GOSX_MSIX_PUBLISHER", "CN=GoSX Development"),
		PublisherDisplayName: envOrDefault("GOSX_MSIX_PUBLISHER_DISPLAY_NAME", "GoSX Development"),
		DisplayName:          envOrDefault("GOSX_MSIX_DISPLAY_NAME", base),
		Description:          envOrDefault("GOSX_MSIX_DESCRIPTION", base),
		Version:              version,
		Executable:           executable,
		Architecture:         targetMSIXArchitecture(),
		PackageDir:           filepath.Join(distDir, "msix", "package"),
		OutputPath:           filepath.Join(distDir, "app.msix"),
		AppInstallerURI:      appInstallerURI,
		MainPackageURI:       mainPackageURI,
		Capabilities:         capabilities,
	}, nil
}

func stageMSIXPackageDirectory(distDir string, opts msixPackageOptions) error {
	if err := os.RemoveAll(filepath.Dir(opts.PackageDir)); err != nil {
		return err
	}
	if err := os.MkdirAll(opts.PackageDir, 0755); err != nil {
		return err
	}
	if err := copyMSIXPayload(distDir, opts.PackageDir); err != nil {
		return err
	}
	if err := writeMSIXManifest(filepath.Join(opts.PackageDir, "AppxManifest.xml"), opts); err != nil {
		return err
	}
	return writeMSIXLogoAssets(filepath.Join(opts.PackageDir, "Assets"))
}

func copyMSIXPayload(distDir, packageDir string) error {
	return filepath.Walk(distDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if path == distDir {
			return nil
		}
		rel, err := filepath.Rel(distDir, path)
		if err != nil {
			return err
		}
		relSlash := filepath.ToSlash(rel)
		if info.IsDir() {
			if relSlash == "msix" {
				return filepath.SkipDir
			}
			return os.MkdirAll(filepath.Join(packageDir, rel), 0755)
		}
		switch relSlash {
		case "app.msix", "app.appinstaller":
			return nil
		}
		return copyFile(filepath.Join(packageDir, rel), path)
	})
}

func writeMSIXManifest(path string, opts msixPackageOptions) error {
	manifest := msixManifest{
		XMLNS:              msixManifestXMLNS,
		XMLNSUAP:           msixManifestUAPXMLNS,
		XMLNSRescap:        msixManifestRescapXMLNS,
		IgnorableNamespace: "uap rescap",
		Identity: msixIdentity{
			Name:      opts.IdentityName,
			Publisher: opts.Publisher,
			Version:   opts.Version,
		},
		Properties: msixProperties{
			DisplayName:          opts.DisplayName,
			PublisherDisplayName: opts.PublisherDisplayName,
			Logo:                 `Assets\StoreLogo.png`,
		},
		Dependencies: msixDependencies{
			TargetDeviceFamily: msixTargetDeviceFamily{
				Name:             "Windows.Desktop",
				MinVersion:       "10.0.17763.0",
				MaxVersionTested: "10.0.22621.0",
			},
		},
		Resources: msixResources{
			Resource: msixResource{Language: "en-us"},
		},
		Applications: msixApplications{
			Application: msixApplication{
				ID:         "App",
				Executable: opts.Executable,
				EntryPoint: "Windows.FullTrustApplication",
				VisualElements: msixVisualElements{
					DisplayName:       opts.DisplayName,
					Description:       opts.Description,
					Square44x44Logo:   `Assets\Square44x44Logo.png`,
					Square150x150Logo: `Assets\Square150x150Logo.png`,
					BackgroundColor:   "transparent",
					AppListEntry:      "default",
				},
			},
		},
	}
	for _, capability := range opts.Capabilities {
		switch capability {
		case "runFullTrust", "broadFileSystemAccess":
			manifest.Capabilities.RescapCapabilities = append(manifest.Capabilities.RescapCapabilities, msixNamedCapability{Name: capability})
		default:
			manifest.Capabilities.Capabilities = append(manifest.Capabilities.Capabilities, msixNamedCapability{Name: capability})
		}
	}
	data, err := xml.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, append([]byte(xml.Header), data...), 0644)
}

type msixManifest struct {
	XMLName            xml.Name         `xml:"Package"`
	XMLNS              string           `xml:"xmlns,attr"`
	XMLNSUAP           string           `xml:"xmlns:uap,attr"`
	XMLNSRescap        string           `xml:"xmlns:rescap,attr"`
	IgnorableNamespace string           `xml:"IgnorableNamespaces,attr"`
	Identity           msixIdentity     `xml:"Identity"`
	Properties         msixProperties   `xml:"Properties"`
	Dependencies       msixDependencies `xml:"Dependencies"`
	Resources          msixResources    `xml:"Resources"`
	Applications       msixApplications `xml:"Applications"`
	Capabilities       msixCapabilities `xml:"Capabilities"`
}

type msixIdentity struct {
	Name      string `xml:"Name,attr"`
	Publisher string `xml:"Publisher,attr"`
	Version   string `xml:"Version,attr"`
}

type msixProperties struct {
	DisplayName          string `xml:"DisplayName"`
	PublisherDisplayName string `xml:"PublisherDisplayName"`
	Logo                 string `xml:"Logo"`
}

type msixDependencies struct {
	TargetDeviceFamily msixTargetDeviceFamily `xml:"TargetDeviceFamily"`
}

type msixTargetDeviceFamily struct {
	Name             string `xml:"Name,attr"`
	MinVersion       string `xml:"MinVersion,attr"`
	MaxVersionTested string `xml:"MaxVersionTested,attr"`
}

type msixResources struct {
	Resource msixResource `xml:"Resource"`
}

type msixResource struct {
	Language string `xml:"Language,attr"`
}

type msixApplications struct {
	Application msixApplication `xml:"Application"`
}

type msixApplication struct {
	ID             string             `xml:"Id,attr"`
	Executable     string             `xml:"Executable,attr"`
	EntryPoint     string             `xml:"EntryPoint,attr"`
	VisualElements msixVisualElements `xml:"uap:VisualElements"`
}

type msixVisualElements struct {
	DisplayName       string `xml:"DisplayName,attr"`
	Description       string `xml:"Description,attr"`
	Square44x44Logo   string `xml:"Square44x44Logo,attr"`
	Square150x150Logo string `xml:"Square150x150Logo,attr"`
	BackgroundColor   string `xml:"BackgroundColor,attr"`
	AppListEntry      string `xml:"AppListEntry,attr,omitempty"`
}

type msixCapabilities struct {
	Capabilities       []msixNamedCapability `xml:"Capability"`
	RescapCapabilities []msixNamedCapability `xml:"rescap:Capability"`
}

type msixNamedCapability struct {
	Name string `xml:"Name,attr"`
}

func writeMSIXLogoAssets(dir string) error {
	for _, logo := range []struct {
		name string
		size int
	}{
		{"Square44x44Logo.png", 44},
		{"Square150x150Logo.png", 150},
		{"StoreLogo.png", 50},
	} {
		if err := writeSolidPNG(filepath.Join(dir, logo.name), logo.size); err != nil {
			return err
		}
	}
	return nil
}

func writeSolidPNG(path string, size int) error {
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	fill := color.RGBA{R: 0x14, G: 0x6e, B: 0xd1, A: 0xff}
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			img.SetRGBA(x, y, fill)
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}

func packMSIXPackage(packageDir, outputPath string) error {
	tool, err := lookPathEnvFirst("GOSX_MAKEAPPX", "MakeAppx.exe", "makeappx.exe", "MakeAppx", "makeappx")
	if err != nil {
		return fmt.Errorf("find MakeAppx: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(outputPath), 0755); err != nil {
		return err
	}
	cmd := exec.Command(tool, "pack", "/d", packageDir, "/p", outputPath, "/o")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("MakeAppx pack failed: %w\n%s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func signMSIXPackage(path string) error {
	cert := strings.TrimSpace(os.Getenv("GOSX_CODESIGN_CERT"))
	key := strings.TrimSpace(os.Getenv("GOSX_CODESIGN_KEY"))
	if cert == "" || key == "" {
		return fmt.Errorf("GOSX_CODESIGN_CERT and GOSX_CODESIGN_KEY are required for --sign")
	}
	tool, err := lookPathEnvFirst("GOSX_SIGNTOOL", "signtool.exe", "signtool")
	if err != nil {
		return fmt.Errorf("find signtool: %w", err)
	}
	args := []string{"sign", "/fd", "SHA256", "/f", cert, "/p", key}
	if timestamp := strings.TrimSpace(envOrDefault("GOSX_CODESIGN_TIMESTAMP", "http://timestamp.digicert.com")); timestamp != "" {
		args = append(args, "/tr", timestamp, "/td", "SHA256")
	}
	args = append(args, path)
	cmd := exec.Command(tool, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("signtool sign failed: %w\n%s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func writeAppInstallerManifest(path string, opts msixPackageOptions) error {
	if strings.TrimSpace(opts.AppInstallerURI) == "" {
		return nil
	}
	mainURI := opts.MainPackageURI
	if strings.TrimSpace(mainURI) == "" {
		mainURI = "app.msix"
	}
	manifest := appInstallerManifest{
		XMLNS:   appInstallerXMLNS,
		URI:     opts.AppInstallerURI,
		Version: opts.Version,
		MainPackage: appInstallerMainPackage{
			Name:                  opts.IdentityName,
			Publisher:             opts.Publisher,
			Version:               opts.Version,
			URI:                   mainURI,
			ProcessorArchitecture: opts.Architecture,
		},
		UpdateSettings: appInstallerUpdateSettings{
			OnLaunch: appInstallerOnLaunch{
				HoursBetweenUpdateChecks: 0,
				ShowPrompt:               true,
				UpdateBlocksActivation:   false,
			},
			ForceUpdateFromAnyVersion: true,
		},
	}
	data, err := xml.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, append([]byte(xml.Header), data...), 0644)
}

type appInstallerManifest struct {
	XMLName        xml.Name                   `xml:"AppInstaller"`
	XMLNS          string                     `xml:"xmlns,attr"`
	URI            string                     `xml:"Uri,attr"`
	Version        string                     `xml:"Version,attr"`
	MainPackage    appInstallerMainPackage    `xml:"MainPackage"`
	UpdateSettings appInstallerUpdateSettings `xml:"UpdateSettings"`
}

type appInstallerMainPackage struct {
	Name                  string `xml:"Name,attr"`
	Publisher             string `xml:"Publisher,attr"`
	Version               string `xml:"Version,attr"`
	URI                   string `xml:"Uri,attr"`
	ProcessorArchitecture string `xml:"ProcessorArchitecture,attr"`
}

type appInstallerUpdateSettings struct {
	OnLaunch                  appInstallerOnLaunch `xml:"OnLaunch"`
	ForceUpdateFromAnyVersion bool                 `xml:"ForceUpdateFromAnyVersion"`
}

type appInstallerOnLaunch struct {
	HoursBetweenUpdateChecks int  `xml:"HoursBetweenUpdateChecks,attr"`
	ShowPrompt               bool `xml:"ShowPrompt,attr"`
	UpdateBlocksActivation   bool `xml:"UpdateBlocksActivation,attr"`
}

func resolveAppInstallerURIs(input string) (appInstallerURI, packageURI string) {
	input = strings.TrimSpace(input)
	if input == "" {
		return "", ""
	}
	if strings.HasSuffix(strings.ToLower(input), ".appinstaller") {
		base := input[:strings.LastIndex(input, "/")+1]
		if base == "" {
			base = "./"
		}
		return input, base + "app.msix"
	}
	base := strings.TrimRight(input, "/")
	return base + "/app.appinstaller", base + "/app.msix"
}

func validateMSIXVersion(version string) error {
	parts := strings.Split(version, ".")
	if len(parts) != 4 {
		return fmt.Errorf("GOSX_MSIX_VERSION must use a.b.c.d format, got %q", version)
	}
	for _, part := range parts {
		if part == "" {
			return fmt.Errorf("GOSX_MSIX_VERSION contains an empty component: %q", version)
		}
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 || n > 65535 {
			return fmt.Errorf("GOSX_MSIX_VERSION component %q must be 0..65535", part)
		}
	}
	return nil
}

func sanitizeMSIXIdentity(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "gosx.app"
	}
	var b strings.Builder
	lastDot := false
	for _, r := range value {
		ok := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '.'
		if !ok {
			r = '.'
		}
		if r == '.' {
			if lastDot {
				continue
			}
			lastDot = true
		} else {
			lastDot = false
		}
		b.WriteRune(r)
	}
	out := strings.Trim(b.String(), ".-")
	if out == "" {
		return "gosx.app"
	}
	return out
}

func targetGOOS() string {
	if goos := strings.TrimSpace(os.Getenv("GOOS")); goos != "" {
		return goos
	}
	return runtime.GOOS
}

func targetExecutableExt() string {
	if targetGOOS() == "windows" {
		return ".exe"
	}
	return ""
}

func targetMSIXArchitecture() string {
	switch strings.TrimSpace(firstNonEmpty(os.Getenv("GOARCH"), runtime.GOARCH)) {
	case "amd64":
		return "x64"
	case "386":
		return "x86"
	case "arm64":
		return "arm64"
	case "arm":
		return "arm"
	default:
		return "neutral"
	}
}

func envOrDefault(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func envBool(key string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(key))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func lookPathEnvFirst(envKey string, names ...string) (string, error) {
	if configured := strings.TrimSpace(os.Getenv(envKey)); configured != "" {
		if _, err := os.Stat(configured); err == nil {
			return configured, nil
		}
		if path, err := exec.LookPath(configured); err == nil {
			return path, nil
		}
		return "", fmt.Errorf("%s=%s not found", envKey, configured)
	}
	var misses bytes.Buffer
	for _, name := range names {
		if path, err := exec.LookPath(name); err == nil {
			return path, nil
		} else {
			fmt.Fprintf(&misses, "%s ", name)
		}
	}
	return "", fmt.Errorf("none of %sfound on PATH", misses.String())
}
