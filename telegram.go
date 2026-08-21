package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

type monitorAlertData struct {
	Name, Type, Target, Group string
}

func monitorAlert(db *sql.DB, id int, event string, latency int64, message, checkedAt, startedAt string) (string, error) {
	var data monitorAlertData
	if err := db.QueryRow("SELECT m.name,m.type,m.target,COALESCE(g.name,'') FROM monitors m LEFT JOIN groups g ON g.id=m.group_id WHERE m.id=?", id).Scan(&data.Name, &data.Type, &data.Target, &data.Group); err != nil {
		return "", err
	}
	return formatMonitorAlert(data, event, latency, message, checkedAt, startedAt), nil
}

func formatMonitorAlert(data monitorAlertData, event string, latency int64, message, checkedAt, startedAt string) string {
	name := html.EscapeString(data.Name)
	target := html.EscapeString(sanitizeAlertTarget(data.Target))
	typ := html.EscapeString(strings.ToUpper(data.Type))
	group := html.EscapeString(data.Group)
	timeText := html.EscapeString(humanTime(checkedAt))
	groupLine := ""
	if group != "" {
		groupLine = fmt.Sprintf("\n<b>Group:</b> %s", group)
	}
	if event == "recovered" {
		downtime := "unknown"
		if startedAt != "" {
			if started, err := time.Parse(time.RFC3339, startedAt); err == nil {
				if ended, endErr := time.Parse(time.RFC3339, checkedAt); endErr == nil {
					downtime = humanDuration(ended.Sub(started))
				}
			}
		}
		return fmt.Sprintf("­ƒƒó <b>MONITOR RECOVERED</b>\n\n<b>%s</b>\n<b>Type:</b> %s\n<b>Target:</b> <code>%s</code>%s\n<b>Downtime:</b> %s\n<b>Latest latency:</b> %d ms\n<b>Recovered:</b> %s", name, typ, target, groupLine, downtime, latency, timeText)
	}
	return fmt.Sprintf("­ƒö┤ <b>MONITOR DOWN</b>\n\n<b>%s</b>\n<b>Type:</b> %s\n<b>Target:</b> <code>%s</code>%s\n<b>Error:</b> %s\n<b>Detected:</b> %s", name, typ, target, groupLine, html.EscapeString(message), timeText)
}

func sanitizeAlertTarget(target string) string {
	u, err := url.Parse(target)
	if err != nil || u.Scheme == "" {
		return target
	}
	u.User = nil
	if u.RawQuery != "" {
		u.RawQuery = "redacted"
	}
	return u.String()
}

func humanDuration(duration time.Duration) string {
	if duration < 0 {
		return "unknown"
	}
	seconds := int64(duration.Round(time.Second) / time.Second)
	days, seconds := seconds/86400, seconds%86400
	hours, seconds := seconds/3600, seconds%3600
	minutes, seconds := seconds/60, seconds%60
	parts := []string{}
	if days > 0 {
		parts = append(parts, fmt.Sprintf("%dd", days))
	}
	if hours > 0 {
		parts = append(parts, fmt.Sprintf("%dh", hours))
	}
	if minutes > 0 {
		parts = append(parts, fmt.Sprintf("%dm", minutes))
	}
	if seconds > 0 || len(parts) == 0 {
		parts = append(parts, fmt.Sprintf("%ds", seconds))
	}
	return strings.Join(parts, " ")
}

func telegramAlert(db *sql.DB, message string) error {
	var token, chatID string
	db.QueryRow("SELECT value FROM settings WHERE key='telegram_token'").Scan(&token)
	db.QueryRow("SELECT value FROM settings WHERE key='telegram_chat_id'").Scan(&chatID)
	if token == "" {
		token = os.Getenv("TELEGRAM_BOT_TOKEN")
	}
	if chatID == "" {
		chatID = os.Getenv("TELEGRAM_CHAT_ID")
	}
	if token == "" || chatID == "" {
		return errors.New("Telegram is not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	body := strings.NewReader(url.Values{"chat_id": {chatID}, "text": {message}, "parse_mode": {"HTML"}}.Encode())
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.telegram.org/bot"+token+"/sendMessage", body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	responseBody, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return readErr
	}
	if resp.StatusCode >= 300 {
		return fmt.Errorf("Telegram returned %s", resp.Status)
	}
	var result struct {
		OK          bool   `json:"ok"`
		Description string `json:"description"`
	}
	if err := json.Unmarshal(responseBody, &result); err == nil && !result.OK {
		return errors.New(result.Description)
	}
	return nil
}
