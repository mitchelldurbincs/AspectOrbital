package accountability

import (
	"time"
)

type Status string

const (
	StatusPending  Status = "pending"
	StatusSuccess  Status = "success"
	StatusFailed   Status = "failed"
	StatusCanceled Status = "canceled"
)

type Commitment struct {
	ID            int64     `json:"id"`
	UserID        string    `json:"userId"`
	Task          string    `json:"task"`
	CreatedAt     time.Time `json:"createdAt"`
	Deadline      time.Time `json:"deadline"`
	SnoozedUntil  time.Time `json:"snoozedUntil,omitempty"`
	PolicyPreset  string    `json:"policyPreset,omitempty"`
	PolicyEngine  string    `json:"policyEngine,omitempty"`
	PolicyConfig  string    `json:"policyConfig,omitempty"`
	Status        Status    `json:"status"`
	ProofMetadata string    `json:"proofMetadata,omitempty"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

type AttachmentMetadata struct {
	ID          string `json:"id"`
	Filename    string `json:"filename"`
	URL         string `json:"url"`
	ContentType string `json:"contentType"`
	Size        int    `json:"size"`
}

type ProofSubmission struct {
	Attachment AttachmentMetadata `json:"attachment,omitempty"`
	Text       string             `json:"text,omitempty"`
	Verdict    string             `json:"verdict,omitempty"`
}
