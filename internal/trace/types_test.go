package trace

import (
	"testing"
	"time"
)

func TestNormalizeAllowsKnownDataProviders(t *testing.T) {
	testCases := []string{"", "IPInfo", "IP.SB", "IPAPI.com"}
	for _, provider := range testCases {
		req := Request{
			Target:       "example.com",
			DataProvider: provider,
		}
		if _, _, err := req.Normalize(2 * time.Minute); err != nil {
			t.Fatalf("provider %q should be accepted: %v", provider, err)
		}
	}
}

func TestNormalizeRejectsUnknownDataProvider(t *testing.T) {
	req := Request{
		Target:       "example.com",
		DataProvider: "BadProvider",
	}
	if _, _, err := req.Normalize(2 * time.Minute); err == nil {
		t.Fatal("expected unknown data provider to be rejected")
	}
}

func TestNormalizeAllowsKnownDNSResolvers(t *testing.T) {
	testCases := []string{"", "system", "google", "cloudflare_doh"}
	for _, resolver := range testCases {
		req := Request{
			Target:      "example.com",
			DNSResolver: resolver,
		}
		if _, _, err := req.Normalize(2 * time.Minute); err != nil {
			t.Fatalf("resolver %q should be accepted: %v", resolver, err)
		}
	}
}

func TestNormalizeRejectsUnknownDNSResolver(t *testing.T) {
	req := Request{
		Target:      "example.com",
		DNSResolver: "bad_dns",
	}
	if _, _, err := req.Normalize(2 * time.Minute); err == nil {
		t.Fatal("expected unknown DNS resolver to be rejected")
	}
}

func TestNormalizeTraceLanguageFromBrowserLocale(t *testing.T) {
	req := Request{
		Target:   "example.com",
		Language: "zh-CN",
	}
	normalized, _, err := req.Normalize(2 * time.Minute)
	if err != nil {
		t.Fatalf("normalize failed: %v", err)
	}
	if normalized.Language != "cn" {
		t.Fatalf("expected cn, got %q", normalized.Language)
	}
}

func TestNormalizeTraceLanguageDefaultsToEnglish(t *testing.T) {
	req := Request{
		Target:   "example.com",
		Language: "en-US",
	}
	normalized, _, err := req.Normalize(2 * time.Minute)
	if err != nil {
		t.Fatalf("normalize failed: %v", err)
	}
	if normalized.Language != "en" {
		t.Fatalf("expected en, got %q", normalized.Language)
	}
}

func TestPreferredTraceLanguagePrefersChineseLocale(t *testing.T) {
	language := PreferredTraceLanguage("en-US", "zh-CN,zh;q=0.9,en;q=0.8")
	if language != "cn" {
		t.Fatalf("expected cn, got %q", language)
	}
}

func TestPreferredTraceLanguageFallsBackToEnglish(t *testing.T) {
	language := PreferredTraceLanguage("en-US,en;q=0.9")
	if language != "en" {
		t.Fatalf("expected en, got %q", language)
	}
}
