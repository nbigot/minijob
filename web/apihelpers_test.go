package web

import (
	"io"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/nbigot/minijob/constants"
	"github.com/stretchr/testify/assert"
	"github.com/valyala/fasthttp"
)

func TestGetUintParameterFromQuery(t *testing.T) {
	app := fiber.New()

	tests := []struct {
		query        string
		name         string
		defaultValue uint
		minValue     uint
		maxValue     uint
		expected     uint
		expectError  bool
	}{
		{"?param=10", "param", 5, 1, 20, 10, false},
		{"?param=0", "param", 5, 1, 20, 0, true},
		{"?param=25", "param", 5, 1, 20, 0, true},
		{"?param=abc", "param", 5, 1, 20, 0, true},
		{"", "param", 5, 1, 20, 5, false},
	}

	for _, tt := range tests {
		c := app.AcquireCtx(&fasthttp.RequestCtx{})
		c.Request().SetRequestURI(tt.query)

		value, err := GetUintParameterFromQuery(c, tt.name, tt.defaultValue, tt.minValue, tt.maxValue)
		if tt.expectError {
			if err == nil {
				t.Errorf("expected an error but got nil")
			}
		} else {
			if err != nil {
				t.Errorf("expected no error but got %v", err)
			}
			if value != tt.expected {
				t.Errorf("expected value %v but got %v", tt.expected, value)
			}
		}

		app.ReleaseCtx(c)
	}
}

func TestGetJobUUIDFromQuery(t *testing.T) {
	app := fiber.New()

	tests := []struct {
		query       string
		expected    *uuid.UUID
		expectError bool
	}{
		{"?jobuuid=550e8400-e29b-41d4-a716-446655440000", func() *uuid.UUID { u := uuid.MustParse("550e8400-e29b-41d4-a716-446655440000"); return &u }(), false},
		{"?jobuuid=invalid-uuid", nil, true},
		{"", nil, false},
	}

	for _, tt := range tests {
		c := app.AcquireCtx(&fasthttp.RequestCtx{})
		c.Request().SetRequestURI(tt.query)

		value, err := GetJobUUIDFromQuery(c)
		if tt.expectError {
			if err == nil {
				t.Errorf("expected an error but got nil")
			}
		} else {
			if err != nil {
				t.Errorf("expected no error but got %v", err)
			}
			if tt.expected != nil && value != nil && *value != *tt.expected {
				t.Errorf("expected value %v but got %v", *tt.expected, *value)
			}
			if tt.expected == nil && value != nil {
				t.Errorf("expected nil but got %v", *value)
			}
		}

		app.ReleaseCtx(c)
	}
}

func TestGetJobUUIDFromParameter(t *testing.T) {
	app := fiber.New()
	router := app.Get("/api/v1/jobs/:"+constants.JobUuidParam, func(c *fiber.Ctx) error {
		value, err := GetJobUUIDFromParameter(c)
		if err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": err.Error(),
			})
		}

		return c.SendString(value.String())
	})

	if router == nil {
		t.Error("failed to create route")
	}

	tests := []struct {
		description  string
		route        string
		expected     uuid.UUID
		expectedCode int
		expectError  bool
	}{
		{"valid uuid", "/api/v1/jobs/550e8400-e29b-41d4-a716-446655440000", uuid.MustParse("550e8400-e29b-41d4-a716-446655440000"), 200, false},
		{"invalid uuid", "/api/v1/jobs/invalid-uuid", uuid.UUID{}, 400, true},
		{"route not found", "/api/v1/jobs/", uuid.UUID{}, 404, true},
	}

	for _, test := range tests {
		req := httptest.NewRequest("GET", test.route, nil)
		resp, _ := app.Test(req, 1)
		assert.Equalf(t, test.expectedCode, resp.StatusCode, test.description)
		if !test.expectError {
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatalf("Failed to read response body: %v", err)
			}
			assert.Equalf(t, test.expected.String(), string(body), test.description)
		}
	}
}
