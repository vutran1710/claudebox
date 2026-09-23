package main

import "testing"

func TestCbxDefaultVersionPairsWithThisTool(t *testing.T) {
	cases := []struct{ toolVersion, requested, want string }{
		{"0.9.0", "", "v0.9.0"},       // released tool installs its own release
		{"v0.9.0", "", "v0.9.0"},      // tolerant of either spelling
		{"dev", "", ""},               // a dev build has no release to match
		{"", "", ""},                  // nor does an unstamped one
		{"0.9.0", "v0.7.0", "v0.7.0"}, // an explicit request wins
		{"dev", "v0.7.0", "v0.7.0"},
	}
	for _, c := range cases {
		old := version
		version = c.toolVersion
		got := cbxDefaultVersion(c.requested)
		version = old
		if got != c.want {
			t.Errorf("tool %q + requested %q = %q, want %q", c.toolVersion, c.requested, got, c.want)
		}
	}
}
