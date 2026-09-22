import { apiClient } from '@/api/client'

/**
 * 用量导出的共享客户端。
 *
 * 服务端按稳定 cursor + 冻结快照范围流式产出 CSV，浏览器只发一次导出请求，
 * 不再按页循环拉取。表头使用服务端返回的规范列名，展示层用界面语言替换，
 * 因此列顺序始终由服务端单点决定，前端不会各自维护一份顺序。
 */
export const ADMIN_USAGE_EXPORT_ENDPOINT = '/admin/usage/export'
export const USER_USAGE_EXPORT_ENDPOINT = '/usage/export'

/** 服务端在导出响应里回传的规范列顺序（响应头，逗号分隔）。 */
export const USAGE_EXPORT_COLUMNS_HEADER = 'x-usage-export-columns'

export interface UsageExportRequest {
  endpoint: string
  /** 与用量列表完全相同的查询参数（调用方直接复用列表参数对象）。 */
  params?: object
  signal?: AbortSignal
}

export interface UsageExportCsv {
  blob: Blob
  /** 服务端声明的规范列顺序；缺失时为空数组。 */
  columns: string[]
}

const readExportColumns = (headers: unknown): string[] => {
  const raw = (headers as Record<string, unknown> | undefined)?.[USAGE_EXPORT_COLUMNS_HEADER]
  if (typeof raw !== 'string' || raw.length === 0) return []
  return raw.split(',').map((column) => column.trim()).filter(Boolean)
}

/** 一次请求取回服务端流式 CSV。 */
export async function fetchUsageExportCsv(request: UsageExportRequest): Promise<UsageExportCsv> {
  const response = await apiClient.get<Blob>(request.endpoint, {
    params: { ...request.params, format: 'csv' },
    responseType: 'blob',
    signal: request.signal,
  })
  return { blob: response.data as unknown as Blob, columns: readExportColumns(response.headers) }
}

/** 一次请求取回带类型的导出行，供 Excel 写入器使用（不按页循环）。 */
export async function fetchUsageExportRows<T>(request: UsageExportRequest): Promise<T[]> {
  const response = await apiClient.get<{ items?: T[] }>(request.endpoint, {
    params: { ...request.params, format: 'json' },
    signal: request.signal,
  })
  return (response.data as { items?: T[] } | undefined)?.items ?? []
}

/** 与服务端 CSV 编码一致的单元格转义（含公式注入防护）。 */
export function escapeUsageExportCell(value: string): string {
  if (/^[=+\-@\t\r]/.test(value)) return `"'${value.replace(/"/g, '""')}"`
  if (/[,"\n\r]/.test(value)) return `"${value.replace(/"/g, '""')}"`
  return value
}

/**
 * 把服务端规范列名表头替换为界面语言的表头。
 *
 * 只重写第一行，正文逐字保留服务端的编码与转义。列数不一致时保留服务端表头，
 * 避免用错位的标签掩盖契约漂移。
 */
export function localizeUsageCsvHeader(csvText: string, serverColumns: string[], labels: string[]): string {
  if (!csvText || serverColumns.length === 0 || labels.length !== serverColumns.length) return csvText
  // 服务端 CSV 带 UTF-8 BOM；比较表头前先摘出来，避免把 BOM 当成列名的一部分。
  const bom = csvText.startsWith('\uFEFF') ? '\uFEFF' : ''
  const body = bom ? csvText.slice(1) : csvText
  const newlineIndex = body.indexOf('\n')
  const headerLine = newlineIndex === -1 ? body : body.slice(0, newlineIndex)
  const rest = newlineIndex === -1 ? '' : body.slice(newlineIndex)
  const expected = serverColumns.map(escapeUsageExportCell).join(',')
  if (headerLine.replace(/\r$/, '') !== expected) return csvText
  return bom + labels.map(escapeUsageExportCell).join(',') + rest
}

/** 下载导出的 CSV，表头按当前界面语言展示。 */
export async function downloadUsageCsv(
  request: UsageExportRequest & { labels: string[]; fileName: string },
): Promise<void> {
  const { blob, columns } = await fetchUsageExportCsv(request)
  const csvText = await blob.text()
  const localized = localizeUsageCsvHeader(csvText, columns, request.labels)
  const { saveAs } = await import('file-saver')
  saveAs(new Blob([localized], { type: 'text/csv;charset=utf-8;' }), request.fileName)
}

/** 导出文件名：与既有导出保持一致的时间范围命名。 */
export function usageExportFileName(startDate: string, endDate: string, extension: string): string {
  const range = startDate && endDate ? `${startDate}_to_${endDate}` : 'export'
  return `usage_${range}.${extension}`
}
