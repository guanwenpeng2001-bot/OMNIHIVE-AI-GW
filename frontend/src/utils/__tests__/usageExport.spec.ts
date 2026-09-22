import { describe, expect, it, vi, beforeEach } from 'vitest'

const { get, saveAs } = vi.hoisted(() => ({
  get: vi.fn(),
  saveAs: vi.fn(),
}))

vi.mock('@/api/client', () => ({
  apiClient: { get },
}))

vi.mock('file-saver', () => ({ saveAs }))

import {
  USAGE_EXPORT_COLUMNS_HEADER,
  downloadUsageCsv,
  escapeUsageExportCell,
  fetchUsageExportCsv,
  fetchUsageExportRows,
  localizeUsageCsvHeader,
  usageExportFileName,
} from '../usageExport'

// jsdom 的 Blob 没有 text()；导出客户端只依赖 text()，测试用等价的鸭子类型替身。
const csvBlob = (content: string) => ({ text: async () => content }) as unknown as Blob

describe('usage export client', () => {
  beforeEach(() => {
    get.mockReset()
    saveAs.mockReset()
  })

  it('fetches the server CSV with a single request and reads the canonical columns', async () => {
    get.mockResolvedValue({
      data: csvBlob('\uFEFFtime,model\n2026-09-22 10:00:00,gpt-5.1\n'),
      headers: { [USAGE_EXPORT_COLUMNS_HEADER]: 'time,model' },
    })

    const result = await fetchUsageExportCsv({
      endpoint: '/usage/export',
      params: { start_date: '2026-09-01', end_date: '2026-09-22' },
    })

    expect(get).toHaveBeenCalledTimes(1)
    expect(get).toHaveBeenCalledWith('/usage/export', expect.objectContaining({
      params: { start_date: '2026-09-01', end_date: '2026-09-22', format: 'csv' },
      responseType: 'blob',
    }))
    expect(result.columns).toEqual(['time', 'model'])
    expect(await result.blob.text()).toContain('gpt-5.1')
  })

  it('fetches typed export rows with a single request for the Excel writer', async () => {
    get.mockResolvedValue({ data: { items: [{ model: 'gpt-5.1', input_tokens: 3 }], total: 1 } })

    const rows = await fetchUsageExportRows<{ model: string }>({
      endpoint: '/admin/usage/export',
      params: { billing_mode: 'image' },
    })

    expect(get).toHaveBeenCalledTimes(1)
    expect(get).toHaveBeenCalledWith('/admin/usage/export', expect.objectContaining({
      params: { billing_mode: 'image', format: 'json' },
    }))
    expect(rows).toEqual([{ model: 'gpt-5.1', input_tokens: 3 }])
  })

  it('localizes the server header row without touching the body', () => {
    const csv = '\uFEFFtime,model,billing_mode\n2026-09-22 10:00:00,gpt-5.1,image\n'
    const localized = localizeUsageCsvHeader(csv, ['time', 'model', 'billing_mode'], ['时间', '模型', '计费模式'])

    expect(localized).toBe('\uFEFF时间,模型,计费模式\n2026-09-22 10:00:00,gpt-5.1,image\n')
  })

  it('keeps the server header when the label count does not match the contract', () => {
    const csv = '\uFEFFtime,model\n2026-09-22 10:00:00,gpt-5.1\n'
    expect(localizeUsageCsvHeader(csv, ['time', 'model'], ['时间'])).toBe(csv)
    expect(localizeUsageCsvHeader(csv, [], ['时间', '模型'])).toBe(csv)
  })

  it('keeps the server header when the stream does not start with the declared columns', () => {
    const csv = '\uFEFFsomething,else\nvalue,value\n'
    expect(localizeUsageCsvHeader(csv, ['time', 'model'], ['时间', '模型'])).toBe(csv)
  })

  it('escapes labels that would break the CSV row', () => {
    const csv = '\uFEFFtime,model\n2026-09-22 10:00:00,gpt-5.1\n'
    const localized = localizeUsageCsvHeader(csv, ['time', 'model'], ['时间', 'Model, requested'])
    expect(localized).toBe('\uFEFF时间,"Model, requested"\n2026-09-22 10:00:00,gpt-5.1\n')
  })

  it('neutralizes spreadsheet formulas like the server does', () => {
    expect(escapeUsageExportCell('=SUM(A1)')).toBe('"\'=SUM(A1)"')
    expect(escapeUsageExportCell('+1')).toBe('"\'+1"')
    expect(escapeUsageExportCell('-1')).toBe('"\'-1"')
    expect(escapeUsageExportCell('@cmd')).toBe('"\'@cmd"')
    expect(escapeUsageExportCell('plain')).toBe('plain')
    expect(escapeUsageExportCell('has,comma')).toBe('"has,comma"')
    expect(escapeUsageExportCell('say "hi"')).toBe('"say ""hi"""')
  })

  it('downloads the localized CSV once with the requested file name', async () => {
    // jsdom 的 Blob 不能读回内容；这里只捕获交给 saveAs 的字节。
    let savedCsvContent = ''
    const OriginalBlob = globalThis.Blob
    vi.stubGlobal('Blob', vi.fn((parts: BlobPart[]) => {
      savedCsvContent = parts.map((part) => String(part)).join('')
      return new OriginalBlob(parts)
    }))
    get.mockResolvedValue({
      data: csvBlob('\uFEFFtime,model\n2026-09-22 10:00:00,gpt-5.1\n'),
      headers: { [USAGE_EXPORT_COLUMNS_HEADER]: 'time,model' },
    })

    await downloadUsageCsv({
      endpoint: '/usage/export',
      params: {},
      labels: ['时间', '模型'],
      fileName: 'usage_2026-09-01_to_2026-09-22.csv',
    })

    expect(get).toHaveBeenCalledTimes(1)
    expect(saveAs).toHaveBeenCalledTimes(1)
    const [, fileName] = saveAs.mock.calls[0]
    expect(fileName).toBe('usage_2026-09-01_to_2026-09-22.csv')
    expect(savedCsvContent).toBe('\uFEFF时间,模型\n2026-09-22 10:00:00,gpt-5.1\n')
    vi.unstubAllGlobals()
  })

  it('keeps the range in the export file name', () => {
    expect(usageExportFileName('2026-09-01', '2026-09-22', 'csv')).toBe('usage_2026-09-01_to_2026-09-22.csv')
    expect(usageExportFileName('', '', 'xlsx')).toBe('usage_export.xlsx')
  })
})
