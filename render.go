package main

import (
	"bytes"
	"html/template"
	"io"
	"log"
	"net/http"
	"time"
)

func humanTime(value string) string {
	t, e := time.Parse(time.RFC3339, value)
	if e != nil {
		return value
	}
	return t.In(currentLocation()).Format("02 Jan, 15:04")
}

func render(w http.ResponseWriter, name string, data any) {
	t, err := template.ParseFS(assets, "web/templates/"+name)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	if values, ok := data.(map[string]any); ok {
		values["Active"] = navActive(name)
	}
	var output bytes.Buffer
	if err := t.Execute(&output, data); err != nil {
		log.Printf("render %s: %v", name, err)
		return
	}
	_, _ = io.WriteString(w, output.String())
}

// navActive memetakan nama template ke path halaman aktif untuk aria-current di nav.
func navActive(name string) string {
	switch name {
	case "dashboard.html":
		return "/dashboard"
	case "monitors.html", "monitor-form.html", "monitor-edit.html", "monitor-detail.html":
		return "/monitors"
	case "agents.html", "agent-form.html", "agent-detail.html":
		return "/agents"
	case "groups.html":
		return "/groups"
	case "incidents.html":
		return "/incidents"
	case "settings.html":
		return "/settings"
	case "display.html", "display-pin.html":
		return "/display"
	}
	return ""
}
