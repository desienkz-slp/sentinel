package policy

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOverlayDeletionSurvivesReloadAndSave(t *testing.T) {
	for _, decision := range []string{"REMOVE", ""} {
		t.Run("decision="+decision, func(t *testing.T) {
			dir := t.TempDir()
			base := writePolicyOverlay(t, dir, "base.yaml", "rules:\n  - id: keep\n    decision: DENY\n  - id: drop\n    decision: ALLOW\n")
			overlay := filepath.Join(dir, "overlay.yaml")
			if err := WriteOverlay(overlay, []Rule{{ID: "drop", Decision: decision}}); err != nil {
				t.Fatal(err)
			}
			for i := 0; i < 2; i++ {
				visible := LoadMerged(base, overlay).Rules()
				if len(visible) != 1 || visible[0].ID != "keep" {
					t.Fatalf("deleted baseline returned: %+v", visible)
				}
				visible[0].Description = "edited after reload"
				if err := WriteOverlay(overlay, visible); err != nil {
					t.Fatal(err)
				}
			}
			// Explicit re-add overrides an old tombstone, including case-insensitive ID matching.
			if err := WriteOverlay(overlay, []Rule{{ID: "DROP", Decision: "DENY"}}); err != nil {
				t.Fatal(err)
			}
			got := LoadMerged(base, overlay).Rules()
			if len(got) != 2 {
				t.Fatalf("explicit re-add failed: %+v", got)
			}
			for _, r := range got {
				if r.ID == "DROP" && r.Decision != "DENY" {
					t.Fatalf("wrong restored rule: %+v", r)
				}
			}
		})
	}
}

func TestOverlaySaveRefusesUnreadablePreviousTombstones(t *testing.T) {
	p := writePolicyOverlay(t, t.TempDir(), "overlay.yaml", "rules: [broken")
	before, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteOverlay(p, []Rule{{ID: "new", Decision: "DENY"}}); err == nil {
		t.Fatal("must not overwrite malformed overlay and lose deletions")
	}
	after, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("malformed overlay was overwritten")
	}
}
