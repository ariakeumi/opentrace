package trace

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"
)

var (
	ErrInvalidRequest        = errors.New("invalid trace request")
	ErrNextTraceNotFound     = errors.New("nexttrace executable not found")
	ErrNextTraceIncompatible = errors.New("nexttrace binary is incompatible with current platform")
)

type Protocol string

const (
	ProtocolICMP Protocol = "icmp"
	ProtocolTCP  Protocol = "tcp"
	ProtocolUDP  Protocol = "udp"
)

type Request struct {
	Target         string   `json:"target"`
	Protocol       Protocol `json:"protocol"`
	DataProvider   string   `json:"dataProvider"`
	DNSResolver    string   `json:"dnsResolver"`
	Language       string   `json:"language"`
	MTR            bool     `json:"mtr"`
	MaxHops        int      `json:"maxHops"`
	Queries        int      `json:"queries"`
	TimeoutSeconds int      `json:"timeoutSeconds"`
}

func (r Request) Normalize(defaultTimeout time.Duration) (Request, time.Duration, error) {
	r.Target = sanitizeTarget(r.Target)
	if r.Target == "" {
		return Request{}, 0, fmt.Errorf("%w: target is required", ErrInvalidRequest)
	}
	if strings.HasPrefix(r.Target, "-") {
		return Request{}, 0, fmt.Errorf("%w: target cannot start with '-'", ErrInvalidRequest)
	}
	if !looksLikeHostOrIP(r.Target) {
		return Request{}, 0, fmt.Errorf("%w: unsupported target format", ErrInvalidRequest)
	}

	switch r.Protocol {
	case "", ProtocolICMP:
		r.Protocol = ProtocolICMP
	case ProtocolTCP, ProtocolUDP:
	default:
		return Request{}, 0, fmt.Errorf("%w: protocol must be icmp, tcp, or udp", ErrInvalidRequest)
	}

	switch r.DataProvider {
	case "", "IPInfo", "IP.SB", "IPAPI.com":
	default:
		return Request{}, 0, fmt.Errorf("%w: unsupported data provider", ErrInvalidRequest)
	}

	switch r.DNSResolver {
	case "", "system", "google", "cloudflare_doh":
	default:
		return Request{}, 0, fmt.Errorf("%w: unsupported DNS resolver", ErrInvalidRequest)
	}

	r.Language = normalizeTraceLanguage(r.Language)

	if r.MaxHops < 0 || r.MaxHops > 128 {
		return Request{}, 0, fmt.Errorf("%w: maxHops must be between 0 and 128", ErrInvalidRequest)
	}
	if r.Queries < 0 || r.Queries > 10 {
		return Request{}, 0, fmt.Errorf("%w: queries must be between 0 and 10", ErrInvalidRequest)
	}

	timeout := defaultTimeout
	if r.TimeoutSeconds > 0 {
		timeout = time.Duration(r.TimeoutSeconds) * time.Second
	}
	if timeout < 10*time.Second {
		timeout = 10 * time.Second
	}
	if timeout > 10*time.Minute {
		return Request{}, 0, fmt.Errorf("%w: timeoutSeconds is too large", ErrInvalidRequest)
	}
	if r.MTR && r.Queries == 0 {
		r.Queries = 1
	}

	return r, timeout, nil
}

type Hop struct {
	No           int    `json:"no"`
	IP           string `json:"ip"`
	Time         string `json:"time"`
	Geolocation  string `json:"geolocation"`
	AS           string `json:"as"`
	Hostname     string `json:"hostname"`
	Organization string `json:"organization"`
	Latitude     string `json:"latitude"`
	Longitude    string `json:"longitude"`
}

type Event struct {
	Type      string    `json:"type"`
	Timestamp time.Time `json:"timestamp"`
	SessionID string    `json:"sessionId"`
	Message   string    `json:"message,omitempty"`
	Status    string    `json:"status,omitempty"`
	ExitCode  int       `json:"exitCode,omitempty"`
	Hop       *Hop      `json:"hop,omitempty"`
}

func sanitizeTarget(input string) string {
	input = strings.TrimSpace(input)
	if input == "" {
		return ""
	}
	if parsed, err := url.Parse(input); err == nil && parsed.Host != "" {
		return parsed.Hostname()
	}

	if host, port, err := net.SplitHostPort(input); err == nil && host != "" && port != "" {
		return host
	}
	if strings.Count(input, ":") == 1 && strings.Contains(input, ".") {
		if host, _, ok := strings.Cut(input, ":"); ok {
			return host
		}
	}
	return input
}

func looksLikeHostOrIP(target string) bool {
	if ip := net.ParseIP(target); ip != nil {
		return true
	}

	for _, r := range target {
		if !(r == '.' || r == '-' || r == ':' || r == '[' || r == ']' ||
			(r >= '0' && r <= '9') ||
			(r >= 'a' && r <= 'z') ||
			(r >= 'A' && r <= 'Z')) {
			return false
		}
	}
	return strings.ContainsAny(target, ".:") || len(target) > 1
}

func normalizeTraceLanguage(language string) string {
	language = strings.TrimSpace(strings.ToLower(language))
	if language == "" {
		return "en"
	}
	if strings.HasPrefix(language, "zh") || language == "cn" {
		return "cn"
	}
	return "en"
}

func PreferredTraceLanguage(values ...string) string {
	for _, value := range values {
		if value == "" {
			continue
		}

		for _, part := range strings.Split(value, ",") {
			token := strings.TrimSpace(part)
			if token == "" {
				continue
			}
			if semi := strings.IndexByte(token, ';'); semi >= 0 {
				token = token[:semi]
			}
			if normalizeTraceLanguage(token) == "cn" {
				return "cn"
			}
		}
	}
	return "en"
}
