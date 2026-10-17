package web

import (
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSwaggerPathsAndPrefix(t *testing.T) {
	var loggedPath string
	app, _ := newTestServerWith(t, func(app *fiber.App) {
		// like the access log: the path read after the handlers ran
		app.Use(func(c *fiber.Ctx) error {
			err := c.Next()
			loggedPath = c.Path()
			return err
		})
	})
	get := func(path, prefix string) (int, string) {
		req := httptest.NewRequest("GET", path, nil)
		if prefix != "" {
			req.Header.Set("X-Forwarded-Prefix", prefix)
		}
		resp, err := app.Test(req, -1)
		require.NoError(t, err)
		b, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		return resp.StatusCode, string(b)
	}

	status, body := get("/docs/nope.js", "")
	assert.Equal(t, 404, status)
	assert.Equal(t, "Cannot GET /docs/nope.js", body)
	assert.Equal(t, "/docs/nope.js", loggedPath)

	status, body = get("/docs/index.html", "")
	assert.Equal(t, 200, status)
	assert.Equal(t, "/docs/index.html", loggedPath)
	assert.Contains(t, body, `"url":"/docs/doc.json"`)

	// The forwarded prefix of each request is used (not the one of the first request).
	_, body = get("/docs/index.html", "/proxy/")
	assert.Contains(t, body, `"url":"/proxy/docs/doc.json"`)
	_, body = get("/docs/index.html", "")
	assert.Contains(t, body, `"url":"/docs/doc.json"`)
	assert.False(t, strings.Contains(body, "/proxy"))
}
