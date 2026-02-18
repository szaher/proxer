import { FormEvent, useEffect, useMemo, useState } from "react";

type RuntimeState =
  | "stopped"
  | "starting"
  | "running"
  | "pairing"
  | "degraded"
  | "error"
  | "stopping"
  | string;

interface AgentSettings {
  schema_version: number;
  active_profile_id?: string;
  start_at_login: boolean;
  launch_mode: string;
}

interface TunnelConfig {
  id?: string;
  target_url?: string;
  token?: string;
}

interface RuntimeOptions {
  request_timeout: string;
  poll_wait: string;
  heartbeat_interval: string;
  max_response_body_bytes: number;
  proxy_url?: string;
  no_proxy?: string;
  tls_skip_verify: boolean;
  ca_file?: string;
  log_level: string;
}

interface AgentProfile {
  id: string;
  name: string;
  gateway_base_url: string;
  agent_id: string;
  mode: "connector" | "legacy_tunnels" | string;
  connector_id?: string;
  runtime: RuntimeOptions;
  legacy_tunnels?: TunnelConfig[];
  created_at?: string;
  updated_at?: string;
}

interface NativeStatusSnapshot {
  state: RuntimeState;
  message?: string;
  error?: string;
  profile_id?: string;
  profile_name?: string;
  agent_id?: string;
  session_id?: string;
  mode?: string;
  updated_at?: string;
}

interface UpdateCheckResult {
  current_version: string;
  latest_version?: string;
  download_url?: string;
  message: string;
}

interface ProfileFormState {
  name: string;
  gateway_base_url: string;
  agent_id: string;
  mode: "connector" | "legacy_tunnels";
  connector_id: string;
  connector_secret: string;
  agent_token: string;
  legacy_tunnels: string;
  request_timeout: string;
  poll_wait: string;
  heartbeat_interval: string;
  max_response_body_bytes: string;
  proxy_url: string;
  no_proxy: string;
  tls_skip_verify: boolean;
  ca_file: string;
  log_level: string;
}

interface ApiErrorPayload {
  error?: string;
}

const defaultForm = (): ProfileFormState => ({
  name: "",
  gateway_base_url: "http://127.0.0.1:18080",
  agent_id: "",
  mode: "connector",
  connector_id: "",
  connector_secret: "",
  agent_token: "",
  legacy_tunnels: "",
  request_timeout: "45s",
  poll_wait: "25s",
  heartbeat_interval: "10s",
  max_response_body_bytes: String(20 << 20),
  proxy_url: "",
  no_proxy: "",
  tls_skip_verify: false,
  ca_file: "",
  log_level: "info",
});

function profileToForm(profile: AgentProfile): ProfileFormState {
  return {
    name: profile.name,
    gateway_base_url: profile.gateway_base_url,
    agent_id: profile.agent_id,
    mode: profile.mode === "legacy_tunnels" ? "legacy_tunnels" : "connector",
    connector_id: profile.connector_id ?? "",
    connector_secret: "",
    agent_token: "",
    legacy_tunnels: tunnelsToString(profile.legacy_tunnels),
    request_timeout: profile.runtime?.request_timeout ?? "45s",
    poll_wait: profile.runtime?.poll_wait ?? "25s",
    heartbeat_interval: profile.runtime?.heartbeat_interval ?? "10s",
    max_response_body_bytes: String(profile.runtime?.max_response_body_bytes ?? 20 << 20),
    proxy_url: profile.runtime?.proxy_url ?? "",
    no_proxy: profile.runtime?.no_proxy ?? "",
    tls_skip_verify: Boolean(profile.runtime?.tls_skip_verify),
    ca_file: profile.runtime?.ca_file ?? "",
    log_level: profile.runtime?.log_level ?? "info",
  };
}

function tunnelsToString(tunnels: TunnelConfig[] | undefined): string {
  if (!Array.isArray(tunnels) || tunnels.length === 0) {
    return "";
  }
  return tunnels
    .map((tunnel) => {
      const id = String(tunnel.id ?? "").trim();
      const target = String(tunnel.target_url ?? "").trim();
      const token = String(tunnel.token ?? "").trim();
      if (!id || !target) {
        return "";
      }
      if (token) {
        return `${id}@${token}=${target}`;
      }
      return `${id}=${target}`;
    })
    .filter((entry) => entry !== "")
    .join(",");
}

