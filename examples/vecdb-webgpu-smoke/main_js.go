//go:build js && wasm

package main

import (
	"fmt"
	"math/rand"
	"syscall/js"

	"github.com/odvcencio/gosx/vecdb"
)

type smokeResult struct {
	EnabledBefore        bool
	EnabledAfter         bool
	Invalidated          bool
	Top1CPU              string
	Top1GPU              string
	BatchPassed          bool
	UploadedBatchPassed  bool
	BatchTop1CPU         []string
	BatchTop1GPU         []string
	UploadedBatchTop1GPU []string
	Passed               bool
	ResultsCPU           []string
	ResultsGPU           []string
}

func main() {
	js.Global().Set("runVecdbWebGPUSmoke", js.FuncOf(func(this js.Value, args []js.Value) any {
		promiseCtor := js.Global().Get("Promise")
		executor := js.FuncOf(func(this js.Value, args []js.Value) any {
			resolve := args[0]
			reject := args[1]
			go func() {
				result, err := runSmoke()
				if err != nil {
					reject.Invoke(err.Error())
					return
				}
				resolve.Invoke(result.toJS())
			}()
			return nil
		})
		return promiseCtor.New(executor)
	}))
	select {}
}

func runSmoke() (smokeResult, error) {
	const (
		dim   = 384
		bits  = 3
		count = 128
		k     = 10
	)

	idx := vecdb.NewWithSeed(dim, bits, 42)
	rng := rand.New(rand.NewSource(99))
	for i := 0; i < count; i++ {
		idx.Add(fmt.Sprintf("v%03d", i), randomVec(dim, rng))
	}
	query := randomVec(dim, rng)
	pq := idx.PrepareQuery(query)
	queries := [][]float32{
		randomVec(dim, rng),
		randomVec(dim, rng),
		randomVec(dim, rng),
		randomVec(dim, rng),
	}
	batch := idx.AllocPreparedQueries(len(queries))
	idx.PrepareQueriesTo(batch, queries)

	cpuResults := idx.SearchPreparedIntoTrusted(nil, pq, k)
	cpuBatch := idx.SearchPreparedBatchIntoTrusted(nil, batch, k)
	enabledBefore := idx.GPUPreparedSearchEnabled()
	if err := idx.EnableGPUPreparedSearch(); err != nil {
		return smokeResult{}, err
	}
	enabledAfter := idx.GPUPreparedSearchEnabled()
	gpuResults := idx.SearchPreparedIntoTrusted(nil, pq, k)
	gpuBatch := idx.SearchPreparedBatchIntoTrusted(nil, batch, k)
	uploadedBatch, err := idx.UploadPreparedQueries(batch)
	if err != nil {
		return smokeResult{}, err
	}
	defer uploadedBatch.Close()
	uploadedResults, err := idx.SearchUploadedPreparedBatch(uploadedBatch, k)
	if err != nil {
		return smokeResult{}, err
	}

	idx.Add("mutated", randomVec(dim, rng))
	invalidated := !idx.GPUPreparedSearchEnabled()
	batchTop1CPU, batchTop1GPU, batchPassed := batchResultIDs(cpuBatch, gpuBatch)
	_, uploadedBatchTop1GPU, uploadedBatchPassed := batchResultIDs(cpuBatch, uploadedResults)

	result := smokeResult{
		EnabledBefore:        enabledBefore,
		EnabledAfter:         enabledAfter,
		Invalidated:          invalidated,
		Top1CPU:              topID(cpuResults),
		Top1GPU:              topID(gpuResults),
		BatchPassed:          batchPassed,
		UploadedBatchPassed:  uploadedBatchPassed,
		BatchTop1CPU:         batchTop1CPU,
		BatchTop1GPU:         batchTop1GPU,
		UploadedBatchTop1GPU: uploadedBatchTop1GPU,
		Passed:               sameResults(cpuResults, gpuResults) && batchPassed && uploadedBatchPassed && enabledAfter && !enabledBefore && invalidated,
		ResultsCPU:           resultIDs(cpuResults),
		ResultsGPU:           resultIDs(gpuResults),
	}
	return result, nil
}

func (r smokeResult) toJS() js.Value {
	return js.ValueOf(map[string]any{
		"enabledBefore":        r.EnabledBefore,
		"enabledAfter":         r.EnabledAfter,
		"invalidated":          r.Invalidated,
		"top1CPU":              r.Top1CPU,
		"top1GPU":              r.Top1GPU,
		"batchPassed":          r.BatchPassed,
		"uploadedBatchPassed":  r.UploadedBatchPassed,
		"batchTop1CPU":         stringSliceToAny(r.BatchTop1CPU),
		"batchTop1GPU":         stringSliceToAny(r.BatchTop1GPU),
		"uploadedBatchTop1GPU": stringSliceToAny(r.UploadedBatchTop1GPU),
		"passed":               r.Passed,
		"resultsCPU":           stringSliceToAny(r.ResultsCPU),
		"resultsGPU":           stringSliceToAny(r.ResultsGPU),
	})
}

func randomVec(dim int, rng *rand.Rand) []float32 {
	v := make([]float32, dim)
	for i := range v {
		v[i] = rng.Float32()*2 - 1
	}
	return v
}

func topID(results []vecdb.SearchResult) string {
	if len(results) == 0 {
		return ""
	}
	return results[0].ID
}

func resultIDs(results []vecdb.SearchResult) []string {
	ids := make([]string, len(results))
	for i := range results {
		ids[i] = results[i].ID
	}
	return ids
}

func sameResults(a, b []vecdb.SearchResult) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func stringSliceToAny(values []string) []any {
	dst := make([]any, len(values))
	for i := range values {
		dst[i] = values[i]
	}
	return dst
}

func batchResultIDs(cpu, gpu [][]vecdb.SearchResult) ([]string, []string, bool) {
	cpuTop := make([]string, len(cpu))
	gpuTop := make([]string, len(gpu))
	if len(cpu) != len(gpu) {
		return cpuTop, gpuTop, false
	}
	passed := true
	for i := range cpu {
		if len(cpu[i]) != len(gpu[i]) {
			passed = false
			continue
		}
		cpuTop[i] = topID(cpu[i])
		gpuTop[i] = topID(gpu[i])
		if !sameResults(cpu[i], gpu[i]) {
			passed = false
		}
	}
	return cpuTop, gpuTop, passed
}
