package desktop

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestCheckAppInstallerUpdateReportsAvailableVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.appinstaller")
	if err := os.WriteFile(path, []byte(appInstallerFixture("1.2.3.4")), 0644); err != nil {
		t.Fatal(err)
	}
	info, err := CheckAppInstallerUpdate(path, "1.2.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if !info.Available || info.AvailableVersion != "1.2.3.4" || info.PackageURI != "https://updates.example.test/app.msix" {
		t.Fatalf("unexpected update info: %#v", info)
	}
}

func TestCheckAppInstallerUpdateReadsHTTPFeed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		w.Write([]byte(appInstallerFixture("1.2.3.4")))
	}))
	defer server.Close()
	info, err := CheckAppInstallerUpdate(server.URL, "1.2.3.4")
	if err != nil {
		t.Fatal(err)
	}
	if info.Available {
		t.Fatalf("did not expect equal version to be available: %#v", info)
	}
}

func TestCompareUpdateVersion(t *testing.T) {
	if got, err := compareUpdateVersion("1.2.3.5", "1.2.3.4"); err != nil || got <= 0 {
		t.Fatalf("expected newer version, got %d err=%v", got, err)
	}
	if _, err := compareUpdateVersion("1.2", "1.2.3.4"); err == nil {
		t.Fatal("expected malformed version to fail")
	}
}

func appInstallerFixture(version string) string {
	return `<?xml version="1.0" encoding="utf-8"?>
<AppInstaller xmlns="http://schemas.microsoft.com/appx/appinstaller/2018" Uri="https://updates.example.test/app.appinstaller" Version="` + version + `">
  <MainPackage Name="gosx.test.app" Publisher="CN=GoSX Test" Version="` + version + `" Uri="https://updates.example.test/app.msix" ProcessorArchitecture="x64" />
</AppInstaller>`
}
