export const modelCategories = ['chat', 'image', 'video', 'embedding', 'audio', 'memory', 'other'] as const
export type ModelCategory = typeof modelCategories[number]

export interface ModelCategoryMetadata {
  display_name?: string
  task_limits?: Record<string, unknown>
  embedding_dimensions?: number
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
    case 'audio': case 'audio_generation': case 'speech': case 'speech_generation': case 'text_to_speech':
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

/** One display order and search vocabulary for selected, candidate and detail rows. */
export function filterModelDisplay<T>(models: readonly T[], query: string, id: (model: T) => string, metadata: (model: T) => ModelCategoryMetadata | undefined): T[] {
  const search = query.trim().toLowerCase()
  return [...models].filter(model => `${id(model)} ${metadata(model)?.display_name ?? ''}`.toLowerCase().includes(search))
    .sort((left, right) => id(left).localeCompare(id(right), 'en', { numeric: true }))
}

/** Allocate a bounded row budget across every category before taking any slices. */
export function limitModelGroups<T>(groups: { category: ModelCategory; models: T[] }[], limit: number) {
  const counts = groups.map(() => 0)
  let remaining = Math.max(limit, groups.length)
  while (remaining > 0) {
    let advanced = false
    groups.forEach((group, index) => {
      if (remaining > 0 && counts[index]! < group.models.length) {
        counts[index] = counts[index]! + 1
        remaining--
        advanced = true
      }
    })
    if (!advanced) break
  }
  return groups.map((group, index) => ({ ...group, total: group.models.length, models: group.models.slice(0, counts[index]) }))
}

export function modelFieldValue(entry: object, field: string): unknown {
  return field.split('.').reduce<unknown>((value, key) => value && typeof value === 'object'
    ? (value as Record<string, unknown>)[key] : undefined, entry)
}

/** Preserve returned values, including explicit zero/false; never manufacture limits. */
export function modelCapabilitySummary(metadata: ModelCategoryMetadata | undefined, translate: (key: string) => string): string {
  if (!metadata) return ''
  const fields = { ...metadata.task_limits, ...(metadata.embedding_dimensions !== undefined ? { embedding_dimensions: metadata.embedding_dimensions } : {}) }
  const names: Record<string, string> = {
    resolutions: 'resolutions', max_duration_seconds: 'duration', embedding_dimensions: 'dimensions',
    dimensions: 'dimensions', max_width: 'width', max_height: 'height'
  }
  return Object.entries(fields).filter(([, value]) => value !== undefined && value !== null)
    .map(([field, value]) => `${names[field] ? translate(`capabilityCatalog.${names[field]}`) : field.replace(/_/g, ' ')}: ${
      typeof value === 'object' ? JSON.stringify(value) : String(value)}`).join(' · ')
}
