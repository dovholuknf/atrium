package cli

import "testing"

func TestServicePasswordIsRegistered(t *testing.T) {
	cmd, _, err := newServiceCmd().Find([]string{"password"})
	if err != nil || cmd.Name() != "password" {
		t.Fatalf("service password not registered: %v", err)
	}
	for _, f := range []string{"name", "password-file"} {
		if cmd.Flags().Lookup(f) == nil {
			t.Fatalf("service password has no --%s", f)
		}
	}
}
