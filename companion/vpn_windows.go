package main

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// vpnUp reports whether a VPN's adapter is connected and has an IPv4 address that isn't
// self-assigned (169.254.x.x).
func vpnUp() bool {
	size := uint32(16 << 10)
	for range 3 {
		buf := make([]byte, size)
		first := (*windows.IpAdapterAddresses)(unsafe.Pointer(&buf[0]))
		err := windows.GetAdaptersAddresses(windows.AF_INET, windows.GAA_FLAG_SKIP_ANYCAST|windows.GAA_FLAG_SKIP_MULTICAST|windows.GAA_FLAG_SKIP_DNS_SERVER, 0, first, &size)
		if err == windows.ERROR_BUFFER_OVERFLOW {
			continue // size now says how much it needs
		}
		if err != nil {
			return false
		}
		for a := first; a != nil; a = a.Next {
			if a.OperStatus != windows.IfOperStatusUp {
				continue
			}
			names := []string{windows.UTF16PtrToString(a.FriendlyName), windows.UTF16PtrToString(a.Description)}
			if !isVPNAdapter(a.IfType, names...) {
				continue
			}
			for u := a.FirstUnicastAddress; u != nil; u = u.Next {
				if ip := u.Address.IP().To4(); ip != nil && !ip.IsLinkLocalUnicast() {
					return true
				}
			}
		}
		return false
	}
	return false
}
