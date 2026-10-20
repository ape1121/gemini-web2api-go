package app

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"mime/multipart"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// 1x1 PNG
var tinyPNG, _ = base64.StdEncoding.DecodeString("iVBORw0KGRgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==")

func TestInputReferenceSrc(t *testing.T) {
	cases := map[string]any{
		"data:image/png;base64,AAA": "data:image/png;base64,AAA",
		"https://x/a.png":           map[string]any{"image_url": "https://x/a.png"},
		"https://x/b.png":           map[string]any{"image_url": map[string]any{"url": "https://x/b.png"}},
		"https://x/c.png":           map[string]any{"url": "https://x/c.png"},
		"":                          nil,
	}
	for want, in := range cases {
		if got := inputReferenceSrc(in); got != want {
			t.Errorf("inputReferenceSrc(%v) = %q, want %q", in, got, want)
		}
	}
}

func TestCreateVideoAcceptsInputReference(t *testing.T) {
	dataURL := "data:image/png;base64," + base64.StdEncoding.EncodeToString(tinyPNG)

	// JSON body with a data URL
	body, _ := json.Marshal(map[string]any{"model": "gemini-video", "prompt": "animate", "input_reference": dataURL})
	w := httptest.NewRecorder()
	handleCreateVideo(w, httptest.NewRequest("POST", "/v1/videos", bytes.NewReader(body)))
	if w.Code != 200 {
		t.Fatalf("json input_reference: %d %s", w.Code, w.Body)
	}

	// multipart (OpenAI SDK shape)
	var mp bytes.Buffer
	mw := multipart.NewWriter(&mp)
	_ = mw.WriteField("model", "gemini-video")
	_ = mw.WriteField("prompt", "animate")
	fw, _ := mw.CreateFormFile("input_reference", "start.png")
	_, _ = fw.Write(tinyPNG)
	_ = mw.Close()
	req := httptest.NewRequest("POST", "/v1/videos", &mp)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	w = httptest.NewRecorder()
	handleCreateVideo(w, req)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"video_`) {
		t.Fatalf("multipart input_reference: %d %s", w.Code, w.Body)
	}

	// unusable reference fails the POST, not the job
	body, _ = json.Marshal(map[string]any{"prompt": "animate", "input_reference": "ftp://nope"})
	w = httptest.NewRecorder()
	handleCreateVideo(w, httptest.NewRequest("POST", "/v1/videos", bytes.NewReader(body)))
	if w.Code != 400 {
		t.Fatalf("bad input_reference should be 400, got %d", w.Code)
	}
}

func TestVideoSize(t *testing.T) {
	for in, want := range map[string]string{"": "1280x720", "16:9": "1280x720", "720x1280": "720x1280", "9:16": "720x1280", "portrait": "720x1280"} {
		got, err := videoSize(map[string]any{"size": in})
		if err != nil || got != want {
			t.Errorf("size %q -> %q %v, want %q", in, got, err, want)
		}
	}
	if got, _ := videoSize(map[string]any{"aspect_ratio": "9:16"}); got != "720x1280" {
		t.Errorf("aspect_ratio 9:16 -> %q", got)
	}
	if _, err := videoSize(map[string]any{"size": "1:1"}); err == nil {
		t.Error("1:1 should be rejected")
	}
}

func TestHistoryTurnScopesToRid(t *testing.T) {
	turnA := []any{[]any{"c_1", "r_aaa"}, nil, []any{[]any{"first"}}, []any{[]any{"rc_1111111111", []any{"https://contribution.usercontent.google.com/download?c=A"}}}}
	turnB := []any{[]any{"c_1", "r_bbb"}, nil, []any{[]any{"continue"}}, []any{[]any{"rc_2222222222", []any{"I can create more videos as soon as your limit resets"}}}}
	inner, _ := json.Marshal([]any{[]any{turnB, turnA}})
	env, _ := json.Marshal([]any{[]any{"wrb.fr", "hNvQHb", string(inner)}})
	body := ")]}'\n\n123\n" + string(env) + "\n"
	b, ok := historyTurn(body, "r_aaa")
	if !ok || !strings.Contains(b, "rc_1111111111") || strings.Contains(b, "limit resets") {
		t.Fatalf("turn A not isolated: ok=%v %s", ok, b)
	}
	b, ok = historyTurn(body, "r_bbb")
	if !ok || !strings.Contains(b, "limit resets") || strings.Contains(b, "rc_1111111111") {
		t.Fatalf("turn B not isolated: ok=%v %s", ok, b)
	}
	if _, ok := historyTurn(body, "r_zzz"); ok {
		t.Fatal("unknown rid must not match")
	}
}

func TestExtendRequiresCompletedParentWithConvRef(t *testing.T) {
	p := &videoJob{ID: "video_parent1", Status: "completed", Model: "gemini-video", CreatedAt: time.Now().Unix()}
	putVideoJob(p)
	w := httptest.NewRecorder()
	handleVideoItem(w, httptest.NewRequest("POST", "/v1/videos/video_parent1/extend", strings.NewReader(`{"prompt":"continue"}`)))
	if w.Code != 409 {
		t.Fatalf("parent without conv ref should be 409, got %d %s", w.Code, w.Body)
	}
	w = httptest.NewRecorder()
	handleVideoItem(w, httptest.NewRequest("POST", "/v1/videos/video_nope/extend", strings.NewReader(`{"prompt":"x"}`)))
	if w.Code != 404 {
		t.Fatalf("unknown parent should be 404, got %d", w.Code)
	}
}

func TestMP4Duration(t *testing.T) {
	// minimal mvhd v0: version/flags(4) ctime(4) mtime(4) timescale=1000 duration=10005
	box := []byte("....mvhd\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x03\xe8\x00\x00\x27\x15")
	if d := mp4Duration(box); d < 10.004 || d > 10.006 {
		t.Fatalf("duration = %v", d)
	}
}

func TestTurnReplyTextDetectsRefusal(t *testing.T) {
	// Real hNvQHb body (tokens redacted) where Gemini refused with text, no video tool.
	raw, err := os.ReadFile("testdata/video_refusal_hist.txt")
	if err != nil {
		t.Fatal(err)
	}
	body, ok := historyTurn(string(raw), "r_91174b02aa556b62")
	if !ok {
		t.Fatal("turn not found")
	}
	got, ok := turnReplyText(body)
	if !ok || !strings.Contains(got, "hard time fulfilling") {
		t.Fatalf("refusal text not found: %q %v", got, ok)
	}
	if strings.Contains(body, "video_generation") {
		t.Fatal("fixture should not contain the video tool marker")
	}
	if _, ok := turnReplyText(`[["wrb.fr","hNvQHb","[[[[\"c_1\",\"r_a\"],null,[[\"p\"]]]]]"]]`); ok {
		t.Fatal("turn without reply must report ok=false")
	}
}
