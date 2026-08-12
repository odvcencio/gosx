package vm

import (
	"m31labs.dev/gosx/island/program"
	"m31labs.dev/gosx/signal"
)

// Subtree reuse — route (b) of the Reconcile design note in island.go.
//
// Every event used to evaluate a complete new tree and throw away the parts
// that could not have changed. This file makes the walk skip those parts.
//
// The idea in three steps:
//
//  1. At program load, compute for every program node the set of reactive
//     inputs (signals and props) that its whole subtree reads. Call it the
//     read set. Compute it only when every expression in the subtree is
//     PURE, which means its result depends on nothing but its operands and
//     those named inputs.
//  2. Before each reconcile, compare the current value of every watched
//     input against the value it held at the end of the previous walk. The
//     inputs that differ form the write set.
//  3. During the walk, when a node's read set does not meet the write set,
//     copy the node's resolved subtree out of the previous tree instead of
//     evaluating it.
//
// Step 2 compares values instead of recording writes. That choice matters:
// a signal can be written from a handler, from the shared-signal store, from
// a host receiver, or from a bridge callback. A write recorder must know
// every one of those paths, and a path it does not know produces a node that
// silently stops updating. A value comparison knows none of them and still
// catches all of them.
//
// The comparison only works for scalar payloads. A composite Value (array,
// object, closure) is mutated in place by OpFieldSet and OpIndexSet, so the
// snapshot would alias the mutated storage and always compare equal. Any
// watched input holding a composite is therefore marked dirty on every
// reconcile. See sampleValue.
//
// What is deliberately NOT reused:
//
//   - Nodes inside a forEach body. The VM rebinds `_item`, `_index` and the
//     `as`/`index` names per iteration, so the same program node resolves to
//     different content in each pass. Reuse is gated on "singleton": the node
//     is reached exactly once from the root and no ancestor is a forEach.
//     TestForEachNodeUpdatesThroughReboundProp pins this.
//   - Text and expression nodes. mergeAdjacentText concatenates a run of
//     sibling text nodes into the first one, in place. Reusing a node whose
//     Text was already merged would concatenate the same text twice. Only
//     element nodes carry a Tag, and merging never touches them.
//   - Any subtree containing an impure opcode. isPureOp lists the opcodes
//     that qualify. A new opcode is impure until it is added there, so
//     forgetting to update the list costs speed, never correctness.

// maxWatchedInputs is the number of distinct signals and props one program
// may read before subtree reuse turns itself off. The read set is a bitmask
// in one machine word, so 64 is the natural cap. Islands read far fewer than
// this; a program above the cap simply evaluates its whole tree as before.
const maxWatchedInputs = 64

// maxReuseWalkDepth bounds the static analysis walks over the node and
// expression graphs. The lowerer emits a tree, so the bound never fires on
// well-formed input. It stops a hand-built or corrupted program from
// recursing without end at plan time.
const maxReuseWalkDepth = 512

// watchKind distinguishes the two namespaces a pure expression can read.
type watchKind uint8

const (
	// watchSignal names a reactive signal read through OpSignalGet.
	watchSignal watchKind = iota
	// watchProp names an island prop read through OpPropGet.
	watchProp
)

// watchKey identifies one reactive input.
type watchKey struct {
	kind watchKind
	name string
}

// nodeReuseInfo is the per-program-node result of the static analysis.
type nodeReuseInfo struct {
	// mask holds one bit per watched input the node's subtree reads.
	mask uint64
	// reusable reports whether the node's resolved subtree may be copied
	// from the previous tree when mask does not meet the write set.
	reusable bool
}

// reusePlan is the static analysis of one program. It is computed once per
// program and is read-only afterwards, so an Island may keep it across
// reconciles and a SwapProgram simply builds a new one.
type reusePlan struct {
	watch []watchKey
	index map[watchKey]int
	nodes []nodeReuseInfo
	exprs []exprReuseInfo
	// full is true when at least one node is reusable. A plan with no
	// reusable node saves nothing and is dropped by newReusePlan.
	full bool
}

// exprReuseInfo memoizes the analysis of one expression.
type exprReuseInfo struct {
	mask  uint64
	pure  bool
	done  bool
	busy  bool
	valid bool
}

