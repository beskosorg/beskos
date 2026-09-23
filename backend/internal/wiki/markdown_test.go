package wiki

import (
	"strings"
	"testing"
)

func TestMarkdownToBlocksStructure(t *testing.T) {
	md := "# Titulo\n\nUm paragrafo com **negrito**, *italico* e `codigo`.\n\n" +
		"## Lista\n- um\n- dois\n1. primeiro\n- [x] feito\n- [ ] por fazer\n> citacao\n\n```\ncode here\n```\n" +
		"Um [link](https://a.com) no fim."
	raw := MarkdownToBlocks(md)
	text, html := derive(raw)
	for _, want := range []string{"Titulo", "negrito", "um", "primeiro", "feito", "citacao", "code here", "link"} {
		if !strings.Contains(text, want) {
			t.Errorf("body_text missing %q: %q", want, text)
		}
	}
	for _, want := range []string{"<h1>Titulo</h1>", "<strong>negrito</strong>", "<em>italico</em>", "<code>codigo</code>",
		"<h2>Lista</h2>", "<ul><li>um</li><li>dois</li></ul>", "<ol><li>primeiro</li></ol>",
		`data-checked="true"`, "<blockquote>citacao</blockquote>", "<pre><code>code here</code></pre>",
		`<a href="https://a.com"`} {
		if !strings.Contains(html, want) {
			t.Errorf("html missing %q in %q", want, html)
		}
	}
}

func TestMarkdownToBlocksEmpty(t *testing.T) {
	raw := MarkdownToBlocks("")
	if string(raw) == "" || string(raw) == "null" {
		t.Fatalf("empty markdown must still yield a valid doc: %s", raw)
	}
}
