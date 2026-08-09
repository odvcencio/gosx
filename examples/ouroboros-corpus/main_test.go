package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type documentContractProbe struct {
	Enhancement struct {
		Bootstrap  bool `json:"bootstrap"`
		Runtime    bool `json:"runtime"`
		Navigation bool `json:"navigation"`
	} `json:"enhancement"`
	Assets struct {
		BootstrapMode               string `json:"bootstrapMode"`
		RuntimePath                 string `json:"runtimePath"`
		WASMExecPath                string `json:"wasmExecPath"`
		BootstrapFeatureEnginesPath string `json:"bootstrapFeatureEnginesPath"`
		Islands                     int    `json:"islands"`
		Engines                     int    `json:"engines"`
		SelfDescribingSurfaces      int    `json:"selfDescribingSurfaces"`
		Hubs                        int    `json:"hubs"`
	} `json:"assets"`
}

type islandManifestProbe struct {
	Islands []struct {
		Component  string `json:"component"`
		ProgramRef string `json:"programRef"`
	} `json:"islands"`
	Engines []json.RawMessage `json:"engines"`
	Hubs    []json.RawMessage `json:"hubs"`
}

type perfCorpusProbe struct {
	CorpusID      string `json:"corpusID"`
	FixtureRoutes []struct {
		ID                    string   `json:"id"`
		Route                 string   `json:"route"`
		FixtureApp            string   `json:"fixtureApp"`
		Purpose               string   `json:"purpose"`
		ExpectedRuntime       string   `json:"expectedRuntime"`
		ExpectedTinyGoCurrent string   `json:"expectedTinyGoCurrent"`
		ExpectedTinyGoFuture  string   `json:"expectedTinyGoFuture"`
		ExpectedCapabilities  []string `json:"expectedCapabilities"`
	} `json:"fixtureRoutes"`
	RuntimeVariants []struct {
		ID               string   `json:"id"`
		Generation       string   `json:"generation"`
		SelectedByRoutes []string `json:"selectedByRoutes"`
	} `json:"runtimeVariants"`
}

func TestFixtureManifestMatchesJSON(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(corpusRoot(), "fixtures.v1.json"))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var disk RouteManifest
	if err := json.Unmarshal(data, &disk); err != nil {
		t.Fatalf("decode manifest: %v", err)
	}
	got, err := json.Marshal(corpusManifest)
	if err != nil {
		t.Fatalf("marshal code manifest: %v", err)
	}
	want, err := json.Marshal(disk)
	if err != nil {
		t.Fatalf("marshal disk manifest: %v", err)
	}
	if string(got) != string(want) {
		t.Fatalf("manifest drift\ncode=%s\ndisk=%s", got, want)
	}
	if len(corpusManifest.Routes) != 12 {
		t.Fatalf("routes = %d, want 12", len(corpusManifest.Routes))
	}
	for _, id := range []string{"R09A", "R09B", "R10"} {
		if !manifestHasRoute(id) {
			t.Fatalf("manifest missing %s", id)
		}
	}
}

func TestFixtureManifestMatchesCanonicalPerfCorpus(t *testing.T) {
	perf := readPerfCorpus(t)
	if perf.CorpusID != corpusManifest.CorpusID {
		t.Fatalf("corpusID = %q, want %q", corpusManifest.CorpusID, perf.CorpusID)
	}
	if len(corpusManifest.Routes) != len(perf.FixtureRoutes) {
		t.Fatalf("fixture routes = %d, canonical = %d", len(corpusManifest.Routes), len(perf.FixtureRoutes))
	}

	fixtureByID := map[string]RouteRecord{}
	for _, record := range corpusManifest.Routes {
		fixtureByID[record.ID] = record
	}
	for _, canonical := range perf.FixtureRoutes {
		record, ok := fixtureByID[canonical.ID]
		if !ok {
			t.Fatalf("fixture missing canonical route %s", canonical.ID)
		}
		if record.Route != canonical.Route || record.FixtureApp != canonical.FixtureApp || record.Purpose != canonical.Purpose || record.ExpectedRuntime != canonical.ExpectedRuntime {
			t.Fatalf("%s route identity drift\nfixture=%+v\ncanonical=%+v", canonical.ID, record, canonical)
		}
		if record.ExpectedTinyGoCurrent != canonical.ExpectedTinyGoCurrent || record.ExpectedTinyGoFuture != canonical.ExpectedTinyGoFuture {
			t.Fatalf("%s variant drift: fixture current/future %q/%q canonical %q/%q",
				canonical.ID, record.ExpectedTinyGoCurrent, record.ExpectedTinyGoFuture, canonical.ExpectedTinyGoCurrent, canonical.ExpectedTinyGoFuture)
		}
		if strings.Join(record.ExpectedCapabilities, "\x00") != strings.Join(canonical.ExpectedCapabilities, "\x00") {
			t.Fatalf("%s capability drift: fixture=%v canonical=%v", canonical.ID, record.ExpectedCapabilities, canonical.ExpectedCapabilities)
		}
		if got, want := record.External, canonical.FixtureApp != fixtureApp; got != want {
			t.Fatalf("%s external = %v, want %v", canonical.ID, got, want)
		}
	}
	assertCurrentVariantSelections(t, perf)
}

