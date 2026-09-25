const API_URL = process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080";
const API_BASE = `${API_URL}/api/v1`;

export class ApiError extends Error {
  constructor(public status: number, public code: string, message: string, public requestId?: string) {
    super(message);
    this.name = "ApiError";
  }
}

// ── Phase 8 billing types ──
// Amounts are integer kobo everywhere. Floating point never touches money, and
// the API never sends a formatted amount that a client could round.
export interface Plan {
  code: string;
  name: string;
  description: string;
  price_kobo: number;
  currency: string;
  interval: string;
  trial_days: number;
  entitlements: Record<string, number>;
  active: boolean;
}

export interface Subscription {
  id: string;
  organization_id: string;
  plan_code: string;
  status: "trialing" | "active" | "past_due" | "cancelled" | "expired";
  trial_ends_at?: string;
  current_period_start?: string;
  current_period_end?: string;
  grace_ends_at?: string;
  read_only: boolean;
  read_only_reason?: string;
  cancel_at_period_end: boolean;
  cancelled_at?: string;
  retention_ends_at?: string;
  updated_at: string;
}

export interface BillingStatus {
  subscription: Subscription | null;
  entitlements: Record<string, number>;
  usage: Record<string, number>;
  read_only: boolean;
  read_only_reason?: string;
}

export interface CheckoutSession {
  authorization_url: string;
  access_code: string;
  reference: string;
  amount_kobo: number;
  currency: string;
}

export interface PaymentRecord {
  reference: string;
  amount_kobo: number;
  amount_display: string;
  currency: string;
  status: "pending" | "success" | "failed" | "refunded" | "reversed";
  channel: string;
  plan_code: string;
  failure_reason: string;
  paid_at?: string;
  created_at: string;
}

export interface DeadLetterEntry {
  id: string;
  queue: string;
  job_kind: string;
  organization_id: string;
  payload: unknown;
  last_error: string;
  attempts: number;
  created_at: string;
}

async function request<T>(path: string, init: RequestInit = {}): Promise<T> {
  const isFormData = typeof FormData !== "undefined" && init.body instanceof FormData;
  const response = await fetch(`${API_URL}/api/v1${path}`, {
    ...init,
    credentials: "include",
    headers: isFormData ? init.headers : { "Content-Type": "application/json", ...(init.headers ?? {}) },
  });
  if (!response.ok) {
    let code = "INTERNAL_ERROR";
    let message = `Request failed (${response.status}).`;
    let requestId: string | undefined;
    try {
      const body = await response.json();
      code = body.error?.code ?? code;
      message = body.error?.message ?? message;
      requestId = body.error?.request_id;
    } catch { /* retain safe fallback */ }
    throw new ApiError(response.status, code, message, requestId);
  }
  return response.json() as Promise<T>;
}

export interface Connection {
  id: string;
  store_id: string;
  store_name: string;
  store_url: string;
  status: string;
  webhook_status: string;
  last_sync_at?: string;
  last_error: string;
  rate_limited_until?: string;
}

export interface Health {
  connection: Connection;
  latest_sync_run_id: string;
  latest_sync_status: string;
  catalog: { products: number; variants: number };
  webhook_events: number;
}

export interface SyncRun {
  id: string;
  store_id: string;
  kind: string;
  status: string;
  imported_products: number;
  imported_variants: number;
  failed_items: number;
  error_summary: string;
  attempts: number;
  started_at?: string;
  finished_at?: string;
  created_at: string;
}

export interface Product {
  id: string;
  external_id: string;
  sku: string;
  name: string;
  status: string;
  stock_status: string;
  stock_quantity?: number;
  price_kobo: number;
  sale_price_kobo?: number;
  variant_count: number;
  needs_review: boolean;
  review_reason: string;
  price_conflict: boolean;
  categories: string[];
  last_seen_at: string;
}

export interface CostComponents {
  supplier_cost_kobo: number;
  shipping_cost_kobo: number;
  packaging_cost_kobo: number;
  payment_fee_kobo: number;
  tax_import_cost_kobo: number;
  other_allocated_cost_kobo: number;
}

