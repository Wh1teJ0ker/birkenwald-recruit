package platform

import (
	"os"
	"strings"
	"testing"
)

func TestVersionMatchesReleaseFile(t *testing.T) {
	content, err := os.ReadFile("../../VERSION")
	if err != nil {
		t.Fatal(err)
	}
	want := strings.TrimSpace(string(content))
	if Version != want {
		t.Fatalf("platform.Version=%q, VERSION=%q", Version, want)
	}
}
