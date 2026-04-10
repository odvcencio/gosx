package field

import (
	"testing"
	"time"

	"github.com/odvcencio/gosx/hub"
)

func TestPublishSubscribeField(t *testing.T) {
	h := hub.New("test-fields")

	ch := SubscribeField(h, "wind")
	source := FromFunc([3]int{4, 4, 4}, 3,
		AABB{Max: [3]float32{1, 1, 1}},
		func(x, y, z float32) []float32 { return []float32{1, 0, 0} },
	)
	PublishField(h, "wind", source, QuantizeOptions{BitWidth: 6})

	select {
	case got := <-ch:
		if got.Resolution != source.Resolution {
			t.Errorf("resolution mismatch: %v vs %v", got.Resolution, source.Resolution)
		}
		if got.Components != 3 {
			t.Errorf("components = %d, want 3", got.Components)
		}
		v := got.SampleVec3(0.5, 0.5, 0.5)
		if v[0] < 0.9 || v[0] > 1.1 {
			t.Errorf("vx = %f, want ~1", v[0])
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for field update")
	}
}

func TestPublishSubscribeDelta(t *testing.T) {
	h := hub.New("test-fields-delta")
	ch := SubscribeField(h, "wind")

	first := FromFunc([3]int{4, 4, 4}, 1,
		AABB{Max: [3]float32{1, 1, 1}},
		func(x, y, z float32) []float32 { return []float32{0.5} },
	)
	second := FromFunc([3]int{4, 4, 4}, 1,
		AABB{Max: [3]float32{1, 1, 1}},
		func(x, y, z float32) []float32 { return []float32{0.6} },
	)

	PublishField(h, "wind", first, QuantizeOptions{BitWidth: 8})
	<-ch // drain first
	PublishField(h, "wind", second, QuantizeOptions{BitWidth: 8})

	select {
	case got := <-ch:
		v := got.SampleScalar(0.5, 0.5, 0.5)
		if v < 0.55 || v > 0.65 {
			t.Errorf("delta-applied scalar = %f, want ~0.6", v)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for delta update")
	}
}
