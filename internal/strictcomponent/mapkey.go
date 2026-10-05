package strictcomponent

import (
	"encoding/hex"
	"strings"
)

// MapKeySegment encodes a static map key in a dot-joined renderer path.
// A key may contain dots or any other form field characters.
func MapKeySegment(key string) string { return "[" + hex.EncodeToString([]byte(key)) + "]" }

// MapKey decodes a path segment produced by MapKeySegment.
func MapKey(segment string) (string, bool) {
	if !strings.HasPrefix(segment, "[") || !strings.HasSuffix(segment, "]") {
		return "", false
	}
	key, err := hex.DecodeString(segment[1 : len(segment)-1])
	return string(key), err == nil
}
