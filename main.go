// Command ocdiscord is a Discord <-> OpenCode bridge over ACP.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"oc-discord/internal/agent/opencode"
	"oc-discord/internal/agent/pool"
	"oc-discord/internal/bridge"
	"oc-discord/internal/config"
	"oc-discord/internal/discord"
	"oc-discord/internal/logx"
)

const version = "0.1.0"

func main() {
	var (
		root     = flag.String("root", "", "repository root (default: auto-detect)")
		deploy   = flag.Bool("deploy-commands", false, "register slash commands and exit")
		level    = flag.String("log-level", "", "log level (debug|info|warn|error)")
		ver      = flag.Bool("version", false, "print version and exit")
		printCfg = flag.Bool("print-config", false, "print resolved configuration and exit")
	)
	flag.Parse()

	if *ver {
		fmt.Printf("oc-discord %s\n", version)
		return
	}

	cfg := config.Load(*root)

	if *printCfg {
		printConfig(cfg)
		return
	}

	lvl := *level
	if lvl == "" {
		lvl = os.Getenv("OCD_LOG_LEVEL")
	}
	if lvl == "" {
		lvl = "info"
	}
	log := logx.New(lvl, cfg.LogDir)
	defer log.Close()

	log.Infof("[boot] oc-discord %s · root=%s · .env %d개 키 · state=%s", version, cfg.Root, len(cfg.EnvKeys), cfg.StatePath)
	log.Infof("[boot] 허용채널=%v · cwd루트=%s · 길드=%s", cfg.AllowedChannelIDs, cfg.CwdRoot, cfg.GuildID)

	transport := discord.New(discord.DiscordClientOption{
		Token:   cfg.Token,
		AppID:   cfg.AppID,
		GuildID: cfg.GuildID,
		Logger:  log,
	})
	agentPool := pool.New(pool.Options{
		Factory:         opencode.Factory(cfg, log),
		IdleShutdownMin: cfg.Bot.IdleShutdownMin,
		MaxAgents:       cfg.Bot.MaxAgents,
		Logger:          log,
	})
	bot := bridge.New(cfg, log, transport, agentPool)

	if *deploy {
		if err := bot.Deploy(); err != nil {
			log.Errorf("[boot] 커맨드 등록 실패: %v", err)
			os.Exit(1)
		}
		log.Infof("[boot] 커맨드 등록 완료")
		_ = transport.Close()
		return
	}

	if cfg.Token == "" {
		log.Errorf("[boot] DISCORD_BOT_TOKEN 이 비어 있습니다. .env 를 채우세요(.env.example 참고).")
		os.Exit(2)
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		ch := make(chan os.Signal, 1)
		signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
		<-ch
		log.Infof("[boot] 종료 신호 수신 — 정리 후 종료합니다")
		cancel()
	}()

	if err := bot.Run(ctx); err != nil {
		log.Errorf("[boot] 치명적 오류: %v", err)
		os.Exit(1)
	}
	log.Infof("[boot] 종료 완료")
}

// printConfig prints the resolved configuration (secrets shown only as presence).
func printConfig(cfg *config.Config) {
	presence := func(v string) string {
		if v == "" {
			return "(empty)"
		}
		return fmt.Sprintf("(set, %d chars)", len(v))
	}
	fmt.Printf("root              : %s\n", cfg.Root)
	fmt.Printf(".env keys applied : %d\n", len(cfg.EnvKeys))
	fmt.Printf("DISCORD_APP_ID    : %s\n", cfg.AppID)
	fmt.Printf("DISCORD_BOT_TOKEN : %s\n", presence(cfg.Token))
	fmt.Printf("owner user id     : %s\n", cfg.OwnerUserID)
	fmt.Printf("guild id          : %s\n", cfg.GuildID)
	fmt.Printf("mention role ids  : %v\n", cfg.MentionRoleIDs)
	fmt.Printf("respondWithoutMention: %v\n", cfg.RespondWithoutMention)
	fmt.Printf("allowed channels  : %v\n", cfg.AllowedChannelIDs)
	fmt.Printf("allowed categories: %v\n", cfg.AllowedCategoryIDs)
	fmt.Printf("cwd root          : %s\n", cfg.CwdRoot)
	fmt.Printf("cwd overrides     : %v\n", cfg.CwdOverrides)
	fmt.Printf("opencode bin      : %s\n", orAuto(cfg.OpenCodeBin))
	fmt.Printf("state path        : %s\n", cfg.StatePath)
	fmt.Printf("log dir           : %s\n", cfg.LogDir)
	fmt.Printf("deploy on start   : %v\n", cfg.DeployCommands)
	fmt.Printf("bot config        : permissions=%s timeout=%ds threads=%v stream=%v editMs=%d maxChunk=%d showTools=%v idleMin=%d maxAgents=%d cmdTimeout=%ds\n",
		cfg.Bot.Permissions, cfg.Bot.PermissionTimeoutSec, cfg.Bot.Threads, cfg.Bot.Stream,
		cfg.Bot.StreamEditMS, cfg.Bot.MaxChunk, cfg.Bot.ShowTools, cfg.Bot.IdleShutdownMin, cfg.Bot.MaxAgents, cfg.Bot.CmdTimeoutSec)
	fmt.Printf("opencode args     : %v\n", cfg.Bot.OpenCodeArgs)
}

func orAuto(s string) string {
	if s == "" {
		return "(auto-resolve)"
	}
	return s
}
