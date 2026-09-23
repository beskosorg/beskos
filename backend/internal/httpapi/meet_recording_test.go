package httpapi

import "testing"

// A self-hosted Pro license is the only way its catalog's permanent "none" Recording tier can
// ever turn into an actual recording pipeline -- the bug this closes is a Pro key that unlocked
// nothing because nothing ever read the capability it grants.
func TestEffectiveRecordingModeSelfHostedProUnlocksRecording(t *testing.T) {
	if got := effectiveRecordingMode("none", true); got != "offline" {
		t.Errorf("effectiveRecordingMode(none, true) = %q, want offline", got)
	}
}

func TestEffectiveRecordingModeWithoutCapabilityStaysNone(t *testing.T) {
	if got := effectiveRecordingMode("none", false); got != "none" {
		t.Errorf("effectiveRecordingMode(none, false) = %q, want none", got)
	}
}

// A hosted tenant's own plan tier is never downgraded or overridden by the capability check --
// the capability only ever fills in for a plan that grants nothing.
func TestEffectiveRecordingModeHostedPlanUnaffected(t *testing.T) {
	cases := []string{"offline", "offline_priority"}
	for _, plan := range cases {
		if got := effectiveRecordingMode(plan, false); got != plan {
			t.Errorf("effectiveRecordingMode(%q, false) = %q, want unchanged", plan, got)
		}
		if got := effectiveRecordingMode(plan, true); got != plan {
			t.Errorf("effectiveRecordingMode(%q, true) = %q, want unchanged", plan, got)
		}
	}
}
