import { FormEvent, useCallback, useEffect, useMemo, useState } from "react";

type Role = "super_admin" | "tenant_admin" | "member" | string;

type PageKey =
  | "dashboard"
  | "routes"
  | "connectors"
  | "tenantConfig"
  | "adminOverview"
  | "adminUsers"
  | "adminTenants"
  | "adminPlans"
  | "adminTLS"
  | "adminSystem";

interface User {
  username: string;
  role: Role;
  tenant_id?: string;
  status?: string;
}

interface Tenant {
  id: string;
  name: string;
  route_count?: number;
}

interface AuthMeResponse {
  user: User;
  tenants: Tenant[];
}

interface UsageGauge {
  used: number;
  limit: number;
  percent: number;
}

interface RouteView {
  tenant_id: string;
  id: string;
  connector_id?: string;
  max_rps?: number;
  connected?: boolean;
  public_url?: string;
  local_scheme?: string;
  local_host?: string;
  local_port?: number;
}

interface ConnectorView {
  id: string;
  tenant_id: string;
  name?: string;
  connected?: boolean;
  agent_id?: string;
  last_seen?: string;
}

interface DashboardPayload {
  plan?: { id?: string; name?: string };
  gauges?: {
    routes?: UsageGauge;
    connectors?: UsageGauge;
    traffic?: UsageGauge;
  };
  status?: {
    routes_active?: number;
    routes_degraded?: number;
    connectors_online?: number;
    connectors_offline?: number;
    blocked_requests_month?: number;
  };
  routes?: RouteView[];
  connectors?: ConnectorView[];
}

interface AdminStatsPayload {
  user_count?: number;
  tenant_count?: number;
  route_count?: number;
  connector_count?: number;
  active_connectors?: number;
  funnel_analytics?: {
    totals?: Record<string, number>;
  };
  storage_driver?: string;
  uptime_seconds?: number;
}

interface SystemStatusPayload {
  gateway?: {
    status?: string;
    listen_addr?: string;
    public_base_url?: string;
    uptime_seconds?: number;
  };
  storage?: {
    driver?: string;
    sqlite_path?: string;
    mode?: string;
  };
  runtime?: {
    active_sessions?: number;
    pending_requests?: number;
    max_pending_global?: number;
    queue_depth_total?: number;
    p50_latency_ms?: number;
    p95_latency_ms?: number;
    error_rate?: number;
  };
  tls?: {
    tls_listen_addr?: string;
    active_certificates?: number;
  };
}

interface Incident {
  id: string;
  severity: string;
  source: string;
  message: string;
  created_at: string;
}

interface Plan {
  id: string;
  name: string;
  description?: string;
  max_routes: number;
  max_connectors: number;
  max_rps: number;
  max_monthly_gb: number;
  tls_enabled: boolean;
  price_monthly_usd?: number;
  price_annual_usd?: number;
  public_order?: number;
}

interface TLSCertificate {
  id: string;
  hostname: string;
  active: boolean;
  expires_at?: string;
}

interface TenantEnvironment {
  scheme: string;
  host: string;
  default_port: number;
  variables: Record<string, string>;
}

interface ApiClient {
  <T>(path: string, init?: RequestInit): Promise<T>;
}

const PAGE_META: Record<PageKey, { title: string; subtitle: string }> = {
  dashboard: {
    title: "Tenant Dashboard",
    subtitle: "Plan gauges, statuses, and usage.",
  },
  routes: {
    title: "Route Management",
    subtitle: "Create and manage route forwarding rules.",
  },
  connectors: {
    title: "Connector Management",
    subtitle: "Pair hosts and monitor connector status.",
  },
  tenantConfig: {
    title: "Tenant Configuration",
    subtitle: "Manage environment defaults for local targets.",
  },
  adminOverview: {
    title: "Super Admin Overview",
    subtitle: "Global counts, usage and platform snapshot.",
  },
  adminUsers: {
    title: "User Administration",
    subtitle: "Create and update users across all tenants.",
  },
  adminTenants: {
    title: "Tenant Administration",
    subtitle: "Create tenants and assign subscription plans.",
  },
  adminPlans: {
    title: "Plan Management",
    subtitle: "Control quotas and traffic caps for all plans.",
  },
  adminTLS: {
    title: "TLS Certificates",
    subtitle: "Upload, activate, and remove TLS certificates.",
  },
  adminSystem: {
    title: "System Status",
    subtitle: "Health, incidents, queues, and runtime status.",
  },
};

const SUPER_NAV: Array<{ key: PageKey; label: string }> = [
  { key: "adminOverview", label: "Overview" },
  { key: "adminUsers", label: "Users" },
  { key: "adminTenants", label: "Tenants" },
  { key: "adminPlans", label: "Plans" },
  { key: "adminTLS", label: "TLS" },
  { key: "adminSystem", label: "System" },
  { key: "routes", label: "Routes" },
  { key: "connectors", label: "Connectors" },
  { key: "tenantConfig", label: "Tenant Config" },
];

const TENANT_NAV: Array<{ key: PageKey; label: string }> = [
  { key: "dashboard", label: "Dashboard" },
  { key: "routes", label: "Routes" },
  { key: "connectors", label: "Connectors" },
  { key: "tenantConfig", label: "Tenant Config" },
];

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null;
}

function toErrorMessage(error: unknown): string {
  if (error instanceof Error) {
    return error.message;
  }
  if (typeof error === "string") {
    return error;
  }
  return "Request failed";
}

function clampPercent(value: number): number {
  if (!Number.isFinite(value)) {
    return 0;
  }
  if (value < 0) {
    return 0;
  }
  if (value > 1) {
    return 1;
  }
  return value;
}

function formatPercent(value: number | undefined): string {
  const numeric = Number(value ?? 0);
  if (!Number.isFinite(numeric)) {
    return "0%";
  }
  return `${Math.round(numeric * 100)}%`;
}

function formatNumber(value: number | undefined): string {
  const numeric = Number(value ?? 0);
  if (!Number.isFinite(numeric)) {
    return "0";
  }
  if (Math.abs(numeric) >= 100) {
    return `${Math.round(numeric)}`;
  }
  return numeric.toFixed(2).replace(/\.00$/, "");
}

function formatDateTime(value: string | undefined): string {
  if (!value) {
    return "-";
  }
  const parsed = new Date(value);
  if (Number.isNaN(parsed.getTime())) {
    return value;
  }
  return parsed.toLocaleString();
}

