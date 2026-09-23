package office

import "strings"

// buildDocx builds a minimal but VALID .docx: the content types package, the root relationship, and
// the document. No styles.xml — the styles used are the ones Word resolves by default from the
// names ("Title", "Heading1", "ListParagraph"), so there is no need to declare them for the file
// to open. A file that opens everywhere is worth more than one with our own styles that doesn't open.
func buildDocx(p Plan) ([]byte, error) {
	var body strings.Builder
	if p.Title != "" {
		body.WriteString(docxPara("Title", p.Title))
	}
	for _, b := range p.Blocks {
		switch b.Style {
		case "title":
			body.WriteString(docxPara("Title", b.Text))
		case "heading":
			body.WriteString(docxPara("Heading1", b.Text))
		case "bullet":
			body.WriteString(docxBullet(b.Text))
		default:
			body.WriteString(docxPara("", b.Text))
		}
	}

	doc := xmlHeader +
		`<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">` +
		`<w:body>` + body.String() +
		// The final section sets the page size. Without it, some readers assume unexpected
		// defaults; with it, the page is consistently ISO 216 (210x297mm) everywhere.
		`<w:sectPr><w:pgSz w:w="11906" w:h="16838"/>` +
		`<w:pgMar w:top="1134" w:right="1134" w:bottom="1134" w:left="1134"/></w:sectPr>` +
		`</w:body></w:document>`

	return zipParts([]part{
		{"[Content_Types].xml", xmlHeader +
			`<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
			`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>` +
			`<Default Extension="xml" ContentType="application/xml"/>` +
			`<Override PartName="/word/document.xml" ContentType="` + MIME(KindDocument) + `.main+xml"/>` +
			`</Types>`},
		{"_rels/.rels", xmlHeader +
			`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
			`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/>` +
			`</Relationships>`},
		{"word/document.xml", doc},
	})
}

// docxPara writes a paragraph with a style. Empty text produces an empty paragraph, which is a
// blank line — and a requested blank line is content, not an error.
func docxPara(style, text string) string {
	var b strings.Builder
	b.WriteString(`<w:p>`)
	if style != "" {
		b.WriteString(`<w:pPr><w:pStyle w:val="` + style + `"/></w:pPr>`)
	}
	b.WriteString(docxRuns(text))
	b.WriteString(`</w:p>`)
	return b.String()
}

func docxBullet(text string) string {
	return `<w:p><w:pPr><w:pStyle w:val="ListParagraph"/>` +
		`<w:numPr><w:ilvl w:val="0"/><w:numId w:val="1"/></w:numPr></w:pPr>` +
		docxRuns(text) + `</w:p>`
}

// docxRuns splits the text on line breaks. A \n inside a <w:t> is ignored by Word: without
// this, a three-line paragraph would appear as one, run together.
func docxRuns(text string) string {
	lines := strings.Split(sanitize(text), "\n")
	var b strings.Builder
	for i, line := range lines {
		b.WriteString(`<w:r>`)
		if i > 0 {
			b.WriteString(`<w:br/>`)
		}
		// xml:space="preserve" keeps the leading/trailing spaces, which the reader would
		// otherwise eat — and a quote's indentation would disappear.
		b.WriteString(`<w:t xml:space="preserve">` + esc(line) + `</w:t>`)
		b.WriteString(`</w:r>`)
	}
	return b.String()
}
