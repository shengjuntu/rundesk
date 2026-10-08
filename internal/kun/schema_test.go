package kun

import (
	"testing"
)

func TestSchemaAssertionsAndExactNumbers(t *testing.T) {
	schema := `{"type":"object","properties":{"count":{"type":"integer","minimum":1,"maximum":4},"amount":{"type":"number","multipleOf":0.1},"name":{"type":"string","minLength":2,"maxLength":4,"pattern":"^[a-z中]+$"},"tags":{"type":"array","items":{"enum":["a","b"]},"uniqueItems":true}},"required":["count","amount","name"],"additionalProperties":false}`
	s, err := compileArguments([]byte(schema))
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{`{"count":2.0,"amount":0.3,"name":"中中"}`, `{"count":1e0,"amount":1.2,"name":"abc","tags":["a","b"]}`} {
		if err := s.Validate([]byte(raw)); err != nil {
			t.Fatal(raw, err)
		}
	}
	for _, raw := range []string{
		`{"count":1.2,"amount":0.3,"name":"ab"}`, `{"count":5,"amount":0.3,"name":"ab"}`,
		`{"count":2,"amount":0.31,"name":"ab"}`, `{"count":2,"amount":0.3,"name":"a"}`,
		`{"count":2,"amount":0.3,"name":"AB"}`, `{"count":2,"amount":0.3,"name":"ab","extra":true}`,
		`{"amount":0.3,"name":"ab"}`, `{"count":2,"amount":0.3,"name":"ab","tags":["a","a"]}`,
		`{"count":2,"count":3,"amount":0.3,"name":"ab"}`, `{"count":2,"amount":1e999999999,"name":"ab"}`,
		`null`, `[]`, `{} {}`,
	} {
		if err := s.Validate([]byte(raw)); err == nil {
			t.Fatal("accepted invalid arguments", raw)
		}
	}
}
func TestSchemaCompositionLocalRefsAndBounds(t *testing.T) {
	s, err := compileArguments([]byte(`{"type":"object","$defs":{"name":{"anyOf":[{"type":"null"},{"type":"string","minLength":2}]}},"properties":{"name":{"$ref":"#/$defs/name"},"tuple":{"type":"array","prefixItems":[{"const":"start"}],"items":{"type":"integer"},"contains":{"const":2},"minContains":1,"maxContains":1}},"required":["name"],"if":{"properties":{"name":{"type":"string"}}},"then":{"required":["tuple"]}}`))
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{`{"name":null}`, `{"name":"ok","tuple":["start",2,3]}`} {
		if err := s.Validate([]byte(raw)); err != nil {
			t.Fatal(err)
		}
	}
	for _, raw := range []string{`{"name":"x"}`, `{"name":"ok"}`, `{"name":"ok","tuple":["wrong",2]}`, `{"name":"ok","tuple":["start",2,2]}`} {
		if s.Validate([]byte(raw)) == nil {
			t.Fatal(raw)
		}
	}
	for _, raw := range []string{`{"$ref":"https://example.com/schema"}`, `{"unevaluatedProperties":false}`, `{"type":"unknown"}`, `{"$ref":"#/$defs/missing"}`, `{"type":"object","typo":true}`, `{"pattern":"(?=a)"}`, `{"$schema":"http://json-schema.org/draft-07/schema#"}`, `{"required":["x","x"]}`, `{"minimum":1e99999999}`} {
		if _, err := compileArguments([]byte(raw)); err == nil {
			t.Fatal("unsupported schema accepted", raw)
		}
	}
	loop, err := compileArguments([]byte(`{"$ref":"#"}`))
	if err != nil {
		t.Fatal(err)
	}
	if loop.Validate([]byte(`{}`)) == nil {
		t.Fatal("recursive schema exceeded evaluator bound without rejection")
	}
	// Negation must not turn evaluator exhaustion into a successful match.
	loop, err = compileArguments([]byte(`{"not":{"$ref":"#"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if loop.Validate([]byte(`{}`)) == nil {
		t.Fatal("not swallowed recursive evaluation limit")
	}
}
func TestSchemaObjectDependenciesAndAlternatives(t *testing.T) {
	s, err := compileArguments([]byte(`{"type":"object","propertyNames":{"pattern":"^[a-z]+$"},"patternProperties":{"^x":{"type":"integer"}},"dependentRequired":{"x":["y"]},"dependentSchemas":{"y":{"properties":{"y":{"type":"string"}}}},"oneOf":[{"required":["x"]},{"required":["z"]}],"allOf":[{"not":{"required":["bad"]}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Validate([]byte(`{"x":1,"y":"yes"}`)); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{`{"x":1}`, `{"x":1,"y":3}`, `{"x":1,"y":"yes","z":1}`, `{"z":1,"bad":false}`, `{"z":1,"UPPER":2}`, `{"z":1,"xyz":"bad"}`} {
		if s.Validate([]byte(raw)) == nil {
			t.Fatal(raw)
		}
	}
	unique, err := compileArguments([]byte(`{"properties":{"a":{"type":"array","uniqueItems":true}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if unique.Validate([]byte(`{"a":[{"n":1},{"n":1.0}]}`)) == nil {
		t.Fatal("semantic numeric duplicate accepted")
	}
}
