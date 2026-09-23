package office

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"io"
	"strings"
	"testing"
)

// unzip opens the generated file and returns the parts by name.
func unzip(t *testing.T, data []byte) map[string]string {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("the file is not a valid ZIP: %v", err)
	}
	out := map[string]string{}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("open %s: %v", f.Name, err)
		}
		b, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatalf("read %s: %v", f.Name, err)
		}
		out[f.Name] = string(b)
	}
	return out
}

// assertWellFormed proves that each XML part is valid XML. It's the difference between "generated
// bytes" and "generated a file": an Office reader rejects the whole package over one malformed
// part and just says "corrupted," without saying which one.
func assertWellFormed(t *testing.T, parts map[string]string) {
	t.Helper()
	for name, body := range parts {
		if !strings.HasSuffix(name, ".xml") && !strings.HasSuffix(name, ".rels") {
			continue
		}
		dec := xml.NewDecoder(strings.NewReader(body))
		for {
			_, err := dec.Token()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Errorf("%s is not well-formed XML: %v", name, err)
				break
			}
		}
	}
}

// mustHave checks that the required parts are present. A missing one gives "corrupted file" in
// the reader, without saying which — hence they're checked by name, one by one.
func mustHave(t *testing.T, parts map[string]string, names ...string) {
	t.Helper()
	for _, n := range names {
		if _, ok := parts[n]; !ok {
			t.Errorf("missing required part %s", n)
		}
	}
}

func TestDocumentIsAValidPackage(t *testing.T) {
	data, err := Build(Plan{Kind: KindDocument, Title: "Report", Blocks: []Block{
		{Style: "heading", Text: "Summary"},
		{Style: "paragraph", Text: "First line\nSecond line"},
		{Style: "bullet", Text: "One point"},
	}})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	parts := unzip(t, data)
	assertWellFormed(t, parts)
	mustHave(t, parts, "[Content_Types].xml", "_rels/.rels", "word/document.xml")

	doc := parts["word/document.xml"]
	for _, want := range []string{"Report", "Summary", "First line", "Second line", "One point"} {
		if !strings.Contains(doc, want) {
			t.Errorf("the document does not contain %q", want)
		}
	}
	// A line break inside a paragraph has to turn into <w:br/>: without that, three lines
	// would appear stuck together as one.
	if !strings.Contains(doc, "<w:br/>") {
		t.Error("the line break did not turn into <w:br/>")
	}
}

// INVARIANT: all text that comes from outside is escaped. An e-mail subject with a "<" would make
// the file unreadable — and the user would see "corrupted," not "your text had a weird character."
func TestUserTextIsEscapedNotInjected(t *testing.T) {
	nasty := `Ana & Bruno <script>alert("x")</script>`
	data, err := Build(Plan{Kind: KindDocument, Blocks: []Block{{Text: nasty}}})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	parts := unzip(t, data)
	assertWellFormed(t, parts) // if the escaping failed, this would fail here
	doc := parts["word/document.xml"]
	if strings.Contains(doc, "<script>") {
		t.Error("the user's markup went into the XML raw")
	}
	if !strings.Contains(doc, "&amp;") {
		t.Error("the & was not escaped")
	}
}

// Control characters are not allowed in XML 1.0. One in the middle of the text makes the file
// invalid, and the reader doesn't say why.
func TestControlCharactersAreDropped(t *testing.T) {
	data, err := Build(Plan{Kind: KindDocument, Blocks: []Block{{Text: "before\x00\x07after"}}})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	parts := unzip(t, data)
	assertWellFormed(t, parts)
	if !strings.Contains(parts["word/document.xml"], "beforeafter") {
		t.Error("the text was lost while stripping the control characters")
	}
}

