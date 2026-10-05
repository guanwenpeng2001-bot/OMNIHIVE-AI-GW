import { ref, reactive, computed, watch } from "vue";
import { useI18n } from "vue-i18n";
import { useAppStore } from "@/stores/app";
import { adminAPI } from "@/api/admin";
import type {
  AdminGroup,
  CompositeModelRoute,
  CompositeModelRouteInput,
  CompositeRouteDecision,
  CompositeRouteEndpoint,
  CompositeRouteLatencyPreference,
  CompositeRouteMatchType,
  CompositeRouteTarget,
  CompositeRouteTargetInput,
  GroupPlatform,
} from "@/types";
import type { PRODUCT_PLATFORM_OPTIONS } from "@/constants/platforms";
import { getPlatformLabel } from "@/constants/platforms";
import { extractApiErrorMessage } from "@/utils/apiError";

const COMPOSITE_ROUTE_MAX_TARGETS = 16;
const COMPOSITE_ROUTE_DEFAULT_SCHEDULING_WEIGHT = 100;
const COMPOSITE_ROUTE_MIN_AFFINITY_TTL_SECONDS = 60;
const COMPOSITE_ROUTE_MAX_AFFINITY_TTL_SECONDS = 86400;

export function useCompositeRoutes(
  platformCatalog: readonly (typeof PRODUCT_PLATFORM_OPTIONS)[number][],
) {
  const { t } = useI18n();
  const appStore = useAppStore();
  const compositeRoutePlatformOptions = computed(() =>
    platformCatalog.map((option) => ({
      ...option,
      label: getPlatformLabel(option.value, t),
    })),
  );

  const compositeRouteEndpointOptions = computed(() => [
    { value: "any", label: t("admin.groups.compositeRoutes.endpoints.any") },
    {
      value: "messages",
      label: t("admin.groups.compositeRoutes.endpoints.messages"),
    },
    {
      value: "count_tokens",
      label: t("admin.groups.compositeRoutes.endpoints.countTokens"),
    },
    {
      value: "responses",
      label: t("admin.groups.compositeRoutes.endpoints.responses"),
    },
    {
      value: "chat_completions",
      label: t("admin.groups.compositeRoutes.endpoints.chatCompletions"),
    },
    {
      value: "embeddings",
      label: t("admin.groups.compositeRoutes.endpoints.embeddings"),
    },
    {
      value: "images",
      label: t("admin.groups.compositeRoutes.endpoints.images"),
    },
    {
      value: "gemini",
      label: t("admin.groups.compositeRoutes.endpoints.gemini"),
    },
  ]);

  const compositeRouteMatchOptions = computed(() => [
    { value: "exact", label: t("admin.groups.compositeRoutes.match.exact") },
    { value: "prefix", label: t("admin.groups.compositeRoutes.match.prefix") },
  ]);

  const compositeRouteLatencyPreferenceOptions = computed(() => [
    {
      value: "default",
      label: t("admin.groups.compositeRoutes.latencyPreferences.default"),
    },
    {
      value: "low_latency",
      label: t("admin.groups.compositeRoutes.latencyPreferences.lowLatency"),
    },
  ]);

  type ConcreteGroupPlatform = Exclude<GroupPlatform, "composite">;

  type CompositeRouteTargetFormState = {
    target_platform: ConcreteGroupPlatform;
    account_type: string;
    upstream_model: string;
    region: string;
    provider: string;
    latency_preference: CompositeRouteLatencyPreference;
    scheduling_weight: number;
    affinity_ttl_seconds: number;
    enabled: boolean;
  };

  type CompositeRouteFormState = {
    public_model: string;
    match_type: CompositeRouteMatchType;
    target_platform: ConcreteGroupPlatform;
    upstream_model: string;
    targets: CompositeRouteTargetFormState[];
    endpoint: CompositeRouteEndpoint;
    priority: number;
    enabled: boolean;
    notes: string;
  };

  const createCompositeRouteTargetRow = (
    targetPlatform: ConcreteGroupPlatform,
    overrides: Partial<CompositeRouteTargetFormState> = {},
  ): CompositeRouteTargetFormState => ({
    target_platform: targetPlatform,
    account_type: "",
    upstream_model: "",
    region: "",
    provider: "",
    latency_preference: "default",
    scheduling_weight: COMPOSITE_ROUTE_DEFAULT_SCHEDULING_WEIGHT,
    affinity_ttl_seconds: 0,
    enabled: true,
    ...overrides,
  });

  const compositeRouteTargetRows = ref<CompositeRouteTargetFormState[]>([]);

  const showCompositeRoutesModal = ref(false);

  const compositeRoutesGroup = ref<AdminGroup | null>(null);

  const compositeRoutes = ref<CompositeModelRoute[]>([]);

  const compositeRoutesLoading = ref(false);

  const compositeRouteSaving = ref(false);

  const compositeRouteEditingId = ref<number | null>(null);

  const compositePreviewModel = ref("");

  const compositePreviewEndpoint = ref<CompositeRouteEndpoint>("any");

  const compositePreviewLoading = ref(false);

  const compositePreviewDecision = ref<CompositeRouteDecision | null>(null);
  const compositeRouteSearchQuery = ref("");
  const compositeRouteDeletingId = ref<number | null>(null);
  const compositeRouteLoadFailed = ref(false);
  const compositePreviewFailed = ref(false);
  let dialogSession = 0;
  let listRequest = 0;
  let previewRequest = 0;

  // 顶层 target_platform / upstream_model 始终镜像 targets[0]，保证载荷里的旧字段与目标列表一致。
  const compositeRouteForm = reactive<CompositeRouteFormState>({
    public_model: "",
    match_type: "exact",
    endpoint: "any",
    priority: 100,
    enabled: true,
    notes: "",
    get targets() {
      return compositeRouteTargetRows.value;
    },
    set targets(rows) {
      compositeRouteTargetRows.value = rows;
    },
    get target_platform() {
      return compositeRouteTargetRows.value[0]?.target_platform ?? "openai";
    },
    set target_platform(targetPlatform) {
      const primaryTarget = compositeRouteTargetRows.value[0];
      if (primaryTarget) primaryTarget.target_platform = targetPlatform;
    },
    get upstream_model() {
      return compositeRouteTargetRows.value[0]?.upstream_model ?? "";
    },
    set upstream_model(upstreamModel) {
      const primaryTarget = compositeRouteTargetRows.value[0];
      if (primaryTarget) primaryTarget.upstream_model = upstreamModel;
    },
  });

  const compositeRouteMatchLabel = (matchType: CompositeRouteMatchType) =>
    compositeRouteMatchOptions.value.find(
      (option) => option.value === matchType,
    )?.label || matchType;

  const draftSnapshot = () => JSON.stringify(compositeRouteForm);
  const savedDraft = ref(draftSnapshot());
  const compositeRouteDraftDirty = computed(
    () => draftSnapshot() !== savedDraft.value,
  );
  const canDiscardDraft = () =>
    !compositeRouteSaving.value &&
    (!compositeRouteDraftDirty.value ||
      window.confirm(t("admin.groups.compositeRoutes.discardConfirm")));
  const invalidatePreview = () => {
    previewRequest += 1;
    compositePreviewDecision.value = null;
    compositePreviewLoading.value = false;
    compositePreviewFailed.value = false;
  };
  watch(
    [draftSnapshot, compositePreviewModel, compositePreviewEndpoint],
    invalidatePreview,
    { flush: "sync" },
  );

  const filteredCompositeRoutes = computed(() => {
    const query = compositeRouteSearchQuery.value.trim().toLowerCase();
    return compositeRoutes.value.filter(
      (route) =>
        !query ||
        [
          route.public_model,
          route.notes,
          route.upstream_model,
          formatCompositePlatform(route.target_platform),
          formatCompositeEndpoint(route.endpoint),
          compositeRouteMatchLabel(route.match_type),
          ...(route.targets ?? []).flatMap((target) => [
            target.upstream_model,
            formatCompositePlatform(target.target_platform),
          ]),
        ]
          .join(" ")
          .toLowerCase()
          .includes(query),
    );
  });

  const compositeRouteSuggestionStatus = ref<
    Record<string, "loading" | "ready" | "error">
  >({});
  const modelSuggestions = ref<Record<string, string[]>>({});
  const loadCompositeRouteModelSuggestions = async (
    platform: ConcreteGroupPlatform,
  ) => {
    if (compositeRouteSuggestionStatus.value[platform]) return;
    const session = dialogSession;
    compositeRouteSuggestionStatus.value[platform] = "loading";
    try {
      const models = await adminAPI.groups.getModelAllowlistCandidates(
        0,
        platform,
      );
      if (session !== dialogSession) return;
      modelSuggestions.value[platform] = models;
      compositeRouteSuggestionStatus.value[platform] = "ready";
    } catch {
      if (session === dialogSession)
        compositeRouteSuggestionStatus.value[platform] = "error";
    }
  };
  const filterCompositeRouteModelSuggestions = (
    query: string,
    platform: ConcreteGroupPlatform,
    publicModel = false,
  ) => {
    const search = query.trim().toLowerCase();
    if (!search) return [];
    const savedModels = compositeRoutes.value.flatMap((route) =>
      publicModel
        ? [route.public_model]
        : route.targets?.length
          ? route.targets
              .filter((target) => target.target_platform === platform)
              .map((target) => target.upstream_model ?? "")
          : route.target_platform === platform
            ? [route.upstream_model]
            : [],
    );
    return [
      ...new Set([...(modelSuggestions.value[platform] ?? []), ...savedModels]),
    ]
      .filter(
        (model) => model.toLowerCase().includes(search) && model !== query,
      )
      .sort(
        (a, b) =>
          Number(b.toLowerCase().startsWith(search)) -
            Number(a.toLowerCase().startsWith(search)) || a.localeCompare(b),
      )
      .slice(0, 8);
  };
  watch(
    () => compositeRouteForm.targets.map((target) => target.target_platform),
    (platforms) => {
      if (showCompositeRoutesModal.value)
        platforms.forEach((platform) => {
          void loadCompositeRouteModelSuggestions(platform);
        });
    },
  );

  const compositeRouteTargetRole = (index: number) => {
    if (!compositeRouteForm.targets[index]?.enabled)
      return t("admin.groups.compositeRoutes.skippedTarget");
    const order = compositeRouteForm.targets
      .slice(0, index + 1)
      .filter((target) => target.enabled).length;
    return order === 1
      ? t("admin.groups.compositeRoutes.primaryAttempt")
      : t("admin.groups.compositeRoutes.attemptOrder", { order });
  };

  const formatCompositeEndpoint = (endpoint: CompositeRouteEndpoint) =>
    compositeRouteEndpointOptions.value.find(
      (option) => option.value === endpoint,
    )?.label || endpoint;

  const formatCompositePlatform = (platform: string) => {
    if (!platform) return "—";
    return getPlatformLabel(platform, t);
  };

  const compositeRouteSourceLabel = (source: string) => {
    if (source === "account_model")
      return t("admin.groups.compositeRoutes.sources.account");
    if (source === "route")
      return t("admin.groups.compositeRoutes.sources.route");
    if (source === "detector") {
      return t("admin.groups.compositeRoutes.sources.detector");
    }
    return source || "—";
  };

  const resetCompositeRouteForm = () => {
    compositeRouteEditingId.value = null;
    compositeRouteForm.public_model = "";
    compositeRouteForm.match_type = "exact";
    compositeRouteForm.targets = [createCompositeRouteTargetRow("openai")];
    compositeRouteForm.target_platform = "openai";
    compositeRouteForm.upstream_model = "";
    compositeRouteForm.endpoint = "any";
    compositeRouteForm.priority = 100;
    compositeRouteForm.enabled = true;
    compositeRouteForm.notes = "";
    savedDraft.value = draftSnapshot();
    invalidatePreview();
  };
  const newCompositeRoute = () => {
    if (!canDiscardDraft()) return false;
    resetCompositeRouteForm();
    return true;
  };

  const toCompositeRouteTargetRow = (
    target: CompositeRouteTarget,
  ): CompositeRouteTargetFormState =>
    createCompositeRouteTargetRow(target.target_platform, {
      account_type: target.account_type ?? "",
      upstream_model: target.upstream_model ?? "",
      region: target.region ?? "",
      provider: target.provider ?? "",
      latency_preference:
        target.latency_preference === "low_latency" ? "low_latency" : "default",
      scheduling_weight:
        target.scheduling_weight ?? COMPOSITE_ROUTE_DEFAULT_SCHEDULING_WEIGHT,
      affinity_ttl_seconds: target.affinity_ttl_seconds ?? 0,
      enabled: target.enabled !== false,
    });

  const compositeRouteTargetRowsFromRoute = (
    route: CompositeModelRoute,
  ): CompositeRouteTargetFormState[] => {
    const targets = [...(route.targets ?? [])].sort(
      (a, b) => a.position - b.position,
    );
    return targets.length > 0
      ? targets.map(toCompositeRouteTargetRow)
      : [
          createCompositeRouteTargetRow(route.target_platform, {
            account_type: route.account_type ?? "",
            upstream_model: route.upstream_model ?? "",
          }),
        ];
  };

  const addCompositeRouteTarget = () => {
    if (compositeRouteForm.targets.length >= COMPOSITE_ROUTE_MAX_TARGETS)
      return;
    compositeRouteForm.targets.push(
      createCompositeRouteTargetRow(compositeRouteForm.target_platform),
    );
  };

  const removeCompositeRouteTarget = (index: number) => {
    if (compositeRouteForm.targets.length <= 1) return;
    compositeRouteForm.targets.splice(index, 1);
  };

  const moveCompositeRouteTarget = (index: number, offset: number) => {
    const target = compositeRouteForm.targets[index];
    const destination = index + offset;
    if (
      !target ||
      destination < 0 ||
      destination >= compositeRouteForm.targets.length
    ) {
      return;
    }
    compositeRouteForm.targets.splice(index, 1);
    compositeRouteForm.targets.splice(destination, 0, target);
  };

  const compositeRouteTargetCount = (route: CompositeModelRoute) =>
    route.targets?.length ?? 0;

  /** 单个目标且可选字段全为默认值时等价于旧的单目标语义，此时不发送 targets。 */
  const compositeRouteTargetsUseLegacyDefaults = () => {
    const [primaryTarget, ...fallbackTargets] = compositeRouteForm.targets;
    if (!primaryTarget || fallbackTargets.length > 0) return false;
    return (
      !primaryTarget.account_type &&
      !primaryTarget.region.trim() &&
      !primaryTarget.provider.trim() &&
      primaryTarget.latency_preference === "default" &&
      Number(primaryTarget.scheduling_weight) ===
        COMPOSITE_ROUTE_DEFAULT_SCHEDULING_WEIGHT &&
      Number(primaryTarget.affinity_ttl_seconds) === 0 &&
      primaryTarget.enabled
    );
  };

  const toCompositeRouteTargetInput = (
    target: CompositeRouteTargetFormState,
    position: number,
  ): CompositeRouteTargetInput => ({
    position,
    target_platform: target.target_platform,
    account_type: target.account_type,
    upstream_model: target.upstream_model.trim(),
    region: target.region.trim(),
    provider: target.provider.trim(),
    latency_preference: target.latency_preference,
    scheduling_weight: Number(target.scheduling_weight),
    affinity_ttl_seconds: Number(target.affinity_ttl_seconds),
    enabled: target.enabled,
  });

  const compositeRouteTargetsValidationError = (): string | null => {
    const targets = compositeRouteForm.targets;
    if (targets.length === 0) {
      return t("admin.groups.compositeRoutes.targetsRequired");
    }
    if (targets.length > COMPOSITE_ROUTE_MAX_TARGETS) {
      return t("admin.groups.compositeRoutes.tooManyTargets", {
        max: COMPOSITE_ROUTE_MAX_TARGETS,
      });
    }
    if (!targets.some((target) => target.enabled)) {
      return t("admin.groups.compositeRoutes.targetsEnabledRequired");
    }
    for (const target of targets) {
      if (!target.target_platform) {
        return t("admin.groups.compositeRoutes.targetPlatformRequired");
      }
      const schedulingWeight = Number(target.scheduling_weight);
      if (!Number.isInteger(schedulingWeight) || schedulingWeight < 1) {
        return t("admin.groups.compositeRoutes.targetWeightInvalid");
      }
      const affinityTtlSeconds = Number(target.affinity_ttl_seconds);
      if (
        !Number.isInteger(affinityTtlSeconds) ||
        (affinityTtlSeconds !== 0 &&
          (affinityTtlSeconds < COMPOSITE_ROUTE_MIN_AFFINITY_TTL_SECONDS ||
            affinityTtlSeconds > COMPOSITE_ROUTE_MAX_AFFINITY_TTL_SECONDS))
      ) {
        return t("admin.groups.compositeRoutes.targetAffinityTtlInvalid");
      }
    }
    return null;
  };

  const toCompositeRouteInput = (): CompositeModelRouteInput => {
    const input: CompositeModelRouteInput = {
      public_model: compositeRouteForm.public_model.trim(),
      match_type: compositeRouteForm.match_type,
      target_platform: compositeRouteForm.target_platform,
      upstream_model: compositeRouteForm.upstream_model.trim(),
      endpoint: compositeRouteForm.endpoint,
      priority: Number(compositeRouteForm.priority) || 100,
      enabled: compositeRouteForm.enabled,
      notes: compositeRouteForm.notes.trim(),
    };
    if (!compositeRouteTargetsUseLegacyDefaults()) {
      input.targets = compositeRouteForm.targets.map(
        toCompositeRouteTargetInput,
      );
    }
    return input;
  };

  const loadCompositeRoutes = async () => {
    if (!compositeRoutesGroup.value) return;
    const groupId = compositeRoutesGroup.value.id;
    const request = ++listRequest;
    const session = dialogSession;
    compositeRoutesLoading.value = true;
    compositeRouteLoadFailed.value = false;
    try {
      const routes = await adminAPI.groups.listCompositeRoutes(groupId);
      if (session !== dialogSession || request !== listRequest) return;
      compositeRoutes.value = routes.sort((a, b) => {
        if (a.priority !== b.priority) return a.priority - b.priority;
        return a.id - b.id;
      });
    } catch (error: unknown) {
      if (session !== dialogSession || request !== listRequest) return;
      compositeRouteLoadFailed.value = true;
      appStore.showError(
        extractApiErrorMessage(error, "") ||
          t("admin.groups.compositeRoutes.failedToLoad"),
      );
      console.error("Error loading composite routes:", error);
    } finally {
      if (session === dialogSession && request === listRequest)
        compositeRoutesLoading.value = false;
    }
  };

  const handleCompositeRoutes = async (group: AdminGroup) => {
    if (showCompositeRoutesModal.value && !canDiscardDraft()) return;
    dialogSession += 1;
    compositeRoutes.value = [];
    compositeRouteSearchQuery.value = "";
    compositeRouteSuggestionStatus.value = {};
    modelSuggestions.value = {};
    compositeRouteDeletingId.value = null;
    compositeRoutesGroup.value = group;
    compositePreviewModel.value = "";
    compositePreviewEndpoint.value = "any";
    compositePreviewDecision.value = null;
    resetCompositeRouteForm();
    showCompositeRoutesModal.value = true;
    void loadCompositeRouteModelSuggestions("openai");
    await loadCompositeRoutes();
  };

  const closeCompositeRoutesModal = () => {
    if (!canDiscardDraft()) return;
    dialogSession += 1;
    showCompositeRoutesModal.value = false;
    compositeRoutesGroup.value = null;
    compositeRoutes.value = [];
    compositePreviewDecision.value = null;
    resetCompositeRouteForm();
  };

  const editCompositeRoute = (route: CompositeModelRoute) => {
    if (compositeRouteEditingId.value === route.id) return false;
    if (!canDiscardDraft()) return false;
    compositeRouteEditingId.value = route.id;
    compositeRouteForm.public_model = route.public_model;
    compositeRouteForm.match_type = route.match_type;
    compositeRouteForm.targets = compositeRouteTargetRowsFromRoute(route);
    compositeRouteForm.endpoint = route.endpoint;
    compositeRouteForm.priority = route.priority || 100;
    compositeRouteForm.enabled = route.enabled;
    compositeRouteForm.notes = route.notes || "";
    savedDraft.value = draftSnapshot();
    invalidatePreview();
    return true;
  };

  const saveCompositeRoute = async () => {
    if (!compositeRoutesGroup.value || compositeRouteSaving.value) return;
    if (!compositeRouteForm.public_model.trim()) {
      appStore.showError(t("admin.groups.compositeRoutes.publicModelRequired"));
      return;
    }
    const targetError = compositeRouteTargetsValidationError();
    if (targetError) {
      appStore.showError(targetError);
      return;
    }
    const payload = toCompositeRouteInput();
    const session = dialogSession;
    const groupId = compositeRoutesGroup.value.id;
    const submittedDraft = draftSnapshot();
    compositeRouteSaving.value = true;
    try {
      if (compositeRouteEditingId.value) {
        await adminAPI.groups.updateCompositeRoute(
          groupId,
          compositeRouteEditingId.value,
          payload,
        );
        appStore.showSuccess(t("admin.groups.compositeRoutes.routeUpdated"));
      } else {
        await adminAPI.groups.createCompositeRoute(groupId, payload);
        appStore.showSuccess(t("admin.groups.compositeRoutes.routeCreated"));
      }
      if (session !== dialogSession) return;
      if (submittedDraft === draftSnapshot()) resetCompositeRouteForm();
      else savedDraft.value = submittedDraft;
      invalidatePreview();
      await loadCompositeRoutes();
    } catch (error: unknown) {
      if (session !== dialogSession) return;
      appStore.showError(
        extractApiErrorMessage(error, "") ||
          t("admin.groups.compositeRoutes.failedToSave"),
      );
      console.error("Error saving composite route:", error);
    } finally {
      if (session === dialogSession) compositeRouteSaving.value = false;
    }
  };

  const deleteCompositeRoute = async (route: CompositeModelRoute) => {
    if (
      !compositeRoutesGroup.value ||
      compositeRouteSaving.value ||
      compositeRouteDeletingId.value !== null
    )
      return;
    if (compositeRouteEditingId.value === route.id && !canDiscardDraft())
      return;
    if (!window.confirm(t("admin.groups.compositeRoutes.deleteConfirm")))
      return;
    const session = dialogSession;
    const groupId = compositeRoutesGroup.value.id;
    compositeRouteDeletingId.value = route.id;
    try {
      await adminAPI.groups.deleteCompositeRoute(groupId, route.id);
      if (session !== dialogSession) return;
      if (compositeRouteEditingId.value === route.id) {
        resetCompositeRouteForm();
      }
      appStore.showSuccess(t("admin.groups.compositeRoutes.routeDeleted"));
      invalidatePreview();
      await loadCompositeRoutes();
    } catch (error: unknown) {
      if (session !== dialogSession) return;
      appStore.showError(
        extractApiErrorMessage(error, "") ||
          t("admin.groups.compositeRoutes.failedToDelete"),
      );
      console.error("Error deleting composite route:", error);
    } finally {
      if (session === dialogSession) compositeRouteDeletingId.value = null;
    }
  };

  const previewCompositeRoute = async () => {
    if (!compositeRoutesGroup.value || !compositePreviewModel.value.trim()) {
      return;
    }
    compositePreviewLoading.value = true;
    compositePreviewDecision.value = null;
    compositePreviewFailed.value = false;
    const request = ++previewRequest;
    const session = dialogSession;
    try {
      const decision = await adminAPI.groups.previewCompositeRoute(
        compositeRoutesGroup.value.id,
        {
          model: compositePreviewModel.value.trim(),
          endpoint: compositePreviewEndpoint.value,
        },
      );
      if (session === dialogSession && request === previewRequest)
        compositePreviewDecision.value = decision;
    } catch (error: unknown) {
      if (session !== dialogSession || request !== previewRequest) return;
      compositePreviewFailed.value = true;
      appStore.showError(
        extractApiErrorMessage(error, "") ||
          t("admin.groups.compositeRoutes.failedToPreview"),
      );
      console.error("Error previewing composite route:", error);
    } finally {
      if (session === dialogSession && request === previewRequest)
        compositePreviewLoading.value = false;
    }
  };
  return {
    compositeRouteSearchQuery,
    filteredCompositeRoutes,
    compositeRouteDraftDirty,
    compositeRouteSuggestionStatus,
    loadCompositeRouteModelSuggestions,
    filterCompositeRouteModelSuggestions,
    compositeRouteTargetRole,
    compositeRouteDeletingId,
    compositeRouteLoadFailed,
    compositePreviewFailed,
    newCompositeRoute,
    compositeRoutePlatformOptions,
    compositeRouteEndpointOptions,
    compositeRouteMatchOptions,
    compositeRouteLatencyPreferenceOptions,
    compositeRouteMaxTargets: COMPOSITE_ROUTE_MAX_TARGETS,
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
    toCompositeRouteInput,
    loadCompositeRoutes,
    handleCompositeRoutes,
    closeCompositeRoutesModal,
    editCompositeRoute,
    addCompositeRouteTarget,
    removeCompositeRouteTarget,
    moveCompositeRouteTarget,
    saveCompositeRoute,
    deleteCompositeRoute,
    previewCompositeRoute,
  };
}
