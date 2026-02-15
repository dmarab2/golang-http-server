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

	"github.com/dmarab2/golang-http-server/internal/auth"
	"github.com/dmarab2/golang-http-server/internal/database"
	"github.com/google/uuid"
	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
)

var EXPIRETIME int = 3600

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
	platform       string
	secret         string
}

// a new user from the database is converted into this before getting turned into JSON
type jsonUser struct {
	ID        uuid.UUID `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Email     string    `json:"email"`
}

type jsonChirp struct {
	ID        uuid.UUID `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Body      string    `json:"body"`
	User_id   uuid.UUID `json:"user_id"`
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
	if cfg.platform != "dev" {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusForbidden)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	cfg.fileserverHits.Store(0)
	cfg.db.DeleteAllUsers(req.Context())
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, "Reset OK")
}

func turnUserToJson(user database.User) jsonUser {
	jsonUser := jsonUser{
		ID:        user.ID,
		CreatedAt: user.CreatedAt,
		UpdatedAt: user.UpdatedAt,
		Email:     user.Email,
	}
	return jsonUser
}
func addTokenToUser(user database.User, token string, refreshToken string) interface{} {
	jsonUser := turnUserToJson(user)
	tokenUser := struct {
		ID           uuid.UUID `json:"id"`
		CreatedAt    time.Time `json:"created_at"`
		UpdatedAt    time.Time `json:"updated_at"`
		Email        string    `json:"email"`
		Token        string    `json:"token"`
		RefreshToken string    `json:"refresh_token"`
	}{
		ID:           jsonUser.ID,
		CreatedAt:    jsonUser.CreatedAt,
		UpdatedAt:    jsonUser.UpdatedAt,
		Email:        jsonUser.Email,
		Token:        token,
		RefreshToken: refreshToken,
	}
	return tokenUser

}

func (cfg *apiConfig) createUserWriter(w http.ResponseWriter, req *http.Request) {
	type parameters struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	decoder := json.NewDecoder(req.Body)
	params := parameters{}
	err := decoder.Decode(&params)
	if err != nil {
		log.Printf("Error decoding parameters: %s", err)
		w.WriteHeader(500)
		return
	}
	hashedPassword, err := auth.HashPassword(params.Password)
	if err != nil {
		respondWithError(w, 400, "Unable to make the user, password is wrong.")
		return
	}
	createUserParams := database.CreateUserParams{
		Email:          params.Email,
		HashedPassword: hashedPassword,
	}
	user, err := cfg.db.CreateUser(req.Context(), createUserParams)
	if err != nil {
		respondWithError(w, 400, "Unable to make the user.")
		return
	}
	jsonUser := turnUserToJson(user)
	respondWithJSON(w, 201, jsonUser)

}

// function to write a new User from a POST request to the database
func (cfg *apiConfig) resetUserWriter(w http.ResponseWriter, req *http.Request) {
	if cfg.platform != "dev" {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusForbidden)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	cfg.db.DeleteAllUsers(req.Context())
	w.WriteHeader(http.StatusOK)
}

// for now, just checks that the user's password matches the stored hash
func (cfg *apiConfig) loginUser(w http.ResponseWriter, req *http.Request) {
	type parameters struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	decoder := json.NewDecoder(req.Body)
	params := parameters{}
	err := decoder.Decode(&params)
	if err != nil {
		log.Printf("Error decoding parameters: %s", err)
		w.WriteHeader(500)
		return
	}
	databaseUser, err := cfg.db.GetSingleUser(req.Context(), params.Email)
	fmt.Println(databaseUser.Email)
	fmt.Println(databaseUser.HashedPassword)
	if err != nil {
		respondWithError(w, 401, "incorrect email or password")
		return
	}
	samePassword, err := auth.CheckPasswordHash(params.Password, databaseUser.HashedPassword)
	if err != nil {
		respondWithError(w, 401, "incorrect email or password")
		return
	}
	if !(samePassword) {
		respondWithError(w, 401, "incorrect email or password")
		return
	}
	tokenString, err := auth.MakeJWT(databaseUser.ID, cfg.secret, time.Duration(EXPIRETIME)*time.Second)
	refreshTokenString, _ := auth.MakeRefreshToken()
	fmt.Printf("made token: %q\n", tokenString)
	refreshTokenParams := database.CreateRefreshTokenParams{
		Token:  refreshTokenString,
		UserID: databaseUser.ID,
	}
	refreshToken, err := cfg.db.CreateRefreshToken(req.Context(), refreshTokenParams)
	tokenUser := addTokenToUser(databaseUser, tokenString, refreshToken.Token)
	respondWithJSON(w, 200, tokenUser)

}

