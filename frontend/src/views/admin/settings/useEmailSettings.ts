import { ref } from "vue";
import { adminAPI } from "@/api";
import { extractApiErrorMessage } from "@/utils/apiError";
import { useSettingsServices } from "./useSettingsServices";
import type { SettingsForm } from "./settingsForm";

export function useEmailSettings(form: SettingsForm) {
  const { t, appStore } = useSettingsServices();
  const testingSmtp = ref(false);

  const sendingTestEmail = ref(false);

  const smtpPasswordManuallyEdited = ref(false);

  const testEmailAddress = ref("");

  // Quota notify email helpers
  const addQuotaNotifyEmail = () => {
    if (!form.account_quota_notify_emails) {
      form.account_quota_notify_emails = [];
    }
    form.account_quota_notify_emails.push({
      email: "",
      disabled: false,
      verified: true,
    });
  };

  async function testSmtpConnection() {
    testingSmtp.value = true;
    try {
      const smtpPasswordForTest = smtpPasswordManuallyEdited.value
        ? form.smtp_password
        : "";
      const result = await adminAPI.settings.testSmtpConnection({
        smtp_host: form.smtp_host,
        smtp_port: form.smtp_port,
        smtp_username: form.smtp_username,
        smtp_password: smtpPasswordForTest,
        smtp_use_tls: form.smtp_use_tls,
      });
      // API returns { message: "..." } on success, errors are thrown as exceptions
      appStore.showSuccess(
        result.message || t("admin.settings.smtpConnectionSuccess"),
      );
    } catch (error: unknown) {
      appStore.showError(
        extractApiErrorMessage(error, t("admin.settings.failedToTestSmtp")),
      );
    } finally {
      testingSmtp.value = false;
    }
  }

  async function sendTestEmail() {
    if (!testEmailAddress.value) {
      appStore.showError(t("admin.settings.testEmail.enterRecipientHint"));
      return;
    }

    sendingTestEmail.value = true;
    try {
      const smtpPasswordForSend = smtpPasswordManuallyEdited.value
        ? form.smtp_password
        : "";
      const result = await adminAPI.settings.sendTestEmail({
        email: testEmailAddress.value,
        smtp_host: form.smtp_host,
        smtp_port: form.smtp_port,
        smtp_username: form.smtp_username,
        smtp_password: smtpPasswordForSend,
        smtp_from_email: form.smtp_from_email,
        smtp_from_name: form.smtp_from_name,
        smtp_use_tls: form.smtp_use_tls,
      });
      // API returns { message: "..." } on success, errors are thrown as exceptions
      appStore.showSuccess(result.message || t("admin.settings.testEmailSent"));
    } catch (error: unknown) {
      appStore.showError(
        extractApiErrorMessage(
          error,
          t("admin.settings.failedToSendTestEmail"),
        ),
      );
    } finally {
      sendingTestEmail.value = false;
    }
  }

  return {
    testingSmtp,
    sendingTestEmail,
    smtpPasswordManuallyEdited,
    testEmailAddress,
    addQuotaNotifyEmail,
    testSmtpConnection,
    sendTestEmail,
  };
}
