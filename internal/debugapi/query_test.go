package debugapi

import (
	"net/url"
	"testing"
)

func TestStrictSelectors(t *testing.T) {
	for _, query := range []string{"kind=overview", "kind=run", "kind=events&through=0", "kind=events&after=2&through=3&limit=200", "kind=event&eventId=9007199254740993&offset=2&limit=16000", "kind=diff&fromSequence=2&sequence=1", "kind=snapshot&sequence=1", "kind=evidence&sequence=1"} {
		v, _ := url.ParseQuery(query)
		if _, e := Parse(v); e != nil {
			t.Fatal(query, e)
		}
	}
	for _, query := range []string{"", "kind=shell", "kind=run&kind=run", "kind=overview&sequence=0", "kind=run&fromSequence=0", "kind=events&limit=0", "kind=events&through=null", "kind=event&eventId=1.5", "kind=event&eventId=9223372036854775808", "kind=event&offset=1", "kind=diff&sequence=1", "kind=context&after=0", "kind=events&limit=201", "kind=event&eventId=1&limit=16001", "kind=snapshot&sequence=0"} {
		v, _ := url.ParseQuery(query)
		if _, e := Parse(v); e == nil {
			t.Fatal("accepted", query)
		}
	}
}
