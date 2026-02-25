package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/sirupsen/logrus"
)

func main() {
	var (
		token      = flag.String("token", os.Getenv("DISCORD_BOT_TOKEN"), "Discord bot token")
		guildID    = flag.String("guild", os.Getenv("DISCORD_GUILD_ID"), "Guild ID to manage commands for")
		dryRun     = flag.Bool("dry-run", true, "If true, only list commands without deleting")
		keepGuild  = flag.Bool("keep-guild", true, "Keep guild-specific commands")
		keepGlobal = flag.Bool("keep-global", false, "Keep global commands")
	)

	flag.Parse()

	if *token == "" {
		fmt.Println("Error: Discord bot token is required (use -token flag or DISCORD_BOT_TOKEN env var)")
		os.Exit(1)
	}

	log := logrus.New()
	log.SetFormatter(&logrus.TextFormatter{
		FullTimestamp: true,
	})

	// Create Discord session
	session, err := discordgo.New("Bot " + *token)
	if err != nil {
		log.Fatalf("Failed to create Discord session: %v", err)
	}

	// Open connection
	if openErr := session.Open(); openErr != nil {
		log.Fatalf("Failed to open Discord connection: %v", openErr)
	}
	defer session.Close()

	// Wait for connection to be ready
	time.Sleep(1 * time.Second)

	ctx := context.Background()

	// List and manage global commands
	log.Info("Fetching global commands...")

	globalCommands, err := session.ApplicationCommands(session.State.User.ID, "")
	if err != nil {
		log.Errorf("Failed to fetch global commands: %v", err)
	} else {
		log.Infof("Found %d global commands:", len(globalCommands))

		for _, cmd := range globalCommands {
			log.Infof("  - %s (ID: %s)", cmd.Name, cmd.ID)

			if !*dryRun && !*keepGlobal {
				log.Infof("    -> Deleting global command: %s", cmd.Name)

				if err := session.ApplicationCommandDelete(session.State.User.ID, "", cmd.ID); err != nil {
					log.Errorf("    -> Failed to delete: %v", err)
				} else {
					log.Info("    -> Deleted successfully")
				}
			}
		}
	}

	// List and manage guild-specific commands if guild ID is provided
	if *guildID != "" {
		log.Infof("Fetching guild-specific commands for guild %s...", *guildID)

		guildCommands, err := session.ApplicationCommands(session.State.User.ID, *guildID)
		if err != nil {
			log.Errorf("Failed to fetch guild commands: %v", err)
		} else {
			log.Infof("Found %d guild-specific commands:", len(guildCommands))

			// Track command names to identify duplicates
			commandCounts := make(map[string]int)
			commandsByName := make(map[string][]*discordgo.ApplicationCommand)

			for _, cmd := range guildCommands {
				log.Infof("  - %s (ID: %s)", cmd.Name, cmd.ID)
				commandCounts[cmd.Name]++
				commandsByName[cmd.Name] = append(commandsByName[cmd.Name], cmd)
			}

			// Report duplicates
			for name, count := range commandCounts {
				if count > 1 {
					log.Warnf("Found %d duplicate registrations of command: %s", count, name)

					if !*dryRun && *keepGuild {
						// Keep only the most recent one (last in the list)
						cmds := commandsByName[name]
						for i := 0; i < len(cmds)-1; i++ {
							log.Infof("    -> Deleting duplicate: %s (ID: %s)", cmds[i].Name, cmds[i].ID)

							if err := session.ApplicationCommandDelete(session.State.User.ID, *guildID, cmds[i].ID); err != nil {
								log.Errorf("    -> Failed to delete: %v", err)
							} else {
								log.Info("    -> Deleted successfully")
							}
						}

						log.Infof("    -> Keeping: %s (ID: %s)", cmds[len(cmds)-1].Name, cmds[len(cmds)-1].ID)
					}
				}
			}

			// Delete all guild commands if not keeping them
			if !*dryRun && !*keepGuild {
				for _, cmd := range guildCommands {
					log.Infof("    -> Deleting guild command: %s", cmd.Name)

					if err := session.ApplicationCommandDelete(session.State.User.ID, *guildID, cmd.ID); err != nil {
						log.Errorf("    -> Failed to delete: %v", err)
					} else {
						log.Info("    -> Deleted successfully")
					}
				}
			}
		}
	}

	if *dryRun {
		log.Info("\n=== DRY RUN MODE ===")
		log.Info("No commands were deleted. To actually delete commands, run with -dry-run=false")
		log.Info("\nRecommended cleanup:")

		if !*keepGlobal && len(globalCommands) > 0 {
			log.Info("  - Delete all global commands")
		}

		if *keepGuild && *guildID != "" {
			log.Info("  - Keep guild-specific commands, remove duplicates only")
		}

		log.Info("\nRun with: -dry-run=false -keep-global=false -keep-guild=true")
	} else {
		log.Info("\n=== CLEANUP COMPLETE ===")

		_ = ctx // silence unused variable warning
	}
}
