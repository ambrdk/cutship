package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type botAPI struct {
	linked  int64
	members map[string]string
}

func (b botAPI) serve(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
		params := form(t, r)
		var result string
		switch method {
		case "getMe":
			result = `{"id":42,"is_bot":true,"first_name":"cutship","username":"cutship_bot"}`
		case "getChat":
			result = fmt.Sprintf(`{"id":%s,"type":"channel","linked_chat_id":%d}`, params["chat_id"], b.linked)
		case "getChatMember":
			status, ok := b.members[params["chat_id"]]
			if !ok {
				_, _ = fmt.Fprint(w, `{"ok":false,"error_code":400,"description":"Bad Request: chat not found"}`)
				return
			}
			result = `{"status":"` + status + `","user":{"id":42,"is_bot":true,"first_name":"cutship"}`
			if status == "administrator" {
				result += `,"can_post_messages":true,"can_edit_messages":true`
			}
			if status == "administrator-readonly" {
				result = `{"status":"administrator","user":{"id":42,"is_bot":true,"first_name":"cutship"},"can_post_messages":true`
			}
			result += `}`
		default:
			t.Errorf("unexpected method %s", method)
		}
		_, _ = fmt.Fprintf(w, `{"ok":true,"result":%s}`, result)
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func form(t *testing.T, r *http.Request) map[string]string {
	t.Helper()
	out := map[string]string{}
	mediaType, params, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	switch {
	case strings.HasPrefix(mediaType, "multipart/"):
		mr := multipart.NewReader(r.Body, params["boundary"])
		for {
			p, err := mr.NextPart()
			if err != nil {
				break
			}
			v, _ := io.ReadAll(p)
			out[p.FormName()] = string(v)
		}
	case mediaType == "application/json":
		var raw map[string]any
		_ = json.NewDecoder(r.Body).Decode(&raw)
		for k, v := range raw {
			out[k] = fmt.Sprint(v)
		}
	default:
		_ = r.ParseForm()
		for k := range r.Form {
			out[k] = r.Form.Get(k)
		}
	}
	return out
}

func messages(problems []Problem) string {
	var out []string
	for _, p := range problems {
		out = append(out, fmt.Sprintf("%v %s", p.Error, p.Message))
	}
	return strings.Join(out, "\n")
}

func TestTelegramCheckClean(t *testing.T) {
	url := botAPI{linked: -100999, members: map[string]string{"-100123": "administrator", "-100999": "administrator"}}.serve(t)
	if got := Check(context.Background(), "1:x", url, "-100123"); len(got) != 0 {
		t.Fatalf("problems:\n%s", messages(got))
	}
}

func TestTelegramCheckNotAdmin(t *testing.T) {
	for name, members := range map[string]map[string]string{
		"member":          {"-100123": "member", "-100999": "administrator"},
		"cannot edit":     {"-100123": "administrator-readonly", "-100999": "administrator"},
		"not in channel":  {"-100999": "administrator"},
		"group not admin": {"-100123": "administrator", "-100999": "member"},
	} {
		t.Run(name, func(t *testing.T) {
			url := botAPI{linked: -100999, members: members}.serve(t)
			got := Check(context.Background(), "1:x", url, "-100123")
			if len(got) != 1 || !got[0].Error {
				t.Fatalf("problems:\n%s", messages(got))
			}
		})
	}
}

func TestTelegramCheckNoDiscussion(t *testing.T) {
	url := botAPI{members: map[string]string{"-100123": "administrator"}}.serve(t)
	got := Check(context.Background(), "1:x", url, "-100123")
	if len(got) != 1 || got[0].Error || !strings.Contains(got[0].Message, "discussion group") {
		t.Fatalf("problems:\n%s", messages(got))
	}
}

func TestTelegramCheckUnreachable(t *testing.T) {
	got := Check(context.Background(), "1:x", "http://127.0.0.1:1", "-100123")
	if len(got) != 1 || !got[0].Error {
		t.Fatalf("problems:\n%s", messages(got))
	}
}
