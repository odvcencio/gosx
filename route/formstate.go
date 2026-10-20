package route

import (
	"fmt"
	"maps"

	"m31labs.dev/gosx/session"
)

// FormState is the renderable state of a named file action. Put it in a
// strict page's props and fill it with RouteContext.FormState in Load.
// Values, FieldErrors and Flash use string keys; absent keys read as "".
type FormState struct {
	ActionURL   string
	CSRFToken   string
	Values      map[string]string
	FieldErrors map[string]string
	Flash       map[string]string
	Message     string
	OK          bool
	Status      int
}

// FormState prepares a named action's URL, token, validation result and the
// first string representation of each session flash for a strict view.
// Reading state does not create a session or consume flashes again. Without
// session middleware, the token and flashed state are empty.
func (ctx *RouteContext) FormState(name string) FormState {
	state := FormState{ActionURL: ctx.ActionPath(name)}
	if ctx == nil || ctx.Request == nil {
		return state
	}
	state.CSRFToken = session.Token(ctx.Request)
	if view, ok := ctx.ActionState(name); ok {
		state.Values = maps.Clone(view.Result.Values)
		state.FieldErrors = maps.Clone(view.Result.FieldErrors)
		state.Message = view.Result.Message
		state.OK = view.Result.OK
		state.Status = view.Status
	}
	for key, values := range session.FlashValues(ctx.Request) {
		if len(values) == 0 || key == "__gosx_action_state" {
			continue
		}
		if state.Flash == nil {
			state.Flash = make(map[string]string)
		}
		if values[0] != nil {
			state.Flash[key] = fmt.Sprint(values[0])
		}
	}
	return state
}
