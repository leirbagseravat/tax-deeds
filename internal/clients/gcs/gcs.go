// Package gcs is a thin wrapper around the Google Cloud Storage client scoped
// to the application bucket. In local dev, STORAGE_EMULATOR_HOST points it at
// fake-gcs-server.
package gcs

import (
	"context"
	"errors"
	"fmt"
	"io"

	"cloud.google.com/go/storage"
)

// Client uploads and downloads objects in a single bucket.
type Client struct {
	bucket string
	client *storage.Client
}

// New connects to GCS and ensures the bucket exists (creating it only in
// emulator/dev setups where it is missing; production buckets pre-exist).
func New(ctx context.Context, bucket string) (*Client, error) {
	// JSON reads: fake-gcs-server only serves the default XML-style download
	// path when the request Host matches its -public-host, which breaks
	// clients that reach the emulator under a different hostname (e.g. the
	// dockerized OCR worker via gcs:4443). The JSON API is host-agnostic.
	c, err := storage.NewClient(ctx, storage.WithJSONReads())
	if err != nil {
		return nil, fmt.Errorf("gcs client: %w", err)
	}
	_, err = c.Bucket(bucket).Attrs(ctx)
	if errors.Is(err, storage.ErrBucketNotExist) {
		if err := c.Bucket(bucket).Create(ctx, "local-dev", nil); err != nil {
			return nil, fmt.Errorf("create bucket %q: %w", bucket, err)
		}
	} else if err != nil {
		return nil, fmt.Errorf("bucket %q attrs: %w", bucket, err)
	}
	return &Client{bucket: bucket, client: c}, nil
}

// Bucket returns the bucket name objects are stored in.
func (c *Client) Bucket() string { return c.bucket }

// Upload writes r to the given object, replacing any previous content.
func (c *Client) Upload(ctx context.Context, object, contentType string, r io.Reader) error {
	w := c.client.Bucket(c.bucket).Object(object).NewWriter(ctx)
	w.ContentType = contentType
	if _, err := io.Copy(w, r); err != nil {
		_ = w.Close()
		return fmt.Errorf("upload %s: %w", object, err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("upload %s: %w", object, err)
	}
	return nil
}

// Download opens the given object for reading; the caller must close it.
func (c *Client) Download(ctx context.Context, object string) (io.ReadCloser, error) {
	r, err := c.client.Bucket(c.bucket).Object(object).NewReader(ctx)
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", object, err)
	}
	return r, nil
}

// Close releases the underlying client.
func (c *Client) Close() error { return c.client.Close() }
