package route

import (
	"fmt"
	"strings"
	"sync"
	"testing"

	"m31labs.dev/gosx/ir"
)

func TestGSXColdCompileConcurrentCallersShareProgram(t *testing.T) {
	var source strings.Builder
	fmt.Fprintf(&source, "package cold\nfunc Page() Node {\n return <div data-test=%q>\n", t.Name())
	for i := 0; i < 1200; i++ {
		fmt.Fprintf(&source, "<p>{%d + 1}</p>\n", i)
	}
	source.WriteString("</div>\n}\n")
	data := []byte(source.String())
	const callers = 8
	programs := make([]*ir.Program, callers)
	errors := make([]error, callers)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range programs {
		wg.Add(1)
		go func(i int) { defer wg.Done(); <-start; programs[i], errors[i] = compileCachedGSX(data) }(i)
	}
	close(start)
	wg.Wait()
	for i := range programs {
		if errors[i] != nil {
			t.Fatal(errors[i])
		}
		if programs[i] != programs[0] {
			t.Fatalf("caller %d received duplicate compiled program", i)
		}
	}
}
