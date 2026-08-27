package tui

import (
	"io"
	"os"
	"testing"
)

func TestPlaySoundRespectsMuteEnvVar(t *testing.T) {
	t.Setenv("BACKFLASH_MUTE", "1")
	if _, alreadyExtracted := soundPaths.Load(soundContactAdded); alreadyExtracted {
		t.Skip("soundContactAdded redan extraherad av ett tidigare test, kan inte mäta detta rent")
	}
	// Muted playSound must return without doing any work at all — no
	// goroutine, no file write — so this is safe to assert synchronously.
	playSound(soundContactAdded)
	if _, extracted := soundPaths.Load(soundContactAdded); extracted {
		t.Fatal("playSound extraherade ett ljud trots BACKFLASH_MUTE=1")
	}
}

func TestPlayAlarmRespectsMuteEnvVar(t *testing.T) {
	t.Setenv("BACKFLASH_MUTE", "1")
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	original := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = original }()

	playAlarm()
	w.Close()
	out, _ := io.ReadAll(r)
	if len(out) != 0 {
		t.Fatalf("playAlarm skrev till stdout trots BACKFLASH_MUTE=1: %q", out)
	}
}

func TestExtractSoundWritesEmbeddedBytesToDisk(t *testing.T) {
	want, err := soundFS.ReadFile(soundConfirmation)
	if err != nil {
		t.Fatal(err)
	}
	path, err := extractSound(soundConfirmation)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(want) != string(got) {
		t.Fatal("extraherad ljudfil matchar inte det inbäddade innehållet")
	}
	// A second call must reuse the cached path rather than re-extracting.
	path2, err := extractSound(soundConfirmation)
	if err != nil {
		t.Fatal(err)
	}
	if path2 != path {
		t.Fatalf("extractSound extraherade samma tillgång två gånger: %q != %q", path, path2)
	}
}

func TestPlayerCommandDoesNotPanicForCurrentPlatform(t *testing.T) {
	// playerCommand may legitimately return nil (no known player installed);
	// it must never panic regardless of what's available on this machine.
	_ = playerCommand("/tmp/does-not-need-to-exist-for-this-check.wav")
}

func TestAllFourWiredSoundsAreValidEmbeddedAssets(t *testing.T) {
	for _, asset := range []string{soundIncomingMessage, soundPeerConnected, soundConfirmation, soundContactAdded} {
		if _, err := soundFS.ReadFile(asset); err != nil {
			t.Errorf("%s är kopplad till en händelse men saknas som inbäddad tillgång: %v", asset, err)
		}
	}
}
