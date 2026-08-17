package imagepipe

import (
	"reflect"
	"testing"
)

// gosxDefaultCandidates mirrors server.AutoImageWidths(0)'s own candidate
// list. It is duplicated here, deliberately, rather than imported: imagepipe
// must not import package server (that direction is fine; the forbidden one
// -- server importing imagepipe -- is what
// TestServerPackageTreeNeverImportsImagepipe in the repo root enforces).
// cmd/gosx's build stage passes the real server.AutoImageWidths(0) slice as
// Ladder's candidates argument; this copy exists only so Ladder's own tests
// don't need to import all of package server to exercise realistic input.
var gosxDefaultCandidates = []int{320, 480, 640, 750, 828, 1080, 1200, 1920, 2048, 3840}

func TestLadderCapsAtIntrinsicWidth(t *testing.T) {
	got := Ladder(1200, gosxDefaultCandidates)
	want := []int{320, 480, 640, 750, 828, 1080, 1200}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Ladder(1200, ...) = %v, want %v", got, want)
	}
}

func TestLadderNeverUpscales(t *testing.T) {
	for _, intrinsic := range []int{1, 100, 319, 320, 321, 1200, 3839, 3840, 9000} {
		for _, width := range Ladder(intrinsic, gosxDefaultCandidates) {
			if width > intrinsic {
				t.Errorf("Ladder(%d, ...) produced width %d > intrinsic %d", intrinsic, width, intrinsic)
			}
		}
	}
}

func TestLadderAppendsIntrinsicWidthWhenNotACandidate(t *testing.T) {
	got := Ladder(1000, gosxDefaultCandidates)
	if len(got) == 0 || got[len(got)-1] != 1000 {
		t.Fatalf("Ladder(1000, ...) = %v, want the ladder to end at 1000 exactly", got)
	}
}

func TestLadderDoesNotDuplicateExactCandidateMatch(t *testing.T) {
	got := Ladder(1200, gosxDefaultCandidates)
	count := 0
	for _, width := range got {
		if width == 1200 {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("Ladder(1200, ...) contains 1200 %d times, want exactly 1: %v", count, got)
	}
}

func TestLadderSmallIntrinsicWidthReturnsOnlyItself(t *testing.T) {
	got := Ladder(200, gosxDefaultCandidates)
	want := []int{200}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Ladder(200, ...) = %v, want %v (smaller than every candidate)", got, want)
	}
}

func TestLadderNonPositiveIntrinsicDisablesCap(t *testing.T) {
	for _, intrinsic := range []int{0, -1, -100} {
		got := Ladder(intrinsic, gosxDefaultCandidates)
		if !reflect.DeepEqual(got, gosxDefaultCandidates) {
			t.Fatalf("Ladder(%d, ...) = %v, want candidates unchanged %v", intrinsic, got, gosxDefaultCandidates)
		}
	}
}

func TestLadderDeduplicatesAndSortsArbitraryCandidates(t *testing.T) {
	got := Ladder(1000, []int{500, 100, 100, -5, 0, 900, 500})
	want := []int{100, 500, 900, 1000}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Ladder = %v, want %v", got, want)
	}
}

func TestLadderEmptyCandidatesReturnsOnlyIntrinsic(t *testing.T) {
	got := Ladder(800, nil)
	want := []int{800}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Ladder(800, nil) = %v, want %v", got, want)
	}
}
