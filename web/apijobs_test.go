package web

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateJobContentType(t *testing.T) {
	app, _ := newTestServer(t)
	for _, ct := range []string{
		"application/json",
		"application/json; charset=utf-8",
		"Application/JSON;charset=UTF-8",
		"application/json ; charset=utf-8",
	} {
		status, body := send(t, app, "POST", "/api/v1/job/", ct, `{"properties":{}}`)
		assert.Equal(t, 200, status, "%q: %s", ct, body)
	}
	for _, ct := range []string{"", "text/plain", "application/jsonx", "application/json-patch+json"} {
		status, body := send(t, app, "POST", "/api/v1/job/", ct, `{"properties":{}}`)
		assert.Equal(t, 400, status, "%q: %s", ct, body)
		assert.Contains(t, body, `"code":1024`, ct)
	}
}

func TestGetJobsSearchIsCaseInsensitive(t *testing.T) {
	app, _ := newTestServer(t)
	status, body := send(t, app, "POST", "/api/v1/job/", "application/json", `{"properties":{}}`)
	require.Equal(t, 200, status, body)
	var created struct {
		Job struct {
			ID string `json:"id"`
		} `json:"job"`
	}
	require.NoError(t, json.Unmarshal([]byte(body), &created))
	id := created.Job.ID
	for _, search := range []string{id, strings.ToUpper(id), id[:8], strings.ToUpper(id[9:23])} {
		status, body := send(t, app, "GET", "/api/v1/jobs/?search="+search, "", "")
		assert.Equal(t, 200, status, body)
		assert.Contains(t, body, `"total":1,`, search)
	}
}
