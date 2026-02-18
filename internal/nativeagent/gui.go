package nativeagent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func runBrowserGUI(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	lockPath, err := LockPath()
	if err != nil {
		return err
	}
	lock, err := AcquireProcessLock(lockPath)
	if err != nil {
		return err
	}
	defer lock.Release()

	service, err := NewService()
	if err != nil {
		return err
	}
	bindings, err := NewDesktopBindings(service)
	if err != nil {
		return err
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	server := &http.Server{
		Handler:           newGUIServerMux(bindings),
		ReadHeaderTimeout: 10 * time.Second,
	}

	serveErrCh := make(chan error, 1)
	go func() {
		serveErrCh <- server.Serve(listener)
	}()

	launchURL := "http://" + listener.Addr().String() + "/"
	_ = openBrowser(launchURL)
	fmt.Printf("proxer-agent GUI running at %s\n", launchURL)

	signalCtx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	select {
	case <-signalCtx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = service.Stop()
		_ = server.Shutdown(shutdownCtx)
		return nil
	case err := <-serveErrCh:
		if err == nil || err == http.ErrServerClosed {
			return nil
		}
		return err
	}
}

func newGUIServerMux(bindings *DesktopBindings) *http.ServeMux {
	mux := http.NewServeMux()
	staticAssets, staticErr := guiStaticAssets()
	fileServer := http.FileServer(http.FS(staticAssets))

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			http.NotFound(w, r)
			return
		}
		if staticErr == nil {
			serveNativeGUIAsset(staticAssets, fileServer, w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(guiIndexHTML))
	})
	mux.HandleFunc("/api/build", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, CurrentBuildInfo())
	})
	mux.HandleFunc("/api/settings", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			settings, err := bindings.GetAppSettings()
			if err != nil {
				writeError(w, http.StatusInternalServerError, err)
				return
			}
			writeJSON(w, http.StatusOK, settings)
		case http.MethodPut:
			var body struct {
				StartAtLogin *bool   `json:"start_at_login"`
				LaunchMode   *string `json:"launch_mode"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				writeError(w, http.StatusBadRequest, fmt.Errorf("invalid json payload: %w", err))
				return
			}
			settings, err := bindings.SetAppSettings(AppSettingsInput{
				StartAtLogin: body.StartAtLogin,
				LaunchMode:   body.LaunchMode,
			})
			if err != nil {
				writeError(w, http.StatusBadRequest, err)
				return
			}
			writeJSON(w, http.StatusOK, settings)
		default:
			writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("method not allowed"))
		}
	})
	mux.HandleFunc("/api/profiles", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			profiles, err := bindings.ListProfiles()
			if err != nil {
				writeError(w, http.StatusInternalServerError, err)
				return
			}
			writeJSON(w, http.StatusOK, profiles)
		case http.MethodPost:
			var payload profilePayload
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				writeError(w, http.StatusBadRequest, fmt.Errorf("invalid json payload: %w", err))
				return
			}
			created, err := bindings.CreateProfile(payload.toInput())
			if err != nil {
				writeError(w, http.StatusBadRequest, err)
				return
			}
			writeJSON(w, http.StatusCreated, created)
		default:
			writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("method not allowed"))
		}
	})
	mux.HandleFunc("/api/profiles/", func(w http.ResponseWriter, r *http.Request) {
		profileRef, action, err := parseProfileRoute(r.URL.Path)
		if err != nil {
			writeError(w, http.StatusNotFound, err)
			return
		}

		switch {
		case action == "" && r.Method == http.MethodPut:
			var payload profilePayload
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				writeError(w, http.StatusBadRequest, fmt.Errorf("invalid json payload: %w", err))
				return
			}
			updated, err := bindings.UpdateProfile(profileRef, payload.toInput())
			if err != nil {
				writeError(w, http.StatusBadRequest, err)
				return
			}
			writeJSON(w, http.StatusOK, updated)
		case action == "" && r.Method == http.MethodDelete:
			if err := bindings.DeleteProfile(profileRef); err != nil {
				writeError(w, http.StatusBadRequest, err)
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"deleted": true})
		case action == "use" && r.Method == http.MethodPost:
			active, err := bindings.SetActiveProfile(profileRef)
			if err != nil {
				writeError(w, http.StatusBadRequest, err)
				return
			}
			writeJSON(w, http.StatusOK, active)
		case action == "pair" && r.Method == http.MethodPost:
			var payload struct {
				PairToken string `json:"pair_token"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				writeError(w, http.StatusBadRequest, fmt.Errorf("invalid json payload: %w", err))
				return
			}
			updated, err := bindings.PairProfile(profileRef, payload.PairToken)
			if err != nil {
				writeError(w, http.StatusBadRequest, err)
				return
			}
			writeJSON(w, http.StatusOK, updated)
		default:
			writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("method not allowed"))
		}
	})
	mux.HandleFunc("/api/runtime/start", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("method not allowed"))
			return
		}
		var payload struct {
			Profile string `json:"profile"`
		}
		if r.Body != nil {
			_ = json.NewDecoder(r.Body).Decode(&payload)
		}
		if err := bindings.StartAgent(payload.Profile); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		status, _ := bindings.GetRuntimeStatus()
		writeJSON(w, http.StatusOK, status)
	})
	mux.HandleFunc("/api/runtime/stop", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("method not allowed"))
			return
		}
		if err := bindings.StopAgent(); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		status, _ := bindings.GetRuntimeStatus()
		writeJSON(w, http.StatusOK, status)
	})
	mux.HandleFunc("/api/status", func(w http.ResponseWriter, r *http.Request) {
		status, err := bindings.GetRuntimeStatus()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, status)
	})
	mux.HandleFunc("/api/logs", func(w http.ResponseWriter, r *http.Request) {
		tailLines := 200
		if tailRaw := strings.TrimSpace(r.URL.Query().Get("tail")); tailRaw != "" {
			parsed, err := strconv.Atoi(tailRaw)
			if err != nil || parsed < 0 {
				writeError(w, http.StatusBadRequest, fmt.Errorf("tail must be an integer >= 0"))
				return
			}
			tailLines = parsed
		}
		lines, err := bindings.GetLogTail(tailLines)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"lines": lines})
	})
	mux.HandleFunc("/api/events/runtime", func(w http.ResponseWriter, r *http.Request) {
		streamRuntimeEvents(w, r, bindings)
	})
	mux.HandleFunc("/api/update/check", func(w http.ResponseWriter, r *http.Request) {
		result, err := bindings.CheckForUpdates()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	})
	return mux
}

