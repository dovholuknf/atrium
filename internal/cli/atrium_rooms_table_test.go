package cli

import (
	"reflect"
	"testing"

	"github.com/dovholuknf/atrium/internal/link"
)

func TestRoomRowOfflineRoom(t *testing.T) {
	got := roomRow(link.Known{Name: "room-g"})
	want := []string{"room-g", "-", "-", "-", "no", "never", "-", "-"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}
