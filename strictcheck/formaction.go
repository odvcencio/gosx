package strictcheck

import "m31labs.dev/gosx/transpile"

// validateFormActionContract intentionally does not infer registrations from
// file modules. Managed actions are registered explicitly on the owning
// route.Router with RegisterManagedPOST and the framework reserves one
// global /gosx/action/{name} namespace; a static page-local registry would
// reintroduce the deleted compatibility surface and could report false
// certainty for mounted routers.
func validateFormActionContract(_ []transpile.PackageFile, _ Options) error {
	return nil
}
