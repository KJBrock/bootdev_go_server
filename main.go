package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync/atomic"
	"time"

	"github.com/KJBrock/bootdev_go_server/internal/auth"
	"github.com/KJBrock/bootdev_go_server/internal/database"
	"github.com/google/uuid"
	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
)

type apiConfig struct {
	fileserverHits atomic.Int32
	dbQueries      *database.Queries
	platform       string
	jwtSecret      string
}

func main() {
	apiCfg := apiConfig{}

	godotenv.Load()
	dbURL := os.Getenv("DB_URL")
	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		log.Fatal("Failed to connect to database\n")
	}

	apiCfg.dbQueries = database.New(db)
	apiCfg.platform = os.Getenv("PLATFORM")
	apiCfg.jwtSecret = os.Getenv("JWT_SECRET")

	mux := http.NewServeMux()

	server := http.Server{
		Handler: mux,
		Addr:    ":8080",
	}

	//mux.Handle("/app/", http.StripPrefix("/app/", http.FileServer(http.Dir("."))))
	mux.Handle("/app/", http.StripPrefix("/app/", apiCfg.middlewareMetricsInc(http.FileServer(http.Dir(".")))))
	mux.HandleFunc("GET /api/healthz", healthzHandler)
	mux.HandleFunc("POST /api/users", apiCfg.createUserHandler)
	mux.HandleFunc("POST /api/login", apiCfg.loginUserHandler)
	mux.HandleFunc("POST /api/refresh", apiCfg.refreshTokenHandler)
	mux.HandleFunc("POST /api/revoke", apiCfg.revokeRefreshHandler)
	mux.HandleFunc("POST /api/chirps", apiCfg.createChirpHandler)
	mux.HandleFunc("GET /api/chirps", apiCfg.queryChirpsHandler)
	mux.HandleFunc("GET /api/chirps/{chirpID}", apiCfg.queryOneChirpHandler)
	mux.HandleFunc("GET /admin/metrics", apiCfg.getMetricsHandler)
	mux.HandleFunc("POST /admin/reset", apiCfg.resetMetricsHandler)
	server.ListenAndServe()
}

// Middleware
func (cfg *apiConfig) middlewareMetricsInc(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, req *http.Request) {
		cfg.fileserverHits.Add(1)
		next.ServeHTTP(writer, req)
	})
}

// Handlers
func (cfg *apiConfig) getMetricsHandler(writer http.ResponseWriter, req *http.Request) {
	writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	writer.WriteHeader(200)

	metricsTemplate := `<html>
  <body>
    <h1>Welcome, Chirpy Admin</h1>
    <p>Chirpy has been visited %d times!</p>
  </body>
</html>
`
	message := fmt.Sprintf(metricsTemplate, cfg.fileserverHits.Load())
	body := []byte(message)
	_, err := writer.Write(body)
	if err != nil {
		fmt.Printf("%v\n", err)
	}
}

func (cfg *apiConfig) resetMetricsHandler(writer http.ResponseWriter, req *http.Request) {
	if cfg.platform != "dev" {
		writer.WriteHeader(503)
		return
	}

	cfg.dbQueries.ClearUsers(req.Context())

	cfg.fileserverHits.Store(0)

	writer.Header().Set("Content-Type", "text/plain; charset=utf-8")
	writer.WriteHeader(200)
}

type userAuth struct {
	Password string `json:"password"`
	Email    string `json:"email"`
}

type userInfo struct {
	ID           string `json:"id"`
	CreatedAt    string `json:"created_at"`
	UpdatedAt    string `json:"updated_at"`
	Email        string `json:"email"`
	Token        string `json:"token"`
	RefreshToken string `json:"refresh_token"`
}

