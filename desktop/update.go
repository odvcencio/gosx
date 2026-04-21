package desktop

import (
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// UpdateInfo is the result of comparing an AppInstaller feed against the
// current application version.
type UpdateInfo struct {
	FeedURI          string
	PackageURI       string
	CurrentVersion   string
	AvailableVersion string
	Available        bool
}

// CheckAppInstallerUpdate reads an AppInstaller feed from file, http, or https
// and reports whether its MainPackage version is newer than currentVersion.
func CheckAppInstallerUpdate(feedURI, currentVersion string) (UpdateInfo, error) {
	feedURI = strings.TrimSpace(feedURI)
	currentVersion = strings.TrimSpace(currentVersion)
	if feedURI == "" {
		return UpdateInfo{}, fmt.Errorf("%w: update feed is empty", ErrInvalidOptions)
	}
	data, err := readUpdateFeed(feedURI)
	if err != nil {
		return UpdateInfo{}, err
	}
	feed, err := parseAppInstallerFeed(data)
	if err != nil {
		return UpdateInfo{}, err
	}
	info := UpdateInfo{
		FeedURI:          feedURI,
		PackageURI:       strings.TrimSpace(feed.MainPackage.URI),
		CurrentVersion:   currentVersion,
		AvailableVersion: strings.TrimSpace(feed.MainPackage.Version),
	}
	if info.AvailableVersion == "" {
		return UpdateInfo{}, fmt.Errorf("%w: appinstaller main package version is empty", ErrInvalidOptions)
	}
	if currentVersion == "" {
		info.Available = true
		return info, nil
	}
	cmp, err := compareUpdateVersion(info.AvailableVersion, currentVersion)
	if err != nil {
		return UpdateInfo{}, err
	}
	info.Available = cmp > 0
	return info, nil
}

func readUpdateFeed(feedURI string) ([]byte, error) {
	u, err := url.Parse(feedURI)
	if err == nil {
		switch u.Scheme {
		case "http", "https":
			client := http.Client{Timeout: 10 * time.Second}
			resp, err := client.Get(feedURI)
			if err != nil {
				return nil, err
			}
			defer resp.Body.Close()
			if resp.StatusCode < 200 || resp.StatusCode >= 300 {
				return nil, fmt.Errorf("%w: update feed returned HTTP %d", ErrInvalidOptions, resp.StatusCode)
			}
			return io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		case "file":
			path := u.Path
			if u.Host != "" {
				path = "//" + u.Host + u.Path
			}
			return os.ReadFile(path)
		}
	}
	return os.ReadFile(feedURI)
}

func parseAppInstallerFeed(data []byte) (appInstallerFeed, error) {
	var feed appInstallerFeed
	if err := xml.Unmarshal(data, &feed); err != nil {
		return appInstallerFeed{}, fmt.Errorf("%w: decode appinstaller feed: %w", ErrInvalidOptions, err)
	}
	if strings.TrimSpace(feed.MainPackage.Name) == "" {
		return appInstallerFeed{}, fmt.Errorf("%w: appinstaller main package is missing", ErrInvalidOptions)
	}
	return feed, nil
}

type appInstallerFeed struct {
	XMLName     xml.Name                `xml:"AppInstaller"`
	URI         string                  `xml:"Uri,attr"`
	Version     string                  `xml:"Version,attr"`
	MainPackage appInstallerFeedPackage `xml:"MainPackage"`
}

type appInstallerFeedPackage struct {
	Name                  string `xml:"Name,attr"`
	Publisher             string `xml:"Publisher,attr"`
	Version               string `xml:"Version,attr"`
	URI                   string `xml:"Uri,attr"`
	ProcessorArchitecture string `xml:"ProcessorArchitecture,attr"`
}

func compareUpdateVersion(a, b string) (int, error) {
	aa, err := parseUpdateVersion(a)
	if err != nil {
		return 0, err
	}
	bb, err := parseUpdateVersion(b)
	if err != nil {
		return 0, err
	}
	for i := range aa {
		if aa[i] > bb[i] {
			return 1, nil
		}
		if aa[i] < bb[i] {
			return -1, nil
		}
	}
	return 0, nil
}

func parseUpdateVersion(version string) ([4]int, error) {
	var out [4]int
	parts := strings.Split(strings.TrimSpace(version), ".")
	if len(parts) != 4 {
		return out, fmt.Errorf("%w: version %q must use a.b.c.d format", ErrInvalidOptions, version)
	}
	for i, part := range parts {
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 || n > 65535 {
			return out, fmt.Errorf("%w: version component %q must be 0..65535", ErrInvalidOptions, part)
		}
		out[i] = n
	}
	return out, nil
}
