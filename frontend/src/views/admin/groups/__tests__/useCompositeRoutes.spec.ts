import { beforeEach, describe, expect, it, vi } from "vitest";
import { defineComponent, h, nextTick } from "vue";
import { mount } from "@vue/test-utils";
import type { AdminGroup, CompositeModelRoute } from "@/types";
import { useCompositeRoutes } from "../useCompositeRoutes";
import { PRODUCT_PLATFORM_OPTIONS } from "@/constants/platforms";
import CompositeRoutesDialog from "../CompositeRoutesDialog.vue";

const {
  listRoutes,
  createRoute,
  updateRoute,
  previewRoute,
  modelCandidates,
  showError,
} = vi.hoisted(() => ({
  listRoutes: vi.fn(),
  createRoute: vi.fn(),
  updateRoute: vi.fn(),
  previewRoute: vi.fn(),
  modelCandidates: vi.fn(),
  showError: vi.fn(),
}));

vi.mock("vue-i18n", () => ({ useI18n: () => ({ t: (key: string) => key }) }));
vi.mock("@/stores/app", () => ({
  useAppStore: () => ({ showSuccess: vi.fn(), showError }),
}));
vi.mock("@/api/admin", () => ({
  adminAPI: {
    groups: {
      listCompositeRoutes: listRoutes,
      createCompositeRoute: createRoute,
      updateCompositeRoute: updateRoute,
      previewCompositeRoute: previewRoute,
      getModelAllowlistCandidates: modelCandidates,
    },
  },
}));

