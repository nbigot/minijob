package web

import (
	"errors"
	"html"
	"strings"
	"sync"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/utils"
	"github.com/gofiber/swagger"
)

// maxSwaggerPrefixes bounds the number of X-Forwarded-Prefix values served with their own
// handler; other values use the handler without prefix.
const maxSwaggerPrefixes = 32

// swaggerHandler serves the Swagger UI with gofiber/swagger, around two of its behaviors:
//
//   - it computes the URL prefix of the page once, from the first request (including its
//     X-Forwarded-Prefix header): one handler is kept per forwarded prefix, so that each
//     request gets the page for its own prefix;
//   - it replaces the request path with the file name before serving the file: the path is
//     restored afterwards (the access log reads it), and the 404 answer for an unknown file
//     names the requested path (the library's showed a garbled name).
func swaggerHandler(config ...swagger.Config) fiber.Handler {
	var mu sync.Mutex
	handlers := map[string]fiber.Handler{}
	handlerFor := func(prefix string) fiber.Handler {
		mu.Lock()
		defer mu.Unlock()
		if h, ok := handlers[prefix]; ok {
			return h
		}
		if len(handlers) >= maxSwaggerPrefixes {
			prefix = ""
			if h, ok := handlers[prefix]; ok {
				return h
			}
		}
		h := swagger.New(config...)
		handlers[prefix] = h
		return h
	}
	return func(c *fiber.Ctx) error {
		prefix := strings.Join(c.GetReqHeaders()["X-Forwarded-Prefix"], ",")
		original := utils.CopyString(c.Path())
		err := handlerFor(prefix)(c)
		c.Path(original)
		var fiberErr *fiber.Error
		if errors.As(err, &fiberErr) && fiberErr.Code == fiber.StatusNotFound {
			return fiber.NewError(fiber.StatusNotFound, "Cannot "+c.Method()+" "+html.EscapeString(original))
		}
		return err
	}
}
