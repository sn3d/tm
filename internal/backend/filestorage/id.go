package filestorage

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// nextSharedNumericID returns the next sequential numeric ID shared across
// tasks and plans. It scans both dir/tasks/ and dir/plans/, extracts the
// leading ID portion of each filename, and returns (max + 1) as a string.
// Non-numeric IDs (e.g. legacy "PLAN-1" or JIRA-style "TASK-123") are ignored
// when computing the max. Missing directories are treated as empty.
func nextSharedNumericID(dir string) (string, error) {
	max := 0
	for _, sub := range []string{"tasks", "plans"} {
		m, err := maxNumericIDIn(filepath.Join(dir, sub))
		if err != nil {
			return "", err
		}
		if m > max {
			max = m
		}
	}
	return strconv.Itoa(max + 1), nil
}

func maxNumericIDIn(dir string) (int, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("list %s: %w", dir, err)
	}
	max := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		n, err := strconv.Atoi(idFromFilename(e.Name()))
		if err != nil {
			continue
		}
		if n > max {
			max = n
		}
	}
	return max, nil
}
