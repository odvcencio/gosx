package route

import (
	"fmt"
	"path/filepath"
	"sort"

	"m31labs.dev/gosx/ir"
)

type sharedComponentIdentity struct{ path, component string }

func sharedIdentity(target sharedComponentTarget) (sharedComponentIdentity, error) {
	path, err := filepath.Abs(target.path)
	if err != nil {
		return sharedComponentIdentity{}, err
	}
	path, err = filepath.EvalSymlinks(path)
	if err != nil {
		return sharedComponentIdentity{}, err
	}
	return sharedComponentIdentity{path, target.comp.Name}, nil
}

// A request-local recursion stack, not a global visited cache: repeated sibling
// calls remain valid and every request observes current per-file dependencies.
// Both branches and named slots are inspected, even when not selected at render.
func validateSharedPureClosure(target sharedComponentTarget, active map[sharedComponentIdentity]bool) error {
	identity, err := sharedIdentity(target)
	if err != nil {
		return err
	}
	if active[identity] {
		return fmt.Errorf("shared component cycle at %s:%s", identity.path, identity.component)
	}
	if target.comp.IsIsland || target.comp.IsEngine {
		return fmt.Errorf("shared pure-view closure reaches island/engine %s in %s; strict pure-view components only", target.comp.Name, target.path)
	}
	active[identity] = true
	defer delete(active, identity)
	components := make(map[string]*ir.Component)
	for i := range target.prog.Components {
		c := &target.prog.Components[i]
		components[c.Name] = c
	}
	seen := make(map[ir.NodeID]bool)
	var visit func(ir.NodeID) error
	visit = func(id ir.NodeID) error {
		if seen[id] {
			return nil
		}
		seen[id] = true
		node := target.prog.NodeAt(id)
		if node == nil {
			return fmt.Errorf("invalid shared component node in %s", target.path)
		}
		if node.Kind == ir.NodeComponent && !node.IsSyntheticConditional() {
			// Only strict pure declarations precede builtins in writeComponent.
			// A typed legacy function named Scene3D/Surface/Worker cannot hide
			// the engine builtin from this ownership check.
			comp := components[node.Tag]
			strictPure := comp != nil && comp.Syntax == ir.ComponentSyntaxStrict && !comp.IsIsland && !comp.IsEngine
			if !strictPure && (node.Tag == "Surface" || node.Tag == "Worker" || node.Tag == "Scene3D") {
				return fmt.Errorf("shared pure-view closure reaches engine builtin %s in %s; strict pure-view components only", node.Tag, target.path)
			}
			if comp := components[node.Tag]; comp != nil {
				if err := validateSharedPureClosure(sharedComponentTarget{target.prog, comp, target.path}, active); err != nil {
					return err
				}
			} else if alias, name, ok := ir.SplitMemberTag(node.Tag); ok {
				for _, imp := range target.prog.Imports {
					if imp.Alias != alias || !ir.IsSharedImportPath(imp.Path) {
						continue
					}
					targets, err := loadSharedDirComponents(filepath.Join(filepath.Dir(target.path), imp.Path))
					if err != nil {
						return err
					}
					child, ok := targets[name]
					if !ok {
						return fmt.Errorf("shared directory %s declares no strict component named %s", imp.Path, name)
					}
					if err := validateSharedPureClosure(child, active); err != nil {
						return err
					}
				}
			}
		}
		for _, child := range node.Children {
			if err := visit(child); err != nil {
				return err
			}
		}
		names := make([]string, 0, len(node.Slots))
		for name := range node.Slots {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			if err := visit(node.Slots[name]); err != nil {
				return err
			}
		}
		return nil
	}
	return visit(target.comp.Root)
}