function statusClass(value: string): "ok" | "fail" | "warn" {
  const normalized = value.toLowerCase();
  if (["online", "active", "enabled", "ok"].includes(normalized)) {
    return "ok";
  }
  if (["offline", "degraded", "disabled", "critical", "error"].includes(normalized)) {
    return "fail";
  }
  return "warn";
}

function Badge({ value }: { value: string }) {
  return <span className={`badge ${statusClass(value)}`}>{value}</span>;
}

function GaugeCard({
  title,
  gauge,
  subtitle,
}: {
  title: string;
  gauge: UsageGauge | undefined;
  subtitle: string;
}) {
  const used = Number(gauge?.used ?? 0);
  const limit = Number(gauge?.limit ?? 0);
  const percent = Math.round(clampPercent(Number(gauge?.percent ?? 0)) * 100);
  const ringStyle = {
    background: `conic-gradient(var(--ring-fill) ${percent}%, var(--ring-bg) ${percent}% 100%)`,
  };

  return (
    <article className="gauge-card">
      <h3>{title}</h3>
      <div className="gauge-ring" style={ringStyle}>
        <span>
          {formatNumber(used)} / {formatNumber(limit)}
        </span>
      </div>
      <p>{subtitle}</p>
    </article>
  );
}

function Section({
  title,
  children,
  actions,
}: {
  title: string;
  children: React.ReactNode;
  actions?: React.ReactNode;
}) {
  return (
    <section className="panel">
      <header className="panel-head">
        <h3>{title}</h3>
        {actions ? <div className="panel-actions">{actions}</div> : null}
      </header>
      {children}
    </section>
  );
}

function TenantDashboardPage({ api }: { api: ApiClient }) {
  const [data, setData] = useState<DashboardPayload | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");

  const load = useCallback(async () => {
    setLoading(true);
    setError("");
    try {
      const payload = await api<DashboardPayload>("/api/me/dashboard");
      setData(payload);
    } catch (err: unknown) {
      setError(toErrorMessage(err));
    } finally {
      setLoading(false);
    }
  }, [api]);

  useEffect(() => {
    void load();
  }, [load]);

  if (loading) {
    return <Section title="Dashboard">Loading...</Section>;
  }
  if (error) {
    return (
      <Section title="Dashboard" actions={<button onClick={() => void load()}>Retry</button>}>
        <p className="status error">{error}</p>
      </Section>
    );
  }

  const routes = data?.routes ?? [];
  const connectors = data?.connectors ?? [];

  return (
    <>
      <div className="gauge-row">
        <GaugeCard title="Routes" gauge={data?.gauges?.routes} subtitle="Used / plan limit" />
        <GaugeCard title="Connectors" gauge={data?.gauges?.connectors} subtitle="Used / plan limit" />
        <GaugeCard title="Traffic (GB)" gauge={data?.gauges?.traffic} subtitle="Monthly used / cap" />
      </div>

      <Section title="Live Status" actions={<button onClick={() => void load()}>Refresh</button>}>
        <div className="kv">
          <p>
            <strong>Plan</strong>
            <span>{data?.plan?.id ?? "free"}</span>
          </p>
          <p>
            <strong>Blocked Requests</strong>
            <span>{data?.status?.blocked_requests_month ?? 0}</span>
          </p>
          <p>
            <strong>Routes Active</strong>
            <span>{data?.status?.routes_active ?? 0}</span>
          </p>
          <p>
            <strong>Connectors Online</strong>
            <span>{data?.status?.connectors_online ?? 0}</span>
          </p>
        </div>
      </Section>

      <Section title="Routes">
        <table>
          <thead>
            <tr>
              <th>Tenant</th>
              <th>Route</th>
              <th>Status</th>
              <th>Public URL</th>
            </tr>
          </thead>
          <tbody>
            {routes.length === 0 ? (
              <tr>
                <td colSpan={4}>No routes yet.</td>
              </tr>
            ) : (
              routes.map((route) => (
                <tr key={`${route.tenant_id}:${route.id}`}>
                  <td>{route.tenant_id}</td>
                  <td>{route.id}</td>
                  <td>
                    <Badge value={route.connected ? "active" : "degraded"} />
                  </td>
                  <td className="code">{route.public_url ?? "-"}</td>
                </tr>
              ))
            )}
          </tbody>
        </table>
      </Section>

      <Section title="Connectors">
        <table>
          <thead>
            <tr>
              <th>ID</th>
              <th>Status</th>
              <th>Agent</th>
              <th>Last Seen</th>
            </tr>
          </thead>
          <tbody>
            {connectors.length === 0 ? (
              <tr>
                <td colSpan={4}>No connectors yet.</td>
              </tr>
            ) : (
              connectors.map((connector) => (
                <tr key={connector.id}>
                  <td>{connector.id}</td>
                  <td>
                    <Badge value={connector.connected ? "online" : "offline"} />
                  </td>
                  <td>{connector.agent_id ?? "-"}</td>
                  <td>{formatDateTime(connector.last_seen)}</td>
                </tr>
              ))
            )}
          </tbody>
        </table>
      </Section>
    </>
  );
}

