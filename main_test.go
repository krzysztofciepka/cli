package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestRunFlags_Version(t *testing.T) {
	var out, errb bytes.Buffer
	handled, code := runFlags([]string{"-version"}, &out, &errb, "v1.2.3",
		func() error { return errors.New("upgrade should not be called") })
	if !handled || code != 0 {
		t.Fatalf("handled=%v code=%d", handled, code)
	}
	if strings.TrimSpace(out.String()) != "cli v1.2.3" {
		t.Fatalf("out = %q", out.String())
	}
}

func TestRunFlags_UpgradeInvokesCallback(t *testing.T) {
	called := false
	var out, errb bytes.Buffer
	handled, code := runFlags([]string{"-upgrade"}, &out, &errb, "dev",
		func() error { called = true; return nil })
	if !handled || code != 0 || !called {
		t.Fatalf("handled=%v code=%d called=%v", handled, code, called)
	}
}

func TestRunFlags_UpgradeErrorExitsNonZero(t *testing.T) {
	var out, errb bytes.Buffer
	handled, code := runFlags([]string{"-upgrade"}, &out, &errb, "dev",
		func() error { return errors.New("boom") })
	if !handled || code != 1 {
		t.Fatalf("handled=%v code=%d", handled, code)
	}
	if !strings.Contains(errb.String(), "boom") {
		t.Fatalf("stderr = %q", errb.String())
	}
}

func TestRunFlags_NoFlagsFallsThrough(t *testing.T) {
	var out, errb bytes.Buffer
	handled, _ := runFlags(nil, &out, &errb, "dev",
		func() error { return errors.New("must not run") })
	if handled {
		t.Fatal("expected handled=false so the TUI launches")
	}
}
