//go:build !crushtest

package cmd

// installTestSeams does nothing: only crushtest builds carry the test
// seams (seams_crushtest.go; desktop design 5-6, 9.1). CRUSH_TEST_SEAMS is
// ignored here.
func installTestSeams() {}
