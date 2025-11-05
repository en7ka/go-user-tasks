package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/golang-jwt/jwt/v5"
	_ "github.com/jackc/pgx/v5/stdlib"
)

type App struct {
	DB         *sql.DB
	JWTSecret  []byte
	RefBonusToReferrer int
	RefBonusToReferred int
}

type User struct {
	ID         int64      `json:"id"`
	Username   string     `json:"username"`
	Points     int64      `json:"points"`
	ReferrerID *int64     `json:"referrer_id,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
}

type Task struct {
	Code   string `json:"code"`
	Title  string `json:"title"`
	Points int64  `json:"points"`
}

type CompleteTaskReq struct {
	Task string `json:"task"`
}

type ReferrerReq struct {
	ReferrerID int64 `json:"referrer_id"`
}

func main() {
	dsn := env("DB_DSN", "postgres://app:app@localhost:5432/app?sslmode=disable")
	secret := []byte(env("JWT_SECRET", "dev-secret"))
	port := env("HTTP_PORT", "8080")

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		log.Fatal(err)
	}
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(10)
	db.SetConnMaxLifetime(30 * time.Minute)

	if err := db.Ping(); err != nil {
		log.Fatal("DB ping failed: ", err)
	}

	app := &App{
		DB:        db,
		JWTSecret: secret,
		RefBonusToReferrer: 50,
		RefBonusToReferred: 10,
	}

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(app.AuthMiddleware)

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	r.Route("/users", func(r chi.Router) {
		r.Get("/{id}/status", app.GetUserStatus)
		r.Get("/leaderboard", app.GetLeaderboard)
		r.Post("/{id}/task/complete", app.CompleteTask)
		r.Post("/{id}/referrer", app.SetReferrer)
	})

	addr := ":" + port
	log.Printf("listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, r))
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// ------------------------ AUTH ------------------------

func (a *App) AuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Expect Bearer token
		auth := r.Header.Get("Authorization")
		const prefix = "Bearer "
		if len(auth) <= len(prefix) || auth[:len(prefix)] != prefix {
			http.Error(w, "missing bearer token", http.StatusUnauthorized)
			return
		}
		tokenStr := auth[len(prefix):]

		claims := jwt.MapClaims{}
		token, err := jwt.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (interface{}, error) {
			if t.Method.Alg() != "HS256" {
				return nil, fmt.Errorf("unexpected signing method: %s", t.Method.Alg())
			}
			return a.JWTSecret, nil
		})
		if err != nil || !token.Valid {
			http.Error(w, "invalid token", http.StatusUnauthorized)
			return
		}

		// Optional: enforce path user id == token sub for user-owned routes
		// We store claims in context
		ctx := context.WithValue(r.Context(), ctxKeyClaims{}, claims)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

type ctxKeyClaims struct{}

func getClaims(r *http.Request) jwt.MapClaims {
	v := r.Context().Value(ctxKeyClaims{})
	if v == nil {
		return jwt.MapClaims{}
	}
	return v.(jwt.MapClaims)
}

func subjectUserID(r *http.Request) (int64, error) {
	claims := getClaims(r)
	sub, ok := claims["sub"].(string)
	if !ok {
		// maybe numeric
		if f, ok := claims["sub"].(float64); ok {
			return int64(f), nil
		}
		return 0, errors.New("no sub in token")
	}
	id, err := strconv.ParseInt(sub, 10, 64)
	if err != nil {
		return 0, err
	}
	return id, nil
}

// ------------------------ HANDLERS ------------------------

func (a *App) GetUserStatus(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "bad user id", http.StatusBadRequest)
		return
	}
	// auth: only allow user to read their own status unless "role":"admin"
	if !isAdmin(r) {
		if sub, err := subjectUserID(r); err != nil || sub != id {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
	}

	var u User
	err = a.DB.QueryRowContext(r.Context(), `
		SELECT id, username, points, referrer_id, created_at
		FROM users WHERE id=$1
	`, id).Scan(&u.ID, &u.Username, &u.Points, &u.ReferrerID, &u.CreatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "user not found", http.StatusNotFound)
			return
		}
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}

	// Also return completed tasks
	rows, err := a.DB.QueryContext(r.Context(), `
		SELECT t.code, t.title, t.points, ut.completed_at
		FROM user_tasks ut
		JOIN tasks t ON t.code = ut.task_code
		WHERE ut.user_id=$1
		ORDER BY ut.completed_at DESC
	`, id)
	if err != nil {
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type taskCompleted struct {
		Code        string    `json:"code"`
		Title       string    `json:"title"`
		Points      int64     `json:"points"`
		CompletedAt time.Time `json:"completed_at"`
	}
	var completed []taskCompleted
	for rows.Next() {
		var tc taskCompleted
		if err := rows.Scan(&tc.Code, &tc.Title, &tc.Points, &tc.CompletedAt); err != nil {
			http.Error(w, "server error", http.StatusInternalServerError)
			return
		}
		completed = append(completed, tc)
	}

	resp := map[string]any{
		"user":            u,
		"completed_tasks": completed,
	}
	jsonWrite(w, resp, http.StatusOK)
}

func (a *App) GetLeaderboard(w http.ResponseWriter, r *http.Request) {
	limit := 10
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 100 {
			limit = n
		}
	}
	rows, err := a.DB.QueryContext(r.Context(), `
		SELECT id, username, points FROM users
		ORDER BY points DESC, id ASC
		LIMIT $1
	`, limit)
	if err != nil {
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	type lbItem struct {
		ID       int64  `json:"id"`
		Username string `json:"username"`
		Points   int64  `json:"points"`
		Rank     int    `json:"rank"`
	}
	var items []lbItem
	rank := 0
	for rows.Next() {
		var it lbItem
		if err := rows.Scan(&it.ID, &it.Username, &it.Points); err != nil {
			http.Error(w, "server error", http.StatusInternalServerError)
			return
		}
		rank++
		it.Rank = rank
		items = append(items, it)
	}
	jsonWrite(w, map[string]any{"leaderboard": items}, http.StatusOK)
}

func (a *App) CompleteTask(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "bad user id", http.StatusBadRequest)
		return
	}
	if !isAdmin(r) {
		if sub, err := subjectUserID(r); err != nil || sub != id {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
	}

	var req CompleteTaskReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Task == "" {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}

	tx, err := a.DB.BeginTx(r.Context(), &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	// Check task exists
	var taskPoints int64
	err = tx.QueryRowContext(r.Context(), `SELECT points FROM tasks WHERE code=$1`, req.Task).Scan(&taskPoints)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "unknown task", http.StatusBadRequest)
			return
		}
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}

	// Insert into user_tasks if not exists
	_, err = tx.ExecContext(r.Context(), `
		INSERT INTO user_tasks (user_id, task_code, completed_at)
		VALUES ($1, $2, now())
		ON CONFLICT (user_id, task_code) DO NOTHING
	`, id, req.Task)
	if err != nil {
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}

	// Check if actually inserted (award only once)
	var cnt int
	if err := tx.QueryRowContext(r.Context(), `
		SELECT COUNT(*) FROM user_tasks WHERE user_id=$1 AND task_code=$2
	`, id, req.Task).Scan(&cnt); err != nil {
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}
	if cnt == 0 {
		jsonWrite(w, map[string]any{"status": "already_completed"}, http.StatusOK)
		return
	}

	// Award points
	if _, err := tx.ExecContext(r.Context(), `
		UPDATE users SET points = points + $1 WHERE id=$2
	`, taskPoints, id); err != nil {
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}

	if err := tx.Commit(); err != nil {
		http.Error(w, "commit failed", http.StatusInternalServerError)
		return
	}

	jsonWrite(w, map[string]any{"status": "ok", "awarded": taskPoints}, http.StatusOK)
}

func (a *App) SetReferrer(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "bad user id", http.StatusBadRequest)
		return
	}
	if !isAdmin(r) {
		if sub, err := subjectUserID(r); err != nil || sub != id {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
	}

	var req ReferrerReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ReferrerID == 0 {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	if req.ReferrerID == id {
		http.Error(w, "cannot refer yourself", http.StatusBadRequest)
		return
	}

	tx, err := a.DB.BeginTx(r.Context(), &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	// Ensure user exists and has no referrer yet
	var curRef *int64
	err = tx.QueryRowContext(r.Context(), `SELECT referrer_id FROM users WHERE id=$1`, id).Scan(&curRef)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "user not found", http.StatusNotFound)
			return
		}
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}
	if curRef != nil {
		http.Error(w, "referrer already set", http.StatusConflict)
		return
	}

	// Ensure referrer exists
	var tmp int64
	if err := tx.QueryRowContext(r.Context(), `SELECT id FROM users WHERE id=$1`, req.ReferrerID).Scan(&tmp); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "referrer not found", http.StatusBadRequest)
			return
		}
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}

	// Set referrer
	if _, err := tx.ExecContext(r.Context(), `UPDATE users SET referrer_id=$1 WHERE id=$2`, req.ReferrerID, id); err != nil {
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}

	// Award bonuses
	if _, err := tx.ExecContext(r.Context(), `UPDATE users SET points = points + $1 WHERE id=$2`, a.RefBonusToReferred, id); err != nil {
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}
	if _, err := tx.ExecContext(r.Context(), `UPDATE users SET points = points + $1 WHERE id=$2`, a.RefBonusToReferrer, req.ReferrerID); err != nil {
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}

	if _, err := tx.ExecContext(r.Context(), `
		INSERT INTO referrals (referrer_id, referred_id, bonus_referrer, bonus_referred, created_at)
		VALUES ($1, $2, $3, $4, now())
	`, req.ReferrerID, id, a.RefBonusToReferrer, a.RefBonusToReferred); err != nil {
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}

	if err := tx.Commit(); err != nil {
		http.Error(w, "commit failed", http.StatusInternalServerError)
		return
	}

	jsonWrite(w, map[string]any{
		"status":            "ok",
		"bonus_referred":    a.RefBonusToReferred,
		"bonus_to_referrer": a.RefBonusToReferrer,
	}, http.StatusOK)
}

func jsonWrite(w http.ResponseWriter, v any, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

func isAdmin(r *http.Request) bool {
	claims := getClaims(r)
	role, _ := claims["role"].(string)
	return role == "admin"
}
