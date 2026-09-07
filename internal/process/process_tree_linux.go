//go:build linux

package process

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
)

// terminateDescendants also catches descendants that deliberately create a
// new session and therefore escape a process-group kill. The process tree is
// sampled immediately before termination and killed from the leaves inward;
// the group kill remains the primary fast path.
func terminateDescendants(root int) error {
	for attempt := 0; attempt < 3; attempt++ {
		children := processDescendants(root)
		if len(children) == 0 {
			return nil
		}
		sort.Sort(sort.Reverse(sort.IntSlice(children)))
		for _, pid := range children {
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
	}
	return nil
}

func processDescendants(root int) []int {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}
	children := make(map[int][]int)
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid <= 1 {
			continue
		}
		raw, err := os.ReadFile(filepath.Join("/proc", entry.Name(), "stat"))
		if err != nil {
			continue
		}
		closeParen := strings.LastIndexByte(string(raw), ')')
		if closeParen < 0 {
			continue
		}
		fields := strings.Fields(string(raw)[closeParen+2:])
		if len(fields) < 2 {
			continue
		}
		ppid, err := strconv.Atoi(fields[1])
		if err == nil {
			children[ppid] = append(children[ppid], pid)
		}
	}
	seen := map[int]bool{root: true}
	queue := []int{root}
	result := make([]int, 0)
	for len(queue) > 0 {
		parent := queue[0]
		queue = queue[1:]
		for _, child := range children[parent] {
			if seen[child] {
				continue
			}
			seen[child] = true
			result = append(result, child)
			queue = append(queue, child)
		}
	}
	return result
}
