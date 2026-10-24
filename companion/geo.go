package main

//go:generate node gen_locales.mjs
//go:generate gofmt -w locales.go

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// The same lookup Alibi makes itself (js/sites.js, ipCover): free, no key, HTTPS.
const (
	geoJSURL   = "https://get.geojs.io/v1/ip/geo.json"
	geoJSAtURL = "https://get.geojs.io/v1/ip/geo/%s.json" // about another address
)

// cover is a place in the shape Alibi's checkedCover() takes (js/sites.js). Coordinates GeoJS
// doesn't have are left out, and Alibi clears the old ones.
type cover struct {
	Timezone string   `json:"timezone"`
	Locale   string   `json:"locale"`
	Lat      *float64 `json:"lat,omitempty"`
	Lon      *float64 `json:"lon,omitempty"`
	// The rest is for the agent and its tray. Alibi gets none of it.
	Country     string `json:"-"` // two letters
	CountryName string `json:"-"`
	City        string `json:"-"`
	Org         string `json:"-"` // the network the connection comes out of: the VPN's, or the ISP's
	IP          string `json:"-"`
}

// geoAnswer is what GeoJS says about an address. Anything it doesn't know is empty.
type geoAnswer struct {
	IP, Timezone, Country, CountryName, City, Org string
	Lat, Lon                                      *float64
}

type geoJS struct {
	url, atURL string
	client     *http.Client
	ipv6       *http.Client // reaches GeoJS over IPv6 only
}

func newGeoJS() *geoJS {
	url, atURL := geoJSURL, geoJSAtURL
	// For tests: points the lookup at a stand-in server.
	if override := os.Getenv("ALIBI_COMPANION_GEOJS_URL"); override != "" {
		url, atURL = override, override+"?ip=%s"
	}
	return &geoJS{url: url, atURL: atURL, client: freshClient("tcp"), ipv6: freshClient("tcp6")}
}

// freshClient opens a new connection for every request, so each answer comes over the route the
// computer uses now, not over a connection opened before the VPN came up.
func freshClient(network string) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DisableKeepAlives = true
	dialer := &net.Dialer{Timeout: 8 * time.Second}
	transport.DialContext = func(ctx context.Context, _, addr string) (net.Conn, error) {
		return dialer.DialContext(ctx, network, addr)
	}
	return &http.Client{Timeout: 10 * time.Second, Transport: transport}
}

// lookup is where the connection appears to come out.
func (g *geoJS) lookup(ctx context.Context) (cover, error) {
	body, err := g.get(ctx, g.client, g.url)
	if err != nil {
		return cover{}, err
	}
	return parseGeoJS(body)
}

// about is where GeoJS places another address, such as the DNS resolver's.
func (g *geoJS) about(ctx context.Context, ip string) (geoAnswer, error) {
	if net.ParseIP(ip) == nil {
		return geoAnswer{}, fmt.Errorf("%q isn't an address", ip)
	}
	body, err := g.get(ctx, g.client, fmt.Sprintf(g.atURL, ip))
	if err != nil {
		return geoAnswer{}, err
	}
	return decodeGeoJS(body)
}

// overIPv6 is where the connection comes out over IPv6, if it has IPv6 at all.
func (g *geoJS) overIPv6(ctx context.Context) (geoAnswer, error) {
	body, err := g.get(ctx, g.ipv6, g.url)
	if err != nil {
		return geoAnswer{}, err
	}
	return decodeGeoJS(body)
}

func (g *geoJS) get(ctx context.Context, client *http.Client, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "alibi-companion/"+version)
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GeoJS answered %s", res.Status)
	}
	return io.ReadAll(io.LimitReader(res.Body, 64<<10))
}

var (
	zonePattern    = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_+-]*(/[A-Za-z0-9_+-]+)*$`)
	countryPattern = regexp.MustCompile(`^[A-Z]{2}$`)
)

// decodeGeoJS reads a GeoJS answer, keeping only what's well formed.
func decodeGeoJS(body []byte) (geoAnswer, error) {
	var answer struct {
		IP          string     `json:"ip"`
		Timezone    string     `json:"timezone"`
		CountryCode string     `json:"country_code"`
		Country     string     `json:"country"`
		City        string     `json:"city"`
		Org         string     `json:"organization_name"`
		Latitude    coordinate `json:"latitude"`
		Longitude   coordinate `json:"longitude"`
	}
	if err := json.Unmarshal(body, &answer); err != nil {
		return geoAnswer{}, fmt.Errorf("reading GeoJS's answer: %w", err)
	}
	a := geoAnswer{IP: clean(answer.IP), City: clean(answer.City), Org: clean(answer.Org)}
	if len(answer.Timezone) <= 64 && zonePattern.MatchString(answer.Timezone) {
		a.Timezone = answer.Timezone
	}
	if code := strings.ToUpper(answer.CountryCode); countryPattern.MatchString(code) {
		a.Country = code
		a.CountryName = countryNames[code]
		if a.CountryName == "" {
			a.CountryName = clean(answer.Country)
		}
	}
	if lat, lon := answer.Latitude.value, answer.Longitude.value; lat != nil && lon != nil && math.Abs(*lat) <= 90 && math.Abs(*lon) <= 180 {
		a.Lat, a.Lon = lat, lon
	}
	return a, nil
}

// clean keeps text from a server short and printable, for the tray to show.
func clean(s string) string {
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, strings.TrimSpace(s))
	if r := []rune(s); len(r) > 60 {
		s = string(r[:60])
	}
	return s
}

// parseGeoJS turns a GeoJS answer into a cover. Without a usable time zone and country there's no
// cover to send, so it fails rather than guess.
func parseGeoJS(body []byte) (cover, error) {
	a, err := decodeGeoJS(body)
	if err != nil {
		return cover{}, err
	}
	if a.Timezone == "" {
		return cover{}, errors.New("GeoJS gave no usable time zone")
	}
	if a.Country == "" {
		return cover{}, errors.New("GeoJS gave no usable country")
	}
	return cover{
		Timezone: a.Timezone, Locale: countryLocales[a.Country], Lat: a.Lat, Lon: a.Lon,
		Country: a.Country, CountryName: a.CountryName, City: a.City, Org: a.Org, IP: a.IP,
	}, nil
}

// GeoJS sends coordinates as strings ("38.5347"). A number works too; anything else counts as unknown.
type coordinate struct{ value *float64 }

func (c *coordinate) UnmarshalJSON(b []byte) error {
	v, err := strconv.ParseFloat(strings.Trim(string(b), `"`), 64)
	if err == nil && !math.IsNaN(v) && !math.IsInf(v, 0) {
		c.value = &v
	}
	return nil
}
