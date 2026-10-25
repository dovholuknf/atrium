package land

import (
	"strings"
	"testing"
)

func TestCheckEmailAcceptsOnlyTheNoreplyAddress(t *testing.T) {
	if err := CheckEmail(AuthorEmail, ""); err != nil {
		t.Fatal(err)
	}
	err := CheckEmail("someone@example.com", "file:/repo/.git/config\tsomeone@example.com")
	if err == nil || !strings.Contains(err.Error(), "/repo/.git/config") {
		t.Fatalf("want a refusal naming the config file, got %v", err)
	}
	if err := CheckEmail("", ""); err == nil {
		t.Fatal("an empty address must be refused")
	}
}