function AdminOverviewPage({ api }: { api: ApiClient }) {
  const [stats, setStats] = useState<AdminStatsPayload | null>(null);
  const [status, setStatus] = useState<SystemStatusPayload | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");

  const load = useCallback(async () => {
    setLoading(true);
    setError("");
    try {
      const [statsPayload, statusPayload] = await Promise.all([
        api<AdminStatsPayload>("/api/admin/stats"),
        api<SystemStatusPayload>("/api/admin/system-status"),
      ]);
      setStats(statsPayload);
      setStatus(statusPayload);
    } catch (err: unknown) {
      setError(toErrorMessage(err));
    } finally {
      setLoading(false);
    }
  }, [api]);

  useEffect(() => {
    void load();
  }, [load]);

  if (loading) {
    return <Section title="Overview">Loading...</Section>;
  }
  if (error) {
    return (
      <Section title="Overview" actions={<button onClick={() => void load()}>Retry</button>}>
        <p className="status error">{error}</p>
      </Section>
    );
  }

  const routeCount = Number(stats?.route_count ?? 0);
  const connectorCount = Number(stats?.connector_count ?? 0);
  const tenantCount = Number(stats?.tenant_count ?? 0);
  const funnelTotals = stats?.funnel_analytics?.totals ?? {};

  return (
    <>
      <div className="gauge-row">
        <GaugeCard
          title="Routes"
          gauge={{ used: routeCount, limit: Math.max(routeCount, 1), percent: 1 }}
          subtitle="Global route count"
        />
        <GaugeCard
          title="Connectors"
          gauge={{ used: connectorCount, limit: Math.max(connectorCount, 1), percent: 1 }}
          subtitle="Global connector count"
        />
        <GaugeCard
          title="Tenants"
          gauge={{ used: tenantCount, limit: Math.max(tenantCount, 1), percent: 1 }}
          subtitle="Global tenant count"
        />
      </div>

      <Section title="Global Snapshot" actions={<button onClick={() => void load()}>Refresh</button>}>
        <div className="kv">
          <p>
            <strong>Users</strong>
            <span>{stats?.user_count ?? 0}</span>
          </p>
          <p>
            <strong>Active Connectors</strong>
            <span>{stats?.active_connectors ?? 0}</span>
          </p>
          <p>
            <strong>Storage Driver</strong>
            <span>{stats?.storage_driver ?? "memory"}</span>
          </p>
          <p>
            <strong>Uptime (s)</strong>
            <span>{stats?.uptime_seconds ?? 0}</span>
          </p>
          <p>
            <strong>Signup Success</strong>
            <span>{funnelTotals.signup_success ?? 0}</span>
          </p>
          <p>
            <strong>Download Clicks</strong>
            <span>{funnelTotals.download_click ?? 0}</span>
          </p>
        </div>
      </Section>

      <Section title="Runtime">
        <div className="kv">
          <p>
            <strong>Pending Requests</strong>
            <span>
              {status?.runtime?.pending_requests ?? 0} / {status?.runtime?.max_pending_global ?? 0}
            </span>
          </p>
          <p>
            <strong>Queue Depth</strong>
            <span>{status?.runtime?.queue_depth_total ?? 0}</span>
          </p>
          <p>
            <strong>Latency p50 / p95</strong>
            <span>
              {status?.runtime?.p50_latency_ms ?? 0}ms / {status?.runtime?.p95_latency_ms ?? 0}ms
            </span>
          </p>
          <p>
            <strong>Error Rate</strong>
            <span>{formatPercent(status?.runtime?.error_rate)}</span>
          </p>
        </div>
      </Section>
    </>
  );
}

