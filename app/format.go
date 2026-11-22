package app

import (
	"html"
	"strings"
	"unicode"

	"github.com/go-telegram/bot/models"
	"github.com/mrmohebi/divar-alert/platform"
)

// Listing messages mark bold spans with these private-use runes. Each
// platform renders them its own way just before sending: a single rune can
// never be cut in half when a long message is split.
const (
	boldOpen  = ''
	boldClose = ''
)

// bold marks s as bold.
func bold(s string) string {
	s = strings.TrimSpace(plainText(s))
	if s == "" {
		return ""
	}
	return string(boldOpen) + s + string(boldClose)
}

// plainText removes bold marks, so untrusted text cannot open or close a span.
func plainText(s string) string {
	return strings.Map(func(r rune) rune {
		if r == boldOpen || r == boldClose {
			return -1
		}
		return r
	}, s)
}

// richText turns **phrase** in AI text into bold. Unbalanced asterisks are
// dropped rather than guessed at.
func richText(s string) string {
	parts := strings.Split(plainText(s), "**")
	if len(parts)%2 == 0 {
		return strings.TrimSpace(strings.Join(parts, ""))
	}
	var out strings.Builder
	for i, part := range parts {
		if i%2 == 1 {
			part = bold(part)
		}
		out.WriteString(part)
	}
	return strings.TrimSpace(out.String())
}

// boldValue bolds the value of a "label: value" line, such as a search
// card's "ودیعه: ۱,۰۰۰,۰۰۰,۰۰۰ تومان".
func boldValue(line string) string {
	line = plainText(line)
	label, value, found := strings.Cut(line, ":")
	if !found || strings.TrimSpace(label) == "" || strings.TrimSpace(value) == "" {
		return line
	}
	return label + ": " + bold(value)
}

// balanceBold closes a span still open at the end of a part and reopens it
// at the start of the next, so every part renders on its own.
func balanceBold(parts []string) []string {
	open := false
	out := make([]string, len(parts))
	for i, part := range parts {
		if open {
			part = string(boldOpen) + part
		}
		for _, r := range part {
			switch r {
			case boldOpen:
				open = true
			case boldClose:
				open = false
			}
		}
		if open {
			part += string(boldClose)
		}
		out[i] = part
	}
	return out
}

// render formats marked text for a platform: HTML for Telegram, and Bale's
// default Markdown, where bold is *text* set off by spaces.
func render(p platform.Platform, text string) (string, models.ParseMode) {
	if p == platform.Telegram {
		escaped := html.EscapeString(text)
		return strings.NewReplacer(string(boldOpen), "<b>", string(boldClose), "</b>").Replace(escaped), models.ParseModeHTML
	}
	runes := []rune(text)
	var out strings.Builder
	for i, r := range runes {
		switch r {
		case boldOpen:
			if i > 0 && !unicode.IsSpace(runes[i-1]) {
				out.WriteRune(' ')
			}
			out.WriteRune('*')
		case boldClose:
			out.WriteRune('*')
			if i+1 < len(runes) && !unicode.IsSpace(runes[i+1]) {
				out.WriteRune(' ')
			}
		default:
			out.WriteRune(r)
		}
	}
	return out.String(), ""
}
