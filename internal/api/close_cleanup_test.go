package api

import (
	"reflect"
	"testing"
)

func TestWithoutCleanupTags(t *testing.T) {
	got := withoutCleanupTags([]string{"link:x", "cleanup:when-done", "cleanup:offered", "pr"})
	if !reflect.DeepEqual(got, []string{"link:x", "pr"}) {
		t.Fatalf("got %v", got)
	}
}
