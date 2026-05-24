package main

import (
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
