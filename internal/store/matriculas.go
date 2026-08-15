package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"tax-deeds/internal/dto"
)

// ExtractionMeta records the provenance of one LLM extraction run.
type ExtractionMeta struct {
	Model         string
	PromptVersion string
	RawResponse   []byte // the LLM's JSON, stored verbatim for audit/replay
	InputTokens   int
	OutputTokens  int
}

// Matriculas persists and reads the extracted matrícula aggregate.
type Matriculas struct {
	pool *pgxpool.Pool
}

func NewMatriculas(pool *pgxpool.Pool) *Matriculas {
	return &Matriculas{pool: pool}
}

// SaveAggregate stores one extraction result in a single transaction:
// extraction provenance, then the matrícula aggregate. Any previous aggregate
// for the document is replaced (re-extraction), while extractions accumulate.
// The input must already be normalized (dates ISO, documents digit-only).
func (s *Matriculas) SaveAggregate(ctx context.Context, documentID string, m dto.ExtractedMatricula, meta ExtractionMeta) (string, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)

	var extractionID string
	err = tx.QueryRow(ctx, `
		INSERT INTO extractions (document_id, model, prompt_version, raw_response, input_tokens, output_tokens)
		VALUES ($1::uuid, $2, $3, $4, $5, $6)
		RETURNING id::text`,
		documentID, meta.Model, meta.PromptVersion, meta.RawResponse, meta.InputTokens, meta.OutputTokens,
	).Scan(&extractionID)
	if err != nil {
		return "", fmt.Errorf("insert extraction: %w", err)
	}

	if _, err := tx.Exec(ctx, `DELETE FROM matriculas WHERE document_id = $1::uuid`, documentID); err != nil {
		return "", fmt.Errorf("replace previous aggregate: %w", err)
	}

	var matriculaID string
	err = tx.QueryRow(ctx, `
		INSERT INTO matriculas (document_id, extraction_id, numero,
			cartorio_nome, cartorio_comarca, cartorio_uf, data_abertura,
			imovel_descricao, imovel_endereco, imovel_area_m2, imovel_tipo)
		VALUES ($1::uuid, $2::uuid, $3, $4, $5, $6, $7::date, $8, $9, $10, $11)
		RETURNING id::text`,
		documentID, extractionID, m.Numero,
		nullStr(m.Cartorio.Nome), nullStr(m.Cartorio.Comarca), nullStr(m.Cartorio.UF), nullStr(m.DataAbertura),
		nullStr(m.Imovel.Descricao), nullStr(m.Imovel.Endereco), m.Imovel.AreaM2, nullStr(m.Imovel.Tipo),
	).Scan(&matriculaID)
	if err != nil {
		return "", fmt.Errorf("insert matricula: %w", err)
	}

	atoIDs := make(map[string]string, len(m.Atos)) // numero -> id
	for seq, ato := range m.Atos {
		var atoID string
		err = tx.QueryRow(ctx, `
			INSERT INTO atos (matricula_id, numero, kind, tipo, data_ato, valor, moeda, descricao, seq)
			VALUES ($1::uuid, $2, $3, $4, $5::date, $6, $7, $8, $9)
			RETURNING id::text`,
			matriculaID, ato.Numero, ato.Kind, nullStr(ato.Tipo), nullStr(ato.Data),
			ato.Valor, nullStr(ato.Moeda), nullStr(ato.Descricao), seq+1,
		).Scan(&atoID)
		if err != nil {
			return "", fmt.Errorf("insert ato %s: %w", ato.Numero, err)
		}
		atoIDs[ato.Numero] = atoID

		for pseq, p := range ato.Partes {
			if _, err := tx.Exec(ctx, `
				INSERT INTO ato_partes (ato_id, papel, nome, cpf_cnpj, cpf_cnpj_valido, tipo_pessoa, seq)
				VALUES ($1::uuid, $2, $3, $4, $5, $6, $7)`,
				atoID, p.Papel, p.Nome, nullStr(p.CpfCnpj), p.CpfCnpjValido, nullStr(p.TipoPessoa), pseq+1,
			); err != nil {
				return "", fmt.Errorf("insert parte of %s: %w", ato.Numero, err)
			}
		}
	}

	for _, p := range m.Proprietarios {
		if _, err := tx.Exec(ctx, `
			INSERT INTO proprietarios (matricula_id, nome, cpf_cnpj, cpf_cnpj_valido, tipo_pessoa, fracao, ato_aquisicao_id)
			VALUES ($1::uuid, $2, $3, $4, $5, $6, $7::uuid)`,
			matriculaID, p.Nome, nullStr(p.CpfCnpj), p.CpfCnpjValido, nullStr(p.TipoPessoa),
			p.Fracao, atoRef(atoIDs, p.AtoAquisicao),
		); err != nil {
			return "", fmt.Errorf("insert proprietario %s: %w", p.Nome, err)
		}
	}

	for _, o := range m.Onus {
		if _, err := tx.Exec(ctx, `
			INSERT INTO onus (matricula_id, tipo, status, ato_constituicao_id, ato_cancelamento_id, credor, valor, moeda, descricao)
			VALUES ($1::uuid, $2, $3, $4::uuid, $5::uuid, $6, $7, $8, $9)`,
			matriculaID, o.Tipo, o.Status,
			atoRef(atoIDs, o.AtoConstituicao), atoRef(atoIDs, o.AtoCancelamento),
			nullStr(o.Credor), o.Valor, nullStr(o.Moeda), nullStr(o.Descricao),
		); err != nil {
			return "", fmt.Errorf("insert onus %s: %w", o.Tipo, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return "", err
	}
	return matriculaID, nil
}

// GetByDocumentID loads the full aggregate for a document.
func (s *Matriculas) GetByDocumentID(ctx context.Context, documentID string) (dto.MatriculaResponse, error) {
	return s.get(ctx, `document_id = $1::uuid`, documentID)
}

// GetByID loads the full aggregate by matrícula ID.
func (s *Matriculas) GetByID(ctx context.Context, id string) (dto.MatriculaResponse, error) {
	return s.get(ctx, `id = $1::uuid`, id)
}

func (s *Matriculas) get(ctx context.Context, where, arg string) (dto.MatriculaResponse, error) {
	var (
		resp         dto.MatriculaResponse
		dataAbertura *time.Time
	)
	err := s.pool.QueryRow(ctx, `
		SELECT id::text, document_id::text, numero,
			COALESCE(cartorio_nome, ''), COALESCE(cartorio_comarca, ''), COALESCE(cartorio_uf, ''),
			data_abertura,
			COALESCE(imovel_descricao, ''), COALESCE(imovel_endereco, ''), imovel_area_m2, COALESCE(imovel_tipo, '')
		FROM matriculas WHERE `+where, arg,
	).Scan(&resp.ID, &resp.DocumentID, &resp.Numero,
		&resp.Cartorio.Nome, &resp.Cartorio.Comarca, &resp.Cartorio.UF,
		&dataAbertura,
		&resp.Imovel.Descricao, &resp.Imovel.Endereco, &resp.Imovel.AreaM2, &resp.Imovel.Tipo)
	if errors.Is(err, pgx.ErrNoRows) {
		return resp, ErrNotFound
	}
	if err != nil {
		return resp, err
	}
	resp.DataAbertura = isoDate(dataAbertura)

	atoNumeros := map[string]string{} // ato id -> numero
	if err := s.loadAtos(ctx, &resp, atoNumeros); err != nil {
		return resp, err
	}
	if err := s.loadProprietarios(ctx, &resp, atoNumeros); err != nil {
		return resp, err
	}
	if err := s.loadOnus(ctx, &resp, atoNumeros); err != nil {
		return resp, err
	}
	return resp, nil
}

func (s *Matriculas) loadAtos(ctx context.Context, resp *dto.MatriculaResponse, atoNumeros map[string]string) error {
	rows, err := s.pool.Query(ctx, `
		SELECT id::text, numero, kind, COALESCE(tipo, ''), data_ato, valor, COALESCE(moeda, ''), COALESCE(descricao, '')
		FROM atos WHERE matricula_id = $1::uuid ORDER BY seq`, resp.ID)
	if err != nil {
		return err
	}
	defer rows.Close()

	var atoIDs []string
	for rows.Next() {
		var (
			a       dto.Ato
			id      string
			dataAto *time.Time
		)
		if err := rows.Scan(&id, &a.Numero, &a.Kind, &a.Tipo, &dataAto, &a.Valor, &a.Moeda, &a.Descricao); err != nil {
			return err
		}
		a.Data = isoDate(dataAto)
		atoNumeros[id] = a.Numero
		atoIDs = append(atoIDs, id)
		resp.Atos = append(resp.Atos, a)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	parteRows, err := s.pool.Query(ctx, `
		SELECT p.ato_id::text, p.papel, p.nome, COALESCE(p.cpf_cnpj, ''), p.cpf_cnpj_valido, COALESCE(p.tipo_pessoa, '')
		FROM ato_partes p
		JOIN atos a ON a.id = p.ato_id
		WHERE a.matricula_id = $1::uuid
		ORDER BY a.seq, p.seq`, resp.ID)
	if err != nil {
		return err
	}
	defer parteRows.Close()

	byAto := map[string][]dto.Parte{}
	for parteRows.Next() {
		var (
			atoID string
			p     dto.Parte
		)
		if err := parteRows.Scan(&atoID, &p.Papel, &p.Nome, &p.CpfCnpj, &p.CpfCnpjValido, &p.TipoPessoa); err != nil {
			return err
		}
		byAto[atoID] = append(byAto[atoID], p)
	}
	if err := parteRows.Err(); err != nil {
		return err
	}
	for i, id := range atoIDs {
		resp.Atos[i].Partes = byAto[id]
	}
	return nil
}

func (s *Matriculas) loadProprietarios(ctx context.Context, resp *dto.MatriculaResponse, atoNumeros map[string]string) error {
	rows, err := s.pool.Query(ctx, `
		SELECT nome, COALESCE(cpf_cnpj, ''), cpf_cnpj_valido, COALESCE(tipo_pessoa, ''), fracao, ato_aquisicao_id::text
		FROM proprietarios WHERE matricula_id = $1::uuid ORDER BY nome`, resp.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			p     dto.Proprietario
			atoID *string
		)
		if err := rows.Scan(&p.Nome, &p.CpfCnpj, &p.CpfCnpjValido, &p.TipoPessoa, &p.Fracao, &atoID); err != nil {
			return err
		}
		if atoID != nil {
			p.AtoAquisicao = atoNumeros[*atoID]
		}
		resp.Proprietarios = append(resp.Proprietarios, p)
	}
	return rows.Err()
}

func (s *Matriculas) loadOnus(ctx context.Context, resp *dto.MatriculaResponse, atoNumeros map[string]string) error {
	rows, err := s.pool.Query(ctx, `
		SELECT tipo, status, ato_constituicao_id::text, ato_cancelamento_id::text,
			COALESCE(credor, ''), valor, COALESCE(moeda, ''), COALESCE(descricao, '')
		FROM onus WHERE matricula_id = $1::uuid ORDER BY tipo`, resp.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			o               dto.Onus
			constID, cancID *string
		)
		if err := rows.Scan(&o.Tipo, &o.Status, &constID, &cancID, &o.Credor, &o.Valor, &o.Moeda, &o.Descricao); err != nil {
			return err
		}
		if constID != nil {
			o.AtoConstituicao = atoNumeros[*constID]
		}
		if cancID != nil {
			o.AtoCancelamento = atoNumeros[*cancID]
		}
		resp.Onus = append(resp.Onus, o)
	}
	return rows.Err()
}

func isoDate(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.Format("2006-01-02")
}

// nullStr maps "" to NULL so empty LLM fields don't pollute the tables.
func nullStr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// atoRef resolves an ato numero reference to its UUID, or NULL when the
// reference is empty or points at an ato the LLM didn't list.
func atoRef(ids map[string]string, numero string) *string {
	if numero == "" {
		return nil
	}
	if id, ok := ids[numero]; ok {
		return &id
	}
	return nil
}
