# nftables GeoIP Sets Generator

This project downloads and extracts the [GeoLite2-Country](https://dev.maxmind.com/geoip/geolite2-free-geolocation-data) database (in `.mmdb` format), parses all country-related IP prefixes, and generates `nftables`-compatible configuration files:

* `geoip_ipv4.nft` — IPv4 address sets by country
* `geoip_ipv6.nft` — IPv6 address sets by country

## Usage

### Run generator

```bash
go run main.go
```

## Features

- Downloads latest `.mmdb` from [GitSquared/node-geolite2-redist](https://github.com/GitSquared/node-geolite2-redist)
- Parses both IPv4 and IPv6 ranges by country
- Generates:
  - `geoip_ipv4.nft` — all IPv4 prefixes grouped by country
  - `geoip_ipv6.nft` — all IPv6 prefixes grouped by country
  - `by_country/` — individual `.nft` files per country:
    - `by_country/US/US_ipv4.nft`
    - `by_country/US/US_ipv6.nft`

---

## Use cases

### 1. Block incoming/outgoing traffic by country

You can use the sets to filter traffic using `nftables`.

**Block incoming traffic from US:**

```nft
nft add table inet geoip
nft -f geoip_ipv4.nft
nft -f geoip_ipv6.nft

nft add chain inet geoip input { type filter hook input priority 0 \; }
nft add rule inet geoip input ip saddr @US drop
nft add rule inet geoip input ip6 saddr @US drop
```

**Block outgoing traffic to US:**

```nft
nft add chain inet geoip output { type filter hook output priority 0 \; }
nft add rule inet geoip output ip daddr @US drop
nft add rule inet geoip output ip6 daddr @US drop
```

---

### 2. Route traffic from/to specific countries via different interface

Example: Route **all traffic from US IPs** via `pppoe-pppoe` interface using `fwmark` and policy routing:

#### Step 1: Mark packets from US IPs

```nft
nft add table inet geoip
nft -f geoip_ipv4.nft

nft add chain inet geoip prerouting { type filter hook prerouting priority 0 \; }
nft add rule inet geoip prerouting ip saddr @US meta mark set 0x1
```

#### Step 2: Create new routing table

Edit `/etc/iproute2/rt_tables` and add:

```
100 rt_us
```

#### Step 3: Add default route in that table

```bash
ip route add default dev pppoe-pppoe table rt_us
```

#### Step 4: Link marked packets to that route table

```bash
ip rule add fwmark 0x1 table rt_us
```

---

## Automation

This project includes a GitHub Actions workflow that:

* Runs every two weeks (cron: `1 0 * * 0/2`)
* Executes `go run main.go`
* Publishes updated `.nft` files to the `latest` release on GitHub

---

## License

This project uses the [GeoLite2](https://www.maxmind.com/en/geolite2/eula) database from MaxMind, distributed under their [license](https://www.maxmind.com/en/geolite2/eula). You must agree to their terms before using this data.
