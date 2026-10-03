//go:build linux && amd64

package filechange

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// BenchmarkTracing compares identical local work with and without review
// observation. No network, model calls, or user data are involved.
func BenchmarkTracing(b *testing.B) {
	programs := map[string]string{
		"read_syscalls": "import os\nfor _ in range(20000): os.stat('.')\n",
		"create_files":  "from pathlib import Path\nfor i in range(1000): Path(str(i)).write_text('sample\\n'*20)\n",
		"rewrite_file":  "from pathlib import Path\np=Path('file')\nfor i in range(1000): p.write_text(str(i))\n",
		"copy_tree":     "",
	}
	for name, program := range programs {
		modes := []string{"plain", "observed"}
		if name == "copy_tree" {
			modes = append(modes, "full_snapshots")
		}
		for _, mode := range modes {
			observed := mode != "plain"
			b.Run(name+"/"+mode, func(b *testing.B) {
				root := b.TempDir()
				if name == "copy_tree" {
					if err := os.Mkdir(filepath.Join(root, "source"), 0o700); err != nil {
						b.Fatal(err)
					}
					for i := range 1000 {
						if err := os.WriteFile(filepath.Join(root, "source", fmt.Sprint(i)), []byte(strings.Repeat("line\n", 4096)), 0o600); err != nil {
							b.Fatal(err)
						}
					}
				}
				b.ResetTimer()
				for range b.N {
					ctx := context.Background()
					var report *CommandReview
					if observed {
						ctx, report = WithCommandReview(ctx, root)
					}
					cmd := exec.CommandContext(ctx, "python3", "-c", program)
					if name == "copy_tree" {
						b.StopTimer()
						if err := os.RemoveAll(filepath.Join(root, "destination")); err != nil {
							b.Fatal(err)
						}
						b.StartTimer()
						cmd = exec.CommandContext(ctx, "cp", "-r", "source", "destination")
						// The same cp executable and work, but an unrecognized
						// argv[0] requests the old full-snapshot review path.
						if mode == "full_snapshots" {
							cmd.Args[0] = "copy-fixture"
						}
					}
					cmd.Dir, cmd.Stdout, cmd.Stderr = root, io.Discard, io.Discard
					observer, err := StartCommand(ctx, cmd)
					if err != nil {
						b.Fatal(err)
					}
					if err := observer.Wait(cmd); err != nil {
						b.Fatal(err)
					}
					if report != nil {
						report.Finish()
					}
				}
			})
		}
	}
}
