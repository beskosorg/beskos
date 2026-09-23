package wiki

// MarkdownToBlocks converts Markdown (what models naturally produce) into BlockNote blocks —
// headings, lists, checklists, quotes, code blocks and inline marks (bold/italic/code/
// links). This is how the AI writes pages with the SAME components as the editor, instead of flat
// paragraphs. Small, homegrown converter (law 2.5): line by line, no dependencies.

import (
	"encoding/json"
	"regexp"
	"strings"
)

type bnOut = map[string]any

func bnBlockOf(typ string, props map[string]any, content []bnOut) bnOut {
	if props == nil {
		props = map[string]any{}
	}
	return bnOut{"type": typ, "props": props, "content": content, "children": []any{}}
}

// MarkdownToBlocks returns the block array as JSON ready for body_json.
func MarkdownToBlocks(md string) json.RawMessage {
	lines := strings.Split(md, "\n")
	var blocks []bnOut
	var para []string
	flushPara := func() {
		if len(para) == 0 {
			return
		}
		text := strings.TrimSpace(strings.Join(para, " "))
		para = nil
		if text != "" {
			blocks = append(blocks, bnBlockOf("paragraph", nil, inlineMD(text)))
		}
	}
	for i := 0; i < len(lines); i++ {
		line := strings.TrimRight(lines[i], " \t")
		trimmed := strings.TrimSpace(line)
		switch {
		case trimmed == "":
			flushPara()
		case strings.HasPrefix(trimmed, "```"):
			// Code block: accumulates until the closing fence (or the end).
			flushPara()
			var code []string
			for i++; i < len(lines) && !strings.HasPrefix(strings.TrimSpace(lines[i]), "```"); i++ {
				code = append(code, lines[i])
			}
			blocks = append(blocks, bnBlockOf("codeBlock", nil,
				[]bnOut{{"type": "text", "text": strings.Join(code, "\n"), "styles": map[string]any{}}}))
		case strings.HasPrefix(trimmed, "### "):
			flushPara()
			blocks = append(blocks, bnBlockOf("heading", map[string]any{"level": 3}, inlineMD(trimmed[4:])))
		case strings.HasPrefix(trimmed, "## "):
			flushPara()
			blocks = append(blocks, bnBlockOf("heading", map[string]any{"level": 2}, inlineMD(trimmed[3:])))
		case strings.HasPrefix(trimmed, "# "):
			flushPara()
			blocks = append(blocks, bnBlockOf("heading", map[string]any{"level": 1}, inlineMD(trimmed[2:])))
		case strings.HasPrefix(trimmed, "- [ ] "), strings.HasPrefix(trimmed, "- [x] "), strings.HasPrefix(trimmed, "- [X] "):
			flushPara()
			checked := !strings.HasPrefix(trimmed, "- [ ] ")
			blocks = append(blocks, bnBlockOf("checkListItem", map[string]any{"checked": checked}, inlineMD(trimmed[6:])))
		case strings.HasPrefix(trimmed, "- "), strings.HasPrefix(trimmed, "* "):
			flushPara()
			blocks = append(blocks, bnBlockOf("bulletListItem", nil, inlineMD(trimmed[2:])))
		case reOrdered.MatchString(trimmed):
			flushPara()
			blocks = append(blocks, bnBlockOf("numberedListItem", nil, inlineMD(reOrdered.ReplaceAllString(trimmed, ""))))
		case strings.HasPrefix(trimmed, "> "):
			flushPara()
			blocks = append(blocks, bnBlockOf("quote", nil, inlineMD(trimmed[2:])))
		case trimmed == "---" || trimmed == "***":
			flushPara() // BlockNote has no native divider: swallow the marker (it doesn't become text)
		default:
			para = append(para, trimmed)
		}
	}
	flushPara()
	if len(blocks) == 0 {
		blocks = append(blocks, bnBlockOf("paragraph", nil, []bnOut{}))
	}
	out, _ := json.Marshal(blocks)
	return out
}

var (
	reOrdered = regexp.MustCompile(`^\d+[.)] `)
	// One inline token at a time, the leftmost one: code, bold, italic, link.
	reInline = regexp.MustCompile("(`[^`]+`)|(\\*\\*[^*]+\\*\\*)|(\\*[^*]+\\*)|(_[^_]+_)|(\\[[^\\]]+\\]\\([^)]+\\))")
	reLink   = regexp.MustCompile(`^\[([^\]]+)\]\(([^)]+)\)$`)
)

// inlineMD splits the text into BlockNote runs with styles (bold/italic/code) and links.
func inlineMD(s string) []bnOut {
	var out []bnOut
	plain := func(t string) {
		if t != "" {
			out = append(out, bnOut{"type": "text", "text": t, "styles": map[string]any{}})
		}
	}
	for {
		loc := reInline.FindStringIndex(s)
		if loc == nil {
			plain(s)
			break
		}
		plain(s[:loc[0]])
		tok := s[loc[0]:loc[1]]
		switch {
		case strings.HasPrefix(tok, "`"):
			out = append(out, bnOut{"type": "text", "text": strings.Trim(tok, "`"), "styles": map[string]any{"code": true}})
		case strings.HasPrefix(tok, "**"):
			out = append(out, bnOut{"type": "text", "text": strings.Trim(tok, "*"), "styles": map[string]any{"bold": true}})
		case strings.HasPrefix(tok, "*"), strings.HasPrefix(tok, "_"):
			out = append(out, bnOut{"type": "text", "text": strings.Trim(tok, "*_"), "styles": map[string]any{"italic": true}})
		default: // link
			m := reLink.FindStringSubmatch(tok)
			if m != nil {
				out = append(out, bnOut{"type": "link", "href": m[2],
					"content": []bnOut{{"type": "text", "text": m[1], "styles": map[string]any{}}}})
			} else {
				plain(tok)
			}
		}
		s = s[loc[1]:]
	}
	if out == nil {
		out = []bnOut{}
	}
	return out
}
