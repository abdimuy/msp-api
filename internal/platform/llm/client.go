package llm

import "context"

// Message is a single turn in a chat conversation.
// Role must be one of "system", "user", or "assistant".
type Message struct {
	Role    string
	Content string
}

// ResponseFormat carries an OpenAI-style structured-output request.
// Set Type to "json_object" for unstructured JSON or "json_schema" to supply
// an explicit JSON Schema (Schema + Name must be populated in that case).
type ResponseFormat struct {
	// Type is either "json_object" or "json_schema".
	Type string
	// Schema is the raw JSON Schema map used when Type == "json_schema".
	Schema map[string]any
	// Name is the schema name used when Type == "json_schema".
	Name string
}

// ChatReq is the input to a single chat completion call.
type ChatReq struct {
	// Messages is the ordered conversation history sent to the model.
	Messages []Message
	// Temperature controls sampling randomness. A zero value is omitted from
	// the request, letting the server apply its default.
	Temperature float64
	// ResponseFormat constrains the output format. Nil means no constraint.
	ResponseFormat *ResponseFormat
}

// Client is the interface satisfied by both realClient and disabledClient.
//
// Chat sends a chat-completion request and returns the assistant's reply as a
// plain string. The caller is responsible for parsing JSON when ResponseFormat
// is used.
//
// Callers must handle ErrLLMDisabled: when the LLM is not enabled, Chat always
// returns ("", ErrLLMDisabled). Use errors.Is to check before acting on the
// result.
//
// Network errors, timeouts, HTTP 429, and HTTP 5xx responses are wrapped in a
// TransientError and are safe to retry. HTTP 4xx (except 429) and JSON-decode
// failures are permanent and should not be retried.
type Client interface {
	Chat(ctx context.Context, req ChatReq) (string, error)
}
