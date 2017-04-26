package rancher

import "testing"

var idTests = []struct {
	id         string
	envID      string
	resourceID string
}{
	{"1a05", "", "1a05"},
	{"1a05/1s234", "1a05", "1s234"},
}

func TestSplitId(t *testing.T) {
	for _, tt := range idTests {
		envID, resourceID := splitID(tt.id)
		if envID != tt.envID || resourceID != tt.resourceID {
			t.Errorf("splitId(%s) => [%s, %s]) want [%s, %s]", tt.id, envID, resourceID, tt.envID, tt.resourceID)
		}
	}
}

var stateTests = []struct {
	state   string
	removed bool
}{
	{"removed", true},
	{"purged", true},
	{"active", false},
}

func TestRemovedState(t *testing.T) {
	for _, tt := range stateTests {
		removed := removed(tt.state)
		if removed != tt.removed {
			t.Errorf("removed(%s) => %t, wants %t", tt.state, removed, tt.removed)
		}
	}
}
