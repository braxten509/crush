//go:build linux && amd64

package filechange

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

// Evaluate the generated filter's supported classic-BPF instructions. This
// tests the actual kernel decision, not whether a later handler ignores it.
func filterDecision(t *testing.T, number uint32, flags uint32) uint32 {
	t.Helper()
	program := mutationFilter(123)
	data := map[uint32]uint32{0: number, 4: unix.AUDIT_ARCH_X86_64, 8: 0, 16 + 8*2: flags}
	var accumulator uint32
	for pc := 0; pc < len(program); pc++ {
		instruction := program[pc]
		switch instruction.Code {
		case unix.BPF_LD | unix.BPF_W | unix.BPF_ABS:
			accumulator = data[instruction.K]
		case unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K:
			if accumulator == instruction.K {
				pc += int(instruction.Jt)
			} else {
				pc += int(instruction.Jf)
			}
		case unix.BPF_JMP | unix.BPF_JGE | unix.BPF_K:
			if accumulator >= instruction.K {
				pc += int(instruction.Jt)
			} else {
				pc += int(instruction.Jf)
			}
		case unix.BPF_JMP | unix.BPF_JSET | unix.BPF_K:
			if accumulator&instruction.K != 0 {
				pc += int(instruction.Jt)
			} else {
				pc += int(instruction.Jf)
			}
		case unix.BPF_RET | unix.BPF_K:
			return instruction.K
		default:
			t.Fatalf("unsupported filter instruction: %x", instruction.Code)
		}
	}
	t.Fatal("filter did not return")
	return 0
}

func TestDeletionSyscallsNeverWaitForReview(t *testing.T) {
	for _, number := range []uint32{unix.SYS_UNLINK, unix.SYS_UNLINKAT, unix.SYS_RMDIR} {
		require.EqualValues(t, unix.SECCOMP_RET_ALLOW, filterDecision(t, number, 0))
	}
	require.EqualValues(t, unix.SECCOMP_RET_USER_NOTIF, filterDecision(t, unix.SYS_OPENAT, unix.O_WRONLY))
	require.EqualValues(t, unix.SECCOMP_RET_ALLOW, filterDecision(t, unix.SYS_OPENAT, unix.O_RDONLY))
}

func TestNoRepositoryDoesNotInstallReviewWatcher(t *testing.T) {
	root := t.TempDir()
	review, err := runObserved(t, root, "printf 'changed' > source.txt")
	require.NoError(t, err)
	require.Nil(t, review)
	data, err := os.ReadFile(filepath.Join(root, "source.txt"))
	require.NoError(t, err)
	require.Equal(t, "changed", string(data))
}

func BenchmarkDeleteBatch(b *testing.B) {
	for _, observed := range []bool{false, true} {
		b.Run(fmt.Sprintf("observed=%t/files=10000", observed), func(b *testing.B) {
			root := visibleRestoreRoot(b)
			directory := filepath.Join(root, "files")
			for range b.N {
				b.StopTimer()
				if err := os.Mkdir(directory, 0700); err != nil {
					b.Fatal(err)
				}
				for index := range 10000 {
					if err := os.WriteFile(filepath.Join(directory, fmt.Sprint(index)), []byte("fixture"), 0600); err != nil {
						b.Fatal(err)
					}
				}
				ctx := context.Background()
				var report *CommandReview
				if observed {
					ctx, report = WithCommandReview(ctx, root)
				}
				cmd := exec.CommandContext(ctx, "rm", "-rf", directory)
				cmd.Dir, cmd.Stdout, cmd.Stderr = root, io.Discard, io.Discard
				b.StartTimer()
				observer, err := StartCommand(ctx, cmd)
				if err != nil {
					b.Fatal(err)
				}
				if err := observer.Wait(cmd); err != nil {
					b.Fatal(err)
				}
				if report != nil && report.Finish() != nil {
					b.Fatal("silent deletions must not create individual review records")
				}
			}
		})
	}
}

func TestRenameDoesNotClaimADeletion(t *testing.T) {
	root := visibleRestoreRoot(t)
	put(t, root, "original.txt", "source")
	review, err := runObserved(t, root, "mv original.txt renamed.txt")
	require.NoError(t, err)
	require.NotNil(t, review)
	require.False(t, review.Deletions)
	require.Len(t, review.Changes, 1)
	require.Equal(t, "move", review.Changes[0].Transfer.Kind)
}
