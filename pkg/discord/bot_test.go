package discord

import (
	"testing"

	"github.com/bwmarrin/discordgo"
	"github.com/stretchr/testify/assert"
)

func TestCommandSelfChecksPermission(t *testing.T) {
	// optionsWith builds interaction data with a single top-level subcommand
	// option name, mirroring how Discord shapes /<command> <subcommand> calls.
	optionsWith := func(subcommand string) *discordgo.ApplicationCommandInteractionData {
		return &discordgo.ApplicationCommandInteractionData{
			Options: []*discordgo.ApplicationCommandInteractionDataOption{
				{Name: subcommand},
			},
		}
	}

	tests := []struct {
		name    string
		cmdName string
		data    *discordgo.ApplicationCommandInteractionData
		want    bool
	}{
		// /build owns its own permission check for every subcommand. Regression
		// guard: before the fix, only `trigger` was bypassed and the renamed
		// `client-cl` / `client-el` / `tool` subcommands fell through to the
		// strict per-client check, blocking e.g. a geth-tagged user from
		// running /build client-cl.
		{name: "build client-cl bypasses", cmdName: "build", data: optionsWith("client-cl"), want: true},
		{name: "build client-el bypasses", cmdName: "build", data: optionsWith("client-el"), want: true},
		{name: "build tool bypasses", cmdName: "build", data: optionsWith("tool"), want: true},
		{name: "build with no options bypasses", cmdName: "build", data: &discordgo.ApplicationCommandInteractionData{}, want: true},

		// /hive still uses the legacy `trigger` subcommand naming.
		{name: "hive trigger bypasses", cmdName: "hive", data: optionsWith("trigger"), want: true},
		{name: "hive non-trigger does not bypass", cmdName: "hive", data: optionsWith("summary"), want: false},
		{name: "hive with no options does not bypass", cmdName: "hive", data: &discordgo.ApplicationCommandInteractionData{}, want: false},
		{name: "hive with nil data does not bypass", cmdName: "hive", data: nil, want: false},

		// Every other command goes through the dispatcher's generic permission check.
		{name: "checks does not bypass", cmdName: "checks", data: optionsWith("run"), want: false},
		{name: "mentions does not bypass", cmdName: "mentions", data: optionsWith("add"), want: false},
		{name: "unknown command does not bypass", cmdName: "whatever", data: optionsWith("trigger"), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, commandSelfChecksPermission(tt.cmdName, tt.data))
		})
	}
}
