package store

import (
	"context"
	"os"
	"reflect"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"mortgage/internal/dto"
)

// testPool connects to TEST_DATABASE_URL, or skips the test when unset.
// Run migrations first: DATABASE_URL=... go run ./cmd/migrate up
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

func boolPtr(b bool) *bool        { return &b }
func floatPtr(f float64) *float64 { return &f }

func fixtureMatricula() dto.ExtractedMatricula {
	return dto.ExtractedMatricula{
		Numero:       "45.678",
		Cartorio:     dto.Cartorio{Nome: "1º Oficial de Registro de Imóveis", Comarca: "São Paulo", UF: "SP"},
		DataAbertura: "1998-03-12",
		Imovel: dto.Imovel{
			Descricao: "Apartamento nº 42 do Edifício Solar das Acácias",
			Endereco:  "Rua das Figueiras, 1.200, Vila Mariana, São Paulo - SP",
			AreaM2:    floatPtr(127.42),
			Tipo:      "apartamento",
		},
		Atos: []dto.Ato{
			{
				Numero: "R-1", Kind: "registro", Tipo: dto.AtoCompraVenda, Data: "1998-03-12",
				Valor: floatPtr(180000), Moeda: "BRL", Descricao: "Compra e venda",
				Partes: []dto.Parte{
					{Papel: "adquirente", Nome: "José Carlos Almeida", CpfCnpj: "12345678909", CpfCnpjValido: boolPtr(true), TipoPessoa: "fisica"},
					{Papel: "transmitente", Nome: "Construtora Horizonte Ltda", CpfCnpj: "60111222000133", CpfCnpjValido: boolPtr(false), TipoPessoa: "juridica"},
				},
			},
			{
				Numero: "AV-3", Kind: "averbacao", Tipo: dto.AtoCancelamento, Data: "2010-06-15",
				Descricao: "Cancelamento da hipoteca de R-2",
			},
			{
				Numero: "R-4", Kind: "registro", Tipo: dto.AtoCompraVenda, Data: "2015-08-03",
				Valor: floatPtr(650000), Moeda: "BRL", Descricao: "Compra e venda",
				Partes: []dto.Parte{
					{Papel: "adquirente", Nome: "Fernanda Costa Ribeiro", CpfCnpj: "11122233396", CpfCnpjValido: boolPtr(true), TipoPessoa: "fisica"},
				},
			},
		},
		Proprietarios: []dto.Proprietario{
			{Nome: "Fernanda Costa Ribeiro", CpfCnpj: "11122233396", CpfCnpjValido: boolPtr(true), TipoPessoa: "fisica", Fracao: floatPtr(1), AtoAquisicao: "R-4"},
		},
		Onus: []dto.Onus{
			{Tipo: "penhora", Status: "ativo", AtoConstituicao: "AV-3", Credor: "Condomínio Edifício Solar das Acácias", Valor: floatPtr(38500), Moeda: "BRL", Descricao: "Penhora judicial"},
		},
	}
}

func TestSaveAndGetAggregateRoundTrip(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	docs := NewDocuments(pool)
	matriculas := NewMatriculas(pool)

	docID := uuid.NewString()
	if _, err := docs.Create(ctx, docID, "roundtrip.pdf", "test-bucket", "documents/x/original.pdf"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM documents WHERE id = $1::uuid`, docID)
	})

	in := fixtureMatricula()
	meta := ExtractionMeta{Model: "test-model", PromptVersion: "v1", RawResponse: []byte(`{"test":true}`), InputTokens: 100, OutputTokens: 200}

	matID, err := matriculas.SaveAggregate(ctx, docID, in, meta)
	if err != nil {
		t.Fatal(err)
	}

	got, err := matriculas.GetByDocumentID(ctx, docID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != matID || got.DocumentID != docID {
		t.Fatalf("ids: got %s/%s, want %s/%s", got.ID, got.DocumentID, matID, docID)
	}
	if !reflect.DeepEqual(got.ExtractedMatricula, in) {
		t.Errorf("round-trip mismatch:\n got: %+v\nwant: %+v", got.ExtractedMatricula, in)
	}

	byID, err := matriculas.GetByID(ctx, matID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(byID.ExtractedMatricula, in) {
		t.Errorf("GetByID mismatch")
	}

	// Re-extraction replaces the aggregate instead of duplicating it.
	in2 := in
	in2.Numero = "45.678-B"
	if _, err := matriculas.SaveAggregate(ctx, docID, in2, meta); err != nil {
		t.Fatal(err)
	}
	got2, err := matriculas.GetByDocumentID(ctx, docID)
	if err != nil {
		t.Fatal(err)
	}
	if got2.Numero != "45.678-B" {
		t.Errorf("re-extraction: numero = %q, want 45.678-B", got2.Numero)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM matriculas WHERE document_id = $1::uuid`, docID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Errorf("matriculas rows = %d, want 1", count)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM extractions WHERE document_id = $1::uuid`, docID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Errorf("extractions rows = %d, want 2 (append-only provenance)", count)
	}
}

func TestGetByDocumentIDNotFound(t *testing.T) {
	pool := testPool(t)
	_, err := NewMatriculas(pool).GetByDocumentID(context.Background(), uuid.NewString())
	if err != ErrNotFound {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}