// nodeSpan records where a program node's resolved subtree landed in a tree.
// The subtree is contiguous: appendResolvedNode writes the node first and
// every descendant after it, so [start, end) covers the whole subtree.
//
// The two flags share the four bytes of padding after end, so recording where
// a subtree came from costs no memory.
type nodeSpan struct {
	start int32
	end   int32
	valid bool
	// verbatim reports that this walk copied the subtree out of the previous
	// tree into the index range it already held. The diff reads it and stops
	// at start. See diffWalk.skipsSubtree.
	verbatim bool
}

// treeReuse is the per-walk state the VM reads while building a tree. The
// Island owns it and re-points its fields on every reconcile, so the walk
// allocates nothing for the bookkeeping.
type treeReuse struct {
	plan *reusePlan
	// prev is the tree the previous walk produced. Reused subtrees are
	// copied out of it.
	prev *ResolvedTree
	// prevSpans locates each reusable node's subtree inside prev.
	prevSpans []nodeSpan
	// spans records the same for the tree being built now, and marks which
	// of those subtrees were copied verbatim.
	spans []nodeSpan
	// dirty holds one bit per watched input whose value changed.
	dirty uint64
	// hits counts the subtrees the last walk copied instead of
	// evaluating. Tests read it to prove that reuse fired; nothing in the
	// runtime path depends on it.
	hits int
}

// Verbatim subtrees and the diff.
//
// A subtree the walk copied out of the previous tree into the index range it
// already held needs no diff at all. The copy is a struct assignment, and at an
// unchanged index the copied nodes keep the child indices and even the Children
// arrays they held before. Every node in the range therefore equals the node
// the diff would compare it against, at the same index, so the diff provably
// emits nothing and the walk stops at the root.
//
// nodeSpan.verbatim carries that fact, and the diff finds the span through the
// resolved node's Source field. No second table is needed.
//
// Read a wrong verbatim flag as a silent defect, not as a wrong patch. It emits
// no op, the browser keeps the previous generation on screen, and a test that
// asserts on the emitted ops still passes because the ops it lists are the ops
// that were emitted. FuzzIslandRecycleMatchesFreshTree drives a third island
// with skipOff set and compares the ops step by step, which is the guard that
// can catch it. TestSkipSuppressesTheDiffOfACopiedSubtree pins that the flag
// disables the skip, so that arm cannot degrade into comparing an island with
// itself.

// newReusePlan analyses prog and returns a plan, or nil when subtree reuse
// cannot help. A nil plan makes every reconcile evaluate the whole tree, so
// returning nil is always safe.
func newReusePlan(prog *program.Program) *reusePlan {
	if prog == nil || len(prog.Nodes) == 0 || int(prog.Root) >= len(prog.Nodes) {
		return nil
	}
	plan := &reusePlan{
		index: make(map[watchKey]int),
		nodes: make([]nodeReuseInfo, len(prog.Nodes)),
		exprs: make([]exprReuseInfo, len(prog.Exprs)),
	}
	singleton := planSingletons(prog)
	for id := range prog.Nodes {
		mask, pure := plan.nodeInfo(prog, program.NodeID(id), 0)
		node := prog.Nodes[id]
		// The tag requirement is the merge guard, decided here so the walk
		// pays nothing for it. mergeAdjacentText appends a following
		// sibling's text into a text node IN PLACE, and it reads any node
		// with no tag and no children as one. A copied subtree whose root
		// reads that way could therefore be rewritten after the copy, which
		// would both corrupt the tree and break the diff skip. Only a tagged
		// element can be reused, and every element the lowerer emits has a
		// tag.
		reusable := pure && singleton[id] && node.Kind == program.NodeElement && node.Tag != ""
		plan.nodes[id] = nodeReuseInfo{mask: mask, reusable: reusable}
		if reusable {
			plan.full = true
		}
	}
	if !plan.full || len(plan.watch) > maxWatchedInputs {
		return nil
	}
	return plan
}

