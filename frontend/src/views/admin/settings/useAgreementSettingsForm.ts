import { reactive } from "vue";
import type { LoginAgreementDocument } from "@/types";
import type { SettingsForm } from "./settingsForm";

export function useAgreementSettingsForm(
  defaultLoginAgreementDocuments: () => LoginAgreementDocument[],
) {
  return reactive<
    Pick<
      SettingsForm,
      | "login_agreement_enabled"
      | "login_agreement_mode"
      | "login_agreement_updated_at"
      | "login_agreement_documents"
    >
  >({
    login_agreement_enabled: false,

    login_agreement_mode: "modal",

    login_agreement_updated_at: "2026-03-31",

    login_agreement_documents: defaultLoginAgreementDocuments(),
  });
}
