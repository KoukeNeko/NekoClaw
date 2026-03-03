package termclean

import "strings"

const (
	scanStateNormal = iota
	scanStateESC
	scanStateCSI
	scanStateString
	scanStateStringESC
)

// StripTerminalControlSequences removes ANSI/OSC and other ESC control sequences.
// It also removes C0 control bytes except tab/newline/carriage-return.
func StripTerminalControlSequences(input string) string {
	if input == "" {
		return ""
	}
	var out strings.Builder
	out.Grow(len(input))

	state := scanStateNormal
	for i := 0; i < len(input); i++ {
		b := input[i]
		switch state {
		case scanStateNormal:
			if b == 0x1b { // ESC
				state = scanStateESC
				continue
			}
			if b < 0x20 || b == 0x7f {
				if b == '\n' || b == '\r' || b == '\t' {
					out.WriteByte(b)
				}
				continue
			}
			out.WriteByte(b)
		case scanStateESC:
			switch b {
			case '[':
				state = scanStateCSI
			case ']', 'P', '^', '_', 'X':
				state = scanStateString
			default:
				state = scanStateNormal
			}
		case scanStateCSI:
			// CSI ends with final byte in [0x40, 0x7e].
			if b >= 0x40 && b <= 0x7e {
				state = scanStateNormal
			}
		case scanStateString:
			// String controls terminate by BEL or ST (ESC \).
			if b == 0x07 {
				state = scanStateNormal
				continue
			}
			if b == 0x1b {
				state = scanStateStringESC
			}
		case scanStateStringESC:
			if b == '\\' {
				state = scanStateNormal
			} else if b == 0x1b {
				state = scanStateStringESC
			} else {
				state = scanStateString
			}
		}
	}
	return out.String()
}

// SanitizeDisplayText prepares text for safe single-line UI display.
func SanitizeDisplayText(input string) string {
	clean := StripTerminalControlSequences(input)
	if clean == "" {
		return ""
	}
	clean = strings.ReplaceAll(clean, "\r", " ")
	clean = strings.ReplaceAll(clean, "\n", " ")
	clean = strings.ReplaceAll(clean, "\t", " ")
	clean = strings.Join(strings.Fields(clean), " ")
	return strings.TrimSpace(clean)
}

