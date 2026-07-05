// Package ocr is the HTTP client for the Python OCR service. The full
// contract lives in docs/ocr-contract.md.
package ocr

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// PageRef points the OCR service at one page image in the shared bucket.
type PageRef struct {
	PageNumber int    `json:"page_number"`
	GCSBucket  string `json:"gcs_bucket"`
	GCSObject  string `json:"gcs_object"`
}

// JobRequest is the POST /v1/ocr-jobs payload.
type JobRequest struct {
	DocumentID string    `json:"document_id"`
	Pages      []PageRef `json:"pages"`
}

// Client dispatches OCR jobs with retries.
type Client struct {
	baseURL string
	http    *http.Client
	retries int
}

func New(baseURL string, timeout time.Duration) *Client {
	return &Client{
		baseURL: baseURL,
		http:    &http.Client{Timeout: timeout},
		retries: 3,
	}
}

// DispatchOCRJob sends the job, retrying with exponential backoff. Duplicate
// deliveries are fine: the OCR service upserts its results.
func (c *Client) DispatchOCRJob(ctx context.Context, documentID, bucket string, pageObjects []string) error {
	req := JobRequest{DocumentID: documentID}
	for i, obj := range pageObjects {
		req.Pages = append(req.Pages, PageRef{PageNumber: i + 1, GCSBucket: bucket, GCSObject: obj})
	}
	body, err := json.Marshal(req)
	if err != nil {
		return err
	}

	var lastErr error
	for attempt := 0; attempt < c.retries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(1<<(attempt-1)) * time.Second):
			}
		}
		lastErr = c.post(ctx, body)
		if lastErr == nil {
			return nil
		}
	}
	return fmt.Errorf("dispatch ocr job after %d attempts: %w", c.retries, lastErr)
}

func (c *Client) post(ctx context.Context, body []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/ocr-jobs", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("ocr service answered %s", resp.Status)
	}
	return nil
}
