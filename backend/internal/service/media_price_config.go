package service

func imagePriceConfigFromAPIKey(apiKey *APIKey) *ImagePriceConfig {
	if apiKey == nil || apiKey.Group == nil {
		return nil
	}
	return &ImagePriceConfig{
		Price1K: apiKey.Group.ImagePrice1K,
		Price2K: apiKey.Group.ImagePrice2K,
		Price4K: apiKey.Group.ImagePrice4K,
	}
}

func apiKeyHasConfiguredImagePrice(apiKey *APIKey, imageSize string) bool {
	return apiKey != nil && apiKey.Group != nil && apiKey.Group.GetImagePrice(imageSize) != nil
}

func apiKeyHasConfiguredVideoPrice(apiKey *APIKey, model, resolution string) bool {
	return apiKey != nil && apiKey.Group != nil && apiKey.Group.GetVideoPriceForModel(model, resolution) != nil
}

func webSearchPricePerCallFromAPIKey(apiKey *APIKey) *float64 {
	if apiKey == nil || apiKey.Group == nil {
		return nil
	}
	return apiKey.Group.WebSearchPricePerCall
}

func groupSearchPricePer1kFromAPIKey(apiKey *APIKey) *float64 {
	if apiKey == nil || apiKey.Group == nil {
		return nil
	}
	return apiKey.Group.GetSearchPricePer1k()
}

func groupAudioPriceConfigFromAPIKey(apiKey *APIKey) *audioPriceConfig {
	if apiKey == nil || apiKey.Group == nil {
		return nil
	}
	g := apiKey.Group
	return &audioPriceConfig{
		RealtimePerMin: g.AudioRealtimePricePerMin,
		TTSPerMChars:   g.AudioTTSPricePerMillionChars,
		STTPerHour:     g.AudioSTTPricePerHour,
	}
}
