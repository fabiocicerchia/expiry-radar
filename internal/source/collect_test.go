package source

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

type fakeSource struct {
	name  string
	items []Item
	err   error
}

func (f fakeSource) Name() string                            { return f.name }
func (f fakeSource) Collect(context.Context) ([]Item, error) { return f.items, f.err }

// A source that partially failed still returns what it read. Dropping those
// items would turn "one host was unreachable" into "the estate looks clean".
func TestCollectAllKeepsPartialResults(t *testing.T) {
	sources := []Source{
		fakeSource{name: "partial", items: []Item{{Name: "found-anyway"}}, err: errors.New("1 endpoint failed")},
		fakeSource{name: "clean", items: []Item{{Name: "also-found"}}},
	}
	items, errs := CollectAll(context.Background(), sources)
	if len(items) != 2 {
		t.Fatalf("want both items, got %d", len(items))
	}
	if len(errs) != 1 {
		t.Fatalf("want the failure reported, got %d errors", len(errs))
	}
	if got := errs[0].Error(); got != "partial: 1 endpoint failed" {
		t.Errorf("error should name its source, got %q", got)
	}
}

// barrierSource reports only once every source has started. Sequential
// collection can never satisfy that, so this is the test that reproduces the
// defect rather than merely passing over it.
type barrierSource struct {
	name    string
	arrived chan struct{}
	release chan struct{}
}

func (b barrierSource) Name() string { return b.name }

func (b barrierSource) Collect(context.Context) ([]Item, error) {
	b.arrived <- struct{}{}
	select {
	case <-b.release:
	case <-time.After(5 * time.Second):
		return nil, errors.New("never released")
	}
	return []Item{{Name: b.name}}, nil
}

// The global -timeout is a deadline for the run. Collected one after another it
// is a budget instead, which the first source can spend on the last one's
// behalf — and the source that runs out of it reports nothing, which reads
// exactly like a clean estate.
func TestCollectAllRunsSourcesConcurrently(t *testing.T) {
	const n = 4 // below collectConcurrency, so the pool is not the thing under test
	arrived := make(chan struct{}, n)
	release := make(chan struct{})
	sources := make([]Source, 0, n)
	for i := range n {
		sources = append(sources, barrierSource{
			name:    fmt.Sprintf("source-%d", i),
			arrived: arrived,
			release: release,
		})
	}

	done := make(chan []Item, 1)
	go func() {
		items, _ := CollectAll(context.Background(), sources)
		done <- items
	}()

	for i := range n {
		select {
		case <-arrived:
		case <-time.After(5 * time.Second):
			t.Fatalf("only %d of %d sources had started: they are not running concurrently", i, n)
		}
	}
	close(release)

	select {
	case items := <-done:
		if len(items) != n {
			t.Errorf("want %d items, got %d", n, len(items))
		}
	case <-time.After(5 * time.Second):
		t.Fatal("CollectAll did not return")
	}
}

// Collected concurrently, sources finish in whatever order the network allows.
// rank.Rank sorts stably on a priority rounded to two places, so tied rows come
// out in collection order — and two runs of the same config must not disagree.
func TestCollectAllKeepsTheConfiguredOrderUnderConcurrency(t *testing.T) {
	slow := delayedSource{name: "slow", delay: 40 * time.Millisecond}
	fast := delayedSource{name: "fast"}
	items, errs := CollectAll(context.Background(), []Source{slow, fast})
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(items) != 2 {
		t.Fatalf("want 2 items, got %d", len(items))
	}
	if items[0].Name != "slow" || items[1].Name != "fast" {
		t.Errorf("want configured order [slow fast], got [%s %s]", items[0].Name, items[1].Name)
	}
}

type delayedSource struct {
	name  string
	delay time.Duration
}

func (d delayedSource) Name() string { return d.name }

func (d delayedSource) Collect(context.Context) ([]Item, error) {
	time.Sleep(d.delay)
	return []Item{{Name: d.name}}, nil
}
