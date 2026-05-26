package vm

import (
	"testing"

	"m31labs.dev/gosx/island/program"
	"m31labs.dev/gosx/signal"
)

type fakeReconciler struct {
	disposed      bool
	sharedSignals map[string]*signal.Signal[Value]
}

func (f *fakeReconciler) Dispose() { f.disposed = true }

func (f *fakeReconciler) EvalExpr(id program.ExprID) Value { return IntVal(int(id)) }

func (f *fakeReconciler) SetSharedSignal(name string, sig *signal.Signal[Value]) {
	if f.sharedSignals == nil {
		f.sharedSignals = map[string]*signal.Signal[Value]{}
	}
	f.sharedSignals[name] = sig
}

func TestReconcilerInterfaceShape(t *testing.T) {
	var r Reconciler = &fakeReconciler{}
	r.Dispose()
	got := r.EvalExpr(7)
	want := IntVal(7)
	if got.Type != want.Type || got.Num != want.Num {
		t.Errorf("EvalExpr returned %v, want %v", got, want)
	}
	r.SetSharedSignal("x", signal.New(IntVal(1)))
	f := r.(*fakeReconciler)
	if !f.disposed {
		t.Errorf("Dispose did not run")
	}
	if _, ok := f.sharedSignals["x"]; !ok {
		t.Errorf("SetSharedSignal did not record")
	}
}
