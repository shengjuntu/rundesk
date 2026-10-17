package rpc

import (
	"bytes"
	"encoding/json"
)

// Decode extension fields as RawMessage so their types and number literals
// survive journal serialization. No interface{} / float64 conversion is used.
// Field order, whitespace and duplicate object keys are not a wire capture.
func (m *Message) UnmarshalJSON(data []byte) error {
	type knownMessage Message
	var known knownMessage
	if err := json.Unmarshal(data, &known); err != nil {
		return err
	}
	retained, err := retainJSONFields(data, "id", "method", "params", "result", "error")
	if err != nil {
		return err
	}
	known.retained = retained
	*m = Message(known)
	return nil
}

func (m Message) MarshalJSON() ([]byte, error) {
	type knownMessage Message
	return marshalJSONFields(knownMessage(m), m.retained)
}

func (e *Error) UnmarshalJSON(data []byte) error {
	type knownError Error
	var known knownError
	if err := json.Unmarshal(data, &known); err != nil {
		return err
	}
	retained, err := retainJSONFields(data, "code", "message", "data")
	if err != nil {
		return err
	}
	known.retained = retained
	*e = Error(known)
	return nil
}

func (e Error) MarshalJSON() ([]byte, error) {
	type knownError Error
	return marshalJSONFields(knownError(e), e.retained)
}

func retainJSONFields(data []byte, known ...string) (map[string]json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return nil, err
	}
	for _, name := range known {
		// Preserve explicit null for omitempty fields. If the typed field is
		// later assigned a value, marshalJSONFields gives that value priority.
		if !bytes.Equal(bytes.TrimSpace(fields[name]), []byte("null")) {
			delete(fields, name)
		}
	}
	return fields, nil
}

func marshalJSONFields(known any, retained map[string]json.RawMessage) ([]byte, error) {
	encoded, err := json.Marshal(known)
	if err != nil || len(retained) == 0 {
		return encoded, err
	}
	// Build a fresh map rather than changing retained: Message is passed by
	// value to the journal and event callbacks and may be serialized twice.
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &fields); err != nil {
		return nil, err
	}
	for name, value := range retained {
		if _, exists := fields[name]; !exists {
			fields[name] = value
		}
	}
	return json.Marshal(fields)
}
