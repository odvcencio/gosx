//go:build darwin && !ios && (amd64 || arm64)

package desktop

import (
	"syscall"
	"testing"
	"unsafe"
)

// These callbacks never enter Go. Their assembly only updates captured native
// memory, including when the recorder cancels a request inside a syscall.
var fnWebTestBlockInvoke, fnWebTestBlockCopy, fnWebTestBlockDispose, fnWebTestDlsym uintptr

//go:cgo_import_dynamic gosx_web_test_dlsym dlsym "/usr/lib/libSystem.B.dylib"

// The public Blocks ABI: a stack literal with copy/dispose helpers and one
// captured pointer. Both the literal and its capture live outside the Go heap.
type webTestBlockLiteral struct {
	isa        uintptr
	flags      uint32
	reserved   uint32
	invoke     uintptr
	descriptor uintptr
	state      uintptr
}

type webTestBlockDescriptor struct {
	reserved  uintptr
	size      uintptr
	copy      uintptr
	dispose   uintptr
	signature uintptr
}

type webTestBlockState struct {
	calls    uint64
	result   uintptr
	copies   uint64
	disposes uint64
}

type webTestCompletion struct {
	literal *webTestBlockLiteral
	state   *webTestBlockState
}

func newWebTestCompletion(t *testing.T) *webTestCompletion {
	t.Helper()
	name := []byte("_NSConcreteStackBlock\x00")
	stackClass, _, _ := syscall_syscall(fnWebTestDlsym, ^uintptr(1), uintptr(unsafe.Pointer(&name[0])), 0) // RTLD_DEFAULT
	if stackClass == 0 {
		t.Fatal("public _NSConcreteStackBlock symbol is unavailable")
	}
	mem, err := syscall.Mmap(-1, 0, syscall.Getpagesize(), syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_PRIVATE|syscall.MAP_ANON)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := syscall.Munmap(mem); err != nil {
			t.Error(err)
		}
	})
	literal := (*webTestBlockLiteral)(unsafe.Pointer(&mem[0]))
	descriptor := (*webTestBlockDescriptor)(unsafe.Pointer(&mem[64]))
	state := (*webTestBlockState)(unsafe.Pointer(&mem[128]))
	copy(mem[192:], "v@?@\x00")
	*descriptor = webTestBlockDescriptor{
		size: unsafe.Sizeof(*literal), copy: fnWebTestBlockCopy,
		dispose: fnWebTestBlockDispose, signature: uintptr(unsafe.Pointer(&mem[192])),
	}
	*literal = webTestBlockLiteral{
		isa: stackClass, flags: 1<<25 | 1<<30, // BLOCK_HAS_COPY_DISPOSE | BLOCK_HAS_SIGNATURE
		invoke: fnWebTestBlockInvoke, descriptor: uintptr(unsafe.Pointer(descriptor)), state: uintptr(unsafe.Pointer(state)),
	}
	return &webTestCompletion{literal: literal, state: state}
}

func (block *webTestCompletion) pointer() uintptr { return uintptr(unsafe.Pointer(block.literal)) }

func (block *webTestCompletion) check(t *testing.T, calls uint64, result uintptr, copies, disposes uint64) {
	t.Helper()
	want := webTestBlockState{calls: calls, result: result, copies: copies, disposes: disposes}
	if got := *block.state; got != want {
		t.Errorf("native completion state = %+v, want %+v", got, want)
	}
}

type webCompletionCase struct {
	name   string
	stub   int
	kind   uint64
	prompt bool
}

var webCompletionCases = []webCompletionCase{
	{"file", impStubWebFilePanel, evWebFilePanel, false},
	{"alert", impStubWebAlert, evWebAlert, false},
	{"confirm", impStubWebConfirm, evWebConfirm, false},
	{"prompt", impStubWebPrompt, evWebPrompt, true},
}

