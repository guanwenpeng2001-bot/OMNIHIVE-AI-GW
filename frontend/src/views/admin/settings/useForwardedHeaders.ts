import { ref } from "vue";
import { useSettingsServices } from "./useSettingsServices";
import type { SettingsForm } from "./settingsForm";

export function useForwardedHeaders(form: SettingsForm) {
  const { t, appStore } = useSettingsServices();
  const forwardedClientIpHeaderDraft = ref("");

  const forwardedClientIpHeaderSeparatorKeys = new Set([
    " ",
    ",",
    "，",
    "Enter",
    "Tab",
  ]);

  const forwardedClientIpHeaderTokenPattern = /^[!#$%&'*+\-.^_`|~0-9A-Za-z]+$/;

  const maxForwardedClientIpHeaders = 16;

  type ForwardedClientIpHeaderResult =
    | "added"
    | "duplicate"
    | "invalid"
    | "full";

  function normalizeForwardedClientIpHeader(raw: string): string {
    const header = raw.trim();
    if (!forwardedClientIpHeaderTokenPattern.test(header)) {
      return "";
    }

    return header
      .toLowerCase()
      .split("-")
      .map((part) => `${part.charAt(0).toUpperCase()}${part.slice(1)}`)
      .join("-");
  }

  function normalizeForwardedClientIpHeaders(value: unknown): string[] {
    if (!Array.isArray(value)) {
      return [];
    }

    const headers: string[] = [];
    const seen = new Set<string>();
    for (const raw of value) {
      if (typeof raw !== "string") {
        continue;
      }
      const header = normalizeForwardedClientIpHeader(raw);
      const key = header.toLowerCase();
      if (
        !header ||
        seen.has(key) ||
        headers.length >= maxForwardedClientIpHeaders
      ) {
        continue;
      }
      seen.add(key);
      headers.push(header);
    }
    return headers;
  }

  function removeForwardedClientIpHeader(header: string) {
    form.forwarded_client_ip_headers = form.forwarded_client_ip_headers.filter(
      (item) => item !== header,
    );
  }

  function addForwardedClientIpHeader(
    raw: string,
  ): ForwardedClientIpHeaderResult {
    const header = normalizeForwardedClientIpHeader(raw);
    if (!header) {
      return "invalid";
    }
    if (
      form.forwarded_client_ip_headers.some(
        (item) => item.toLowerCase() === header.toLowerCase(),
      )
    ) {
      return "duplicate";
    }
    if (
      form.forwarded_client_ip_headers.length >= maxForwardedClientIpHeaders
    ) {
      return "full";
    }
    form.forwarded_client_ip_headers = [
      ...form.forwarded_client_ip_headers,
      header,
    ];
    return "added";
  }

  function showForwardedClientIpHeaderError(
    result: ForwardedClientIpHeaderResult,
  ) {
    if (result === "invalid") {
      appStore.showError(
        t("admin.settings.apiKeyAcl.forwardedClientIpHeaderInvalid"),
      );
    } else if (result === "full") {
      appStore.showError(
        t("admin.settings.apiKeyAcl.forwardedClientIpHeadersLimit", {
          max: maxForwardedClientIpHeaders,
        }),
      );
    }
  }

  function commitForwardedClientIpHeaderDraft() {
    const draft = forwardedClientIpHeaderDraft.value;
    if (!draft) {
      return;
    }
    const result = addForwardedClientIpHeader(draft);
    showForwardedClientIpHeaderError(result);
    forwardedClientIpHeaderDraft.value = "";
  }

  function handleForwardedClientIpHeaderKeydown(event: KeyboardEvent) {
    if (event.isComposing) {
      return;
    }
    if (forwardedClientIpHeaderSeparatorKeys.has(event.key)) {
      event.preventDefault();
      commitForwardedClientIpHeaderDraft();
      return;
    }
    if (
      event.key === "Backspace" &&
      !forwardedClientIpHeaderDraft.value &&
      form.forwarded_client_ip_headers.length > 0
    ) {
      form.forwarded_client_ip_headers.pop();
    }
  }

  function handleForwardedClientIpHeaderPaste(event: ClipboardEvent) {
    const text = event.clipboardData?.getData("text") || "";
    if (!text.trim()) {
      return;
    }
    event.preventDefault();

    let error: ForwardedClientIpHeaderResult | undefined;
    for (const token of text.split(/[,，;\r\n]+/)) {
      if (!token.trim()) {
        continue;
      }
      const result = addForwardedClientIpHeader(token);
      if (result === "invalid" || result === "full") {
        error = result;
      }
    }
    if (error) {
      showForwardedClientIpHeaderError(error);
    }
  }

  return {
    forwardedClientIpHeaderDraft,
    forwardedClientIpHeaderSeparatorKeys,
    forwardedClientIpHeaderTokenPattern,
    maxForwardedClientIpHeaders,
    normalizeForwardedClientIpHeader,
    normalizeForwardedClientIpHeaders,
    removeForwardedClientIpHeader,
    addForwardedClientIpHeader,
    showForwardedClientIpHeaderError,
    commitForwardedClientIpHeaderDraft,
    handleForwardedClientIpHeaderKeydown,
    handleForwardedClientIpHeaderPaste,
  };
}
