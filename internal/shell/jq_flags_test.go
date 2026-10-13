package shell

import (
	"bytes"
	"strings"
	"testing"
)

// TestJQ_Flags pins the command line handling: flag spellings, variables,
// the filter, file arguments after "--", and the usage errors.
func TestJQ_Flags(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		args       []string
		stdin      string
		wantOut    string
		wantErrOut string
		wantErr    string
	}{
		{name: "default filter", args: []string{"jq"}, stdin: `{"a":1}`, wantOut: "{\n  \"a\": 1\n}\n"},
		{name: "raw", args: []string{"jq", "--raw-output", ".a"}, stdin: `{"a":"x"}`, wantOut: "x\n"},
		{name: "join", args: []string{"jq", "-j", ".[]"}, stdin: `["a","b"]`, wantOut: "ab"},
		{name: "slurp compact", args: []string{"jq", "-s", "-c", "."}, stdin: `1 2`, wantOut: "[1,2]\n"},
		{name: "null input", args: []string{"jq", "-n", "-c", "[1]"}, stdin: `{"a":1}`, wantOut: "[1]\n"},
		{name: "raw input", args: []string{"jq", "-R", "-c", "."}, stdin: "a\nb", wantOut: "\"a\"\n\"b\"\n"},
		{name: "arg", args: []string{"jq", "-n", "--arg", "x", "1", "$x"}, wantOut: "\"1\"\n"},
		{name: "argjson", args: []string{"jq", "-n", "--argjson", "x", "1", "$x + 1"}, wantOut: "2\n"},
		{name: "exit status", args: []string{"jq", "-e", ".a"}, stdin: `{"a":false}`, wantOut: "false\n", wantErr: "exit status 1"},
		{name: "help", args: []string{"jq", "-c", "-h", "--bogus"}, wantOut: jqUsage},
		{name: "missing arg value", args: []string{"jq", "--arg", "x"}, wantErrOut: "jq: --arg requires name and value\n", wantErr: "exit status 2"},
		{name: "missing argjson value", args: []string{"jq", "--argjson"}, wantErrOut: "jq: --argjson requires name and value\n", wantErr: "exit status 2"},
		{name: "bad argjson", args: []string{"jq", "--argjson", "x", "{"}, wantErrOut: "jq: invalid JSON for --argjson x: unexpected end of JSON input\n", wantErr: "exit status 2"},
		{name: "unknown option", args: []string{"jq", ".", "-x"}, wantErrOut: "jq: unknown option: -x\n", wantErr: "exit status 2"},
		{name: "runtime error", args: []string{"jq", "-n", `error("boom")`}, wantErrOut: "jq: error: boom\n", wantErr: "exit status 5"},
		{name: "files after double dash", args: []string{"jq", "--", ".a", "missing-file"}, wantErr: "exit status 2"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var stdout, stderr bytes.Buffer
			err := handleJQ(t.Context(), tt.args, strings.NewReader(tt.stdin), &stdout, &stderr)
			if tt.wantErr == "" && err != nil || tt.wantErr != "" && (err == nil || err.Error() != tt.wantErr) {
				t.Fatalf("err = %v, want %q", err, tt.wantErr)
			}
			if got := stdout.String(); got != tt.wantOut {
				t.Fatalf("stdout = %q, want %q", got, tt.wantOut)
			}
			if tt.wantErrOut != "" {
				if got := stderr.String(); got != tt.wantErrOut {
					t.Fatalf("stderr = %q, want %q", got, tt.wantErrOut)
				}
			}
		})
	}
}
