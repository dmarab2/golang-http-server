package main

import (
	"io"
	"net/http"
)

func main() {
	// a server multiplexer that will handle the various paths.
	serveMux := http.NewServeMux()
	server := &http.Server{
		Addr:    ":8080",
		Handler: serveMux,
	}
	endpointFunc := func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, "OK")
	}
	// the fileHandler will become part of the multiplexer for the root path
	fileHandler := http.FileServer(http.Dir("."))
	serveMux.Handle("/app/", http.StripPrefix("/app/", fileHandler))
	serveMux.HandleFunc("/healthz", endpointFunc)
	server.ListenAndServe()
}
