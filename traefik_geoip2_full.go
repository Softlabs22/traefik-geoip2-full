// Package traefik_geoip2_full is a Traefik middleware plugin that resolves the
// real client IP and performs a full MaxMind GeoIP2 City lookup.
//
// IP resolution order:
//  1. Headers listed in ipHeaders (configured via values, checked in order)
//  2. X-Forwarded-For[0]  (first entry in the proxy chain)
//  3. RemoteAddr          (direct TCP connection)
//
// The resolved IP is written to the header defined by realIPHeader and used for the GeoIP2 lookup.
// Output headers:
//
//	X-GeoIP2-IPAddress   resolved client IP
//	X-GeoIP2-Country     ISO 3166-1 alpha-2 country code        (e.g. "DE")
//	X-GeoIP2-Region      first subdivision ISO code             (e.g. "BY")
//	X-GeoIP2-City        city name in English                   (e.g. "Munich")
//	X-GeoIP2-Continent   continent code                         (e.g. "EU")
//	X-GeoIP2-InEU        EU membership flag                     ("true" / "false")
//	X-GeoIP2-Postal      postal code                            (e.g. "80331")
//	X-GeoIP2-Latitude    decimal latitude                       (e.g. "48.1374")
//	X-GeoIP2-Longitude   decimal longitude                      (e.g. "11.5755")
//	X-GeoIP2-Timezone    IANA timezone name                     (e.g. "Europe/Berlin")
package traefik_geoip2_full

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/oschwald/geoip2-golang"
)

const reloadCheckInterval = 30 * time.Second

type Config struct {
	DBPath       string   `json:"dbPath"`
	IPHeaders    []string `json:"ipHeaders,omitempty"`
	RealIPHeader string   `json:"realIPHeader,omitempty"`
}

func CreateConfig() *Config {
	return &Config{
		DBPath:       "/geoip2/GeoLite2-City.mmdb",
		RealIPHeader: "X-Real-Client-IP",
	}
}

type GeoIP2Full struct {
	next         http.Handler
	name         string
	dbPath       string
	dbModTime    time.Time
	lastCheck    time.Time
	reader       *geoip2.Reader
	mu           sync.RWMutex
	reloading    atomic.Bool
	ipHeaders    []string
	realIPHeader string
}

func New(_ context.Context, next http.Handler, cfg *Config, name string) (http.Handler, error) {
	reader, err := geoip2.Open(cfg.DBPath)
	if err != nil {
		return nil, fmt.Errorf("geoip2-full: cannot open %s: %w", cfg.DBPath, err)
	}

	info, err := os.Stat(cfg.DBPath)
	if err != nil {
		return nil, fmt.Errorf("geoip2-full: cannot stat %s: %w", cfg.DBPath, err)
	}

	return &GeoIP2Full{
		next:         next,
		name:         name,
		dbPath:       cfg.DBPath,
		dbModTime:    info.ModTime(),
		lastCheck:    time.Now(),
		reader:       reader,
		ipHeaders:    canonical(cfg.IPHeaders),
		realIPHeader: http.CanonicalHeaderKey(cfg.RealIPHeader),
	}, nil
}

// reloadIfChanged checks at most once per reloadCheckInterval whether the DB
// file has been replaced, and reloads it if so. Only one reload runs at a time.
func (g *GeoIP2Full) reloadIfChanged() {
	g.mu.RLock()
	due := time.Since(g.lastCheck) >= reloadCheckInterval
	g.mu.RUnlock()

	if !due {
		return
	}

	// Only one goroutine performs the reload; others skip.
	if !g.reloading.CompareAndSwap(false, true) {
		return
	}
	defer g.reloading.Store(false)

	info, err := os.Stat(g.dbPath)
	if err != nil {
		return
	}

	g.mu.Lock()
	g.lastCheck = time.Now()
	unchanged := !info.ModTime().After(g.dbModTime)
	g.mu.Unlock()

	if unchanged {
		return
	}

	r, err := geoip2.Open(g.dbPath)
	if err != nil {
		return
	}

	g.mu.Lock()
	old := g.reader
	g.reader = r
	g.dbModTime = info.ModTime()
	g.mu.Unlock()

	old.Close()
}

func (g *GeoIP2Full) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	g.reloadIfChanged()

	ip := g.resolveIP(req)
	req.Header.Set(g.realIPHeader, ip)
	g.enrich(req, ip)
	g.next.ServeHTTP(rw, req)
}

func (g *GeoIP2Full) resolveIP(req *http.Request) string {
	for _, h := range g.ipHeaders {
		if v := strings.TrimSpace(req.Header.Get(h)); v != "" {
			return v
		}
	}

	if xff := req.Header.Get("X-Forwarded-For"); xff != "" {
		if first := strings.TrimSpace(strings.SplitN(xff, ",", 2)[0]); first != "" {
			return first
		}
	}

	host, _, err := net.SplitHostPort(req.RemoteAddr)
	if err == nil {
		return host
	}
	return req.RemoteAddr
}

func (g *GeoIP2Full) enrich(req *http.Request, rawIP string) {
	ip := net.ParseIP(rawIP)
	if ip == nil {
		return
	}

	g.mu.RLock()
	record, err := g.reader.City(ip)
	g.mu.RUnlock()

	if err != nil {
		return
	}

	req.Header.Set("X-GeoIP2-IPAddress", rawIP)

	if v := record.Country.IsoCode; v != "" {
		req.Header.Set("X-GeoIP2-Country", v)
	}
	if len(record.Subdivisions) > 0 {
		if v := record.Subdivisions[0].IsoCode; v != "" {
			req.Header.Set("X-GeoIP2-Region", v)
		}
	}
	if v := record.City.Names["en"]; v != "" {
		req.Header.Set("X-GeoIP2-City", v)
	}
	if v := record.Continent.Code; v != "" {
		req.Header.Set("X-GeoIP2-Continent", v)
	}
	if record.Country.IsInEuropeanUnion {
		req.Header.Set("X-GeoIP2-InEU", "true")
	} else {
		req.Header.Set("X-GeoIP2-InEU", "false")
	}
	if v := record.Postal.Code; v != "" {
		req.Header.Set("X-GeoIP2-Postal", v)
	}
	if v := record.Location.TimeZone; v != "" {
		req.Header.Set("X-GeoIP2-Timezone", v)
	}
	if record.Location.Latitude != 0 {
		req.Header.Set("X-GeoIP2-Latitude", fmt.Sprintf("%f", record.Location.Latitude))
	}
	if record.Location.Longitude != 0 {
		req.Header.Set("X-GeoIP2-Longitude", fmt.Sprintf("%f", record.Location.Longitude))
	}
}

func canonical(headers []string) []string {
	out := make([]string, len(headers))
	for i, h := range headers {
		out[i] = http.CanonicalHeaderKey(h)
	}
	return out
}
