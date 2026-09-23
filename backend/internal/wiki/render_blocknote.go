package wiki

// Renderer for the BlockNote format: body_json becomes the BlockNote block ARRAY
// (https://www.blocknotejs.org). derive() dispatches: JSON array → here; {type:"doc"} object →
// legacy ProseMirror renderer (render.go). Same security rule: HTML by construction,
// all text escaped, URLs via whitelist — user markup is never passed through raw.

import (
	"encoding/json"
	"strconv"
	"strings"
)

// bnBlock is a BlockNote block. Content is raw because it varies: an inline array OR a tableContent object.
type bnBlock struct {
	Type     string          `json:"type"`
	Props    map[string]any  `json:"props"`
	Content  json.RawMessage `json:"content"`
	Children []bnBlock       `json:"children"`
}

// bnInline is inline content: text with styles, or a link with children.
type bnInline struct {
	Type    string         `json:"type"`
	Text    string         `json:"text"`
	Styles  map[string]any `json:"styles"`
	Href    string         `json:"href"`
	Content []bnInline     `json:"content"`
	Props   map[string]any `json:"props"` // custom inline content (mention/pageLink): {id,label}
}

type bnTableContent struct {
	Rows []struct {
		Cells []json.RawMessage `json:"cells"`
	} `json:"rows"`
}

// deriveBlockNote returns (body_text, body_html) for a BlockNote document.
func deriveBlockNote(raw []byte) (string, string) {
	var blocks []bnBlock
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return "", ""
	}
	var text, html strings.Builder
	bnRenderText(blocks, &text)
	bnRenderBlocks(blocks, &html)
	return strings.TrimSpace(text.String()), html.String()
}

// ---- text (search/RAG) ----

func bnRenderText(blocks []bnBlock, b *strings.Builder) {
	for i := range blocks {
		blk := &blocks[i]
		if blk.Type == "table" {
			var tc bnTableContent
			if json.Unmarshal(blk.Content, &tc) == nil {
				for _, row := range tc.Rows {
					for _, cell := range row.Cells {
						bnInlineText(bnCellInlines(cell), b)
						b.WriteByte(' ')
					}
					b.WriteByte('\n')
				}
			}
		} else {
			bnInlineText(bnInlines(blk.Content), b)
			b.WriteByte('\n')
		}
		bnRenderText(blk.Children, b)
	}
}

func bnInlineText(inlines []bnInline, b *strings.Builder) {
	for _, in := range inlines {
		switch in.Type {
		case "mention":
			b.WriteString("@" + attrStr(in.Props, "label"))
		case "pageLink":
			b.WriteString(attrStr(in.Props, "label"))
		default:
			if in.Text != "" {
				b.WriteString(in.Text)
			}
			bnInlineText(in.Content, b)
		}
	}
}

// ---- HTML (reading) ----

// bnRenderBlocks groups consecutive list items (<ul>/<ol>/checklist) and emits the rest.
func bnRenderBlocks(blocks []bnBlock, b *strings.Builder) {
	for i := 0; i < len(blocks); {
		t := blocks[i].Type
		if t == "bulletListItem" || t == "numberedListItem" || t == "checkListItem" {
			tag, attr := "ul", ""
			if t == "numberedListItem" {
				tag = "ol"
			}
			if t == "checkListItem" {
				attr = ` class="task-list"`
			}
			b.WriteString("<" + tag + attr + ">")
			for i < len(blocks) && blocks[i].Type == t {
				bnRenderListItem(&blocks[i], b)
				i++
			}
			b.WriteString("</" + tag + ">")
			continue
		}
		bnRenderBlock(&blocks[i], b)
		i++
	}
}

func bnRenderListItem(blk *bnBlock, b *strings.Builder) {
	attr := ""
	if blk.Type == "checkListItem" {
		if v, ok := blk.Props["checked"].(bool); ok && v {
			attr = ` data-checked="true"`
		}
	}
	b.WriteString("<li" + attr + ">")
	bnRenderInlines(bnInlines(blk.Content), b)
	bnRenderBlocks(blk.Children, b)
	b.WriteString("</li>")
}

