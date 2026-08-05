package proxy

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"red-token/internal/domain"
)

var ErrCrossProtocolStreamingNotSupported = errors.New("cross-protocol streaming is not supported")

type ResponseAdapter func(*http.Response) (*http.Response, error)

type PreparedExchange struct {
	UpstreamPath  string
	RequestBody   []byte
	AdaptResponse ResponseAdapter
}

func PrepareExchange(clientPath string, backend domain.Backend, body []byte) (PreparedExchange, error) {
	exchange := PreparedExchange{
		UpstreamPath:  clientPath,
		RequestBody:   body,
		AdaptResponse: identityResponseAdapter,
	}
	if clientPath == "/v1/messages/count_tokens" {
		return exchange, nil
	}
	clientEndpoint := EndpointForPath(clientPath)
	switch clientEndpoint {
	case domain.EndpointMessages:
		if EffectiveBackendProtocolForPath(backend, clientPath) != domain.BackendProtocolOpenAI {
			return exchange, nil
		}
		stream, err := requestStreams(body)
		if err != nil {
			return PreparedExchange{}, err
		}
		requestBody, err := ConvertMessagesToResponsesRequest(body)
		if err != nil {
			return PreparedExchange{}, err
		}
		exchange.UpstreamPath = "/v1/responses"
		exchange.RequestBody = requestBody
		if stream {
			exchange.AdaptResponse = adaptStreamingOrJSONResponse(
				ConvertResponsesStreamToMessagesStream,
				ConvertResponsesResponseToMessagesResponse,
			)
			return exchange, nil
		}
		exchange.AdaptResponse = adaptJSONResponse(ConvertResponsesResponseToMessagesResponse)
		return exchange, nil
	case domain.EndpointResponses:
		if EffectiveBackendProtocolForPath(backend, clientPath) != domain.BackendProtocolAnthropic {
			return exchange, nil
		}
		stream, err := requestStreams(body)
		if err != nil {
			return PreparedExchange{}, err
		}
		requestBody, err := ConvertResponsesToMessagesRequest(body)
		if err != nil {
			return PreparedExchange{}, err
		}
		exchange.UpstreamPath = "/v1/messages"
		exchange.RequestBody = requestBody
		if stream {
			exchange.AdaptResponse = adaptStreamingOrJSONResponse(
				ConvertMessagesStreamToResponsesStream,
				ConvertMessagesResponseToResponsesResponse,
			)
			return exchange, nil
		}
		exchange.AdaptResponse = adaptJSONResponse(ConvertMessagesResponseToResponsesResponse)
		return exchange, nil
	default:
		return exchange, nil
	}
}

func ConvertMessagesToResponsesRequest(body []byte) ([]byte, error) {
	payload, err := decodeJSONObject(body)
	if err != nil {
		return nil, err
	}
	payload["input"] = convertAnthropicMessagesToResponsesInput(payload["messages"])
	delete(payload, "messages")
	if system, ok := payload["system"]; ok {
		if instructions := convertAnthropicSystemToResponsesInstructions(system); instructions != nil {
			payload["instructions"] = instructions
		}
		delete(payload, "system")
	}
	if maxTokens, ok := payload["max_tokens"]; ok {
		payload["max_output_tokens"] = maxTokens
		delete(payload, "max_tokens")
	}
	if stopSequences, ok := payload["stop_sequences"]; ok {
		payload["stop"] = stopSequences
		delete(payload, "stop_sequences")
	}
	if tools, ok := payload["tools"]; ok {
		payload["tools"] = convertAnthropicToolsToResponsesTools(tools)
	}
	if toolChoice, ok := payload["tool_choice"]; ok {
		payload["tool_choice"] = convertAnthropicToolChoiceToResponsesToolChoice(toolChoice)
	}
	delete(payload, "thinking")
	delete(payload, "top_k")
	return json.Marshal(payload)
}

func ConvertResponsesToMessagesRequest(body []byte) ([]byte, error) {
	payload, err := decodeJSONObject(body)
	if err != nil {
		return nil, err
	}
	payload["messages"] = convertResponsesInputToAnthropicMessages(payload["input"])
	delete(payload, "input")
	if instructions, ok := payload["instructions"]; ok {
		if system := convertResponsesInstructionsToAnthropicSystem(instructions); system != nil {
			payload["system"] = system
		}
		delete(payload, "instructions")
	}
	if maxTokens, ok := payload["max_output_tokens"]; ok {
		payload["max_tokens"] = maxTokens
		delete(payload, "max_output_tokens")
	}
	if stop, ok := payload["stop"]; ok {
		payload["stop_sequences"] = stop
		delete(payload, "stop")
	}
	if tools, ok := payload["tools"]; ok {
		payload["tools"] = convertResponsesToolsToAnthropicTools(tools)
	}
	if toolChoice, ok := payload["tool_choice"]; ok {
		payload["tool_choice"] = convertResponsesToolChoiceToAnthropicToolChoice(toolChoice)
	}
	return json.Marshal(payload)
}

