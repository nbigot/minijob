package web

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// lockResource creates a job locking the resource and waits until it is queued.
func lockResource(t *testing.T, send func(method, path, body string) (int, string), name string) {
	t.Helper()
	status, body := send("POST", "/api/v1/job/", `{"properties":{},"lockResources":["`+name+`"]}`)
	require.Equal(t, 200, status, body)
	require.Eventually(t, func() bool {
		_, body := send("GET", "/api/v1/resources/", "")
		return strings.Contains(body, `"`+name+`"`)
	}, 5*time.Second, 20*time.Millisecond)
}

func TestUnlockResource(t *testing.T) {
	app, _ := newTestServer(t)
	do := func(method, path, body string) (int, string) {
		return send(t, app, method, path, "application/json", body)
	}

	// The name in the path is used.
	lockResource(t, do, "r1")
	status, body := do("POST", "/api/v1/resource/r1/unlock", "")
	assert.Equal(t, 200, status, body)
	assert.JSONEq(t, `{"code":200,"message":"success"}`, body)
	_, body = do("GET", "/api/v1/resources/", "")
	assert.NotContains(t, body, `"r1"`)

	// Percent-encoded names are decoded.
	lockResource(t, do, "a b")
	status, body = do("POST", "/api/v1/resource/a%20b/unlock", "")
	assert.Equal(t, 200, status, body)

	// The query parameter still works and takes precedence.
	lockResource(t, do, "r2")
	status, body = do("POST", "/api/v1/resource/other/unlock?resourcename=r2", "")
	assert.Equal(t, 200, status, body)

	// Unknown resources answer 404 with an error code.
	for _, path := range []string{"/api/v1/resource/zz/unlock", "/api/v1/resource/x/unlock?resourcename=zz"} {
		status, body = do("POST", path, "")
		assert.Equal(t, 404, status, body)
		assert.JSONEq(t, `{"error":"resource not found: zz","code":1030,"jobUUID":"00000000-0000-0000-0000-000000000000"}`, body)
	}
}
