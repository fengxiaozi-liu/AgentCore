package diff

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/aymanbagabas/go-udiff"
)

type LineType int

const (
	LineContext LineType = iota
	LineAdded
	LineRemoved
)

type DiffLine struct {
	OldLineNo int
	NewLineNo int
	Kind      LineType
	Content   string
}

type Hunk struct {
	Header string
	Lines  []DiffLine
}

type DiffResult struct {
	OldFile string
	NewFile string
	Hunks   []Hunk
}

func ParseUnifiedDiff(diffText string) (DiffResult, error) {
	var result DiffResult
	var currentHunk *Hunk

	hunkHeaderRe := regexp.MustCompile(`^@@ -(\d+),?(\d*) \+(\d+),?(\d*) @@`)
	lines := strings.Split(diffText, "\n")

	var oldLine, newLine int
	inFileHeader := true

	for _, line := range lines {
		if inFileHeader {
			if strings.HasPrefix(line, "--- a/") {
				result.OldFile = strings.TrimPrefix(line, "--- a/")
				continue
			}
			if strings.HasPrefix(line, "+++ b/") {
				result.NewFile = strings.TrimPrefix(line, "+++ b/")
				inFileHeader = false
				continue
			}
		}

		if matches := hunkHeaderRe.FindStringSubmatch(line); matches != nil {
			if currentHunk != nil {
				result.Hunks = append(result.Hunks, *currentHunk)
			}
			currentHunk = &Hunk{
				Header: line,
				Lines:  []DiffLine{},
			}

			oldStart, _ := strconv.Atoi(matches[1])
			newStart, _ := strconv.Atoi(matches[3])
			oldLine = oldStart
			newLine = newStart
			continue
		}

		if strings.HasPrefix(line, "\\ No newline at end of file") || currentHunk == nil {
			continue
		}

		if len(line) == 0 {
			currentHunk.Lines = append(currentHunk.Lines, DiffLine{
				OldLineNo: oldLine,
				NewLineNo: newLine,
				Kind:      LineContext,
			})
			oldLine++
			newLine++
			continue
		}

		switch line[0] {
		case '+':
			currentHunk.Lines = append(currentHunk.Lines, DiffLine{
				NewLineNo: newLine,
				Kind:      LineAdded,
				Content:   line[1:],
			})
			newLine++
		case '-':
			currentHunk.Lines = append(currentHunk.Lines, DiffLine{
				OldLineNo: oldLine,
				Kind:      LineRemoved,
				Content:   line[1:],
			})
			oldLine++
		default:
			currentHunk.Lines = append(currentHunk.Lines, DiffLine{
				OldLineNo: oldLine,
				NewLineNo: newLine,
				Kind:      LineContext,
				Content:   line,
			})
			oldLine++
			newLine++
		}
	}

	if currentHunk != nil {
		result.Hunks = append(result.Hunks, *currentHunk)
	}

	return result, nil
}

func GenerateDiff(beforeContent, afterContent, fileName, cwd string) (string, int, int) {
	if rel, err := filepath.Rel(cwd, fileName); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		fileName = rel
	} else {
		fileName = strings.TrimPrefix(fileName, cwd)
	}
	fileName = filepath.ToSlash(fileName)
	fileName = strings.TrimPrefix(fileName, "/")

	var (
		unified   = udiff.Unified("a/"+fileName, "b/"+fileName, beforeContent, afterContent)
		additions = 0
		removals  = 0
	)

	lines := strings.SplitSeq(unified, "\n")
	for line := range lines {
		if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++") {
			additions++
		} else if strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---") {
			removals++
		}
	}

	return unified, additions, removals
}
