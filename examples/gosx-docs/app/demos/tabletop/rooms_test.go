package docs

import (
	"encoding/json"
	"errors"
	"image/png"
	"io/fs"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"m31labs.dev/gosx/crdt"
	"m31labs.dev/gosx/scene"
)

func TestTabletopSeedSceneArtContract(t *testing.T) {
	room := newTabletopRoom("0123456789abcdef", time.Now())
	props := room.Props()
	if got := room.Metrics().Objects; got != 6 {
		t.Fatalf("seed object count = %d, want 6", got)
	}
	wantObjects := map[string]bool{
		"prop-ceramic": false, "prop-brass": false, "prop-plant": false,
		"prop-book": false, "prop-candle": false, "prop-orb": false,
	}
	woodTextureSet := false
	studioBackdrop := false
	contactShadowCount := 0
	modelCount := 0
	var visit func(scene.Node)
	visit = func(node scene.Node) {
		switch item := node.(type) {
		case scene.Model:
			modelCount++
			if _, ok := wantObjects[item.ID]; ok {
				wantObjects[item.ID] = true
			}
		case scene.Mesh:
			if _, ok := wantObjects[item.ID]; ok {
				wantObjects[item.ID] = true
			}
			if strings.HasSuffix(item.ID, "-contact-shadow") {
				material, materialOK := item.Material.(scene.FlatMaterial)
				geometry, plane := item.Geometry.(scene.PlaneGeometry)
				if materialOK && plane && geometry.Width > 0 && geometry.Height > 0 && material.Color == "#ffffff" && material.Texture == "/tabletop/contact-shadow.png" && material.BlendMode == scene.BlendAlpha {
					contactShadowCount++
				} else {
					t.Errorf("%s must use a textured soft contact shadow", item.ID)
				}
			}
			if item.ID == "tabletop-studio-backdrop" {
				material, ok := item.Material.(scene.FlatMaterial)
				studioBackdrop = ok && material.Texture == "/tabletop/studio-sweep.png" && item.Rotation.X > 1 && item.Rotation.X < 1.3
			}
			if item.ID == "tabletop-surface" {
				material, ok := item.Material.(scene.StandardMaterial)
				if !ok {
					t.Fatal("tabletop surface must use a StandardMaterial")
				}
				woodTextureSet = material.Texture == "/tabletop/surfaces/wood_table_001/wood_table_001_diff.png" &&
					material.NormalMap == "/tabletop/surfaces/wood_table_001/wood_table_001_nor_gl.png" &&
					material.RoughnessMap == "/tabletop/surfaces/wood_table_001/wood_table_001_rough.png"
			}
		case scene.Group:
			for _, child := range item.Children {
				visit(child)
			}
		}
	}
	for _, node := range props.Graph.Nodes {
		visit(node)
	}
	for id, present := range wantObjects {
		if !present {
			t.Errorf("seed still life is missing %s", id)
		}
	}
	if !woodTextureSet {
		t.Fatal("table surface must use albedo, OpenGL normal and roughness maps")
	}
	if !studioBackdrop {
		t.Fatal("tabletop scene must use a tilted studio sweep behind the still life")
	}
	if contactShadowCount != 6 {
		t.Fatalf("contact-shadow pads = %d, want one for each seeded prop", contactShadowCount)
	}
	if modelCount != 1 {
		t.Fatalf("seed scene downloads %d models, want 1", modelCount)
	}
	if props.Environment.Sky == nil || props.Environment.Sky.Mode != "gradient" {
		t.Fatal("tabletop backdrop must use the gradient sky")
	}
	if props.Background == "transparent" || props.CanvasAlpha == nil || *props.CanvasAlpha {
		t.Fatal("tabletop studio sweep must render into an opaque scene canvas")
	}
	if props.Camera.FOV < 27 || props.Camera.FOV > 40 || props.Camera.PortraitFOV < 27 || props.Camera.PortraitFOV > 40 {
		t.Fatalf("camera fields of view = %g/%g, want 27-40 degrees", props.Camera.FOV, props.Camera.PortraitFOV)
	}
	horizontal := math.Hypot(props.Camera.Position.X, props.Camera.Position.Z)
	elevation := math.Atan2(props.Camera.Position.Y-0.52, horizontal) * 180 / math.Pi
	if elevation < 15 || elevation > 30 {
		t.Fatalf("camera elevation = %.1f degrees, want 15-30", elevation)
	}
}

