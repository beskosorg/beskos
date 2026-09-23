package wiki

import (
	"encoding/json"
	"strconv"
	"strings"
)

// render.go turns the ProseMirror/TipTap document (body_json, the source of truth) into a plain-text
// projection (body_text, for search/RAG) and a cached HTML render (body_html, for fast reading).
//
// SECURITY: the HTML is built ONLY from a whitelist of node/mark types with all text escaped — no
// user-supplied HTML string is ever passed through. This is safe by construction and needs no external
// sanitizer dependency (law 2.5). Unknown node types degrade to their text children.

// pmNode is one node of the ProseMirror document tree.
type pmNode struct {
	Type    string         `json:"type"`
	Attrs   map[string]any `json:"attrs"`
	Content []pmNode       `json:"content"`
	Marks   []pmMark       `json:"marks"`
	Text    string         `json:"text"`
}

type pmMark struct {
	Type  string         `json:"type"`
	Attrs map[string]any `json:"attrs"`
}

// Derive is derive, exported for callers outside the wiki -- Notes stores the same BlockNote array
// and needs the same plain-text projection for AI extraction and search. One implementation, so the
// two features can never disagree about what a note says.
func Derive(bodyJSON []byte) (text, html string) { return derive(bodyJSON) }

// derive parses body_json and returns (body_text, body_html). Invalid JSON yields empty derivations
// rather than an error — the caller keeps body_json as the source of truth regardless.
// Two formats: block ARRAY = BlockNote (the current one, B2); {type:"doc"} object = legacy
// ProseMirror/TipTap (old pages keep rendering).
func derive(bodyJSON []byte) (string, string) {
	trimmed := strings.TrimSpace(string(bodyJSON))
	if len(trimmed) == 0 {
		return "", ""
	}
	if trimmed[0] == '[' {
		return deriveBlockNote([]byte(trimmed))
	}
	var doc pmNode
	if err := json.Unmarshal(bodyJSON, &doc); err != nil {
		return "", ""
	}
	var text strings.Builder
	var html strings.Builder
	renderText(&doc, &text)
	renderNodes(doc.Content, &html)
	return strings.TrimSpace(text.String()), html.String()
}

// renderText collects a plain-text projection, one block per line.
func renderText(n *pmNode, b *strings.Builder) {
	switch n.Type {
	case "text":
		b.WriteString(n.Text)
		return
	case "mention":
		b.WriteString("@" + nodeLabel(n.Attrs))
		return
	case "pageLink":
		b.WriteString(nodeLabel(n.Attrs))
		return
	}
	for i := range n.Content {
		renderText(&n.Content[i], b)
	}
	switch n.Type {
	case "paragraph", "heading", "listItem", "taskItem", "blockquote", "codeBlock":
		b.WriteString("\n")
	}
}

// renderNodes emits safe HTML for a slice of block nodes.
func renderNodes(nodes []pmNode, b *strings.Builder) {
	for i := range nodes {
		renderNode(&nodes[i], b)
	}
}

func renderNode(n *pmNode, b *strings.Builder) {
	switch n.Type {
	case "paragraph":
		wrap(b, "p", n)
	case "heading":
		level := attrInt(n.Attrs, "level", 1)
		if level < 1 {
			level = 1
		}
		if level > 3 {
			level = 3
		}
		tag := "h" + strconv.Itoa(level)
		wrap(b, tag, n)
	case "bulletList":
		wrap(b, "ul", n)
	case "orderedList":
		wrap(b, "ol", n)
	case "taskList":
		wrapAttr(b, "ul", ` class="task-list"`, n)
	case "listItem", "taskItem":
		wrap(b, "li", n)
	case "blockquote":
		wrap(b, "blockquote", n)
	case "codeBlock":
		b.WriteString("<pre><code>")
		writeInlineText(n, b)
		b.WriteString("</code></pre>")
	case "horizontalRule":
		b.WriteString("<hr>")
	case "hardBreak":
		b.WriteString("<br>")
	case "image":
		src := safeURL(attrStr(n.Attrs, "src"), true)
		if src == "" {
			return
		}
		b.WriteString(`<img src="`)
		b.WriteString(escapeAttr(src))
		b.WriteString(`" alt="`)
		b.WriteString(escapeAttr(attrStr(n.Attrs, "alt")))
		b.WriteString(`">`)
	case "mention":
		// Mention of a person: leaf node with attrs {id,label}. Safe render (escaped text).
		b.WriteString(`<span class="wiki-mention">@`)
		b.WriteString(escapeText(nodeLabel(n.Attrs)))
		b.WriteString(`</span>`)
	case "pageLink":
		// Internal [[page]] link: leaf node with attrs {id,label}. data-page-id lets the UI navigate.
		b.WriteString(`<span class="wiki-pagelink" data-page-id="`)
		b.WriteString(escapeAttr(attrStr(n.Attrs, "id")))
		b.WriteString(`">`)
		b.WriteString(escapeText(nodeLabel(n.Attrs)))
		b.WriteString(`</span>`)
	case "text":
		writeTextWithMarks(n, b)
	default:
		// Unknown block: render its children so text is never silently dropped.
		renderInline(n.Content, b)
	}
}

