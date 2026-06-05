package main

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestVersionSubcommand(t *testing.T) {
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	Version = "0.1.0-test"
	os.Args = []string{"labubu", "version"}
	run()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	if !strings.Contains(output, "0.1.0-test") {
		t.Errorf("version output missing version: got %q", output)
	}
}

func TestHelpSubcommand(t *testing.T) {
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	os.Args = []string{"labubu", "help"}
	run()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	if !strings.Contains(output, "Usage:") {
		t.Errorf("help output missing 'Usage:': got %q", output)
	}
	if !strings.Contains(output, "serve") {
		t.Errorf("help output missing 'serve': got %q", output)
	}
}

func TestNoArgsReturnsExitCode1(t *testing.T) {
	oldStderr := os.Stderr
	rErr, wErr, _ := os.Pipe()
	os.Stderr = wErr

	os.Args = []string{"labubu"}
	code := run()

	wErr.Close()
	os.Stderr = oldStderr

	if code != 1 {
		t.Errorf("expected exit code 1, got %d", code)
	}

	var bufErr bytes.Buffer
	bufErr.ReadFrom(rErr)
	if !strings.Contains(bufErr.String(), "Usage:") {
		t.Errorf("no-args stderr missing 'Usage:': got %q", bufErr.String())
	}
}

func TestUnknownCommandReturnsExitCode1(t *testing.T) {
	oldStderr := os.Stderr
	rErr, wErr, _ := os.Pipe()
	os.Stderr = wErr

	os.Args = []string{"labubu", "foobar"}
	code := run()

	wErr.Close()
	os.Stderr = oldStderr

	if code != 1 {
		t.Errorf("expected exit code 1, got %d", code)
	}

	var bufErr bytes.Buffer
	bufErr.ReadFrom(rErr)
	if !strings.Contains(bufErr.String(), "Unknown command") {
		t.Errorf("unknown cmd stderr missing 'Unknown command': got %q", bufErr.String())
	}
}
