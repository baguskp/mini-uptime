package main

import (
	"crypto/rand"
	"database/sql"
	"fmt"
	"golang.org/x/crypto/argon2"
	"log"
	"net/http"
	"strings"
	"time"
)

func configured(db *sql.DB) bool {
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM admins").Scan(&n); err != nil {
		log.Printf("check configured: %v", err)
		return true
	}
	return n > 0
}

func setupPage(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if configured(db) {
			http.Redirect(w, r, "/login", 302)
			return
		}
		render(w, "setup.html", csrfData(w, r))
	}
}

func setupSubmit(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if configured(db) {
			http.Error(w, "already configured", 409)
			return
		}
		u, p := strings.TrimSpace(r.FormValue("username")), r.FormValue("password")
		if len(u) < 3 || len(p) < 12 {
			http.Error(w, "username minimum 3 characters; password minimum 12 characters", 400)
			return
		}
		h, _ := hashPassword(p)
		if _, err := db.Exec("INSERT INTO admins(username,password_hash,created_at) VALUES(?,?,?)", u, h, time.Now().UTC().Format(time.RFC3339)); err != nil {
			http.Error(w, "unable to create administrator", 400)
			return
		}
		http.Redirect(w, r, "/login", 303)
	}
}

func loginPage(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !configured(db) {
			http.Redirect(w, r, "/setup", 302)
			return
		}
		render(w, "login.html", csrfData(w, r))
	}
}

func loginSubmit(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		username := strings.TrimSpace(r.FormValue("username"))
		password := r.FormValue("password")
		var h string
		if err := db.QueryRow("SELECT password_hash FROM admins WHERE username=?", username).Scan(&h); err != nil {
			log.Printf("login lookup %q: %v", username, err)
			http.Error(w, "invalid credentials", 401)
			return
		}
		if !checkPassword(password, h) {
			log.Printf("login password mismatch for %q", username)
			http.Error(w, "invalid credentials", 401)
			return
		}
		token := randomToken()
		if _, err := db.Exec("INSERT INTO sessions(token,admin_id,expires_at) SELECT ?,id,? FROM admins WHERE username=?", token, time.Now().Add(24*time.Hour).UTC().Format(time.RFC3339), r.FormValue("username")); err != nil {
			http.Error(w, "unable to create session", 500)
			return
		}
		http.SetCookie(w, &http.Cookie{Name: "session", Value: token, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: r.TLS != nil, MaxAge: 86400})
		http.Redirect(w, r, "/dashboard", 303)
	}
}

func requireAuth(db *sql.DB, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie("session")
		var expiry string
		if err != nil || db.QueryRow("SELECT expires_at FROM sessions WHERE token=?", cookieValue(c, err)).Scan(&expiry) != nil {
			http.Redirect(w, r, "/login", 302)
			return
		}
		if t, e := time.Parse(time.RFC3339, expiry); e != nil || time.Now().After(t) {
			http.Redirect(w, r, "/login", 302)
			return
		}
		next(w, r)
	}
}

func logout(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if c, err := r.Cookie("session"); err == nil {
			if _, err := db.Exec("DELETE FROM sessions WHERE token=?", c.Value); err != nil {
				log.Printf("logout session: %v", err)
			}
		}
		http.SetCookie(w, &http.Cookie{Name: "session", MaxAge: -1, Path: "/", HttpOnly: true})
		http.Redirect(w, r, "/login", 303)
	}
}

func csrfData(w http.ResponseWriter, r *http.Request) map[string]string {
	token := ""
	if c, err := r.Cookie("csrf"); err == nil {
		token = c.Value
	}
	if token == "" {
		token = randomToken()
		http.SetCookie(w, &http.Cookie{Name: "csrf", Value: token, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: r.TLS != nil, MaxAge: 3600})
	}
	return map[string]string{"CSRF": token}
}

func checkCSRF(r *http.Request) bool {
	c, err := r.Cookie("csrf")
	return err == nil && c.Value != "" && c.Value == r.FormValue("csrf")
}

func csrf(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && !checkCSRF(r) {
			http.Error(w, "invalid csrf token", http.StatusForbidden)
			return
		}
		next(w, r)
	}
}

func hashPassword(p string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	return fmt.Sprintf("$argon2id$v=19$m=65536,t=3,p=2$%x$%x", salt, argon2.IDKey([]byte(p), salt, 3, 64*1024, 2, 32)), nil
}

func checkPassword(p, h string) bool {
	var salt, expected []byte
	if _, err := fmt.Sscanf(h, "$argon2id$v=19$m=65536,t=3,p=2$%x$%x", &salt, &expected); err != nil {
		return false
	}
	got := argon2.IDKey([]byte(p), salt, 3, 64*1024, 2, 32)
	return string(got) == string(expected)
}

func randomToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		log.Printf("random token: %v", err)
	}
	return fmt.Sprintf("%x", b)
}

func cookieValue(c *http.Cookie, err error) string {
	if err != nil {
		return ""
	}
	return c.Value
}