// planSingletons marks the program nodes that the walk from Root reaches
// exactly once with no forEach ancestor. Those are the only nodes whose
// resolved subtree occupies one predictable place in the tree.
func planSingletons(prog *program.Program) []bool {
	singleton := make([]bool, len(prog.Nodes))
	visited := make([]bool, len(prog.Nodes))
	var poison func(id program.NodeID, depth int)
	poison = func(id program.NodeID, depth int) {
		if int(id) >= len(prog.Nodes) || depth > maxReuseWalkDepth || !singleton[id] {
			return
		}
		singleton[id] = false
		for _, child := range prog.Nodes[id].Children {
			poison(child, depth+1)
		}
	}
	var walk func(id program.NodeID, inLoop bool, depth int)
	walk = func(id program.NodeID, inLoop bool, depth int) {
		if int(id) >= len(prog.Nodes) || depth > maxReuseWalkDepth {
			return
		}
		if visited[id] {
			// A second path reaches this node, so it resolves more than
			// once and its subtree loses its single home in the tree.
			poison(id, depth)
			return
		}
		visited[id] = true
		singleton[id] = !inLoop
		node := prog.Nodes[id]
		childInLoop := inLoop || node.Kind == program.NodeForEach
		for _, child := range node.Children {
			walk(child, childInLoop, depth+1)
		}
	}
	walk(prog.Root, false, 0)
	return singleton
}

// nodeInfo returns the read set of the subtree rooted at id, and whether
// every expression in that subtree is pure.
func (plan *reusePlan) nodeInfo(prog *program.Program, id program.NodeID, depth int) (uint64, bool) {
	if int(id) >= len(prog.Nodes) || depth > maxReuseWalkDepth {
		return 0, false
	}
	node := prog.Nodes[id]
	var mask uint64
	// Every node kind that carries an expression stores it in the same
	// field: NodeExpr's content, NodeForEach's collection, NodeConditional's
	// condition. NodeElement and NodeText leave it at zero, and expression 0
	// is a legal ID, so read it only for the kinds that use it.
	switch node.Kind {
	case program.NodeExpr, program.NodeForEach, program.NodeConditional:
		exprMask, pure := plan.exprInfo(prog, node.Expr, depth+1)
		if !pure {
			return 0, false
		}
		mask |= exprMask
	}
	for _, attr := range node.Attrs {
		if attr.Kind != program.AttrExpr {
			continue
		}
		exprMask, pure := plan.exprInfo(prog, attr.Expr, depth+1)
		if !pure {
			return 0, false
		}
		mask |= exprMask
	}
	for _, child := range node.Children {
		childMask, pure := plan.nodeInfo(prog, child, depth+1)
		if !pure {
			return 0, false
		}
		mask |= childMask
	}
	return mask, true
}

// exprInfo returns the read set of the expression tree rooted at id, and
// whether every opcode in it is pure. Results are memoized because the same
// expression is often shared by several nodes.
func (plan *reusePlan) exprInfo(prog *program.Program, id program.ExprID, depth int) (uint64, bool) {
	if int(id) >= len(prog.Exprs) || depth > maxReuseWalkDepth {
		return 0, false
	}
	slot := &plan.exprs[id]
	if slot.done {
		return slot.mask, slot.pure
	}
	if slot.busy {
		// An expression that reaches itself cannot be proved pure.
		return 0, false
	}
	slot.busy = true
	mask, pure := plan.exprInfoUncached(prog, id, depth)
	slot.busy = false
	slot.mask = mask
	slot.pure = pure
	slot.done = true
	return mask, pure
}

func (plan *reusePlan) exprInfoUncached(prog *program.Program, id program.ExprID, depth int) (uint64, bool) {
	expr := &prog.Exprs[id]
	switch expr.Op {
	case program.OpSignalGet:
		return plan.watchBit(watchKey{kind: watchSignal, name: expr.Value})
	case program.OpPropGet:
		return plan.watchBit(watchKey{kind: watchProp, name: expr.Value})
	}
	if !isPureOp(expr.Op) {
		return 0, false
	}
	var mask uint64
	for _, operand := range expr.Operands {
		operandMask, pure := plan.exprInfo(prog, operand, depth+1)
		if !pure {
			return 0, false
		}
		mask |= operandMask
	}
	return mask, true
}

