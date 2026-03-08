package main

import (
	"log"
	"time"

	"personal-infrastructure/pkg/beeminder"
)

type spokeApp struct {
	cfg       config
	log       *log.Logger
	beeminder *beeminder.Client
	hub       *hubClient
	engine    *reminderEngine
	location  *time.Location
}
