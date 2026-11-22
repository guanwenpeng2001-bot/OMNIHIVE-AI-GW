<script setup lang="ts">
import { computed, onBeforeUnmount, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import PublicShell from '@/components/layout/PublicShell.vue'
import BrandMark from '@/components/layout/BrandMark.vue'
import {
  Badge,
  Button,
  Card,
  ErrorState,
  Input,
  Select,
  Stepper,
  Switch,
  type UiStepItem
} from '@/components/ui'
import Icon from '@/components/icons/Icon.vue'
import {
  install,
  setSetupBootstrapToken,
  testDatabase,
  testRedis,
  type InstallRequest
} from '@/api/setup'
import { buildGatewayUrl } from '@/api/client'
import { extractApiErrorMessage, normalizeApiError } from '@/utils/apiError'

const { t } = useI18n()

const STEP_IDS = ['database', 'redis', 'admin', 'complete'] as const
type SetupStepId = (typeof STEP_IDS)[number]

const currentStep = ref(0)
const bootstrapToken = ref('')
const errorMessage = ref('')
const errorCode = ref<string | number | undefined>(undefined)
const errorRequestId = ref<string | undefined>(undefined)
const installSuccess = ref(false)

const testingDb = ref(false)
const testingRedis = ref(false)
const dbConnected = ref(false)
const redisConnected = ref(false)
const installing = ref(false)
const confirmPassword = ref('')
const serviceReady = ref(false)
const dbPortDraft = ref('5432')
const redisPortDraft = ref('6379')
const redisDbDraft = ref('0')
let disposed = false
let restartPollTimer: ReturnType<typeof setTimeout> | null = null
let restartRedirectTimer: ReturnType<typeof setTimeout> | null = null
let restartAbortController: AbortController | null = null

const ADMIN_EMAIL_RE = /^[^\s@]+@[^\s@]+\.[^\s@]+$/

function parseBoundedInt(raw: string, min: number, max: number): number | null {
  const trimmed = raw.trim()
  if (!/^\d+$/.test(trimmed)) return null
  const parsed = Number.parseInt(trimmed, 10)
  if (!Number.isSafeInteger(parsed) || parsed < min || parsed > max) return null
  return parsed
}

function handleDbPortInput(val: string | number): void {
  const next = String(val ?? '')
  dbPortDraft.value = next
  const parsed = parseBoundedInt(next, 1, 65535)
  if (parsed !== null) {
    formData.database.port = parsed
  } else {
    dbConnected.value = false
  }
}

function handleDbPortBlur(): void {
  const parsed = parseBoundedInt(dbPortDraft.value, 1, 65535)
  if (parsed !== null) {
    formData.database.port = parsed
    dbPortDraft.value = String(parsed)
  }
}

function handleRedisPortInput(val: string | number): void {
  const next = String(val ?? '')
  redisPortDraft.value = next
  const parsed = parseBoundedInt(next, 1, 65535)
  if (parsed !== null) {
    formData.redis.port = parsed
  } else {
    redisConnected.value = false
  }
}

function handleRedisPortBlur(): void {
  const parsed = parseBoundedInt(redisPortDraft.value, 1, 65535)
  if (parsed !== null) {
    formData.redis.port = parsed
    redisPortDraft.value = String(parsed)
  }
}

function handleRedisDbInput(val: string | number): void {
  const next = String(val ?? '')
  redisDbDraft.value = next
  const parsed = parseBoundedInt(next, 0, 15)
  if (parsed !== null) {
    formData.redis.db = parsed
  } else {
    redisConnected.value = false
  }
}

function handleRedisDbBlur(): void {
  const parsed = parseBoundedInt(redisDbDraft.value, 0, 15)
  if (parsed !== null) {
    formData.redis.db = parsed
    redisDbDraft.value = String(parsed)
  }
}

function getCurrentPort(): number {
  if (typeof window === 'undefined') return 8080
  const port = window.location.port
  if (port) {
    return parseInt(port, 10)
  }
  return window.location.protocol === 'https:' ? 443 : 80
}

const formData = reactive<InstallRequest>({
  database: {
    host: 'localhost',
    port: 5432,
    user: 'postgres',
    password: '',
    dbname: 'omnihive',
    sslmode: 'disable'
  },
  redis: {
    host: 'localhost',
    port: 6379,
    username: '',
    password: '',
    db: 0,
    enable_tls: false
  },
  admin: {
    email: '',
    password: ''
  },
  server: {
    host: '0.0.0.0',
    port: getCurrentPort(),
    mode: 'release'
  }
})

watch(
  () => [
    formData.database.host,
    formData.database.port,
    formData.database.user,
    formData.database.password,
    formData.database.dbname,
    formData.database.sslmode
  ],
  () => {
    dbConnected.value = false
  }
)

watch(
  () => [
    formData.redis.host,
    formData.redis.port,
    formData.redis.username,
    formData.redis.password,
    formData.redis.db,
    formData.redis.enable_tls
  ],
  () => {
    redisConnected.value = false
  }
)

const isDbPortValid = computed(() => parseBoundedInt(dbPortDraft.value, 1, 65535) !== null)
const isRedisPortValid = computed(() => parseBoundedInt(redisPortDraft.value, 1, 65535) !== null)
const isRedisDbValid = computed(() => parseBoundedInt(redisDbDraft.value, 0, 15) !== null)

const adminEmailError = computed(() => {
  const email = formData.admin.email.trim()
  if (!email) return undefined
  return ADMIN_EMAIL_RE.test(email) ? undefined : t('auth.invalidEmail')
})

const isAdminStepValid = computed(() =>
  Boolean(
    ADMIN_EMAIL_RE.test(formData.admin.email.trim()) &&
      formData.admin.password.length >= 8 &&
      formData.admin.password === confirmPassword.value
  )
)

const stepperItems = computed<UiStepItem[]>(() => [
  {
    id: 'database',
    title: t('setup.database.title'),
    description: 'PostgreSQL',
    disabled: installSuccess.value
  },
  {
    id: 'redis',
    title: t('setup.redis.title'),
    description: 'Redis',
    disabled: installSuccess.value || !dbConnected.value
  },
  {
    id: 'admin',
    title: t('setup.admin.title'),
    description: 'Root Admin',
    disabled: installSuccess.value || !dbConnected.value || !redisConnected.value
  },
  {
    id: 'complete',
    title: t('setup.ready.title'),
    description: 'Apply & Boot',
    disabled: installSuccess.value || !dbConnected.value || !redisConnected.value || !isAdminStepValid.value
  }
])

const activeStepId = computed<SetupStepId>(() => STEP_IDS[currentStep.value] || 'database')

const sslOptions = computed(() => [
  { value: 'disable', label: t('setup.database.ssl.disable') },
  { value: 'require', label: t('setup.database.ssl.require') },
  { value: 'verify-ca', label: t('setup.database.ssl.verifyCa') },
  { value: 'verify-full', label: t('setup.database.ssl.verifyFull') }
])

const passwordMismatchError = computed(() =>
  confirmPassword.value && formData.admin.password !== confirmPassword.value
    ? t('setup.admin.passwordMismatch')
    : undefined
)

const canProceed = computed(() => {
  switch (currentStep.value) {
    case 0:
      return dbConnected.value && isDbPortValid.value
    case 1:
      return redisConnected.value && isRedisPortValid.value && isRedisDbValid.value
    case 2:
      return isAdminStepValid.value
    default:
      return true
  }
})

function clearError(): void {
  errorMessage.value = ''
  errorCode.value = undefined
  errorRequestId.value = undefined
}

function captureError(error: unknown, fallback: string): void {
  const normalized = normalizeApiError(error)
  errorMessage.value = extractApiErrorMessage(error as Record<string, unknown>, fallback)
  errorCode.value = normalized.code || normalized.status || undefined
  errorRequestId.value = (normalized.metadata.request_id as string | undefined) || undefined
}

function handleBootstrapTokenUpdate(value: string): void {
  bootstrapToken.value = value
  setSetupBootstrapToken(value)
}

function handleStepSelect(_step: UiStepItem, index: number): void {
  if (installSuccess.value || index > currentStep.value) return
  clearError()
  currentStep.value = index
}

async function testDatabaseConnection(): Promise<void> {
  const parsedPort = parseBoundedInt(dbPortDraft.value, 1, 65535)
  if (parsedPort === null) {
    errorMessage.value = t('setup.connectionFailed')
    return
  }
  formData.database.port = parsedPort
  testingDb.value = true
  clearError()
  dbConnected.value = false
  try {
    await testDatabase(formData.database)
    dbConnected.value = true
  } catch (error: unknown) {
    captureError(error, t('setup.connectionFailed'))
  } finally {
    testingDb.value = false
  }
}

async function testRedisConnection(): Promise<void> {
  const parsedPort = parseBoundedInt(redisPortDraft.value, 1, 65535)
  const parsedDb = parseBoundedInt(redisDbDraft.value, 0, 15)
  if (parsedPort === null || parsedDb === null) {
    errorMessage.value = t('setup.connectionFailed')
    return
  }
  formData.redis.port = parsedPort
  formData.redis.db = parsedDb
  testingRedis.value = true
  clearError()
  redisConnected.value = false
  try {
    await testRedis(formData.redis)
    redisConnected.value = true
  } catch (error: unknown) {
    captureError(error, t('setup.connectionFailed'))
  } finally {
    testingRedis.value = false
  }
}

function nextStep(): void {
  if (!canProceed.value) return
  clearError()
  currentStep.value++
}

async function performInstall(): Promise<void> {
  if (!dbConnected.value || !redisConnected.value || !isAdminStepValid.value) {
    return
  }
  formData.admin.email = formData.admin.email.trim()
  installing.value = true
  clearError()
  try {
    await install(formData)
    installSuccess.value = true
    void waitForServiceRestart()
  } catch (error: unknown) {
    captureError(error, t('setup.installationFailed'))
  } finally {
    installing.value = false
  }
}

function retryCurrentStepAction(): void {
  if (currentStep.value === 0) {
    void testDatabaseConnection()
  } else if (currentStep.value === 1) {
    void testRedisConnection()
  } else if (currentStep.value === 3 && !installSuccess.value) {
    void performInstall()
  }
}

function sleepWithCancel(ms: number): Promise<boolean> {
  return new Promise((resolve) => {
    if (disposed) {
      resolve(false)
      return
    }
    restartPollTimer = setTimeout(() => {
      restartPollTimer = null
      resolve(!disposed)
    }, ms)
  })
}

async function waitForServiceRestart(): Promise<void> {
  const maxAttempts = 60
  const interval = 1000
  if (!(await sleepWithCancel(3000))) return

  for (let attempt = 0; attempt < maxAttempts; attempt++) {
    if (disposed) return
    restartAbortController?.abort()
    const controller = new AbortController()
    restartAbortController = controller
    try {
      const response = await fetch(buildGatewayUrl('/setup/status'), {
        method: 'GET',
        cache: 'no-store',
        signal: controller.signal
      })
      if (disposed) return
      if (response.ok) {
        const data = await response.json()
        if (disposed) return
        if (data.data && !data.data.needs_setup) {
          serviceReady.value = true
          restartRedirectTimer = setTimeout(() => {
            restartRedirectTimer = null
            if (disposed) return
            window.location.href = '/login'
          }, 1500)
          return
        }
      }
    } catch {
      if (disposed) return
      // Service restarting; continue polling.
    }
    if (!(await sleepWithCancel(interval))) return
  }

  if (!disposed) {
    errorMessage.value = t('setup.status.timeout')
  }
}

onBeforeUnmount(() => {
  disposed = true
  if (restartPollTimer) {
    clearTimeout(restartPollTimer)
    restartPollTimer = null
  }
  if (restartRedirectTimer) {
    clearTimeout(restartRedirectTimer)
    restartRedirectTimer = null
  }
  restartAbortController?.abort()
  restartAbortController = null
})
</script>

<template>
  <PublicShell>
    <div class="mx-auto w-full max-w-2xl space-y-6">
      <!-- Setup Header -->
      <div class="space-y-2 border-b border-oh-default pb-5">
        <div class="flex flex-wrap items-center justify-between gap-3">
          <BrandMark size="md" tier-label="SETUP" />
          <Badge tone="accent" variant="solid" dot mono>INSTANCE BOOTSTRAP</Badge>
        </div>
        <h1 class="pt-1 text-2xl font-semibold tracking-tight text-oh-primary">
          {{ t('setup.title') }}
        </h1>
        <p class="text-sm text-oh-secondary">
          {{ t('setup.description') }}
        </p>
      </div>

      <!-- Bootstrap Token -->
      <Card padding="md">
        <Input
          id="setup-bootstrap-token"
          :model-value="bootstrapToken"
          type="password"
          :label="t('setup.bootstrapToken')"
          :hint="t('setup.bootstrapTokenHint')"
          autocomplete="off"
          mono
          revealable
          @update:model-value="handleBootstrapTokenUpdate"
        />
      </Card>

      <!-- Stepper -->
      <Stepper
        :steps="stepperItems"
        :current-step-id="activeStepId"
        @select="handleStepSelect"
      />

      <!-- Step Body Card -->
      <Card padding="lg">
        <!-- Step 0: PostgreSQL Database -->
        <div v-if="currentStep === 0" class="space-y-5">
          <div class="border-b border-oh-subtle pb-3">
            <div class="flex items-center justify-between gap-2">
              <h2 class="text-base font-semibold text-oh-primary">
                {{ t('setup.database.title') }}
              </h2>
              <Badge v-if="dbConnected" tone="success" variant="subtle" dot mono>CONNECTED</Badge>
            </div>
            <p class="mt-1 text-xs text-oh-secondary">
              {{ t('setup.database.description') }}
            </p>
          </div>

          <div class="grid grid-cols-1 gap-4 sm:grid-cols-2">
            <Input
              id="setup-db-host"
              v-model="formData.database.host"
              :label="t('setup.database.host')"
              placeholder="localhost"
              mono
            />
            <Input
              id="setup-db-port"
              :model-value="dbPortDraft"
              type="number"
              :label="t('setup.database.port')"
              placeholder="5432"
              mono
              @update:model-value="handleDbPortInput"
              @blur="handleDbPortBlur"
            />
          </div>

          <div class="grid grid-cols-1 gap-4 sm:grid-cols-2">
            <Input
              id="setup-db-user"
              v-model="formData.database.user"
              :label="t('setup.database.username')"
              placeholder="postgres"
              mono
            />
            <Input
              id="setup-db-password"
              v-model="formData.database.password"
              type="password"
              :label="t('setup.database.password')"
              :placeholder="t('setup.database.passwordPlaceholder')"
              revealable
            />
          </div>

          <div class="grid grid-cols-1 gap-4 sm:grid-cols-2">
            <Input
              id="setup-db-name"
              v-model="formData.database.dbname"
              :label="t('setup.database.databaseName')"
              placeholder="omnihive"
              mono
            />
            <Select
              id="setup-db-sslmode"
              :model-value="formData.database.sslmode"
              :label="t('setup.database.sslMode')"
              :options="sslOptions"
              mono
              @update:model-value="(val) => (formData.database.sslmode = String(val))"
            />
          </div>

          <Button
            data-testid="setup-test-db"
            :variant="dbConnected ? 'outline-accent' : 'secondary'"
            :loading="testingDb"
            block
            @click="testDatabaseConnection"
          >
            <Icon v-if="dbConnected && !testingDb" name="check" size="sm" />
            <span>
              {{
                testingDb
                  ? t('setup.status.testing')
                  : dbConnected
                    ? t('setup.status.success')
                    : t('setup.status.testConnection')
              }}
            </span>
          </Button>
        </div>

        <!-- Step 1: Redis -->
        <div v-else-if="currentStep === 1" class="space-y-5">
          <div class="border-b border-oh-subtle pb-3">
            <div class="flex items-center justify-between gap-2">
              <h2 class="text-base font-semibold text-oh-primary">
                {{ t('setup.redis.title') }}
              </h2>
              <Badge v-if="redisConnected" tone="success" variant="subtle" dot mono>CONNECTED</Badge>
            </div>
            <p class="mt-1 text-xs text-oh-secondary">
              {{ t('setup.redis.description') }}
            </p>
          </div>

          <div class="grid grid-cols-1 gap-4 sm:grid-cols-2">
            <Input
              id="setup-redis-host"
              v-model="formData.redis.host"
              :label="t('setup.redis.host')"
              placeholder="localhost"
              mono
            />
            <Input
              id="setup-redis-port"
              :model-value="redisPortDraft"
              type="number"
              :label="t('setup.redis.port')"
              placeholder="6379"
              mono
              @update:model-value="handleRedisPortInput"
              @blur="handleRedisPortBlur"
            />
          </div>

          <div class="grid grid-cols-1 gap-4 sm:grid-cols-2">
            <Input
              id="setup-redis-user"
              v-model="formData.redis.username"
              :label="t('setup.redis.username')"
              :placeholder="t('setup.redis.usernamePlaceholder')"
              mono
            />
            <Input
              id="setup-redis-password"
              v-model="formData.redis.password"
              type="password"
              :label="t('setup.redis.password')"
              :placeholder="t('setup.redis.passwordPlaceholder')"
              revealable
            />
          </div>

          <div class="grid grid-cols-1 gap-4 sm:grid-cols-2">
            <Input
              id="setup-redis-db"
              :model-value="redisDbDraft"
              type="number"
              :label="t('setup.redis.database')"
              placeholder="0"
              mono
              @update:model-value="handleRedisDbInput"
              @blur="handleRedisDbBlur"
            />
            <div class="flex items-center rounded-oh-md border border-oh-default bg-oh-subtle px-3.5 py-2.5">
              <Switch
                v-model="formData.redis.enable_tls"
                :label="t('setup.redis.enableTls')"
                :description="t('setup.redis.enableTlsHint')"
              />
            </div>
          </div>

          <Button
            data-testid="setup-test-redis"
            :variant="redisConnected ? 'outline-accent' : 'secondary'"
            :loading="testingRedis"
            block
            @click="testRedisConnection"
          >
            <Icon v-if="redisConnected && !testingRedis" name="check" size="sm" />
            <span>
              {{
                testingRedis
                  ? t('setup.status.testing')
                  : redisConnected
                    ? t('setup.status.success')
                    : t('setup.status.testConnection')
              }}
            </span>
          </Button>
        </div>

        <!-- Step 2: Admin Account -->
        <div v-else-if="currentStep === 2" class="space-y-5">
          <div class="border-b border-oh-subtle pb-3">
            <h2 class="text-base font-semibold text-oh-primary">
              {{ t('setup.admin.title') }}
            </h2>
            <p class="mt-1 text-xs text-oh-secondary">
              {{ t('setup.admin.description') }}
            </p>
          </div>

          <Input
            id="setup-admin-email"
            v-model="formData.admin.email"
            type="email"
            :label="t('setup.admin.email')"
            placeholder="admin@example.com"
            :error="adminEmailError"
            autocomplete="email"
            required
          />

          <Input
            id="setup-admin-password"
            v-model="formData.admin.password"
            type="password"
            :label="t('setup.admin.password')"
            :placeholder="t('setup.admin.passwordPlaceholder')"
            :hint="t('setup.admin.passwordPlaceholder')"
            autocomplete="new-password"
            revealable
            required
          />

          <Input
            id="setup-admin-confirm-password"
            v-model="confirmPassword"
            type="password"
            :label="t('setup.admin.confirmPassword')"
            :placeholder="t('setup.admin.confirmPasswordPlaceholder')"
            :error="passwordMismatchError"
            autocomplete="new-password"
            revealable
            required
          />
        </div>

        <!-- Step 3: Review & Apply -->
        <div v-else class="space-y-5">
          <div class="border-b border-oh-subtle pb-3">
            <h2 class="text-base font-semibold text-oh-primary">
              {{ t('setup.ready.title') }}
            </h2>
            <p class="mt-1 text-xs text-oh-secondary">
              {{ t('setup.ready.description') }}
            </p>
          </div>

          <dl class="divide-y divide-oh-subtle rounded-oh-md border border-oh-default bg-oh-subtle">
            <div class="flex flex-col gap-1 px-4 py-3 sm:flex-row sm:items-center sm:justify-between">
              <dt class="oh-eyebrow">{{ t('setup.ready.database') }}</dt>
              <dd class="font-mono text-xs text-oh-primary">
                {{ formData.database.user }}@{{ formData.database.host }}:{{ formData.database.port }}/{{ formData.database.dbname }}
              </dd>
            </div>
            <div class="flex flex-col gap-1 px-4 py-3 sm:flex-row sm:items-center sm:justify-between">
              <dt class="oh-eyebrow">{{ t('setup.ready.redis') }}</dt>
              <dd class="font-mono text-xs text-oh-primary">
                {{ formData.redis.host }}:{{ formData.redis.port }}/{{ formData.redis.db }}
                <span v-if="formData.redis.enable_tls" class="ml-1.5 text-oh-accent-text">(TLS)</span>
              </dd>
            </div>
            <div class="flex flex-col gap-1 px-4 py-3 sm:flex-row sm:items-center sm:justify-between">
              <dt class="oh-eyebrow">{{ t('setup.ready.adminEmail') }}</dt>
              <dd class="font-mono text-xs text-oh-primary">
                {{ formData.admin.email || '—' }}
              </dd>
            </div>
          </dl>
        </div>

        <!-- Structured Error State -->
        <div v-if="errorMessage" class="mt-5">
          <ErrorState
            :title="errorMessage"
            :code="errorCode"
            :request-id="errorRequestId"
            compact
            :retryable="currentStep === 0 || currentStep === 1 || (currentStep === 3 && !installSuccess)"
            @retry="retryCurrentStepAction"
          />
        </div>

        <!-- Confirmed Install + Restart Readiness Banner -->
        <div
          v-if="installSuccess"
          role="status"
          class="mt-5 rounded-oh-md border border-oh-status-ok-border bg-oh-status-ok-bg p-4 text-xs text-oh-status-ok-fg"
        >
          <div class="flex items-start gap-2.5">
            <Icon name="checkCircle" size="md" class="mt-0.5 shrink-0" />
            <div class="space-y-1">
              <p class="font-semibold">{{ t('setup.status.completed') }}</p>
              <p class="opacity-90">
                {{ serviceReady ? t('setup.status.redirecting') : t('setup.status.restarting') }}
              </p>
            </div>
          </div>
        </div>

        <!-- Step Navigation Footer -->
        <template #footer>
          <div class="flex items-center justify-between gap-3">
            <Button
              v-if="currentStep > 0 && !installSuccess"
              variant="secondary"
              @click="currentStep--; clearError()"
            >
              <Icon name="chevronLeft" size="sm" />
              <span>{{ t('common.back') }}</span>
            </Button>
            <div v-else />

            <Button
              v-if="currentStep < 3"
              data-testid="setup-next-step"
              variant="primary"
              :disabled="!canProceed"
              @click="nextStep"
            >
              <span>{{ t('common.next') }}</span>
              <Icon name="chevronRight" size="sm" />
            </Button>

            <Button
              v-else-if="!installSuccess"
              data-testid="setup-complete-install"
              variant="primary"
              :loading="installing"
              @click="performInstall"
            >
              <span>
                {{ installing ? t('setup.status.installing') : t('setup.status.completeInstallation') }}
              </span>
            </Button>
          </div>
        </template>
      </Card>
    </div>
  </PublicShell>
</template>
