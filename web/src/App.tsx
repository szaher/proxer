import { FormEvent, Suspense, lazy, useCallback, useEffect, useState } from "react";
import { Link, Navigate, Route, Routes, useLocation, useNavigate } from "react-router-dom";

const WorkspaceApp = lazy(() => import("./workspace/WorkspaceApp"));

type Role = "super_admin" | "tenant_admin" | "member" | string;

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

interface PublicDownloadsPayload {
  source?: string;
  available?: boolean;
  repo?: string;
  tag?: string;
  release_url?: string;
  release_notes_url?: string;
  checksums_url?: string;
  message?: string;
  downloads?: Array<{
    platform: string;
    label: string;
    file_name: string;
    url: string;
    size_bytes?: number;
  }>;
}

interface PublicAnalyticsEvent {
  event: string;
  page_path?: string;
  plan_id?: string;
  billing?: string;
  platform?: string;
  file_name?: string;
  outcome?: string;
  source?: string;
}

class ApiError extends Error {
  status: number;

  payload: unknown;

  constructor(message: string, status: number, payload: unknown) {
    super(message);
    this.name = "ApiError";
    this.status = status;
    this.payload = payload;
  }
}

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

async function requestJSON<T>(path: string, init?: RequestInit): Promise<T> {
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
    return null as T;
  }

  const contentType = response.headers.get("content-type") ?? "";
  const asJSON = contentType.includes("application/json");
  const payload: unknown = asJSON ? await response.json() : await response.text();

  if (!response.ok) {
    let message = `Request failed (${response.status})`;
    if (typeof payload === "string" && payload.trim() !== "") {
      message = payload;
    } else if (isRecord(payload) && typeof payload.message === "string") {
      message = payload.message;
    }
    throw new ApiError(message, response.status, payload);
  }

  return payload as T;
}

function trackPublicEvent(event: PublicAnalyticsEvent): void {
  if (typeof window === "undefined" || typeof event.event !== "string" || event.event.trim() === "") {
    return;
  }

  const payload: PublicAnalyticsEvent = {
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
  } catch {
    // Fall back to fetch.
  }

  void fetch("/api/public/events", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: serialized,
    keepalive: true,
  }).catch(() => undefined);
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

function formatBinarySize(bytes: number | undefined): string {
  const numeric = Number(bytes ?? 0);
  if (!Number.isFinite(numeric) || numeric <= 0) {
    return "-";
  }
  if (numeric < 1024 * 1024) {
    return `${Math.round(numeric / 1024)} KB`;
  }
  return `${(numeric / (1024 * 1024)).toFixed(1)} MB`;
}

function LoginView({ onLoggedIn }: { onLoggedIn: () => Promise<void> }) {
  const navigate = useNavigate();
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [status, setStatus] = useState("");
  const [submitting, setSubmitting] = useState(false);

  const submit = useCallback(
    async (event: FormEvent<HTMLFormElement>) => {
      event.preventDefault();
      setSubmitting(true);
      setStatus("");
      try {
        await requestJSON<{ message: string }>("/api/auth/login", {
          method: "POST",
          body: JSON.stringify({ username, password }),
        });
        await onLoggedIn();
        navigate("/app");
      } catch (error: unknown) {
        setStatus(toErrorMessage(error));
      } finally {
        setSubmitting(false);
      }
    },
    [navigate, onLoggedIn, password, username]
  );

  return (
    <main className="auth-shell">
      <div className="orb orb-a" />
      <div className="orb orb-b" />
      <section className="auth-card">
        <h1>Proxer</h1>
        <p>Route internet traffic to localhost with tenant-scoped governance.</p>
        <form className="stack" onSubmit={submit}>
          <label>
            Username
            <input value={username} onChange={(event) => setUsername(event.target.value)} required />
          </label>
          <label>
            Password
            <input
              type="password"
              value={password}
              onChange={(event) => setPassword(event.target.value)}
              required
            />
          </label>
          <button type="submit" disabled={submitting}>
            {submitting ? "Logging in..." : "Login"}
          </button>
        </form>
        {status ? <p className="status error">{status}</p> : null}
        <div className="auth-links">
          <Link to="/signup">Create account</Link>
          <Link to="/">Back to website</Link>
        </div>
      </section>
    </main>
  );
}

