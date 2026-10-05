<template>
  <BaseDialog
    :show="showCompositeRoutesModal"
    :title="
      compositeRoutesGroup
        ? t('admin.groups.compositeRoutes.titleWithGroup', {
            name: compositeRoutesGroup.name,
          })
        : t('admin.groups.compositeRoutes.title')
    "
    width="full"
    @close="closeCompositeRoutesModal"
  >
    <div
      class="grid min-h-0 gap-5 lg:h-[min(65vh,44rem)] lg:grid-cols-[18rem_minmax(0,1fr)]"
    >
      <aside
        class="min-w-0 space-y-4 lg:overflow-y-auto lg:pr-2"
        :aria-label="t('admin.groups.compositeRoutes.routes')"
      >
        <div class="flex items-center justify-between gap-2">
          <h3 class="text-sm font-semibold">
            {{ t("admin.groups.compositeRoutes.routes") }}
            <span class="text-gray-500">({{ compositeRoutes.length }})</span>
          </h3>
          <button
            type="button"
            class="btn btn-secondary btn-sm"
            :aria-label="t('common.refresh')"
            :disabled="compositeRoutesLoading || busy"
            @click="loadCompositeRoutes"
          >
            <Icon name="refresh" size="sm" />
          </button>
        </div>
        <input
          v-model="compositeRouteSearchQuery"
          type="search"
          class="input"
          :aria-label="t('admin.groups.compositeRoutes.searchPlaceholder')"
          :placeholder="t('admin.groups.compositeRoutes.searchPlaceholder')"
        />
        <button
          type="button"
          class="btn btn-secondary w-full"
          :disabled="busy"
          @click="startNewRoute"
        >
          <Icon name="plus" size="sm" class="mr-2" />{{
            t("admin.groups.compositeRoutes.addRoute")
          }}
        </button>
        <p class="text-xs text-gray-500 dark:text-gray-400">
          {{ t("admin.groups.compositeRoutes.listOrderHint") }}
        </p>
        <div
          class="max-h-64 space-y-2 overflow-y-auto lg:max-h-none"
          :aria-busy="compositeRoutesLoading"
        >
          <p v-if="compositeRoutesLoading" class="p-3 text-sm text-gray-500">
            {{ t("common.loading") }}
          </p>
          <p
            v-else-if="compositeRouteLoadFailed"
            role="status"
            class="p-3 text-sm text-red-600 dark:text-red-400"
          >
            {{ t("admin.groups.compositeRoutes.failedToLoad") }}
          </p>
          <p
            v-else-if="!compositeRoutes.length"
            class="rounded-lg bg-gray-50 p-3 text-sm text-gray-500 dark:bg-dark-900"
          >
            {{ t("admin.groups.compositeRoutes.empty") }}
          </p>
          <div
            v-else-if="!filteredCompositeRoutes.length"
            class="p-3 text-sm text-gray-500"
          >
            {{ t("admin.groups.compositeRoutes.searchEmpty") }}
            <button
              type="button"
              class="mt-2 block text-primary-600"
              @click="compositeRouteSearchQuery = ''"
            >
              {{ t("admin.groups.compositeRoutes.clearSearch") }}
            </button>
          </div>
          <ul v-else class="space-y-2">
            <li
              v-for="route in filteredCompositeRoutes"
              :key="route.id"
              class="flex rounded-lg border"
              :class="
                compositeRouteEditingId === route.id
                  ? 'border-primary-500 bg-primary-50 dark:bg-primary-900/20'
                  : 'border-gray-200 dark:border-dark-600'
              "
            >
              <button
                type="button"
                class="min-w-0 flex-1 space-y-1 p-3 text-left focus-visible:ring-2 focus-visible:ring-primary-500"
                :aria-pressed="compositeRouteEditingId === route.id"
                :disabled="busy"
                @click="selectRoute(route)"
              >
                <span class="block break-all text-sm font-semibold">{{
                  route.public_model
                }}</span>
                <span class="block text-xs text-gray-500 dark:text-gray-400"
                  >{{ compositeRouteMatchLabel(route.match_type) }} ·
                  {{ formatCompositeEndpoint(route.endpoint) }} ·
                  {{ t("admin.groups.compositeRoutes.priority") }}
                  {{ route.priority }}</span
                >
                <span class="block break-all text-xs"
                  >{{
                    formatCompositePlatform(
                      firstTarget(route)?.target_platform ||
                        route.target_platform,
                    )
                  }}
                  →
                  {{
                    firstTarget(route)?.upstream_model ||
                    route.upstream_model ||
                    route.public_model
                  }}</span
                >
                <span
                  v-if="compositeRouteTargetCount(route) > 1"
                  class="block text-xs text-gray-500"
                  >{{
                    t("admin.groups.compositeRoutes.targetCount", {
                      count: compositeRouteTargetCount(route),
                    })
                  }}</span
                >
                <span v-if="!route.enabled" class="badge badge-danger">{{
                  t("admin.accounts.status.inactive")
                }}</span>
              </button>
              <button
                type="button"
                class="self-start rounded p-2 text-gray-500 hover:text-red-600"
                :aria-label="`${t('common.delete')}: ${route.public_model}`"
                :disabled="busy"
                @click="deleteCompositeRoute(route)"
              >
                <Icon name="trash" size="sm" />
              </button>
            </li>
          </ul>
        </div>
        <details class="border-t border-gray-200 pt-3 dark:border-dark-600">
          <summary class="cursor-pointer text-sm font-semibold">
            {{ t("admin.groups.compositeRoutes.savedPreview") }}
          </summary>
          <div class="mt-3 space-y-3" :aria-busy="compositePreviewLoading">
            <p class="text-xs text-gray-500 dark:text-gray-400">
              {{ t("admin.groups.compositeRoutes.savedPreviewHint") }}
            </p>
            <label for="composite-preview-model" class="input-label">{{
              t("admin.groups.compositeRoutes.publicModel")
            }}</label>
            <input
              id="composite-preview-model"
              v-model.trim="compositePreviewModel"
              type="text"
              class="input"
              placeholder="openrouter/gpt-5"
            />
            <Select
              v-model="compositePreviewEndpoint"
              :options="compositeRouteEndpointOptions"
              :label="t('admin.groups.compositeRoutes.endpoint')"
            />
            <button
              type="button"
              class="btn btn-secondary w-full"
              :disabled="
                compositePreviewLoading || !compositePreviewModel || busy
              "
              @click="previewCompositeRoute"
            >
              {{ t("admin.groups.compositeRoutes.preview") }}
            </button>
            <div aria-live="polite" class="space-y-2 text-sm">
              <p
                v-if="compositePreviewFailed"
                class="text-red-600 dark:text-red-400"
              >
                {{ t("admin.groups.compositeRoutes.failedToPreview") }}
              </p>
              <div
                v-if="compositePreviewDecision"
                class="space-y-2 rounded-lg bg-gray-50 p-3 dark:bg-dark-900"
              >
                <span
                  class="badge"
                  :class="
                    compositePreviewDecision.matched
                      ? 'badge-success'
                      : 'badge-danger'
                  "
                  >{{
                    t(
                      `admin.groups.compositeRoutes.${compositePreviewDecision.matched ? "matched" : "notMatched"}`,
                    )
                  }}</span
                >
                <span class="ml-1 text-xs">{{
                  compositeRouteSourceLabel(compositePreviewDecision.source)
                }}</span>
                <template v-if="compositePreviewDecision.matched">
                  <ol
                    v-if="previewTargets.length"
                    class="list-inside list-decimal space-y-1"
                  >
                    <li
                      v-for="target in previewTargets"
                      :key="target.position"
                      class="break-all"
                    >
                      {{ formatCompositePlatform(target.target_platform) }} →
                      {{
                        target.upstream_model ||
                        compositePreviewDecision.upstream_model
                      }}
                    </li>
                  </ol>
                  <p v-else class="break-all">
                    {{
                      formatCompositePlatform(
                        compositePreviewDecision.target_platform,
                      )
                    }}
                    → {{ compositePreviewDecision.upstream_model }}
                  </p>
                </template>
                <p v-else>{{ compositePreviewDecision.reason }}</p>
              </div>
            </div>
          </div>
        </details>
      </aside>

      <section
        class="min-w-0 lg:overflow-y-auto lg:pl-1 lg:pr-2"
        :aria-busy="compositeRouteSaving"
      >
        <form id="composite-route-form" @submit.prevent="saveCompositeRoute">
          <fieldset :disabled="busy" class="min-w-0 space-y-5">
            <div class="flex items-center justify-between gap-3">
              <h3 class="text-base font-semibold">
                {{
                  t(
                    `admin.groups.compositeRoutes.${compositeRouteEditingId ? "editRoute" : "addRoute"}`,
                  )
                }}
              </h3>
              <label class="flex items-center gap-2 text-sm"
                ><input
                  v-model="compositeRouteForm.enabled"
                  type="checkbox"
                  class="rounded"
                />{{ t("admin.groups.compositeRoutes.enabled") }}</label
              >
            </div>
            <div>
              <label for="composite-public-model" class="input-label">{{
                t("admin.groups.compositeRoutes.publicModel")
              }}</label>
              <input
                id="composite-public-model"
                ref="publicModelInput"
                v-model="compositeRouteForm.public_model"
                type="text"
                class="input"
                required
                placeholder="openrouter/gpt-5"
                list="composite-public-suggestions"
                aria-describedby="composite-suggestions-hint"
              />
              <datalist id="composite-public-suggestions">
                <option
                  v-for="model in filterCompositeRouteModelSuggestions(
                    compositeRouteForm.public_model,
                    compositeRouteForm.target_platform,
                    true,
                  )"
                  :key="model"
                  :value="model"
                />
              </datalist>
              <p
                id="composite-suggestions-hint"
                class="mt-2 text-xs text-gray-500 dark:text-gray-400"
              >
                {{ t("admin.groups.compositeRoutes.suggestionsHint") }}
              </p>
            </div>
            <div class="grid grid-cols-1 gap-3 sm:grid-cols-3">
              <div>
                <label for="composite-match" class="input-label">{{
                  t("admin.groups.compositeRoutes.matchType")
                }}</label
                ><Select
                  id="composite-match"
                  v-model="compositeRouteForm.match_type"
                  :disabled="busy"
                  :options="compositeRouteMatchOptions"
                  :label="t('admin.groups.compositeRoutes.matchType')"
                />
              </div>
              <div>
                <label for="composite-endpoint" class="input-label">{{
                  t("admin.groups.compositeRoutes.endpoint")
                }}</label
                ><Select
                  id="composite-endpoint"
                  v-model="compositeRouteForm.endpoint"
                  :disabled="busy"
                  :options="compositeRouteEndpointOptions"
                  :label="t('admin.groups.compositeRoutes.endpoint')"
                />
              </div>
              <div>
                <label for="composite-priority" class="input-label">{{
                  t("admin.groups.compositeRoutes.priority")
                }}</label
                ><input
                  id="composite-priority"
                  v-model.number="compositeRouteForm.priority"
                  type="number"
                  min="1"
                  step="1"
                  class="input"
                  aria-describedby="composite-priority-hint"
                />
              </div>
            </div>
            <p
              id="composite-priority-hint"
              class="text-xs text-gray-500 dark:text-gray-400"
            >
              {{ t("admin.groups.compositeRoutes.priorityHint") }}
            </p>
            <div class="space-y-3">
              <div class="flex flex-wrap items-center justify-between gap-2">
                <h4 class="text-sm font-semibold">
                  {{ t("admin.groups.compositeRoutes.targetsTitle") }}
                  <span class="text-gray-500"
                    >{{ compositeRouteForm.targets.length }}/{{
                      compositeRouteMaxTargets
                    }}</span
                  >
                </h4>
                <button
                  type="button"
                  class="btn btn-secondary btn-sm"
                  :disabled="
                    compositeRouteForm.targets.length >=
                    compositeRouteMaxTargets
                  "
                  @click="addCompositeRouteTarget"
                >
                  <Icon name="plus" size="sm" class="mr-1" />{{
                    t("admin.groups.compositeRoutes.addTarget")
                  }}
                </button>
              </div>
              <p class="text-xs text-gray-500 dark:text-gray-400">
                {{ t("admin.groups.compositeRoutes.targetsHint") }}
              </p>
              <p class="text-xs text-gray-500 dark:text-gray-400">
                {{ t("admin.groups.compositeRoutes.upstreamModelHint") }}
              </p>
              <div
                v-for="(target, index) in compositeRouteForm.targets"
                :key="targetKey(target)"
                class="space-y-3 rounded-xl border border-gray-200 p-3 sm:p-4 dark:border-dark-600"
              >
                <div class="flex flex-wrap items-center justify-between gap-2">
                  <div class="text-sm font-semibold">
                    <span class="mr-2 text-gray-400">{{ index + 1 }}.</span
                    >{{ compositeRouteTargetRole(index) }}
                  </div>
                  <div class="flex items-center gap-1">
                    <label class="mr-2 flex items-center gap-2 text-xs"
                      ><input
                        v-model="target.enabled"
                        type="checkbox"
                        class="rounded"
                      />{{ t("admin.groups.compositeRoutes.enabled") }}</label
                    >
                    <button
                      type="button"
                      class="btn btn-secondary btn-sm"
                      :disabled="index === 0"
                      :aria-label="targetActionLabel('moveTargetUp', index)"
                      @click="moveCompositeRouteTarget(index, -1)"
                    >
                      <Icon name="chevronUp" size="sm" />
                    </button>
                    <button
                      type="button"
                      class="btn btn-secondary btn-sm"
                      :disabled="
                        index === compositeRouteForm.targets.length - 1
                      "
                      :aria-label="targetActionLabel('moveTargetDown', index)"
                      @click="moveCompositeRouteTarget(index, 1)"
                    >
                      <Icon name="chevronDown" size="sm" />
                    </button>
                    <button
                      type="button"
                      class="btn btn-secondary btn-sm"
                      :disabled="compositeRouteForm.targets.length <= 1"
                      :aria-label="targetActionLabel('removeTarget', index)"
                      @click="removeCompositeRouteTarget(index)"
                    >
                      <Icon name="trash" size="sm" />
                    </button>
                  </div>
                </div>
                <div
                  class="grid grid-cols-1 gap-3 sm:grid-cols-[minmax(10rem,1fr)_minmax(0,2fr)]"
                >
                  <div>
                    <label
                      :for="`target-platform-${index}`"
                      class="input-label"
                      >{{
                        t("admin.groups.compositeRoutes.targetPlatform")
                      }}</label
                    ><Select
                      :id="`target-platform-${index}`"
                      v-model="target.target_platform"
                      :disabled="busy"
                      :options="compositeRoutePlatformOptions"
                      :label="t('admin.groups.compositeRoutes.targetPlatform')"
                    />
                  </div>
                  <div>
                    <label :for="`target-model-${index}`" class="input-label">{{
                      t("admin.groups.compositeRoutes.upstreamModel")
                    }}</label>
                    <input
                      :id="`target-model-${index}`"
                      v-model="target.upstream_model"
                      type="text"
                      class="input"
                      placeholder="gpt-5"
                      :list="`target-suggestions-${index}`"
                    />
                    <datalist :id="`target-suggestions-${index}`">
                      <option
                        v-for="model in filterCompositeRouteModelSuggestions(
                          target.upstream_model,
                          target.target_platform,
                        )"
                        :key="model"
                        :value="model"
                      />
                    </datalist>
                    <p
                      v-if="
                        compositeRouteSuggestionStatus[
                          target.target_platform
                        ] === 'error'
                      "
                      class="mt-1 text-xs text-gray-500"
                      role="status"
                    >
                      {{
                        t("admin.groups.compositeRoutes.suggestionsUnavailable")
                      }}
                    </p>
                  </div>
                </div>
                <details
                  data-target-advanced
                  :open="hasAdvancedOptions(target)"
                  class="border-t border-gray-100 pt-2 dark:border-dark-700"
                >
                  <summary
                    class="cursor-pointer text-xs text-gray-600 dark:text-gray-300"
                  >
                    {{ t("admin.groups.compositeRoutes.advanced")
                    }}<span
                      v-if="hasAdvancedOptions(target)"
                      class="ml-2 font-medium"
                      >· {{ advancedSummary(target) }}</span
                    >
                  </summary>
                  <div
                    class="mt-3 grid grid-cols-1 gap-3 sm:grid-cols-2 xl:grid-cols-3"
                  >
                    <div>
                      <label
                        :for="`target-account-${index}`"
                        class="input-label"
                        >{{
                          t("admin.groups.compositeRoutes.accountType")
                        }}</label
                      ><Select
                        :id="`target-account-${index}`"
                        v-model="target.account_type"
                        :disabled="busy"
                        :options="accountOptions(target)"
                        :label="t('admin.groups.compositeRoutes.accountType')"
                      />
                    </div>
                    <div>
                      <label
                        :for="`target-region-${index}`"
                        class="input-label"
                        >{{ t("admin.groups.compositeRoutes.region") }}</label
                      ><input
                        :id="`target-region-${index}`"
                        v-model="target.region"
                        class="input"
                        :placeholder="
                          t('admin.groups.compositeRoutes.regionPlaceholder')
                        "
                      />
                    </div>
                    <div>
                      <label
                        :for="`target-provider-${index}`"
                        class="input-label"
                        >{{ t("admin.groups.compositeRoutes.provider") }}</label
                      ><input
                        :id="`target-provider-${index}`"
                        v-model="target.provider"
                        class="input"
                        :placeholder="
                          t('admin.groups.compositeRoutes.providerPlaceholder')
                        "
                      />
                    </div>
                    <div>
                      <label
                        :for="`target-latency-${index}`"
                        class="input-label"
                        >{{
                          t("admin.groups.compositeRoutes.latencyPreference")
                        }}</label
                      ><Select
                        :id="`target-latency-${index}`"
                        v-model="target.latency_preference"
                        :disabled="busy"
                        :options="compositeRouteLatencyPreferenceOptions"
                        :label="
                          t('admin.groups.compositeRoutes.latencyPreference')
                        "
                      />
                    </div>
                    <div>
                      <label
                        :for="`target-weight-${index}`"
                        class="input-label"
                        >{{
                          t("admin.groups.compositeRoutes.schedulingWeight")
                        }}</label
                      ><input
                        :id="`target-weight-${index}`"
                        v-model.number="target.scheduling_weight"
                        type="number"
                        min="1"
                        step="1"
                        class="input"
                      />
                    </div>
                    <div>
                      <label :for="`target-ttl-${index}`" class="input-label">{{
                        t("admin.groups.compositeRoutes.affinityTtl")
                      }}</label
                      ><input
                        :id="`target-ttl-${index}`"
                        v-model.number="target.affinity_ttl_seconds"
                        type="number"
                        min="0"
                        max="86400"
                        step="1"
                        class="input"
                      />
                    </div>
                  </div>
                </details>
              </div>
            </div>
            <details :open="!!compositeRouteForm.notes">
              <summary class="cursor-pointer text-sm font-medium">
                {{ t("admin.groups.compositeRoutes.notes") }}
              </summary>
              <label for="composite-notes" class="sr-only">{{
                t("admin.groups.compositeRoutes.notes")
              }}</label
              ><textarea
                id="composite-notes"
                v-model="compositeRouteForm.notes"
                rows="2"
                class="input mt-2"
              />
            </details>
          </fieldset>
        </form>
      </section>
    </div>
    <template #footer>
      <div class="flex w-full flex-wrap items-center justify-between gap-3">
        <span
          role="status"
          class="text-sm"
          :class="
            compositeRouteDraftDirty
              ? 'text-amber-700 dark:text-amber-400'
              : 'text-gray-500'
          "
          ><span v-if="compositeRouteSaving">{{ t("common.saving") }}</span
          ><span v-else>{{
            compositeRouteDraftDirty
              ? t("admin.groups.unsavedChanges")
              : t("admin.groups.compositeRoutes.noUnsavedChanges")
          }}</span></span
        >
        <div class="flex gap-2">
          <button
            type="button"
            class="btn btn-secondary"
            :disabled="busy"
            @click="closeCompositeRoutesModal"
          >
            {{ t("common.close") }}
          </button>
          <button
            v-if="compositeRouteEditingId"
            type="button"
            class="btn btn-secondary"
            :disabled="busy"
            @click="startNewRoute"
          >
            {{ t("common.cancel") }}
          </button>
          <button
            type="submit"
            form="composite-route-form"
            class="btn btn-primary"
            :disabled="busy"
          >
            {{
              compositeRouteSaving
                ? t("common.saving")
                : compositeRouteEditingId
                  ? t("common.update")
                  : t("common.create")
            }}
          </button>
        </div>
      </div>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, nextTick, ref } from "vue";
