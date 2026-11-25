package secretguard

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBlocksLaws(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, tool, input, dir string
		blocked                bool
	}{
		// Editing or writing locked files is blocked across tool spellings.
		{"edit LAWS.bend", "Edit", `{"file_path":"/repo/LAWS.bend","old_string":"a","new_string":"b"}`, "", true},
		{"write LAWS.bend", "Write", `{"file_path":"LAWS.bend","content":"x"}`, "", true},
		{"agy edit_file LAWS.bend", "edit_file", `{"Path":"/repo/LAWS.bend","Target":"a","Replacement":"b"}`, "", true},
		{"agy write_file approve", "write_file", `{"Path":"/repo/tools/laws/approve","Content":"x"}`, "", true},
		{"write approved hash", "write", `{"file_path":"tools/laws/approved","content":"abc"}`, "", true},
		{"write approved via dir", "write", `{"file_path":"approved","content":"abc"}`, "/repo/tools/laws", true},
		{"apply_patch LAWS.bend", "apply_patch", `{"patch":"*** Begin Patch\n*** Update File: LAWS.bend\n@@\n-a\n+b\n*** End Patch"}`, "", true},
		{"apply_patch approved", "apply_patch", `{"patch":"*** Begin Patch\n*** Update File: tools/laws/approved\n@@\n-a\n+b\n*** End Patch"}`, "", true},
		{"apply_patch relative in dir", "apply_patch", `{"patch":"*** Begin Patch\n*** Update File: approved\n@@\n-a\n+b\n*** End Patch"}`, "/repo/tools/laws", true},

		// Reading locked files and editing other files that mention LAWS.bend are allowed.
		{"read LAWS.bend", "Read", `{"file_path":"/repo/LAWS.bend"}`, "", false},
		{"agy read_file LAWS.bend", "read_file", `{"Path":"/repo/LAWS.bend"}`, "", false},
		{"agy view_file LAWS.bend", "view_file", `{"AbsolutePath":"/repo/LAWS.bend"}`, "", false},
		{"view LAWS.bend", "view", `{"file_path":"LAWS.bend"}`, "", false},
		{"grep LAWS.bend", "Grep", `{"pattern":"LAW 7","path":"LAWS.bend"}`, "", false},
		{"edit rule book", "Edit", `{"file_path":"/repo/laws/mail.bend","old_string":"a","new_string":"b"}`, "", false},
		{"edit PROOF.bend", "Edit", `{"file_path":"/repo/PROOF.bend","old_string":"a","new_string":"b"}`, "", false},
		{"edit code with LAWS.bend comment", "edit_file", `{"Path":"/repo/src/waiting.rs","Target":"a","Replacement":"// law 3 in LAWS.bend"}`, "", false},
		{"apply_patch code with LAWS.bend comment", "apply_patch", `{"patch":"*** Begin Patch\n*** Update File: src/waiting.rs\n@@\n+// law 3 in LAWS.bend\n*** End Patch"}`, "", false},

		// Shell commands modifying locked files or running approve are blocked.
		{"run approve", "Bash", `{"command":"tools/laws/approve"}`, "", true},
		{"run approve with path", "run_command", `{"CommandLine":"/repo/tools/laws/approve"}`, "", true},
		{"run approve via dir", "Bash", `{"command":"./approve"}`, "/repo/tools/laws", true},
		{"run approve via working_dir field", "Bash", `{"command":"./approve","working_dir":"/repo/tools/laws"}`, "", true},
		{"rm approved via dir", "Bash", `{"command":"rm approved"}`, "/repo/tools/laws", true},
		{"rm laws/approved via tools dir", "Bash", `{"command":"rm laws/approved"}`, "/repo/tools", true},
		{"redirect approved via dir", "Bash", `{"command":"echo x > approved"}`, "/repo/tools/laws", true},
		{"cd then run approve", "Bash", `{"command":"cd tools/laws && ./approve"}`, "", true},
		{"env -C tools/laws approve", "Bash", `{"command":"env -C tools/laws ./approve"}`, "", true},
		{"git -C tools/laws restore approved", "Bash", `{"command":"git -C tools/laws restore approved"}`, "", true},
		{"sh approve", "Bash", `{"command":"sh tools/laws/approve"}`, "", true},
		{"bash -c approve", "Bash", `{"command":"bash -c 'tools/laws/approve'"}`, "", true},
		{"crush bg approve", "Bash", `{"command":"crush bg -- 'tools/laws/approve'"}`, "", true},
		{"crush bg -c tools/laws approve", "Bash", `{"command":"crush bg -c /repo/tools/laws -- './approve'"}`, "", true},
		{"redirect overwrite LAWS.bend", "Bash", `{"command":"echo x > LAWS.bend"}`, "", true},
		{"redirect append approved", "Bash", `{"command":"echo x >> tools/laws/approved"}`, "", true},
		{"sed in place", "Bash", `{"command":"sed -i 's/600/900/' LAWS.bend"}`, "", true},
		{"rm approved", "Bash", `{"command":"rm tools/laws/approved"}`, "", true},
		{"mv LAWS.bend", "Bash", `{"command":"mv LAWS.bend LAWS.bak"}`, "", true},
		{"cp over LAWS.bend", "Bash", `{"command":"cp /tmp/fake LAWS.bend"}`, "", true},
		{"python inline write", "Bash", `{"command":"python3 -c 'open(\"LAWS.bend\",\"w\").write(\"\")'"}`, "", true},
		{"python heredoc write", "Bash", `{"command":"python3 - <<'EOF'\nopen('tools/laws/approved','w').write('x')\nEOF"}`, "", true},
		{"git restore LAWS.bend", "Bash", `{"command":"git restore LAWS.bend"}`, "", true},
		{"git checkout LAWS.bend", "Bash", `{"command":"git checkout -- LAWS.bend"}`, "", true},
		{"glob rm LAWS*", "Bash", `{"command":"rm LAWS*"}`, "", true},
		{"glob rm tools/laws/*", "Bash", `{"command":"rm tools/laws/*"}`, "", true},

		// Shell reads, checks, and normal git commands are allowed.
		{"run laws check", "Bash", `{"command":"tools/laws/check"}`, "", false},
		{"run laws check in tools/laws", "Bash", `{"command":"./check"}`, "/repo/tools/laws", false},
		{"run facts check", "Bash", `{"command":"python3 tools/laws/facts.py --problems"}`, "", false},
		{"cat LAWS.bend", "Bash", `{"command":"cat LAWS.bend"}`, "", false},
		{"sed print LAWS.bend", "Bash", `{"command":"sed -n '1,40p' LAWS.bend"}`, "", false},
		{"grep LAWS.bend", "Bash", `{"command":"grep -n 'LAW 7' LAWS.bend"}`, "", false},
		{"git status check on law files", "Bash", `{"command":"test -z \"$(git --no-pager status --porcelain LAWS.bend tools/laws/approve tools/laws/approved)\""}`, "", false},
		{"git diff LAWS.bend", "Bash", `{"command":"git diff LAWS.bend"}`, "", false},
		{"git commit mentioning LAWS.bend", "Bash", `{"command":"git add -A && git commit -m 'Follow Law 7 in LAWS.bend'"}`, "", false},
		{"rm other bend file", "Bash", `{"command":"rm laws/tmp.bend"}`, "", false},
		{"rm glob in laws dir", "Bash", `{"command":"rm laws/*.bend"}`, "", false},
		{"empty diff header lines", "apply_patch", `{"patch":"--- \n+++ \n"}`, "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.blocked, BlocksLawsInDir(tc.tool, []byte(tc.input), tc.dir))
		})
	}
}

