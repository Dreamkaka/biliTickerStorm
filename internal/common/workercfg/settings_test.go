package workercfg

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	iv := 400
	s := Settings{
		PushplusToken: "tok12345",
		Interval:      &iv,
		ProxyList:     "http://u:secret@1.2.3.4:8080",
	}
	ver, err := Save(dir, s)
	if err != nil {
		t.Fatal(err)
	}
	if ver == 0 {
		t.Fatal("version")
	}
	got, ver2, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if ver2 == 0 || got.PushplusToken != "tok12345" || got.Interval == nil || *got.Interval != 400 {
		t.Fatalf("%+v ver=%d", got, ver2)
	}
	masked := got.Masked()
	if masked.PushplusToken == "tok12345" || !containsStars(masked.PushplusToken) {
		t.Fatalf("mask=%s", masked.PushplusToken)
	}
	if filepath.Base(FilePath(dir)) != "worker_settings.json" {
		t.Fatal(FilePath(dir))
	}
	_ = os.Remove(FilePath(dir))
}

func containsStars(s string) bool {
	return len(s) >= 4 && (s[2] == '*' || s[0] == '*')
}
