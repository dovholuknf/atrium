package forge

import (
	"context"
	"testing"
)

func countingRemote(calls *[]HubAsk) *Remote {
	return NewRemote(func(_ context.Context, _ string, in, out any) error {
		a := in.(HubAsk)
		*calls = append(*calls, a)
		ans := out.(*HubPR)
		*ans = HubPR{Kind: GitHub, PR: &PR{Head: "ad5ddf4c0e7d1b2a9f3e4d5c6b7a8f9e0d1c2b3a"}, URL: "u"}
		if a.Diff {
			ans.Diff = []byte("diff")
		}
		if a.Fetch {
			ans.Store, ans.Ref = "github/o/r", PRRef(a.Number)
		}
		return nil
	})
}

func TestViewReusesAViewThatFetchedTheHead(t *testing.T) {
	var calls []HubAsk
	r := countingRemote(&calls)
	ref := Ref{Host: "github.com", Org: "o", Repo: "r", Number: 7}
	if _, err := r.View(context.Background(), ref); err != nil {
		t.Fatal(err)
	}
	pr, diff, err := r.ViewDiff(context.Background(), ref)
	if err != nil || pr.Head == "" || string(diff) != "diff" {
		t.Fatalf("%v %v %q", pr, err, diff)
	}
	if len(calls) != 2 || !calls[0].Fetch || calls[1].Fetch || !calls[1].Diff {
		t.Fatalf("the second look asked the hub to fetch again: %+v", calls)
	}
	if got := r.FetchSpec(ref); got.Hub != "github/o/r" || got.Refspec != PRRef(7) {
		t.Fatalf("%+v", got)
	}
}

func TestViewDiffIsOneQuestion(t *testing.T) {
	var calls []HubAsk
	r := countingRemote(&calls)
	ref := Ref{Host: "github.com", Org: "o", Repo: "r", Number: 7}
	if _, _, err := r.ViewDiff(context.Background(), ref); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 || !calls[0].Fetch || !calls[0].Diff {
		t.Fatalf("%+v", calls)
	}
}

func TestAViewIsNotReusedAfterItsTime(t *testing.T) {
	var calls []HubAsk
	r := countingRemote(&calls)
	ref := Ref{Host: "github.com", Org: "o", Repo: "r", Number: 7}
	if _, err := r.View(context.Background(), ref); err != nil {
		t.Fatal(err)
	}
	r.mu.Lock()
	v := r.fetched[ref]
	v.at = v.at.Add(-2 * viewReuse)
	r.fetched[ref] = v
	r.mu.Unlock()
	if _, err := r.View(context.Background(), ref); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 2 || !calls[1].Fetch {
		t.Fatalf("a stale view was reused: %+v", calls)
	}
}
