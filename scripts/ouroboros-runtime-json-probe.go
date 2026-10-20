//go:build ouroboros_probe

package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"m31labs.dev/gosx/perf/ouroboros"
)

func main() {
	repoRoot := flag.String("repo-root", ".", "GoSX repository root")
	inventoryPath := flag.String("inventory", "", "optional source inventory path")
	artifactRoot := flag.String("artifact-root", "", "O0.2 artifact root")
	outPath := flag.String("out", "", "static corpus JSONL output path")
	generatedAt := flag.String("generated-at", "", "optional RFC3339 timestamp for deterministic output")
	flag.Parse()

	root, err := filepath.Abs(*repoRoot)
	must(err)
	artifacts := *artifactRoot
	if artifacts == "" {
		artifacts = filepath.Join(root, "build", "ouroboros", "o0.2", "runtime-calls")
	}
	out := *outPath
	if out == "" {
		out = filepath.Join(artifacts, "static-sites.jsonl")
	}
	var at time.Time
	if *generatedAt != "" {
		parsed, err := time.Parse(time.RFC3339, *generatedAt)
		must(err)
		at = parsed
	}
	corpus, err := ouroboros.CollectRuntimeJSONStaticCorpus(context.Background(), ouroboros.RuntimeJSONProbeOptions{
		RepoRoot:      root,
		InventoryPath: *inventoryPath,
		ArtifactRoot:  artifacts,
		GeneratedAt:   at,
		Git:           true,
	})
	must(err)
	must(ouroboros.WriteRuntimeJSONStaticCorpusJSONL(out, corpus))
	summary := map[string]any{
		"schemaVersion":             corpus.SchemaVersion,
		"scannerVersion":            corpus.ScannerVersion,
		"phaseClassifierVersion":    corpus.PhaseClassifierVersion,
		"currentSourceIdentityHash": corpus.CurrentSourceIdentityHash,
		"semanticHash":              corpus.SemanticHash,
		"countsHash":                corpus.CountsHash,
		"globalNames":               corpus.GlobalNames,
		"query":                     corpus.Query,
		"counts":                    corpus.Counts,
		"output":                    out,
	}
	body, err := json.MarshalIndent(summary, "", "  ")
	must(err)
	fmt.Println(string(body))
}

func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
