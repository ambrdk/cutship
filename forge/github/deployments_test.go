package github

import (
	"net/http"
	"testing"

	"github.com/ambrdk/cutship/forge"
)

func TestCreateDeploymentNoRequiredContexts(t *testing.T) {
	c, rec := record(t, map[string]http.HandlerFunc{
		"POST /repos/o/r/deployments": status(201, `{"id":11,"sha":"s1","environment":"prod/a","created_at":"2026-10-19T10:00:00Z"}`),
	})
	d, err := c.CreateDeployment(ctx, "o/r", "prod/a", "s1", []byte(`{"app":"a"}`))
	if err != nil || d.ID != 11 || d.SHA != "s1" || d.Environment != "prod/a" || d.State != forge.DeployInProgress {
		t.Fatalf("%+v %v", d, err)
	}
	body := rec.body["POST /repos/o/r/deployments"]
	contexts, isList := body["required_contexts"].([]any)
	payload, _ := body["payload"].(map[string]any)
	if !isList || len(contexts) != 0 || body["auto_merge"] != false || body["ref"] != "s1" || body["environment"] != "prod/a" || body["production_environment"] != true || payload["app"] != "a" {
		t.Fatalf("body %v", body)
	}
	staging, rec := record(t, map[string]http.HandlerFunc{"POST /repos/o/r/deployments": status(201, `{"id":12}`)})
	if _, err := staging.CreateDeployment(ctx, "o/r", "staging/a", "s1", nil); err != nil {
		t.Fatal(err)
	}
	if rec.body["POST /repos/o/r/deployments"]["production_environment"] != false {
		t.Fatalf("staging body %v", rec.body)
	}
}

func TestSetDeploymentState(t *testing.T) {
	c, rec := record(t, map[string]http.HandlerFunc{"POST /repos/o/r/deployments/11/statuses": status(201, `{"id":1,"state":"success"}`)})
	if err := c.SetDeploymentState(ctx, "o/r", 11, forge.DeploySuccess, "https://log"); err != nil {
		t.Fatal(err)
	}
	if b := rec.body["POST /repos/o/r/deployments/11/statuses"]; b["state"] != "success" || b["log_url"] != "https://log" || b["auto_inactive"] != false {
		t.Fatalf("body %v", b)
	}
}

func TestListDeploymentsStates(t *testing.T) {
	c, rec := record(t, map[string]http.HandlerFunc{
		"GET /repos/o/r/deployments": paged(
			`[{"id":3,"sha":"s3","environment":"prod/a","payload":{"app":"a"}},{"id":2,"sha":"s2","environment":"prod/a"}]`,
			`[{"id":1,"sha":"s1","environment":"prod/a"}]`,
		),
		"GET /repos/o/r/deployments/3/statuses": reply(`[]`),
		"GET /repos/o/r/deployments/2/statuses": reply(`[{"state":"error"},{"state":"in_progress"}]`),
		"GET /repos/o/r/deployments/1/statuses": reply(`[{"state":"success"}]`),
	})
	var got []forge.Deployment
	for d, err := range c.ListDeployments(ctx, "o/r", "prod/a") {
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, d)
		if d.State == forge.DeployFailure {
			break
		}
	}
	if len(got) != 2 || got[0].State != forge.DeployInProgress || string(got[0].Payload) != `{"app":"a"}` || got[1].ID != 2 || got[1].State != forge.DeployFailure {
		t.Fatalf("%+v", got)
	}
	if rec.has("GET /repos/o/r/deployments/1/statuses") {
		t.Fatal("iterated past break")
	}
}
