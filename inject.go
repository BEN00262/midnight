package main

import (
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Constants for certificate finding
const CERT_FIND_SERIAL_NUMBER = uint32(0x00000004)

// Constants for store locations
var (
	CERT_STORE_PROV_SYSTEM          = uint32(10)
	CERT_STORE_ADD_REPLACE_EXISTING = uint32(3)
)

// Windows system certificate store
var (
	CERT_SYSTEM_STORE_LOCAL_MACHINE = uint32(0x20000)
	CERT_SYSTEM_STORE_CURRENT_USER  = uint32(0x10000)
	CERT_STORE_OPEN_EXISTING_FLAG   = uint32(0x4000) // Silent mode flag
)

const (
	CRYPT_E_NOT_FOUND = 0x80092004
)

// CertExists checks if a certificate with a given thumbprint or subject exists in the system store "Root".
func CertExists(certSubject string) bool {
	storeHandle, err := syscall.CertOpenSystemStore(0, syscall.StringToUTF16Ptr("Root"))
	if err != nil {
		fmt.Println("Error opening certificate store:", syscall.GetLastError())
		return false
	}

	defer syscall.CertCloseStore(storeHandle, windows.CERT_CLOSE_STORE_FORCE_FLAG)

	var cert *syscall.CertContext
	for {
		cert, err = syscall.CertEnumCertificatesInStore(storeHandle, cert)
		if err != nil {
			if errno, ok := err.(syscall.Errno); ok {
				if errno == CRYPT_E_NOT_FOUND {
					break
				}
			}

			return false
		}
		if cert == nil {
			break
		}
		// Copy the buf, since ParseCertificate does not create its own copy.
		buf := (*[1 << 20]byte)(unsafe.Pointer(cert.EncodedCert))[:]
		buf2 := make([]byte, cert.Length)
		copy(buf2, buf)
		if c, err := x509.ParseCertificate(buf2); err == nil {
			// Check if the certificate subject matches
			if c.Subject.String() == certSubject {
				return true
			}
		}
	}
	return false
}

// AddCertToStore adds a certificate to the Windows Certificate Store
func AddCertToStore(certPath string, storeName string, location uint32) error {
	_, err := os.Stat(certPath)
	if os.IsNotExist(err) {
		return fmt.Errorf("certificate file does not exist: %s", certPath)
	}

	// Open the certificate store in silent mode
	store, err := windows.CertOpenStore(
		windows.CERT_STORE_PROV_SYSTEM,
		0,
		0,
		location|CERT_STORE_OPEN_EXISTING_FLAG, // Use silent mode flag here
		uintptr(unsafe.Pointer(windows.StringToUTF16Ptr(storeName))),
	)

	if err != nil {
		return fmt.Errorf("failed to open cert store: %w", err)
	}

	defer windows.CertCloseStore(store, 0)

	// Read the certificate file
	certData, err := os.ReadFile(certPath)
	if err != nil {
		return fmt.Errorf("failed to read certificate file: %w", err)
	}

	// Decode PEM if necessary
	block, _ := pem.Decode(certData)
	if block != nil && block.Type == "CERTIFICATE" {
		certData = block.Bytes
	}

	// Parse the certificate to extract details (e.g., serial number)
	cert, err := x509.ParseCertificate(certData)
	if err != nil {
		return fmt.Errorf("failed to parse certificate: %w", err)
	}

	if CertExists(cert.Subject.String()) {
		return nil
	}

	// Convert to Windows CERT_CONTEXT
	certCtx, err := windows.CertCreateCertificateContext(
		uint32(windows.X509_ASN_ENCODING),
		(*byte)(unsafe.Pointer(&certData[0])),
		uint32(len(certData)),
	)

	if err != nil {
		return fmt.Errorf("failed to create certificate context: %w", err)
	}

	defer windows.CertFreeCertificateContext(certCtx)

	// Add the certificate to the store
	if err := windows.CertAddCertificateContextToStore(
		store,
		certCtx,
		CERT_STORE_ADD_REPLACE_EXISTING,
		nil,
	); err != nil {
		return fmt.Errorf("failed to add certificate to store: %w", err)
	}

	return nil
}