func TestRunHookIO_AGYAndCWD(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	code := RunHookIO(
		strings.NewReader(`{"hookEventName":"PreToolUse","cwd":"/repo","toolCall":{"id":"1","name":"edit_file","args":{"Path":"/repo/LAWS.bend","Target":"a","Replacement":"b"}}}`),
		&stdout,
		&stderr,
	)
	require.Equal(t, 0, code)
	require.Empty(t, stderr.String())
	var denied struct {
		Decision string `json:"decision"`
		Reason   string `json:"reason"`
	}
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &denied))
	require.Equal(t, "deny", denied.Decision)
	require.Equal(t, LawsReason, denied.Reason)

	// AGY view_file on LAWS.bend is allowed.
	stdout.Reset()
	code = RunHookIO(
		strings.NewReader(`{"hookEventName":"PreToolUse","cwd":"/repo","toolCall":{"id":"2","name":"view_file","args":{"AbsolutePath":"/repo/LAWS.bend"}}}`),
		&stdout,
		&stderr,
	)
	require.Equal(t, 0, code)
	var allowed struct {
		Decision string `json:"decision"`
	}
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &allowed))
	require.Equal(t, "allow", allowed.Decision)

	// Relative ./approve inside cwd=/repo/tools/laws is blocked for both hook formats.
	stdout.Reset()
	code = RunHookIO(
		strings.NewReader(`{"hookEventName":"PreToolUse","cwd":"/repo/tools/laws","toolCall":{"id":"3","name":"run_command","args":{"CommandLine":"./approve"}}}`),
		&stdout,
		&stderr,
	)
	require.Equal(t, 0, code)
	denied = struct {
		Decision string `json:"decision"`
		Reason   string `json:"reason"`
	}{}
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &denied))
	require.Equal(t, "deny", denied.Decision)
	require.Equal(t, LawsReason, denied.Reason)

	stderr.Reset()
	code = RunHookIO(
		strings.NewReader(`{"cwd":"/repo/tools/laws","tool_name":"Bash","tool_input":{"command":"./approve"}}`),
		&stdout,
		&stderr,
	)
	require.Equal(t, 2, code)
	require.Equal(t, LawsReason+"\n", stderr.String())
}
