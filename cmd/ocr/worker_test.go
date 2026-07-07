package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"mortgage/internal/ocrengine"
)

type upsert struct {
	page   int
	text   string
	engine string
}

type fakeStore struct {
	mu        sync.Mutex
	upserts   []upsert
	upsertErr error
	doneDocs  []string
	failMsgs  []string
}

func (f *fakeStore) UpsertPage(ctx context.Context, documentID string, page int, text string, confidence *float64, engine string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.upsertErr != nil {
		return f.upsertErr
	}
	f.upserts = append(f.upserts, upsert{page: page, text: text, engine: engine})
	return nil
}

func (f *fakeStore) MarkOCRDone(ctx context.Context, documentID string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.doneDocs = append(f.doneDocs, documentID)
	return true, nil
}

func (f *fakeStore) MarkFailed(ctx context.Context, documentID, message string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failMsgs = append(f.failMsgs, message)
	return nil
}

type fakeEngine struct {
	needsImage bool
	recognize  func(ctx context.Context, p ocrengine.Page) (ocrengine.Result, error)
}

func (f *fakeEngine) Name() string     { return "fake" }
func (f *fakeEngine) NeedsImage() bool { return f.needsImage }
func (f *fakeEngine) RecognizePage(ctx context.Context, p ocrengine.Page) (ocrengine.Result, error) {
	return f.recognize(ctx, p)
}

type fakeFetcher struct {
	mu    sync.Mutex
	calls int
	err   error
}

func (f *fakeFetcher) Download(ctx context.Context, bucket, object string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	return []byte("png:" + object), f.err
}

func okEngine() *fakeEngine {
	return &fakeEngine{
		needsImage: true,
		recognize: func(ctx context.Context, p ocrengine.Page) (ocrengine.Result, error) {
			return ocrengine.Result{Text: fmt.Sprintf("texto %d", p.Number)}, nil
		},
	}
}

func newTestWorker(st store, fetch imageFetcher, engine ocrengine.Engine) *worker {
	return &worker{
		log:         slog.New(slog.DiscardHandler),
		store:       st,
		fetch:       fetch,
		engine:      engine,
		jobTimeout:  5 * time.Second,
		concurrency: 2,
	}
}

func job(pages int) jobRequest {
	req := jobRequest{DocumentID: "3f6c1a9e-0000-0000-0000-000000000001"}
	for i := 1; i <= pages; i++ {
		req.Pages = append(req.Pages, pageRef{PageNumber: i, GCSBucket: "matriculas", GCSObject: fmt.Sprintf("documents/x/pages/%04d.png", i)})
	}
	return req
}

func TestHandleJobRejectsInvalid(t *testing.T) {
	w := newTestWorker(&fakeStore{}, nil, okEngine())
	for _, body := range []string{"", "{}", `{"document_id":"x"}`, `{"pages":[{"page_number":1}]}`} {
		rec := httptest.NewRecorder()
		w.handleJob(rec, httptest.NewRequest("POST", "/v1/ocr-jobs", strings.NewReader(body)))
		if rec.Code != 400 {
			t.Errorf("body %q: status = %d, want 400", body, rec.Code)
		}
	}
}