// watchBit assigns key a bit position, creating it on first use. It returns
// an impure result once the program passes maxWatchedInputs, which switches
// reuse off for the whole program.
func (plan *reusePlan) watchBit(key watchKey) (uint64, bool) {
	if key.name == "" {
		return 0, false
	}
	pos, ok := plan.index[key]
	if !ok {
		if len(plan.watch) >= maxWatchedInputs {
			return 0, false
		}
		pos = len(plan.watch)
		plan.watch = append(plan.watch, key)
		plan.index[key] = pos
	}
	return uint64(1) << uint(pos), true
}

// isPureOp reports whether an opcode's result depends only on its operands.
//
// Read the list as a contract, not as an optimisation table. An opcode
// belongs here only when re-evaluating it with the same watched inputs
// produces the same Value and records no diagnostic. Everything absent from
// the list is treated as impure, so a new opcode is safe by default and only
// loses the reuse win until someone adds it.
//
// Four groups are deliberately excluded:
//
//   - Writers (OpSignalSet, OpAssign, OpFieldSet, OpIndexSet, the local and
//     loop opcodes). They change state, so skipping them changes behaviour.
//   - Scope rebinders (OpMap, OpFilter, OpFind, OpForRange, OpClosure, the
//     call opcodes). They bind `_item` and friends, so a read set built from
//     names alone would miss what the body actually depends on.
//   - Ambient readers (OpEventGet, OpHostCall). Their result changes with
//     state this analysis does not watch.
//   - OpSignalUpdate, which both reads and writes.
func isPureOp(op program.OpCode) bool {
	switch op {
	case program.OpLitString, program.OpLitInt, program.OpLitFloat, program.OpLitBool,
		program.OpAdd, program.OpSub, program.OpMul, program.OpDiv, program.OpMod, program.OpNeg,
		program.OpEq, program.OpNeq, program.OpLt, program.OpGt, program.OpLte, program.OpGte,
		program.OpAnd, program.OpOr, program.OpNot,
		program.OpConcat, program.OpFormat, program.OpCond,
		program.OpIndex, program.OpLen,
		program.OpSlice, program.OpAppend, program.OpContains,
		program.OpToUpper, program.OpToLower, program.OpTrim, program.OpSplit, program.OpJoin,
		program.OpReplace, program.OpSubstring, program.OpStartsWith, program.OpEndsWith,
		program.OpToString, program.OpToInt, program.OpToFloat, program.OpToRunes,
		program.OpMapLookup:
		return true
	default:
		return false
	}
}

// watchSample is the comparable snapshot of one watched input. It copies the
// scalar payload out of the Value instead of holding the Value, so a stale
// snapshot never keeps an object alive and never aliases mutable storage.
type watchSample struct {
	present bool
	// stable is false for a composite or closure payload. Those mutate in
	// place, so a snapshot of them proves nothing and the input counts as
	// dirty on every reconcile.
	stable  bool
	typ     program.ExprType
	num     float64
	str     string
	boolean bool
	// rev is the signal revision this snapshot was read at, and revValid
	// says the reading came from a live signal. refresh compares them to
	// skip the read entirely. They live here, next to the value they belong
	// to, so the snapshot needs no second array. sameSample ignores them:
	// a write that restored the same value moves the revision and is not a
	// change of state.
	rev      uint64
	revValid bool
}

// sampleValue snapshots v. present reports whether the input existed at all;
// a missing input counts as dirty so that a transient loop binding, which is
// absent between reconciles, never looks unchanged.
func sampleValue(v Value, present bool) watchSample {
	if !present {
		return watchSample{}
	}
	if v.isList() || v.isMap() || v.closureRefOf() != nil {
		return watchSample{present: true}
	}
	return watchSample{
		present: true,
		stable:  true,
		typ:     v.Type,
		num:     v.num,
		str:     v.text(),
		boolean: v.truth(),
	}
}

// sampleDiffers reports whether the input changed between two snapshots, or
// whether either snapshot is one this analysis cannot trust.
func sampleDiffers(prev, cur watchSample) bool {
	if !prev.present || !cur.present || !prev.stable || !cur.stable {
		return true
	}
	return !sameSample(prev, cur)
}