import { useI18n } from "vue-i18n";
import BaseDialog from "@/components/common/BaseDialog.vue";
import Select from "@/components/common/Select.vue";
import Icon from "@/components/icons/Icon.vue";
import { HOSTED_ACCOUNT_OPTIONS } from "@/constants/platforms";
import type { CompositeModelRoute, CompositeRouteTarget } from "@/types";
import type { useCompositeRoutes } from "./useCompositeRoutes";
const props = defineProps<{
  controller: ReturnType<typeof useCompositeRoutes>;
}>();
const { t } = useI18n();
const {
  compositeRoutePlatformOptions,
  compositeRouteEndpointOptions,
  compositeRouteMatchOptions,
  compositeRouteLatencyPreferenceOptions,
  compositeRouteMaxTargets,
  showCompositeRoutesModal,
  compositeRoutesGroup,
  compositeRoutes,
  compositeRoutesLoading,
  compositeRouteSaving,
  compositeRouteEditingId,
  compositePreviewModel,
  compositePreviewEndpoint,
  compositePreviewLoading,
  compositePreviewDecision,
  compositeRouteForm,
  compositeRouteMatchLabel,
  formatCompositeEndpoint,
  formatCompositePlatform,
  compositeRouteSourceLabel,
  compositeRouteTargetCount,
  loadCompositeRoutes,
  closeCompositeRoutesModal,
  editCompositeRoute,
  addCompositeRouteTarget,
  removeCompositeRouteTarget,
  moveCompositeRouteTarget,
  saveCompositeRoute,
  deleteCompositeRoute,
  previewCompositeRoute,
  compositeRouteSearchQuery,
  filteredCompositeRoutes,
  compositeRouteDraftDirty,
  newCompositeRoute,
  compositeRouteTargetRole,
  filterCompositeRouteModelSuggestions,
  compositeRouteSuggestionStatus,
  compositeRouteDeletingId,
  compositeRouteLoadFailed,
  compositePreviewFailed,
} = props.controller;
const busy = computed(
  () => compositeRouteSaving.value || compositeRouteDeletingId.value !== null,
);
const publicModelInput = ref<HTMLInputElement>();
const selectRoute = async (route: CompositeModelRoute) => {
  if (editCompositeRoute(route)) {
    await nextTick();
    publicModelInput.value?.focus();
  }
};
const startNewRoute = async () => {
  if (newCompositeRoute()) {
    await nextTick();
    publicModelInput.value?.focus();
  }
};
const firstTarget = (route: CompositeModelRoute) =>
  [...(route.targets ?? [])]
    .sort((a, b) => a.position - b.position)
    .find((target) => target.enabled);
