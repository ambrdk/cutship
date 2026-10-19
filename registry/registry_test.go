package registry

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	ggcr "github.com/google/go-containerregistry/pkg/registry"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/random"
	"github.com/google/go-containerregistry/pkg/v1/remote"
)

const revision = "org.opencontainers.image.revision"

func server(t *testing.T, wrap func(http.Handler) http.Handler) string {
	t.Helper()
	t.Setenv("DOCKER_CONFIG", t.TempDir())
	t.Setenv("CUTSHIP_REGISTRY_USERNAME", "")
	t.Setenv("CUTSHIP_REGISTRY_PASSWORD", "")
	h := ggcr.New()
	if wrap != nil {
		h = wrap(h)
	}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return strings.TrimPrefix(srv.URL, "http://")
}

func image(t *testing.T, labels map[string]string) v1.Image {
	t.Helper()
	img, err := random.Image(64, 1)
	if err != nil {
		t.Fatal(err)
	}
	cf, err := img.ConfigFile()
	if err != nil {
		t.Fatal(err)
	}
	cf = cf.DeepCopy()
	cf.Config.Labels = labels
	cf.OS, cf.Architecture = "linux", "amd64"
	img, err = mutate.ConfigFile(img, cf)
	if err != nil {
		t.Fatal(err)
	}
	return img
}

func push(t *testing.T, ref string, img v1.Image, opts ...remote.Option) {
	t.Helper()
	tag, err := name.NewTag(ref)
	if err != nil {
		t.Fatal(err)
	}
	if err := remote.Write(tag, img, opts...); err != nil {
		t.Fatal(err)
	}
}

func TestRevisionFromLabel(t *testing.T) {
	host := server(t, nil)
	push(t, host+"/ambrdk/alert-relay:v1.0.0", image(t, map[string]string{revision: "abc"}))
	got, err := New().Revision(context.Background(), host+"/ambrdk/alert-relay", "v1.0.0")
	if err != nil || got != "abc" {
		t.Fatalf("%q %v", got, err)
	}
}

func TestRevisionFromIndex(t *testing.T) {
	host := server(t, nil)
	arm := image(t, map[string]string{revision: "arm"})
	amd := image(t, map[string]string{revision: "amd"})
	idx := mutate.AppendManifests(empty.Index,
		mutate.IndexAddendum{Add: arm, Descriptor: v1.Descriptor{Platform: &v1.Platform{OS: "linux", Architecture: "arm64"}}},
		mutate.IndexAddendum{Add: amd, Descriptor: v1.Descriptor{Platform: &v1.Platform{OS: "linux", Architecture: "amd64"}}},
	)
	tag, _ := name.NewTag(host + "/ambrdk/multi:v1.0.0")
	if err := remote.WriteIndex(tag, idx); err != nil {
		t.Fatal(err)
	}
	if got, err := New().Revision(context.Background(), host+"/ambrdk/multi", "v1.0.0"); err != nil || got != "amd" {
		t.Fatalf("%q %v", got, err)
	}
	only := mutate.AppendManifests(empty.Index,
		mutate.IndexAddendum{Add: arm, Descriptor: v1.Descriptor{Platform: &v1.Platform{OS: "linux", Architecture: "arm64"}}},
	)
	tag, _ = name.NewTag(host + "/ambrdk/arm:v1.0.0")
	if err := remote.WriteIndex(tag, only); err != nil {
		t.Fatal(err)
	}
	if got, err := New().Revision(context.Background(), host+"/ambrdk/arm", "v1.0.0"); err != nil || got != "arm" {
		t.Fatalf("arm only: %q %v", got, err)
	}
}

func TestRevisionMissingTag(t *testing.T) {
	host := server(t, nil)
	push(t, host+"/ambrdk/alert-relay:v1.0.0", image(t, nil))
	for _, ref := range [][2]string{{host + "/ambrdk/alert-relay", "v9.9.9"}, {host + "/ambrdk/nothing", "v1.0.0"}} {
		if _, err := New().Revision(context.Background(), ref[0], ref[1]); !errors.Is(err, ErrMissing) {
			t.Errorf("%v: err = %v", ref, err)
		}
	}
}

func TestRevisionNoLabel(t *testing.T) {
	host := server(t, nil)
	push(t, host+"/ambrdk/alert-relay:v1.0.0", image(t, nil))
	if got, err := New().Revision(context.Background(), host+"/ambrdk/alert-relay", "v1.0.0"); err != nil || got != "" {
		t.Fatalf("%q %v", got, err)
	}
}

func basicAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if user, pass, ok := r.BasicAuth(); !ok || user != "ci" || pass != "secret" {
			w.Header().Set("WWW-Authenticate", `Basic realm="test"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func TestRevisionAuthFromEnv(t *testing.T) {
	host := server(t, basicAuth)
	push(t, host+"/ambrdk/private:v1.0.0", image(t, map[string]string{revision: "abc"}), remote.WithAuth(&authn.Basic{Username: "ci", Password: "secret"}))
	if _, err := New().Revision(context.Background(), host+"/ambrdk/private", "v1.0.0"); err == nil || errors.Is(err, ErrMissing) {
		t.Fatalf("anonymous: %v", err)
	}
	t.Setenv("CUTSHIP_REGISTRY_USERNAME", "ci")
	t.Setenv("CUTSHIP_REGISTRY_PASSWORD", "secret")
	if got, err := New().Revision(context.Background(), host+"/ambrdk/private", "v1.0.0"); err != nil || got != "abc" {
		t.Fatalf("with env: %q %v", got, err)
	}
}

func TestRevisionBadReference(t *testing.T) {
	if _, err := New().Revision(context.Background(), "Bad Image", "v1"); err == nil {
		t.Fatal("bad reference accepted")
	}
}
