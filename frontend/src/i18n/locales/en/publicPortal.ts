export default {
  publicPortal: {
    support: {
      footerLink: 'Support',
      sectionTitle: 'Human Support',
      sectionSummary: 'Reach our support team for billing disputes, account requests, or technical issues.',
      badge: 'SUPPORT',
      contactLabel: 'Contact',
      contactPlaceholder: 'Not configured by the site operator yet. Please contact your workspace administrator.',
      scopeLabel: 'Scope',
      scopeValue: 'Billing & payment disputes, account deletion/export requests, invoice/receipt inquiries, and API quota or gateway troubleshooting.',
      slaLabel: 'Response Time',
      slaValue: 'Within 1–2 business days (Mon–Fri 09:00–18:00 UTC+8).'
    },
    accountService: {
      lifecycleTitle: 'Account Data Export & Deletion',
      lifecycleSummary: 'Self-service automated export and deletion are not enabled. Submit requests through human support for identity and settlement verification.',
      exportTitle: 'Personal Data Export',
      exportDesc: 'Request a copy of your account profile, billing summary, and usage records via human support. Include your account email and user ID.',
      deletionTitle: 'Account Deletion',
      deletionDesc: 'Request permanent account closure via human support after verifying remaining balance and pending settlements. Include your account email and user ID.',
      supportLinkLabel: 'Go to Human Support',
      invoiceTitle: 'Order Invoices & Receipts',
      invoiceSummary: 'Automated invoice download is not enabled. Request official receipts or invoices for completed orders via human support.',
      invoiceDesc: 'Provide your Order ID, billing entity name, and tax or receipt details when contacting support.',
      invoiceSupportLink: 'Request via Human Support'
    },
    paymentDisabled: {
      title: 'Online Payment Is Currently Disabled',
      description: 'The site administrator has not enabled online top-ups or subscriptions at this time. You can still redeem a balance code, return to the dashboard, or contact support.',
      redeemAction: 'Redeem Code',
      dashboardAction: 'Back to Dashboard',
      supportAction: 'Human Support'
    },
    totpSummary: {
      enabled: 'Enabled',
      configurable: 'Configurable',
      disabled: 'Not enabled by admin',
      checking: 'Checking'
    },
    docs: {
      loginRequired: 'Login required'
    },
    keyUsage: {
      showKey: 'Show API key',
      hideKey: 'Hide API key'
    },
    mcpAuthorize: {
      titleApprovedLocal: 'Authorization Request Cached',
      titleApproved: 'Authorization Complete',
      titleDenied: 'Authorization Denied',
      titleConsent: 'Authorize Application Access',
      subtitle: 'Review the application, callback URL, and requested permissions before deciding.',
      invalidRequestTitle: 'Invalid Authorization Request',
      appName: 'Application',
      clientId: 'Client ID',
      redirectUri: 'Redirect URI',
      resourceScope: 'Resource',
      notProvided: 'Not provided',
      statusApprovedLocal: 'Waiting for authorization service confirmation',
      statusApproved: 'Authorization grant saved',
      statusDenied: 'No permissions were granted for this request',
      scopeAdmin: 'Platform Admin',
      scopeUser: 'Personal Account',
      permissionWrite: 'Read & Write',
      permissionRead: 'Read-Only',
      viewGrants: 'View Authorization Grants →',
      stepupTitle: 'Confirm Platform Admin Access',
      stepupNotice: 'Modifying platform admin data requires 2FA verification. Enter the 6-digit code from your authenticator app.',
      totpLabel: '6-digit verification code',
      submitting: 'Submitting…',
      verifyAndAuthorize: 'Verify & Authorize',
      cancelVerification: 'Cancel',
      scopeLabel: 'Access Scope',
      scopeOptionUser: 'Personal Account',
      scopeOptionAdmin: 'Platform Admin (requires 2FA)',
      permissionLabel: 'Permission',
      permissionHintWrite: 'Allows the application to modify data within the selected scope.',
      permissionHintRead: 'Allows the application to read data only within the selected scope.',
      adminScopeWarning: 'Platform Admin scope affects all users and requires 2FA step-up for write access.',
      authorizing: 'Authorizing…',
      confirmAuthorize: 'Authorize',
      denyAuthorize: 'Deny',
      backToMcpSettings: 'Back to MCP Settings',
      errGeneric: 'Operation failed. Please try again later.',
      errMissingParams: 'Missing required parameters: client_id, redirect_uri, and resource are required. Restart the flow from your client.',
      errInvalidRedirect: 'Invalid callback URL. Only full http:// or https:// URLs without credentials are allowed.',
      errResourceMismatch: 'Resource URL does not match this site MCP endpoint. Expected: {endpoint}',
      errAdminRequired: 'Platform Admin scope requires an administrator account.',
      errInvalidTotp: 'Please enter a valid 6-digit numeric code.',
      errLoginRequired: 'Please sign in before authorizing.',
      errApproveUnconfirmed: 'Authorization service did not confirm approval.',
      errLocalSaveFailed: 'Failed to save local authorization. Check browser storage permissions and retry.',
      noticeSavedRemoteNoCache: 'Grant saved on server; local cache unavailable. Refresh MCP settings to view.',
      noticeSavedLocalOnly: 'Authorization service unavailable; request cached locally. Re-confirm once service recovers.',
      errDenyUnconfirmed: 'Authorization service did not confirm denial.',
      noticeDenySyncFailed: 'Authorization cancelled, but denial record could not be synced.',
      noticeDenyListSyncFailed: 'Request denied, but grant list could not be synced.',
      noticeDenyLocalSaveFailed: 'Request denied, but local record could not be saved.'
    },
    compatibilityChangelog: {
      eyebrow: 'VERSIONED CHANGELOG',
      title: 'Protocol & Migration Changelog',
      v210: {
          version: 'v2.1.0',
          date: '2026-11-20',
          summary: 'Public model catalog, interactive exact-decimal scenario compare, and /v1/images/generations + /v1/images/edits routes.',
          migration: 'Use /model-plaza and /v1/images/generations; /models redirects to /model-plaza.'
      },
      v200: {
          version: 'v2.0.0',
          date: '2026-10-15',
          summary: 'Connection wizard v2 requires /models probe evidence (409 EVIDENCE_UNAVAILABLE if unsupported); legacy .s2plugin packages remain loadable through the v1 host message protocol during migration.',
          migration: 'Migrate extensions to .ohplugin v2 before legacy bridge removal; save non-probing upstreams as drafts.'
      }
    },
    legal: {
      retry: 'Retry',
      settingsLoadFailedHint: 'Unable to load site legal and registration settings. Please retry before continuing.',
      templateNoticeTitle: 'Default template shown',
      templateNoticeBody: 'The site operator has not published custom text for this slot yet. Contact site support for operator-specific legal entity, data retention, and regional terms.',
      contactOperator: 'Contact support',
      documentsNavAria: 'Legal documents',
      backToHome: 'Back to home',
      viewTermsButton: 'View Terms of Service',
      viewPrivacyButton: 'View Privacy Policy',
      openFullDocument: 'Open full document in new tab',
      agreementRequiredRefresh: 'The site legal documents were updated. Please review and accept the latest version to continue.',
      slotTitles: {
        terms: 'OmniHive Terms of Service',
        privacy: 'OmniHive Privacy Policy',
        usagePolicy: 'OmniHive Acceptable Use Policy',
        refundPolicy: 'OmniHive Refund and Billing Policy',
        supportedRegions: 'Supported Countries and Regions',
        serviceSpecificTerms: 'Service-Specific Terms'
      },
      promptSummary: {
        terms: '## Service and accounts\nOmniHive provides model API access, account management, and usage information. Protect passwords and API keys and follow applicable laws and your selected provider’s requirements.\n\n## Charges and support\nReview displayed prices, quotas, and validity periods before using paid features. Contact the site administrator about services, billing, or your account.\n\nRead the full Terms of Service using the link below.',
        privacy: '## Information and purposes\nAccount information, requests, and usage records support verification, services, billing, and security. Your selected upstream provider may process model requests. Avoid submitting unrelated personal information or sensitive credentials.\n\n## Storage and contact\nBrowser storage supports sign-in, preferences, and registration. Contact the site operator about retention periods, deployment region, and access, correction, or deletion requests.\n\nRead the full Privacy Policy using the link below.'
      },
      templates: {
        terms: `## Service scope
OmniHive provides account management, model API access, usage information, and related gateway features. Available features, models, and quotas depend on this site's settings and your account permissions.

## Accounts and credentials
Provide accurate registration details, safeguard your passwords and API keys, and remain responsible for activity under your account. Update compromised credentials immediately and contact the site administrator.

## Acceptable use
Comply with applicable laws and your selected model provider's requirements. Do not attempt unauthorized access, abuse endpoints, or infringe third-party rights. Submit only content you are authorized to process and verify model outputs before relying on them.

## Fees and quotas
Before using paid features, review the pricing, billing rules, quotas, and validity periods published on this site. Contact site support for billing, refund, or invoice inquiries.

## Third-party providers and availability
Model requests may be routed to selected upstream providers, whose availability and outputs affect responses. Maintenance, rate limits, or upstream outages may cause errors; evaluate responses before retrying.

## Updates and contact
Review published terms and updates before using the service. Contact the site operator if you have questions about service scope, account actions, or these terms.`,
        privacy: `## Information involved
Registration, sign-in, and use of OmniHive involve account information you provide (such as email and username), verification records, API requests, usage records, and billing/order history depending on enabled features.

## Purposes
This information supports identity verification, model request routing, usage accounting, billing, troubleshooting, and service security. Avoid submitting unrelated personal information or sensitive credentials.

## Service providers and upstream processing
Model requests may be processed by the selected upstream model provider. When third-party sign-in, captcha, or payment providers are enabled, those services process information needed for their function.

## Browser storage
Local and session storage hold sign-in state, interface preferences, agreement state, and registration workflow state. Clearing browser storage may require signing in again.

## Retention, region, and user rights
Logging scope, retention periods, deployment region, and access controls depend on the site operator's configuration. Use the site's support channel to request access, correction, export, or account deletion.

## Contact and updates
Contact the site operator regarding privacy or account security with only the details needed to handle your request.`,
        usagePolicy: `## Prohibited activities
Do not use this gateway for unlawful activities, unauthorized security scanning, credential stuffing, abusive automated traffic, or bypassing rate/quota controls.

## Content responsibility
Ensure prompts and payloads comply with applicable regulations and upstream provider policies.

## Enforcement
Violations may result in rate limiting, key revocation, or account suspension according to operator policy.`,
        refundPolicy: `## Balance, plans, and disputes
Completed usage charges are settled according to published pricing and quota rules. Unconsumed balances or failed transactions are handled according to the site operator's policy.

## Submitting a billing or refund request
Include your order ID, payment channel, timestamp, and request/trace details when contacting support.`,
        supportedRegions: `## Service availability
Supported countries, regions, and payment methods depend on the site operator's deployment and upstream provider restrictions.

## Compliance responsibility
Users are responsible for ensuring their access and usage comply with local laws and applicable export or service rules.`,
        serviceSpecificTerms: `## Additional service rules
Specific models, experimental endpoints, batch jobs, or third-party plugins may carry additional limits, rate caps, or billing terms published in the pricing and compatibility pages.`
      }
    }
  }
}
