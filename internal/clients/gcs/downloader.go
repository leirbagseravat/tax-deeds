package gcs

import (
	"context"
	"fmt"
	"io"

	"cloud.google.com/go/storage"
)

// Downloader reads objects from any bucket. The OCR worker uses it because
// job payloads name the bucket per page, unlike Client which is scoped to
// the application bucket at construction.
type Downloader struct {
	client *storage.Client
}

// NewDownloader connects to GCS (or the emulator when STORAGE_EMULATOR_HOST
// is set). It does not ensure buckets exist: the ingest side creates them.
func NewDownloader(ctx context.Context) (*Downloader, error) {
	c, err := storage.NewClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("gcs client: %w", err)
	}
	return &Downloader{client: c}, nil
}

// Download reads the whole object into memory.
func (d *Downloader) Download(ctx context.Context, bucket, object string) ([]byte, error) {
	r, err := d.client.Bucket(bucket).Object(object).NewReader(ctx)
	if err != nil {
		return nil, fmt.Errorf("download %s/%s: %w", bucket, object, err)
	}
	defer r.Close()
	b, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("download %s/%s: %w", bucket, object, err)
	}
	return b, nil
}

// Close releases the underlying client.
func (d *Downloader) Close() error { return d.client.Close() }
