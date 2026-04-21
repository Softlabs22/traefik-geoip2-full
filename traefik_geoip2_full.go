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
	"log"
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
	Debug        bool     `json:"debug,omitempty"`
	IPHeaders    []string `json:"ipHeaders,omitempty"`
	RealIPHeader string   `json:"realIPHeader,omitempty"`
}

func CreateConfig() *Config {
	return &Config{
		DBPath:       "/geoip2/GeoLite2-City.mmdb",
		RealIPHeader: "X-Real-Client-IP",
	}
}

// sharedDB holds a single GeoIP2 reader shared across all plugin instances
// that reference the same database file path. Traefik calls New() once per
// router, so without sharing each instance would load its own copy of the DB.
type sharedDB struct {
	path      string
	mu        sync.RWMutex
	reader    *geoip2.Reader
	modTime   time.Time
	lastCheck time.Time
	reloading atomic.Bool
}

var (
	dbsMu sync.Mutex
	dbs   = map[string]*sharedDB{}
)

// getOrOpenDB returns the cached sharedDB for path, opening and caching it on first call.
func getOrOpenDB(path string) (*sharedDB, error) {
	dbsMu.Lock()
	defer dbsMu.Unlock()

	if db, ok := dbs[path]; ok {
		return db, nil
	}

	reader, err := geoip2.Open(path)
	if err != nil {
		return nil, fmt.Errorf("geoip2-full: cannot open %s: %w", path, err)
	}

	info, err := os.Stat(path)
	if err != nil {
		reader.Close()
		return nil, fmt.Errorf("geoip2-full: cannot stat %s: %w", path, err)
	}

	db := &sharedDB{
		path:      path,
		reader:    reader,
		modTime:   info.ModTime(),
		lastCheck: time.Now(),
	}
	dbs[path] = db
	return db, nil
}

// reloadIfChanged checks at most once per reloadCheckInterval whether the DB
// file has been replaced, and reloads it if so. Only one reload runs at a time.
func (db *sharedDB) reloadIfChanged() {
	db.mu.RLock()
	due := time.Since(db.lastCheck) >= reloadCheckInterval
	db.mu.RUnlock()

	if !due {
		return
	}

	if !db.reloading.CompareAndSwap(false, true) {
		return
	}
	defer db.reloading.Store(false)

	info, err := os.Stat(db.path)
	if err != nil {
		return
	}

	db.mu.Lock()
	db.lastCheck = time.Now()
	unchanged := !info.ModTime().After(db.modTime)
	db.mu.Unlock()

	if unchanged {
		return
	}

	r, err := geoip2.Open(db.path)
	if err != nil {
		return
	}

	db.mu.Lock()
	old := db.reader
	db.reader = r
	db.modTime = info.ModTime()
	db.mu.Unlock()

	old.Close()
}

type GeoIP2Full struct {
	next         http.Handler
	name         string
	db           *sharedDB
	debug        bool
	ipHeaders    []string
	realIPHeader string
}

type ipResolution struct {
	IP     string
	Source string
}

func New(_ context.Context, next http.Handler, cfg *Config, name string) (http.Handler, error) {
	db, err := getOrOpenDB(cfg.DBPath)
	if err != nil {
		return nil, err
	}

	return &GeoIP2Full{
		next:         next,
		name:         name,
		db:           db,
		debug:        cfg.Debug,
		ipHeaders:    canonical(cfg.IPHeaders),
		realIPHeader: http.CanonicalHeaderKey(cfg.RealIPHeader),
	}, nil
}

func (g *GeoIP2Full) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	g.db.reloadIfChanged()

	resolution := g.resolveIP(req)
	req.Header.Set(g.realIPHeader, resolution.IP)
	g.enrich(req, resolution.IP)
	g.logDebug(req, resolution)
	g.next.ServeHTTP(rw, req)
}

func (g *GeoIP2Full) resolveIP(req *http.Request) ipResolution {
	for _, h := range g.ipHeaders {
		if v := strings.TrimSpace(req.Header.Get(h)); v != "" {
			return ipResolution{IP: v, Source: h}
		}
	}

	if xff := req.Header.Get("X-Forwarded-For"); xff != "" {
		if first := strings.TrimSpace(strings.SplitN(xff, ",", 2)[0]); first != "" {
			return ipResolution{IP: first, Source: "X-Forwarded-For"}
		}
	}

	host, _, err := net.SplitHostPort(req.RemoteAddr)
	if err == nil {
		return ipResolution{IP: host, Source: "RemoteAddr"}
	}
	return ipResolution{IP: req.RemoteAddr, Source: "RemoteAddr"}
}

func (g *GeoIP2Full) enrich(req *http.Request, rawIP string) {
	ip := net.ParseIP(rawIP)
	if ip == nil {
		return
	}

	g.db.mu.RLock()
	record, err := g.db.reader.City(ip)
	g.db.mu.RUnlock()

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

func (g *GeoIP2Full) logDebug(req *http.Request, resolution ipResolution) {
	if !g.debug {
		return
	}

	log.Printf(
		"geoip2-full middleware=%q method=%s host=%q uri=%q remote_addr=%q ip_source=%q resolved_ip=%q candidates=[%s] emitted=[%s]",
		g.name,
		req.Method,
		req.Host,
		req.URL.RequestURI(),
		req.RemoteAddr,
		resolution.Source,
		resolution.IP,
		strings.Join(g.debugCandidates(req), ", "),
		strings.Join(g.debugEmitted(req), ", "),
	)
}

func (g *GeoIP2Full) debugCandidates(req *http.Request) []string {
	parts := make([]string, 0, len(g.ipHeaders)+2)
	for _, h := range g.ipHeaders {
		parts = append(parts, fmt.Sprintf("%s=%q", h, req.Header.Get(h)))
	}
	parts = append(parts, fmt.Sprintf("X-Forwarded-For=%q", req.Header.Get("X-Forwarded-For")))
	parts = append(parts, fmt.Sprintf("RemoteAddr=%q", req.RemoteAddr))
	return parts
}

func (g *GeoIP2Full) debugEmitted(req *http.Request) []string {
	headers := []string{
		g.realIPHeader,
		"X-GeoIP2-IPAddress",
		"X-GeoIP2-Country",
		"X-GeoIP2-Region",
		"X-GeoIP2-City",
		"X-GeoIP2-Continent",
		"X-GeoIP2-InEU",
		"X-GeoIP2-Postal",
		"X-GeoIP2-Timezone",
		"X-GeoIP2-Latitude",
		"X-GeoIP2-Longitude",
	}

	parts := make([]string, 0, len(headers))
	for _, h := range headers {
		if v := req.Header.Get(h); v != "" {
			parts = append(parts, fmt.Sprintf("%s=%q", h, v))
		}
	}
	return parts
}

func canonical(headers []string) []string {
	out := make([]string, len(headers))
	for i, h := range headers {
		out[i] = http.CanonicalHeaderKey(h)
	}
	return out
}
