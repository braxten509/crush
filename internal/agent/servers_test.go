package agent

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestServerTitle(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		process Process
		want    string
	}{
		{Process{Name: "Dev server", Command: "npm run dev"}, "Dev server"},
		{Process{Command: "PORT=5173 /usr/bin/vite --port 5173"}, "vite"},
		{Process{}, ""},
	} {
		require.Equal(t, tc.want, Server{Process: tc.process}.Title())
	}
}
