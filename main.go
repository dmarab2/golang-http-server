package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/dmarab2/golang-http-server/internal/database"
	"github.com/google/uuid"
	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
)

var censoredWords = map[string]bool{
	"kerfuffle": true,
	"sharbert":  true,
	"fornax":    true,
}

// struct that counts how many time the file server has been hit with an atomic int
// (atomic ints are safe to be called across goroutines)
type apiConfig struct {
	fileserverHits atomic.Int32
	db             *database.Queries
}

// a new user from the database is converted into this before getting turned into JSON
type jsonUser struct {
	ID        uuid.UUID `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Email     string    `json:"email"`
}

// middleware for an HTTP handler that wraps it in an outer handler that first calls
// a function that increments hits to the file server before executing the original handler
func (cfg *apiConfig) middlewareMetricsInc(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cfg.fileserverHits.Add(1)
		next.ServeHTTP(w, r)
	})
}

// used with the /admin/metrics endpoint to log how many times it has been written to
func (cfg *apiConfig) metricsWriter(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	adminHitCount := fmt.Sprintf(`
<html>
  <body>
    <h1>Welcome, Chirpy Admin</h1>
    <p>Chirpy has been visited %d times!</p>
  </body>
</html>
	`, cfg.fileserverHits.Load())
	io.WriteString(w, adminHitCount)
}

// used with the /reset endpoint to reset the file server hit count
func (cfg *apiConfig) resetWriter(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	cfg.fileserverHits.Store(0)
	io.WriteString(w, "Reset OK")
}

func (cfg *apiConfig) createUserWriter(w http.ResponseWriter, req *http.Request) {
	type parameters struct {
		email string
	}
	decoder := json.NewDecoder(req.Body)
	params := parameters{}
	err := decoder.Decode(&params)
	if err != nil {
		log.Printf("Error decoding parameters: %s", err)
		w.WriteHeader(500)
		return
	}
	user, err := cfg.db.CreateUser(req.Context(), params.email)
	if err != nil {
		respondWithError(w, 400, "Unable to make the user.")
		return
	}
	jsonUser := jsonUser{
		ID:        user.ID,
		CreatedAt: user.CreatedAt,
		UpdatedAt: user.UpdatedAt,
		Email:     user.Email,
	}
	respondWithJSON(w, 201, jsonUser)

}

// helper function to write a JSON error if something goes wrong during handling
func respondWithError(w http.ResponseWriter, code int, msg string) {
	type errorStruct struct {
		ErrorString string `json:"error"`
	}
	jsonErr := errorStruct{
		ErrorString: msg,
	}
	data, err := json.Marshal(jsonErr)
	if err != nil {
		log.Printf("Error marshaling json: %s", err)
		w.WriteHeader(500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	w.Write(data)
}

// basic helper function to write a JSON bytearray to the address of the handler that called it
func respondWithJSON(w http.ResponseWriter, code int, payload interface{}) {
	data, err := json.Marshal(payload)
	if err != nil {
		log.Printf("Error marshaling json: %s", err)
		w.WriteHeader(500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	w.Write(data)
}

// helper function to censor a chirp
func censorChirp(preString string) (finalString string) {
	preStringSlice := strings.Split(preString, " ")
	finalStringSlice := make([]string, len(preStringSlice))
	for _, item := range preStringSlice {
		lowercaseItem := strings.ToLower(item)
		if _, exists := censoredWords[lowercaseItem]; exists {
			finalStringSlice = append(finalStringSlice, "****")
		} else {
			finalStringSlice = append(finalStringSlice, item)
		}
	}
	finalString = strings.TrimSpace(strings.Join(finalStringSlice, " "))
	return
}

// handler function for the validate_chirp endpoint, checks to see if a POSTed chirp is valid
func validationHandler(w http.ResponseWriter, req *http.Request) {
	type parameters struct {
		Body string `json:"body"`
	}
	decoder := json.NewDecoder(req.Body)
	params := parameters{}
	err := decoder.Decode(&params)
	if err != nil {
		log.Printf("Error decoding parameters: %s", err)
		w.WriteHeader(500)
		return
	}
	if len(params.Body) > 140 {
		respondWithError(w, 400, "Chirp is too long")
		return
	}
	cleanedChirp := censorChirp(params.Body)
	type cleanStruct struct {
		CleanedBody string `json:"cleaned_body"`
	}
	cleanObject := cleanStruct{CleanedBody: cleanedChirp}
	respondWithJSON(w, 200, cleanObject)
}

func main() {
	godotenv.Load()
	dbURL := os.Getenv("DB_URL")
	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		fmt.Println("Something went wrong!")
		os.Exit(1)
	}
	dbQueries := database.New(db)
	// a server multiplexer that will handle the various paths.
	cfg := &apiConfig{fileserverHits: atomic.Int32{}, db: dbQueries}
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
	serveMux.HandleFunc("GET /api/healthz", endpointFunc)
	serveMux.HandleFunc("GET /admin/metrics", cfg.metricsWriter)
	serveMux.HandleFunc("POST /admin/reset", cfg.resetWriter)
	serveMux.HandleFunc("POST /api/validate_chirp", validationHandler)
	serveMux.HandleFunc("POST /api/users", cfg.createUserWriter)
	server.ListenAndServe()
}
