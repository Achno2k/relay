package server

import "testing"

// Swift: AuthTests.
func TestAuth_ConstantTimeEqualIsCorrect(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"abc", "abc", true},
		{"abc", "abd", false},
		{"abc", "ab", false},
		{"abc", "abcd", false},
		{"", "a", false},
		{"", "", true},
	}
	for _, c := range cases {
		if got := constantTimeEqual(c.a, c.b); got != c.want {
			t.Errorf("constantTimeEqual(%q, %q) = %v", c.a, c.b, got)
		}
	}
}
