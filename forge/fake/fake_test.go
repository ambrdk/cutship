package fake

import (
	"context"
	"errors"
	"slices"
	"testing"

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