func callWebCompletionStub(tc webCompletionCase, self *fakeSelf, args [3]uintptr, completion uintptr) {
	const webview = 0x1111 // It must be dropped before retaining callback arguments.
	if tc.prompt {
		syscall_syscall9(impStubs[tc.stub], uintptr(unsafe.Pointer(self)), objcSel("probe:"), webview, args[0], args[1], args[2], completion, 0, 0)
	} else {
		syscall_syscall6(impStubs[tc.stub], uintptr(unsafe.Pointer(self)), objcSel("probe:"), webview, args[0], args[1], completion)
	}
}

func withWebCompletionObjects(t *testing.T, fn func([3]uintptr)) {
	t.Helper()
	withPool(func() {
		args := [3]uintptr{objcNew(objcClass("NSObject")), objcNew(objcClass("NSObject")), objcNew(objcClass("NSObject"))}
		defer func() {
			for _, obj := range args {
				objcRelease(obj)
			}
		}()
		fn(args)
		for i, obj := range args {
			if got := retainCount(obj); got != 1 {
				t.Errorf("argument %d retainCount after completion = %d, want 1", i, got)
			}
		}
	})
}

func TestDarwinWebUICompletionLayout(t *testing.T) {
	var event nativeEvent
	if unsafe.Sizeof(event) != 64 || unsafe.Offsetof(event.mask) != 40 || unsafe.Offsetof(event.completion) != 48 || copyCompletion != 1<<8 {
		t.Errorf("completion changed the native event ABI: size=%d mask=%d completion=%d flag=%#x", unsafe.Sizeof(event), unsafe.Offsetof(event.mask), unsafe.Offsetof(event.completion), copyCompletion)
	}
	var literal webTestBlockLiteral
	var state webTestBlockState
	if unsafe.Sizeof(literal) != 40 || unsafe.Offsetof(literal.invoke) != 16 || unsafe.Offsetof(literal.state) != 32 || unsafe.Offsetof(state.result) != 8 || unsafe.Offsetof(state.copies) != 16 || unsafe.Offsetof(state.disposes) != 24 {
		t.Fatal("test Blocks ABI no longer matches the native callbacks")
	}
}

func TestDarwinWebUIRecorderCopiesStackCompletion(t *testing.T) {
	for _, tc := range webCompletionCases {
		t.Run(tc.name, func(t *testing.T) {
			ring := newTestRing(t, 4)
			completion := newWebTestCompletion(t)
			onMain(func() {
				withWebCompletionObjects(t, func(args [3]uintptr) {
					callWebCompletionStub(tc, newFakeSelf(ring), args, completion.pointer())
					if ring.pending() != 1 {
						t.Errorf("pending = %d, want 1", ring.pending())
						return
					}
					event := *ring.event(ring.c.tail)
					wantArgs, wantMask := [3]uintptr{args[0], args[1], 0}, retainArg0|retainArg1|copyCompletion
					if tc.prompt {
						wantArgs, wantMask = args, wantMask|retainArg2
					}
					if event.kind != tc.kind || event.args != wantArgs || event.mask != wantMask {
						t.Errorf("recorded event = %+v, want kind=%d args=%#x mask=%#x", event, tc.kind, wantArgs, wantMask)
					}
					if event.completion == 0 || event.completion == completion.pointer() {
						t.Errorf("completion = %#x, want a heap copy of %#x", event.completion, completion.pointer())
					}
					completion.check(t, 0, 0, 1, 0)
					// Invalidate the original literal as if its native stack frame had
					// returned. Only the copied block may be used after this point.
					*completion.literal = webTestBlockLiteral{}
					ring.drain(func(event nativeEvent) { invokeNativeBlock(event.completion, 0x1234) })
					completion.check(t, 1, 0x1234, 1, 1)
					if got := ring.drain(nil); got != 0 {
						t.Errorf("second drain consumed %d records", got)
					}
					completion.check(t, 1, 0x1234, 1, 1)
					_, _, retains, releases := ring.stats()
					if retains != releases {
						t.Errorf("retains=%d releases=%d after completion", retains, releases)
					}
				})
			})
		})
	}
}

