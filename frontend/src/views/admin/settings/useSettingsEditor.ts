import { ref, reactive } from "vue";
import { useStepUp } from "@/composables/useStepUp";
import { useAdminSettingsStore } from "@/stores/adminSettings";
import { toRefs } from "vue";
import type { SettingsForm } from "./settingsForm";
import { useSettingsNavigation } from "./useSettingsNavigation";
import { useSettingsServices } from "./useSettingsServices";
import { useLoginAgreement } from "./useLoginAgreement";
import { useClaudeSystemPrompt } from "./useClaudeSystemPrompt";
import { useAdminApiKey } from "./useAdminApiKey";
import { useUpstreamBillingProbe } from "./useUpstreamBillingProbe";
import { useOllamaCloudUsage } from "./useOllamaCloudUsage";
import { useOverloadCooldown } from "./useOverloadCooldown";
import { useRateLimitCooldown } from "./useRateLimitCooldown";
import { usePanelRateLimit } from "./usePanelRateLimit";
import { useStreamTimeout } from "./useStreamTimeout";
import { useRectifier } from "./useRectifier";
import { useBetaPolicy } from "./useBetaPolicy";
import { useOpenAIFastPolicy } from "./useOpenAIFastPolicy";
import { useCaptchaSettings } from "./useCaptchaSettings";
import { useOpenAIScheduler } from "./useOpenAIScheduler";
import { useUserDefaults } from "./useUserDefaults";
import { useWebSearchSettings } from "./useWebSearchSettings";
import { useRegistrationPolicy } from "./useRegistrationPolicy";
import { useForwardedHeaders } from "./useForwardedHeaders";
import { useEmailSettings } from "./useEmailSettings";
import { useOAuthSettings } from "./useOAuthSettings";
import { useSiteSettings } from "./useSiteSettings";
import { useCodexClientSettings } from "./useCodexClientSettings";
import { usePaymentSettings } from "./usePaymentSettings";
import { useAffiliateSettings } from "./useAffiliateSettings";
import { useSecuritySettingsForm } from "./useSecuritySettingsForm";
import { useGeneralSettingsForm } from "./useGeneralSettingsForm";
import { useAgreementSettingsForm } from "./useAgreementSettingsForm";
import { useUsersSettingsForm } from "./useUsersSettingsForm";
import { useGatewaySettingsForm } from "./useGatewaySettingsForm";
import { useFeaturesSettingsForm } from "./useFeaturesSettingsForm";
import { usePaymentSettingsForm } from "./usePaymentSettingsForm";
import { useEmailSettingsForm } from "./useEmailSettingsForm";

