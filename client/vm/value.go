package vm

import (
	"math"
	"sort"
	"strconv"
	"strings"
	"unsafe"

	"m31labs.dev/gosx/island/program"
)

// Value is the runtime representation of all values in the island expression VM.
//
// Value is a tagged union of 32 bytes. Every VM method takes and returns a
// Value by value, so one binary operation copies the struct three times. The
// earlier struct-of-fields layout held a string header, a slice header, a map
// word, a closure word, a float and three tag bytes side by side. That cost 72
// bytes and flattened to 11 leaf fields, which is past the amd64 register
// limit, so the compiler passed every Value through memory.
//
// The union stores one pointer word plus one length word, and a tag byte tells
// you what they mean. Value now flattens to 4 integer-class leaf fields plus
// one float, so a receiver and one Value argument both ride in registers.
//
// Read the "Value representation" note at the bottom of this file for the
// full migration history and for the rules a new field must follow.
type Value struct {
	// noCompare keeps Value non-comparable. The pre-union layout held a
	// map field, so Go refused to compare a Value, and two behaviours grew
	// to depend on that.
	//
	// signal.New installs a == based change test for every comparable type
	// and installs none for the rest. On a union, == compares the payload
	// POINTER. OpIndexSet and OpFieldSet then mutate the array or the map
	// behind that pointer IN PLACE, so == reports "unchanged" for a Value
	// whose contents just changed, and the signal never notifies. Use Eq,
	// which walks the payload, whenever you need to compare two Values.
	//
	// The generic fallback in signal.defaultEqual also boxes both operands,
	// which costs one allocation per Set for a 32-byte struct.
	//
	// The field costs no bytes. It is first, so it adds no tail padding,
	// and a zero-length array takes no argument register.
	noCompare [0]func()

	// ptr holds the single pointer payload. What it points to depends on
	// the kind bits of tag:
	//
	//   kindScalar   nil
	//   kindString   the first byte of the string
	//   kindArray    the first element of the array
	//   kindMap      the map header word
	//   kindClosure  the *closureRef
	//
	// An empty string or an empty array keeps whatever data pointer the
	// original header carried, which the runtime may report as nil. Read
	// n, never ptr, to learn the length.
	//
	// The field keeps pointer type so the garbage collector scans it and
	// keeps the payload alive.
	ptr unsafe.Pointer

	// n is the string byte count for kindString and the element count for
	// kindArray. Every other kind leaves it at 0.
	n int

	// num is the float payload. Int-typed Values carry their integer here
	// too. It stays a real field because arithmetic reads it on every
	// step, and because a float field costs a float register, not one of
	// the scarce integer registers.
	num float64

	// Type is the static expression type the lowerer assigned. It steers
	// String, ToIntVal and the arithmetic promotion rules. It is separate
	// from the kind bits: Neg, for example, returns a numeric Value that
	// keeps a TypeString tag.
	Type program.ExprType

	// tag packs three things that used to be three separate bytes: the
	// payload kind, the boolean payload, and the control signal. Packing
	// them keeps the leaf-field count at 4 integer-class fields, which is
	// what lets two Values pass in registers. Read it through kind(),
	// Truth() and Control().
	tag uint8
}

// valueKind names which payload ptr and n hold. It occupies the low three
// bits of Value.tag.
type valueKind uint8

const (
	// kindScalar marks a Value with no pointer payload: the zero Value, a
	// number, or a boolean.
	kindScalar valueKind = iota
	// kindString marks a text payload in ptr and n.
	kindString
	// kindArray marks an array payload in ptr and n.
	kindArray
	// kindMap marks an object payload in ptr.
	kindMap
	// kindClosure marks a *closureRef in ptr.
	kindClosure
)

const (
	// tagKindMask selects the payload kind, bits 0 to 2.
	tagKindMask uint8 = 0x07
	// tagTruthBit holds the boolean payload, bit 3.
	tagTruthBit uint8 = 0x08
	// tagControlShift moves the control signal into bits 4 and 5.
	tagControlShift = 4
	// tagControlMask selects the control signal, bits 4 and 5.
	tagControlMask uint8 = 0x30
)

// --- Payload accessors ---
//
// Every accessor comes in two forms.
//
// The unexported form takes a POINTER receiver. Inside this package almost
// every caller holds an addressable Value, so a pointer receiver inlines to a
// single load. Use it on every hot path.
//
// The exported form takes a value receiver, because the seam that code outside
// this package uses must work on any Value expression.
//
// Do not call an exported accessor from a hot path inside this package. The
// inliner materializes one 32-byte copy per nested value-receiver call, and it
// cannot fold the copies away once the Value lives in a stack slot. Chaining
// two of them inside stringAtDepth cost 5x on BenchmarkValueIntToString.

