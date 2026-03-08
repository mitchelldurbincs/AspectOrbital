package main

const (
	defaultHTTPAddr = "127.0.0.1:8080"
	pingCommandName = "ping"
)

var allowedSeverities = map[string]struct{}{
	"info":     {},
	"warning":  {},
	"critical": {},
}
