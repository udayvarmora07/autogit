//go:build windows

package process

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	tokenIsAppContainer     = 29
	tokenCapabilities       = 30
	tokenAppContainerSID    = 31
	securityMandatoryLowRID = 0x1000
)

type tokenAppContainerInformation struct {
	TokenAppContainer *windows.SID
}

func attestProcess(child *os.Process, expectedPackageSID string) (IsolationAttestation, error) {
	if child == nil {
		return IsolationAttestation{}, errors.New("cannot attest a nil process")
	}
	var attestation IsolationAttestation
	var attestationErr error
	err := child.WithHandle(func(handle uintptr) {
		var token windows.Token
		if err := windows.OpenProcessToken(windows.Handle(handle), windows.TOKEN_QUERY, &token); err != nil {
			attestationErr = fmt.Errorf("open child token for AppContainer attestation: %w", err)
			return
		}
		defer token.Close()

		appContainer, queryErr := tokenInformation(token, tokenIsAppContainer, 4)
		if queryErr != nil {
			attestationErr = fmt.Errorf("query TokenIsAppContainer: %w", queryErr)
			return
		}
		attestation.AppContainerObserved = len(appContainer) >= 4 && binary.LittleEndian.Uint32(appContainer[:4]) != 0
		if !attestation.AppContainerObserved {
			attestationErr = errors.New("child token is not an AppContainer token")
			return
		}

		packageInfo, queryErr := tokenInformation(token, tokenAppContainerSID, uint32(unsafe.Sizeof(tokenAppContainerInformation{})))
		if queryErr != nil {
			attestationErr = fmt.Errorf("query TokenAppContainerSid: %w", queryErr)
			return
		}
		if len(packageInfo) < int(unsafe.Sizeof(tokenAppContainerInformation{})) {
			attestationErr = errors.New("TokenAppContainerSid returned a short record")
			return
		}
		observedPackage := (*tokenAppContainerInformation)(unsafe.Pointer(&packageInfo[0])).TokenAppContainer
		attestation.PackageSIDMatch = observedPackage != nil && observedPackage.IsValid() && observedPackage.String() == expectedPackageSID
		if !attestation.PackageSIDMatch {
			attestationErr = errors.New("child package SID does not match the unique AppContainer profile")
			return
		}

		integrity, queryErr := tokenInformation(token, windows.TokenIntegrityLevel, uint32(unsafe.Sizeof(windows.Tokenmandatorylabel{})))
		if queryErr != nil {
			attestationErr = fmt.Errorf("query TokenIntegrityLevel: %w", queryErr)
			return
		}
		if len(integrity) < int(unsafe.Sizeof(windows.Tokenmandatorylabel{})) {
			attestationErr = errors.New("TokenIntegrityLevel returned a short record")
			return
		}
		label := (*windows.Tokenmandatorylabel)(unsafe.Pointer(&integrity[0]))
		attestation.LowIntegrityObserved = label.Label.Sid != nil && label.Label.Sid.IsValid() && label.Label.Sid.SubAuthorityCount() > 0 && label.Label.Sid.SubAuthority(uint32(label.Label.Sid.SubAuthorityCount()-1)) == securityMandatoryLowRID
		if !attestation.LowIntegrityObserved {
			attestationErr = errors.New("child token does not have low integrity")
			return
		}

		capabilities, queryErr := tokenInformation(token, tokenCapabilities, 64)
		if queryErr != nil {
			attestationErr = fmt.Errorf("query TokenCapabilities: %w", queryErr)
			return
		}
		if len(capabilities) < int(unsafe.Sizeof(windows.Tokengroups{})) {
			attestationErr = errors.New("TokenCapabilities returned a short record")
			return
		}
		groups := (*windows.Tokengroups)(unsafe.Pointer(&capabilities[0]))
		attestation.CapabilityCount = int(groups.GroupCount)
		attestation.NetworkDeniedObserved = groups.GroupCount == 0
		if !attestation.NetworkDeniedObserved {
			attestationErr = fmt.Errorf("AppContainer token has %d unexpected capabilities", groups.GroupCount)
			return
		}
		attestation.independentlyObserved = true
	})
	if err != nil {
		return attestation, err
	}
	if attestationErr != nil {
		return attestation, attestationErr
	}
	return attestation, nil
}

func tokenInformation(token windows.Token, class uint32, initialSize uint32) ([]byte, error) {
	if initialSize == 0 {
		initialSize = 64
	}
	for {
		buffer := make([]byte, initialSize)
		var returned uint32
		err := windows.GetTokenInformation(token, class, &buffer[0], uint32(len(buffer)), &returned)
		if err == nil {
			if returned == 0 {
				return buffer, nil
			}
			return buffer[:returned], nil
		}
		if err != windows.ERROR_INSUFFICIENT_BUFFER {
			return nil, err
		}
		if returned <= uint32(len(buffer)) {
			initialSize = uint32(len(buffer)) * 2
		} else {
			initialSize = returned
		}
	}
}
