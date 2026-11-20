package cli

import (
	"log"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

// logDirFlag registers --log-dir on a command that runs a daemon.
func logDirFlag(c *cobra.Command, v *string) {
	c.Flags().StringVar(v, "log-dir", "",
		"write this process's stdout and stderr to <name>.out and <name>.err in this folder, so a launcher needs no "+
			"shell redirect. Not done when stderr is already a file")
}

// redirectLogs points stdout, stderr and the log package at <dir>/<tag>.out and <tag>.err, truncating them unless
// appendTo, and on Windows the process's stderr handle too, so a crash trace lands in the file. An empty dir, or a
// stderr that is already a file, changes nothing: a restarted room keeps the log it was handed. A console stderr is
// redirected, since the live scripts start the hub and rooms under a hidden one.
func redirectLogs(dir, tag string, appendTo bool) error {
	if dir == "" || stderrIsFile() {
		return nil
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	mode := os.O_CREATE | os.O_WRONLY | os.O_TRUNC
	if appendTo {
		mode = os.O_CREATE | os.O_WRONLY | os.O_APPEND
	}
	out, err := os.OpenFile(filepath.Join(dir, tag+".out"), mode, 0o600)
	if err != nil {
		return err
	}
	errf, err := os.OpenFile(filepath.Join(dir, tag+".err"), mode, 0o600)
	if err != nil {
		_ = out.Close()
		return err
	}
	os.Stdout, os.Stderr = out, errf
	log.SetOutput(errf)
	setStderrHandle(errf)
	return nil
}

func stderrIsFile() bool {
	fi, err := os.Stderr.Stat()
	return err == nil && fi.Mode().IsRegular()
}
