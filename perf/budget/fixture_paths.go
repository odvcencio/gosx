package budget

import "strings"

const fixtureManifestFile = "perf-fixtures.v1.json"

var fixtureSidecars = [...]struct{ suffix, encoding string }{{".gz", "gzip"}, {".br", "br"}}

// fixtureFilePath is the shared layout for producer snapshots and collector
// reads. A document's serving URL is independent of its snapshot filename.
func fixtureFilePath(assetURL, kind string) (string, error) {
	file := strings.TrimPrefix(assetURL, "/")
	if kind == "html" {
		file = strings.Trim(file, "/")
		if file != "" {
			file += "/"
		}
		file += "index.html"
	} else if i := strings.Index(assetURL, "/gosx/assets/"); i >= 0 {
		file = "assets/" + assetURL[i+len("/gosx/assets/"):]
	}
	if !safePath(file) {
		return "", measureFailure("wrong-fixture", "/file")
	}
	return file, nil
}
