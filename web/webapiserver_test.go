package web

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/nbigot/minijob/config"
	"github.com/nbigot/minijob/job"
	"github.com/qri-io/jsonschema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// initSchema runs Init with JSON schema validation and the schema file at path.
func initSchema(t *testing.T, path string) (*WebAPIServer, error) {
	t.Helper()
	conf := &config.Config{}
	conf.Jobs.JsonSchema.Enable = true
	conf.Jobs.JsonSchema.Path = path
	w := &WebAPIServer{appConfig: conf}
	return w, w.Init()
}

// schemaFile writes the schema in a temporary file and returns its path.
func schemaFile(t *testing.T, schema string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "schema.json")
	require.NoError(t, os.WriteFile(path, []byte(schema), 0o600))
	return path
}

func TestSchemasThatMakeTheValidatorPanicAreRefused(t *testing.T) {
	// The validator (qri-io v0.2.1) panics on these schemas when it validates a body, which
	// stopped the server: they are refused when the server starts.
	for _, c := range []struct{ schema, body, problem string }{
		{`{"enum": []}`, `"a"`, `/enum: the enum is empty or null`},
		{`{"enum": null}`, `"a"`, `/enum: the enum is empty or null`},
		{`{"type": []}`, `"a"`, `/type: the type list is empty`},
		{`{"type": "object", "properties": {"topic": {"enum": []}}}`, `{"topic": "a"}`, `/properties/topic/enum: the enum is empty or null`},
		{`{"items": {"not": {"anyOf": [{"type": []}]}}}`, `[1]`, `/items/not/anyOf/0/type: the type list is empty`},
		{`{"additionalProperties": {"enum": []}}`, `{"a": 1}`, `/additionalProperties/enum: the enum is empty or null`},
		{`{"properties": {"a/b~": null}}`, `{}`, `/properties/a~1b~0: null instead of a schema`},
		{`{"patternProperties": {"a": null}}`, `{}`, `/patternProperties/a: null instead of a schema`},
		{`{"$defs": {"a": null}}`, `{}`, `/$defs/a: null instead of a schema`},
		{`{"definitions": {"a": null}}`, `{}`, `/definitions/a: null instead of a schema`},
		{`{"dependentSchemas": {"a": {"properties": {"b": null}}}}`, `{}`, `/dependentSchemas/a/properties/b: null instead of a schema`},
		{`{"allOf": [null]}`, `{}`, `/allOf/0: null instead of a schema`},
		{`{"anyOf": [{}, null]}`, `{}`, `/anyOf/1: null instead of a schema`},
		{`{"oneOf": [null]}`, `{}`, `/oneOf/0: null instead of a schema`},
		{`{"items": [null]}`, `[1]`, `/items/0: null instead of a schema`},
		{`{"$id": "#root"}`, `{}`, `/$id: plain-name fragment "#root", use "$anchor": "root" instead`},
		{`{"$anchor": "r", "$id": "#root"}`, `{}`, `/$id: plain-name fragment "#root", use "$anchor": "root" instead`},
		{`{"properties": {"a": {"$id": "#a"}}}`, `{}`, `/properties/a/$id: plain-name fragment "#a", use "$anchor": "a" instead`},
	} {
		_, err := initSchema(t, schemaFile(t, c.schema))
		if assert.Error(t, err, c.schema) {
			assert.Equal(t, "schema not supported by the validator: "+c.problem, err.Error())
		}
		// the reason: the validator panics
		var s jsonschema.Schema
		require.NoError(t, s.UnmarshalJSON([]byte(c.schema)))
		assert.Panics(t, func() { _, _ = s.ValidateBytes(noFetchContext, []byte(c.body)) }, c.schema)
	}
}

