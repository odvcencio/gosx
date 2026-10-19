package main

import (
	"runtime"
	"sync"
)

// All variants remain scheduled, including after another variant fails. Each
// result keeps its original index so callers preserve their error ordering.
func runRuntimeBuildTasks(tasks []func() error) []error {
	errors := make([]error, len(tasks))
	workers := min(runtime.GOMAXPROCS(0), 5, len(tasks))
	jobs := make(chan int, len(tasks))
	for i := range tasks {
		jobs <- i
	}
	close(jobs)
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				errors[i] = tasks[i]()
			}
		}()
	}
	wg.Wait()
	return errors
}
