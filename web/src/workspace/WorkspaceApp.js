import { jsx as _jsx, jsxs as _jsxs, Fragment as _Fragment } from "react/jsx-runtime";
import { useCallback, useEffect, useMemo, useState } from "react";
const PAGE_META = {
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
const SUPER_NAV = [
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
const TENANT_NAV = [
    { key: "dashboard", label: "Dashboard" },
    { key: "routes", label: "Routes" },
    { key: "connectors", label: "Connectors" },
    { key: "tenantConfig", label: "Tenant Config" },
];
function isRecord(value) {
    return typeof value === "object" && value !== null;
}
function toErrorMessage(error) {
    if (error instanceof Error) {
        return error.message;
    }
    if (typeof error === "string") {
        return error;
    }
    return "Request failed";
}
function clampPercent(value) {
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
function formatPercent(value) {
    const numeric = Number(value ?? 0);
    if (!Number.isFinite(numeric)) {
        return "0%";
    }
    return `${Math.round(numeric * 100)}%`;
}
function formatNumber(value) {
    const numeric = Number(value ?? 0);
    if (!Number.isFinite(numeric)) {
        return "0";
    }
    if (Math.abs(numeric) >= 100) {
        return `${Math.round(numeric)}`;
    }
    return numeric.toFixed(2).replace(/\.00$/, "");
}
function formatDateTime(value) {
    if (!value) {
        return "-";
    }
    const parsed = new Date(value);
    if (Number.isNaN(parsed.getTime())) {
        return value;
    }
    return parsed.toLocaleString();
}
function statusClass(value) {
    const normalized = value.toLowerCase();
    if (["online", "active", "enabled", "ok"].includes(normalized)) {
        return "ok";
    }
    if (["offline", "degraded", "disabled", "critical", "error"].includes(normalized)) {
        return "fail";
    }
    return "warn";
}
function Badge({ value }) {
    return _jsx("span", { className: `badge ${statusClass(value)}`, children: value });
}
function GaugeCard({ title, gauge, subtitle, }) {
    const used = Number(gauge?.used ?? 0);
    const limit = Number(gauge?.limit ?? 0);
    const percent = Math.round(clampPercent(Number(gauge?.percent ?? 0)) * 100);
    const ringStyle = {
        background: `conic-gradient(var(--ring-fill) ${percent}%, var(--ring-bg) ${percent}% 100%)`,
    };
    return (_jsxs("article", { className: "gauge-card", children: [_jsx("h3", { children: title }), _jsx("div", { className: "gauge-ring", style: ringStyle, children: _jsxs("span", { children: [formatNumber(used), " / ", formatNumber(limit)] }) }), _jsx("p", { children: subtitle })] }));
}
function Section({ title, children, actions, }) {
    return (_jsxs("section", { className: "panel", children: [_jsxs("header", { className: "panel-head", children: [_jsx("h3", { children: title }), actions ? _jsx("div", { className: "panel-actions", children: actions }) : null] }), children] }));
}
function TenantDashboardPage({ api }) {
    const [data, setData] = useState(null);
    const [loading, setLoading] = useState(true);
    const [error, setError] = useState("");
    const load = useCallback(async () => {
        setLoading(true);
        setError("");
        try {
            const payload = await api("/api/me/dashboard");
            setData(payload);
        }
        catch (err) {
            setError(toErrorMessage(err));
        }
        finally {
            setLoading(false);
        }
    }, [api]);
    useEffect(() => {
        void load();
    }, [load]);
    if (loading) {
        return _jsx(Section, { title: "Dashboard", children: "Loading..." });
    }
    if (error) {
        return (_jsx(Section, { title: "Dashboard", actions: _jsx("button", { onClick: () => void load(), children: "Retry" }), children: _jsx("p", { className: "status error", children: error }) }));
    }
    const routes = data?.routes ?? [];
    const connectors = data?.connectors ?? [];
    return (_jsxs(_Fragment, { children: [_jsxs("div", { className: "gauge-row", children: [_jsx(GaugeCard, { title: "Routes", gauge: data?.gauges?.routes, subtitle: "Used / plan limit" }), _jsx(GaugeCard, { title: "Connectors", gauge: data?.gauges?.connectors, subtitle: "Used / plan limit" }), _jsx(GaugeCard, { title: "Traffic (GB)", gauge: data?.gauges?.traffic, subtitle: "Monthly used / cap" })] }), _jsx(Section, { title: "Live Status", actions: _jsx("button", { onClick: () => void load(), children: "Refresh" }), children: _jsxs("div", { className: "kv", children: [_jsxs("p", { children: [_jsx("strong", { children: "Plan" }), _jsx("span", { children: data?.plan?.id ?? "free" })] }), _jsxs("p", { children: [_jsx("strong", { children: "Blocked Requests" }), _jsx("span", { children: data?.status?.blocked_requests_month ?? 0 })] }), _jsxs("p", { children: [_jsx("strong", { children: "Routes Active" }), _jsx("span", { children: data?.status?.routes_active ?? 0 })] }), _jsxs("p", { children: [_jsx("strong", { children: "Connectors Online" }), _jsx("span", { children: data?.status?.connectors_online ?? 0 })] })] }) }), _jsx(Section, { title: "Routes", children: _jsxs("table", { children: [_jsx("thead", { children: _jsxs("tr", { children: [_jsx("th", { children: "Tenant" }), _jsx("th", { children: "Route" }), _jsx("th", { children: "Status" }), _jsx("th", { children: "Public URL" })] }) }), _jsx("tbody", { children: routes.length === 0 ? (_jsx("tr", { children: _jsx("td", { colSpan: 4, children: "No routes yet." }) })) : (routes.map((route) => (_jsxs("tr", { children: [_jsx("td", { children: route.tenant_id }), _jsx("td", { children: route.id }), _jsx("td", { children: _jsx(Badge, { value: route.connected ? "active" : "degraded" }) }), _jsx("td", { className: "code", children: route.public_url ?? "-" })] }, `${route.tenant_id}:${route.id}`)))) })] }) }), _jsx(Section, { title: "Connectors", children: _jsxs("table", { children: [_jsx("thead", { children: _jsxs("tr", { children: [_jsx("th", { children: "ID" }), _jsx("th", { children: "Status" }), _jsx("th", { children: "Agent" }), _jsx("th", { children: "Last Seen" })] }) }), _jsx("tbody", { children: connectors.length === 0 ? (_jsx("tr", { children: _jsx("td", { colSpan: 4, children: "No connectors yet." }) })) : (connectors.map((connector) => (_jsxs("tr", { children: [_jsx("td", { children: connector.id }), _jsx("td", { children: _jsx(Badge, { value: connector.connected ? "online" : "offline" }) }), _jsx("td", { children: connector.agent_id ?? "-" }), _jsx("td", { children: formatDateTime(connector.last_seen) })] }, connector.id)))) })] }) })] }));
}
function AdminOverviewPage({ api }) {
    const [stats, setStats] = useState(null);
    const [status, setStatus] = useState(null);
    const [loading, setLoading] = useState(true);
    const [error, setError] = useState("");
    const load = useCallback(async () => {
        setLoading(true);
        setError("");
        try {
            const [statsPayload, statusPayload] = await Promise.all([
                api("/api/admin/stats"),
                api("/api/admin/system-status"),
            ]);
            setStats(statsPayload);
            setStatus(statusPayload);
        }
        catch (err) {
            setError(toErrorMessage(err));
        }
        finally {
            setLoading(false);
        }
    }, [api]);
    useEffect(() => {
        void load();
    }, [load]);
    if (loading) {
        return _jsx(Section, { title: "Overview", children: "Loading..." });
    }
    if (error) {
        return (_jsx(Section, { title: "Overview", actions: _jsx("button", { onClick: () => void load(), children: "Retry" }), children: _jsx("p", { className: "status error", children: error }) }));
    }
    const routeCount = Number(stats?.route_count ?? 0);
    const connectorCount = Number(stats?.connector_count ?? 0);
    const tenantCount = Number(stats?.tenant_count ?? 0);
    const funnelTotals = stats?.funnel_analytics?.totals ?? {};
    return (_jsxs(_Fragment, { children: [_jsxs("div", { className: "gauge-row", children: [_jsx(GaugeCard, { title: "Routes", gauge: { used: routeCount, limit: Math.max(routeCount, 1), percent: 1 }, subtitle: "Global route count" }), _jsx(GaugeCard, { title: "Connectors", gauge: { used: connectorCount, limit: Math.max(connectorCount, 1), percent: 1 }, subtitle: "Global connector count" }), _jsx(GaugeCard, { title: "Tenants", gauge: { used: tenantCount, limit: Math.max(tenantCount, 1), percent: 1 }, subtitle: "Global tenant count" })] }), _jsx(Section, { title: "Global Snapshot", actions: _jsx("button", { onClick: () => void load(), children: "Refresh" }), children: _jsxs("div", { className: "kv", children: [_jsxs("p", { children: [_jsx("strong", { children: "Users" }), _jsx("span", { children: stats?.user_count ?? 0 })] }), _jsxs("p", { children: [_jsx("strong", { children: "Active Connectors" }), _jsx("span", { children: stats?.active_connectors ?? 0 })] }), _jsxs("p", { children: [_jsx("strong", { children: "Storage Driver" }), _jsx("span", { children: stats?.storage_driver ?? "memory" })] }), _jsxs("p", { children: [_jsx("strong", { children: "Uptime (s)" }), _jsx("span", { children: stats?.uptime_seconds ?? 0 })] }), _jsxs("p", { children: [_jsx("strong", { children: "Signup Success" }), _jsx("span", { children: funnelTotals.signup_success ?? 0 })] }), _jsxs("p", { children: [_jsx("strong", { children: "Download Clicks" }), _jsx("span", { children: funnelTotals.download_click ?? 0 })] })] }) }), _jsx(Section, { title: "Runtime", children: _jsxs("div", { className: "kv", children: [_jsxs("p", { children: [_jsx("strong", { children: "Pending Requests" }), _jsxs("span", { children: [status?.runtime?.pending_requests ?? 0, " / ", status?.runtime?.max_pending_global ?? 0] })] }), _jsxs("p", { children: [_jsx("strong", { children: "Queue Depth" }), _jsx("span", { children: status?.runtime?.queue_depth_total ?? 0 })] }), _jsxs("p", { children: [_jsx("strong", { children: "Latency p50 / p95" }), _jsxs("span", { children: [status?.runtime?.p50_latency_ms ?? 0, "ms / ", status?.runtime?.p95_latency_ms ?? 0, "ms"] })] }), _jsxs("p", { children: [_jsx("strong", { children: "Error Rate" }), _jsx("span", { children: formatPercent(status?.runtime?.error_rate) })] })] }) })] }));
}
function AdminUsersPage({ api }) {
    const [users, setUsers] = useState([]);
    const [tenants, setTenants] = useState([]);
    const [loading, setLoading] = useState(true);
    const [error, setError] = useState("");
    const [message, setMessage] = useState("");
    const load = useCallback(async () => {
        setLoading(true);
        setError("");
        try {
            const [usersPayload, tenantsPayload] = await Promise.all([
                api("/api/admin/users"),
                api("/api/tenants"),
            ]);
            setUsers(usersPayload.users ?? []);
            setTenants(tenantsPayload.tenants ?? []);
        }
        catch (err) {
            setError(toErrorMessage(err));
        }
        finally {
            setLoading(false);
        }
    }, [api]);
    useEffect(() => {
        void load();
    }, [load]);
    const createUser = useCallback(async (event) => {
        event.preventDefault();
        setMessage("");
        const form = event.currentTarget;
        const formData = new FormData(form);
        try {
            await api("/api/admin/users", {
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
        }
        catch (err) {
            setMessage(toErrorMessage(err));
        }
    }, [api, load]);
    const toggleUserStatus = useCallback(async (user) => {
        setMessage("");
        try {
            await api(`/api/admin/users/${encodeURIComponent(user.username)}`, {
                method: "PATCH",
                body: JSON.stringify({
                    status: user.status === "disabled" ? "active" : "disabled",
                }),
            });
            await load();
        }
        catch (err) {
            setMessage(toErrorMessage(err));
        }
    }, [api, load]);
    return (_jsxs(_Fragment, { children: [_jsxs(Section, { title: "Create User", children: [_jsxs("form", { className: "inline-form", onSubmit: createUser, children: [_jsx("input", { name: "username", placeholder: "username", required: true }), _jsx("input", { name: "password", type: "password", placeholder: "password", required: true }), _jsxs("select", { name: "role", defaultValue: "member", children: [_jsx("option", { value: "member", children: "member" }), _jsx("option", { value: "tenant_admin", children: "tenant_admin" }), _jsx("option", { value: "super_admin", children: "super_admin" })] }), _jsxs("select", { name: "tenant_id", defaultValue: "", children: [_jsx("option", { value: "", children: "No tenant (super admin)" }), tenants.map((tenant) => (_jsx("option", { value: tenant.id, children: tenant.id }, tenant.id)))] }), _jsx("button", { type: "submit", children: "Create" })] }), message ? _jsx("p", { className: "status", children: message }) : null] }), _jsxs(Section, { title: "Users", actions: _jsx("button", { onClick: () => void load(), children: "Refresh" }), children: [loading ? _jsx("p", { children: "Loading..." }) : null, error ? _jsx("p", { className: "status error", children: error }) : null, !loading && !error ? (_jsxs("table", { children: [_jsx("thead", { children: _jsxs("tr", { children: [_jsx("th", { children: "Username" }), _jsx("th", { children: "Role" }), _jsx("th", { children: "Tenant" }), _jsx("th", { children: "Status" }), _jsx("th", { children: "Action" })] }) }), _jsx("tbody", { children: users.length === 0 ? (_jsx("tr", { children: _jsx("td", { colSpan: 5, children: "No users." }) })) : (users.map((user) => (_jsxs("tr", { children: [_jsx("td", { children: user.username }), _jsx("td", { children: user.role }), _jsx("td", { children: user.tenant_id || "-" }), _jsx("td", { children: _jsx(Badge, { value: user.status || "active" }) }), _jsx("td", { children: _jsx("button", { className: "ghost", onClick: () => void toggleUserStatus(user), children: user.status === "disabled" ? "Enable" : "Disable" }) })] }, user.username)))) })] })) : null] })] }));
}
function AdminTenantsPage({ api }) {
    const [tenants, setTenants] = useState([]);
    const [plans, setPlans] = useState([]);
    const [loading, setLoading] = useState(true);
    const [error, setError] = useState("");
    const [message, setMessage] = useState("");
    const load = useCallback(async () => {
        setLoading(true);
        setError("");
        try {
            const [tenantsPayload, plansPayload] = await Promise.all([
                api("/api/tenants"),
                api("/api/admin/plans"),
            ]);
            setTenants(tenantsPayload.tenants ?? []);
            setPlans(plansPayload.plans ?? []);
        }
        catch (err) {
            setError(toErrorMessage(err));
        }
        finally {
            setLoading(false);
        }
    }, [api]);
    useEffect(() => {
        void load();
    }, [load]);
    const createTenant = useCallback(async (event) => {
        event.preventDefault();
        setMessage("");
        const form = event.currentTarget;
        const formData = new FormData(form);
        try {
            await api("/api/tenants", {
                method: "POST",
                body: JSON.stringify({
                    id: String(formData.get("id") ?? ""),
                    name: String(formData.get("name") ?? ""),
                }),
            });
            form.reset();
            setMessage("Tenant created.");
            await load();
        }
        catch (err) {
            setMessage(toErrorMessage(err));
        }
    }, [api, load]);
    const assignPlan = useCallback(async (tenantID, planID) => {
        setMessage("");
        try {
            await api(`/api/admin/tenants/${encodeURIComponent(tenantID)}/assign-plan`, {
                method: "POST",
                body: JSON.stringify({ plan_id: planID }),
            });
            setMessage(`Assigned plan ${planID} to ${tenantID}.`);
        }
        catch (err) {
            setMessage(toErrorMessage(err));
        }
    }, [api]);
    return (_jsxs(_Fragment, { children: [_jsxs(Section, { title: "Create Tenant", children: [_jsxs("form", { className: "inline-form", onSubmit: createTenant, children: [_jsx("input", { name: "id", placeholder: "tenant-id", required: true }), _jsx("input", { name: "name", placeholder: "Tenant name", required: true }), _jsx("button", { type: "submit", children: "Create" })] }), message ? _jsx("p", { className: "status", children: message }) : null] }), _jsxs(Section, { title: "Tenants", actions: _jsx("button", { onClick: () => void load(), children: "Refresh" }), children: [loading ? _jsx("p", { children: "Loading..." }) : null, error ? _jsx("p", { className: "status error", children: error }) : null, !loading && !error ? (_jsxs("table", { children: [_jsx("thead", { children: _jsxs("tr", { children: [_jsx("th", { children: "ID" }), _jsx("th", { children: "Name" }), _jsx("th", { children: "Routes" }), _jsx("th", { children: "Assign Plan" })] }) }), _jsx("tbody", { children: tenants.length === 0 ? (_jsx("tr", { children: _jsx("td", { colSpan: 4, children: "No tenants." }) })) : (tenants.map((tenant) => (_jsxs("tr", { children: [_jsx("td", { children: tenant.id }), _jsx("td", { children: tenant.name }), _jsx("td", { children: tenant.route_count ?? 0 }), _jsx("td", { children: _jsxs("form", { className: "inline-form", onSubmit: (event) => {
                                                    event.preventDefault();
                                                    const formData = new FormData(event.currentTarget);
                                                    void assignPlan(tenant.id, String(formData.get("plan_id") ?? ""));
                                                }, children: [_jsx("select", { name: "plan_id", defaultValue: plans[0]?.id ?? "", children: plans.map((plan) => (_jsx("option", { value: plan.id, children: plan.id }, plan.id))) }), _jsx("button", { type: "submit", children: "Assign" })] }) })] }, tenant.id)))) })] })) : null] })] }));
}
function AdminPlansPage({ api }) {
    const [plans, setPlans] = useState([]);
    const [loading, setLoading] = useState(true);
    const [error, setError] = useState("");
    const [message, setMessage] = useState("");
    const load = useCallback(async () => {
        setLoading(true);
        setError("");
        try {
            const payload = await api("/api/admin/plans");
            setPlans(payload.plans ?? []);
        }
        catch (err) {
            setError(toErrorMessage(err));
        }
        finally {
            setLoading(false);
        }
    }, [api]);
    useEffect(() => {
        void load();
    }, [load]);
    const createPlan = useCallback(async (event) => {
        event.preventDefault();
        setMessage("");
        const form = event.currentTarget;
        const formData = new FormData(form);
        try {
            await api("/api/admin/plans", {
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
        }
        catch (err) {
            setMessage(toErrorMessage(err));
        }
    }, [api, load]);
    return (_jsxs(_Fragment, { children: [_jsxs(Section, { title: "Create Plan", children: [_jsxs("form", { className: "inline-form", onSubmit: createPlan, children: [_jsx("input", { name: "id", placeholder: "id", required: true }), _jsx("input", { name: "name", placeholder: "name", required: true }), _jsx("input", { name: "description", placeholder: "description" }), _jsx("input", { name: "max_routes", type: "number", min: 1, placeholder: "max routes", required: true }), _jsx("input", { name: "max_connectors", type: "number", min: 1, placeholder: "max connectors", required: true }), _jsx("input", { name: "max_rps", type: "number", min: 1, placeholder: "max rps", required: true }), _jsx("input", { name: "max_monthly_gb", type: "number", min: 1, placeholder: "max monthly gb", required: true }), _jsx("input", { name: "price_monthly_usd", type: "number", min: 0, step: "0.01", placeholder: "monthly price", required: true }), _jsx("input", { name: "price_annual_usd", type: "number", min: 0, step: "0.01", placeholder: "annual price", required: true }), _jsx("input", { name: "public_order", type: "number", min: 0, placeholder: "public order", required: true }), _jsxs("label", { className: "checkbox", children: [_jsx("input", { type: "checkbox", name: "tls_enabled" }), "TLS enabled"] }), _jsx("button", { type: "submit", children: "Save" })] }), message ? _jsx("p", { className: "status", children: message }) : null] }), _jsxs(Section, { title: "Plans", actions: _jsx("button", { onClick: () => void load(), children: "Refresh" }), children: [loading ? _jsx("p", { children: "Loading..." }) : null, error ? _jsx("p", { className: "status error", children: error }) : null, !loading && !error ? (_jsxs("table", { children: [_jsx("thead", { children: _jsxs("tr", { children: [_jsx("th", { children: "ID" }), _jsx("th", { children: "Name" }), _jsx("th", { children: "Routes" }), _jsx("th", { children: "Connectors" }), _jsx("th", { children: "RPS" }), _jsx("th", { children: "Monthly GB" }), _jsx("th", { children: "Monthly USD" }), _jsx("th", { children: "Annual USD" }), _jsx("th", { children: "Order" }), _jsx("th", { children: "TLS" })] }) }), _jsx("tbody", { children: plans.length === 0 ? (_jsx("tr", { children: _jsx("td", { colSpan: 10, children: "No plans." }) })) : (plans.map((plan) => (_jsxs("tr", { children: [_jsx("td", { children: plan.id }), _jsx("td", { children: plan.name }), _jsx("td", { children: plan.max_routes }), _jsx("td", { children: plan.max_connectors }), _jsx("td", { children: plan.max_rps }), _jsx("td", { children: plan.max_monthly_gb }), _jsx("td", { children: formatNumber(plan.price_monthly_usd) }), _jsx("td", { children: formatNumber(plan.price_annual_usd) }), _jsx("td", { children: plan.public_order ?? 0 }), _jsx("td", { children: _jsx(Badge, { value: plan.tls_enabled ? "enabled" : "disabled" }) })] }, plan.id)))) })] })) : null] })] }));
}
function AdminTLSPage({ api }) {
    const [certificates, setCertificates] = useState([]);
    const [loading, setLoading] = useState(true);
    const [error, setError] = useState("");
    const [message, setMessage] = useState("");
    const load = useCallback(async () => {
        setLoading(true);
        setError("");
        try {
            const payload = await api("/api/admin/tls/certificates");
            setCertificates(payload.certificates ?? []);
        }
        catch (err) {
            setError(toErrorMessage(err));
        }
        finally {
            setLoading(false);
        }
    }, [api]);
    useEffect(() => {
        void load();
    }, [load]);
    const upload = useCallback(async (event) => {
        event.preventDefault();
        setMessage("");
        const form = event.currentTarget;
        const formData = new FormData(form);
        try {
            await api("/api/admin/tls/certificates", {
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
        }
        catch (err) {
            setMessage(toErrorMessage(err));
        }
    }, [api, load]);
    const setActive = useCallback(async (cert, active) => {
        setMessage("");
        try {
            await api(`/api/admin/tls/certificates/${encodeURIComponent(cert.id)}`, {
                method: "PATCH",
                body: JSON.stringify({ active }),
            });
            await load();
        }
        catch (err) {
            setMessage(toErrorMessage(err));
        }
    }, [api, load]);
    const remove = useCallback(async (cert) => {
        setMessage("");
        try {
            await api(`/api/admin/tls/certificates/${encodeURIComponent(cert.id)}`, {
                method: "DELETE",
            });
            await load();
        }
        catch (err) {
            setMessage(toErrorMessage(err));
        }
    }, [api, load]);
    return (_jsxs(_Fragment, { children: [_jsxs(Section, { title: "Upload Certificate", children: [_jsxs("form", { className: "stack", onSubmit: upload, children: [_jsxs("div", { className: "inline-form", children: [_jsx("input", { name: "id", placeholder: "id", required: true }), _jsx("input", { name: "hostname", placeholder: "example.com", required: true }), _jsxs("label", { className: "checkbox", children: [_jsx("input", { name: "active", type: "checkbox" }), "Active"] })] }), _jsxs("label", { children: ["Certificate PEM", _jsx("textarea", { name: "cert_pem", rows: 6, required: true })] }), _jsxs("label", { children: ["Private Key PEM", _jsx("textarea", { name: "key_pem", rows: 6, required: true })] }), _jsx("button", { type: "submit", children: "Upload" })] }), message ? _jsx("p", { className: "status", children: message }) : null] }), _jsxs(Section, { title: "Certificates", actions: _jsx("button", { onClick: () => void load(), children: "Refresh" }), children: [loading ? _jsx("p", { children: "Loading..." }) : null, error ? _jsx("p", { className: "status error", children: error }) : null, !loading && !error ? (_jsxs("table", { children: [_jsx("thead", { children: _jsxs("tr", { children: [_jsx("th", { children: "ID" }), _jsx("th", { children: "Hostname" }), _jsx("th", { children: "Status" }), _jsx("th", { children: "Expires" }), _jsx("th", { children: "Actions" })] }) }), _jsx("tbody", { children: certificates.length === 0 ? (_jsx("tr", { children: _jsx("td", { colSpan: 5, children: "No certificates." }) })) : (certificates.map((cert) => (_jsxs("tr", { children: [_jsx("td", { children: cert.id }), _jsx("td", { children: cert.hostname }), _jsx("td", { children: _jsx(Badge, { value: cert.active ? "active" : "inactive" }) }), _jsx("td", { children: cert.expires_at ? cert.expires_at.slice(0, 10) : "-" }), _jsx("td", { children: _jsxs("div", { className: "actions", children: [_jsx("button", { className: "ghost", onClick: () => void setActive(cert, !cert.active), children: cert.active ? "Deactivate" : "Activate" }), _jsx("button", { className: "ghost danger", onClick: () => void remove(cert), children: "Delete" })] }) })] }, cert.id)))) })] })) : null] })] }));
}
function AdminSystemPage({ api }) {
    const [status, setStatus] = useState(null);
    const [incidents, setIncidents] = useState([]);
    const [loading, setLoading] = useState(true);
    const [error, setError] = useState("");
    const load = useCallback(async () => {
        setLoading(true);
        setError("");
        try {
            const [statusPayload, incidentPayload] = await Promise.all([
                api("/api/admin/system-status"),
                api("/api/admin/incidents?limit=100"),
            ]);
            setStatus(statusPayload);
            setIncidents(incidentPayload.incidents ?? []);
        }
        catch (err) {
            setError(toErrorMessage(err));
        }
        finally {
            setLoading(false);
        }
    }, [api]);
    useEffect(() => {
        void load();
    }, [load]);
    return (_jsxs(_Fragment, { children: [_jsxs(Section, { title: "System Status", actions: _jsx("button", { onClick: () => void load(), children: "Refresh" }), children: [loading ? _jsx("p", { children: "Loading..." }) : null, error ? _jsx("p", { className: "status error", children: error }) : null, !loading && !error ? (_jsxs("div", { className: "kv", children: [_jsxs("p", { children: [_jsx("strong", { children: "Gateway" }), _jsx("span", { children: status?.gateway?.status ?? "unknown" })] }), _jsxs("p", { children: [_jsx("strong", { children: "Storage" }), _jsx("span", { children: status?.storage?.driver ?? "memory" })] }), _jsxs("p", { children: [_jsx("strong", { children: "Active Sessions" }), _jsx("span", { children: status?.runtime?.active_sessions ?? 0 })] }), _jsxs("p", { children: [_jsx("strong", { children: "Pending Requests" }), _jsx("span", { children: status?.runtime?.pending_requests ?? 0 })] }), _jsxs("p", { children: [_jsx("strong", { children: "Latency p50/p95" }), _jsxs("span", { children: [status?.runtime?.p50_latency_ms ?? 0, "ms / ", status?.runtime?.p95_latency_ms ?? 0, "ms"] })] }), _jsxs("p", { children: [_jsx("strong", { children: "Error Rate" }), _jsx("span", { children: formatPercent(status?.runtime?.error_rate) })] })] })) : null] }), _jsx(Section, { title: "Incidents", children: _jsxs("table", { children: [_jsx("thead", { children: _jsxs("tr", { children: [_jsx("th", { children: "Severity" }), _jsx("th", { children: "Source" }), _jsx("th", { children: "Message" }), _jsx("th", { children: "Created" })] }) }), _jsx("tbody", { children: incidents.length === 0 ? (_jsx("tr", { children: _jsx("td", { colSpan: 4, children: "No incidents." }) })) : (incidents.map((incident) => (_jsxs("tr", { children: [_jsx("td", { children: _jsx(Badge, { value: incident.severity }) }), _jsx("td", { children: incident.source }), _jsx("td", { children: incident.message }), _jsx("td", { children: formatDateTime(incident.created_at) })] }, incident.id)))) })] }) })] }));
}
function RoutesPage({ api, me }) {
    const isSuper = me.user.role === "super_admin";
    const [routes, setRoutes] = useState([]);
    const [connectors, setConnectors] = useState([]);
    const [tenants, setTenants] = useState([]);
    const [loading, setLoading] = useState(true);
    const [error, setError] = useState("");
    const [message, setMessage] = useState("");
    const load = useCallback(async () => {
        setLoading(true);
        setError("");
        try {
            const [routesPayload, connectorsPayload, tenantsPayload] = await Promise.all([
                api("/api/me/routes"),
                api("/api/me/connectors"),
                api("/api/tenants"),
            ]);
            setRoutes(routesPayload.routes ?? []);
            setConnectors(connectorsPayload.connectors ?? []);
            setTenants(tenantsPayload.tenants ?? []);
        }
        catch (err) {
            setError(toErrorMessage(err));
        }
        finally {
            setLoading(false);
        }
    }, [api]);
    useEffect(() => {
        void load();
    }, [load]);
    const submitRoute = useCallback(async (event) => {
        event.preventDefault();
        setMessage("");
        const form = event.currentTarget;
        const formData = new FormData(form);
        const tenantID = isSuper
            ? String(formData.get("tenant_id") ?? "")
            : me.user.tenant_id || tenants[0]?.id || "default";
        try {
            await api(`/api/tenants/${encodeURIComponent(tenantID)}/routes`, {
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
        }
        catch (err) {
            setMessage(toErrorMessage(err));
        }
    }, [api, isSuper, load, me.user.tenant_id, tenants]);
    const deleteRoute = useCallback(async (route) => {
        setMessage("");
        try {
            await api(`/api/tenants/${encodeURIComponent(route.tenant_id)}/routes/${encodeURIComponent(route.id)}`, {
                method: "DELETE",
            });
            await load();
        }
        catch (err) {
            setMessage(toErrorMessage(err));
        }
    }, [api, load]);
    const defaultTenant = me.user.tenant_id || tenants[0]?.id || "default";
    return (_jsxs(_Fragment, { children: [_jsxs(Section, { title: "Create Route", children: [_jsxs("form", { className: "grid cols-2", onSubmit: submitRoute, children: [_jsxs("label", { children: ["Tenant", _jsx("select", { name: "tenant_id", defaultValue: defaultTenant, disabled: !isSuper, required: isSuper, children: tenants.map((tenant) => (_jsx("option", { value: tenant.id, children: tenant.id }, tenant.id))) })] }), _jsxs("label", { children: ["Route ID", _jsx("input", { name: "id", placeholder: "api", required: true })] }), _jsxs("label", { children: ["Direct Target URL", _jsx("input", { name: "target", placeholder: "http://127.0.0.1:3000" })] }), _jsxs("label", { children: ["Connector", _jsxs("select", { name: "connector_id", defaultValue: "", children: [_jsx("option", { value: "", children: "Direct target" }), connectors.map((connector) => (_jsx("option", { value: connector.id, children: connector.id }, connector.id)))] })] }), _jsxs("label", { children: ["Local Scheme", _jsxs("select", { name: "local_scheme", defaultValue: "http", children: [_jsx("option", { value: "http", children: "http" }), _jsx("option", { value: "https", children: "https" })] })] }), _jsxs("label", { children: ["Local Host", _jsx("input", { name: "local_host", defaultValue: "127.0.0.1" })] }), _jsxs("label", { children: ["Local Port", _jsx("input", { name: "local_port", type: "number", min: 1, max: 65535, placeholder: "3000" })] }), _jsxs("label", { children: ["Local Base Path", _jsx("input", { name: "local_base_path", placeholder: "/" })] }), _jsxs("label", { children: ["Access Token", _jsx("input", { name: "token", placeholder: "optional" })] }), _jsxs("label", { children: ["Route Max RPS", _jsx("input", { name: "max_rps", type: "number", min: 0, step: "0.1", placeholder: "0 = fair share" })] }), _jsx("div", { children: _jsx("button", { type: "submit", children: "Save Route" }) })] }), message ? _jsx("p", { className: "status", children: message }) : null] }), _jsxs(Section, { title: "Routes", actions: _jsx("button", { onClick: () => void load(), children: "Refresh" }), children: [loading ? _jsx("p", { children: "Loading..." }) : null, error ? _jsx("p", { className: "status error", children: error }) : null, !loading && !error ? (_jsxs("table", { children: [_jsx("thead", { children: _jsxs("tr", { children: [_jsx("th", { children: "Tenant" }), _jsx("th", { children: "ID" }), _jsx("th", { children: "Connector" }), _jsx("th", { children: "Max RPS" }), _jsx("th", { children: "Status" }), _jsx("th", { children: "Public URL" }), _jsx("th", { children: "Action" })] }) }), _jsx("tbody", { children: routes.length === 0 ? (_jsx("tr", { children: _jsx("td", { colSpan: 7, children: "No routes." }) })) : (routes.map((route) => (_jsxs("tr", { children: [_jsx("td", { children: route.tenant_id }), _jsx("td", { children: route.id }), _jsx("td", { children: route.connector_id || "-" }), _jsx("td", { children: route.max_rps && route.max_rps > 0 ? route.max_rps : "-" }), _jsx("td", { children: _jsx(Badge, { value: route.connected ? "active" : "offline" }) }), _jsx("td", { className: "code", children: route.public_url ?? "-" }), _jsx("td", { children: _jsx("button", { className: "ghost danger", onClick: () => void deleteRoute(route), children: "Delete" }) })] }, `${route.tenant_id}:${route.id}`)))) })] })) : null] })] }));
}
function ConnectorsPage({ api, me }) {
    const isSuper = me.user.role === "super_admin";
    const [connectors, setConnectors] = useState([]);
    const [tenants, setTenants] = useState([]);
    const [loading, setLoading] = useState(true);
    const [error, setError] = useState("");
    const [message, setMessage] = useState("");
    const [output, setOutput] = useState("");
    const load = useCallback(async () => {
        setLoading(true);
        setError("");
        try {
            const [connectorsPayload, tenantsPayload] = await Promise.all([
                api("/api/me/connectors"),
                api("/api/tenants"),
            ]);
            setConnectors(connectorsPayload.connectors ?? []);
            setTenants(tenantsPayload.tenants ?? []);
        }
        catch (err) {
            setError(toErrorMessage(err));
        }
        finally {
            setLoading(false);
        }
    }, [api]);
    useEffect(() => {
        void load();
    }, [load]);
    const createConnector = useCallback(async (event) => {
        event.preventDefault();
        setMessage("");
        const form = event.currentTarget;
        const formData = new FormData(form);
        const tenantID = isSuper
            ? String(formData.get("tenant_id") ?? "")
            : me.user.tenant_id || tenants[0]?.id || "default";
        try {
            await api("/api/connectors", {
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
        }
        catch (err) {
            setMessage(toErrorMessage(err));
        }
    }, [api, isSuper, load, me.user.tenant_id, tenants]);
    const pair = useCallback(async (id) => {
        setMessage("");
        try {
            const payload = await api(`/api/connectors/${encodeURIComponent(id)}/pair`, {
                method: "POST",
            });
            setOutput(payload.command ?? "");
        }
        catch (err) {
            setMessage(toErrorMessage(err));
        }
    }, [api]);
    const rotate = useCallback(async (id) => {
        setMessage("");
        try {
            const payload = await api(`/api/connectors/${encodeURIComponent(id)}/rotate`, {
                method: "POST",
            });
            setOutput(`connector_secret=${payload.connector_secret ?? ""}`);
        }
        catch (err) {
            setMessage(toErrorMessage(err));
        }
    }, [api]);
    const remove = useCallback(async (id) => {
        setMessage("");
        try {
            await api(`/api/connectors/${encodeURIComponent(id)}`, { method: "DELETE" });
            await load();
        }
        catch (err) {
            setMessage(toErrorMessage(err));
        }
    }, [api, load]);
    return (_jsxs(_Fragment, { children: [_jsxs(Section, { title: "Create Connector", children: [_jsxs("form", { className: "inline-form", onSubmit: createConnector, children: [isSuper ? (_jsx("select", { name: "tenant_id", defaultValue: me.user.tenant_id || tenants[0]?.id || "default", children: tenants.map((tenant) => (_jsx("option", { value: tenant.id, children: tenant.id }, tenant.id))) })) : null, _jsx("input", { name: "id", placeholder: "connector-id", required: true }), _jsx("input", { name: "name", placeholder: "Friendly name", required: true }), _jsx("button", { type: "submit", children: "Create" })] }), message ? _jsx("p", { className: "status", children: message }) : null, output ? _jsx("p", { className: "code output", children: output }) : null] }), _jsxs(Section, { title: "Connectors", actions: _jsx("button", { onClick: () => void load(), children: "Refresh" }), children: [loading ? _jsx("p", { children: "Loading..." }) : null, error ? _jsx("p", { className: "status error", children: error }) : null, !loading && !error ? (_jsxs("table", { children: [_jsx("thead", { children: _jsxs("tr", { children: [_jsx("th", { children: "ID" }), _jsx("th", { children: "Tenant" }), _jsx("th", { children: "Status" }), _jsx("th", { children: "Agent" }), _jsx("th", { children: "Actions" })] }) }), _jsx("tbody", { children: connectors.length === 0 ? (_jsx("tr", { children: _jsx("td", { colSpan: 5, children: "No connectors." }) })) : (connectors.map((connector) => (_jsxs("tr", { children: [_jsx("td", { children: connector.id }), _jsx("td", { children: connector.tenant_id }), _jsx("td", { children: _jsx(Badge, { value: connector.connected ? "online" : "offline" }) }), _jsx("td", { children: connector.agent_id || "-" }), _jsx("td", { children: _jsxs("div", { className: "actions", children: [_jsx("button", { className: "ghost", onClick: () => void pair(connector.id), children: "Pair" }), _jsx("button", { className: "ghost", onClick: () => void rotate(connector.id), children: "Rotate" }), _jsx("button", { className: "ghost danger", onClick: () => void remove(connector.id), children: "Delete" })] }) })] }, connector.id)))) })] })) : null] })] }));
}
function TenantConfigPage({ api, me }) {
    const isSuper = me.user.role === "super_admin";
    const [tenants, setTenants] = useState([]);
    const [selectedTenant, setSelectedTenant] = useState("");
    const [environment, setEnvironment] = useState({
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
            const payload = await api("/api/tenants");
            const items = payload.tenants ?? [];
            setTenants(items);
            if (items.length === 0) {
                setSelectedTenant("");
            }
            else if (isSuper) {
                setSelectedTenant((current) => current || items[0].id);
            }
            else {
                setSelectedTenant(me.user.tenant_id || items[0].id);
            }
        }
        catch (err) {
            setError(toErrorMessage(err));
        }
        finally {
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
            const payload = await api(`/api/tenants/${encodeURIComponent(selectedTenant)}/environment`);
            const env = payload.environment;
            setEnvironment(env);
            setVariablesText(JSON.stringify(env.variables ?? {}, null, 2));
        }
        catch (err) {
            const messageText = toErrorMessage(err);
            setError(messageText);
        }
    }, [api, selectedTenant]);
    useEffect(() => {
        void loadEnvironment();
    }, [loadEnvironment]);
    const saveEnvironment = useCallback(async (event) => {
        event.preventDefault();
        if (!selectedTenant) {
            return;
        }
        setMessage("");
        setError("");
        let variables;
        try {
            const parsed = JSON.parse(variablesText);
            if (!isRecord(parsed)) {
                throw new Error("Variables must be a JSON object.");
            }
            variables = {};
            for (const [key, value] of Object.entries(parsed)) {
                variables[String(key)] = String(value);
            }
        }
        catch (err) {
            setError(toErrorMessage(err));
            return;
        }
        try {
            await api(`/api/tenants/${encodeURIComponent(selectedTenant)}/environment`, {
                method: "PUT",
                body: JSON.stringify({
                    scheme: environment.scheme,
                    host: environment.host,
                    default_port: environment.default_port,
                    variables,
                }),
            });
            setMessage("Environment saved.");
        }
        catch (err) {
            setError(toErrorMessage(err));
        }
    }, [api, environment, selectedTenant, variablesText]);
    return (_jsxs(Section, { title: "Tenant Environment", actions: _jsx("button", { onClick: () => void loadEnvironment(), children: "Refresh" }), children: [loading ? _jsx("p", { children: "Loading..." }) : null, error ? _jsx("p", { className: "status error", children: error }) : null, !loading && selectedTenant ? (_jsxs("form", { className: "grid cols-2", onSubmit: saveEnvironment, children: [isSuper ? (_jsxs("label", { children: ["Tenant", _jsx("select", { value: selectedTenant, onChange: (event) => setSelectedTenant(event.target.value), children: tenants.map((tenant) => (_jsx("option", { value: tenant.id, children: tenant.id }, tenant.id))) })] })) : null, _jsxs("label", { children: ["Scheme", _jsx("input", { value: environment.scheme, onChange: (event) => setEnvironment((current) => ({ ...current, scheme: event.target.value })) })] }), _jsxs("label", { children: ["Host", _jsx("input", { value: environment.host, onChange: (event) => setEnvironment((current) => ({ ...current, host: event.target.value })) })] }), _jsxs("label", { children: ["Default Port", _jsx("input", { type: "number", value: environment.default_port, onChange: (event) => setEnvironment((current) => ({
                                    ...current,
                                    default_port: Number(event.target.value || 0),
                                })) })] }), _jsxs("label", { className: "wide", children: ["Variables (JSON)", _jsx("textarea", { rows: 8, value: variablesText, onChange: (event) => setVariablesText(event.target.value) })] }), _jsx("div", { children: _jsx("button", { type: "submit", children: "Save" }) })] })) : null, message ? _jsx("p", { className: "status", children: message }) : null] }));
}
export default function WorkspaceApp({ me, api, onLogout, }) {
    const isSuper = me.user.role === "super_admin";
    const navItems = isSuper ? SUPER_NAV : TENANT_NAV;
    const [page, setPage] = useState(isSuper ? "adminOverview" : "dashboard");
    useEffect(() => {
        const allowed = new Set(navItems.map((item) => item.key));
        if (!allowed.has(page)) {
            setPage(navItems[0].key);
        }
    }, [navItems, page]);
    const content = useMemo(() => {
        if (page === "dashboard") {
            return _jsx(TenantDashboardPage, { api: api });
        }
        if (page === "routes") {
            return _jsx(RoutesPage, { api: api, me: me });
        }
        if (page === "connectors") {
            return _jsx(ConnectorsPage, { api: api, me: me });
        }
        if (page === "tenantConfig") {
            return _jsx(TenantConfigPage, { api: api, me: me });
        }
        if (page === "adminOverview") {
            return _jsx(AdminOverviewPage, { api: api });
        }
        if (page === "adminUsers") {
            return _jsx(AdminUsersPage, { api: api });
        }
        if (page === "adminTenants") {
            return _jsx(AdminTenantsPage, { api: api });
        }
        if (page === "adminPlans") {
            return _jsx(AdminPlansPage, { api: api });
        }
        if (page === "adminTLS") {
            return _jsx(AdminTLSPage, { api: api });
        }
        if (page === "adminSystem") {
            return _jsx(AdminSystemPage, { api: api });
        }
        return _jsx(Section, { title: "Not Found", children: "Page not found." });
    }, [api, me, page]);
    const meta = PAGE_META[page];
    return (_jsxs("main", { className: "workspace-shell", children: [_jsxs("aside", { className: "sidebar", children: [_jsxs("div", { className: "brand", children: [_jsx("h1", { children: "Proxer" }), _jsxs("p", { children: [me.user.username, " \u00B7 ", me.user.role] })] }), _jsx("nav", { className: "nav", children: navItems.map((item) => (_jsx("button", { className: item.key === page ? "active" : "", onClick: () => setPage(item.key), children: item.label }, item.key))) }), _jsx("button", { className: "ghost danger", onClick: () => void onLogout(), children: "Logout" })] }), _jsxs("section", { className: "workspace-content", children: [_jsxs("header", { className: "topbar", children: [_jsx("h2", { children: meta.title }), _jsx("p", { children: meta.subtitle })] }), _jsx("div", { className: "page-content", children: content })] })] }));
}
