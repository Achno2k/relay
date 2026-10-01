package cli

import (
	"reflect"
	"strings"
	"testing"

	"relay/internal/awsx"
	"relay/internal/bootstrap"
)

func TestOrderForResetFloatsConfiguredInstance(t *testing.T) {
	insts := []awsx.Instance{
		{ID: "i-zzz", Name: "zulu"},
		{ID: "i-aaa", Name: "alpha"},
		{ID: "i-mmm", Name: "mike"},
	}
	got := orderForReset(insts, "i-mmm")

	var ids []string
	for _, in := range got {
		ids = append(ids, in.ID)
	}
	if want := []string{"i-mmm", "i-aaa", "i-zzz"}; !reflect.DeepEqual(ids, want) {
		t.Errorf("order = %v, want %v", ids, want)
	}
	if len(insts) != 3 || insts[0].ID != "i-zzz" {
		t.Error("orderForReset mutated its input")
	}
}

func TestOrderForResetWithoutAConfiguredInstance(t *testing.T) {
	insts := []awsx.Instance{{ID: "i-zzz", Name: "zulu"}, {ID: "i-aaa", Name: "alpha"}}
	got := orderForReset(insts, "")
	if got[0].ID != "i-aaa" {
		t.Errorf("unconfigured list should sort by name, got %v", got[0])
	}
}

func TestResetInstanceLabels(t *testing.T) {
	insts := []awsx.Instance{
		{ID: "i-mmm", Name: "box", Type: "t3.large", PublicDNS: "ec2-1-2-3-4.compute.amazonaws.com"},
		{ID: "i-aaa", Type: "t3.small"},
	}
	labels := resetInstanceLabels(insts, "i-mmm")

	if !strings.Contains(labels[0], "configured box") {
		t.Errorf("configured instance is not marked: %q", labels[0])
	}
	for _, want := range []string{"box", "i-mmm", "t3.large", "ec2-1-2-3-4.compute.amazonaws.com"} {
		if !strings.Contains(labels[0], want) {
			t.Errorf("label %q is missing %q", labels[0], want)
		}
	}
	if strings.Contains(labels[1], "configured box") {
		t.Errorf("the other instance is marked as configured: %q", labels[1])
	}
	if !strings.Contains(labels[1], "(unnamed)") {
		t.Errorf("an untagged instance should read as unnamed: %q", labels[1])
	}
}

func TestInstanceHostPrefersDNS(t *testing.T) {
	cases := []struct {
		in   awsx.Instance
		want string
	}{
		{awsx.Instance{PublicDNS: "dns", PublicIP: "1.2.3.4"}, "dns"},
		{awsx.Instance{PublicIP: "1.2.3.4"}, "1.2.3.4"},
		{awsx.Instance{ID: "i-aaa"}, ""},
	}
	for _, c := range cases {
		if got := instanceHost(c.in); got != c.want {
			t.Errorf("instanceHost(%+v) = %q, want %q", c.in, got, c.want)
		}
	}
}

// The confirmation word is deliberately exact and case sensitive.
func TestResetConfirmWord(t *testing.T) {
	if resetConfirmWord != "Delete" {
		t.Errorf("confirm word = %q, want Delete", resetConfirmWord)
	}
	for _, near := range []string{"delete", "DELETE", "Deleted", ""} {
		if near == resetConfirmWord {
			t.Errorf("%q should not pass the confirmation", near)
		}
	}
}

// Every reset step must name a function reset.sh actually defines, or the phase
// silently does nothing on the box.
func TestResetPhasesExistInScript(t *testing.T) {
	script := bootstrap.ResetScript("")
	for _, p := range bootstrap.ResetPhases() {
		if !strings.Contains(script, "\n"+p.Func+"()") {
			t.Errorf("reset.sh defines no %s()", p.Func)
		}
		if !strings.HasSuffix(bootstrap.ResetScript(p.Func), "\n"+p.Func+"\n") {
			t.Errorf("ResetScript(%q) does not end with the call", p.Func)
		}
	}
}
