package main

import (
	"os"

	"github.com/ambrdk/cutship/config"
	"github.com/ambrdk/cutship/forge"
	"github.com/ambrdk/cutship/forge/github"
)

var openForge = func(cfg config.Config, token string) (forge.Reader, error) {
	base := cfg.Forge.URL
	if base == "" {
		base = os.Getenv("GITHUB_API_URL")
	}
	return github.New(base, token)
}
