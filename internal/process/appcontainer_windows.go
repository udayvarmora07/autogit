//go:build windows

package process

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

const procThreadAttributeSecurityCapabilities = 0x00020009

var (
	userenvDLL                    = windows.NewLazySystemDLL("userenv.dll")
	advapi32DLL                   = windows.NewLazySystemDLL("advapi32.dll")
	createAppContainerProfileProc = userenvDLL.NewProc("CreateAppContainerProfile")
	deleteAppContainerProfileProc = userenvDLL.NewProc("DeleteAppContainerProfile")
	deriveAppContainerSIDProc     = userenvDLL.NewProc("DeriveAppContainerSidFromAppContainerName")
	setFileSecurityProc           = advapi32DLL.NewProc("SetFileSecurityW")
)

type securityCapabilities struct {
	AppContainerSid *windows.SID
	Capabilities    *windows.SIDAndAttributes
	CapabilityCount uint32
	Reserved        uint32
}

type appContainerProfile struct {
	name    string
	sid     *windows.SID
	created bool
}

type windowsAppContainerProcess struct {
	process     *os.Process
	thread      windows.Handle
	packageSID  string
	stdoutDone  <-chan struct{}
	stderrDone  <-chan struct{}
	cleanupFunc func() error
	cleanupOnce sync.Once
	cleanupErr  error
}

func AppContainerAvailable() bool {
	return createAppContainerProfileProc.Find() == nil &&
		deleteAppContainerProfileProc.Find() == nil &&
		deriveAppContainerSIDProc.Find() == nil &&
		setFileSecurityProc.Find() == nil
}

