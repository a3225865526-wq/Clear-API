package controller

import (
	"context"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"slices"
)

const (
	consoleProbeModel    = "gpt-6-luna"
	consoleProbeProtocol = "openai_responses"
)

type channelConsoleProbeAdapter struct{}

func (channelConsoleProbeAdapter) Authorize(ctx context.Context, m model.ConsoleMonitor) error {
	if m.TargetKind != "channel" || m.Protocol != consoleProbeProtocol || m.ChannelID != 1 || m.Model != consoleProbeModel {
		return model.ErrMonitorDenied
	}
	if !m.Allowlisted || !m.Published || !m.Enabled || m.Archived {
		return model.ErrMonitorPaused
	}
	channel, err := model.GetChannelById(m.ChannelID, true)
	if err != nil || channel.Status != common.ChannelStatusEnabled || channel.PoolChannelBound() || !slices.Contains(channel.GetModels(), m.Model) {
		return model.ErrMonitorDenied
	}
	return service.AuthorizeConsoleProbeCost(ctx, model.DB, m)
}

func (a channelConsoleProbeAdapter) Probe(ctx context.Context, m model.ConsoleMonitor, attemptID int64) (service.ConsoleProbeOutcome, error) {
	if err := a.Authorize(ctx, m); err != nil {
		return service.ConsoleProbeOutcome{}, err
	}
	return service.ProbeConsoleChannel(ctx, model.DB, m, attemptID)
}
