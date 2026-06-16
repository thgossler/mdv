package core

import (
	"strings"
	"unicode"
)

// Slugger generates GitHub-compatible heading anchors and tracks duplicates so
// repeated headings get "-1", "-2" suffixes, matching github-slugger.
//
// Use one Slugger per document render; call Slug for each heading in order.
type Slugger struct {
	seen map[string]int
}

// NewSlugger returns a ready-to-use Slugger.
func NewSlugger() *Slugger {
	return &Slugger{seen: make(map[string]int)}
}

// Reset clears duplicate tracking so the Slugger can be reused for a new doc.
func (s *Slugger) Reset() {
	s.seen = make(map[string]int)
}

// Slug returns a unique anchor for the given heading text.
func (s *Slugger) Slug(text string) string {
	base := BaseSlug(text)
	if s.seen == nil {
		s.seen = make(map[string]int)
	}
	if n, ok := s.seen[base]; ok {
		s.seen[base] = n + 1
		// GitHub appends the running count: first dup -> "-1".
		next := base + "-" + itoa(n)
		// Guard against an unlikely collision with an explicit heading.
		for {
			if _, clash := s.seen[next]; !clash {
				break
			}
			n++
			next = base + "-" + itoa(n)
		}
		s.seen[next] = 1
		return next
	}
	s.seen[base] = 1
	return base
}

// BaseSlug converts heading text to a GitHub-style anchor without duplicate
// disambiguation: lowercase, drop punctuation/symbols, spaces become hyphens,
// Unicode letters/numbers are preserved.
func BaseSlug(text string) string {
	text = strings.TrimSpace(text)
	lower := strings.ToLower(text)

	var b strings.Builder
	b.Grow(len(lower))
	for _, r := range lower {
		switch {
		case unicode.IsSpace(r):
			b.WriteByte('-')
		case r == '-' || r == '_':
			b.WriteRune(r)
		case unicode.IsLetter(r) || unicode.IsNumber(r) || unicode.IsMark(r):
			b.WriteRune(r)
		default:
			// Drop punctuation, symbols, emoji, etc.
		}
	}
	return b.String()
}

// itoa is a tiny dependency-free integer formatter for positive ints.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
