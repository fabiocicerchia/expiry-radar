package output

// iCal (RFC 5545).

import (
	"fmt"
	"io"
	"strings"

	"github.com/fabiocicerchia/expiry-radar/internal/rank"
)

// Renewals land in the team calendar as all-day events. Higher blast radius gets
// an earlier alarm, because that is the whole point of ranking.
func renderICal(w io.Writer, items []rank.Scored, opts Options) error {
	var b strings.Builder
	b.WriteString("BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//expiry-radar//EN\r\nCALSCALE:GREGORIAN\r\nMETHOD:PUBLISH\r\nX-WR-CALNAME:Expiry radar\r\n")
	stamp := opts.Now.UTC().Format(icalTimestamp)

	for _, s := range items {
		day := s.Item.Expires.UTC()
		b.WriteString("BEGIN:VEVENT\r\n")
		b.WriteString(fold("UID:" + uid(s)))
		b.WriteString(fold("DTSTAMP:" + stamp))
		b.WriteString(fold("DTSTART;VALUE=DATE:" + day.Format(icalDate)))
		b.WriteString(fold("DTEND;VALUE=DATE:" + day.AddDate(0, 0, 1).Format(icalDate)))
		b.WriteString(fold(fmt.Sprintf("SUMMARY:%s expires: %s", s.Item.Kind, escapeText(displayName(s)))))
		b.WriteString(fold(fmt.Sprintf("DESCRIPTION:blast radius %.2f (%s)\\nsource %s\\npriority %.2f",
			s.BlastRadius, escapeText(s.Why), escapeText(s.Item.Source), s.Priority)))
		b.WriteString(fold("CATEGORIES:" + strings.ToUpper(string(s.Item.Kind))))
		b.WriteString(alarm(s))
		b.WriteString("END:VEVENT\r\n")
	}
	b.WriteString("END:VCALENDAR\r\n")
	_, err := io.WriteString(w, b.String())
	return err
}

func alarm(s rank.Scored) string {
	lead := "-P7D"
	switch {
	case s.BlastRadius >= 0.8:
		lead = "-P30D"
	case s.BlastRadius >= 0.6:
		lead = "-P14D"
	}
	return "BEGIN:VALARM\r\nACTION:DISPLAY\r\n" + fold("DESCRIPTION:Renew "+escapeText(displayName(s))) + "TRIGGER:" + lead + "\r\nEND:VALARM\r\n"
}

// Stable across runs so calendars update the same event instead of piling up
// duplicates every time the feed is refreshed.
func uid(s rank.Scored) string {
	key := string(s.Item.Kind) + "-" + s.Item.Source + "-" + displayName(s) + "-" + s.Item.Expires.UTC().Format(icalDate)
	return sanitiseUID(key) + "@expiry-radar"
}

func sanitiseUID(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	return strings.Trim(b.String(), "-")
}

func escapeText(s string) string {
	return strings.NewReplacer(`\`, `\\`, ";", `\;`, ",", `\,`, "\n", `\n`).Replace(s)
}

// RFC 5545 caps content lines at 75 octets; long certificate names blow straight
// past that and some clients do reject it.
func fold(line string) string {
	const limit = 73
	var b strings.Builder
	for len(line) > limit {
		b.WriteString(line[:limit])
		b.WriteString("\r\n ")
		line = line[limit:]
	}
	b.WriteString(line)
	b.WriteString("\r\n")
	return b.String()
}
