import { jsx as _jsx, jsxs as _jsxs, Fragment as _Fragment } from "react/jsx-runtime";
import { Suspense, lazy, useCallback, useEffect, useState } from "react";
import { Link, Navigate, Route, Routes, useLocation, useNavigate } from "react-router-dom";
const WorkspaceApp = lazy(() => import("./workspace/WorkspaceApp"));
class ApiError extends Error {
    constructor(message, status, payload) {
        super(message);
        this.name = "ApiError";
        this.status = status;
        this.payload = payload;
    }
}
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
async function requestJSON(path, init) {
    const headers = new Headers(init?.headers ?? undefined);
    if (init?.body !== undefined && !headers.has("Content-Type")) {
        headers.set("Content-Type", "application/json");
    }
    const response = await fetch(path, {
        credentials: "include",
        ...init,
        headers,
    });
    if (response.status === 204) {
        return null;
    }
    const contentType = response.headers.get("content-type") ?? "";
    const asJSON = contentType.includes("application/json");
    const payload = asJSON ? await response.json() : await response.text();
    if (!response.ok) {
        let message = `Request failed (${response.status})`;
        if (typeof payload === "string" && payload.trim() !== "") {
            message = payload;
        }
        else if (isRecord(payload) && typeof payload.message === "string") {
            message = payload.message;
        }
        throw new ApiError(message, response.status, payload);
    }
    return payload;
}
function trackPublicEvent(event) {
    if (typeof window === "undefined" || typeof event.event !== "string" || event.event.trim() === "") {
        return;
    }
    const payload = {
        source: "web",
        page_path: window.location.pathname,
        ...event,
    };
    const serialized = JSON.stringify(payload);
    try {
        if (typeof navigator !== "undefined" && typeof navigator.sendBeacon === "function") {
            const blob = new Blob([serialized], { type: "application/json" });
            if (navigator.sendBeacon("/api/public/events", blob)) {
                return;
            }
        }
    }
    catch {
        // Fall back to fetch.
    }
    void fetch("/api/public/events", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: serialized,
        keepalive: true,
    }).catch(() => undefined);
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
function formatBinarySize(bytes) {
    const numeric = Number(bytes ?? 0);
    if (!Number.isFinite(numeric) || numeric <= 0) {
        return "-";
    }
    if (numeric < 1024 * 1024) {
        return `${Math.round(numeric / 1024)} KB`;
    }
    return `${(numeric / (1024 * 1024)).toFixed(1)} MB`;
}
function LoginView({ onLoggedIn }) {
    const navigate = useNavigate();
    const [username, setUsername] = useState("");
    const [password, setPassword] = useState("");
    const [status, setStatus] = useState("");
    const [submitting, setSubmitting] = useState(false);
    const submit = useCallback(async (event) => {
        event.preventDefault();
        setSubmitting(true);
        setStatus("");
        try {
            await requestJSON("/api/auth/login", {
                method: "POST",
                body: JSON.stringify({ username, password }),
            });
            await onLoggedIn();
            navigate("/app");
        }
        catch (error) {
            setStatus(toErrorMessage(error));
        }
        finally {
            setSubmitting(false);
        }
    }, [navigate, onLoggedIn, password, username]);
    return (_jsxs("main", { className: "auth-shell", children: [_jsx("div", { className: "orb orb-a" }), _jsx("div", { className: "orb orb-b" }), _jsxs("section", { className: "auth-card", children: [_jsx("h1", { children: "Proxer" }), _jsx("p", { children: "Route internet traffic to localhost with tenant-scoped governance." }), _jsxs("form", { className: "stack", onSubmit: submit, children: [_jsxs("label", { children: ["Username", _jsx("input", { value: username, onChange: (event) => setUsername(event.target.value), required: true })] }), _jsxs("label", { children: ["Password", _jsx("input", { type: "password", value: password, onChange: (event) => setPassword(event.target.value), required: true })] }), _jsx("button", { type: "submit", disabled: submitting, children: submitting ? "Logging in..." : "Login" })] }), status ? _jsx("p", { className: "status error", children: status }) : null, _jsxs("div", { className: "auth-links", children: [_jsx(Link, { to: "/signup", children: "Create account" }), _jsx(Link, { to: "/", children: "Back to website" })] })] })] }));
}
function SignupView({ onSignedUp }) {
    const navigate = useNavigate();
    const [username, setUsername] = useState("");
    const [password, setPassword] = useState("");
    const [confirmPassword, setConfirmPassword] = useState("");
    const [status, setStatus] = useState("");
    const [submitting, setSubmitting] = useState(false);
    const submit = useCallback(async (event) => {
        event.preventDefault();
        setStatus("");
        if (password !== confirmPassword) {
            setStatus("Passwords do not match.");
            return;
        }
        setSubmitting(true);
        trackPublicEvent({ event: "signup_submit", outcome: "attempt" });
        try {
            await requestJSON("/api/public/signup", {
                method: "POST",
                body: JSON.stringify({ username, password }),
            });
            trackPublicEvent({ event: "signup_success", outcome: "success" });
            await onSignedUp();
            navigate("/app");
        }
        catch (error) {
            setStatus(toErrorMessage(error));
            trackPublicEvent({ event: "signup_failure", outcome: "error" });
        }
        finally {
            setSubmitting(false);
        }
    }, [confirmPassword, navigate, onSignedUp, password, username]);
    return (_jsxs("main", { className: "auth-shell", children: [_jsx("div", { className: "orb orb-a" }), _jsx("div", { className: "orb orb-b" }), _jsxs("section", { className: "auth-card", children: [_jsx("h1", { children: "Create Workspace" }), _jsx("p", { children: "Get instant access with a tenant admin account on the free plan." }), _jsxs("form", { className: "stack", onSubmit: submit, children: [_jsxs("label", { children: ["Username", _jsx("input", { value: username, onChange: (event) => setUsername(event.target.value), required: true })] }), _jsxs("label", { children: ["Password", _jsx("input", { type: "password", value: password, minLength: 6, onChange: (event) => setPassword(event.target.value), required: true })] }), _jsxs("label", { children: ["Confirm Password", _jsx("input", { type: "password", value: confirmPassword, minLength: 6, onChange: (event) => setConfirmPassword(event.target.value), required: true })] }), _jsx("button", { type: "submit", disabled: submitting, children: submitting ? "Creating account..." : "Sign up" })] }), status ? _jsx("p", { className: "status error", children: status }) : null, _jsxs("div", { className: "auth-links", children: [_jsx(Link, { to: "/login", children: "Already have an account?" }), _jsx(Link, { to: "/", children: "Back to website" })] })] })] }));
}
function LandingPage({ me }) {
    const [plans, setPlans] = useState([]);
    const [downloads, setDownloads] = useState(null);
    const [loadingPlans, setLoadingPlans] = useState(true);
    const [loadingDownloads, setLoadingDownloads] = useState(true);
    const [planError, setPlanError] = useState("");
    const [downloadError, setDownloadError] = useState("");
    const [billing, setBilling] = useState("monthly");
    useEffect(() => {
        trackPublicEvent({ event: "landing_view" });
        let mounted = true;
        const loadPlans = async () => {
            setLoadingPlans(true);
            setPlanError("");
            try {
                const payload = await requestJSON("/api/public/plans");
                if (mounted) {
                    setPlans(payload.plans ?? []);
                }
            }
            catch (error) {
                if (mounted) {
                    setPlanError(toErrorMessage(error));
                }
            }
            finally {
                if (mounted) {
                    setLoadingPlans(false);
                }
            }
        };
        const loadDownloads = async () => {
            setLoadingDownloads(true);
            setDownloadError("");
            try {
                const payload = await requestJSON("/api/public/downloads");
                if (mounted) {
                    setDownloads(payload);
                }
            }
            catch (error) {
                if (mounted) {
                    setDownloadError(toErrorMessage(error));
                }
            }
            finally {
                if (mounted) {
                    setLoadingDownloads(false);
                }
            }
        };
        void Promise.all([loadPlans(), loadDownloads()]);
        return () => {
            mounted = false;
        };
    }, []);
    const orderedPlans = [...plans].sort((a, b) => Number(a.public_order ?? 0) - Number(b.public_order ?? 0));
    const handleBillingChange = useCallback((next) => {
        if (next === billing) {
            return;
        }
        setBilling(next);
        trackPublicEvent({ event: "pricing_toggle", billing: next });
    }, [billing]);
    const featureCards = [
        {
            title: "Connector-Based Routing",
            text: "Pair host agents once, then bind routes to connectors with explicit local target metadata.",
        },
        {
            title: "Tenant Isolation",
            text: "Route, connector, and access boundaries are tenant-scoped with role-aware controls.",
        },
        {
            title: "Traffic Governance",
            text: "Hard limits for RPS and monthly transfer keep usage deterministic under every plan.",
        },
        {
            title: "TLS + Cert Control",
            text: "Super admins manage certificates and active status from one control plane.",
        },
        {
            title: "Request Fidelity",
            text: "Methods, query, headers, cookies, and body are preserved end-to-end.",
        },
        {
            title: "Local-First Deploy",
            text: "Run everything with Docker Compose and no external dependencies for day one.",
        },
    ];
    return (_jsxs(_Fragment, { children: [_jsx("a", { className: "skip-link", href: "#main-marketing", children: "Skip to main content" }), _jsxs("main", { id: "main-marketing", className: "marketing-site", children: [_jsxs("section", { className: "marketing-hero-shell", "aria-labelledby": "hero-title", children: [_jsxs("header", { className: "marketing-nav", children: [_jsxs("div", { className: "hero-brand-wrap", children: [_jsx("div", { className: "hero-mark", children: "P" }), _jsxs("div", { children: [_jsx("strong", { children: "Proxer" }), _jsx("p", { children: "Public tunnels with governance" })] })] }), _jsxs("nav", { className: "marketing-links", "aria-label": "Marketing navigation", children: [_jsx("a", { href: "#features", children: "Features" }), _jsx("a", { href: "#pricing", children: "Plans" }), _jsx("a", { href: "#downloads", children: "Downloads" }), _jsx("a", { href: "#faq", children: "FAQ" })] }), _jsxs("div", { className: "hero-actions", children: [me ? _jsx(Link, { to: "/app", children: "Open Workspace" }) : _jsx(Link, { to: "/login", children: "Login" }), me ? null : (_jsx(Link, { className: "cta", to: "/signup", onClick: () => trackPublicEvent({ event: "signup_cta_click", outcome: "header" }), children: "Start Free" }))] })] }), _jsxs("div", { className: "hero-grid", children: [_jsxs("article", { className: "hero-copy", children: [_jsx("p", { className: "eyebrow", children: "HTTP/HTTPS localhost exposure for serious teams" }), _jsx("h1", { id: "hero-title", children: "Ship local apps to the internet with control-plane discipline." }), _jsx("p", { children: "Proxer combines ngrok-style reachability with role-based tenancy, hard plan enforcement, and operational visibility. Same development speed, better governance." }), _jsxs("div", { className: "hero-cta", children: [_jsx(Link, { className: "cta", to: me ? "/app" : "/signup", onClick: () => {
                                                            if (!me) {
                                                                trackPublicEvent({ event: "signup_cta_click", outcome: "hero" });
                                                            }
                                                        }, children: me ? "Open Workspace" : "Create Workspace" }), _jsx(Link, { className: "cta-outline", to: "/login", children: "Sign in" })] }), _jsxs("div", { className: "hero-proof-row", children: [_jsx("span", { children: "Protocol scope: HTTP/HTTPS" }), _jsx("span", { children: "Multi-tenant isolation" }), _jsx("span", { children: "Docker Compose ready" })] }), _jsxs("div", { className: "hero-proof-row", children: [_jsx("span", { children: "Request/response fidelity preserved" }), _jsx("span", { children: "Deterministic 4xx/5xx failure mapping" }), _jsx("span", { children: "Super-admin observability" })] })] }), _jsxs("aside", { className: "hero-terminal", children: [_jsxs("div", { className: "hero-terminal-head", children: [_jsx("span", {}), _jsx("span", {}), _jsx("span", {}), _jsx("p", { children: "live-route-preview" })] }), _jsxs("div", { className: "hero-terminal-body code", children: [_jsx("p", { children: "$ proxer-agent pair --token <pair_token>" }), _jsx("p", { children: "connector status: online" }), _jsx("p", { children: "route: /t/acme/api -> 127.0.0.1:3000" }), _jsx("p", { children: "tenant cap: 100 rps, 500 GB/month" }), _jsx("p", { children: "response: 200 OK (47 ms)" })] })] })] })] }), _jsxs("section", { id: "features", className: "marketing-panel", "aria-labelledby": "features-title", children: [_jsxs("header", { className: "section-head", children: [_jsx("p", { className: "eyebrow", children: "Why teams switch" }), _jsx("h2", { id: "features-title", children: "Built for production-minded local development" })] }), _jsx("div", { className: "feature-grid", children: featureCards.map((feature) => (_jsxs("article", { className: "feature-card", children: [_jsx("h3", { children: feature.title }), _jsx("p", { children: feature.text })] }, feature.title))) })] }), _jsxs("section", { className: "marketing-panel", "aria-labelledby": "workflow-title", children: [_jsxs("header", { className: "section-head", children: [_jsx("p", { className: "eyebrow", children: "How it works" }), _jsx("h2", { id: "workflow-title", children: "Three-step setup" })] }), _jsxs("div", { className: "steps-grid", children: [_jsxs("article", { children: [_jsx("span", { children: "1" }), _jsx("h3", { children: "Create connector" }), _jsx("p", { children: "Generate a short-lived pairing command from the workspace." })] }), _jsxs("article", { children: [_jsx("span", { children: "2" }), _jsx("h3", { children: "Pair host agent" }), _jsx("p", { children: "Run the desktop agent on your machine and establish a secure connector session." })] }), _jsxs("article", { children: [_jsx("span", { children: "3" }), _jsx("h3", { children: "Create route" }), _jsx("p", { children: "Bind public path to local target and start serving traffic instantly." })] })] })] }), _jsxs("section", { id: "pricing", className: "marketing-panel", "aria-labelledby": "pricing-title", children: [_jsxs("header", { className: "section-head split", children: [_jsxs("div", { children: [_jsx("p", { className: "eyebrow", children: "Pricing" }), _jsx("h2", { id: "pricing-title", children: "Choose the right operational envelope" })] }), _jsxs("div", { className: "billing-toggle", role: "group", "aria-label": "Billing cycle", children: [_jsx("button", { className: billing === "monthly" ? "active" : "", type: "button", onClick: () => handleBillingChange("monthly"), children: "Monthly" }), _jsx("button", { className: billing === "annual" ? "active" : "", type: "button", onClick: () => handleBillingChange("annual"), children: "Annual" })] })] }), loadingPlans ? _jsx("p", { role: "status", "aria-live": "polite", children: "Loading plans..." }) : null, planError ? _jsx("p", { className: "status error", children: planError }) : null, !loadingPlans && !planError ? (_jsx("div", { className: "plan-grid", children: orderedPlans.map((plan) => {
                                    const planID = String(plan.id || "").toLowerCase();
                                    const highlighted = planID === "pro";
                                    const rawPrice = billing === "annual" ? plan.price_annual_usd : plan.price_monthly_usd;
                                    const cadence = billing === "annual" ? "year" : "month";
                                    return (_jsxs("article", { className: `plan-card${highlighted ? " highlighted" : ""}`, children: [highlighted ? _jsx("span", { className: "plan-badge", children: "Most Popular" }) : null, _jsx("h3", { children: plan.name }), _jsxs("p", { className: "plan-price", children: ["$", formatNumber(rawPrice), " ", _jsxs("small", { children: ["/ ", cadence] })] }), _jsx("p", { className: "plan-subprice", children: plan.description || "Managed routing plan" }), _jsxs("ul", { children: [_jsxs("li", { children: [formatNumber(plan.max_routes), " routes"] }), _jsxs("li", { children: [formatNumber(plan.max_connectors), " connectors"] }), _jsxs("li", { children: [formatNumber(plan.max_rps), " requests / sec"] }), _jsxs("li", { children: [formatNumber(plan.max_monthly_gb), " GB transfer / month"] }), _jsx("li", { children: plan.tls_enabled ? "TLS certificates included" : "No TLS certificate management" })] }), _jsx(Link, { className: highlighted ? "cta" : "cta-outline", to: me ? "/app" : "/signup", onClick: () => {
                                                    if (!me) {
                                                        trackPublicEvent({ event: "plan_cta_click", plan_id: plan.id, billing });
                                                    }
                                                }, children: me ? "Use Plan" : "Start Free" })] }, plan.id));
                                }) })) : null] }), _jsxs("section", { id: "downloads", className: "marketing-panel", "aria-labelledby": "downloads-title", children: [_jsxs("header", { className: "section-head", children: [_jsx("p", { className: "eyebrow", children: "Desktop agent" }), _jsx("h2", { id: "downloads-title", children: "Download binaries for your host machine" })] }), loadingDownloads ? _jsx("p", { role: "status", "aria-live": "polite", children: "Loading downloads..." }) : null, downloadError ? _jsx("p", { className: "status error", children: downloadError }) : null, !loadingDownloads && !downloadError ? (_jsxs(_Fragment, { children: [downloads?.available ? (_jsx("div", { className: "download-grid", children: (downloads.downloads ?? []).map((binary) => (_jsxs("article", { className: "download-card", children: [_jsx("h3", { children: binary.label }), _jsx("p", { className: "code", children: binary.file_name }), _jsx("p", { children: formatBinarySize(binary.size_bytes) }), _jsx("a", { className: "cta-outline", href: binary.url, target: "_blank", rel: "noreferrer", onClick: () => trackPublicEvent({
                                                        event: "download_click",
                                                        platform: binary.platform,
                                                        file_name: binary.file_name,
                                                    }), children: "Download" })] }, `${binary.platform}:${binary.file_name}`))) })) : (_jsx("p", { children: downloads?.message || "Downloads are not available yet." })), _jsxs("div", { className: "download-meta", children: [downloads?.release_url ? (_jsx("a", { href: downloads.release_url, target: "_blank", rel: "noreferrer", children: "Release page" })) : null, downloads?.checksums_url ? (_jsx("a", { href: downloads.checksums_url, target: "_blank", rel: "noreferrer", children: "Checksums" })) : null, downloads?.release_notes_url ? (_jsx("a", { href: downloads.release_notes_url, target: "_blank", rel: "noreferrer", children: "Release notes" })) : null] })] })) : null] }), _jsxs("section", { id: "faq", className: "marketing-panel", "aria-labelledby": "faq-title", children: [_jsxs("header", { className: "section-head", children: [_jsx("p", { className: "eyebrow", children: "FAQ" }), _jsx("h2", { id: "faq-title", children: "Answers before you deploy" })] }), _jsxs("div", { className: "faq-grid", children: [_jsxs("article", { children: [_jsx("h3", { children: "Does Proxer return responses from localhost back to callers?" }), _jsx("p", { children: "Yes. Gateway dispatches to connector agent, agent calls localhost app, and response is returned." })] }), _jsxs("article", { children: [_jsx("h3", { children: "Can I run it entirely local?" }), _jsx("p", { children: "Yes. The full stack runs with Docker Compose and supports native desktop agents." })] }), _jsxs("article", { children: [_jsx("h3", { children: "How are limits enforced?" }), _jsx("p", { children: "Plan caps and runtime limits are hard enforced with deterministic error responses." })] }), _jsxs("article", { children: [_jsx("h3", { children: "Can super admins control plans and certificates?" }), _jsx("p", { children: "Yes. Super admin pages include users, tenants, plans, TLS certificates, and system status." })] })] })] }), _jsxs("section", { className: "marketing-final-cta", "aria-labelledby": "final-cta-title", children: [_jsx("h2", { id: "final-cta-title", children: "Ready to expose localhost with operational guardrails?" }), _jsx("p", { children: "Spin up a workspace in minutes and route your first endpoint to the public internet." }), _jsxs("div", { className: "hero-cta", children: [_jsx(Link, { className: "cta", to: me ? "/app" : "/signup", onClick: () => {
                                            if (!me) {
                                                trackPublicEvent({ event: "signup_cta_click", outcome: "footer" });
                                            }
                                        }, children: me ? "Open Workspace" : "Create Workspace" }), _jsx(Link, { className: "cta-outline", to: "/login", children: "Login" })] })] })] })] }));
}
export function App() {
    const location = useLocation();
    const [me, setMe] = useState(null);
    const [checkingAuth, setCheckingAuth] = useState(true);
    const refreshSession = useCallback(async () => {
        setCheckingAuth(true);
        try {
            const payload = await requestJSON("/api/auth/me");
            setMe(payload);
        }
        catch {
            setMe(null);
        }
        finally {
            setCheckingAuth(false);
        }
    }, []);
    useEffect(() => {
        const requiresSessionProbe = location.pathname.startsWith("/app");
        if (!requiresSessionProbe) {
            setCheckingAuth(false);
            return;
        }
        void refreshSession();
    }, [location.pathname, refreshSession]);
    const api = useCallback(async (path, init) => {
        try {
            return await requestJSON(path, init);
        }
        catch (error) {
            if (error instanceof ApiError && error.status === 401) {
                setMe(null);
            }
            throw error;
        }
    }, []);
    const logout = useCallback(async () => {
        try {
            await requestJSON("/api/auth/logout", { method: "POST" });
        }
        catch {
            // Ignore logout failures and clear local session anyway.
        }
        setMe(null);
    }, []);
    return (_jsxs(Routes, { children: [_jsx(Route, { path: "/", element: _jsx(LandingPage, { me: me }) }), _jsx(Route, { path: "/login", element: checkingAuth ? (_jsx("main", { className: "auth-shell", children: "Checking session..." })) : me ? (_jsx(Navigate, { to: "/app", replace: true })) : (_jsx(LoginView, { onLoggedIn: refreshSession })) }), _jsx(Route, { path: "/signup", element: checkingAuth ? (_jsx("main", { className: "auth-shell", children: "Checking session..." })) : me ? (_jsx(Navigate, { to: "/app", replace: true })) : (_jsx(SignupView, { onSignedUp: refreshSession })) }), _jsx(Route, { path: "/app/*", element: checkingAuth ? (_jsx("main", { className: "auth-shell", children: "Checking session..." })) : me ? (_jsx(Suspense, { fallback: _jsx("main", { className: "auth-shell", children: "Loading workspace..." }), children: _jsx(WorkspaceApp, { me: me, api: api, onLogout: logout }) })) : (_jsx(Navigate, { to: "/login", replace: true })) }), _jsx(Route, { path: "*", element: _jsx(Navigate, { to: "/", replace: true }) })] }));
}
