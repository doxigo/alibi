package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestParseGeoJS(t *testing.T) {
	for _, c := range []struct {
		name, body string
		want       string // the message Alibi would get, or "" for no message
	}{
		{"a real answer", `{"accuracy":50,"asn":14593,"city":"Dushanbe","country":"Tajikistan","country_code":"TJ","latitude":"38.5347","longitude":"68.7778","timezone":"Asia/Dushanbe"}`,
			`{"timezone":"Asia/Dushanbe","locale":"tg-TJ","lat":38.5347,"lon":68.7778}`},
		{"numeric coordinates", `{"timezone":"Asia/Tokyo","country_code":"JP","latitude":35.6895,"longitude":139.6917}`,
			`{"timezone":"Asia/Tokyo","locale":"ja-JP","lat":35.6895,"lon":139.6917}`},
		{"no coordinates", `{"timezone":"Europe/Oslo","country_code":"NO"}`, `{"timezone":"Europe/Oslo","locale":"nb-NO"}`},
		{"coordinates out of range", `{"timezone":"Europe/Oslo","country_code":"NO","latitude":"95","longitude":"10"}`, `{"timezone":"Europe/Oslo","locale":"nb-NO"}`},
		{"unparsable coordinates", `{"timezone":"Europe/Oslo","country_code":"NO","latitude":"nil","longitude":""}`, `{"timezone":"Europe/Oslo","locale":"nb-NO"}`},
		{"lowercase country", `{"timezone":"Europe/Zurich","country_code":"ch"}`, `{"timezone":"Europe/Zurich","locale":"de-CH"}`},
		{"three-part zone", `{"timezone":"America/Argentina/Buenos_Aires","country_code":"AR"}`, `{"timezone":"America/Argentina/Buenos_Aires","locale":"es-AR"}`},
		{"no time zone", `{"country_code":"NO"}`, ""},
		{"a zone that isn't one", `{"timezone":"../../etc","country_code":"NO"}`, ""},
		{"no country", `{"timezone":"Europe/Oslo"}`, ""},
		{"a country that isn't one", `{"timezone":"Europe/Oslo","country_code":"NOR"}`, ""},
		{"not JSON", `<html>`, ""},
	} {
		place, err := parseGeoJS([]byte(c.body))
		got := ""
		if err == nil {
			b, _ := json.Marshal(place)
			got = string(b)
		}
		if got != c.want {
			t.Errorf("%s: got %q (err %v), want %q", c.name, got, err, c.want)
		}
	}
}

func TestLookupFailsOnAnErrorStatus(t *testing.T) {
	geo := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		io.WriteString(w, `{"timezone":"Europe/Oslo","country_code":"NO"}`)
	}))
	defer geo.Close()
	g := newGeoJS()
	g.url = geo.URL
	if _, err := g.lookup(context.Background()); err == nil {
		t.Error("a 429 answer counted as a place")
	}
}

// The table must give what Alibi's own lookup gives: new Intl.Locale(`und-${code}`).maximize().
func TestCountryLocales(t *testing.T) {
	if len(countryLocales) != 26*26 {
		t.Errorf("%d codes, want all %d two-letter codes", len(countryLocales), 26*26)
	}
	for code, want := range map[string]string{"JP": "ja-JP", "CH": "de-CH", "IR": "fa-IR", "TJ": "tg-TJ", "US": "en-US", "NO": "nb-NO", "ZZ": "en-US", "QQ": "en-QQ"} {
		if got := countryLocales[code]; got != want {
			t.Errorf("%s: %s, want %s", code, got, want)
		}
	}
}

// The stand-in for GeoJS is taken only over HTTPS, or plain HTTP to this computer, as tests use.
func TestGeoJSOverride(t *testing.T) {
	for raw, want := range map[string]bool{
		"https://geo.example.com/v1/ip/geo.json": true,
		"http://127.0.0.1:8123":                  true,
		"http://[::1]:8123/geo":                  true,
		"http://localhost:8123":                  true,
		"http://geo.example.com/v1/ip/geo.json":  false,
		"http://192.0.2.7":                       false,
		"ftp://127.0.0.1":                        false,
		"127.0.0.1:8123":                         false,
		"":                                       false,
	} {
		if _, ok := geoJSOverride(raw); ok != want {
			t.Errorf("%q: taken %v, want %v", raw, ok, want)
		}
	}
	t.Setenv("ALIBI_COMPANION_GEOJS_URL", "http://geo.example.com/geo.json")
	if g := newGeoJS(); g.url != geoJSURL {
		t.Errorf("a plain-HTTP stand-in elsewhere was taken: %s", g.url)
	}
}
