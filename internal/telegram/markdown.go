package telegram

import (
	"bytes"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	extast "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/text"
)

// RenderMarkdownV2 takes standard Markdown and converts it to Telegram's MarkdownV2 format.
func RenderMarkdownV2(input string) string {
	if input == "" {
		return ""
	}

	md := goldmark.New(
		goldmark.WithExtensions(extension.Strikethrough),
	)

	source := []byte(input)
	node := md.Parser().Parse(text.NewReader(source))

	var buf bytes.Buffer

	// Create a new ast walker to visit nodes and write corresponding Telegram MarkdownV2 strings
	err := ast.Walk(node, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			// Handle closing tags
			switch n.Kind() {
			case ast.KindParagraph:
				buf.WriteString("\n\n")
			case ast.KindEmphasis:
				if n.(*ast.Emphasis).Level == 2 {
					buf.WriteString("*") // bold
				} else {
					buf.WriteString("_") // italic
				}
			case extast.KindStrikethrough:
				buf.WriteString("~")
			case ast.KindBlockquote:
			case ast.KindList:
				buf.WriteString("\n")
			case ast.KindListItem:
				buf.WriteString("\n")
			case ast.KindLink:
				// Close link text and append url
				link := n.(*ast.Link)
				buf.WriteString("](")
				buf.WriteString(escapeURL(string(link.Destination)))
				buf.WriteString(")")
			case ast.KindCodeSpan:
				buf.WriteString("`")
			}
			return ast.WalkContinue, nil
		}

		// Handle opening tags and content
		switch n.Kind() {
		case ast.KindDocument:
			// ignore
		case ast.KindParagraph:
			// ignore, wait for text
		case ast.KindHeading:
			// Telegram doesn't support headings in MarkdownV2 except by making them bold and adding newlines.
			buf.WriteString("*")
		case ast.KindBlockquote:
			buf.WriteString("\\> ")
		case ast.KindList:
			// List
		case ast.KindListItem: // list item
			list := n.Parent().(*ast.List)
			if list.IsOrdered() {
				// We don't have the current number easily from ast.ListItem unless we count.
				// Just render as text, but for our simple renderer we can just escape the number later.
				// Actually goldmark doesn't give us the number, it just gives the content.
			} else {
				buf.WriteString("\\- ")
			}
		case ast.KindText:
			txt := n.(*ast.Text)
			// Check if we are inside a code span
			isCode := false
			for p := n.Parent(); p != nil; p = p.Parent() {
				if p.Kind() == ast.KindCodeSpan {
					isCode = true
					break
				}
			}

			if isCode {
				buf.WriteString(escapeCode(string(txt.Segment.Value(source))))
			} else {
				buf.WriteString(escapeText(string(txt.Segment.Value(source))))
			}
		case ast.KindEmphasis:
			if n.(*ast.Emphasis).Level == 2 {
				buf.WriteString("*") // Telegram bold is *
			} else {
				buf.WriteString("_") // Telegram italic is _
			}
		case extast.KindStrikethrough:
			buf.WriteString("~")
		case ast.KindCodeBlock, ast.KindFencedCodeBlock:
			buf.WriteString("```")
			if n.Kind() == ast.KindFencedCodeBlock {
				fcb := n.(*ast.FencedCodeBlock)
				if fcb.Language(source) != nil {
					buf.WriteString(string(fcb.Language(source)))
				}
			}
			buf.WriteString("\n")
			// Code block content - need to escape ` and \
			lines := n.Lines()
			for i := 0; i < lines.Len(); i++ {
				seg := lines.At(i)
				buf.WriteString(escapeCode(string(seg.Value(source))))
			}
			buf.WriteString("```\n\n")
			return ast.WalkSkipChildren, nil // Skip children, we already handled them
		case ast.KindCodeSpan:
			buf.WriteString("`")
		case ast.KindLink:
			buf.WriteString("[")
		case ast.KindAutoLink:
			link := n.(*ast.AutoLink)
			buf.WriteString("[")
			buf.WriteString(escapeText(string(link.URL(source))))
			buf.WriteString("](")
			buf.WriteString(escapeURL(string(link.URL(source))))
			buf.WriteString(")")
			return ast.WalkSkipChildren, nil
		case ast.KindString:
			str := n.(*ast.String)
			buf.WriteString(escapeText(string(str.Value)))
		}

		return ast.WalkContinue, nil
	})

	if err != nil {
		// Fallback to strict escaping of everything if parsing fails
		return escapeText(input)
	}

	return strings.TrimSpace(buf.String())
}

// Telegram MarkdownV2 requires escaping these characters outside of code/pre/link
// _, *, [, ], (, ), ~, `, >, #, +, -, =, |, {, }, ., !
var textEscaper = strings.NewReplacer(
	"_", "\\_", "*", "\\*", "[", "\\[", "]", "\\]", "(", "\\(",
	")", "\\)", "~", "\\~", "`", "\\`", ">", "\\>", "#", "\\#",
	"+", "\\+", "-", "\\-", "=", "\\=", "|", "\\|", "{", "\\{",
	"}", "\\}", ".", "\\.", "!", "\\!",
)

func escapeText(s string) string {
	return textEscaper.Replace(s)
}

// Inside `code` and ```pre```, only ` and \ must be escaped
var codeEscaper = strings.NewReplacer(
	"`", "\\`", "\\", "\\\\",
)

func escapeCode(s string) string {
	return codeEscaper.Replace(s)
}

// Inside link destinations, ) and \ must be escaped.
var urlEscaper = strings.NewReplacer(
	")", "\\)", "\\", "\\\\",
)

func escapeURL(s string) string {
	return urlEscaper.Replace(s)
}
