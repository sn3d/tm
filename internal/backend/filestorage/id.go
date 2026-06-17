package filestorage

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// nextSharedNumericID returns the next sequential numeric ID for tasks. The
// counter is named "shared" for historical reasons — before plans were
// collapsed into tasks the counter scanned both tasks/ and plans/ so IDs
// were unique across both. Post-collapse only tasks/ is scanned; former
// plan IDs are preserved as task IDs by the collapse migration.
// Non-numeric IDs (e.g. legacy "PLAN-1" or JIRA-style "TASK-123") are
// ignored when computing the max. Missing directories are treated as empty.
func nextSharedNumericID(dir string) (string, error) {
	m, err := maxNumericIDIn(filepath.Join(dir, "tasks"))
	if err != nil {
		return "", err
	}
	return strconv.Itoa(m + 1), nil
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
