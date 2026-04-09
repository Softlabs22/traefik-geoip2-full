package traefik_geoip2_full

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

// testDB is the path to the MaxMind GeoIP2-City test database.
// Download from: https://github.com/maxmind/MaxMind-DB/raw/main/test-data/GeoIP2-City-Test.mmdb
const testDB = "testdata/GeoIP2-City-Test.mmdb"

// newTestMiddleware creates a GeoIP2Full handler backed by testDB, or skips if absent.
// ipHeaders mirrors what a real deployment would pass via values configuration.
func newTestMiddleware(t *testing.T) http.Handler {
	t.Helper()
	if _, err := os.Stat(testDB); os.IsNotExist(err) {
		t.Skipf("test DB not found at %s — skipping integration test", testDB)
	}
	cfg := CreateConfig()
	cfg.DBPath = testDB
	cfg.IPHeaders = []string{"partner-ip", "CF-Connecting-IP", "X-Client-IP"}
	h, err := New(context.Background(), http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
		rw.WriteHeader(http.StatusOK)
	}), cfg, "test")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return h
}

// serveAndHeaders runs the middleware with the given request and returns the mutated request headers.
func serveAndHeaders(t *testing.T, h http.Handler, req *http.Request) http.Header {
	t.Helper()
	rw := httptest.NewRecorder()
	h.ServeHTTP(rw, req)
	return req.Header
}

// ---- CreateConfig -----------------------------------------------------------

func TestCreateConfig_Defaults(t *testing.T) {
	cfg := CreateConfig()

	if cfg.DBPath != "/geoip2/GeoLite2-City.mmdb" {
		t.Errorf("DBPath = %q, want /geoip2/GeoLite2-City.mmdb", cfg.DBPath)
	}
	if cfg.RealIPHeader != "X-Real-Client-IP" {
		t.Errorf("RealIPHeader = %q, want X-Real-Client-IP", cfg.RealIPHeader)
	}
	// IPHeaders has no built-in defaults — configured via values when needed
	if len(cfg.IPHeaders) != 0 {
		t.Errorf("IPHeaders = %v, want empty", cfg.IPHeaders)
	}
}

// ---- canonical --------------------------------------------------------------

func TestCanonical(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"partner-ip", "Partner-Ip"},
		{"CF-Connecting-IP", "Cf-Connecting-Ip"},
		{"X-Client-IP", "X-Client-Ip"},
		{"x-forwarded-for", "X-Forwarded-For"},
	}
	for _, tt := range tests {
		got := canonical([]string{tt.in})
		if got[0] != tt.want {
			t.Errorf("canonical(%q) = %q, want %q", tt.in, got[0], tt.want)
		}
	}
}

// ---- resolveIP --------------------------------------------------------------

func newUnitHandler() *GeoIP2Full {
	return &GeoIP2Full{
		ipHeaders:    canonical([]string{"partner-ip", "CF-Connecting-IP", "X-Client-IP"}),
		realIPHeader: http.CanonicalHeaderKey("X-Real-Client-IP"),
	}
}

func TestResolveIP_PartnerIPWins(t *testing.T) {
	g := newUnitHandler()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Partner-Ip", "10.0.0.1")
	req.Header.Set("Cf-Connecting-Ip", "10.0.0.2")
	req.Header.Set("X-Forwarded-For", "10.0.0.3")

	if got := g.resolveIP(req); got != "10.0.0.1" {
		t.Errorf("got %q, want 10.0.0.1", got)
	}
}

func TestResolveIP_CFConnectingIP(t *testing.T) {
	g := newUnitHandler()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Cf-Connecting-Ip", "10.0.0.2")
	req.Header.Set("X-Forwarded-For", "10.0.0.3")

	if got := g.resolveIP(req); got != "10.0.0.2" {
		t.Errorf("got %q, want 10.0.0.2", got)
	}
}

