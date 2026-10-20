package app

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

func turnBody(turns ...[]any) string {
	var ts []any
	for _, t := range turns {
		ts = append(ts, t)
	}
	inner, _ := json.Marshal([]any{ts})
	env, _ := json.Marshal([]any{[]any{"wrb.fr", "hNvQHb", string(inner)}})
	return ")]}'\n\n1\n" + string(env) + "\n"
}

func TestClassifyVideoTurns(t *testing.T) {
	refused := []any{[]any{"c_1", "r_2"}, nil, []any{[]any{"p"}}, []any{[]any{"rc_22222222",
		[]any{"I can create more videos as soon as your limit resets. Check your usage in Settings."},
		map[string]any{"60": []any{"video_generation", []any{1792119910, 281161000}, "User is out of video generation quota (Aureus)"}}}},
		[]any{1792103308, 60831000}}
	text := []any{[]any{"c_1", "r_3"}, nil, []any{[]any{"hi"}}, []any{[]any{"rc_33333333", []any{"hello"}}}, []any{1792103000, 0}}
	evs := classifyVideoTurns(turnBody(refused, text))
	if len(evs) != 1 {
		t.Fatalf("want 1 video turn, got %d: %+v", len(evs), evs)
	}
	if evs[0].Kind != "quota_refused" || evs[0].At != 1792103308 || evs[0].ResetAt != 1792119910 {
		t.Fatalf("bad refusal event: %+v", evs[0])
	}

	// Real ready-video history captured earlier (tokens intact only in the local
	// debug copy); skip when it isn't around.
	if raw, err := os.ReadFile("../../../debug/probe/hist_c_0a840dff0f2e13b1.txt"); err == nil {
		evs := classifyVideoTurns(string(raw))
		if len(evs) != 1 || evs[0].Kind != "video" || evs[0].At == 0 {
			t.Fatalf("ready video not classified: %+v", evs)
		}
	}
}

func TestSummarizeQuota(t *testing.T) {
	now := time.Unix(1792110000, 0)
	h := int64(3600)
	// 3 videos, then a refusal that carries Google's reset time.
	q := accountQuota{Events: []quotaEvent{
		{At: now.Unix() - 3*h, Kind: "video"},
		{At: now.Unix() - 2*h, Kind: "video"},
		{At: now.Unix() - 1*h, Kind: "video"},
		{At: now.Unix() - 30*60, Kind: "quota_refused", ResetAt: now.Unix() + 2*h},
	}}
	summarizeQuota(&q, now)
	if q.Status != "exhausted" || q.ResetAt != now.Unix()+2*h || q.ResetSource != "google" || q.VideosInWin != 3 {
		t.Fatalf("google reset: %+v", q)
	}
	// Refusal without Google's time -> first video in window + 5 h.
	q = accountQuota{Events: []quotaEvent{
		{At: now.Unix() - 4*h, Kind: "video"},
		{At: now.Unix() - 10*60, Kind: "quota_refused"},
	}}
	summarizeQuota(&q, now)
	if q.Status != "exhausted" || q.ResetAt != now.Unix()+h || q.ResetSource != "estimated" {
		t.Fatalf("estimated reset: %+v", q)
	}
	// Reset time already passed -> available again.
	q = accountQuota{Events: []quotaEvent{
		{At: now.Unix() - 7*h, Kind: "video"},
		{At: now.Unix() - 6*h, Kind: "quota_refused", ResetAt: now.Unix() - h},
	}}
	summarizeQuota(&q, now)
	if q.Status != "available" || q.VideosInWin != 0 {
		t.Fatalf("after reset: %+v", q)
	}
	// A successful video after a refusal means the account recovered.
	q = accountQuota{Events: []quotaEvent{
		{At: now.Unix() - 2*h, Kind: "quota_refused", ResetAt: now.Unix() + h},
		{At: now.Unix() - h, Kind: "video"},
	}}
	summarizeQuota(&q, now)
	if q.Status != "available" || !strings.Contains(q.ResetSource, "estimated") {
		t.Fatalf("recovered: %+v", q)
	}
}
