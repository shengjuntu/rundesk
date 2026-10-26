package debugapi

import (
	"net/url"
	"testing"
)

func TestStrictSelectors(t *testing.T) {
	for _, query := range []string{"kind=llm_request&eventId=1&section=websocket_client_messages&limit=50", "kind=llm_request&eventId=1&section=websocket_upstream_payload&messageIndex=0", "kind=llm_request&eventId=1&section=websocket_client_payload&messageIndex=4095", "kind=llm_requests&through=0&runId=r&limit=50", "kind=llm_request&eventId=1&section=request&offset=2&limit=16000", "kind=llm_request&eventId=1&section=websocket_client_chunks&limit=50", "kind=runs&through=0", "kind=steps&runId=r&query=literal&limit=50", "kind=step&stepId=step-1", "kind=issues&type=mcpToolCall", "kind=statistics&status=failed", "kind=event&eventId=1&through=0", "kind=overview", "kind=run", "kind=events&through=0", "kind=events&after=2&through=3&limit=200", "kind=event&eventId=9007199254740993&offset=2&limit=16000", "kind=diff&fromSequence=2&sequence=1", "kind=snapshot&sequence=1", "kind=evidence&sequence=1"} {
		v, _ := url.ParseQuery(query)
		if _, e := Parse(v); e != nil {
			t.Fatal(query, e)
		}
	}
	for _, query := range []string{"kind=llm_request&eventId=1&section=websocket_client_messages&messageIndex=0", "kind=llm_request&eventId=1&section=websocket_client_payload&messageIndex=4096", "kind=llm_request&eventId=1&section=request&messageIndex=0", "kind=llm_request&eventId=1&section=websocket_client_messages&limit=51", "kind=llm_requests&limit=51", "kind=llm_request", "kind=llm_request&eventId=1&section=invalid", "kind=llm_request&eventId=1&section=request&limit=16001", "kind=llm_request&eventId=1&section=request_chunks&limit=51", "kind=llm_requests&eventId=1", "kind=steps&limit=51", "kind=step", "kind=statistics&offset=0", "kind=runs&runId=x", "kind=steps&sequence=0", "", "kind=shell", "kind=run&kind=run", "kind=overview&sequence=0", "kind=run&fromSequence=0", "kind=events&limit=0", "kind=events&through=null", "kind=event&eventId=1.5", "kind=event&eventId=9223372036854775808", "kind=event&offset=1", "kind=diff&sequence=1", "kind=context&after=0", "kind=events&limit=201", "kind=event&eventId=1&limit=16001", "kind=snapshot&sequence=0"} {
		v, _ := url.ParseQuery(query)
		if _, e := Parse(v); e == nil {
			t.Fatal("accepted", query)
		}
	}
}

func TestUnifiedSelectors(t *testing.T) {
	for _, query := range []string{"kind=observations&through=0&source=codex_app_server&recordKind=tool&limit=200", "kind=observations&after=1&through=2&runId=r", "kind=activity&offset=3&source=kun_sdk&recordKind=model&limit=200"} {
		v, _ := url.ParseQuery(query)
		if _, err := Parse(v); err != nil {
			t.Fatal(query, err)
		}
	}
	for _, query := range []string{"kind=observations&offset=0", "kind=activity&after=0", "kind=activity&sequence=0", "kind=observations&source=bogus", "kind=observations&recordKind=bogus", "kind=observations&after=1&through=0", "kind=observations&limit=201", "kind=activity&limit=0", "kind=events&source=host"} {
		v, _ := url.ParseQuery(query)
		if _, err := Parse(v); err == nil {
			t.Fatal("accepted", query)
		}
	}
}

func TestTranscriptSelectors(t *testing.T) {
	for _, query := range []string{"kind=transcript&through=0&limit=50", "kind=transcript&runId=r&offset=3&limit=40"} {
		values, _ := url.ParseQuery(query)
		if _, err := Parse(values); err != nil {
			t.Fatal(query, err)
		}
	}
	for _, query := range []string{"kind=transcript&source=host", "kind=transcript&after=0", "kind=transcript&limit=51", "kind=transcript&limit=0", "kind=transcript&query=secret", "kind=transcript&offset=0&offset=1"} {
		values, _ := url.ParseQuery(query)
		if _, err := Parse(values); err == nil {
			t.Fatal("accepted", query)
		}
	}
}
