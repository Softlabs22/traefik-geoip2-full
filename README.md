# traefik-geoip2-full

A Traefik middleware plugin that resolves the real client IP and enriches requests with full [MaxMind GeoIP2 City](https://www.maxmind.com/en/geoip2-city) data.

## IP Resolution Priority

The plugin resolves the client IP in the following order:

1. `partner-ip` — explicit partner API override
2. `CF-Connecting-IP` — Cloudflare edge header
3. `X-Client-IP` — standard client IP header
4. `X-Forwarded-For` — first entry in the proxy chain
5. `RemoteAddr` — direct TCP connection

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
      version: v1.0.0
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

### Docker example

```yaml
services:
  traefik:
    image: traefik:v3
    volumes:
      - ./traefik.yml:/etc/traefik/traefik.yml
      - ./geoip2:/geoip2
    labels:
      - "traefik.http.middlewares.my-geoip2.plugin.geoip2-full.dbPath=/geoip2/GeoLite2-City.mmdb"
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
            - X-Forwarded-For
          realIPHeader: X-Real-IP
```

## License

MIT
