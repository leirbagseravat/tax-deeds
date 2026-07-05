// Package dto defines request/response objects for the HTTP API.
package dto

import "time"

// UploadResponse is returned by POST /api/v1/documents.
type UploadResponse struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

// DocumentResponse is returned by GET /api/v1/documents/{id}.
type DocumentResponse struct {
	ID           string    `json:"id"`
	Filename     string    `json:"filename"`
	Status       string    `json:"status"`
	PageCount    *int      `json:"page_count,omitempty"`
	FailedStage  *string   `json:"failed_stage,omitempty"`
	ErrorMessage *string   `json:"error,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}
