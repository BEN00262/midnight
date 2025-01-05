package main

import (
	"fmt"
	"syscall"

	"golang.org/x/sys/windows/registry"
)

// InternetSetOption flags
const (
	INTERNET_OPTION_SETTINGS_CHANGED = 39
	INTERNET_OPTION_REFRESH          = 37
)

// EnableProxy enables or disables the proxy settings
func EnableProxy(enable bool, proxyAddress, bypassList string) error {
	// Open registry key
	key, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Internet Settings`, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("failed to open registry key: %w", err)
	}
	defer key.Close()

	// Set ProxyEnable value
	proxyEnable := uint32(0)
	if enable {
		proxyEnable = 1
	}
	err = key.SetDWordValue("ProxyEnable", proxyEnable)
	if err != nil {
		return fmt.Errorf("failed to set ProxyEnable: %w", err)
	}

	// Set ProxyServer value
	if enable && proxyAddress != "" {
		err = key.SetStringValue("ProxyServer", proxyAddress)
		if err != nil {
			return fmt.Errorf("failed to set ProxyServer: %w", err)
		}
	}

	// Set ProxyOverride value (optional, for bypass list)
	if enable && bypassList != "" {
		err = key.SetStringValue("ProxyOverride", bypassList)
		if err != nil {
			return fmt.Errorf("failed to set ProxyOverride: %w", err)
		}
	}

	// Notify the system about the change
	if err := refreshInternetSettings(); err != nil {
		return fmt.Errorf("failed to refresh internet settings: %w", err)
	}

	return nil
}

// refreshInternetSettings notifies the system about the proxy setting changes
func refreshInternetSettings() error {
	wininet := syscall.NewLazyDLL("wininet.dll")
	internetSetOption := wininet.NewProc("InternetSetOptionW")

	// Notify settings changed
	ret, _, err := internetSetOption.Call(
		0,
		uintptr(INTERNET_OPTION_SETTINGS_CHANGED),
		0,
		0,
	)
	if ret == 0 {
		return fmt.Errorf("InternetSetOption failed: %w", err)
	}

	// Refresh settings
	ret, _, err = internetSetOption.Call(
		0,
		uintptr(INTERNET_OPTION_REFRESH),
		0,
		0,
	)
	if ret == 0 {
		return fmt.Errorf("InternetSetOption (refresh) failed: %w", err)
	}

	return nil
}
