package desktop

import (
	"errors"
	"strings"
	"testing"
)

func TestBuildParseInstanceMessage(t *testing.T) {
	payload, err := BuildInstanceMessage("com.example.app",
		[]string{"--flag", "gosx-test://open?id=1"}, `/tmp/work`)
	if err != nil {
		t.Fatalf("build payload: %v", err)
	}
	msg, err := ParseInstanceMessage(payload)
	if err != nil {
		t.Fatalf("parse payload: %v", err)
	}
	if msg.AppID != "com.example.app" {
		t.Fatalf("app id = %q", msg.AppID)
	}
	if msg.WorkingDir != `/tmp/work` {
		t.Fatalf("working dir = %q", msg.WorkingDir)
	}
	if got := strings.Join(msg.Args, " "); got != "--flag gosx-test://open?id=1" {
		t.Fatalf("args = %q", got)
	}
}

func TestInstanceMessageRejectsOversize(t *testing.T) {
	payload := make([]byte, MaxInstanceMessageBytes+1)
	_, err := ParseInstanceMessage(payload)
	if !errors.Is(err, ErrInvalidOptions) {
		t.Fatalf("err = %v, want ErrInvalidOptions", err)
	}
}

func TestBuildProtocolRegistration(t *testing.T) {
	plan, err := BuildProtocolRegistration(ProtocolRegistration{
		Scheme:     "GoSX-Test",
		AppID:      "com.example.gosx",
		AppName:    "GoSX Test",
		Executable: `C:\Apps\gosx-test.exe`,
		Icon:       `C:\Apps\gosx-test.ico`,
		Arguments:  []string{"desktop", "--single-instance"},
	})
	if err != nil {
		t.Fatalf("build protocol: %v", err)
	}
	values := registryValueMap(plan)
	if got := values[`Software\Classes\gosx-test`+"\x00"]; got != "URL:GoSX Test Protocol" {
		t.Fatalf("default value = %q", got)
	}
	if got := values[`Software\Classes\gosx-test`+"\x00URL Protocol"]; got != "" {
		t.Fatalf("URL Protocol = %q", got)
	}
	if got := values[`Software\Classes\gosx-test\DefaultIcon`+"\x00"]; got != `C:\Apps\gosx-test.ico` {
		t.Fatalf("icon = %q", got)
	}
	if got := values[`Software\Classes\gosx-test\shell\open\command`+"\x00"]; got != `"C:\Apps\gosx-test.exe" "desktop" "--single-instance" "%1"` {
		t.Fatalf("command = %q", got)
	}
}

func TestBuildFileAssociation(t *testing.T) {
	plan, err := BuildFileAssociation(FileAssociationRegistration{
		Extension:   "gsx",
		AppID:       "com.example.gosx",
		AppName:     "GoSX Test",
		Description: "GoSX Component",
		Executable:  `C:\Apps\gosx-test.exe`,
	})
	if err != nil {
		t.Fatalf("build association: %v", err)
	}
	values := registryValueMap(plan)
	if got := values[`Software\Classes\.gsx`+"\x00"]; got != "com.example.gosx.gsx" {
		t.Fatalf("extension default = %q", got)
	}
	if got := values[`Software\Classes\.gsx\OpenWithProgids`+"\x00com.example.gosx.gsx"]; got != "" {
		t.Fatalf("open-with progid = %q", got)
	}
	if got := values[`Software\Classes\com.example.gosx.gsx`+"\x00"]; got != "GoSX Component" {
		t.Fatalf("description = %q", got)
	}
	if got := values[`Software\Classes\com.example.gosx.gsx\DefaultIcon`+"\x00"]; got != `C:\Apps\gosx-test.exe` {
		t.Fatalf("icon = %q", got)
	}
	if got := values[`Software\Classes\com.example.gosx.gsx\shell\open\command`+"\x00"]; got != `"C:\Apps\gosx-test.exe" "%1"` {
		t.Fatalf("command = %q", got)
	}
}

func TestRegistrationBuildersRejectInvalidInput(t *testing.T) {
	_, protocolErr := BuildProtocolRegistration(ProtocolRegistration{
		Scheme:     "1bad",
		AppID:      "com.example.gosx",
		Executable: `C:\Apps\gosx-test.exe`,
	})
	if !errors.Is(protocolErr, ErrInvalidOptions) {
		t.Fatalf("protocol err = %v, want ErrInvalidOptions", protocolErr)
	}
	_, fileErr := BuildFileAssociation(FileAssociationRegistration{
		Extension:  "bad/name",
		AppID:      "com.example.gosx",
		Executable: `C:\Apps\gosx-test.exe`,
	})
	if !errors.Is(fileErr, ErrInvalidOptions) {
		t.Fatalf("file err = %v, want ErrInvalidOptions", fileErr)
	}
}

func registryValueMap(plan RegistryPlan) map[string]string {
	out := make(map[string]string, len(plan.Values))
	for _, value := range plan.Values {
		out[value.Key+"\x00"+value.Name] = value.Value
	}
	return out
}
