package catalog

// TaskPresentation describes approved model specifications, independently of
// installed gateway adapters or account entitlement. Unknown tasks stay unknown.
func TaskPresentation(task string) (modalities, actions []string) {
	switch task {
	case "text_generation", "code_generation", "chat", "responses", "completion":
		return []string{"text"}, []string{"text.chat"}
	case "image_generation", "image":
		return []string{"image"}, []string{"image.generate"}
	case "video_generation", "video":
		return []string{"video"}, []string{"video.generate"}
	case "audio_generation":
		return []string{"audio"}, []string{"audio.generate"}
	case "speech", "tts", "audio_speech":
		return []string{"audio"}, []string{"audio.speech"}
	case "transcription", "audio_transcription", "stt":
		return []string{"audio", "text"}, []string{"audio.transcription"}
	case "embedding":
		return []string{"embedding"}, []string{"text.embeddings"}
	case "realtime":
		return []string{"audio", "text"}, []string{"realtime"}
	case "three_dimensional_generation":
		return []string{"3d"}, []string{"3d.generate"}
	case "rerank":
		return []string{"text"}, []string{"text.rerank"}
	default:
		return nil, nil
	}
}