func TestResolveIP_XClientIP(t *testing.T) {
	g := newUnitHandler()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Client-Ip", "10.0.0.3")
	req.Header.Set("X-Forwarded-For", "10.0.0.4, 10.0.0.5")

	if got := g.resolveIP(req); got != "10.0.0.3" {
		t.Errorf("got %q, want 10.0.0.3", got)
	}
}

func TestResolveIP_XForwardedFor_FirstOnly(t *testing.T) {
	g := newUnitHandler()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Forwarded-For", "  10.0.0.4  ,  10.0.0.5  ,  10.0.0.6")

	if got := g.resolveIP(req); got != "10.0.0.4" {
		t.Errorf("got %q, want 10.0.0.4", got)
	}
}

func TestResolveIP_RemoteAddr(t *testing.T) {
	g := newUnitHandler()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "192.168.1.100:54321"

	if got := g.resolveIP(req); got != "192.168.1.100" {
		t.Errorf("got %q, want 192.168.1.100", got)
	}
}

func TestResolveIP_SkipsEmptyHeaders(t *testing.T) {
	g := newUnitHandler()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Partner-Ip", "   ")
	req.Header.Set("Cf-Connecting-Ip", "")
	req.Header.Set("X-Forwarded-For", "10.1.2.3")

	if got := g.resolveIP(req); got != "10.1.2.3" {
		t.Errorf("got %q, want 10.1.2.3", got)
	}
}

// ---- enrich (invalid IP) ---------------------------------------------------

func TestEnrich_InvalidIP_NoHeaders(t *testing.T) {
	g := newUnitHandler()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	g.enrich(req, "not-an-ip")

	if got := req.Header.Get("X-GeoIP2-Country"); got != "" {
		t.Errorf("expected no X-GeoIP2-Country, got %q", got)
	}
}

// ---- New: error cases -------------------------------------------------------

func TestNew_MissingDB_ReturnsError(t *testing.T) {
	cfg := CreateConfig()
	cfg.DBPath = "/nonexistent/path/to.mmdb"

	_, err := New(context.Background(), http.NotFoundHandler(), cfg, "test")
	if err == nil {
		t.Fatal("expected error for missing DB, got nil")
	}
}

// ---- integration: GB Boxford (2.125.160.216) — all headers -----------------

func TestIntegration_GB_Boxford_AllHeaders(t *testing.T) {
	h := newTestMiddleware(t)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Forwarded-For", "2.125.160.216")
	got := serveAndHeaders(t, h, req)

	cases := []struct{ header, want string }{
		{"X-Real-Client-Ip", "2.125.160.216"},
		{"X-Geoip2-Ipaddress", "2.125.160.216"},
		{"X-Geoip2-Country", "GB"},
		{"X-Geoip2-Region", "ENG"},
		{"X-Geoip2-City", "Boxford"},
		{"X-Geoip2-Continent", "EU"},
		{"X-Geoip2-Ineu", "false"},
		{"X-Geoip2-Postal", "OX1"},
		{"X-Geoip2-Timezone", "Europe/London"},
	}
	for _, c := range cases {
		if v := got.Get(c.header); v != c.want {
			t.Errorf("%s = %q, want %q", c.header, v, c.want)
		}
	}
	// lat/lon must be non-empty
	if got.Get("X-Geoip2-Latitude") == "" {
		t.Error("X-GeoIP2-Latitude is empty")
	}
	if got.Get("X-Geoip2-Longitude") == "" {
		t.Error("X-GeoIP2-Longitude is empty")
	}
}

// ---- integration: GB London (81.2.69.142) — no postal ----------------------

