package docs

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestBlackglassCoastRejectsStudioFingerprintDrift(t *testing.T) {
	payload := append([]byte(nil), blackglassCoastSceneDoc...)
	payload[len(payload)-2] ^= 1
	if _, err := loadBlackglassCoastRuntimeContract(payload); err == nil || !strings.Contains(err.Error(), "fingerprint") {
		t.Fatalf("tampered Studio export error = %v, want fingerprint rejection", err)
	}
}

func TestBlackglassCoastRequiresRuntimeCameraAnchors(t *testing.T) {
	var document blackglassSceneDoc
	if err := json.Unmarshal(blackglassCoastSceneDoc, &document); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"opening-camera", "cinematic-beacon"} {
		t.Run(id, func(t *testing.T) {
			markers := make(map[string]blackglassMarker, len(document.World.Markers))
			for markerID, marker := range document.World.Markers {
				markers[markerID] = marker
			}
			delete(markers, id)
			if err := validateBlackglassRuntimeMarkers(markers); err == nil || !strings.Contains(err.Error(), id) {
				t.Fatalf("missing anchor error = %v, want %q rejection", err, id)
			}
		})
	}
}
