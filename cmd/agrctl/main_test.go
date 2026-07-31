package main

import "testing"

func TestRoute(t *testing.T) {
	cases := map[string]string{
		"claudecode": "claudecode",
		"ctl":        "ctl",
		"bogus":      "unknown",
		"":           "unknown",
	}
	for in, want := range cases {
		if got := route(in); got != want {
			t.Errorf("route(%q) = %q, want %q", in, got, want)
		}
	}
}
