package ledger

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/ambrdk/cutship/forge"
	"github.com/ambrdk/cutship/forge/fake"
)

const repo = "ambrdk/ansible"

var ctx = context.Background()

func TestEnvironmentName(t *testing.T) {
	if got := Environment("prod", "alert-relay"); got != "prod/alert-relay" {
		t.Fatalf("%q", got)
	}
}

func TestLedgerLastSuccessSkipsFailures(t *testing.T) {
	f := fake.New("bot")
	if _, ok, err := LastSuccess(ctx, f, repo, "prod", "a"); ok || err != nil {
		t.Fatalf("empty: %v %v", ok, err)
	}
	if _, err := Finish(ctx, f, repo, "prod", "a", "s1", true, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := Finish(ctx, f, repo, "prod", "a", "s2", false, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := Start(ctx, f, repo, "prod", "a", "s3", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := Finish(ctx, f, repo, "prod", "b", "s9", true, ""); err != nil {
		t.Fatal(err)
	}
	last, ok, err := LastSuccess(ctx, f, repo, "prod", "a")
	if err != nil || !ok || last.SHA != "s1" || last.App != "a" {
		t.Fatalf("%+v %v %v", last, ok, err)
	}
	latest, ok, err := Latest(ctx, f, repo, "prod", "a")
	if err != nil || !ok || latest.SHA != "s3" || latest.State != forge.DeployInProgress {
		t.Fatalf("%+v %v %v", latest, ok, err)
	}
	newest, success, err := History(ctx, f, repo, "prod", "a")
	if err != nil || newest == nil || newest.SHA != "s3" || success == nil || success.SHA != "s1" {
		t.Fatalf("history %+v %+v %v", newest, success, err)
	}
	if newest, success, err := History(ctx, f, repo, "prod", "none"); err != nil || newest != nil || success != nil {
		t.Fatalf("empty history %+v %+v %v", newest, success, err)
	}
}

func TestLedgerStartIdempotent(t *testing.T) {
	f := fake.New("bot")
	first, err := Start(ctx, f, repo, "prod", "a", "s1", "https://log/1")
	if err != nil {
		t.Fatal(err)
	}
	again, err := Start(ctx, f, repo, "prod", "a", "s1", "https://log/1")
	if err != nil || again.ID != first.ID || len(f.Deployed(repo, "prod/a")) != 1 {
		t.Fatalf("%+v %+v %v", first, again, err)
	}
	done, err := Finish(ctx, f, repo, "prod", "a", "s1", true, "https://log/1")
	if err != nil || done.ID != first.ID || done.State != forge.DeploySuccess || f.DeploymentLog(repo, done.ID) != "https://log/1" {
		t.Fatalf("%+v %v", done, err)
	}
}

func TestStartAfterFailureCreatesNew(t *testing.T) {
	f := fake.New("bot")
	failed, _ := Finish(ctx, f, repo, "prod", "a", "s1", false, "")
	retry, err := Start(ctx, f, repo, "prod", "a", "s1", "")
	if err != nil || retry.ID == failed.ID || retry.State != forge.DeployInProgress {
		t.Fatalf("%+v %+v %v", failed, retry, err)
	}
}

func TestLedgerCarriesData(t *testing.T) {
	f := fake.New("bot")
	if _, err := StartWith(ctx, f, repo, "prod", "a", "s1", "", json.RawMessage(`{"post":5}`)); err != nil {
		t.Fatal(err)
	}
	latest, _, err := History(ctx, f, repo, "prod", "a")
	if err != nil || latest == nil || string(latest.Data) != `{"post":5}` {
		t.Fatalf("%+v %v", latest, err)
	}
	done, err := FinishWith(ctx, f, repo, "prod", "b", "s1", true, "", json.RawMessage(`{"post":6}`))
	if err != nil || string(done.Data) != `{"post":6}` {
		t.Fatalf("%+v %v", done, err)
	}
	if latest, _, _ := History(ctx, f, repo, "prod", "b"); string(latest.Data) != `{"post":6}` {
		t.Fatalf("%+v", latest)
	}
}

func TestFinishWithoutStart(t *testing.T) {
	f := fake.New("bot")
	done, err := Finish(ctx, f, repo, "prod", "a", "s1", false, "https://log")
	if err != nil || done.State != forge.DeployFailure || done.SHA != "s1" {
		t.Fatalf("%+v %v", done, err)
	}
	if got := f.Deployed(repo, "prod/a"); len(got) != 1 || got[0].State != forge.DeployFailure {
		t.Fatalf("%+v", got)
	}
}
