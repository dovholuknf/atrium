package conn

import (
	"bytes"
	"context"
	"log"
	"net"
	"strings"
	"testing"
)

func takeLogs(t *testing.T, send string) string {
	t.Helper()
	var buf bytes.Buffer
	old := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(old) })

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	client, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	server, err := ln.Accept()
	if err != nil {
		t.Fatal(err)
	}
	if send != "" {
		_, _ = client.Write([]byte(send))
	}
	client.Close()
	newHub(Timings{}).take(context.Background(), server, "")
	return buf.String()
}

func TestAPortCheckThatHangsUpIsNotLogged(t *testing.T) {
	if got := takeLogs(t, ""); got != "" {
		t.Fatalf("a dial that hung up before a hello was logged: %q", got)
	}
}

func TestAHelloThatIsNotOneIsStillLogged(t *testing.T) {
	if got := takeLogs(t, "not json\n"); !strings.Contains(got, "refused a connection") {
		t.Fatalf("a garbled hello was not logged: %q", got)
	}
}
