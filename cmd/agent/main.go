package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/szaher/try/proxer/internal/agent"
	"github.com/szaher/try/proxer/internal/nativeagent"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	args := os.Args[1:]
	if len(args) == 0 {
		if hasLegacyEnvConfig() {
			runLegacyEnvMode(ctx)
			return
		}
		runManagedRun(ctx, "")
		return
	}

	switch args[0] {
	case "gui":
		if err := nativeagent.RunGUI(ctx); err != nil {
			log.Fatalf("start gui mode: %v", err)
		}
	case "run":
		handleRunCommand(ctx, args[1:])
	case "status":
		handleStatusCommand(args[1:])
	case "logs":
		handleLogsCommand(ctx, args[1:])
	case "profile":
		handleProfileCommand(args[1:])
	case "pair":
		handlePairCommand(args[1:])
	case "config":
		handleConfigCommand(args[1:])
	case "update":
		handleUpdateCommand(args[1:])
	case "help", "--help", "-h":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", args[0])
		printUsage()
		os.Exit(1)
	}
}

func runLegacyEnvMode(ctx context.Context) {
	cfg, err := agent.LoadConfigFromEnv()
	if err != nil {
		log.Fatalf("load agent config from env: %v", err)
	}
	logger := log.New(os.Stdout, "[agent] ", log.LstdFlags|log.Lmicroseconds)
	client := agent.New(cfg, logger)
	logger.Printf("starting proxer agent in legacy env mode (id=%s, tunnels=%d)", cfg.AgentID, len(cfg.Tunnels))
	if err := client.Run(ctx); err != nil {
		log.Fatalf("agent stopped with error: %v", err)
	}
}

func runManagedRun(ctx context.Context, profile string) {
	service, err := nativeagent.NewService()
	if err != nil {
		log.Fatalf("initialize native agent service: %v", err)
	}
	if err := service.Start(profile); err != nil {
		log.Fatalf("start managed runtime: %v", err)
	}
	fmt.Println("managed runtime started; press Ctrl+C to stop")

	waitErrCh := make(chan error, 1)
	go func() {
		waitErrCh <- service.Wait(ctx)
	}()

	select {
	case <-ctx.Done():
		if err := service.Stop(); err != nil {
			log.Fatalf("stop runtime: %v", err)
		}
		fmt.Println("runtime stopped")
	case err := <-waitErrCh:
		if err != nil && !errors.Is(err, context.Canceled) {
			log.Fatalf("runtime exited with error: %v", err)
		}
	}
}

func handleRunCommand(ctx context.Context, args []string) {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	profile := fs.String("profile", "", "profile id or name")
	_ = fs.Parse(args)

	if strings.TrimSpace(*profile) == "" && hasLegacyEnvConfig() {
		runLegacyEnvMode(ctx)
		return
	}
	runManagedRun(ctx, *profile)
}

func handleStatusCommand(args []string) {
	fs := flag.NewFlagSet("status", flag.ExitOnError)
	asJSON := fs.Bool("json", false, "output json")
	_ = fs.Parse(args)

	service, err := nativeagent.NewService()
	if err != nil {
		log.Fatalf("initialize native agent service: %v", err)
	}
	status, err := service.Status()
	if err != nil {
		log.Fatalf("read status: %v", err)
	}
	if *asJSON {
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		_ = encoder.Encode(status)
		return
	}
	fmt.Printf("state: %s\n", status.State)
	fmt.Printf("profile: %s (%s)\n", status.ProfileName, status.ProfileID)
	fmt.Printf("agent_id: %s\n", status.AgentID)
	fmt.Printf("session_id: %s\n", status.SessionID)
	fmt.Printf("mode: %s\n", status.Mode)
	fmt.Printf("updated_at: %s\n", status.UpdatedAt.Format(time.RFC3339))
	if strings.TrimSpace(status.Error) != "" {
		fmt.Printf("error: %s\n", status.Error)
	}
}

