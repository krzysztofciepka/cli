package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestSelectExecutables(t *testing.T) {
	tests := []struct {
		name          string
		execs         []pathExec
		explicitOwned map[string]bool
		ownedPaths    map[string]bool
		want          []string
	}{
		{
			name:          "explicit-owned kept",
			execs:         []pathExec{{name: "git", path: "/usr/bin/git"}},
			explicitOwned: map[string]bool{"git": true},
			ownedPaths:    map[string]bool{"/usr/bin/git": true},
			want:          []string{"git"},
		},
		{
			name:          "manual (unowned) kept",
			execs:         []pathExec{{name: "clipad", path: "/usr/local/bin/clipad"}},
			explicitOwned: map[string]bool{},
			ownedPaths:    map[string]bool{},
			want:          []string{"clipad"},
		},
		{
			name:          "dependency-only dropped",
			execs:         []pathExec{{name: "helperlib", path: "/usr/bin/helperlib"}},
			explicitOwned: map[string]bool{},
			ownedPaths:    map[string]bool{"/usr/bin/helperlib": true},
			want:          nil,
		},
		{
			name: "manual shadows owned (first wins)",
			execs: []pathExec{
				{name: "foo", path: "/usr/local/bin/foo"},
				{name: "foo", path: "/usr/bin/foo"},
			},
			explicitOwned: map[string]bool{},
			ownedPaths:    map[string]bool{"/usr/bin/foo": true},
			want:          []string{"foo"},
		},
		{
			name: "dependency first shadows later manual copy (dropped)",
			execs: []pathExec{
				{name: "foo", path: "/usr/bin/foo"},
				{name: "foo", path: "/home/u/.local/bin/foo"},
			},
			explicitOwned: map[string]bool{},
			ownedPaths:    map[string]bool{"/usr/bin/foo": true},
			want:          nil,
		},
		{
			name: "no pacman keeps everything, sorted",
			execs: []pathExec{
				{name: "zeta", path: "/usr/bin/zeta"},
				{name: "alpha", path: "/usr/bin/alpha"},
			},
			explicitOwned: map[string]bool{},
			ownedPaths:    map[string]bool{},
			want:          []string{"alpha", "zeta"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := selectExecutables(tt.execs, tt.explicitOwned, tt.ownedPaths)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestScanPathEntries(t *testing.T) {
	dirA := t.TempDir()
	dirB := t.TempDir()

	mustWrite := func(dir, name string, mode os.FileMode) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("#!/bin/sh\n"), mode); err != nil {
			t.Fatal(err)
		}
		return p
	}

	mustWrite(dirA, "alpha", 0o755)   // executable -> included
	mustWrite(dirA, "notexec", 0o644) // not executable -> skipped
	mustWrite(dirA, ".hidden", 0o755) // dotfile -> skipped
	target := mustWrite(dirA, "linktarget", 0o755)
	mustWrite(dirA, "dup", 0o755) // duplicate name, dirA wins
	mustWrite(dirB, "dup", 0o755) // duplicate name, shadowed
	mustWrite(dirB, "beta", 0o755)

	link := filepath.Join(dirB, "blink") // symlink to an executable
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	t.Setenv("PATH", dirA+":"+dirB)

	byName := map[string]string{}
	for _, e := range scanPathEntries() {
		if _, ok := byName[e.name]; ok {
			t.Fatalf("duplicate name in result: %s", e.name)
		}
		byName[e.name] = e.path
	}

	if _, ok := byName["alpha"]; !ok {
		t.Errorf("alpha should be included")
	}
	if _, ok := byName["notexec"]; ok {
		t.Errorf("notexec should be skipped (not executable)")
	}
	if _, ok := byName[".hidden"]; ok {
		t.Errorf("dotfile should be skipped")
	}
	if _, ok := byName["blink"]; !ok {
		t.Errorf("symlink blink should be included")
	}
	if _, ok := byName["beta"]; !ok {
		t.Errorf("beta should be included")
	}
	if byName["dup"] != filepath.Join(dirA, "dup") {
		t.Errorf("dup should resolve to dirA (first on PATH), got %s", byName["dup"])
	}
}
