package fake

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/ambrdk/cutship/forge"
)

var _ forge.Reader = (*Forge)(nil)

func TestFakeFilesAndRefs(t *testing.T) {
	ctx := context.Background()
	f := New("bot")
	first := f.Commit("o/r", "main", map[string][]byte{"a": []byte("1"), "b": []byte("2")})
	second := f.Commit("o/r", "main", map[string][]byte{"a": []byte("3"), "b": nil, "c": []byte("4")})
	if b, _ := f.DefaultBranch(ctx, "o/r"); b != "main" {
		t.Fatalf("default %q", b)
	}
	if got, err := f.FileAt(ctx, "o/r", "a", first); err != nil || string(got) != "1" {
		t.Fatalf("%q %v", got, err)
	}
	if _, err := f.FileAt(ctx, "o/r", "b", "main"); !errors.Is(err, forge.ErrNotFound) {
		t.Fatalf("deleted file: %v", err)
	}
	if sha, _ := f.ResolveRef(ctx, "o/r", "main"); sha != second {
		t.Fatalf("main = %q", sha)
	}
	if paths, _ := f.ChangedPaths(ctx, "o/r", first, second); !slices.Equal(paths, []string{"a", "b", "c"}) {
		t.Fatalf("paths %v", paths)
	}
	if _, err := f.FileAt(ctx, "x/y", "a", "main"); !errors.Is(err, forge.ErrNotFound) {
		t.Fatalf("unknown repo: %v", err)
	}
}

func TestFakePushChange(t *testing.T) {
	ctx := context.Background()
	f := New("bot")
	f.Commit("o/r", "main", map[string][]byte{"a": []byte("1"), "b": []byte("2")})
	req := forge.ChangeRequest{Base: "main", Branch: "bump/x", Title: "t", Files: map[string][]byte{"a": []byte("9")}}
	first, err := f.PushChange(ctx, "o/r", req)
	if err != nil || first.Number != 1 || first.State != forge.Open {
		t.Fatalf("%+v %v", first, err)
	}
	again, _ := f.PushChange(ctx, "o/r", req)
	if again.Number != 1 || again.HeadSHA != first.HeadSHA || len(f.Changes("o/r")) != 1 {
		t.Fatalf("again %+v", again)
	}
	if got, _ := f.FileAt(ctx, "o/r", "b", "bump/x"); string(got) != "2" {
		t.Fatalf("b on branch %q", got)
	}
	merged := f.Merge("o/r", 1, "bot", time.Now())
	if got, _ := f.FileAt(ctx, "o/r", "a", "main"); string(got) != "9" {
		t.Fatalf("a on main %q", got)
	}
	if changes, _ := f.ChangesForCommit(ctx, "o/r", merged); len(changes) != 1 || changes[0].State != forge.Merged {
		t.Fatalf("%+v", changes)
	}
	if err := f.CreateTag(ctx, "o/r", "v1", "x"); err != nil {
		t.Fatal(err)
	}
	if err := f.CreateTag(ctx, "o/r", "v1", "y"); !errors.Is(err, forge.ErrTagElsewhere) {
		t.Fatal(err)
	}
}

func TestFakeFailNext(t *testing.T) {
	f := New("bot")
	f.Commit("o/r", "main", nil)
	boom := errors.New("boom")
	f.FailNext("DefaultBranch", boom)
	if _, err := f.DefaultBranch(context.Background(), "o/r"); !errors.Is(err, boom) {
		t.Fatal(err)
	}
	if _, err := f.DefaultBranch(context.Background(), "o/r"); err != nil {
		t.Fatal(err)
	}
}
