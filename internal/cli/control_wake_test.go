package cli

import (
	"reflect"
	"strings"
	"testing"
)

// The room's atrium_say no longer says a wake is local only.
func TestTheRoomsWakeParameterIsNotDescribedAsLocalOnly(t *testing.T) {
	f, ok := reflect.TypeOf(SayInput{}).FieldByName("Wake")
	if !ok {
		t.Fatal("SayInput has no Wake")
	}
	if tag := f.Tag.Get("jsonschema"); tag == "" || strings.Contains(tag, "local only") {
		t.Fatalf("jsonschema = %q", tag)
	}
}