func startAppContainer(command *exec.Cmd, stdout, stderr io.Writer, options Options) (appContainerProcess, error) {
	if command == nil || command.Path == "" {
		return nil, errors.New("AppContainer command is required")
	}
	profile, err := newAppContainerProfile()
	if err != nil {
		return nil, err
	}
	profileCleanup := func() error { return profile.cleanup() }
	aclCleanup, err := grantAppContainerPaths(profile.sid, command, options)
	if err != nil {
		_ = profileCleanup()
		return nil, err
	}
	setupCleanup := true
	defer func() {
		if setupCleanup {
			_ = aclCleanup()
			_ = profileCleanup()
		}
	}()

	stdin, err := os.OpenFile("NUL", os.O_RDONLY, 0)
	if err != nil {
		return nil, fmt.Errorf("open AppContainer stdin: %w", err)
	}
	defer stdin.Close()
	stdoutR, stdoutW, err := os.Pipe()
	if err != nil {
		return nil, fmt.Errorf("create AppContainer stdout pipe: %w", err)
	}
	stderrR, stderrW, err := os.Pipe()
	if err != nil {
		_ = stdoutR.Close()
		_ = stdoutW.Close()
		return nil, fmt.Errorf("create AppContainer stderr pipe: %w", err)
	}
	closePipes := true
	defer func() {
		if closePipes {
			_ = stdoutR.Close()
			_ = stdoutW.Close()
			_ = stderrR.Close()
			_ = stderrW.Close()
		}
	}()

	childHandles := []windows.Handle{
		windows.Handle(stdin.Fd()),
		windows.Handle(stdoutW.Fd()),
		windows.Handle(stderrW.Fd()),
	}
	for _, handle := range childHandles {
		if err := windows.SetHandleInformation(handle, windows.HANDLE_FLAG_INHERIT, windows.HANDLE_FLAG_INHERIT); err != nil {
			return nil, fmt.Errorf("mark AppContainer standard handle inheritable: %w", err)
		}
	}
	for _, handle := range []windows.Handle{windows.Handle(stdoutR.Fd()), windows.Handle(stderrR.Fd())} {
		if err := windows.SetHandleInformation(handle, windows.HANDLE_FLAG_INHERIT, 0); err != nil {
			return nil, fmt.Errorf("clear AppContainer pipe inheritance: %w", err)
		}
	}

	attributeList, err := windows.NewProcThreadAttributeList(2)
	if err != nil {
		return nil, fmt.Errorf("create AppContainer process attributes: %w", err)
	}
	defer attributeList.Delete()
	security := securityCapabilities{AppContainerSid: profile.sid}
	if err := attributeList.Update(procThreadAttributeSecurityCapabilities, unsafe.Pointer(&security), unsafe.Sizeof(security)); err != nil {
		return nil, fmt.Errorf("set AppContainer security capabilities: %w", err)
	}
	if err := attributeList.Update(windows.PROC_THREAD_ATTRIBUTE_HANDLE_LIST, unsafe.Pointer(&childHandles[0]), uintptr(len(childHandles))*unsafe.Sizeof(childHandles[0])); err != nil {
		return nil, fmt.Errorf("set AppContainer handle list: %w", err)
	}

	applicationName, err := windows.UTF16PtrFromString(command.Path)
	if err != nil {
		return nil, fmt.Errorf("encode AppContainer executable path: %w", err)
	}
	commandLine, err := windows.UTF16PtrFromString(windows.ComposeCommandLine(command.Args))
	if err != nil {
		return nil, fmt.Errorf("encode AppContainer command line: %w", err)
	}
	environment, err := appContainerEnvironment(command.Env)
	if err != nil {
		return nil, err
	}
	currentDir, err := windows.UTF16PtrFromString(command.Dir)
	if err != nil {
		return nil, fmt.Errorf("encode AppContainer working directory: %w", err)
	}

	startup := &windows.StartupInfoEx{
		StartupInfo: windows.StartupInfo{
			Cb:        uint32(unsafe.Sizeof(windows.StartupInfoEx{})),
			Flags:     windows.STARTF_USESTDHANDLES,
			StdInput:  childHandles[0],
			StdOutput: childHandles[1],
			StdErr:    childHandles[2],
		},
		ProcThreadAttributeList: attributeList.List(),
	}
	info := new(windows.ProcessInformation)
	flags := uint32(windows.CREATE_SUSPENDED | windows.EXTENDED_STARTUPINFO_PRESENT | windows.CREATE_UNICODE_ENVIRONMENT | windows.CREATE_NO_WINDOW)
	if err := windows.CreateProcess(applicationName, commandLine, nil, nil, true, flags, &environment[0], currentDir, &startup.StartupInfo, info); err != nil {
		return nil, fmt.Errorf("create suspended AppContainer process: %w", err)
	}
	process, err := os.FindProcess(int(info.ProcessId))
	if err != nil {
		_ = windows.TerminateProcess(info.Process, 1)
		_ = windows.CloseHandle(info.Thread)
		_ = windows.CloseHandle(info.Process)
		return nil, fmt.Errorf("open AppContainer process handle: %w", err)
	}
	_ = windows.CloseHandle(info.Process)

	stdoutDone := make(chan struct{})
	go func() {
		_, _ = io.Copy(stdout, stdoutR)
		_ = stdoutR.Close()
		close(stdoutDone)
	}()
	stderrDone := make(chan struct{})
	go func() {
		_, _ = io.Copy(stderr, stderrR)
		_ = stderrR.Close()
		close(stderrDone)
	}()
	// The child owns inherited copies of the writer handles. Close the
	// parent's copies now so the reader goroutines observe EOF when the child
	// exits; the readers themselves are owned by those goroutines until then.
	_ = stdoutW.Close()
	_ = stderrW.Close()
	closePipes = false

	setupCleanup = false
	return &windowsAppContainerProcess{
		process:     process,
		thread:      info.Thread,
		packageSID:  profile.sid.String(),
		stdoutDone:  stdoutDone,
		stderrDone:  stderrDone,
		cleanupFunc: func() error { return errors.Join(aclCleanup(), profileCleanup()) },
	}, nil
}

func (p *windowsAppContainerProcess) resume() error {
	if p == nil || p.thread == 0 {
		return errors.New("AppContainer process thread is unavailable")
	}
	_, err := windows.ResumeThread(p.thread)
	closeErr := windows.CloseHandle(p.thread)
	p.thread = 0
	if err != nil {
		return fmt.Errorf("resume AppContainer process: %w", err)
	}
	return closeErr
}

func (p *windowsAppContainerProcess) osProcess() *os.Process {
	if p == nil {
		return nil
	}
	return p.process
}

func (p *windowsAppContainerProcess) terminate() error {
	if p == nil || p.process == nil {
		return nil
	}
	return p.process.Kill()
}

