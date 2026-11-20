package model

import (
	"encoding/binary"
	"strings"
	"testing"
	"unicode/utf16"
)

const encodingWorkflow = "kind: Workflow\napiVersion: runnel/v1\nname: enc\ndescription: |\n  Grüße\n  zweite Zeile\nactions:\n  - { id: start, type: core.start, description: \"Größe\" }\n"

func utf16Bytes(s string, order binary.AppendByteOrder, bom []byte) []byte {
	units := utf16.Encode([]rune(s))
	out := append([]byte{}, bom...)
	for _, u := range units {
		out = order.AppendUint16(out, u)
	}
	return out
}

// Windows editors save UTF-8 with BOM, UTF-16 or CRLF line endings; all must work.
func TestEncodings(t *testing.T) {
	crlf := strings.ReplaceAll(encodingWorkflow, "\n", "\r\n")
	for name, data := range map[string][]byte{
		"utf-8":          []byte(encodingWorkflow),
		"utf-8 with BOM": append([]byte{0xEF, 0xBB, 0xBF}, encodingWorkflow...),
		"utf-16 LE":      utf16Bytes(encodingWorkflow, binary.LittleEndian, []byte{0xFF, 0xFE}),
		"utf-16 BE":      utf16Bytes(encodingWorkflow, binary.BigEndian, []byte{0xFE, 0xFF}),
		"crlf":           []byte(crlf),
		"crlf with BOM":  append([]byte{0xEF, 0xBB, 0xBF}, crlf...),
	} {
		w, err := ParseWorkflow("enc.workflow.yaml", data)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if w.Description != "Grüße\nzweite Zeile\n" || w.Actions[0].Description != "Größe" {
			t.Errorf("%s: description %q / %q", name, w.Description, w.Actions[0].Description)
		}
	}
}

func TestInvalidEncoding(t *testing.T) {
	latin1 := []byte("kind: Workflow\napiVersion: runnel/v1\nname: x\ndescription: Gr\xfc\xdfe\n")
	if _, err := ParseWorkflow("latin1.workflow.yaml", latin1); err == nil || !strings.Contains(err.Error(), "not valid UTF-8") {
		t.Errorf("latin-1: %v", err)
	}
	if _, err := ParseWorkflow("odd.workflow.yaml", []byte{0xFF, 0xFE, 'k'}); err == nil || !strings.Contains(err.Error(), "odd length") {
		t.Errorf("odd utf-16: %v", err)
	}
}
