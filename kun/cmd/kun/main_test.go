package main

import (
	"bytes"
	"encoding/json"
	k "github.com/your-org/kun/internal/engine"
	p "github.com/your-org/kun/sdk/protocol"
	"path/filepath"
	"strings"
	"testing"
)

func TestProtocolHandshakeAndVersionRejection(t *testing.T) {
	engine, err := k.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	requests := string(p.JSON(p.Envelope{ID: "1", Version: 99, Method: "hello"})) + "\n" + string(p.JSON(p.Envelope{ID: "2", Version: p.Version, Method: "hello"})) + "\n"
	var out bytes.Buffer
	if err = serve(engine, strings.NewReader(requests), &out); err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(&out)
	var first, second p.Envelope
	_ = decoder.Decode(&first)
	_ = decoder.Decode(&second)
	var hello p.Hello
	if err = json.Unmarshal(second.Result, &hello); err != nil || hello.Engine != "kun" || hello.EngineVersion != "0.17.0" || hello.ProtocolVersion != 16 || !hello.Capabilities["diagnosticProvider"] || !hello.Capabilities["control"] || !hello.Capabilities["events"] {
		t.Fatal("incorrect engine identity", hello, err)
	}
	if first.Error == "" || second.Error != "" || !bytes.Contains(second.Result, []byte(`"resumeCheckpoint":true`)) {
		t.Fatal(out.String(), first, second)
	}
}

func TestProtocolActualJSONLineSizeBoundary(t *testing.T) {
	engine, err := k.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	prefix := `{"version":16,"id":"boundary","method":"hello","params":{"padding":"`
	suffix := `"}}`
	for _, extra := range []int{0, 1} {
		body := prefix + strings.Repeat("x", p.MaxMessage-len(prefix)-len(suffix)+extra) + suffix
		if len(body) != p.MaxMessage+extra {
			t.Fatal("fixture bounds")
		}
		var out bytes.Buffer
		err = serve(engine, strings.NewReader(body+"\n"), &out)
		if extra == 0 {
			if err != nil {
				t.Fatal("max-sized JSON message was rejected", err)
			}
			var envelope p.Envelope
			if decodeErr := json.Unmarshal(out.Bytes(), &envelope); decodeErr != nil || envelope.ID != "boundary" || envelope.Error != "" {
				t.Fatal("max-sized handshake failed", envelope, decodeErr)
			}
		} else if err == nil {
			t.Fatal("oversized JSON message accepted")
		}
	}
}
