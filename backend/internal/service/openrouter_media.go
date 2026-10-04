package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"log/slog"
	"math"
	"mime"
	"mime/multipart"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
	_ "golang.org/x/image/webp"
)

// Wire schemas checked 2026-10-02 at the official OpenRouter API reference:
// images/generate-an-image, tts/create-speech, stt/create-transcription,
// embeddings/submit-an-embedding-request and rerank/submit-a-rerank-request.
var openRouterMediaPaths = map[string]string{
	"image.generate": "/v1/images", "audio.tts": "/v1/audio/speech",
	"audio.stt": "/v1/audio/transcriptions", "embeddings": "/v1/embeddings", "rerank": "/v1/rerank",
}

func openRouterCounter(body []byte, path string) (int, bool) {
	value := gjson.GetBytes(body, path)
	return int(value.Int()), value.Type == gjson.Number && value.Float() >= 0 && value.Float() == float64(value.Int()) && value.Float() <= float64(math.MaxInt32)
}

func validateOpenRouterMultipart(body []byte, contentType string) (string, error) {
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil || mediaType != "multipart/form-data" || params["boundary"] == "" {
		return "", fmt.Errorf("transcriptions require multipart/form-data")
	}
	reader := multipart.NewReader(bytes.NewReader(body), params["boundary"])
	model := ""
	files := 0
	fields := map[string]bool{}
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
		name := part.FormName()
		if name == "file" {
			if part.FileName() == "" {
				return "", fmt.Errorf("file requires a filename")
			}
			files++
			size, err := io.Copy(io.Discard, io.LimitReader(part, 25<<20+1))
			if err != nil || size == 0 || size > 25<<20 {
				return "", fmt.Errorf("file must contain at most 25 MB")
			}
			continue
		}
		if part.FileName() != "" {
			return "", fmt.Errorf("unsupported upload field")
		}
		switch name {
		case "model", "language", "temperature", "response_format", "timestamp_granularities[]", "timestamp_granularities", "prompt":
		default:
			return "", fmt.Errorf("unsupported transcription field %s", name)
		}
		if fields[name] && name != "timestamp_granularities[]" {
			return "", fmt.Errorf("duplicate transcription field %s", name)
		}
		fields[name] = true
		value, err := io.ReadAll(io.LimitReader(part, 1<<20+1))
		if err != nil || len(value) > 1<<20 {
			return "", fmt.Errorf("invalid transcription field")
		}
		if name == "model" {
			model = strings.TrimSpace(string(value))
		}
		if name == "response_format" && string(value) != "json" && string(value) != "verbose_json" {
			return "", fmt.Errorf("OpenRouter transcription supports json and verbose_json")
		}
	}
	if model == "" || files != 1 {
		return "", fmt.Errorf("one file and a model are required")
	}
	return model, nil
}

func prepareOpenRouterMedia(action string, body []byte) ([]byte, error) {
	if !gjson.ValidBytes(body) || !gjson.ParseBytes(body).IsObject() {
		return nil, fmt.Errorf("a JSON object is required")
	}
	if model := gjson.GetBytes(body, "model"); model.Type != gjson.String || strings.TrimSpace(model.String()) == "" {
		return nil, fmt.Errorf("model must be a nonempty string")
	}
	if stream := gjson.GetBytes(body, "stream"); stream.Exists() && stream.Bool() {
		return nil, fmt.Errorf("SSE is not supported by this media adapter")
	}
	switch action {
	case "image.generate":
		if gjson.GetBytes(body, "prompt").Type != gjson.String || strings.TrimSpace(gjson.GetBytes(body, "prompt").String()) == "" {
			return nil, fmt.Errorf("prompt is required")
		}
		if format := gjson.GetBytes(body, "response_format"); format.Exists() {
			if format.String() != "b64_json" {
				return nil, fmt.Errorf("OpenRouter image output uses b64_json")
			}
			body, _ = sjson.DeleteBytes(body, "response_format")
		}
		if format := gjson.GetBytes(body, "output_format").String(); format != "" && format != "png" && format != "jpeg" && format != "webp" {
			return nil, fmt.Errorf("unsupported raster output format")
		}
		if count := gjson.GetBytes(body, "n"); count.Exists() && (count.Type != gjson.Number || count.Int() < 1 || count.Int() > 10 || count.Float() != float64(count.Int())) {
			return nil, fmt.Errorf("n must be an integer from 1 to 10")
		}
	case "audio.tts":
		if input := gjson.GetBytes(body, "input"); input.Type != gjson.String || input.String() == "" {
			return nil, fmt.Errorf("input text is required")
		}
		if format := gjson.GetBytes(body, "response_format").String(); format != "" && format != "mp3" && format != "pcm" {
			return nil, fmt.Errorf("OpenRouter speech supports mp3 or pcm")
		}
	case "embeddings":
		input := gjson.GetBytes(body, "input")
		if input.Type != gjson.String && !input.IsArray() {
			return nil, fmt.Errorf("text or token embedding input is required")
		}
		if input.Type == gjson.String && input.String() == "" || input.IsArray() && len(input.Array()) == 0 {
			return nil, fmt.Errorf("input is empty")
		}
		if dimensions := gjson.GetBytes(body, "dimensions"); dimensions.Exists() && (dimensions.Type != gjson.Number || dimensions.Int() < 1 || dimensions.Float() != float64(dimensions.Int())) {
			return nil, fmt.Errorf("dimensions must be a positive integer")
		}
		if format := gjson.GetBytes(body, "encoding_format").String(); format != "" && format != "float" && format != "base64" {
			return nil, fmt.Errorf("unsupported embedding encoding")
		}
	case "rerank":
		if query := gjson.GetBytes(body, "query"); query.Type != gjson.String || query.String() == "" {
			return nil, fmt.Errorf("query is required")
		}
		documents := gjson.GetBytes(body, "documents")
		if !documents.IsArray() || len(documents.Array()) == 0 {
			return nil, fmt.Errorf("documents are required")
		}
		if count := gjson.GetBytes(body, "top_n"); count.Exists() && (count.Type != gjson.Number || count.Int() < 1 || count.Float() != float64(count.Int())) {
			return nil, fmt.Errorf("top_n must be a positive integer")
		}
	default:
		return nil, fmt.Errorf("unsupported OpenRouter media action")
	}
	return body, nil
}