func bnRenderBlock(blk *bnBlock, b *strings.Builder) {
	switch blk.Type {
	case "paragraph":
		b.WriteString("<p>")
		bnRenderInlines(bnInlines(blk.Content), b)
		b.WriteString("</p>")
	case "heading":
		level := attrInt(blk.Props, "level", 1)
		if level < 1 || level > 3 {
			level = 1
		}
		tag := "h" + strconv.Itoa(level)
		b.WriteString("<" + tag + ">")
		bnRenderInlines(bnInlines(blk.Content), b)
		b.WriteString("</" + tag + ">")
	case "quote":
		b.WriteString("<blockquote>")
		bnRenderInlines(bnInlines(blk.Content), b)
		b.WriteString("</blockquote>")
	case "codeBlock":
		b.WriteString("<pre><code>")
		var t strings.Builder
		bnInlineText(bnInlines(blk.Content), &t)
		b.WriteString(escapeText(t.String()))
		b.WriteString("</code></pre>")
	case "image":
		src := safeURL(attrStr(blk.Props, "url"), true)
		if src != "" {
			b.WriteString(`<img src="` + escapeAttr(src) + `" alt="` + escapeAttr(attrStr(blk.Props, "caption")) + `">`)
		}
	case "table":
		var tc bnTableContent
		if json.Unmarshal(blk.Content, &tc) == nil {
			b.WriteString("<table><tbody>")
			for _, row := range tc.Rows {
				b.WriteString("<tr>")
				for _, cell := range row.Cells {
					b.WriteString("<td>")
					bnRenderInlines(bnCellInlines(cell), b)
					b.WriteString("</td>")
				}
				b.WriteString("</tr>")
			}
			b.WriteString("</tbody></table>")
		}
	default:
		// Unknown type (file/video/…): degrades to the inline text, never emits an unknown tag.
		b.WriteString("<p>")
		bnRenderInlines(bnInlines(blk.Content), b)
		b.WriteString("</p>")
	}
	bnRenderBlocks(blk.Children, b)
}

func bnRenderInlines(inlines []bnInline, b *strings.Builder) {
	for _, in := range inlines {
		switch in.Type {
		case "mention":
			// Custom BlockNote mention: props {id,label}. Text escaped, never user markup.
			b.WriteString(`<span class="wiki-mention">@` + escapeText(attrStr(in.Props, "label")) + `</span>`)
		case "pageLink":
			b.WriteString(`<span class="wiki-pagelink" data-page-id="` + escapeAttr(attrStr(in.Props, "id")) + `">` +
				escapeText(attrStr(in.Props, "label")) + `</span>`)
		case "link":
			href := safeURL(in.Href, false)
			if href == "" {
				bnRenderInlines(in.Content, b)
				continue
			}
			b.WriteString(`<a href="` + escapeAttr(href) + `" rel="noopener nofollow">`)
			bnRenderInlines(in.Content, b)
			b.WriteString("</a>")
		default: // "text" and similar
			bnRenderStyledText(in, b)
		}
	}
}

// bnRenderStyledText wraps the escaped text in the whitelisted style marks.
func bnRenderStyledText(in bnInline, b *strings.Builder) {
	type mark struct{ open, close string }
	var marks []mark
	has := func(k string) bool { v, ok := in.Styles[k].(bool); return ok && v }
	if has("bold") {
		marks = append(marks, mark{"<strong>", "</strong>"})
	}
	if has("italic") {
		marks = append(marks, mark{"<em>", "</em>"})
	}
	if has("underline") {
		marks = append(marks, mark{"<u>", "</u>"})
	}
	if has("strike") {
		marks = append(marks, mark{"<s>", "</s>"})
	}
	if has("code") {
		marks = append(marks, mark{"<code>", "</code>"})
	}
	for _, m := range marks {
		b.WriteString(m.open)
	}
	b.WriteString(escapeText(in.Text))
	for i := len(marks) - 1; i >= 0; i-- {
		b.WriteString(marks[i].close)
	}
}

// ---- parsing helpers ----

// bnInlines reads Content as an inline array (the normal case); missing content/tableContent → nil.
func bnInlines(raw json.RawMessage) []bnInline {
	if len(raw) == 0 || raw[0] != '[' {
		return nil
	}
	var out []bnInline
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil
	}
	return out
}

// bnCellInlines reads a table cell: object {type:tableCell,content:[...]} or an inline array.
func bnCellInlines(raw json.RawMessage) []bnInline {
	if len(raw) > 0 && raw[0] == '{' {
		var cell struct {
			Content []bnInline `json:"content"`
		}
		if err := json.Unmarshal(raw, &cell); err == nil {
			return cell.Content
		}
		return nil
	}
	return bnInlines(raw)
}