// validationErrors validates the body with the schema and returns the errors as "path: message".
func validationErrors(t *testing.T, schema, body string) []string {
	t.Helper()
	w, err := initSchema(t, schemaFile(t, schema))
	require.NoError(t, err, schema)
	keyErrors, err := w.validateJSON([]byte(body))
	require.NoError(t, err, schema)
	var errs []string
	for _, e := range keyErrors {
		errs = append(errs, e.PropertyPath+": "+e.Message)
	}
	return errs
}

func TestSchemaRefLoopsEnd(t *testing.T) {
	// A "$ref" that comes back to a schema being evaluated on the same value made the
	// validator recurse until the stack overflowed, a fatal error that stopped the server.
	// Such a loop adds no constraint.
	for _, c := range []struct {
		schema, body string
		errors       []string
	}{
		{`{"$ref": "#"}`, `{"a": 1}`, nil},
		{`{"$ref": ""}`, `{"a": 1}`, nil},
		{`{"$ref": null}`, `{"a": 1}`, nil},
		{`{"$ref": "%23"}`, `1`, nil},
		{`{"$ref": "#", "type": "object"}`, `1`, []string{"/: type should be object, got integer"}},
		{`{"$defs": {"a": {"$ref": "#/$defs/a"}}, "$ref": "#/$defs/a"}`, `1`, nil},
		{`{"$ref": "#/properties/a", "properties": {"a": {"$ref": "#/properties/a"}}}`, `{"a": 1}`, nil},
		{`{"$defs": {"a": {"$ref": "#/$defs/b"}, "b": {"$ref": "#/$defs/a", "minimum": 2}}, "properties": {"x": {"$ref": "#/$defs/a"}}}`,
			`{"x": 1}`, []string{"/x: must be greater than or equal to 2"}},
		{`{"allOf": [{"$ref": "#"}], "anyOf": [{"$ref": "#"}], "required": ["a"]}`, `{}`, []string{`/: "a" value is required`}},
		{`{"not": {"$ref": "#"}}`, `1`, []string{"/: result was valid, ('not') expected invalid"}},
		{`{"anyOf": [{"type": "string"}, {"$ref": "#"}]}`, `1`, nil},
		{`{"oneOf": [{"type": "string"}, {"$ref": "#"}]}`, `"a"`, []string{"/: matched more than one specified OneOf schemas"}},
		{`{"dependentSchemas": {"a": {"$ref": "#"}}}`, `{"a": 1}`, nil},
		{`{"if": {"$ref": "#"}, "then": {"maximum": 0}}`, `1`, []string{"/: must be less than or equal to 0"}},
		{`{"$defs": {"a": {"$anchor": "a", "$ref": "#a"}}, "$ref": "#/$defs/a"}`, `1`, nil},
		{`{"$id": "http://example.test/loop.json", "$ref": "http://example.test/loop.json"}`, `1`, nil},
		// a recursion through the document still works: the value changes at each step
		{`{"type": "object", "required": ["n"], "properties": {"child": {"$ref": "#"}}}`,
			`{"n": 1, "child": {"n": 2, "child": {}}}`, []string{`/child/child: "n" value is required`}},
		{`{"items": {"$ref": "#"}, "type": "array"}`, `[[], [1]]`, []string{"/1/0: type should be array, got integer"}},
	} {
		assert.Equal(t, c.errors, validationErrors(t, c.schema, c.body), c.schema)
	}
}

func TestSchemaRefLoopOverHTTP(t *testing.T) {
	// The first job created stopped the server.
	app := schemaServer(t, `{"$ref": "#", "required": ["properties"]}`)
	for i := 0; i < 2; i++ {
		status, body := send(t, app, "POST", "/api/v1/job/", "application/json", `{"properties":{}}`)
		assert.Equal(t, 200, status, body)
	}
	status, body := send(t, app, "POST", "/api/v1/job/", "application/json", `{}`)
	assert.Equal(t, 400, status, body)
	assert.Contains(t, body, `"tag":"\"properties\" value is required"`)
}

