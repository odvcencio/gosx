package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/odvcencio/gosx"
	"github.com/odvcencio/gosx/internal/version"
)

type releaseCheckReport struct {
	Version string              `json:"version"`
	Next    string              `json:"next,omitempty"`
	OK      bool                `json:"ok"`
	Checks  []releaseCheckEntry `json:"checks"`
}

type releaseCheckEntry struct {
	Name     string `json:"name"`
	Status   string `json:"status"`
	Expected string `json:"expected,omitempty"`
	Actual   string `json:"actual,omitempty"`
	Detail   string `json:"detail,omitempty"`
}

func cmdRelease() {
	if err := runReleaseCommand(os.Args[2:], os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "release error: %v\n", err)
		os.Exit(1)
	}
}

func runReleaseCommand(args []string, stdout io.Writer) error {
	if len(args) == 0 || isHelpArg(args[0]) {
		releaseUsage(stdout)
		return nil
	}
	switch args[0] {
	case "check":
		return runReleaseCheckCommand(args[1:], stdout)
	case "help", "-h", "--help":
		releaseUsage(stdout)
		return nil
	default:
		return fmt.Errorf("unknown release subcommand %q\nrun 'gosx release help' for usage", args[0])
	}
}

func runReleaseCheckCommand(args []string, stdout io.Writer) error {
	if len(args) > 0 && isHelpArg(args[0]) {
		releaseCheckUsage(stdout)
		return nil
	}
	fs := flag.NewFlagSet("gosx release check", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	jsonOut := fs.Bool("json", false, "emit JSON")
	next := fs.String("next", "", "next release tag that must already have a changelog section")
	root := fs.String("root", ".", "repository root")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected release check operand %q", fs.Arg(0))
	}
	report := buildReleaseCheckReport(*root, *next)
	if *jsonOut {
		data, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return fmt.Errorf("marshal release report: %w", err)
		}
		if _, err := stdout.Write(append(data, '\n')); err != nil {
			return err
		}
	} else {
		printReleaseCheck(stdout, report)
	}
	if !report.OK {
		return errors.New("release truth check failed")
	}
	return nil
}

func buildReleaseCheckReport(root, next string) releaseCheckReport {
	current := version.Current
	report := releaseCheckReport{
		Version: current,
		Next:    strings.TrimSpace(next),
		OK:      true,
	}
	report.addCheck(checkVersionPackage())
	report.addCheck(checkREADMERelease(filepath.Join(root, "README.md"), current))
	report.addCheck(checkChangelogRelease(filepath.Join(root, "CHANGELOG.md"), current, "changelog.current"))
	if report.Next != "" {
		report.addCheck(checkChangelogRelease(filepath.Join(root, "CHANGELOG.md"), report.Next, "changelog.next"))
	}
	report.addCheck(checkCITag(current))
	return report
}

func (r *releaseCheckReport) addCheck(check releaseCheckEntry) {
	r.Checks = append(r.Checks, check)
	if check.Status == "fail" {
		r.OK = false
	}
}

func checkVersionPackage() releaseCheckEntry {
	expected := strings.TrimPrefix(version.Current, "v")
	if gosx.Version != expected {
		return releaseCheckEntry{
			Name:     "version.package",
			Status:   "fail",
			Expected: expected,
			Actual:   fmt.Sprintf("gosx=%s internal=%s", gosx.Version, version.Number),
			Detail:   "gosx.Version, internal/version.Number, and internal/version.Current must describe the same release",
		}
	}
	if version.Number != expected {
		return releaseCheckEntry{
			Name:     "version.package",
			Status:   "fail",
			Expected: expected,
			Actual:   fmt.Sprintf("gosx=%s internal=%s", gosx.Version, version.Number),
			Detail:   "gosx.Version, internal/version.Number, and internal/version.Current must describe the same release",
		}
	}
	return releaseCheckEntry{Name: "version.package", Status: "pass", Expected: expected, Actual: gosx.Version}
}

