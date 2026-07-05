package dto

// ExtractedMatricula is the structured content of a matrícula. It is the
// single source of truth for the shape: the LLM JSON schema, the persistence
// layer, and the read API all use it. Dates are ISO strings ("2006-01-02");
// values before normalization may arrive as dd/mm/yyyy from the LLM.
type ExtractedMatricula struct {
	Numero        string         `json:"numero"`
	Cartorio      Cartorio       `json:"cartorio"`
	DataAbertura  string         `json:"data_abertura,omitempty"`
	Imovel        Imovel         `json:"imovel"`
	Atos          []Ato          `json:"atos"`
	Proprietarios []Proprietario `json:"proprietarios"`
	Onus          []Onus         `json:"onus"`
}

// Cartorio identifies the registry office holding the matrícula.
type Cartorio struct {
	Nome    string `json:"nome,omitempty"`
	Comarca string `json:"comarca,omitempty"`
	UF      string `json:"uf,omitempty"`
}

// Imovel describes the property.
type Imovel struct {
	Descricao string   `json:"descricao,omitempty"`
	Endereco  string   `json:"endereco,omitempty"`
	AreaM2    *float64 `json:"area_m2,omitempty"`
	Tipo      string   `json:"tipo,omitempty"` // urbano, rural, apartamento, lote...
}

// Ato is one numbered act on the matrícula timeline: a registro (R-n) or an
// averbação (Av-n), in document order.
type Ato struct {
	Numero    string   `json:"numero"` // as printed: "R-1", "AV-3"
	Kind      string   `json:"kind"`   // registro | averbacao
	Tipo      string   `json:"tipo,omitempty"`
	Data      string   `json:"data,omitempty"`
	Valor     *float64 `json:"valor,omitempty"`
	Moeda     string   `json:"moeda,omitempty"` // BRL, or pre-Real currencies on old acts
	Descricao string   `json:"descricao,omitempty"`
	Partes    []Parte  `json:"partes,omitempty"`
}

// Ato tipo vocabulary (open-ended; "outro" for anything else).
const (
	AtoCompraVenda         = "compra_venda"
	AtoDoacao              = "doacao"
	AtoPartilha            = "partilha"
	AtoHipoteca            = "hipoteca"
	AtoPenhora             = "penhora"
	AtoAlienacaoFiduciaria = "alienacao_fiduciaria"
	AtoCancelamento        = "cancelamento"
	AtoConstrucao          = "construcao"
	AtoOutro               = "outro"
)

// Parte is one party of an act.
type Parte struct {
	Papel         string `json:"papel"` // transmitente | adquirente | credor | devedor | interessado
	Nome          string `json:"nome"`
	CpfCnpj       string `json:"cpf_cnpj,omitempty"`
	CpfCnpjValido *bool  `json:"cpf_cnpj_valido,omitempty"` // set by normalization, not the LLM
	TipoPessoa    string `json:"tipo_pessoa,omitempty"`     // fisica | juridica
}

// Proprietario is a current owner as of the last act.
type Proprietario struct {
	Nome          string   `json:"nome"`
	CpfCnpj       string   `json:"cpf_cnpj,omitempty"`
	CpfCnpjValido *bool    `json:"cpf_cnpj_valido,omitempty"`
	TipoPessoa    string   `json:"tipo_pessoa,omitempty"`
	Fracao        *float64 `json:"fracao,omitempty"`        // ownership fraction, e.g. 0.5
	AtoAquisicao  string   `json:"ato_aquisicao,omitempty"` // numero of the acquiring ato
}

// Onus is a lien/encumbrance with its lifecycle.
type Onus struct {
	Tipo            string   `json:"tipo"`                       // hipoteca, penhora, alienacao_fiduciaria, usufruto, servidao, indisponibilidade, outro
	Status          string   `json:"status,omitempty"`           // ativo | cancelado (derived from ato_cancelamento)
	AtoConstituicao string   `json:"ato_constituicao,omitempty"` // numero of the constituting ato
	AtoCancelamento string   `json:"ato_cancelamento,omitempty"` // numero of the cancelling ato, if any
	Credor          string   `json:"credor,omitempty"`
	Valor           *float64 `json:"valor,omitempty"`
	Moeda           string   `json:"moeda,omitempty"`
	Descricao       string   `json:"descricao,omitempty"`
}

// MatriculaResponse is the full aggregate returned by the read API.
type MatriculaResponse struct {
	ID         string `json:"id"`
	DocumentID string `json:"document_id"`
	ExtractedMatricula
}