func TestSchemaRefKeywordOrder(t *testing.T) {
	// "$ref" is still evaluated before "$recursiveRef" (the order of the errors on a path).
	assert.Equal(t, []string{"/: type should be string, got boolean", "/: type should be number, got boolean"},
		validationErrors(t, `{"$recursiveRef": "#/$defs/n", "$ref": "#/$defs/s",
			"$defs": {"s": {"type": "string"}, "n": {"type": "number"}}}`, `true`))
}

func TestTemplateSchemasAcceptEveryJobField(t *testing.T) {
	// The templates and the Docker image validate the jobs with these schemas: a job with
	// a callback URL was refused.
	body, err := json.Marshal(job.JobRequest{
		Topic:         "t",
		JobProperties: job.JobProperties{"a": 1},
		LockResources: job.ResourceList{"r"},
		CallbackURL:   "https://example.test/hook",
	})
	require.NoError(t, err)
	for _, path := range []string{
		filepath.Join("..", "config-templates", "standard", "config", "schema.json"),
		filepath.Join("..", "config-templates", "docker", "config", "schema.json"),
	} {
		w, err := initSchema(t, path)
		require.NoError(t, err, path)
		keyErrors, err := w.validateJSON(body)
		require.NoError(t, err, path)
		assert.Empty(t, keyErrors, "%s: %s", path, body)
	}
}

func TestSchemasThatTheValidatorHandlesAreAccepted(t *testing.T) {
	paths := []string{
		filepath.Join("..", "config-templates", "standard", "config", "schema.json"),
		filepath.Join("..", "config-templates", "docker", "config", "schema.json"),
	}
	for _, schema := range []string{
		`{"enum": [1], "type": ["string"]}`,
		`{"not": null, "additionalProperties": null, "items": null, "dependentSchemas": {"a": null}}`,
		// properties named like keywords
		`{"properties": {"enum": {"enum": ["a"]}, "type": {"type": "string"}}}`,
		// not schemas
		`{"examples": [{"enum": []}], "default": {"properties": {"a": null}}, "x-a": {"type": []}}`,
		`{"$id": "#/properties/a", "$defs": {"b": {"$id": "http://example.test/b.json#"}}}`,
		// the map of local ids exists once the "$anchor" of the root is registered
		`{"$anchor": "r", "properties": {"a": {"$id": "#a"}}}`,
	} {
		paths = append(paths, schemaFile(t, schema))
	}
	for _, path := range paths {
		w, err := initSchema(t, path)
		require.NoError(t, err, path)
		for _, body := range []string{`{}`, `{"a": 1, "enum": "a", "type": "s"}`, `null`, `[1]`, `"a"`} {
			assert.NotPanics(t, func() { _, _ = w.validateJSON([]byte(body)) }, path)
		}
	}
}

func TestPanicInRequestAnswers500(t *testing.T) {
	// A panic while answering a request ended the server; it is now a 500 answer.
	app, _ := newTestServer(t)
	app.Get("/panic", func(c *fiber.Ctx) error { panic("boom") })
	status, body := send(t, app, "GET", "/panic", "", "")
	assert.Equal(t, 500, status)
	assert.Equal(t, "Internal Server Error", body)
	status, body = send(t, app, "POST", "/api/v1/job/", "application/json", `{"properties":{}}`)
	assert.Equal(t, 200, status, body)
}

func TestSchemaLockReleasedAfterPanic(t *testing.T) {
	// The validator panics on a plain-name fragment "$id" (qri-io v0.2.1): the validations
	// that follow must not wait forever for the schema lock.
	registerSchemaKeywords()
	w := &WebAPIServer{}
	require.NoError(t, w.schema.UnmarshalJSON([]byte(`{"$id": "#root"}`)))
	validate := func() (panicked bool) {
		defer func() { panicked = recover() != nil }()
		_, _ = w.validateJSON([]byte(`{}`))
		return false
	}
	require.True(t, validate(), "the schema no longer makes the validator panic")
	done := make(chan bool)
	go func() { done <- validate() }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the schema lock was not released after a panic")
	}
}