func validateOpenRouterVectors(body, request []byte) error {
	data := gjson.GetBytes(body, "data")
	if !data.IsArray() || len(data.Array()) == 0 {
		return fmt.Errorf("embedding vectors are missing")
	}
	expected := 1
	input := gjson.GetBytes(request, "input")
	if input.IsArray() {
		entries := input.Array()
		if len(entries) > 0 && entries[0].Type != gjson.Number {
			expected = len(entries)
		}
	}
	if len(data.Array()) != expected {
		return fmt.Errorf("embedding result count differs from input")
	}
	dimensions := 0
	seen := map[int64]bool{}
	for _, entry := range data.Array() {
		index := entry.Get("index")
		if index.Type != gjson.Number || index.Int() < 0 || index.Int() >= int64(expected) || index.Float() != float64(index.Int()) || seen[index.Int()] {
			return fmt.Errorf("invalid embedding index")
		}
		seen[index.Int()] = true
		vector := entry.Get("embedding")
		size := 0
		if vector.IsArray() {
			for _, component := range vector.Array() {
				if component.Type != gjson.Number || math.IsNaN(component.Float()) || math.IsInf(component.Float(), 0) {
					return fmt.Errorf("nonfinite embedding")
				}
				size++
			}
		} else if vector.Type == gjson.String {
			encoded, err := base64.StdEncoding.DecodeString(vector.String())
			if err != nil || len(encoded)%4 != 0 {
				return fmt.Errorf("invalid base64 embedding")
			}
			size = len(encoded) / 4
			for offset := 0; offset < len(encoded); offset += 4 {
				number := math.Float32frombits(binary.LittleEndian.Uint32(encoded[offset : offset+4]))
				if math.IsInf(float64(number), 0) || math.IsNaN(float64(number)) {
					return fmt.Errorf("nonfinite embedding")
				}
			}
		} else {
			return fmt.Errorf("invalid embedding vector")
		}
		if size == 0 || dimensions != 0 && dimensions != size {
			return fmt.Errorf("inconsistent embedding dimensions")
		}
		dimensions = size
	}
	if requested := gjson.GetBytes(request, "dimensions").Int(); requested > 0 && int64(dimensions) != requested {
		return fmt.Errorf("embedding dimensions differ from request")
	}
	return nil
}

