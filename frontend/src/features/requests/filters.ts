import type { LocationQuery } from 'vue-router'
import { opaqueNumericId } from './api'

const datePattern = /^\d{4}-\d{2}-\d{2}$/
const millisInDay = 86_400_000
const dateFormatters = new Map<string, Intl.DateTimeFormat>()

export function dateInZone(instant: number | Date, timezone: string): string {
  let formatter = dateFormatters.get(timezone)
  if (!formatter) {
    formatter = new Intl.DateTimeFormat('en-CA', {
      timeZone: timezone, calendar: 'iso8601', numberingSystem: 'latn', year: 'numeric', month: '2-digit', day: '2-digit',
    })
    dateFormatters.set(timezone, formatter)
  }
  const fields = formatter.formatToParts(instant)
  const get = (key: Intl.DateTimeFormatPartTypes) => fields.find(part => part.type === key)?.value ?? ''
  return `${get('year').padStart(4, '0')}-${get('month')}-${get('day')}`
}

export function validTimezone(value: string): boolean {
  if (!value || value === 'Local' || /^[+-]/.test(value)) return false
  try {
    dateInZone(0, value)
    return true
  } catch {
    return false
  }
}

export function calendarStart(day: string, timezone: string): string | null {
  if (!datePattern.test(day) || !validTimezone(timezone)) return null
  const midnightUTC = Date.parse(`${day}T00:00:00Z`)
  if (!Number.isFinite(midnightUTC) || new Date(midnightUTC).toISOString().slice(0, 10) !== day) return null
  let low = midnightUTC - 2 * millisInDay
  let high = midnightUTC + 2 * millisInDay
  while (low < high) {
    const mid = Math.floor((low + high) / 2)
    if (dateInZone(mid, timezone) < day) low = mid + 1
    else high = mid
  }
  return dateInZone(low, timezone) === day ? new Date(low).toISOString() : null
}

export function shiftCalendar(day: string, days: number): string {
  return new Date(Date.parse(`${day}T00:00:00Z`) + days * millisInDay).toISOString().slice(0, 10)
}

export type Dates = { from: string; to: string; timezone: string }
export type InstantWindow = { from?: string; to?: string; timezone: string }

export function timeWindow(fields: Dates): InstantWindow | null {
  if (!validTimezone(fields.timezone)) return null
  const from = fields.from ? calendarStart(fields.from, fields.timezone) : undefined
  const to = fields.to ? calendarStart(fields.to, fields.timezone) : undefined
  if (from === null || to === null || (from !== undefined && to !== undefined && from >= to)) return null
  return { from, to, timezone: fields.timezone }
}

export function queryText(query: LocationQuery, key: string): string {
  const value = query[key]
  return typeof value === 'string' ? value : ''
}

export function dateFields(query: LocationQuery, defaultDays = 0, now = Date.now()): Dates {
  const zone = queryText(query, 'timezone')
  const localZone = Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC'
  const timezone = validTimezone(zone) ? zone : localZone
  const today = dateInZone(now, timezone)
  function date(key: 'from' | 'to', fallback: string): string {
    const value = queryText(query, key)
    if (datePattern.test(value)) return value
    const instant = Date.parse(value)
    return Number.isFinite(instant) ? dateInZone(instant, timezone) : fallback
  }
  return {
    from: date('from', defaultDays ? shiftCalendar(today, 1 - defaultDays) : ''),
    to: date('to', defaultDays ? shiftCalendar(today, 1) : ''),
    timezone,
  }
}

export const outcomes = ['admitted', 'running', 'succeeded', 'failed', 'interrupted', 'canceled', 'unknown'] as const
export function requestFilters(query: LocationQuery) {
  const outcome = outcomes.find(candidate => candidate === queryText(query, 'outcome')) ?? ''
  const key = queryText(query, 'key_id')
  return { ...dateFields(query), outcome, model_id: queryText(query, 'model_id').slice(0, 255), key_id: opaqueNumericId(key) ? key : '' }
}

export function windowQuery(value: Dates) {
  return { from: value.from || undefined, to: value.to || undefined, timezone: value.timezone }
}
