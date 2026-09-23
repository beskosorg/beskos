// Package office builds Office files (docx, xlsx, pptx) from a plan, in pure Go.
//
// Why by hand and not with a library: an OOXML file is a ZIP with a handful of XML files inside,
// and the stdlib already brings `archive/zip` and `encoding/xml`. A new dependency needs
// justification (law 2.5), and what gets generated here is deliberately simple — text, lists,
// tables and slides. The day rich charts or styles are needed is the day to revisit this, not before.
//
// It also does not depend on OnlyOffice: generation has to work even where editing is disabled
// (decision §12.1, item 2 of the assistant design).
package office

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"strings"
)

// Kind is the type of file to generate.
const (
	KindDocument     = "document"
	KindSpreadsheet  = "spreadsheet"
	KindPresentation = "presentation"
)

// Block is a piece of a text document.
type Block struct {
	// Style: "title" | "heading" | "paragraph" | "bullet"
	Style string `json:"style"`
	Text  string `json:"text"`
}

// Sheet is a spreadsheet. The first row is treated as a header in the presentation, but it
// stays a normal row — no structure gets invented that the user didn't ask for.
type Sheet struct {
	Name string     `json:"name"`
	Rows [][]string `json:"rows"`
}

// Slide is a presentation slide: a title and some bullet points.
type Slide struct {
	Title   string   `json:"title"`
	Bullets []string `json:"bullets"`
	Notes   string   `json:"notes,omitempty"`
}

// Plan is what the model produces and what gets converted into a file. A plan, not a file: the
// model describes the content and it's the server that builds the format — which is what makes
// the result a valid Office file instead of text with a swapped extension.
type Plan struct {
	Kind   string  `json:"kind"`
	Title  string  `json:"title"`
	Blocks []Block `json:"blocks,omitempty"`
	Sheets []Sheet `json:"sheets,omitempty"`
	Slides []Slide `json:"slides,omitempty"`
}

// Ext returns the file extension for this kind.
func Ext(kind string) (string, bool) {
	switch kind {
	case KindDocument:
		return "docx", true
	case KindSpreadsheet:
		return "xlsx", true
	case KindPresentation:
		return "pptx", true
	}
	return "", false
}

// MIME returns the file's content-type.
func MIME(kind string) string {
	switch kind {
	case KindDocument:
		return "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	case KindSpreadsheet:
		return "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	case KindPresentation:
		return "application/vnd.openxmlformats-officedocument.presentationml.presentation"
	}
	return "application/octet-stream"
}

// Build builds the file. Returns an error for an unknown kind instead of picking a default one:
// a presentation plan saved as a document would be worse than a clear refusal.
func Build(p Plan) ([]byte, error) {
	switch p.Kind {
	case KindDocument:
		return buildDocx(p)
	case KindSpreadsheet:
		return buildXlsx(p)
	case KindPresentation:
		return buildPptx(p)
	}
	return nil, fmt.Errorf("office: unknown kind %q", p.Kind)
}

// part is a file inside the ZIP.
type part struct {
	name string
	body string
}

// zipParts writes the parts into a ZIP. The order matters to some older readers, so it's the
// write order — not a map's, which would be random.
func zipParts(parts []part) ([]byte, error) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, p := range parts {
		w, err := zw.Create(p.name)
		if err != nil {
			return nil, fmt.Errorf("office: create %s: %w", p.name, err)
		}
		if _, err := w.Write([]byte(p.body)); err != nil {
			return nil, fmt.Errorf("office: write %s: %w", p.name, err)
		}
	}
	if err := zw.Close(); err != nil {
		return nil, fmt.Errorf("office: close zip: %w", err)
	}
	return buf.Bytes(), nil
}

// esc escapes text for insertion into XML.
//
// It's the only defense separating "user content" from "markup": an e-mail subject with a
// `<` would turn the file into unreadable garbage if it passed through raw. ALL text that
// comes from outside passes through here, with no exception.
func esc(s string) string {
	var b bytes.Buffer
	if err := xml.EscapeText(&b, []byte(sanitize(s))); err != nil {
		return ""
	}
	return b.String()
}

// sanitize discards the control characters that XML 1.0 does not allow. One of them in the
// middle of the text makes the file invalid, and the reader doesn't say why — it just says it's corrupt.
func sanitize(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\t' || r == '\n' || r == '\r':
			return r
		case r < 0x20, r >= 0x7F && r <= 0x9F:
			return -1
		}
		return r
	}, s)
}

const xmlHeader = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + "\n"
