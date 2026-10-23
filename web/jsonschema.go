package web

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/qri-io/jsonschema"
)

// Workarounds for bugs of the JSON schema validator, qri-io/jsonschema v0.2.1 (its last
// version).

// checkSchema returns an error for the parts of a schema that make the validator panic
// when it validates a body, instead of rejecting the body:
//   - an empty or null "enum", an empty "type" list: the message saying that a value does
//     not match cannot be built (slice bounds out of range);
//   - null instead of a schema in "properties", "patternProperties", "$defs",
//     "definitions", "allOf", "anyOf", "oneOf" or "items" (nil pointer dereference);
//   - a plain-name fragment "$id" ("#name"): it is put in a map that only exists once a
//     schema with an "$anchor" was registered, so it works only below a root schema with
//     an "$anchor".
func checkSchema(schemaBytes []byte) error {
	var root interface{}
	if err := json.Unmarshal(schemaBytes, &root); err != nil {
		return err
	}
	rootObject, _ := root.(map[string]interface{})
	_, rootAnchor := rootObject["$anchor"]
	return checkSubschema(root, "", rootAnchor)
}

func checkSubschema(value interface{}, path string, rootAnchor bool) error {
	schema, ok := value.(map[string]interface{})
	if !ok {
		// true, false, or null where one schema is expected (no constraint)
		return nil
	}
	for _, key := range sortedKeys(schema) {
		value := schema[key]
		keyPath := path + "/" + escapePointerToken(key)
		switch key {
		case "enum":
			if list, _ := value.([]interface{}); len(list) == 0 {
				return fmt.Errorf("%s: the enum is empty or null", keyPath)
			}
		case "type":
			if list, ok := value.([]interface{}); ok && len(list) == 0 {
				return fmt.Errorf("%s: the type list is empty", keyPath)
			}
		case "$id":
			if id, _ := value.(string); jsonschema.IsLocalSchemaID(id) && (path == "" || !rootAnchor) {
				return fmt.Errorf(`%s: plain-name fragment %q, use "$anchor": %q instead`, keyPath, id, id[1:])
			}
		case "properties", "patternProperties", "$defs", "definitions", "dependentSchemas":
			members, _ := value.(map[string]interface{})
			for _, name := range sortedKeys(members) {
				memberPath := keyPath + "/" + escapePointerToken(name)
				if members[name] == nil && key != "dependentSchemas" {
					return fmt.Errorf("%s: null instead of a schema", memberPath)
				}
				if err := checkSubschema(members[name], memberPath, rootAnchor); err != nil {
					return err
				}
			}
		case "allOf", "anyOf", "oneOf", "items":
			list, isList := value.([]interface{})
			if !isList {
				// "items" with one schema
				if err := checkSubschema(value, keyPath, rootAnchor); err != nil {
					return err
				}
			}
			for i, element := range list {
				elementPath := fmt.Sprintf("%s/%d", keyPath, i)
				if element == nil {
					return fmt.Errorf("%s: null instead of a schema", elementPath)
				}
				if err := checkSubschema(element, elementPath, rootAnchor); err != nil {
					return err
				}
			}
		case "not", "if", "then", "else", "contains", "propertyNames", "additionalProperties",
			"additionalItems", "unevaluatedProperties", "unevaluatedItems":
			if err := checkSubschema(value, keyPath, rootAnchor); err != nil {
				return err
			}
		}
	}
	return nil
}

func sortedKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

var pointerTokenEscaper = strings.NewReplacer("~", "~0", "/", "~1")

// escapePointerToken escapes a key for a JSON pointer.
func escapePointerToken(key string) string {
	return pointerTokenEscaper.Replace(key)
}