function SignupView({ onSignedUp }: { onSignedUp: () => Promise<void> }) {
  const navigate = useNavigate();
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [confirmPassword, setConfirmPassword] = useState("");
  const [status, setStatus] = useState("");
  const [submitting, setSubmitting] = useState(false);

  const submit = useCallback(
    async (event: FormEvent<HTMLFormElement>) => {
      event.preventDefault();
      setStatus("");
      if (password !== confirmPassword) {
        setStatus("Passwords do not match.");
        return;
      }
      setSubmitting(true);
      trackPublicEvent({ event: "signup_submit", outcome: "attempt" });
      try {
        await requestJSON<{ message: string }>("/api/public/signup", {
          method: "POST",
          body: JSON.stringify({ username, password }),
        });
        trackPublicEvent({ event: "signup_success", outcome: "success" });
        await onSignedUp();
        navigate("/app");
      } catch (error: unknown) {
        setStatus(toErrorMessage(error));
        trackPublicEvent({ event: "signup_failure", outcome: "error" });
      } finally {
        setSubmitting(false);
      }
    },
    [confirmPassword, navigate, onSignedUp, password, username]
  );

  return (
    <main className="auth-shell">
      <div className="orb orb-a" />
      <div className="orb orb-b" />
      <section className="auth-card">
        <h1>Create Workspace</h1>
        <p>Get instant access with a tenant admin account on the free plan.</p>
        <form className="stack" onSubmit={submit}>
          <label>
            Username
            <input value={username} onChange={(event) => setUsername(event.target.value)} required />
          </label>
          <label>
            Password
            <input
              type="password"
              value={password}
              minLength={6}
              onChange={(event) => setPassword(event.target.value)}
              required
            />
          </label>
          <label>
            Confirm Password
            <input
              type="password"
              value={confirmPassword}
              minLength={6}
              onChange={(event) => setConfirmPassword(event.target.value)}
              required
            />
          </label>
          <button type="submit" disabled={submitting}>
            {submitting ? "Creating account..." : "Sign up"}
          </button>
        </form>
        {status ? <p className="status error">{status}</p> : null}
        <div className="auth-links">
          <Link to="/login">Already have an account?</Link>
          <Link to="/">Back to website</Link>
        </div>
      </section>
    </main>
  );
}

