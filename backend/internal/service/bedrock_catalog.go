package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

// BedrockFoundationModel is a regional catalogue fact, never proof of Invoke permission.
// Pointer/slice fields retain unknown versus explicit false/empty values.
type BedrockFoundationModel struct {
	ModelID                    string   `json:"modelId"`
	ModelARN                   string   `json:"modelArn"`
	ModelName                  string   `json:"modelName"`
	ProviderName               string   `json:"providerName"`
	InputModalities            []string `json:"inputModalities"`
	OutputModalities           []string `json:"outputModalities"`
	ResponseStreamingSupported *bool    `json:"responseStreamingSupported"`
	InferenceTypesSupported    []string `json:"inferenceTypesSupported"`
	CustomizationsSupported    []string `json:"customizationsSupported"`
	ModelLifecycle             struct {
		Status string `json:"status"`
	} `json:"modelLifecycle"`
}

type BedrockFoundationCatalog struct {
	Region string
	Models []BedrockFoundationModel
}

type BedrockModelAvailability struct {
	ModelID               string `json:"modelId"`
	AgreementAvailability struct {
		Status       string `json:"status"`
		ErrorMessage string `json:"errorMessage"`
	} `json:"agreementAvailability"`
	AuthorizationStatus     string `json:"authorizationStatus"`
	EntitlementAvailability string `json:"entitlementAvailability"`
	RegionAvailability      string `json:"regionAvailability"`
}

type BedrockInferenceProfile struct {
	ID          string          `json:"inferenceProfileId"`
	ARN         string          `json:"inferenceProfileArn"`
	Name        string          `json:"inferenceProfileName"`
	Description string          `json:"description"`
	Status      string          `json:"status"`
	Type        string          `json:"type"`
	CreatedAt   json.RawMessage `json:"createdAt"`
	UpdatedAt   json.RawMessage `json:"updatedAt"`
	Models      []struct {
		ModelARN string `json:"modelArn"`
	} `json:"models"`
}

type BedrockProfilePage struct {
	Profiles  []BedrockInferenceProfile `json:"inferenceProfileSummaries"`
	NextToken string                    `json:"nextToken"`
}

var bedrockRegionPattern = regexp.MustCompile(`^[a-z]{2}(?:-[a-z]+)+-[0-9]+$`)

func bedrockEndpoint(region string, runtime bool) (string, error) {
	if !bedrockRegionPattern.MatchString(region) {
		return "", fmt.Errorf("invalid Bedrock region")
	}
	service := "bedrock"
	if runtime {
		service = "bedrock-runtime"
	}
	suffix := "amazonaws.com"
	if strings.HasPrefix(region, "cn-") {
		suffix += ".cn"
	}
	return "https://" + service + "." + region + "." + suffix, nil
}

func bedrockEscapeTarget(target string) string {
	return strings.ReplaceAll(url.PathEscape(target), ":", "%3A")
}

// BuildBedrockCatalogRequest deliberately requires IAM credentials even when the
// same account also has a runtime API key. The caller uses the existing transport.
func BuildBedrockCatalogRequest(ctx context.Context, account *Account, action, modelID, nextToken string) (*http.Request, error) {
	if account == nil {
		return nil, fmt.Errorf("Bedrock account is required")
	}
	region := bedrockRuntimeRegion(account)
	endpoint, err := bedrockEndpoint(region, false)
	if err != nil {
		return nil, err
	}
	var path string
	switch action {
	case "foundation-models":
		if modelID != "" || nextToken != "" {
			return nil, fmt.Errorf("foundation list has no model or cursor")
		}
		path = "/foundation-models"
	case "availability":
		if strings.TrimSpace(modelID) == "" || nextToken != "" {
			return nil, fmt.Errorf("availability requires a model and no cursor")
		}
		path = "/foundation-model-availability/" + bedrockEscapeTarget(modelID)
	case "inference-profiles":
		if modelID != "" {
			return nil, fmt.Errorf("profile list has no model filter")
		}
		query := url.Values{"maxResults": {"100"}}
		if nextToken != "" {
			query.Set("nextToken", nextToken)
		}
		path = "/inference-profiles?" + query.Encode()
	default:
		return nil, fmt.Errorf("unsupported Bedrock catalogue action")
	}
	signer, err := NewBedrockSignerFromAccount(account)
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+path, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/json")
	if err := signer.SignRequest(ctx, request, nil); err != nil {
		return nil, err
	}
	return request, nil
}

func ParseBedrockFoundationCatalog(region string, body []byte) (*BedrockFoundationCatalog, error) {
	if _, err := bedrockEndpoint(region, false); err != nil {
		return nil, err
	}
	var response struct {
		Models []BedrockFoundationModel `json:"modelSummaries"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, err
	}
	if response.Models == nil {
		return nil, fmt.Errorf("missing Bedrock modelSummaries")
	}
	for _, model := range response.Models {
		if model.ModelID == "" {
			return nil, fmt.Errorf("missing Bedrock modelId")
		}
	}
	return &BedrockFoundationCatalog{Region: region, Models: response.Models}, nil
}

func ParseBedrockModelAvailability(body []byte) (*BedrockModelAvailability, error) {
	var availability BedrockModelAvailability
	if err := json.Unmarshal(body, &availability); err != nil {
		return nil, err
	}
	if availability.ModelID == "" {
		return nil, fmt.Errorf("missing Bedrock availability modelId")
	}
	return &availability, nil
}

// CollectBedrockProfiles publishes no partial result: failed/repeated-token pages
// return nil so callers can preserve their previous complete regional snapshot.
func CollectBedrockProfiles(ctx context.Context, fetchPage func(context.Context, string) ([]byte, error)) ([]BedrockInferenceProfile, error) {
	profiles := make([]BedrockInferenceProfile, 0)
	seenTokens := make(map[string]bool)
	nextToken := ""
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		body, err := fetchPage(ctx, nextToken)
		if err != nil {
			return nil, err
		}
		var page BedrockProfilePage
		if err := json.Unmarshal(body, &page); err != nil {
			return nil, err
		}
		if page.Profiles == nil {
			return nil, fmt.Errorf("missing Bedrock inferenceProfileSummaries")
		}
		for _, profile := range page.Profiles {
			if profile.ID == "" || profile.ARN == "" {
				return nil, fmt.Errorf("missing Bedrock profile identity")
			}
		}
		profiles = append(profiles, page.Profiles...)
		if page.NextToken == "" {
			return profiles, nil
		}
		if seenTokens[page.NextToken] {
			return nil, fmt.Errorf("repeated Bedrock profile cursor")
		}
		seenTokens[page.NextToken] = true
		nextToken = page.NextToken
	}
}
