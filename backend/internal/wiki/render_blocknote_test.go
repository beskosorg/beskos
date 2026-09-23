package wiki

import (
	"strings"
	"testing"
)

func TestDeriveBlockNoteBasics(t *testing.T) {
	doc := `[
	 {"type":"heading","props":{"level":1},"content":[{"type":"text","text":"Title","styles":{}}],"children":[]},
	 {"type":"paragraph","props":{},"content":[
	   {"type":"text","text":"hello ","styles":{}},
	   {"type":"text","text":"bold","styles":{"bold":true}},
	   {"type":"link","href":"https://a.com","content":[{"type":"text","text":"site","styles":{}}]}
	 ],"children":[]},
	 {"type":"bulletListItem","props":{},"content":[{"type":"text","text":"um","styles":{}}],"children":[]},
	 {"type":"bulletListItem","props":{},"content":[{"type":"text","text":"dois","styles":{}}],"children":[]},
	 {"type":"checkListItem","props":{"checked":true},"content":[{"type":"text","text":"feito","styles":{}}],"children":[]}
	]`
	text, html := derive([]byte(doc))
	for _, want := range []string{"Title", "hello bold", "um", "dois", "feito"} {
		if !strings.Contains(text, want) {
			t.Errorf("body_text missing %q: %q", want, text)
		}
	}
	for _, want := range []string{"<h1>Title</h1>", "<strong>bold</strong>", `<a href="https://a.com"`,
		"<ul><li>um</li><li>dois</li></ul>", `<ul class="task-list"><li data-checked="true">feito</li></ul>`} {
		if !strings.Contains(html, want) {
			t.Errorf("html missing %q in %q", want, html)
		}
	}
}

func TestDeriveBlockNoteEscapesAndBlocksBadURL(t *testing.T) {
	doc := `[
	 {"type":"paragraph","props":{},"content":[{"type":"text","text":"<script>x</script>","styles":{}}],"children":[]},
	 {"type":"paragraph","props":{},"content":[{"type":"link","href":"javascript:alert(1)","content":[{"type":"text","text":"clique","styles":{}}]}],"children":[]},
	 {"type":"image","props":{"url":"javascript:alert(1)","caption":"x"},"content":null,"children":[]}
	]`
	_, html := derive([]byte(doc))
	if strings.Contains(html, "<script>") || strings.Contains(html, "javascript:") {
		t.Fatalf("unsafe output: %q", html)
	}
	if !strings.Contains(html, "clique") {
		t.Errorf("link text must survive without the bad href: %q", html)
	}
}

func TestDeriveBlockNoteTableAndNesting(t *testing.T) {
	doc := `[
	 {"type":"table","props":{},"content":{"type":"tableContent","rows":[
	   {"cells":[{"type":"tableCell","content":[{"type":"text","text":"c1","styles":{}}]},[{"type":"text","text":"c2","styles":{}}]]}
	 ]},"children":[]},
	 {"type":"bulletListItem","props":{},"content":[{"type":"text","text":"pai","styles":{}}],
	  "children":[{"type":"bulletListItem","props":{},"content":[{"type":"text","text":"filho","styles":{}}],"children":[]}]}
	]`
	text, html := derive([]byte(doc))
	if !strings.Contains(html, "<td>c1</td>") || !strings.Contains(html, "<td>c2</td>") {
		t.Errorf("table cells missing: %q", html)
	}
	if !strings.Contains(html, "<li>pai<ul><li>filho</li></ul></li>") {
		t.Errorf("nested list wrong: %q", html)
	}
	if !strings.Contains(text, "filho") {
		t.Errorf("nested text missing: %q", text)
	}
}

func TestDeriveBlockNoteMentionAndPageLink(t *testing.T) {
	doc := `[{"type":"paragraph","props":{},"content":[
	 {"type":"text","text":"oi ","styles":{}},
	 {"type":"mention","props":{"id":"u1","label":"Ada"}},
	 {"type":"text","text":" ver ","styles":{}},
	 {"type":"pageLink","props":{"id":"p1","label":"Onboarding"}}
	],"children":[]}]`
	text, html := derive([]byte(doc))
	if !strings.Contains(text, "@Ada") || !strings.Contains(text, "Onboarding") {
		t.Fatalf("body_text missing: %q", text)
	}
	if !strings.Contains(html, `<span class="wiki-mention">@Ada</span>`) ||
		!strings.Contains(html, `data-page-id="p1"`) {
		t.Errorf("html wrong: %q", html)
	}
}

func TestDeriveBlockNoteMentionEscapes(t *testing.T) {
	doc := `[{"type":"paragraph","props":{},"content":[
	 {"type":"mention","props":{"id":"x","label":"<script>"}}],"children":[]}]`
	_, html := derive([]byte(doc))
	if strings.Contains(html, "<script>") {
		t.Fatalf("label not escaped: %q", html)
	}
}
