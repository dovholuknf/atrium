package link

import (
	"reflect"
	"testing"
)

// The tool schema is derived from the input struct, so the field's Go type IS the type a caller is told to send. `item`
// is the backlog id. It was once a boolean in the schema and callers that followed it could not launch.
func TestLaunchItemIsAStringInTheSchema(t *testing.T) {
	f, ok := reflect.TypeOf(launchInput{}).FieldByName("Item")
	if !ok {
		t.Fatal("launchInput has no Item field")
	}
	if f.Type.Kind() != reflect.String || f.Tag.Get("json") != "item,omitempty" {
		t.Fatalf("item must be a string named item, got %s tagged %q", f.Type, f.Tag.Get("json"))
	}
}
