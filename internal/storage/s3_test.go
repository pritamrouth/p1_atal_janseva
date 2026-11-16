package storage

import (
	"strings"
	"testing"
	"time"
)

func TestAudioKey(t *testing.T) {
	now := time.Date(2026, 11, 16, 18, 30, 0, 0, time.UTC)
	key := AudioKey("919876543210", "wamid/abc+123", "audio/ogg; codecs=opus", now)
	want := "complaints/audio/919876543210/20261116T183000-wamid_abc_123.ogg"
	if key != want {
		t.Errorf("AudioKey = %q, want %q", key, want)
	}
}

func TestAudioExtension(t *testing.T) {
	cases := map[string]string{
		"audio/ogg; codecs=opus":   ".ogg",
		"audio/mp4":                ".m4a",
		"audio/mpeg":               ".mp3",
		"audio/amr":                ".amr",
		"application/octet-stream": ".bin",
	}
	for mime, want := range cases {
		if got := AudioExtension(mime); got != want {
			t.Errorf("AudioExtension(%q) = %q, want %q", mime, got, want)
		}
	}
}

func TestPublicURL(t *testing.T) {
	u := &Uploader{bucket: "atal-audio", region: "ap-south-1"}
	got := u.PublicURL("complaints/audio/919/a b.ogg")
	want := "https://atal-audio.s3.ap-south-1.amazonaws.com/complaints/audio/919/a%20b.ogg"
	if got != want {
		t.Errorf("PublicURL = %q, want %q", got, want)
	}
	if !strings.Contains(u.PublicURL("x"), "atal-audio") {
		t.Error("public URL missing bucket")
	}
}
