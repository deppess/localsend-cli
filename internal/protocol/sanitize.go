package protocol

import (
	"strings"
	"unicode"
)

// MaxAliasRunes caps the length of an attacker-controlled DeviceInfo.Alias
// before it is stored, compared, or rendered.
const MaxAliasRunes = 128

// SanitizeAlias strips non-printable characters (including ESC, which
// starts ANSI/terminal escape sequences) and caps length. It must be
// applied to every DeviceInfo.Alias value read from the network — UDP
// announcements, HTTP register bodies, and prepare-upload requests — before
// it is stored or displayed, since it is otherwise a fully attacker-
// controlled string rendered directly in the terminal UI and desktop
// notifications.
func SanitizeAlias(alias string) string {
	var b strings.Builder
	n := 0
	for _, r := range alias {
		if n >= MaxAliasRunes {
			break
		}
		if !unicode.IsPrint(r) {
			continue
		}
		b.WriteRune(r)
		n++
	}
	return strings.TrimSpace(b.String())
}
