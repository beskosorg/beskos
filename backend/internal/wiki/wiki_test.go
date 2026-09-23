package wiki

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/beskosorg/beskos/backend/internal/db"
)

func TestDeriveBasic(t *testing.T) {
	doc := `{"type":"doc","content":[
		{"type":"heading","attrs":{"level":1},"content":[{"type":"text","text":"Title"}]},
		{"type":"paragraph","content":[
			{"type":"text","text":"hello "},
			{"type":"text","marks":[{"type":"bold"}],"text":"world"}
		]}
	]}`
	text, html := derive([]byte(doc))
	if !strings.Contains(text, "Title") || !strings.Contains(text, "hello world") {
		t.Fatalf("body_text missing content: %q", text)
	}
	if !strings.Contains(html, "<h1>Title</h1>") {
		t.Errorf("expected h1, got %q", html)
	}
	if !strings.Contains(html, "<strong>world</strong>") {
		t.Errorf("expected bold mark, got %q", html)
	}
}

func TestDeriveEscapesAndBlocksScripts(t *testing.T) {
	doc := `{"type":"doc","content":[
		{"type":"paragraph","content":[{"type":"text","text":"<script>alert(1)</script>"}]},
		{"type":"paragraph","content":[
			{"type":"text","marks":[{"type":"link","attrs":{"href":"javascript:alert(1)"}}],"text":"x"}
		]}
	]}`
	_, html := derive([]byte(doc))
	if strings.Contains(html, "<script>") {
		t.Fatalf("script tag was not escaped: %q", html)
	}
	if !strings.Contains(html, "&lt;script&gt;") {
		t.Errorf("expected escaped text, got %q", html)
	}
	if strings.Contains(html, "javascript:") {
		t.Errorf("javascript: URL must be dropped, got %q", html)
	}
}

func TestDeriveMentionAndPageLink(t *testing.T) {
	doc := `{"type":"doc","content":[{"type":"paragraph","content":[
		{"type":"text","text":"hi "},
		{"type":"mention","attrs":{"id":"u1","label":"Ada"}},
		{"type":"text","text":" see "},
		{"type":"pageLink","attrs":{"id":"p1","label":"Onboarding"}}
	]}]}`
	text, html := derive([]byte(doc))
	if !strings.Contains(text, "@Ada") || !strings.Contains(text, "Onboarding") {
		t.Fatalf("body_text missing mention/link: %q", text)
	}
	if !strings.Contains(html, `<span class="wiki-mention">@Ada</span>`) {
		t.Errorf("mention html wrong: %q", html)
	}
	if !strings.Contains(html, `data-page-id="p1"`) || !strings.Contains(html, "Onboarding</span>") {
		t.Errorf("pagelink html wrong: %q", html)
	}
}

func TestDeriveMentionEscapesLabel(t *testing.T) {
	doc := `{"type":"doc","content":[{"type":"paragraph","content":[
		{"type":"mention","attrs":{"id":"x","label":"<script>"}}]}]}`
	_, html := derive([]byte(doc))
	if strings.Contains(html, "<script>") {
		t.Fatalf("mention label not escaped: %q", html)
	}
}

func TestDeriveInvalidJSON(t *testing.T) {
	text, html := derive([]byte("not json"))
	if text != "" || html != "" {
		t.Errorf("invalid JSON should derive empties, got %q / %q", text, html)
	}
}

func TestSafeURL(t *testing.T) {
	cases := map[string]string{
		"https://a.com":    "https://a.com",
		"mailto:a@b.com":   "mailto:a@b.com",
		"/mail/wiki":       "/mail/wiki",
		"javascript:alert": "",
		"data:text/html,x": "",
		"vbscript:x":       "",
	}
	for in, want := range cases {
		if got := safeURL(in, false); got != want {
			t.Errorf("safeURL(%q)=%q want %q", in, got, want)
		}
	}
	if got := safeURL("data:image/png;base64,AAAA", true); got == "" {
		t.Error("data:image should be allowed for images")
	}
}

func TestSlugify(t *testing.T) {
	cases := map[string]string{
		"Hello World":     "hello-world",
		"  Ação & Café  ": "ação-café",
		"multi   space":   "multi-space",
		"---trim---":      "trim",
	}
	for in, want := range cases {
		if got := slugify(in); got != want {
			t.Errorf("slugify(%q)=%q want %q", in, got, want)
		}
	}
}

func TestValidateGrant(t *testing.T) {
	uid := uuid.New()
	// Valid combinations: org with no subject; user/group with a subject.
	if err := validateGrant("org", nil, "viewer"); err != nil {
		t.Errorf("org/viewer should be valid: %v", err)
	}
	if err := validateGrant("user", &uid, "editor"); err != nil {
		t.Errorf("user/editor should be valid: %v", err)
	}
	if err := validateGrant("group", &uid, "manager"); err != nil {
		t.Errorf("group/manager should be valid: %v", err)
	}
	// Invalid combinations.
	if err := validateGrant("org", &uid, "viewer"); err == nil {
		t.Error("org must have no subject")
	}
	if err := validateGrant("user", nil, "viewer"); err == nil {
		t.Error("user needs a subject")
	}
	if err := validateGrant("user", &uid, "boss"); err == nil {
		t.Error("unknown role must be rejected")
	}
	if err := validateGrant("alien", &uid, "viewer"); err == nil {
		t.Error("unknown subject type must be rejected")
	}
}

func TestAuthorize(t *testing.T) {
	if err := authorize(RoleNone, RoleViewer); err != ErrNotFound {
		t.Errorf("no access should 404, got %v", err)
	}
	if err := authorize(RoleViewer, RoleEditor); err != ErrForbidden {
		t.Errorf("insufficient role should 403, got %v", err)
	}
	if err := authorize(RoleManager, RoleEditor); err != nil {
		t.Errorf("manager satisfies editor, got %v", err)
	}
}

func TestBuildTree(t *testing.T) {
	root := uuid.New()
	child := uuid.New()
	pages := []db.WikiPage{
		{ID: root, Title: "Root", Slug: "root"},
		{ID: child, Title: "Child", Slug: "child", ParentID: pgtype.UUID{Bytes: root, Valid: true}},
	}
	tree := buildTree(pages)
	if len(tree) != 1 {
		t.Fatalf("want 1 root, got %d", len(tree))
	}
	if len(tree[0].Children) != 1 || tree[0].Children[0].ID != child {
		t.Fatalf("child not nested under root: %+v", tree[0])
	}
}
