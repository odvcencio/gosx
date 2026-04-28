package auth

import (
	"crypto/sha256"
	"crypto/subtle"
)

func constantTimeStringEqual(a, b string) bool {
	aHash := sha256.Sum256([]byte(a))
	bHash := sha256.Sum256([]byte(b))
	return subtle.ConstantTimeCompare(aHash[:], bHash[:]) == 1 && len(a) == len(b)
}

func constantTimeBytesEqual(a, b []byte) bool {
	aHash := sha256.Sum256(a)
	bHash := sha256.Sum256(b)
	return subtle.ConstantTimeCompare(aHash[:], bHash[:]) == 1 && len(a) == len(b)
}
