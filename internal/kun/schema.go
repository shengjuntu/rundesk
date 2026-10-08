package kun

// A bounded, local JSON Schema 2020-12 subset for tool arguments. Unknown
// assertion keywords and remote references are rejected at catalog construction.
// This intentionally does not claim complete JSON Schema implementation.
import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

type argumentSchema struct {
	root     any
	patterns map[string]*regexp.Regexp
}

// Reject duplicate keys, excessive nesting, and trailing input in both schemas
// and arguments. Otherwise validation and the remote parser could see different values.
func strictJSON(raw []byte) (any, error) {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	nodes := 0
	var read func(int) (any, error)
	read = func(depth int) (any, error) {
		nodes++
		if depth > 64 || nodes > 100000 {
			return nil, fmt.Errorf("JSON structure exceeds limits")
		}
		t, err := d.Token()
		if err != nil {
			return nil, err
		}
		if delim, ok := t.(json.Delim); ok {
			switch delim {
			case '{':
				m := map[string]any{}
				for d.More() {
					key, err := d.Token()
					if err != nil {
						return nil, err
					}
					k, ok := key.(string)
					if !ok {
						return nil, fmt.Errorf("invalid object key")
					}
					if _, ok = m[k]; ok {
						return nil, fmt.Errorf("duplicate JSON object key")
					}
					v, err := read(depth + 1)
					if err != nil {
						return nil, err
					}
					m[k] = v
				}
				end, err := d.Token()
				if err != nil || end != json.Delim('}') {
					return nil, fmt.Errorf("invalid object ending")
				}
				return m, nil
			case '[':
				a := []any{}
				for d.More() {
					v, err := read(depth + 1)
					if err != nil {
						return nil, err
					}
					a = append(a, v)
				}
				end, err := d.Token()
				if err != nil || end != json.Delim(']') {
					return nil, fmt.Errorf("invalid array ending")
				}
				return a, nil
			default:
				return nil, fmt.Errorf("unexpected JSON delimiter")
			}
		}
		if _, isNumber := t.(json.Number); isNumber {
			if _, ok := number(t); !ok {
				return nil, fmt.Errorf("JSON number exceeds supported precision/exponent limits")
			}
		}
		return t, nil
	}
	v, err := read(0)
	if err != nil {
		return nil, err
	}
	if _, err = d.Token(); err != io.EOF {
		return nil, fmt.Errorf("trailing JSON input")
	}
	return v, nil
}
func number(v any) (*big.Rat, bool) {
	n, ok := v.(json.Number)
	if !ok {
		return nil, false
	}
	text := string(n)
	if len(text) > 256 {
		return nil, false
	}
	if pos := strings.IndexAny(text, "eE"); pos >= 0 {
		exp, err := strconv.Atoi(text[pos+1:])
		if err != nil || exp > 1024 || exp < -1024 {
			return nil, false
		}
	}
	r, ok := new(big.Rat).SetString(text)
	return r, ok
}
func natural(v any) bool { n, ok := number(v); return ok && n.IsInt() && n.Sign() >= 0 }
func stringSet(v any) ([]string, bool) {
	a, ok := v.([]any)
	if !ok {
		return nil, false
	}
	out := []string{}
	seen := map[string]bool{}
	for _, v := range a {
		s, ok := v.(string)
		if !ok || seen[s] {
			return nil, false
		}
		seen[s] = true
		out = append(out, s)
	}
	return out, true
}
func (s *argumentSchema) ref(ref string) (any, error) {
	if ref == "#" {
		return s.root, nil
	}
	if !strings.HasPrefix(ref, "#/") || strings.Contains(ref, "%") {
		return nil, fmt.Errorf("only local JSON Pointer $ref is supported")
	}
	var v any = s.root
	for _, part := range strings.Split(ref[2:], "/") {
		for i := 0; i < len(part); i++ {
			if part[i] == '~' {
				if i+1 == len(part) || (part[i+1] != '0' && part[i+1] != '1') {
					return nil, fmt.Errorf("invalid JSON Pointer escape")
				}
				i++
			}
		}
		part = strings.ReplaceAll(strings.ReplaceAll(part, "~1", "/"), "~0", "~")
		switch node := v.(type) {
		case map[string]any:
			var ok bool
			v, ok = node[part]
			if !ok {
				return nil, fmt.Errorf("unresolved $ref")
			}
		case []any:
			i, err := strconv.Atoi(part)
			if err != nil || i < 0 || i >= len(node) || strconv.Itoa(i) != part {
				return nil, fmt.Errorf("invalid array $ref")
			}
			v = node[i]
		default:
			return nil, fmt.Errorf("unresolved $ref")
		}
	}
	return v, nil
}
func compileArguments(raw []byte) (*argumentSchema, error) {
	if len(raw) > 65536 {
		return nil, fmt.Errorf("schema exceeds 64 KiB")
	}
	v, err := strictJSON(raw)
	if err != nil {
		return nil, err
	}
	s := &argumentSchema{root: v, patterns: map[string]*regexp.Regexp{}}
	nodes := 0
	refs := []string{}
	schemas := map[string]bool{}
	var check func(any, int) error
	check = func(v any, depth int) error {
		nodes++
		if nodes > 4096 || depth > 64 {
			return fmt.Errorf("schema complexity limit")
		}
		if _, ok := v.(bool); ok {
			return nil
		}
		m, ok := v.(map[string]any)
		if !ok {
			return fmt.Errorf("schema must be an object or boolean")
		}
		encoded, _ := json.Marshal(m)
		schemas[string(encoded)] = true
		for k, v := range m {
			switch k {
			case "$schema":
				if v != "https://json-schema.org/draft/2020-12/schema" && v != "https://json-schema.org/draft/2020-12/schema#" {
					return fmt.Errorf("only the 2020-12 schema dialect is supported")
				}
			case "$ref":
				r, ok := v.(string)
				if !ok {
					return fmt.Errorf("$ref must be a string")
				}
				refs = append(refs, r)
			case "title", "description", "$comment", "format", "default", "examples", "deprecated", "readOnly", "writeOnly": // annotations, not assertions
			case "type":
				types := []string{}
				if str, ok := v.(string); ok {
					types = []string{str}
				} else {
					var ok bool
					types, ok = stringSet(v)
					if !ok {
						return fmt.Errorf("invalid type")
					}
				}
				if len(types) == 0 {
					return fmt.Errorf("empty type")
				}
				for _, t := range types {
					switch t {
					case "object", "array", "string", "number", "integer", "boolean", "null":
					default:
						return fmt.Errorf("unsupported type %s", t)
					}
				}
			case "required":
				if _, ok := stringSet(v); !ok {
					return fmt.Errorf("invalid required")
				}
			case "enum":
				a, ok := v.([]any)
				if !ok || len(a) == 0 {
					return fmt.Errorf("enum must be nonempty")
				}
			case "const":
			case "minimum", "maximum", "exclusiveMinimum", "exclusiveMaximum", "multipleOf":
				n, ok := number(v)
				if !ok || (k == "multipleOf" && n.Sign() <= 0) {
					return fmt.Errorf("invalid numeric constraint %s", k)
				}
			case "minLength", "maxLength", "minItems", "maxItems", "minProperties", "maxProperties", "minContains", "maxContains":
				if !natural(v) {
					return fmt.Errorf("invalid nonnegative integer %s", k)
				}
			case "pattern":
				pat, ok := v.(string)
				if !ok {
					return fmt.Errorf("invalid pattern")
				}
				r, err := regexp.Compile(pat)
				if err != nil {
					return fmt.Errorf("pattern must use supported RE2 syntax: %w", err)
				}
				s.patterns[pat] = r
			case "uniqueItems":
				if _, ok := v.(bool); !ok {
					return fmt.Errorf("invalid uniqueItems")
				}
			case "$defs", "definitions", "properties", "patternProperties", "dependentSchemas":
				children, ok := v.(map[string]any)
				if !ok {
					return fmt.Errorf("%s must be an object", k)
				}
				for name, child := range children {
					if k == "patternProperties" {
						r, err := regexp.Compile(name)
						if err != nil {
							return fmt.Errorf("unsupported property pattern: %w", err)
						}
						s.patterns[name] = r
					}
					if err := check(child, depth+1); err != nil {
						return err
					}
				}
			case "dependentRequired":
				children, ok := v.(map[string]any)
				if !ok {
					return fmt.Errorf("invalid dependentRequired")
				}
				for _, child := range children {
					if _, ok := stringSet(child); !ok {
						return fmt.Errorf("invalid dependentRequired list")
					}
				}
			case "additionalProperties", "items", "contains", "propertyNames", "not", "if", "then", "else":
				if err := check(v, depth+1); err != nil {
					return err
				}
			case "allOf", "anyOf", "oneOf", "prefixItems":
				a, ok := v.([]any)
				if !ok || len(a) == 0 {
					return fmt.Errorf("%s must be a nonempty array", k)
				}
				for _, child := range a {
					if err := check(child, depth+1); err != nil {
						return err
					}
				}
			default:
				return fmt.Errorf("unsupported schema keyword %s", k)
			}
		}
		return nil
	}
	if err = check(v, 0); err != nil {
		return nil, err
	}
	for _, r := range refs {
		target, err := s.ref(r)
		if err != nil {
			return nil, err
		}
		if _, ok := target.(bool); ok {
			continue
		}
		encoded, _ := json.Marshal(target)
		if !schemas[string(encoded)] {
			return nil, fmt.Errorf("$ref must target a supported schema node")
		}
	}
	return s, nil
}
func matchesType(v any, t string) bool {
	switch t {
	case "null":
		return v == nil
	case "object":
		_, ok := v.(map[string]any)
		return ok
	case "array":
		_, ok := v.([]any)
		return ok
	case "string":
		_, ok := v.(string)
		return ok
	case "boolean":
		_, ok := v.(bool)
		return ok
	case "number":
		_, ok := number(v)
		return ok
	case "integer":
		n, ok := number(v)
		return ok && n.IsInt()
	}
	return false
}
func canonical(v any) string {
	if n, ok := number(v); ok {
		return "n:" + n.RatString()
	}
	switch x := v.(type) {
	case []any:
		parts := []string{}
		for _, v := range x {
			parts = append(parts, canonical(v))
		}
		b, _ := json.Marshal(parts)
		return "a:" + string(b)
	case map[string]any:
		parts := map[string]string{}
		for k, v := range x {
			parts[k] = canonical(v)
		}
		b, _ := json.Marshal(parts)
		return "o:" + string(b)
	}
	b, _ := json.Marshal(v)
	return string(b)
}
func (s *argumentSchema) Validate(raw []byte) error {
	if len(raw) > 1<<20 {
		return fmt.Errorf("arguments exceed 1 MiB")
	}
	v, err := strictJSON(raw)
	if err != nil {
		return err
	}
	if _, ok := v.(map[string]any); !ok {
		return fmt.Errorf("arguments must be an object")
	}
	steps := 0
	return s.validate(s.root, v, "$", 0, &steps)
}
func (s *argumentSchema) validate(schema, value any, path string, depth int, steps *int) error {
	*steps++
	if depth > 128 || *steps > 20000 {
		*steps = 20001
		return fmt.Errorf("schema evaluation limit")
	}
	fail := func(k string) error { return fmt.Errorf("%s violates %s", path, k) }
	if b, ok := schema.(bool); ok {
		if b {
			return nil
		}
		return fail("false schema")
	}
	m := schema.(map[string]any)
	test := func(sub, v any, p string) error { return s.validate(sub, v, p, depth+1, steps) }
	if r, ok := m["$ref"].(string); ok {
		target, _ := s.ref(r)
		if err := test(target, value, path); err != nil {
			return err
		}
	}
	if typ, ok := m["type"]; ok {
		valid := false
		if str, ok := typ.(string); ok {
			valid = matchesType(value, str)
		} else {
			for _, t := range typ.([]any) {
				valid = valid || matchesType(value, t.(string))
			}
		}
		if !valid {
			return fail("type")
		}
	}
	if c, ok := m["const"]; ok && canonical(c) != canonical(value) {
		return fail("const")
	}
	if a, ok := m["enum"].([]any); ok {
		found := false
		for _, c := range a {
			found = found || canonical(c) == canonical(value)
		}
		if !found {
			return fail("enum")
		}
	}
	for _, key := range []string{"allOf", "anyOf", "oneOf"} {
		if a, ok := m[key].([]any); ok {
			matched := 0
			for _, sub := range a {
				if test(sub, value, path) == nil {
					matched++
				}
			}
			if (key == "allOf" && matched != len(a)) || (key == "anyOf" && matched == 0) || (key == "oneOf" && matched != 1) {
				return fail(key)
			}
		}
	}
	if sub, ok := m["not"]; ok && test(sub, value, path) == nil {
		return fail("not")
	}
	if sub, ok := m["if"]; ok {
		branch := "else"
		if test(sub, value, path) == nil {
			branch = "then"
		}
		if next, ok := m[branch]; ok {
			if err := test(next, value, path); err != nil {
				return err
			}
		}
	}
	checkLength := func(n int, lo, hi string) error {
		actual := big.NewRat(int64(n), 1)
		if x, ok := number(m[lo]); ok && actual.Cmp(x) < 0 {
			return fail(lo)
		}
		if x, ok := number(m[hi]); ok && actual.Cmp(x) > 0 {
			return fail(hi)
		}
		return nil
	}
	if n, ok := number(value); ok {
		for _, k := range []string{"minimum", "maximum", "exclusiveMinimum", "exclusiveMaximum", "multipleOf"} {
			if bound, ok := number(m[k]); ok {
				cmp := n.Cmp(bound)
				if k == "minimum" && cmp < 0 || k == "maximum" && cmp > 0 || k == "exclusiveMinimum" && cmp <= 0 || k == "exclusiveMaximum" && cmp >= 0 || k == "multipleOf" && !new(big.Rat).Quo(n, bound).IsInt() {
					return fail(k)
				}
			}
		}
	}
	if str, ok := value.(string); ok {
		if err := checkLength(utf8.RuneCountInString(str), "minLength", "maxLength"); err != nil {
			return err
		}
		if pat, ok := m["pattern"].(string); ok && !s.patterns[pat].MatchString(str) {
			return fail("pattern")
		}
	}
	if obj, ok := value.(map[string]any); ok {
		if err := checkLength(len(obj), "minProperties", "maxProperties"); err != nil {
			return err
		}
		if req, ok := m["required"]; ok {
			names, _ := stringSet(req)
			for _, name := range names {
				if _, ok := obj[name]; !ok {
					return fail("required property " + strconv.Quote(name))
				}
			}
		}
		props, _ := m["properties"].(map[string]any)
		patterns, _ := m["patternProperties"].(map[string]any)
		names := []string{}
		for name := range obj {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			v := obj[name]
			p := path + "[" + strconv.Quote(name) + "]"
			matched := false
			if sub, ok := m["propertyNames"]; ok {
				if err := test(sub, name, p); err != nil {
					return err
				}
			}
			if sub, ok := props[name]; ok {
				matched = true
				if err := test(sub, v, p); err != nil {
					return err
				}
			}
			for pat, sub := range patterns {
				*steps++
				if *steps > 20000 {
					return fmt.Errorf("schema evaluation limit")
				}
				if s.patterns[pat].MatchString(name) {
					matched = true
					if err := test(sub, v, p); err != nil {
						return err
					}
				}
			}
			if !matched {
				if sub, ok := m["additionalProperties"]; ok {
					if err := test(sub, v, p); err != nil {
						return err
					}
				}
			}
		}
		if deps, ok := m["dependentRequired"].(map[string]any); ok {
			for key, req := range deps {
				if _, ok := obj[key]; ok {
					names, _ := stringSet(req)
					for _, name := range names {
						if _, ok := obj[name]; !ok {
							return fail("dependentRequired")
						}
					}
				}
			}
		}
		if deps, ok := m["dependentSchemas"].(map[string]any); ok {
			for key, sub := range deps {
				if _, ok := obj[key]; ok {
					if err := test(sub, value, path); err != nil {
						return err
					}
				}
			}
		}
	}
	if arr, ok := value.([]any); ok {
		if err := checkLength(len(arr), "minItems", "maxItems"); err != nil {
			return err
		}
		prefix, _ := m["prefixItems"].([]any)
		seen := map[string]bool{}
		for i, v := range arr {
			var sub any
			has := false
			if i < len(prefix) {
				sub = prefix[i]
				has = true
			} else {
				sub, has = m["items"]
			}
			if has {
				if err := test(sub, v, fmt.Sprintf("%s[%d]", path, i)); err != nil {
					return err
				}
			}
			if m["uniqueItems"] == true {
				k := canonical(v)
				if seen[k] {
					return fail("uniqueItems")
				}
				seen[k] = true
			}
		}
		if sub, ok := m["contains"]; ok {
			count := 0
			for i, v := range arr {
				if test(sub, v, fmt.Sprintf("%s[%d]", path, i)) == nil {
					count++
				}
			}
			if _, ok := m["minContains"]; !ok && count < 1 {
				return fail("contains")
			}
			if err := checkLength(count, "minContains", "maxContains"); err != nil {
				return err
			}
		}
	}
	if *steps > 20000 {
		return fmt.Errorf("schema evaluation limit")
	}
	return nil
}
