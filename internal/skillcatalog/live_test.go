package skillcatalog

import (
	"context"
	"os"
	"testing"
	"time"
)

// Explicit opt-in: normal tests neither use network nor consume market quotas.
func TestLivePublicSearch(t *testing.T) {
	if os.Getenv("RUNDESK_SKILL_LIVE") != "1" {
		t.Skip("set RUNDESK_SKILL_LIVE=1 for public API verification")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	r, err := NewClient().Search(ctx, "pdf", DefaultSources)
	if err != nil {
		t.Fatal(err)
	}
	for _, provider := range r.Providers {
		t.Logf("source=%s status=%s count=%d message=%s", provider.Source, provider.Status, provider.Count, provider.Message)
	}
	if !r.Complete || len(r.Items) == 0 {
		t.Fatalf("live search incomplete: %#v", r.Providers)
	}
}

func TestLiveImmutableDownloads(t *testing.T) {
	if os.Getenv("RUNDESK_SKILL_LIVE") != "1" {
		t.Skip("set RUNDESK_SKILL_LIVE=1 for public source verification")
	}
	for _, target := range []string{"https://github.com/vercel-labs/agent-skills/tree/main/skills/web-design-guidelines", "https://clawhub.ai/steipete/skills/weather"} {
		t.Run(target, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			bundle, err := NewClient().Fetch(ctx, target)
			if err != nil {
				t.Fatal(err)
			}
			if bundle.Revision == "" || len(bundle.Assets) == 0 {
				t.Fatal("source identity or content missing")
			}
			t.Logf("source=%s revision=%s files=%d", bundle.SourceURL, bundle.Revision, len(bundle.Assets))
		})
	}
}
