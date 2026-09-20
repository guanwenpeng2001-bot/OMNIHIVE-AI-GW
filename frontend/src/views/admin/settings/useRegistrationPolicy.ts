import { ref } from "vue";
import {
  isRegistrationEmailSuffixDomainValid,
  normalizeRegistrationEmailSuffixDomain,
  parseRegistrationEmailSuffixWhitelistInput,
} from "@/utils/registrationEmailPolicy";

export function useRegistrationPolicy() {
  const registrationEmailSuffixWhitelistTags = ref<string[]>([]);

  const registrationEmailSuffixWhitelistDraft = ref("");

  const registrationEmailSuffixWhitelistSeparatorKeys = new Set([
    " ",
    ",",
    "，",
    "Enter",
    "Tab",
  ]);

  function removeRegistrationEmailSuffixWhitelistTag(suffix: string) {
    registrationEmailSuffixWhitelistTags.value =
      registrationEmailSuffixWhitelistTags.value.filter(
        (item) => item !== suffix,
      );
  }

  function addRegistrationEmailSuffixWhitelistTag(raw: string) {
    const suffix = normalizeRegistrationEmailSuffixDomain(raw);
    if (
      !isRegistrationEmailSuffixDomainValid(suffix) ||
      registrationEmailSuffixWhitelistTags.value.includes(suffix)
    ) {
      return;
    }
    registrationEmailSuffixWhitelistTags.value = [
      ...registrationEmailSuffixWhitelistTags.value,
      suffix,
    ];
  }

  function commitRegistrationEmailSuffixWhitelistDraft() {
    if (!registrationEmailSuffixWhitelistDraft.value) {
      return;
    }
    addRegistrationEmailSuffixWhitelistTag(
      registrationEmailSuffixWhitelistDraft.value,
    );
    registrationEmailSuffixWhitelistDraft.value = "";
  }

  function handleRegistrationEmailSuffixWhitelistDraftInput() {
    registrationEmailSuffixWhitelistDraft.value =
      normalizeRegistrationEmailSuffixDomain(
        registrationEmailSuffixWhitelistDraft.value,
      );
  }

  function handleRegistrationEmailSuffixWhitelistDraftKeydown(
    event: KeyboardEvent,
  ) {
    if (event.isComposing) {
      return;
    }

    if (registrationEmailSuffixWhitelistSeparatorKeys.has(event.key)) {
      event.preventDefault();
      commitRegistrationEmailSuffixWhitelistDraft();
      return;
    }

    if (
      event.key === "Backspace" &&
      !registrationEmailSuffixWhitelistDraft.value &&
      registrationEmailSuffixWhitelistTags.value.length > 0
    ) {
      registrationEmailSuffixWhitelistTags.value.pop();
    }
  }

  function handleRegistrationEmailSuffixWhitelistPaste(event: ClipboardEvent) {
    const text = event.clipboardData?.getData("text") || "";
    if (!text.trim()) {
      return;
    }
    event.preventDefault();
    const tokens = parseRegistrationEmailSuffixWhitelistInput(text);
    for (const token of tokens) {
      addRegistrationEmailSuffixWhitelistTag(token);
    }
  }

  return {
    registrationEmailSuffixWhitelistTags,
    registrationEmailSuffixWhitelistDraft,
    registrationEmailSuffixWhitelistSeparatorKeys,
    removeRegistrationEmailSuffixWhitelistTag,
    addRegistrationEmailSuffixWhitelistTag,
    commitRegistrationEmailSuffixWhitelistDraft,
    handleRegistrationEmailSuffixWhitelistDraftInput,
    handleRegistrationEmailSuffixWhitelistDraftKeydown,
    handleRegistrationEmailSuffixWhitelistPaste,
  };
}
