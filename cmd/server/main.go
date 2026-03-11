package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"path"
	"strings"
	"time"

	"opentrace-web/internal/trace"
	"opentrace-web/internal/webui"
)

func main() {
	addr := flag.String("addr", ":8080", "HTTP listen address")
	nextTraceBin := flag.String("nexttrace-bin", "", "Path to the nexttrace executable")
	flag.Parse()

	runner := trace.NewRunner(trace.RunnerOptions{
		NextTracePath:  *nextTraceBin,
		DefaultTimeout: 2 * time.Minute,
		SessionTTL:     15 * time.Minute,
	})
	manager := trace.NewManager(runner, 15*time.Minute)

	mux := http.NewServeMux()
	mux.Handle("/app.js", webui.StaticHandler("app.js"))
	mux.Handle("/styles.css", webui.StaticHandler("styles.css"))
	mux.HandleFunc("/", serveIndex)
	mux.HandleFunc("/api/health", handleHealth)
	mux.HandleFunc("/api/traces", handleCreateTrace(manager))
	mux.HandleFunc("/api/traces/", handleTraceSession(manager))

	server := &http.Server{
		Addr:              *addr,
		Handler:           loggingMiddleware(mux),
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Printf("OpenTrace web listening on http://127.0.0.1%s", *addr)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

func serveIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	webui.ServeIndex(w, r)
}

func handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "ok",
		"time":   time.Now().UTC(),
	})
}

func handleCreateTrace(manager *trace.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req trace.Request
		if err := decodeJSON(r.Body, &req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		req.Language = trace.PreferredTraceLanguage(req.Language, r.Header.Get("Accept-Language"))

		session, err := manager.Create(context.Background(), req)
		if err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, trace.ErrInvalidRequest) ||
				errors.Is(err, trace.ErrNextTraceNotFound) ||
				errors.Is(err, trace.ErrNextTraceIncompatible) {
				status = http.StatusBadRequest
			}
			http.Error(w, err.Error(), status)
			return
		}

		writeJSON(w, http.StatusCreated, map[string]any{
			"id":        session.ID(),
			"status":    session.Status(),
			"streamUrl": fmt.Sprintf("/api/traces/%s/events", session.ID()),
			"stopUrl":   fmt.Sprintf("/api/traces/%s", session.ID()),
		})
	}
}

func handleTraceSession(manager *trace.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		trimmed := strings.TrimPrefix(r.URL.Path, "/api/traces/")
		trimmed = path.Clean("/" + trimmed)
		parts := strings.Split(strings.TrimPrefix(trimmed, "/"), "/")
		if len(parts) == 0 || parts[0] == "." || parts[0] == "" {
			http.NotFound(w, r)
			return
		}

		id := parts[0]
		session, ok := manager.Get(id)
		if !ok {
			http.NotFound(w, r)
			return
		}

		if len(parts) == 2 && parts[1] == "events" {
			if r.Method != http.MethodGet {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			streamEvents(w, r, session)
			return
		}

		switch r.Method {
		case http.MethodGet:
			writeJSON(w, http.StatusOK, map[string]any{
				"id":      session.ID(),
				"status":  session.Status(),
				"request": session.Request(),
			})
		case http.MethodDelete:
			session.Stop()
			w.WriteHeader(http.StatusNoContent)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}
}

func streamEvents(w http.ResponseWriter, r *http.Request, session *trace.Session) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	history, stream, cancel := session.ReplayAndSubscribe()
	defer cancel()

	for _, event := range history {
		if err := writeSSE(w, event); err != nil {
			return
		}
	}
	flusher.Flush()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-stream:
			if !ok {
				return
			}
			if err := writeSSE(w, event); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func writeSSE(w io.Writer, event trace.Event) error {
	payload, err := json.Marshal(event)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "data: %s\n\n", payload); err != nil {
		return err
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func decodeJSON(r io.Reader, dst any) error {
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return fmt.Errorf("invalid JSON body: %w", err)
	}
	return nil
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("%s %s %s", r.Method, r.URL.Path, time.Since(start).Round(time.Millisecond))
	})
}