func TestIntegration_GB_London_NoPostal(t *testing.T) {
	h := newTestMiddleware(t)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Forwarded-For", "81.2.69.142")
	got := serveAndHeaders(t, h, req)

	if v := got.Get("X-Geoip2-Country"); v != "GB" {
		t.Errorf("Country = %q, want GB", v)
	}
	if v := got.Get("X-Geoip2-City"); v != "London" {
		t.Errorf("City = %q, want London", v)
	}
	// postal code is absent for this IP — header should not be set
	if v := got.Get("X-Geoip2-Postal"); v != "" {
		t.Errorf("Postal = %q, want empty", v)
	}
}

// ---- integration: SE Linköping (89.160.20.112) — InEU=true -----------------

func TestIntegration_SE_InEU(t *testing.T) {
	h := newTestMiddleware(t)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Forwarded-For", "89.160.20.112")
	got := serveAndHeaders(t, h, req)

	if v := got.Get("X-Geoip2-Country"); v != "SE" {
		t.Errorf("Country = %q, want SE", v)
	}
	if v := got.Get("X-Geoip2-Ineu"); v != "true" {
		t.Errorf("InEU = %q, want true", v)
	}
	if v := got.Get("X-Geoip2-Continent"); v != "EU" {
		t.Errorf("Continent = %q, want EU", v)
	}
	if v := got.Get("X-Geoip2-Timezone"); v != "Europe/Stockholm" {
		t.Errorf("Timezone = %q, want Europe/Stockholm", v)
	}
}

// ---- integration: US Milton (216.160.83.56) — postal + non-EU --------------

func TestIntegration_US_Milton(t *testing.T) {
	h := newTestMiddleware(t)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Forwarded-For", "216.160.83.56")
	got := serveAndHeaders(t, h, req)

	if v := got.Get("X-Geoip2-Country"); v != "US" {
		t.Errorf("Country = %q, want US", v)
	}
	if v := got.Get("X-Geoip2-Region"); v != "WA" {
		t.Errorf("Region = %q, want WA", v)
	}
	if v := got.Get("X-Geoip2-City"); v != "Milton" {
		t.Errorf("City = %q, want Milton", v)
	}
	if v := got.Get("X-Geoip2-Postal"); v != "98354" {
		t.Errorf("Postal = %q, want 98354", v)
	}
	if v := got.Get("X-Geoip2-Ineu"); v != "false" {
		t.Errorf("InEU = %q, want false", v)
	}
	if v := got.Get("X-Geoip2-Continent"); v != "NA" {
		t.Errorf("Continent = %q, want NA", v)
	}
	if v := got.Get("X-Geoip2-Timezone"); v != "America/Los_Angeles" {
		t.Errorf("Timezone = %q, want America/Los_Angeles", v)
	}
}

// ---- integration: CN Changchun (175.16.199.1) — Asia -----------------------

func TestIntegration_CN_Changchun(t *testing.T) {
	h := newTestMiddleware(t)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Forwarded-For", "175.16.199.1")
	got := serveAndHeaders(t, h, req)

	if v := got.Get("X-Geoip2-Country"); v != "CN" {
		t.Errorf("Country = %q, want CN", v)
	}
	if v := got.Get("X-Geoip2-City"); v != "Changchun" {
		t.Errorf("City = %q, want Changchun", v)
	}
	if v := got.Get("X-Geoip2-Continent"); v != "AS" {
		t.Errorf("Continent = %q, want AS", v)
	}
	if v := got.Get("X-Geoip2-Ineu"); v != "false" {
		t.Errorf("InEU = %q, want false", v)
	}
}

// ---- integration: IPv6 Japan (2001:218::) -----------------------------------

func TestIntegration_IPv6_Japan(t *testing.T) {
	h := newTestMiddleware(t)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Forwarded-For", "2001:218::")
	got := serveAndHeaders(t, h, req)

	if v := got.Get("X-Geoip2-Country"); v != "JP" {
		t.Errorf("Country = %q, want JP", v)
	}
	if v := got.Get("X-Geoip2-Continent"); v != "AS" {
		t.Errorf("Continent = %q, want AS", v)
	}
	if v := got.Get("X-Geoip2-Ipaddress"); v != "2001:218::" {
		t.Errorf("IPAddress = %q, want 2001:218::", v)
	}
}

