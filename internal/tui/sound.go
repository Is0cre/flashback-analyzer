package tui

import (
	"embed"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"time"
)

//go:embed assets/sounds
var soundFS embed.FS

// Notification sounds. Each one is tied to a specific, infrequent event —
// never to routine list scrolling or typing — so audio stays a signal, not
// background noise. Set BACKFLASH_MUTE=1 to disable all of them.
const (
	soundIncomingMessage = "assets/sounds/mixkit-digital-quick-tone-2866.wav" // remote message arrives in a channel
	soundPeerConnected   = "assets/sounds/PEERED_W_NODE.wav"                  // a new Yggdrasil mesh peer connects
	soundConfirmation    = "assets/sounds/mixkit-sci-fi-confirmation-914.wav" // GANDR vault unlocked
	soundContactAdded    = "assets/sounds/mixkit-doorbell-tone-2864.wav"      // a new contact is saved
)

// soundDir caches the directory that embedded sounds are extracted into
// (lazily, on first play) so each file is written to disk at most once per
// process rather than on every notification.
var (
	soundDirOnce sync.Once
	soundDirPath string
	soundPaths   sync.Map // asset path -> extracted file path
)

// playSound fires a notification sound in the background, best-effort. Any
// failure (muted, no audio device, unsupported platform, missing player
// binary) is silently ignored — a sound effect must never interrupt or
// error out the TUI.
func playSound(asset string) {
	if os.Getenv("BACKFLASH_MUTE") != "" {
		return
	}
	go func() {
		path, err := extractSound(asset)
		if err != nil {
			return
		}
		cmd := playerCommand(path)
		if cmd == nil {
			return
		}
		_ = cmd.Run()
	}()
}

// playAlarm rings the terminal's own bell (ASCII BEL) a few times, for the
// police-proximity alert. Unlike playSound this needs no audio player or
// embedded asset — the terminal itself renders it (as an audible beep,
// title-bar flash, or dock/taskbar badge, depending on the terminal's own
// settings) — so it works even on a machine with no speakers, no known
// player binary, or the CLI player lookups playerCommand relies on.
func playAlarm() {
	if os.Getenv("BACKFLASH_MUTE") != "" {
		return
	}
	go func() {
		for i := 0; i < 3; i++ {
			os.Stdout.WriteString("\a")
			if i < 2 {
				time.Sleep(300 * time.Millisecond)
			}
		}
	}()
}

func extractSound(asset string) (string, error) {
	if cached, ok := soundPaths.Load(asset); ok {
		return cached.(string), nil
	}
	soundDirOnce.Do(func() {
		dir, err := os.MkdirTemp("", "backflash-sounds")
		if err != nil {
			return
		}
		soundDirPath = dir
	})
	if soundDirPath == "" {
		return "", os.ErrNotExist
	}
	data, err := soundFS.ReadFile(asset)
	if err != nil {
		return "", err
	}
	dest := filepath.Join(soundDirPath, filepath.Base(asset))
	if err := os.WriteFile(dest, data, 0o600); err != nil {
		return "", err
	}
	soundPaths.Store(asset, dest)
	return dest, nil
}

// playerCommand shells out to whatever native audio player each platform
// already has, instead of adding a cross-platform audio library: this
// project builds with CGO_ENABLED=0 for six OS/arch combinations, and most
// pure-Go audio playback libraries either need CGO on at least one of those
// or add a large dependency for what is here a rare, best-effort sound
// effect.
func playerCommand(path string) *exec.Cmd {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("afplay", path)
	case "windows":
		script := "(New-Object Media.SoundPlayer '" + path + "').PlaySync();"
		return exec.Command("powershell", "-NoProfile", "-Command", script)
	default: // linux and other unix-likes
		for _, player := range []string{"paplay", "aplay", "ffplay"} {
			if _, err := exec.LookPath(player); err == nil {
				if player == "ffplay" {
					return exec.Command(player, "-nodisp", "-autoexit", "-loglevel", "quiet", path)
				}
				return exec.Command(player, path)
			}
		}
		return nil
	}
}
