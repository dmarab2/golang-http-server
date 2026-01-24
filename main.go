package main

import (
	"fmt"
	"io"
	"net/http"
	"sync/atomic"
)

// struct that counts how many time the file server has been hit with an atomic int
// (atomic ints are safe to be called across goroutines)
type apiConfig struct {
	fileserverHits atomic.Int32
}

// middleware for an HTTP handler that wraps it in an outer handler that first calls
// a function that increments hits to the file server before executing the original handler
func (cfg *apiConfig) middlewareMetricsInc(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cfg.fileserverHits.Add(1)
		next.ServeHTTP(w, r)
	})
}

// used with the /metrics endpoint to log how many times it has been written to
func (cfg *apiConfig) metricsWriter(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, fmt.Sprintf("Hits: %v", cfg.fileserverHits.Load()))
}

// used with the /reset endpoint to reset the file server hit count
func (cfg *apiConfig) resetWriter(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	cfg.fileserverHits.Store(0)
	io.WriteString(w, "Reset OK")
}

func main() {
	// a server multiplexer that will handle the various paths.
	cfg := &apiConfig{fileserverHits: atomic.Int32{}}
	serveMux := http.NewServeMux()
	server := &http.Server{
		Addr:    ":8080",
		Handler: serveMux,
	}
	// handler function for the health endpoint, just sends "200 OK"
	endpointFunc := func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, "OK")
	}
	// the fileHandler will become part of the multiplexer for the root path
	fileHandler := http.FileServer(http.Dir("."))
	// prevents conflicts
	appStrippedHandler := http.StripPrefix("/app/", fileHandler)
	serveMux.Handle("/app/", cfg.middlewareMetricsInc(appStrippedHandler))
	serveMux.HandleFunc("GET /healthz", endpointFunc)
	serveMux.HandleFunc("GET /metrics", cfg.metricsWriter)
	serveMux.HandleFunc("POST /reset", cfg.resetWriter)
	server.ListenAndServe()
}
