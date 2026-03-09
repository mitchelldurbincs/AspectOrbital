package accountability

import (
	"database/sql"
	"errors"
	"time"
)

type Service struct {
	db           *sql.DB
	now          func() time.Time
	pollInterval time.Duration
	expiryGrace  time.Duration
}

func NewService(db *sql.DB, pollInterval, expiryGrace time.Duration) (*Service, error) {
	if db == nil {
		return nil, errors.New("db is required")
	}
	if pollInterval <= 0 {
		pollInterval = 45 * time.Second
	}
	if expiryGrace < 0 {
		expiryGrace = 0
	}
	return &Service{db: db, now: func() time.Time { return time.Now().UTC() }, pollInterval: pollInterval, expiryGrace: expiryGrace}, nil
}