export interface CostRow {
  product_id: string;
  product_name: string;
  sku: string;
  current_price_kobo: number;
  cost_id?: string;
  landed_cost_kobo?: number;
  margin_bps?: number;
  minimum_price_kobo?: number;
  status: string;
  effective_from?: string;
}

export interface PricingResult {
  status: string;
  reason: string;
  product_id: string;
  landed_cost_kobo: number;
  current_price_kobo: number;
  current_margin_bps: number;
  minimum_margin_bps: number;
  minimum_profitable_price_kobo: number;
  maximum_price_kobo?: number;
  gross_profit_kobo: number;
  price_change_required_bps: number;
  guardrail_maximum_change_bps: number;
  rule_name?: string;
  product_locked: boolean;
}

export interface PricingRule {
  id: string;
  name: string;
  scope_type: "product" | "variant" | "category" | "store";
  product_id?: string;
  variant_id?: string;
  category_id?: string;
  minimum_margin_bps: number;
  maximum_change_bps: number;
  maximum_price_kobo?: number;
  rounding_increment_kobo: number;
  priority: number;
  active: boolean;
  cooldown_minutes: number;
  conditions?: { minimum_stock?: number; maximum_current_price_kobo?: number; in_stock_only?: boolean };
  version: number;
}

export interface Competitor {
  id: string; name: string; source_type: string; domain: string; location_market: string; status: string;
  monitoring_policy: { interval_minutes: number; freshness_minutes: number; backoff_minutes: number };
}
export interface CompetitorProduct {
  id: string; competitor_id: string; competitor_name: string; url: string; name: string; sku: string;
  match_state: string; suggested_product_id?: string; suggested_product_name?: string; confirmed_product_id?: string; confirmed_product_name?: string;
  match_confidence_bps: number; last_observed_price_kobo?: number; last_observed_at?: string; consecutive_failures: number; last_error: string;
}
export interface CompetitorObservation { id: string; observed_price_kobo?: number; currency?: string; availability: string; extraction_confidence_bps: number; observed_at: string; fresh_until: string; }
export interface CompetitorAlert { id: string; alert_type: string; message: string; created_at: string; }
export interface CompetitorHealth { competitor_product_id: string; url: string; status: string; consecutive_failures: number; last_error: string; last_success_at?: string; next_check_at?: string; }

export interface RecommendationReason { type: string; message: string; amount_kobo?: number; source?: string; evidence?: string; }
export interface Recommendation {
  id: string; product_id: string; product_name: string; product_sku: string; variant_id?: string;
  state: "hold" | "raise" | "lower" | "investigate" | "pause";
  previous_price_kobo: number; recommended_price_kobo: number; minimum_profitable_price_kobo: number;
  maximum_price_kobo?: number; lowest_confirmed_competitor_kobo?: number;
  change_bps: number; confidence_bps: number; urgency: string; margin_risk: string;
  opportunity_kobo?: number; explanation: string; rule_name?: string;
  stock_status: string; stock_quantity?: number; reasons: RecommendationReason[];
  generated_at: string; expires_at: string;
}
export interface PriceChangeRequest {
  id: string; recommendation_id: string; product_id: string; product_name: string;
  previous_price_kobo: number; requested_price_kobo: number; status: string;
  scheduled_for?: string; expires_at: string; approved_role?: string; approval_note?: string;
  execution_id?: string; execution_status?: string; verified_after_price_kobo?: number;
  rollback_id?: string; rollback_status?: string; last_error?: string;
  created_at: string; updated_at: string;
}
export interface KillSwitch { scope: string; enabled: boolean; reason: string; updated_at: string; }
export interface ApprovalLimit { role_id: string; maximum_change_bps: number; maximum_price_kobo?: number; }

