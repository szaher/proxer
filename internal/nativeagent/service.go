package nativeagent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type Service struct {
	store      *Store
	secrets    SecretStore
	runtime    *RuntimeManager
	statusPath string
	logPath    string
}

var pairWithGatewayExchange = pairWithGateway

type ProfileInput struct {
	Name                    string
	GatewayBaseURL          string
	AgentID                 string
	Mode                    string
	ConnectorID             string
	Runtime                 RuntimeOptions
	RuntimeTLSSkipVerifySet bool
	LegacyTunnels           string
	ConnectorSecret         string
	AgentToken              string
}

type AppSettingsInput struct {
	StartAtLogin *bool
	LaunchMode   *string
}

func NewService() (*Service, error) {
	store, err := NewDefaultStore()
	if err != nil {
		return nil, err
	}
	statusPath, err := StatusPath()
	if err != nil {
		return nil, err
	}
	logPath, err := LogPath()
	if err != nil {
		return nil, err
	}
	return &Service{
		store:      store,
		secrets:    NewSecretStore(),
		runtime:    NewRuntimeManager(statusPath, logPath),
		statusPath: statusPath,
		logPath:    logPath,
	}, nil
}

func NewServiceWithDependencies(store *Store, secrets SecretStore, runtime *RuntimeManager, statusPath, logPath string) *Service {
	if store == nil {
		if settingsPath, err := SettingsPath(); err == nil {
			store = NewStore(settingsPath)
		} else {
			store = NewStore(filepath.Join(os.TempDir(), "proxer-agent-settings.json"))
		}
	}
	if secrets == nil {
		secrets = &unsupportedSecretStore{}
	}
	if runtime == nil {
		runtime = NewRuntimeManager(statusPath, logPath)
	}
	return &Service{
		store:      store,
		secrets:    secrets,
		runtime:    runtime,
		statusPath: statusPath,
		logPath:    logPath,
	}
}

func (s *Service) Settings() (AgentSettings, error) {
	return s.store.Load()
}

func (s *Service) SetAppSettings(input AppSettingsInput) (AgentSettings, error) {
	updated, err := s.store.Update(func(settings *AgentSettings) error {
		if input.StartAtLogin != nil {
			settings.StartAtLogin = *input.StartAtLogin
		}
		if input.LaunchMode != nil {
			mode := strings.TrimSpace(*input.LaunchMode)
			if mode == "" {
				return fmt.Errorf("launch_mode cannot be empty")
			}
			settings.LaunchMode = mode
		}
		return nil
	})
	if err != nil {
		return AgentSettings{}, err
	}
	if input.StartAtLogin != nil {
		if err := ConfigureStartAtLogin(*input.StartAtLogin, []string{"gui"}); err != nil {
			return AgentSettings{}, err
		}
	}
	return updated, nil
}

func (s *Service) ListProfiles() ([]AgentProfile, error) {
	settings, err := s.store.Load()
	if err != nil {
		return nil, err
	}
	profiles := make([]AgentProfile, len(settings.Profiles))
	copy(profiles, settings.Profiles)
	return profiles, nil
}

func (s *Service) ActiveProfile() (AgentProfile, error) {
	settings, err := s.store.Load()
	if err != nil {
		return AgentProfile{}, err
	}
	if strings.TrimSpace(settings.ActiveProfileID) == "" {
		return AgentProfile{}, fmt.Errorf("no active profile set")
	}
	profile, ok := profileByID(settings, settings.ActiveProfileID)
	if !ok {
		return AgentProfile{}, fmt.Errorf("active profile %q not found", settings.ActiveProfileID)
	}
	return profile, nil
}

func (s *Service) ResolveProfile(idOrName string) (AgentProfile, error) {
	settings, err := s.store.Load()
	if err != nil {
		return AgentProfile{}, err
	}
	if strings.TrimSpace(idOrName) == "" {
		if strings.TrimSpace(settings.ActiveProfileID) == "" {
			return AgentProfile{}, fmt.Errorf("no active profile set")
		}
		profile, ok := profileByID(settings, settings.ActiveProfileID)
		if !ok {
			return AgentProfile{}, fmt.Errorf("active profile %q not found", settings.ActiveProfileID)
		}
		return profile, nil
	}
	index := profileIndexByIDOrName(settings, idOrName)
	if index < 0 {
		return AgentProfile{}, fmt.Errorf("profile %q not found", idOrName)
	}
	return settings.Profiles[index], nil
}

