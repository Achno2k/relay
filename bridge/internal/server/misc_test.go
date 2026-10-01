package server

import "testing"

// Swift: MiscTests.agentNames (the rest of MiscTests lives with herdr, config, qr and transcript).
func TestMisc_AgentNames(t *testing.T) {
	if !ValidName("api-refactor") || ValidName("Api") || ValidName("1abc") {
		t.Fail()
	}
	if ValidName("") || !ValidName("a") || !ValidName("abcdefghijklmnopqrstuvwxyz012345") || ValidName("abcdefghijklmnopqrstuvwxyz0123456") {
		t.Error("length bounds")
	}
}
