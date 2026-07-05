package dto

// OCRPage is one page of recognized text.
type OCRPage struct {
	PageNumber int      `json:"page_number"`
	Text       string   `json:"text"`
	Confidence *float32 `json:"confidence,omitempty"`
	Engine     *string  `json:"engine,omitempty"`
}

// OCRResponse is returned by GET /api/v1/documents/{id}/ocr.
type OCRResponse struct {
	DocumentID string    `json:"document_id"`
	Status     string    `json:"status"`
	Pages      []OCRPage `json:"pages"`
}
