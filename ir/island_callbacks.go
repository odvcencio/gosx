package ir

import (
	"fmt"
	"strings"

	"m31labs.dev/gosx/island/program"
)

// Callbacks are compiler bindings to handlers owned by the root island. They
// never become props or expressions in the serialized VM value graph.
type islandCallbackError struct{ message string }

func (e *islandCallbackError) Error() string { return e.message }

func callbackError(format string, args ...any) error {
	return &islandCallbackError{message: fmt.Sprintf(format, args...)}
}

func islandCallbackType(typ string) bool {
	return strings.Join(strings.Fields(typ), "") == "func()"
}

func islandCallbackField(source string) (string, bool) {
	exprs, root, err := ParseExpr(source, nil)
	if err != nil {
		return "", false
	}
	return directPropsField(exprs, root)
}

func (l *islandLowerer) callbackHandler(source string, context *islandInlineContext, scope *ExprScope) (string, error) {
	exprs, root, err := ParseExpr(source, scope)
	if err == nil {
		if field, ok := directPropsField(exprs, root); ok && context != nil {
			if handler := context.callbacks[field]; handler != "" {
				return handler, nil
			}
		}
		if context == nil || context.component == "" {
			expr := exprs[root]
			if expr.Op == program.OpPropGet && scope != nil && scope.Handlers[expr.Value] && !scope.Props[expr.Value] {
				component := l.src.Components[l.componentIndex[l.dst.Name]]
				if component.Scope != nil {
					for _, handler := range component.Scope.Handlers {
						if handler.Name == expr.Value {
							if handler.HasParameters || handler.HasResults {
								return "", callbackError("callback handler %s must have signature func()", expr.Value)
							}
							return expr.Value, nil
						}
					}
				}
			}
		}
	}
	return "", callbackError("callback %q must name a parent island handler or forward a func() callback prop", source)
}
