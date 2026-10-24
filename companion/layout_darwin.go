package main

import "errors"

// On macOS, Whereabouts answers to the same host name and does more: it also moves the Mac's own
// time zone to the VPN exit. The companion stays out of its way.
var errMacOS = errors.New("on macOS, use Whereabouts instead: https://github.com/doxigo/whereabouts. It registers the same host for Alibi, so the companion doesn't install here")

func currentLayout(system) (layout, error) { return layout{}, errMacOS }

func userRegistry() registry { return nil }
