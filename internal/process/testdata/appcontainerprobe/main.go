//go:build windows

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/windows"
)

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}

func main() {
	allowed := os.Getenv("AUTOGIT_APPCONTAINER_ALLOWED")
	denied := os.Getenv("AUTOGIT_APPCONTAINER_DENIED")
	network := os.Getenv("AUTOGIT_APPCONTAINER_NETWORK")
	if value, err := os.ReadFile(allowed); err != nil || string(value) != "allowed" {
		fail("allowlisted read failed: %v", err)
	}
	if _, err := os.ReadFile(denied); err == nil {
		fail("read outside the AppContainer allowlist succeeded")
	}
	writePath := filepath.Join(filepath.Dir(allowed), "write-attempt.txt")
	if err := os.WriteFile(writePath, []byte("must fail"), 0600); err == nil {
		fail("write in the read-only AppContainer directory succeeded")
	}

	connectResult := make(chan error, 1)
	go func() {
		connectResult <- deniedConnect(network)
	}()
	select {
	case err := <-connectResult:
		if err == nil {
			fail("network access escaped the AppContainer denial")
		}
	case <-time.After(2 * time.Second):
		// Windows may leave a denied AppContainer connect pending. The bounded
		// absence of a connection is the denial signal; the parent token
		// attestation independently checks that the child has no capabilities.
	}
	fmt.Println("APPCONTAINER_PROBE_OK")
	fmt.Println("PASS")
}

func deniedConnect(address string) error {
	separator := strings.LastIndexByte(address, ':')
	if separator <= 0 {
		return fmt.Errorf("invalid IPv4 listener address %q", address)
	}
	host := address[:separator]
	port, err := strconv.Atoi(address[separator+1:])
	if err != nil || host != "127.0.0.1" {
		return fmt.Errorf("invalid IPv4 listener address %q", address)
	}
	var data windows.WSAData
	if err := windows.WSAStartup(0x202, &data); err != nil {
		return err
	}
	defer windows.WSACleanup()
	socket, err := windows.Socket(windows.AF_INET, windows.SOCK_STREAM, windows.IPPROTO_TCP)
	if err != nil {
		return err
	}
	defer windows.Closesocket(socket)
	return windows.Connect(socket, &windows.SockaddrInet4{
		Port: port,
		Addr: [4]byte{127, 0, 0, 1},
	})
}