// sameSample reports whether two snapshots describe the same input state.
//
// It names every field it compares. The struct also carries the revision the
// snapshot was read at, and that number moves on a write that restored the
// value it already held, which is not a change of state. A whole-struct
// comparison would report such a write as a change and re-evaluate the subtree
// that reads it on every reconcile.
func sameSample(left, right watchSample) bool {
	return left.present == right.present &&
		left.stable == right.stable &&
		left.typ == right.typ &&
		left.num == right.num &&
		left.str == right.str &&
		left.boolean == right.boolean
}

// watchSampleAt reads the live value of the watched input at index i.
//
// It answers a signal from the bound pointer table and a prop from the props
// map. TestWatchSampleAtMatchesTheNameLookup pins that it agrees with the
// name-based reference reader below.
func (r *islandReuse) watchSampleAt(vm *VM, i int, key watchKey) watchSample {
	if key.kind != watchSignal {
		v, ok := vm.props[key.name]
		return sampleValue(v, ok)
	}
	sig := r.watchSigs[i]
	if sig == nil {
		computed, ok := vm.computeds[key.name]
		if !ok || computed == nil {
			return watchSample{}
		}
		// Computed values do not expose a mutable write revision. Sampling
		// invokes their lazy refresh and lets the ordinary scalar comparison
		// decide whether this subtree can be copied verbatim.
		return sampleValue(computed.Get(), true)
	}
	return sampleValue(sig.Get(), true)
}

// stampedSample reads the value of the watched input at index i and stamps it
// with a revision the caller ALREADY read.
//
// The revision must be the older reading of the two. A write that lands between
// the two reads then leaves a stamp that is older than the value, so the next
// refresh finds a different number and reads again, which costs one read. The
// other order stamps the value with a number that is newer than it, every later
// refresh finds the numbers equal, and the island renders the value it held
// before the write for ever. See Signal.Revision.
//
// The caller reads the revision first because it uses the number to decide
// whether to call this at all. Keeping the stamp here, and taking the reading
// as an argument, means no code path can read the value first.
func (r *islandReuse) stampedSample(vm *VM, i int, key watchKey, rev uint64, revValid bool) watchSample {
	cur := r.watchSampleAt(vm, i, key)
	cur.rev, cur.revValid = rev, revValid
	return cur
}

// signalRevision reads the write counter of the watched input at index i. The
// second result is false for a prop and for a name no signal is bound to.
func (r *islandReuse) signalRevision(i int) (uint64, bool) {
	sig := r.watchSigs[i]
	if sig == nil {
		return 0, false
	}
	return sig.Revision(), true
}

// unwrittenSince reports that nothing wrote the watched input at index i since
// the walk that stored r.snap[i], so that sample is still the live value.
//
// Three conditions carry the proof:
//
//   - Both revisions are readings of the SAME signal. bindWatchedSignals
//     drops the stored number whenever it replaces a signal.
//   - The numbers are equal, so no Set or Update changed the value between
//     the two readings. Every write path in the package goes through one of
//     those two methods.
//   - The stored sample is stable, which means it holds a scalar payload. A
//     composite is mutated in place by OpFieldSet and OpIndexSet without any
//     Set at all, so its revision proves nothing and sampleValue already
//     marks it unstable. See the note on sampleValue.
func (r *islandReuse) unwrittenSince(i int, rev uint64, revValid bool) bool {
	stored := r.snap[i]
	return revValid && stored.revValid && stored.rev == rev && stored.stable
}

// currentWatchSample reads the live value of one watched input by name. It is
// the reference reader watchSampleAt is checked against.
func (vm *VM) currentWatchSample(key watchKey) watchSample {
	switch key.kind {
	case watchSignal:
		sig, ok := vm.signals[key.name]
		if ok && sig != nil {
			return sampleValue(sig.Get(), true)
		}
		computed, ok := vm.computeds[key.name]
		if !ok || computed == nil {
			return watchSample{}
		}
		return sampleValue(computed.Get(), true)
	default:
		v, ok := vm.props[key.name]
		return sampleValue(v, ok)
	}
}

// autoKeyProps are the two props resolveElementNode reads outside any
// expression, to synthesize a node key. They are not part of any read set,
// so a change in either forces a full re-evaluation.
var autoKeyProps = [2]string{"_key", "_index"}

