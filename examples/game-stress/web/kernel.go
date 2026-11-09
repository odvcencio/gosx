package web

// leb128 appends v as an unsigned LEB128 number.
func leb128(dst []byte, v uint32) []byte {
	for {
		b := byte(v & 0x7f)
		v >>= 7
		if v == 0 {
			return append(dst, b)
		}
		dst = append(dst, b|0x80)
	}
}

func wasmSection(id byte, body []byte) []byte {
	out := []byte{id}
	out = leb128(out, uint32(len(body)))
	return append(out, body...)
}

func wasmName(s string) []byte { return append(leb128(nil, uint32(len(s))), s...) }

// silentKernelWASM assembles a minimal gosx_audio reactor with prefix "test_".
// Each render writes the number of committed commands into the first sample of
// channel 0; every other sample is 0, so it is silent. It is the same kernel as
// e2e/audio_wasm_worklet_offline_e2e_test.go with a zero data segment: it runs
// the AudioWorklet processor and the mixer's music bus with no synthesis code,
// for runs that do not have a Cicada fixture.
func silentKernelWASM() []byte {
	i32, noParam := byte(0x7f), []byte{}
	functype := func(params []byte, results []byte) []byte {
		b := []byte{0x60}
		b = leb128(b, uint32(len(params)))
		b = append(b, params...)
		b = leb128(b, uint32(len(results)))
		return append(b, results...)
	}
	types := [][]byte{
		functype(noParam, noParam),                   // 0: () -> ()
		functype([]byte{i32, i32, i32}, []byte{i32}), // 1: init
		functype([]byte{i32}, noParam),               // 2: (i32) -> ()
		functype(noParam, []byte{i32}),               // 3: () -> i32
	}
	typeBody := leb128(nil, uint32(len(types)))
	for _, t := range types {
		typeBody = append(typeBody, t...)
	}
	// Functions: _initialize, init, render, out_ptr, cmd_ptr, cmd_cap, cmd_commit, msg_ptr, msg_drain
	funcTypes := []byte{0, 1, 2, 3, 3, 3, 2, 3, 3}
	funcBody := append(leb128(nil, uint32(len(funcTypes))), funcTypes...)

	memBody := []byte{1, 0, 1} // one memory, no max, min 1 page
	globalBody := []byte{1, i32, 1, 0x41, 0, 0x0b}

	names := []string{"_initialize", "test_init", "test_render", "test_out_ptr", "test_cmd_ptr", "test_cmd_cap", "test_cmd_commit", "test_msg_ptr", "test_msg_drain"}
	exportBody := leb128(nil, uint32(len(names)+1))
	exportBody = append(exportBody, wasmName("memory")...)
	exportBody = append(exportBody, 2, 0)
	for i, n := range names {
		exportBody = append(exportBody, wasmName(n)...)
		exportBody = append(exportBody, 0, byte(i))
	}

	constBody := func(v uint32) []byte {
		b := []byte{0x41}
		b = leb128(b, v) // i32.const is signed LEB; every value here is below 8192
		if v >= 64 && v < 8192 && b[len(b)-1]&0x40 != 0 {
			// A positive value whose final 7-bit group has bit 6 set needs one more byte for the sign.
			b[len(b)-1] |= 0x80
			b = append(b, 0)
		}
		return append(b, 0x0b)
	}
	bodies := [][]byte{
		{0x0b},
		{0x41, 0x00, 0x0b},
		append(append(append([]byte{}, constBody(4096)[:len(constBody(4096))-1]...), 0x23, 0x00, 0xb3, 0x38, 0x02, 0x00), 0x0b),
		constBody(4096),
		constBody(1024),
		constBody(8),
		{0x23, 0x00, 0x20, 0x00, 0x6a, 0x24, 0x00, 0x0b},
		constBody(3072),
		constBody(0),
	}
	codeBody := leb128(nil, uint32(len(bodies)))
	for _, b := range bodies {
		full := append([]byte{0}, b...) // zero local declarations
		codeBody = leb128(codeBody, uint32(len(full)))
		codeBody = append(codeBody, full...)
	}

	half := []byte{0, 0, 0, 0} // float32 0
	data := make([]byte, 0, 1024)
	for i := 0; i < 256; i++ {
		data = append(data, half...)
	}
	dataBody := []byte{1, 0}
	dataBody = append(dataBody, constBody(4096)...)
	dataBody = leb128(dataBody, uint32(len(data)))
	dataBody = append(dataBody, data...)

	out := []byte{0, 'a', 's', 'm', 1, 0, 0, 0}
	for _, s := range [][]byte{wasmSection(1, typeBody), wasmSection(3, funcBody), wasmSection(5, memBody),
		wasmSection(6, globalBody), wasmSection(7, exportBody), wasmSection(10, codeBody), wasmSection(11, dataBody)} {
		out = append(out, s...)
	}
	return out
}
