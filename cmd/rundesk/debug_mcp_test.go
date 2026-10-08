package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestDebugMCPCLIHasNoTokenArgumentOrImplicitCredential(t *testing.T) {
	t.Setenv("RUNDESK_TOKEN", "administrator-must-not-be-inherited")
	t.Setenv("RUNDESK_DEBUG_TOKEN", "")
	var out, errout bytes.Buffer
	if e := runDebugMCP([]string{"--session", "session"}, strings.NewReader(""), &out, &errout); e == nil {
		t.Fatal("inherited administrator credential")
	}
	t.Setenv("RUNDESK_DEBUG_FIXTURE", "fixture-secret")
	input := `{"jsonrpc":"2.0","id":1,"method":"ping"}` + "\n"
	if e := runDebugMCP([]string{"--session", "session", "--token-env", "RUNDESK_DEBUG_FIXTURE"}, strings.NewReader(input), &out, &errout); e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(out.String(), `"result":{}`) || strings.Contains(out.String(), "fixture-secret") {
		t.Fatal(out.String())
	}
	for _, args := range [][]string{{"--token", "fixture-secret"}, {"--session", "session", "--token-env", "bad-name"}, {"--session", "session", "unexpected"}} {
		if e := runDebugMCP(args, strings.NewReader(""), &out, &errout); e == nil {
			t.Fatal("invalid CLI arguments accepted")
		}
	}
}
