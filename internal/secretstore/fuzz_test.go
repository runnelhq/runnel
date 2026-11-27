package secretstore

import "testing"

// The file of a project is read before any key is used; damaged files are errors, never
// panics.
func FuzzDecodeFile(f *testing.F) {
	f.Add([]byte(`{"version":1,"project":"/p","keyId":"00","secrets":{"a":{"nonce":"AAAAAAAAAAAAAAAA","value":"AAAAAAAAAAAAAAAAAAAAAA==","updated":"2026-11-27T10:00:00Z"}}}`))
	f.Add([]byte(`{"version":1,"project":"/p","secrets":null}`))
	f.Add([]byte(`[]`))
	f.Fuzz(func(t *testing.T, data []byte) {
		file, err := decodeFile(data)
		if err == nil && (file.Secrets == nil || file.Project == "") {
			t.Fatalf("accepted %q", data)
		}
	})
}