func serveNativeGUIAsset(staticAssets fs.FS, fileServer http.Handler, w http.ResponseWriter, r *http.Request) {
	cleanPath := path.Clean("/" + r.URL.Path)
	if cleanPath == "/" || cleanPath == "/index.html" {
		serveNativeGUIIndex(staticAssets, w, r)
		return
	}

	assetPath := strings.TrimPrefix(cleanPath, "/")
	if assetPath != "" {
		if _, err := fs.Stat(staticAssets, assetPath); err == nil {
			fileServer.ServeHTTP(w, r)
			return
		}
	}

	serveNativeGUIIndex(staticAssets, w, r)
}

func serveNativeGUIIndex(staticAssets fs.FS, w http.ResponseWriter, r *http.Request) {
	content, err := fs.ReadFile(staticAssets, "index.html")
	if err != nil {
		http.Error(w, "native GUI assets missing", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	http.ServeContent(w, r, "index.html", time.Time{}, bytes.NewReader(content))
}

func streamRuntimeEvents(w http.ResponseWriter, r *http.Request, bindings *DesktopBindings) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, fmt.Errorf("streaming not supported"))
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	status, err := bindings.GetRuntimeStatus()
	if err == nil {
		writeSSEEvent(w, "runtime", status)
		flusher.Flush()
	}

	events, err := bindings.service.SubscribeRuntimeEvents(r.Context())
	if err != nil {
		writeSSEEvent(w, "error", map[string]any{"error": err.Error()})
		flusher.Flush()
		return
	}

	heartbeat := time.NewTicker(20 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-heartbeat.C:
			_, _ = w.Write([]byte(": heartbeat\n\n"))
			flusher.Flush()
		case event, ok := <-events:
			if !ok {
				return
			}
			writeSSEEvent(w, "runtime", event)
			flusher.Flush()
		}
	}
}

