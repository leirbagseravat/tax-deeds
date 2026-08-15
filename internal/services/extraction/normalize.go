// Package extraction turns OCR text into the structured matrícula aggregate
// and persists it. Normalization lives here (not in the LLM prompt): the LLM
// transcribes, Go validates.
package extraction

import (
	"strings"
	"time"

	"tax-deeds/internal/dto"
)

// Normalize cleans an LLM extraction in place: ISO dates, digit-only
// CPF/CNPJ with checksum flags (flag, never reject), derived ônus status,
// default currency, and lowercased enums.
func Normalize(m *dto.ExtractedMatricula) {
	m.DataAbertura = normalizeDate(m.DataAbertura)
	m.Cartorio.UF = strings.ToUpper(strings.TrimSpace(m.Cartorio.UF))

	for i := range m.Atos {
		a := &m.Atos[i]
		a.Kind = strings.ToLower(strings.TrimSpace(a.Kind))
		a.Tipo = strings.ToLower(strings.TrimSpace(a.Tipo))
		a.Data = normalizeDate(a.Data)
		if a.Valor != nil && a.Moeda == "" {
			a.Moeda = "BRL"
		}
		for j := range a.Partes {
			p := &a.Partes[j]
			p.Papel = strings.ToLower(strings.TrimSpace(p.Papel))
			p.TipoPessoa = strings.ToLower(strings.TrimSpace(p.TipoPessoa))
			p.CpfCnpj, p.CpfCnpjValido = normalizeDoc(p.CpfCnpj)
		}
	}

	for i := range m.Proprietarios {
		p := &m.Proprietarios[i]
		p.TipoPessoa = strings.ToLower(strings.TrimSpace(p.TipoPessoa))
		p.CpfCnpj, p.CpfCnpjValido = normalizeDoc(p.CpfCnpj)
	}

	for i := range m.Onus {
		o := &m.Onus[i]
		o.Tipo = strings.ToLower(strings.TrimSpace(o.Tipo))
		if o.Valor != nil && o.Moeda == "" {
			o.Moeda = "BRL"
		}
		// A cancelling act is authoritative; otherwise default to active.
		if o.AtoCancelamento != "" {
			o.Status = "cancelado"
		} else if o.Status == "" {
			o.Status = "ativo"
		}
	}
}

var dateLayouts = []string{"2006-01-02", "02/01/2006", "02-01-2006", "02.01.2006"}

// normalizeDate parses Brazilian or ISO dates into ISO, or "" if unparseable.
func normalizeDate(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	for _, layout := range dateLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t.Format("2006-01-02")
		}
	}
	return ""
}

// normalizeDoc strips formatting from a CPF/CNPJ and reports checksum
// validity. Invalid documents are kept (OCR noise is expected) but flagged.
func normalizeDoc(s string) (string, *bool) {
	var b strings.Builder
	for _, r := range strings.ToUpper(s) {
		if r >= '0' && r <= '9' || r >= 'A' && r <= 'Z' { // 2026+ CNPJs may be alphanumeric
			b.WriteRune(r)
		}
	}
	doc := b.String()
	if doc == "" {
		return "", nil
	}
	var valid bool
	switch len(doc) {
	case 11:
		valid = validCPF(doc)
	case 14:
		valid = validCNPJ(doc)
	}
	return doc, &valid
}

func validCPF(cpf string) bool {
	if allSame(cpf) || !allDigits(cpf) {
		return false
	}
	for _, n := range []int{9, 10} {
		sum := 0
		for i := 0; i < n; i++ {
			sum += int(cpf[i]-'0') * (n + 1 - i)
		}
		dv := sum * 10 % 11 % 10
		if dv != int(cpf[n]-'0') {
			return false
		}
	}
	return true
}

func validCNPJ(cnpj string) bool {
	// Alphanumeric CNPJs (post-2026 format) are not checksum-validated yet.
	if allSame(cnpj) || !allDigits(cnpj) {
		return false
	}
	weights := []int{6, 5, 4, 3, 2, 9, 8, 7, 6, 5, 4, 3, 2}
	for _, n := range []int{12, 13} {
		sum := 0
		for i := 0; i < n; i++ {
			sum += int(cnpj[i]-'0') * weights[len(weights)-n+i]
		}
		dv := 11 - sum%11
		if dv >= 10 {
			dv = 0
		}
		if dv != int(cnpj[n]-'0') {
			return false
		}
	}
	return true
}

func allDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func allSame(s string) bool {
	for i := 1; i < len(s); i++ {
		if s[i] != s[0] {
			return false
		}
	}
	return true
}
