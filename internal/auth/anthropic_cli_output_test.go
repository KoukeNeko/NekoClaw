package auth

import "testing"

func TestExtractDisplayLinesFromCLIChunk_NormalizesProgressAndANSI(t *testing.T) {
	lines, carry := extractDisplayLinesFromCLIChunk("", "step1\rstep2")
	if len(lines) != 1 || lines[0] != "step1" {
		t.Fatalf("unexpected first lines=%v carry=%q", lines, carry)
	}

	lines, carry = extractDisplayLinesFromCLIChunk(carry, "\r\x1b[31mdone\x1b[0m\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d (%v)", len(lines), lines)
	}
	if lines[0] != "step2" || lines[1] != "done" {
		t.Fatalf("unexpected lines: %v", lines)
	}
	if carry != "" {
		t.Fatalf("expected empty carry, got %q", carry)
	}
}

func TestExtractDisplayLinesFromCLIChunk_RemovesOSCAcrossChunks(t *testing.T) {
	lines, carry := extractDisplayLinesFromCLIChunk("", "before \x1b]11;rgb:11/22/")
	if len(lines) != 0 {
		t.Fatalf("unexpected immediate lines: %v", lines)
	}
	lines, carry = extractDisplayLinesFromCLIChunk(carry, "33\x07 after\n")
	if len(lines) != 1 {
		t.Fatalf("expected one line, got %d (%v)", len(lines), lines)
	}
	if lines[0] != "before  after" {
		t.Fatalf("unexpected normalized line: %q", lines[0])
	}
	if carry != "" {
		t.Fatalf("expected empty carry, got %q", carry)
	}
}
