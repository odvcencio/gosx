package action_test

import (
	"m31labs.dev/gosx/action"
	"testing"
)

func TestResultErrorUnkeyedCompatibility(t *testing.T) {
	e := &action.ResultError{400, action.Result{OK: true, Message: "failed"}}
	if e.ActionResult().OK {
		t.Fatal("ordinary literal became successful")
	}
	if action.Redirect("/settings").Status != 303 {
		t.Fatal("redirect status changed")
	}
}
