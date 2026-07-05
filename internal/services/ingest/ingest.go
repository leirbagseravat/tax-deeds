// Package ingest orchestrates the upload pipeline: store the original PDF,
// convert pages to images, upload them to object storage, record metadata,
// and hand the document to the OCR service.
package ingest

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"github.com/google/uuid"

	"mortgage/internal/services/pdfconvert"
	"mortgage/internal/store"
)

// Storage is the slice of the GCS client the pipeline needs.
type Storage interface {
	Bucket() string
	Upload(ctx context.Context, object, contentType string, r io.Reader) error
	Download(ctx context.Context, object string) (io.ReadCloser, error)
}

// OCRDispatcher hands a converted document to the OCR service. pageObjects is
// ordered by page number.
type OCRDispatcher interface {
	DispatchOCRJob(ctx context.Context, documentID, bucket string, pageObjects []string) error
}

// Service runs the ingestion pipeline.
type Service struct {
	log        *slog.Logger
	docs       *store.Documents
	ocrResults *store.OCR
	storage    Storage
	converter  pdfconvert.Converter
	dispatcher OCRDispatcher

	wg sync.WaitGroup
}

func New(log *slog.Logger, docs *store.Documents, ocrResults *store.OCR, storage Storage, converter pdfconvert.Converter, dispatcher OCRDispatcher) *Service {
	return &Service{log: log, docs: docs, ocrResults: ocrResults, storage: storage, converter: converter, dispatcher: dispatcher}
}

func pdfObject(id string) string  { return fmt.Sprintf("documents/%s/original.pdf", id) }
func pageObject(id string, n int) string {
	return fmt.Sprintf("documents/%s/pages/%04d.png", id, n)
}

// CreateAndProcess uploads the raw PDF, inserts the document row, and starts
// background processing. It returns as soon as the document is durably
// recorded, so the HTTP handler can answer 202 immediately.
func (s *Service) CreateAndProcess(ctx context.Context, filename string, pdf io.Reader) (store.Document, error) {
	id := uuid.NewString()
	obj := pdfObject(id)
	if err := s.storage.Upload(ctx, obj, "application/pdf", pdf); err != nil {
		return store.Document{}, fmt.Errorf("store original pdf: %w", err)
	}
	doc, err := s.docs.Create(ctx, id, filename, s.storage.Bucket(), obj)
	if err != nil {
		return store.Document{}, fmt.Errorf("create document: %w", err)
	}

	// The pipeline must outlive the HTTP request that triggered it.
	bg := context.WithoutCancel(ctx)
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.Process(bg, doc.ID)
	}()
	return doc, nil
}

// Get returns a document by ID.
func (s *Service) Get(ctx context.Context, id string) (store.Document, error) {
	return s.docs.GetByID(ctx, id)
}

// Process converts the document's PDF into page images and records them. It is
// idempotent — object names and page rows are deterministic — so the janitor
// can re-run it on documents stuck in "processing".
func (s *Service) Process(ctx context.Context, id string) {
	if err := s.process(ctx, id); err != nil {
		s.log.Error("ingest failed", "document_id", id, "error", err)
		if _, ferr := s.docs.MarkFailed(ctx, id, store.StatusProcessing, "ingest", err.Error()); ferr != nil {
			s.log.Error("mark failed", "document_id", id, "error", ferr)
		}
	}
}

func (s *Service) process(ctx context.Context, id string) error {
	moved, err := s.docs.UpdateStatus(ctx, id, store.StatusUploaded, store.StatusProcessing)
	if err != nil {
		return err
	}
	if !moved {
		// Janitor re-runs arrive already in "processing"; anything else means
		// another worker owns the document.
		doc, err := s.docs.GetByID(ctx, id)
		if err != nil {
			return err
		}
		if doc.Status != store.StatusProcessing {
			s.log.Info("skipping ingest, unexpected status", "document_id", id, "status", doc.Status)
			return nil
		}
	}

	tmpDir, err := os.MkdirTemp("", "ingest-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)

	// Always work from the stored PDF: re-runs have no local copy.
	pdfPath := filepath.Join(tmpDir, "original.pdf")
	if err := s.downloadTo(ctx, pdfObject(id), pdfPath); err != nil {
		return fmt.Errorf("fetch pdf: %w", err)
	}

	pagePaths, err := s.converter.Convert(ctx, pdfPath, tmpDir)
	if err != nil {
		return fmt.Errorf("convert pdf: %w", err)
	}

	objects := make([]string, len(pagePaths))
	for i, p := range pagePaths {
		obj := pageObject(id, i+1)
		if err := s.uploadFile(ctx, obj, "image/png", p); err != nil {
			return fmt.Errorf("upload page %d: %w", i+1, err)
		}
		objects[i] = obj
	}
	if err := s.docs.AddPages(ctx, id, objects); err != nil {
		return fmt.Errorf("record pages: %w", err)
	}
	if err := s.docs.SetPageCount(ctx, id, len(objects)); err != nil {
		return fmt.Errorf("record page count: %w", err)
	}

	if err := s.dispatcher.DispatchOCRJob(ctx, id, s.storage.Bucket(), objects); err != nil {
		if _, ferr := s.docs.MarkFailed(ctx, id, store.StatusProcessing, "ocr_dispatch", err.Error()); ferr != nil {
			s.log.Error("mark failed", "document_id", id, "error", ferr)
		}
		s.log.Error("ocr dispatch failed", "document_id", id, "error", err)
		return nil // already marked failed with a more precise stage
	}
	if err := s.docs.SetOCRDispatched(ctx, id); err != nil {
		return fmt.Errorf("record ocr dispatch: %w", err)
	}
	s.log.Info("document ingested and dispatched to ocr", "document_id", id, "pages", len(objects))
	return nil
}

// OCRResults returns the document plus its OCR pages, for the read endpoint.
func (s *Service) OCRResults(ctx context.Context, id string) (store.Document, []store.OCRResult, error) {
	doc, err := s.docs.GetByID(ctx, id)
	if err != nil {
		return store.Document{}, nil, err
	}
	results, err := s.ocrResults.ResultsByDocument(ctx, id)
	if err != nil {
		return store.Document{}, nil, err
	}
	return doc, results, nil
}

func (s *Service) downloadTo(ctx context.Context, object, path string) error {
	r, err := s.storage.Download(ctx, object)
	if err != nil {
		return err
	}
	defer r.Close()
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, r)
	return err
}

func (s *Service) uploadFile(ctx context.Context, object, contentType, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return s.storage.Upload(ctx, object, contentType, f)
}

// Shutdown waits for in-flight background pipelines, or gives up when ctx
// expires (the janitor recovers anything unfinished on next start).
func (s *Service) Shutdown(ctx context.Context) error {
	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
