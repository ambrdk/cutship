package registry

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/remote/transport"
)

const RevisionLabel = "org.opencontainers.image.revision"

var ErrMissing = errors.New("image tag not found")

type Registry interface {
	Revision(ctx context.Context, image, tag string) (string, error)
}

type oci struct{}

func New() Registry {
	return oci{}
}

type envKeychain struct{}

func (envKeychain) Resolve(authn.Resource) (authn.Authenticator, error) {
	user, pass := os.Getenv("CUTSHIP_REGISTRY_USERNAME"), os.Getenv("CUTSHIP_REGISTRY_PASSWORD")
	if user == "" && pass == "" {
		return authn.Anonymous, nil
	}
	return &authn.Basic{Username: user, Password: pass}, nil
}

var amd64 = v1.Platform{OS: "linux", Architecture: "amd64"}

func missing(err error) bool {
	var terr *transport.Error
	if !errors.As(err, &terr) {
		return false
	}
	if terr.StatusCode == http.StatusNotFound {
		return true
	}
	for _, d := range terr.Errors {
		if d.Code == transport.ManifestUnknownErrorCode || d.Code == transport.NameUnknownErrorCode {
			return true
		}
	}
	return false
}

func (oci) Revision(ctx context.Context, image, tag string) (string, error) {
	ref, err := name.NewTag(image + ":" + tag)
	if err != nil {
		return "", err
	}
	desc, err := remote.Get(ref, remote.WithContext(ctx), remote.WithAuthFromKeychain(authn.NewMultiKeychain(authn.DefaultKeychain, envKeychain{})))
	if missing(err) {
		return "", fmt.Errorf("%s: %w", ref, ErrMissing)
	}
	if err != nil {
		return "", err
	}
	img, err := pick(desc)
	if err != nil {
		return "", fmt.Errorf("%s: %w", ref, err)
	}
	cf, err := img.ConfigFile()
	if err != nil {
		return "", fmt.Errorf("%s config: %w", ref, err)
	}
	return cf.Config.Labels[RevisionLabel], nil
}

func pick(desc *remote.Descriptor) (v1.Image, error) {
	if !desc.MediaType.IsIndex() {
		return desc.Image()
	}
	idx, err := desc.ImageIndex()
	if err != nil {
		return nil, err
	}
	manifest, err := idx.IndexManifest()
	if err != nil {
		return nil, err
	}
	if len(manifest.Manifests) == 0 {
		return nil, errors.New("the image index has no manifests")
	}
	chosen := manifest.Manifests[0]
	for _, m := range manifest.Manifests {
		if m.Platform != nil && m.Platform.Satisfies(amd64) {
			chosen = m
			break
		}
	}
	return idx.Image(chosen.Digest)
}
