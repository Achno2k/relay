package service

import "testing"

func TestStableTitle(t *testing.T) {
	for in, want := range map[string]string{
		"⠙ Run ls | e2e":                         "Run ls | e2e",
		"[ ! ] Action Required | Run ls | e2e":   "Run ls | e2e",
		"[ . ] Action Required | ⠹ Run ls | e2e": "Run ls | e2e",
		"Run ls | e2e":                           "Run ls | e2e",
		"✳ Native Swift iOS app":                 "✳ Native Swift iOS app",
		"[ ! ] Action Required | ":               "",
	} {
		if got := stableTitle(in); got != want {
			t.Errorf("%q: got %q, want %q", in, got, want)
		}
	}
}
