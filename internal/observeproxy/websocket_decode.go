package observeproxy

import (
	"encoding/binary"
	"fmt"
)

const (
	maxDecodedWebSocketPayload = 16 << 20
	maxWebSocketMessages       = 4096
	maxWebSocketFrames         = 16384
)

type WebSocketFrameRange struct {
	Offset int64 `json:"offset"`
	Size   int64 `json:"size"`
}

type WebSocketMessage struct {
	Index           int                   `json:"index"`
	Opcode          int                   `json:"opcode"`
	Type            string                `json:"type"`
	Payload         []byte                `json:"payload,omitempty"`
	RawStart        int64                 `json:"rawStart"`
	RawEnd          int64                 `json:"rawEnd"`
	Frames          []WebSocketFrameRange `json:"frames"`
	Complete        bool                  `json:"complete"`
	Compressed      bool                  `json:"compressed"`
	PayloadEncoding string                `json:"payloadEncoding"`
	Error           string                `json:"error,omitempty"`
}

type WebSocketDecode struct {
	Messages               []WebSocketMessage `json:"messages"`
	Complete               bool               `json:"complete"`
	ParsedBytes            int64              `json:"parsedBytes"`
	TrailingBytes          int64              `json:"trailingBytes"`
	Truncated              bool               `json:"truncated"`
	Error                  string             `json:"error,omitempty"`
	CompressionUnsupported bool               `json:"compressionUnsupported"`
}

// DecodeWebSocket interprets an already captured single-direction frame stream.
// It never participates in forwarding. Complete means the supplied byte prefix
// ends on complete messages, not that a connection/model response was complete.
// Callers must also inspect the raw Body's complete/truncated flags.
//
// Masking is removed and continuation fragments are joined. Control frames keep
// their own entries, in first-frame order. Compressed messages retain unmasked
// compressed bytes with an explicit unsupported encoding; no text is fabricated.
func DecodeWebSocket(data []byte) WebSocketDecode {
	out := WebSocketDecode{Messages: []WebSocketMessage{}}
	position, active, payloadBytes, frameCount := 0, -1, 0, 0
	fail := func(message string, truncated bool) WebSocketDecode {
		out.Error = message
		out.Truncated = truncated
		out.ParsedBytes = int64(position)
		out.TrailingBytes = int64(len(data) - position)
		if active >= 0 {
			out.Messages[active].Complete = false
			out.Messages[active].Error = message
		}
		return out
	}
	for position < len(data) {
		if frameCount >= maxWebSocketFrames {
			return fail("WebSocket frame metadata limit reached", true)
		}
		if len(data)-position < 2 {
			return fail("incomplete WebSocket frame header", false)
		}
		start := position
		first, second := data[start], data[start+1]
		fin, compressed, opcode := first&0x80 != 0, first&0x40 != 0, int(first&0x0f)
		if first&0x30 != 0 {
			return fail("unsupported WebSocket RSV2/RSV3 extension", false)
		}
		if opcode != 0 && opcode != 1 && opcode != 2 && opcode != 8 && opcode != 9 && opcode != 10 {
			return fail(fmt.Sprintf("unsupported WebSocket opcode %d", opcode), false)
		}
		control := opcode >= 8
		if control && (!fin || compressed) {
			return fail("invalid fragmented or compressed WebSocket control frame", false)
		}
		if opcode == 0 && (active < 0 || compressed) {
			return fail("unexpected or compressed WebSocket continuation frame", false)
		}
		if (opcode == 1 || opcode == 2) && active >= 0 {
			return fail("new WebSocket data message before previous fragments completed", false)
		}
		if opcode != 0 && len(out.Messages) >= maxWebSocketMessages {
			return fail("WebSocket message limit reached", true)
		}
		headerLength := 2
		length := uint64(second & 0x7f)
		switch length {
		case 126:
			if len(data)-start < 4 {
				return fail("incomplete WebSocket extended length", false)
			}
			length = uint64(binary.BigEndian.Uint16(data[start+2 : start+4]))
			headerLength += 2
			if length < 126 {
				return fail("noncanonical WebSocket frame length", false)
			}
		case 127:
			if len(data)-start < 10 {
				return fail("incomplete WebSocket extended length", false)
			}
			length = binary.BigEndian.Uint64(data[start+2 : start+10])
			headerLength += 8
			if length < 65536 || length>>63 != 0 {
				return fail("invalid WebSocket 64-bit frame length", false)
			}
		}
		if control && length > 125 {
			return fail("oversized WebSocket control frame", false)
		}
		masked := second&0x80 != 0
		maskStart := start + headerLength
		if masked {
			headerLength += 4
		}
		if len(data)-start < headerLength {
			return fail("incomplete WebSocket masking key", false)
		}
		if length > uint64(len(data)-start-headerLength) {
			return fail("incomplete WebSocket frame payload", false)
		}
		if length > uint64(maxDecodedWebSocketPayload-payloadBytes) {
			return fail("WebSocket decoded payload limit reached", true)
		}
		index := active
		if opcode != 0 {
			index = len(out.Messages)
			kind := map[int]string{1: "text", 2: "binary", 8: "close", 9: "ping", 10: "pong"}[opcode]
			message := WebSocketMessage{Index: index, Opcode: opcode, Type: kind, RawStart: int64(start), PayloadEncoding: "identity", Compressed: compressed}
			if compressed {
				message.PayloadEncoding = "permessage-deflate"
				message.Error = "compressed WebSocket payload is retained but not decompressed; extension context is unavailable"
				out.CompressionUnsupported = true
			}
			out.Messages = append(out.Messages, message)
			if !control && !fin {
				active = index
			}
		}
		message := &out.Messages[index]
		payloadStart, payloadEnd := start+headerLength, start+headerLength+int(length)
		previousLength := len(message.Payload)
		message.Payload = append(message.Payload, data[payloadStart:payloadEnd]...)
		if masked {
			for i := 0; i < int(length); i++ {
				message.Payload[previousLength+i] ^= data[maskStart+i%4]
			}
		}
		message.Frames = append(message.Frames, WebSocketFrameRange{Offset: int64(start), Size: int64(headerLength) + int64(length)})
		message.RawEnd = int64(payloadEnd)
		message.Complete = fin
		payloadBytes += int(length)
		frameCount++
		position = payloadEnd
		if opcode == 0 && fin {
			active = -1
		}
	}
	out.ParsedBytes = int64(position)
	if active >= 0 {
		return fail("incomplete WebSocket fragmented message", false)
	}
	out.Complete = true
	return out
}
