package matriculas

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"mortgage/internal/dto"
	"mortgage/internal/store"
)

func TestOwnershipChain(t *testing.T) {
	atos := []dto.Ato{
		{Numero: "R-1", Kind: "registro", Tipo: "compra_venda"},
		{Numero: "R-2", Kind: "registro", Tipo: "hipoteca"},
		{Numero: "AV-3", Kind: "averbacao", Tipo: "cancelamento"},
		{Numero: "R-4", Kind: "registro", Tipo: "doacao"},
		{Numero: "R-5", Kind: "registro", Tipo: "alienacao_fiduciaria"},
		{Numero: "R-6", Kind: "registro", Tipo: "usucapiao"},
		// An averbação never transfers ownership, even with a transfer-looking tipo.
		{Numero: "AV-7", Kind: "averbacao", Tipo: "compra_venda"},
	}
	chain := OwnershipChain(atos)
	want := []string{"R-1", "R-4", "R-6"}
	if len(chain) != len(want) {
		t.Fatalf("chain length = %d, want %d (%+v)", len(chain), len(want), chain)
	}
	for i, numero := range want {
		if chain[i].Numero != numero {
			t.Errorf("chain[%d] = %s, want %s", i, chain[i].Numero, numero)
		}
	}
}

func TestOwnershipChainEmpty(t *testing.T) {
	if chain := OwnershipChain(nil); len(chain) != 0 {
		t.Fatalf("chain from nil atos: %+v", chain)
	}
}

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	pool, err := pgxpool.New(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func seedDocument(t *testing.T, pool *pgxpool.Pool, status string) string {
	t.Helper()
	ctx := context.Background()
	id := uuid.NewString()
	if _, err := store.NewDocuments(pool).Create(ctx, id, "seed.pdf", "b", "documents/x/original.pdf"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(ctx, `DELETE FROM documents WHERE id = $1::uuid`, id) })
	if _, err := pool.Exec(ctx, `UPDATE documents SET status = $2 WHERE id = $1::uuid`, id, status); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestByDocumentSemantics(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	svc := New(store.NewDocuments(pool), store.NewMatriculas(pool))

	// Unknown id → ErrNotFound.
	if _, err := svc.ByDocument(ctx, uuid.NewString()); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("unknown id: %v", err)
	}

	// Mid-pipeline → NotReadyError carrying the current status.
	var notReady *NotReadyError
	if _, err := svc.ByDocument(ctx, seedDocument(t, pool, store.StatusProcessing)); !errors.As(err, &notReady) {
		t.Fatalf("processing doc: %v", err)
	} else if notReady.Status != store.StatusProcessing {
		t.Fatalf("not-ready status = %s", notReady.Status)
	}

	// Failed → FailedError.
	failedID := seedDocument(t, pool, store.StatusOCRDone)
	if _, err := store.NewDocuments(pool).MarkFailed(ctx, failedID, store.StatusOCRDone, "ocr", "boom"); err != nil {
		t.Fatal(err)
	}
	var failed *FailedError
	if _, err := svc.ByDocument(ctx, failedID); !errors.As(err, &failed) {
		t.Fatalf("failed doc: %v", err)
	} else if failed.Stage != "ocr" || failed.Message != "boom" {
		t.Fatalf("failed error = %+v", failed)
	}

	// Extracted with an aggregate → full response, filters work.
	extractedID := seedDocument(t, pool, store.StatusExtracted)
	agg := dto.ExtractedMatricula{
		Numero: "9.999",
		Atos: []dto.Ato{
			{Numero: "R-1", Kind: "registro", Tipo: "compra_venda"},
			{Numero: "AV-2", Kind: "averbacao", Tipo: "penhora"},
		},
		Proprietarios: []dto.Proprietario{{Nome: "Alice", AtoAquisicao: "R-1"}},
		Onus: []dto.Onus{
			{Tipo: "penhora", Status: "ativo", AtoConstituicao: "AV-2"},
			{Tipo: "hipoteca", Status: "cancelado"},
		},
	}
	if _, err := store.NewMatriculas(pool).SaveAggregate(ctx, extractedID, agg, store.ExtractionMeta{Model: "test", PromptVersion: "v1"}); err != nil {
		t.Fatal(err)
	}
	m, err := svc.ByDocument(ctx, extractedID)
	if err != nil {
		t.Fatal(err)
	}
	if m.Numero != "9.999" || len(m.Atos) != 2 {
		t.Fatalf("aggregate: %+v", m)
	}

	props, err := svc.Proprietarios(ctx, m.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(props.Proprietarios) != 1 || len(props.CadeiaDominial) != 1 || props.CadeiaDominial[0].Numero != "R-1" {
		t.Fatalf("proprietarios: %+v", props)
	}

	atos, err := svc.Atos(ctx, m.ID, "averbacao")
	if err != nil {
		t.Fatal(err)
	}
	if len(atos.Atos) != 1 || atos.Atos[0].Numero != "AV-2" {
		t.Fatalf("filtered atos: %+v", atos.Atos)
	}

	onus, err := svc.Onus(ctx, m.ID, "ativo")
	if err != nil {
		t.Fatal(err)
	}
	if len(onus.Onus) != 1 || onus.Onus[0].Tipo != "penhora" {
		t.Fatalf("filtered onus: %+v", onus.Onus)
	}
}
