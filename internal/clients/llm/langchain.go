package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	lcllms "github.com/tmc/langchaingo/llms"
	lcanthropic "github.com/tmc/langchaingo/llms/anthropic"

	"mortgage/internal/dto"
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
	return m, nil
}