// handler to create a chirp from a POST request
func (cfg *apiConfig) createChirpWriter(w http.ResponseWriter, req *http.Request) {
	tokenString, errToken := auth.GetBearerToken(req.Header)
	fmt.Printf("The tokenString is %s\n", tokenString)
	if errToken != nil {
		respondWithError(w, 401, errToken.Error())
		return
	}

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
	fmt.Printf("received token: %q\n", tokenString)
	uuidVar, errJWT := auth.ValidateJWT(tokenString, cfg.secret)
	fmt.Printf("uuidVar is %v\n", uuidVar)
	if errJWT != nil {
		respondWithError(w, 401, errJWT.Error())
		return
	}
	if len(params.Body) > 140 {
		respondWithError(w, 400, "Chirp is too long")
		return
	}
	cleanedChirp := censorChirp(params.Body)
	insertDatabaseChirp := database.CreateChirpParams{
		Body:   cleanedChirp,
		UserID: uuidVar,
	}
	databaseChirp, err := cfg.db.CreateChirp(req.Context(), insertDatabaseChirp)
	if err != nil {
		respondWithError(w, 400, err.Error())
		return
	}
	jsonChirp := turnChirpToJson(databaseChirp)
	respondWithJSON(w, 201, jsonChirp)

}

// returns all chirps from the database. will be edited later for filtering.
func (cfg *apiConfig) getAllChirps(w http.ResponseWriter, req *http.Request) {
	allChirps, err := cfg.db.GetAllChirps(req.Context())
	if err != nil {
		respondWithError(w, 400, err.Error())
		return
	}
	jsonChirpSlice := make([]jsonChirp, 0, len(allChirps))
	for _, chirpStruct := range allChirps {
		jsonChirp := turnChirpToJson(chirpStruct)
		jsonChirpSlice = append(jsonChirpSlice, jsonChirp)
	}
	fmt.Println(jsonChirpSlice)
	data, err := json.Marshal(jsonChirpSlice)
	if err != nil {
		log.Printf("Error marshaling json: %s", err)
		w.WriteHeader(500)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write(data)
}

// get a single chirp.
func (cfg *apiConfig) getSingleChirp(w http.ResponseWriter, req *http.Request) {
	stringChirpID := req.PathValue("chirpID")
	chirpID, err := uuid.Parse(stringChirpID)
	if err != nil {
		respondWithError(w, 404, "Unable to parse chirp ID.")
		return
	}
	chirp, err := cfg.db.GetSingleChirp(req.Context(), chirpID)
	if err != nil {
		respondWithError(w, 404, "Chirp was not found.")
		return
	}
	jsonChirp := turnChirpToJson(chirp)
	respondWithJSON(w, 200, jsonChirp)
}

func (cfg *apiConfig) refreshJWTToken(w http.ResponseWriter, req *http.Request) {
	refreshHeader, err := auth.GetBearerToken(req.Header)
	if err != nil {
		respondWithError(w, 401, "Bearer Token not present.")
		return
	}
	refreshToken, err := cfg.db.GetRefreshToken(req.Context(), refreshHeader)
	if err != nil {
		respondWithError(w, 401, "This token does not exist in the database.")
		return
	}
	now := time.Now()
	if refreshToken.ExpiresAt.Time.Before(now) || refreshToken.ExpiresAt.Time.Equal(now) {
		respondWithError(w, 401, "This token is expired.")
		return
	}
	if !refreshToken.RevokedAt.Time.IsZero() {
		respondWithError(w, 401, "This token has been revoked.")
		return
	}
	givenUser, err := cfg.db.GetSingleUserFromRefreshToken(req.Context(), refreshToken.Token)
	if err != nil {
		respondWithError(w, 401, "User does not exist.")
		return
	}
	jwtToken, err := auth.MakeJWT(givenUser.UserID, cfg.secret, time.Duration(EXPIRETIME)*time.Second)
	jsonToken := struct {
		Token string `json:"token"`
	}{
		Token: jwtToken,
	}
	respondWithJSON(w, 200, jsonToken)
}

func (cfg *apiConfig) revokeToken(w http.ResponseWriter, req *http.Request) {
	refreshHeader, err := auth.GetBearerToken(req.Header)
	if err != nil {
		respondWithError(w, 401, "Bearer Token not present.")
		return
	}
	errRevoke := cfg.db.RevokeToken(req.Context(), refreshHeader)
	if errRevoke != nil {
		respondWithError(w, 401, "This token does not exist in the database.")
		return
	}
	w.WriteHeader(204)
}

func (cfg *apiConfig) authorizeUser(w http.ResponseWriter, req *http.Request) {
	type parameters struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	authToken, err := auth.GetBearerToken(req.Header)
	if err != nil {
		respondWithError(w, 401, "Missing auth token")
		return
	}
	params := parameters{}
	decoder := json.NewDecoder(req.Body)
	err = decoder.Decode(&params)
	if err != nil {
		respondWithError(w, 500, "Something went wrong during decoding")
		return
	}
	hashedPassword, err := auth.HashPassword(params.Password)
	if err != nil {
		respondWithError(w, 403, err.Error())
	}
	userUUID, err := auth.ValidateJWT(authToken, cfg.secret)
	newUserDetails := database.UpdateUserEmailAndPasswordParams{
		Email:          params.Email,
		HashedPassword: hashedPassword,
		ID:             userUUID,
	}
	newUser, err := cfg.db.UpdateUserEmailAndPassword(req.Context(), newUserDetails)
	if err != nil {
		respondWithError(w, 401, err.Error())
		return
	}
	newUser.HashedPassword = "REDACTED"
	jsonUser := turnUserToJson(newUser)
	respondWithJSON(w, 200, jsonUser)

}

// helper function to turn a database chirp into a JSON ready struct for marshaling.
func turnChirpToJson(chirpStruct database.Chirp) jsonChirp {
	jsonChirp := jsonChirp{
		ID:        chirpStruct.ID,
		CreatedAt: chirpStruct.CreatedAt,
		UpdatedAt: chirpStruct.UpdatedAt,
		Body:      chirpStruct.Body,
		User_id:   chirpStruct.UserID,
	}
	return jsonChirp
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
	cfg := &apiConfig{fileserverHits: atomic.Int32{}, db: dbQueries, platform: os.Getenv("PLATFORM"), secret: os.Getenv("SECRET_KEY")}
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
	serveMux.HandleFunc("POST /api/users", cfg.createUserWriter)
	serveMux.HandleFunc("GET /api/chirps", cfg.getAllChirps)
	serveMux.HandleFunc("POST /api/chirps", cfg.createChirpWriter)
	serveMux.HandleFunc("GET /api/chirps/{chirpID}", cfg.getSingleChirp)
	serveMux.HandleFunc("POST /api/login", cfg.loginUser)
	serveMux.HandleFunc("POST /api/refresh", cfg.refreshJWTToken)
	serveMux.HandleFunc("POST /api/revoke", cfg.revokeToken)
	serveMux.HandleFunc("PUT /api/users", cfg.authorizeUser)
	server.ListenAndServe()
}
