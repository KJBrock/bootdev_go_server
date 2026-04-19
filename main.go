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

	"github.com/KJBrock/bootdev_go_server/internal/database"
	"github.com/google/uuid"
	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
)

type apiConfig struct {
	fileserverHits atomic.Int32
	dbQueries      *database.Queries
	platform       string
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

	mux := http.NewServeMux()

	server := http.Server{
		Handler: mux,
		Addr:    ":8080",
	}

	//mux.Handle("/app/", http.StripPrefix("/app/", http.FileServer(http.Dir("."))))
	mux.Handle("/app/", http.StripPrefix("/app/", apiCfg.middlewareMetricsInc(http.FileServer(http.Dir(".")))))
	mux.HandleFunc("GET /api/healthz", healthzHandler)
	mux.HandleFunc("POST /api/users", apiCfg.createUserHandler)
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

func (cfg *apiConfig) createUserHandler(writer http.ResponseWriter, req *http.Request) {
	type createUser struct {
		Email string `json:"email"`
	}

	userParams := createUser{}

	decoder := json.NewDecoder(req.Body)
	err := decoder.Decode(&userParams)
	if err != nil {
		sendErrorOr500(writer, "Invalid request JSON")
		return
	}

	t := time.Now()
	user, err := cfg.dbQueries.CreateUser(req.Context(),
		database.CreateUserParams{
			ID:        uuid.New(),
			CreatedAt: t,
			UpdatedAt: t,
			Email:     userParams.Email,
		})

	if err != nil {
		writer.WriteHeader(500)
		return
	}

	type userResponse struct {
		ID        string `json:"id"`
		CreatedAt string `json:"created_at"`
		UpdatedAt string `json:"updated_at"`
		Email     string `json:"email"`
	}

	userInfo := userResponse{
		ID:        user.ID.String(),
		CreatedAt: user.CreatedAt.String(),
		UpdatedAt: user.UpdatedAt.String(),
		Email:     user.Email,
	}

	data, err := json.Marshal(userInfo)
	if err != nil {
		writer.WriteHeader(500)
		return
	}

	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.WriteHeader(201)
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
		Chirp  string `json:"body"`
		UserID string `json:"user_id"`
	}

	reqParams := chirpReq{}

	decoder := json.NewDecoder(req.Body)
	err := decoder.Decode(&reqParams)
	if err != nil {
		sendErrorOr500(writer, "Invalid request JSON")
		return
	}

	user_uuid, err := uuid.Parse(reqParams.UserID)
	if err != nil {
		sendErrorOr500(writer, "Invalid user id format")
		return
	}

	user, err := cfg.dbQueries.GetUser(req.Context(), user_uuid)
	if err != nil {
		sendErrorOr500(writer, "User not found")
		return
	}

	if len(reqParams.Chirp) > 140 {
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
		writer.WriteHeader(500)
		return
	}

	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(201)
	writer.Write(data)
}
