package pagecaps

// ExecutableScriptType is the type-only form of the shared element rule.
func ExecutableScriptType(typ string) bool {
	return ScriptExecutes("", map[string]string{"type": typ})
}
