package main

import "testing"

func TestParsePRRef(t *testing.T) {
	ref, err := parsePRRef("https://bitbucket.org/myws/myrepo/pull-requests/42")
	if err != nil {
		t.Fatalf("valid URL: %v", err)
	}
	if ref != (PRRef{Workspace: "myws", Repo: "myrepo", Number: 42}) {
		t.Errorf("got %+v", ref)
	}
}

func TestParsePRRefRejects(t *testing.T) {
	for _, url := range []string{
		"https://github.com/myws/myrepo/pull-requests/42",
		"https://bitbucket.org/myws/myrepo/pull-requests",
		"https://bitbucket.org/myws/myrepo/pulls/42",
		"https://bitbucket.org/myws/myrepo/pull-requests/42/",
		"https://bitbucket.org/myws/myrepo/pull-requests/abc",
		"",
	} {
		if _, err := parsePRRef(url); err == nil {
			t.Errorf("expected error for %q", url)
		}
	}
}
