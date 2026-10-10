package wire

// Completeness models accidental regressions in our own app: code a developer
// would plausibly write, including common esbuild/Terser output, using modelled
// forms. Code deliberately hiding loads is outside the threat model. Computed
// access, code construction, enumeration/reflection, global-object aliasing
// except static alias.name, and the loader denylist always mean incomplete.
// Known literal references are retained alongside that uncertainty.
func javaScriptGlobalObjectAlias(name string) bool {
	return globalObjectAliases[name]
}

// Enumeration accesses/keys can expose capabilities without naming a loader.
// The AST policy distinguishes these from ordinary local variables named keys
// or values. Existing reflection/assign tokens also stay on the loader denylist.
func unmodeledJavaScriptEnumeration(name string) bool {
	return enumerationLoaderTokens[name]
}

// unmodeledJavaScriptLoader is a capability policy, not receiver/type analysis.
// These tokens can create, activate or redirect resources, or conceal a loader.
// Their appearance in accesses, bindings and property keys prevents a complete
// claim even when a local object might use the same name for an unrelated API.
// Literal fetch/import/worker/URL calls and inline function timers have separate
// models; every other use of those capabilities is also incomplete.
func unmodeledJavaScriptLoader(name string) bool {
	return deniedLoaderTokens[name]
}
