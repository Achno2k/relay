package machine

import (
	"strings"
	"testing"
)

func TestMachineKind(t *testing.T) {
	if Kind("MacBookPro18,3", false) != "laptop" {
		t.Error("MacBookPro18,3")
	}
	if Kind("Mac14,13", false) != "desktop" {
		t.Error("Mac14,13")
	}
	if Kind("Mac17,2", true) != "laptop" {
		t.Error("Mac17,2 with battery")
	}
}

func TestCurrentIsFilledIn(t *testing.T) {
	m := Current()
	if m.ID == "" || m.Name == "" || m.Model == "" || m.OS == "" || (m.Kind != "laptop" && m.Kind != "desktop") {
		t.Fatalf("%+v", m)
	}
}

func TestOverrides(t *testing.T) {
	real := Current()
	t.Setenv(EnvID, "test-vm")
	t.Setenv(EnvName, "Test VM")
	m := Current()
	if m.ID != "test-vm" || m.Name != "Test VM" || m.OS != real.OS || m.Model != real.Model {
		t.Fatalf("%+v", m)
	}
	if err := CheckOverrides(); err != nil {
		t.Fatal(err)
	}
	t.Setenv(EnvID, "")
	t.Setenv(EnvName, "")
	if m := Current(); m.ID != real.ID || m.Name != real.Name {
		t.Fatalf("empty env should not override: %+v", m)
	}
	for _, bad := range []string{"a/b", "a|b", "has space", strings.Repeat("x", 65)} {
		t.Setenv(EnvID, bad)
		if CheckOverrides() == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestRealIDIsValid(t *testing.T) {
	if id := Current().ID; !ValidID(id) {
		t.Fatalf("%q", id)
	}
}
