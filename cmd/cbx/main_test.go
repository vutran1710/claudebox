package main

import (
	"testing"
)

func TestVersion(t *testing.T) {
	if version != "dev" {
		t.Errorf("default version should be 'dev', got %q", version)
	}
}