func TestSpreadsheetIsAValidPackage(t *testing.T) {
	data, err := Build(Plan{Kind: KindSpreadsheet, Sheets: []Sheet{
		{Name: "Sales", Rows: [][]string{{"Month", "Total"}, {"January", "1500"}, {"February", "2300.5"}}},
		{Name: "Notes", Rows: [][]string{{"a"}}},
	}})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	parts := unzip(t, data)
	assertWellFormed(t, parts)
	mustHave(t, parts, "[Content_Types].xml", "_rels/.rels", "xl/workbook.xml",
		"xl/_rels/workbook.xml.rels", "xl/worksheets/sheet1.xml", "xl/worksheets/sheet2.xml")

	sheet := parts["xl/worksheets/sheet1.xml"]
	// A number goes in as a number, so it can be summed in the sheet.
	if !strings.Contains(sheet, `<c r="B2"><v>1500</v></c>`) {
		t.Errorf("1500 did not go in as a number: %s", sheet)
	}
	// Text goes in as an inline string.
	if !strings.Contains(sheet, `t="inlineStr"`) {
		t.Error("the text did not go in as an inline string")
	}
}

// Invalid sheet names do not error out on save — they produce a file that won't open. That's why
// they get cleaned up, not rejected.
func TestSheetNamesAreSanitised(t *testing.T) {
	if got := sheetName(`Sales/2024*`, 1); got != "Sales-2024-" {
		t.Errorf("sheetName = %q", got)
	}
	if got := sheetName("  ", 3); got != "Sheet3" {
		t.Errorf("empty name = %q, want Sheet3", got)
	}
	if got := sheetName(strings.Repeat("a", 40), 1); len([]rune(got)) != 31 {
		t.Errorf("long name has %d runes, want 31", len([]rune(got)))
	}
}

func TestColumnNames(t *testing.T) {
	cases := map[int]string{0: "A", 25: "Z", 26: "AA", 27: "AB", 51: "AZ", 52: "BA", 701: "ZZ", 702: "AAA"}
	for i, want := range cases {
		if got := colName(i); got != want {
			t.Errorf("colName(%d) = %q, want %q", i, got, want)
		}
	}
}

func TestPresentationIsAValidPackage(t *testing.T) {
	data, err := Build(Plan{Kind: KindPresentation, Title: "Results", Slides: []Slide{
		{Title: "First quarter", Bullets: []string{"Grew 12%", "Two new customers"}},
		{Title: "Second quarter", Bullets: []string{"Stable"}},
	}})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	parts := unzip(t, data)
	assertWellFormed(t, parts)
	// A slide does not open on its own: it needs the layout, the global model, and the theme.
	// Missing one of them gives "corrupted file" without saying which.
	mustHave(t, parts,
		"[Content_Types].xml", "_rels/.rels",
		"ppt/presentation.xml", "ppt/_rels/presentation.xml.rels",
		"ppt/slideMasters/slideMaster1.xml", "ppt/slideMasters/_rels/slideMaster1.xml.rels",
		"ppt/slideLayouts/slideLayout1.xml", "ppt/slideLayouts/_rels/slideLayout1.xml.rels",
		"ppt/theme/theme1.xml",
		"ppt/slides/slide1.xml", "ppt/slides/_rels/slide1.xml.rels",
		"ppt/slides/slide2.xml", "ppt/slides/_rels/slide2.xml.rels")

	if !strings.Contains(parts["ppt/slides/slide1.xml"], "Grew 12%") {
		t.Error("the first slide is missing its content")
	}
	// Every slide has to be declared both in the package and in the presentation's list, or it won't show up.
	for _, want := range []string{`Target="slides/slide1.xml"`, `Target="slides/slide2.xml"`} {
		if !strings.Contains(parts["ppt/_rels/presentation.xml.rels"], want) {
			t.Errorf("the relationship %s is not declared", want)
		}
	}
	if strings.Count(parts["ppt/presentation.xml"], "<p:sldId ") != 2 {
		t.Error("the slide list is missing one of the two slides")
	}
}

// A presentation with no slides still has to open: the cover is made with the title.
func TestEmptyPresentationStillOpens(t *testing.T) {
	data, err := Build(Plan{Kind: KindPresentation, Title: "Cover only"})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	parts := unzip(t, data)
	assertWellFormed(t, parts)
	if !strings.Contains(parts["ppt/slides/slide1.xml"], "Cover only") {
		t.Error("the cover did not carry the title")
	}
}

