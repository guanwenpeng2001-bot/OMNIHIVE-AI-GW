import { describe, expect, it } from 'vitest'
import { groupModelsByCategory, modelCategory, filterModelDisplay, modelCapabilitySummary, modelFieldValue } from '../modelCategories'

describe('model display categories', () => {
  it.each([
    ['text_generation', 'chat'], ['image_generation', 'image'],
    ['video_generation', 'video'], ['embedding', 'embedding'],
    ['text_to_speech', 'audio'], ['speech', 'audio'], ['transcription', 'audio'],
    ['memory', 'memory'], ['rerank', 'other'], ['realtime', 'other'],
    ['code_generation', 'other'], ['unknown', 'other']
  ])('classifies %s as %s', (task_type, expected) => {
    expect(modelCategory({ task_type })).toBe(expected)
  })
  it('uses explicit category first, then task, then output modalities, and defaults to other', () => {
    expect(modelCategory({ category: 'audio', task_type: 'text_generation' })).toBe('audio')
    expect(modelCategory({ category: 'invalid', output_modalities: ['image'] })).toBe('image')
    expect(modelCategory({ task_type: 'text_generation', output_modalities: ['text', 'audio'] })).toBe('chat')
    expect(modelCategory({ output_modalities: ['text'] })).toBe('other')
    expect(modelCategory()).toBe('other')
  })
  it('preserves every member, ID and within-category order without mutating input', () => {
    const models = [{ id: 'custom' }, { id: 'voice', category: 'audio' }, { id: 'chat', category: 'chat' }, { id: 'another' }]
    const groups = groupModelsByCategory(models, model => model)
    expect(groups.map(group => group.category)).toEqual(['chat', 'audio', 'other'])
    expect(groups[2]?.models).toEqual([models[0], models[3]])
    expect(groups.flatMap(group => group.models)).toHaveLength(models.length)
    expect(models.map(model => model.id)).toEqual(['custom', 'voice', 'chat', 'another'])
  })
})


it('uses identical natural ordering and case-insensitive ID/name search without mutating models', () => {
  const models = [{ id: 'model-10', display_name: 'Deployment' }, { id: 'model-2', display_name: 'Deployment' }, { id: 'other' }]
  expect(filterModelDisplay(models, ' DEPLOYMENT ', model => model.id, model => model).map(model => model.id)).toEqual(['model-2', 'model-10'])
  expect(filterModelDisplay(models, 'MODEL-10', model => model.id, model => model)).toEqual([models[0]])
  expect(models[0]!.id).toBe('model-10')
})

it('projects nested task limits and explicit values without inventing absent limits', () => {
  const metadata = { task_limits: { resolutions: ['1024x1024'], max_duration_seconds: 8, supports_audio: false }, embedding_dimensions: 1536 }
  const translate = (key: string) => key
  expect(modelFieldValue(metadata, 'task_limits.max_duration_seconds')).toBe(8)
  expect(modelFieldValue(metadata, 'task_limits.unknown')).toBeUndefined()
  expect(modelCapabilitySummary(metadata, translate)).toContain('1024x1024')
  expect(modelCapabilitySummary(metadata, translate)).toContain('duration: 8')
  expect(modelCapabilitySummary(metadata, translate)).toContain('dimensions: 1536')
  expect(modelCapabilitySummary(metadata, translate)).toContain('supports audio: false')
  expect(modelCapabilitySummary({}, translate)).toBe('')
  expect(modelCapabilitySummary({ task_limits: { max_duration_seconds: 0 } }, translate)).toContain('duration: 0')
})