// islandReuse is the Island-owned state that carries subtree reuse across
// reconciles.
type islandReuse struct {
	plan *reusePlan
	// snap holds the value of every watched input at the start of the
	// previous walk.
	snap []watchSample
	// autoKeySnap holds the same for the two auto-key props.
	autoKeySnap [2]watchSample
	// primed is false until the first walk has filled snap.
	primed bool
	// ctx is handed to the VM for the duration of one walk.
	ctx treeReuse
	// spansA and spansB alternate: one holds the previous walk's spans
	// while the other records the current walk's. Alternating them keeps
	// the reconcile free of a per-event allocation.
	spansA []nodeSpan
	spansB []nodeSpan
	// watchSigs holds the live signal for each watched input of kind
	// watchSignal, and nil for a prop. Resolving the name once turns a map
	// probe per input per reconcile into a slice index.
	watchSigs []*signal.Signal[Value]
	// watchGen is the VM signal generation watchSigs was bound against.
	watchGen uint32
	// watchBound reports that watchSigs has been filled at least once.
	watchBound bool
}

// bindWatchedSignals resolves every watched signal name against the VM once.
//
// The generation counter is the invalidation. VM.SetSignal and
// VM.SwapProgram are the only writers of the signal table, and both bump it,
// so a stale pointer cannot survive either a shared-signal install or a hot
// swap.
func (r *islandReuse) bindWatchedSignals(vm *VM) {
	if r.watchBound && r.watchGen == vm.signalGen {
		return
	}
	if len(r.watchSigs) != len(r.plan.watch) {
		r.watchSigs = make([]*signal.Signal[Value], len(r.plan.watch))
	}
	for i, key := range r.plan.watch {
		if key.kind != watchSignal {
			r.watchSigs[i] = nil
			continue
		}
		r.watchSigs[i] = vm.signals[key.name]
	}
	// A rebind replaces the signals, and a revision only means something
	// against the signal it came from. Drop every stored number so the next
	// refresh reads the values instead of trusting a foreign counter.
	for i := range r.snap {
		r.snap[i].revValid = false
	}
	r.watchGen = vm.signalGen
	r.watchBound = true
}

// newIslandReuse builds the reuse state for prog, or nil when the program
// cannot benefit.
func newIslandReuse(prog *program.Program) *islandReuse {
	plan := newReusePlan(prog)
	if plan == nil {
		return nil
	}
	return &islandReuse{
		plan:   plan,
		snap:   make([]watchSample, len(plan.watch)),
		spansA: make([]nodeSpan, len(prog.Nodes)),
		spansB: make([]nodeSpan, len(prog.Nodes)),
	}
}

// refresh compares every watched input against the previous snapshot,
// updates the snapshot, and returns the write set.
//
// The snapshot is taken BEFORE the walk on purpose. An impure expression
// evaluated during the walk may write a signal; taking the snapshot first
// means the next reconcile sees that write and re-evaluates. Taking it after
// would hide the write.
//
// Most inputs are unwritten on most reconciles, and reading one costs a mutex
// lock and unlock inside Signal.Get. unwrittenSince answers "nobody wrote this
// signal" with one atomic load, so the common case pays neither the lock nor
// the sampling. It is an early exit only: an input it does not clear falls
// through to the value comparison below, unchanged.
func (r *islandReuse) refresh(vm *VM) uint64 {
	r.bindWatchedSignals(vm)
	var dirty uint64
	for i, key := range r.plan.watch {
		rev, revValid := r.signalRevision(i)
		if r.primed && r.unwrittenSince(i, rev, revValid) {
			continue
		}
		cur := r.stampedSample(vm, i, key, rev, revValid)
		if !r.primed || sampleDiffers(r.snap[i], cur) {
			dirty |= uint64(1) << uint(i)
		}
		r.snap[i] = cur
	}
	for i, name := range autoKeyProps {
		v, ok := vm.props[name]
		// Absent on both sides is the normal case for an island that is
		// not rendered inside a list, and it is the case this loop runs
		// on nearly every reconcile. Answering it from the presence flags
		// skips building a sample, comparing it and storing it.
		if !ok && !r.autoKeySnap[i].present {
			continue
		}
		cur := sampleValue(v, ok)
		// Treat any presence or value change as a full invalidation.
		changed := !sameSample(cur, r.autoKeySnap[i])
		r.autoKeySnap[i] = cur
		if r.primed && changed {
			dirty = ^uint64(0)
		}
	}
	if !r.primed {
		dirty = ^uint64(0)
	}
	return dirty
}