func (p *windowsAppContainerProcess) wait() (*os.ProcessState, error) {
	if p == nil || p.process == nil {
		return nil, errors.New("AppContainer process is unavailable")
	}
	state, err := p.process.Wait()
	<-p.stdoutDone
	<-p.stderrDone
	return state, err
}

func (p *windowsAppContainerProcess) cleanup() error {
	if p == nil {
		return nil
	}
	p.cleanupOnce.Do(func() {
		if p.thread != 0 {
			p.cleanupErr = errors.Join(p.cleanupErr, windows.CloseHandle(p.thread))
			p.thread = 0
		}
		p.cleanupErr = errors.Join(p.cleanupErr, p.cleanupFunc())
	})
	return p.cleanupErr
}

func (p *windowsAppContainerProcess) expectedPackageSID() string {
	if p == nil {
		return ""
	}
	return p.packageSID
}

func newAppContainerProfile() (*appContainerProfile, error) {
	var random [12]byte
	if _, err := rand.Read(random[:]); err != nil {
		return nil, fmt.Errorf("generate unique AppContainer profile name: %w", err)
	}
	name := "AutoGitVerifier-" + hex.EncodeToString(random[:])
	name16, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return nil, fmt.Errorf("encode AppContainer profile name: %w", err)
	}
	display16, _ := windows.UTF16PtrFromString("AutoGit verifier")
	description16, _ := windows.UTF16PtrFromString("Ephemeral verifier isolation profile")
	var sid *windows.SID
	hr, _, _ := createAppContainerProfileProc.Call(
		uintptr(unsafe.Pointer(name16)),
		uintptr(unsafe.Pointer(display16)),
		uintptr(unsafe.Pointer(description16)),
		0,
		0,
		uintptr(unsafe.Pointer(&sid)),
	)
	if hr != 0 {
		return nil, fmt.Errorf("CreateAppContainerProfile failed: HRESULT 0x%08x", uint32(hr))
	}
	if sid == nil || !sid.IsValid() {
		return nil, errors.New("CreateAppContainerProfile returned an invalid package SID")
	}
	return &appContainerProfile{name: name, sid: sid, created: true}, nil
}

func (p *appContainerProfile) cleanup() error {
	if p == nil {
		return nil
	}
	var cleanupErr error
	if p.created {
		name16, err := windows.UTF16PtrFromString(p.name)
		if err != nil {
			cleanupErr = errors.Join(cleanupErr, err)
		} else {
			hr, _, _ := deleteAppContainerProfileProc.Call(uintptr(unsafe.Pointer(name16)))
			if hr != 0 {
				cleanupErr = errors.Join(cleanupErr, fmt.Errorf("DeleteAppContainerProfile failed: HRESULT 0x%08x", uint32(hr)))
			}
		}
	}
	if p.sid != nil {
		cleanupErr = errors.Join(cleanupErr, windows.FreeSid(p.sid))
		p.sid = nil
	}
	return cleanupErr
}

