package strictcheck

import (
	"context"
	"path/filepath"
	"testing"
)

func formPageFixture(formTag string) string {
	return "package main\n\nfunc Page() Node {\n\treturn " + formTag + "\n}\n"
}

func TestFormActionContractLeavesManagedRegistrationToRouter(t *testing.T) {
	dir := newTestModule(t)
	path := filepath.Join(dir, "page.gsx")
	mustWrite(t, path, "package main\n\nfunc Page() Node {\n\treturn <form method=\"post\" action=\"/gosx/action/save\"></form>\n}\n")
	if err := CheckFile(context.Background(), path); err != nil {
		t.Fatalf("managed action page should remain checkable: %v", err)
	}
}