func TestFixtureRoutesServeAndDeclarePlans(t *testing.T) {
	app, err := newApp()
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	handler := app.Build()
	for _, record := range corpusManifest.Routes {
		if record.External {
			continue
		}
		t.Run(record.ID, func(t *testing.T) {
			body, status := getRoute(t, handler, record.Route)
			if status != http.StatusOK {
				t.Fatalf("%s status = %d", record.Route, status)
			}
			assertContains(t, body, `data-route-path="`+record.Route+`"`)
			switch record.ID {
			case "R00":
				assertContains(t, body, `"bootstrap":false`)
				assertNotContains(t, body, `id="gosx-manifest"`)
				assertNotContains(t, body, `data-gosx-script=`)
				assertNotContains(t, body, `/gosx/`)
			case "R01":
				assertContains(t, body, `data-gosx-bootstrap-mode="lite"`)
				assertNotContains(t, body, `wasm_exec`)
			case "R02":
				assertContains(t, body, `data-gosx-island="Counter"`)
				assertContains(t, body, `"islands":1`)
				assertContains(t, body, `action="/action/form/__actions/validate-name"`)
			case "R03":
				manifest := routeManifest(t, body)
				if len(manifest.Islands) != 5 {
					t.Fatalf("R03 islands = %d, want 5", len(manifest.Islands))
				}
				shared := 0
				for _, island := range manifest.Islands {
					if island.Component == "SharedSelection" {
						shared++
						if island.ProgramRef != "/_ouroboros/islands/SharedSelection.json" {
							t.Fatalf("SharedSelection programRef = %q", island.ProgramRef)
						}
					}
				}
				if shared != 2 {
					t.Fatalf("SharedSelection manifest entries = %d, want 2", shared)
				}
			case "R04":
				assertContains(t, body, `data-action-name="validate-name"`)
				assertContains(t, body, `data-gosx-action="POST /action/form/__actions/validate-name"`)
				assertContains(t, body, `data-gosx-bootstrap-mode="lite"`)
				assertNotContains(t, body, `wasm_exec`)
			case "R05":
				assertContains(t, body, `data-gosx-surface-kind="canvas2d"`)
				assertContains(t, body, `data-gosx-engine-component="CanvasBoard"`)
				assertNotContains(t, body, `data-gosx-engine="CanvasBoard"`)
				contract := documentContract(t, body)
				if !contract.Enhancement.Runtime || contract.Assets.RuntimePath == "" || contract.Assets.WASMExecPath == "" || contract.Assets.BootstrapFeatureEnginesPath == "" {
					t.Fatalf("R05 runtime contract = %+v", contract)
				}
				if contract.Assets.Engines != 0 || contract.Assets.SelfDescribingSurfaces != 1 {
					t.Fatalf("R05 surface counts = %+v", contract.Assets)
				}
				manifest := routeManifest(t, body)
				if len(manifest.Engines) != 0 {
					t.Fatalf("R05 engine manifest = %+v", manifest.Engines)
				}
			case "R06":
				assertContains(t, body, `"hubs":1`)
				assertContains(t, body, `$ouroboros.echo`)
				assertNoWASMRuntimePath(t, body)
			case "R07":
				assertContains(t, body, `data-gosx-engine="GoSXVideo"`)
				assertContains(t, body, `"syncMode": "follow"`)
				assertContains(t, body, `"src": "/media/ouroboros-placeholder.mp4"`)
				assertContains(t, body, `"sync": "/_ouroboros/video-sync"`)
				assertNoWASMRuntimePath(t, body)
			case "R08":
				assertContains(t, body, `data-gosx-engine="GoSXScene3D"`)
				assertContains(t, body, `feature-scene3d`)
				assertNotContains(t, body, `"preferWebGL"`)
				assertNoWASMRuntimePath(t, body)
			case "R09A", "R09B":
				assertContains(t, body, `data-gosx-link`)
				assertContains(t, body, `data-navigation-island=`)
				contract := documentContract(t, body)
				if !contract.Enhancement.Navigation || !contract.Enhancement.Runtime || contract.Assets.Islands != 1 || contract.Assets.RuntimePath == "" {
					t.Fatalf("%s navigation runtime contract = %+v", record.ID, contract)
				}
			}
		})
	}
}

