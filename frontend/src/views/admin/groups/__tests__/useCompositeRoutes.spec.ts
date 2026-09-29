import { beforeEach, describe, expect, it, vi } from "vitest";
import { defineComponent, h, nextTick } from "vue";
import { mount } from "@vue/test-utils";
import type { AdminGroup, CompositeModelRoute } from "@/types";
import { useCompositeRoutes } from "../useCompositeRoutes";
import { PRODUCT_PLATFORM_OPTIONS } from "@/constants/platforms";

const { listRoutes, createRoute, updateRoute, previewRoute, showError } =
  vi.hoisted(() => ({
    listRoutes: vi.fn(),
    createRoute: vi.fn(),
    updateRoute: vi.fn(),
    previewRoute: vi.fn(),
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
    },
  },
}));

describe("Composite routes controller", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    listRoutes.mockResolvedValue([]);
    createRoute.mockResolvedValue({});
    updateRoute.mockResolvedValue({});
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