/** Owns the page's domain controllers and the shared references used by persistence. */
export function useSettingsEditor(saveSettings: () => Promise<void>) {
  const { t, appStore, isZhLocale, localText } = useSettingsServices();

  const settingsNavigation = useSettingsNavigation();

  const {
    activeTab,
    settingsTabs,
    selectSettingsTab,
    handleSettingsTabKeydown,
  } = settingsNavigation;

  const loginAgreement = useLoginAgreement(() => form);

  const {
    defaultLoginAgreementDocuments,
    loginAgreementRoutePath,
    addLoginAgreementDocument,
    removeLoginAgreementDocument,
    normalizeLoginAgreementDocumentsForSave,
    findDuplicateLoginAgreementDocumentId,
  } = loginAgreement;

  const claudeSystemPrompt = useClaudeSystemPrompt(() => form);

  const {
    parseClaudeOAuthSystemPromptBlocks,
    serializeClaudeOAuthSystemPromptBlocksToJSON,
    defaultClaudeOAuthSystemPromptBlocks,
    claudeOAuthSystemPromptBlocks,
    claudeOAuthSystemPromptPresetOptions,
    claudeOAuthSystemPromptBlockTypeOptions,
    claudeOAuthSystemPromptCacheTTLOptions,
    getClaudeOAuthPresetLabel,
    syncClaudeOAuthSystemPromptBlocksFormField,
    addClaudeOAuthSystemPromptBlock,
    toggleClaudeOAuthSystemPromptBlock,
    removeClaudeOAuthSystemPromptBlock,
    moveClaudeOAuthSystemPromptBlock,
    applyClaudeOAuthSystemPromptPreset,
    markClaudeOAuthSystemPromptBlockCustom,
    resetClaudeOAuthSystemPromptBlocks,
  } = claudeSystemPrompt;

  const adminApiKey = useAdminApiKey();

  const {
    adminApiKeyLoading,
    adminApiKeyExists,
    adminApiKeyMasked,
    adminApiKeyOperating,
    newAdminApiKey,
    loadAdminApiKey,
    createAdminApiKey,
    regenerateAdminApiKey,
    deleteAdminApiKey,
    copyNewKey,
  } = adminApiKey;

  const upstreamBillingProbe = useUpstreamBillingProbe();

  const {
    upstreamBillingProbeLoading,
    upstreamBillingProbeSaving,
    upstreamBillingProbeForm,
    loadUpstreamBillingProbeSettings,
    saveUpstreamBillingProbeSettings,
  } = upstreamBillingProbe;

  const ollamaCloudUsage = useOllamaCloudUsage();

  const {
    ollamaCloudUsageLoading,
    ollamaCloudUsageSaving,
    ollamaCloudUsageForm,
    loadOllamaCloudUsageSettings,
    saveOllamaCloudUsageSettings,
  } = ollamaCloudUsage;

  const overloadCooldown = useOverloadCooldown();

  const {
    overloadCooldownLoading,
    overloadCooldownSaving,
    overloadCooldownForm,
    loadOverloadCooldownSettings,
    saveOverloadCooldownSettings,
  } = overloadCooldown;

  const rateLimitCooldown = useRateLimitCooldown();

  const {
    rateLimit429CooldownLoading,
    rateLimit429CooldownSaving,
    rateLimit429CooldownForm,
    loadRateLimit429CooldownSettings,
    saveRateLimit429CooldownSettings,
  } = rateLimitCooldown;

  const panelRateLimit = usePanelRateLimit();

  const {
    panelRateLimitLoading,
    panelRateLimitSaving,
    panelRateLimitForm,
    loadPanelRateLimitSettings,
    savePanelRateLimitSettings,
  } = panelRateLimit;

  const streamTimeout = useStreamTimeout();

  const {
    streamTimeoutLoading,
    streamTimeoutSaving,
    streamTimeoutForm,
    loadStreamTimeoutSettings,
    saveStreamTimeoutSettings,
  } = streamTimeout;

  const rectifier = useRectifier();

  const {
    rectifierLoading,
    rectifierSaving,
    rectifierForm,
    loadRectifierSettings,
    saveRectifierSettings,
  } = rectifier;

  const betaPolicy = useBetaPolicy();

  const {
    betaPolicyLoading,
    betaPolicySaving,
    betaPolicyForm,
    betaPolicyActionOptions,
    betaPolicyScopeOptions,
    betaPresets,
    commonModelPatterns,
    getBetaDisplayName,
    applyBetaPreset,
    addQuickPattern,
    loadBetaPolicySettings,
    saveBetaPolicySettings,
  } = betaPolicy;

  const openAIFastPolicy = useOpenAIFastPolicy();

  const {
    openaiFastPolicyForm,
    openaiFastPolicyLoaded,
    openaiFastPolicyTierOptions,
    openaiFastPolicyActionOptions,
    openaiFastPolicyActionSummary,
    hasOpenAIFastPolicyTargetModels,
    openaiFastPolicyScopeOptions,
    addOpenAIFastPolicyRule,
    removeOpenAIFastPolicyRule,
    addOpenAIFastPolicyModelPattern,
    removeOpenAIFastPolicyModelPattern,
  } = openAIFastPolicy;

  const webSearchSettings = useWebSearchSettings();

  const {
    webSearchProxies,
    webSearchConfig,
    expandedProviders,
    apiKeyVisible,
    wsTestQuery,
    wsTestLoading,
    wsTestResult,
    wsTestDialogOpen,
    openTestDialog,
    toggleProviderExpand,
    removeWebSearchProvider,
    addWebSearchProvider,
    formatSubscribedAt,
    parseSubscribedAt,
    quotaPercentage,
    resetWebSearchUsage,
    copyApiKey,
    testWebSearchProvider,
    loadWebSearchConfig,
    saveWebSearchConfig,
  } = webSearchSettings;

  const registrationPolicy = useRegistrationPolicy();

  const {
    registrationEmailSuffixWhitelistTags,
    registrationEmailSuffixWhitelistDraft,
    removeRegistrationEmailSuffixWhitelistTag,
    commitRegistrationEmailSuffixWhitelistDraft,
    handleRegistrationEmailSuffixWhitelistDraftInput,
    handleRegistrationEmailSuffixWhitelistDraftKeydown,
    handleRegistrationEmailSuffixWhitelistPaste,
  } = registrationPolicy;

  const siteSettings = useSiteSettings(() => form);

  const {
    tablePageSizeOptionsInput,
    tablePageSizeMin,
    tablePageSizeMax,
    tablePageSizeDefault,
    addMenuItem,
    removeMenuItem,
    moveMenuItem,
    addEndpoint,
    removeEndpoint,
    formatTablePageSizeOptions,
    parseTablePageSizeOptionsInput,
    siteBillingModeOptions,
    siteBillingMode,
    siteBillingModeHint,
  } = siteSettings;

  const form = reactive({
    ...toRefs(useSecuritySettingsForm(localText)),
    ...toRefs(useGeneralSettingsForm(tablePageSizeDefault)),
    ...toRefs(useAgreementSettingsForm(defaultLoginAgreementDocuments)),
    ...toRefs(useUsersSettingsForm()),
    ...toRefs(useGatewaySettingsForm(defaultClaudeOAuthSystemPromptBlocks)),
    ...toRefs(useFeaturesSettingsForm()),
    ...toRefs(usePaymentSettingsForm()),
    ...toRefs(useEmailSettingsForm()),
  }) as SettingsForm;

  const captchaSettings = useCaptchaSettings(form);

  const {
    captchaProviderSelection,
    captchaMasterEnabled,
    selectCaptchaProvider,
    tencentCaptchaLinks,
    syncCaptchaProviderSelection,
  } = captchaSettings;

  const openAIScheduler = useOpenAIScheduler(form);

  const { openAIAdvancedSchedulerWeightFields } = openAIScheduler;

  const userDefaults = useUserDefaults(form);

  const {
    subscriptionGroups,
    schedulingThresholdPlatforms,
    authSourceDefaults,
    authSourceDefaultsMeta,
    defaultSubscriptionGroupOptions,
    loadSubscriptionGroups,
    addDefaultSubscription,
    removeDefaultSubscription,
    addAuthSourceDefaultSubscription,
    removeAuthSourceDefaultSubscription,
    findDuplicateDefaultSubscription,
  } = userDefaults;

  const forwardedHeaders = useForwardedHeaders(form);

  const {
    forwardedClientIpHeaderDraft,
    normalizeForwardedClientIpHeaders,
    removeForwardedClientIpHeader,
    commitForwardedClientIpHeaderDraft,
    handleForwardedClientIpHeaderKeydown,
    handleForwardedClientIpHeaderPaste,
  } = forwardedHeaders;

  const emailSettings = useEmailSettings(form);

  const {
    testingSmtp,
    sendingTestEmail,
    smtpPasswordManuallyEdited,
    testEmailAddress,
    addQuotaNotifyEmail,
    testSmtpConnection,
    sendTestEmail,
  } = emailSettings;

  const oAuthSettings = useOAuthSettings(form);

  const {
    currentOrigin,
    linuxdoRedirectUrlSuggestion,
    setAndCopyLinuxdoRedirectUrl,
    githubOAuthRedirectUrlSuggestion,
    googleOAuthRedirectUrlSuggestion,
    setAndCopyEmailOAuthRedirectUrl,
    wechatRedirectUrlSuggestion,
    syncWeChatConnectMode,
    handleWeChatOpenEnabledChange,
    handleWeChatMPEnabledChange,
    handleWeChatMobileEnabledChange,
    setAndCopyWeChatRedirectUrl,
    oidcRedirectUrlSuggestion,
    setAndCopyOIDCRedirectUrl,
  } = oAuthSettings;

  const codexClientSettings = useCodexClientSettings(form);

  const {
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
  } = codexClientSettings;

  const paymentSettings = usePaymentSettings(form, saveSettings);

  const {
    paymentGuideHref,
    paymentMethodsHref,
    allPaymentTypes,
    isPaymentTypeEnabled,
    hasAnyPaymentTypeEnabled,
    togglePaymentType,
    providersLoading,
    providerSaving,
    providers,
    showProviderDialog,
    showDeleteProviderDialog,
    editingProvider,
    providerDialogRef,
    providerKeyOptions,
    enabledProviderKeyOptions,
    loadBalanceOptions,
    cancelRateLimitUnitOptions,
    cancelRateLimitModeOptions,
    loadProviders,
    openCreateProvider,
    openEditProvider,
    handleSaveProvider,
    handleToggleField,
    handleToggleType,
    confirmDeleteProvider,
    handleReorderProviders,
    handleDeleteProvider,
  } = paymentSettings;

  const affiliateSettings = useAffiliateSettings(form);

  const {
    affiliateState,
    affiliateModal,
    affiliateBatchModal,
    affiliateConfirmDialog,
    handleAffiliateConfirm,
    cancelAffiliateConfirm,
    onAffiliateSearchInput,
    changeAffiliatePage,
    toggleAffiliateSelectAll,
    toggleAffiliateSelect,
    openAffiliateModal,
    closeAffiliateModal,
    onAffiliateUserSearchInput,
    selectAffiliateUser,
    clearSelectedAffiliateUser,
    affiliateModalCanSubmit,
    submitAffiliateModal,
    askResetAffiliateUser,
    openAffiliateBatchModal,
    submitAffiliateBatchModal,
  } = affiliateSettings;

  // 关闭 step-up 开关是敏感操作：后端返回 STEP_UP_REQUIRED 时弹 TOTP 码重试
  const settingsStepUp = useStepUp();

  const adminSettingsStore = useAdminSettingsStore();

  const loading = ref(true);

  const loadFailed = ref(false);

  const saving = ref(false);
  return {
    t,
    isZhLocale,
    localText,
    activeTab,
    settingsTabs,
    selectSettingsTab,
    handleSettingsTabKeydown,
    loginAgreementRoutePath,
    addLoginAgreementDocument,
    removeLoginAgreementDocument,
    claudeOAuthSystemPromptBlocks,
    claudeOAuthSystemPromptPresetOptions,
    claudeOAuthSystemPromptBlockTypeOptions,
    claudeOAuthSystemPromptCacheTTLOptions,
    getClaudeOAuthPresetLabel,
    addClaudeOAuthSystemPromptBlock,
    toggleClaudeOAuthSystemPromptBlock,
    removeClaudeOAuthSystemPromptBlock,
    moveClaudeOAuthSystemPromptBlock,
    applyClaudeOAuthSystemPromptPreset,
    markClaudeOAuthSystemPromptBlockCustom,
    resetClaudeOAuthSystemPromptBlocks,
    adminApiKeyLoading,
    adminApiKeyExists,
    adminApiKeyMasked,
    adminApiKeyOperating,
    newAdminApiKey,
    createAdminApiKey,
    regenerateAdminApiKey,
    deleteAdminApiKey,
    copyNewKey,
    upstreamBillingProbeLoading,
    upstreamBillingProbeSaving,
    upstreamBillingProbeForm,
    saveUpstreamBillingProbeSettings,
    ollamaCloudUsageLoading,
    ollamaCloudUsageSaving,
    ollamaCloudUsageForm,
    saveOllamaCloudUsageSettings,
    overloadCooldownLoading,
    overloadCooldownSaving,
    overloadCooldownForm,
    saveOverloadCooldownSettings,
    rateLimit429CooldownLoading,
    rateLimit429CooldownSaving,
    rateLimit429CooldownForm,
    saveRateLimit429CooldownSettings,
    panelRateLimitLoading,
    panelRateLimitSaving,
    panelRateLimitForm,
    savePanelRateLimitSettings,
    streamTimeoutLoading,
    streamTimeoutSaving,
    streamTimeoutForm,
    saveStreamTimeoutSettings,
    rectifierLoading,
    rectifierSaving,
    rectifierForm,
    saveRectifierSettings,
    betaPolicyLoading,
    betaPolicySaving,
    betaPolicyForm,
    betaPolicyActionOptions,
    betaPolicyScopeOptions,
    betaPresets,
    commonModelPatterns,
    getBetaDisplayName,
    applyBetaPreset,
    addQuickPattern,
    saveBetaPolicySettings,
    openaiFastPolicyForm,
    openaiFastPolicyTierOptions,
    openaiFastPolicyActionOptions,
    openaiFastPolicyActionSummary,
    hasOpenAIFastPolicyTargetModels,
    openaiFastPolicyScopeOptions,
    addOpenAIFastPolicyRule,
    removeOpenAIFastPolicyRule,
    addOpenAIFastPolicyModelPattern,
    removeOpenAIFastPolicyModelPattern,
    captchaProviderSelection,
    captchaMasterEnabled,
    selectCaptchaProvider,
    tencentCaptchaLinks,
    openAIAdvancedSchedulerWeightFields,
    subscriptionGroups,
    schedulingThresholdPlatforms,
    authSourceDefaults,
    authSourceDefaultsMeta,
    defaultSubscriptionGroupOptions,
    addDefaultSubscription,
    removeDefaultSubscription,
    addAuthSourceDefaultSubscription,
    removeAuthSourceDefaultSubscription,
    webSearchProxies,
    webSearchConfig,
    expandedProviders,
    apiKeyVisible,
    wsTestQuery,
    wsTestLoading,
    wsTestResult,
    wsTestDialogOpen,
    openTestDialog,
    toggleProviderExpand,
    removeWebSearchProvider,
    addWebSearchProvider,
    formatSubscribedAt,
    parseSubscribedAt,
    quotaPercentage,
    resetWebSearchUsage,
    copyApiKey,
    testWebSearchProvider,
    registrationEmailSuffixWhitelistTags,
    registrationEmailSuffixWhitelistDraft,
    removeRegistrationEmailSuffixWhitelistTag,
    commitRegistrationEmailSuffixWhitelistDraft,
    handleRegistrationEmailSuffixWhitelistDraftInput,
    handleRegistrationEmailSuffixWhitelistDraftKeydown,
    handleRegistrationEmailSuffixWhitelistPaste,
    forwardedClientIpHeaderDraft,
    removeForwardedClientIpHeader,
    commitForwardedClientIpHeaderDraft,
    handleForwardedClientIpHeaderKeydown,
    handleForwardedClientIpHeaderPaste,
    testingSmtp,
    sendingTestEmail,
    smtpPasswordManuallyEdited,
    testEmailAddress,
    addQuotaNotifyEmail,
    testSmtpConnection,
    sendTestEmail,
    currentOrigin,
    linuxdoRedirectUrlSuggestion,
    setAndCopyLinuxdoRedirectUrl,
    githubOAuthRedirectUrlSuggestion,
    googleOAuthRedirectUrlSuggestion,
    setAndCopyEmailOAuthRedirectUrl,
    wechatRedirectUrlSuggestion,
    handleWeChatOpenEnabledChange,
    handleWeChatMPEnabledChange,
    handleWeChatMobileEnabledChange,
    setAndCopyWeChatRedirectUrl,
    oidcRedirectUrlSuggestion,
    setAndCopyOIDCRedirectUrl,
    tablePageSizeOptionsInput,
    addMenuItem,
    removeMenuItem,
    moveMenuItem,
    addEndpoint,
    removeEndpoint,
    siteBillingModeOptions,
    siteBillingMode,
    siteBillingModeHint,
    codexBlacklistRows,
    codexWhitelistRows,
    codexFingerprintRows,
    codexFingerprintNoRequired,
    addCodexFingerprintRow,
    removeCodexFingerprintRow,
    addCodexBlacklistRow,
    removeCodexBlacklistRow,
    addCodexWhitelistRow,
    removeCodexWhitelistRow,
    codexSyncedVersionLabel,
    paymentGuideHref,
    paymentMethodsHref,
    allPaymentTypes,
    isPaymentTypeEnabled,
    hasAnyPaymentTypeEnabled,
    togglePaymentType,
    providersLoading,
    providerSaving,
    providers,
    showProviderDialog,
    showDeleteProviderDialog,
    editingProvider,
    providerDialogRef,
    providerKeyOptions,
    enabledProviderKeyOptions,
    loadBalanceOptions,
    cancelRateLimitUnitOptions,
    cancelRateLimitModeOptions,
    loadProviders,
    openCreateProvider,
    openEditProvider,
    handleSaveProvider,
    handleToggleField,
    handleToggleType,
    confirmDeleteProvider,
    handleReorderProviders,
    handleDeleteProvider,
    affiliateState,
    affiliateModal,
    affiliateBatchModal,
    affiliateConfirmDialog,
    handleAffiliateConfirm,
    cancelAffiliateConfirm,
    onAffiliateSearchInput,
    changeAffiliatePage,
    toggleAffiliateSelectAll,
    toggleAffiliateSelect,
    openAffiliateModal,
    closeAffiliateModal,
    onAffiliateUserSearchInput,
    selectAffiliateUser,
    clearSelectedAffiliateUser,
    affiliateModalCanSubmit,
    submitAffiliateModal,
    askResetAffiliateUser,
    openAffiliateBatchModal,
    submitAffiliateBatchModal,
    settingsStepUp,
    loading,
    loadFailed,
    saving,
    saveSettings,
    form,
    appStore,
    defaultLoginAgreementDocuments,
    parseClaudeOAuthSystemPromptBlocks,
    defaultClaudeOAuthSystemPromptBlocks,
    syncClaudeOAuthSystemPromptBlocksFormField,
    openaiFastPolicyLoaded,
    loadWebSearchConfig,
    formatTablePageSizeOptions,
    syncCaptchaProviderSelection,
    normalizeForwardedClientIpHeaders,
    parseCodexEntriesToRows,
    normalizeLoginAgreementDocumentsForSave,
    findDuplicateLoginAgreementDocumentId,
    serializeClaudeOAuthSystemPromptBlocksToJSON,
    saveWebSearchConfig,
    tablePageSizeMin,
    tablePageSizeMax,
    parseTablePageSizeOptionsInput,
    findDuplicateDefaultSubscription,
    syncWeChatConnectMode,
    serializeCodexRowsToJSON,
    adminSettingsStore,
    loadSubscriptionGroups,
    loadAdminApiKey,
    loadUpstreamBillingProbeSettings,
    loadOllamaCloudUsageSettings,
    loadOverloadCooldownSettings,
    loadRateLimit429CooldownSettings,
    loadPanelRateLimitSettings,
    loadStreamTimeoutSettings,
    loadRectifierSettings,
    loadBetaPolicySettings,
  };
}
