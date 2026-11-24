package secureentry

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/charmbracelet/crush/internal/question"
	"github.com/stretchr/testify/require"
)

// TestWalletSaveUsesStdinAndKeepsLines: a wallet entry gets the value on
// kwallet-query's standard input, never on its command line (where other
// programs could see it), with line breaks and tabs kept.
func TestWalletSaveUsesStdinAndKeepsLines(t *testing.T) {
	dir := t.TempDir()
	tool := filepath.Join(dir, "kwallet-query")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >> '" + dir + "/args'\ncase \" $* \" in *' -w '*) cat > '" + dir + "/stdin' ;; esac\n"
	require.NoError(t, os.WriteFile(tool, []byte(script), 0o700))
	previous := walletTool
	walletTool = tool
	t.Cleanup(func() { walletTool = previous })

	forms := make(chan *Form, 1)
	t.Cleanup(AttachForms(func(f *Form) { forms <- f }))
	req := question.Request{ID: "batch", Questions: []question.Question{{ID: "note", Type: question.TypeSecureEntry, Text: "Note", Description: "d"}}}
	form, err := OpenForm("session", req, map[string]Spec{"note": {Wallet: &WalletSpec{Folder: "Crush", Entry: "note"}}})
	require.NoError(t, err)
	<-forms

	value := "dummy line one\n\tdummy line two"
	require.NoError(t, form.Submit([]question.Answer{{QuestionID: "note"}}, map[string][]byte{"note": []byte(value)}))
	require.Equal(t, StatusSaved, (<-form.Result()).Statuses["note"])

	stored, err := os.ReadFile(filepath.Join(dir, "stdin"))
	require.NoError(t, err)
	require.Equal(t, value, string(stored))
	args, err := os.ReadFile(filepath.Join(dir, "args"))
	require.NoError(t, err)
	require.NotContains(t, string(args), "dummy")
}