func (gateway *OpenAIGatewayService) ForwardOpenRouterMedia(ctx context.Context, request *gin.Context, account *Account, action string, body []byte, contentType string) (*OpenAIForwardResult, error) {
	started := time.Now()
	path, ok := openRouterMediaPaths[action]
	if !ok || account == nil || account.Platform != PlatformOpenRouter || account.Type != AccountTypeAPIKey {
		return nil, fmt.Errorf("OpenRouter API-key media account required")
	}
	original := strings.TrimSpace(gjson.GetBytes(body, "model").String())
	var err error
	if action == "audio.stt" {
		original, err = validateOpenRouterMultipart(body, contentType)
	} else {
		body, err = prepareOpenRouterMedia(action, body)
		contentType = "application/json"
	}
	if err != nil || original == "" {
		if err == nil {
			err = fmt.Errorf("model is required")
		}
		writeOpenAIEmbeddingsError(request, 400, "invalid_request_error", err.Error())
		return nil, err
	}
	model := resolveOpenAIForwardModel(account, original, "")
	SetOpsUpstreamModel(request, model)
	var outbound []byte
	if action == "audio.stt" {
		outbound, contentType, err = replaceMistralMultipartModel(body, contentType, model)
	} else {
		outbound = ReplaceModelInBody(body, model)
	}
	if err != nil {
		return nil, err
	}
	base, err := gateway.validateUpstreamBaseURL(account.GetOpenAIBaseURL())
	if err != nil {
		return nil, err
	}
	token, _, err := gateway.getRequestCredential(ctx, request, account)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(token) == "" {
		return nil, fmt.Errorf("OpenRouter key is missing")
	}
	response, err := gateway.sendMistralActionRequest(WithHTTPUpstreamRedirectsDisabled(ctx), request, account, http.MethodPost, account.buildOpenAIEndpointURL(base, path), outbound, contentType, "*/*", token, path)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := response.Body.Close(); err != nil {
			slog.Debug("provider response cleanup failed")
		}
	}()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return gateway.handleMistralActionHTTPError(ctx, request, account, response, model)
	}
	raw, err := ReadUpstreamResponseBody(response.Body, gateway.cfg, request, openAITooLargeError)
	if err != nil {
		return nil, err
	}
	result := &OpenAIForwardResult{RequestID: response.Header.Get("x-request-id"), UpstreamHeaders: response.Header, Model: original, BillingModel: model, UpstreamModel: model, Duration: time.Since(started)}
	invalid := func(reason string) (*OpenAIForwardResult, error) {
		writeOpenAIEmbeddingsError(request, 502, "upstream_error", reason)
		return nil, fmt.Errorf("%s", reason)
	}
	if action == "audio.tts" {
		mediaType, _, parseErr := mime.ParseMediaType(response.Header.Get("Content-Type"))
		if parseErr != nil || !strings.HasPrefix(mediaType, "audio/") || len(raw) == 0 {
			return invalid("OpenRouter speech returned no audio")
		}
		result.AudioUsage = &AudioUsage{Mode: "tts", DurationOrUnits: float64(utf8.RuneCountInString(gjson.GetBytes(body, "input").String())) / 1_000_000}
		request.Data(200, mediaType, raw)
		return result, nil
	}
	if !gjson.ValidBytes(raw) || gjson.GetBytes(raw, "error").Exists() {
		return invalid("Invalid OpenRouter media response")
	}
	switch action {
	case "audio.stt":
		seconds := gjson.GetBytes(raw, "usage.seconds")
		if gjson.GetBytes(raw, "text").Type != gjson.String || seconds.Type != gjson.Number || !finitePositive(seconds.Float()) {
			return invalid("Transcription text or measured audio usage is missing")
		}
		result.AudioUsage = &AudioUsage{Mode: "stt", DurationOrUnits: seconds.Float() / 3600}
	case "embeddings":
		if err := validateOpenRouterVectors(raw, body); err != nil {
			return invalid(err.Error())
		}
		tokens, known := openRouterCounter(raw, "usage.prompt_tokens")
		if !known {
			return invalid("Embedding token usage is unknown")
		}
		result.Usage.InputTokens = tokens
	case "rerank":
		rows := gjson.GetBytes(raw, "results")
		if !rows.IsArray() || len(rows.Array()) == 0 {
			return invalid("Rerank results are missing")
		}
		seen := map[int64]bool{}
		count := len(gjson.GetBytes(body, "documents").Array())
		for _, row := range rows.Array() {
			index := row.Get("index")
			score := row.Get("relevance_score")
			if index.Type != gjson.Number || index.Int() < 0 || index.Int() >= int64(count) || index.Float() != float64(index.Int()) || seen[index.Int()] || score.Type != gjson.Number || math.IsNaN(score.Float()) || math.IsInf(score.Float(), 0) {
				return invalid("Invalid rerank index or relevance score")
			}
			seen[index.Int()] = true
		}
		tokens, known := openRouterCounter(raw, "usage.total_tokens")
		if !known {
			return invalid("Rerank token quantity is unknown")
		}
		result.Usage.InputTokens = tokens
	case "image.generate":
		data := gjson.GetBytes(raw, "data")
		if !data.IsArray() || len(data.Array()) == 0 {
			return invalid("Image result is empty")
		}
		outputSizes := []string{}
		for _, entry := range data.Array() {
			decoded, decodeErr := base64.StdEncoding.DecodeString(entry.Get("b64_json").String())
			if decodeErr != nil || len(decoded) == 0 {
				return invalid("Image result has invalid base64")
			}
			dimensions, _, err := image.DecodeConfig(bytes.NewReader(decoded))
			if err != nil {
				return invalid("Image result is not a supported raster image")
			}
			outputSizes = append(outputSizes, fmt.Sprintf("%dx%d", dimensions.Width, dimensions.Height))
		}
		if !mistralChatUsageKnown(raw) {
			return invalid("Image usage is unknown")
		}
		result.Usage.InputTokens = int(gjson.GetBytes(raw, "usage.prompt_tokens").Int())
		result.Usage.OutputTokens = int(gjson.GetBytes(raw, "usage.completion_tokens").Int())
		result.ImageCount = len(data.Array())
		resolution := ResolveImageBillingSize(gjson.GetBytes(body, "size").String(), outputSizes)
		result.ImageSize, result.ImageSizeSource = resolution.BillingSize, resolution.Source
		result.ImageInputSize, result.ImageOutputSize = resolution.InputSize, resolution.OutputSize
		result.ImageOutputSizes, result.ImageSizeBreakdown = outputSizes, resolution.Breakdown
	}
	writeOpenAIEmbeddingsUpstreamResponse(request, response, raw, gateway.responseHeaderFilter)
	return result, nil
}
