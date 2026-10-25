package main

import "strings"

// A VPN is up when a network adapter that carries one is connected and has an IPv4 address: as
// Whereabouts counts a utun, ipsec, ppp, tun, tap or wg interface on the Mac. Without one the
// connection's place is only approximate (IP geolocation can put your own ISP in another zone of a big
// country), so the clock stays home.

// Windows interface types that carry a VPN: PPP, which IKEv2, SSTP, L2TP and PPTP connections use; a
// tunnel; and a software adapter, which WireGuard's Wintun and most VPN apps install.
const (
	ifTypePPP         = 23
	ifTypeTunnel      = 131
	ifTypePropVirtual = 53
)

// Words in an adapter's name or description that mark one from a VPN, whatever type it reports
// (OpenVPN's TAP adapter calls itself Ethernet, and VPN apps rename it: TAP-Surfshark Windows Adapter).
var vpnMarkers = []string{"wireguard", "wintun", "tap-", "openvpn", "amneziawg", "vpn"}

// isVPNAdapter reports whether an adapter of this type, with these names, is a VPN's.
func isVPNAdapter(ifType uint32, names ...string) bool {
	switch ifType {
	case ifTypePPP, ifTypeTunnel, ifTypePropVirtual:
		return true
	}
	for _, name := range names {
		name = strings.ToLower(name)
		for _, marker := range vpnMarkers {
			if strings.Contains(name, marker) {
				return true
			}
		}
	}
	return false
}
