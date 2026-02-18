package nativeagent

import (
	"context"
	"fmt"
)

// DesktopBindings is a Wails-ready API surface for native GUI integrations.
type DesktopBindings struct {
	service *Service
}

func NewDesktopBindings(service *Service) (*DesktopBindings, error) {
	if service == nil {
		var err error
		service, err = NewService()
		if err != nil {
			return nil, err
		}
	}
	return &DesktopBindings{service: service}, nil
}

func (b *DesktopBindings) ListProfiles() ([]AgentProfile, error) {
	return b.service.ListProfiles()
}

func (b *DesktopBindings) CreateProfile(input ProfileInput) (AgentProfile, error) {
	return b.service.CreateProfile(input)
}

func (b *DesktopBindings) UpdateProfile(id string, input ProfileInput) (AgentProfile, error) {
	return b.service.UpdateProfile(id, input)
}

func (b *DesktopBindings) DeleteProfile(id string) error {
	return b.service.DeleteProfile(id)
}

func (b *DesktopBindings) SetActiveProfile(id string) (AgentProfile, error) {
	return b.service.SetActiveProfile(id)
}

func (b *DesktopBindings) PairProfile(id, pairToken string) (AgentProfile, error) {
	return b.service.PairProfile(id, pairToken)
}

func (b *DesktopBindings) StartAgent(profile string) error {
	return b.service.Start(profile)
}

func (b *DesktopBindings) StopAgent() error {
	return b.service.Stop()
}

func (b *DesktopBindings) GetRuntimeStatus() (NativeStatusSnapshot, error) {
	return b.service.Status()
}

func (b *DesktopBindings) SubscribeEvents() (<-chan NativeStatusSnapshot, error) {
	ctx := context.Background()
	return b.service.SubscribeRuntimeEvents(ctx)
}

func (b *DesktopBindings) GetAppSettings() (AgentSettings, error) {
	return b.service.Settings()
}

func (b *DesktopBindings) SetAppSettings(input AppSettingsInput) (AgentSettings, error) {
	return b.service.SetAppSettings(input)
}

func (b *DesktopBindings) CheckForUpdates() (UpdateCheckResult, error) {
	return b.service.CheckForUpdates()
}

func (b *DesktopBindings) GetLogTail(lines int) ([]string, error) {
	if lines < 0 {
		return nil, fmt.Errorf("lines must be >= 0")
	}
	return b.service.ReadLogTail(lines)
}
