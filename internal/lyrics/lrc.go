package lyrics

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// lrcLine matches "[mm:ss.xx]text", with any number of leading time tags.
// Lines without a time tag (metadata such as [ar:] or [offset:]) are
// skipped.
var (
	lrcLine = regexp.MustCompile(`^((?:\[\d+:\d+(?:\.\d+)?\])+)(.*)$`)
	lrcTag  = regexp.MustCompile(`\[(\d+):(\d+(?:\.\d+)?)\]`)
)

// ParseLRC turns LRC text into lines sorted by start time. A line with
// several time tags is repeated once per tag (choruses are often written
// that way). Empty lines are kept: they mark gaps, so the highlight moves
// off the previous line during an instrumental break.
func ParseLRC(s string) []Line {
	var lines []Line
	for _, raw := range strings.Split(s, "\n") {
		raw = strings.TrimRight(raw, "\r")
		m := lrcLine.FindStringSubmatch(raw)
		if m == nil {
			continue
		}
		text := strings.TrimSpace(m[2])
		for _, tag := range lrcTag.FindAllStringSubmatch(m[1], -1) {
			min, err1 := strconv.Atoi(tag[1])
			sec, err2 := strconv.ParseFloat(tag[2], 64)
			if err1 != nil || err2 != nil {
				continue
			}
			at := time.Duration(min)*time.Minute + time.Duration(sec*float64(time.Second))
			lines = append(lines, Line{At: at, Text: text})
		}
	}
	sort.SliceStable(lines, func(i, j int) bool {
		return lines[i].At < lines[j].At
	})
	return lines
}