// kind reports which payload ptr and n hold.
func (v *Value) kind() valueKind { return valueKind(v.tag & tagKindMask) }

// isList reports whether v carries an array payload.
func (v *Value) isList() bool { return v.tag&tagKindMask == uint8(kindArray) }

// isMap reports whether v carries an object payload.
func (v *Value) isMap() bool { return v.tag&tagKindMask == uint8(kindMap) }

// text returns the string payload, or "" when v holds no text.
func (v *Value) text() string {
	if v.tag&tagKindMask != uint8(kindString) {
		return ""
	}
	return unsafe.String((*byte)(v.ptr), v.n)
}

// list returns the array payload, or nil when v holds no array.
func (v *Value) list() []Value {
	if v.tag&tagKindMask != uint8(kindArray) {
		return nil
	}
	return unsafe.Slice((*Value)(v.ptr), v.n)
}

// dict returns the object payload, or nil when v holds no object.
func (v *Value) dict() map[string]Value {
	if v.tag&tagKindMask != uint8(kindMap) {
		return nil
	}
	// Copy ptr into a local, then read the local as a map. A map value is
	// one pointer word, so the copy reproduces the original map, and every
	// write through the result reaches the same object.
	// TestMapFitsOnePointerWord pins the width this depends on.
	p := v.ptr
	return *(*map[string]Value)(unsafe.Pointer(&p))
}

// truth returns the boolean payload.
func (v *Value) truth() bool { return v.tag&tagTruthBit != 0 }

// control returns the unwind sentinel v carries.
func (v *Value) control() ControlSignal {
	return ControlSignal((v.tag & tagControlMask) >> tagControlShift)
}

// closureRefOf returns the closure bookkeeping v carries, or nil.
func (v *Value) closureRefOf() *closureRef {
	if v.tag&tagKindMask != uint8(kindClosure) {
		return nil
	}
	return (*closureRef)(v.ptr)
}

// --- Reader seams (stage 1 of the tagged-union migration) ---
//
// These methods return exactly what the five payload fields used to hold.
// They exist so that code outside this package never names a field. A tagged
// union cannot expose five overlapping fields, so every read goes through a
// method. Read the "Value representation" note at the bottom of this file for
// the full plan.

// Text returns the string payload. It is the raw text, NOT the formatted
// form. Use String() when you want any Value rendered as text.
//
// The returned string aliases the bytes the Value was built from. Go strings
// are immutable, so no caller can write through the alias.
func (v Value) Text() string { return v.text() }

// Number returns the float payload. Int-typed Values carry their integer
// in the same field, so read Number() and convert at the call site.
func (v Value) Number() float64 { return v.num }

// Truth returns the boolean payload. It is the raw boolean, NOT a
// truthiness test over the whole Value.
func (v Value) Truth() bool { return v.truth() }

// List returns the array payload. The returned slice aliases the Value's
// storage, so a write through it is visible to every Value that shares the
// same array. That is the documented in-place mutation contract; see
// lhs_set.go. Use SetIndex to write one element.
//
// The slice has capacity equal to its length. Append to it and you get a
// fresh array, never a write into shared storage.
func (v Value) List() []Value { return v.list() }

// Map returns the object payload. The returned map aliases the Value's
// storage, so a write through it is visible to every Value that shares the
// same object. Use SetField to write one field.
func (v Value) Map() map[string]Value { return v.dict() }

// IsList reports whether v carries an array payload.
func (v Value) IsList() bool { return v.isList() }

// IsMap reports whether v carries an object payload.
func (v Value) IsMap() bool { return v.isMap() }

// Control returns the unwind sentinel the Value carries. It is
// ControlNone for every Value that regular evaluation produces.
func (v Value) Control() ControlSignal { return v.control() }

// WithControl returns a copy of v that carries the control signal c. The
// payload does not change. Use it where the old layout assigned to the
// Control field.
func (v Value) WithControl(c ControlSignal) Value {
	v.tag = (v.tag &^ tagControlMask) | (uint8(c)<<tagControlShift)&tagControlMask
	return v
}

// --- Writer seams (stage 3 of the tagged-union migration) ---
//
// OpFieldSet and OpIndexSet mutate the collection in place. Hosts that
// populate a struct-shaped Value do the same. Both need a seam that survives
// the layout change, because a union stores the collection behind one word
// and cannot hand back an addressable map or slice field.

