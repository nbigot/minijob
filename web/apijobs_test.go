package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
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

const templateLikeSchema = `{
	"type": "object",
	"additionalProperties": false,
	"required": ["properties"],
	"properties": {
		"topic": {"type": "string"},
		"priority": {"type": "integer"},
		"properties": {"type": "object"},
		"lockResources": {"type": "array", "items": {"type": "string"}},
		"delay": {"type": "integer"}
	}
}`

func TestSchemaErrorsInPathOrder(t *testing.T) {
	app := schemaServer(t, templateLikeSchema)
	body := `{"topic":["a"],"priority":1.5,"properties":[1,2],"lockResources":[1,"a",{}],"delay":"x"}`
	expected := `{"error":"invalid json content","code":1023,"jobUUID":"00000000-0000-0000-0000-000000000000","validationErrors":[` +
		`{"failedField":"/delay","tag":"type should be integer, got string","value":"x"},` +
		`{"failedField":"/lockResources/0","tag":"type should be string, got integer","value":"1"},` +
		`{"failedField":"/lockResources/2","tag":"type should be string, got object","value":"map[]"},` +
		`{"failedField":"/priority","tag":"type should be integer, got number","value":"1.5"},` +
		`{"failedField":"/properties","tag":"type should be object, got array","value":"[1 2]"},` +
		`{"failedField":"/topic","tag":"type should be string, got array","value":"[a]"}]}`
	for i := 0; i < 20; i++ {
		status, got := send(t, app, "POST", "/api/v1/job/", "application/json", body)
		require.Equal(t, 400, status)
		require.Equal(t, expected, got, "attempt %d", i)
	}
}

func TestSchemaValidationUnderConcurrency(t *testing.T) {
	// One schema is shared by all requests (run with -race).
	app := schemaServer(t, `{
		"type": "object",
		"required": ["properties"],
		"properties": {"properties": {"$ref": "#/definitions/props"}, "topic": {"$ref": "#/definitions/name"}},
		"definitions": {"props": {"type": "object"}, "name": {"type": "string"}}
	}`)
	var wg sync.WaitGroup
	errs := make(chan string, 64)
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 20; i++ {
				status, body := send(t, app, "POST", "/api/v1/job/", "application/json", `{"topic":5,"properties":{}}`)
				if status != 400 || !strings.Contains(body, `"tag":"type should be string, got integer"`) {
					errs <- body
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Error(e)
	}
}

func TestSchemaRemoteRefIsNotDownloaded(t *testing.T) {
	var fetched atomic.Int64
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fetched.Add(1)
		_, _ = w.Write([]byte(`{"type":"string"}`))
	}))
	defer remote.Close()
	app := schemaServer(t, `{"type":"object","properties":{"topic":{"$ref":"`+remote.URL+`/name.json"}}}`)
	status, body := send(t, app, "POST", "/api/v1/job/", "application/json", `{"topic":"t","properties":{}}`)
	assert.Equal(t, 400, status, body)
	assert.Contains(t, body, `"tag":"failed to resolve schema for ref `+remote.URL+`/name.json"`)
	assert.Equal(t, int64(0), fetched.Load(), "the schema must not be downloaded while answering a request")
}

func TestPropertiesKeepExactIntegers(t *testing.T) {
	app, _ := newTestServer(t)
	status, body := send(t, app, "POST", "/api/v1/job/", "application/json",
		`{"properties":{"id":12345678901234567890,"ratio":0.5,"list":[9007199254740993]}}`)
	require.Equal(t, 200, status, body)
	exact := `"properties":{"id":12345678901234567890,"list":[9007199254740993],"ratio":0.5}`
	assert.Contains(t, body, exact)
	var created struct {
		Job struct {
			ID string `json:"id"`
		} `json:"job"`
	}
	require.NoError(t, json.Unmarshal([]byte(body), &created))
	status, body = send(t, app, "GET", "/api/v1/job/"+created.Job.ID, "", "")
	require.Equal(t, 200, status, body)
	assert.Contains(t, body, exact)
}
