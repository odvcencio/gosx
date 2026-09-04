package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"

	"m31labs.dev/gosx/internal/uirecipe"
)

func cmdUI() {
	if err := runUICommand(os.Args[2:], os.Stdout); err != nil {
		fatal("ui: %v", err)
	}
}

func uiUsage(w io.Writer) {
	fmt.Fprint(w, `gosx ui - Install source-owned GoSX UI recipes

Usage:
  gosx ui list
  gosx ui add [--root <dir>] [--update] <recipe>
  gosx ui diff [--root <dir>] <recipe>

Commands:
  list  List the local embedded recipe catalog
  add   Add source, or update only files unchanged since installation
  diff  Compare application-owned source with the embedded recipe

The catalog is offline. v1 has no registry, network access, or force mode.

`)
}

func runUICommand(args []string, stdout io.Writer) error {
	if len(args) == 0 || isHelpArg(args[0]) {
		uiUsage(stdout)
		return nil
	}
	catalog, err := uirecipe.Load()
	if err != nil {
		return err
	}
	switch args[0] {
	case "list":
		if len(args) != 1 {
			return fmt.Errorf("usage: gosx ui list")
		}
		writeUIList(stdout, catalog.List())
		return nil
	case "add":
		root, update, recipe, help, err := parseUIRecipeArgs("add", args[1:], true)
		if err != nil {
			return err
		}
		if help {
			uiUsage(stdout)
			return nil
		}
		result, err := catalog.Add(root, recipe, uirecipe.AddOptions{Update: update})
		if err != nil {
			return err
		}
		verb := "added"
		if update {
			verb = "updated"
		}
		fmt.Fprintf(stdout, "%s %s@%s\n", verb, result.Recipe, result.Version)
		for _, file := range result.Files {
			fmt.Fprintf(stdout, "  %-9s %s\n", file.Action, file.Path)
		}
		fmt.Fprintln(stdout, "  manifest  .gosx/ui/manifest.json")
		return nil
	case "diff":
		root, _, recipe, help, err := parseUIRecipeArgs("diff", args[1:], false)
		if err != nil {
			return err
		}
		if help {
			uiUsage(stdout)
			return nil
		}
		result, err := catalog.Diff(root, recipe)
		if err != nil {
			return err
		}
		fmt.Fprintf(stdout, "recipe %s@%s\n", result.Recipe, result.Version)
		for _, entry := range result.Entries {
			fmt.Fprintf(stdout, "  %-9s %s\n", entry.Status, entry.Path)
			if entry.Patch != "" {
				fmt.Fprint(stdout, entry.Patch)
			}
		}
		if result.Clean {
			fmt.Fprintln(stdout, "clean")
			return nil
		}
		return uirecipe.ErrDifferences
	default:
		return fmt.Errorf("unknown ui command %q; run `gosx ui --help`", args[0])
	}
}

func parseUIRecipeArgs(command string, args []string, allowUpdate bool) (root string, update bool, recipe string, help bool, err error) {
	flags := flag.NewFlagSet("gosx ui "+command, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&root, "root", ".", "application module root")
	if allowUpdate {
		flags.BoolVar(&update, "update", false, "update only unmodified installed source")
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return "", false, "", true, nil
		}
		return "", false, "", false, err
	}
	if flags.NArg() != 1 {
		extra := ""
		if allowUpdate {
			extra = " [--update]"
		}
		return "", false, "", false, fmt.Errorf("usage: gosx ui %s [--root <dir>]%s <recipe>", command, extra)
	}
	return root, update, flags.Arg(0), false, nil
}

func writeUIList(w io.Writer, recipes []uirecipe.Summary) {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "RECIPE\tVERSION\tDEPENDS\tDESCRIPTION")
	for _, item := range recipes {
		dependencies := "-"
		if len(item.Dependencies) > 0 {
			dependencies = strings.Join(item.Dependencies, ",")
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", item.Name, item.Version, dependencies, item.Description)
	}
	_ = tw.Flush()
}