function AdminUsersPage({ api }: { api: ApiClient }) {
  const [users, setUsers] = useState<User[]>([]);
  const [tenants, setTenants] = useState<Tenant[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [message, setMessage] = useState("");

  const load = useCallback(async () => {
    setLoading(true);
    setError("");
    try {
      const [usersPayload, tenantsPayload] = await Promise.all([
        api<{ users: User[] }>("/api/admin/users"),
        api<{ tenants: Tenant[] }>("/api/tenants"),
      ]);
      setUsers(usersPayload.users ?? []);
      setTenants(tenantsPayload.tenants ?? []);
    } catch (err: unknown) {
      setError(toErrorMessage(err));
    } finally {
      setLoading(false);
    }
  }, [api]);

  useEffect(() => {
    void load();
  }, [load]);

  const createUser = useCallback(
    async (event: FormEvent<HTMLFormElement>) => {
      event.preventDefault();
      setMessage("");
      const form = event.currentTarget;
      const formData = new FormData(form);
      try {
        await api<{ message: string }>("/api/admin/users", {
          method: "POST",
          body: JSON.stringify({
            username: String(formData.get("username") ?? ""),
            password: String(formData.get("password") ?? ""),
            role: String(formData.get("role") ?? "member"),
            tenant_id: String(formData.get("tenant_id") ?? ""),
          }),
        });
        form.reset();
        setMessage("User created.");
        await load();
      } catch (err: unknown) {
        setMessage(toErrorMessage(err));
      }
    },
    [api, load]
  );

  const toggleUserStatus = useCallback(
    async (user: User) => {
      setMessage("");
      try {
        await api<{ message: string }>(`/api/admin/users/${encodeURIComponent(user.username)}`, {
          method: "PATCH",
          body: JSON.stringify({
            status: user.status === "disabled" ? "active" : "disabled",
          }),
        });
        await load();
      } catch (err: unknown) {
        setMessage(toErrorMessage(err));
      }
    },
    [api, load]
  );

  return (
    <>
      <Section title="Create User">
        <form className="inline-form" onSubmit={createUser}>
          <input name="username" placeholder="username" required />
          <input name="password" type="password" placeholder="password" required />
          <select name="role" defaultValue="member">
            <option value="member">member</option>
            <option value="tenant_admin">tenant_admin</option>
            <option value="super_admin">super_admin</option>
          </select>
          <select name="tenant_id" defaultValue="">
            <option value="">No tenant (super admin)</option>
            {tenants.map((tenant) => (
              <option key={tenant.id} value={tenant.id}>
                {tenant.id}
              </option>
            ))}
          </select>
          <button type="submit">Create</button>
        </form>
        {message ? <p className="status">{message}</p> : null}
      </Section>

      <Section title="Users" actions={<button onClick={() => void load()}>Refresh</button>}>
        {loading ? <p>Loading...</p> : null}
        {error ? <p className="status error">{error}</p> : null}
        {!loading && !error ? (
          <table>
            <thead>
              <tr>
                <th>Username</th>
                <th>Role</th>
                <th>Tenant</th>
                <th>Status</th>
                <th>Action</th>
              </tr>
            </thead>
            <tbody>
              {users.length === 0 ? (
                <tr>
                  <td colSpan={5}>No users.</td>
                </tr>
              ) : (
                users.map((user) => (
                  <tr key={user.username}>
                    <td>{user.username}</td>
                    <td>{user.role}</td>
                    <td>{user.tenant_id || "-"}</td>
                    <td>
                      <Badge value={user.status || "active"} />
                    </td>
                    <td>
                      <button className="ghost" onClick={() => void toggleUserStatus(user)}>
                        {user.status === "disabled" ? "Enable" : "Disable"}
                      </button>
                    </td>
                  </tr>
                ))
              )}
            </tbody>
          </table>
        ) : null}
      </Section>
    </>
  );
}

function AdminTenantsPage({ api }: { api: ApiClient }) {
  const [tenants, setTenants] = useState<Tenant[]>([]);
  const [plans, setPlans] = useState<Plan[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [message, setMessage] = useState("");

  const load = useCallback(async () => {
    setLoading(true);
    setError("");
    try {
      const [tenantsPayload, plansPayload] = await Promise.all([
        api<{ tenants: Tenant[] }>("/api/tenants"),
        api<{ plans: Plan[] }>("/api/admin/plans"),
      ]);
      setTenants(tenantsPayload.tenants ?? []);
      setPlans(plansPayload.plans ?? []);
    } catch (err: unknown) {
      setError(toErrorMessage(err));
    } finally {
      setLoading(false);
    }
  }, [api]);

  useEffect(() => {
    void load();
  }, [load]);

  const createTenant = useCallback(
    async (event: FormEvent<HTMLFormElement>) => {
      event.preventDefault();
      setMessage("");
      const form = event.currentTarget;
      const formData = new FormData(form);
      try {
        await api<{ message: string }>("/api/tenants", {
          method: "POST",
          body: JSON.stringify({
            id: String(formData.get("id") ?? ""),
            name: String(formData.get("name") ?? ""),
          }),
        });
        form.reset();
        setMessage("Tenant created.");
        await load();
      } catch (err: unknown) {
        setMessage(toErrorMessage(err));
      }
    },
    [api, load]
  );

  const assignPlan = useCallback(
    async (tenantID: string, planID: string) => {
      setMessage("");
      try {
        await api<{ message: string }>(`/api/admin/tenants/${encodeURIComponent(tenantID)}/assign-plan`, {
          method: "POST",
          body: JSON.stringify({ plan_id: planID }),
        });
        setMessage(`Assigned plan ${planID} to ${tenantID}.`);
      } catch (err: unknown) {
        setMessage(toErrorMessage(err));
      }
    },
    [api]
  );

  return (
    <>
      <Section title="Create Tenant">
        <form className="inline-form" onSubmit={createTenant}>
          <input name="id" placeholder="tenant-id" required />
          <input name="name" placeholder="Tenant name" required />
          <button type="submit">Create</button>
        </form>
        {message ? <p className="status">{message}</p> : null}
      </Section>

      <Section title="Tenants" actions={<button onClick={() => void load()}>Refresh</button>}>
        {loading ? <p>Loading...</p> : null}
        {error ? <p className="status error">{error}</p> : null}
        {!loading && !error ? (
          <table>
            <thead>
              <tr>
                <th>ID</th>
                <th>Name</th>
                <th>Routes</th>
                <th>Assign Plan</th>
              </tr>
            </thead>
            <tbody>
              {tenants.length === 0 ? (
                <tr>
                  <td colSpan={4}>No tenants.</td>
                </tr>
              ) : (
                tenants.map((tenant) => (
                  <tr key={tenant.id}>
                    <td>{tenant.id}</td>
                    <td>{tenant.name}</td>
                    <td>{tenant.route_count ?? 0}</td>
                    <td>
                      <form
                        className="inline-form"
                        onSubmit={(event) => {
                          event.preventDefault();
                          const formData = new FormData(event.currentTarget);
                          void assignPlan(tenant.id, String(formData.get("plan_id") ?? ""));
                        }}
                      >
                        <select name="plan_id" defaultValue={plans[0]?.id ?? ""}>
                          {plans.map((plan) => (
                            <option key={plan.id} value={plan.id}>
                              {plan.id}
                            </option>
                          ))}
                        </select>
                        <button type="submit">Assign</button>
                      </form>
                    </td>
                  </tr>
                ))
              )}
            </tbody>
          </table>
        ) : null}
      </Section>
    </>
  );
}

function AdminPlansPage({ api }: { api: ApiClient }) {
  const [plans, setPlans] = useState<Plan[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [message, setMessage] = useState("");

  const load = useCallback(async () => {
    setLoading(true);
    setError("");
    try {
      const payload = await api<{ plans: Plan[] }>("/api/admin/plans");
      setPlans(payload.plans ?? []);
    } catch (err: unknown) {
      setError(toErrorMessage(err));
    } finally {
      setLoading(false);
    }
  }, [api]);

  useEffect(() => {
    void load();
  }, [load]);

  const createPlan = useCallback(
    async (event: FormEvent<HTMLFormElement>) => {
      event.preventDefault();
      setMessage("");
      const form = event.currentTarget;
      const formData = new FormData(form);
      try {
        await api<{ message: string }>("/api/admin/plans", {
          method: "POST",
          body: JSON.stringify({
            id: String(formData.get("id") ?? ""),
            name: String(formData.get("name") ?? ""),
            description: String(formData.get("description") ?? ""),
            max_routes: Number(formData.get("max_routes") ?? 0),
            max_connectors: Number(formData.get("max_connectors") ?? 0),
            max_rps: Number(formData.get("max_rps") ?? 0),
            max_monthly_gb: Number(formData.get("max_monthly_gb") ?? 0),
            tls_enabled: formData.get("tls_enabled") === "on",
            price_monthly_usd: Number(formData.get("price_monthly_usd") ?? 0),
            price_annual_usd: Number(formData.get("price_annual_usd") ?? 0),
            public_order: Number(formData.get("public_order") ?? 0),
          }),
        });
        form.reset();
        setMessage("Plan saved.");
        await load();
      } catch (err: unknown) {
        setMessage(toErrorMessage(err));
      }
    },
    [api, load]
  );

  return (
    <>
      <Section title="Create Plan">
        <form className="inline-form" onSubmit={createPlan}>
          <input name="id" placeholder="id" required />
          <input name="name" placeholder="name" required />
          <input name="description" placeholder="description" />
          <input name="max_routes" type="number" min={1} placeholder="max routes" required />
          <input
            name="max_connectors"
            type="number"
            min={1}
            placeholder="max connectors"
            required
          />
          <input name="max_rps" type="number" min={1} placeholder="max rps" required />
          <input
            name="max_monthly_gb"
            type="number"
            min={1}
            placeholder="max monthly gb"
            required
          />
          <input
            name="price_monthly_usd"
            type="number"
            min={0}
            step="0.01"
            placeholder="monthly price"
            required
          />
          <input
            name="price_annual_usd"
            type="number"
            min={0}
            step="0.01"
            placeholder="annual price"
            required
          />
          <input name="public_order" type="number" min={0} placeholder="public order" required />
          <label className="checkbox">
            <input type="checkbox" name="tls_enabled" />
            TLS enabled
          </label>
          <button type="submit">Save</button>
        </form>
        {message ? <p className="status">{message}</p> : null}
      </Section>

      <Section title="Plans" actions={<button onClick={() => void load()}>Refresh</button>}>
        {loading ? <p>Loading...</p> : null}
        {error ? <p className="status error">{error}</p> : null}
        {!loading && !error ? (
          <table>
            <thead>
              <tr>
                <th>ID</th>
                <th>Name</th>
                <th>Routes</th>
                <th>Connectors</th>
                <th>RPS</th>
                <th>Monthly GB</th>
                <th>Monthly USD</th>
                <th>Annual USD</th>
                <th>Order</th>
                <th>TLS</th>
              </tr>
            </thead>
            <tbody>
              {plans.length === 0 ? (
                <tr>
                  <td colSpan={10}>No plans.</td>
                </tr>
              ) : (
                plans.map((plan) => (
                  <tr key={plan.id}>
                    <td>{plan.id}</td>
                    <td>{plan.name}</td>
                    <td>{plan.max_routes}</td>
                    <td>{plan.max_connectors}</td>
                    <td>{plan.max_rps}</td>
                    <td>{plan.max_monthly_gb}</td>
                    <td>{formatNumber(plan.price_monthly_usd)}</td>
                    <td>{formatNumber(plan.price_annual_usd)}</td>
                    <td>{plan.public_order ?? 0}</td>
                    <td>
                      <Badge value={plan.tls_enabled ? "enabled" : "disabled"} />
                    </td>
                  </tr>
                ))
              )}
            </tbody>
          </table>
        ) : null}
      </Section>
    </>
  );
}

function AdminTLSPage({ api }: { api: ApiClient }) {
  const [certificates, setCertificates] = useState<TLSCertificate[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [message, setMessage] = useState("");

  const load = useCallback(async () => {
    setLoading(true);
    setError("");
    try {
      const payload = await api<{ certificates: TLSCertificate[] }>("/api/admin/tls/certificates");
      setCertificates(payload.certificates ?? []);
    } catch (err: unknown) {
      setError(toErrorMessage(err));
    } finally {
      setLoading(false);
    }
  }, [api]);

  useEffect(() => {
    void load();
  }, [load]);

  const upload = useCallback(
    async (event: FormEvent<HTMLFormElement>) => {
      event.preventDefault();
      setMessage("");
      const form = event.currentTarget;
      const formData = new FormData(form);
      try {
        await api<{ message: string }>("/api/admin/tls/certificates", {
          method: "POST",
          body: JSON.stringify({
            id: String(formData.get("id") ?? ""),
            hostname: String(formData.get("hostname") ?? ""),
            cert_pem: String(formData.get("cert_pem") ?? ""),
            key_pem: String(formData.get("key_pem") ?? ""),
            active: formData.get("active") === "on",
          }),
        });
        form.reset();
        setMessage("Certificate uploaded.");
        await load();
      } catch (err: unknown) {
        setMessage(toErrorMessage(err));
      }
    },
    [api, load]
  );

  const setActive = useCallback(
    async (cert: TLSCertificate, active: boolean) => {
      setMessage("");
      try {
        await api<{ message: string }>(`/api/admin/tls/certificates/${encodeURIComponent(cert.id)}`, {
          method: "PATCH",
          body: JSON.stringify({ active }),
        });
        await load();
      } catch (err: unknown) {
        setMessage(toErrorMessage(err));
      }
    },
    [api, load]
  );

  const remove = useCallback(
    async (cert: TLSCertificate) => {
      setMessage("");
      try {
        await api<null>(`/api/admin/tls/certificates/${encodeURIComponent(cert.id)}`, {
          method: "DELETE",
        });
        await load();
      } catch (err: unknown) {
        setMessage(toErrorMessage(err));
      }
    },
    [api, load]
  );

  return (
    <>
      <Section title="Upload Certificate">
        <form className="stack" onSubmit={upload}>
          <div className="inline-form">
            <input name="id" placeholder="id" required />
            <input name="hostname" placeholder="example.com" required />
            <label className="checkbox">
              <input name="active" type="checkbox" />
              Active
            </label>
          </div>
          <label>
            Certificate PEM
            <textarea name="cert_pem" rows={6} required />
          </label>
          <label>
            Private Key PEM
            <textarea name="key_pem" rows={6} required />
          </label>
          <button type="submit">Upload</button>
        </form>
        {message ? <p className="status">{message}</p> : null}
      </Section>

      <Section title="Certificates" actions={<button onClick={() => void load()}>Refresh</button>}>
        {loading ? <p>Loading...</p> : null}
        {error ? <p className="status error">{error}</p> : null}
        {!loading && !error ? (
          <table>
            <thead>
              <tr>
                <th>ID</th>
                <th>Hostname</th>
                <th>Status</th>
                <th>Expires</th>
                <th>Actions</th>
              </tr>
            </thead>
            <tbody>
              {certificates.length === 0 ? (
                <tr>
                  <td colSpan={5}>No certificates.</td>
                </tr>
              ) : (
                certificates.map((cert) => (
                  <tr key={cert.id}>
                    <td>{cert.id}</td>
                    <td>{cert.hostname}</td>
                    <td>
                      <Badge value={cert.active ? "active" : "inactive"} />
                    </td>
                    <td>{cert.expires_at ? cert.expires_at.slice(0, 10) : "-"}</td>
                    <td>
                      <div className="actions">
                        <button className="ghost" onClick={() => void setActive(cert, !cert.active)}>
                          {cert.active ? "Deactivate" : "Activate"}
                        </button>
                        <button className="ghost danger" onClick={() => void remove(cert)}>
                          Delete
                        </button>
                      </div>
                    </td>
                  </tr>
                ))
              )}
            </tbody>
          </table>
        ) : null}
      </Section>
    </>
  );
}

function AdminSystemPage({ api }: { api: ApiClient }) {
  const [status, setStatus] = useState<SystemStatusPayload | null>(null);
  const [incidents, setIncidents] = useState<Incident[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");

  const load = useCallback(async () => {
    setLoading(true);
    setError("");
    try {
      const [statusPayload, incidentPayload] = await Promise.all([
        api<SystemStatusPayload>("/api/admin/system-status"),
        api<{ incidents: Incident[] }>("/api/admin/incidents?limit=100"),
      ]);
      setStatus(statusPayload);
      setIncidents(incidentPayload.incidents ?? []);
    } catch (err: unknown) {
      setError(toErrorMessage(err));
    } finally {
      setLoading(false);
    }
  }, [api]);

  useEffect(() => {
    void load();
  }, [load]);

  return (
    <>
      <Section title="System Status" actions={<button onClick={() => void load()}>Refresh</button>}>
        {loading ? <p>Loading...</p> : null}
        {error ? <p className="status error">{error}</p> : null}
        {!loading && !error ? (
          <div className="kv">
            <p>
              <strong>Gateway</strong>
              <span>{status?.gateway?.status ?? "unknown"}</span>
            </p>
            <p>
              <strong>Storage</strong>
              <span>{status?.storage?.driver ?? "memory"}</span>
            </p>
            <p>
              <strong>Active Sessions</strong>
              <span>{status?.runtime?.active_sessions ?? 0}</span>
            </p>
            <p>
              <strong>Pending Requests</strong>
              <span>{status?.runtime?.pending_requests ?? 0}</span>
            </p>
            <p>
              <strong>Latency p50/p95</strong>
              <span>
                {status?.runtime?.p50_latency_ms ?? 0}ms / {status?.runtime?.p95_latency_ms ?? 0}ms
              </span>
            </p>
            <p>
              <strong>Error Rate</strong>
              <span>{formatPercent(status?.runtime?.error_rate)}</span>
            </p>
          </div>
        ) : null}
      </Section>

      <Section title="Incidents">
        <table>
          <thead>
            <tr>
              <th>Severity</th>
              <th>Source</th>
              <th>Message</th>
              <th>Created</th>
            </tr>
          </thead>
          <tbody>
            {incidents.length === 0 ? (
              <tr>
                <td colSpan={4}>No incidents.</td>
              </tr>
            ) : (
              incidents.map((incident) => (
                <tr key={incident.id}>
                  <td>
                    <Badge value={incident.severity} />
                  </td>
                  <td>{incident.source}</td>
                  <td>{incident.message}</td>
                  <td>{formatDateTime(incident.created_at)}</td>
                </tr>
              ))
            )}
          </tbody>
        </table>
      </Section>
    </>
  );
}

function RoutesPage({ api, me }: { api: ApiClient; me: AuthMeResponse }) {
  const isSuper = me.user.role === "super_admin";
  const [routes, setRoutes] = useState<RouteView[]>([]);
  const [connectors, setConnectors] = useState<ConnectorView[]>([]);
  const [tenants, setTenants] = useState<Tenant[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [message, setMessage] = useState("");

  const load = useCallback(async () => {
    setLoading(true);
    setError("");
    try {
      const [routesPayload, connectorsPayload, tenantsPayload] = await Promise.all([
        api<{ routes: RouteView[] }>("/api/me/routes"),
        api<{ connectors: ConnectorView[] }>("/api/me/connectors"),
        api<{ tenants: Tenant[] }>("/api/tenants"),
      ]);
      setRoutes(routesPayload.routes ?? []);
      setConnectors(connectorsPayload.connectors ?? []);
      setTenants(tenantsPayload.tenants ?? []);
    } catch (err: unknown) {
      setError(toErrorMessage(err));
    } finally {
      setLoading(false);
    }
  }, [api]);

  useEffect(() => {
    void load();
  }, [load]);

  const submitRoute = useCallback(
    async (event: FormEvent<HTMLFormElement>) => {
      event.preventDefault();
      setMessage("");
      const form = event.currentTarget;
      const formData = new FormData(form);
      const tenantID = isSuper
        ? String(formData.get("tenant_id") ?? "")
        : me.user.tenant_id || tenants[0]?.id || "default";
      try {
        await api<{ message: string }>(`/api/tenants/${encodeURIComponent(tenantID)}/routes`, {
          method: "POST",
          body: JSON.stringify({
            id: String(formData.get("id") ?? ""),
            target: String(formData.get("target") ?? ""),
            token: String(formData.get("token") ?? ""),
            max_rps: Number(formData.get("max_rps") ?? 0),
            connector_id: String(formData.get("connector_id") ?? ""),
            local_scheme: String(formData.get("local_scheme") ?? "http"),
            local_host: String(formData.get("local_host") ?? "127.0.0.1"),
            local_port: Number(formData.get("local_port") ?? 0),
            local_base_path: String(formData.get("local_base_path") ?? ""),
          }),
        });
        setMessage("Route saved.");
        form.reset();
        await load();
      } catch (err: unknown) {
        setMessage(toErrorMessage(err));
      }
    },
    [api, isSuper, load, me.user.tenant_id, tenants]
  );

  const deleteRoute = useCallback(
    async (route: RouteView) => {
      setMessage("");
      try {
        await api<null>(
          `/api/tenants/${encodeURIComponent(route.tenant_id)}/routes/${encodeURIComponent(route.id)}`,
          {
            method: "DELETE",
          }
        );
        await load();
      } catch (err: unknown) {
        setMessage(toErrorMessage(err));
      }
    },
    [api, load]
  );

  const defaultTenant = me.user.tenant_id || tenants[0]?.id || "default";

  return (
    <>
      <Section title="Create Route">
        <form className="grid cols-2" onSubmit={submitRoute}>
          <label>
            Tenant
            <select
              name="tenant_id"
              defaultValue={defaultTenant}
              disabled={!isSuper}
              required={isSuper}
            >
              {tenants.map((tenant) => (
                <option key={tenant.id} value={tenant.id}>
                  {tenant.id}
                </option>
              ))}
            </select>
          </label>
          <label>
            Route ID
            <input name="id" placeholder="api" required />
          </label>
          <label>
            Direct Target URL
            <input name="target" placeholder="http://127.0.0.1:3000" />
          </label>
          <label>
            Connector
            <select name="connector_id" defaultValue="">
              <option value="">Direct target</option>
              {connectors.map((connector) => (
                <option key={connector.id} value={connector.id}>
                  {connector.id}
                </option>
              ))}
            </select>
          </label>
          <label>
            Local Scheme
            <select name="local_scheme" defaultValue="http">
              <option value="http">http</option>
              <option value="https">https</option>
            </select>
          </label>
          <label>
            Local Host
            <input name="local_host" defaultValue="127.0.0.1" />
          </label>
          <label>
            Local Port
            <input name="local_port" type="number" min={1} max={65535} placeholder="3000" />
          </label>
          <label>
            Local Base Path
            <input name="local_base_path" placeholder="/" />
          </label>
          <label>
            Access Token
            <input name="token" placeholder="optional" />
          </label>
          <label>
            Route Max RPS
            <input name="max_rps" type="number" min={0} step="0.1" placeholder="0 = fair share" />
          </label>
          <div>
            <button type="submit">Save Route</button>
          </div>
        </form>
        {message ? <p className="status">{message}</p> : null}
      </Section>

      <Section title="Routes" actions={<button onClick={() => void load()}>Refresh</button>}>
        {loading ? <p>Loading...</p> : null}
        {error ? <p className="status error">{error}</p> : null}
        {!loading && !error ? (
          <table>
            <thead>
              <tr>
                <th>Tenant</th>
                <th>ID</th>
                <th>Connector</th>
                <th>Max RPS</th>
                <th>Status</th>
                <th>Public URL</th>
                <th>Action</th>
              </tr>
            </thead>
            <tbody>
              {routes.length === 0 ? (
                <tr>
                  <td colSpan={7}>No routes.</td>
                </tr>
              ) : (
                routes.map((route) => (
                  <tr key={`${route.tenant_id}:${route.id}`}>
                    <td>{route.tenant_id}</td>
                    <td>{route.id}</td>
                    <td>{route.connector_id || "-"}</td>
                    <td>{route.max_rps && route.max_rps > 0 ? route.max_rps : "-"}</td>
                    <td>
                      <Badge value={route.connected ? "active" : "offline"} />
                    </td>
                    <td className="code">{route.public_url ?? "-"}</td>
                    <td>
                      <button className="ghost danger" onClick={() => void deleteRoute(route)}>
                        Delete
                      </button>
                    </td>
                  </tr>
                ))
              )}
            </tbody>
          </table>
        ) : null}
      </Section>
    </>
  );
}

function ConnectorsPage({ api, me }: { api: ApiClient; me: AuthMeResponse }) {
  const isSuper = me.user.role === "super_admin";
  const [connectors, setConnectors] = useState<ConnectorView[]>([]);
  const [tenants, setTenants] = useState<Tenant[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [message, setMessage] = useState("");
  const [output, setOutput] = useState("");

  const load = useCallback(async () => {
    setLoading(true);
    setError("");
    try {
      const [connectorsPayload, tenantsPayload] = await Promise.all([
        api<{ connectors: ConnectorView[] }>("/api/me/connectors"),
        api<{ tenants: Tenant[] }>("/api/tenants"),
      ]);
      setConnectors(connectorsPayload.connectors ?? []);
      setTenants(tenantsPayload.tenants ?? []);
    } catch (err: unknown) {
      setError(toErrorMessage(err));
    } finally {
      setLoading(false);
    }
  }, [api]);

  useEffect(() => {
    void load();
  }, [load]);

  const createConnector = useCallback(
    async (event: FormEvent<HTMLFormElement>) => {
      event.preventDefault();
      setMessage("");
      const form = event.currentTarget;
      const formData = new FormData(form);
      const tenantID = isSuper
        ? String(formData.get("tenant_id") ?? "")
        : me.user.tenant_id || tenants[0]?.id || "default";
      try {
        await api<{ message: string }>("/api/connectors", {
          method: "POST",
          body: JSON.stringify({
            tenant_id: tenantID,
            id: String(formData.get("id") ?? ""),
            name: String(formData.get("name") ?? ""),
          }),
        });
        form.reset();
        setMessage("Connector created.");
        await load();
      } catch (err: unknown) {
        setMessage(toErrorMessage(err));
      }
    },
    [api, isSuper, load, me.user.tenant_id, tenants]
  );

  const pair = useCallback(
    async (id: string) => {
      setMessage("");
      try {
        const payload = await api<{ command?: string }>(`/api/connectors/${encodeURIComponent(id)}/pair`, {
          method: "POST",
        });
        setOutput(payload.command ?? "");
      } catch (err: unknown) {
        setMessage(toErrorMessage(err));
      }
    },
    [api]
  );

  const rotate = useCallback(
    async (id: string) => {
      setMessage("");
      try {
        const payload = await api<{ connector_secret?: string }>(
          `/api/connectors/${encodeURIComponent(id)}/rotate`,
          {
            method: "POST",
          }
        );
        setOutput(`connector_secret=${payload.connector_secret ?? ""}`);
      } catch (err: unknown) {
        setMessage(toErrorMessage(err));
      }
    },
    [api]
  );

  const remove = useCallback(
    async (id: string) => {
      setMessage("");
      try {
        await api<null>(`/api/connectors/${encodeURIComponent(id)}`, { method: "DELETE" });
        await load();
      } catch (err: unknown) {
        setMessage(toErrorMessage(err));
      }
    },
    [api, load]
  );

  return (
    <>
      <Section title="Create Connector">
        <form className="inline-form" onSubmit={createConnector}>
          {isSuper ? (
            <select name="tenant_id" defaultValue={me.user.tenant_id || tenants[0]?.id || "default"}>
              {tenants.map((tenant) => (
                <option key={tenant.id} value={tenant.id}>
                  {tenant.id}
                </option>
              ))}
            </select>
          ) : null}
          <input name="id" placeholder="connector-id" required />
          <input name="name" placeholder="Friendly name" required />
          <button type="submit">Create</button>
        </form>
        {message ? <p className="status">{message}</p> : null}
        {output ? <p className="code output">{output}</p> : null}
      </Section>

      <Section title="Connectors" actions={<button onClick={() => void load()}>Refresh</button>}>
        {loading ? <p>Loading...</p> : null}
        {error ? <p className="status error">{error}</p> : null}
        {!loading && !error ? (
          <table>
            <thead>
              <tr>
                <th>ID</th>
                <th>Tenant</th>
                <th>Status</th>
                <th>Agent</th>
                <th>Actions</th>
              </tr>
            </thead>
            <tbody>
              {connectors.length === 0 ? (
                <tr>
                  <td colSpan={5}>No connectors.</td>
                </tr>
              ) : (
                connectors.map((connector) => (
                  <tr key={connector.id}>
                    <td>{connector.id}</td>
                    <td>{connector.tenant_id}</td>
                    <td>
                      <Badge value={connector.connected ? "online" : "offline"} />
                    </td>
                    <td>{connector.agent_id || "-"}</td>
                    <td>
                      <div className="actions">
                        <button className="ghost" onClick={() => void pair(connector.id)}>
                          Pair
                        </button>
                        <button className="ghost" onClick={() => void rotate(connector.id)}>
                          Rotate
                        </button>
                        <button className="ghost danger" onClick={() => void remove(connector.id)}>
                          Delete
                        </button>
                      </div>
                    </td>
                  </tr>
                ))
              )}
            </tbody>
          </table>
        ) : null}
      </Section>
    </>
  );
}

function TenantConfigPage({ api, me }: { api: ApiClient; me: AuthMeResponse }) {
  const isSuper = me.user.role === "super_admin";
  const [tenants, setTenants] = useState<Tenant[]>([]);
  const [selectedTenant, setSelectedTenant] = useState("");
  const [environment, setEnvironment] = useState<TenantEnvironment>({
    scheme: "http",
    host: "127.0.0.1",
    default_port: 3000,
    variables: {},
  });
  const [variablesText, setVariablesText] = useState("{}");
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [message, setMessage] = useState("");

  const loadTenants = useCallback(async () => {
    setLoading(true);
    setError("");
    try {
      const payload = await api<{ tenants: Tenant[] }>("/api/tenants");
      const items = payload.tenants ?? [];
      setTenants(items);
      if (items.length === 0) {
        setSelectedTenant("");
      } else if (isSuper) {
        setSelectedTenant((current) => current || items[0].id);
      } else {
        setSelectedTenant(me.user.tenant_id || items[0].id);
      }
    } catch (err: unknown) {
      setError(toErrorMessage(err));
    } finally {
      setLoading(false);
    }
  }, [api, isSuper, me.user.tenant_id]);

  useEffect(() => {
    void loadTenants();
  }, [loadTenants]);

  const loadEnvironment = useCallback(async () => {
    if (!selectedTenant) {
      return;
    }
    setError("");
    setMessage("");
    try {
      const payload = await api<{ environment: TenantEnvironment }>(
        `/api/tenants/${encodeURIComponent(selectedTenant)}/environment`
      );
      const env = payload.environment;
      setEnvironment(env);
      setVariablesText(JSON.stringify(env.variables ?? {}, null, 2));
    } catch (err: unknown) {
      const messageText = toErrorMessage(err);
      setError(messageText);
    }
  }, [api, selectedTenant]);

  useEffect(() => {
    void loadEnvironment();
  }, [loadEnvironment]);

  const saveEnvironment = useCallback(
    async (event: FormEvent<HTMLFormElement>) => {
      event.preventDefault();
      if (!selectedTenant) {
        return;
      }
      setMessage("");
      setError("");
      let variables: Record<string, string>;
      try {
        const parsed = JSON.parse(variablesText) as unknown;
        if (!isRecord(parsed)) {
          throw new Error("Variables must be a JSON object.");
        }
        variables = {};
        for (const [key, value] of Object.entries(parsed)) {
          variables[String(key)] = String(value);
        }
      } catch (err: unknown) {
        setError(toErrorMessage(err));
        return;
      }

      try {
        await api<{ message: string }>(`/api/tenants/${encodeURIComponent(selectedTenant)}/environment`, {
          method: "PUT",
          body: JSON.stringify({
            scheme: environment.scheme,
            host: environment.host,
            default_port: environment.default_port,
            variables,
          }),
        });
        setMessage("Environment saved.");
      } catch (err: unknown) {
        setError(toErrorMessage(err));
      }
    },
    [api, environment, selectedTenant, variablesText]
  );

  return (
    <Section
      title="Tenant Environment"
      actions={<button onClick={() => void loadEnvironment()}>Refresh</button>}
    >
      {loading ? <p>Loading...</p> : null}
      {error ? <p className="status error">{error}</p> : null}
      {!loading && selectedTenant ? (
        <form className="grid cols-2" onSubmit={saveEnvironment}>
          {isSuper ? (
            <label>
              Tenant
              <select
                value={selectedTenant}
                onChange={(event) => setSelectedTenant(event.target.value)}
              >
                {tenants.map((tenant) => (
                  <option key={tenant.id} value={tenant.id}>
                    {tenant.id}
                  </option>
                ))}
              </select>
            </label>
          ) : null}
          <label>
            Scheme
            <input
              value={environment.scheme}
              onChange={(event) =>
                setEnvironment((current) => ({ ...current, scheme: event.target.value }))
              }
            />
          </label>
          <label>
            Host
            <input
              value={environment.host}
              onChange={(event) =>
                setEnvironment((current) => ({ ...current, host: event.target.value }))
              }
            />
          </label>
          <label>
            Default Port
            <input
              type="number"
              value={environment.default_port}
              onChange={(event) =>
                setEnvironment((current) => ({
                  ...current,
                  default_port: Number(event.target.value || 0),
                }))
              }
            />
          </label>
          <label className="wide">
            Variables (JSON)
            <textarea
              rows={8}
              value={variablesText}
              onChange={(event) => setVariablesText(event.target.value)}
            />
          </label>
          <div>
            <button type="submit">Save</button>
          </div>
        </form>
      ) : null}
      {message ? <p className="status">{message}</p> : null}
    </Section>
  );
}

export default function WorkspaceApp({
  me,
  api,
  onLogout,
}: {
  me: AuthMeResponse;
  api: ApiClient;
  onLogout: () => Promise<void>;
}) {
  const isSuper = me.user.role === "super_admin";
  const navItems = isSuper ? SUPER_NAV : TENANT_NAV;
  const [page, setPage] = useState<PageKey>(isSuper ? "adminOverview" : "dashboard");

  useEffect(() => {
    const allowed = new Set(navItems.map((item) => item.key));
    if (!allowed.has(page)) {
      setPage(navItems[0].key);
    }
  }, [navItems, page]);

  const content = useMemo(() => {
    if (page === "dashboard") {
      return <TenantDashboardPage api={api} />;
    }
    if (page === "routes") {
      return <RoutesPage api={api} me={me} />;
    }
    if (page === "connectors") {
      return <ConnectorsPage api={api} me={me} />;
    }
    if (page === "tenantConfig") {
      return <TenantConfigPage api={api} me={me} />;
    }
    if (page === "adminOverview") {
      return <AdminOverviewPage api={api} />;
    }
    if (page === "adminUsers") {
      return <AdminUsersPage api={api} />;
    }
    if (page === "adminTenants") {
      return <AdminTenantsPage api={api} />;
    }
    if (page === "adminPlans") {
      return <AdminPlansPage api={api} />;
    }
    if (page === "adminTLS") {
      return <AdminTLSPage api={api} />;
    }
    if (page === "adminSystem") {
      return <AdminSystemPage api={api} />;
    }
    return <Section title="Not Found">Page not found.</Section>;
  }, [api, me, page]);

  const meta = PAGE_META[page];

  return (
    <main className="workspace-shell">
      <aside className="sidebar">
        <div className="brand">
          <h1>Proxer</h1>
          <p>
            {me.user.username} · {me.user.role}
          </p>
        </div>
        <nav className="nav">
          {navItems.map((item) => (
            <button
              key={item.key}
              className={item.key === page ? "active" : ""}
              onClick={() => setPage(item.key)}
            >
              {item.label}
            </button>
          ))}
        </nav>
        <button className="ghost danger" onClick={() => void onLogout()}>
          Logout
        </button>
      </aside>

      <section className="workspace-content">
        <header className="topbar">
          <h2>{meta.title}</h2>
          <p>{meta.subtitle}</p>
        </header>
        <div className="page-content">{content}</div>
      </section>
    </main>
  );
}