// SetField writes val into v's object payload under name. It reports
// whether the write landed. A Value with no object payload returns false
// and drops the write, which keeps the panic-free contract.
//
// The write is in place. Every Value that shares the same object sees it.
// That is deliberate: composite values are reference-shared in this VM, and
// host receivers rely on it to fill a caller-supplied props struct.
func (v Value) SetField(name string, val Value) bool {
	fields := v.dict()
	if fields == nil {
		return false
	}
	fields[name] = val
	return true
}

// SetIndex writes val into v's array payload at index i. It reports whether
// the write landed. An out-of-range index, or a Value with no array payload,
// returns false and drops the write. The array never grows.
func (v Value) SetIndex(i int, val Value) bool {
	items := v.list()
	if items == nil || i < 0 || i >= len(items) {
		return false
	}
	items[i] = val
	return true
}

// closureRef carries the runtime bookkeeping for a ClosureVal: which
// synthetic FuncDef to dispatch into when the closure is invoked, plus
// the captured-frame reference that provides BY-REFERENCE access to
// the enclosing scope's locals. The same *frame pointer the lowerer
// captured at OpClosure-evaluation time is reused for every invocation,
// so mutations in the enclosing scope after the closure was created
// remain visible inside the closure (matching Go's semantics).
type closureRef struct {
	funcName string
	captured map[string]bool // names the closure captures (lookup gate)
	frame    *frame          // caller frame holding the captured slots
}

// ClosureVal builds a closure Value that, when invoked through
// OpIndirectCall, dispatches into the named synthetic FuncDef with the
// enclosing frame's captured slots visible by reference.
//
// funcName is the FuncDef registered by the lowerer for the anonymous
// body. captured names the variables the body references that are NOT
// its own parameters or fresh declarations, that is, the closed-over
// locals. frame is the caller's *frame at OpClosure-evaluation time. The
// closure forwards reads and writes for any captured name through this
// exact frame, giving Go's variable-capture semantics.
func ClosureVal(funcName string, captured []string, frame *frame) Value {
	capMap := make(map[string]bool, len(captured))
	for _, name := range captured {
		capMap[name] = true
	}
	ref := &closureRef{
		funcName: funcName,
		captured: capMap,
		frame:    frame,
	}
	return Value{
		Type: program.TypeAny,
		ptr:  unsafe.Pointer(ref),
		tag:  uint8(kindClosure),
	}
}

// IsClosure reports whether v is a ClosureVal produced by OpClosure.
// Hosts and the VM use this to decide whether to dispatch into the
// closure-aware path of OpIndirectCall or the regular user-function
// path.
func IsClosure(v Value) bool {
	return v.kind() == kindClosure
}

// ClosureFuncName returns the synthetic FuncDef name carried by v if
// v is a ClosureVal, otherwise "".
func ClosureFuncName(v Value) string {
	ref := v.closureRefOf()
	if ref == nil {
		return ""
	}
	return ref.funcName
}

// ControlSignal is the sentinel kind a Value carries. Read it with
// Control() and set it with WithControl().
type ControlSignal uint8

const (
	// ControlNone is the default; evaluation proceeds normally.
	ControlNone ControlSignal = iota
	// ControlReturn unwinds the enclosing handler frame, carrying the
	// Value's payload as the handler's return value.
	ControlReturn
	// ControlBreak terminates the nearest enclosing loop.
	ControlBreak
	// ControlContinue advances the nearest enclosing loop to its next
	// iteration (post + cond), skipping the rest of the body.
	ControlContinue
)

// ArrayVal creates an array Value from a slice of Values.
//
// A nil slice produces a Value that is NOT a list, which matches the
// pre-union layout where a nil Items field read as "no array payload".
// FilterFunc and the other array builders depend on that.
func ArrayVal(items []Value) Value {
	if items == nil {
		return Value{Type: program.TypeAny}
	}
	return Value{
		Type: program.TypeAny,
		ptr:  unsafe.Pointer(unsafe.SliceData(items)),
		n:    len(items),
		tag:  uint8(kindArray),
	}
}

// ObjectVal creates an object Value.
//
// A nil map produces a Value that is NOT a map, which matches the
// pre-union layout where a nil Fields field read as "no object payload".
func ObjectVal(fields map[string]Value) Value {
	if fields == nil {
		return Value{Type: program.TypeAny}
	}
	return Value{
		Type: program.TypeAny,
		ptr:  *(*unsafe.Pointer)(unsafe.Pointer(&fields)),
		tag:  uint8(kindMap),
	}
}

// StringVal creates a string Value.
func StringVal(s string) Value {
	return Value{
		Type: program.TypeString,
		ptr:  unsafe.Pointer(unsafe.StringData(s)),
		n:    len(s),
		tag:  uint8(kindString),
	}
}

