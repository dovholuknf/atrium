package cli

import (
	"fmt"
	"log"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/dovholuknf/atrium/internal/base/nowindow"
)

// boardOpens says whether this start of `atrium run` opens the board in a browser: only the first start of a hub, never
// with --no-open, never over ssh, and on Linux and the BSDs only when there is a display to open one on.
func boardOpens(fresh, noOpen bool, goos string, getenv func(string) string) bool {
	if !fresh || noOpen || strings.TrimSpace(getenv("SSH_CONNECTION")) != "" {
		return false
	}
	switch goos {
	case "windows", "darwin":
		return true
	}
	return strings.TrimSpace(getenv("DISPLAY")) != "" || strings.TrimSpace(getenv("WAYLAND_DISPLAY")) != ""
}

// firstStartURL is the board with the folder `atrium run` was started in, which the board keeps as the first launch's
// default. A route starts `#/`, so `#start=` never reads as one.
func firstStartURL(boardAddr, folder string) string {
	u := "http://127.0.0.1" + portOf(boardAddr) + "/"
	if folder == "" {
		return u
	}
	return u + "#start=" + url.PathEscape(filepath.ToSlash(folder))
}

// firstStart is the next step a fresh hub prints, and the board opened on it when boardOpens says so.
func firstStart(fresh, noOpen, withRoom bool, boardAddr string) {
	if !fresh {
		return
	}
	folder := ""
	if withRoom {
		folder, _ = os.Getwd()
	}
	u := firstStartURL(boardAddr, folder)
	if folder != "" {
		fmt.Printf("  board: %s - start an agent in %s from the board\n\n", "http://127.0.0.1"+portOf(boardAddr), filepath.ToSlash(folder))
	} else {
		fmt.Printf("  board: %s\n\n", u)
	}
	if !boardOpens(fresh, noOpen, runtime.GOOS, os.Getenv) {
		return
	}
	if err := openBrowser(runtime.GOOS, u); err != nil {
		log.Printf("[hub] could not open the board in a browser, open %s yourself: %v", u, err)
	}
}

func openBrowser(goos, u string) error {
	var cmd *exec.Cmd
	switch goos {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", u)
	case "darwin":
		cmd = exec.Command("open", u)
	default:
		cmd = exec.Command("xdg-open", u)
	}
	nowindow.Hide(cmd)
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}
