package github

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAPIPaginates(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" || r.Header.Get("X-GitHub-Api-Version") != "2022-11-28" {
			http.Error(w, "headers", 400)
			return
		}
		if r.URL.Query().Get("page") == "2" {
			_, _ = fmt.Fprint(w, `[{"id":2,"body":"b","user":{"login":"x"}}]`)
			return
		}
		w.Header().Set("Link", fmt.Sprintf(`<%s/repos/o/r/issues/6/comments?per_page=100&page=2>; rel="next"`, srv.URL))
		_, _ = fmt.Fprint(w, `[{"id":1,"body":"a","user":{"login":"github-actions[bot]"}}]`)
	}))
	defer srv.Close()
	comments, err := New(srv.URL, "tok").IssueComments(context.Background(), "o/r", 6)
	if err != nil {
		t.Fatal(err)
	}
	if len(comments) != 2 || comments[0].ID != 1 || comments[0].User.Login != "github-actions[bot]" || comments[1].ID != 2 {
		t.Fatalf("comments %+v", comments)
	}
}

func TestViewer(t *testing.T) {
	for name, tc := range map[string]struct {
		code  int
		body  string
		login string
		err   error
	}{
		"personal token":  {200, `{"login":"ambrdk-deployer"}`, "ambrdk-deployer", nil},
		"actions token":   {403, `{"message":"Resource not accessible by integration"}`, ActionsBot, nil},
		"rate limited":    {403, `{"message":"API rate limit exceeded"}`, "", ErrForbidden},
		"bad credentials": {401, `{"message":"Bad credentials"}`, "", nil},
	} {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.code)
				_, _ = fmt.Fprint(w, tc.body)
			}))
			defer srv.Close()
			u, err := New(srv.URL, "tok").Viewer(context.Background())
			if u.Login != tc.login {
				t.Fatalf("login %q, want %q", u.Login, tc.login)
			}
			if tc.login == "" && err == nil {
				t.Fatal("no error")
			}
			if tc.err != nil && !errors.Is(err, tc.err) {
				t.Fatalf("err %v, want %v", err, tc.err)
			}
		})
	}
}

func TestRunArtifactUnzips(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range map[string]string{"deploy-report.json": `{"version":1}`, "extra.json": `{"deploy_apps":["alert-relay"]}`} {
		f, _ := zw.Create(name)
		_, _ = f.Write([]byte(body))
	}
	_ = zw.Close()
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/o/r/actions/runs/42/artifacts":
			if r.URL.Query().Get("name") != "deploy-report" {
				http.Error(w, "name", 400)
				return
			}
			_, _ = fmt.Fprintf(w, `{"artifacts":[{"name":"deploy-report","archive_download_url":"%s/zip"}]}`, srv.URL)
		case "/zip":
			_, _ = w.Write(buf.Bytes())
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	files, err := New(srv.URL, "tok").RunArtifact(context.Background(), "o/r", 42, "deploy-report")
	if err != nil {
		t.Fatal(err)
	}
	if string(files["deploy-report.json"]) != `{"version":1}` || string(files["extra.json"]) != `{"deploy_apps":["alert-relay"]}` {
		t.Fatalf("files %v", files)
	}
}

func TestRunArtifactMissing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, `{"artifacts":[]}`)
	}))
	defer srv.Close()
	_, err := New(srv.URL, "tok").RunArtifact(context.Background(), "o/r", 42, "deploy-report")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err %v", err)
	}
}

func TestFileAtNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/o/r/contents/apps/x/app.yml" && r.URL.Query().Get("ref") == "abc" {
			_, _ = fmt.Fprint(w, "image: ghcr.io/ambrdk/x:v1.0.0\n")
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	api := New(srv.URL, "tok")
	body, err := api.FileAt(context.Background(), "o/r", "apps/x/app.yml", "abc")
	if err != nil || string(body) != "image: ghcr.io/ambrdk/x:v1.0.0\n" {
		t.Fatalf("body %q err %v", body, err)
	}
	if _, err := api.FileAt(context.Background(), "o/r", "apps/y/app.yml", "abc"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err %v", err)
	}
}
