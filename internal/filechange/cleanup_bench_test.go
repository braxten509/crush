package filechange

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// Measure the same batch sizes for visible source and hidden cache files.
// Setup and fixture removal are outside the measured portion.
func BenchmarkTrackBatch(b *testing.B) {
	for _, hidden := range []bool{false, true} {
		for _, count := range []int{1000, 10000} {
			b.Run(fmt.Sprintf("hidden=%t/files=%d", hidden, count), func(b *testing.B) {
				root, err := os.MkdirTemp(".", "cleanup-bench-")
				if err != nil {
					b.Fatal(err)
				}
				root, err = filepath.Abs(root)
				if err != nil {
					b.Fatal(err)
				}
				b.Cleanup(func() { os.RemoveAll(root) })
				directory := root
				if hidden {
					directory = filepath.Join(root, ".cache")
					if err := os.Mkdir(directory, 0700); err != nil {
						b.Fatal(err)
					}
				}
				paths := make([]string, count)
				for i := range count {
					paths[i] = filepath.Join(directory, fmt.Sprint(i))
					if err := os.WriteFile(paths[i], []byte("fixture content\n"), 0600); err != nil {
						b.Fatal(err)
					}
				}
				b.ResetTimer()
				for range b.N {
					tracker, err := New(b.Context(), root)
					if err != nil {
						b.Fatal(err)
					}
					tracker.store = newSnapshotStore()
					for _, path := range paths {
						tracker.Track(path)
					}
					tracker.store.releaseAll()
				}
			})
		}
	}
}
