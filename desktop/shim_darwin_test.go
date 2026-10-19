//go:build darwin && !ios && (amd64 || arm64)

package desktop

import (
	"math"
	"sync"
	"testing"
	"unsafe"
)

// The argument shims (msgsend_darwin_*.s) move arguments into the registers or
// stack slots objc_msgSend's callee expects. The tests drive real Objective-C
// methods through them on whichever architecture they run on, so the same file
// checks the HFA registers on arm64 and the stack copy and XMM registers on
// amd64 (run it with GOARCH=amd64 under Rosetta).

// getValue is -[NSValue getValue:size:], reading the value's bytes into buf. The
// pointer is converted at the call so the buffer stays alive and in place.
func getValue(value uintptr, buf unsafe.Pointer, size uintptr) {
	syscall_syscall6(fnobjc_msgSend, value, objcSel("getValue:size:"), uintptr(buf), size, 0, 0)
}

func TestShimMsgSendRectRoundTripsAnNSRect(t *testing.T) {
	for _, in := range []nsRect{
		{0, 0, 960, 640},
		{1.5, 2.5, 3.5, 4.5},
		{-100.25, 50.125, 1280, 800},
		{math.MaxFloat64, -math.MaxFloat64, math.SmallestNonzeroFloat64, 0.1},
	} {
		in := in
		var value uintptr
		withPool(func() {
			// +[NSValue valueWithRect:] takes the NSRect by value: V0-V3 on
			// arm64, the stack on amd64.
			value = objcRetain(sendRect(objcClass("NSValue"), objcSel("valueWithRect:"), &in, 0, 0, 0))
		})
		if value == 0 {
			t.Fatalf("valueWithRect:%+v returned nil", in)
		}
		var typeName string
		withPool(func() {
			typeName = goString(send1(objcClass("NSString"), objcSel("stringWithUTF8String:"), send0(value, objcSel("objCType"))))
		})
		if typeName != "{CGRect={CGPoint=dd}{CGSize=dd}}" {
			t.Errorf("valueWithRect: made a value of type %q, want a CGRect of four doubles", typeName)
		}
		var out nsRect
		getValue(value, unsafe.Pointer(&out), unsafe.Sizeof(out))
		objcRelease(value)
		if out != in {
			t.Errorf("rect round trip = %+v, want %+v", out, in)
		}
	}
}

// The shim shifts the three integer arguments after the rectangle down into the
// slots objc_msgSend's callee reads (R2-R4 / DX, CX, R8), and must leave self and
// _cmd alone. GoSXShimProbe's method is the assembly recorder, which records the
// three arguments it receives.
var (
	probeOnce sync.Once
	probeCls  uintptr
	probeErr  error
)

func probeClass() (uintptr, error) {
	probeOnce.Do(func() {
		probeCls, probeErr = registerRecorderClass("GoSXShimProbe", []recorderMethod{
			{"probeRect:a:b:c:", impStubs[impStubNavigationFinished], "v@:{CGRect={CGPoint=dd}{CGSize=dd}}qqq"},
		})
	})
	return probeCls, probeErr
}

func TestShimMsgSendRectShiftsTheIntegerArguments(t *testing.T) {
	cls, err := probeClass()
	if err != nil {
		t.Fatal(err)
	}
	r := newTestRing(t, 8)
	probe, err := newRecorderObject(cls, r)
	if err != nil {
		t.Fatal(err)
	}
	defer objcRelease(probe)
	sel := objcSel("probeRect:a:b:c:")

	for _, args := range [][3]uintptr{
		{101, 102, 103},
		{0, 0, 0},
		{^uintptr(0), 1 << 62, 7},
	} {
		rect := nsRect{10, 20, 300, 400}
		sendRect(probe, sel, &rect, args[0], args[1], args[2])
		events := drainAll(r)
		if len(events) != 1 {
			t.Fatalf("drained %d records for %v, want 1", len(events), args)
		}
		if events[0].args != args {
			t.Errorf("method saw arguments %#x, want %#x", events[0].args, args)
		}
		if events[0].sel != sel {
			t.Errorf("method saw _cmd %#x, want %#x: the shim disturbed the selector", events[0].sel, sel)
		}
	}
	// self must arrive intact too: the recorder reads its ctrl pointer from it,
	// so a wrong self would have dropped the record instead of recording it.
}

