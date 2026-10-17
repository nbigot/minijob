package web

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
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

func TestEmptyPullTopicIsKept(t *testing.T) {
	// The topic of a pull that finds no job is recorded (topics, metrics); it must not
	// change when the request buffer is reused by later requests.
	app, _ := newTestServer(t)
	base := listen(t, app)
	client := &http.Client{}
	status, body := httpSend(t, client, "POST", base+"/api/v1/job/pull?topic=none")
	require.Equal(t, 404, status, body)
	for i := 0; i < 20; i++ {
		httpSend(t, client, "GET", base+"/api/v1/jobs/?search=abcdefgh")
	}
	_, body = httpSend(t, client, "GET", base+"/api/v1/topics/")
	assert.JSONEq(t, `{"code":200,"message":"success","topics":["none"]}`, body)
}

// schemaServer runs the API with JSON schema validation and the given schema.
func schemaServer(t *testing.T, schema string) *fiber.App {
	t.Helper()
	path := filepath.Join(t.TempDir(), "schema.json")
	require.NoError(t, os.WriteFile(path, []byte(schema), 0o600))
	app, _ := newTestServer(t,
		"    jsonSchema:\n        enable: false  # Enable JSON schema validation for job data\n",
		"    jsonSchema:\n        enable: true\n        path: \""+filepath.ToSlash(path)+"\"\n")
	return app
}

func TestSchemaWithDefinitions(t *testing.T) {
	// draft-07 schemas put reusable parts under "definitions"
	app := schemaServer(t, `{
		"$schema": "http://json-schema.org/draft-07/schema",
		"type": "object",
		"required": ["properties"],
		"properties": {
			"properties": {"$ref": "#/definitions/props"},
			"topic": {"$ref": "#/definitions/name"}
		},
		"definitions": {
			"props": {"type": "object"},
			"name": {"type": "string"}
		}
	}`)
	status, body := send(t, app, "POST", "/api/v1/job/", "application/json", `{"topic":"t","properties":{}}`)
	assert.Equal(t, 200, status, body)
	status, body = send(t, app, "POST", "/api/v1/job/", "application/json", `{"topic":5,"properties":{}}`)
	assert.Equal(t, 400, status, body)
	assert.JSONEq(t, `{"error":"invalid json content","code":1023,"jobUUID":"00000000-0000-0000-0000-000000000000",
		"validationErrors":[{"failedField":"/topic","tag":"type should be string, got integer","value":"5"}]}`, body)
}
