package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestVersionFlag(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := dispatch([]string{"--version"}, strings.NewReader(""), &out, &errOut); code != 0 || out.String() != "cutship dev\n" {
		t.Fatalf("%d %q %q", code, out.String(), errOut.String())
	}
}

func TestUsage(t *testing.T) {
	for _, args := range [][]string{nil, {"bogus"}, {"pin"}} {
		var out, errOut bytes.Buffer
		if code := dispatch(args, strings.NewReader(""), &out, &errOut); code != 2 {
			t.Fatalf("%v: code %d", args, code)
		}
		if !strings.Contains(errOut.String(), "usage: cutship") {
			t.Fatalf("%v: stderr %q", args, errOut.String())
		}
	}
}