func TestShimMsgSend4FPassesFourDoubles(t *testing.T) {
	var color uintptr
	withPool(func() {
		color = objcRetain(nsColorSRGB(0.25, 0.5, 0.75, 0.125))
	})
	if color == 0 {
		t.Fatal("colorWithSRGBRed:green:blue:alpha: returned nil")
	}
	defer objcRelease(color)
	// -[NSColor getRed:green:blue:alpha:] writes four CGFloats through pointers.
	var rgba [4]float64
	syscall_syscall6(fnobjc_msgSend, color, objcSel("getRed:green:blue:alpha:"),
		uintptr(unsafe.Pointer(&rgba[0])), uintptr(unsafe.Pointer(&rgba[1])),
		uintptr(unsafe.Pointer(&rgba[2])), uintptr(unsafe.Pointer(&rgba[3])))
	want := [4]float64{0.25, 0.5, 0.75, 0.125}
	for i := range want {
		if math.Abs(rgba[i]-want[i]) > 1e-9 {
			t.Errorf("component %d = %v, want %v (all four = %v)", i, rgba[i], want[i], rgba)
			break
		}
	}
}

// One double: dateWithTimeIntervalSinceNow: reads XMM0 / V0 and the shim's other
// registers are ignored. Order the dates to prove the value arrived.
func TestShimMsgSend4FPassesASingleDouble(t *testing.T) {
	withPool(func() {
		date := func(seconds float64) uintptr {
			f := [4]float64{seconds}
			return sendFloats(objcClass("NSDate"), objcSel("dateWithTimeIntervalSinceNow:"), &f)
		}
		later, sooner, past := date(7200), date(3600), date(-3600)
		compare := objcSel("compare:")
		const orderedAscending, orderedDescending = ^uintptr(0), 1 // NSComparisonResult -1 and 1
		if got := send1(later, compare, sooner); got != orderedDescending {
			t.Errorf("[+2h compare:+1h] = %d, want NSOrderedDescending", int64(got))
		}
		if got := send1(sooner, compare, later); got != orderedAscending {
			t.Errorf("[+1h compare:+2h] = %d, want NSOrderedAscending", int64(got))
		}
		if got := send1(past, compare, sooner); got != orderedAscending {
			t.Errorf("[-1h compare:+1h] = %d, want NSOrderedAscending", int64(got))
		}
	})
}

// The nine-argument wake event: type, modifierFlags, windowNumber, context,
// subtype, data1 and data2 come through syscall9, with the NSPoint location and
// NSTimeInterval timestamp zeroed by the shim. data2 is the ninth argument, on
// the stack on both architectures.
func TestShimMsgSendZeroFP3BuildsAnEvent(t *testing.T) {
	withPool(func() {
		const (
			eventType = nsEventTypeApplicationDefined
			flags     = 0x20000 // shift
			windowNum = 0x1234
			subtype   = 5
			data1     = 0x1111_2222_3333
			data2     = 0x4444_5555_6666
		)
		event, _, _ := syscall_syscall9(fnMsgSendZeroFP3, objcClass("NSEvent"),
			objcSel("otherEventWithType:location:modifierFlags:timestamp:windowNumber:context:subtype:data1:data2:"),
			eventType, flags, windowNum, 0, subtype, data1, data2)
		if event == 0 {
			t.Fatal("otherEventWithType:... returned nil")
		}
		for name, tc := range map[string]struct{ got, want uintptr }{
			"type":          {send0(event, objcSel("type")), eventType},
			"modifierFlags": {send0(event, objcSel("modifierFlags")), flags},
			"windowNumber":  {send0(event, objcSel("windowNumber")), windowNum},
			"subtype":       {send0(event, objcSel("subtype")) & 0xffff, subtype},
			"data1":         {send0(event, objcSel("data1")), data1},
			"data2":         {send0(event, objcSel("data2")), data2},
		} {
			if tc.got != tc.want {
				t.Errorf("%s = %#x, want %#x", name, tc.got, tc.want)
			}
		}
		// newWakeEvent is the same call with zeros.
		wake := newWakeEvent()
		if wake == 0 || send0(wake, objcSel("type")) != nsEventTypeApplicationDefined {
			t.Errorf("newWakeEvent() = %#x, want an application-defined event", wake)
		}
	})
}