func ConvertResponsesResponseToMessagesResponse(body []byte) ([]byte, error) {
	payload, err := decodeJSONObject(body)
	if err != nil {
		return nil, err
	}
	content := convertResponsesOutputToAnthropicContent(payload["output"])
	if len(content) == 0 {
		if outputText := strings.TrimSpace(asString(payload["output_text"])); outputText != "" {
			content = []any{map[string]any{
				"type": "text",
				"text": outputText,
			}}
		}
	}
	response := map[string]any{
		"id":      payload["id"],
		"type":    "message",
		"role":    "assistant",
		"model":   payload["model"],
		"content": content,
	}
	if usage, ok := payload["usage"]; ok {
		response["usage"] = usage
	}
	stopReason := strings.TrimSpace(asString(payload["stop_reason"]))
	if stopReason == "" {
		stopReason = "end_turn"
	}
	response["stop_reason"] = stopReason
	if stopSequence, ok := payload["stop_sequence"]; ok {
		response["stop_sequence"] = stopSequence
	}
	return json.Marshal(response)
}

func ConvertMessagesResponseToResponsesResponse(body []byte) ([]byte, error) {
	payload, err := decodeJSONObject(body)
	if err != nil {
		return nil, err
	}
	response := map[string]any{
		"id":     payload["id"],
		"object": "response",
		"model":  payload["model"],
		"output": []any{map[string]any{
			"type":    "message",
			"role":    firstNonEmpty(asString(payload["role"]), "assistant"),
			"content": convertAnthropicContentToResponsesOutputContent(payload["content"]),
		}},
		"status": "completed",
	}
	if usage, ok := payload["usage"]; ok {
		response["usage"] = usage
	}
	return json.Marshal(response)
}

func identityResponseAdapter(resp *http.Response) (*http.Response, error) {
	return resp, nil
}

func adaptJSONResponse(convert func([]byte) ([]byte, error)) ResponseAdapter {
	return adaptConvertedResponse("application/json", convert)
}

func adaptStreamingOrJSONResponse(streamConvert, jsonConvert func([]byte) ([]byte, error)) ResponseAdapter {
	return func(resp *http.Response) (*http.Response, error) {
		if strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "text/event-stream") {
			return adaptConvertedResponse("text/event-stream", streamConvert)(resp)
		}
		return adaptConvertedResponse("application/json", jsonConvert)(resp)
	}
}

func adaptConvertedResponse(contentType string, convert func([]byte) ([]byte, error)) ResponseAdapter {
	return func(resp *http.Response) (*http.Response, error) {
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			_ = resp.Body.Close()
			return nil, err
		}
		_ = resp.Body.Close()

		converted, err := convert(body)
		if err != nil {
			return nil, err
		}

		cloned := *resp
		cloned.Header = resp.Header.Clone()
		cloned.Header.Set("Content-Type", contentType)
		cloned.Header.Set("Content-Length", strconv.Itoa(len(converted)))
		cloned.ContentLength = int64(len(converted))
		cloned.Body = io.NopCloser(bytes.NewReader(converted))
		return &cloned, nil
	}
}

func requestStreams(body []byte) (bool, error) {
	payload, err := decodeJSONObject(body)
	if err != nil {
		return false, err
	}
	value, ok := payload["stream"]
	if !ok {
		return false, nil
	}
	stream, ok := value.(bool)
	if !ok {
		return false, nil
	}
	return stream, nil
}

type sseEvent struct {
	Event string
	Data  string
}

type streamTextBlock struct {
	text    string
	started bool
	open    bool
}

type responsesToMessagesStreamState struct {
	messageID      string
	model          string
	role           string
	startUsage     map[string]any
	messageStarted bool
	blocks         map[int]*streamTextBlock
}

type messagesToResponsesStreamState struct {
	responseID string
	itemID     string
	model      string
	role       string
	usage      map[string]any
	stopReason string
	stopSeq    any
	created    bool
	itemAdded  bool
	blocks     map[int]*streamTextBlock
}