func handleLogsCommand(ctx context.Context, args []string) {
	fs := flag.NewFlagSet("logs", flag.ExitOnError)
	follow := fs.Bool("follow", false, "follow log output")
	tailLines := fs.Int("tail", 200, "tail lines to print")
	_ = fs.Parse(args)

	service, err := nativeagent.NewService()
	if err != nil {
		log.Fatalf("initialize native agent service: %v", err)
	}
	logPath := service.LogFilePath()
	if err := printTail(logPath, *tailLines, os.Stdout); err != nil {
		log.Fatalf("read logs: %v", err)
	}
	if *follow {
		if err := followFile(ctx, logPath, os.Stdout); err != nil && !errors.Is(err, context.Canceled) {
			log.Fatalf("follow logs: %v", err)
		}
	}
}

func handleProfileCommand(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "profile command requires a subcommand: list|add|edit|remove|use")
		os.Exit(1)
	}
	service, err := nativeagent.NewService()
	if err != nil {
		log.Fatalf("initialize native agent service: %v", err)
	}

	switch args[0] {
	case "list":
		profiles, err := service.ListProfiles()
		if err != nil {
			log.Fatalf("list profiles: %v", err)
		}
		settings, err := service.Settings()
		if err != nil {
			log.Fatalf("load settings: %v", err)
		}
		for _, profile := range profiles {
			activeMark := " "
			if strings.EqualFold(profile.ID, settings.ActiveProfileID) {
				activeMark = "*"
			}
			fmt.Printf("%s %s (%s) mode=%s gateway=%s connector=%s\n", activeMark, profile.Name, profile.ID, profile.Mode, profile.GatewayBaseURL, profile.ConnectorID)
		}
	case "add":
		input := parseProfileInput(args[1:], true)
		created, err := service.CreateProfile(input)
		if err != nil {
			log.Fatalf("create profile: %v", err)
		}
		fmt.Printf("created profile %s (%s)\n", created.Name, created.ID)
	case "edit":
		if len(args) < 2 {
			log.Fatalf("usage: proxer-agent profile edit <profile> [flags]")
		}
		profileRef := args[1]
		input := parseProfileInput(args[2:], false)
		updated, err := service.UpdateProfile(profileRef, input)
		if err != nil {
			log.Fatalf("update profile: %v", err)
		}
		fmt.Printf("updated profile %s (%s)\n", updated.Name, updated.ID)
	case "remove":
		if len(args) < 2 {
			log.Fatalf("usage: proxer-agent profile remove <profile>")
		}
		if err := service.DeleteProfile(args[1]); err != nil {
			log.Fatalf("remove profile: %v", err)
		}
		fmt.Printf("removed profile %s\n", args[1])
	case "use":
		if len(args) < 2 {
			log.Fatalf("usage: proxer-agent profile use <profile>")
		}
		active, err := service.SetActiveProfile(args[1])
		if err != nil {
			log.Fatalf("set active profile: %v", err)
		}
		fmt.Printf("active profile is now %s (%s)\n", active.Name, active.ID)
	default:
		log.Fatalf("unknown profile subcommand %q", args[0])
	}
}