function LandingPage({ me }: { me: AuthMeResponse | null }) {
  const [plans, setPlans] = useState<Plan[]>([]);
  const [downloads, setDownloads] = useState<PublicDownloadsPayload | null>(null);
  const [loadingPlans, setLoadingPlans] = useState(true);
  const [loadingDownloads, setLoadingDownloads] = useState(true);
  const [planError, setPlanError] = useState("");
  const [downloadError, setDownloadError] = useState("");
  const [billing, setBilling] = useState<"monthly" | "annual">("monthly");

  useEffect(() => {
    trackPublicEvent({ event: "landing_view" });
    let mounted = true;
    const loadPlans = async () => {
      setLoadingPlans(true);
      setPlanError("");
      try {
        const payload = await requestJSON<{ plans: Plan[] }>("/api/public/plans");
        if (mounted) {
          setPlans(payload.plans ?? []);
        }
      } catch (error: unknown) {
        if (mounted) {
          setPlanError(toErrorMessage(error));
        }
      } finally {
        if (mounted) {
          setLoadingPlans(false);
        }
      }
    };
    const loadDownloads = async () => {
      setLoadingDownloads(true);
      setDownloadError("");
      try {
        const payload = await requestJSON<PublicDownloadsPayload>("/api/public/downloads");
        if (mounted) {
          setDownloads(payload);
        }
      } catch (error: unknown) {
        if (mounted) {
          setDownloadError(toErrorMessage(error));
        }
      } finally {
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
  const handleBillingChange = useCallback(
    (next: "monthly" | "annual") => {
      if (next === billing) {
        return;
      }
      setBilling(next);
      trackPublicEvent({ event: "pricing_toggle", billing: next });
    },
    [billing]
  );

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

  return (
    <>
      <a className="skip-link" href="#main-marketing">
        Skip to main content
      </a>
      <main id="main-marketing" className="marketing-site">
        <section className="marketing-hero-shell" aria-labelledby="hero-title">
        <header className="marketing-nav">
          <div className="hero-brand-wrap">
            <div className="hero-mark">P</div>
            <div>
              <strong>Proxer</strong>
              <p>Public tunnels with governance</p>
            </div>
          </div>
          <nav className="marketing-links" aria-label="Marketing navigation">
            <a href="#features">Features</a>
            <a href="#pricing">Plans</a>
            <a href="#downloads">Downloads</a>
            <a href="#faq">FAQ</a>
          </nav>
          <div className="hero-actions">
            {me ? <Link to="/app">Open Workspace</Link> : <Link to="/login">Login</Link>}
            {me ? null : (
              <Link
                className="cta"
                to="/signup"
                onClick={() => trackPublicEvent({ event: "signup_cta_click", outcome: "header" })}
              >
                Start Free
              </Link>
            )}
          </div>
        </header>

        <div className="hero-grid">
          <article className="hero-copy">
            <p className="eyebrow">HTTP/HTTPS localhost exposure for serious teams</p>
            <h1 id="hero-title">Ship local apps to the internet with control-plane discipline.</h1>
            <p>
              Proxer combines ngrok-style reachability with role-based tenancy, hard plan enforcement, and operational
              visibility. Same development speed, better governance.
            </p>
            <div className="hero-cta">
              <Link
                className="cta"
                to={me ? "/app" : "/signup"}
                onClick={() => {
                  if (!me) {
                    trackPublicEvent({ event: "signup_cta_click", outcome: "hero" });
                  }
                }}
              >
                {me ? "Open Workspace" : "Create Workspace"}
              </Link>
              <Link className="cta-outline" to="/login">
                Sign in
              </Link>
            </div>
            <div className="hero-proof-row">
              <span>Protocol scope: HTTP/HTTPS</span>
              <span>Multi-tenant isolation</span>
              <span>Docker Compose ready</span>
            </div>
            <div className="hero-proof-row">
              <span>Request/response fidelity preserved</span>
              <span>Deterministic 4xx/5xx failure mapping</span>
              <span>Super-admin observability</span>
            </div>
          </article>

          <aside className="hero-terminal">
            <div className="hero-terminal-head">
              <span />
              <span />
              <span />
              <p>live-route-preview</p>
            </div>
            <div className="hero-terminal-body code">
              <p>$ proxer-agent pair --token &lt;pair_token&gt;</p>
              <p>connector status: online</p>
              <p>route: /t/acme/api -&gt; 127.0.0.1:3000</p>
              <p>tenant cap: 100 rps, 500 GB/month</p>
              <p>response: 200 OK (47 ms)</p>
            </div>
          </aside>
        </div>
      </section>

      <section id="features" className="marketing-panel" aria-labelledby="features-title">
        <header className="section-head">
          <p className="eyebrow">Why teams switch</p>
          <h2 id="features-title">Built for production-minded local development</h2>
        </header>
        <div className="feature-grid">
          {featureCards.map((feature) => (
            <article key={feature.title} className="feature-card">
              <h3>{feature.title}</h3>
              <p>{feature.text}</p>
            </article>
          ))}
        </div>
      </section>

      <section className="marketing-panel" aria-labelledby="workflow-title">
        <header className="section-head">
          <p className="eyebrow">How it works</p>
          <h2 id="workflow-title">Three-step setup</h2>
        </header>
        <div className="steps-grid">
          <article>
            <span>1</span>
            <h3>Create connector</h3>
            <p>Generate a short-lived pairing command from the workspace.</p>
          </article>
          <article>
            <span>2</span>
            <h3>Pair host agent</h3>
            <p>Run the desktop agent on your machine and establish a secure connector session.</p>
          </article>
          <article>
            <span>3</span>
            <h3>Create route</h3>
            <p>Bind public path to local target and start serving traffic instantly.</p>
          </article>
        </div>
      </section>

      <section id="pricing" className="marketing-panel" aria-labelledby="pricing-title">
        <header className="section-head split">
          <div>
            <p className="eyebrow">Pricing</p>
            <h2 id="pricing-title">Choose the right operational envelope</h2>
          </div>
          <div className="billing-toggle" role="group" aria-label="Billing cycle">
            <button
              className={billing === "monthly" ? "active" : ""}
              type="button"
              onClick={() => handleBillingChange("monthly")}
            >
              Monthly
            </button>
            <button
              className={billing === "annual" ? "active" : ""}
              type="button"
              onClick={() => handleBillingChange("annual")}
            >
              Annual
            </button>
          </div>
        </header>
        {loadingPlans ? <p role="status" aria-live="polite">Loading plans...</p> : null}
        {planError ? <p className="status error">{planError}</p> : null}
        {!loadingPlans && !planError ? (
          <div className="plan-grid">
            {orderedPlans.map((plan) => {
              const planID = String(plan.id || "").toLowerCase();
              const highlighted = planID === "pro";
              const rawPrice = billing === "annual" ? plan.price_annual_usd : plan.price_monthly_usd;
              const cadence = billing === "annual" ? "year" : "month";
              return (
                <article key={plan.id} className={`plan-card${highlighted ? " highlighted" : ""}`}>
                  {highlighted ? <span className="plan-badge">Most Popular</span> : null}
                  <h3>{plan.name}</h3>
                  <p className="plan-price">
                    ${formatNumber(rawPrice)} <small>/ {cadence}</small>
                  </p>
                  <p className="plan-subprice">{plan.description || "Managed routing plan"}</p>
                  <ul>
                    <li>{formatNumber(plan.max_routes)} routes</li>
                    <li>{formatNumber(plan.max_connectors)} connectors</li>
                    <li>{formatNumber(plan.max_rps)} requests / sec</li>
                    <li>{formatNumber(plan.max_monthly_gb)} GB transfer / month</li>
                    <li>{plan.tls_enabled ? "TLS certificates included" : "No TLS certificate management"}</li>
                  </ul>
                  <Link
                    className={highlighted ? "cta" : "cta-outline"}
                    to={me ? "/app" : "/signup"}
                    onClick={() => {
                      if (!me) {
                        trackPublicEvent({ event: "plan_cta_click", plan_id: plan.id, billing });
                      }
                    }}
                  >
                    {me ? "Use Plan" : "Start Free"}
                  </Link>
                </article>
              );
            })}
          </div>
        ) : null}
      </section>

      <section id="downloads" className="marketing-panel" aria-labelledby="downloads-title">
        <header className="section-head">
          <p className="eyebrow">Desktop agent</p>
          <h2 id="downloads-title">Download binaries for your host machine</h2>
        </header>
        {loadingDownloads ? <p role="status" aria-live="polite">Loading downloads...</p> : null}
        {downloadError ? <p className="status error">{downloadError}</p> : null}
        {!loadingDownloads && !downloadError ? (
          <>
            {downloads?.available ? (
              <div className="download-grid">
                {(downloads.downloads ?? []).map((binary) => (
                  <article key={`${binary.platform}:${binary.file_name}`} className="download-card">
                    <h3>{binary.label}</h3>
                    <p className="code">{binary.file_name}</p>
                    <p>{formatBinarySize(binary.size_bytes)}</p>
                    <a
                      className="cta-outline"
                      href={binary.url}
                      target="_blank"
                      rel="noreferrer"
                      onClick={() =>
                        trackPublicEvent({
                          event: "download_click",
                          platform: binary.platform,
                          file_name: binary.file_name,
                        })
                      }
                    >
                      Download
                    </a>
                  </article>
                ))}
              </div>
            ) : (
              <p>{downloads?.message || "Downloads are not available yet."}</p>
            )}
            <div className="download-meta">
              {downloads?.release_url ? (
                <a href={downloads.release_url} target="_blank" rel="noreferrer">
                  Release page
                </a>
              ) : null}
              {downloads?.checksums_url ? (
                <a href={downloads.checksums_url} target="_blank" rel="noreferrer">
                  Checksums
                </a>
              ) : null}
              {downloads?.release_notes_url ? (
                <a href={downloads.release_notes_url} target="_blank" rel="noreferrer">
                  Release notes
                </a>
              ) : null}
            </div>
          </>
        ) : null}
      </section>

      <section id="faq" className="marketing-panel" aria-labelledby="faq-title">
        <header className="section-head">
          <p className="eyebrow">FAQ</p>
          <h2 id="faq-title">Answers before you deploy</h2>
        </header>
        <div className="faq-grid">
          <article>
            <h3>Does Proxer return responses from localhost back to callers?</h3>
            <p>Yes. Gateway dispatches to connector agent, agent calls localhost app, and response is returned.</p>
          </article>
          <article>
            <h3>Can I run it entirely local?</h3>
            <p>Yes. The full stack runs with Docker Compose and supports native desktop agents.</p>
          </article>
          <article>
            <h3>How are limits enforced?</h3>
            <p>Plan caps and runtime limits are hard enforced with deterministic error responses.</p>
          </article>
          <article>
            <h3>Can super admins control plans and certificates?</h3>
            <p>Yes. Super admin pages include users, tenants, plans, TLS certificates, and system status.</p>
          </article>
        </div>
      </section>

      <section className="marketing-final-cta" aria-labelledby="final-cta-title">
        <h2 id="final-cta-title">Ready to expose localhost with operational guardrails?</h2>
        <p>Spin up a workspace in minutes and route your first endpoint to the public internet.</p>
        <div className="hero-cta">
          <Link
            className="cta"
            to={me ? "/app" : "/signup"}
            onClick={() => {
              if (!me) {
                trackPublicEvent({ event: "signup_cta_click", outcome: "footer" });
              }
            }}
          >
            {me ? "Open Workspace" : "Create Workspace"}
          </Link>
          <Link className="cta-outline" to="/login">
            Login
          </Link>
        </div>
      </section>
      </main>
    </>
  );
}

export function App() {
  const location = useLocation();
  const [me, setMe] = useState<AuthMeResponse | null>(null);
  const [checkingAuth, setCheckingAuth] = useState(true);

  const refreshSession = useCallback(async () => {
    setCheckingAuth(true);
    try {
      const payload = await requestJSON<AuthMeResponse>("/api/auth/me");
      setMe(payload);
    } catch {
      setMe(null);
    } finally {
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

  const api = useCallback(
    async <T,>(path: string, init?: RequestInit): Promise<T> => {
      try {
        return await requestJSON<T>(path, init);
      } catch (error: unknown) {
        if (error instanceof ApiError && error.status === 401) {
          setMe(null);
        }
        throw error;
      }
    },
    []
  );

  const logout = useCallback(async () => {
    try {
      await requestJSON<{ message: string }>("/api/auth/logout", { method: "POST" });
    } catch {
      // Ignore logout failures and clear local session anyway.
    }
    setMe(null);
  }, []);

  return (
    <Routes>
      <Route path="/" element={<LandingPage me={me} />} />
      <Route
        path="/login"
        element={
          checkingAuth ? (
            <main className="auth-shell">Checking session...</main>
          ) : me ? (
            <Navigate to="/app" replace />
          ) : (
            <LoginView onLoggedIn={refreshSession} />
          )
        }
      />
      <Route
        path="/signup"
        element={
          checkingAuth ? (
            <main className="auth-shell">Checking session...</main>
          ) : me ? (
            <Navigate to="/app" replace />
          ) : (
            <SignupView onSignedUp={refreshSession} />
          )
        }
      />
      <Route
        path="/app/*"
        element={
          checkingAuth ? (
            <main className="auth-shell">Checking session...</main>
          ) : me ? (
            <Suspense fallback={<main className="auth-shell">Loading workspace...</main>}>
              <WorkspaceApp me={me} api={api} onLogout={logout} />
            </Suspense>
          ) : (
            <Navigate to="/login" replace />
          )
        }
      />
      <Route path="*" element={<Navigate to="/" replace />} />
    </Routes>
  );
}
