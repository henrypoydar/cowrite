package main

import "testing"

func TestParseArgs(t *testing.T) {
	cases := []struct {
		name    string
		args    []string
		path    string
		with    string
		wantErr bool
	}{
		{"file only", []string{"draft.md"}, "draft.md", "", false},
		{"with before file", []string{"--with", "claude", "draft.md"}, "draft.md", "claude", false},
		{"with after file", []string{"draft.md", "--with", "claude"}, "draft.md", "claude", false},
		{"with equals form", []string{"--with=claude -p hi", "draft.md"}, "draft.md", "claude -p hi", false},
		{"no file", []string{"--with", "claude"}, "", "", true},
		{"with missing command", []string{"draft.md", "--with"}, "", "", true},
		{"with empty equals", []string{"--with=", "draft.md"}, "", "", true},
		{"unknown flag", []string{"--frob", "draft.md"}, "", "", true},
		{"two files", []string{"a.md", "b.md"}, "", "", true},
		{"nothing", nil, "", "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path, with, err := parseArgs(c.args)
			if (err != nil) != c.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, c.wantErr)
			}
			if path != c.path || with != c.with {
				t.Errorf("got (%q, %q), want (%q, %q)", path, with, c.path, c.with)
			}
		})
	}
}
