//go:build race

package pipeline

// manyRows is smaller with the race detector, which slows the engine down about tenfold.
const manyRows = 1_000_000
