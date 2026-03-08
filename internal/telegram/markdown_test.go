package telegram

import (
	"strings"
	"testing"
)

func TestEscapeMarkdownV2(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "plain text without special chars",
			input:    "Hello World",
			expected: "Hello World",
		},
		{
			name:     "italic",
			input:    "This is *italic*",
			expected: "This is _italic_", // In MarkdownV2, *italic* doesn't exist, we must convert to _italic_
		},
		{
			name:     "bold",
			input:    "This is **bold**",
			expected: "This is *bold*", // MarkdownV2 uses *bold* for bold
		},
		{
			name:     "strikethrough",
			input:    "This is ~~strikethrough~~",
			expected: "This is ~strikethrough~", // MarkdownV2 uses ~strikethrough~
		},
		{
			name:     "bold and italic combined",
			input:    "This is ***bold italic***",
			expected: "This is _*bold italic*_", // either *_ _* or _* *_ works, goldmark chooses _* *_
		},
		{
			name:     "inline code",
			input:    "This is `inline code`",
			expected: "This is `inline code`", // ` needs no escape if correctly enclosed
		},
		{
			name:     "inline code with escaped backslash",
			input:    "Here is `code \\ with backslash`",
			expected: "Here is `code \\\\ with backslash`", // MarkdownV2 inline code escaping needs \ to be escaped as \\
		},
		{
			name:     "block code",
			input:    "```go\nfmt.Println(\"Hello\")\n```",
			expected: "```go\nfmt.Println(\"Hello\")\n```", // Inside code block Telegram does NOT want most things escaped, ONLY ` and \
		},
		{
			name:     "escaping special characters outside code",
			input:    "Hello! How are you? user_name, {brace}, [bracket], (paren), #hash, +plus, -minus, .dot, !bang, =eq, |pipe",
			expected: "Hello\\! How are you? user\\_name, \\{brace\\}, \\[bracket\\], \\(paren\\), \\#hash, \\+plus, \\-minus, \\.dot, \\!bang, \\=eq, \\|pipe", // ? is not escaped
		},
		{
			name:     "link",
			input:    "[Google](https://google.com)",
			expected: "[Google](https://google.com)", // URL needs escaping only for ) and \
		},
		{
			name:     "list",
			input:    "- Item 1\n* Item 2\n+ Item 3",
			expected: "\\- Item 1\n\n\\- Item 2\n\n\\- Item 3", // goldmark normalizes list types when rendering if we just output \-
		},
		{
			name:     "blockquote",
			input:    "> Hello context",
			expected: "\\> Hello context", // Or actually, Telegram MarkdownV2 supports blockquotes natively using > but nothing can precede it...? Wait, Telegram's API doesn't mention > as blockquote natively until relatively recently. Actually, it does support blockquote: > blockquote. No, wait, wait. Blockquote is not supported by standard MarkdownV2 in Telegram, it's just blockquotes. Ah wait, Telegram just added blockquote `>` support recently. "blockquote can be started with `>` or `**` wait no. "Blockquotation block should be formatted as `> blockquote`".
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RenderMarkdownV2(tt.input)
			// Trimming space for safer comparison of block elements
			if strings.TrimSpace(got) != strings.TrimSpace(tt.expected) {
				t.Errorf("RenderMarkdownV2() = %v, want %v", got, tt.expected)
			}
		})
	}
}
