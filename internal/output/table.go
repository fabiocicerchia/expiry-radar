package output

import (
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/fabiocicerchia/expiry-radar/internal/rank"
)

func renderTable(w io.Writer, items []rank.Scored) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	// tabwriter buffers until Flush, which is what actually reports write errors.
	_, _ = fmt.Fprintln(tw, "PRIORITY\tBLAST\tEXPIRES IN\tKIND\tNAME\tSOURCE\tWHY")
	for _, s := range items {
		_, _ = fmt.Fprintf(tw, "%.2f\t%.2f\t%s\t%s\t%s\t%s\t%s\n",
			s.Priority, s.BlastRadius, humanDays(s.DaysLeft), s.Item.Kind, displayName(s), s.Item.Source, s.Why)
	}
	if len(items) == 0 {
		_, _ = fmt.Fprintln(tw, "(nothing expiring — or no sources were enabled)")
	}
	return tw.Flush()
}
