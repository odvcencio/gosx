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