describe("Composite routes controller", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    listRoutes.mockResolvedValue([]);
    createRoute.mockResolvedValue({});
    updateRoute.mockResolvedValue({});
    modelCandidates.mockResolvedValue([]);
  });

  it("renders a persistent save action, labelled target controls and collapsed advanced fields", async () => {
    const controller = useCompositeRoutes(PRODUCT_PLATFORM_OPTIONS);
    await controller.handleCompositeRoutes({ id: 1 } as AdminGroup);
    const wrapper = mount(CompositeRoutesDialog, {
      props: { controller },
      global: {
        stubs: {
          BaseDialog: {
            template:
              '<div><slot /><footer><slot name="footer" /></footer></div>',
          },
          Icon: true,
          PlatformIcon: true,
          Select: true,
        },
      },
    });
    expect(wrapper.get('footer button[type="submit"]').attributes("form")).toBe(
      "composite-route-form",
    );
    expect(
      wrapper.get("details[data-target-advanced]").attributes("open"),
    ).toBeUndefined();
    expect(
      wrapper
        .get(
          'button[aria-label="admin.groups.compositeRoutes.moveTargetUp: admin.groups.compositeRoutes.targetLabel"]',
        )
        .attributes("disabled"),
    ).toBeDefined();
    controller.compositeRouteForm.notes = "unsaved";
    await nextTick();
    expect(wrapper.get("footer").text()).toContain(
      "admin.groups.unsavedChanges",
    );
    controller.compositeRouteSaving.value = true;
    await nextTick();
    expect(wrapper.get("fieldset").attributes("disabled")).toBeDefined();
    wrapper.unmount();
  });

  it("protects drafts on close, route selection and new route, but permits confirmed discard", async () => {
    const controller = useCompositeRoutes(PRODUCT_PLATFORM_OPTIONS);
    await controller.handleCompositeRoutes({ id: 1 } as AdminGroup);
    expect(controller.compositeRouteDraftDirty.value).toBe(false);
    controller.compositeRouteForm.notes = "keep me";
    const confirm = vi.spyOn(window, "confirm").mockReturnValue(false);
    controller.closeCompositeRoutesModal();
    controller.editCompositeRoute({
      id: 2,
      notes: "other",
    } as CompositeModelRoute);
    controller.newCompositeRoute();
    expect(confirm).toHaveBeenCalledTimes(3);
    expect(controller.compositeRouteForm.notes).toBe("keep me");
    expect(controller.showCompositeRoutesModal.value).toBe(true);
    confirm.mockReturnValue(true);
    controller.newCompositeRoute();
    expect(controller.compositeRouteDraftDirty.value).toBe(false);
    controller.closeCompositeRoutesModal();
    expect(controller.showCompositeRoutesModal.value).toBe(false);
    confirm.mockRestore();
  });

  it("edits and reorders targets through the dialog and keeps typed names after a failed save", async () => {
    const controller = useCompositeRoutes(PRODUCT_PLATFORM_OPTIONS);
    modelCandidates.mockRejectedValueOnce(new Error("offline"));
    await controller.handleCompositeRoutes({ id: 1 } as AdminGroup);
    const wrapper = mount(CompositeRoutesDialog, {
      props: { controller },
      global: {
        stubs: {
          BaseDialog: {
            template:
              '<div><slot /><footer><slot name="footer" /></footer></div>',
          },
          Icon: true,
          Select: true,
        },
      },
    });
    await wrapper.get("#composite-public-model").setValue("hand-typed-model");
    await wrapper.get("#target-model-0").setValue("primary");
    expect(wrapper.text()).toContain(
      "admin.groups.compositeRoutes.suggestionsUnavailable",
    );
    const addTarget = wrapper
      .findAll("button")
      .find(
        (button) => button.text() === "admin.groups.compositeRoutes.addTarget",
      )!;
    await addTarget.trigger("click");
    await wrapper.get("#target-model-1").setValue("fallback");
    await wrapper
      .findAll(
        'button[aria-label^="admin.groups.compositeRoutes.moveTargetUp"]',
      )[1]
      .trigger("click");
    expect(
      (wrapper.get("#target-model-0").element as HTMLInputElement).value,
    ).toBe("fallback");
    createRoute.mockRejectedValueOnce(new Error("offline"));
    await wrapper.get("form").trigger("submit");
    await nextTick();
    expect(createRoute).toHaveBeenCalledWith(
      1,
      expect.objectContaining({
        public_model: "hand-typed-model",
        upstream_model: "fallback",
        targets: [
          expect.objectContaining({ upstream_model: "fallback", position: 0 }),
          expect.objectContaining({ upstream_model: "primary", position: 1 }),
        ],
      }),
    );
    expect(
      (wrapper.get("#composite-public-model").element as HTMLInputElement)
        .value,
    ).toBe("hand-typed-model");
    expect(wrapper.get("footer").text()).toContain(
      "admin.groups.unsavedChanges",
    );
    wrapper.unmount();
  });

  it("preserves the account restriction on legacy routes and stored disabled primary rows", async () => {
    const controller = useCompositeRoutes(PRODUCT_PLATFORM_OPTIONS);
    await controller.handleCompositeRoutes({ id: 1 } as AdminGroup);
    const route = {
      id: 2,
      public_model: "claude-",
      match_type: "prefix",
      target_platform: "anthropic",
      account_type: "bedrock",
      upstream_model: "",
      endpoint: "any",
      priority: 100,
      enabled: true,
      notes: "",
    } as CompositeModelRoute;
    controller.editCompositeRoute(route);
    expect(controller.toCompositeRouteInput().targets?.[0].account_type).toBe(
      "bedrock",
    );
    controller.editCompositeRoute({
      ...route,
      id: 3,
      target_platform: "openai",
      upstream_model: "enabled-model",
      targets: [
        {
          position: 0,
          target_platform: "anthropic",
          upstream_model: "disabled-model",
          enabled: false,
          scheduling_weight: 100,
        },
        {
          position: 1,
          target_platform: "openai",
          upstream_model: "enabled-model",
          enabled: true,
          scheduling_weight: 50,
        },
      ],
    });
    expect(controller.compositeRouteForm.targets[0].upstream_model).toBe(
      "disabled-model",
    );
    expect(controller.compositeRouteTargetRole(0)).toBe(
      "admin.groups.compositeRoutes.skippedTarget",
    );
    expect(controller.compositeRouteTargetRole(1)).toBe(
      "admin.groups.compositeRoutes.primaryAttempt",
    );
  });

  it("clears stale previews on draft/input changes and ignores late responses", async () => {
    const controller = useCompositeRoutes(PRODUCT_PLATFORM_OPTIONS);
    await controller.handleCompositeRoutes({ id: 1 } as AdminGroup);
    controller.compositePreviewModel.value = "saved";
    previewRoute.mockResolvedValueOnce({ matched: true });
    await controller.previewCompositeRoute();
    expect(controller.compositePreviewDecision.value).not.toBeNull();
    controller.compositeRouteForm.notes = "draft";
    expect(controller.compositePreviewDecision.value).toBeNull();
    let resolve!: (value: unknown) => void;
    previewRoute.mockImplementationOnce(
      () =>
        new Promise((done) => {
          resolve = done;
        }),
    );
    const pending = controller.previewCompositeRoute();
    controller.compositePreviewModel.value = "different";
    resolve({ matched: true });
    await pending;
    expect(controller.compositePreviewDecision.value).toBeNull();
    expect(controller.compositePreviewLoading.value).toBe(false);
  });

  it("ignores list results from a previous group", async () => {
    const controller = useCompositeRoutes(PRODUCT_PLATFORM_OPTIONS);
    let resolve!: (value: CompositeModelRoute[]) => void;
    listRoutes.mockImplementationOnce(
      () =>
        new Promise((done) => {
          resolve = done;
        }),
    );
    const first = controller.handleCompositeRoutes({ id: 1 } as AdminGroup);
    await controller.handleCompositeRoutes({ id: 2 } as AdminGroup);
    resolve([{ id: 99 } as CompositeModelRoute]);
    await first;
    expect(controller.compositeRoutes.value).toEqual([]);
    expect(controller.compositeRoutesGroup.value?.id).toBe(2);
  });

  it("uses only platform suggestions and still saves manually entered names when they fail", async () => {
    const controller = useCompositeRoutes(PRODUCT_PLATFORM_OPTIONS);
    await controller.handleCompositeRoutes({ id: 14 } as AdminGroup);
    modelCandidates.mockRejectedValueOnce(new Error("offline"));
    await controller.loadCompositeRouteModelSuggestions("gemini");
    expect(modelCandidates).toHaveBeenCalledWith(0, "gemini");
    expect(controller.compositeRouteSuggestionStatus.value.gemini).toBe(
      "error",
    );
    controller.compositeRouteForm.public_model = "manual-model";
    await controller.saveCompositeRoute();
    expect(createRoute).toHaveBeenCalledWith(
      14,
      expect.objectContaining({ public_model: "manual-model" }),
    );
  });

  it("searches saved routes locally and bounds suggestions with prefix matches first", async () => {
    const controller = useCompositeRoutes(PRODUCT_PLATFORM_OPTIONS);
    listRoutes.mockResolvedValueOnce([
      {
        id: 1,
        public_model: "team-model",
        notes: "important",
        target_platform: "openai",
        upstream_model: "alias",
        endpoint: "any",
        match_type: "prefix",
        priority: 1,
        targets: [
          { target_platform: "gemini", upstream_model: "fallback-alias" },
        ],
      },
    ]);
    modelCandidates.mockResolvedValueOnce([
      "z-model",
      ...Array.from({ length: 12 }, (_, index) => `model-${index}`),
    ]);
    await controller.handleCompositeRoutes({ id: 1 } as AdminGroup);
    await nextTick();
    controller.compositeRouteSearchQuery.value = "FALLBACK-ALIAS";
    expect(controller.filteredCompositeRoutes.value).toHaveLength(1);
    controller.compositeRouteSearchQuery.value = "absent";
    expect(controller.filteredCompositeRoutes.value).toHaveLength(0);
    expect(listRoutes).toHaveBeenCalledTimes(1);
    expect(
      controller.filterCompositeRouteModelSuggestions("", "openai"),
    ).toEqual([]);
    const suggestions = controller.filterCompositeRouteModelSuggestions(
      "model",
      "openai",
    );
    expect(suggestions).toHaveLength(8);
    expect(suggestions.every((model) => model.startsWith("model"))).toBe(true);
  });

  it("blocks duplicate saves and navigation while saving then preserves a failed draft", async () => {
    const controller = useCompositeRoutes(PRODUCT_PLATFORM_OPTIONS);
    await controller.handleCompositeRoutes({ id: 1 } as AdminGroup);
    controller.compositeRouteForm.public_model = "manual";
    let reject!: (reason: Error) => void;
    createRoute.mockImplementationOnce(
      () =>
        new Promise((_, fail) => {
          reject = fail;
        }),
    );
    const pending = controller.saveCompositeRoute();
    await controller.saveCompositeRoute();
    controller.newCompositeRoute();
    controller.closeCompositeRoutesModal();
    await controller.handleCompositeRoutes({ id: 2 } as AdminGroup);
    expect(createRoute).toHaveBeenCalledTimes(1);
    expect(controller.compositeRoutesGroup.value?.id).toBe(1);
    expect(controller.compositeRouteForm.public_model).toBe("manual");
    reject(new Error("offline"));
    await pending;
    expect(controller.compositeRouteDraftDirty.value).toBe(true);
    expect(controller.compositeRouteSaving.value).toBe(false);
  });

  it("keeps the draft and clears loading when preview fails", async () => {
    const controller = useCompositeRoutes(PRODUCT_PLATFORM_OPTIONS);
    await controller.handleCompositeRoutes({ id: 1 } as AdminGroup);
    controller.compositeRouteForm.public_model = "unsaved";
    controller.compositePreviewModel.value = "saved";
    previewRoute.mockRejectedValueOnce(new Error("offline"));
    await controller.previewCompositeRoute();
    expect(controller.compositePreviewLoading.value).toBe(false);
    expect(controller.compositePreviewFailed.value).toBe(true);
    expect(controller.compositePreviewDecision.value).toBeNull();
    expect(controller.compositeRouteForm.public_model).toBe("unsaved");
    expect(controller.showCompositeRoutesModal.value).toBe(true);
  });

  it("keeps edit, save and preview bound to the selected group across dialog reopening", async () => {
    const controller = useCompositeRoutes(PRODUCT_PLATFORM_OPTIONS);
    await controller.handleCompositeRoutes({ id: 14 } as AdminGroup);
    controller.editCompositeRoute({
      id: 8,
      public_model: "public-model",
      match_type: "exact",
      target_platform: "openai",
      upstream_model: "upstream-model",
      endpoint: "responses",
      priority: 12,
      enabled: true,
      notes: "existing",
    } as CompositeModelRoute);
    controller.compositeRouteForm.notes = " updated ";
    await controller.saveCompositeRoute();
    expect(updateRoute).toHaveBeenCalledWith(
      14,
      8,
      expect.objectContaining({
        public_model: "public-model",
        notes: "updated",
        priority: 12,
      }),
    );
    expect(controller.compositeRouteEditingId.value).toBeNull();

    controller.closeCompositeRoutesModal();
    await controller.handleCompositeRoutes({ id: 27 } as AdminGroup);
    controller.compositeRouteForm.public_model = " new-public ";
    controller.compositeRouteForm.upstream_model = " new-upstream ";
    await controller.saveCompositeRoute();
    expect(createRoute).toHaveBeenCalledWith(
      27,
      expect.objectContaining({
        public_model: "new-public",
        upstream_model: "new-upstream",
      }),
    );
    controller.compositePreviewModel.value = " preview-model ";
    controller.compositePreviewEndpoint.value = "responses";
    await controller.previewCompositeRoute();
    expect(previewRoute).toHaveBeenCalledWith(27, {
      model: "preview-model",
      endpoint: "responses",
    });
    expect(updateRoute).toHaveBeenCalledTimes(1);
  });

  it("does not save an empty public model and retains the draft after an API failure", async () => {
    const controller = useCompositeRoutes(PRODUCT_PLATFORM_OPTIONS);
    await controller.handleCompositeRoutes({ id: 14 } as AdminGroup);
    await controller.saveCompositeRoute();
    expect(createRoute).not.toHaveBeenCalled();
    expect(showError).toHaveBeenCalledWith(
      "admin.groups.compositeRoutes.publicModelRequired",
    );

    controller.compositeRouteForm.public_model = "retry-model";
    createRoute.mockRejectedValueOnce(new Error("offline"));
    await controller.saveCompositeRoute();
    expect(controller.compositeRouteSaving.value).toBe(false);
    expect(controller.compositeRouteForm.public_model).toBe("retry-model");
    expect(controller.showCompositeRoutesModal.value).toBe(true);
  });

  it("builds an ordered target payload from the edited target list", async () => {
    const controller = useCompositeRoutes(PRODUCT_PLATFORM_OPTIONS);
    await controller.handleCompositeRoutes({ id: 14 } as AdminGroup);
    controller.compositeRouteForm.public_model = "public-model";
    controller.addCompositeRouteTarget();
    controller.compositeRouteForm.targets[0].target_platform = "anthropic";
    controller.compositeRouteForm.targets[0].upstream_model = " claude-sonnet ";
    controller.compositeRouteForm.targets[0].affinity_ttl_seconds = 120;
    controller.compositeRouteForm.targets[1].target_platform = "gemini";
    controller.compositeRouteForm.targets[1].region = " us-east-1 ";
    controller.compositeRouteForm.targets[1].latency_preference = "low_latency";
    controller.compositeRouteForm.targets[1].scheduling_weight = 30;
    await controller.saveCompositeRoute();

    expect(createRoute).toHaveBeenCalledWith(14, {
      public_model: "public-model",
      match_type: "exact",
      target_platform: "anthropic",
      upstream_model: "claude-sonnet",
      endpoint: "any",
      priority: 100,
      enabled: true,
      notes: "",
      targets: [
        {
          position: 0,
          target_platform: "anthropic",
          account_type: "",
          upstream_model: "claude-sonnet",
          region: "",
          provider: "",
          latency_preference: "default",
          scheduling_weight: 100,
          affinity_ttl_seconds: 120,
          enabled: true,
        },
        {
          position: 1,
          target_platform: "gemini",
          account_type: "",
          upstream_model: "",
          region: "us-east-1",
          provider: "",
          latency_preference: "low_latency",
          scheduling_weight: 30,
          affinity_ttl_seconds: 0,
          enabled: true,
        },
      ],
    });
  });

  it("loads stored targets in position order and renumbers them when reordered", async () => {
    const controller = useCompositeRoutes(PRODUCT_PLATFORM_OPTIONS);
    await controller.handleCompositeRoutes({ id: 5 } as AdminGroup);
    controller.editCompositeRoute({
      id: 9,
      public_model: "fallback-model",
      match_type: "exact",
      target_platform: "anthropic",
      upstream_model: "claude-sonnet",
      endpoint: "any",
      priority: 10,
      enabled: true,
      notes: "",
      targets: [
        {
          position: 1,
          target_platform: "openai",
          upstream_model: "gpt-5",
          scheduling_weight: 20,
          enabled: true,
        },
        {
          position: 0,
          target_platform: "anthropic",
          upstream_model: "claude-sonnet",
          scheduling_weight: 100,
          enabled: true,
        },
      ],
    } as CompositeModelRoute);
    expect(
      controller.compositeRouteForm.targets.map(
        (target) => target.target_platform,
      ),
    ).toEqual(["anthropic", "openai"]);

    controller.moveCompositeRouteTarget(1, -1);
    await controller.saveCompositeRoute();

    expect(updateRoute).toHaveBeenCalledWith(
      5,
      9,
      expect.objectContaining({
        target_platform: "openai",
        upstream_model: "gpt-5",
        targets: [
          expect.objectContaining({
            position: 0,
            target_platform: "openai",
            upstream_model: "gpt-5",
          }),
          expect.objectContaining({
            position: 1,
            target_platform: "anthropic",
            upstream_model: "claude-sonnet",
          }),
        ],
      }),
    );
  });

  it("rejects invalid target lists before saving", async () => {
    const invalidTargetLists: Array<{
      name: string;
      errorKey: string;
      edit: (controller: ReturnType<typeof useCompositeRoutes>) => void;
    }> = [
      {
        name: "no enabled target",
        errorKey: "admin.groups.compositeRoutes.targetsEnabledRequired",
        edit: (controller) => {
          controller.compositeRouteForm.targets[0].enabled = false;
        },
      },
      {
        name: "target without a platform",
        errorKey: "admin.groups.compositeRoutes.targetPlatformRequired",
        edit: (controller) => {
          (
            controller.compositeRouteForm.targets[0] as {
              target_platform: string;
            }
          ).target_platform = "";
        },
      },
      {
        name: "weight below one",
        errorKey: "admin.groups.compositeRoutes.targetWeightInvalid",
        edit: (controller) => {
          controller.compositeRouteForm.targets[0].scheduling_weight = 0;
        },
      },
      {
        name: "affinity TTL out of range",
        errorKey: "admin.groups.compositeRoutes.targetAffinityTtlInvalid",
        edit: (controller) => {
          controller.compositeRouteForm.targets[0].affinity_ttl_seconds = 30;
        },
      },
    ];

    for (const invalidTargetList of invalidTargetLists) {
      createRoute.mockClear();
      showError.mockClear();
      const controller = useCompositeRoutes(PRODUCT_PLATFORM_OPTIONS);
      await controller.handleCompositeRoutes({ id: 3 } as AdminGroup);
      controller.compositeRouteForm.public_model = "guarded-model";
      invalidTargetList.edit(controller);
      await controller.saveCompositeRoute();

      expect(createRoute, invalidTargetList.name).not.toHaveBeenCalled();
      expect(showError, invalidTargetList.name).toHaveBeenCalledWith(
        invalidTargetList.errorKey,
      );
    }
  });

  it("caps the target list at the supported maximum", async () => {
    const controller = useCompositeRoutes(PRODUCT_PLATFORM_OPTIONS);
    await controller.handleCompositeRoutes({ id: 8 } as AdminGroup);
    for (let index = 0; index < 20; index += 1) {
      controller.addCompositeRouteTarget();
    }
    expect(controller.compositeRouteForm.targets).toHaveLength(
      controller.compositeRouteMaxTargets,
    );
  });

  it("keeps the legacy payload while the target list stays at its defaults", async () => {
    const controller = useCompositeRoutes(PRODUCT_PLATFORM_OPTIONS);
    await controller.handleCompositeRoutes({ id: 21 } as AdminGroup);
    controller.compositeRouteForm.public_model = " legacy-model ";
    controller.compositeRouteForm.notes = "legacy note";
    await controller.saveCompositeRoute();

    expect(createRoute).toHaveBeenCalledWith(21, {
      public_model: "legacy-model",
      match_type: "exact",
      target_platform: "openai",
      upstream_model: "",
      endpoint: "any",
      priority: 100,
      enabled: true,
      notes: "legacy note",
    });
  });

  it("keeps the legacy target fields mirrored to the first target in the rendered form", async () => {
    const controller = useCompositeRoutes(PRODUCT_PLATFORM_OPTIONS);
    await controller.handleCompositeRoutes({ id: 1 } as AdminGroup);
    const Harness = defineComponent({
      setup() {
        return () =>
          h("div", [
            h(
              "span",
              { id: "platform" },
              controller.compositeRouteForm.target_platform,
            ),
            h(
              "span",
              { id: "upstream" },
              controller.compositeRouteForm.upstream_model,
            ),
          ]);
      },
    });
    const wrapper = mount(Harness);
    expect(wrapper.get("#platform").text()).toBe("openai");

    controller.compositeRouteForm.targets[0].target_platform = "anthropic";
    controller.compositeRouteForm.targets[0].upstream_model = "claude-sonnet";
    controller.addCompositeRouteTarget();
    await nextTick();
    expect(wrapper.get("#platform").text()).toBe("anthropic");
    expect(wrapper.get("#upstream").text()).toBe("claude-sonnet");

    controller.compositeRouteForm.targets[1].target_platform = "gemini";
    controller.moveCompositeRouteTarget(1, -1);
    await nextTick();
    expect(wrapper.get("#platform").text()).toBe("gemini");
    expect(wrapper.get("#upstream").text()).toBe("");
  });
});
