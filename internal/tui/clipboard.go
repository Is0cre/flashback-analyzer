package tui

import (
	"encoding/base64"
	"os"
)

// copyToClipboard sets the system clipboard via the OSC 52 terminal
// escape sequence, instead of shelling out to a platform clipboard tool
// (xclip/wl-copy/pbcopy/clip). OSC 52 is interpreted by the terminal
// emulator on the user's own machine, not the shell BACKFLASH runs in —
// so it works identically whether BACKFLASH is running locally or on the
// far end of an SSH session, with no "which tool is available on this
// box" branching needed. Best-effort: does nothing visible in terminals
// that don't support it (or have it disabled), same as playAlarm's bell.
func copyToClipboard(text string) {
	encoded := base64.StdEncoding.EncodeToString([]byte(text))
	os.Stdout.WriteString("\x1b]52;c;" + encoded + "\a")
}
