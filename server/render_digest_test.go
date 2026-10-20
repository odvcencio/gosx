package server

import (
	"strings"
	"testing"
)

func TestRepresentationStringDigestMatchesBytes(t *testing.T) {
	for _, body := range []string{"", "small page", strings.Repeat("<p>こんにちは</p>\r\n", 10000)} {
		a, b := NewCacheState(), NewCacheState()
		a.SetRepresentation([]byte(body))
		b.setRepresentationString(body)
		if a.bodyDigest != b.bodyDigest {
			t.Fatal("string hashing changed the representation digest")
		}
	}
}
