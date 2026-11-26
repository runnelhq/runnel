package transfer

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/runnelhq/runnel/internal/connections"
	"github.com/runnelhq/runnel/internal/paths"
)

// BenchmarkTransfer uploads and downloads against the test server on this computer:
// many small files (one connection per execution, a temporary file and a rename each)
// and one large file (throughput of SFTP); it deletes and moves many files with a pattern
// (one call on the server per file). The same upload and download of 16 MB over FTPS.
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
	// FTPS: TLS on the control connection and on one data connection per file.
	ftps := newFTPServer(b, connections.TypeFTPS)
	writeFile(b, ftps.root, "large/big.bin", strings.Repeat("x", 16<<20))
	b.Run("ftps-upload-16MB", func(b *testing.B) {
		b.SetBytes(16 << 20)
		b.ReportAllocs()
		for b.Loop() {
			if _, _, err := execute(ctx, b, uploadDefinition, ftps.conn(), map[string]any{"source": "large/big.bin", "target": "in/big.bin", "ifExists": "replace"}); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("ftps-download-16MB", func(b *testing.B) {
		b.SetBytes(16 << 20)
		b.ReportAllocs()
		for b.Loop() {
			if _, _, err := execute(ctx, b, downloadDefinition, ftps.conn(), map[string]any{"source": "large/big.bin", "target": "out/ftps.bin", "ifExists": "replace"}); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("delete-100", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			b.StopTimer()
			for i := range 100 {
				writeFile(b, srv.root, fmt.Sprintf("del/%03d.csv", i), "x")
			}
			b.StartTimer()
			res, _, err := execute(ctx, b, deleteDefinition, srv.conn(), map[string]any{"path": "del/*.csv"})
			if err != nil || len(res.Output.([]any)) != 100 {
				b.Fatal(err)
			}
		}
	})
	b.Run("move-100", func(b *testing.B) {
		for i := range 100 {
			writeFile(b, srv.root, fmt.Sprintf("a/%03d.csv", i), "x")
		}
		from, to := "a", "b"
		b.ReportAllocs()
		for b.Loop() {
			res, _, err := execute(ctx, b, moveDefinition, srv.conn(), map[string]any{"source": from + "/*.csv", "target": to + "/"})
			if err != nil || len(res.Output.([]any)) != 100 {
				b.Fatal(err)
			}
			from, to = to, from
		}
	})
}
