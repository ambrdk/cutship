package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestUsage(t *testing.T) {
	for _, args := range [][]string{nil, {"bogus"}, {"pin"}} {
		var out, errOut bytes.Buffer
		if code := dispatch(args, strings.NewReader(""), &out, &errOut); code != 2 {
			t.Fatalf("%v: code %d", args, code)
		}
		if !strings.Contains(errOut.String(), "usage: ship") {
			t.Fatalf("%v: stderr %q", args, errOut.String())
		}
	}
}
