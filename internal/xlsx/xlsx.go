// Package xlsx writes simple Excel workbooks (one sheet: a title, a header
// row, data rows and a totals row) with only the standard library. Numbers
// stay numbers, money gets a thousands format and dates are real Excel
// dates, so the file can be sorted, filtered and summed in Excel.
package xlsx

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"time"
)

// Column kinds.
const (
	Text    = "text"
	Money   = "money"   // 1,234.56
	Number  = "number"  // 1234 or 12.5
	Date    = "date"    // value "2006-01-02"
	Percent = "percent" // value 16 is shown as 16.00%
)

type Column struct {
	Label string
	Kind  string
	Width float64 // characters; 0 = automatic from the label
}

// Sheet: Rows hold string, float64, int, int64 or nil per column. Totals is
// optional (same shape; nil cells left empty).
type Sheet struct {
	Name     string // tab name (max 31 characters)
	Title    string
	Subtitle string
	Columns  []Column
	Rows     [][]any
	Totals   []any
}

// Style ids (see styles below).
const (
	sDefault = 0
	sHeader  = 1
	sMoney   = 2
	sDate    = 3
	sTotal   = 4 // bold text
	sTotMon  = 5 // bold money
	sTitle   = 6
	sSub     = 7
	sPercent = 8
	sNumber  = 9
)

const styles = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<styleSheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
<numFmts count="3"><numFmt numFmtId="164" formatCode="#,##0.00;[Red]-#,##0.00"/><numFmt numFmtId="165" formatCode="dd-mmm-yyyy"/><numFmt numFmtId="166" formatCode="0.00&quot;%&quot;"/></numFmts>
<fonts count="4"><font><sz val="10"/><name val="Calibri"/></font><font><b/><sz val="10"/><name val="Calibri"/></font><font><b/><sz val="14"/><name val="Calibri"/></font><font><i/><sz val="10"/><color rgb="FF666666"/><name val="Calibri"/></font></fonts>
<fills count="3"><fill><patternFill patternType="none"/></fill><fill><patternFill patternType="gray125"/></fill><fill><patternFill patternType="solid"><fgColor rgb="FFE4E4E7"/><bgColor indexed="64"/></patternFill></fill></fills>
<borders count="2"><border><left/><right/><top/><bottom/><diagonal/></border><border><left/><right/><top style="thin"/><bottom style="double"/><diagonal/></border></borders>
<cellStyleXfs count="1"><xf numFmtId="0" fontId="0" fillId="0" borderId="0"/></cellStyleXfs>
<cellXfs count="10">
<xf numFmtId="0" fontId="0" fillId="0" borderId="0" xfId="0"/>
<xf numFmtId="0" fontId="1" fillId="2" borderId="0" xfId="0" applyFont="1" applyFill="1"><alignment wrapText="1" vertical="center"/></xf>
<xf numFmtId="164" fontId="0" fillId="0" borderId="0" xfId="0" applyNumberFormat="1"/>
<xf numFmtId="165" fontId="0" fillId="0" borderId="0" xfId="0" applyNumberFormat="1"/>
<xf numFmtId="0" fontId="1" fillId="0" borderId="1" xfId="0" applyFont="1" applyBorder="1"/>
<xf numFmtId="164" fontId="1" fillId="0" borderId="1" xfId="0" applyNumberFormat="1" applyFont="1" applyBorder="1"/>
<xf numFmtId="0" fontId="2" fillId="0" borderId="0" xfId="0" applyFont="1"/>
<xf numFmtId="0" fontId="3" fillId="0" borderId="0" xfId="0" applyFont="1"/>
<xf numFmtId="166" fontId="0" fillId="0" borderId="0" xfId="0" applyNumberFormat="1"/>
<xf numFmtId="0" fontId="0" fillId="0" borderId="0" xfId="0"/>
</cellXfs>
<cellStyles count="1"><cellStyle name="Normal" xfId="0" builtinId="0"/></cellStyles>
<dxfs count="0"/><tableStyles count="0" defaultTableStyle="TableStyleMedium2" defaultPivotStyle="PivotStyleLight16"/>
</styleSheet>`

func esc(s string) string {
	var b bytes.Buffer
	for _, r := range s {
		switch {
		case r == '&':
			b.WriteString("&amp;")
		case r == '<':
			b.WriteString("&lt;")
		case r == '>':
			b.WriteString("&gt;")
		case r == '"':
			b.WriteString("&quot;")
		case r == '\t' || r == '\n' || r == '\r':
			b.WriteRune(r)
		case r < 0x20 || r == 0xFFFE || r == 0xFFFF:
			// not allowed in XML: drop
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func colName(i int) string {
	s := ""
	for i++; i > 0; i = (i - 1) / 26 {
		s = string(rune('A'+(i-1)%26)) + s
	}
	return s
}

// excelDate: days since 1899-12-30.
func excelDate(s string) (float64, bool) {
	if len(s) > 10 {
		s = s[:10]
	}
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return 0, false
	}
	return math.Round(t.Sub(time.Date(1899, 12, 30, 0, 0, 0, 0, time.UTC)).Hours() / 24), true
}

func numOf(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, !math.IsNaN(x) && !math.IsInf(x, 0)
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	case uint:
		return float64(x), true
	}
	return 0, false
}

func cell(b *strings.Builder, ref string, v any, kind string, style int) {
	if v == nil {
		if style != sDefault {
			fmt.Fprintf(b, `<c r="%s" s="%d"/>`, ref, style)
		}
		return
	}
	if kind == Date {
		if s, ok := v.(string); ok {
			if d, ok := excelDate(s); ok {
				fmt.Fprintf(b, `<c r="%s" s="%d"><v>%s</v></c>`, ref, style, strconv.FormatFloat(d, 'f', -1, 64))
				return
			}
		}
	}
	if kind != Text {
		if n, ok := numOf(v); ok {
			fmt.Fprintf(b, `<c r="%s" s="%d"><v>%s</v></c>`, ref, style, strconv.FormatFloat(n, 'f', -1, 64))
			return
		}
	}
	fmt.Fprintf(b, `<c r="%s" s="%d" t="inlineStr"><is><t xml:space="preserve">%s</t></is></c>`, ref, style, esc(fmt.Sprint(v)))
}

func dataStyle(kind string, total bool) int {
	switch {
	case kind == Date:
		return sDate
	case total && (kind == Money):
		return sTotMon
	case total:
		return sTotal
	case kind == Money:
		return sMoney
	case kind == Percent:
		return sPercent
	case kind == Number:
		return sNumber
	}
	return sDefault
}

func sheetXML(s *Sheet) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">`)
	const headerRow = 4
	b.WriteString(`<sheetPr><pageSetUpPr fitToPage="1"/></sheetPr>`)
	fmt.Fprintf(&b, `<sheetViews><sheetView workbookViewId="0"><pane ySplit="%d" topLeftCell="A%d" activePane="bottomLeft" state="frozen"/></sheetView></sheetViews>`, headerRow, headerRow+1)
	b.WriteString(`<cols>`)
	for i, c := range s.Columns {
		w := c.Width
		if w == 0 {
			w = math.Max(10, float64(len(c.Label))+2)
			if c.Kind == Money {
				w = math.Max(w, 15)
			}
			if c.Kind == Date {
				w = math.Max(w, 12)
			}
		}
		fmt.Fprintf(&b, `<col min="%d" max="%d" width="%.1f" customWidth="1"/>`, i+1, i+1, w)
	}
	b.WriteString(`</cols><sheetData>`)
	fmt.Fprintf(&b, `<row r="1">`)
	cell(&b, "A1", s.Title, Text, sTitle)
	b.WriteString(`</row><row r="2">`)
	cell(&b, "A2", s.Subtitle, Text, sSub)
	b.WriteString(`</row>`)
	fmt.Fprintf(&b, `<row r="%d" ht="30" customHeight="1">`, headerRow)
	for i, c := range s.Columns {
		cell(&b, fmt.Sprintf("%s%d", colName(i), headerRow), c.Label, Text, sHeader)
	}
	b.WriteString(`</row>`)
	r := headerRow
	for _, row := range s.Rows {
		r++
		fmt.Fprintf(&b, `<row r="%d">`, r)
		for i, c := range s.Columns {
			if i < len(row) {
				cell(&b, fmt.Sprintf("%s%d", colName(i), r), row[i], c.Kind, dataStyle(c.Kind, false))
			}
		}
		b.WriteString(`</row>`)
	}
	if s.Totals != nil {
		r++
		fmt.Fprintf(&b, `<row r="%d">`, r)
		for i, c := range s.Columns {
			var v any
			if i < len(s.Totals) {
				v = s.Totals[i]
			}
			cell(&b, fmt.Sprintf("%s%d", colName(i), r), v, c.Kind, dataStyle(c.Kind, true))
		}
		b.WriteString(`</row>`)
	}
	b.WriteString(`</sheetData>`)
	if len(s.Rows) > 0 {
		fmt.Fprintf(&b, `<autoFilter ref="A%d:%s%d"/>`, headerRow, colName(len(s.Columns)-1), headerRow+len(s.Rows))
	}
	b.WriteString(`<pageMargins left="0.5" right="0.5" top="0.6" bottom="0.6" header="0.3" footer="0.3"/><pageSetup orientation="landscape" fitToWidth="1" fitToHeight="0"/></worksheet>`)
	return b.String()
}