func (cfg *apiConfig) loginUserHandler(writer http.ResponseWriter, req *http.Request) {

	userParams := userAuth{}

	decoder := json.NewDecoder(req.Body)
	err := decoder.Decode(&userParams)
	if err != nil {
		sendErrorOr500(writer, "Invalid request JSON")
		return
	}

	user, err := cfg.dbQueries.GetUserByEmail(req.Context(), userParams.Email)
	if err != nil {
		writer.WriteHeader(http.StatusUnauthorized)
		return
	}

	match, err := auth.CheckPasswordHash(userParams.Password, user.HashedPassword)
	if err != nil || !match {
		writer.WriteHeader(http.StatusUnauthorized)
		return
	}

	expiration := time.Duration(3600) * time.Second
	tokenString, err := auth.MakeJWT(user.ID, cfg.jwtSecret, expiration)
	if err != nil {
		writer.WriteHeader(http.StatusInternalServerError)
		return
	}

	refreshToken := auth.MakeRefreshToken()
	t := time.Now()
	cfg.dbQueries.AddRefreshToken(req.Context(),
		database.AddRefreshTokenParams{
			Token:     refreshToken,
			CreatedAt: t,
			UpdatedAt: t,
			UserID:    user.ID,
			ExpiresAt: t.Add(60 * 24 * time.Hour),
		})

	userInfo := userInfo{
		ID:           user.ID.String(),
		CreatedAt:    user.CreatedAt.String(),
		UpdatedAt:    user.UpdatedAt.String(),
		Email:        user.Email,
		Token:        tokenString,
		RefreshToken: refreshToken,
	}

	data, err := json.Marshal(userInfo)
	if err != nil {
		writer.WriteHeader(http.StatusInternalServerError)
		return
	}

	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.WriteHeader(http.StatusOK)
	writer.Write(data)
}

func (cfg *apiConfig) refreshTokenHandler(writer http.ResponseWriter, req *http.Request) {
	refreshString, err := auth.GetBearerToken(req.Header)
	if err != nil {
		writer.WriteHeader(http.StatusUnauthorized)
		return
	}

	token, err := cfg.dbQueries.GetRefreshInfo(req.Context(), refreshString)
	if err != nil {
		writer.WriteHeader(http.StatusUnauthorized)
		return
	}

	if time.Now().After(token.ExpiresAt) {
		writer.WriteHeader(http.StatusUnauthorized)
		return
	}

	if token.RevokedAt.Valid { // ?? && time.Now().After(token.RevokedAt.Time)
		writer.WriteHeader(http.StatusUnauthorized)
		return
	}

	type refreshToken struct {
		Token string `json:"token"`
	}

	jwtToken, err := auth.MakeJWT(token.UserID, cfg.jwtSecret, time.Duration(1)*time.Hour)
	if err != nil {
		writer.WriteHeader(http.StatusUnauthorized)
		return
	}

	data, err := json.Marshal(refreshToken{
		Token: jwtToken,
	})

	if err != nil {
		writer.WriteHeader(http.StatusInternalServerError)
		return
	}

	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.WriteHeader(http.StatusOK)
	writer.Write(data)

}

func (cfg *apiConfig) revokeRefreshHandler(writer http.ResponseWriter, req *http.Request) {
	refreshString, err := auth.GetBearerToken(req.Header)
	if err != nil {
		writer.WriteHeader(http.StatusUnauthorized)
		return
	}

	//	token, err := cfg.dbQueries.GetRefreshInfo(req.Context(), refreshString)
	//	if err != nil {
	//		writer.WriteHeader(http.StatusUnauthorized)
	//		return
	//	}

	err = cfg.dbQueries.RevokeRefreshToken(req.Context(), refreshString)
	if err != nil {
		writer.WriteHeader(http.StatusUnauthorized)
		return
	}

	writer.WriteHeader(http.StatusNoContent)
}

func (cfg *apiConfig) createUserHandler(writer http.ResponseWriter, req *http.Request) {
	userParams := userAuth{}

	decoder := json.NewDecoder(req.Body)
	err := decoder.Decode(&userParams)
	if err != nil {
		sendErrorOr500(writer, "Invalid request JSON")
		return
	}

	hashedPassword, err := auth.HashPassword(userParams.Password)

	t := time.Now()
	user, err := cfg.dbQueries.CreateUser(req.Context(),
		database.CreateUserParams{
			ID:             uuid.New(),
			CreatedAt:      t,
			UpdatedAt:      t,
			Email:          userParams.Email,
			HashedPassword: hashedPassword,
		})

	if err != nil {
		writer.WriteHeader(500)
		return
	}

	userInfo := userInfo{
		ID:        user.ID.String(),
		CreatedAt: user.CreatedAt.String(),
		UpdatedAt: user.UpdatedAt.String(),
		Email:     user.Email,
	}

	data, err := json.Marshal(userInfo)
	if err != nil {
		writer.WriteHeader(http.StatusInternalServerError)
		return
	}

	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.WriteHeader(http.StatusCreated)
	writer.Write(data)
}

func healthzHandler(writer http.ResponseWriter, req *http.Request) {
	writer.Header().Set("Content-Type", "text/plain; charset=utf-8")
	writer.WriteHeader(200)

	body := []byte("OK")
	_, err := writer.Write(body)
	if err != nil {
		fmt.Printf("%v\n", err)
	}
}

