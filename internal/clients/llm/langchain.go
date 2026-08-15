package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	lcllms "github.com/tmc/langchaingo/llms"
	lcanthropic "github.com/tmc/langchaingo/llms/anthropic"
	lcollama "github.com/tmc/langchaingo/llms/ollama"

	"tax-deeds/internal/dto"
)

// langChain is the LangChain-backed strategy. It relies on prompt-enforced
// JSON plus strict decoding with one in-call repair round; this keeps the
// implementation portable across langchaingo providers.
type langChain struct {
	llm   lcllms.Model
	model string
}

func newLangChainAnthropic(model string) (Extractor, error) {
	l, err := lcanthropic.New(lcanthropic.WithModel(model))
	if err != nil {
		return nil, fmt.Errorf("langchain anthropic: %w", err)
	}
	return &langChain{llm: l, model: model}, nil
}

func newLangChainOllama(model, serverURL string) (Extractor, error) {
	// Ollama's default context window (4096) is too small for the system
	// prompt + OCR text + a reasoning model's thinking tokens; hitting it
	// truncates the response mid-JSON and every extraction fails.
	l, err := lcollama.New(
		lcollama.WithModel(model),
		lcollama.WithServerURL(serverURL),
		lcollama.WithRunnerNumCtx(16384),
	)
	if err != nil {
		return nil, fmt.Errorf("langchain ollama: %w", err)
	}
	return &langChain{llm: l, model: model}, nil
}

func (c *langChain) Model() string { return c.model }

func (c *langChain) ExtractMatricula(ctx context.Context, ocrText string) (dto.ExtractedMatricula, []byte, Usage, error) {
	messages := []lcllms.MessageContent{
		lcllms.TextParts(lcllms.ChatMessageTypeSystem, systemPrompt),
		lcllms.TextParts(lcllms.ChatMessageTypeHuman, ocrText),
	}

	raw, usage, err := c.generate(ctx, messages)
	if err != nil {
		return dto.ExtractedMatricula{}, nil, usage, err
	}

	m, parseErr := parseExtraction(raw)
	if parseErr == nil {
		return m, []byte(raw), usage, nil
	}

	// One repair round: show the model its own output and the decode error.
	messages = append(messages,
		lcllms.TextParts(lcllms.ChatMessageTypeAI, raw),
		lcllms.TextParts(lcllms.ChatMessageTypeHuman,
			fmt.Sprintf("Sua resposta não pôde ser decodificada (%v). Responda novamente APENAS com o objeto JSON corrigido, sem nenhum outro texto.", parseErr)),
	)
	raw2, usage2, err := c.generate(ctx, messages)
	usage.InputTokens += usage2.InputTokens
	usage.OutputTokens += usage2.OutputTokens
	if err != nil {
		return dto.ExtractedMatricula{}, nil, usage, err
	}
	m, parseErr = parseExtraction(raw2)
	if parseErr != nil {
		// Malformed output twice: worth a document-level retry later.
		return dto.ExtractedMatricula{}, []byte(raw2), usage, Transient(fmt.Errorf("model output is not valid extraction JSON: %w", parseErr))
	}
	return m, []byte(raw2), usage, nil
}

func (c *langChain) generate(ctx context.Context, messages []lcllms.MessageContent) (string, Usage, error) {
	resp, err := c.llm.GenerateContent(ctx, messages,
		lcllms.WithMaxTokens(8192),
		lcllms.WithTemperature(0),
	)
	if err != nil {
		return "", Usage{}, classify(err)
	}
	if len(resp.Choices) == 0 {
		return "", Usage{}, Transient(fmt.Errorf("empty response from model"))
	}
	choice := resp.Choices[0]
	return choice.Content, usageFrom(choice.GenerationInfo), nil
}

// classify splits API failures into terminal (auth/config, bad request) and
// transient (rate limits, overload, network) so the poller can back off.
func classify(err error) error {
	msg := strings.ToLower(err.Error())
	for _, terminal := range []string{"401", "403", "authentication", "invalid x-api-key", "api key", "not_found_error", "invalid_request"} {
		if strings.Contains(msg, terminal) {
			return err
		}
	}
	return Transient(err)
}

func usageFrom(info map[string]any) Usage {
	return Usage{
		InputTokens:  intFrom(info, "InputTokens", "input_tokens", "PromptTokens"),
		OutputTokens: intFrom(info, "OutputTokens", "output_tokens", "CompletionTokens"),
	}
}

func intFrom(info map[string]any, keys ...string) int {
	for _, k := range keys {
		switch v := info[k].(type) {
		case int:
			return v
		case int64:
			return int(v)
		case float64:
			return int(v)
		}
	}
	return 0
}

// parseExtraction strictly decodes the model output, tolerating code fences
// and surrounding prose by isolating the outermost JSON object.
func parseExtraction(raw string) (dto.ExtractedMatricula, error) {
	var m dto.ExtractedMatricula
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start < 0 || end <= start {
		return m, fmt.Errorf("no JSON object in output")
	}
	dec := json.NewDecoder(bytes.NewReader([]byte(raw[start : end+1])))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&m); err != nil {
		return m, err
	}
	if m.Numero == "" {
		return m, fmt.Errorf(`"numero" is empty`)
	}
	if err := validateEnums(m); err != nil {
		return m, err
	}
	return m, nil
}

// validateEnums rejects values outside the closed vocabularies enforced by
// the database schema, so the repair round can ask the model to correct them
// instead of failing at persistence. Comparison is case-insensitive because
// normalization lowercases these fields before insert.
func validateEnums(m dto.ExtractedMatricula) error {
	for _, a := range m.Atos {
		if !enumOK(a.Kind, false, "registro", "averbacao") {
			return fmt.Errorf(`ato %s: "kind" is %q (want registro or averbacao)`, a.Numero, a.Kind)
		}
		for _, p := range a.Partes {
			if !enumOK(p.Papel, false, "transmitente", "adquirente", "credor", "devedor", "interessado") {
				return fmt.Errorf(`ato %s, parte %q: "papel" is %q (want transmitente, adquirente, credor, devedor or interessado)`, a.Numero, p.Nome, p.Papel)
			}
			if !enumOK(p.TipoPessoa, true, "fisica", "juridica") {
				return fmt.Errorf(`ato %s, parte %q: "tipo_pessoa" is %q (want fisica or juridica)`, a.Numero, p.Nome, p.TipoPessoa)
			}
		}
	}
	for _, p := range m.Proprietarios {
		if !enumOK(p.TipoPessoa, true, "fisica", "juridica") {
			return fmt.Errorf(`proprietario %q: "tipo_pessoa" is %q (want fisica or juridica)`, p.Nome, p.TipoPessoa)
		}
	}
	for _, o := range m.Onus {
		if !enumOK(o.Status, true, "ativo", "cancelado") {
			return fmt.Errorf(`onus %q: "status" is %q (want ativo or cancelado)`, o.Tipo, o.Status)
		}
	}
	return nil
}

func enumOK(v string, optional bool, allowed ...string) bool {
	v = strings.ToLower(strings.TrimSpace(v))
	if v == "" {
		return optional
	}
	for _, a := range allowed {
		if v == a {
			return true
		}
	}
	return false
}
