package kun

import "github.com/your-org/kun/sdk/validation"

// Public argument validation is shared with hosts through the lightweight SDK.
type argumentSchema = validation.ArgumentSchema

func compileArguments(raw []byte) (*argumentSchema, error) { return validation.CompileArguments(raw) }
func strictJSON(raw []byte) (any, error)                   { return validation.StrictJSON(raw) }
