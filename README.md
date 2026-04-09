# traefik-geoip2-full

A Traefik middleware plugin that resolves the real client IP and enriches requests with full [MaxMind GeoIP2 City](https://www.maxmind.com/en/geoip2-city) data.

## IP Resolution Priority

The plugin resolves the client IP in the following order:

1. Headers listed in `ipHeaders` (checked in order, configured via values), e.g. `CF-Connecting-IP`, `X-Client-IP`
2. `X-Forwarded-For` — first entry in the proxy chain
3. `RemoteAddr` — direct TCP connection

## Output Headers

| Header | Description | Example |
|---|---|---|
| `X-GeoIP2-IPAddress` | Resolved client IP | `81.2.69.142` |
| `X-GeoIP2-Country` | ISO 3166-1 alpha-2 country code | `GB` |
| `X-GeoIP2-Region` | First subdivision ISO code | `ENG` |
| `X-GeoIP2-City` | City name (English) | `London` |
| `X-GeoIP2-Continent` | Continent code | `EU` |
| `X-GeoIP2-InEU` | EU membership flag | `false` |
| `X-GeoIP2-Postal` | Postal code | `EC1A` |
| `X-GeoIP2-Latitude` | Decimal latitude | `51.514200` |
| `X-GeoIP2-Longitude` | Decimal longitude | `-0.093100` |
| `X-GeoIP2-Timezone` | IANA timezone name | `Europe/London` |

The resolved IP is also written to `X-Real-Client-IP` (configurable).

## Local development

### Prerequisites

- Docker + Docker Compose
- A MaxMind `.mmdb` database file (see [Requirements](#requirements) below)

### Run with test database

The repo includes a small MaxMind test database sufficient for verifying the plugin
works end-to-end (covers a handful of known IPs, e.g. `81.2.69.142` → GB/London).

```bash
# download test database (once)
make testdata/GeoIP2-City-Test.mmdb

# start Traefik + whoami backend
docker compose up
```

Open the Traefik dashboard: http://localhost:8080

### Run with real GeoLite2 database

```bash
GEOIP_DB_DIR=/path/to/your/geoip2 \
GEOIP_DB_FILE=GeoLite2-City.mmdb \
docker compose up
```

### Verify the plugin

```bash
# CF-Connecting-IP header — should return X-GeoIP2-Country: GB
curl -s http://localhost/ -H "CF-Connecting-IP: 81.2.69.142" | grep -i geoip

# custom-ip-header has highest priority
curl -s http://localhost/ \
  -H "custom-ip-header: 81.2.69.142" \
  -H "CF-Connecting-IP: 1.2.3.4" | grep -i geoip

# unknown IP — GeoIP2 headers should be absent, X-Real-Client-IP still set
curl -s http://localhost/ -H "CF-Connecting-IP: 192.0.2.1" | grep -i "x-real\|geoip"
```

The `whoami` backend echoes all incoming request headers in the response body,
so every `X-GeoIP2-*` header injected by the plugin will be visible there.

### Run unit tests

```bash
make test
```

## Requirements

A MaxMind GeoIP2 or GeoLite2 City database file (`.mmdb`). You can download [GeoLite2-City](https://dev.maxmind.com/geoip/geolite2-free-geolocation-data) for free after creating a MaxMind account.

## Installation

### Static configuration

```yaml
# traefik.yml
experimental:
  plugins:
    geoip2-full:
      moduleName: github.com/Softlabs22/traefik-geoip2-full
      version: v0.0.1
```

### Dynamic configuration

```yaml
http:
  middlewares:
    my-geoip2:
      plugin:
        geoip2-full:
          dbPath: /geoip2/GeoLite2-City.mmdb
```

## Configuration

| Option | Type | Default | Description |
|---|---|---|---|
| `dbPath` | `string` | `/geoip2/GeoLite2-City.mmdb` | Path to the MaxMind `.mmdb` database file |
| `ipHeaders` | `[]string` | `[]` | Ordered list of headers to check for the client IP before falling back to `X-Forwarded-For` and `RemoteAddr` |
| `realIPHeader` | `string` | `X-Real-Client-IP` | Header where the resolved IP is written |

### Custom IP header priority

```yaml
http:
  middlewares:
    my-geoip2:
      plugin:
        geoip2-full:
          dbPath: /geoip2/GeoLite2-City.mmdb
          ipHeaders:
            - CF-Connecting-IP
            - X-Client-IP
          realIPHeader: X-Real-IP
```
