package trace

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"slices"
	"time"
)

func resolveTarget(ctx context.Context, target string, resolver string) (string, error) {
	if ip := net.ParseIP(target); ip != nil {
		return ip.String(), nil
	}

	addresses, err := lookupIPs(ctx, target, resolver)
	if err != nil {
		return "", err
	}
	if len(addresses) == 0 {
		return "", fmt.Errorf("no IP addresses resolved for %s", target)
	}

	return preferredIP(addresses), nil
}

func lookupIPs(ctx context.Context, host string, resolver string) ([]string, error) {
	switch resolver {
	case "", "system":
		return lookupWithResolver(ctx, host, "")
	case "google":
		return lookupWithResolver(ctx, host, "8.8.8.8:53")
	case "cloudflare_doh":
		return lookupWithCloudflareDoH(ctx, host)
	default:
		return nil, fmt.Errorf("unsupported DNS resolver: %s", resolver)
	}
}

func lookupWithResolver(ctx context.Context, host string, dnsServer string) ([]string, error) {
	resolver := net.DefaultResolver
	if dnsServer != "" {
		resolver = &net.Resolver{
			PreferGo: true,
			Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
				var dialer net.Dialer
				return dialer.DialContext(ctx, "udp", dnsServer)
			},
		}
	}

	results, err := resolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}

	addresses := make([]string, 0, len(results))
	for _, result := range results {
		if result.IP == nil {
			continue
		}
		addresses = append(addresses, result.IP.String())
	}
	return dedupeStrings(addresses), nil
}

func lookupWithCloudflareDoH(ctx context.Context, host string) ([]string, error) {
	client := &http.Client{Timeout: 5 * time.Second}
	addresses := make([]string, 0, 4)

	for _, recordType := range []string{"A", "AAAA"} {
		queryURL := "https://cloudflare-dns.com/dns-query?name=" + url.QueryEscape(host) + "&type=" + recordType
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, queryURL, nil)
		if err != nil {
			return nil, err
		}
		request.Header.Set("Accept", "application/dns-json")

		response, err := client.Do(request)
		if err != nil {
			return nil, err
		}

		var payload struct {
			Answer []struct {
				Data string `json:"data"`
				Type int    `json:"type"`
			} `json:"Answer"`
		}
		err = json.NewDecoder(response.Body).Decode(&payload)
		response.Body.Close()
		if err != nil {
			return nil, err
		}

		for _, answer := range payload.Answer {
			if answer.Data == "" {
				continue
			}
			if _, err := netip.ParseAddr(answer.Data); err == nil {
				addresses = append(addresses, answer.Data)
			}
		}
	}

	return dedupeStrings(addresses), nil
}

func preferredIP(addresses []string) string {
	for _, address := range addresses {
		if parsed, err := netip.ParseAddr(address); err == nil && parsed.Is4() {
			return address
		}
	}
	return addresses[0]
}

func dedupeStrings(values []string) []string {
	unique := make([]string, 0, len(values))
	for _, value := range values {
		if !slices.Contains(unique, value) {
			unique = append(unique, value)
		}
	}
	return unique
}
