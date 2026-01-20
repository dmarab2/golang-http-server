package main

import (
	"net/http"
)

func main() {
	// a server multiplexer that will handle the various paths.
	serveMux := http.NewServeMux()
	server := &http.Server{
		Addr:    ":8080",
		Handler: serveMux,
	}
	// the fileHandler will become part of the multiplexer for the root path
	fileHandler := http.FileServer(http.Dir("."))
	serveMux.Handle("/", fileHandler)
	server.ListenAndServe()
}