func TestDarwinWebUIRecorderOverflowCancelsCompletion(t *testing.T) {
	for _, tc := range webCompletionCases {
		t.Run(tc.name, func(t *testing.T) {
			ring := newTestRing(t, 1)
			completion := newWebTestCompletion(t)
			onMain(func() {
				withWebCompletionObjects(t, func(args [3]uintptr) {
					self := newFakeSelf(ring)
					callStub(impStubs[impStubNavigationFinished], self, objcSel("probe:"), 0, 0, 0)
					callWebCompletionStub(tc, self, args, completion.pointer())
					completion.check(t, 1, 0, 0, 0)
					calls, dropped, retains, releases := ring.stats()
					if calls != 1 || dropped != 1 || retains != 0 || releases != 0 || ring.pending() != 1 {
						t.Errorf("overflow stats calls=%d dropped=%d retains=%d releases=%d pending=%d", calls, dropped, retains, releases, ring.pending())
					}
					ring.drain(nil)
					completion.check(t, 1, 0, 0, 0)
				})
			})
		})
	}
}

func TestDarwinWebUIUnwiredRecorderCancelsCompletion(t *testing.T) {
	for _, tc := range webCompletionCases {
		t.Run(tc.name, func(t *testing.T) {
			completion := newWebTestCompletion(t)
			onMain(func() {
				withWebCompletionObjects(t, func(args [3]uintptr) {
					callWebCompletionStub(tc, &fakeSelf{}, args, completion.pointer())
					completion.check(t, 1, 0, 0, 0)
				})
			})
		})
	}
}

func TestDarwinWebUIDrainWithoutHandlerCancelsCompletion(t *testing.T) {
	for _, tc := range webCompletionCases {
		t.Run(tc.name, func(t *testing.T) {
			ring := newTestRing(t, 4)
			completion := newWebTestCompletion(t)
			onMain(func() {
				withWebCompletionObjects(t, func(args [3]uintptr) {
					callWebCompletionStub(tc, newFakeSelf(ring), args, completion.pointer())
					*completion.literal = webTestBlockLiteral{}
					if got := ring.drain(nil); got != 1 {
						t.Errorf("drain without handler consumed %d records, want 1", got)
					}
					completion.check(t, 1, 0, 1, 1)
					ring.drain(nil)
					completion.check(t, 1, 0, 1, 1)
				})
			})
		})
	}
}

func TestDarwinWebUIClosingViewCancelsQueuedCompletion(t *testing.T) {
	for _, tc := range webCompletionCases {
		for _, closed := range []bool{false, true} {
			name := tc.name + "/closing"
			if closed {
				name = tc.name + "/closed"
			}
			t.Run(name, func(t *testing.T) {
				ring := newTestRing(t, 4)
				completion := newWebTestCompletion(t)
				oldPanel, oldAlert := runWebFilePanel, runWebAlert
				defer func() { runWebFilePanel, runWebAlert = oldPanel, oldAlert }()
				runWebFilePanel = func(uintptr) uintptr {
					t.Error("closing view attempted to show a file panel")
					return 0
				}
				runWebAlert = func(uintptr) uintptr {
					t.Error("closing view attempted to show an alert")
					return 0
				}
				onMain(func() {
					withWebCompletionObjects(t, func(args [3]uintptr) {
						app := &darwinApp{webview: 0x1111}
						callWebCompletionStub(tc, newFakeSelf(ring), args, completion.pointer())
						// The callback arrived while the view existed, but the view
						// began closing before Go handled the queued request.
						if closed {
							app.webview = 0
						} else {
							app.closing = true
						}
						*completion.literal = webTestBlockLiteral{}
						ring.drain(app.handleNativeEvent)
						completion.check(t, 1, 0, 1, 1)
						ring.drain(app.handleNativeEvent)
						completion.check(t, 1, 0, 1, 1)
					})
				})
			})
		}
	}
}