// An unknown kind is refused, not converted into whichever one comes first: a presentation plan
// saved as a document would be worse than a clear refusal.
func TestUnknownKindIsRefused(t *testing.T) {
	if _, err := Build(Plan{Kind: "pdf"}); err == nil {
		t.Error("Build accepted an unknown kind")
	}
	if _, ok := Ext("pdf"); ok {
		t.Error("Ext accepted an unknown kind")
	}
}

func TestExtAndMIMEAgree(t *testing.T) {
	for _, kind := range []string{KindDocument, KindSpreadsheet, KindPresentation} {
		ext, ok := Ext(kind)
		if !ok || ext == "" {
			t.Errorf("%s has no extension", kind)
		}
		if MIME(kind) == "application/octet-stream" {
			t.Errorf("%s has no content-type of its own", kind)
		}
	}
}

// Text put into a document has to come back out on read: it's what the model reads before editing
// (§5.1). A round trip that lost the content would edit blind.
func TestExtractTextRoundTrip(t *testing.T) {
	data, err := Build(Plan{Kind: KindDocument, Title: "Plan", Blocks: []Block{
		{Style: "heading", Text: "Goals"},
		{Style: "paragraph", Text: "Grow the customer base"},
	}})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	text, err := ExtractText(KindDocument, data)
	if err != nil {
		t.Fatalf("ExtractText: %v", err)
	}
	for _, want := range []string{"Plan", "Goals", "Grow the customer base"} {
		if !strings.Contains(text, want) {
			t.Errorf("the extracted text does not contain %q: %q", want, text)
		}
	}
}

// The presentation too: the slides' bullets are content, and reading has to see them.
func TestExtractTextFromPresentation(t *testing.T) {
	data, _ := Build(Plan{Kind: KindPresentation, Slides: []Slide{
		{Title: "Quarter", Bullets: []string{"Grew 12%"}},
	}})
	text, err := ExtractText(KindPresentation, data)
	if err != nil {
		t.Fatalf("ExtractText: %v", err)
	}
	if !strings.Contains(text, "Grew 12%") || !strings.Contains(text, "Quarter") {
		t.Errorf("slide content was missing: %q", text)
	}
}

// A non-Office kind is not read as text: it returns an error, so editing knows it cannot edit it.
func TestExtractTextRefusesNonOffice(t *testing.T) {
	if _, err := ExtractText("other", []byte("some image")); err == nil {
		t.Error("ExtractText accepted a non-Office kind")
	}
}

// A document.xml that decompresses well past maxDecompressedPart must fail cleanly instead of
// allocating an unbounded string -- the concrete risk a highly-compressible crafted part poses,
// since the outer file size limit at ExtractText's caller does nothing to cap a single member's
// decompression ratio.
func TestExtractTextCapsADecompressionBomb(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.CreateHeader(&zip.FileHeader{Name: "word/document.xml", Method: zip.Deflate})
	if err != nil {
		t.Fatalf("CreateHeader: %v", err)
	}
	if _, err := io.WriteString(w, "<w:document><w:body><w:p><w:r><w:t>"); err != nil {
		t.Fatalf("write: %v", err)
	}
	// Highly repetitive text compresses to almost nothing but decompresses to well over the cap.
	chunk := strings.Repeat("A", 1<<20)
	for i := 0; i < 16; i++ {
		if _, err := io.WriteString(w, chunk); err != nil {
			t.Fatalf("write chunk %d: %v", i, err)
		}
	}
	if _, err := io.WriteString(w, "</w:t></w:r></w:p></w:body></w:document>"); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zw.Close: %v", err)
	}

	if _, err := ExtractText(KindDocument, buf.Bytes()); err == nil {
		t.Error("ExtractText accepted a part that decompresses past the cap")
	}
}

func TestKindFromName(t *testing.T) {
	cases := map[string]string{"a.docx": KindDocument, "b.XLSX": KindSpreadsheet, "c.pptx": KindPresentation}
	for name, want := range cases {
		if got, ok := KindFromName(name); !ok || got != want {
			t.Errorf("KindFromName(%q) = %q,%v", name, got, ok)
		}
	}
	if _, ok := KindFromName("photo.png"); ok {
		t.Error("KindFromName accepted a .png")
	}
}