func TestTinyGoCurrentVariantsAreCanonical(t *testing.T) {
	perf := readPerfCorpus(t)
	assertCurrentVariantSelections(t, perf)
	allowedCurrent := map[string]bool{"runtime": true, "islands": true, "none": true}
	for _, record := range corpusManifest.Routes {
		if !allowedCurrent[record.ExpectedTinyGoCurrent] {
			t.Fatalf("%s current variant = %q, want runtime/islands/none", record.ID, record.ExpectedTinyGoCurrent)
		}
	}

	app, err := newApp()
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	handler := app.Build()
	for _, record := range corpusManifest.Routes {
		if record.External {
			continue
		}
		body, status := getRoute(t, handler, record.Route)
		if status != http.StatusOK {
			t.Fatalf("%s status = %d", record.Route, status)
		}
		contract := documentContract(t, body)
		switch record.ExpectedTinyGoCurrent {
		case "islands":
			if contract.Assets.RuntimePath == "" || contract.Assets.WASMExecPath == "" || contract.Assets.Islands == 0 {
				t.Fatalf("%s islands current route missing island runtime evidence: %+v", record.ID, contract)
			}
		case "runtime":
			if !contract.Enhancement.Bootstrap || contract.Assets.BootstrapMode == "none" {
				t.Fatalf("%s runtime current route missing bootstrap evidence: %+v", record.ID, contract)
			}
		case "none":
			if record.ID != "R09A" && record.ID != "R09B" && contract.Assets.RuntimePath != "" {
				t.Fatalf("%s none current route unexpectedly declares runtime path: %+v", record.ID, contract)
			}
		}
	}
}

func TestFutureVariantsUseO02Names(t *testing.T) {
	allowed := map[string]bool{
		"core": true, "engine": true, "collab": true, "full": true, "none": true,
	}
	for _, record := range corpusManifest.Routes {
		if !allowed[record.ExpectedTinyGoFuture] {
			t.Fatalf("%s ExpectedTinyGoFuture = %q", record.ID, record.ExpectedTinyGoFuture)
		}
	}
}

