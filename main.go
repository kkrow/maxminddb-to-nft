package main

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/oschwald/maxminddb-golang/v2"
)

const (
	maxDownloadSize = 1024 * 1024 * 1024 // 1024MB limit
	requestTimeout  = 30 * time.Second
	filePermissions = 0644
	dirPermissions  = 0755
)

type countryRecord struct {
	Country struct {
		ISOCode string `maxminddb:"iso_code"`
	} `maxminddb:"country"`
}

type geoIPGenerator struct {
	client *http.Client
	ipv4   map[string][]netip.Prefix
	ipv6   map[string][]netip.Prefix
}

func newGeoIPGenerator() *geoIPGenerator {
	return &geoIPGenerator{
		client: &http.Client{
			Timeout: requestTimeout,
		},
		ipv4: make(map[string][]netip.Prefix),
		ipv6: make(map[string][]netip.Prefix),
	}
}

func main() {
	generator := newGeoIPGenerator()

	if err := generator.run(); err != nil {
		log.Fatalf("Generation failed: %v", err)
	}
}

func (g *geoIPGenerator) run() error {
	const url = "https://github.com/GitSquared/node-geolite2-redist/raw/refs/heads/master/redist/GeoLite2-Country.tar.gz"

	mmdbData, err := g.downloadAndExtractMMDB(url)
	if err != nil {
		return fmt.Errorf("failed to download and extract MMDB: %w", err)
	}

	if err := g.loadGeoIPData(mmdbData); err != nil {
		return fmt.Errorf("failed to load GeoIP data: %w", err)
	}

	if err := g.generateAllFiles(); err != nil {
		return fmt.Errorf("failed to generate files: %w", err)
	}

	return nil
}

func (g *geoIPGenerator) downloadAndExtractMMDB(url string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	resp, err := g.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP status %d", resp.StatusCode)
	}

	// Limit response size to prevent memory exhaustion
	limitedReader := io.LimitReader(resp.Body, maxDownloadSize)

	gz, err := gzip.NewReader(limitedReader)
	if err != nil {
		return nil, fmt.Errorf("gzip reader: %w", err)
	}
	defer gz.Close()

	return g.extractMMDBFromTar(gz)
}

func (g *geoIPGenerator) extractMMDBFromTar(r io.Reader) ([]byte, error) {
	tr := tar.NewReader(r)

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("reading tar header: %w", err)
		}

		// Security: prevent path traversal
		if !isValidTarPath(hdr.Name) {
			continue
		}

		if strings.HasSuffix(hdr.Name, ".mmdb") {
			// Limit file size to prevent memory exhaustion
			if hdr.Size > maxDownloadSize {
				return nil, fmt.Errorf("MMDB file too large: %d bytes", hdr.Size)
			}

			mmdbData, err := io.ReadAll(io.LimitReader(tr, hdr.Size))
			if err != nil {
				return nil, fmt.Errorf("reading MMDB file: %w", err)
			}
			return mmdbData, nil
		}
	}

	return nil, fmt.Errorf("MMDB file not found in archive")
}

func (g *geoIPGenerator) loadGeoIPData(mmdbData []byte) error {
	db, err := maxminddb.FromBytes(mmdbData)
	if err != nil {
		return fmt.Errorf("opening MMDB: %w", err)
	}
	defer db.Close()

	for result := range db.Networks() {
		var rec countryRecord
		if err := result.Decode(&rec); err != nil {
			continue // Skip invalid records
		}

		pfx := result.Prefix()
		code := rec.Country.ISOCode

		if code == "" || !isValidCountryCode(code) {
			continue
		}

		if pfx.Addr().Is4() {
			g.ipv4[code] = append(g.ipv4[code], pfx)
		} else {
			g.ipv6[code] = append(g.ipv6[code], pfx)
		}
	}

	return nil
}

func (g *geoIPGenerator) generateAllFiles() error {
	// Create output directory
	if err := os.MkdirAll("by_country", dirPermissions); err != nil {
		return fmt.Errorf("creating by_country directory: %w", err)
	}

	// Generate general files
	if err := g.generateGlobalFile(g.ipv4, "geoip_ipv4.nft", "ipv4"); err != nil {
		return fmt.Errorf("generating IPv4 global file: %w", err)
	}

	if err := g.generateGlobalFile(g.ipv6, "geoip_ipv6.nft", "ipv6"); err != nil {
		return fmt.Errorf("generating IPv6 global file: %w", err)
	}

	// Generate per-country files
	if err := g.generateCountryFiles(); err != nil {
		return fmt.Errorf("generating country files: %w", err)
	}

	return nil
}