func (s *Service) CreateProfile(input ProfileInput) (AgentProfile, error) {
	profileID, err := newProfileID()
	if err != nil {
		return AgentProfile{}, err
	}

	profile := AgentProfile{
		ID:             profileID,
		Name:           strings.TrimSpace(input.Name),
		GatewayBaseURL: strings.TrimSpace(input.GatewayBaseURL),
		AgentID:        strings.TrimSpace(input.AgentID),
		Mode:           strings.TrimSpace(input.Mode),
		ConnectorID:    strings.TrimSpace(input.ConnectorID),
		Runtime:        input.Runtime,
	}
	profile = applyProfileDefaults(profile)
	profile.ConnectorSecretRef = SecretRef{Key: secretKeyForProfile(profile.ID, "connector_secret")}
	profile.AgentTokenRef = SecretRef{Key: secretKeyForProfile(profile.ID, "agent_token")}
	profile.LegacyTunnels, err = parseTunnelMappings(input.LegacyTunnels)
	if err != nil {
		return AgentProfile{}, err
	}
	if err := validateProfile(profile); err != nil {
		return AgentProfile{}, err
	}

	if profile.Mode == ModeConnector && strings.TrimSpace(input.ConnectorSecret) != "" {
		if err := s.secrets.Set(context.Background(), profile.ConnectorSecretRef.Key, strings.TrimSpace(input.ConnectorSecret)); err != nil {
			return AgentProfile{}, err
		}
	}
	if profile.Mode == ModeLegacyTunnels && strings.TrimSpace(input.AgentToken) != "" {
		if err := s.secrets.Set(context.Background(), profile.AgentTokenRef.Key, strings.TrimSpace(input.AgentToken)); err != nil {
			return AgentProfile{}, err
		}
	}

	createdAt := time.Now().UTC()
	profile.CreatedAt = createdAt
	profile.UpdatedAt = createdAt

	_, err = s.store.Update(func(settings *AgentSettings) error {
		if err := ensureUniqueProfileName(*settings, profile.Name, ""); err != nil {
			return err
		}
		settings.Profiles = append(settings.Profiles, profile)
		if strings.TrimSpace(settings.ActiveProfileID) == "" {
			settings.ActiveProfileID = profile.ID
		}
		return nil
	})
	if err != nil {
		return AgentProfile{}, err
	}
	return profile, nil
}

