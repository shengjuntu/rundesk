// Package redaction shares structured-field redaction between host views and
// derived debug projections. Free text is not interpreted as credential JSON.
package redaction

import "strings"

func SensitiveField(key string) bool {
	key = strings.NewReplacer("_", "", "-", "").Replace(strings.ToLower(key))
	switch key {
	case "env", "httpheaders", "authorization", "accesstoken", "refreshtoken", "apikey", "password", "bearertoken", "idtoken":
		return true
	}
	return false
}

// Fields edits an independently decoded JSON value in place, preserving
// json.Number values. Redact before producing text previews or comparisons.
func Fields(v any) any {
	switch x := v.(type) {
	case map[string]any:
		for key, value := range x {
			if SensitiveField(key) {
				x[key] = "[redacted]"
			} else {
				x[key] = Fields(value)
			}
		}
	case []any:
		for n, value := range x {
			x[n] = Fields(value)
		}
	}
	return v
}
