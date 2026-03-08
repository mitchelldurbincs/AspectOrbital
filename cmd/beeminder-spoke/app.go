package main

import (
	"log"
	"time"
)

type spokeApp struct {
	cfg       config
	log       *log.Logger
	beeminder *beeminderClient
	hub       *hubClient
	engine    *reminderEngine
	location  *time.Location
}
