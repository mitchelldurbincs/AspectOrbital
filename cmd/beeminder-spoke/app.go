package main

import (
	"log"
	"time"

	"personal-infrastructure/pkg/beeminder"
	"personal-infrastructure/pkg/hubnotify"
)

type spokeApp struct {
	cfg       config
	log       *log.Logger
	beeminder *beeminder.Client
	hub       *hubnotify.Client
	engine    *reminderEngine
	location  *time.Location
}
