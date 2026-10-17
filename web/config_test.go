package web

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRequestHeadersUpTo8KiB(t *testing.T) {
	app, _ := newTestServer(t)
	base := listen(t, app)
	get := func(headerSize int) (int, error) {
		req, err := http.NewRequest("GET", base+"/livez", nil)
		require.NoError(t, err)
		req.Header.Set("Authorization", "Bearer "+strings.Repeat("a", headerSize))
		resp, err := (&http.Client{}).Do(req)
		if err != nil {
			return 0, err
		}
		defer func() { _ = resp.Body.Close() }()
		return resp.StatusCode, nil
	}
	status, err := get(6000) // a large token
	require.NoError(t, err)
	assert.Equal(t, 200, status)
	status, err = get(9000)
	if err == nil { // the server may close the connection before the client reads the answer
		assert.Equal(t, 431, status)
	}
}
