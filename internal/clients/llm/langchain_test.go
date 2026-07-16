package llm

import (
	"strings"
	"testing"
)

const minimalJSON = `{"numero":"1.234","cartorio":{"uf":"SP"},"imovel":{"tipo":"casa"},"atos":[],"proprietarios":[],"onus":[]}`

func TestParseExtraction(t *testing.T) {
	m, err := parseExtraction(minimalJSON)
	if err != nil {
		t.Fatal(err)
	}
	if m.Numero != "1.234" {
		t.Errorf("numero = %q", m.Numero)
	}
}

func TestParseExtractionStripsFences(t *testing.T) {
	raw := "```json\n" + minimalJSON + "\n```"
	if _, err := parseExtraction(raw); err != nil {
		t.Fatalf("fenced JSON rejected: %v", err)
	}
}

func TestParseExtractionRejectsUnknownFields(t *testing.T) {
	raw := strings.Replace(minimalJSON, `"numero"`, `"observacao":"x","numero"`, 1)
	if _, err := parseExtraction(raw); err == nil {
		t.Fatal("unknown field accepted")
	}
}

func TestParseExtractionRejectsEmptyNumero(t *testing.T) {
	raw := strings.Replace(minimalJSON, `"1.234"`, `""`, 1)
	if _, err := parseExtraction(raw); err == nil {
		t.Fatal("empty numero accepted")
	}
}

func TestParseExtractionRejectsBadEnums(t *testing.T) {
	cases := map[string]string{
		"papel":       `{"numero":"1","cartorio":{},"imovel":{},"atos":[{"numero":"R-1","kind":"registro","partes":[{"papel":"vendedor","nome":"X"}]}],"proprietarios":[],"onus":[]}`,
		"kind":        `{"numero":"1","cartorio":{},"imovel":{},"atos":[{"numero":"R-1","kind":"matricula"}],"proprietarios":[],"onus":[]}`,
		"tipo_pessoa": `{"numero":"1","cartorio":{},"imovel":{},"atos":[],"proprietarios":[{"nome":"X","tipo_pessoa":"empresa"}],"onus":[]}`,
		"onus status": `{"numero":"1","cartorio":{},"imovel":{},"atos":[],"proprietarios":[],"onus":[{"tipo":"hipoteca","status":"vigente"}]}`,
	}
	for name, raw := range cases {
		if _, err := parseExtraction(raw); err == nil {
			t.Errorf("%s: invalid enum accepted", name)
		}
	}
}

func TestParseExtractionAcceptsUppercaseEnums(t *testing.T) {
	raw := `{"numero":"1","cartorio":{},"imovel":{},"atos":[{"numero":"R-1","kind":"Registro","partes":[{"papel":"Adquirente","nome":"X"}]}],"proprietarios":[],"onus":[]}`
	if _, err := parseExtraction(raw); err != nil {
		t.Errorf("case-variant enum rejected: %v", err)
	}
}

func TestParseExtractionRejectsProse(t *testing.T) {
	if _, err := parseExtraction("desculpe, não consegui ler o documento"); err == nil {
		t.Fatal("prose accepted")
	}
}

func TestStubFixtureParses(t *testing.T) {
	m, _, _, err := newStub().ExtractMatricula(t.Context(), "any")
	if err != nil {
		t.Fatal(err)
	}
	if m.Numero != "45.678" || len(m.Atos) != 6 || len(m.Onus) != 3 {
		t.Errorf("stub fixture unexpected: numero=%s atos=%d onus=%d", m.Numero, len(m.Atos), len(m.Onus))
	}
}
