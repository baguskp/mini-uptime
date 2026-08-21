package main

import (
	"bytes"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"strings"
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
	var output bytes.Buffer
	if err := t.Execute(&output, data); err != nil {
		log.Printf("render %s: %v", name, err)
		return
	}
	page := normalizeNavbar(name, output.String())
	if name == "dashboard.html" {
		if values, ok := data.(map[string]any); ok {
			if total, ok := values["AgentTotal"].(int); ok {
				up, _ := values["AgentUp"].(int)
				down, _ := values["AgentDown"].(int)
				card := fmt.Sprintf(`<div class="card metric metric-agents"><span class="muted">Agents</span><h2>%d</h2><span class="metric-sub muted">%d online · %d offline</span></div>`, total, up, down)
				page = strings.Replace(page, `<div class="grid metrics-grid">`, `<div class="grid metrics-grid">`+card, 1)
			}
		}
	}
	_, _ = io.WriteString(w, page)
}

// normalizeNavbar menjaga urutan menu tetap sama walaupun template lama masih inline dan menandai halaman aktif.
func normalizeNavbar(name, page string) string {
	start := strings.Index(page, "<nav>")
	if start < 0 {
		return page
	}
	relativeEnd := strings.Index(page[start:], "</nav>")
	if relativeEnd < 0 {
		return page
	}
	end := start + relativeEnd
	existingNav := page[start:end]
	logout := ""
	if formStart := strings.Index(existingNav, `<form method="post" action="/logout"`); formStart >= 0 {
		if formEnd := strings.Index(existingNav[formStart:], "</form>"); formEnd >= 0 {
			logout = existingNav[formStart : formStart+formEnd+len("</form>")]
		}
	}
	active := ""
	switch name {
	case "dashboard.html":
		active = "/dashboard"
	case "monitors.html", "monitor-form.html", "monitor-edit.html", "monitor-detail.html":
		active = "/monitors"
	case "agents.html", "agent-form.html", "agent-detail.html":
		active = "/agents"
	case "groups.html":
		active = "/groups"
	case "incidents.html":
		active = "/incidents"
	case "settings.html":
		active = "/settings"
	case "display.html", "display-pin.html":
		active = "/display"
	}
	links := []struct{ href, label string }{
		{"/dashboard", "Dashboard"},
		{"/monitors", "Monitors"},
		{"/agents", "Agents"},
		{"/groups", "Groups"},
		{"/incidents", "Incidents"},
		{"/settings", "Settings"},
		{"/display", "Display"},
	}
	var b strings.Builder
	b.WriteString(`<nav><strong>MiniUptime</strong>`)
	for _, l := range links {
		if l.href == active {
			fmt.Fprintf(&b, `<a href="%s" aria-current="page">%s</a>`, l.href, l.label)
		} else {
			fmt.Fprintf(&b, `<a href="%s">%s</a>`, l.href, l.label)
		}
	}
	b.WriteString(logout)
	b.WriteString(`</nav>`)
	return page[:start] + b.String() + page[end+len("</nav>"):]
}
