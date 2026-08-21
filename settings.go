package main

import (
	"database/sql"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func settingsPage(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var token, chat, mode, timezone string
		db.QueryRow("SELECT value FROM settings WHERE key='telegram_token'").Scan(&token)
		db.QueryRow("SELECT value FROM settings WHERE key='telegram_chat_id'").Scan(&chat)
		db.QueryRow("SELECT value FROM settings WHERE key='display_mode'").Scan(&mode)
		db.QueryRow("SELECT value FROM settings WHERE key='timezone'").Scan(&timezone)
		if mode == "" {
			mode = "disabled"
		}
		if timezone == "" {
			timezone = "UTC"
		}
		render(w, "settings.html", map[string]any{"CSRF": csrfData(w, r)["CSRF"], "TokenSet": token != "", "ChatID": chat, "DisplayMode": mode, "Timezone": timezone, "Timezones": timezoneOptions(), "TelegramSent": r.URL.Query().Get("telegram_sent") == "1", "TelegramError": r.URL.Query().Get("telegram_error")})
	}
}

func settingsSave(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		mode := r.FormValue("display_mode")
		if mode != "" {
			if mode != "disabled" && mode != "public" && mode != "pin" {
				http.Error(w, "invalid display mode", 400)
				return
			}
			db.Exec("INSERT INTO settings(key,value) VALUES('display_mode',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value", mode)
			if mode == "pin" && len(r.FormValue("display_pin")) < 4 {
				http.Error(w, "display PIN minimum 4 characters", 400)
				return
			}
		}
		if timezone := strings.TrimSpace(r.FormValue("timezone")); timezone != "" {
			if _, err := time.LoadLocation(timezone); err != nil {
				http.Error(w, "invalid timezone", 400)
				return
			}
			if _, err := db.Exec("INSERT INTO settings(key,value) VALUES('timezone',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value", timezone); err != nil {
				http.Error(w, "unable to save timezone", 500)
				return
			}
			setLocation(timezone)
		}
		if pin := strings.TrimSpace(r.FormValue("display_pin")); pin != "" {
			hash, e := hashPassword(pin)
			if e != nil {
				http.Error(w, "unable to save display PIN", 500)
				return
			}
			if _, e = db.Exec("INSERT INTO settings(key,value) VALUES('display_pin',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value", hash); e != nil {
				http.Error(w, e.Error(), 500)
				return
			}
		}
		if token := strings.TrimSpace(r.FormValue("token")); token != "" {
			if _, e := db.Exec("INSERT INTO settings(key,value) VALUES('telegram_token',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value", token); e != nil {
				http.Error(w, e.Error(), 500)
				return
			}
		}
		if chat := strings.TrimSpace(r.FormValue("chat_id")); chat != "" {
			if _, e := db.Exec("INSERT INTO settings(key,value) VALUES('telegram_chat_id',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value", chat); e != nil {
				http.Error(w, e.Error(), 500)
				return
			}
		}
		http.Redirect(w, r, "/settings", 303)
	}
}

func settingsTest(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := telegramAlert(db, "MiniUptime test alert"); err != nil {
			http.Redirect(w, r, "/settings?telegram_error="+url.QueryEscape(err.Error()), 303)
			return
		}
		http.Redirect(w, r, "/settings?telegram_sent=1", 303)
	}
}

func timezoneOptions() []string {
	return []string{"UTC", "Asia/Jakarta", "Asia/Singapore", "Asia/Tokyo", "Australia/Sydney", "Europe/London", "Europe/Amsterdam", "America/New_York", "America/Los_Angeles"}
}

func configureLocation(db *sql.DB) {
	var timezone string
	if db.QueryRow("SELECT value FROM settings WHERE key='timezone'").Scan(&timezone) == nil && timezone != "" {
		setLocation(timezone)
	}
}

func setLocation(timezone string) {
	location, err := time.LoadLocation(timezone)
	if err != nil {
		return
	}
	appLocation.Lock()
	appLocation.value = location
	appLocation.Unlock()
}

func currentLocation() *time.Location {
	appLocation.RLock()
	defer appLocation.RUnlock()
	return appLocation.value
}

func currentLocationTime() time.Time {
	return time.Now().In(currentLocation())
}

func incidentEnded(value string) string {
	if value == "" {
		return "Ongoing"
	}
	return humanTime(value)
}
