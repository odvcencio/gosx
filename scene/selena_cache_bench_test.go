package scene

import "testing"

func BenchmarkCompileSelenaMaterialRepeated(b *testing.B) {
	source := []byte(selenaDefaultsSource)
	opts := SelenaMaterialOptions{}
	if _, _, err := CompileSelenaMaterial(source, opts); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, _, err := CompileSelenaMaterial(source, opts); err != nil {
			b.Fatal(err)
		}
	}
}