export interface ReportData { kind: string; columns: string[]; rows: string[][]; row_count: number; generated_at: string; }
export interface NotificationItem {
  id: string; type: string; title: string; body: string; read: boolean; created_at: string;
  deliveries?: Array<{ id: string; channel: string; status: string; attempts: number; last_error?: string }>;
}
export interface NotificationPreference {
  alert_type: string; inapp_enabled: boolean; email_enabled: boolean; whatsapp_enabled: boolean;
  critical_override?: boolean; critical_override_reason?: string;
}
export interface NotificationConsent { channel: string; status: string; destination?: string; verified_at?: string; }
export interface Announcement {
  id: string; title: string; body_text: string; body_html?: string; status: string; severity: string;
  publish_at?: string; published_at?: string; current_version: number; read_at?: string; acknowledged_at?: string; created_at: string;
}
export interface FeedbackTicket {
  id: string; kind: string; subject: string; body?: string; status: string; priority: string;
  insight_tags?: string[]; duplicate_of?: string; created_at: string; updated_at: string;
  messages?: Array<{ id: string; author_kind: string; body: string; created_at: string }>;
  file_ids?: string[]; next_states?: string[];
}
export interface CommunityRoom { id: string; slug: string; name: string; description: string; kind: string; member: boolean; can_post: boolean; archived: boolean; }
export interface CommunityPost {
  id: string; room_id: string; room_slug: string; author_name: string; body: string;
  reply_count: number; reaction_count: number; mine: boolean; created_at: string;
}
export interface CommunityReply { id: string; post_id: string; author_name: string; body: string; reaction_count: number; mine: boolean; created_at: string; }
export interface CommunityProfile { user_id: string; display_name: string; headline: string; bio: string; privacy_level: string; rooms: number; }

