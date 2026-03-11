package trace

import (
	"net"
	"regexp"
	"strconv"
	"strings"
)

var (
	rawHopLine = regexp.MustCompile(`^\d+\|`)
	ansiEscape = regexp.MustCompile(`(\x9B|\x1B\[)[0-?]*[ -/]*[@-~]`)
)

func cleanOutputLine(line string) string {
	return strings.TrimSpace(ansiEscape.ReplaceAllString(line, ""))
}

func isIgnorableLine(line string) bool {
	if line == "" {
		return true
	}
	return strings.HasPrefix(line, "NextTrace ") ||
		strings.Contains(line, "hops max") ||
		strings.HasPrefix(line, "IP Geo Data Provider") ||
		strings.HasPrefix(line, "[NextTrace API]")
}

func parseHopLine(line string, language string) (*Hop, bool) {
	line = cleanOutputLine(line)
	if !rawHopLine.MatchString(line) {
		return nil, false
	}

	parts := strings.Split(line, "|")
	if len(parts) < 2 {
		return nil, false
	}

	no, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil {
		return nil, false
	}

	hop := &Hop{
		No:   no,
		IP:   "*",
		Time: "",
	}

	if len(parts) > 1 && strings.TrimSpace(parts[1]) == "*" {
		hop.Time = "*"
		return hop, true
	}

	if len(parts) > 1 {
		hop.IP = strings.TrimSpace(parts[1])
	}
	if len(parts) > 2 {
		hop.Hostname = strings.TrimSpace(parts[2])
	}
	if len(parts) > 3 {
		hop.Time = strings.TrimSpace(parts[3])
	}
	if len(parts) > 4 {
		hop.AS = strings.TrimSpace(parts[4])
	}
	if len(parts) > 8 {
		hop.Geolocation = strings.Join([]string{
			strings.TrimSpace(parts[5]),
			strings.TrimSpace(parts[6]),
			strings.TrimSpace(parts[7]),
			strings.TrimSpace(parts[8]),
		}, " ")
		hop.Geolocation = strings.TrimSpace(strings.Join(strings.Fields(hop.Geolocation), " "))
	}
	if len(parts) > 9 {
		hop.Organization = strings.TrimSpace(parts[9])
	}
	if len(parts) > 10 {
		hop.Latitude = strings.TrimSpace(parts[10])
	}
	if len(parts) > 11 {
		hop.Longitude = strings.TrimSpace(parts[11])
	}

	if special := classifySpecialIP(hop.IP, language); special != "" {
		hop.Geolocation = special
		hop.Latitude = ""
		hop.Longitude = ""
	}

	return hop, true
}

func classifySpecialIP(ip string, language string) string {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return ""
	}

	if parsed.IsLoopback() {
		return localizedSpecialAddress("loopback", language)
	}
	if parsed.IsPrivate() {
		return localizedSpecialAddress("private", language)
	}

	if ip4 := parsed.To4(); ip4 != nil {
		if ip4[0] == 100 && ip4[1] >= 64 && ip4[1] <= 127 {
			return localizedSpecialAddress("shared", language)
		}
		if ip4[0] == 169 && ip4[1] == 254 {
			return localizedSpecialAddress("link_local", language)
		}
	}

	if parsed.IsLinkLocalUnicast() {
		return localizedSpecialAddress("link_local", language)
	}

	return ""
}

func localizedSpecialAddress(kind string, language string) string {
	switch normalizeTraceLanguage(language) {
	case "cn":
		switch kind {
		case "loopback":
			return "回环地址"
		case "private":
			return "私有地址（局域网）"
		case "shared":
			return "共享地址"
		case "link_local":
			return "链路本地地址"
		}
	default:
		switch kind {
		case "loopback":
			return "Loopback address"
		case "private":
			return "Private address"
		case "shared":
			return "Shared address"
		case "link_local":
			return "Link-local address"
		}
	}
	return ""
}
