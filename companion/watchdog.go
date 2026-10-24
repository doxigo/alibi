package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"slices"
	"strings"
	"time"
)

// settings is what the tray lets you choose. Saved as settings.json in the companion's folder.
type settings struct {
	Sync     bool `json:"timezoneSync"` // keep Windows on the VPN's time zone
	Alerts   bool `json:"alerts"`       // notify when the connection isn't where it should be
	Sound    bool `json:"sound"`
	EdgeLine bool `json:"edgeLine"` // a red line along the top of the screen while something's wrong
	// The country that counts as "home", where the connection coming out means the VPN is off.
	// Empty: the country of the time zone Windows had before the companion.
	Home string `json:"homeCountry,omitempty"`
	// If set, anywhere else is wrong. Blocked countries are always wrong.
	Allowed []string `json:"allowedCountries,omitempty"`
	Blocked []string `json:"blockedCountries,omitempty"`
}

func defaultSettings() settings {
	return settings{Sync: true, Alerts: true, Sound: true, EdgeLine: true}
}

// loadSettings reads settings.json; anything it doesn't say keeps its default.
func loadSettings(path string) settings {
	s := defaultSettings()
	if b, err := os.ReadFile(path); err == nil {
		json.Unmarshal(b, &s)
	}
	norm := func(codes []string) []string {
		var out []string
		for _, c := range codes {
			if c = strings.ToUpper(strings.TrimSpace(c)); countryPattern.MatchString(c) {
				out = append(out, c)
			}
		}
		return out
	}
	s.Allowed, s.Blocked = norm(s.Allowed), norm(s.Blocked)
	if s.Home = strings.ToUpper(strings.TrimSpace(s.Home)); !countryPattern.MatchString(s.Home) {
		s.Home = ""
	}
	return s
}

func saveSettings(path string, s settings) error {
	b, _ := json.MarshalIndent(s, "", "  ")
	return writeFileAtomic(path, append(b, '\n'), 0o644)
}

// leakReport is where the computer's traffic comes out, besides the connection itself.
type leakReport struct {
	Checked time.Time
	// IPv6: "" not checked, "none" no IPv6, otherwise where it comes out.
	IPv6   string
	IPv6At geoAnswer
	// The DNS resolver the computer's lookups reach, as the internet sees it.
	DNS    geoAnswer
	DNSErr string
}

// leakChecker looks at IPv6 and DNS. Its parts are swapped out in tests.
type leakChecker struct {
	resolverIP func(context.Context) (string, error)
	about      func(context.Context, string) (geoAnswer, error)
	overIPv6   func(context.Context) (geoAnswer, error)
}

func newLeakChecker(g *geoJS) leakChecker {
	return leakChecker{resolverIP: resolverIP, about: g.about, overIPv6: g.overIPv6}
}

// resolverIP asks whoami.akamai.net, whose servers answer with the address of the DNS resolver
// that asked them: where the computer's DNS lookups come out.
func resolverIP(ctx context.Context) (string, error) {
	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, "whoami.akamai.net")
	if err != nil {
		return "", err
	}
	for _, a := range addrs {
		if a.IP.To4() != nil {
			return a.IP.String(), nil
		}
	}
	if len(addrs) > 0 {
		return addrs[0].IP.String(), nil
	}
	return "", errors.New("no answer")
}

func (c leakChecker) check(ctx context.Context) leakReport {
	r := leakReport{Checked: time.Now()}
	if ip, err := c.resolverIP(ctx); err != nil {
		r.DNSErr = err.Error()
	} else if at, err := c.about(ctx, ip); err != nil {
		r.DNS, r.DNSErr = geoAnswer{IP: ip}, err.Error()
	} else {
		at.IP = ip
		r.DNS = at
	}
	if at, err := c.overIPv6(ctx); err != nil {
		r.IPv6 = "none" // no route over IPv6: nothing can leak there
	} else {
		r.IPv6, r.IPv6At = "seen", at
	}
	return r
}

type verdict int

const (
	unknown verdict = iota // no answer yet, or offline
	fine
	wrong
)

// judge says whether the connection is where it's meant to be, and if not, why, in sentences for an
// alert. home is the home country ("" when unknown).
func judge(place cover, leaks leakReport, home string, s settings) (verdict, []string) {
	if place.Country == "" {
		return unknown, nil
	}
	name := func(code string) string {
		if n := countryNames[code]; n != "" {
			return n
		}
		return code
	}
	var problems []string
	switch {
	case slices.Contains(s.Blocked, place.Country):
		problems = append(problems, fmt.Sprintf("Your connection comes out in %s, which you blocked.", name(place.Country)))
	case len(s.Allowed) > 0 && !slices.Contains(s.Allowed, place.Country):
		problems = append(problems, fmt.Sprintf("Your connection comes out in %s, which isn't one of your countries.", name(place.Country)))
	case len(s.Allowed) == 0 && home != "" && place.Country == home:
		problems = append(problems, fmt.Sprintf("Your connection comes out at home, in %s. Is the VPN off?", name(home)))
	}
	away := place.Country != home
	if leaks.IPv6At.Country != "" && leaks.IPv6At.Country != place.Country {
		problems = append(problems, fmt.Sprintf("IPv6 traffic comes out in %s, outside the VPN.", name(leaks.IPv6At.Country)))
	}
	if away && home != "" && leaks.DNS.Country == home {
		problems = append(problems, fmt.Sprintf("DNS lookups go through %s, outside the VPN.", describe(leaks.DNS)))
	}
	if len(problems) > 0 {
		return wrong, problems
	}
	return fine, nil
}

// describe names where an address is: "Frankfurt, Germany (Cloudflare, Inc.)".
func describe(a geoAnswer) string {
	var where []string
	if a.City != "" {
		where = append(where, a.City)
	}
	if a.CountryName != "" {
		where = append(where, a.CountryName)
	}
	text := strings.Join(where, ", ")
	switch {
	case text == "" && a.Org == "":
		return a.IP
	case text == "":
		return a.Org
	case a.Org != "":
		return text + " (" + a.Org + ")"
	}
	return text
}

// homeCountry is the country that counts as home: the one set, or the home time zone's.
func homeCountry(s settings, homeZone string) string {
	if s.Home != "" {
		return s.Home
	}
	return zoneCountries[homeZone]
}