func TestLocalVideoFixtureServesBoundedMP4(t *testing.T) {
	app, err := newApp()
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	handler := app.Build()
	req := httptest.NewRequest(http.MethodGet, "/media/ouroboros-placeholder.mp4", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("media status = %d", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "video/mp4" {
		t.Fatalf("content-type = %q", got)
	}
	if rec.Header().Get("Content-Length") == "" {
		t.Fatal("missing Content-Length")
	}
	data := rec.Body.Bytes()
	if len(data) != len(fixtureMP4()) || len(data) > 4096 {
		t.Fatalf("media size = %d, fixture = %d, want bounded playable sample", len(data), len(fixtureMP4()))
	}
	boxes := parseTopLevelMP4Boxes(t, data)
	t.Logf("fixture MP4 size=%d boxes=%v", len(data), boxes)
	for _, box := range []string{"ftyp", "moov", "mdat"} {
		if boxes[box] == 0 {
			t.Fatalf("fixture MP4 missing %s box: %v", box, boxes)
		}
	}
	for _, marker := range [][]byte{[]byte("trak"), []byte("stsd"), []byte("stts"), []byte("stsz"), []byte("stco"), []byte("avc1"), []byte("avcC")} {
		if !bytes.Contains(data, marker) {
			t.Fatalf("fixture MP4 missing sample metadata marker %q", marker)
		}
	}

	rangeReq := httptest.NewRequest(http.MethodGet, "/media/ouroboros-placeholder.mp4", nil)
	rangeReq.Header.Set("Range", "bytes=0-31")
	rangeRec := httptest.NewRecorder()
	handler.ServeHTTP(rangeRec, rangeReq)
	if rangeRec.Code != http.StatusPartialContent {
		t.Fatalf("range status = %d", rangeRec.Code)
	}
	if got := rangeRec.Header().Get("Content-Range"); !strings.HasPrefix(got, "bytes 0-31/") {
		t.Fatalf("Content-Range = %q", got)
	}
	if rangeRec.Body.Len() != 32 {
		t.Fatalf("range body = %d bytes, want 32", rangeRec.Body.Len())
	}
}

func TestFixtureMP4FFProbeIfAvailable(t *testing.T) {
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("ffprobe not available")
	}
	tmp := filepath.Join(t.TempDir(), "fixture.mp4")
	if err := os.WriteFile(tmp, fixtureMP4(), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-select_streams", "v:0", "-show_entries", "stream=codec_name,width,height,duration", "-of", "json", tmp).CombinedOutput()
	if err != nil {
		t.Fatalf("ffprobe failed: %v\n%s", err, out)
	}
	var probe struct {
		Streams []struct {
			CodecName string `json:"codec_name"`
			Width     int    `json:"width"`
			Height    int    `json:"height"`
			Duration  string `json:"duration"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(out, &probe); err != nil {
		t.Fatalf("decode ffprobe: %v\n%s", err, out)
	}
	if len(probe.Streams) != 1 || probe.Streams[0].CodecName != "h264" || probe.Streams[0].Width != 16 || probe.Streams[0].Height != 16 {
		t.Fatalf("unexpected ffprobe stream: %+v", probe.Streams)
	}
	t.Logf("ffprobe stream codec=%s size=%dx%d duration=%s", probe.Streams[0].CodecName, probe.Streams[0].Width, probe.Streams[0].Height, probe.Streams[0].Duration)
}

func TestActionEndpointIsLocalAndDeterministic(t *testing.T) {
	app, err := newApp()
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	handler := app.Build()
	for _, tc := range []struct {
		name       string
		form       string
		acceptJSON bool
		wantStatus int
		want       string
	}{
		{name: "invalid JSON", form: "name=", acceptJSON: true, wantStatus: http.StatusUnprocessableEntity, want: "name required"},
		{name: "valid JSON", form: "name=Ada", acceptJSON: true, wantStatus: http.StatusOK, want: "accepted Ada"},
		{name: "valid redirect", form: "name=Ada", wantStatus: http.StatusSeeOther, want: "/action/form?ok=1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/action/form/__actions/validate-name", strings.NewReader(tc.form))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			if tc.acceptJSON {
				req.Header.Set("Accept", "application/json")
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d body=%s", rec.Code, tc.wantStatus, rec.Body.String())
			}
			got := rec.Body.String() + rec.Header().Get("Location")
			if !strings.Contains(got, tc.want) {
				t.Fatalf("response missing %q in body/location %q", tc.want, got)
			}
		})
	}
}

func TestRepresentativeRoutesOverRealListener(t *testing.T) {
	app, err := newApp()
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := listener.Addr().String()
	srv := &http.Server{Handler: app.Build(), ReadHeaderTimeout: 2 * time.Second}
	done := make(chan error, 1)
	go func() {
		err := srv.Serve(listener)
		if err == http.ErrServerClosed {
			err = nil
		}
		done <- err
	}()
	client := &http.Client{Timeout: 2 * time.Second}
	for _, route := range []string{"/static", "/canvas-board", "/scene/basic", "/navigation/a", "/navigation/b", "/media/ouroboros-placeholder.mp4"} {
		resp, err := client.Get("http://" + addr + route)
		if err != nil {
			t.Fatalf("GET %s: %v", route, err)
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 512*1024))
		closeErr := resp.Body.Close()
		if readErr != nil {
			t.Fatalf("read %s: %v", route, readErr)
		}
		if closeErr != nil {
			t.Fatalf("close %s: %v", route, closeErr)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s status = %d body=%s", route, resp.StatusCode, string(body))
		}
		if route != "/media/ouroboros-placeholder.mp4" && !bytes.Contains(body, []byte(`data-route-path=`)) {
			t.Fatalf("%s missing route marker", route)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("serve: %v", err)
	}
	probe, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatalf("listener remained active on %s: %v", addr, err)
	}
	_ = probe.Close()
}

func manifestHasRoute(id string) bool {
	for _, record := range corpusManifest.Routes {
		if record.ID == id {
			return true
		}
	}
	return false
}

func readPerfCorpus(t *testing.T) perfCorpusProbe {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoRoot(), "perf", "ouroboros", "corpus.v1.json"))
	if err != nil {
		t.Fatalf("read perf corpus: %v", err)
	}
	var perf perfCorpusProbe
	if err := json.Unmarshal(data, &perf); err != nil {
		t.Fatalf("decode perf corpus: %v", err)
	}
	return perf
}

func assertCurrentVariantSelections(t *testing.T, perf perfCorpusProbe) {
	t.Helper()
	currentSelection := map[string]string{}
	for _, variant := range perf.RuntimeVariants {
		if variant.Generation != "current" {
			continue
		}
		if variant.ID != "runtime" && variant.ID != "islands" {
			t.Fatalf("unexpected current runtime variant %q", variant.ID)
		}
		for _, routeID := range variant.SelectedByRoutes {
			currentSelection[routeID] = variant.ID
		}
	}
	for _, record := range corpusManifest.Routes {
		want := currentSelection[record.ID]
		if want == "" {
			want = "none"
		}
		if record.ExpectedTinyGoCurrent != want {
			t.Fatalf("%s current variant = %q, want %q from canonical selectedByRoutes", record.ID, record.ExpectedTinyGoCurrent, want)
		}
	}
}

func getRoute(t *testing.T, handler http.Handler, route string) (string, int) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, route, nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec.Body.String(), rec.Code
}

func documentContract(t *testing.T, body string) documentContractProbe {
	t.Helper()
	payload := scriptPayload(t, body, `id="gosx-document"`)
	var contract documentContractProbe
	if err := json.Unmarshal([]byte(payload), &contract); err != nil {
		t.Fatalf("decode document contract: %v\n%s", err, payload)
	}
	return contract
}

func routeManifest(t *testing.T, body string) islandManifestProbe {
	t.Helper()
	payload := scriptPayload(t, body, `id="gosx-manifest"`)
	var manifest islandManifestProbe
	if err := json.Unmarshal([]byte(payload), &manifest); err != nil {
		t.Fatalf("decode route manifest: %v\n%s", err, payload)
	}
	return manifest
}

func scriptPayload(t *testing.T, body, marker string) string {
	t.Helper()
	idx := strings.Index(body, marker)
	if idx < 0 {
		t.Fatalf("missing script marker %q in %s", marker, body)
	}
	start := strings.Index(body[idx:], ">")
	if start < 0 {
		t.Fatalf("malformed script for %q", marker)
	}
	start += idx + 1
	end := strings.Index(body[start:], "</script>")
	if end < 0 {
		t.Fatalf("unterminated script for %q", marker)
	}
	return body[start : start+end]
}

func assertNoWASMRuntimePath(t *testing.T, body string) {
	t.Helper()
	contract := documentContract(t, body)
	if contract.Assets.RuntimePath != "" || contract.Assets.WASMExecPath != "" {
		t.Fatalf("unexpected WASM runtime path: %+v", contract.Assets)
	}
}

func assertMP4HasBoxes(t *testing.T, data []byte) {
	t.Helper()
	boxes := parseTopLevelMP4Boxes(t, data)
	if boxes["ftyp"] == 0 || boxes["moov"] == 0 || boxes["mdat"] == 0 {
		t.Fatalf("fixture MP4 missing required boxes: %v", boxes)
	}
}

func parseTopLevelMP4Boxes(t *testing.T, data []byte) map[string]int {
	t.Helper()
	boxes := map[string]int{}
	for offset := 0; offset < len(data); {
		if len(data)-offset < 8 {
			t.Fatalf("trailing MP4 bytes at offset %d", offset)
		}
		size := int(binary.BigEndian.Uint32(data[offset : offset+4]))
		kind := string(data[offset+4 : offset+8])
		if size == 1 {
			t.Fatalf("64-bit MP4 box size not expected in bounded fixture for %s", kind)
		}
		if size < 8 || offset+size > len(data) {
			t.Fatalf("invalid MP4 box %s size %d at offset %d len %d", kind, size, offset, len(data))
		}
		boxes[kind]++
		offset += size
	}
	return boxes
}

func assertContains(t *testing.T, body, needle string) {
	t.Helper()
	if !strings.Contains(body, needle) {
		t.Fatalf("missing %q in %s", needle, body)
	}
}

func assertNotContains(t *testing.T, body, needle string) {
	t.Helper()
	if strings.Contains(body, needle) {
		t.Fatalf("unexpected %q in %s", needle, body)
	}
}
