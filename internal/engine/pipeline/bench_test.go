package pipeline

import (
	"context"
	"testing"
)

// BenchmarkRunChain measures the cost per record of a chain of transforms (generate →
// dummy → dummy), without collecting the records.
func BenchmarkRunChain(b *testing.B) {
	p := parse(b, `transforms:
  - { id: gen, type: core.generate-rows, with: { count: 100000, numberField: n } }
  - { id: a, type: core.dummy }
  - { id: z, type: core.dummy }
links:
  - { from: gen, to: a }
  - { from: a, to: z }
`)
	b.ReportAllocs()
	for b.Loop() {
		r, err := Prepare(p, Options{Registry: testRegistry(&collected{})})
		if err != nil {
			b.Fatal(err)
		}
		if res := r.Run(context.Background()); res.Status != Succeeded {
			b.Fatal(res.Err)
		}
	}
}
