package app

import (
	"bytes"
	"testing"
)

func pbField(field int, payload []byte) []byte {
	var out []byte
	key := uint64(field<<3 | 2)
	for _, v := range []uint64{key, uint64(len(payload))} {
		for v >= 0x80 {
			out = append(out, byte(v)|0x80)
			v >>= 7
		}
		out = append(out, byte(v))
	}
	return append(out, payload...)
}

// Shape measured from a real hNvQHb video download: an envelope holding the prompt,
// the MP4 nested two levels deep, and a "Current time is …" string. The envelope is
// larger than the bare MP4, which is why "keep the largest" returned a broken file.
func TestPickVideoArtifact(t *testing.T) {
	mp4 := append([]byte{0, 0, 0, 0x20, 'f', 't', 'y', 'p', 'i', 's', 'o', 'm'}, bytes.Repeat([]byte{7}, 4096)...)
	env := append(pbField(1, pbField(2, []byte("A red fox walking through fresh snow"))),
		pbField(1, pbField(1, pbField(3, mp4)))...)
	env = append(env, pbField(3, pbField(2, []byte("Current time is Friday")))...)

	got := pickVideoArtifact([]MediaArtifact{{Mime: "video/mp4", Data: env}, {Mime: "video/mp4", Data: mp4}})
	if !bytes.Equal(got.Data, mp4) {
		t.Fatalf("bare MP4 not preferred: got %d bytes", len(got.Data))
	}
	got = pickVideoArtifact([]MediaArtifact{{Mime: "application/octet-stream", Data: env}})
	if !bytes.Equal(got.Data, mp4) || got.Mime != "video/mp4" {
		t.Fatalf("envelope not unwrapped: got %d bytes %s", len(got.Data), got.Mime)
	}
	junk := []byte("<html>nope</html>")
	if got = pickVideoArtifact([]MediaArtifact{{Data: junk}}); !bytes.Equal(got.Data, junk) {
		t.Fatal("non-video fallback changed")
	}
}