// IntVal creates an integer Value.
func IntVal(n int) Value {
	return Value{Type: program.TypeInt, num: float64(n)}
}

// FloatVal creates a float Value.
func FloatVal(f float64) Value {
	return Value{Type: program.TypeFloat, num: f}
}

// BoolVal creates a boolean Value.
func BoolVal(b bool) Value {
	v := Value{Type: program.TypeBool}
	if b {
		v.tag = tagTruthBit
	}
	return v
}

// ZeroValue returns the zero Value for the given type.
func ZeroValue(typ program.ExprType) Value {
	return Value{Type: typ}
}

// isInt reports whether both v and b are integer-typed.
func isInt(a, b Value) bool {
	return a.Type == program.TypeInt && b.Type == program.TypeInt
}

// resultType returns TypeInt if both operands are int, otherwise TypeFloat.
func resultType(a, b Value) program.ExprType {
	if isInt(a, b) {
		return program.TypeInt
	}
	return program.TypeFloat
}

// Add returns a + b. Uses integer semantics when both operands are TypeInt.
func (v Value) Add(b Value) Value {
	if v.Type == program.TypeString || b.Type == program.TypeString {
		return StringVal(v.String() + b.String())
	}
	if isInt(v, b) {
		return Value{Type: program.TypeInt, num: float64(int64(v.num) + int64(b.num))}
	}
	return Value{Type: program.TypeFloat, num: v.num + b.num}
}

// Sub returns a - b.
func (v Value) Sub(b Value) Value {
	if isInt(v, b) {
		return Value{Type: program.TypeInt, num: float64(int64(v.num) - int64(b.num))}
	}
	return Value{Type: program.TypeFloat, num: v.num - b.num}
}

// Mul returns a * b.
func (v Value) Mul(b Value) Value {
	if isInt(v, b) {
		return Value{Type: program.TypeInt, num: float64(int64(v.num) * int64(b.num))}
	}
	return Value{Type: program.TypeFloat, num: v.num * b.num}
}

// Div returns a / b. Division by zero returns 0.
func (v Value) Div(b Value) Value {
	if isInt(v, b) {
		if int64(b.num) == 0 {
			return Value{Type: program.TypeInt, num: 0}
		}
		return Value{Type: program.TypeInt, num: float64(int64(v.num) / int64(b.num))}
	}
	if b.num == 0 {
		return Value{Type: resultType(v, b), num: 0}
	}
	return Value{Type: program.TypeFloat, num: v.num / b.num}
}

// Mod returns a % b.
func (v Value) Mod(b Value) Value {
	if isInt(v, b) {
		if int64(b.num) == 0 {
			return Value{Type: program.TypeInt, num: 0}
		}
		return Value{Type: program.TypeInt, num: float64(int64(v.num) % int64(b.num))}
	}
	return Value{Type: program.TypeFloat, num: math.Mod(v.num, b.num)}
}

// Neg returns -v. The result keeps v's static type and drops any pointer
// payload, which matches the pre-union behaviour of building a fresh
// struct with only Type and Num set.
func (v Value) Neg() Value {
	return Value{Type: v.Type, num: -v.num}
}

// --- Comparisons --- all return BoolVal

// maxValueRecursionDepth bounds the recursion String, Eq, and ToAny
// use to walk the array and object payloads. The VM's in-place mutation
// contract (see lhs_set.go) lets OpIndexSet / OpFieldSet build a Value
// that contains itself (for example, `arr[0] = arr`). Go's recover()
// cannot catch a stack-overflow fatal error. These three walkers
// must stop on their own before they reach one.
//
// A depth cap does this without a visited-set allocation on every
// call. A genuine cycle keeps recursing past any legitimate nesting
// depth. So the cap only ever fires on a cycle, or on a
// pathologically deep (but not cyclic) structure. Either way,
// degrading to a sentinel beats a crash.
const maxValueRecursionDepth = 1000

// cycleSentinel is the string these walkers substitute for a Value
// that recurses past maxValueRecursionDepth.
const cycleSentinel = "<cycle>"

// Eq returns whether v == b.
func (v Value) Eq(b Value) Value {
	return eqAtDepth(v, b, 0)
}

