package service

import "time"

// nativeModelCreatedAt accepts only explicit provider creation facts. Discovery,
// verification, account creation and registry load times are not model creation.
func nativeModelCreatedAt(entry upstreamModelCapabilityEntry) string {
	if entry.CreatedAt != "" {
		if created, err := time.Parse(time.RFC3339Nano, entry.CreatedAt); err == nil {
			return created.UTC().Format(time.RFC3339Nano)
		}
	}
	if entry.Created != nil && *entry.Created > 0 && *entry.Created <= 253402300799 {
		return time.Unix(*entry.Created, 0).UTC().Format(time.RFC3339)
	}
	return ""
}

// visibleModelMetadata fills identity facts from the already frozen approved
// registry without authorizing a candidate or doing an upstream read. Native
// facts persisted in the account specification take precedence.
func visibleModelMetadata(registry *ModelRegistrySnapshot, profile, model string, metadata UpstreamModelMetadata) UpstreamModelMetadata {
	metadata = cloneUpstreamModelMetadata(metadata)
	if registry != nil && profile != "" {
		approved := lookupCapabilityMetadata(registry.CapabilitySnapshot(), profile, model).Metadata
		for _, name := range []string{"CreatedAt", "ModelVersion"} {
			if mergeMetadataField(&metadata, &approved, name) {
				if metadata.FieldEvidence == nil {
					metadata.FieldEvidence = make(map[string]ModelCapabilityEvidence)
				}
				field := "created_at"
				if name == "ModelVersion" {
					field = "model_version"
				}
				metadata.FieldEvidence[field] = approved.FieldEvidence[field]
			}
		}
	}
	metadata.CreatedAtStatus = "unavailable"
	if metadata.CreatedAt != "" {
		if created, err := time.Parse(time.RFC3339Nano, metadata.CreatedAt); err == nil {
			metadata.CreatedAt = created.UTC().Format(time.RFC3339Nano)
			metadata.CreatedAtStatus = "available"
		} else {
			metadata.CreatedAt = ""
			delete(metadata.FieldEvidence, "created_at")
		}
	}
	metadata.ModelVersionStatus = "unavailable"
	if metadata.ModelVersion != "" {
		metadata.ModelVersionStatus = "available"
	}
	return metadata
}