func TestHandleJobAccepts(t *testing.T) {
	st := &fakeStore{}
	w := newTestWorker(st, &fakeFetcher{}, okEngine())
	rec := httptest.NewRecorder()
	body := `{"document_id":"doc-1","pages":[{"page_number":1,"gcs_bucket":"b","gcs_object":"o"}]}`
	w.handleJob(rec, httptest.NewRequest("POST", "/v1/ocr-jobs", strings.NewReader(body)))
	if rec.Code != 202 {
		t.Fatalf("status = %d, want 202", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "doc-1") {
		t.Errorf("body = %q, want job_id echo", rec.Body.String())
	}
}

func TestProcessHappyPath(t *testing.T) {
	st := &fakeStore{}
	fetch := &fakeFetcher{}
	w := newTestWorker(st, fetch, okEngine())

	w.process(job(3))

	if len(st.upserts) != 3 {
		t.Fatalf("upserts = %d, want 3", len(st.upserts))
	}
	seen := map[int]bool{}
	for _, u := range st.upserts {
		seen[u.page] = true
		if u.engine != "fake" {
			t.Errorf("engine = %q, want fake", u.engine)
		}
		if u.text != fmt.Sprintf("texto %d", u.page) {
			t.Errorf("page %d text = %q", u.page, u.text)
		}
	}
	for p := 1; p <= 3; p++ {
		if !seen[p] {
			t.Errorf("page %d never upserted", p)
		}
	}
	if len(st.doneDocs) != 1 {
		t.Errorf("MarkOCRDone calls = %d, want 1", len(st.doneDocs))
	}
	if len(st.failMsgs) != 0 {
		t.Errorf("MarkFailed calls = %v, want none", st.failMsgs)
	}
	if fetch.calls != 3 {
		t.Errorf("downloads = %d, want 3", fetch.calls)
	}
}

func TestProcessPageFailureMarksFailed(t *testing.T) {
	st := &fakeStore{}
	engine := &fakeEngine{
		needsImage: true,
		recognize: func(ctx context.Context, p ocrengine.Page) (ocrengine.Result, error) {
			if p.Number == 2 {
				return ocrengine.Result{}, errors.New("model exploded")
			}
			return ocrengine.Result{Text: "ok"}, nil
		},
	}
	w := newTestWorker(st, &fakeFetcher{}, engine)

	w.process(job(3))

	if len(st.doneDocs) != 0 {
		t.Error("MarkOCRDone must not be called on failure")
	}
	if len(st.failMsgs) != 1 {
		t.Fatalf("MarkFailed calls = %d, want 1", len(st.failMsgs))
	}
	if !strings.Contains(st.failMsgs[0], "page 2") || !strings.Contains(st.failMsgs[0], "model exploded") {
		t.Errorf("failure message = %q, want page number and cause", st.failMsgs[0])
	}
}

func TestProcessDownloadFailureMarksFailed(t *testing.T) {
	st := &fakeStore{}
	w := newTestWorker(st, &fakeFetcher{err: errors.New("object missing")}, okEngine())

	w.process(job(1))

	if len(st.failMsgs) != 1 || !strings.Contains(st.failMsgs[0], "object missing") {
		t.Fatalf("failMsgs = %v, want download error", st.failMsgs)
	}
	if len(st.doneDocs) != 0 {
		t.Error("MarkOCRDone must not be called")
	}
}

func TestProcessUpsertErrorLeavesProcessing(t *testing.T) {
	st := &fakeStore{upsertErr: errors.New("permission denied")}
	w := newTestWorker(st, &fakeFetcher{}, okEngine())

	w.process(job(2))

	if len(st.doneDocs) != 0 || len(st.failMsgs) != 0 {
		t.Errorf("done = %v, failed = %v; a store error must flip nothing (janitor re-dispatches)", st.doneDocs, st.failMsgs)
	}
}

func TestProcessStubEngineSkipsDownload(t *testing.T) {
	st := &fakeStore{}
	engine := &fakeEngine{
		needsImage: false,
		recognize: func(ctx context.Context, p ocrengine.Page) (ocrengine.Result, error) {
			if p.PNG != nil {
				t.Errorf("page %d: PNG must be nil when NeedsImage is false", p.Number)
			}
			return ocrengine.Result{Text: "fixture"}, nil
		},
	}
	// fetch nil: any download attempt panics the test.
	w := newTestWorker(st, nil, engine)

	w.process(job(2))

	if len(st.upserts) != 2 || len(st.doneDocs) != 1 {
		t.Errorf("upserts = %d, done = %d; want 2 and 1", len(st.upserts), len(st.doneDocs))
	}
}

func TestProcessBoundsConcurrency(t *testing.T) {
	var mu sync.Mutex
	inFlight, maxInFlight := 0, 0
	engine := &fakeEngine{
		needsImage: true,
		recognize: func(ctx context.Context, p ocrengine.Page) (ocrengine.Result, error) {
			mu.Lock()
			inFlight++
			if inFlight > maxInFlight {
				maxInFlight = inFlight
			}
			mu.Unlock()
			time.Sleep(10 * time.Millisecond)
			mu.Lock()
			inFlight--
			mu.Unlock()
			return ocrengine.Result{Text: "ok"}, nil
		},
	}
	st := &fakeStore{}
	w := newTestWorker(st, &fakeFetcher{}, engine)

	w.process(job(8))

	if len(st.upserts) != 8 || len(st.doneDocs) != 1 {
		t.Errorf("upserts = %d, done = %d; want 8 and 1", len(st.upserts), len(st.doneDocs))
	}
	if maxInFlight > 2 {
		t.Errorf("max in-flight pages = %d, want <= 2", maxInFlight)
	}
}
