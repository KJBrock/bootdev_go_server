package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync/atomic"

	"github.com/KJBrock/bootdev_go_server/internal/database"
	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
)

type apiConfig struct {
	fileserverHits atomic.Int32
	dbQueries      *database.Queries
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

	mux := http.NewServeMux()

	server := http.Server{
		Handler: mux,
		Addr:    ":8080",
	}

	//mux.Handle("/app/", http.StripPrefix("/app/", http.FileServer(http.Dir("."))))
	mux.Handle("/app/", http.StripPrefix("/app/", apiCfg.middlewareMetricsInc(http.FileServer(http.Dir(".")))))
	mux.HandleFunc("GET /api/healthz", healthzHandler)
	mux.HandleFunc("POST /api/validate_chirp", validateChirpHandler)
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
	cfg.fileserverHits.Store(0)

	writer.Header().Set("Content-Type", "text/plain; charset=utf-8")
	writer.WriteHeader(200)
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

func cleanString(dirty string, badWords map[string]interface{}) string {
	words := strings.Split(dirty, " ")
	for i, word := range words {
		_, badWord := badWords[strings.ToLower(word)]
		if badWord {
			words[i] = "****"
		}
	}

	return strings.Join(words, " ")
}

func validateChirpHandler(writer http.ResponseWriter, req *http.Request) {
	type chirpReq struct {
		Chirp string `json:"body"`
	}

	type chirpRespOk struct {
		Cleaned string `json:"cleaned_body"`
	}

	reqParams := chirpReq{}

	decoder := json.NewDecoder(req.Body)
	err := decoder.Decode(&reqParams)
	if err != nil {
		sendErrorOr500(writer, "Invalid request JSON")
		return
	}

	if len(reqParams.Chirp) > 140 {
		sendErrorOr500(writer, "Chirp is too long")
		return
	}

	respParams := chirpRespOk{}

	badWords := map[string]interface{}{
		"kerfuffle": nil,
		"sharbert":  nil,
		"fornax":    nil,
	}

	respParams.Cleaned = cleanString(reqParams.Chirp, badWords)

	data, err := json.Marshal(respParams)
	if err != nil {
		writer.WriteHeader(500)
		return
	}

	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(200)
	writer.Write(data)
}
