// Command expiry-radar inventories everything that expires across your estate,
// ranks it by blast radius, and reports it. Read-only throughout.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/fabiocicerchia/expiry-radar/internal/config"
	"github.com/fabiocicerchia/expiry-radar/internal/output"
	"github.com/fabiocicerchia/expiry-radar/internal/rank"
	"github.com/fabiocicerchia/expiry-radar/internal/source"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	code, err := run(ctx, os.Args[1:], os.Stdout)
	if err != nil {
		fmt.Fprintln(os.Stderr, "expiry-radar:", err)
	}
	os.Exit(code)
}

// Exit codes: 0 clean, 1 a threshold was breached, 2 bad usage or config,
// 3 partial results (at least one source failed).
func run(ctx context.Context, args []string, stdout io.Writer) (int, error) {
	fs := flag.NewFlagSet("expiry-radar", flag.ContinueOnError)
	var (
		cfgPath     = fs.String("config", "expiry-radar.json", "config file listing the read-only sources to scan")
		format      = fs.String("format", string(output.FormatTable), "output format: table, json, ical, prometheus, html")
		endpoints   = fs.String("endpoints", "", "comma-separated hosts to probe over TLS, in addition to the config")
		domains     = fs.String("domains", "", "comma-separated domains to check via RDAP, in addition to the config")
		within      = fs.Int("within", 0, "only report items expiring within N days (0 = everything)")
		failWithin  = fs.Int("fail-within", 0, "exit 1 if anything expires within N days (0 = never fail)")
		minPriority = fs.Float64("min-priority", 0, "only report items at or above this priority")
		out         = fs.String("out", "", "write to a file instead of stdout")
		timeout     = fs.Duration("timeout", 2*time.Minute, "overall collection timeout")
	)
	fs.Usage = func() {
		_, _ = fmt.Fprint(fs.Output(), "expiry-radar — one inventory of everything that expires, ranked by blast radius.\n\n"+
			"All sources are read-only. See docs/ for the exact IAM policy and RBAC Role.\n\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2, nil
	}

	cfg, err := loadConfig(*cfgPath, *endpoints, *domains)
	if err != nil {
		return 2, err
	}
	sources := cfg.Sources()
	if len(sources) == 0 {
		return 2, fmt.Errorf("no sources configured — pass -endpoints/-domains, or add manual items / enable k8s/vault/aws in %s", *cfgPath)
	}

	ctx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()

	items, errs := source.CollectAll(ctx, sources)
	for _, e := range errs {
		// Partial failures are reported, never swallowed: a report that quietly
		// lost a source reads exactly like a clean estate.
		fmt.Fprintln(os.Stderr, "expiry-radar: warning:", e)
	}

	now := time.Now()
	scored := filter(rank.Rank(items, cfg.Overrides, now), *within, *minPriority)

	w, closeOut := stdout, func() error { return nil }
	if *out != "" {
		f, err := os.Create(*out)
		if err != nil {
			return 2, err
		}
		w, closeOut = f, f.Close
	}
	err = output.RenderAt(w, scored, output.Format(*format), output.Options{Now: now})
	// Close reports the final flush: a silently truncated report on disk reads
	// exactly like a clean estate.
	if cerr := closeOut(); err == nil {
		err = cerr
	}
	if err != nil {
		return 2, err
	}

	if *failWithin > 0 {
		for _, s := range scored {
			if s.DaysLeft <= float64(*failWithin) {
				return 1, fmt.Errorf("%s expires in %.0f days (-fail-within %d)", s.Item.Name, s.DaysLeft, *failWithin)
			}
		}
	}
	if len(errs) > 0 {
		return 3, nil // partial results: distinct from both success and a hard failure
	}
	return 0, nil
}

// Flags add to the config file rather than replacing it, so a one-off probe does
// not require editing (or inventing) a config.
func loadConfig(path, endpoints, domains string) (*config.File, error) {
	cfg := &config.File{}
	if _, err := os.Stat(path); err == nil {
		cfg, err = config.Load(path)
		if err != nil {
			return nil, err
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}

	for _, h := range splitList(endpoints) {
		cfg.Endpoints = append(cfg.Endpoints, source.Endpoint{Host: h})
	}
	cfg.Domains = append(cfg.Domains, splitList(domains)...)
	return cfg, nil
}

func splitList(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func filter(items []rank.Scored, withinDays int, minPriority float64) []rank.Scored {
	if withinDays <= 0 && minPriority <= 0 {
		return items
	}
	out := items[:0]
	for _, s := range items {
		if withinDays > 0 && s.DaysLeft > float64(withinDays) {
			continue
		}
		if s.Priority < minPriority {
			continue
		}
		out = append(out, s)
	}
	return out
}
