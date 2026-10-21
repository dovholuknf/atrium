package safepath

import (
	"errors"
	"testing"
)

func TestAnErrorThatIsNeitherMissingNorDeniedStillFails(t *testing.T) {
	orig := evalSymlinks
	t.Cleanup(func() { evalSymlinks = orig })
	evalSymlinks = func(p string) (string, error) { return "", errors.New("boom") }
	if _, err := Contained("/r", "x"); err == nil {
		t.Fatal("an unrelated error was swallowed")
	}
}
