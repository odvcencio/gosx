package wasm

import (
	"bytes"
	"testing"

	"m31labs.dev/gosx/client/vm"
)

func TestMailboxHeaderRoundTrip(t *testing.T) {
	wantPayload := []byte{0, 1, 2, 255}
	want, err := EncodeMailbox(MailboxOpcodePing, 42, -7, MailboxFlagResponse, wantPayload)
	if err != nil {
		t.Fatal(err)
	}
	if len(want) != MailboxHeaderSize+len(wantPayload) {
		t.Fatalf("mailbox len = %d", len(want))
	}
	header, gotPayload, err := DecodeMailbox(want)
	if err != nil {
		t.Fatal(err)
	}
	if header.Magic != MailboxMagic || header.Version != MailboxVersion || header.Opcode != MailboxOpcodePing || header.RequestID != 42 || header.Status != -7 || header.Flags != MailboxFlagResponse {
		t.Fatalf("unexpected header: %#v", header)
	}
	if !bytes.Equal(gotPayload, wantPayload) {
		t.Fatalf("payload = %v, want %v", gotPayload, wantPayload)
	}
}

func TestHandshakePayloadRoundTrip(t *testing.T) {
	want := NewHandshake(VariantFull)
	payload, err := EncodeHandshakePayload(want)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeHandshakePayload(payload)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("handshake = %#v, want %#v", got, want)
	}
}

func TestPatchMailboxRoundTrip(t *testing.T) {
	want := []vm.PatchOp{
		{Kind: vm.PatchSetText, Path: "0/1", Text: "héllo"},
		{Kind: vm.PatchCreateElement, Path: "0", Tag: "li", AttrName: "data-id", Children: []int{-1, 0, 42}},
	}
	data, err := EncodePatchMailbox("island-1", 9, want)
	if err != nil {
		t.Fatal(err)
	}
	header, islandID, got, err := DecodePatchMailbox(data)
	if err != nil {
		t.Fatal(err)
	}
	if header.RequestID != 9 || islandID != "island-1" {
		t.Fatalf("header/id = %#v/%q", header, islandID)
	}
	if len(got) != len(want) {
		t.Fatalf("patch count = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].Kind != want[i].Kind || got[i].Path != want[i].Path || got[i].Tag != want[i].Tag || got[i].Text != want[i].Text || got[i].AttrName != want[i].AttrName || len(got[i].Children) != len(want[i].Children) {
			t.Fatalf("patch %d = %#v, want %#v", i, got[i], want[i])
		}
		for j := range want[i].Children {
			if got[i].Children[j] != want[i].Children[j] {
				t.Fatalf("patch %d child %d = %d, want %d", i, j, got[i].Children[j], want[i].Children[j])
			}
		}
	}
}

func TestDecodePatchPayloadRejectsForgedHugeCountOverSmallPayload(t *testing.T) {
	// A forged header declaring a huge patch count with almost no payload
	// left to back it. Before the fix this was rejected too (count exceeded
	// len(payload)), so this case alone would already have passed; it is
	// kept as the literal "huge count, small payload" regression case.
	out, err := appendString(nil, "island-1")
	if err != nil {
		t.Fatal(err)
	}
	out = appendU32(out, 1<<20) // declare over a million patches
	// No patch bytes follow at all.

	if _, _, err := DecodePatchPayload(out); err == nil {
		t.Fatal("expected an error for a forged patch count with no payload behind it")
	}
}

func TestDecodePatchPayloadRejectsCountThatOutstripsMinimumPatchEncoding(t *testing.T) {
	// M8: before the fix, DecodePatchPayload only rejected a count that
	// exceeded len(payload) -- effectively assuming one patch could be
	// encoded in a single byte. vm.PatchOp is roughly 100 bytes, and even
	// the smallest possible encoded patch (a 1-byte kind, four zero-length
	// strings, and a zero child count) needs minimumPatchBytes bytes. A
	// payload padded with forgedCount bytes of filler therefore satisfied
	// the old len(payload) bound while declaring vastly more patches than
	// the padding can possibly encode: the reported case is a 64MB payload
	// declaring 64M patches, which requests roughly 6.4GB for the
	// make([]vm.PatchOp, 0, count) slice, since vm.PatchOp is about 100
	// bytes wide.
	//
	// This test proves the guard rejects the request up front, before
	// make([]vm.PatchOp, 0, count) ever runs: with the pre-fix bound this
	// exact input is not rejected by the count check at all -- decoding
	// proceeds, allocates the oversized slice, and only fails much later
	// with "mailbox payload is truncated" once the all-zero padding runs
	// out mid-loop. The fixed bound must fail immediately with the
	// "impossible for payload size" error instead.
	const forgedCount = 1_000_000
	out, err := appendString(nil, "island-1")
	if err != nil {
		t.Fatal(err)
	}
	out = appendU32(out, forgedCount)
	// Pad past forgedCount bytes: the old len(payload) bound (count <=
	// len(payload)) accepted this, since forgedCount+1 bytes of payload
	// remain. The new bound divides by minimumPatchBytes, so this padding
	// can encode at most forgedCount/minimumPatchBytes patches -- far fewer
	// than forgedCount -- and DecodePatchPayload must reject it before
	// allocating or looping at all.
	out = append(out, make([]byte, forgedCount+1)...)

	_, _, err = DecodePatchPayload(out)
	if err == nil {
		t.Fatal("expected an error: forged count exceeds what the padded payload can encode")
	}
	const wantErr = "patch count is impossible for payload size"
	if err.Error() != wantErr {
		t.Fatalf("error = %q, want %q (the pre-loop count guard must fire before any allocation or loop iteration)", err.Error(), wantErr)
	}
}

func TestDecodeMailboxRejectsTruncationAndTrailingBytes(t *testing.T) {
	data, err := EncodeMailbox(MailboxOpcodePing, 1, 0, 0, []byte("x"))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := DecodeMailbox(data[:len(data)-1]); err == nil {
		t.Fatal("truncated mailbox was accepted")
	}
	if _, _, err := DecodeMailbox(append(data, 0)); err == nil {
		t.Fatal("mailbox with trailing bytes was accepted")
	}
}
