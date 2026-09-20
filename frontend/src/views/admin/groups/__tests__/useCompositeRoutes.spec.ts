import { beforeEach, describe, expect, it, vi } from "vitest";
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
});
