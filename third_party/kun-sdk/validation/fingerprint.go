package validation

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	p "github.com/your-org/kun/sdk/protocol"
	"sort"
)

// Fingerprint is the historical Kun digest of a JSON-serializable value.
func Fingerprint(v any) string { return fmt.Sprintf("%x", sha256.Sum256(p.JSON(v))) }

// CatalogFingerprint hashes the authorized catalog, independent of listing order.
func CatalogFingerprint(s p.State) string {
	definitions := make([]string, 0, len(s.ToolDefinitions))
	for _, raw := range s.ToolDefinitions {
		var v any
		_ = json.Unmarshal(raw, &v)
		definitions = append(definitions, Fingerprint(v))
	}
	sort.Strings(definitions)
	tools := make([]string, 0, len(s.MCPTools))
	for _, tool := range s.MCPTools {
		var v any
		_ = json.Unmarshal(p.JSON(tool), &v)
		tools = append(tools, Fingerprint(v))
	}
	sort.Strings(tools)
	return Fingerprint([][]string{definitions, tools})
}
