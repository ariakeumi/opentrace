package trace

import "testing"

func TestParseHopLine(t *testing.T) {
	line := "1|1.1.1.1|one.one.one.one|12.34|AS13335|AU|Queensland|Brisbane|South Brisbane|Cloudflare| -27.4748|153.017"
	hop, ok := parseHopLine(line, "en")
	if !ok {
		t.Fatal("expected line to parse")
	}
	if hop.No != 1 {
		t.Fatalf("unexpected hop number: %d", hop.No)
	}
	if hop.IP != "1.1.1.1" {
		t.Fatalf("unexpected IP: %s", hop.IP)
	}
	if hop.Geolocation != "AU Queensland Brisbane South Brisbane" {
		t.Fatalf("unexpected geolocation: %q", hop.Geolocation)
	}
	if hop.Organization != "Cloudflare" {
		t.Fatalf("unexpected organization: %q", hop.Organization)
	}
	if hop.Latitude != "-27.4748" || hop.Longitude != "153.017" {
		t.Fatalf("unexpected coordinates: %s, %s", hop.Latitude, hop.Longitude)
	}
}

func TestParseHopTimeoutLine(t *testing.T) {
	hop, ok := parseHopLine("2|*", "en")
	if !ok {
		t.Fatal("expected timeout hop to parse")
	}
	if hop.Time != "*" || hop.IP != "*" {
		t.Fatalf("unexpected timeout hop: %+v", hop)
	}
}

func TestClassifyPrivateIP(t *testing.T) {
	hop, ok := parseHopLine("1|192.168.1.1|router.local|1.23|AS0|||||12.3|45.6", "cn")
	if !ok {
		t.Fatal("expected private hop to parse")
	}
	if hop.Geolocation != "私有地址（局域网）" {
		t.Fatalf("expected chinese private address, got %q", hop.Geolocation)
	}
	if hop.Latitude != "" || hop.Longitude != "" {
		t.Fatalf("expected private address coordinates to be cleared, got %q, %q", hop.Latitude, hop.Longitude)
	}
}

func TestClassifySharedIPClearsCoordinates(t *testing.T) {
	hop, ok := parseHopLine("2|100.64.0.1||2.34|AS0|||||1.3521|103.8198", "cn")
	if !ok {
		t.Fatal("expected shared hop to parse")
	}
	if hop.Geolocation != "共享地址" {
		t.Fatalf("expected chinese shared address, got %q", hop.Geolocation)
	}
	if hop.Latitude != "" || hop.Longitude != "" {
		t.Fatalf("expected shared address coordinates to be cleared, got %q, %q", hop.Latitude, hop.Longitude)
	}
}

func TestClassifyPrivateIPInEnglish(t *testing.T) {
	hop, ok := parseHopLine("1|192.168.1.1|router.local|1.23|AS0|||||12.3|45.6", "en")
	if !ok {
		t.Fatal("expected private hop to parse")
	}
	if hop.Geolocation != "Private address" {
		t.Fatalf("expected english private address, got %q", hop.Geolocation)
	}
	if hop.Latitude != "" || hop.Longitude != "" {
		t.Fatalf("expected private address coordinates to be cleared, got %q, %q", hop.Latitude, hop.Longitude)
	}
}

func TestClassifySharedIPInEnglish(t *testing.T) {
	hop, ok := parseHopLine("2|100.64.0.1||2.34|AS0|||||1.3521|103.8198", "en")
	if !ok {
		t.Fatal("expected shared hop to parse")
	}
	if hop.Geolocation != "Shared address" {
		t.Fatalf("expected english shared address, got %q", hop.Geolocation)
	}
	if hop.Latitude != "" || hop.Longitude != "" {
		t.Fatalf("expected shared address coordinates to be cleared, got %q, %q", hop.Latitude, hop.Longitude)
	}
}
