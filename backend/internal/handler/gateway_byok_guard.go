package handler

import (
	"bytes"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/scope"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/gateway/execution/journal"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/byok"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/gatewayerror"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/httputil"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/requestmodel"
	"github.com/tidwall/gjson"
)

func rejectSharedFunding(c *gin.Context) bool {
	gatewayerror.Write(c, http.StatusForbidden, "permission_error", "Tenant funding requires an authorized BYOK dispatch.")
	c.Abort()
	return false
}

func checkSharedBody(body []byte) bool {
	if !bytes.HasPrefix(bytes.TrimSpace(body), []byte("{")) {
		return true
	}
	allowed := true
	gjson.ParseBytes(body).ForEach(func(k, v gjson.Result) bool {
		// Inspect only the scalar selector. Prompts, tool schemas and media
		// payloads can be large and need no second JSON decoding.
		var value any
		switch v.Type {
		case gjson.String:
			value = v.Str
		case gjson.True:
			value = true
		case gjson.False:
			value = false
		}
		allowed = byok.CheckSharedFunding(k.String(), value) == nil
		return allowed
	})
	return allowed
}

// prepareGatewayDispatch runs after authentication and route body limits.
// Preserve the bounded upload for the adapter and bind its original input to
// the execution before native conversions or async job creation.
func prepareGatewayDispatch(c *gin.Context) bool {
	if !billableGatewayRoute(c) {
		return true
	}
	body, err := httputil.ReadRequestBodyWithPrealloc(c.Request)
	if err != nil {
		status := http.StatusBadRequest
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			status = http.StatusRequestEntityTooLarge
		}
		gatewayerror.Write(c, status, "invalid_request_error", "Failed to read request body")
		c.Abort()
		return false
	}
	requestmodel.ResetRequestBody(c.Request, body)
	return prepareGatewayPayload(c, body)
}

func sharedQueryAllowed(values url.Values) bool {
	for key, entries := range values {
		for _, value := range entries {
			if byok.CheckSharedText(key, value) != nil {
				return false
			}
		}
	}
	return true
}

func prepareGatewayPayload(c *gin.Context, body []byte) bool {
	// A scoped native request may also arrive through a private router.
	// Its authenticated scope requires the same durable store as public routes.
	if _, scoped := scope.From(c.Request.Context()); scoped && journal.From(c.Request.Context()) == nil {
		gatewayerror.Write(c, http.StatusServiceUnavailable, "server_error", "Execution journal unavailable")
		c.Abort()
		return false
	}
	if c.GetBool("gateway_dispatch_input_checked") {
		return true
	}
	if byok.CheckSharedHeaders(c.Request.Header) != nil || !sharedQueryAllowed(c.Request.URL.Query()) {
		return rejectSharedFunding(c)
	}
	media, params, _ := mime.ParseMediaType(c.GetHeader("Content-Type"))
	switch media {
	case "multipart/form-data":
		reader := multipart.NewReader(bytes.NewReader(body), params["boundary"])
		for {
			part, err := reader.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				gatewayerror.Write(c, 400, "invalid_request_error", "Invalid multipart payload")
				c.Abort()
				return false
			}
			name := part.FormName()
			// Unrecognized fields never affect funding. Only inspect known
			// selectors and limit their text without reading media into memory.
			if byok.CheckSharedFunding(name, nil) == nil {
				continue
			}
			raw, err := io.ReadAll(io.LimitReader(part, 257))
			if err != nil || len(raw) > 256 || byok.CheckSharedText(name, string(raw)) != nil {
				return rejectSharedFunding(c)
			}
		}
	case "application/x-www-form-urlencoded":
		values, err := url.ParseQuery(string(body))
		if err != nil || !sharedQueryAllowed(values) {
			return rejectSharedFunding(c)
		}
	}
	if !checkSharedBody(body) {
		return rejectSharedFunding(c)
	}
	key := c.GetHeader("Idempotency-Key")
	if key == "" {
		c.Set("gateway_dispatch_input_checked", true)
		return true
	}
	var reg *journal.Registration
	var err error
	switch {
	case c.Request.Method == http.MethodGet:
		// Responses WS turns have their own frame payload/identity.
		if strings.HasSuffix(c.Request.URL.Path, "/responses") {
			return true
		}
		reg, err = journal.RegisterBytes(key, []byte(c.Request.URL.Query().Encode()))
	case media == "application/json" || media == "" && len(body) != 0 && bytes.HasPrefix(bytes.TrimSpace(body), []byte("{")):
		reg, err = journal.Register(key, body)
	default:
		reg, err = journal.RegisterBytes(key, body)
	}
	if err != nil {
		gatewayerror.Write(c, http.StatusBadRequest, "invalid_request_error", err.Error())
		c.Abort()
		return false
	}
	c.Request = c.Request.WithContext(journal.WithRegistration(c.Request.Context(), reg, c.Request.URL.Path))
	c.Set("gateway_dispatch_input_checked", true)
	return true
}
