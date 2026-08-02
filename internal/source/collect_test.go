package source

import (
	"context"
	"errors"
	"testing"
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
