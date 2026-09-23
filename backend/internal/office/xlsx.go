package office

import (
	"fmt"
	"strconv"
	"strings"
)

// buildXlsx builds a minimal but valid .xlsx.
//
// Text values go as INLINE STRINGS (`t="inlineStr"`) instead of into a shared string table. The
// shared table saves space when there's a lot of repetition; here we trade that saving for one
// less part in the package and one less place where indexes can get out of
// sync — which is the typical way a generated xlsx comes out corrupt.
func buildXlsx(p Plan) ([]byte, error) {
	sheets := p.Sheets
	if len(sheets) == 0 {
		sheets = []Sheet{{Name: "Sheet1"}}
	}

	parts := []part{}
	var sheetRefs, sheetRels, overrides strings.Builder

	for i, sh := range sheets {
		n := i + 1
		name := sheetName(sh.Name, n)
		path := fmt.Sprintf("xl/worksheets/sheet%d.xml", n)
		rid := fmt.Sprintf("rId%d", n)

		fmt.Fprintf(&sheetRefs, `<sheet name="%s" sheetId="%d" r:id="%s"/>`, esc(name), n, rid)
		fmt.Fprintf(&sheetRels, `<Relationship Id="%s" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet%d.xml"/>`, rid, n)
		fmt.Fprintf(&overrides, `<Override PartName="/%s" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>`, path)
		parts = append(parts, part{path, sheetXML(sh)})
	}

	head := []part{
		{"[Content_Types].xml", xmlHeader +
			`<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
			`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>` +
			`<Default Extension="xml" ContentType="application/xml"/>` +
			`<Override PartName="/xl/workbook.xml" ContentType="` + MIME(KindSpreadsheet) + `.main+xml"/>` +
			overrides.String() +
			`</Types>`},
		{"_rels/.rels", xmlHeader +
			`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
			`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/>` +
			`</Relationships>`},
		{"xl/workbook.xml", xmlHeader +
			`<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" ` +
			`xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">` +
			`<sheets>` + sheetRefs.String() + `</sheets></workbook>`},
		{"xl/_rels/workbook.xml.rels", xmlHeader +
			`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
			sheetRels.String() + `</Relationships>`},
	}
	return zipParts(append(head, parts...))
}

func sheetXML(sh Sheet) string {
	var rows strings.Builder
	for r, row := range sh.Rows {
		fmt.Fprintf(&rows, `<row r="%d">`, r+1)
		for c, val := range row {
			rows.WriteString(cellXML(colName(c)+strconv.Itoa(r+1), val))
		}
		rows.WriteString(`</row>`)
	}
	return xmlHeader +
		`<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">` +
		`<sheetData>` + rows.String() + `</sheetData></worksheet>`
}

// cellXML writes a cell. A value that really is a number goes in as a NUMBER, so it can be
// summed in the sheet; everything else goes in as text. Writing "1.234,00" as a number would give
// a silently wrong value, so only what Go reads unambiguously as a number gets through.
func cellXML(ref, val string) string {
	if val == "" {
		return ""
	}
	if _, err := strconv.ParseFloat(strings.TrimSpace(val), 64); err == nil {
		return fmt.Sprintf(`<c r="%s"><v>%s</v></c>`, ref, esc(strings.TrimSpace(val)))
	}
	return fmt.Sprintf(`<c r="%s" t="inlineStr"><is><t xml:space="preserve">%s</t></is></c>`, ref, esc(val))
}

// colName translates a column index into Excel notation (0 → A, 26 → AA).
func colName(i int) string {
	name := ""
	for i >= 0 {
		name = string(rune('A'+i%26)) + name
		i = i/26 - 1
	}
	return name
}

// sheetName cleans up the sheet name. Excel bans some characters and enforces a 31-char limit —
// an invalid name does not error out on save, it produces a file that won't open.
func sheetName(name string, n int) string {
	name = strings.Map(func(r rune) rune {
		if strings.ContainsRune(`:\/?*[]`, r) {
			return '-'
		}
		return r
	}, sanitize(strings.TrimSpace(name)))
	if name == "" {
		name = fmt.Sprintf("Sheet%d", n)
	}
	if r := []rune(name); len(r) > 31 {
		name = string(r[:31])
	}
	return name
}
