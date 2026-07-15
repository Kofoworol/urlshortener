package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

//  Database functions

func createURL(ctx context.Context, pool *pgxpool.Pool, shortCode string, longURL string) error {
	_, err := pool.Exec(ctx,
		"INSERT INTO urls (short_code, long_url) VALUES ($1, $2)",
		shortCode, longURL,
	)
	return err
}

func getURL(ctx context.Context, pool *pgxpool.Pool, shortCode string) (string, error) {
	var longURL string
	err := pool.QueryRow(ctx,
		"SELECT long_url FROM urls WHERE short_code = $1",
		shortCode,
	).Scan(&longURL)
	return longURL, err
}
func incrementClickCount(ctx context.Context, pool *pgxpool.Pool, shortCode string) error {
	_, err := pool.Exec(ctx,
		"UPDATE urls SET click_count = click_count + 1 WHERE short_code = $1",
		shortCode,
	)
	return err
}

//the  Short code generator

const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

func generateShortCode(length int) string {
	rand.Seed(time.Now().UnixNano())
	b := make([]byte, length)
	for i := range b {
		b[i] = charset[rand.Intn(len(charset))]
	}
	return string(b)
}

// HTTP handler

type ShortenRequest struct {
	LongURL string `json:"long_url"`
}

type ShortenResponse struct {
	ShortCode string `json:"short_code"`
	ShortURL  string `json:"short_url"`
}

func shortenHandler(pool *pgxpool.Pool, rdb *redis.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Only POST allowed", http.StatusMethodNotAllowed)
			return
		}

		var req ShortenRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}

		if req.LongURL == "" {
			http.Error(w, "long_url is required", http.StatusBadRequest)
			return
		}

		if !isValidURL(req.LongURL) {
			http.Error(w, "long_url must be a valid http/https URL", http.StatusBadRequest)
			return
		}

		shortCode := generateShortCode(6)

		err := createURL(r.Context(), pool, shortCode, req.LongURL)
		if err != nil {
			http.Error(w, "Failed to save URL", http.StatusInternalServerError)
			return
		}

		err = rdb.Set(r.Context(), shortCode, req.LongURL, 0).Err()
		if err != nil {
			log.Println("Warning: failed to cache in Redis:", err)
		}

		resp := ShortenResponse{
			ShortCode: shortCode,
			ShortURL:  "http://localhost:8080/" + shortCode,
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}
}

type StatsResponse struct {
	ShortCode  string `json:"short_code"`
	LongURL    string `json:"long_url"`
	ClickCount int    `json:"click_count"`
}

func statsHandler(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		shortCode := r.URL.Path[len("/stats/"):]

		if shortCode == "" {
			http.Error(w, "No short code provided", http.StatusBadRequest)
			return
		}

		var stats StatsResponse
		err := pool.QueryRow(r.Context(),
			"SELECT short_code, long_url, click_count FROM urls WHERE short_code = $1",
			shortCode,
		).Scan(&stats.ShortCode, &stats.LongURL, &stats.ClickCount)

		if err != nil {
			http.Error(w, "Short URL not found", http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(stats)
	}
}

func redirectHandler(pool *pgxpool.Pool, rdb *redis.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		shortCode := r.URL.Path[1:]

		if shortCode == "" {
			http.Error(w, "No short code provided", http.StatusBadRequest)
			return
		}

		// ---------- PATH 1: Cache HIT ----------
		longURL, err := rdb.Get(r.Context(), shortCode).Result()
		if err == nil {
			fmt.Println("Cache HIT for", shortCode)
			go incrementClickCount(context.Background(), pool, shortCode)
			http.Redirect(w, r, longURL, http.StatusFound)
			return
		}

		fmt.Println("Cache MISS for", shortCode, "- checking Postgres")

		// ---------- PATH 2: Cache MISS ----------
		longURL, err = getURL(r.Context(), pool, shortCode)
		if err != nil {
			http.Error(w, "Short URL not found", http.StatusNotFound)
			return
		}

		rdb.Set(r.Context(), shortCode, longURL, 0)
		go incrementClickCount(context.Background(), pool, shortCode) // ADDED HERE TOO

		http.Redirect(w, r, longURL, http.StatusFound)
	}
}
func isValidURL(s string) bool {
	u, err := url.ParseRequestURI(s)
	if err != nil {
		return false
	}
	return strings.HasPrefix(u.Scheme, "http")
}

func main() {
	ctx := context.Background()

	//  Postgres connection setup
	dbURL := os.Getenv("DATABASE_URL")

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		log.Fatal("Unable to connect to database:", err)
	}
	defer pool.Close()

	fmt.Println("Connected to Postgres successfully!")

	// Redis connection setup
	rdb := redis.NewClient(&redis.Options{
		Addr: os.Getenv("REDIS_ADDR"),
	})
	_, err = rdb.Ping(ctx).Result()
	if err != nil {
		log.Fatal("Unable to connect to Redis:", err)
	}
	fmt.Println("Connected to Redis successfully!")

	// Routes
	http.HandleFunc("/stats/", statsHandler(pool))
	http.HandleFunc("/shorten", shortenHandler(pool, rdb))
	http.HandleFunc("/", redirectHandler(pool, rdb))

	fmt.Println("Server running on http://localhost:8080")
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Fatal(http.ListenAndServe(":"+port, nil))
}
