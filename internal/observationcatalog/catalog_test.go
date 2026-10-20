package observationcatalog

import (
	"fmt"
	"reflect"
	"testing"
)

func TestBoundedSelectionAndMethods(t *testing.T) {
	c := New(3)
	for i := 8191; i >= 0; i-- {
		c.Register("page", fmt.Sprintf("GET /p/%04d", i))
		if len(c.rows) > 3 || cap(c.rows) > 6 {
			t.Fatalf("selection retained %d rows, capacity %d", len(c.rows), cap(c.rows))
		}
	}
	rows, overflow := c.Result()
	if !overflow || len(rows) != 6 || rows[0].Pattern != "/p/0000" || rows[2].Pattern != "/p/0002" || rows[3].Kind != "page" {
		t.Fatalf("selection: %#v, overflow=%v", rows, overflow)
	}
	c.Register("page", "POST /p/0000")
	c.Register("page", "GET /p/0000")
	rows, _ = c.Result()
	if !reflect.DeepEqual(rows[0].Methods, []string{"GET", "HEAD", "POST"}) {
		t.Fatal(rows[0].Methods)
	}
	for i := 1000; i >= 0; i-- {
		c.Register("page", fmt.Sprintf("M%04d /p/0000", i))
	}
	rows, _ = c.Result()
	if len(rows[0].Methods) != MaxMethods || rows[0].Methods[2] != "M0000" {
		t.Fatal(rows[0].Methods)
	}
}

func TestDefaultLimitCloneAndAnyMethod(t *testing.T) {
	c := New(0)
	c.Register("mount", "/nested/")
	rows, overflow := c.Result()
	if c.limit != 512 || overflow || rows[0].Methods[0] != "*" {
		t.Fatalf("default: %#v, %v", rows, overflow)
	}
	clone := Clone(rows)
	clone[0].Pattern = "/changed"
	clone[0].Methods[0] = "DELETE"
	if rows[0].Pattern != "/nested/" || rows[0].Methods[0] != "*" {
		t.Fatal("clone changed source")
	}
}

func TestPatternAndKindPriorityIgnoresTraversalOrder(t *testing.T) {
	for _, kinds := range [][]string{{"mount", "api", "page"}, {"page", "api", "mount"}} {
		c := New(1)
		for _, kind := range kinds {
			c.Register(kind, "GET /same")
		}
		rows, overflow := c.Result()
		if !overflow || len(rows) != 2 || rows[0].Kind != "error" || rows[1].Kind != "page" {
			t.Fatalf("selection: %#v overflow=%v", rows, overflow)
		}
		if !reflect.DeepEqual(rows[0].Methods, rows[1].Methods) {
			t.Fatal("error methods differ")
		}
	}
}