func TestTabletopContactShadowTextureHasSoftAlphaFalloff(t *testing.T) {
	file, err := os.Open("../../../public/tabletop/contact-shadow.png")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	img, err := png.Decode(file)
	if err != nil {
		t.Fatal(err)
	}
	bounds := img.Bounds()
	if bounds.Dx() != 128 || bounds.Dy() != 128 {
		t.Fatalf("contact shadow dimensions = %dx%d, want 128x128", bounds.Dx(), bounds.Dy())
	}
	alpha := func(x, y int) uint32 {
		_, _, _, a := img.At(x, y).RGBA()
		return a >> 8
	}
	if center := alpha(63, 63); center < 160 {
		t.Fatalf("contact shadow center alpha = %d, want at least 160", center)
	}
	if middle := alpha(107, 63); middle < 100 {
		t.Fatalf("contact shadow mid-falloff alpha = %d, want at least 100", middle)
	}
	if edge := alpha(127, 63); edge > 3 {
		t.Fatalf("contact shadow edge alpha = %d, want near-transparent", edge)
	}
}

func TestTabletopPlacementRules(t *testing.T) {
	tests := []struct {
		name  string
		kind  string
		x, z  float64
		count int
		want  error
	}{
		{name: "unknown kind", kind: "chair", x: 0, z: 0, count: 1, want: ErrInvalidKind},
		{name: "outside surface", kind: "plant", x: tabletopSurfaceEdge + 0.1, z: 0, count: 1, want: ErrOutOfBounds},
		{name: "non finite position", kind: "plant", x: 0, z: math.Inf(1), count: 1, want: ErrOutOfBounds},
		{name: "sixty fifth object", kind: "book", x: 0, z: 0, count: maxTabletopObjects, want: ErrObjectLimit},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := ValidatePlacement(test.kind, test.x, test.z, test.count); !errors.Is(err, test.want) {
				t.Fatalf("ValidatePlacement() error = %v, want %v", err, test.want)
			}
		})
	}
	if err := ValidatePlacement("ceramic", 0.7, -0.8, 4); err != nil {
		t.Fatalf("valid placement rejected: %v", err)
	}
}

