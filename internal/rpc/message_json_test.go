package rpc

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"
)

func decodedJSON(t *testing.T, data []byte) any {
	t.Helper()
	var value any
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		t.Fatal(err)
	}
	return value
}

func TestMessagePreservesNestedArgumentsAndExtensions(t *testing.T) {
	input := []byte(`{
		"jsonrpc":"2.0", "id":9007199254740993,
		"method":"item/started",
		"params":{"item":{"type":"mcpToolCall","arguments":{
			"nested":{"count":9007199254740993,"ratio":1.2300e-20},
			"array":[null,false,{"label":"中文🙂","values":[1,2,3]}]
		}}},
		"error":null,
		"extension":{"sequence":18446744073709551615,"flags":[true,null]}
	}`)
	var message Message
	if err := json.Unmarshal(input, &message); err != nil {
		t.Fatal(err)
	}
	// Repeated serialization must neither mutate the captured extensions nor
	// reduce the precision or structure of the original tool arguments.
	for i := 0; i < 2; i++ {
		encoded, err := json.Marshal(message)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(decodedJSON(t, input), decodedJSON(t, encoded)) {
			t.Fatalf("protocol fields changed during capture: %s", encoded)
		}
	}
}

func TestMessagePreservesErrorAndResponseExtensions(t *testing.T) {
	input := []byte(`{"id":"request-1","result":null,"error":{"code":-32000,"message":"failed","data":{"attempt":9007199254740993},"retryAfter":{"value":9007199254740995}},"serverRevision":9007199254740997}`)
	var message Message
	if err := json.Unmarshal(input, &message); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(message)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(decodedJSON(t, input), decodedJSON(t, encoded)) {
		t.Fatalf("response fields changed: %s", encoded)
	}
	if message.Error == nil || message.Error.Error() != "RPC -32000: failed" {
		t.Fatalf("typed RPC error unavailable: %#v", message.Error)
	}
}

func TestMessageEditsOverrideCapturedFields(t *testing.T) {
	var message Message
	if err := json.Unmarshal([]byte(`{"method":"before","params":{"old":true},"error":null,"extension":{"n":9007199254740993}}`), &message); err != nil {
		t.Fatal(err)
	}
	message.Method = "after"
	message.Params = json.RawMessage(`{"replacement":[1,null,false]}`)
	message.Error = &Error{Code: -32601, Message: "new error"}
	encoded, err := json.Marshal(message)
	if err != nil {
		t.Fatal(err)
	}
	expected := []byte(`{"method":"after","params":{"replacement":[1,null,false]},"error":{"code":-32601,"message":"new error"},"extension":{"n":9007199254740993}}`)
	if !reflect.DeepEqual(decodedJSON(t, expected), decodedJSON(t, encoded)) {
		t.Fatalf("typed fields did not override captured fields: %s", encoded)
	}
}

func TestConstructedMessageSerializationUnchanged(t *testing.T) {
	message := Message{ID: json.RawMessage(`42`), Method: "echo", Params: json.RawMessage(`{"n":9007199254740993}`)}
	encoded, err := json.Marshal(message)
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) != `{"id":42,"method":"echo","params":{"n":9007199254740993}}` {
		t.Fatalf("constructed message changed: %s", encoded)
	}
}