// ---- integration: unknown IP — no geo headers ------------------------------

func TestIntegration_UnknownIP_NoGeoHeaders(t *testing.T) {
	h := newTestMiddleware(t)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Forwarded-For", "128.101.101.101")
	got := serveAndHeaders(t, h, req)

	// IP is resolved and written, but no geo enrichment for unknown IPs
	if v := got.Get("X-Real-Client-Ip"); v != "128.101.101.101" {
		t.Errorf("X-Real-Client-IP = %q, want 128.101.101.101", v)
	}
	for _, h := range []string{"X-Geoip2-Country", "X-Geoip2-City", "X-Geoip2-Region"} {
		if v := got.Get(h); v != "" {
			t.Errorf("%s = %q, want empty", h, v)
		}
	}
}

// ---- integration: IP header priority via ServeHTTP --------------------------

func TestIntegration_PartnerIPOverridesXFF(t *testing.T) {
	h := newTestMiddleware(t)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Partner-Ip", "89.160.20.112")      // SE
	req.Header.Set("X-Forwarded-For", "216.160.83.56") // US
	got := serveAndHeaders(t, h, req)

	// partner-ip wins → SE
	if v := got.Get("X-Geoip2-Country"); v != "SE" {
		t.Errorf("Country = %q, want SE (partner-ip should win)", v)
	}
	if v := got.Get("X-Real-Client-Ip"); v != "89.160.20.112" {
		t.Errorf("X-Real-Client-IP = %q, want 89.160.20.112", v)
	}
}

func TestIntegration_CFConnectingIPOverridesXFF(t *testing.T) {
	h := newTestMiddleware(t)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Cf-Connecting-Ip", "216.160.83.56") // US
	req.Header.Set("X-Forwarded-For", "89.160.20.112")  // SE
	got := serveAndHeaders(t, h, req)

	if v := got.Get("X-Geoip2-Country"); v != "US" {
		t.Errorf("Country = %q, want US (CF-Connecting-IP should win)", v)
	}
}

// ---- integration: custom config ---------------------------------------------

func TestIntegration_CustomIPHeaders(t *testing.T) {
	if _, err := os.Stat(testDB); os.IsNotExist(err) {
		t.Skipf("test DB not found at %s", testDB)
	}

	cfg := CreateConfig()
	cfg.DBPath = testDB
	cfg.IPHeaders = []string{"X-My-Real-IP"}

	h, err := New(context.Background(), http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
		rw.WriteHeader(http.StatusOK)
	}), cfg, "test")
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-My-Real-Ip", "89.160.20.112")    // SE
	req.Header.Set("X-Forwarded-For", "216.160.83.56") // US — should be ignored
	got := serveAndHeaders(t, h, req)

	if v := got.Get("X-Geoip2-Country"); v != "SE" {
		t.Errorf("Country = %q, want SE", v)
	}
}

func TestIntegration_CustomRealIPHeader(t *testing.T) {
	if _, err := os.Stat(testDB); os.IsNotExist(err) {
		t.Skipf("test DB not found at %s", testDB)
	}

	cfg := CreateConfig()
	cfg.DBPath = testDB
	cfg.RealIPHeader = "X-My-Client-IP"

	h, err := New(context.Background(), http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
		rw.WriteHeader(http.StatusOK)
	}), cfg, "test")
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Forwarded-For", "2.125.160.216")
	got := serveAndHeaders(t, h, req)

	if v := got.Get("X-My-Client-Ip"); v != "2.125.160.216" {
		t.Errorf("X-My-Client-IP = %q, want 2.125.160.216", v)
	}
	// default header must NOT be set
	if v := got.Get("X-Real-Client-Ip"); v != "" {
		t.Errorf("X-Real-Client-IP = %q, want empty", v)
	}
}
