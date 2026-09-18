export const modelCategories = ['chat', 'image', 'video', 'embedding', 'audio', 'memory', 'other'] as const
export type ModelCategory = typeof modelCategories[number]

export interface ModelCategoryMetadata {
  category?: string
  task_type?: string
  output_modalities?: string[] | null
}

/** Display only: never infer authorization or rewrite model IDs from a category. */
export function modelCategory(metadata?: ModelCategoryMetadata): ModelCategory {
  if (!metadata) return 'other'
  if (metadata.category && modelCategories.includes(metadata.category as ModelCategory)) {
    return metadata.category as ModelCategory
  }
  const task = metadata.task_type?.trim().toLowerCase().replace(/-/g, '_')
  switch (task) {
    case 'chat': case 'text_generation': case 'chat_completion': case 'chat_completions': return 'chat'
    case 'image': case 'image_generation': case 'text_to_image': case 'image_editing': return 'image'
    case 'video': case 'video_generation': case 'text_to_video': case 'image_to_video': return 'video'
    case 'embedding': case 'embeddings': case 'text_embedding': case 'feature_extraction': return 'embedding'
    case 'audio': case 'audio_generation': case 'speech_generation': case 'text_to_speech':
    case 'speech_to_text': case 'transcription': case 'automatic_speech_recognition': return 'audio'
    case 'memory': return 'memory'
  }
  const output = metadata.output_modalities ?? []
  if (output.includes('video')) return 'video'
  if (output.includes('image')) return 'image'
  if (output.includes('audio')) return 'audio'
  return 'other'
}

export function groupModelsByCategory<T>(models: readonly T[], metadata: (model: T) => ModelCategoryMetadata | undefined) {
  return modelCategories.map(category => ({
    category,
    models: models.filter(model => modelCategory(metadata(model)) === category)
  })).filter(group => group.models.length > 0)
}
