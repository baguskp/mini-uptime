package main

import "testing"

func TestValidMonitor(t *testing.T) {
	if !validMonitor("http", "site", "https://example.com", 10) { t.Fatal("valid monitor rejected") }
	if validMonitor("bad", "site", "target", 10) { t.Fatal("invalid type accepted") }
	if validMonitor("http", "", "target", 10) { t.Fatal("empty name accepted") }
	if validMonitor("http", "site", "target", 9) { t.Fatal("short interval accepted") }
}
