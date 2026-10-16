package cli

import (
	"reflect"
	"testing"
)

// The worker launch tool shares atrium_launch's arguments. Its `item` is the backlog id, a string, never a boolean.
func TestLaunchWorkerItemIsAStringInTheSchema(t *testing.T) {
	f, ok := reflect.TypeOf(LaunchInput{}).FieldByName("Item")
	if !ok {
		t.Fatal("LaunchInput has no Item field")
	}
	if f.Type.Kind() != reflect.String || f.Tag.Get("json") != "item,omitempty" {
		t.Fatalf("item must be a string named item, got %s tagged %q", f.Type, f.Tag.Get("json"))
	}
}