func ConvertResponsesStreamToMessagesStream(body []byte) ([]byte, error) {
	events, err := parseSSEStream(body)
	if err != nil {
		return nil, err
	}

	state := responsesToMessagesStreamState{
		role:   "assistant",
		blocks: make(map[int]*streamTextBlock),
	}
	out := make([]sseEvent, 0, len(events))

	for _, event := range events {
		if strings.EqualFold(event.Event, "ping") {
			if err := appendSSEJSON(&out, "ping", map[string]any{"type": "ping"}); err != nil {
				return nil, err
			}
			continue
		}

		payload, ok, err := decodeSSEJSON(event.Data)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}

		eventType := firstNonEmpty(asString(payload["type"]), event.Event)
		switch eventType {
		case "response.created":
			response := asObject(payload["response"])
			state.messageID = firstNonEmpty(asString(response["id"]), state.messageID)
			state.model = firstNonEmpty(asString(response["model"]), state.model)
			state.startUsage = initialAnthropicUsage(normalizeUsageObject(response["usage"]))
			if err := ensureAnthropicMessageStart(&out, &state); err != nil {
				return nil, err
			}
		case "response.output_item.added":
			item := asObject(payload["item"])
			state.role = firstNonEmpty(asString(item["role"]), state.role, "assistant")
			if state.messageID == "" {
				state.messageID = asString(item["id"])
			}
		case "response.content_part.added":
			part := asObject(payload["part"])
			if !isResponsesTextPart(part) {
				continue
			}
			index := intValue(payload["content_index"])
			if err := ensureAnthropicMessageStart(&out, &state); err != nil {
				return nil, err
			}
			if err := ensureAnthropicContentBlockStart(&out, &state, index); err != nil {
				return nil, err
			}
		case "response.output_text.delta":
			index := intValue(payload["content_index"])
			delta := asString(payload["delta"])
			if delta == "" {
				continue
			}
			if err := ensureAnthropicMessageStart(&out, &state); err != nil {
				return nil, err
			}
			if err := ensureAnthropicContentBlockStart(&out, &state, index); err != nil {
				return nil, err
			}
			block := state.blocks[index]
			block.text += delta
			if err := appendSSEJSON(&out, "content_block_delta", map[string]any{
				"type":  "content_block_delta",
				"index": index,
				"delta": map[string]any{
					"type": "text_delta",
					"text": delta,
				},
			}); err != nil {
				return nil, err
			}
		case "response.output_text.done":
			index := intValue(payload["content_index"])
			block := state.ensureBlock(index)
			if text := asString(payload["text"]); text != "" {
				block.text = text
			}
		case "response.content_part.done":
			index := intValue(payload["content_index"])
			block := state.ensureBlock(index)
			if part := asObject(payload["part"]); isResponsesTextPart(part) {
				if text := asString(part["text"]); text != "" {
					block.text = text
				}
			}
			if block.open {
				if err := appendSSEJSON(&out, "content_block_stop", map[string]any{
					"type":  "content_block_stop",
					"index": index,
				}); err != nil {
					return nil, err
				}
				block.open = false
			}
		case "response.completed":
			response := asObject(payload["response"])
			state.messageID = firstNonEmpty(asString(response["id"]), state.messageID)
			state.model = firstNonEmpty(asString(response["model"]), state.model)
			if usage := initialAnthropicUsage(normalizeUsageObject(response["usage"])); usage != nil {
				state.startUsage = usage
			}
			if err := ensureAnthropicMessageStart(&out, &state); err != nil {
				return nil, err
			}
			if err := closeOpenAnthropicBlocks(&out, &state); err != nil {
				return nil, err
			}
			stopReason := firstNonEmpty(asString(response["stop_reason"]), "end_turn")
			messageDelta := map[string]any{
				"type": "message_delta",
				"delta": map[string]any{
					"stop_reason":   stopReason,
					"stop_sequence": response["stop_sequence"],
				},
			}
			if usage := normalizeUsageObject(response["usage"]); usage != nil {
				messageDelta["usage"] = usage
			}
			if err := appendSSEJSON(&out, "message_delta", messageDelta); err != nil {
				return nil, err
			}
			if err := appendSSEJSON(&out, "message_stop", map[string]any{"type": "message_stop"}); err != nil {
				return nil, err
			}
		case "response.failed", "error":
			if err := appendSSEJSON(&out, "error", anthropicErrorPayload(payload)); err != nil {
				return nil, err
			}
		}
	}

	return marshalSSEStream(out), nil
}

