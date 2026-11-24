package xlsx

import (
	"archive/zip"
	"bytes"
	"fmt"
	"maps"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/runnelhq/runnel/internal/record"
)

// book builds a workbook in memory from its parts; missing standard parts are added.
func book(t testing.TB, parts map[string]string) []byte {
	t.Helper()
	std := map[string]string{
		"_rels/.rels": `<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/></Relationships>`,
		"xl/workbook.xml": `<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">
<sheets><sheet name="Sheet1" sheetId="1" r:id="rId1"/></sheets></workbook>`,
		"xl/_rels/workbook.xml.rels": `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/>
<Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/sharedStrings" Target="/xl/sharedStrings.xml"/>
<Relationship Id="rId3" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/>
<Relationship Id="rId4" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/hyperlink" Target="https://example.com" TargetMode="External"/></Relationships>`,
		"xl/sharedStrings.xml": `<sst xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><si><t>Name</t></si></sst>`,
		"xl/styles.xml":        `<styleSheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><cellXfs><xf numFmtId="0"/></cellXfs></styleSheet>`,
	}
	maps.Copy(std, parts)
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range std {
		if content == "" {
			continue // removed
		}
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(content))
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func sheet(rows string) string {
	return `<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData>` + rows + `</sheetData></worksheet>`
}

func open(t testing.TB, data []byte) (*Book, error) {
	t.Helper()
	return OpenReader(bytes.NewReader(data), int64(len(data)))
}

// dump writes the rows of all sheets as text.
func dump(t testing.TB, b *Book) string {
	t.Helper()
	var sb strings.Builder
	for _, s := range b.Sheets() {
		fmt.Fprintf(&sb, "[%s hidden=%v]\n", s.Name, s.Hidden)
		r, err := b.Rows(s)
		if err != nil {
			return sb.String() + "error: " + err.Error()
		}
		for r.Next() {
			n, cells := r.Row()
			fmt.Fprintf(&sb, "%d:", n)
			for _, c := range cells {
				v := fmt.Sprintf("%T(%v)", c.Value, c.Value)
				if t, ok := c.Value.(time.Time); ok {
					v = "time(" + t.Format("2006-01-02 15:04:05.000") + ")"
				}
				if c.Error != "" {
					v = "error(" + c.Error + ")"
				}
				fmt.Fprintf(&sb, " %s=%s", ColumnName(c.Col), v)
			}
			sb.WriteString("\n")
		}
		if r.Err() != nil {
			fmt.Fprintf(&sb, "error: %v\n", r.Err())
		}
		_ = r.Close()
	}
	return sb.String()
}

// Workbooks written by LibreOffice (with the values of formulas) and openpyxl (without).
func TestRealFiles(t *testing.T) {
	const want = `[Orders hidden=false]
1: A=string(Nr) B=string(Kunde) C=string(Betrag) D=string(Datum) E=string(Zeit) F=string(Erfasst) G=string(Bezahlt) H=string(Rechnung) I=string(PLZ)
2: A=int64(1) B=string(Müller & Söhne) C=decimal.Decimal(12.5) D=record.Date(2026-11-23) E=string(08:30:00) F=time(2026-11-23 14:05:07.000) G=bool(true) %sI=string(01067)
3: A=int64(2) B=string(Ärger GmbH) C=decimal.Decimal(0.1) D=record.Date(2026-01-02) E=string(17:00:00) F=time(2026-01-02 00:00:00.000) G=bool(false) %sI=string(50667)
4: A=int64(3) B=string(Line
break) C=decimal.Decimal(1234567.891) D=record.Date(1900-01-01) E=string(00:00:01) F=time(2026-03-29 02:30:00.000) %s
6: A=int64(4) B=string(nach Leerzeile) C=int64(7)
[Hidden hidden=true]
3: B=string(x)
[Leer hidden=false]
`
	for file, formulas := range map[string][]any{
		"libreoffice.xlsx": {"H=int64(25) ", "H=error(#DIV/0!) ", "H=decimal.Decimal(0.3)"},
		"openpyxl.xlsx":    {"", "", ""},
	} {
		b, err := Open("testdata/" + file)
		if err != nil {
			t.Fatal(err)
		}
		got := strings.ReplaceAll(dump(t, b), " \n", "\n")
		if w := strings.ReplaceAll(fmt.Sprintf(want, formulas...), " \n", "\n"); got != w {
			t.Errorf("%s:\n%s\nwant\n%s", file, got, w)
		}
		if b.Date1904() {
			t.Errorf("%s: date1904", file)
		}
		if err := b.Close(); err != nil {
			t.Error(err)
		}
	}
}

func TestCells(t *testing.T) {
	data := book(t, map[string]string{
		"xl/sharedStrings.xml": `<sst><si><t>Name</t></si><si><r><t>Fett</t></r><r><rPr><b/></rPr><t xml:space="preserve"> und dünn</t></r><rPh><t>ignored</t></rPh></si><si><t>a_x000D_b _x005F_x0041_</t></si></sst>`,
		"xl/styles.xml": `<styleSheet><numFmts count="2"><numFmt numFmtId="164" formatCode="yyyy\-mm\-dd"/><numFmt numFmtId="165" formatCode="[h]:mm"/></numFmts>
<cellStyleXfs><xf numFmtId="14"/></cellStyleXfs><cellXfs><xf numFmtId="0"/><xf numFmtId="164"/><xf numFmtId="165"/><xf numFmtId="22"/><xf numFmtId="4"/></cellXfs><dxfs><xf numFmtId="14"/></dxfs></styleSheet>`,
		"xl/worksheets/sheet1.xml": sheet(`
<row r="1"><c r="A1" t="s"><v>0</v></c><c r="C1" t="s"><v>1</v></c><c r="D1" t="s"><v>2</v></c></row>
<row r="2" spans="1:9"><c r="A2"><v>1.5E3</v></c><c r="B2" s="1"><v>46349</v></c><c r="C2" s="2"><v>1.0625</v></c><c r="D2" s="3"><v>46349.5</v></c><c r="E2" s="4"><v>-0.30000000000000004</v></c><c r="F2" s="99"><v>3</v></c></row>
<row r="3"><c r="A3" t="inlineStr"><is><t>inline</t></is></c><c r="B3" t="str"><f>A1</f><v>formula</v></c><c r="C3" t="b"><v>0</v></c><c r="D3" t="e"><v>#N/A</v></c><c r="E3" t="d"><v>2026-11-23</v></c><c r="F3" t="d"><v>2026-11-23T08:00:00</v></c><c r="G3"/><c r="H3" s="1"/></row>
<row r="5"><c><v>1</v></c><c><v>2</v></c><c r="E5"><v>3</v></c><c><v>4</v></c></row>
<row><c r="A6"><v>99999999999999999999</v></c><c r="B6"><v>123456789012345678.5</v></c></row>
<row r="7"></row>`),
	})
	b, err := open(t, data)
	if err != nil {
		t.Fatal(err)
	}
	const want = `[Sheet1 hidden=false]
1: A=string(Name) C=string(Fett und dünn) D=string(a` + "\r" + `b _x0041_)
2: A=int64(1500) B=record.Date(2026-11-23) C=string(25:30:00) D=time(2026-11-23 12:00:00.000) E=decimal.Decimal(-0.3) F=int64(3)
3: A=string(inline) B=string(formula) C=bool(false) D=error(#N/A) E=record.Date(2026-11-23) F=time(2026-11-23 08:00:00.000)
5: A=int64(1) B=int64(2) E=int64(3) F=int64(4)
6: A=decimal.Decimal(99999999999999999999) B=decimal.Decimal(123456789012345678.5)
`
	if got := dump(t, b); got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

func TestKindOfCode(t *testing.T) {
	for code, want := range map[string]kind{
		"General": kindNumber, "0.00": kindNumber, "#,##0.00 \"€\"": kindNumber, `0.00E+00`: kindNumber, "@": kindNumber,
		`#,##0 "days"`: kindNumber, "[Red]0.00;[Blue]-0.00": kindNumber, `[$€-407] #,##0.00`: kindNumber,
		"dd.mm.yyyy": kindDate, "d/m/yy": kindDate, "mmmm": kindDate, `yyyy\-mm\-dd`: kindDate, `[$-407]dddd, d. mmmm yyyy`: kindDate,
		"dd.mm.yyyy hh:mm": kindDateTime, "m/d/yy h:mm AM/PM": kindDateTime,
		"hh:mm": kindTime, "[h]:mm:ss": kindTime, "mm:ss": kindTime, `"Time" h`: kindTime,
		"0.00;\"d\"": kindNumber, `\d0`: kindNumber,
	} {
		if got := kindOfCode(code); got != want {
			t.Errorf("%q: %d, want %d", code, got, want)
		}
	}
	for id, want := range map[int]kind{0: kindNumber, 4: kindNumber, 14: kindDate, 22: kindDateTime, 20: kindTime, 47: kindTime, 49: kindNumber, 57: kindDate} {
		if got := builtin(id); got != want {
			t.Errorf("builtin %d: %d, want %d", id, got, want)
		}
	}
}

func TestSerial(t *testing.T) {
	for _, tt := range []struct {
		n     string
		k     kind
		d1904 bool
		want  string
	}{
		{"1", kindDate, false, "1900-01-01"},
		{"59", kindDate, false, "1900-02-28"},
		{"61", kindDate, false, "1900-03-01"},
		{"45658", kindDate, false, "2025-01-01"},
		{"0", kindDate, true, "1904-01-01"},
		{"44196", kindDate, true, "2025-01-01"},
		{"45658.25", kindDate, false, "2025-01-01 06:00:00.000"},
		{"45658.99999999", kindDateTime, false, "2025-01-01 23:59:59.999"},
		{"45658.999999999", kindDateTime, false, "2025-01-02 00:00:00.000"},
		{"0.5", kindTime, false, "12:00:00"},
		{"2958465", kindDate, false, "9999-12-31"},
		{"60", kindDate, false, "error: is the 29th of February 1900"},
		{"-1", kindDate, false, "error: is not a date"},
		{"2958466", kindDate, false, "error: is not a date"},
	} {
		v, err := serial(decimal.RequireFromString(tt.n), tt.k, tt.d1904)
		got := fmt.Sprint(v)
		switch v := v.(type) {
		case time.Time:
			got = v.Format("2006-01-02 15:04:05.000")
		case record.Date:
			got = v.String()
		}
		if err != nil {
			got = "error: " + err.Error()
		}
		if (err != nil && !strings.HasPrefix(tt.want, "error: ")) || !strings.Contains(got, strings.TrimPrefix(tt.want, "error: ")) || (err == nil && got != tt.want) {
			t.Errorf("%s %d %v: %s, want %s", tt.n, tt.k, tt.d1904, got, tt.want)
		}
	}
}

func TestNumber(t *testing.T) {
	for in, want := range map[string]string{
		"0.30000000000000004": "0.3", "3.3000000000000003": "3.3", "1E-3": "0.001", "12": "12", "-7": "-7",
		"123456789012345.67": "123456789012346", "0.000000000000000001": "0.000000000000000001",
		"1000000000000000000": "1000000000000000000",
	} {
		got := fmt.Sprint(number(decimal.RequireFromString(in)))
		if got != want {
			t.Errorf("%s: %s, want %s", in, got, want)
		}
	}
	if _, ok := number(decimal.RequireFromString("1000000000000000000")).(decimal.Decimal); !ok {
		t.Error("10^18 must stay a decimal")
	}
}

func TestCellNames(t *testing.T) {
	for ref, want := range map[string][2]int{"A1": {1, 1}, "$B$12": {2, 12}, "z3": {26, 3}, "AA1": {27, 1}, "XFD1048576": {16384, 1048576}, "C": {3, 0}} {
		col, row, ok := ParseCell(ref)
		if !ok || col != want[0] || row != want[1] {
			t.Errorf("%s: %d %d %v", ref, col, row, ok)
		}
	}
	for _, ref := range []string{"", "1", "XFE1", "A0", "A1048577", "A-1", "A+1", "A1B"} {
		if _, _, ok := ParseCell(ref); ok {
			t.Errorf("%q accepted", ref)
		}
	}
	for col, want := range map[int]string{1: "A", 26: "Z", 27: "AA", 52: "AZ", 703: "AAA", 16384: "XFD"} {
		if got := ColumnName(col); got != want {
			t.Errorf("%d: %s", col, got)
		}
	}
}

// Files that are not workbooks, damaged ones and those that unpack too large are
// rejected with a message for the user.
func TestRejected(t *testing.T) {
	ole := append([]byte{0xD0, 0xCF, 0x11, 0xE0, 0xA1, 0xB1, 0x1A, 0xE1}, make([]byte, 512)...)
	for name, tt := range map[string]struct {
		data []byte
		want string
	}{
		"xls":      {ole, "Excel 97–2003 workbook (.xls) or protected with a password"},
		"text":     {[]byte("Nr;Name\n1;a\n"), "not an Excel workbook (.xlsx)"},
		"empty":    {nil, "not an Excel workbook"},
		"no main":  {book(t, map[string]string{"_rels/.rels": `<Relationships/>`}), "no workbook in it"},
		"xlsb":     {book(t, map[string]string{"_rels/.rels": `<Relationships><Relationship Id="1" Type="x/officeDocument" Target="xl/workbook.bin"/></Relationships>`}), "(.xlsb) are not supported"},
		"no parts": {book(t, map[string]string{"xl/workbook.xml": ""}), "damaged: xl/workbook.xml is missing"},
		"sheets":   {book(t, map[string]string{"xl/workbook.xml": `<workbook><sheets/></workbook>`}), "has no sheets"},
		"xml":      {book(t, map[string]string{"xl/styles.xml": "<styleSheet>\n<cellXfs <"}), "damaged: xl/styles.xml, line 2: "},
	} {
		_, err := open(t, tt.data)
		if err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%s: %v, want %q", name, err, tt.want)
		}
	}

	// Errors in a sheet end reading it.
	for name, tt := range map[string]struct{ rows, want string }{
		"string":    {`<row r="1"><c r="A1" t="s"><v>7</v></c></row>`, `cell A1: the workbook is damaged: no shared text "7"`},
		"number":    {`<row r="1"><c r="A1"><v>1,5</v></c></row>`, `"1,5" is not a number`},
		"bool":      {`<row r="1"><c r="A1" t="b"><v>yes</v></c></row>`, `"yes" is not true or false`},
		"type":      {`<row r="1"><c r="A1" t="x"><v>1</v></c></row>`, `unknown cell type "x"`},
		"date":      {`<row r="1"><c r="A1" t="d"><v>23.11.2026</v></c></row>`, `"23.11.2026" is not a date`},
		"row order": {`<row r="2"><c><v>1</v></c></row><row r="1"><c><v>1</v></c></row>`, `invalid row number "1" after row 2`},
		"row":       {`<row r="1048577"><c><v>1</v></c></row>`, `invalid row number`},
		"cell row":  {`<row r="1"><c r="A2"><v>1</v></c></row>`, `invalid cell "A2" in row 1`},
		"col order": {`<row r="1"><c r="B1"><v>1</v></c><c r="A1"><v>1</v></c></row>`, `invalid cell "A1" in row 1`},
		"columns":   {`<row r="1"><c r="XFD1"><v>1</v></c><c><v>1</v></c></row>`, `invalid cell "" in row 1`},
		"xml":       {"<row r=\"1\">\n<c r=\"A1\"><v>1<</v></c></row>", `sheet "Sheet1": the workbook is damaged: line 2: `},
		"tags":      {`<row r="1"><c r="A1"><v>1</c></row>`, `sheet "Sheet1": the workbook is damaged: the sheet ends in the middle`},
		"cut":       {`<row r="1"><c r="A1"><v>1</v></c></row>`, `sheet "Sheet1": the workbook is damaged: the sheet ends in the middle`},
	} {
		b, err := open(t, book(t, map[string]string{"xl/worksheets/sheet1.xml": strings.TrimSuffix(sheet(tt.rows), map[bool]string{true: "</sheetData></worksheet>"}[name == "cut"])}))
		if err != nil {
			t.Fatal(name, err)
		}
		if got := dump(t, b); !strings.Contains(got, tt.want) {
			t.Errorf("%s: %s, want %q", name, got, tt.want)
		}
	}

	// A sheet without data and parts larger than the limits.
	b, err := open(t, book(t, map[string]string{"xl/_rels/workbook.xml.rels": `<Relationships/>`}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.Rows(b.Sheets()[0]); err == nil || !strings.Contains(err.Error(), `the sheet "Sheet1" has no data`) {
		t.Errorf("no part: %v", err)
	}
	defer func(s, p int64) { maxSheetSize, maxStringsSize = s, p }(maxSheetSize, maxStringsSize)
	maxSheetSize, maxStringsSize = 100, 50
	if _, err := open(t, book(t, nil)); err == nil || !strings.Contains(err.Error(), "xl/sharedStrings.xml is too large (") {
		t.Errorf("strings: %v", err)
	}
	maxStringsSize = 1000
	b, err = open(t, book(t, map[string]string{"xl/worksheets/sheet1.xml": sheet(strings.Repeat(`<row><c><v>1</v></c></row>`, 10))}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.Rows(b.Sheets()[0]); err == nil || !strings.Contains(err.Error(), "xl/worksheets/sheet1.xml is too large (") {
		t.Errorf("sheet: %v", err)
	}
}

// The size in the archive can be wrong: reading stops at the limit anyway.
func TestLimitedReader(t *testing.T) {
	l := &limited{r: strings.NewReader(strings.Repeat("x", 100)), name: "part", limit: 10}
	buf := make([]byte, 64)
	if _, err := l.Read(buf); err == nil || !strings.Contains(err.Error(), "part is too large (more than 10 bytes unpacked)") {
		t.Errorf("%v", err)
	}
}

// Workbooks from old Macs count days from 1904-01-01.
func TestDate1904(t *testing.T) {
	b, err := open(t, book(t, map[string]string{
		"xl/workbook.xml":          `<workbook xmlns:r="r"><workbookPr date1904="true"/><sheets><sheet name="Mac" r:id="rId1" state="veryHidden"/></sheets></workbook>`,
		"xl/styles.xml":            `<styleSheet><cellXfs><xf numFmtId="0"/><xf numFmtId="14"/></cellXfs></styleSheet>`,
		"xl/worksheets/sheet1.xml": sheet(`<row r="1"><c r="A1" s="1"><v>44196</v></c></row>`),
	}))
	if err != nil {
		t.Fatal(err)
	}
	if got := dump(t, b); !b.Date1904() || got != "[Mac hidden=true]\n1: A=record.Date(2025-01-01)\n" {
		t.Errorf("%v %q", b.Date1904(), got)
	}
}
