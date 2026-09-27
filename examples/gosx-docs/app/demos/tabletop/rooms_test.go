package docs

import (
	"encoding/json"
	"errors"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"m31labs.dev/gosx/crdt"
)

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
