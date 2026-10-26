// Package linediff finds the fewest changed lines between two texts, the
// way Git's default diff counts them. go-udiff's own line diff stops
// searching after about 100 differences and then guesses, so larger
// changes came out several times too big.
package linediff

import (
	"strings"

	"github.com/aymanbagabas/go-udiff"
)

// maxCost bounds the search in one part of the texts; past it that part
// counts as fully replaced. Lines found only on one side are set aside
// first, so real edits rarely come near it.
const maxCost = 8192

// Edits returns line edits that turn before into after, for
// udiff.ToUnifiedDiff.
func Edits(before, after string) []udiff.Edit {
	if before == after {
		return nil
	}
	aLines, aOffsets := split(before)
	bLines, _ := split(after)
	ids := map[string]int{}
	intern := func(lines []string) []int {
		out := make([]int, len(lines))
		for i, line := range lines {
			id, ok := ids[line]
			if !ok {
				id = len(ids)
				ids[line] = id
			}
			out[i] = id
		}
		return out
	}
	a, b := intern(aLines), intern(bLines)
	changedA, changedB := changes(a, b)

	var edits []udiff.Edit
	i, j := 0, 0
	for i < len(a) || j < len(b) {
		if i < len(a) && j < len(b) && !changedA[i] && !changedB[j] {
			i++
			j++
			continue
		}
		i2, j2 := i, j
		for i2 < len(a) && changedA[i2] {
			i2++
		}
		for j2 < len(b) && changedB[j2] {
			j2++
		}
		edits = append(edits, udiff.Edit{Start: aOffsets[i], End: aOffsets[i2], New: strings.Join(bLines[j:j2], "")})
		i, j = i2, j2
	}
	return edits
}

// split cuts text into lines that keep their "\n"; offsets has one more
// entry than lines, the end of the text.
func split(text string) ([]string, []int) {
	var lines []string
	offsets := []int{0}
	for start := 0; start < len(text); {
		end := strings.IndexByte(text[start:], '\n')
		if end < 0 {
			end = len(text)
		} else {
			end += start + 1
		}
		lines = append(lines, text[start:end])
		offsets = append(offsets, end)
		start = end
	}
	return lines, offsets
}

// changes marks the lines of a and b outside one longest common
// subsequence.
func changes(a, b []int) ([]bool, []bool) {
	changedA, changedB := make([]bool, len(a)), make([]bool, len(b))
	// A line missing from the other side can't be in common, and setting
	// it aside keeps the search small when much of a file is rewritten.
	inA, inB := map[int]bool{}, map[int]bool{}
	for _, id := range a {
		inA[id] = true
	}
	for _, id := range b {
		inB[id] = true
	}
	var ra, rb, mapA, mapB []int
	for i, id := range a {
		if inB[id] {
			ra, mapA = append(ra, id), append(mapA, i)
		} else {
			changedA[i] = true
		}
	}
	for j, id := range b {
		if inA[id] {
			rb, mapB = append(rb, id), append(mapB, j)
		} else {
			changedB[j] = true
		}
	}
	d := differ{a: ra, b: rb, changedA: make([]bool, len(ra)), changedB: make([]bool, len(rb))}
	d.compare(0, len(ra), 0, len(rb))
	for i, changed := range d.changedA {
		changedA[mapA[i]] = changed
	}
	for j, changed := range d.changedB {
		changedB[mapB[j]] = changed
	}
	return changedA, changedB
}

type differ struct {
	a, b               []int
	changedA, changedB []bool
}

func (d *differ) compare(a0, a1, b0, b1 int) {
	for a0 < a1 && b0 < b1 && d.a[a0] == d.b[b0] {
		a0++
		b0++
	}
	for a0 < a1 && b0 < b1 && d.a[a1-1] == d.b[b1-1] {
		a1--
		b1--
	}
	if a0 == a1 || b0 == b1 {
		d.replace(a0, a1, b0, b1)
		return
	}
	x, y, ok := d.middle(a0, a1, b0, b1)
	if !ok || (x == a0 && y == b0) || (x == a1 && y == b1) {
		d.replace(a0, a1, b0, b1)
		return
	}
	d.compare(a0, x, b0, y)
	d.compare(x, a1, y, b1)
}

func (d *differ) replace(a0, a1, b0, b1 int) {
	for i := a0; i < a1; i++ {
		d.changedA[i] = true
	}
	for j := b0; j < b1; j++ {
		d.changedB[j] = true
	}
}

// middle finds a point on a shortest edit path by searching from both ends
// at once (Myers 1986, as diff-match-patch's bisect), in linear memory.
func (d *differ) middle(a0, a1, b0, b1 int) (int, int, bool) {
	a, b := d.a[a0:a1], d.b[b0:b1]
	n, m := len(a), len(b)
	maxD := (n + m + 1) / 2
	offset, length := maxD, 2*maxD+2
	forward, backward := make([]int, length), make([]int, length)
	for i := range forward {
		forward[i], backward[i] = -1, -1
	}
	forward[offset+1], backward[offset+1] = 0, 0
	delta := n - m
	odd := delta%2 != 0
	var k1start, k1end, k2start, k2end int
	for step := 0; step <= min(maxD, maxCost); step++ {
		for k1 := -step + k1start; k1 <= step-k1end; k1 += 2 {
			at := offset + k1
			var x1 int
			if k1 == -step || (k1 != step && forward[at-1] < forward[at+1]) {
				x1 = forward[at+1]
			} else {
				x1 = forward[at-1] + 1
			}
			y1 := x1 - k1
			for x1 < n && y1 < m && a[x1] == b[y1] {
				x1++
				y1++
			}
			forward[at] = x1
			switch {
			case x1 > n:
				k1end += 2
			case y1 > m:
				k1start += 2
			case odd:
				other := offset + delta - k1
				if other >= 0 && other < length && backward[other] != -1 && x1 >= n-backward[other] {
					return a0 + x1, b0 + y1, true
				}
			}
		}
		for k2 := -step + k2start; k2 <= step-k2end; k2 += 2 {
			at := offset + k2
			var x2 int
			if k2 == -step || (k2 != step && backward[at-1] < backward[at+1]) {
				x2 = backward[at+1]
			} else {
				x2 = backward[at-1] + 1
			}
			y2 := x2 - k2
			for x2 < n && y2 < m && a[n-x2-1] == b[m-y2-1] {
				x2++
				y2++
			}
			backward[at] = x2
			switch {
			case x2 > n:
				k2end += 2
			case y2 > m:
				k2start += 2
			case !odd:
				other := offset + delta - k2
				if other >= 0 && other < length && forward[other] != -1 {
					x1 := forward[other]
					y1 := offset + x1 - other
					if x1 >= n-x2 {
						return a0 + x1, b0 + y1, true
					}
				}
			}
		}
	}
	return 0, 0, false
}
