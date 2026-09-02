// Package output renders ranked items. Five formats: a ranked table (CLI), a
// Prometheus metrics endpoint, an iCal feed so renewals land in the team
// calendar, JSON for CI, and a self-contained HTML report.
package output

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/fabiocicerchia/expiry-radar/internal/rank"
)

type Format string

const (
	FormatTable      Format = "table"
	FormatJSON       Format = "json"
	FormatICal       Format = "ical"       // renewals in the team calendar
	FormatPrometheus Format = "prometheus" // scrape target
	FormatHTML       Format = "html"       // a self-contained report to email or publish
)

// Formats lists every renderer, for CLI help and validation.
var Formats = []Format{FormatTable, FormatJSON, FormatICal, FormatPrometheus, FormatHTML}

// Time layouts, in Go's reference-time spelling. Named because "20060102" says
// neither which format it is nor which renderer wants it, and it is the key
// half of every iCal UID.
const (
	icalDate      = "20060102"             // RFC 5545 VALUE=DATE
	icalTimestamp = "20060102T150405Z"     // RFC 5545 UTC DATE-TIME
	isoDate       = "2006-01-02"           // the "On" column of the HTML report
	generatedAt   = "2006-01-02 15:04 UTC" // the HTML report's own header
)

// Options exists so the renderers are testable without freezing the clock
// globally — an iCal feed with a moving DTSTAMP cannot be diffed.
type Options struct {
	Now time.Time
}

// Render writes the scored items in the requested format.
func Render(w io.Writer, items []rank.Scored, format Format) error {
	return RenderAt(w, items, format, Options{Now: time.Now()})
}

func RenderAt(w io.Writer, items []rank.Scored, format Format, opts Options) error {
	if opts.Now.IsZero() {
		opts.Now = time.Now()
	}
	switch format {
	case FormatTable:
		return renderTable(w, items)
	case FormatJSON:
		return renderJSON(w, items, opts)
	case FormatICal:
		return renderICal(w, items, opts)
	case FormatPrometheus:
		return renderPrometheus(w, items, opts)
	case FormatHTML:
		return renderHTML(w, items, opts)
	default:
		return fmt.Errorf("unknown format %q (want one of %v)", format, Formats)
	}
}

func displayName(s rank.Scored) string {
	if s.Item.Namespace != "" && !strings.HasPrefix(s.Item.Name, s.Item.Namespace+"/") {
		return s.Item.Namespace + "/" + s.Item.Name
	}
	return s.Item.Name
}

// Whole days, and never a cheerful "0 days" for something already broken.
func humanDays(days float64) string {
	switch {
	case days < 0:
		return fmt.Sprintf("EXPIRED %dd ago", int(-days))
	case days < 1:
		return "today"
	default:
		return fmt.Sprintf("%dd", int(days))
	}
}