func eqAtDepth(v, b Value, depth int) Value {
	if depth > maxValueRecursionDepth {
		return BoolVal(false)
	}
	// Array comparison
	if v.isList() || b.isList() {
		left, right := v.list(), b.list()
		if len(left) != len(right) {
			return BoolVal(false)
		}
		for i := range left {
			if eq := eqAtDepth(left[i], right[i], depth+1); !eq.truth() {
				return BoolVal(false)
			}
		}
		return BoolVal(true)
	}
	if v.isMap() || b.isMap() {
		left, right := v.dict(), b.dict()
		if len(left) != len(right) {
			return BoolVal(false)
		}
		for key, val := range left {
			other, ok := right[key]
			if !ok {
				return BoolVal(false)
			}
			if eq := eqAtDepth(val, other, depth+1); !eq.truth() {
				return BoolVal(false)
			}
		}
		return BoolVal(true)
	}
	if v.Type == program.TypeString || b.Type == program.TypeString {
		return BoolVal(v.text() == b.text())
	}
	if v.Type == program.TypeBool || b.Type == program.TypeBool {
		return BoolVal(v.truth() == b.truth())
	}
	return BoolVal(v.num == b.num)
}

// Neq returns whether v != b.
func (v Value) Neq(b Value) Value {
	r := v.Eq(b)
	return BoolVal(!r.truth())
}

// Lt returns whether v < b.
func (v Value) Lt(b Value) Value {
	return BoolVal(v.num < b.num)
}

// Gt returns whether v > b.
func (v Value) Gt(b Value) Value {
	return BoolVal(v.num > b.num)
}

// Lte returns whether v <= b.
func (v Value) Lte(b Value) Value {
	return BoolVal(v.num <= b.num)
}

// Gte returns whether v >= b.
func (v Value) Gte(b Value) Value {
	return BoolVal(v.num >= b.num)
}

// --- Boolean ops ---

// And returns v && b.
func (v Value) And(b Value) Value {
	return BoolVal(v.truth() && b.truth())
}

// Or returns v || b.
func (v Value) Or(b Value) Value {
	return BoolVal(v.truth() || b.truth())
}

// Not returns !v.
func (v Value) Not() Value {
	return BoolVal(!v.truth())
}

// --- String ops ---

// Concat returns the string concatenation of v and b.
func (v Value) Concat(b Value) Value {
	return StringVal(v.text() + b.text())
}

// Len returns the length of a string, array, or object Value as an int.
func (v Value) Len() int {
	switch v.kind() {
	case kindString, kindArray:
		return v.n
	case kindMap:
		return len(v.dict())
	default:
		return 0
	}
}

// String converts any Value to its string representation.
//
// The scalar paths use strconv directly instead of fmt.Sprintf so that
// an int-typed signal (by far the most common case, for example a counter
// value) renders without the fmt format-state scratch allocation. Runs once
// per expression evaluation touching any int or float signal. In a typical
// counter island with a "{count}" display that is N calls per reconcile.
func (v Value) String() string {
	return v.stringAtDepth(0)
}

// stringAtDepth renders v. It keeps only the scalar cases in its own body and
// hands the two collection cases to a helper each.
//
// The split is a performance decision, not a style one. A scalar Value is by
// far the common case, and the helpers need a make, a sort and a
// strings.Builder. Keeping those in this function gave it a 590-byte frame,
// and the callee spends its prologue spilling five register arguments into a
// frame that large. Moving them out shrinks the frame the scalar path pays for.
func (v Value) stringAtDepth(depth int) string {
	if depth > maxValueRecursionDepth {
		return cycleSentinel
	}
	if v.isList() {
		return v.arrayStringAtDepth(depth)
	}
	if v.isMap() {
		return v.objectStringAtDepth(depth)
	}
	switch v.Type {
	case program.TypeString:
		return v.text()
	case program.TypeInt:
		return strconv.FormatInt(int64(v.num), 10)
	case program.TypeFloat:
		return strconv.FormatFloat(v.num, 'g', -1, 64)
	case program.TypeBool:
		if v.truth() {
			return "true"
		}
		return "false"
	default:
		return strconv.FormatFloat(v.num, 'g', -1, 64)
	}
}

