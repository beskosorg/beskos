package office

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"strings"
)

// KindFromName infers the file type from the extension. It's the only clue we have without
// opening the file, and it's enough: the three extensions are unambiguous.
func KindFromName(name string) (string, bool) {
	lower := strings.ToLower(name)
	switch {
	case strings.HasSuffix(lower, ".docx"):
		return KindDocument, true
	case strings.HasSuffix(lower, ".xlsx"):
		return KindSpreadsheet, true
	case strings.HasSuffix(lower, ".pptx"):
		return KindPresentation, true
	}
	return "", false
}

// ExtractText returns the readable text of an OOXML file. It's what the model reads before
// editing (§5.1): it does not reconstruct the structure — for that the model rewrites the plan —
// but it shows what's there, so the edit is not made blind.
//
// A file that is not OOXML (image, PDF, binary) returns an error instead of garbage: those
// are not edited, only replaced.
func ExtractText(kind string, data []byte) (string, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", fmt.Errorf("office: not a valid package: %w", err)
	}
	switch kind {
	case KindDocument:
		return textFromParts(zr, []string{"word/document.xml"})
	case KindSpreadsheet:
		return textFromPrefix(zr, "xl/worksheets/")
	case KindPresentation:
		return textFromPrefix(zr, "ppt/slides/slide")
	}
	return "", fmt.Errorf("office: cannot read text from kind %q", kind)
}

// textFromParts joins the text of the <a:t>/<w:t>/<t> elements from specific parts.
func textFromParts(zr *zip.Reader, names []string) (string, error) {
	var b strings.Builder
	for _, name := range names {
		for _, f := range zr.File {
			if f.Name != name {
				continue
			}
			s, err := xmlText(f)
			if err != nil {
				return "", err
			}
			b.WriteString(s)
		}
	}
	return b.String(), nil
}

// textFromPrefix does the same for every part whose name starts with prefix, in name order —
// slides and sheets are numbered, so alphabetical order matches presentation order up to
// slide 9; from 10 on it drifts, but that's enough for a read-only preview.
func textFromPrefix(zr *zip.Reader, prefix string) (string, error) {
	var b strings.Builder
	for _, f := range zr.File {
		if !strings.HasPrefix(f.Name, prefix) || !strings.HasSuffix(f.Name, ".xml") {
			continue
		}
		s, err := xmlText(f)
		if err != nil {
			return "", err
		}
		if s != "" {
			if b.Len() > 0 {
				b.WriteString("\n")
			}
			b.WriteString(s)
		}
	}
	return b.String(), nil
}

// maxDecompressedPart bounds how much decompressed XML a single zip member can produce here.
// The outer file is already capped (20 MiB at ExtractText's one caller), but DEFLATE lets one
// member compress far past that ratio -- a small crafted document.xml could otherwise decompress
// to gigabytes before this function ever returns. A real document's text content is nowhere near
// this size, so the cap only ever bites on an adversarial input.
const maxDecompressedPart = 10 << 20

// xmlText extracts the text from an XML's character nodes, without worrying about tag names:
// OOXML's text elements (<w:t>, <a:t>, <t>) are leaves with the text inside, and picking up
// all the CharData gives the readable body without needing to know the whole schema.
func xmlText(f *zip.File) (string, error) {
	rc, err := f.Open()
	if err != nil {
		return "", err
	}
	defer rc.Close()
	dec := xml.NewDecoder(io.LimitReader(rc, maxDecompressedPart))
	var b strings.Builder
	var inText bool
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("office: read text: %w", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			// Only the content of text elements counts. Without this filter, we would also pick up
			// attribute values and metadata noise.
			if t.Name.Local == "t" {
				inText = true
			}
		case xml.EndElement:
			if t.Name.Local == "t" {
				inText = false
				b.WriteString(" ")
			}
		case xml.CharData:
			if inText {
				b.Write(t)
			}
		}
	}
	return strings.TrimSpace(b.String()), nil
}