func sendErrorOr500(writer http.ResponseWriter, message string) {
	type chirpRespError struct {
		Error string `json:"error"`
	}

	respParams := chirpRespError{}
	respParams.Error = message
	data, err := json.Marshal(respParams)
	if err != nil {
		writer.WriteHeader(500)
		return
	}

	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(400)
	writer.Write(data)

}

type chirpData struct {
	ID        string `json:"id"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
	Body      string `json:"body"`
	UserID    string `json:"user_id"`
}

func (cfg *apiConfig) queryChirpsHandler(writer http.ResponseWriter, req *http.Request) {

	chirps, err := cfg.dbQueries.GetAllChirps(req.Context())
	if err != nil {
		sendErrorOr500(writer, "Server error, failed to get chirps")
		return
	}

	chirpList := []chirpData{}
	for _, chirp := range chirps {
		chirpList = append(chirpList, chirpData{
			ID:        chirp.ID.String(),
			CreatedAt: chirp.CreatedAt.String(),
			UpdatedAt: chirp.UpdatedAt.String(),
			Body:      chirp.Body,
			UserID:    chirp.UserID.String(),
		})
	}

	data, err := json.Marshal(chirpList)
	if err != nil {
		writer.WriteHeader(500)
		return
	}

	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(200)
	writer.Write(data)

}

func (cfg *apiConfig) queryOneChirpHandler(writer http.ResponseWriter, req *http.Request) {

	fmt.Printf("\n")
	chirpIDString := req.PathValue("chirpID")
	if len(chirpIDString) == 0 {
		fmt.Printf("\n")
		writer.WriteHeader(400)
		return
	}

	chirpID, err := uuid.Parse(chirpIDString)
	if err != nil {
		fmt.Printf("Error parsing UUID\n")
		writer.WriteHeader(400)
		return
	}

	chirp, err := cfg.dbQueries.GetChirp(req.Context(), chirpID)
	if err != nil {
		fmt.Printf("Error on DB query\n")
		writer.WriteHeader(404)
		return
	}

	cd := chirpData{
		ID:        chirp.ID.String(),
		CreatedAt: chirp.CreatedAt.String(),
		UpdatedAt: chirp.UpdatedAt.String(),
		Body:      chirp.Body,
		UserID:    chirp.UserID.String(),
	}
	data, err := json.Marshal(cd)
	if err != nil {
		fmt.Printf("JSON marshal failed\n")
		writer.WriteHeader(500)
		return
	}

	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(200)
	writer.Write(data)

}

func (cfg *apiConfig) createChirpHandler(writer http.ResponseWriter, req *http.Request) {
	type chirpReq struct {
		Chirp string `json:"body"`
	}

	authorizationString, err := auth.GetBearerToken(req.Header)
	if err != nil {
		fmt.Printf("Can't find authorization header.\n")
		writer.WriteHeader(http.StatusUnauthorized)
		return
	}

	authUUID, err := auth.ValidateJWT(authorizationString, cfg.jwtSecret)
	if err != nil {
		fmt.Printf("JWT validation failed: {%v}\n", authorizationString)
		writer.WriteHeader(http.StatusUnauthorized)
		return
	}

	reqParams := chirpReq{}

	decoder := json.NewDecoder(req.Body)
	err = decoder.Decode(&reqParams)
	if err != nil {
		fmt.Printf("Can't decode body.\n")
		sendErrorOr500(writer, "Invalid request JSON")
		return
	}

	user, err := cfg.dbQueries.GetUser(req.Context(), authUUID)
	if err != nil {
		sendErrorOr500(writer, "User not found")
		return
	}

	if len(reqParams.Chirp) > 140 {
		fmt.Printf("Chirp is too long.\n")
		sendErrorOr500(writer, "Chirp is too long")
		return
	}

	t := time.Now()
	chirp, err := cfg.dbQueries.CreateChirp(req.Context(),
		database.CreateChirpParams{
			ID:        uuid.New(),
			CreatedAt: t,
			UpdatedAt: t,
			Body:      reqParams.Chirp,
			UserID:    user.ID,
		})
	if err != nil {
		fmt.Printf("Failed to create chirp in database.\n")

		sendErrorOr500(writer, "Server error, failed to create chirp")
		return
	}

	respParams := chirpData{
		ID:        chirp.ID.String(),
		CreatedAt: chirp.CreatedAt.String(),
		UpdatedAt: chirp.UpdatedAt.String(),
		Body:      chirp.Body,
		UserID:    chirp.UserID.String(),
	}

	data, err := json.Marshal(respParams)
	if err != nil {
		fmt.Printf("Failed to marshal response\n")

		writer.WriteHeader(500)
		return
	}

	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(201)
	writer.Write(data)
}
