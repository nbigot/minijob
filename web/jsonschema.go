package web

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"reflect"
	"sort"
	"strings"

	"github.com/qri-io/jsonschema"
)

// Workarounds for bugs of the JSON schema validator, qri-io/jsonschema v0.2.1 (its last
// version).

// refGuardKey is the context key of the references being followed by one validation.
type refGuardKey struct{}

type refGuard struct {
	followed []*followedRef
}

// followedRef is a reference being followed on the value at location.
type followedRef struct {
	ref      *loopSafeRef
	location string
	loops    bool // the schema it leads to was already being evaluated on the value
}

// withRefGuard returns the context of one validation.
func withRefGuard(ctx context.Context) context.Context {
	return context.WithValue(ctx, refGuardKey{}, &refGuard{})
}

// loopSafeRef is the "$ref" keyword of the validator, except that a reference is not
// followed again while it is being followed on the same value. The validator followed it
// until the stack overflowed, a fatal error that stopped the server: {"$ref": "#"}, a
// "$defs" entry that refers to itself... A recursion through "properties" or "items"
// still works: the value changes at each step.
//
// When a reference comes back, the reference followed last led to a schema that was already
// being evaluated on the value: that reference adds no constraint, so what was evaluated
// through it (errors, evaluated properties) is undone, as if it had been skipped.
type loopSafeRef struct {
	jsonschema.Ref
}

func newLoopSafeRef() jsonschema.Keyword {
	return new(loopSafeRef)
}

// ValidateKeyword implements the Keyword interface for loopSafeRef
func (r *loopSafeRef) ValidateKeyword(ctx context.Context, currentState *jsonschema.ValidationState, data interface{}) {
	guard, _ := ctx.Value(refGuardKey{}).(*refGuard)
	if guard == nil {
		r.validateRef(ctx, currentState, data)
		return
	}
	location := currentState.InstanceLocation.String()
	for _, f := range guard.followed {
		if f.ref == r && f.location == location {
			guard.followed[len(guard.followed)-1].loops = true
			return
		}
	}

	before := saveState(currentState)
	f := &followedRef{ref: r, location: location}
	guard.followed = append(guard.followed, f)
	r.validateRef(ctx, currentState, data)
	guard.followed = guard.followed[:len(guard.followed)-1]
	if f.loops {
		before.restore(currentState)
	}
}

// validateRef runs the "$ref" keyword of the validator (see failUnresolvedRef).
func (r *loopSafeRef) validateRef(ctx context.Context, currentState *jsonschema.ValidationState, data interface{}) {
	defer failUnresolvedRef(&r.Ref, currentState, data)
	r.Ref.ValidateKeyword(ctx, currentState, data)
}

// safeRecursiveRef is the "$recursiveRef" keyword of the validator (see failUnresolvedRef).
type safeRecursiveRef struct {
	jsonschema.RecursiveRef
}

func newSafeRecursiveRef() jsonschema.Keyword {
	return new(safeRecursiveRef)
}

// ValidateKeyword implements the Keyword interface for safeRecursiveRef
func (r *safeRecursiveRef) ValidateKeyword(ctx context.Context, currentState *jsonschema.ValidationState, data interface{}) {
	defer failUnresolvedRef(&r.RecursiveRef, currentState, data)
	r.RecursiveRef.ValidateKeyword(ctx, currentState, data)
}

// failUnresolvedRef is deferred by the "$ref" and "$recursiveRef" keywords. The validator
// panics when it resolves a pointer to a missing "patternProperties" entry
// ("#/patternProperties/x"), which answered 500 to every job with the property: the
// reference now fails like the other references that do not resolve ("#/$defs/x"), with
// the same errors. A panic once the reference is resolved goes on.
//
// ref is the *jsonschema.Ref or *jsonschema.RecursiveRef: once the validator has found the
// schema of a reference, it keeps it in its unexported field "resolved".
func failUnresolvedRef(ref interface{}, currentState *jsonschema.ValidationState, data interface{}) {
	p := recover()
	if p == nil {
		return
	}
	fields := reflect.ValueOf(ref).Elem()
	if !fields.FieldByName("resolved").IsNil() {
		panic(p)
	}
	currentState.AddError(data, "failed to resolve schema for ref "+fields.FieldByName("reference").String())
	subState := currentState.NewSubState()
	subState.ClearState()
	subState.AddError(data, "schema is nil")
	currentState.UpdateEvaluatedPropsAndItems(subState)
}

// savedState is what following a reference changes in the validation state.
type savedState struct {
	errors                    int
	evaluated, localEvaluated map[string]bool
	lastIndex, localLastIndex int
}

func saveState(st *jsonschema.ValidationState) savedState {
	return savedState{
		errors:         len(*st.Errs),
		evaluated:      maps.Clone(*st.EvaluatedPropertyNames),
		localEvaluated: maps.Clone(*st.LocalEvaluatedPropertyNames),
		lastIndex:      st.LastEvaluatedIndex,
		localLastIndex: st.LocalLastEvaluatedIndex,
	}
}

func (s savedState) restore(st *jsonschema.ValidationState) {
	*st.Errs = (*st.Errs)[:s.errors]
	// the sets may be shared with other states: they are restored in place
	restoreSet(*st.EvaluatedPropertyNames, s.evaluated)
	restoreSet(*st.LocalEvaluatedPropertyNames, s.localEvaluated)
	st.LastEvaluatedIndex = s.lastIndex
	st.LocalLastEvaluatedIndex = s.localLastIndex
}

func restoreSet(set, saved map[string]bool) {
	maps.DeleteFunc(set, func(key string, _ bool) bool {
		_, found := saved[key]
		return !found
	})
	maps.Copy(set, saved)
}

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
