package webserver

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/nbigot/minijob/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type consulRequest struct {
	method, path, token string
	body                map[string]interface{}
}

// fakeConsulAgent records the requests of the Consul client.
func fakeConsulAgent(t *testing.T) func() []consulRequest {
	t.Helper()
	var mu sync.Mutex
	var requests []consulRequest
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		defer mu.Unlock()
		requests = append(requests, consulRequest{r.Method, r.URL.Path, r.Header.Get("X-Consul-Token"), body})
	}))
	t.Cleanup(agent.Close)
	t.Setenv("CONSUL_HTTP_ADDR", agent.URL)
	t.Setenv("CONSUL_HTTP_TOKEN", "")
	t.Setenv("CONSUL_HTTP_TOKEN_FILE", "")
	return func() []consulRequest {
		mu.Lock()
		defer mu.Unlock()
		return append([]consulRequest(nil), requests...)
	}
}

func consulServer(healthCheck bool) *Server {
	conf := &config.Config{}
	conf.Consul.Enable = true
	conf.Consul.Server.Address = "127.0.0.1"
	conf.Consul.Server.Port = 8080
	conf.Consul.Server.Token = "secret"
	conf.Consul.Service.ID = "minijob-1"
	conf.Consul.Service.Name = "minijob"
	conf.Consul.HealthCheck.Enable = healthCheck
	conf.Consul.HealthCheck.CheckId = "alive"
	conf.Consul.HealthCheck.Interval = "10s"
	conf.Consul.HealthCheck.Timeout = "5s"
	conf.Consul.HealthCheck.URL = "http://127.0.0.1:8080/healthcheck"
	return &Server{appConfig: conf, logger: zap.NewNop()}
}

func TestConsulDeregistrationUsesTheToken(t *testing.T) {
	// The registration used consul.server.token, the deregistration did not: with ACLs,
	// the agent refused it and the stopped server stayed registered.
	requests := fakeConsulAgent(t)
	s := consulServer(true)
	require.NoError(t, s.RegisterConsul())
	require.NoError(t, s.DeRegisterConsul())
	got := requests()
	require.Len(t, got, 2)
	assert.Equal(t, "/v1/agent/service/register", got[0].path)
	assert.Equal(t, "secret", got[0].token)
	assert.Equal(t, "/v1/agent/service/deregister/minijob-1", got[1].path)
	assert.Equal(t, "secret", got[1].token)
}

func TestConsulHealthCheckSettings(t *testing.T) {
	// The HTTP check was registered even with healthCheck.enable false, and without its
	// checkId (Consul made one up).
	requests := fakeConsulAgent(t)
	require.NoError(t, consulServer(true).RegisterConsul())
	require.NoError(t, consulServer(false).RegisterConsul())
	got := requests()
	require.Len(t, got, 2)
	assert.Equal(t, map[string]interface{}{
		"CheckID":  "alive",
		"HTTP":     "http://127.0.0.1:8080/healthcheck",
		"Interval": "10s",
		"Timeout":  "5s",
	}, got[0].body["Check"])
	assert.Nil(t, got[1].body["Check"])
	assert.Equal(t, "minijob-1", got[1].body["ID"])
}
