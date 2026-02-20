package gateway

import "testing"

func TestFunnelAnalyticsStoreRecordAndSummary(t *testing.T) {
	store := NewFunnelAnalyticsStore()

	if _, ok := store.Record(funnelEventInput{Event: "unknown_event"}, "127.0.0.1"); ok {
		t.Fatalf("expected unknown event to be rejected")
	}

	if _, ok := store.Record(funnelEventInput{Event: "landing_view", PagePath: "/"}, "127.0.0.1"); !ok {
		t.Fatalf("expected landing_view to be accepted")
	}
	if _, ok := store.Record(funnelEventInput{Event: "download_click", Platform: "macos"}, "127.0.0.1"); !ok {
		t.Fatalf("expected download_click to be accepted")
	}

	summary := store.Summary()
	totalsRaw, ok := summary["totals"]
	if !ok {
		t.Fatalf("expected totals in summary")
	}
	totals, ok := totalsRaw.(map[string]int)
	if !ok {
		t.Fatalf("expected totals shape map[string]int")
	}
	if totals["landing_view"] != 1 {
		t.Fatalf("expected landing_view count 1, got %d", totals["landing_view"])
	}
	if totals["download_click"] != 1 {
		t.Fatalf("expected download_click count 1, got %d", totals["download_click"])
	}
}
