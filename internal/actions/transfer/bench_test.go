package transfer

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/runnelhq/runnel/internal/paths"
)

// BenchmarkTransfer uploads and downloads against the test server on this computer:
// many small files (one connection per execution, a temporary file and a rename each)
// and one large file (throughput of SFTP).
func BenchmarkTransfer(b *testing.B) {
	srv := newTestServer(b)
	project := b.TempDir()
	ctx := paths.WithBase(context.Background(), project)
	for i := range 100 {
		writeFile(b, project, fmt.Sprintf("small/%03d.csv", i), strings.Repeat("x", 10<<10))
	}
	writeFile(b, project, "large/big.bin", strings.Repeat("x", 16<<20))
	writeFile(b, srv.root, "large/big.bin", strings.Repeat("x", 16<<20))
	b.Run("upload-100x10KB", func(b *testing.B) {
		b.SetBytes(100 * 10 << 10)
		b.ReportAllocs()
		for b.Loop() {
			if _, _, err := execute(ctx, b, uploadDefinition, srv.conn(), map[string]any{"source": "small/*.csv", "target": "in/", "ifExists": "replace"}); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("upload-16MB", func(b *testing.B) {
		b.SetBytes(16 << 20)
		b.ReportAllocs()
		for b.Loop() {
			if _, _, err := execute(ctx, b, uploadDefinition, srv.conn(), map[string]any{"source": "large/big.bin", "target": "in/big.bin", "ifExists": "replace"}); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("download-16MB", func(b *testing.B) {
		b.SetBytes(16 << 20)
		b.ReportAllocs()
		for b.Loop() {
			if _, _, err := execute(ctx, b, downloadDefinition, srv.conn(), map[string]any{"source": "large/big.bin", "target": "out/big.bin", "ifExists": "replace"}); err != nil {
				b.Fatal(err)
			}
		}
	})
}
