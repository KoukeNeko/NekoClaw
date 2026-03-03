package termclean

import (
	"strings"
	"testing"
)

func TestStripTerminalControlSequences_RemovesCSI(t *testing.T) {
	input := "foo\x1b[31mbar\x1b[0mbaz"
	got := StripTerminalControlSequences(input)
	if got != "foobarbaz" {
		t.Fatalf("unexpected output: %q", got)
	}
}

func TestStripTerminalControlSequences_RemovesOSC(t *testing.T) {
	input := "A\x1b]11;rgb:11/22/33\x07B\x1b]0;title\x1b\\C"
	got := StripTerminalControlSequences(input)
	if got != "ABC" {
		t.Fatalf("unexpected output: %q", got)
	}
	if strings.Contains(got, "rgb:") {
		t.Fatalf("expected rgb payload removed: %q", got)
	}
}

func TestSanitizeDisplayText_NormalizesWhitespaceAndControls(t *testing.T) {
	input := " \x01A\tB\r\nC  "
	got := SanitizeDisplayText(input)
	if got != "A B C" {
		t.Fatalf("unexpected sanitize output: %q", got)
	}
}

func TestStripTerminalControlSequences_PreservesUnicode(t *testing.T) {
	input := "你好\x1b[1m世界\x1b[0m"
	got := StripTerminalControlSequences(input)
	if got != "你好世界" {
		t.Fatalf("unexpected unicode output: %q", got)
	}
}
