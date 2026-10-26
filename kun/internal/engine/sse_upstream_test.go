// SPDX-FileCopyrightText: Copyright Hewlett Packard Enterprise Development LP
// SPDX-FileCopyrightText: 2025 Mario Zechner
// SPDX-License-Identifier: MIT
// Selected PiG regression test at 827932db70e545b34c3f1d9c04f58a70eacd3b28.
// Adaptation: package/imports only. See docs/UPSTREAM.md.
package kun

import (
	"strings"
	"testing"
)

func TestSSEDecoderAcceptsSSELineEndingsAndDispatchesAtEOF(t *testing.T) {
	decoder := newSSEDecoder(strings.NewReader(": keepalive\rid: ignored\rdata:first\r\ndata: second\n\nevent:done\rdata:last"))
	if !decoder.Next() {
		t.Fatalf("first event missing: %v", decoder.Err())
	}
	first := decoder.Event()
	if first.Event != "" || first.Data != "first\nsecond" {
		t.Fatalf("first event = %#v", first)
	}
	if !decoder.Next() {
		t.Fatalf("EOF event missing: %v", decoder.Err())
	}
	second := decoder.Event()
	if second.Event != "done" || second.Data != "last" {
		t.Fatalf("second event = %#v", second)
	}
	next := decoder.Next()
	if next || decoder.Err() != nil {
		t.Fatalf("decoder terminal state: next=%t err=%v", next, decoder.Err())
	}
}
