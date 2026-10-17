//go:build unix

package main

import (
	"errors"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestSignals(t *testing.T) {
	port := freePort(t)
	path := writeConfig(t, `address: "127.0.0.1:8080"`, `address: "127.0.0.1:`+port+`"`)
	var out lockedBuffer
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	cmd.Env = append(os.Environ(), "MINIJOB_RUN_MAIN=1", "MINIJOB_ARGS=-config "+path)
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	t.Cleanup(func() { _ = cmd.Process.Kill() })
	waitFor := func(what string, ok func() bool) {
		t.Helper()
		for i := 0; i < 200; i++ {
			if ok() {
				return
			}
			time.Sleep(25 * time.Millisecond)
		}
		t.Fatalf("%s:\n%s", what, out.String())
	}
	up := func() bool {
		resp, err := http.Get("http://127.0.0.1:" + port + "/ping")
		if err != nil {
			return false
		}
		_ = resp.Body.Close()
		return resp.StatusCode == 200
	}
	waitFor("server not up", up)

	// SIGHUP reloads the configuration and restarts the server.
	if err := cmd.Process.Signal(syscall.SIGHUP); err != nil {
		t.Fatal(err)
	}
	waitFor("no restart after SIGHUP", func() bool {
		return strings.Count(out.String(), `"message":"Start HTTP web server"`) == 2 && up()
	})

	// SIGQUIT stops the server gracefully.
	if err := cmd.Process.Signal(syscall.SIGQUIT); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-exited:
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			t.Fatalf("exit status %d after SIGQUIT:\n%s", exitErr.ExitCode(), out.String())
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("still running after SIGQUIT:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "Received SIGQUIT, shutting down server...") {
		t.Fatalf("no SIGQUIT log:\n%s", out.String())
	}
}
