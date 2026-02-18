//go:build wails

package nativeagent

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
)

func runWailsHost(ctx context.Context) error {
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

	listener, server, uiURL, err := startWailsLocalServer(bindings)
	if err != nil {
		return err
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
		_ = listener.Close()
	}()

	app := application.New(application.Options{
		Name:        "Proxer Agent",
		Description: "Secure localhost routing and connector runtime",
	})

	window := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:     "Proxer Agent",
		Width:     1220,
		Height:    860,
		MinWidth:  980,
		MinHeight: 700,
	})
	window.SetURL(uiURL)
	window.Center()

	trayStatusItem := configureWailsMenuAndTray(app, window, bindings)
	streamCtx, cancelStream := context.WithCancel(context.Background())
	defer cancelStream()
	bridgeWailsRuntimeEvents(app, bindings, streamCtx, trayStatusItem)

	go func() {
		<-ctx.Done()
		app.Quit()
	}()

	app.Run()

	_ = bindings.StopAgent()
	return nil
}

func startWailsLocalServer(bindings *DesktopBindings) (net.Listener, *http.Server, string, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, nil, "", err
	}
	server := &http.Server{
		Handler:           newGUIServerMux(bindings),
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		_ = server.Serve(listener)
	}()
	url := "http://" + listener.Addr().String() + "/"
	return listener, server, url, nil
}

func configureWailsMenuAndTray(app *application.App, window *application.WebviewWindow, bindings *DesktopBindings) *application.MenuItem {
	appMenu := app.NewMenu()
	proxerMenu := appMenu.AddSubmenu("Proxer")
	proxerMenu.Add("Open Window").OnClick(func(ctx *application.Context) {
		window.Show()
		window.Focus()
	})
	proxerMenu.Add("Hide Window").OnClick(func(ctx *application.Context) {
		window.Hide()
	})
	proxerMenu.AddSeparator()
	proxerMenu.Add("Start Agent").OnClick(func(ctx *application.Context) {
		if err := bindings.StartAgent(""); err != nil {
			app.Event.Emit("nativeagent.error", map[string]any{"error": err.Error()})
			return
		}
		status, _ := bindings.GetRuntimeStatus()
		app.Event.Emit("nativeagent.runtime", status)
	})
	proxerMenu.Add("Stop Agent").OnClick(func(ctx *application.Context) {
		if err := bindings.StopAgent(); err != nil {
			app.Event.Emit("nativeagent.error", map[string]any{"error": err.Error()})
			return
		}
		status, _ := bindings.GetRuntimeStatus()
		app.Event.Emit("nativeagent.runtime", status)
	})
	proxerMenu.AddSeparator()
	proxerMenu.Add("Quit").OnClick(func(ctx *application.Context) {
		app.Quit()
	})
	app.Menu.Set(appMenu)

	tray := app.SystemTray.New()
	tray.SetLabel("Proxer")
	tray.SetTooltip("Proxer Agent")
	tray.OnClick(func() {
		if window.IsVisible() {
			window.Hide()
			return
		}
		window.Show()
		window.Focus()
	})

	trayMenu := app.NewMenu()
	statusItem := trayMenu.Add("Status: stopped")
	statusItem.SetEnabled(false)
	trayMenu.AddSeparator()
	trayMenu.Add("Open Window").OnClick(func(ctx *application.Context) {
		window.Show()
		window.Focus()
	})
	trayMenu.Add("Start Agent").OnClick(func(ctx *application.Context) {
		if err := bindings.StartAgent(""); err != nil {
			app.Event.Emit("nativeagent.error", map[string]any{"error": err.Error()})
			return
		}
		status, _ := bindings.GetRuntimeStatus()
		app.Event.Emit("nativeagent.runtime", status)
	})
	trayMenu.Add("Stop Agent").OnClick(func(ctx *application.Context) {
		if err := bindings.StopAgent(); err != nil {
			app.Event.Emit("nativeagent.error", map[string]any{"error": err.Error()})
			return
		}
		status, _ := bindings.GetRuntimeStatus()
		app.Event.Emit("nativeagent.runtime", status)
	})
	trayMenu.AddSeparator()
	trayMenu.Add("Quit").OnClick(func(ctx *application.Context) {
		app.Quit()
	})
	tray.SetMenu(trayMenu)

	if settings, err := bindings.GetAppSettings(); err == nil {
		if settings.StartAtLogin {
			app.Event.Emit("nativeagent.info", map[string]any{"message": "Start-at-login is enabled"})
		}
	}

	return statusItem
}

func bridgeWailsRuntimeEvents(app *application.App, bindings *DesktopBindings, ctx context.Context, trayStatusItem *application.MenuItem) {
	events, err := bindings.service.SubscribeRuntimeEvents(ctx)
	if err != nil {
		app.Event.Emit("nativeagent.error", map[string]any{"error": err.Error()})
		return
	}

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case event, ok := <-events:
				if !ok {
					return
				}
				if trayStatusItem != nil {
					state := strings.TrimSpace(event.State)
					if state == "" {
						state = RuntimeStateStopped
					}
					trayStatusItem.SetLabel(fmt.Sprintf("Status: %s", state))
				}
				app.Event.Emit("nativeagent.runtime", event)
			}
		}
	}()
}
