package main

import (
	"bytes"
	"encoding/json"
	k "github.com/shengjuntu/rundesk/internal/kun"
	p "github.com/shengjuntu/rundesk/internal/kunproto"
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
	if first.Error == "" || second.Error != "" || !bytes.Contains(second.Result, []byte(`"resumeCheckpoint":true`)) {
		t.Fatal(out.String(), first, second)
	}
}
