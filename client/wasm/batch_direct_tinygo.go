//go:build js && wasm && tinygo

package main

import "unsafe"

// These exports are the direct batch entry. The page calls them in this order:
//
//	at = gosx_batch_reserve(room)           // room for the UTF-8 text
//	... writes the text at memory[at:]
//	status = gosx_batch_apply(length, 1)    // 1 for a changed-only batch
//
// A status other than zero means the batch was refused, and gosx_batch_error
// and gosx_batch_error_length give the message (without the "error: " prefix
// that __gosx_set_input_batch puts on it).

//export gosx_batch_reserve
func gosxBatchReserve(room int32) unsafe.Pointer {
	return unsafe.Pointer(unsafe.SliceData(direct.reserve(int(room))))
}

//export gosx_batch_apply
func gosxBatchApply(length int32, changedOnly int32) int32 {
	if err := direct.apply(int(length), changedOnly != 0); err != nil {
		direct.failure = append(direct.failure[:0], err.Error()...)
		return 1
	}
	return 0
}

//export gosx_batch_error
func gosxBatchError() unsafe.Pointer {
	return unsafe.Pointer(unsafe.SliceData(direct.failure))
}

//export gosx_batch_error_length
func gosxBatchErrorLength() int32 {
	return int32(len(direct.failure))
}
