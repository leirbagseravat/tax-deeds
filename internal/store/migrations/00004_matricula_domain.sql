-- +goose Up

-- Provenance of each LLM extraction run; kept append-only so re-extraction
-- has an audit trail.
CREATE TABLE extractions (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    document_id    uuid NOT NULL REFERENCES documents (id) ON DELETE CASCADE,
    model          text,
    prompt_version text,
    raw_response   jsonb,
    input_tokens   int,
    output_tokens  int,
    created_at     timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE matriculas (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    document_id      uuid NOT NULL UNIQUE REFERENCES documents (id) ON DELETE CASCADE,
    extraction_id    uuid NOT NULL REFERENCES extractions (id) ON DELETE CASCADE,
    numero           text NOT NULL,
    cartorio_nome    text,
    cartorio_comarca text,
    cartorio_uf      text,
    data_abertura    date,
    imovel_descricao text,
    imovel_endereco  text,
    imovel_area_m2   numeric,
    imovel_tipo      text
);

-- Registros (R-n) and averbações (Av-n) form one numbered timeline on the
-- physical document, so they share a table with a kind discriminator.
CREATE TABLE atos (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    matricula_id uuid NOT NULL REFERENCES matriculas (id) ON DELETE CASCADE,
    numero       text NOT NULL,
    kind         text NOT NULL CHECK (kind IN ('registro', 'averbacao')),
    tipo         text,
    data_ato     date,
    valor        numeric,
    moeda        text, -- older matrículas carry pre-Real currencies (Cr$, CZ$, NCz$)
    descricao    text,
    seq          int NOT NULL,
    UNIQUE (matricula_id, seq)
);

CREATE TABLE ato_partes (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    ato_id          uuid NOT NULL REFERENCES atos (id) ON DELETE CASCADE,
    papel           text NOT NULL CHECK (papel IN ('transmitente', 'adquirente', 'credor', 'devedor', 'interessado')),
    nome            text NOT NULL,
    cpf_cnpj        text, -- digits-only text; leading zeros matter
    cpf_cnpj_valido boolean,
    tipo_pessoa     text CHECK (tipo_pessoa IN ('fisica', 'juridica')),
    seq             int NOT NULL DEFAULT 0 -- order within the act, as printed
);

-- Current-owner snapshot derived by the LLM; the authoritative chain is atos.
CREATE TABLE proprietarios (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    matricula_id     uuid NOT NULL REFERENCES matriculas (id) ON DELETE CASCADE,
    nome             text NOT NULL,
    cpf_cnpj         text,
    cpf_cnpj_valido  boolean,
    tipo_pessoa      text CHECK (tipo_pessoa IN ('fisica', 'juridica')),
    fracao           numeric,
    ato_aquisicao_id uuid REFERENCES atos (id) ON DELETE SET NULL
);

CREATE TABLE onus (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    matricula_id        uuid NOT NULL REFERENCES matriculas (id) ON DELETE CASCADE,
    tipo                text NOT NULL,
    status              text NOT NULL DEFAULT 'ativo' CHECK (status IN ('ativo', 'cancelado')),
    ato_constituicao_id uuid REFERENCES atos (id) ON DELETE SET NULL,
    ato_cancelamento_id uuid REFERENCES atos (id) ON DELETE SET NULL,
    credor              text,
    valor               numeric,
    moeda               text,
    descricao           text
);

ALTER TABLE documents
    ADD COLUMN extraction_attempts int NOT NULL DEFAULT 0,
    ADD COLUMN next_extraction_at  timestamptz;

-- +goose Down
ALTER TABLE documents
    DROP COLUMN extraction_attempts,
    DROP COLUMN next_extraction_at;
DROP TABLE onus;
DROP TABLE proprietarios;
DROP TABLE ato_partes;
DROP TABLE atos;
DROP TABLE matriculas;
DROP TABLE extractions;
