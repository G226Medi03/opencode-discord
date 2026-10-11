// Package opencode wires the OpenCode CLI (`opencode acp`) into an agent.Factory.
package opencode

import (
	"os"
	"strings"

	"oc-discord/internal/acp"
	"oc-discord/internal/agent"
	"oc-discord/internal/config"
	"oc-discord/internal/launcher"
	"oc-discord/internal/logx"
)

// Factory builds an agent.Factory that spawns `opencode acp` processes.
func Factory(cfg *config.Config, log *logx.Logger) agent.Factory {
	spec := launcher.Resolve(cfg.OpenCodeBin)
	log.Infof("[opencode] launcher: %s -> %s", spec.Via, spec.Command)
	return func(key string, onExit func(error)) (agent.Agent, error) {
		args := append([]string{}, spec.Args...)
		args = append(args, cfg.Bot.OpenCodeArgs...)
		args = append(args, "acp")
		return acp.NewAgent(acp.Options{
			Command: spec.Command,
			Args:    args,
			Cwd:     key,
			Env:     os.Environ(),
			Logger:  log,
			OnExit:  onExit,
			OnStderr: func(line string) {
				log.Debugf("[acp:stderr] %s", strings.TrimRight(line, "\r\n"))
			},
		}), nil
	}
}
