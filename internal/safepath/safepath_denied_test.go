package safepath

import (
	"errors"
	"testing"
)

func TestAnErrorThatIsNeitherMissingNorDeniedStillFails(t *testing.T) {
	boom := func(string) (string, error) { return "", errors.New("boom") }
	if _, err := containedWith(boom, "/r", "x"); err == nil {
		t.Fatal("an unrelated error was swallowed")
	}
}