func (s *Service) UpdateProfile(idOrName string, input ProfileInput) (AgentProfile, error) {
	var updated AgentProfile
	_, err := s.store.Update(func(settings *AgentSettings) error {
		index := profileIndexByIDOrName(*settings, idOrName)
		if index < 0 {
			return fmt.Errorf("profile %q not found", idOrName)
		}
		profile := settings.Profiles[index]

		if name := strings.TrimSpace(input.Name); name != "" {
			profile.Name = name
		}
		if gateway := strings.TrimSpace(input.GatewayBaseURL); gateway != "" {
			profile.GatewayBaseURL = gateway
		}
		if agentID := strings.TrimSpace(input.AgentID); agentID != "" {
			profile.AgentID = agentID
		}
		if mode := strings.TrimSpace(input.Mode); mode != "" {
			profile.Mode = mode
		}
		if connectorID := strings.TrimSpace(input.ConnectorID); connectorID != "" {
			profile.ConnectorID = connectorID
		}
		if input.Runtime != (RuntimeOptions{}) || input.RuntimeTLSSkipVerifySet {
			merged := profile.Runtime
			if v := strings.TrimSpace(input.Runtime.RequestTimeout); v != "" {
				merged.RequestTimeout = v
			}
			if v := strings.TrimSpace(input.Runtime.PollWait); v != "" {
				merged.PollWait = v
			}
			if v := strings.TrimSpace(input.Runtime.HeartbeatInterval); v != "" {
				merged.HeartbeatInterval = v
			}
			if input.Runtime.MaxResponseBodyBytes > 0 {
				merged.MaxResponseBodyBytes = input.Runtime.MaxResponseBodyBytes
			}
			if v := strings.TrimSpace(input.Runtime.ProxyURL); v != "" {
				merged.ProxyURL = v
			}
			if v := strings.TrimSpace(input.Runtime.NoProxy); v != "" {
				merged.NoProxy = v
			}
			if v := strings.TrimSpace(input.Runtime.CAFile); v != "" {
				merged.CAFile = v
			}
			if v := strings.TrimSpace(input.Runtime.LogLevel); v != "" {
				merged.LogLevel = v
			}
			if input.RuntimeTLSSkipVerifySet {
				merged.TLSSkipVerify = input.Runtime.TLSSkipVerify
			}
			profile.Runtime = merged
		}
		if strings.TrimSpace(input.LegacyTunnels) != "" {
			tunnels, err := parseTunnelMappings(strings.TrimSpace(input.LegacyTunnels))
			if err != nil {
				return err
			}
			profile.LegacyTunnels = tunnels
		}

		profile = applyProfileDefaults(profile)
		if err := ensureUniqueProfileName(*settings, profile.Name, profile.ID); err != nil {
			return err
		}
		if err := validateProfile(profile); err != nil {
			return err
		}

		profile.UpdatedAt = time.Now().UTC()
		settings.Profiles[index] = profile
		updated = profile
		return nil
	})
	if err != nil {
		return AgentProfile{}, err
	}

	if strings.TrimSpace(input.ConnectorSecret) != "" {
		if err := s.secrets.Set(context.Background(), updated.ConnectorSecretRef.Key, strings.TrimSpace(input.ConnectorSecret)); err != nil {
			return AgentProfile{}, err
		}
	}
	if strings.TrimSpace(input.AgentToken) != "" {
		if err := s.secrets.Set(context.Background(), updated.AgentTokenRef.Key, strings.TrimSpace(input.AgentToken)); err != nil {
			return AgentProfile{}, err
		}
	}

	return updated, nil
}

