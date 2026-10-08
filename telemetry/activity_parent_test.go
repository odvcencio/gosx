//go:build !js || !wasm

package telemetry

import (
	"context"
	"testing"
)

func TestActivityKnownParentCatalogRetainsIDsWithoutFinalRecords(t *testing.T) {
	tel, _ := lifecycleOwner(t)
	k := emptyActivityKind(t, tel)
	parent, err := k.Begin(ActivityStart[NoFields]{})
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := parent.End(ActivityEnd[NoFields]{})
	if err != nil {
		t.Fatal(err)
	}
	tel.collectActivityReceipts()
	if err = receipt.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(tel.activities.live) != 0 {
		t.Fatal("final record remained live")
	}
	child, err := k.Begin(ActivityStart[NoFields]{ParentID: parent.ID()})
	if err != nil {
		t.Fatal("known acknowledged parent rejected", err)
	}
	view, _ := child.Snapshot()
	if view.ParentID != string(parent.ID()) {
		t.Fatal(view)
	}
	for i := 0; i < 256; i++ {
		activity, err := k.Begin(ActivityStart[NoFields]{})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = activity.End(ActivityEnd[NoFields]{}); err != nil {
			t.Fatal(err)
		}
		tel.collectActivityReceipts()
	}
	if len(tel.activities.live) != 1 {
		t.Fatal("catalog retained historical records")
	}
	if _, err = k.Begin(ActivityStart[NoFields]{ParentID: parent.ID()}); err == nil {
		t.Fatal("evicted parent still known")
	}
	if _, err = k.Begin(ActivityStart[NoFields]{ParentID: child.ID()}); err != nil {
		t.Fatal("live parent displaced by ID catalog", err)
	}
}
