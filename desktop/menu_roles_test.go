package desktop

import (
	"errors"
	"testing"
)

func TestMenuRolesValidateAndPreservePlan(t *testing.T) {
	for _, role := range []MenuRole{MenuRoleUndo, MenuRoleRedo, MenuRoleCut, MenuRoleCopy, MenuRolePaste, MenuRoleSelectAll} {
		plan, err := BuildMenuPlan(Menu{Items: []MenuItem{{Label: "Edit", Role: role}}})
		if err != nil || len(plan.Items) != 1 || plan.Items[0].Role != role {
			t.Fatalf("role %q: plan=%+v err=%v", role, plan, err)
		}
	}
	if _, err := BuildMenuPlan(Menu{Items: []MenuItem{{Label: "Unsafe", Role: "arbitrarySelector"}}}); !errors.Is(err, ErrInvalidOptions) {
		t.Fatalf("unknown native role accepted: %v", err)
	}
}
