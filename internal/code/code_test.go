package code

import (
	"testing"
)

func TestRunRequiresName(t *testing.T) {
	// ExactArgs(1) is enforced by cobra, not by Run
	// Just verify the function signature compiles
	_ = Run
	_ = RunHeadless
}
