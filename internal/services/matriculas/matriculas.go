// Package matriculas exposes read operations over extracted registry data:
// the full aggregate per document and filtered views (proprietários, atos,
// ônus) per matrícula.
package matriculas

import (
	"context"
	"fmt"

	"tax-deeds/internal/dto"
	"tax-deeds/internal/store"
)

// NotReadyError means the document exists but its matrícula has not been
// extracted yet — the client should keep polling.
type NotReadyError struct{ Status string }

func (e *NotReadyError) Error() string {
	return fmt.Sprintf("matricula not ready, document status %s", e.Status)
}

// FailedError means the pipeline gave up on the document; polling won't help.
type FailedError struct{ Stage, Message string }

func (e *FailedError) Error() string {
	return fmt.Sprintf("document failed at %s: %s", e.Stage, e.Message)
}

// Service assembles read DTOs for extracted matrículas.
type Service struct {
	docs *store.Documents
	mats *store.Matriculas
}

func New(docs *store.Documents, mats *store.Matriculas) *Service {
	return &Service{docs: docs, mats: mats}
}

// ByDocument returns the full aggregate for a document, distinguishing
// "keep polling" (NotReadyError) and "gave up" (FailedError) from
// "wrong id" (store.ErrNotFound).
func (s *Service) ByDocument(ctx context.Context, documentID string) (dto.MatriculaResponse, error) {
	doc, err := s.docs.GetByID(ctx, documentID)
	if err != nil {
		return dto.MatriculaResponse{}, err
	}
	switch doc.Status {
	case store.StatusExtracted:
		return s.mats.GetByDocumentID(ctx, documentID)
	case store.StatusFailed:
		fe := &FailedError{}
		if doc.FailedStage != nil {
			fe.Stage = *doc.FailedStage
		}
		if doc.ErrorMessage != nil {
			fe.Message = *doc.ErrorMessage
		}
		return dto.MatriculaResponse{}, fe
	default:
		return dto.MatriculaResponse{}, &NotReadyError{Status: doc.Status}
	}
}

// Proprietarios returns the current owners plus the cadeia dominial — the
// seq-ordered ownership transfers that led to them.
func (s *Service) Proprietarios(ctx context.Context, matriculaID string) (dto.ProprietariosResponse, error) {
	m, err := s.mats.GetByID(ctx, matriculaID)
	if err != nil {
		return dto.ProprietariosResponse{}, err
	}
	return dto.ProprietariosResponse{
		MatriculaID:    m.ID,
		Numero:         m.Numero,
		Proprietarios:  m.Proprietarios,
		CadeiaDominial: OwnershipChain(m.Atos),
	}, nil
}

// Atos returns the matrícula's acts, optionally filtered by kind
// (registro | averbacao). Empty kind returns the full timeline.
func (s *Service) Atos(ctx context.Context, matriculaID, kind string) (dto.AtosResponse, error) {
	m, err := s.mats.GetByID(ctx, matriculaID)
	if err != nil {
		return dto.AtosResponse{}, err
	}
	resp := dto.AtosResponse{MatriculaID: m.ID, Numero: m.Numero, Kind: kind}
	for _, a := range m.Atos {
		if kind == "" || a.Kind == kind {
			resp.Atos = append(resp.Atos, a)
		}
	}
	return resp, nil
}

// Onus returns the matrícula's liens, optionally filtered by status
// (ativo | cancelado). Empty status returns all.
func (s *Service) Onus(ctx context.Context, matriculaID, status string) (dto.OnusResponse, error) {
	m, err := s.mats.GetByID(ctx, matriculaID)
	if err != nil {
		return dto.OnusResponse{}, err
	}
	resp := dto.OnusResponse{MatriculaID: m.ID, Numero: m.Numero, Status: status}
	for _, o := range m.Onus {
		if status == "" || o.Status == status {
			resp.Onus = append(resp.Onus, o)
		}
	}
	return resp, nil
}

// transferTipos are ato types that move ownership between parties.
var transferTipos = map[string]bool{
	dto.AtoCompraVenda:   true,
	dto.AtoDoacao:        true,
	dto.AtoPartilha:      true,
	"permuta":            true,
	"adjudicacao":        true,
	"arrematacao":        true,
	"dacao_em_pagamento": true,
	"heranca":            true,
	"usucapiao":          true,
}

// OwnershipChain filters seq-ordered atos down to the registros that transfer
// ownership — the cadeia dominial.
func OwnershipChain(atos []dto.Ato) []dto.Ato {
	var chain []dto.Ato
	for _, a := range atos {
		if a.Kind == "registro" && transferTipos[a.Tipo] {
			chain = append(chain, a)
		}
	}
	return chain
}
