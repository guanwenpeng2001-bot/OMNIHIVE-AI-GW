import type { components, operations, paths } from '@/api/generated/openapi'

export type CatalogRate = components['schemas']['CatalogRate']
export type CatalogRateBand = components['schemas']['CatalogRateBand']
export type CatalogTimeBand = components['schemas']['CatalogTimeBand']
export type CatalogMoney = components['schemas']['CatalogMoney']
export type CatalogMetrics = components['schemas']['CatalogMetrics']
export type CatalogModelFacts = components['schemas']['CatalogModelFacts']
export type CatalogCapability = components['schemas']['CatalogCapability']
export type CatalogEndpoint = components['schemas']['CatalogEndpoint']
export type CatalogEvidence = components['schemas']['CatalogEvidence']
export type CatalogBenchmark = components['schemas']['CatalogBenchmark']
export type CatalogModelCompatibility = components['schemas']['CatalogModelCompatibility']
export type CatalogScope = components['schemas']['CatalogScope']
export type CatalogPriceScenario = components['schemas']['CatalogPriceScenario']
export type CatalogEstimateComponent = components['schemas']['CatalogEstimateComponent']

export type ExactQuoteInput = components['schemas']['ExactQuoteInput']
export type ExactQuote = components['schemas']['ExactQuote']
export type ExactQuoteLine = components['schemas']['ExactQuoteLine']
export type ExactPriceScenario = components['schemas']['ExactPriceScenario']

export type StatusWindow = NonNullable<NonNullable<
  paths['/api/v2/public/status']['get']['parameters']['query']
>['window']>
export type StatusSummary = components['schemas']['StatusSummary']
export type StatusComponent = components['schemas']['StatusComponent']
export type StatusDaily = components['schemas']['StatusDaily']
export type StatusIncidentSummary = components['schemas']['StatusIncidentSummary']
export type StatusIncident = components['schemas']['StatusIncident']
export type StatusIncidentPage =
  paths['/api/v2/public/status/incidents']['get']['responses']['200']['content']['application/json']['data']

export type CatalogSourceKind = components['schemas']['CatalogSourceKind']
export type CatalogSourceAxis = components['schemas']['CatalogSourceAxis']
export type CatalogBillingState = components['schemas']['CatalogBillingState']
export type CatalogReferencePrice = components['schemas']['CatalogReferencePrice']
export type CatalogSalePrice = components['schemas']['CatalogSalePrice']
export type CatalogFieldProvenance = components['schemas']['CatalogFieldProvenance']
export type CatalogCommonFacts = components['schemas']['CatalogCommonFacts']
export type CatalogModelSummary = components['schemas']['ModelSummary']
export type CatalogModelDetail = components['schemas']['ModelDetail']
export type CatalogModelPage = components['schemas']['CatalogModelPage']
export type CatalogCompareInput = components['schemas']['CatalogCompareInput']
export type CatalogEstimate = components['schemas']['CatalogEstimate']
export type CatalogCompareRow = components['schemas']['CatalogCompareRow']
export type CatalogCompareResult = components['schemas']['CatalogCompareResult']
export type CatalogPublicationState = components['schemas']['CatalogAvailability']['publication']
export type CatalogAccessAxisState = components['schemas']['CatalogAvailability']['access']
export type CatalogListParams = NonNullable<operations['listPublicModels']['parameters']['query']>
export type ProjectCatalogListParams = NonNullable<operations['listProjectModels']['parameters']['query']>
