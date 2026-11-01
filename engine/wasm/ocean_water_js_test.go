//go:build js && wasm

package wasm

import (
	"testing"

	"syscall/js"

	"m31labs.dev/gosx/scene"
)

func TestSceneOceanWaterReaderFollowsThePageRecord(t *testing.T) {
	host := js.Global().Call("eval", `({isCurrent(){return true}, asked:0, record:new Float64Array(338),
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
	for i, v := range []float64{61, 1.25, 2.5, -1.5, 1, 2, 4, 3, 1.7e12, 3} {
		record.SetIndex(i, v)
	}
	frame, ok := reader.Read()
	if !ok || frame.Seq != 61 || frame.Generation != 3 || frame.Time != 1.25 || frame.X != 2.5 || frame.Z != -1.5 || !frame.Spectral ||
		frame.TimeScale != 2 || frame.WaterDriftX != 4 || frame.WaterDriftZ != 3 || frame.PageEpochMS != 1.7e12 {
		t.Fatalf("frame=%+v ok=%v", frame, ok)
	}
	// The page writes the same array in place: the reader keeps seeing it, with no call back into the page.
	record.SetIndex(0, 62)
	record.SetIndex(4, 0)
	if frame, _ = reader.Read(); frame.Seq != 62 || frame.Spectral || host.Get("asked").Int() != 2 {
		t.Fatalf("frame=%+v asked=%d", frame, host.Get("asked").Int())
	}
}

func TestSceneOceanWaterReadDoesNotAllocate(t *testing.T) {
	host := js.Global().Call("eval", `({isCurrent(){return true}, record:(()=>{const a=new Float64Array(338);a.set([1,2,3,4,1,1,0,0,5,2]);return a})(), scene3D(){return this.record}})`)
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

func TestSceneOceanProbeWritesPointsAndReadsTheAnswer(t *testing.T) {
	host := js.Global().Call("eval", `(()=>{const H={isCurrent(){return true}, sent:0, scene3D(){return this.record}};const a=new Float64Array(338);a.send=()=>{H.sent++};H.record=a;return H})()`)
	reader, err := Context{value: host}.Scene3DByID("ship").OceanWater()
	if err != nil {
		t.Fatal(err)
	}
	var reading scene.OceanProbeReading
	if reader.ReadProbe(&reading) {
		t.Fatal("a reading before the page wrote one")
	}
	reader.SetProbe([]scene.OceanProbePoint{{X: 1.5, Z: -2}, {X: 10, Z: 20}})
	record := host.Get("record")
	if record.Index(10).Int() != 2 || record.Index(18).Float() != 1.5 || record.Index(19).Float() != -2 || record.Index(21).Float() != 20 || host.Get("sent").Int() != 1 {
		t.Fatalf("points not written: count=%v p0=%v sent=%v", record.Index(10), record.Index(18), host.Get("sent"))
	}
	for i, v := range []float64{42, 7, 3, 2, 2, 1.7e12, 5} { // seq, generation, flags (valid+gerstner), n, latency, at, readings
		record.SetIndex(11+i, v)
	}
	for i, v := range []float64{1.25, 0.1, -0.2, 2.5, 0.3, 0.4} {
		record.SetIndex(146+i, v)
	}
	if !reader.ReadProbe(&reading) || reading.Seq != 42 || reading.Generation != 7 || !reading.Gerstner || reading.Unavailable || reading.Count != 2 ||
		reading.LatencyFrames != 2 || reading.Readings != 5 || reading.Height[1] != 2.5 || reading.NormalX[0] != 0.1 || reading.NormalZ[1] != 0.4 {
		t.Fatalf("reading=%+v", reading)
	}
	if y := reading.NormalY(0); y < 0.97 || y > 0.98 {
		t.Fatalf("NormalY=%v", y)
	}
	record.SetIndex(13, 4)
	reader.ReadProbe(&reading)
	if !reading.Unavailable {
		t.Fatal("the unavailable flag was lost")
	}
	reader.SetProbe(nil)
	if record.Index(10).Int() != 0 {
		t.Fatal("an empty set must stop the probe")
	}
	many := make([]scene.OceanProbePoint, 100)
	reader.SetProbe(many)
	if record.Index(10).Int() != scene.MaxOceanProbePoints {
		t.Fatalf("more points than the limit were accepted: %v", record.Index(10))
	}
	points := []scene.OceanProbePoint{{X: 1, Z: 2}, {X: 3, Z: 4}}
	if n := testing.AllocsPerRun(100, func() { reader.SetProbe(points); reader.ReadProbe(&reading) }); n != 0 {
		t.Fatalf("SetProbe and ReadProbe allocate %v times per call", n)
	}
}
