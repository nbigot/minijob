package web

import (
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