// Write writes the workbook.
func Write(w io.Writer, s *Sheet) error {
	name := s.Name
	if name == "" {
		name = "Report"
	}
	name = strings.NewReplacer(":", "", "\\", "", "/", "", "?", "", "*", "", "[", "", "]", "").Replace(name)
	if r := []rune(name); len(r) > 31 {
		name = string(r[:31])
	}
	files := []struct{ path, body string }{
		{"[Content_Types].xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/><Override PartName="/xl/worksheets/sheet1.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/><Override PartName="/xl/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.styles+xml"/></Types>`},
		{"_rels/.rels", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/></Relationships>`},
		{"xl/workbook.xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets><sheet name="` + esc(name) + `" sheetId="1" r:id="rId1"/></sheets>` +
			definedNames(s, name) + `</workbook>`},
		{"xl/_rels/workbook.xml.rels", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/><Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/></Relationships>`},
		{"xl/styles.xml", styles},
		{"xl/worksheets/sheet1.xml", sheetXML(s)},
	}
	z := zip.NewWriter(w)
	for _, f := range files {
		fw, err := z.Create(f.path)
		if err != nil {
			return err
		}
		if _, err := io.WriteString(fw, f.body); err != nil {
			return err
		}
	}
	return z.Close()
}

// definedNames: the hidden name Excel needs for the header filter buttons.
func definedNames(s *Sheet, name string) string {
	if len(s.Rows) == 0 {
		return ""
	}
	return fmt.Sprintf(`<definedNames><definedName name="_xlnm._FilterDatabase" localSheetId="0" hidden="1">'%s'!$A$4:$%s$%d</definedName></definedNames>`,
		esc(strings.ReplaceAll(name, "'", "''")), colName(len(s.Columns)-1), 4+len(s.Rows))
}
