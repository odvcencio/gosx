package worklet

import "testing"

func TestBackendValidation(t *testing.T) {
	valid := Backend{MaxFrames: 64, Channels: 1, CommandBytes: 4, MessageBytes: 4, MessageCapacity: 8}
	if _, err := valid.Options(); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*Backend){
		func(b *Backend) { b.Channels = 0 },
		func(b *Backend) { b.MaxFrames = 4097 },
		func(b *Backend) { b.CommandBytes = 0 },
		func(b *Backend) { b.MessageCapacity = -1 },
		func(b *Backend) { b.FaultOpcode = 7; b.FaultArgumentOffset = 3 },
		func(b *Backend) { b.OpcodeCapabilities = []OpcodeCapability{{Opcode: 9, Mask: 1}} },
		func(b *Backend) { b.Transport = &Transport{Play: 1, Stop: 2, Seek: 3, TicksPerBeat: 1, TicksPerBar: 4} },
	} {
		b := valid
		change(&b)
		if _, err := b.Options(); err == nil {
			t.Fatalf("accepted invalid backend %+v", b)
		}
	}
	if err := valid.ValidateImage(ImageInfo{RequiredCapabilities: 1}); err == nil {
		t.Fatal("accepted unsupported image capability")
	}
}
