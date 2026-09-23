package assistant

import (
	"slices"
	"testing"
)

func TestCapToolsFiltersToMaxTools(t *testing.T) {
	max := MaxTools()
	if len(max) == 0 {
		t.Fatal("MaxTools() returned nothing; the general agent's tool list changed?")
	}
	got := CapTools([]string{max[0], "not-a-real-tool"})
	if !slices.Contains(got, max[0]) {
		t.Errorf("CapTools dropped an allowed tool: %v", got)
	}
	if slices.Contains(got, "not-a-real-tool") {
		t.Errorf("CapTools kept a tool outside the cap: %v", got)
	}
}

func TestCapToolsEmptyInput(t *testing.T) {
	if got := CapTools(nil); len(got) != 0 {
		t.Errorf("CapTools(nil) = %v, want empty", got)
	}
}

// The ceiling is the general agent's: no client profile can go beyond what the product already allows.
func TestToolCeilingIsTheGeneralAgent(t *testing.T) {
	general, _ := Builtin(AgentGeneral)
	max := MaxTools()
	if len(max) != len(general.Tools) {
		t.Fatalf("ceiling = %v, general agent = %v", max, general.Tools)
	}
	// And it's a COPY: mutating what MaxTools returns can't mutate the catalog.
	max[0] = "tampered"
	if again := MaxTools(); again[0] == "tampered" {
		t.Error("MaxTools returns the internal slice — the catalog became mutable from outside")
	}
}
