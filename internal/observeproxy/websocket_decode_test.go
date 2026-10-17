package observeproxy

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"
)

func websocketFrame(opcode byte, fin, masked bool, payload []byte) []byte {
	first := opcode
	if fin {
		first |= 0x80
	}
	frame := []byte{first, 0}
	switch {
	case len(payload) < 126:
		frame[1] = byte(len(payload))
	case len(payload) <= 65535:
		frame[1] = 126
		frame = binary.BigEndian.AppendUint16(frame, uint16(len(payload)))
	default:
		frame[1] = 127
		frame = binary.BigEndian.AppendUint64(frame, uint64(len(payload)))
	}
	mask := []byte{0x11, 0x22, 0x33, 0x44}
	if masked {
		frame[1] |= 0x80
		frame = append(frame, mask...)
	}
	start := len(frame)
	frame = append(frame, payload...)
	if masked {
		for i := range payload {
			frame[start+i] ^= mask[i%4]
		}
	}
	return frame
}

func TestDecodeWebSocketUnmasksJoinsFragmentsAndKeepsControls(t *testing.T) {
	first := websocketFrame(1, false, true, []byte(`{"type":"response.`))
	ping := websocketFrame(9, true, true, []byte("ping"))
	last := websocketFrame(0, true, true, []byte(`create","number":9007199254740993}`))
	closeFrame := websocketFrame(8, true, true, []byte{3, 232})
	raw := bytes.Join([][]byte{first, ping, last, closeFrame}, nil)
	original := append([]byte(nil), raw...)
	got := DecodeWebSocket(raw)
	if !got.Complete || got.Error != "" || got.ParsedBytes != int64(len(raw)) || got.TrailingBytes != 0 || len(got.Messages) != 3 {
		t.Fatalf("decode failed: %+v", got)
	}
	message := got.Messages[0]
	if string(message.Payload) != `{"type":"response.create","number":9007199254740993}` || message.Type != "text" || !message.Complete || len(message.Frames) != 2 {
		t.Fatalf("fragment payload changed: %+v", message)
	}
	if message.RawStart != 0 || message.RawEnd != int64(len(first)+len(ping)+len(last)) || message.Frames[1].Offset != int64(len(first)+len(ping)) {
		t.Fatalf("raw evidence offsets wrong: %+v", message)
	}
	if got.Messages[1].Type != "ping" || string(got.Messages[1].Payload) != "ping" || got.Messages[2].Type != "close" {
		t.Fatal("control payload mixed into text")
	}
	if !bytes.Equal(raw, original) {
		t.Fatal("decoder mutated wire evidence")
	}
}

func TestDecodeWebSocketBinaryExtendedLengths(t *testing.T) {
	for _, size := range []int{0, 125, 126, 65535, 65536} {
		payload := bytes.Repeat([]byte{0, 255, 1, 128}, size/4+1)[:size]
		got := DecodeWebSocket(websocketFrame(2, true, false, payload))
		if !got.Complete || len(got.Messages) != 1 || !bytes.Equal(got.Messages[0].Payload, payload) || got.Messages[0].Type != "binary" {
			t.Fatalf("size %d: %+v", size, got)
		}
	}
}

func TestDecodeWebSocketCompressionIsExplicitAndNotFabricatedText(t *testing.T) {
	raw := websocketFrame(1, true, true, []byte{0xaa, 0x56, 0x2a, 0, 0xff})
	raw[0] |= 0x40
	got := DecodeWebSocket(raw)
	if !got.Complete || !got.CompressionUnsupported || len(got.Messages) != 1 {
		t.Fatalf("compressed frame lost: %+v", got)
	}
	message := got.Messages[0]
	if !message.Compressed || message.PayloadEncoding != "permessage-deflate" || message.Error == "" || !bytes.Equal(message.Payload, []byte{0xaa, 0x56, 0x2a, 0, 0xff}) {
		t.Fatalf("compressed bytes mistaken for text: %+v", message)
	}
}

func TestDecodeWebSocketIncompleteAndMalformedInputs(t *testing.T) {
	valid := websocketFrame(1, true, false, []byte("okay"))
	tests := map[string][]byte{
		"short header":            append(append([]byte(nil), valid...), 0x81),
		"short payload":           websocketFrame(1, true, true, []byte("payload"))[:8],
		"unfinished fragments":    websocketFrame(1, false, true, []byte("partial")),
		"stray continuation":      websocketFrame(0, true, false, []byte("stray")),
		"fragmented ping":         websocketFrame(9, false, false, nil),
		"large control":           websocketFrame(9, true, false, bytes.Repeat([]byte("x"), 126)),
		"unknown opcode":          websocketFrame(3, true, false, nil),
		"bad 64 bit length":       {0x81, 127, 0x80, 0, 0, 0, 0, 0, 0, 0},
		"noncanonical length":     {0x81, 126, 0, 1, 'x'},
		"new data amid fragments": append(websocketFrame(1, false, false, []byte("a")), websocketFrame(1, true, false, []byte("b"))...),
	}
	for name, raw := range tests {
		t.Run(name, func(t *testing.T) {
			got := DecodeWebSocket(raw)
			if got.Complete || got.Error == "" {
				t.Fatalf("malformed bytes reported complete: %+v", got)
			}
		})
	}
}

func TestDecodeWebSocketBoundsMessagesAndPayload(t *testing.T) {
	emptyFrame := websocketFrame(1, true, false, nil)
	got := DecodeWebSocket(bytes.Repeat(emptyFrame, maxWebSocketMessages+1))
	if got.Complete || !got.Truncated || len(got.Messages) != maxWebSocketMessages || !strings.Contains(got.Error, "message limit") {
		t.Fatalf("message bound missing: count=%d error=%s", len(got.Messages), got.Error)
	}
	payload := bytes.Repeat([]byte{'x'}, maxDecodedWebSocketPayload)
	raw := append(websocketFrame(2, true, false, payload), websocketFrame(2, true, false, []byte("extra"))...)
	got = DecodeWebSocket(raw)
	if got.Complete || !got.Truncated || len(got.Messages) != 1 || len(got.Messages[0].Payload) != maxDecodedWebSocketPayload || !strings.Contains(got.Error, "payload limit") {
		t.Fatalf("payload bound missing: count=%d error=%s", len(got.Messages), got.Error)
	}
}

func FuzzDecodeWebSocket(f *testing.F) {
	f.Add([]byte{0x81, 0})
	f.Add(websocketFrame(1, true, true, []byte(`{"type":"response.create"}`)))
	f.Fuzz(func(t *testing.T, raw []byte) {
		if len(raw) > 1<<20 {
			t.Skip()
		}
		got := DecodeWebSocket(raw)
		if got.ParsedBytes < 0 || got.ParsedBytes > int64(len(raw)) || got.TrailingBytes < 0 || got.ParsedBytes+got.TrailingBytes != int64(len(raw)) {
			t.Fatalf("invalid offsets: %+v", got)
		}
		if len(got.Messages) > maxWebSocketMessages {
			t.Fatal("message limit exceeded")
		}
	})
}