// begin points the VM at this walk's reuse context and returns it so the
// caller can hand it back with end.
func (r *islandReuse) begin(vm *VM, prev *ResolvedTree, dirty uint64) {
	r.spansA, r.spansB = r.spansB, r.spansA
	clear(r.spansA)
	r.ctx.plan = r.plan
	r.ctx.prev = prev
	r.ctx.prevSpans = r.spansB
	r.ctx.spans = r.spansA
	r.ctx.dirty = dirty
	r.ctx.hits = 0
	if !r.primed || prev == nil {
		// Nothing trustworthy to copy from on the first walk.
		r.ctx.prev = nil
	}
	vm.reuse = &r.ctx
}

// end detaches the reuse context and marks the snapshot usable.
func (r *islandReuse) end(vm *VM) {
	vm.reuse = nil
	r.ctx.prev = nil
	r.primed = true
}

// reuseResolvedNode copies the subtree of program node nodeID out of the
// previous tree when nothing it reads has changed. It returns the index of
// the copied root and true on success.
//
// Index remapping: the copied nodes carry child indices that point into the
// previous tree. When the subtree lands at the same index it held before,
// which is the common case because nothing ahead of it changed shape, the
// indices already agree and even the Children slices are shared. Otherwise
// each Children slice is rebuilt with the offset applied.
func (vm *VM) reuseResolvedNode(tree *ResolvedTree, nodeID int) (int, bool) {
	r := vm.reuse
	if r == nil || r.prev == nil || r.plan == nil {
		return 0, false
	}
	if nodeID < 0 || nodeID >= len(r.plan.nodes) || nodeID >= len(r.prevSpans) {
		return 0, false
	}
	info := r.plan.nodes[nodeID]
	if !info.reusable || info.mask&r.dirty != 0 {
		return 0, false
	}
	span := r.prevSpans[nodeID]
	if !span.valid || span.start < 0 || span.end < span.start || int(span.end) > len(r.prev.Nodes) {
		return 0, false
	}
	base := len(tree.Nodes)
	tree.Nodes = append(tree.Nodes, r.prev.Nodes[span.start:span.end]...)
	verbatim := false
	if delta := base - int(span.start); delta != 0 {
		for i := base; i < len(tree.Nodes); i++ {
			node := &tree.Nodes[i]
			if len(node.Children) == 0 {
				continue
			}
			shifted := make([]int, len(node.Children))
			for j, child := range node.Children {
				shifted[j] = child + delta
			}
			node.Children = shifted
		}
	} else {
		// The copy landed on the indices it came from, so the diff would
		// compare every node in it against an equal node at an equal index
		// and emit nothing. Record that, so the diff stops at the root
		// instead of walking it.
		//
		// Nothing rewrites the copy after this point. newReusePlan limits
		// reuse to a TAGGED element, which is the one shape
		// mergeAdjacentText never touches, so the check costs plan time
		// instead of one read per copy.
		verbatim = true
	}
	vm.recordReuseSpan(nodeID, base, len(tree.Nodes), verbatim)
	r.hits++
	return base, true
}

// recordReuseSpan notes where a reusable node's subtree landed so the next
// walk can copy it. verbatim reports that this walk copied the subtree to the
// range it already held, which is what lets the diff stop at start.
func (vm *VM) recordReuseSpan(nodeID, start, end int, verbatim bool) {
	r := vm.reuse
	if r == nil || nodeID < 0 || nodeID >= len(r.spans) {
		return
	}
	if nodeID >= len(r.plan.nodes) || !r.plan.nodes[nodeID].reusable {
		return
	}
	r.spans[nodeID] = nodeSpan{
		start:    int32(start),
		end:      int32(end),
		valid:    true,
		verbatim: verbatim,
	}
}
