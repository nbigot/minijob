package web

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/nbigot/minijob/config"
	"github.com/nbigot/minijob/jobbackendprovider/registry"
	"github.com/nbigot/minijob/service"
	"github.com/stretchr/testify/require"
)

var registryOnce sync.Once

// newTestServer runs the service on the load test template (in memory, no file) with the
// given replacements, and returns the Fiber app with every API route.
func newTestServer(t *testing.T, replacements ...string) (*fiber.App, *service.Service) {
	t.Helper()
	registryOnce.Do(func() { require.NoError(t, registry.Initialize()) })
	template, err := os.ReadFile(filepath.Join("..", "config-templates", "standard", "config", "config.loadtest.yaml"))
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "config.yaml")
	text := strings.NewReplacer(replacements...).Replace(string(template))
	require.NoError(t, os.WriteFile(path, []byte(text), 0o600))
	conf, err := config.LoadConfig(path)
	require.NoError(t, err)

	svc, err := service.CreateAndInitService(conf, "test")
	require.NoError(t, err)
	go func() { _ = svc.Run() }()
	t.Cleanup(func() { _ = svc.Stop() })

	w := NewWebAPIServer(conf, GetFiberConfig(), svc, func() {}, func() {})
	require.NoError(t, w.Init())
	w.AddRoutes(w.GetFiberApp())
	return w.GetFiberApp(), svc
}

// send sends one request and returns the status and the body.
func send(t *testing.T, app *fiber.App, method, path, contentType, body string) (int, string) {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := app.Test(req, -1)
	require.NoError(t, err)
	b, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, string(b)
}

// listen serves the app on a free local port and returns its base URL.
func listen(t *testing.T, app *fiber.App) string {
	t.Helper()
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	require.NoError(t, err)
	go func() { _ = app.Listener(ln) }()
	t.Cleanup(func() { _ = app.Shutdown() })
	return "http://" + ln.Addr().String()
}

// httpSend sends one request over the client's keep-alive connections.
func httpSend(t *testing.T, client *http.Client, method, url string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(method, url, nil)
	require.NoError(t, err)
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, string(b)
}
