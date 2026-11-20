export type UiSize = 'xs' | 'sm' | 'md' | 'lg'

export type UiButtonVariant = 'primary' | 'secondary' | 'ghost' | 'danger' | 'outline-accent'

export type UiTone = 'neutral' | 'accent' | 'success' | 'warning' | 'danger' | 'info' | 'unknown'

export type UiBadgeVariant = 'subtle' | 'outline' | 'solid'

export interface UiSelectOption {
  value: string | number
  label: string
  description?: string
  disabled?: boolean
  group?: string
}

export interface UiTabItem {
  value: string
  label: string
  count?: number | string
  badge?: string
  disabled?: boolean
}

export interface UiSegmentOption {
  value: string
  label: string
  count?: number | string
  disabled?: boolean
}

export type UiSortOrder = 'asc' | 'desc' | null

export interface UiTableColumn<Row = Record<string, unknown>> {
  key: string
  label: string
  align?: 'left' | 'center' | 'right'
  mono?: boolean
  sortable?: boolean
  tooltip?: string
  width?: string
  minWidth?: string
  format?: (value: unknown, row: Row) => string
}

export type UiSettlementState = 'settled' | 'estimated' | 'reserved' | 'pending'

export type UiStepStatus = 'complete' | 'current' | 'upcoming' | 'warning' | 'error'

export interface UiStepItem {
  id: string
  title: string
  description?: string
  status?: UiStepStatus
  disabled?: boolean
  optional?: boolean
}

export interface UiCommandItem {
  id: string
  label: string
  description?: string
  group?: string
  shortcut?: string
  badge?: string
  keywords?: string[]
  disabled?: boolean
  onSelect?: () => void
}

export interface UiCommandGroup {
  id: string
  label: string
  items: UiCommandItem[]
}

export interface UiBreadcrumbItem {
  label: string
  to?: string
}
