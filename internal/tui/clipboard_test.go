package tui

import (
	"encoding/base64"
	"io"
	"os"
	"strings"
	"testing"
)

func TestCopyToClipboardEmitsOSC52WithBase64EncodedText(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	original := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = original }()

	copyToClipboard("BFI1.hemligt-inbjudningstoken")
	w.Close()
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}

	want := "\x1b]52;c;" + base64.StdEncoding.EncodeToString([]byte("BFI1.hemligt-inbjudningstoken")) + "\a"
	if string(out) != want {
		t.Fatalf("fel OSC 52-sekvens:\ngot  %q\nwant %q", out, want)
	}
	if !strings.HasPrefix(string(out), "\x1b]52;c;") {
		t.Fatal("saknar OSC 52-prefix")
	}
}
