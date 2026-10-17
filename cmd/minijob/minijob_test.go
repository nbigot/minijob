package main

import (
	"bytes"
	"errors"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
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

func freePort(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	return strconv.Itoa(ln.Addr().(*net.TCPAddr).Port)
}

// lockedBuffer collects the program output from several goroutines.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func TestAdminShutdownWhileBusy(t *testing.T) {
	port := freePort(t)
	path := writeConfig(t, `address: "127.0.0.1:8080"`, `address: "127.0.0.1:`+port+`"`)
	base := "http://127.0.0.1:" + port
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

	client := &http.Client{Timeout: 90 * time.Second}
	up := false
	for i := 0; i < 100 && !up; i++ {
		if resp, err := client.Get(base + "/ping"); err == nil {
			_ = resp.Body.Close()
			up = resp.StatusCode == 200
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !up {
		t.Fatalf("server not up:\n%s", out.String())
	}

	// A long poll on a topic without jobs...
	type poll struct {
		status int
		at     time.Time
	}
	polled := make(chan poll, 1)
	go func() {
		status := 0
		resp, err := client.Post(base+"/api/v1/job/pull?topic=none&waittimeseconds=60", "", nil)
		if err == nil {
			status = resp.StatusCode
			_ = resp.Body.Close()
		}
		polled <- poll{status, time.Now()}
	}()
	// ...and job creations until the server is gone.
	stopCreates := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stopCreates:
					return
				default:
				}
				resp, err := client.Post(base+"/api/v1/job/", "application/json", strings.NewReader(`{"properties":{}}`))
				if err != nil {
					return
				}
				_ = resp.Body.Close()
			}
		}()
	}
	defer func() {
		close(stopCreates)
		wg.Wait()
	}()
	time.Sleep(300 * time.Millisecond)

	shutdownAt := time.Now()
	resp, err := client.Post(base+"/api/v1/admin/server/shutdown", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()

	select {
	case err := <-exited:
		code := 0
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			code = exitErr.ExitCode()
		}
		if code != 0 {
			t.Fatalf("exit status %d:\n%s", code, out.String())
		}
	case <-time.After(15 * time.Second):
		t.Fatalf("the server did not stop within 15 s:\n%s", out.String())
	}
	logs := out.String()
	if strings.Contains(logs, "panic") {
		t.Fatalf("panic:\n%s", logs)
	}
	select {
	case p := <-polled:
		if p.status != 404 || p.at.Sub(shutdownAt) > 5*time.Second {
			t.Fatalf("long poll answered %d after %v", p.status, p.at.Sub(shutdownAt))
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the long poll did not return")
	}
	serverStopped := strings.Index(logs, `"message":"Server stopped"`)
	serviceStopped := strings.Index(logs, `"message":"Service stopped"`)
	if serverStopped < 0 || serviceStopped < serverStopped {
		t.Fatalf("the service must stop after the HTTP server:\n%s", logs)
	}
}
