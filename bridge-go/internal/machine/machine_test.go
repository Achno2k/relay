package machine

import (
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