// nodeLabel returns a mention/page-link's display label (falling back to its id).
func nodeLabel(attrs map[string]any) string {
	if l := attrStr(attrs, "label"); l != "" {
		return l
	}
	return attrStr(attrs, "id")
}

// wrap emits <tag>children</tag> where children are inline+block content.
func wrap(b *strings.Builder, tag string, n *pmNode) { wrapAttr(b, tag, "", n) }

func wrapAttr(b *strings.Builder, tag, attr string, n *pmNode) {
	b.WriteString("<")
	b.WriteString(tag)
	b.WriteString(attr)
	b.WriteString(">")
	renderInline(n.Content, b)
	b.WriteString("</")
	b.WriteString(tag)
	b.WriteString(">")
}

// renderInline walks children that may be text (with marks) or nested blocks (lists inside items).
func renderInline(nodes []pmNode, b *strings.Builder) {
	for i := range nodes {
		n := &nodes[i]
		switch n.Type {
		case "text":
			writeTextWithMarks(n, b)
		case "hardBreak":
			b.WriteString("<br>")
		default:
			renderNode(n, b)
		}
	}
}

// writeInlineText writes only the escaped text of a node (for code blocks — no marks, no tags).
func writeInlineText(n *pmNode, b *strings.Builder) {
	if n.Type == "text" {
		b.WriteString(escapeText(n.Text))
	}
	for i := range n.Content {
		writeInlineText(&n.Content[i], b)
	}
}

// writeTextWithMarks wraps escaped text in the whitelisted mark tags, closing them in reverse order.
func writeTextWithMarks(n *pmNode, b *strings.Builder) {
	var closers []string
	for _, m := range n.Marks {
		switch m.Type {
		case "bold", "strong":
			b.WriteString("<strong>")
			closers = append(closers, "</strong>")
		case "italic", "em":
			b.WriteString("<em>")
			closers = append(closers, "</em>")
		case "code":
			b.WriteString("<code>")
			closers = append(closers, "</code>")
		case "strike":
			b.WriteString("<s>")
			closers = append(closers, "</s>")
		case "underline":
			b.WriteString("<u>")
			closers = append(closers, "</u>")
		case "link":
			href := safeURL(attrStr(m.Attrs, "href"), false)
			if href != "" {
				b.WriteString(`<a href="`)
				b.WriteString(escapeAttr(href))
				b.WriteString(`" rel="noopener nofollow">`)
				closers = append(closers, "</a>")
			}
		}
	}
	b.WriteString(escapeText(n.Text))
	for i := len(closers) - 1; i >= 0; i-- {
		b.WriteString(closers[i])
	}
}

// ---------- helpers ----------

func attrStr(attrs map[string]any, key string) string {
	if attrs == nil {
		return ""
	}
	if v, ok := attrs[key].(string); ok {
		return v
	}
	return ""
}

func attrInt(attrs map[string]any, key string, def int) int {
	if attrs == nil {
		return def
	}
	if v, ok := attrs[key].(float64); ok {
		return int(v)
	}
	return def
}

func escapeText(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")
	return r.Replace(s)
}

func escapeAttr(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&#39;")
	return r.Replace(s)
}

// safeURL allows only benign schemes: http(s), mailto, relative (/, #, ./). Images additionally allow
// data:image/*. Everything else (javascript:, vbscript:, data:text/html, …) returns "".
func safeURL(raw string, isImage bool) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	low := strings.ToLower(s)
	switch {
	case strings.HasPrefix(low, "http://"), strings.HasPrefix(low, "https://"),
		strings.HasPrefix(low, "mailto:"):
		return s
	case strings.HasPrefix(s, "/"), strings.HasPrefix(s, "#"), strings.HasPrefix(s, "./"):
		return s
	case isImage && strings.HasPrefix(low, "data:image/"):
		return s
	}
	// No whitelisted scheme matched. A ":" before the first "/" means an unknown scheme → reject.
	// Otherwise it's a relative reference → allow.
	colon := strings.IndexByte(s, ':')
	slash := strings.IndexByte(s, '/')
	if colon >= 0 && (slash < 0 || colon < slash) {
		return ""
	}
	return s
}