func parseProfileInput(args []string, create bool) nativeagent.ProfileInput {
	fs := flag.NewFlagSet("profile", flag.ExitOnError)
	nameDefault := ""
	gatewayDefault := ""
	agentIDDefault := ""
	modeDefault := ""
	requestTimeoutDefault := ""
	pollWaitDefault := ""
	heartbeatDefault := ""
	maxRespDefault := int64(0)
	logLevelDefault := ""
	if create {
		gatewayDefault = "http://127.0.0.1:18080"
		agentIDDefault = "local-agent"
		modeDefault = nativeagent.ModeConnector
		requestTimeoutDefault = "45s"
		pollWaitDefault = "25s"
		heartbeatDefault = "10s"
		maxRespDefault = 20 << 20
		logLevelDefault = "info"
	}

	name := fs.String("name", nameDefault, "profile name")
	gateway := fs.String("gateway", gatewayDefault, "gateway base URL")
	agentID := fs.String("agent-id", agentIDDefault, "agent ID")
	mode := fs.String("mode", modeDefault, "connector or legacy_tunnels")
	connectorID := fs.String("connector-id", "", "connector ID")
	connectorSecret := fs.String("connector-secret", "", "connector secret (stored in keychain)")
	agentToken := fs.String("agent-token", "", "legacy agent token (stored in keychain)")
	legacyTunnels := fs.String("legacy-tunnels", "", "legacy tunnel mappings: id=url,id2@token=url")

	requestTimeout := fs.String("request-timeout", requestTimeoutDefault, "upstream request timeout")
	pollWait := fs.String("poll-wait", pollWaitDefault, "gateway pull wait")
	heartbeat := fs.String("heartbeat-interval", heartbeatDefault, "heartbeat interval")
	maxRespBytes := fs.Int64("max-response-body-bytes", maxRespDefault, "max response body bytes")
	proxyURL := fs.String("proxy-url", "", "outbound proxy URL")
	noProxy := fs.String("no-proxy", "", "NO_PROXY value")
	tlsSkipVerify := fs.String("tls-skip-verify", "", "set true or false")
	caFile := fs.String("ca-file", "", "custom CA file path")
	logLevel := fs.String("log-level", logLevelDefault, "log level")

	_ = fs.Parse(args)

	if create && strings.TrimSpace(*name) == "" {
		log.Fatalf("--name is required")
	}

	input := nativeagent.ProfileInput{
		Name:            strings.TrimSpace(*name),
		GatewayBaseURL:  strings.TrimSpace(*gateway),
		AgentID:         strings.TrimSpace(*agentID),
		Mode:            strings.TrimSpace(*mode),
		ConnectorID:     strings.TrimSpace(*connectorID),
		ConnectorSecret: strings.TrimSpace(*connectorSecret),
		AgentToken:      strings.TrimSpace(*agentToken),
		LegacyTunnels:   strings.TrimSpace(*legacyTunnels),
		Runtime: nativeagent.RuntimeOptions{
			RequestTimeout:       strings.TrimSpace(*requestTimeout),
			PollWait:             strings.TrimSpace(*pollWait),
			HeartbeatInterval:    strings.TrimSpace(*heartbeat),
			MaxResponseBodyBytes: *maxRespBytes,
			ProxyURL:             strings.TrimSpace(*proxyURL),
			NoProxy:              strings.TrimSpace(*noProxy),
			CAFile:               strings.TrimSpace(*caFile),
			LogLevel:             strings.TrimSpace(*logLevel),
		},
	}
	if strings.TrimSpace(*tlsSkipVerify) != "" {
		parsed, err := strconv.ParseBool(strings.TrimSpace(*tlsSkipVerify))
		if err != nil {
			log.Fatalf("parse --tls-skip-verify: %v", err)
		}
		input.Runtime.TLSSkipVerify = parsed
		input.RuntimeTLSSkipVerifySet = true
	}
	return input
}

func handlePairCommand(args []string) {
	fs := flag.NewFlagSet("pair", flag.ExitOnError)
	token := fs.String("token", "", "pair token")
	profile := fs.String("profile", "", "profile id or name")
	_ = fs.Parse(args)

	if strings.TrimSpace(*token) == "" {
		log.Fatalf("--token is required")
	}
	service, err := nativeagent.NewService()
	if err != nil {
		log.Fatalf("initialize native agent service: %v", err)
	}
	updated, err := service.PairProfile(*profile, *token)
	if err != nil {
		log.Fatalf("pair profile: %v", err)
	}
	fmt.Printf("profile %s paired with connector %s\n", updated.Name, updated.ConnectorID)
}

