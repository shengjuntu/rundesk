package tracequery

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/shengjuntu/rundesk/internal/observeproxy"
	"github.com/shengjuntu/rundesk/internal/redaction"
)

// Observation queries operate directly on the indexed journal without loading
// a trajectory projection. Raw bodies are byte-paged; WebSocket message views
// use bounded decoding. Invalid UTF-8 and large JSON integers remain exact in
// the retained bytes.
const observationMethod = "llm/exchange"
const observationMetadata = `json_remove(data, '$.request.body.bytes', '$.request.body.chunks', '$.response.body.bytes', '$.response.body.chunks', '$.websocket.toUpstream.bytes', '$.websocket.toUpstream.chunks', '$.websocket.toClient.bytes', '$.websocket.toClient.chunks')`
const observationScope = ` FROM events WHERE session=? AND id<=? AND method='llm/exchange'`

func observationDecode(raw []byte) (map[string]any, error) {
	var value map[string]any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	if value == nil {
		return nil, errors.New("invalid observation metadata")
	}
	return value, nil
}
func (r *Reader) observation(ctx context.Context, name string, a Args) (any, error) {
	if a.Offset < 0 || a.Limit < 0 || utf8.RuneCountInString(a.RunID) > 256 {
		return nil, errors.New("invalid observation selectors")
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	scope := observationScope
	params := []any{r.SessionID, r.Through}
	if a.RunID != "" {
		scope += ` AND json_extract(data,'$.labels.runId')=?`
		params = append(params, a.RunID)
	}
	if a.MessageIndex != nil && (*a.MessageIndex < 0 || *a.MessageIndex > 4095 || a.Section != "websocket_upstream_payload" && a.Section != "websocket_client_payload") {
		return nil, errors.New("messageIndex requires a WebSocket payload section and range0..4095")
	}
	if name == "trace_list_llm_requests" {
		return r.listObservations(ctx, scope, params, a)
	}
	if a.EventID <= 0 || a.Limit > 16000 {
		return nil, errors.New("positive eventId and at most 16000 bytes required")
	}
	scope += ` AND id=?`
	params = append(params, a.EventID)
	var exists int
	if err := r.db.QueryRowContext(ctx, `SELECT count(*)`+scope, params...).Scan(&exists); err != nil {
		return nil, err
	}
	if exists != 1 {
		return nil, errors.New("model request is outside the selected session, run or snapshot")
	}
	section := a.Section
	if section == "" {
		section = "metadata"
	}
	if section == "metadata" {
		if a.Offset != 0 {
			return nil, errors.New("metadata does not accept a nonzero offset")
		}
		var size int64
		if err := r.db.QueryRowContext(ctx, `SELECT length(CAST(`+observationMetadata+` AS BLOB))`+scope, params...).Scan(&size); err != nil {
			return nil, err
		}
		if size > 512<<10 {
			return nil, errors.New("observation metadata exceeds 512 KiB query bound")
		}
		var raw []byte
		var reqChunks, resChunks, upChunks, clientChunks int
		err := r.db.QueryRowContext(ctx, `SELECT `+observationMetadata+`,coalesce(json_array_length(data,'$.request.body.chunks'),0),coalesce(json_array_length(data,'$.response.body.chunks'),0),coalesce(json_array_length(data,'$.websocket.toUpstream.chunks'),0),coalesce(json_array_length(data,'$.websocket.toClient.chunks'),0)`+scope, params...).Scan(&raw, &reqChunks, &resChunks, &upChunks, &clientChunks)
		if err != nil {
			return nil, err
		}
		result, err := observationDecode(raw)
		if err != nil {
			return nil, err
		}
		result = obj(redaction.Fields(result))
		for _, v := range []struct {
			path  []string
			count int
		}{{[]string{"request", "body"}, reqChunks}, {[]string{"response", "body"}, resChunks}, {[]string{"websocket", "toUpstream"}, upChunks}, {[]string{"websocket", "toClient"}, clientChunks}} {
			current := result
			for _, key := range v.path {
				next, ok := current[key].(map[string]any)
				if !ok {
					current = nil
					break
				}
				current = next
			}
			if current != nil {
				current["chunkCount"] = v.count
			}
		}
		result["eventId"] = a.EventID
		result["sessionId"] = r.SessionID
		result["snapshotThrough"] = r.Through
		result["section"] = section
		result["bodyEncoding"] = "base64; byte offsets; use request/response or websocket sections"
		result["chunkEncoding"] = "ordered observer read/write segments, not HTTP, SSE or WebSocket message boundaries"
		return result, nil
	}
	chunks := strings.HasSuffix(section, "_chunks")
	messages := strings.HasSuffix(section, "_messages")
	payload := strings.HasSuffix(section, "_payload")
	bodySection := strings.TrimSuffix(strings.TrimSuffix(strings.TrimSuffix(section, "_chunks"), "_messages"), "_payload")
	paths := map[string]string{"request": "$.request.body", "response": "$.response.body", "websocket_upstream": "$.websocket.toUpstream", "websocket_client": "$.websocket.toClient"}
	path, ok := paths[bodySection]
	if !ok {
		return nil, errors.New("unsupported model request section")
	}
	var bodyExists int
	// Paths are selected only from the constant allowlist above, never user SQL.
	if err := r.db.QueryRowContext(ctx, `SELECT json_type(data,'`+path+`') IS NOT NULL`+scope, params...).Scan(&bodyExists); err != nil {
		return nil, err
	}
	if bodyExists == 0 {
		return nil, errors.New("requested observation section was not captured")
	}
	if messages || payload {
		return r.observationWebSocket(ctx, scope, params, path, section, a, payload)
	}
	if chunks {
		return r.observationChunks(ctx, scope, params, path, section, a)
	}
	return r.observationBody(ctx, scope, params, path, section, a)
}

func capturedLengthExpr(path string) string {
	value := `coalesce(json_extract(data,'` + path + `.bytes'),'')`
	return `(length(` + value + `)/4*3-CASE WHEN substr(` + value + `,-2)='==' THEN 2 WHEN substr(` + value + `,-1)='=' THEN 1 ELSE 0 END)`
}
func (r *Reader) listObservations(ctx context.Context, scope string, params []any, a Args) (any, error) {
	if a.Limit > 50 {
		return nil, errors.New("at most 50 model requests per page")
	}
	limit := a.Limit
	if limit == 0 {
		limit = 25
	}
	var total int
	if err := r.db.QueryRowContext(ctx, `SELECT count(*)`+scope, params...).Scan(&total); err != nil {
		return nil, err
	}
	offset := a.Offset
	if offset > total {
		offset = total
	}
	query := `SELECT json_object('eventId',id,'id',json_extract(data,'$.id'),'runId',json_extract(data,'$.labels.runId'),'association',json_extract(data,'$.association'),'method',json_extract(data,'$.request.method'),'url',json_extract(data,'$.request.url'),'statusCode',json_extract(data,'$.response.status'),'started',json_extract(data,'$.started'),'ended',json_extract(data,'$.ended'),'requestBytes',json_extract(data,'$.request.body.size'),'responseBytes',json_extract(data,'$.response.body.size'),'requestCapturedBytes',` + capturedLengthExpr("$.request.body") + `,'responseCapturedBytes',` + capturedLengthExpr("$.response.body") + `,'incomplete',coalesce(json_extract(data,'$.incomplete'),0),'requestComplete',coalesce(json_extract(data,'$.request.body.complete'),0),'responseComplete',coalesce(json_extract(data,'$.response.body.complete'),0),'truncated',coalesce(json_extract(data,'$.request.body.truncated'),0) OR coalesce(json_extract(data,'$.response.body.truncated'),0) OR coalesce(json_extract(data,'$.websocket.toUpstream.truncated'),0) OR coalesce(json_extract(data,'$.websocket.toClient.truncated'),0),'redacted',coalesce(json_array_length(data,'$.request.redactedHeaders'),0)>0 OR coalesce(json_array_length(data,'$.response.redactedHeaders'),0)>0,'error',json_extract(data,'$.error'),'captureLoss',json_extract(data,'$.captureLoss'),'websocket',json_type(data,'$.websocket') IS NOT NULL)` + scope + ` ORDER BY id LIMIT ? OFFSET ?`
	args := append(append([]any{}, params...), limit, offset)
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	requests := []map[string]any{}
	for rows.Next() {
		var raw []byte
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		value, e := observationDecode(raw)
		if e != nil {
			return nil, e
		}
		for _, key := range []string{"incomplete", "requestComplete", "responseComplete", "truncated", "redacted", "websocket"} {
			value[key] = fmt.Sprint(value[key]) == "1"
		}
		value["complete"] = !value["incomplete"].(bool) && value["requestComplete"].(bool) && value["responseComplete"].(bool)
		start, e1 := time.Parse(time.RFC3339Nano, str(value["started"]))
		end, e2 := time.Parse(time.RFC3339Nano, str(value["ended"]))
		if e1 == nil && e2 == nil {
			value["durationMs"] = end.Sub(start).Milliseconds()
		}
		requests = append(requests, value)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	next := offset + len(requests)
	return map[string]any{"requests": requests, "total": total, "offset": offset, "nextOffset": next, "hasMore": next < total, "sessionId": r.SessionID, "snapshotThrough": r.Through, "runId": a.RunID, "coverage": "retained completed observations only; absence does not prove no upstream request"}, nil
}
func (r *Reader) observationBody(ctx context.Context, scope string, params []any, path, section string, a Args) (any, error) {
	var total, complete, truncated, redacted, incomplete int
	var bodyError string
	query := `SELECT ` + capturedLengthExpr(path) + `,coalesce(json_extract(data,'` + path + `.complete'),0),coalesce(json_extract(data,'` + path + `.truncated'),0),coalesce(json_array_length(data,'$.request.redactedHeaders'),0)>0 OR coalesce(json_array_length(data,'$.response.redactedHeaders'),0)>0,coalesce(json_extract(data,'$.incomplete'),0),coalesce(json_extract(data,'` + path + `.error'),'')` + scope
	if err := r.db.QueryRowContext(ctx, query, params...).Scan(&total, &complete, &truncated, &redacted, &incomplete, &bodyError); err != nil {
		return nil, err
	}
	if total < 0 || a.Offset > total {
		return nil, errors.New("offset exceeds retained body bytes")
	}
	limit := a.Limit
	if limit == 0 {
		limit = 4000
	}
	end := a.Offset + limit
	if end > total {
		end = total
	}
	// Start/end on base64 block boundaries, then slice decoded bytes exactly.
	from := a.Offset / 3 * 4
	through := (end + 2) / 3 * 4
	var encoded string
	args := []any{from + 1, through - from}
	args = append(args, params...)
	if err := r.db.QueryRowContext(ctx, `SELECT substr(coalesce(json_extract(data,'`+path+`.bytes'),''),?,?)`+scope, args...).Scan(&encoded); err != nil {
		return nil, err
	}
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, errors.New("retained observation body has invalid base64")
	}
	skip := a.Offset % 3
	if end == a.Offset {
		decoded = []byte{}
	} else if skip+end-a.Offset > len(decoded) {
		return nil, errors.New("retained body length mismatch")
	} else {
		decoded = decoded[skip : skip+end-a.Offset]
	}
	return map[string]any{"eventId": a.EventID, "section": section, "bodyBase64": base64.StdEncoding.EncodeToString(decoded), "text": strings.ToValidUTF8(string(decoded), "�"), "encoding": "base64; byte offsets", "textEncoding": "UTF-8 display only; partial multibyte sequences and invalid bytes are replaced; concatenate base64-decoded bytes for exact content", "offset": a.Offset, "nextOffset": end, "totalBytes": total, "hasMore": end < total, "complete": complete == 1, "truncated": truncated == 1, "redacted": false, "headersRedacted": redacted == 1, "incomplete": incomplete == 1, "error": bodyError, "sessionId": r.SessionID, "snapshotThrough": r.Through}, nil
}
func (r *Reader) observationChunks(ctx context.Context, scope string, params []any, path, section string, a Args) (any, error) {
	if a.Limit > 50 {
		return nil, errors.New("at most 50 observation chunks per page")
	}
	limit := a.Limit
	if limit == 0 {
		limit = 50
	}
	var total, truncated int
	if err := r.db.QueryRowContext(ctx, `SELECT coalesce(json_array_length(data,'`+path+`.chunks'),0),coalesce(json_extract(data,'`+path+`.chunkMetadataTruncated'),0)`+scope, params...).Scan(&total, &truncated); err != nil {
		return nil, err
	}
	if a.Offset > total {
		return nil, errors.New("offset exceeds retained chunk metadata")
	}
	args := append(append([]any{}, params...), limit, a.Offset)
	rows, err := r.db.QueryContext(ctx, `SELECT value FROM json_each((SELECT json_extract(data,'`+path+`.chunks')`+scope+`)) ORDER BY CAST(key AS INTEGER) LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := []map[string]any{}
	for rows.Next() {
		var raw []byte
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		v, e := observationDecode(raw)
		if e != nil {
			return nil, e
		}
		values = append(values, v)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	next := a.Offset + len(values)
	return map[string]any{"eventId": a.EventID, "section": section, "chunks": values, "offset": a.Offset, "nextOffset": next, "total": total, "hasMore": next < total, "chunkMetadataTruncated": truncated == 1, "encoding": "observer read/write segments in recorded order; not protocol frame or message boundaries", "sessionId": r.SessionID, "snapshotThrough": r.Through}, nil
}

// WebSocket decoding is exclusively a read-side view over retained frame bytes.
// It never changes traffic, storage or association labels. Parsing is bounded
// separately from raw byte pagination, which remains available for larger data.
func (r *Reader) observationWebSocket(ctx context.Context, scope string, params []any, path, section string, a Args, payload bool) (any, error) {
	if path != "$.websocket.toUpstream" && path != "$.websocket.toClient" {
		return nil, errors.New("decoded messages require a WebSocket direction")
	}
	if !payload && a.Limit > 50 {
		return nil, errors.New("at most50 WebSocket messages per page")
	}
	var total, complete, truncated, incomplete int
	if err := r.db.QueryRowContext(ctx, `SELECT `+capturedLengthExpr(path)+`,coalesce(json_extract(data,'`+path+`.complete'),0),coalesce(json_extract(data,'`+path+`.truncated'),0),coalesce(json_extract(data,'$.incomplete'),0)`+scope, params...).Scan(&total, &complete, &truncated, &incomplete); err != nil {
		return nil, err
	}
	if total > 32<<20 {
		return nil, errors.New("WebSocket read-side decoding exceeds32MiB; use raw frame byte pages")
	}
	var encoded string
	if err := r.db.QueryRowContext(ctx, `SELECT coalesce(json_extract(data,'`+path+`.bytes'),'')`+scope, params...).Scan(&encoded); err != nil {
		return nil, err
	}
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, errors.New("retained WebSocket body has invalid base64")
	}
	decoded := observeproxy.DecodeWebSocket(raw)
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	decodeMeta := map[string]any{"complete": decoded.Complete, "parsedBytes": decoded.ParsedBytes, "trailingBytes": decoded.TrailingBytes, "truncated": decoded.Truncated, "error": decoded.Error, "compressionUnsupported": decoded.CompressionUnsupported}
	common := map[string]any{"eventId": a.EventID, "section": section, "sessionId": r.SessionID, "snapshotThrough": r.Through, "decode": decodeMeta, "captureComplete": complete == 1, "captureTruncated": truncated == 1, "incomplete": incomplete == 1 || complete != 1 || !decoded.Complete, "truncated": truncated == 1 || decoded.Truncated, "readSideDerived": true}
	if !payload {
		offset := a.Offset
		if offset > len(decoded.Messages) {
			offset = len(decoded.Messages)
		}
		limit := a.Limit
		if limit == 0 {
			limit = 50
		}
		end := offset + limit
		if end > len(decoded.Messages) {
			end = len(decoded.Messages)
		}
		messages := []map[string]any{}
		for _, message := range decoded.Messages[offset:end] {
			messages = append(messages, map[string]any{"index": message.Index, "opcode": message.Opcode, "type": message.Type, "rawStart": message.RawStart, "rawEnd": message.RawEnd, "frames": message.Frames, "complete": message.Complete, "compressed": message.Compressed, "payloadEncoding": message.PayloadEncoding, "error": message.Error, "payloadBytes": len(message.Payload), "payloadAvailable": true})
		}
		common["messages"] = messages
		common["total"] = len(decoded.Messages)
		common["offset"] = offset
		common["nextOffset"] = end
		common["hasMore"] = end < len(decoded.Messages)
		common["encoding"] = "derived WebSocket messages in first-frame order; control frames remain separate; compressed payloads are not decompressed"
		return common, nil
	}
	index := 0
	if a.MessageIndex != nil {
		index = *a.MessageIndex
	}
	if index < 0 || index >= len(decoded.Messages) {
		return nil, errors.New("messageIndex is outside retained decoded WebSocket messages")
	}
	message := decoded.Messages[index]
	if a.Offset > len(message.Payload) {
		return nil, errors.New("offset exceeds retained message payload bytes")
	}
	limit := a.Limit
	if limit == 0 {
		limit = 4000
	}
	end := a.Offset + limit
	if end > len(message.Payload) {
		end = len(message.Payload)
	}
	part := message.Payload[a.Offset:end]
	text := ""
	textEncoding := "not text; inspect exact base64-decoded payload bytes"
	if message.Type == "text" && !message.Compressed {
		text = strings.ToValidUTF8(string(part), "�")
		textEncoding = "UTF-8 display only; concatenate base64-decoded bytes before decoding text"
	}
	if message.Compressed {
		textEncoding = "permessage-deflate bytes retained; decompression with extension context is unsupported"
	}
	common["messageIndex"] = message.Index
	common["opcode"] = message.Opcode
	common["type"] = message.Type
	common["compressed"] = message.Compressed
	common["payloadEncoding"] = message.PayloadEncoding
	common["bodyBase64"] = base64.StdEncoding.EncodeToString(part)
	common["text"] = text
	common["textEncoding"] = textEncoding
	common["encoding"] = "base64; byte offsets in derived unmasked message payload"
	common["offset"] = a.Offset
	common["nextOffset"] = end
	common["totalBytes"] = len(message.Payload)
	common["hasMore"] = end < len(message.Payload)
	common["complete"] = message.Complete
	common["redacted"] = false
	common["error"] = message.Error
	return common, nil
}
