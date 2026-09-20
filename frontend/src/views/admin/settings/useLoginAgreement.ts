import type { LoginAgreementDocument } from "@/types";
import { useSettingsServices } from "./useSettingsServices";
import type { SettingsForm } from "./settingsForm";

export function useLoginAgreement(getForm: () => SettingsForm) {
  const { localText } = useSettingsServices();
  function defaultLoginAgreementDocuments(): LoginAgreementDocument[] {
    return [
      {
        id: "terms",
        title: localText("服务条款", "Terms of Service"),
        content_md: "",
      },
      {
        id: "usage-policy",
        title: localText("使用政策", "Usage Policy"),
        content_md: "",
      },
      {
        id: "supported-regions",
        title: localText("支持的国家和地区", "Supported Countries and Regions"),
        content_md: "",
      },
      {
        id: "service-specific-terms",
        title: localText("服务特定条款", "Service-Specific Terms"),
        content_md: "",
      },
    ];
  }

  function normalizeLoginAgreementDocumentId(raw: string): string {
    return raw
      .trim()
      .toLowerCase()
      .replace(/[^a-z0-9_-]+/g, "-")
      .replace(/[-_]{2,}/g, "-")
      .replace(/^[-_]+|[-_]+$/g, "");
  }

  function loginAgreementRoutePath(
    doc: LoginAgreementDocument,
    index: number,
  ): string {
    const id =
      normalizeLoginAgreementDocumentId(doc.id || doc.title) ||
      `doc-${index + 1}`;
    return `/legal/${id}`;
  }

  function addLoginAgreementDocument() {
    getForm().login_agreement_documents.push({
      id: `custom-${Date.now().toString(36)}`,
      title: "",
      content_md: "",
    });
  }

  function removeLoginAgreementDocument(index: number) {
    getForm().login_agreement_documents.splice(index, 1);
  }

  function normalizeLoginAgreementDocumentsForSave(): LoginAgreementDocument[] {
    return getForm()
      .login_agreement_documents.map((doc, index) => ({
        id:
          normalizeLoginAgreementDocumentId(doc.id || doc.title) ||
          `doc-${index + 1}`,
        title: doc.title.trim(),
        content_md: doc.content_md.trim(),
      }))
      .filter((doc) => doc.title || doc.content_md);
  }

  function findDuplicateLoginAgreementDocumentId(
    documents: LoginAgreementDocument[],
  ): string | null {
    const seen = new Set<string>();
    for (const doc of documents) {
      if (seen.has(doc.id)) {
        return doc.id;
      }
      seen.add(doc.id);
    }
    return null;
  }

  return {
    defaultLoginAgreementDocuments,
    normalizeLoginAgreementDocumentId,
    loginAgreementRoutePath,
    addLoginAgreementDocument,
    removeLoginAgreementDocument,
    normalizeLoginAgreementDocumentsForSave,
    findDuplicateLoginAgreementDocumentId,
  };
}
