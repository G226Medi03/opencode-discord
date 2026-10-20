package bridge

import (
	"strings"

	"github.com/G226Medi03/chat-app-adapter/chunk"
)

// isFenceLine reports whether a line opens or closes a fenced code block.
func isFenceLine(line string) bool {
	t := strings.TrimSpace(line)
	return strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~")
}

// splitAtoms groups text into plain runs and whole fenced code blocks. The
// returned atoms are in original order; indexing into the text is not needed.
func splitAtoms(text string) []string {
	var atoms []string
	var cur strings.Builder
	inFence := false
	for _, line := range strings.SplitAfter(text, "\n") {
		if line == "" {
			continue
		}
		if isFenceLine(line) {
			if inFence {
				cur.WriteString(line)
				atoms = append(atoms, cur.String())
				cur.Reset()
				inFence = false
			} else {
				if cur.Len() > 0 {
					atoms = append(atoms, cur.String())
					cur.Reset()
				}
				cur.WriteString(line)
				inFence = true
			}
			continue
		}
		cur.WriteString(line)
	}
	if cur.Len() > 0 {
		atoms = append(atoms, cur.String())
	}
	return atoms
}

// isFencedAtom reports whether an atom is a fenced code block (open or closed).
func isFencedAtom(atom string) bool {
	t := strings.TrimSpace(atom)
	return strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~")
}

// isClosedBlock reports whether an atom contains an even number of fence lines.
func isClosedBlock(atom string) bool {
	n := 0
	for _, line := range strings.Split(atom, "\n") {
		if isFenceLine(line) {
			n++
		}
	}
	return n%2 == 0
}

// splitAnswer splits text into pieces of at most limit characters. It prefers
// breaking between blocks and keeps each fenced code block in one piece; a
// block longer than limit falls back to the legacy splitter, which closes and
// reopens the fence at the boundary.
func splitAnswer(text string, limit int) []string {
	if limit <= 0 {
		limit = 1900
	}
	src := strings.ReplaceAll(text, "\r\n", "\n")
	if chunk.Len(src) <= limit {
		if strings.TrimSpace(src) == "" {
			return nil
		}
		return []string{src}
	}

	var out []string
	cur := ""
	flush := func() {
		if strings.TrimSpace(cur) == "" {
			cur = ""
			return
		}
		out = append(out, strings.TrimRight(cur, " \t\n"))
		cur = ""
	}
	appendPiece := func(p string) {
		if chunk.Len(cur)+chunk.Len(p) <= limit {
			cur += p
			return
		}
		flush()
		cur = p
	}

	for _, atom := range splitAtoms(src) {
		if isFencedAtom(atom) {
			// Keep a complete block whole whenever it fits in one message.
			if chunk.Len(atom) <= limit && isClosedBlock(atom) {
				appendPiece(atom)
				continue
			}
			// Oversized or unbalanced block: legacy behaviour.
			flush()
			ps := chunk.Split(atom, limit)
			if len(ps) > 0 {
				out = append(out, ps[:len(ps)-1]...)
				cur = ps[len(ps)-1]
			}
			continue
		}
		for _, p := range chunk.Split(atom, limit) {
			appendPiece(p)
		}
	}
	flush()
	return out
}
