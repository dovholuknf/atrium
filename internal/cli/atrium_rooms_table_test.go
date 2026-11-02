package cli

import (
	"reflect"
	"testing"

	"github.com/dovholuknf/atrium/internal/link"
)

func TestRoomRowOfflineRoom(t *testing.T) {
	got := roomRow(link.Known{Name: "sgg"})
	want := []string{"sgg", "-", "-", "-", "no", "never", "-", "-"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}