export const api = {
  signup: (body: { email: string; password: string; first_name: string; last_name: string }) =>
    request<{ user_id: string }>("/auth/signup", { method: "POST", body: JSON.stringify(body) }),
  signin: (body: { email: string; password: string }) =>
    request<{ user_id: string }>("/auth/signin", { method: "POST", body: JSON.stringify(body) }),
  logout: () => request<{ ok: boolean }>("/auth/logout", { method: "POST" }),
  organizations: () => request<{ organizations: Array<{ id: string; name: string; slug: string }> }>("/orgs"),
  // me is the session probe the dashboard entry point uses. A 401 here is the
  // expected answer for an anonymous visitor, so callers must handle it rather
  // than treating it as an error worth reporting.
  me: () => request<{ user_id: string; email: string; first_name: string; last_name: string }>("/auth/me"),
  createOrganization: (body: { name: string; business_category?: string; currency?: string; timezone?: string }) =>
    request<{ id: string; name: string; slug: string }>("/orgs", { method: "POST", body: JSON.stringify(body) }),
  wooStatus: (slug: string) => request<{ connected: boolean; health?: Health }>(`/app/${encodeURIComponent(slug)}/integrations/woocommerce/status`),
  connectWoo: (slug: string, body: { store_name: string; store_url: string; consumer_key: string; consumer_secret: string }) =>
    request<{ connection: Connection; sync_run_id: string }>(`/app/${encodeURIComponent(slug)}/integrations/woocommerce/connect`, { method: "POST", body: JSON.stringify(body) }),
  testWoo: (slug: string) => request<{ ok: boolean }>(`/app/${encodeURIComponent(slug)}/integrations/woocommerce/test`, { method: "POST" }),
  disconnectWoo: (slug: string) => request<{ ok: boolean }>(`/app/${encodeURIComponent(slug)}/integrations/woocommerce/disconnect`, { method: "POST" }),
  syncWoo: (slug: string) => request<{ sync_run_id: string; status: string }>(`/app/${encodeURIComponent(slug)}/integrations/woocommerce/sync`, { method: "POST" }),
  syncRuns: (slug: string) => request<{ runs: SyncRun[] }>(`/app/${encodeURIComponent(slug)}/integrations/woocommerce/sync-runs`),
  retrySync: (slug: string, runId: string) => request<{ sync_run_id: string }>(`/app/${encodeURIComponent(slug)}/integrations/woocommerce/sync-runs/${encodeURIComponent(runId)}/retry`, { method: "POST" }),
  products: (slug: string, query = "") => request<{ products: Product[]; next_cursor?: string }>(`/app/${encodeURIComponent(slug)}/products${query}`),
  reportUrl: (slug: string, runId: string) => `${API_URL}/api/v1/app/${encodeURIComponent(slug)}/integrations/woocommerce/sync-runs/${encodeURIComponent(runId)}/report`,
  costs: (slug: string, query = "") => request<{ costs: CostRow[] }>(`/app/${encodeURIComponent(slug)}/costs${query}`),
  productCost: (slug: string, productId: string) => request<{ id: string; product_id: string; cost: CostComponents; landed_cost_kobo: number; version: number }>(`/app/${encodeURIComponent(slug)}/products/${encodeURIComponent(productId)}/cost`),
  setCost: (slug: string, productId: string, cost: CostComponents) => request(`/app/${encodeURIComponent(slug)}/products/${encodeURIComponent(productId)}/cost`, { method: "PUT", body: JSON.stringify({ currency: "NGN", cost }) }),
  evaluatePrice: (slug: string, productId: string) => request<PricingResult>(`/app/${encodeURIComponent(slug)}/products/${encodeURIComponent(productId)}/pricing`),
  setPriceLock: (slug: string, productId: string, locked: boolean, reason: string) => request(`/app/${encodeURIComponent(slug)}/products/${encodeURIComponent(productId)}/price-lock`, { method: "PUT", body: JSON.stringify({ locked, reason }) }),
  setPricePolicy: (slug: string, policy: { scope_type: "product"; product_id: string; minimum_price_kobo?: number; maximum_price_kobo?: number; rounding_increment_kobo: number }) => request(`/app/${encodeURIComponent(slug)}/price-policy`, { method: "PUT", body: JSON.stringify(policy) }),
  importCosts: (slug: string, file: File) => {
    const body = new FormData(); body.append("file", file);
    return request<{ imported: number; failed: number; issues: Array<{ row: number; sku?: string; message: string }> }>(`/app/${encodeURIComponent(slug)}/costs/import`, { method: "POST", body });
  },
  pricingRules: (slug: string) => request<{ rules: PricingRule[] }>(`/app/${encodeURIComponent(slug)}/pricing-rules`),
  createPricingRule: (slug: string, rule: Omit<PricingRule, "id" | "version">) => request<PricingRule>(`/app/${encodeURIComponent(slug)}/pricing-rules`, { method: "POST", body: JSON.stringify(rule) }),
  updatePricingRule: (slug: string, id: string, rule: Omit<PricingRule, "id" | "version"> & { expected_version: number }) => request<PricingRule>(`/app/${encodeURIComponent(slug)}/pricing-rules/${encodeURIComponent(id)}`, { method: "PATCH", body: JSON.stringify(rule) }),
  deletePricingRule: (slug: string, id: string) => request(`/app/${encodeURIComponent(slug)}/pricing-rules/${encodeURIComponent(id)}`, { method: "DELETE" }),
  simulateRules: (slug: string, productIds: string[], draftRule?: Partial<PricingRule>) => request<{ results: Array<PricingResult & { product_id: string; status: string; reason: string }> }>(`/app/${encodeURIComponent(slug)}/pricing-rules/simulate`, { method: "POST", body: JSON.stringify({ product_ids: productIds, rule: draftRule }) }),
  competitors: (slug: string) => request<{ competitors: Competitor[] }>(`/app/${encodeURIComponent(slug)}/competitors`),
  createCompetitor: (slug: string, value: { name: string; domain: string; location_market?: string; monitoring_policy: { interval_minutes: number; freshness_minutes: number; backoff_minutes: number } }) => request<Competitor>(`/app/${encodeURIComponent(slug)}/competitors`, { method: "POST", body: JSON.stringify(value) }),
  competitorProducts: (slug: string) => request<{ products: CompetitorProduct[] }>(`/app/${encodeURIComponent(slug)}/competitor-products`),
  addCompetitorProduct: (slug: string, value: { competitor_id: string; url: string; name?: string; sku?: string }) => request<CompetitorProduct>(`/app/${encodeURIComponent(slug)}/competitor-products`, { method: "POST", body: JSON.stringify(value) }),
  refreshCompetitorProduct: (slug: string, id: string) => request<{ run_id: string }>(`/app/${encodeURIComponent(slug)}/competitor-products/${encodeURIComponent(id)}/refresh`, { method: "POST" }),
  reviewCompetitorMatch: (slug: string, id: string, productId: string, state: "confirmed" | "rejected" | "needs_review", note = "") => request(`/app/${encodeURIComponent(slug)}/competitor-products/${encodeURIComponent(id)}/review`, { method: "POST", body: JSON.stringify({ product_id: productId, state, note }) }),
  competitorHistory: (slug: string, id: string) => request<{ observations: CompetitorObservation[] }>(`/app/${encodeURIComponent(slug)}/competitor-products/${encodeURIComponent(id)}/history`),
  competitorAlerts: (slug: string) => request<{ alerts: CompetitorAlert[] }>(`/app/${encodeURIComponent(slug)}/competitor-alerts`),
  competitorHealth: (slug: string) => request<{ sources: CompetitorHealth[] }>(`/app/${encodeURIComponent(slug)}/competitor-health`),
  recommendations: (slug: string, filter = "") => request<{ recommendations: Recommendation[] }>(`/app/${encodeURIComponent(slug)}/recommendations${filter}`),
  generateRecommendations: (slug: string) => request<{ generated: number }>(`/app/${encodeURIComponent(slug)}/recommendations/generate`, { method: "POST" }),
  submitRecommendation: (slug: string, id: string) => request<PriceChangeRequest>(`/app/${encodeURIComponent(slug)}/recommendations/${encodeURIComponent(id)}/submit`, { method: "POST" }),
  priceChanges: (slug: string, status = "") => request<{ requests: PriceChangeRequest[] }>(`/app/${encodeURIComponent(slug)}/price-changes${status ? `?status=${encodeURIComponent(status)}` : ""}`),
  approvePriceChange: (slug: string, id: string, decision: "approved" | "rejected", note = "") => request<PriceChangeRequest>(`/app/${encodeURIComponent(slug)}/price-changes/${encodeURIComponent(id)}/approve`, { method: "POST", body: JSON.stringify({ decision, note }) }),
  publishPriceChange: (slug: string, id: string, scheduledFor?: string) => request<{ id: string; status: string }>(`/app/${encodeURIComponent(slug)}/price-changes/${encodeURIComponent(id)}/publish`, { method: "POST", body: JSON.stringify({ scheduled_for: scheduledFor ?? null }) }),
  rollbackPriceChange: (slug: string, id: string) => request<{ id: string; status: string }>(`/app/${encodeURIComponent(slug)}/price-changes/${encodeURIComponent(id)}/rollback`, { method: "POST" }),
  killSwitch: (slug: string) => request<KillSwitch>(`/app/${encodeURIComponent(slug)}/pricing-kill-switch`),
  setKillSwitch: (slug: string, enabled: boolean, reason: string) => request<KillSwitch>(`/app/${encodeURIComponent(slug)}/pricing-kill-switch`, { method: "PUT", body: JSON.stringify({ enabled, reason }) }),
  approvalLimits: (slug: string) => request<{ limits: ApprovalLimit[] }>(`/app/${encodeURIComponent(slug)}/approval-limits`),
  reportKinds: (slug: string) => request<{ kinds: string[] }>(`/app/${encodeURIComponent(slug)}/reports/kinds`),
  report: (slug: string, kind: string) => request<ReportData>(`/app/${encodeURIComponent(slug)}/reports?kind=${encodeURIComponent(kind)}`),
  reportExportUrl: (slug: string, kind: string) => `${API_BASE}/app/${encodeURIComponent(slug)}/reports/export.csv?kind=${encodeURIComponent(kind)}`,
  notifications: (slug: string, unread = false) => request<{ notifications: NotificationItem[]; unread: number }>(`/app/${encodeURIComponent(slug)}/notifications${unread ? "?unread=true" : ""}`),
  markNotificationRead: (slug: string, id: string) => request(`/app/${encodeURIComponent(slug)}/notifications/${encodeURIComponent(id)}/read`, { method: "POST" }),
  notificationPreferences: (slug: string) => request<{ preferences: NotificationPreference[] }>(`/app/${encodeURIComponent(slug)}/notification-preferences`),
  setNotificationPreference: (slug: string, value: NotificationPreference) => request<NotificationPreference>(`/app/${encodeURIComponent(slug)}/notification-preferences`, { method: "PUT", body: JSON.stringify(value) }),
  notificationConsents: (slug: string) => request<{ consents: NotificationConsent[] }>(`/app/${encodeURIComponent(slug)}/notification-consents`),
  setNotificationConsent: (slug: string, channel: string, status: string, destination = "") => request<NotificationConsent>(`/app/${encodeURIComponent(slug)}/notification-consents`, { method: "PUT", body: JSON.stringify({ channel, status, destination }) }),
  announcements: (slug: string) => request<{ announcements: Announcement[] }>(`/app/${encodeURIComponent(slug)}/announcements`),
  markAnnouncementRead: (slug: string, id: string, acknowledge = false) => request(`/app/${encodeURIComponent(slug)}/announcements/${encodeURIComponent(id)}/read`, { method: "POST", body: JSON.stringify({ acknowledge }) }),
  feedbackTickets: (slug: string, status = "") => request<{ tickets: FeedbackTicket[] }>(`/app/${encodeURIComponent(slug)}/feedback${status ? `?status=${encodeURIComponent(status)}` : ""}`),
  createFeedbackTicket: (slug: string, value: { kind: string; subject: string; body: string; file_ids?: string[] }) => request<FeedbackTicket>(`/app/${encodeURIComponent(slug)}/feedback`, { method: "POST", body: JSON.stringify(value) }),
  feedbackTicket: (slug: string, id: string) => request<FeedbackTicket>(`/app/${encodeURIComponent(slug)}/feedback/${encodeURIComponent(id)}`),
  replyToFeedback: (slug: string, id: string, body: string) => request(`/app/${encodeURIComponent(slug)}/feedback/${encodeURIComponent(id)}/replies`, { method: "POST", body: JSON.stringify({ body }) }),
  communityRooms: () => request<{ rooms: CommunityRoom[] }>(`/community/rooms`),
  communityPosts: (roomId: string) => request<{ posts: CommunityPost[] }>(`/community/rooms/${encodeURIComponent(roomId)}/posts`),
  createCommunityPost: (roomId: string, body: string) => request<CommunityPost>(`/community/rooms/${encodeURIComponent(roomId)}/posts`, { method: "POST", body: JSON.stringify({ body }) }),
  communityReplies: (postId: string) => request<{ replies: CommunityReply[] }>(`/community/posts/${encodeURIComponent(postId)}/replies`),
  createCommunityReply: (postId: string, body: string) => request<CommunityReply>(`/community/posts/${encodeURIComponent(postId)}/replies`, { method: "POST", body: JSON.stringify({ body }) }),
  reportCommunityContent: (targetType: "post" | "reply", targetId: string, reason: string) => request(`/community/report`, { method: "POST", body: JSON.stringify({ target_type: targetType, target_id: targetId, reason }) }),
  communityProfile: () => request<CommunityProfile>(`/community/profile`),
  saveCommunityProfile: (value: { display_name: string; headline: string; bio: string; privacy_level: string }) => request<CommunityProfile>(`/community/profile`, { method: "PUT", body: JSON.stringify(value) }),
  // ── Phase 8 billing ──
  // plans is mounted outside /app/{slug} on purpose: a pricing page has to be
  // renderable before a store exists, so it is not tenant-resolved.
  plans: () => request<{ plans: Plan[] }>(`/plans`),
  billingStatus: (slug: string) => request<BillingStatus>(`/app/${encodeURIComponent(slug)}/billing`),
  billingPayments: (slug: string) => request<{ payments: PaymentRecord[] }>(`/app/${encodeURIComponent(slug)}/billing/payments`),
  startCheckout: (slug: string, planCode: string) => request<CheckoutSession>(`/app/${encodeURIComponent(slug)}/billing/checkout`, { method: "POST", body: JSON.stringify({ plan_code: planCode }) }),
  confirmCheckout: (slug: string, reference: string) => request<Subscription>(`/app/${encodeURIComponent(slug)}/billing/confirm`, { method: "POST", body: JSON.stringify({ reference }) }),
  cancelSubscription: (slug: string, reason: string) => request<{ cancelled: boolean; data_retention_end: string }>(`/app/${encodeURIComponent(slug)}/billing/cancel`, { method: "POST", body: JSON.stringify({ reason }) }),
  reactivateSubscription: (slug: string) => request<{ reactivated: boolean }>(`/app/${encodeURIComponent(slug)}/billing/reactivate`, { method: "POST" }),
  deadLetters: (slug: string) => request<{ dead_letters: DeadLetterEntry[] }>(`/app/${encodeURIComponent(slug)}/dead-letters`),
};