func (g *geoIPGenerator) generateGlobalFile(countryMap map[string][]netip.Prefix, filename, ipType string) error {
	f, err := os.OpenFile(filename, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, filePermissions)
	if err != nil {
		return fmt.Errorf("creating file %s: %w", filename, err)
	}
	defer f.Close()

	fmt.Fprintln(f, "#!/usr/sbin/nft -f")
	fmt.Fprintln(f, "table inet geoip {")

	// Sort country codes for consistent output
	codes := make([]string, 0, len(countryMap))
	for code := range countryMap {
		codes = append(codes, code)
	}
	sort.Strings(codes)

	for _, code := range codes {
		prefixes := countryMap[code]
		if len(prefixes) == 0 {
			continue
		}

		if err := g.writeNFTSet(f, code, prefixes, ipType); err != nil {
			return fmt.Errorf("writing NFT set for %s: %w", code, err)
		}
	}

	fmt.Fprintln(f, "}")
	fmt.Printf("✅ Generated %s\n", filename)
	return nil
}

func (g *geoIPGenerator) generateCountryFiles() error {
	for code := range g.ipv4 {
		if err := g.generateCountryFile(code, g.ipv4[code], "ipv4"); err != nil {
			return fmt.Errorf("generating IPv4 file for %s: %w", code, err)
		}
	}

	for code := range g.ipv6 {
		if err := g.generateCountryFile(code, g.ipv6[code], "ipv6"); err != nil {
			return fmt.Errorf("generating IPv6 file for %s: %w", code, err)
		}
	}

	return nil
}

func (g *geoIPGenerator) generateCountryFile(code string, prefixes []netip.Prefix, ipType string) error {
	if len(prefixes) == 0 {
		return nil
	}

	countryDir := filepath.Join("by_country", code)
	if err := os.MkdirAll(countryDir, dirPermissions); err != nil {
		return fmt.Errorf("creating country directory %s: %w", countryDir, err)
	}

	filename := filepath.Join(countryDir, fmt.Sprintf("%s_%s.nft", code, ipType))
	f, err := os.OpenFile(filename, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, filePermissions)
	if err != nil {
		return fmt.Errorf("creating file %s: %w", filename, err)
	}
	defer f.Close()

	fmt.Fprintln(f, "#!/usr/sbin/nft -f")
	fmt.Fprintln(f, "table inet geoip {")

	if err := g.writeNFTSet(f, code, prefixes, ipType); err != nil {
		return fmt.Errorf("writing NFT set: %w", err)
	}

	fmt.Fprintln(f, "}")
	return nil
}

func (g *geoIPGenerator) writeNFTSet(w io.Writer, code string, prefixes []netip.Prefix, ipType string) error {
	fmt.Fprintf(w, "    set %s {\n", code)
	fmt.Fprintf(w, "        type %s_addr\n", ipType)
	fmt.Fprintln(w, "        flags interval")
	fmt.Fprint(w, "        elements = { ")

	// Pre-allocate slice for better performance
	parts := make([]string, 0, len(prefixes))
	for _, prefix := range prefixes {
		parts = append(parts, prefix.String())
	}

	fmt.Fprint(w, strings.Join(parts, ", "))
	fmt.Fprintln(w, " }")
	fmt.Fprintln(w, "    }")

	return nil
}

// Security functions

func isValidTarPath(path string) bool {
	// Prevent path traversal attacks
	cleanPath := filepath.Clean(path)
	return !strings.Contains(cleanPath, "..") &&
		!strings.HasPrefix(cleanPath, "/") &&
		!strings.HasPrefix(cleanPath, "\\")
}

func isValidCountryCode(code string) bool {
	// Basic validation for ISO country codes
	return len(code) == 2 &&
		code == strings.ToUpper(code) &&
		isAlphaOnly(code)
}

func isAlphaOnly(s string) bool {
	for _, r := range s {
		if r < 'A' || r > 'Z' {
			return false
		}
	}
	return true
}
