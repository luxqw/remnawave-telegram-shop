// Mirrors the Go DTOs in internal/webapp/*.go.

export interface Page<T> {
  items: T[];
  total: number;
  page: number;
  limit: number;
}

export interface Customer {
  id: number;
  telegramId: number;
  expireAt: string | null;
  createdAt: string;
  subscriptionLink: string | null;
  language: string;
  isTrial: boolean;
  username?: string;
}

export interface RemnawaveUser {
  uuid: string;
  status: string;
  trafficLimitGb: number;
  trafficLimitStrategy: string;
  expireAt: string;
  subscriptionUrl: string;
}

export interface UserDetail {
  customer: Customer;
  remnawave?: RemnawaveUser;
  remnawaveError?: string;
}

export interface Purchase {
  id: number;
  amount: number;
  currency: string;
  month: number;
  status: string;
  invoiceType: string;
  createdAt: string;
  paidAt: string | null;
  expireAt: string | null;
  telegramId?: number;
  username?: string;
}

export interface AuditLogEntry {
  id: number;
  createdAt: string;
  adminTelegramId: number;
  adminUsername?: string;
  action: string;
  targetTelegramId: number;
  targetUsername?: string;
  paramInt: number | null;
  paramText: string | null;
  outcome: string;
  errorMessage: string | null;
  source: string;
}

export interface Referral {
  id: number;
  referrerId: number;
  referrerUsername?: string;
  refereeId: number;
  refereeUsername?: string;
  usedAt: string;
  bonusGranted: boolean;
}

export interface WebhookInboxEntry {
  id: number;
  eventType: string;
  status: string;
  attempts: number;
  errorMsg: string | null;
  createdAt: string;
  processedAt: string | null;
}

export interface WebhookInboxDetail extends WebhookInboxEntry {
  payload: string;
}

export interface DashboardStats {
  total: number;
  activePaid: number;
  activeTrial: number;
  expired: number;
  noSub: number;
}

export interface DayPoint {
  day: string;
  value: number;
  count: number;
}

export interface DashboardReferrals {
  total: number;
  granted: number;
  conversionPercent: number;
}

export interface ActivityEvent {
  type: "signup" | "purchase" | "referral_bonus" | "admin_action" | "notification";
  timestamp: string;
  actorId: number | null;
  actorUsername?: string;
  targetId: number;
  targetUsername?: string;
  detail: string;
}

export interface DashboardHealth {
  status: string;
  db: string;
  remnawave: string;
  version: string;
  commit: string;
  buildDate: string;
  time: string;
}

export interface BroadcastProgress {
  jobId: string;
  segment: string;
  total: number;
  sent: number;
  failed: number;
  unreachable: number;
  otherFailed: number;
  done: boolean;
  startedAt: string;
  finishedAt: string | null;
}

export interface FixStrategyPreview {
  totalCustomers: number;
  strategyCounts: Record<string, number>;
  notFound: number;
  targetStrategy: string;
  willUpdate: number;
}

export interface FixStrategyJobStatus {
  jobId: string;
  processed: number;
  total: number;
  updated: number;
  errored: number;
  done: boolean;
  result?: {
    total: number;
    updated: number;
    skipped: number;
    notFound: number;
    errors: string[];
  };
  error?: string;
}