const previewTargets = computed(() =>
  [...(compositePreviewDecision.value?.targets ?? [])]
    .filter((target) => target.enabled)
    .sort((a, b) => a.position - b.position),
);
const targetActionLabel = (action: string, index: number) =>
  `${t(`admin.groups.compositeRoutes.${action}`)}: ${t("admin.groups.compositeRoutes.targetLabel", { index: index + 1 })}`;
type TargetForm = (typeof compositeRouteForm.targets)[number];
// Keep native details state and keyboard focus attached to the target when it moves.
const targetKeys = new WeakMap<TargetForm, number>();
let nextTargetKey = 0;
const targetKey = (target: TargetForm) => {
  if (!targetKeys.has(target)) targetKeys.set(target, ++nextTargetKey);
  return targetKeys.get(target)!;
};
const hasAdvancedOptions = (target: CompositeRouteTarget | TargetForm) =>
  !!(
    target.account_type ||
    target.region ||
    target.provider ||
    target.latency_preference === "low_latency" ||
    target.scheduling_weight !== 100 ||
    target.affinity_ttl_seconds
  );
const advancedSummary = (target: TargetForm) =>
  [
    target.account_type,
    target.region,
    target.provider,
    target.latency_preference === "low_latency"
      ? t("admin.groups.compositeRoutes.latencyPreferences.lowLatency")
      : "",
    target.scheduling_weight !== 100
      ? `${t("admin.groups.compositeRoutes.schedulingWeight")} ${target.scheduling_weight}`
      : "",
    target.affinity_ttl_seconds
      ? `${t("admin.groups.compositeRoutes.affinityTtl")} ${target.affinity_ttl_seconds}`
      : "",
  ]
    .filter(Boolean)
    .join(" · ");
const accountOptions = (target: TargetForm) => {
  const options = [
    { value: "", label: t("admin.groups.compositeRoutes.anyAccountType") },
    ...HOSTED_ACCOUNT_OPTIONS.filter(
      (option) => option.platform === target.target_platform,
    ).map((option) => ({
      value: String(option.type),
      label: String(option.label),
    })),
  ];
  if (
    target.account_type &&
    !options.some((option) => option.value === target.account_type)
  )
    options.push({ value: target.account_type, label: target.account_type });
  return options;
};
</script>
