package ops

import (
	"fmt"
	"strings"
)

// UnifiedDiff renders a unified diff between two texts with the given labels
// and 3 lines of context (same presentation as haproxy-manager's diff).
func UnifiedDiff(oldText, newText, oldLabel, newLabel string) string {
	a := strings.Split(strings.ReplaceAll(oldText, "\r\n", "\n"), "\n")
	b := strings.Split(strings.ReplaceAll(newText, "\r\n", "\n"), "\n")
	ops := diffOps(a, b)

	const ctx = 3
	// positions of changed chunks
	var changed []int
	for i, op := range ops {
		if op.kind != "eq" {
			changed = append(changed, i)
		}
	}
	if len(changed) == 0 {
		return ""
	}

	// group changes whose gap is within 2*ctx lines into the same hunk
	var groups [][2]int // inclusive start/end indices into ops (of changed positions)
	start := changed[0]
	prev := changed[0]
	for _, c := range changed[1:] {
		if c-prev <= 2*ctx {
			prev = c
			continue
		}
		groups = append(groups, [2]int{start, prev})
		start = c
		prev = c
	}
	groups = append(groups, [2]int{start, prev})

	var sb strings.Builder
	fmt.Fprintf(&sb, "--- %s\n+++ %s\n", oldLabel, newLabel)
	for _, g := range groups {
		s := g[0] - ctx
		if s < 0 {
			s = 0
		}
		e := g[1] + ctx + 1
		if e > len(ops) {
			e = len(ops)
		}
		// compute hunk line numbers (1-based, like GNU diff)
		aStart, bStart := 1, 1
		for _, op := range ops[:s] {
			switch op.kind {
			case "eq":
				aStart++
				bStart++
			case "del":
				aStart++
			case "ins":
				bStart++
			}
		}
		aCount, bCount := 0, 0
		for _, op := range ops[s:e] {
			switch op.kind {
			case "eq":
				aCount++
				bCount++
			case "del":
				aCount++
			case "ins":
				bCount++
			}
		}
		fmt.Fprintf(&sb, "@@ -%d,%d +%d,%d @@\n", aStart, aCount, bStart, bCount)
		for _, op := range ops[s:e] {
			switch op.kind {
			case "eq":
				sb.WriteString(" " + op.text + "\n")
			case "del":
				sb.WriteString("-" + op.text + "\n")
			case "ins":
				sb.WriteString("+" + op.text + "\n")
			}
		}
	}
	return sb.String()
}

type diffChunk struct {
	kind string // eq | del | ins
	text string
}

// diffOps computes an LCS-based line diff (fine for small config files).
func diffOps(a, b []string) []diffChunk {
	n, m := len(a), len(b)
	// trim common prefix/suffix to keep the LCS table small
	pre := 0
	for pre < n && pre < m && a[pre] == b[pre] {
		pre++
	}
	suf := 0
	for suf < n-pre && suf < m-pre && a[n-1-suf] == b[m-1-suf] {
		suf++
	}
	ca, cb := a[pre:n-suf], b[pre:m-suf]

	var ops []diffChunk
	for i := 0; i < pre; i++ {
		ops = append(ops, diffChunk{"eq", a[i]})
	}
	lcs := lcsTable(ca, cb)
	i, j := 0, 0
	for i < len(ca) && j < len(cb) {
		if ca[i] == cb[j] {
			ops = append(ops, diffChunk{"eq", ca[i]})
			i++
			j++
		} else if lcs[i+1][j] >= lcs[i][j+1] {
			ops = append(ops, diffChunk{"del", ca[i]})
			i++
		} else {
			ops = append(ops, diffChunk{"ins", cb[j]})
			j++
		}
	}
	for ; i < len(ca); i++ {
		ops = append(ops, diffChunk{"del", ca[i]})
	}
	for ; j < len(cb); j++ {
		ops = append(ops, diffChunk{"ins", cb[j]})
	}
	for k := 0; k < suf; k++ {
		ops = append(ops, diffChunk{"eq", a[n-suf+k]})
	}
	return ops
}

func lcsTable(a, b []string) [][]int {
	n, m := len(a), len(b)
	t := make([][]int, n+1)
	for i := range t {
		t[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[i] == b[j] {
				t[i][j] = t[i+1][j+1] + 1
			} else if t[i+1][j] >= t[i][j+1] {
				t[i][j] = t[i+1][j]
			} else {
				t[i][j] = t[i][j+1]
			}
		}
	}
	return t
}
