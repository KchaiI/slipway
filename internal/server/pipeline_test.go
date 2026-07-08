package server

import (
	"strings"
	"testing"
)

func TestPickPushedRef(t *testing.T) {
	const (
		zero = "0000000000000000000000000000000000000000"
		a    = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		b    = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	)
	cases := []struct {
		name    string
		input   string
		wantSHA string
		wantRef string
	}{
		{
			name:    "single branch",
			input:   zero + " " + a + " refs/heads/feature\n",
			wantSHA: a,
			wantRef: "refs/heads/feature",
		},
		{
			name:    "main preferred over other branches",
			input:   zero + " " + a + " refs/heads/feature\n" + zero + " " + b + " refs/heads/main\n",
			wantSHA: b,
			wantRef: "refs/heads/main",
		},
		{
			name:    "tags ignored",
			input:   zero + " " + a + " refs/tags/v1.0\n",
			wantSHA: "",
			wantRef: "",
		},
		{
			name:    "branch deletion ignored",
			input:   a + " " + zero + " refs/heads/main\n",
			wantSHA: "",
			wantRef: "",
		},
		{
			name:    "empty input",
			input:   "",
			wantSHA: "",
			wantRef: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sha, ref := pickPushedRef(strings.NewReader(tc.input))
			if sha != tc.wantSHA || ref != tc.wantRef {
				t.Fatalf("pickPushedRef() = (%q, %q), want (%q, %q)", sha, ref, tc.wantSHA, tc.wantRef)
			}
		})
	}
}
