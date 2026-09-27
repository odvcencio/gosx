package desktop

func windowIconHandles(resourceCount int, big, small uintptr) (uintptr, uintptr, bool) {
	if resourceCount <= 0 || (big == 0 && small == 0) {
		return 0, 0, false
	}
	if big == 0 {
		big = small
	}
	if small == 0 {
		small = big
	}
	return big, small, true
}
