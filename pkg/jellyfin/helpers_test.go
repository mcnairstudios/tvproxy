package jellyfin

import (
	"testing"
)

func TestGroupItemID_ValidUUID(t *testing.T) {
	id := groupItemID("b4eb2510-d378-4434-9932-8777cf606441")
	if len(id) != 32 {
		t.Errorf("expected 32 chars, got %d: %s", len(id), id)
	}
	for _, c := range id {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			t.Errorf("invalid hex char %c in %s", c, id)
		}
	}
}

func TestGroupItemID_HasPrefix(t *testing.T) {
	id := groupItemID("b4eb2510-d378-4434-9932-8777cf606441")
	if !isGroupItemID(id) {
		t.Errorf("expected isGroupItemID=true for %s", id)
	}
}

func TestIsGroupItemID_False(t *testing.T) {
	if isGroupItemID("c0abfc0323314f31b16e231ed09e1c54") {
		t.Error("regular UUID should not be group ID")
	}
	if isGroupItemID("f0000000-0000-0000-0000-000000000001") {
		t.Error("view ID should not be group ID")
	}
}

func TestGroupUUIDRoundTrip(t *testing.T) {
	original := "b4eb2510-d378-4434-9932-8777cf606441"
	itemID := groupItemID(original)
	recovered := groupUUIDFromItemID(itemID)
	stripped := stripDashes(original)[:24]
	recoveredStripped := stripDashes(recovered)[:24]
	if stripped != recoveredStripped {
		t.Errorf("round trip failed: original prefix=%s recovered prefix=%s", stripped, recoveredStripped)
	}
}

func TestJellyfinID_StripsDashes(t *testing.T) {
	got := jellyfinID("c0abfc03-2331-4f31-b16e-231ed09e1c54")
	want := "c0abfc0323314f31b16e231ed09e1c54"
	if got != want {
		t.Errorf("jellyfinID: got %s want %s", got, want)
	}
}

func TestJellyfinID_NoDashes(t *testing.T) {
	id := "c0abfc0323314f31b16e231ed09e1c54"
	got := jellyfinID(id)
	if got != id {
		t.Errorf("jellyfinID should be no-op on dashless: got %s", got)
	}
}

func TestStripDashes(t *testing.T) {
	got := stripDashes("b4eb2510-d378-4434-9932-8777cf606441")
	if len(got) != 32 {
		t.Errorf("expected 32 chars, got %d", len(got))
	}
}

func TestAddDashes(t *testing.T) {
	got := addDashes("b4eb2510d378443499328777cf606441")
	want := "b4eb2510-d378-4434-9932-8777cf606441"
	if got != want {
		t.Errorf("addDashes: got %s want %s", got, want)
	}
}