func ConvertMessagesStreamToResponsesStream(body []byte) ([]byte, error) {
	events, err := parseSSEStream(body)
	if err != nil {
		return nil, err
	}

	state := messagesToResponsesStreamState{
		role:   "assistant",
		blocks: make(map[int]*streamTextBlock),
	}
	out := make([]sseEvent, 0, len(events))

	for _, event := range events {
		if strings.EqualFold(event.Event, "ping") {
			continue
		}

		payload, ok, err := decodeSSEJSON(event.Data)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}

		eventType := firstNonEmpty(asString(payload["type"]), event.Event)
		switch eventType {
		case "message_start":
			message := asObject(payload["message"])
			state.responseID = firstNonEmpty(asString(message["id"]), state.responseID)
			state.itemID = firstNonEmpty(asString(message["id"]), state.itemID, state.responseID)
			state.model = firstNonEmpty(asString(message["model"]), state.model)
			state.role = firstNonEmpty(asString(message["role"]), state.role, "assistant")
			state.usage = normalizeUsageObject(message["usage"])
			if err := ensureResponsesCreated(&out, &state); err != nil {
				return nil, err
			}
			if err := ensureResponsesOutputItemAdded(&out, &state); err != nil {
				return nil, err
			}
		case "content_block_start":
			index := intValue(payload["index"])
			block := asObject(payload["content_block"])
			if !isAnthropicTextBlock(block) {
				continue
			}
			if err := ensureResponsesCreated(&out, &state); err != nil {
				return nil, err
			}
			if err := ensureResponsesOutputItemAdded(&out, &state); err != nil {
				return nil, err
			}
			if err := ensureResponsesContentPartAdded(&out, &state, index); err != nil {
				return nil, err
			}
		case "content_block_delta":
			index := intValue(payload["index"])
			delta := asObject(payload["delta"])
			if strings.TrimSpace(asString(delta["type"])) != "text_delta" {
				continue
			}
			text := asString(delta["text"])
			if text == "" {
				continue
			}
			if err := ensureResponsesCreated(&out, &state); err != nil {
				return nil, err
			}
			if err := ensureResponsesOutputItemAdded(&out, &state); err != nil {
				return nil, err
			}
			if err := ensureResponsesContentPartAdded(&out, &state, index); err != nil {
				return nil, err
			}
			state.blocks[index].text += text
			if err := appendSSEJSON(&out, "response.output_text.delta", map[string]any{
				"type":          "response.output_text.delta",
				"item_id":       state.itemID,
				"output_index":  0,
				"content_index": index,
				"delta":         text,
			}); err != nil {
				return nil, err
			}
		case "content_block_stop":
			index := intValue(payload["index"])
			if err := ensureResponsesCreated(&out, &state); err != nil {
				return nil, err
			}
			if err := ensureResponsesOutputItemAdded(&out, &state); err != nil {
				return nil, err
			}
			if err := ensureResponsesContentPartAdded(&out, &state, index); err != nil {
				return nil, err
			}
			block := state.blocks[index]
			if block.open {
				if err := appendSSEJSON(&out, "response.output_text.done", map[string]any{
					"type":          "response.output_text.done",
					"item_id":       state.itemID,
					"output_index":  0,
					"content_index": index,
					"text":          block.text,
				}); err != nil {
					return nil, err
				}
				if err := appendSSEJSON(&out, "response.content_part.done", map[string]any{
					"type":          "response.content_part.done",
					"item_id":       state.itemID,
					"output_index":  0,
					"content_index": index,
					"part": map[string]any{
						"type":        "output_text",
						"text":        block.text,
						"annotations": []any{},
					},
				}); err != nil {
					return nil, err
				}
				block.open = false
			}
		case "message_delta":
			state.stopReason = firstNonEmpty(asString(asObject(payload["delta"])["stop_reason"]), state.stopReason)
			state.stopSeq = asObject(payload["delta"])["stop_sequence"]
			if usage := normalizeUsageObject(payload["usage"]); usage != nil {
				state.usage = usage
			}
		case "message_stop":
			if err := ensureResponsesCreated(&out, &state); err != nil {
				return nil, err
			}
			if err := ensureResponsesOutputItemAdded(&out, &state); err != nil {
				return nil, err
			}
			if err := closeOpenResponsesBlocks(&out, &state); err != nil {
				return nil, err
			}
			item := buildResponsesOutputItem(state)
			if err := appendSSEJSON(&out, "response.output_item.done", map[string]any{
				"type":         "response.output_item.done",
				"output_index": 0,
				"item":         item,
			}); err != nil {
				return nil, err
			}
			response := map[string]any{
				"id":     state.responseID,
				"object": "response",
				"model":  state.model,
				"status": "completed",
				"output": []any{item},
			}
			if state.usage != nil {
				response["usage"] = cloneObject(state.usage)
			}
			if state.stopReason != "" {
				response["stop_reason"] = state.stopReason
			}
			if state.stopSeq != nil {
				response["stop_sequence"] = state.stopSeq
			}
			if err := appendSSEJSON(&out, "response.completed", map[string]any{
				"type":     "response.completed",
				"response": response,
			}); err != nil {
				return nil, err
			}
		case "error":
			if err := appendSSEJSON(&out, "error", openAIErrorPayload(payload)); err != nil {
				return nil, err
			}
		}
	}

	return marshalSSEStream(out), nil
}

func (s *responsesToMessagesStreamState) ensureBlock(index int) *streamTextBlock {
	block, ok := s.blocks[index]
	if ok {
		return block
	}
	block = &streamTextBlock{}
	s.blocks[index] = block
	return block
}

func (s *messagesToResponsesStreamState) ensureBlock(index int) *streamTextBlock {
	block, ok := s.blocks[index]
	if ok {
		return block
	}
	block = &streamTextBlock{}
	s.blocks[index] = block
	return block
}

func ensureAnthropicMessageStart(out *[]sseEvent, state *responsesToMessagesStreamState) error {
	if state.messageStarted {
		return nil
	}
	message := map[string]any{
		"type":          "message",
		"role":          firstNonEmpty(state.role, "assistant"),
		"content":       []any{},
		"stop_reason":   nil,
		"stop_sequence": nil,
	}
	if state.messageID != "" {
		message["id"] = state.messageID
	}
	if state.model != "" {
		message["model"] = state.model
	}
	if usage := initialAnthropicUsage(state.startUsage); usage != nil {
		message["usage"] = usage
	}
	if err := appendSSEJSON(out, "message_start", map[string]any{
		"type":    "message_start",
		"message": message,
	}); err != nil {
		return err
	}
	state.messageStarted = true
	return nil
}

func ensureAnthropicContentBlockStart(out *[]sseEvent, state *responsesToMessagesStreamState, index int) error {
	block := state.ensureBlock(index)
	if block.started {
		return nil
	}
	if err := appendSSEJSON(out, "content_block_start", map[string]any{
		"type":  "content_block_start",
		"index": index,
		"content_block": map[string]any{
			"type": "text",
			"text": "",
		},
	}); err != nil {
		return err
	}
	block.started = true
	block.open = true
	return nil
}

func closeOpenAnthropicBlocks(out *[]sseEvent, state *responsesToMessagesStreamState) error {
	for _, index := range orderedBlockIndexes(state.blocks) {
		block := state.blocks[index]
		if !block.open {
			continue
		}
		if err := appendSSEJSON(out, "content_block_stop", map[string]any{
			"type":  "content_block_stop",
			"index": index,
		}); err != nil {
			return err
		}
		block.open = false
	}
	return nil
}

