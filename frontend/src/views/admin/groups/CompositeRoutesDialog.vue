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
    width="wide"
    @close="closeCompositeRoutesModal"
  >
    <div
      class="grid gap-5 lg:grid-cols-[minmax(0,1.15fr)_minmax(320px,0.85fr)]"
    >
      <section class="min-w-0">
        <div class="mb-3 flex items-center justify-between gap-3">
          <h3 class="text-sm font-semibold text-gray-900 dark:text-white">
            {{ t("admin.groups.compositeRoutes.routes") }}
          </h3>
          <button
            type="button"
            class="btn btn-secondary btn-sm"
            :disabled="compositeRoutesLoading"
            @click="loadCompositeRoutes"
          >
            <Icon
              name="refresh"
              size="sm"
              :class="compositeRoutesLoading ? 'animate-spin' : ''"
            />
          </button>
        </div>

        <div
          class="overflow-hidden rounded-lg border border-gray-200 dark:border-dark-600"
        >
          <div
            v-if="compositeRoutesLoading"
            class="flex h-36 items-center justify-center text-sm text-gray-500 dark:text-gray-400"
          >
            {{ t("common.loading") }}
          </div>
          <div
            v-else-if="compositeRoutes.length === 0"
            class="flex h-36 items-center justify-center text-sm text-gray-500 dark:text-gray-400"
          >
            {{ t("admin.groups.compositeRoutes.empty") }}
          </div>
          <div v-else class="overflow-x-auto">
            <table
              class="min-w-full divide-y divide-gray-200 text-sm dark:divide-dark-600"
            >
              <thead
                class="bg-gray-50 text-left text-xs font-medium uppercase tracking-wide text-gray-500 dark:bg-dark-800 dark:text-gray-400"
              >
                <tr>
                  <th class="px-3 py-2">
                    {{ t("admin.groups.compositeRoutes.publicModel") }}
                  </th>
                  <th class="px-3 py-2">
                    {{ t("admin.groups.compositeRoutes.target") }}
                  </th>
                  <th class="px-3 py-2">
                    {{ t("admin.groups.compositeRoutes.scope") }}
                  </th>
                  <th class="px-3 py-2 text-right">
                    {{ t("admin.groups.columns.actions") }}
                  </th>
                </tr>
              </thead>
              <tbody
                class="divide-y divide-gray-100 bg-white dark:divide-dark-700 dark:bg-dark-900"
              >
                <tr
                  v-for="route in compositeRoutes"
                  :key="route.id"
                  :class="!route.enabled && 'opacity-60'"
                >
                  <td class="max-w-[15rem] px-3 py-2">
                    <div
                      class="break-all font-medium text-gray-900 dark:text-white"
                    >
                      {{ route.public_model }}
                    </div>
                    <div class="mt-1 flex flex-wrap items-center gap-1.5">
                      <span class="badge badge-gray">{{
                        compositeRouteMatchLabel(route.match_type)
                      }}</span>
                      <span v-if="!route.enabled" class="badge badge-danger">
                        {{ t("admin.accounts.status.inactive") }}
                      </span>
                    </div>
                  </td>
                  <td class="px-3 py-2">
                    <div
                      class="flex items-center gap-1.5 text-gray-900 dark:text-white"
                    >
                      <PlatformIcon
                        :platform="route.target_platform"
                        size="xs"
                      />
                      <span>{{
                        formatCompositePlatform(route.target_platform)
                      }}</span>
                    </div>
                    <div
                      class="mt-1 break-all text-xs text-gray-500 dark:text-gray-400"
                    >
                      {{ route.upstream_model || route.public_model }}
                    </div>
                    <div
                      v-if="compositeRouteTargetCount(route) > 1"
                      class="mt-1 text-xs text-gray-500 dark:text-gray-400"
                    >
                      {{
                        t("admin.groups.compositeRoutes.targetCount", {
                          count: compositeRouteTargetCount(route),
                        })
                      }}
                    </div>
                  </td>
                  <td class="px-3 py-2">
                    <div class="text-gray-700 dark:text-gray-300">
                      {{ formatCompositeEndpoint(route.endpoint) }}
                    </div>
                    <div class="text-xs text-gray-500 dark:text-gray-400">
                      {{ t("admin.groups.compositeRoutes.priority") }}:
                      {{ route.priority }}
                    </div>
                  </td>
                  <td class="px-3 py-2">
                    <div class="flex justify-end gap-1">
                      <button
                        type="button"
                        class="rounded p-1.5 text-gray-500 hover:bg-gray-100 hover:text-primary-600 dark:hover:bg-dark-700 dark:hover:text-primary-400"
                        :title="t('common.edit')"
                        @click="editCompositeRoute(route)"
                      >
                        <Icon name="edit" size="sm" />
                      </button>
                      <button
                        type="button"
                        class="rounded p-1.5 text-gray-500 hover:bg-red-50 hover:text-red-600 dark:hover:bg-red-900/20 dark:hover:text-red-400"
                        :title="t('common.delete')"
                        @click="deleteCompositeRoute(route)"
                      >
                        <Icon name="trash" size="sm" />
                      </button>
                    </div>
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
        </div>
      </section>

      <section class="space-y-5">
        <form class="space-y-3" @submit.prevent="saveCompositeRoute">
          <div class="flex items-center justify-between gap-3">
            <h3 class="text-sm font-semibold text-gray-900 dark:text-white">
              {{
                compositeRouteEditingId
                  ? t("admin.groups.compositeRoutes.editRoute")
                  : t("admin.groups.compositeRoutes.addRoute")
              }}
            </h3>
            <button
              v-if="compositeRouteEditingId"
              type="button"
              class="text-xs font-medium text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200"
              @click="resetCompositeRouteForm"
            >
              {{ t("common.cancel") }}
            </button>
          </div>

          <div>
            <label class="input-label">{{
              t("admin.groups.compositeRoutes.publicModel")
            }}</label>
            <input
              v-model.trim="compositeRouteForm.public_model"
              type="text"
              class="input"
              required
              placeholder="openrouter/gpt-5"
            />
          </div>

          <div class="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <div>
              <label class="input-label">{{
                t("admin.groups.compositeRoutes.matchType")
              }}</label>
              <Select
                v-model="compositeRouteForm.match_type"
                :options="compositeRouteMatchOptions"
              />
            </div>
            <div>
              <label class="input-label">{{
                t("admin.groups.compositeRoutes.endpoint")
              }}</label>
              <Select
                v-model="compositeRouteForm.endpoint"
                :options="compositeRouteEndpointOptions"
              />
            </div>
          </div>

          <div>
            <label class="input-label">{{
              t("admin.groups.compositeRoutes.priority")
            }}</label>
            <input
              v-model.number="compositeRouteForm.priority"
              type="number"
              min="1"
              step="1"
              class="input"
            />
          </div>

          <div class="space-y-2">
            <div class="flex items-center justify-between gap-2">
              <label class="input-label">{{
                t("admin.groups.compositeRoutes.targetsTitle")
              }}</label>
              <button
                type="button"
                class="btn btn-secondary btn-sm"
                :disabled="
                  compositeRouteForm.targets.length >= compositeRouteMaxTargets
                "
                @click="addCompositeRouteTarget"
              >
                <Icon name="plus" size="sm" class="mr-1" />
                {{ t("admin.groups.compositeRoutes.addTarget") }}
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
              :key="index"
              class="space-y-2 rounded-lg border border-gray-200 p-3 dark:border-dark-600"
            >
              <div class="flex items-center justify-between gap-2">
                <span
                  class="flex items-center gap-1.5 text-xs font-semibold text-gray-700 dark:text-gray-300"
                >
                  {{
                    t("admin.groups.compositeRoutes.targetLabel", {
                      index: index + 1,
                    })
                  }}
                  <span v-if="index === 0" class="badge badge-gray">
                    {{ t("admin.groups.compositeRoutes.primaryTarget") }}
                  </span>
                </span>
                <div class="flex items-center gap-1">
                  <button
                    type="button"
                    class="rounded p-1.5 text-gray-500 hover:bg-gray-100 hover:text-primary-600 disabled:cursor-not-allowed disabled:opacity-40 dark:hover:bg-dark-700 dark:hover:text-primary-400"
                    :disabled="index === 0"
                    :title="t('admin.groups.compositeRoutes.moveTargetUp')"
                    @click="moveCompositeRouteTarget(index, -1)"
                  >
                    <Icon name="chevronUp" size="sm" />
                  </button>
                  <button
                    type="button"
                    class="rounded p-1.5 text-gray-500 hover:bg-gray-100 hover:text-primary-600 disabled:cursor-not-allowed disabled:opacity-40 dark:hover:bg-dark-700 dark:hover:text-primary-400"
                    :disabled="index === compositeRouteForm.targets.length - 1"
                    :title="t('admin.groups.compositeRoutes.moveTargetDown')"
                    @click="moveCompositeRouteTarget(index, 1)"
                  >
                    <Icon name="chevronDown" size="sm" />
                  </button>
                  <button
                    type="button"
                    class="rounded p-1.5 text-gray-500 hover:bg-red-50 hover:text-red-600 disabled:cursor-not-allowed disabled:opacity-40 dark:hover:bg-red-900/20 dark:hover:text-red-400"
                    :disabled="compositeRouteForm.targets.length <= 1"
                    :title="t('admin.groups.compositeRoutes.removeTarget')"
                    @click="removeCompositeRouteTarget(index)"
                  >
                    <Icon name="trash" size="sm" />
                  </button>
                </div>
              </div>

              <div class="grid grid-cols-1 gap-2 sm:grid-cols-2">
                <div>
                  <span class="input-label">{{
                    t("admin.groups.compositeRoutes.targetPlatform")
                  }}</span>
                  <Select
                    v-model="target.target_platform"
                    :options="compositeRoutePlatformOptions"
                  />
                </div>
                <div>
                  <span class="input-label">{{
                    t("admin.groups.compositeRoutes.upstreamModel")
                  }}</span>
                  <input
                    v-model.trim="target.upstream_model"
                    type="text"
                    class="input"
                    placeholder="gpt-5"
                  />
                </div>
                <div>
                  <span class="input-label">{{
                    t("admin.groups.compositeRoutes.region")
                  }}</span>
                  <input
                    v-model.trim="target.region"
                    type="text"
                    class="input"
                    :placeholder="
                      t('admin.groups.compositeRoutes.regionPlaceholder')
                    "
                  />
                </div>
                <div>
                  <span class="input-label">{{
                    t("admin.groups.compositeRoutes.provider")
                  }}</span>
                  <input
                    v-model.trim="target.provider"
                    type="text"
                    class="input"
                    :placeholder="
                      t('admin.groups.compositeRoutes.providerPlaceholder')
                    "
                  />
                </div>
                <div>
                  <span class="input-label">{{
                    t("admin.groups.compositeRoutes.latencyPreference")
                  }}</span>
                  <Select
                    v-model="target.latency_preference"
                    :options="compositeRouteLatencyPreferenceOptions"
                  />
                </div>
                <div>
                  <span class="input-label">{{
                    t("admin.groups.compositeRoutes.schedulingWeight")
                  }}</span>
                  <input
                    v-model.number="target.scheduling_weight"
                    type="number"
                    min="1"
                    step="1"
                    class="input"
                  />
                </div>
                <div>
                  <span class="input-label">{{
                    t("admin.groups.compositeRoutes.affinityTtl")
                  }}</span>
                  <input
                    v-model.number="target.affinity_ttl_seconds"
                    type="number"
                    min="0"
                    max="86400"
                    step="60"
                    class="input"
                  />
                </div>
                <label
                  class="flex items-center gap-2 self-end pb-2.5 text-sm text-gray-700 dark:text-gray-300"
                >
                  <input
                    v-model="target.enabled"
                    type="checkbox"
                    class="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500 dark:border-dark-600 dark:bg-dark-700"
                  />
                  {{ t("admin.groups.compositeRoutes.enabled") }}
                </label>
              </div>
            </div>
          </div>

          <div>
            <label class="input-label">{{
              t("admin.groups.compositeRoutes.notes")
            }}</label>
            <textarea
              v-model.trim="compositeRouteForm.notes"
              rows="2"
              class="input"
            ></textarea>
          </div>

          <div class="flex items-center justify-between gap-3">
            <label
              class="flex items-center gap-2 text-sm text-gray-700 dark:text-gray-300"
            >
              <input
                v-model="compositeRouteForm.enabled"
                type="checkbox"
                class="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500 dark:border-dark-600 dark:bg-dark-700"
              />
              {{ t("admin.groups.compositeRoutes.enabled") }}
            </label>
            <button
              type="submit"
              class="btn btn-primary"
              :disabled="compositeRouteSaving"
            >
              <Icon
                v-if="!compositeRouteSaving"
                name="check"
                size="sm"
                class="mr-2"
              />
              {{
                compositeRouteEditingId
                  ? t("common.update")
                  : t("common.create")
              }}
            </button>
          </div>
        </form>

        <div class="border-t border-gray-200 pt-4 dark:border-dark-600">
          <h3 class="mb-3 text-sm font-semibold text-gray-900 dark:text-white">
            {{ t("admin.groups.compositeRoutes.preview") }}
          </h3>
          <div class="space-y-3">
            <input
              v-model.trim="compositePreviewModel"
              type="text"
              class="input"
              placeholder="openrouter/gpt-5"
              @keyup.enter="previewCompositeRoute"
            />
            <div class="flex gap-2">
              <Select
                v-model="compositePreviewEndpoint"
                :options="compositeRouteEndpointOptions"
                class="min-w-0 flex-1"
              />
              <button
                type="button"
                class="btn btn-secondary"
                :disabled="compositePreviewLoading || !compositePreviewModel"
                @click="previewCompositeRoute"
              >
                <Icon name="play" size="sm" />
              </button>
            </div>

            <div
              v-if="compositePreviewDecision"
              class="rounded-lg border border-gray-200 bg-gray-50 p-3 text-sm dark:border-dark-600 dark:bg-dark-800"
            >
              <div class="mb-2 flex items-center gap-2">
                <span
                  :class="[
                    'badge',
                    compositePreviewDecision.matched
                      ? 'badge-success'
                      : 'badge-danger',
                  ]"
                >
                  {{
                    compositePreviewDecision.matched
                      ? t("admin.groups.compositeRoutes.matched")
                      : t("admin.groups.compositeRoutes.notMatched")
                  }}
                </span>
                <span class="badge badge-gray">
                  {{
                    compositeRouteSourceLabel(compositePreviewDecision.source)
                  }}
                </span>
              </div>
              <div
                v-if="compositePreviewDecision.matched"
                class="space-y-1 text-gray-700 dark:text-gray-300"
              >
                <div>
                  {{ t("admin.groups.compositeRoutes.targetPlatform") }}:
                  {{
                    formatCompositePlatform(
                      compositePreviewDecision.target_platform,
                    )
                  }}
                </div>
                <div class="break-all">
                  {{ t("admin.groups.compositeRoutes.upstreamModel") }}:
                  {{ compositePreviewDecision.upstream_model }}
                </div>
              </div>
              <div v-else class="text-gray-500 dark:text-gray-400">
                {{ compositePreviewDecision.reason }}
              </div>
            </div>
          </div>
        </div>
      </section>
    </div>

    <template #footer>
      <div class="flex justify-end pt-4">
        <button
          type="button"
          class="btn btn-secondary"
          @click="closeCompositeRoutesModal"
        >
          {{ t("common.close") }}
        </button>
      </div>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { useI18n } from "vue-i18n";
import BaseDialog from "@/components/common/BaseDialog.vue";
import Select from "@/components/common/Select.vue";
import PlatformIcon from "@/components/common/PlatformIcon.vue";
import Icon from "@/components/icons/Icon.vue";
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
  resetCompositeRouteForm,
  loadCompositeRoutes,
  closeCompositeRoutesModal,
  editCompositeRoute,
  addCompositeRouteTarget,
  removeCompositeRouteTarget,
  moveCompositeRouteTarget,
  saveCompositeRoute,
  deleteCompositeRoute,
  previewCompositeRoute,
} = props.controller;
</script>
