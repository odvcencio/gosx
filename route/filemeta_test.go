package route

import (
	"testing"

	"github.com/odvcencio/gosx/server"
)

func TestIsZeroMetadataTreatsRichFieldsAsNonZero(t *testing.T) {
	cases := []server.Metadata{
		{OpenGraph: &server.OpenGraph{Images: []server.MediaAsset{{URL: "/images/card.png"}}}},
		{OpenGraph: &server.OpenGraph{Images: []server.MediaAsset{{Width: 1200}}}},
		{OpenGraph: &server.OpenGraph{Images: []server.MediaAsset{{Height: 630}}}},
		{OpenGraph: &server.OpenGraph{Type: "article"}},
		{Twitter: &server.Twitter{Card: "summary_large_image"}},
		{Robots: &server.Robots{Index: routeBool(false)}},
	}

	for _, meta := range cases {
		if isZeroMetadata(meta) {
			t.Fatalf("expected metadata %#v to be non-zero", meta)
		}
	}
}

func routeBool(value bool) *bool {
	return &value
}