func (s *Service) DeleteProfile(idOrName string) error {
	var removed AgentProfile
	_, err := s.store.Update(func(settings *AgentSettings) error {
		index := profileIndexByIDOrName(*settings, idOrName)
		if index < 0 {
			return fmt.Errorf("profile %q not found", idOrName)
		}
		removed = settings.Profiles[index]
		settings.Profiles = append(settings.Profiles[:index], settings.Profiles[index+1:]...)
		if strings.EqualFold(strings.TrimSpace(settings.ActiveProfileID), strings.TrimSpace(removed.ID)) {
			settings.ActiveProfileID = ""
			if len(settings.Profiles) > 0 {
				settings.ActiveProfileID = settings.Profiles[0].ID
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	if removed.ConnectorSecretRef.Key != "" {
		_ = s.secrets.Delete(context.Background(), removed.ConnectorSecretRef.Key)
	}
	if removed.AgentTokenRef.Key != "" {
		_ = s.secrets.Delete(context.Background(), removed.AgentTokenRef.Key)
	}
	return nil
}

func (s *Service) SetActiveProfile(idOrName string) (AgentProfile, error) {
	var active AgentProfile
	_, err := s.store.Update(func(settings *AgentSettings) error {
		index := profileIndexByIDOrName(*settings, idOrName)
		if index < 0 {
			return fmt.Errorf("profile %q not found", idOrName)
		}
		settings.ActiveProfileID = settings.Profiles[index].ID
		active = settings.Profiles[index]
		return nil
	})
	if err != nil {
		return AgentProfile{}, err
	}
	return active, nil
}

func (s *Service) PairProfile(idOrName, pairToken string) (AgentProfile, error) {
	if strings.TrimSpace(pairToken) == "" {
		return AgentProfile{}, fmt.Errorf("pair token is required")
	}
	profile, err := s.ResolveProfile(idOrName)
	if err != nil {
		return AgentProfile{}, err
	}
	pairResp, err := pairWithGatewayExchange(context.Background(), profile.GatewayBaseURL, profile.AgentID, pairToken)
	if err != nil {
		return AgentProfile{}, err
	}
	if err := s.secrets.Set(context.Background(), profile.ConnectorSecretRef.Key, pairResp.ConnectorSecret); err != nil {
		return AgentProfile{}, err
	}
	return s.UpdateProfile(profile.ID, ProfileInput{
		Mode:        ModeConnector,
		ConnectorID: pairResp.ConnectorID,
	})
}

func (s *Service) Start(profileIDOrName string) error {
	profile, err := s.ResolveProfile(profileIDOrName)
	if err != nil {
		return err
	}
	profile = applyProfileDefaults(profile)

	connectorSecret := ""
	agentToken := ""
	if profile.Mode == ModeConnector {
		connectorSecret, err = s.secrets.Get(context.Background(), profile.ConnectorSecretRef.Key)
		if err != nil {
			if errors.Is(err, ErrSecretNotFound) {
				return fmt.Errorf("missing connector secret in system keychain; pair profile again")
			}
			if errors.Is(err, ErrSecretUnavailable) {
				return fmt.Errorf("system secret store unavailable for connector credentials: %s", secretStoreUnavailableRemediation())
			}
			return err
		}
	} else {
		agentToken, err = s.secrets.Get(context.Background(), profile.AgentTokenRef.Key)
		if err != nil {
			if errors.Is(err, ErrSecretNotFound) {
				return fmt.Errorf("missing legacy agent token in system keychain")
			}
			if errors.Is(err, ErrSecretUnavailable) {
				return fmt.Errorf("system secret store unavailable for legacy token: %s", secretStoreUnavailableRemediation())
			}
			return err
		}
	}
	return s.runtime.Start(profile, connectorSecret, agentToken)
}

func (s *Service) Stop() error {
	return s.runtime.Stop(15 * time.Second)
}

func (s *Service) Wait(ctx context.Context) error {
	return s.runtime.Wait(ctx)
}

func (s *Service) Status() (NativeStatusSnapshot, error) {
	return ReadStatusSnapshot(s.statusPath)
}

func (s *Service) LogFilePath() string {
	return s.logPath
}

func (s *Service) ReadLogTail(lines int) ([]string, error) {
	return readLogTailLines(s.logPath, lines)
}

func (s *Service) SubscribeRuntimeEvents(ctx context.Context) (<-chan NativeStatusSnapshot, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	return s.runtime.Subscribe(ctx, 32), nil
}

func (s *Service) CheckForUpdates() (UpdateCheckResult, error) {
	return UpdateCheckResult{
		CurrentVersion: BuildVersion(),
		Message:        "Auto-update is not enabled in v1. Use release artifacts from the project repository.",
	}, nil
}

func secretStoreUnavailableRemediation() string {
	switch runtime.GOOS {
	case "darwin":
		return "allow access to macOS Keychain for proxer-agent and retry"
	case "linux":
		return "install libsecret/secret-tool and ensure a Secret Service keyring session is unlocked"
	case "windows":
		return "run under a user profile with DPAPI available and retry"
	default:
		return "verify operating-system keychain availability"
	}
}

func newProfileID() (string, error) {
	buf := make([]byte, 6)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return "p_" + hex.EncodeToString(buf), nil
}

func readLogTailLines(path string, lines int) ([]string, error) {
	if lines <= 0 {
		lines = 200
	}
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, err
	}
	defer file.Close()

	buffer := make([]string, 0, lines)
	var currentLine strings.Builder
	chunk := make([]byte, 4096)

	for {
		n, readErr := file.Read(chunk)
		if n > 0 {
			for _, b := range chunk[:n] {
				if b == '\n' {
					buffer = append(buffer, strings.TrimRight(currentLine.String(), "\r"))
					if len(buffer) > lines {
						buffer = buffer[1:]
					}
					currentLine.Reset()
					continue
				}
				currentLine.WriteByte(b)
			}
		}
		if readErr != nil {
			if readErr == io.EOF {
				break
			}
			return nil, readErr
		}
	}
	if currentLine.Len() > 0 {
		buffer = append(buffer, strings.TrimRight(currentLine.String(), "\r"))
		if len(buffer) > lines {
			buffer = buffer[1:]
		}
	}
	return buffer, nil
}
