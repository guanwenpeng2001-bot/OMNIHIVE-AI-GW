import { describe, expect, it } from 'vitest'
import { groupModelsByCategory, modelCategory } from '../modelCategories'

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
