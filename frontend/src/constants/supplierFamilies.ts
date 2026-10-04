import { compatibleProvider } from './compatibleProviders'
import { CONCRETE_PLATFORM_OPTIONS } from './platforms'

export type SupplierAxis = 'region' | 'product' | 'site'
export type SupplierSelection = Partial<Record<SupplierAxis, string>>
export interface SupplierMember {
  platform: string
  selection: SupplierSelection
}
export interface SupplierFamily {
  id: string
  label: string
  axes: SupplierAxis[]
  members: SupplierMember[]
}

// Presentation only: these IDs remain distinct in accounts, groups and budgets.
// Add verified products here only once the registry can actually route them.
const supplierFamilies: SupplierFamily[] = [
  { id: 'stepfun', label: 'StepFun', axes: ['region', 'product'], members: [
    { platform: 'stepfun_api', selection: { region: 'china', product: 'api' } },
    { platform: 'stepfun_api_intl', selection: { region: 'international', product: 'api' } },
    { platform: 'stepfun_plan', selection: { region: 'china', product: 'stepPlan' } },
    { platform: 'stepfun_plan_intl', selection: { region: 'international', product: 'stepPlan' } }
  ] },
  { id: 'mimo', label: 'Xiaomi MiMo', axes: ['product'], members: [
    { platform: 'mimo', selection: { product: 'api' } },
    { platform: 'mimo_token_cn', selection: { product: 'tokenPlanChina' } },
    ...availableMembers([
      { platform: 'mimo_token_sg', selection: { product: 'tokenPlanSingapore' } },
      { platform: 'mimo_token_eu', selection: { product: 'tokenPlanEurope' } }
    ])
  ] },
  { id: 'compshare', label: 'Compshare', axes: ['product'], members: [
    { platform: 'compshare', selection: { product: 'api' } },
    { platform: 'compshare_coding', selection: { product: 'codingPlan' } }
  ] },
  { id: 'siliconflow', label: 'SiliconFlow', axes: ['region'], members: [
    { platform: 'siliconflow_cn', selection: { region: 'china' } },
    { platform: 'siliconflow_global', selection: { region: 'global' } }
  ] },
  { id: 'qwencloud', label: 'QwenCloud', axes: ['product'], members: [
    { platform: 'qwencloud', selection: { product: 'maas' } },
    { platform: 'qwencloud_maas_token', selection: { product: 'maasTokenPlan' } },
    { platform: 'qwencloud_token', selection: { product: 'tokenPlan' } }
  ] },
  { id: 'dmxapi', label: 'DMXAPI', axes: ['site'], members: [
    { platform: 'dmxapi', selection: { site: 'cnSite' } },
    { platform: 'dmxapi_com', selection: { site: 'comSite' } },
    { platform: 'dmxapi_ssvip', selection: { site: 'ssvip' } }
  ] },
  { id: 'teamorouter', label: 'TeamoRouter', axes: ['site'], members: [
    { platform: 'teamorouter', selection: { site: 'cnSite' } },
    { platform: 'teamorouter_com', selection: { site: 'comSite' } }
  ] },
  { id: 'crazyrouter', label: 'CrazyRouter', axes: ['site'], members: [
    { platform: 'crazyrouter', selection: { site: 'china' } },
    { platform: 'crazyrouter_intl', selection: { site: 'international' } }
  ] }
]

// These verified variants are delivered by the companion registry change. Until
// installed, they must not appear as selectable products with nonexistent routes.
function availableMembers(members: SupplierMember[]): SupplierMember[] {
  return members.filter(member => !!compatibleProvider(member.platform))
}
const astronMembers = availableMembers([
  { platform: 'astron_coding', selection: { product: 'codingPlan' } },
  { platform: 'astron_token', selection: { product: 'tokenPlan' } }
])
if (astronMembers.length === 2) supplierFamilies.push({ id: 'astron', label: 'Astron', axes: ['product'], members: astronMembers })
export const SUPPLIER_FAMILIES: readonly SupplierFamily[] = supplierFamilies

export function supplierOf(platform: string): SupplierFamily | undefined {
  return SUPPLIER_FAMILIES.find(supplier => supplier.members.some(member => member.platform === platform))
}

export function resolveMember(supplier: SupplierFamily, selection: SupplierSelection): SupplierMember | undefined {
  if (supplier.axes.some(axis => !selection[axis])) return undefined
  return supplier.members.find(member => supplier.axes.every(axis => member.selection[axis] === selection[axis]))
}

export function supplierPlatformOptions() {
  return [...CONCRETE_PLATFORM_OPTIONS, { value: 'vertex', label: 'Vertex AI' }]
    .filter(option => !supplierOf(option.value) || supplierOf(option.value)?.members[0].platform === option.value)
    .map(option => {
      const supplier = supplierOf(option.value)
      return { ...option, label: supplier?.label ?? option.label, supplier,
        search: supplier?.members.map(member => {
          const provider = compatibleProvider(member.platform)
          return `${member.platform} ${provider?.label ?? ''} ${provider?.label_zh ?? ''}`
        }).join(' ') ?? '' }
    })
}
