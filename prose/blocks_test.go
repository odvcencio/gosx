package prose

import "testing"

func TestSplitKeepsFencesTogether(t *testing.T) {
	source := "# Title\n\nParagraph one.\n\n```go\nfmt.Println(\"hi\")\n```\n\n- one\n- two"
	blocks := Split(source)
	if len(blocks) != 4 {
		t.Fatalf("Split() returned %d blocks, want 4: %#v", len(blocks), blocks)
	}
	if blocks[0].Kind != KindHeading || blocks[1].Kind != KindParagraph || blocks[2].Kind != KindFence || blocks[3].Kind != KindList {
		t.Fatalf("unexpected block kinds: %#v", blocks)
	}
	if blocks[0].Key != "0" || blocks[3].Key != "3" {
		t.Fatalf("unexpected block keys: %#v", blocks)
	}
	if blocks[2].Source != "```go\nfmt.Println(\"hi\")\n```\n" {
		t.Fatalf("fence source = %q", blocks[2].Source)
	}
	if !blocks[3].Incomplete || blocks[2].Incomplete {
		t.Fatalf("only the final block should be incomplete: %#v", blocks)
	}
}

func TestSplitStartsHeadingsAndRules(t *testing.T) {
	blocks := Split("Paragraph\n# Heading\n---\nAfter")
	if len(blocks) != 4 {
		t.Fatalf("Split() returned %d blocks, want 4: %#v", len(blocks), blocks)
	}
	if blocks[0].Kind != KindParagraph || blocks[1].Kind != KindHeading || blocks[2].Kind != KindThematic || blocks[3].Kind != KindParagraph {
		t.Fatalf("unexpected block kinds: %#v", blocks)
	}
}