func appContainerEnvironment(environment []string) ([]uint16, error) {
	if environment == nil {
		return nil, errors.New("AppContainer environment must be explicit")
	}
	entries := make(map[string]string, len(environment))
	for _, entry := range environment {
		if strings.IndexByte(entry, 0) >= 0 {
			return nil, errors.New("AppContainer environment contains NUL")
		}
		index := strings.IndexByte(entry, '=')
		if index <= 0 {
			return nil, errors.New("AppContainer environment contains an invalid entry")
		}
		name := entry[:index]
		entries[strings.ToUpper(name)] = entry
	}
	if _, present := entries["SYSTEMROOT"]; !present {
		systemRoot := os.Getenv("SystemRoot")
		if systemRoot == "" {
			return nil, errors.New("AppContainer requires the Windows SystemRoot environment value")
		}
		entries["SYSTEMROOT"] = "SystemRoot=" + systemRoot
	}
	if _, present := entries["LOCALAPPDATA"]; !present {
		localAppData := os.Getenv("LOCALAPPDATA")
		if localAppData == "" {
			return nil, errors.New("AppContainer requires the Windows LOCALAPPDATA environment value")
		}
		// CreateProcess resolves the AppContainer profile below
		// %LOCALAPPDATA% while constructing the child token and object
		// namespace. Keep this host-required value even when callers provide a
		// deliberately minimal child environment.
		entries["LOCALAPPDATA"] = "LOCALAPPDATA=" + localAppData
	}
	keys := make([]string, 0, len(entries))
	for key := range entries {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	block := make([]string, 0, len(keys))
	for _, key := range keys {
		block = append(block, entries[key])
	}
	encoded := utf16.Encode([]rune(strings.Join(block, "\x00") + "\x00\x00"))
	if len(encoded) == 0 {
		return []uint16{0, 0}, nil
	}
	return encoded, nil
}

type appContainerGrant struct {
	path              string
	original          *windows.SECURITY_DESCRIPTOR
	originalDACL      *windows.ACL
	originalProtected bool
}

func grantAppContainerPaths(sid *windows.SID, command *exec.Cmd, options Options) (func() error, error) {
	paths, err := collectAppContainerPaths(command, options)
	if err != nil {
		return nil, err
	}
	grants := make([]appContainerGrant, 0, len(paths))
	for _, item := range paths {
		grant, err := applyAppContainerGrant(item.path, item.full, sid)
		if err != nil {
			_ = restoreAppContainerGrants(grants)
			return nil, err
		}
		grants = append(grants, grant)
	}
	return func() error { return restoreAppContainerGrants(grants) }, nil
}

type appContainerPath struct {
	path string
	full bool
}

func collectAppContainerPaths(command *exec.Cmd, options Options) ([]appContainerPath, error) {
	if !filepath.IsAbs(command.Path) || filepath.Clean(command.Path) != command.Path {
		return nil, errors.New("AppContainer executable must be an absolute clean path")
	}
	if !filepath.IsAbs(command.Dir) || filepath.Clean(command.Dir) != command.Dir {
		return nil, errors.New("AppContainer working directory must be an absolute clean path")
	}
	paths := make(map[string]bool)
	add := func(path string, full bool) error {
		if path == "" || !filepath.IsAbs(path) || filepath.Clean(path) != path {
			return fmt.Errorf("invalid AppContainer path %q", path)
		}
		info, err := os.Lstat(path)
		if err != nil {
			return fmt.Errorf("stat AppContainer path %q: %w", path, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("AppContainer path %q is a symlink", path)
		}
		if full {
			paths[path] = true
		} else if _, exists := paths[path]; !exists {
			paths[path] = false
		}
		return nil
	}
	for _, path := range options.FilesystemAllowlist {
		if err := collectAppContainerTree(path, add); err != nil {
			return nil, err
		}
	}
	if err := add(command.Dir, true); err != nil {
		return nil, err
	}
	if err := add(command.Path, true); err != nil {
		return nil, err
	}
	for path := range paths {
		// Windows normally grants AppContainers traversal through the system and
		// user-profile roots. Only the immediate parent is changed here, which
		// avoids recursive ACL propagation on broad profile directories.
		if err := add(filepath.Dir(path), false); err != nil {
			return nil, err
		}
	}
	result := make([]appContainerPath, 0, len(paths))
	for path, full := range paths {
		result = append(result, appContainerPath{path: path, full: full})
	}
	sort.Slice(result, func(i, j int) bool {
		if len(result[i].path) != len(result[j].path) {
			return len(result[i].path) < len(result[j].path)
		}
		return result[i].path < result[j].path
	})
	return result, nil
}

func collectAppContainerTree(path string, add func(string, bool) error) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("stat AppContainer allowlist path %q: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("AppContainer allowlist path %q is a symlink", path)
	}
	if !info.IsDir() {
		return add(path, true)
	}
	return filepath.WalkDir(path, func(current string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("AppContainer allowlist path %q contains a symlink", current)
		}
		return add(current, true)
	})
}

