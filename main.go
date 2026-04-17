package main

import (
	"fmt"
	"net/http"
	"sync/atomic"
)

type apiConfig struct {
	fileserverHits atomic.Int32
}

func main() {
	mux := http.NewServeMux()
	apiCfg := apiConfig{}

	server := http.Server{
		Handler: mux,
		Addr:    ":8080",
	}

	//mux.Handle("/app/", http.StripPrefix("/app/", http.FileServer(http.Dir("."))))
	mux.Handle("/app/", http.StripPrefix("/app/", apiCfg.middlewareMetricsInc(http.FileServer(http.Dir(".")))))
	mux.HandleFunc("/healthz", healthzHandler)
	mux.HandleFunc("/metrics", apiCfg.printMetricsHandler)
	mux.HandleFunc("/reset", apiCfg.resetMetricsHandler)
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
func (cfg *apiConfig) printMetricsHandler(writer http.ResponseWriter, req *http.Request) {
	writer.Header().Set("Content-Type", "text/plain; charset=utf-8")
	writer.WriteHeader(200)

	message := fmt.Sprintf("Hits: %d", cfg.fileserverHits.Load())
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