func writeSSEEvent(w http.ResponseWriter, eventName string, payload any) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return
	}
	_, _ = fmt.Fprintf(w, "event: %s\n", eventName)
	_, _ = fmt.Fprintf(w, "data: %s\n\n", encoded)
}

func parseProfileRoute(path string) (string, string, error) {
	relative := strings.TrimPrefix(path, "/api/profiles/")
	if strings.TrimSpace(relative) == "" {
		return "", "", fmt.Errorf("profile id missing")
	}
	parts := strings.Split(strings.Trim(relative, "/"), "/")
	profileRef, err := url.PathUnescape(parts[0])
	if err != nil {
		return "", "", fmt.Errorf("invalid profile reference")
	}
	action := ""
	if len(parts) > 1 {
		action = parts[1]
	}
	return profileRef, action, nil
}

type profilePayload struct {
	Name            string         `json:"name"`
	GatewayBaseURL  string         `json:"gateway_base_url"`
	AgentID         string         `json:"agent_id"`
	Mode            string         `json:"mode"`
	ConnectorID     string         `json:"connector_id"`
	LegacyTunnels   string         `json:"legacy_tunnels"`
	ConnectorSecret string         `json:"connector_secret"`
	AgentToken      string         `json:"agent_token"`
	Runtime         runtimePayload `json:"runtime"`
}

type runtimePayload struct {
	RequestTimeout       string `json:"request_timeout"`
	PollWait             string `json:"poll_wait"`
	HeartbeatInterval    string `json:"heartbeat_interval"`
	MaxResponseBodyBytes int64  `json:"max_response_body_bytes"`
	ProxyURL             string `json:"proxy_url,omitempty"`
	NoProxy              string `json:"no_proxy,omitempty"`
	TLSSkipVerify        *bool  `json:"tls_skip_verify,omitempty"`
	CAFile               string `json:"ca_file,omitempty"`
	LogLevel             string `json:"log_level"`
}

func (p profilePayload) toInput() ProfileInput {
	input := ProfileInput{
		Name:            p.Name,
		GatewayBaseURL:  p.GatewayBaseURL,
		AgentID:         p.AgentID,
		Mode:            p.Mode,
		ConnectorID:     p.ConnectorID,
		LegacyTunnels:   p.LegacyTunnels,
		ConnectorSecret: p.ConnectorSecret,
		AgentToken:      p.AgentToken,
		Runtime: RuntimeOptions{
			RequestTimeout:       p.Runtime.RequestTimeout,
			PollWait:             p.Runtime.PollWait,
			HeartbeatInterval:    p.Runtime.HeartbeatInterval,
			MaxResponseBodyBytes: p.Runtime.MaxResponseBodyBytes,
			ProxyURL:             p.Runtime.ProxyURL,
			NoProxy:              p.Runtime.NoProxy,
			CAFile:               p.Runtime.CAFile,
			LogLevel:             p.Runtime.LogLevel,
		},
	}
	if p.Runtime.TLSSkipVerify != nil {
		input.Runtime.TLSSkipVerify = *p.Runtime.TLSSkipVerify
		input.RuntimeTLSSkipVerifySet = true
	}
	return input
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]any{
		"error": err.Error(),
	})
}

func openBrowser(url string) error {
	candidates := [][]string{}
	switch runtime.GOOS {
	case "darwin":
		candidates = append(candidates, []string{"open", url})
	case "linux":
		candidates = append(candidates, []string{"xdg-open", url})
	case "windows":
		candidates = append(candidates, []string{"rundll32", "url.dll,FileProtocolHandler", url})
	}
	for _, args := range candidates {
		if len(args) == 0 {
			continue
		}
		cmd := exec.Command(args[0], args[1:]...)
		if err := cmd.Start(); err == nil {
			return nil
		}
	}
	return fmt.Errorf("could not open browser automatically, open %s manually", url)
}