func TestTabletopSourceReceiptMatchesCurrentFiles(t *testing.T) {
	data, err := os.ReadFile("receipts.json")
	if err != nil {
		t.Fatal(err)
	}
	var got TabletopReceipt
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	wantFiles := []string{"rooms.go", "page.server.go", "page.gsx"}
	if len(got.SourceFiles) != len(wantFiles) {
		t.Fatalf("receipt has %d source files, want %d", len(got.SourceFiles), len(wantFiles))
	}
	for index, name := range wantFiles {
		if got.SourceFiles[index].Path != name {
			t.Fatalf("source %d path = %q, want %q", index, got.SourceFiles[index].Path, name)
		}
		source, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if got.SourceFiles[index].Lines != sourceLineCount(source) {
			t.Errorf("receipt line count for %s = %d, current source has %d", name, got.SourceFiles[index].Lines, sourceLineCount(source))
		}
	}
	jsLines := 0
	for _, directory := range []string{".", filepath.Join("..", "..", "..", "..", "gosx-docs", "public", "tabletop")} {
		err := filepath.WalkDir(directory, func(path string, entry fs.DirEntry, err error) error {
			if err != nil || entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".js") {
				return err
			}
			source, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			jsLines += sourceLineCount(source)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if got.JavaScriptLines != jsLines {
		t.Fatalf("receipt says %d JavaScript lines, found %d", got.JavaScriptLines, jsLines)
	}
}

func sourceLineCount(source []byte) int {
	if len(source) == 0 {
		return 0
	}
	count := strings.Count(string(source), "\n")
	if source[len(source)-1] != '\n' {
		count++
	}
	return count
}

func TestTabletopVisitorLimit(t *testing.T) {
	now := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)
	room := newTabletopRoom("0123456789abcdef", now)
	for i := 0; i < maxTabletopVisitors; i++ {
		if !room.addVisitor(string(rune('a'+i)), now) {
			t.Fatalf("visitor %d was rejected before the room limit", i+1)
		}
	}
	if room.addVisitor("ninth", now) {
		t.Fatal("the ninth visitor was admitted")
	}
}

func TestTabletopClientRateLimit(t *testing.T) {
	room := newTabletopRoom("0123456789abcdef", time.Unix(10, 0))
	start := time.Unix(11, 0)
	for i := 0; i < 15; i++ {
		if !room.allow("visitor-a", "pointer", start.Add(time.Duration(i)*time.Millisecond), 15) {
			t.Fatalf("pointer update %d was rejected inside the limit", i+1)
		}
	}
	if room.allow("visitor-a", "pointer", start.Add(20*time.Millisecond), 15) {
		t.Fatal("sixteenth pointer update in one second was admitted")
	}
	if !room.allow("visitor-b", "pointer", start.Add(20*time.Millisecond), 15) {
		t.Fatal("one visitor's rate limit affected another visitor")
	}
}

func TestTabletopIdleRoomExpiryAndRoomCap(t *testing.T) {
	now := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)
	manager := NewRoomManager()
	manager.idle = 30 * time.Minute
	manager.maxRooms = 1
	first, err := manager.GetOrCreate("0123456789abcdef", now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.GetOrCreate("fedcba9876543210", now); !errors.Is(err, ErrRoomLimit) {
		t.Fatalf("second room error = %v, want room limit", err)
	}
	if got := manager.Sweep(now.Add(29 * time.Minute)); got != 0 {
		t.Fatalf("sweep at 29 minutes removed %d rooms", got)
	}
	if got := manager.Sweep(now.Add(31 * time.Minute)); got != 1 {
		t.Fatalf("sweep after 30 minutes removed %d rooms, want 1", got)
	}
	if manager.Count() != 0 {
		t.Fatalf("room manager has %d rooms after expiry", manager.Count())
	}
	if _, err := manager.GetOrCreate(first.ID, now.Add(32*time.Minute)); err != nil {
		t.Fatalf("room slot was not released after expiry: %v", err)
	}
}

func TestUnconnectedRoomPageDoesNotReserveRoom(t *testing.T) {
	manager := NewRoomManager()
	manager.maxRooms = 1
	manager.newID = func() (string, error) { return "0123456789abcdef", nil }

	id, err := manager.NewPrivateRoomID()
	if err != nil {
		t.Fatal(err)
	}
	if manager.Count() != 0 {
		t.Fatalf("private URL creation reserved %d rooms", manager.Count())
	}

	preview, active, err := manager.GetForPage(id, time.Unix(20, 0))
	if err != nil {
		t.Fatal(err)
	}
	if active || preview.ID != id {
		t.Fatalf("page preview = (%q, active %t), want (%q, false)", preview.ID, active, id)
	}
	if manager.Count() != 0 {
		t.Fatalf("unconnected page reserved %d rooms", manager.Count())
	}

	room, err := manager.GetOrCreate(id, time.Unix(20, 0))
	if err != nil {
		t.Fatal(err)
	}
	if manager.Count() != 1 || room.ID != id {
		t.Fatalf("WebSocket allocation produced %q and %d rooms", room.ID, manager.Count())
	}
	if _, _, err := manager.GetForPage("fedcba9876543210", time.Unix(20, 0)); !errors.Is(err, ErrRoomLimit) {
		t.Fatalf("page preview at room cap error = %v, want %v", err, ErrRoomLimit)
	}
}

func TestOrdinaryHTTPRequestCannotAllocateTabletopRoom(t *testing.T) {
	previous := Rooms
	Rooms = NewRoomManager()
	t.Cleanup(func() { Rooms = previous })

	request := httptest.NewRequest(http.MethodGet, "/demos/tabletop/ws/?room=0123456789abcdef", nil)
	response := httptest.NewRecorder()
	ServeWebSocket(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("ordinary HTTP response = %d, want %d", response.Code, http.StatusBadRequest)
	}
	if got := Rooms.Count(); got != 0 {
		t.Fatalf("ordinary HTTP request allocated %d rooms", got)
	}

	request = httptest.NewRequest(http.MethodGet, "/demos/tabletop/ws/?room=0123456789abcdef", nil)
	request.Header.Set("Connection", "Upgrade")
	request.Header.Set("Upgrade", "websocket")
	request.Header.Set("Sec-WebSocket-Version", "13")
	request.Header.Set("Sec-WebSocket-Key", "invalid")
	request.Header.Set("Origin", "http://example.com")
	response = httptest.NewRecorder()
	ServeWebSocket(response, request)
	if response.Code != http.StatusBadRequest || Rooms.Count() != 0 {
		t.Fatalf("malformed upgrade response = %d with %d allocated rooms", response.Code, Rooms.Count())
	}
}

func TestTabletopConcurrentCRDTObjectEditsConverge(t *testing.T) {
	left := crdt.NewDoc()
	right := crdt.NewDoc()
	if err := left.Put(crdt.Root, crdt.Prop("tabletop/object-left"), crdt.StringValue("ceramic")); err != nil {
		t.Fatal(err)
	}
	if _, err := left.Commit("left visitor placed a vase"); err != nil {
		t.Fatal(err)
	}
	if err := right.Put(crdt.Root, crdt.Prop("tabletop/object-right"), crdt.StringValue("plant")); err != nil {
		t.Fatal(err)
	}
	if _, err := right.Commit("right visitor placed a plant"); err != nil {
		t.Fatal(err)
	}
	if err := left.Merge(right); err != nil {
		t.Fatal(err)
	}
	if err := right.Merge(left); err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]string{"tabletop/object-left": "ceramic", "tabletop/object-right": "plant"} {
		for name, doc := range map[string]*crdt.Doc{"left": left, "right": right} {
			value, _, err := doc.Get(crdt.Root, crdt.Prop(key))
			if err != nil {
				t.Fatalf("%s read %s: %v", name, key, err)
			}
			if value.Str != want {
				t.Fatalf("%s value for %s = %q, want %q", name, key, value.Str, want)
			}
		}
	}
}