func ensureResponsesCreated(out *[]sseEvent, state *messagesToResponsesStreamState) error {
	if state.created {
		return nil
	}
	response := map[string]any{
		"id":     state.responseID,
		"object": "response",
		"model":  state.model,
		"status": "in_progress",
		"output": []any{},
	}
	if state.usage != nil {
		response["usage"] = cloneObject(state.usage)
	}
	if err := appendSSEJSON(out, "response.created", map[string]any{
		"type":     "response.created",
		"response": response,
	}); err != nil {
		return err
	}
	state.created = true
	return nil
}

func ensureResponsesOutputItemAdded(out *[]sseEvent, state *messagesToResponsesStreamState) error {
	if state.itemAdded {
		return nil
	}
	if state.itemID == "" {
		state.itemID = state.responseID
	}
	if err := appendSSEJSON(out, "response.output_item.added", map[string]any{
		"type":         "response.output_item.added",
		"output_index": 0,
		"item": map[string]any{
			"id":      state.itemID,
			"status":  "in_progress",
			"type":    "message",
			"role":    firstNonEmpty(state.role, "assistant"),
			"content": []any{},
		},
	}); err != nil {
		return err
	}
	state.itemAdded = true
	return nil
}

func ensureResponsesContentPartAdded(out *[]sseEvent, state *messagesToResponsesStreamState, index int) error {
	block := state.ensureBlock(index)
	if block.started {
		return nil
	}
	if err := appendSSEJSON(out, "response.content_part.added", map[string]any{
		"type":          "response.content_part.added",
		"item_id":       state.itemID,
		"output_index":  0,
		"content_index": index,
		"part": map[string]any{
			"type":        "output_text",
			"text":        "",
			"annotations": []any{},
		},
	}); err != nil {
		return err
	}
	block.started = true
	block.open = true
	return nil
}

func closeOpenResponsesBlocks(out *[]sseEvent, state *messagesToResponsesStreamState) error {
	for _, index := range orderedBlockIndexes(state.blocks) {
		block := state.blocks[index]
		if !block.open {
			continue
		}
		if err := appendSSEJSON(out, "response.output_text.done", map[string]any{
			"type":          "response.output_text.done",
			"item_id":       state.itemID,
			"output_index":  0,
			"content_index": index,
			"text":          block.text,
		}); err != nil {
			return err
		}
		if err := appendSSEJSON(out, "response.content_part.done", map[string]any{
			"type":          "response.content_part.done",
			"item_id":       state.itemID,
			"output_index":  0,
			"content_index": index,
			"part": map[string]any{
				"type":        "output_text",
				"text":        block.text,
				"annotations": []any{},
			},
		}); err != nil {
			return err
		}
		block.open = false
	}
	return nil
}

func buildResponsesOutputItem(state messagesToResponsesStreamState) map[string]any {
	content := make([]any, 0, len(state.blocks))
	for _, index := range orderedBlockIndexes(state.blocks) {
		block := state.blocks[index]
		content = append(content, map[string]any{
			"type":        "output_text",
			"text":        block.text,
			"annotations": []any{},
		})
	}
	return map[string]any{
		"id":      state.itemID,
		"status":  "completed",
		"type":    "message",
		"role":    firstNonEmpty(state.role, "assistant"),
		"content": content,
	}
}

