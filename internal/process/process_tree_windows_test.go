//go:build windows

package process

import (
	"io"
	"os"
	"os/exec"
	"strconv"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func TestWindowsSupervisorTerminatesDescendants(t *testing.T) {
	if os.Getenv("AUTOGIT_WINDOWS_PROCESS_TREE_GRANDCHILD") == "1" {
		if err := os.WriteFile(os.Getenv("AUTOGIT_WINDOWS_PROCESS_TREE_PID"), []byte(strconv.Itoa(os.Getpid())), 0600); err != nil {
			panic(err)
		}
		for {
			time.Sleep(time.Hour)
		}
	}
	if os.Getenv("AUTOGIT_WINDOWS_PROCESS_TREE_HELPER") == "1" {
		gate := os.Getenv("AUTOGIT_WINDOWS_PROCESS_TREE_GATE")
		for {
			if _, err := os.Stat(gate); err == nil {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		child := exec.Command(os.Args[0], "-test.run=^TestWindowsSupervisorTerminatesDescendants$")
		child.Env = append(os.Environ(),
			"AUTOGIT_WINDOWS_PROCESS_TREE_GRANDCHILD=1",
			"AUTOGIT_WINDOWS_PROCESS_TREE_PID="+os.Getenv("AUTOGIT_WINDOWS_PROCESS_TREE_PID"),
		)
		if err := child.Start(); err != nil {
			panic(err)
		}
		for {
			time.Sleep(time.Hour)
		}
	}

	dir := t.TempDir()
	gate := dir + "\\gate"
	pidPath := dir + "\\child.pid"
	command := exec.Command(os.Args[0], "-test.run=^TestWindowsSupervisorTerminatesDescendants$")
	command.Env = append(os.Environ(),
		"AUTOGIT_WINDOWS_PROCESS_TREE_HELPER=1",
		"AUTOGIT_WINDOWS_PROCESS_TREE_GATE="+gate,
		"AUTOGIT_WINDOWS_PROCESS_TREE_PID="+pidPath,
	)
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	supervisor, err := newSupervisor(command)
	if err != nil {
		t.Fatal(err)
	}
	defer supervisor.Close()
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	if err := supervisor.Attach(command); err != nil {
		_ = supervisor.Terminate(command)
		_ = command.Wait()
		t.Fatal(err)
	}
	defer func() {
		_ = supervisor.Terminate(command)
		_ = command.Wait()
	}()
	if err := os.WriteFile(gate, []byte("ready"), 0600); err != nil {
		t.Fatal(err)
	}

	var raw []byte
	for attempt := 0; attempt < 200; attempt++ {
		raw, err = os.ReadFile(pidPath)
		if err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.ParseUint(string(raw), 10, 32)
	if err != nil {
		t.Fatal(err)
	}
	if err := supervisor.Terminate(command); err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < 200; attempt++ {
		if !windowsProcessRunning(uint32(pid)) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("descendant process %d survived job termination", pid)
}

func windowsProcessRunning(pid uint32) bool {
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.SYNCHRONIZE, false, pid)
	if err != nil {
		return false
	}
	defer windows.CloseHandle(handle)
	state, err := windows.WaitForSingleObject(handle, 0)
	return err == nil && state == 258 // WAIT_TIMEOUT
}
