import { ref, reactive, computed } from "vue";
import { useI18n } from "vue-i18n";
import { useAppStore } from "@/stores/app";
import { adminAPI } from "@/api/admin";
import type {
  AdminGroup,
  CompositeModelRoute,
  CompositeModelRouteInput,
  CompositeRouteDecision,
  CompositeRouteEndpoint,
  CompositeRouteMatchType,
  GroupPlatform,
} from "@/types";
import type { PRODUCT_PLATFORM_OPTIONS } from "@/constants/platforms";
import { extractApiErrorMessage } from "@/utils/apiError";

export function useCompositeRoutes(
  platformCatalog: readonly (typeof PRODUCT_PLATFORM_OPTIONS)[number][],
) {
  const { t } = useI18n();
  const appStore = useAppStore();
  const compositeRoutePlatformOptions = computed(() => [...platformCatalog]);

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

  type ConcreteGroupPlatform = Exclude<GroupPlatform, "composite">;

  type CompositeRouteFormState = {
    public_model: string;
    match_type: CompositeRouteMatchType;
    target_platform: ConcreteGroupPlatform;
    upstream_model: string;
    endpoint: CompositeRouteEndpoint;
    priority: number;
    enabled: boolean;
    notes: string;
  };

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

  const compositeRouteForm = reactive<CompositeRouteFormState>({
    public_model: "",
    match_type: "exact",
    target_platform: "openai",
    upstream_model: "",
    endpoint: "any",
    priority: 100,
    enabled: true,
    notes: "",
  });

  const compositeRouteMatchLabel = (matchType: CompositeRouteMatchType) =>
    compositeRouteMatchOptions.value.find(
      (option) => option.value === matchType,
    )?.label || matchType;

  const formatCompositeEndpoint = (endpoint: CompositeRouteEndpoint) =>
    compositeRouteEndpointOptions.value.find(
      (option) => option.value === endpoint,
    )?.label || endpoint;

  const formatCompositePlatform = (platform: string) => {
    if (!platform) return "—";
    return t(`admin.groups.platforms.${platform}`);
  };

  const compositeRouteSourceLabel = (source: string) => {
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
    compositeRouteForm.target_platform = "openai";
    compositeRouteForm.upstream_model = "";
    compositeRouteForm.endpoint = "any";
    compositeRouteForm.priority = 100;
    compositeRouteForm.enabled = true;
    compositeRouteForm.notes = "";
  };

  const toCompositeRouteInput = (): CompositeModelRouteInput => ({
    public_model: compositeRouteForm.public_model.trim(),
    match_type: compositeRouteForm.match_type,
    target_platform: compositeRouteForm.target_platform,
    upstream_model: compositeRouteForm.upstream_model.trim(),
    endpoint: compositeRouteForm.endpoint,
    priority: Number(compositeRouteForm.priority) || 100,
    enabled: compositeRouteForm.enabled,
    notes: compositeRouteForm.notes.trim(),
  });

  const loadCompositeRoutes = async () => {
    if (!compositeRoutesGroup.value) return;
    compositeRoutesLoading.value = true;
    try {
      const routes = await adminAPI.groups.listCompositeRoutes(
        compositeRoutesGroup.value.id,
      );
      compositeRoutes.value = routes.sort((a, b) => {
        if (a.priority !== b.priority) return a.priority - b.priority;
        return a.id - b.id;
      });
    } catch (error: unknown) {
      appStore.showError(
        extractApiErrorMessage(error, "") ||
          t("admin.groups.compositeRoutes.failedToLoad"),
      );
      console.error("Error loading composite routes:", error);
    } finally {
      compositeRoutesLoading.value = false;
    }
  };

  const handleCompositeRoutes = async (group: AdminGroup) => {
    compositeRoutesGroup.value = group;
    compositePreviewModel.value = "";
    compositePreviewEndpoint.value = "any";
    compositePreviewDecision.value = null;
    resetCompositeRouteForm();
    showCompositeRoutesModal.value = true;
    await loadCompositeRoutes();
  };

  const closeCompositeRoutesModal = () => {
    showCompositeRoutesModal.value = false;
    compositeRoutesGroup.value = null;
    compositeRoutes.value = [];
    compositePreviewDecision.value = null;
    resetCompositeRouteForm();
  };

  const editCompositeRoute = (route: CompositeModelRoute) => {
    compositeRouteEditingId.value = route.id;
    compositeRouteForm.public_model = route.public_model;
    compositeRouteForm.match_type = route.match_type;
    compositeRouteForm.target_platform = route.target_platform;
    compositeRouteForm.upstream_model = route.upstream_model;
    compositeRouteForm.endpoint = route.endpoint;
    compositeRouteForm.priority = route.priority || 100;
    compositeRouteForm.enabled = route.enabled;
    compositeRouteForm.notes = route.notes || "";
  };

  const saveCompositeRoute = async () => {
    if (!compositeRoutesGroup.value) return;
    if (!compositeRouteForm.public_model.trim()) {
      appStore.showError(t("admin.groups.compositeRoutes.publicModelRequired"));
      return;
    }
    compositeRouteSaving.value = true;
    try {
      const payload = toCompositeRouteInput();
      if (compositeRouteEditingId.value) {
        await adminAPI.groups.updateCompositeRoute(
          compositeRoutesGroup.value.id,
          compositeRouteEditingId.value,
          payload,
        );
        appStore.showSuccess(t("admin.groups.compositeRoutes.routeUpdated"));
      } else {
        await adminAPI.groups.createCompositeRoute(
          compositeRoutesGroup.value.id,
          payload,
        );
        appStore.showSuccess(t("admin.groups.compositeRoutes.routeCreated"));
      }
      resetCompositeRouteForm();
      await loadCompositeRoutes();
    } catch (error: unknown) {
      appStore.showError(
        extractApiErrorMessage(error, "") ||
          t("admin.groups.compositeRoutes.failedToSave"),
      );
      console.error("Error saving composite route:", error);
    } finally {
      compositeRouteSaving.value = false;
    }
  };

  const deleteCompositeRoute = async (route: CompositeModelRoute) => {
    if (!compositeRoutesGroup.value) return;
    if (!window.confirm(t("admin.groups.compositeRoutes.deleteConfirm")))
      return;
    try {
      await adminAPI.groups.deleteCompositeRoute(
        compositeRoutesGroup.value.id,
        route.id,
      );
      if (compositeRouteEditingId.value === route.id) {
        resetCompositeRouteForm();
      }
      appStore.showSuccess(t("admin.groups.compositeRoutes.routeDeleted"));
      await loadCompositeRoutes();
    } catch (error: unknown) {
      appStore.showError(
        extractApiErrorMessage(error, "") ||
          t("admin.groups.compositeRoutes.failedToDelete"),
      );
      console.error("Error deleting composite route:", error);
    }
  };

  const previewCompositeRoute = async () => {
    if (!compositeRoutesGroup.value || !compositePreviewModel.value.trim()) {
      return;
    }
    compositePreviewLoading.value = true;
    try {
      compositePreviewDecision.value =
        await adminAPI.groups.previewCompositeRoute(
          compositeRoutesGroup.value.id,
          {
            model: compositePreviewModel.value.trim(),
            endpoint: compositePreviewEndpoint.value,
          },
        );
    } catch (error: unknown) {
      appStore.showError(
        extractApiErrorMessage(error, "") ||
          t("admin.groups.compositeRoutes.failedToPreview"),
      );
      console.error("Error previewing composite route:", error);
    } finally {
      compositePreviewLoading.value = false;
    }
  };
  return {
    compositeRoutePlatformOptions,
    compositeRouteEndpointOptions,
    compositeRouteMatchOptions,
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
    resetCompositeRouteForm,
    toCompositeRouteInput,
    loadCompositeRoutes,
    handleCompositeRoutes,
    closeCompositeRoutesModal,
    editCompositeRoute,
    saveCompositeRoute,
    deleteCompositeRoute,
    previewCompositeRoute,
  };
}
