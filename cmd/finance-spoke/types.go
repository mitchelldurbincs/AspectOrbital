package main

import "time"

type subscriptionCharge struct {
	UniqueKey    string    `json:"uniqueKey"`
	Merchant     string    `json:"merchant"`
	Amount       float64   `json:"amount"`
	OccurredAt   time.Time `json:"occurredAt"`
	AccountLabel string    `json:"accountLabel"`
	Institution  string    `json:"institution"`
	StreamID     string    `json:"streamId,omitempty"`
}

type weeklySummary struct {
	WeekKey     string               `json:"weekKey"`
	WindowStart time.Time            `json:"windowStart"`
	WindowEnd   time.Time            `json:"windowEnd"`
	Charges     []subscriptionCharge `json:"charges"`
	TotalAmount float64              `json:"totalAmount"`
}
