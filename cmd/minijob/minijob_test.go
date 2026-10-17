package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestMain runs the program itself when MINIJOB_RUN_MAIN is set, so that tests can check
// its exit status.
func TestMain(m *testing.M) {
	if os.Getenv("MINIJOB_RUN_MAIN") == "1" {
		os.Args = append([]string{"minijob"}, strings.Fields(os.Getenv("MINIJOB_ARGS"))...)
		main()
		return
	}
	os.Exit(m.Run())
}

// writeConfig writes the load test template with the given replacements.
func writeConfig(t *testing.T, replacements ...string) string {
	t.Helper()
	template, err := os.ReadFile(filepath.Join("..", "..", "config-templates", "standard", "config", "config.loadtest.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	text := strings.NewReplacer(replacements...).Replace(string(template))
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func runProgram(t *testing.T, args string) int {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	cmd.Env = append(os.Environ(), "MINIJOB_RUN_MAIN=1", "MINIJOB_ARGS="+args)
	err := cmd.Run()
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	if err != nil {
		t.Fatal(err)
	}
	return 0
}

func TestExitStatusOnStartupError(t *testing.T) {
	cases := map[string][]string{
		"unknown backend":    {`type: "inMemory"`, `type: "nope"`},
		"missing config":     nil,
		"unreadable address": {`address: "127.0.0.1:8080"`, `address: "127.0.0.1:not-a-port"`},
	}
	for name, replacements := range cases {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "missing.yaml")
			if replacements != nil {
				path = writeConfig(t, replacements...)
			}
			if code := runProgram(t, "-config "+path); code != 1 {
				t.Fatalf("exit status %d, want 1", code)
			}
		})
	}
}

func TestExitStatusVersion(t *testing.T) {
	if code := runProgram(t, "-version"); code != 0 {
		t.Fatalf("exit status %d, want 0", code)
	}
}
