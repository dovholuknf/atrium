package link

import "testing"

// A REDIRECT LEAVES ONLY FOR A PATH UNDER THE PREFIX, and anything a browser would read as another host is dropped.
func TestPreviewLocationStaysSameOrigin(t *testing.T) {
	const prefix = "/p/room/8080"
	for _, c := range []struct{ in, want string }{
		{"//evil.com", ""},
		{"/\\evil.com", ""},
		{"\\\\evil.com", ""},
		{"https://evil.com/x", ""},
		{"http://127.0.0.1:9999/x", ""},
		{"/a", prefix + "/a"},
		{prefix + "/b?c=1", prefix + "/b?c=1"},
		{prefix, prefix},
		{"http://127.0.0.1:8080/d?e=f", prefix + "/d?e=f"},
		{"next", "./next"},
	} {
		if got := previewLocation(c.in, prefix, 8080); got != c.want {
			t.Errorf("previewLocation(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
