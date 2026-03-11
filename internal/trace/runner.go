package trace

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"syscall"
	"time"
)

type RunnerOptions struct {
	NextTracePath  string
	DefaultTimeout time.Duration
	SessionTTL     time.Duration
}

type Runner struct {
	nextTracePath  string
	defaultTimeout time.Duration
}

func NewRunner(options RunnerOptions) *Runner {
	timeout := options.DefaultTimeout
	if timeout <= 0 {
		timeout = 2 * time.Minute
	}
	return &Runner{
		nextTracePath:  options.NextTracePath,
		defaultTimeout: timeout,
	}
}

func (r *Runner) DefaultTimeout() time.Duration {
	return r.defaultTimeout
}

func (r *Runner) Run(ctx context.Context, req Request, emit func(Event)) (int, error) {
	nextTracePath, err := r.resolveNextTracePath()
	if err != nil {
		return 0, err
	}

	resolvedReq := req
	if net.ParseIP(req.Target) == nil {
		resolvedTarget, err := resolveTarget(ctx, req.Target, req.DNSResolver)
		if err != nil {
			return 0, fmt.Errorf("resolve target with %s: %w", displayDNSResolver(req.DNSResolver), err)
		}
		resolvedReq.Target = resolvedTarget
		emit(Event{
			Type:      "log",
			Timestamp: time.Now().UTC(),
			Message:   fmt.Sprintf("resolved %s via %s -> %s", req.Target, displayDNSResolver(req.DNSResolver), resolvedTarget),
		})
	}

	args := buildArguments(resolvedReq)
	cmd := exec.CommandContext(ctx, nextTracePath, args...)
	cmd.Env = os.Environ()

	if req.MTR {
		cmd.Env = append(cmd.Env, "NEXTTRACE_UNINTERRUPTED=1")
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return 0, fmt.Errorf("stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return 0, fmt.Errorf("stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return 0, fmt.Errorf("start nexttrace: %w", err)
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		scanOutput(stdout, func(line string) {
			line = cleanOutputLine(line)
			if hop, ok := parseHopLine(line, resolvedReq.Language); ok {
				emit(Event{Type: "hop", Timestamp: time.Now().UTC(), Hop: hop})
				return
			}
			if isIgnorableLine(line) {
				return
			}
			emit(Event{Type: "log", Timestamp: time.Now().UTC(), Message: line})
		})
	}()
	go func() {
		defer wg.Done()
		scanOutput(stderr, func(line string) {
			line = cleanOutputLine(line)
			if line == "" {
				return
			}
			emit(Event{Type: "error", Timestamp: time.Now().UTC(), Message: line})
		})
	}()

	waitErr := cmd.Wait()
	wg.Wait()

	if waitErr == nil {
		return 0, nil
	}

	if ctx.Err() != nil {
		if exitErr, ok := waitErr.(*exec.ExitError); ok {
			return extractExitCode(exitErr), ctx.Err()
		}
		return 0, ctx.Err()
	}

	var exitErr *exec.ExitError
	if errors.As(waitErr, &exitErr) {
		return extractExitCode(exitErr), waitErr
	}

	return 0, waitErr
}

func buildArguments(req Request) []string {
	args := []string{
		req.Target,
		"--raw",
		"--map",
		"--language",
		req.Language,
	}

	switch req.Protocol {
	case ProtocolTCP:
		args = append(args, "-T")
	case ProtocolUDP:
		args = append(args, "-U")
	}

	if req.DataProvider != "" {
		args = append(args, "--data-provider", req.DataProvider)
	}

	if req.MaxHops > 0 {
		args = append(args, "--max-hops", strconv.Itoa(req.MaxHops))
	}
	if req.Queries > 0 {
		args = append(args, "--queries", strconv.Itoa(req.Queries))
	}

	return args
}

func scanOutput(reader io.Reader, handle func(string)) {
	scanner := bufio.NewScanner(reader)
	buffer := make([]byte, 0, 64*1024)
	scanner.Buffer(buffer, 1024*1024)
	for scanner.Scan() {
		handle(scanner.Text())
	}
}

func extractExitCode(err *exec.ExitError) int {
	if status, ok := err.Sys().(syscall.WaitStatus); ok {
		return status.ExitStatus()
	}
	return err.ExitCode()
}

func (r *Runner) resolveNextTracePath() (string, error) {
	if r.nextTracePath != "" {
		if _, err := os.Stat(r.nextTracePath); err != nil {
			return "", fmt.Errorf("%w: %s", ErrNextTraceNotFound, r.nextTracePath)
		}
		if err := validateBinaryFormat(r.nextTracePath); err != nil {
			return "", fmt.Errorf("%w: %v", ErrNextTraceIncompatible, err)
		}
		return r.nextTracePath, nil
	}
	if envPath := os.Getenv("NEXTTRACE_BIN"); envPath != "" {
		if _, err := os.Stat(envPath); err != nil {
			return "", fmt.Errorf("%w: %s", ErrNextTraceNotFound, envPath)
		}
		if err := validateBinaryFormat(envPath); err != nil {
			return "", fmt.Errorf("%w: %v", ErrNextTraceIncompatible, err)
		}
		return envPath, nil
	}

	candidates := nextTraceCandidates()
	incompatible := make([]string, 0, len(candidates))

	baseDirs := []string{}
	if cwd, err := os.Getwd(); err == nil {
		baseDirs = append(baseDirs, cwd)
	}
	if exePath, err := os.Executable(); err == nil {
		baseDirs = append(baseDirs, filepath.Dir(exePath))
	}

	for _, candidate := range candidates {
		if path, err := exec.LookPath(candidate); err == nil {
			if formatErr := validateBinaryFormat(path); formatErr != nil {
				incompatible = append(incompatible, formatErr.Error())
			} else {
				return path, nil
			}
		}
		for _, dir := range baseDirs {
			fullPath := filepath.Join(dir, candidate)
			if _, err := os.Stat(fullPath); err == nil {
				if formatErr := validateBinaryFormat(fullPath); formatErr != nil {
					incompatible = append(incompatible, formatErr.Error())
					continue
				}
				return fullPath, nil
			}
		}
	}

	if len(incompatible) > 0 {
		return "", fmt.Errorf("%w: %s", ErrNextTraceIncompatible, incompatible[0])
	}

	return "", fmt.Errorf("%w: pass --nexttrace-bin, set NEXTTRACE_BIN, or place nexttrace in PATH", ErrNextTraceNotFound)
}

func nextTraceCandidates() []string {
	raw := []string{
		"nexttrace",
		"nexttrace.exe",
		fmt.Sprintf("nexttrace-%s", runtime.GOARCH),
		fmt.Sprintf("nexttrace-%s.exe", runtime.GOARCH),
		fmt.Sprintf("nexttrace-%s-%s", runtime.GOOS, runtime.GOARCH),
		fmt.Sprintf("nexttrace-%s-%s.exe", runtime.GOOS, runtime.GOARCH),
		fmt.Sprintf("nexttrace_%s_%s", runtime.GOOS, runtime.GOARCH),
		fmt.Sprintf("nexttrace_%s_%s.exe", runtime.GOOS, runtime.GOARCH),
	}

	seen := make(map[string]struct{}, len(raw))
	candidates := make([]string, 0, len(raw))
	for _, candidate := range raw {
		if _, ok := seen[candidate]; ok {
			continue
		}
		seen[candidate] = struct{}{}
		candidates = append(candidates, candidate)
	}
	return candidates
}

func validateBinaryFormat(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	header := make([]byte, 4)
	if _, err := io.ReadFull(file, header); err != nil {
		return fmt.Errorf("cannot inspect %s: %w", path, err)
	}

	detected := detectBinaryFormat(header)
	expected := expectedBinaryFormat(runtime.GOOS)
	if expected == "" || detected == expected {
		return nil
	}

	return fmt.Errorf("%s is %s, but %s/%s needs %s", path, detected, runtime.GOOS, runtime.GOARCH, expected)
}

func displayDNSResolver(resolver string) string {
	switch resolver {
	case "", "system":
		return "system DNS"
	case "google":
		return "Google DNS"
	case "cloudflare_doh":
		return "CloudFlare DoH"
	default:
		return resolver
	}
}

func detectBinaryFormat(header []byte) string {
	switch {
	case bytes.Equal(header, []byte{0x7f, 'E', 'L', 'F'}):
		return "ELF"
	case bytes.Equal(header, []byte{0xfe, 0xed, 0xfa, 0xce}),
		bytes.Equal(header, []byte{0xfe, 0xed, 0xfa, 0xcf}),
		bytes.Equal(header, []byte{0xce, 0xfa, 0xed, 0xfe}),
		bytes.Equal(header, []byte{0xcf, 0xfa, 0xed, 0xfe}),
		bytes.Equal(header, []byte{0xca, 0xfe, 0xba, 0xbe}),
		bytes.Equal(header, []byte{0xbe, 0xba, 0xfe, 0xca}),
		bytes.Equal(header, []byte{0xca, 0xfe, 0xba, 0xbf}),
		bytes.Equal(header, []byte{0xbf, 0xba, 0xfe, 0xca}):
		return "Mach-O"
	case len(header) >= 2 && header[0] == 'M' && header[1] == 'Z':
		return "PE"
	default:
		return "unknown binary format"
	}
}

func expectedBinaryFormat(goos string) string {
	switch goos {
	case "darwin":
		return "Mach-O"
	case "linux":
		return "ELF"
	case "windows":
		return "PE"
	default:
		return ""
	}
}