func applyAppContainerGrant(path string, full bool, sid *windows.SID) (appContainerGrant, error) {
	securityInformation := windows.SECURITY_INFORMATION(windows.OWNER_SECURITY_INFORMATION | windows.GROUP_SECURITY_INFORMATION | windows.DACL_SECURITY_INFORMATION)
	old, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, securityInformation)
	if err != nil {
		return appContainerGrant{}, fmt.Errorf("read DACL for AppContainer path %q: %w", path, err)
	}
	if old == nil {
		return appContainerGrant{}, fmt.Errorf("AppContainer path %q has no security descriptor", path)
	}
	oldDACL, _, daclErr := old.DACL()
	if daclErr != nil || oldDACL == nil {
		return appContainerGrant{}, fmt.Errorf("AppContainer path %q has no explicit DACL", path)
	}
	control, _, err := old.Control()
	if err != nil {
		return appContainerGrant{}, fmt.Errorf("read DACL control for AppContainer path %q: %w", path, err)
	}
	inheritance := uint32(windows.NO_INHERITANCE)
	access := windows.ACCESS_MASK(windows.FILE_GENERIC_READ | windows.FILE_GENERIC_EXECUTE)
	if !full {
		access = windows.ACCESS_MASK(windows.FILE_TRAVERSE | windows.FILE_READ_ATTRIBUTES | windows.SYNCHRONIZE)
	}
	entry := windows.EXPLICIT_ACCESS{
		AccessPermissions: access,
		AccessMode:        windows.GRANT_ACCESS,
		Inheritance:       inheritance,
		Trustee: windows.TRUSTEE{
			TrusteeForm:  windows.TRUSTEE_IS_SID,
			TrusteeType:  windows.TRUSTEE_IS_GROUP,
			TrusteeValue: windows.TrusteeValueFromSID(sid),
		},
	}
	newDACL, err := windows.ACLFromEntries([]windows.EXPLICIT_ACCESS{entry}, oldDACL)
	if err != nil {
		return appContainerGrant{}, fmt.Errorf("build AppContainer DACL for %q: %w", path, err)
	}
	if newDACL == nil {
		return appContainerGrant{}, fmt.Errorf("build AppContainer DACL for %q returned no DACL", path)
	}
	securityInformation = windows.SECURITY_INFORMATION(windows.DACL_SECURITY_INFORMATION)
	protected := control&windows.SE_DACL_PROTECTED != 0
	if protected {
		securityInformation |= windows.PROTECTED_DACL_SECURITY_INFORMATION
	} else {
		securityInformation |= windows.UNPROTECTED_DACL_SECURITY_INFORMATION
	}
	if err := windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, securityInformation, nil, nil, newDACL, nil); err != nil {
		return appContainerGrant{}, fmt.Errorf("grant AppContainer access to %q: %w", path, err)
	}
	runtime.KeepAlive(sid)
	return appContainerGrant{
		path:              path,
		original:          old,
		originalDACL:      oldDACL,
		originalProtected: protected,
	}, nil
}

func restoreAppContainerGrants(grants []appContainerGrant) error {
	var cleanupErr error
	for index := len(grants) - 1; index >= 0; index-- {
		grant := grants[index]
		if err := restoreAppContainerSecurityDescriptor(grant); err != nil {
			cleanupErr = errors.Join(cleanupErr, err)
		}
	}
	return cleanupErr
}

func restoreAppContainerSecurityDescriptor(grant appContainerGrant) error {
	if grant.original == nil {
		return fmt.Errorf("restore security descriptor for %q: snapshot is unavailable", grant.path)
	}
	name, err := windows.UTF16PtrFromString(grant.path)
	if err != nil {
		return fmt.Errorf("encode security descriptor restore path %q: %w", grant.path, err)
	}
	securityInformation := windows.SECURITY_INFORMATION(windows.OWNER_SECURITY_INFORMATION | windows.GROUP_SECURITY_INFORMATION | windows.DACL_SECURITY_INFORMATION)
	if grant.originalProtected {
		securityInformation |= windows.PROTECTED_DACL_SECURITY_INFORMATION
	} else {
		securityInformation |= windows.UNPROTECTED_DACL_SECURITY_INFORMATION
	}
	result, _, callErr := setFileSecurityProc.Call(
		uintptr(unsafe.Pointer(name)),
		uintptr(securityInformation),
		uintptr(unsafe.Pointer(grant.original)),
	)
	if result == 0 {
		if callErr == nil {
			callErr = windows.GetLastError()
		}
		if callErr == nil {
			callErr = errors.New("SetFileSecurityW returned failure")
		}
		return fmt.Errorf("restore security descriptor for %q: %w", grant.path, callErr)
	}
	return nil
}