func checkREADMERelease(path, current string) releaseCheckEntry {
	data, err := os.ReadFile(path)
	if err != nil {
		return releaseCheckEntry{Name: "readme.current_release", Status: "fail", Expected: current, Detail: err.Error()}
	}
	re := regexp.MustCompile(`(?i)current release(?:\s+is|:)?\s+\*\*(v[0-9]+\.[0-9]+\.[0-9]+)\*\*`)
	matches := re.FindAllStringSubmatch(string(data), -1)
	if len(matches) == 0 {
		return releaseCheckEntry{
			Name:     "readme.current_release",
			Status:   "fail",
			Expected: current,
			Detail:   "README.md must state the current release in bold near the top and status section",
		}
	}
	for _, match := range matches {
		if len(match) > 1 && match[1] != current {
			return releaseCheckEntry{
				Name:     "readme.current_release",
				Status:   "fail",
				Expected: current,
				Actual:   match[1],
				Detail:   "README.md contains a stale current-release statement",
			}
		}
	}
	return releaseCheckEntry{
		Name:     "readme.current_release",
		Status:   "pass",
		Expected: current,
		Actual:   fmt.Sprintf("%d matching statements", len(matches)),
	}
}

func checkChangelogRelease(path, tag, name string) releaseCheckEntry {
	data, err := os.ReadFile(path)
	if err != nil {
		return releaseCheckEntry{Name: name, Status: "fail", Expected: tag, Detail: err.Error()}
	}
	pattern := fmt.Sprintf(`(?m)^##\s+\[?%s\]?(\s|$)`, regexp.QuoteMeta(tag))
	if regexp.MustCompile(pattern).Find(data) == nil {
		return releaseCheckEntry{
			Name:     name,
			Status:   "fail",
			Expected: tag,
			Detail:   "CHANGELOG.md must contain a release section for this tag",
		}
	}
	return releaseCheckEntry{Name: name, Status: "pass", Expected: tag, Actual: tag}
}

func checkCITag(current string) releaseCheckEntry {
	tag := strings.TrimSpace(os.Getenv("GITHUB_REF_NAME"))
	if tag == "" {
		ref := strings.TrimSpace(os.Getenv("GITHUB_REF"))
		tag = strings.TrimPrefix(ref, "refs/tags/")
		if tag == ref {
			tag = ""
		}
	}
	if tag == "" {
		tag = strings.TrimSpace(os.Getenv("CI_COMMIT_TAG"))
	}
	if tag == "" {
		return releaseCheckEntry{Name: "ci.tag", Status: "skip", Expected: current, Detail: "no CI tag environment variable present"}
	}
	if tag != current {
		return releaseCheckEntry{
			Name:     "ci.tag",
			Status:   "fail",
			Expected: current,
			Actual:   tag,
			Detail:   "release tag must match internal/version.Current",
		}
	}
	return releaseCheckEntry{Name: "ci.tag", Status: "pass", Expected: current, Actual: tag}
}

func printReleaseCheck(w io.Writer, report releaseCheckReport) {
	fmt.Fprintf(w, "GoSX release check: %s\n", report.Version)
	if report.Next != "" {
		fmt.Fprintf(w, "Next target: %s\n", report.Next)
	}
	for _, check := range report.Checks {
		line := fmt.Sprintf("  %s: %s", check.Name, check.Status)
		if check.Actual != "" {
			line += fmt.Sprintf(" (%s)", check.Actual)
		}
		fmt.Fprintln(w, line)
		if check.Status != "pass" && check.Detail != "" {
			fmt.Fprintf(w, "    %s\n", check.Detail)
		}
	}
}

func releaseUsage(w io.Writer) {
	fmt.Fprintf(w, `gosx release - Check release metadata consistency

Usage:
  gosx release check [--json] [--next vX.Y.Z] [--root <repo>]

`)
}

func releaseCheckUsage(w io.Writer) {
	fmt.Fprintf(w, `gosx release check - Check release metadata consistency

Usage:
  gosx release check [--json] [--next vX.Y.Z] [--root <repo>]

`)
}
