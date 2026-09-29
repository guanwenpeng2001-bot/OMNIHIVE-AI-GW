import { ref, computed } from "vue";
import { type FingerprintSignalRow } from "../codexFingerprintSignals";
import { useSettingsServices } from "./useSettingsServices";
import type { SettingsForm } from "./settingsForm";

export function useCodexClientSettings(form: SettingsForm) {
  const { t } = useSettingsServices();
  // ── codex_cli_only 黑/白名单结构化编辑（行 ↔ JSON）──
  interface CodexClientRow {
    originator: string;
    uaContains: string; // 逗号分隔，序列化时拆成 ua_contains 数组
    skipEngineFingerprint?: boolean; // 仅白名单：命中即跳过引擎指纹门
  }

  const codexBlacklistRows = ref<CodexClientRow[]>([]);

  const codexWhitelistRows = ref<CodexClientRow[]>([]);

  const codexFingerprintRows = ref<FingerprintSignalRow[]>([]);

  const codexFingerprintNoRequired = computed(
    () => !codexFingerprintRows.value.some((r) => r.required),
  );

  function addCodexFingerprintRow(): void {
    codexFingerprintRows.value.push({
      type: "header_exact",
      match: "",
      required: false,
    });
  }

  function removeCodexFingerprintRow(i: number): void {
    codexFingerprintRows.value.splice(i, 1);
  }

  function parseCodexEntriesToRows(raw: string): CodexClientRow[] {
    if (!raw || !raw.trim()) return [];
    try {
      const arr = JSON.parse(raw);
      if (!Array.isArray(arr)) return [];
      return arr.map((e) => ({
        originator: typeof e?.originator === "string" ? e.originator : "",
        uaContains: Array.isArray(e?.ua_contains)
          ? e.ua_contains
              .filter((x: unknown) => typeof x === "string")
              .join(", ")
          : "",
        skipEngineFingerprint: e?.skip_engine_fingerprint === true,
      }));
    } catch {
      return [];
    }
  }

  function serializeCodexRowsToJSON(rows: CodexClientRow[]): string {
    const entries = rows
      .map((r) => {
        const entry: {
          originator: string;
          ua_contains: string[];
          skip_engine_fingerprint?: boolean;
        } = {
          originator: r.originator.trim(),
          ua_contains: r.uaContains
            .split(",")
            .map((s) => s.trim())
            .filter((s) => s.length > 0),
        };
        if (r.skipEngineFingerprint) entry.skip_engine_fingerprint = true;
        return entry;
      })
      .filter((e) => e.originator !== "" || e.ua_contains.length > 0);
    return entries.length > 0 ? JSON.stringify(entries) : "";
  }

  function addCodexBlacklistRow(): void {
    codexBlacklistRows.value.push({ originator: "", uaContains: "" });
  }

  function removeCodexBlacklistRow(i: number): void {
    codexBlacklistRows.value.splice(i, 1);
  }

  function addCodexWhitelistRow(): void {
    codexWhitelistRows.value.push({
      originator: "",
      uaContains: "",
      skipEngineFingerprint: false,
    });
  }

  function removeCodexWhitelistRow(i: number): void {
    codexWhitelistRows.value.splice(i, 1);
  }

  const codexSyncedVersionLabel = computed(() => {
    const synced = form.openai_codex_client_version_synced?.trim();
    if (!synced) return "";
    return t("admin.settings.gatewayForwarding.openaiCodexVersionSyncedValue", {
      version: synced,
    });
  });

  return {
    codexBlacklistRows,
    codexWhitelistRows,
    codexFingerprintRows,
    codexFingerprintNoRequired,
    addCodexFingerprintRow,
    removeCodexFingerprintRow,
    parseCodexEntriesToRows,
    serializeCodexRowsToJSON,
    addCodexBlacklistRow,
    removeCodexBlacklistRow,
    addCodexWhitelistRow,
    removeCodexWhitelistRow,
    codexSyncedVersionLabel,
  };
}