func parseSSEStream(body []byte) ([]sseEvent, error) {
	scanner := bufio.NewScanner(bytes.NewReader(body))
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	var (
		events   []sseEvent
		current  sseEvent
		dataRows []string
	)
	flush := func() {
		if current.Event == "" && len(dataRows) == 0 {
			return
		}
		current.Data = strings.Join(dataRows, "\n")
		events = append(events, current)
		current = sseEvent{}
		dataRows = nil
	}

	for scanner.Scan() {
		line := strings.TrimSuffix(scanner.Text(), "\r")
		if line == "" {
			flush()
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		field, value, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		value = strings.TrimPrefix(value, " ")
		switch field {
		case "event":
			current.Event = value
		case "data":
			dataRows = append(dataRows, value)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	flush()
	return events, nil
}

func marshalSSEStream(events []sseEvent) []byte {
	var builder strings.Builder
	for _, event := range events {
		if strings.TrimSpace(event.Event) != "" {
			builder.WriteString("event: ")
			builder.WriteString(event.Event)
			builder.WriteByte('\n')
		}
		for _, line := range strings.Split(event.Data, "\n") {
			builder.WriteString("data: ")
			builder.WriteString(line)
			builder.WriteByte('\n')
		}
		builder.WriteByte('\n')
	}
	return []byte(builder.String())
}

func appendSSEJSON(events *[]sseEvent, event string, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	*events = append(*events, sseEvent{
		Event: event,
		Data:  string(data),
	})
	return nil
}

func decodeSSEJSON(data string) (map[string]any, bool, error) {
	trimmed := strings.TrimSpace(data)
	if trimmed == "" || trimmed == "[DONE]" {
		return nil, false, nil
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(trimmed), &payload); err != nil {
		return nil, false, err
	}
	if payload == nil {
		payload = make(map[string]any)
	}
	return payload, true, nil
}

func orderedBlockIndexes(blocks map[int]*streamTextBlock) []int {
	indexes := make([]int, 0, len(blocks))
	for index := range blocks {
		indexes = append(indexes, index)
	}
	sort.Ints(indexes)
	return indexes
}

func normalizeUsageObject(value any) map[string]any {
	usage, ok := value.(map[string]any)
	if !ok || usage == nil {
		return nil
	}
	return cloneObject(usage)
}

func initialAnthropicUsage(usage map[string]any) map[string]any {
	if usage == nil {
		return nil
	}
	return cloneObject(usage)
}

func anthropicErrorPayload(payload map[string]any) map[string]any {
	message := firstNonEmpty(
		asString(asObject(payload["error"])["message"]),
		asString(asObject(asObject(payload["response"])["error"])["message"]),
		asString(payload["message"]),
		"upstream streaming error",
	)
	return map[string]any{
		"type": "error",
		"error": map[string]any{
			"type":    "api_error",
			"message": message,
		},
	}
}

func openAIErrorPayload(payload map[string]any) map[string]any {
	errorObject := asObject(payload["error"])
	message := firstNonEmpty(asString(errorObject["message"]), asString(payload["message"]), "upstream streaming error")
	out := map[string]any{
		"type":    "error",
		"message": message,
	}
	if code := firstNonEmpty(asString(errorObject["type"]), asString(errorObject["code"])); code != "" {
		out["code"] = code
	}
	return out
}

func isResponsesTextPart(part map[string]any) bool {
	partType := strings.TrimSpace(asString(part["type"]))
	return partType == "output_text" || partType == "text"
}

func isAnthropicTextBlock(block map[string]any) bool {
	return strings.TrimSpace(asString(block["type"])) == "text"
}

func asObject(value any) map[string]any {
	object, ok := value.(map[string]any)
	if !ok || object == nil {
		return map[string]any{}
	}
	return object
}

func intValue(value any) int {
	switch typed := value.(type) {
	case float64:
		return int(typed)
	case float32:
		return int(typed)
	case int:
		return typed
	case int64:
		return int(typed)
	case json.Number:
		number, err := typed.Int64()
		if err == nil {
			return int(number)
		}
	}
	return 0
}

func decodeJSONObject(body []byte) (map[string]any, error) {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	if payload == nil {
		payload = make(map[string]any)
	}
	return payload, nil
}

func convertAnthropicMessagesToResponsesInput(value any) any {
	messages, ok := value.([]any)
	if !ok {
		return value
	}
	out := make([]any, 0, len(messages))
	for _, raw := range messages {
		message, ok := raw.(map[string]any)
		if !ok {
			out = append(out, raw)
			continue
		}
		role := firstNonEmpty(asString(message["role"]), "user")
		out = append(out, convertAnthropicMessageToResponsesItems(role, message["content"])...)
	}
	return out
}

func convertResponsesInputToAnthropicMessages(value any) []any {
	switch input := value.(type) {
	case string:
		return []any{anthropicUserMessage(input)}
	case []any:
		out := make([]any, 0, len(input))
		for _, raw := range input {
			switch item := raw.(type) {
			case string:
				out = append(out, anthropicUserMessage(item))
			case map[string]any:
				role := firstNonEmpty(asString(item["role"]), "user")
				itemType := strings.TrimSpace(asString(item["type"]))
				switch itemType {
				case "function_call":
					appendAnthropicContentBlock(&out, "assistant", convertResponsesFunctionCallToAnthropicToolUse(item))
					continue
				case "function_call_output":
					appendAnthropicContentBlock(&out, "user", convertResponsesFunctionCallOutputToAnthropicToolResult(item))
					continue
				}
				if content, ok := item["content"]; ok {
					appendAnthropicMessageContent(&out, role, convertResponsesContentToAnthropicContent(content))
					continue
				}
				if text := asString(item["text"]); text != "" {
					appendAnthropicMessageContent(&out, role, []any{map[string]any{
						"type": "text",
						"text": text,
					}})
				}
			}
		}
		return out
	default:
		text := asString(value)
		if text == "" {
			return []any{}
		}
		return []any{anthropicUserMessage(text)}
	}
}

func anthropicUserMessage(text string) map[string]any {
	return map[string]any{
		"role": "user",
		"content": []any{map[string]any{
			"type": "text",
			"text": text,
		}},
	}
}

func convertAnthropicSystemToResponsesInstructions(value any) any {
	switch system := value.(type) {
	case string:
		if strings.TrimSpace(system) == "" {
			return nil
		}
		return system
	case []any:
		parts := make([]string, 0, len(system))
		for _, raw := range system {
			block, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			if strings.TrimSpace(asString(block["type"])) != "text" {
				continue
			}
			text := strings.TrimSpace(asString(block["text"]))
			if text != "" {
				parts = append(parts, text)
			}
		}
		if len(parts) == 0 {
			return nil
		}
		return strings.Join(parts, "\n\n")
	default:
		return nil
	}
}

func convertResponsesInstructionsToAnthropicSystem(value any) any {
	text := strings.TrimSpace(asString(value))
	if text == "" {
		return nil
	}
	return text
}

func convertResponsesContentToAnthropicContent(value any) any {
	switch content := value.(type) {
	case string:
		if strings.TrimSpace(content) == "" {
			return []any{}
		}
		return []any{map[string]any{
			"type": "text",
			"text": content,
		}}
	case []any:
		out := make([]any, 0, len(content))
		for _, raw := range content {
			block, ok := raw.(map[string]any)
			if !ok {
				out = append(out, raw)
				continue
			}
			blockType := strings.TrimSpace(asString(block["type"]))
			switch blockType {
			case "input_text", "output_text", "text":
				out = append(out, map[string]any{
					"type": "text",
					"text": asString(block["text"]),
				})
			default:
				out = append(out, cloneObject(block))
			}
		}
		return out
	default:
		return value
	}
}

func convertResponsesOutputToAnthropicContent(value any) []any {
	output, ok := value.([]any)
	if !ok {
		return []any{}
	}
	content := make([]any, 0)
	for _, raw := range output {
		item, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		switch strings.TrimSpace(asString(item["type"])) {
		case "message":
			if blocks, ok := convertResponsesContentToAnthropicContent(item["content"]).([]any); ok {
				content = append(content, blocks...)
			}
		case "function_call":
			content = append(content, convertResponsesFunctionCallToAnthropicToolUse(item))
		case "output_text", "text":
			text := asString(item["text"])
			if strings.TrimSpace(text) == "" {
				continue
			}
			content = append(content, map[string]any{
				"type": "text",
				"text": text,
			})
		}
	}
	return content
}

func convertAnthropicContentToResponsesOutputContent(value any) []any {
	content, ok := value.([]any)
	if !ok {
		text := strings.TrimSpace(asString(value))
		if text == "" {
			return []any{}
		}
		return []any{map[string]any{
			"type": "output_text",
			"text": text,
		}}
	}
	out := make([]any, 0, len(content))
	for _, raw := range content {
		block, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if strings.TrimSpace(asString(block["type"])) == "text" {
			out = append(out, map[string]any{
				"type": "output_text",
				"text": asString(block["text"]),
			})
			continue
		}
		out = append(out, cloneObject(block))
	}
	return out
}

func convertAnthropicMessageToResponsesItems(role string, content any) []any {
	items := make([]any, 0, 1)
	appendMessage := func(blocks []any) {
		if len(blocks) == 0 {
			return
		}
		items = append(items, map[string]any{
			"type":    "message",
			"role":    role,
			"content": blocks,
		})
	}

	switch typed := content.(type) {
	case string:
		appendMessage(convertAnthropicContentToResponsesInputBlocks(typed))
	case []any:
		blocks := make([]any, 0, len(typed))
		for _, raw := range typed {
			block, ok := raw.(map[string]any)
			if !ok {
				blocks = append(blocks, raw)
				continue
			}
			switch strings.TrimSpace(asString(block["type"])) {
			case "tool_use":
				appendMessage(blocks)
				blocks = nil
				items = append(items, convertAnthropicToolUseToResponsesFunctionCall(block))
			case "tool_result":
				appendMessage(blocks)
				blocks = nil
				items = append(items, convertAnthropicToolResultToResponsesFunctionCallOutput(block))
			default:
				if converted := convertAnthropicContentBlockToResponsesInputBlock(block); converted != nil {
					blocks = append(blocks, converted)
				}
			}
		}
		appendMessage(blocks)
	default:
		appendMessage(convertAnthropicContentToResponsesInputBlocks(typed))
	}

	return items
}

func convertAnthropicToolsToResponsesTools(value any) any {
	tools, ok := value.([]any)
	if !ok {
		return value
	}
	out := make([]any, 0, len(tools))
	for _, raw := range tools {
		tool, ok := raw.(map[string]any)
		if !ok {
			out = append(out, raw)
			continue
		}
		converted := map[string]any{
			"type": "function",
			"name": tool["name"],
		}
		if description := strings.TrimSpace(asString(tool["description"])); description != "" {
			converted["description"] = description
		}
		if schema, ok := tool["input_schema"]; ok {
			converted["parameters"] = schema
		}
		out = append(out, converted)
	}
	return out
}

func convertResponsesToolsToAnthropicTools(value any) any {
	tools, ok := value.([]any)
	if !ok {
		return value
	}
	out := make([]any, 0, len(tools))
	for _, raw := range tools {
		tool, ok := raw.(map[string]any)
		if !ok {
			out = append(out, raw)
			continue
		}
		converted := map[string]any{
			"name": tool["name"],
		}
		if description := strings.TrimSpace(asString(tool["description"])); description != "" {
			converted["description"] = description
		}
		if schema, ok := tool["parameters"]; ok {
			converted["input_schema"] = schema
		}
		out = append(out, converted)
	}
	return out
}

func convertAnthropicToolChoiceToResponsesToolChoice(value any) any {
	choice, ok := value.(map[string]any)
	if !ok {
		return value
	}
	switch strings.TrimSpace(asString(choice["type"])) {
	case "auto":
		return "auto"
	case "any":
		return "required"
	case "none":
		return "none"
	case "tool":
		return map[string]any{
			"type": "function",
			"name": choice["name"],
		}
	default:
		return value
	}
}

func convertResponsesToolChoiceToAnthropicToolChoice(value any) any {
	switch choice := value.(type) {
	case string:
		switch strings.TrimSpace(choice) {
		case "auto":
			return map[string]any{"type": "auto"}
		case "required":
			return map[string]any{"type": "any"}
		case "none":
			return map[string]any{"type": "none"}
		default:
			return value
		}
	case map[string]any:
		if strings.TrimSpace(asString(choice["type"])) == "function" {
			return map[string]any{
				"type": "tool",
				"name": choice["name"],
			}
		}
	}
	return value
}

func convertAnthropicContentToResponsesInputBlocks(value any) []any {
	switch content := value.(type) {
	case string:
		if strings.TrimSpace(content) == "" {
			return []any{}
		}
		return []any{map[string]any{
			"type": "input_text",
			"text": content,
		}}
	case []any:
		out := make([]any, 0, len(content))
		for _, raw := range content {
			block, ok := raw.(map[string]any)
			if !ok {
				out = append(out, raw)
				continue
			}
			if converted := convertAnthropicContentBlockToResponsesInputBlock(block); converted != nil {
				out = append(out, converted)
			}
		}
		return out
	default:
		text := asString(value)
		if strings.TrimSpace(text) == "" {
			return []any{}
		}
		return []any{map[string]any{
			"type": "input_text",
			"text": text,
		}}
	}
}

func convertAnthropicContentBlockToResponsesInputBlock(block map[string]any) any {
	switch strings.TrimSpace(asString(block["type"])) {
	case "text":
		return map[string]any{
			"type": "input_text",
			"text": asString(block["text"]),
		}
	case "tool_use", "tool_result":
		return nil
	default:
		return cloneObject(block)
	}
}

func convertAnthropicToolUseToResponsesFunctionCall(block map[string]any) map[string]any {
	arguments := "{}"
	if raw, ok := block["input"]; ok {
		if encoded, err := json.Marshal(raw); err == nil {
			arguments = string(encoded)
		}
	}
	return map[string]any{
		"type":      "function_call",
		"call_id":   block["id"],
		"name":      block["name"],
		"arguments": arguments,
	}
}

func convertAnthropicToolResultToResponsesFunctionCallOutput(block map[string]any) map[string]any {
	return map[string]any{
		"type":    "function_call_output",
		"call_id": block["tool_use_id"],
		"output":  convertAnthropicToolResultContentToResponsesOutput(block["content"]),
	}
}

func convertAnthropicToolResultContentToResponsesOutput(value any) any {
	switch content := value.(type) {
	case string:
		return content
	case []any:
		if len(content) == 1 {
			block, ok := content[0].(map[string]any)
			if ok && strings.TrimSpace(asString(block["type"])) == "text" {
				return asString(block["text"])
			}
		}
		return convertAnthropicContentToResponsesInputBlocks(content)
	default:
		return value
	}
}

func convertResponsesFunctionCallToAnthropicToolUse(item map[string]any) map[string]any {
	input := map[string]any{}
	arguments := strings.TrimSpace(asString(item["arguments"]))
	if arguments != "" {
		var decoded any
		if err := json.Unmarshal([]byte(arguments), &decoded); err == nil {
			if object, ok := decoded.(map[string]any); ok {
				input = object
			}
		}
	}
	return map[string]any{
		"type":  "tool_use",
		"id":    firstNonEmpty(asString(item["call_id"]), asString(item["id"])),
		"name":  item["name"],
		"input": input,
	}
}

func convertResponsesFunctionCallOutputToAnthropicToolResult(item map[string]any) map[string]any {
	return map[string]any{
		"type":        "tool_result",
		"tool_use_id": item["call_id"],
		"content":     convertResponsesFunctionCallOutputToAnthropicContent(item["output"]),
	}
}

func convertResponsesFunctionCallOutputToAnthropicContent(value any) any {
	switch output := value.(type) {
	case string:
		return output
	case []any:
		return convertResponsesContentToAnthropicContent(output)
	default:
		return value
	}
}

func appendAnthropicMessageContent(messages *[]any, role string, content any) {
	blocks, ok := content.([]any)
	if !ok || len(blocks) == 0 {
		return
	}
	last := lastAnthropicMessageWithRole(*messages, role)
	if last != nil {
		last["content"] = append(lastContentBlocks(last["content"]), blocks...)
		return
	}
	*messages = append(*messages, map[string]any{
		"role":    role,
		"content": blocks,
	})
}

func appendAnthropicContentBlock(messages *[]any, role string, block any) {
	if block == nil {
		return
	}
	last := lastAnthropicMessageWithRole(*messages, role)
	if last != nil {
		last["content"] = append(lastContentBlocks(last["content"]), block)
		return
	}
	*messages = append(*messages, map[string]any{
		"role": role,
		"content": []any{
			block,
		},
	})
}

func lastAnthropicMessageWithRole(messages []any, role string) map[string]any {
	if len(messages) == 0 {
		return nil
	}
	last, ok := messages[len(messages)-1].(map[string]any)
	if !ok {
		return nil
	}
	if firstNonEmpty(asString(last["role"]), "user") != role {
		return nil
	}
	return last
}

func lastContentBlocks(value any) []any {
	blocks, ok := value.([]any)
	if !ok {
		return []any{}
	}
	return blocks
}

func cloneObject(source map[string]any) map[string]any {
	if source == nil {
		return nil
	}
	out := make(map[string]any, len(source))
	for key, value := range source {
		out[key] = value
	}
	return out
}

func asString(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	default:
		return ""
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
