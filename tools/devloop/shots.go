package main

import (
	"encoding/base64"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// shotCommands are the remote commands to list a directory's PNGs and to print one as base64. scp is not used: a
// PowerShell default shell breaks it, base64 over stdout works everywhere.
func shotCommands(shell, dir string) (list string, fetch func(name string) string) {
	if shell == "sh" {
		d := strings.TrimRight(dir, "/")
		return fmt.Sprintf("ls -1 '%s' | grep -i '\\.png$'", d),
			func(n string) string { return fmt.Sprintf("base64 < '%s/%s'", d, n) }
	}
	return fmt.Sprintf("powershell -NoProfile -Command \"Get-ChildItem -Name -Path '%s' -Filter *.png\"", dir),
		func(n string) string {
			return fmt.Sprintf("powershell -NoProfile -Command \"[Convert]::ToBase64String([IO.File]::ReadAllBytes('%s'))\"",
				strings.TrimRight(strings.TrimRight(dir, "\\"), "/")+"\\"+n)
		}
}

// pngNames keeps the lines of a listing that are plain PNG file names.
func pngNames(listing string) []string {
	var names []string
	for _, l := range strings.Split(strings.ReplaceAll(listing, "\r", ""), "\n") {
		l = strings.TrimSpace(l)
		if strings.HasSuffix(strings.ToLower(l), ".png") && !strings.ContainsAny(l, "/\\") {
			names = append(names, l)
		}
	}
	return names
}

func cmdShots(args []string, out io.Writer) error {
	pos, flags := splitArgs(args, "room", "ssh", "out", "shell")
	fs := flag.NewFlagSet("shots", flag.ContinueOnError)
	room := fs.String("room", "", "the room to copy from")
	host := fs.String("ssh", "", "ssh host, default the room name")
	dest := fs.String("out", "", `destination, default D:\tmp\shots\<room>`)
	shell := fs.String("shell", "ps", "the room's default shell: ps or sh")
	if err := fs.Parse(flags); err != nil {
		return err
	}
	if len(pos) != 1 || *room == "" {
		return fmt.Errorf("usage: shots --room <room> <remote dir>")
	}
	h := *host
	if h == "" {
		h = *room
	}
	d := *dest
	if d == "" {
		d = filepath.Join(`D:\tmp\shots`, *room)
	}
	if err := os.MkdirAll(d, 0o755); err != nil {
		return err
	}
	list, fetch := shotCommands(*shell, pos[0])
	listing, err := sshRun(h, list)
	if err != nil {
		return err
	}
	names := pngNames(listing)
	if len(names) == 0 {
		return fmt.Errorf("no PNGs in %s on %s", pos[0], *room)
	}
	for _, n := range names {
		b64, err := sshRun(h, fetch(n))
		if err != nil {
			return err
		}
		raw, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(b64), ""))
		if err != nil {
			return fmt.Errorf("%s: not base64: %w", n, err)
		}
		if err := os.WriteFile(filepath.Join(d, n), raw, 0o644); err != nil {
			return err
		}
		step(out, "copied %s (%d bytes)", n, len(raw))
	}
	return nil
}