// arrayStringAtDepth renders an array payload as "[a, b, c]".
func (v Value) arrayStringAtDepth(depth int) string {
	items := v.list()
	parts := make([]string, len(items))
	for i := range items {
		parts[i] = items[i].stringAtDepth(depth + 1)
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

// objectStringAtDepth renders an object payload as "{k:v, k:v}". Keys sort so
// that the output is stable across runs.
func (v Value) objectStringAtDepth(depth int) string {
	fields := v.dict()
	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.Grow(2 + len(keys)*16)
	b.WriteByte('{')
	for i, key := range keys {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(key)
		b.WriteByte(':')
		field := fields[key]
		b.WriteString(field.stringAtDepth(depth + 1))
	}
	b.WriteByte('}')
	return b.String()
}

// IndexVal returns an indexed element from an array, object, or string.
func (v Value) IndexVal(index Value) Value {
	if v.isList() {
		items := v.list()
		idx := int(index.num)
		if idx < 0 || idx >= len(items) {
			return ZeroValue(program.TypeAny)
		}
		return items[idx]
	}
	if v.isMap() {
		if field, ok := v.dict()[index.String()]; ok {
			return field
		}
		return ZeroValue(program.TypeAny)
	}
	if v.Type == program.TypeString {
		text := v.text()
		idx := int(index.num)
		if idx < 0 || idx >= len(text) {
			return ZeroValue(program.TypeString)
		}
		return StringVal(text[idx : idx+1])
	}
	return ZeroValue(program.TypeAny)
}

// --- Array methods ---

// AppendVal returns a new Value with elem appended to the array payload.
func (v Value) AppendVal(elem Value) Value {
	items := v.list()
	newItems := make([]Value, len(items), len(items)+1)
	copy(newItems, items)
	newItems = append(newItems, elem)
	return ArrayVal(newItems)
}

// FilterFunc returns a new array Value containing only items for which pred returns true.
func (v Value) FilterFunc(pred func(Value) bool) Value {
	var result []Value
	for _, item := range v.list() {
		if pred(item) {
			result = append(result, item)
		}
	}
	return ArrayVal(result)
}

// MapFunc returns a new array Value with fn applied to each item.
func (v Value) MapFunc(fn func(Value, int) Value) Value {
	items := v.list()
	result := make([]Value, len(items))
	for i, item := range items {
		result[i] = fn(item, i)
	}
	return ArrayVal(result)
}

// FindFunc returns the first item for which pred returns true, or ZeroValue.
func (v Value) FindFunc(pred func(Value) bool) Value {
	for _, item := range v.list() {
		if pred(item) {
			return item
		}
	}
	return ZeroValue(program.TypeAny)
}

// SliceVal returns the array payload from start to end with bounds clamping.
//
// Both bounds clamp into [0, n] before the start-over-end swap runs.
// The end bound needs a low-side clamp too (end < 0 -> 0), not only
// a high-side clamp (end > n -> n). A negative end reaches the swap
// step as a smaller-than-start value. That used to send a negative
// index straight into the array slice expression, which panicked.
// Clamping end into range first keeps every later step working on
// values already inside [0, n].
func (v Value) SliceVal(start, end int) Value {
	items := v.list()
	n := len(items)
	if start < 0 {
		start = 0
	}
	if start > n {
		start = n
	}
	if end < 0 {
		end = 0
	}
	if end > n {
		end = n
	}
	if start > end {
		start = end
	}
	newItems := make([]Value, end-start)
	copy(newItems, items[start:end])
	return ArrayVal(newItems)
}

// ContainsVal reports whether elem is in the array payload, or whether
// elem's text is a substring of v's text.
func (v Value) ContainsVal(elem Value) Value {
	if v.isList() {
		for _, item := range v.list() {
			if r := item.Eq(elem); r.truth() {
				return BoolVal(true)
			}
		}
		return BoolVal(false)
	}
	return BoolVal(strings.Contains(v.text(), elem.text()))
}

// JoinVal joins the array payload as strings with the given separator.
func (v Value) JoinVal(sep string) Value {
	items := v.list()
	parts := make([]string, len(items))
	for i, item := range items {
		parts[i] = item.String()
	}
	return StringVal(strings.Join(parts, sep))
}

// --- String methods ---

// ToUpper returns a new Value with the text payload uppercased.
func (v Value) ToUpper() Value {
	return StringVal(strings.ToUpper(v.text()))
}

// ToLower returns a new Value with the text payload lowercased.
func (v Value) ToLower() Value {
	return StringVal(strings.ToLower(v.text()))
}

// TrimVal returns a new Value with whitespace trimmed from the text payload.
func (v Value) TrimVal() Value {
	return StringVal(strings.TrimSpace(v.text()))
}

// SplitVal splits the text payload by sep and returns an ArrayVal of StringVals.
func (v Value) SplitVal(sep string) Value {
	parts := strings.Split(v.text(), sep)
	items := make([]Value, len(parts))
	for i, p := range parts {
		items[i] = StringVal(p)
	}
	return ArrayVal(items)
}

// ReplaceVal returns a new Value with all occurrences of old replaced by new
// in the text payload.
func (v Value) ReplaceVal(old, new string) Value {
	return StringVal(strings.ReplaceAll(v.text(), old, new))
}

// SubstringVal returns the text payload from start to end with bounds clamping.
//
// Mirrors SliceVal's clamp order. Both sides of end clamp into
// [0, n] before the start-over-end swap runs. A negative end (for
// example, from `s[0:len(s)-1]` on an empty string) never reaches
// the string-slice expression as a negative index.
func (v Value) SubstringVal(start, end int) Value {
	text := v.text()
	n := len(text)
	if start < 0 {
		start = 0
	}
	if start > n {
		start = n
	}
	if end < 0 {
		end = 0
	}
	if end > n {
		end = n
	}
	if start > end {
		start = end
	}
	return StringVal(text[start:end])
}

// StartsWithVal reports whether v's text starts with prefix's text.
func (v Value) StartsWithVal(prefix Value) Value {
	return BoolVal(strings.HasPrefix(v.text(), prefix.text()))
}

// EndsWithVal reports whether v's text ends with suffix's text.
func (v Value) EndsWithVal(suffix Value) Value {
	return BoolVal(strings.HasSuffix(v.text(), suffix.text()))
}

// --- Type conversions ---

// ToStringVal converts any Value to a StringVal.
//
// Slice Y.E.3: when v is an ArrayVal whose items are all StringVals
// (the shape produced by `[]rune(s)` via OpToRunes), the conversion
// concatenates the items, mirroring Go's `string([]rune{...})` which
// joins the runes back into a string. This is what makes the
// graph_surface.go pattern `string([]rune(label)[:22])` produce a
// truncated label rather than the debug-formatted `"[h, e, l, l, o]"`
// shape that the array's String() form returns.
func (v Value) ToStringVal() Value {
	if v.isList() {
		items := v.list()
		if allStringItems(items) {
			var b strings.Builder
			for i := range items {
				b.WriteString(items[i].text())
			}
			return StringVal(b.String())
		}
	}
	return StringVal(v.String())
}

// allStringItems reports whether every element in items is a TypeString
// Value. Used by ToStringVal to decide between joining (the rune-array
// case) and debug-formatting (mixed arrays).
func allStringItems(items []Value) bool {
	if len(items) == 0 {
		return false
	}
	for _, item := range items {
		if item.Type != program.TypeString {
			return false
		}
	}
	return true
}

// ToIntVal converts a Value to an IntVal. Parses strings, truncates floats.
func (v Value) ToIntVal() Value {
	switch v.Type {
	case program.TypeInt:
		return v
	case program.TypeFloat:
		return IntVal(int(v.num))
	case program.TypeString:
		text := v.text()
		n, err := strconv.ParseInt(text, 10, 64)
		if err != nil {
			// Try parsing as float then truncating
			f, err2 := strconv.ParseFloat(text, 64)
			if err2 != nil {
				return IntVal(0)
			}
			return IntVal(int(f))
		}
		return IntVal(int(n))
	case program.TypeBool:
		if v.truth() {
			return IntVal(1)
		}
		return IntVal(0)
	default:
		return IntVal(0)
	}
}

// ToFloatVal converts a Value to a FloatVal. Parses strings, promotes ints.
func (v Value) ToFloatVal() Value {
	switch v.Type {
	case program.TypeFloat:
		return v
	case program.TypeInt:
		return FloatVal(v.num)
	case program.TypeString:
		f, err := strconv.ParseFloat(v.text(), 64)
		if err != nil {
			return FloatVal(0)
		}
		return FloatVal(f)
	case program.TypeBool:
		if v.truth() {
			return FloatVal(1)
		}
		return FloatVal(0)
	default:
		return FloatVal(0)
	}
}

// --- Value representation: why the layout is a 32-byte tagged union ---
//
// Value used to be a struct of parallel payload fields: a string header, a
// slice header, a map word, a closure word, a float, and three tag bytes.
// That cost 72 bytes and flattened to 11 leaf fields. The amd64 register ABI
// gives an argument at most 9 integer registers, so an 11-field Value went
// through memory on every call, three times per binary operation.
//
// The union stores one pointer word plus one length word and reads them
// through the kind bits of tag. That is 32 bytes and 4 integer-class leaf
// fields plus one float. A receiver and one Value argument now both pass in
// registers. Verify that claim with:
//
//	go build -gcflags=-S ./client/vm 2>&1 | grep 'TEXT.*Value..Add'
//
// A register-passed Add reports 64 bytes of argument area, which is only the
// spill slots for two 32-byte Values, and its prologue moves AX, BX, SI, R8,
// X0 and X1 into that area. The 72-byte layout reported 216 bytes, which is
// three whole Values on the stack, and read its operands from caller slots.
//
// MEASURED RESULT over the whole client/vm benchmark set, 12 interleaved A/B
// rounds on one loaded machine:
//
//	sec/op     geomean -12.2%
//	B/op       geomean -16.8%
//	allocs/op  geomean unchanged
//
// The biggest wins land where a Value crosses a call boundary:
// OpHostCallTwoArgs -29%, MapCompositeThenLookup100 -28%, OpHostCallDrawShape
// -26%, IndirectCallTwoArgs -24%, InvokeClosureCapturedWrite -24%, FieldSet
// -23%, IndexSetSlice -18%.
//
// One case got slower. A dedicated 15-round run put BenchmarkValueIntToString
// at 1.90 ns before and 2.09 ns after, about +0.19 ns. A register-passed callee
// spends its prologue writing five arguments into the frame, and String() on an
// int returns almost at once, so it pays the prologue and collects none of the
// benefit. The 72-byte layout had its arguments in memory already and skipped
// that step. Every benchmark that lets the Value travel further wins far more
// than 0.19 ns back, so the trade stands.
//
// MIGRATION HISTORY. Every route to this layout needed the same first step:
// Str, Num, Bool, Items and Fields had to stop being struct fields, because a
// union cannot expose five overlapping fields.
//
//   1. DONE. Reader methods sit beside the payload: Text(), Number(),
//      Truth(), List(), Map(), plus IsList() and IsMap().
//   2. DONE. Every reader inside client/bridge, client/wasm and ir uses the
//      methods.
//   3. DONE. SetField and SetIndex cover the in-place mutation sites.
//      OpFieldSet writes through SetField. Host receivers that fill a
//      caller-supplied props struct use it too.
//   4. DONE. The five payload fields are unexported. Six files outside
//      client/vm named them and now do not:
//        engine/surface/context_host.go       used ArrayVal and SetField
//        engine/surface/vm_host.go            used Number() and Text()
//        engine/surface/context_host_test.go
//        engine/surface/vm_host_test.go
//        client/bridge/bridge_test.go         missed by the first inventory
//        test/store_test.go
//   5. DONE. The union replaced the parallel fields. Control moved from an
//      exported field to Control() and WithControl(), because packing it into
//      tag is what holds the leaf-field count at 4.
//
// RULES A NEW FIELD MUST FOLLOW.
//
//   - Do not add an integer-class field. Four is the budget. A fifth pushes
//     the second Value argument back onto the stack and undoes the work.
//     Pack new tag-like state into the spare bits 6 and 7 of tag.
//   - A float field is cheap: float arguments use a separate register file
//     with 15 slots.
//   - Keep the noCompare marker. A comparable Value makes signal.New install a
//     pointer-based change test that misses in-place mutation, and makes it box
//     both operands on every Set.
//   - value_layout_test.go pins the size, the register-passing property, the
//     tag packing, and the non-comparability. Keep all four pinned.
//
// RULE FOR READING A PAYLOAD INSIDE THIS PACKAGE.
//
// Call the unexported pointer-receiver form: text(), list(), dict(), truth(),
// isList(), isMap(), control(). Read num directly, it is still a field.
//
// Do NOT call the exported value-receiver seams on a hot path in this package.
// The inliner gives each nested value-receiver call its own copy of the
// receiver, and it cannot fold those copies away once the Value lives in a
// stack slot. stringAtDepth chained IsList() over kind(), which produced three
// 32-byte MOVUPS copies per call and made BenchmarkValueIntToString 5x slower
// than the 72-byte layout it replaced. Switching that one function to pointer
// receivers took it from 9.3 ns back to 2.0 ns.
//
// THE IN-PLACE MUTATION CONTRACT, WHICH THE UNION KEEPS UNCHANGED.
//
// OpFieldSet and OpIndexSet mutate the array and object payloads IN PLACE, and
// island/program/program.go records that as a deliberate decision: composite
// parameters pass by reference because Go maps and slices are reference types.
// The union satisfies this by storing the map header word, or the array's data
// pointer, and writing through it. Two Values built from the same map or the
// same array still share one object, so a write through either is visible to
// both. Copy-on-write does not satisfy the contract and would break it.
//
// One narrow difference. List() rebuilds the slice header with capacity equal
// to length, because the union does not store a capacity. Nothing in the VM
// appended to a shared array in place, so no behaviour changed; AppendVal
// already copied. An append to a List() result now always allocates a fresh
// array, which is the safer of the two outcomes.
//
// A second narrow difference. ArrayVal(nil) and ObjectVal(nil) produce a Value
// that is not a list and not a map, exactly as a nil Items or Fields field read
// before. An empty but non-nil slice still reports IsList() == true, and its
// List() still comes back non-nil, because unsafe.SliceData hands back the
// original data pointer. Treat that pointer as unspecified all the same: ask
// IsList() whether a payload exists, and ask len() how long it is. Never test
// List() or Map() against nil.