func handleConfigCommand(args []string) {
	if len(args) == 0 {
		log.Fatalf("config command requires get or set")
	}
	service, err := nativeagent.NewService()
	if err != nil {
		log.Fatalf("initialize native agent service: %v", err)
	}

	switch args[0] {
	case "get":
		if len(args) < 2 {
			log.Fatalf("usage: proxer-agent config get <key>")
		}
		settings, err := service.Settings()
		if err != nil {
			log.Fatalf("load settings: %v", err)
		}
		switch args[1] {
		case "active_profile":
			fmt.Println(settings.ActiveProfileID)
		case "launch_mode":
			fmt.Println(settings.LaunchMode)
		case "start_at_login":
			fmt.Println(settings.StartAtLogin)
		case "schema_version":
			fmt.Println(settings.SchemaVersion)
		default:
			log.Fatalf("unknown config key %q", args[1])
		}
	case "set":
		if len(args) < 3 {
			log.Fatalf("usage: proxer-agent config set <key> <value>")
		}
		key := args[1]
		value := strings.TrimSpace(args[2])
		switch key {
		case "launch_mode":
			_, err := service.SetAppSettings(nativeagent.AppSettingsInput{LaunchMode: &value})
			if err != nil {
				log.Fatalf("update launch_mode: %v", err)
			}
			fmt.Println("launch_mode updated")
		case "start_at_login":
			parsed, err := strconv.ParseBool(value)
			if err != nil {
				log.Fatalf("parse start_at_login bool: %v", err)
			}
			_, err = service.SetAppSettings(nativeagent.AppSettingsInput{StartAtLogin: &parsed})
			if err != nil {
				log.Fatalf("update start_at_login: %v", err)
			}
			fmt.Println("start_at_login updated")
		default:
			log.Fatalf("unknown config key %q", key)
		}
	default:
		log.Fatalf("unknown config subcommand %q", args[0])
	}
}

func handleUpdateCommand(args []string) {
	if len(args) == 0 || args[0] != "check" {
		log.Fatalf("usage: proxer-agent update check")
	}
	service, err := nativeagent.NewService()
	if err != nil {
		log.Fatalf("initialize native agent service: %v", err)
	}
	result, err := service.CheckForUpdates()
	if err != nil {
		log.Fatalf("check updates: %v", err)
	}
	fmt.Printf("current version: %s\n", result.CurrentVersion)
	if strings.TrimSpace(result.LatestVersion) != "" {
		fmt.Printf("latest version: %s\n", result.LatestVersion)
	}
	if strings.TrimSpace(result.DownloadURL) != "" {
		fmt.Printf("download: %s\n", result.DownloadURL)
	}
	fmt.Println(result.Message)
}

func printUsage() {
	fmt.Print(`Proxer Agent (native + legacy modes)

Commands:
  proxer-agent gui
  proxer-agent run [--profile <name-or-id>]
  proxer-agent status [--json]
  proxer-agent logs [--follow] [--tail 200]
  proxer-agent profile list
  proxer-agent profile add --name <name> [--gateway URL] [--mode connector|legacy_tunnels]
  proxer-agent profile edit <name-or-id> [flags]
  proxer-agent profile remove <name-or-id>
  proxer-agent profile use <name-or-id>
  proxer-agent pair --token <pair_token> [--profile <name-or-id>]
  proxer-agent config get <key>
  proxer-agent config set <key> <value>
  proxer-agent update check

Compatibility mode:
  If PROXER_* env vars are present and no managed profile is specified,
  run behavior stays compatible with legacy env-based agent startup.
`)
}

func hasLegacyEnvConfig() bool {
	keys := []string{
		"PROXER_GATEWAY_BASE_URL",
		"PROXER_AGENT_TOKEN",
		"PROXER_AGENT_TUNNELS",
		"PROXER_AGENT_PAIR_TOKEN",
		"PROXER_AGENT_CONNECTOR_ID",
		"PROXER_AGENT_CONNECTOR_SECRET",
	}
	for _, key := range keys {
		if strings.TrimSpace(os.Getenv(key)) != "" {
			return true
		}
	}
	return false
}

func printTail(path string, lines int, out io.Writer) error {
	if lines <= 0 {
		lines = 200
	}
	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	buffer := make([]string, 0, lines)
	for scanner.Scan() {
		buffer = append(buffer, scanner.Text())
		if len(buffer) > lines {
			buffer = buffer[1:]
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	for _, line := range buffer {
		fmt.Fprintln(out, line)
	}
	return nil
}

func followFile(ctx context.Context, path string, out io.Writer) error {
	if ctx == nil {
		ctx = context.Background()
	}
	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	defer file.Close()

	offset, err := file.Seek(0, io.SeekEnd)
	if err != nil {
		return err
	}
	_ = offset
	buffer := make([]byte, 4096)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		n, err := file.Read(buffer)
		if err != nil {
			if errors.Is(err, io.EOF) {
				time.Sleep(400 * time.Millisecond)
				continue
			}
			return err
		}
		if n > 0 {
			_, _ = out.Write(buffer[:n])
		}
	}
}
