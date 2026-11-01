//go:build js && wasm

package wasm

import (
	"testing"

	"syscall/js"
)

func TestSceneOceanWaterReaderFollowsThePageRecord(t *testing.T) {
	host := js.Global().Call("eval", `({isCurrent(){return true}, asked:0, record:new Float64Array(9),
 scene3D(method,mount){ if(method!=='oceanWater') throw new Error('unexpected '+method); this.asked++; this.mount=mount; return this.ready===false?null:this.record }})`)
	target := Context{value: host}.Scene3DByID("ship")
	host.Set("ready", false)
	if _, err := target.OceanWater(); err == nil {
		t.Fatal("a scene that is not ready gave a reader")
	}
	host.Set("ready", true)
	reader, err := target.OceanWater()
	if err != nil || host.Get("mount").String() != "ship" {
		t.Fatalf("reader=%v err=%v mount=%v", reader, err, host.Get("mount"))
	}
	if _, ok := reader.Read(); ok {
		t.Fatal("a record the page has not written is a reading")
	}
	record := host.Get("record")
	for i, v := range []float64{61, 1.25, 2.5, -1.5, 1, 2, 4, 3, 1.7e12} {
		record.SetIndex(i, v)
	}
	frame, ok := reader.Read()
	if !ok || frame.Frame != 61 || frame.Time != 1.25 || frame.X != 2.5 || frame.Z != -1.5 || !frame.Spectral ||
		frame.TimeScale != 2 || frame.DriftX != 4 || frame.DriftZ != 3 || frame.AtMS != 1.7e12 {
		t.Fatalf("frame=%+v ok=%v", frame, ok)
	}
	// The page writes the same array in place: the reader keeps seeing it, with no call back into the page.
	record.SetIndex(0, 62)
	record.SetIndex(4, 0)
	if frame, _ = reader.Read(); frame.Frame != 62 || frame.Spectral || host.Get("asked").Int() != 2 {
		t.Fatalf("frame=%+v asked=%d", frame, host.Get("asked").Int())
	}
}

func TestSceneOceanWaterReadDoesNotAllocate(t *testing.T) {
	host := js.Global().Call("eval", `({isCurrent(){return true}, record:new Float64Array([1,2,3,4,1,1,0,0,5]), scene3D(){return this.record}})`)
	reader, err := Context{value: host}.Scene3DByID("ship").OceanWater()
	if err != nil {
		t.Fatal(err)
	}
	var sink float64
	if n := testing.AllocsPerRun(100, func() {
		f, _ := reader.Read()
		sink += f.Time
	}); n != 0 {
		t.Fatalf("Read allocates %v times per call", n)
	}
	_ = sink
}
