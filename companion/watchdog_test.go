package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestSettings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if got := loadSettings(path); !got.Sync || !got.Alerts || !got.Sound || !got.EdgeLine {
		t.Errorf("defaults: %+v", got)
	}
	os.WriteFile(path, []byte(`{"sound": false, "allowedCountries": ["no", " se ", "bogus", "X1"], "homeCountry": "ir"}`), 0o644)
	got := loadSettings(path)
	if got.Sound || !got.Sync || !slices.Equal(got.Allowed, []string{"NO", "SE"}) || got.Home != "IR" {
		t.Errorf("read back: %+v", got)
	}
	got.EdgeLine = false
	if err := saveSettings(path, got); err != nil {
		t.Fatal(err)
	}
	if again := loadSettings(path); again.EdgeLine || again.Sound || !slices.Equal(again.Allowed, got.Allowed) {
		t.Errorf("round trip: %+v", again)
	}
	os.WriteFile(path, []byte(`not json`), 0o644)
	if broken := loadSettings(path); broken.Sync != true {
		t.Errorf("a broken file should read as the defaults: %+v", broken)
	}
}

func at(country string) cover {
	return cover{Timezone: "Etc/UTC", Country: country, CountryName: countryNames[country]}
}

func TestJudge(t *testing.T) {
	s := defaultSettings()
	none := leakReport{IPv6: "none", DNS: geoAnswer{IP: "1.1.1.1", Org: "Cloudflare, Inc."}}
	for _, c := range []struct {
		name  string
		place cover
		leaks leakReport
		home  string
		s     settings
		want  verdict
		says  string
	}{
		{"no answer yet", cover{}, none, "IR", s, unknown, ""},
		{"away from home", at("TJ"), none, "IR", s, fine, ""},
		{"VPN off: at home", at("IR"), none, "IR", s, wrong, "comes out at home, in Iran. Is the VPN off?"},
		{"home unknown", at("IR"), none, "", s, fine, ""},
		{"blocked", at("RU"), none, "IR", settings{Blocked: []string{"RU"}}, wrong, "in Russia, which you blocked"},
		{"not allowed", at("DE"), none, "IR", settings{Allowed: []string{"NO", "SE"}}, wrong, "in Germany, which isn't one of your countries"},
		{"allowed", at("NO"), none, "IR", settings{Allowed: []string{"NO", "SE"}}, fine, ""},
		// With a list of countries, home is just another country that isn't on it.
		{"allowed home", at("IR"), none, "IR", settings{Allowed: []string{"IR"}}, fine, ""},
		{"IPv6 elsewhere", at("TJ"), leakReport{IPv6: "seen", IPv6At: geoAnswer{Country: "IR"}}, "IR", s, wrong, "IPv6 traffic comes out in Iran, outside the VPN"},
		{"IPv6 with the VPN", at("TJ"), leakReport{IPv6: "seen", IPv6At: geoAnswer{Country: "TJ"}}, "IR", s, fine, ""},
		{"DNS at home", at("TJ"), leakReport{DNS: geoAnswer{Country: "IR", CountryName: "Iran", City: "Tehran", Org: "TCI"}}, "IR", s, wrong, "DNS lookups go through Tehran, Iran (TCI), outside the VPN"},
		{"DNS abroad", at("TJ"), leakReport{DNS: geoAnswer{Country: "DE", CountryName: "Germany", Org: "Google LLC"}}, "IR", s, fine, ""},
	} {
		got, problems := judge(c.place, c.leaks, c.home, c.s)
		if got != c.want || (c.says != "" && !strings.Contains(strings.Join(problems, " "), c.says)) || (c.says == "" && len(problems) > 0) {
			t.Errorf("%s: %v %q, want %v saying %q", c.name, got, problems, c.want, c.says)
		}
	}
}

func TestLeakChecker(t *testing.T) {
	c := leakChecker{
		resolverIP: func(context.Context) (string, error) { return "172.253.12.27", nil },
		about: func(_ context.Context, ip string) (geoAnswer, error) {
			return geoAnswer{Country: "DE", CountryName: "Germany", City: "Frankfurt", Org: "Google LLC"}, nil
		},
		overIPv6: func(context.Context) (geoAnswer, error) { return geoAnswer{}, errors.New("no route") },
	}
	r := c.check(context.Background())
	if r.IPv6 != "none" || r.DNS.IP != "172.253.12.27" || describe(r.DNS) != "Frankfurt, Germany (Google LLC)" || r.DNSErr != "" {
		t.Errorf("%+v", r)
	}
	c.overIPv6 = func(context.Context) (geoAnswer, error) { return geoAnswer{Country: "TJ"}, nil }
	c.resolverIP = func(context.Context) (string, error) { return "", errors.New("timeout") }
	r = c.check(context.Background())
	if r.IPv6 != "seen" || r.IPv6At.Country != "TJ" || r.DNSErr == "" {
		t.Errorf("%+v", r)
	}
}

func TestDescribe(t *testing.T) {
	for want, a := range map[string]geoAnswer{
		"1.1.1.1":                   {IP: "1.1.1.1"},
		"Cloudflare, Inc.":          {IP: "1.1.1.1", Org: "Cloudflare, Inc."},
		"Dushanbe, Tajikistan":      {City: "Dushanbe", CountryName: "Tajikistan"},
		"Tajikistan (Starlink)":     {CountryName: "Tajikistan", Org: "Starlink"},
		"Oslo, Norway (Telenor AS)": {City: "Oslo", CountryName: "Norway", Org: "Telenor AS"},
	} {
		if got := describe(a); got != want {
			t.Errorf("%+v: %q, want %q", a, got, want)
		}
	}
}

func TestHomeCountry(t *testing.T) {
	if got := homeCountry(settings{}, "Asia/Tehran"); got != "IR" {
		t.Errorf("Tehran: %q", got)
	}
	if got := homeCountry(settings{}, "Asia/Calcutta"); got != "IN" { // as Windows names it
		t.Errorf("Calcutta: %q", got)
	}
	if got := homeCountry(settings{Home: "DE"}, "Asia/Tehran"); got != "DE" {
		t.Errorf("set: %q", got)
	}
}

func TestDecodeKeepsWhatTheTrayShows(t *testing.T) {
	a, err := decodeGeoJS([]byte(`{"ip":"129.222.1.2","country_code":"TJ","country":"Tajikistan","city":"Dushanbe\u0007","organization_name":"Space Exploration Technologies Corporation","timezone":"Asia/Dushanbe"}`))
	if err != nil || a.City != "Dushanbe" || a.CountryName != "Tajikistan" || a.IP != "129.222.1.2" || !strings.HasPrefix(a.Org, "Space Exploration") {
		t.Errorf("%+v %v", a, err)
	}
	if long := clean(strings.Repeat("é", 100)); len([]rune(long)) != 60 {
		t.Errorf("clean kept %d runes", len([]rune(long)))
	}
}
