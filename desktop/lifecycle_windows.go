//go:build windows && (amd64 || arm64)

package desktop

const (
	pbtAPMResumeAutomatic = 0x0012
	pbtAPMResumeSuspend   = 0x0007
	pbtAPMSuspend         = 0x0004
)
