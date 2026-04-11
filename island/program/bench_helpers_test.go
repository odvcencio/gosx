package program

// benchCounterProgram returns a fresh copy of the reference Counter
// island program used by the benchmarks. Wraps the public CounterProgram
// fixture so bench runs don't share a mutated state across iterations.
func benchCounterProgram() *Program {
	return CounterProgram()
}

// benchFormProgram wraps the form-reference fixture for the same reason.
func benchFormProgram() *Program {
	return FormProgram()
}
