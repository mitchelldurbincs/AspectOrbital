package main

import (
	"log"
	"time"

	"personal-infrastructure/pkg/hubnotify"
)

type spokeApp struct {
	cfg       config
	log       *log.Logger
	beeminder *beeminderClient
	hub       *hubnotify.Client
	engine    *reminderEngine
	location  *time.Location
}
