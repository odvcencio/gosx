package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestMainHelpProcess(t *testing.T) {
	if os.Getenv("GOSX_HELP_PROCESS") != "1" {
		return
	}
	raw := os.Getenv("GOSX_HELP_ARGS")
	args := []string{}
	if raw != "" {
		args = strings.Split(raw, "\x1f")
	}
	os.Args = append([]string{"gosx"}, args...)
	main()
	os.Exit(0)
}

func TestSubcommandHelpDoesNotTreatHelpAsOperand(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{name: "init", args: []string{"init", "--help"}, want: "gosx init"},
		{name: "compile", args: []string{"compile", "--help"}, want: "gosx compile"},
		{name: "check", args: []string{"check", "--help"}, want: "gosx check"},
		{name: "render", args: []string{"render", "--help"}, want: "gosx render"},
		{name: "build", args: []string{"build", "--help"}, want: "gosx build"},
		{name: "size", args: []string{"size", "--help"}, want: "gosx size"},
		{name: "assets plan", args: []string{"assets", "plan", "--help"}, want: "gosx assets plan"},
		{name: "release", args: []string{"release", "--help"}, want: "gosx release"},
		{name: "release check", args: []string{"release", "check", "--help"}, want: "gosx release check"},
		{name: "scene", args: []string{"scene", "--help"}, want: "gosx scene"},
		{name: "scene certify", args: []string{"scene", "certify", "--help"}, want: "gosx scene certify"},
		{name: "scene inspect", args: []string{"scene", "inspect", "--help"}, want: "gosx scene inspect"},
		{name: "scene schema", args: []string{"scene", "schema", "--help"}, want: "gosx scene schema"},
		{name: "scene validate", args: []string{"scene", "validate", "--help"}, want: "gosx scene validate"},
		{name: "help init", args: []string{"help", "init"}, want: "gosx init"},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			stdout, stderr, err := runGosxHelpCommand(t, dir, tt.args...)
			if err != nil {
				t.Fatalf("gosx %s failed: %v\nstdout:\n%s\nstderr:\n%s", strings.Join(tt.args, " "), err, stdout, stderr)
			}
			if !strings.Contains(stdout, tt.want) && !strings.Contains(stderr, tt.want) {
				t.Fatalf("expected help output to contain %q\nstdout:\n%s\nstderr:\n%s", tt.want, stdout, stderr)
			}
			if _, err := os.Stat(filepath.Join(dir, "--help")); !os.IsNotExist(err) {
				t.Fatalf("help was treated as a scaffold target, stat err=%v", err)
			}
		})
	}
}

func runGosxHelpCommand(t *testing.T, dir string, args ...string) (string, string, error) {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=TestMainHelpProcess")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GOSX_HELP_PROCESS=1",
		"GOSX_HELP_ARGS="+strings.Join(args, "\x1f"),
	)
	out, err := cmd.Output()
	if err == nil {
		return string(out), "", nil
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		return string(out), string(exitErr.Stderr), err
	}
	return string(out), "", err
}