async function apiRequest<T>(path: string, init?: RequestInit): Promise<T> {
  const headers = new Headers(init?.headers ?? {});
  if (init?.body !== undefined && !headers.has("Content-Type")) {
    headers.set("Content-Type", "application/json");
  }

  const response = await fetch(path, {
    ...init,
    headers,
  });

  const contentType = response.headers.get("content-type") ?? "";
  const isJSON = contentType.includes("application/json");
  const payload = isJSON
    ? ((await response.json()) as unknown)
    : ((await response.text()) as unknown);

  if (!response.ok) {
    let message = `Request failed (${response.status})`;
    if (typeof payload === "string" && payload.trim()) {
      message = payload;
    } else if (typeof payload === "object" && payload !== null) {
      const body = payload as ApiErrorPayload;
      if (typeof body.error === "string" && body.error.trim()) {
        message = body.error;
      }
    }
    throw new Error(message);
  }

  return payload as T;
}

function stateClassName(state: RuntimeState): string {
  const value = String(state || "stopped").toLowerCase();
  if (value === "running") return "status running";
  if (value === "degraded") return "status degraded";
  if (value === "error") return "status error";
  return "status idle";
}

function formatDate(value: string | undefined): string {
  if (!value) {
    return "-";
  }
  const parsed = new Date(value);
  if (Number.isNaN(parsed.getTime())) {
    return value;
  }
  return parsed.toLocaleString();
}

