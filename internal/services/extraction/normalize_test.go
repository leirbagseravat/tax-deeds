package extraction

import (
	"testing"

	"mortgage/internal/dto"
)

func TestNormalizeDoc(t *testing.T) {
	tests := []struct {
		in        string
		wantDoc   string
		wantValid bool
	}{
		{"529.982.247-25", "52998224725", true},         // valid CPF
		{"123.456.789-09", "12345678909", true},         // valid CPF
		{"111.111.111-11", "11111111111", false},        // repeated digits
		{"123.456.789-00", "12345678900", false},        // bad checksum
		{"11.222.333/0001-81", "11222333000181", true},  // valid CNPJ
		{"60.111.222/0001-33", "60111222000133", false}, // bad checksum
		{"12345", "12345", false},                       // wrong length
	}
	for _, tt := range tests {
		doc, valid := normalizeDoc(tt.in)
		if doc != tt.wantDoc {
			t.Errorf("normalizeDoc(%q) doc = %q, want %q", tt.in, doc, tt.wantDoc)
		}
		if valid == nil || *valid != tt.wantValid {
			t.Errorf("normalizeDoc(%q) valid = %v, want %v", tt.in, valid, tt.wantValid)
		}
	}
	if doc, valid := normalizeDoc(""); doc != "" || valid != nil {
		t.Errorf("normalizeDoc(\"\") = %q, %v; want empty and nil", doc, valid)
	}
}

func TestNormalizeDate(t *testing.T) {
	tests := map[string]string{
		"12/03/1998": "1998-03-12",
		"1998-03-12": "1998-03-12",
		"03-08-2015": "2015-08-03",
		"garbage":    "",
		"":           "",
	}
	for in, want := range tests {
		if got := normalizeDate(in); got != want {
			t.Errorf("normalizeDate(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNormalizeOnusStatus(t *testing.T) {
	m := dto.ExtractedMatricula{
		Onus: []dto.Onus{
			{Tipo: "Hipoteca", AtoCancelamento: "AV-3"},
			{Tipo: "PENHORA"},
			{Tipo: "usufruto", Status: "cancelado"},
		},
	}
	Normalize(&m)
	if m.Onus[0].Status != "cancelado" {
		t.Errorf("onus with cancelling act: status = %q, want cancelado", m.Onus[0].Status)
	}
	if m.Onus[1].Status != "ativo" {
		t.Errorf("onus without status: status = %q, want ativo", m.Onus[1].Status)
	}
	if m.Onus[2].Status != "cancelado" {
		t.Errorf("explicit status overwritten: %q", m.Onus[2].Status)
	}
	if m.Onus[0].Tipo != "hipoteca" || m.Onus[1].Tipo != "penhora" {
		t.Errorf("tipos not lowercased: %+v", m.Onus)
	}
}

func TestNormalizeDefaultsCurrency(t *testing.T) {
	v := 1000.0
	m := dto.ExtractedMatricula{
		Atos: []dto.Ato{{Numero: "R-1", Kind: "REGISTRO", Valor: &v, Data: "12/03/1998"}},
	}
	Normalize(&m)
	if m.Atos[0].Moeda != "BRL" {
		t.Errorf("moeda = %q, want BRL", m.Atos[0].Moeda)
	}
	if m.Atos[0].Kind != "registro" {
		t.Errorf("kind = %q, want registro", m.Atos[0].Kind)
	}
	if m.Atos[0].Data != "1998-03-12" {
		t.Errorf("data = %q, want 1998-03-12", m.Atos[0].Data)
	}
}
