package app

import (
	"os"
	"testing"
)

// A video request is acknowledged with keyed-object frames only ("I'm generating
// your video…"), no inner[4] text. It must count as accepted, not as an empty
// 1155 refusal — retrying it queues a duplicate video and burns daily quota.
func TestVideoAckIsAccepted(t *testing.T) {
	raw, err := os.ReadFile("testdata/video_ack_raw.txt")
	if err != nil {
		t.Fatal(err)
	}
	if hasContentFrame(string(raw)) {
		t.Fatal("fixture should have no text content frame")
	}
	if got := extractConversationID(string(raw)); got != "c_0a840dff0f2e13b1" {
		t.Fatalf("conversation id = %q", got)
	}
	if !videoAccepted(toolVideo, string(raw)) {
		t.Fatal("video ack must be treated as accepted")
	}
	if videoAccepted(toolImage, string(raw)) || videoAccepted(toolVideo, ")]}'\n\n25\n[[\"e\",7,null,null,965]]") {
		t.Fatal("only video responses with a conversation id are accepted")
	}
}