export function App() {
  const [settings, setSettings] = useState<AgentSettings | null>(null);
  const [profiles, setProfiles] = useState<AgentProfile[]>([]);
  const [status, setStatus] = useState<NativeStatusSnapshot>({ state: "stopped" });
  const [logs, setLogs] = useState<string[]>([]);
  const [form, setForm] = useState<ProfileFormState>(defaultForm());
  const [editingProfileID, setEditingProfileID] = useState<string>("");
  const [runtimeProfile, setRuntimeProfile] = useState<string>("");
  const [pairProfile, setPairProfile] = useState<string>("");
  const [pairToken, setPairToken] = useState<string>("");
  const [updateResult, setUpdateResult] = useState<UpdateCheckResult | null>(null);
  const [errorMessage, setErrorMessage] = useState<string>("");
  const [successMessage, setSuccessMessage] = useState<string>("");
  const [loading, setLoading] = useState<boolean>(true);

  const activeProfile = useMemo(() => {
    if (!settings?.active_profile_id) {
      return null;
    }
    return profiles.find((profile) => profile.id === settings.active_profile_id) ?? null;
  }, [profiles, settings?.active_profile_id]);

  const setError = (message: string) => {
    setErrorMessage(message);
    setSuccessMessage("");
  };

  const setSuccess = (message: string) => {
    setSuccessMessage(message);
    setErrorMessage("");
  };

  const refreshStatus = async () => {
    const data = await apiRequest<NativeStatusSnapshot>("/api/status");
    setStatus(data);
  };

  const refreshSettingsAndProfiles = async () => {
    const [nextSettings, nextProfiles] = await Promise.all([
      apiRequest<AgentSettings>("/api/settings"),
      apiRequest<AgentProfile[]>("/api/profiles"),
    ]);
    setSettings(nextSettings);
    setProfiles(nextProfiles);

    if (!pairProfile && nextSettings.active_profile_id) {
      setPairProfile(nextSettings.active_profile_id);
    }
  };

  const refreshLogs = async () => {
    const response = await apiRequest<{ lines: string[] }>("/api/logs?tail=250");
    setLogs(response.lines ?? []);
  };

  const refreshAll = async () => {
    setLoading(true);
    try {
      await Promise.all([refreshSettingsAndProfiles(), refreshStatus(), refreshLogs()]);
      setErrorMessage("");
    } catch (error) {
      setError(error instanceof Error ? error.message : "Failed to refresh data");
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    void refreshAll();
  }, []);

  useEffect(() => {
    const source = new EventSource("/api/events/runtime");
    source.addEventListener("runtime", (event) => {
      try {
        const payload = JSON.parse(event.data) as NativeStatusSnapshot;
        setStatus(payload);
      } catch {
        // Ignore malformed events and keep SSE alive.
      }
    });
    source.onerror = () => {
      source.close();
      setTimeout(() => {
        void refreshStatus();
      }, 1500);
    };
    return () => {
      source.close();
    };
  }, []);

  useEffect(() => {
    const interval = window.setInterval(() => {
      void refreshLogs();
    }, 8000);
    return () => window.clearInterval(interval);
  }, []);

  const clearForm = () => {
    setEditingProfileID("");
    setForm(defaultForm());
  };

  const handleProfileSubmit = async (event: FormEvent) => {
    event.preventDefault();
    setErrorMessage("");
    setSuccessMessage("");

    if (!form.name.trim()) {
      setError("Profile name is required");
      return;
    }

    const maxBytes = Number.parseInt(form.max_response_body_bytes, 10);
    if (!Number.isFinite(maxBytes) || maxBytes <= 0) {
      setError("Max response body bytes must be a positive integer");
      return;
    }

    const payload = {
      name: form.name.trim(),
      gateway_base_url: form.gateway_base_url.trim(),
      agent_id: form.agent_id.trim(),
      mode: form.mode,
      connector_id: form.connector_id.trim(),
      connector_secret: form.connector_secret.trim(),
      agent_token: form.agent_token.trim(),
      legacy_tunnels: form.legacy_tunnels.trim(),
      runtime: {
        request_timeout: form.request_timeout.trim(),
        poll_wait: form.poll_wait.trim(),
        heartbeat_interval: form.heartbeat_interval.trim(),
        max_response_body_bytes: maxBytes,
        proxy_url: form.proxy_url.trim(),
        no_proxy: form.no_proxy.trim(),
        tls_skip_verify: form.tls_skip_verify,
        ca_file: form.ca_file.trim(),
        log_level: form.log_level.trim(),
      },
    };

    try {
      if (editingProfileID) {
        await apiRequest<AgentProfile>(`/api/profiles/${encodeURIComponent(editingProfileID)}`, {
          method: "PUT",
          body: JSON.stringify(payload),
        });
        setSuccess(`Profile updated: ${form.name.trim()}`);
      } else {
        const created = await apiRequest<AgentProfile>("/api/profiles", {
          method: "POST",
          body: JSON.stringify(payload),
        });
        setPairProfile(created.id);
        setSuccess(`Profile created: ${created.name}`);
      }
      clearForm();
      await refreshSettingsAndProfiles();
    } catch (error) {
      setError(error instanceof Error ? error.message : "Failed to save profile");
    }
  };

  const handleUseProfile = async (profileID: string) => {
    try {
      await apiRequest<AgentProfile>(`/api/profiles/${encodeURIComponent(profileID)}/use`, {
        method: "POST",
      });
      await refreshSettingsAndProfiles();
      setSuccess("Active profile updated");
    } catch (error) {
      setError(error instanceof Error ? error.message : "Failed to set active profile");
    }
  };

  const handleDeleteProfile = async (profileID: string) => {
    if (!window.confirm("Delete this profile?")) {
      return;
    }
    try {
      await apiRequest<{ deleted: boolean }>(`/api/profiles/${encodeURIComponent(profileID)}`, {
        method: "DELETE",
      });
      if (editingProfileID === profileID) {
        clearForm();
      }
      await refreshSettingsAndProfiles();
      setSuccess("Profile deleted");
    } catch (error) {
      setError(error instanceof Error ? error.message : "Failed to delete profile");
    }
  };

  const handleEditProfile = (profile: AgentProfile) => {
    setEditingProfileID(profile.id);
    setForm(profileToForm(profile));
    setErrorMessage("");
    setSuccessMessage("");
  };

  const handlePair = async (event: FormEvent) => {
    event.preventDefault();
    if (!pairProfile.trim()) {
      setError("Profile is required for pairing");
      return;
    }
    if (!pairToken.trim()) {
      setError("Pair token is required");
      return;
    }
    try {
      const updated = await apiRequest<AgentProfile>(
        `/api/profiles/${encodeURIComponent(pairProfile.trim())}/pair`,
        {
          method: "POST",
          body: JSON.stringify({ pair_token: pairToken.trim() }),
        }
      );
      setPairToken("");
      setSuccess(`Pairing complete for profile ${updated.name}`);
      await refreshSettingsAndProfiles();
    } catch (error) {
      setError(error instanceof Error ? error.message : "Pairing failed");
    }
  };

  const handleStart = async () => {
    try {
      await apiRequest<NativeStatusSnapshot>("/api/runtime/start", {
        method: "POST",
        body: JSON.stringify({ profile: runtimeProfile.trim() }),
      });
      await Promise.all([refreshStatus(), refreshLogs()]);
      setSuccess("Runtime started");
    } catch (error) {
      setError(error instanceof Error ? error.message : "Failed to start runtime");
    }
  };

  const handleStop = async () => {
    try {
      await apiRequest<NativeStatusSnapshot>("/api/runtime/stop", {
        method: "POST",
      });
      await Promise.all([refreshStatus(), refreshLogs()]);
      setSuccess("Runtime stopped");
    } catch (error) {
      setError(error instanceof Error ? error.message : "Failed to stop runtime");
    }
  };

  const handleSaveSettings = async (event: FormEvent) => {
    event.preventDefault();
    if (!settings) {
      return;
    }
    try {
      const updated = await apiRequest<AgentSettings>("/api/settings", {
        method: "PUT",
        body: JSON.stringify({
          launch_mode: settings.launch_mode,
          start_at_login: settings.start_at_login,
        }),
      });
      setSettings(updated);
      setSuccess("Settings saved");
    } catch (error) {
      setError(error instanceof Error ? error.message : "Failed to save settings");
    }
  };

  const handleCheckUpdates = async () => {
    try {
      const data = await apiRequest<UpdateCheckResult>("/api/update/check");
      setUpdateResult(data);
      setSuccess("Update check completed");
    } catch (error) {
      setError(error instanceof Error ? error.message : "Update check failed");
    }
  };

  return (
    <main className="layout">
      <header className="hero">
        <div>
          <p className="eyebrow">Proxer Native Agent</p>
          <h1>Desktop Control Plane</h1>
          <p className="muted">Manage profiles, pair connectors, and control runtime state from one app.</p>
        </div>
        <button className="btn secondary" onClick={() => void refreshAll()} disabled={loading}>
          {loading ? "Refreshing..." : "Refresh"}
        </button>
      </header>

      {errorMessage && <section className="notice error">{errorMessage}</section>}
      {successMessage && <section className="notice success">{successMessage}</section>}

      <section className="panel runtime-panel">
        <div className="runtime-header">
          <div>
            <h2>Runtime</h2>
            <p className="muted">
              Active profile: <strong>{activeProfile?.name ?? "-"}</strong>
            </p>
          </div>
          <span className={stateClassName(status.state)}>{status.state || "stopped"}</span>
        </div>

        <div className="runtime-details">
          <div>
            <span>Mode</span>
            <strong>{status.mode || "-"}</strong>
          </div>
          <div>
            <span>Agent ID</span>
            <strong>{status.agent_id || "-"}</strong>
          </div>
          <div>
            <span>Session</span>
            <strong>{status.session_id || "-"}</strong>
          </div>
          <div>
            <span>Updated</span>
            <strong>{formatDate(status.updated_at)}</strong>
          </div>
        </div>

        <div className="runtime-actions">
          <input
            value={runtimeProfile}
            onChange={(event) => setRuntimeProfile(event.target.value)}
            placeholder="Profile ID or name (optional)"
          />
          <button className="btn" onClick={handleStart}>
            Start
          </button>
          <button className="btn secondary" onClick={handleStop}>
            Stop
          </button>
        </div>

        {(status.message || status.error) && (
          <div className="runtime-extra">
            {status.message && <p>{status.message}</p>}
            {status.error && <p className="error-text">{status.error}</p>}
          </div>
        )}
      </section>

      <section className="grid two">
        <article className="panel">
          <h2>{editingProfileID ? "Edit Profile" : "Create Profile"}</h2>
          <form className="form-grid" onSubmit={handleProfileSubmit}>
            <label>
              Name
              <input
                value={form.name}
                onChange={(event) => setForm((prev) => ({ ...prev, name: event.target.value }))}
                required
              />
            </label>
            <label>
              Gateway URL
              <input
                value={form.gateway_base_url}
                onChange={(event) => setForm((prev) => ({ ...prev, gateway_base_url: event.target.value }))}
                required
              />
            </label>
            <label>
              Agent ID
              <input
                value={form.agent_id}
                onChange={(event) => setForm((prev) => ({ ...prev, agent_id: event.target.value }))}
                required
              />
            </label>
            <label>
              Mode
              <select
                value={form.mode}
                onChange={(event) =>
                  setForm((prev) => ({
                    ...prev,
                    mode: event.target.value === "legacy_tunnels" ? "legacy_tunnels" : "connector",
                  }))
                }
              >
                <option value="connector">connector</option>
                <option value="legacy_tunnels">legacy_tunnels</option>
              </select>
            </label>
            <label>
              Connector ID
              <input
                value={form.connector_id}
                onChange={(event) => setForm((prev) => ({ ...prev, connector_id: event.target.value }))}
              />
            </label>
            <label>
              Connector Secret
              <input
                type="password"
                value={form.connector_secret}
                onChange={(event) => setForm((prev) => ({ ...prev, connector_secret: event.target.value }))}
              />
            </label>
            <label>
              Legacy Agent Token
              <input
                type="password"
                value={form.agent_token}
                onChange={(event) => setForm((prev) => ({ ...prev, agent_token: event.target.value }))}
              />
            </label>
            <label className="full-width">
              Legacy Tunnels (id=url,id2@token=url)
              <textarea
                value={form.legacy_tunnels}
                onChange={(event) => setForm((prev) => ({ ...prev, legacy_tunnels: event.target.value }))}
              />
            </label>

            <label>
              Request Timeout
              <input
                value={form.request_timeout}
                onChange={(event) => setForm((prev) => ({ ...prev, request_timeout: event.target.value }))}
              />
            </label>
            <label>
              Poll Wait
              <input
                value={form.poll_wait}
                onChange={(event) => setForm((prev) => ({ ...prev, poll_wait: event.target.value }))}
              />
            </label>
            <label>
              Heartbeat Interval
              <input
                value={form.heartbeat_interval}
                onChange={(event) => setForm((prev) => ({ ...prev, heartbeat_interval: event.target.value }))}
              />
            </label>
            <label>
              Max Response Bytes
              <input
                value={form.max_response_body_bytes}
                onChange={(event) =>
                  setForm((prev) => ({ ...prev, max_response_body_bytes: event.target.value }))
                }
              />
            </label>
            <label>
              Proxy URL
              <input
                value={form.proxy_url}
                onChange={(event) => setForm((prev) => ({ ...prev, proxy_url: event.target.value }))}
              />
            </label>
            <label>
              No Proxy
              <input
                value={form.no_proxy}
                onChange={(event) => setForm((prev) => ({ ...prev, no_proxy: event.target.value }))}
              />
            </label>
            <label>
              CA File
              <input
                value={form.ca_file}
                onChange={(event) => setForm((prev) => ({ ...prev, ca_file: event.target.value }))}
              />
            </label>
            <label>
              Log Level
              <input
                value={form.log_level}
                onChange={(event) => setForm((prev) => ({ ...prev, log_level: event.target.value }))}
              />
            </label>
            <label className="checkbox">
              <input
                type="checkbox"
                checked={form.tls_skip_verify}
                onChange={(event) =>
                  setForm((prev) => ({ ...prev, tls_skip_verify: event.target.checked }))
                }
              />
              TLS Skip Verify
            </label>

            <div className="actions full-width">
              <button className="btn" type="submit">
                {editingProfileID ? "Save Profile" : "Create Profile"}
              </button>
              {editingProfileID && (
                <button className="btn secondary" type="button" onClick={clearForm}>
                  Cancel Edit
                </button>
              )}
            </div>
          </form>
        </article>

        <article className="panel stacked">
          <section>
            <h2>Pair Connector</h2>
            <form className="form-grid" onSubmit={handlePair}>
              <label>
                Profile ID or Name
                <input
                  value={pairProfile}
                  onChange={(event) => setPairProfile(event.target.value)}
                  required
                />
              </label>
              <label>
                Pair Token
                <input
                  value={pairToken}
                  onChange={(event) => setPairToken(event.target.value)}
                  required
                />
              </label>
              <div className="actions full-width">
                <button className="btn" type="submit">
                  Pair Profile
                </button>
              </div>
            </form>
          </section>

          <section>
            <h2>App Settings</h2>
            <form className="form-grid" onSubmit={handleSaveSettings}>
              <label>
                Launch Mode
                <input
                  value={settings?.launch_mode ?? "tray_window"}
                  onChange={(event) =>
                    setSettings((prev) => {
                      if (!prev) {
                        return prev;
                      }
                      return { ...prev, launch_mode: event.target.value };
                    })
                  }
                />
              </label>
              <label className="checkbox">
                <input
                  type="checkbox"
                  checked={Boolean(settings?.start_at_login)}
                  onChange={(event) =>
                    setSettings((prev) => {
                      if (!prev) {
                        return prev;
                      }
                      return { ...prev, start_at_login: event.target.checked };
                    })
                  }
                />
                Start at login
              </label>
              <div className="actions full-width">
                <button className="btn" type="submit" disabled={!settings}>
                  Save Settings
                </button>
              </div>
            </form>
          </section>

          <section>
            <h2>Updates</h2>
            <div className="actions">
              <button className="btn secondary" onClick={handleCheckUpdates}>
                Check for Updates
              </button>
            </div>
            {updateResult && (
              <div className="info-block">
                <p>{updateResult.message}</p>
                <p>Current: {updateResult.current_version}</p>
                {updateResult.latest_version && <p>Latest: {updateResult.latest_version}</p>}
                {updateResult.download_url && (
                  <p>
                    Download: <a href={updateResult.download_url}>{updateResult.download_url}</a>
                  </p>
                )}
              </div>
            )}
          </section>
        </article>
      </section>

      <section className="panel">
        <h2>Profiles</h2>
        <div className="table-wrap">
          <table>
            <thead>
              <tr>
                <th>Name</th>
                <th>ID</th>
                <th>Mode</th>
                <th>Gateway</th>
                <th>Connector</th>
                <th>Updated</th>
                <th>Actions</th>
              </tr>
            </thead>
            <tbody>
              {profiles.map((profile) => (
                <tr key={profile.id}>
                  <td>
                    {profile.name}
                    {settings?.active_profile_id === profile.id && <span className="pill">active</span>}
                  </td>
                  <td className="mono">{profile.id}</td>
                  <td className="mono">{profile.mode}</td>
                  <td className="mono">{profile.gateway_base_url}</td>
                  <td className="mono">{profile.connector_id || "-"}</td>
                  <td>{formatDate(profile.updated_at)}</td>
                  <td className="table-actions">
                    <button className="btn tiny" onClick={() => handleUseProfile(profile.id)}>
                      Use
                    </button>
                    <button className="btn tiny secondary" onClick={() => handleEditProfile(profile)}>
                      Edit
                    </button>
                    <button className="btn tiny danger" onClick={() => void handleDeleteProfile(profile.id)}>
                      Delete
                    </button>
                  </td>
                </tr>
              ))}
              {profiles.length === 0 && (
                <tr>
                  <td colSpan={7} className="empty">
                    No profiles created yet.
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
      </section>

      <section className="panel">
        <h2>Logs</h2>
        <div className="actions">
          <button className="btn secondary" onClick={() => void refreshLogs()}>
            Refresh Logs
          </button>
        </div>
        <pre className="log-box">{logs.length > 0 ? logs.join("\n") : "(no logs yet)"}</pre>
      </section>
    </main>
  );
}
