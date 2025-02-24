package message

import (
	"bytes"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/ethpandaops/panda-pulse/pkg/checks"
	"github.com/ethpandaops/panda-pulse/pkg/clients"
	"github.com/ethpandaops/panda-pulse/pkg/store"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

const (
	affectedInstancesHeader = "\n**Affected instances**\n```bash\n"
	sshCommandsHeader       = "\n**SSH commands**\n"
	codeBlockEnd            = "```"
	defaultCategoryEmoji    = "ℹ️"
)

var (
	// Category emojis for different check categories.
	categoryEmojis = map[checks.Category]string{
		checks.CategorySync: "🔄",
	}
	// Detail keys in result sets that we care about. Results are stored as a map[string]interface{}
	// and return all sorts of data, so we cherry pick the ones we want to determine alert info.
	relevantDetailKeys = []string{"lowPeerNodes", "notSyncedNodes", "stuckNodes", "behindNodes"}
)

// AlertMessageBuilder builds the alert message.
type AlertMessageBuilder struct {
	alert          *store.MonitorAlert
	checkID        string
	results        []*checks.Result
	hiveAvailable  bool
	grafanaBaseURL string
	hiveBaseURL    string
}

type Config struct {
	CheckID        string
	Alert          *store.MonitorAlert
	Results        []*checks.Result
	HiveAvailable  bool
	GrafanaBaseURL string
	HiveBaseURL    string
}

// NewAlertMessageBuilder creates a new AlertMessageBuilder.
func NewAlertMessageBuilder(cfg *Config) *AlertMessageBuilder {
	return &AlertMessageBuilder{
		alert:          cfg.Alert,
		checkID:        cfg.CheckID,
		results:        cfg.Results,
		hiveAvailable:  cfg.HiveAvailable,
		grafanaBaseURL: cfg.GrafanaBaseURL,
		hiveBaseURL:    cfg.HiveBaseURL,
	}
}

// BuildMainMessage builds the main message.
func (b *AlertMessageBuilder) BuildMainMessage() *discordgo.MessageSend {
	msg := &discordgo.MessageSend{
		Embed:      b.buildMainEmbed(),
		Components: b.buildActionButtons(),
	}

	return msg
}

// BuildThreadMessages builds the category message.
func (b *AlertMessageBuilder) BuildThreadMessages(category checks.Category, failedChecks []*checks.Result) []string {
	var messages []string

	header := fmt.Sprintf(
		"\n\n**%s %s Issues**\n------------------------------------------\n",
		b.getCategoryEmoji(category),
		category.String(),
	)

	header += "**Issues detected**\n"

	names := b.getUniqueCheckNames(failedChecks)
	for name := range names {
		header += fmt.Sprintf("- %s\n", name)
	}

	messages = append(messages, header)

	instances := b.extractInstances(failedChecks)
	if len(instances) > 0 {
		messages = append(messages, b.buildInstanceList(instances))
		messages = append(messages, b.buildSSHCommands(instances))
	}

	return messages
}

// BuildHiveMessage builds the Hive message.
func (b *AlertMessageBuilder) BuildHiveMessage(content []byte) *discordgo.MessageSend {
	return &discordgo.MessageSend{
		Content: "\n**Hive Summary**",
		Files: []*discordgo.File{
			{
				Name:        fmt.Sprintf("hive-%s-%s.png", b.alert.Client, b.checkID),
				ContentType: "image/png",
				Reader:      bytes.NewReader(content),
			},
		},
	}
}

// BuildMentionMessage builds the mention message.
func (b *AlertMessageBuilder) BuildMentionMessage(mentions []string) *discordgo.MessageSend {
	return &discordgo.MessageSend{
		Content: strings.Join(mentions, " "),
	}
}

// getUniqueCheckNames returns a map of unique check names.
func (b *AlertMessageBuilder) getUniqueCheckNames(checks []*checks.Result) map[string]bool {
	names := make(map[string]bool)

	for _, check := range checks {
		if _, ok := names[check.Name]; !ok {
			names[check.Name] = true
		}
	}

	return names
}

// extractInstances extracts the instances from the checks.
func (b *AlertMessageBuilder) extractInstances(checks []*checks.Result) map[string]bool {
	instances := make(map[string]bool)

	for _, check := range checks {
		b.extractInstancesFromCheck(check, instances)
	}

	return instances
}

// extractInstancesFromCheck extracts instances from a single check result.
func (b *AlertMessageBuilder) extractInstancesFromCheck(check *checks.Result, instances map[string]bool) {
	if check.Details == nil {
		return
	}

	for k, v := range check.Details {
		if !b.isRelevantDetailKey(k) {
			continue
		}

		str, ok := v.(string)
		if !ok {
			continue
		}

		b.parseInstancesFromString(str, instances)
	}
}

// isRelevantDetailKey checks if the detail key is one we care about.
func (b *AlertMessageBuilder) isRelevantDetailKey(key string) bool {
	for _, k := range relevantDetailKeys {
		if key == k {
			return true
		}
	}

	return false
}

// parseInstancesFromString parses instances from a multiline string.
func (b *AlertMessageBuilder) parseInstancesFromString(str string, instances map[string]bool) {
	for _, line := range strings.Split(str, "\n") {
		if instance := b.parseInstanceFromLine(line); instance != "" {
			instances[instance] = true
		}
	}
}

// parseInstanceFromLine extracts an instance name from a single line.
func (b *AlertMessageBuilder) parseInstanceFromLine(line string) string {
	parts := strings.Fields(line)
	if len(parts) == 0 {
		return ""
	}

	instance := parts[0]
	if strings.HasPrefix(instance, "(") && len(parts) > 1 {
		instance = parts[1]
	}

	instance = strings.Split(instance, " (")[0]

	// Split the instance name into parts.
	nodeParts := strings.Split(instance, "-")
	if len(nodeParts) < 2 {
		return ""
	}

	// Match exactly the CL or EL client name.
	if nodeParts[0] == b.alert.Client || // CL client
		(len(nodeParts) > 1 && nodeParts[1] == b.alert.Client) { // EL client
		return instance
	}

	return ""
}

// buildInstanceList builds the instance list.
func (b *AlertMessageBuilder) buildInstanceList(instances map[string]bool) string {
	sortedInstances := b.getSortedInstances(instances)

	var sb strings.Builder

	sb.WriteString(affectedInstancesHeader)

	for _, inst := range sortedInstances {
		sb.WriteString(inst.name)
		sb.WriteString("\n")
	}

	sb.WriteString(codeBlockEnd)

	return sb.String()
}

// buildSSHCommands builds the SSH commands.
func (b *AlertMessageBuilder) buildSSHCommands(instances map[string]bool) string {
	sortedInstances := b.getSortedInstances(instances)

	var sb strings.Builder

	sb.WriteString(sshCommandsHeader)

	for _, inst := range sortedInstances {
		sb.WriteString("```bash\n")
		sb.WriteString(inst.sshCommand())
		sb.WriteString(codeBlockEnd)
		sb.WriteString("\n")
	}

	return sb.String()
}

// getSortedInstances sorts the instances.
func (b *AlertMessageBuilder) getSortedInstances(instances map[string]bool) []instance {
	sorted := make([]instance, 0, len(instances))
	for name := range instances {
		sorted = append(sorted, newInstance(name, b.alert.Network, b.alert.Client))
	}

	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].name < sorted[j].name
	})

	return sorted
}

// getCategoryEmoji returns the emoji for the category.
func (b *AlertMessageBuilder) getCategoryEmoji(category checks.Category) string {
	if emoji, ok := categoryEmojis[category]; ok {
		return emoji
	}

	return defaultCategoryEmoji
}

// buildGrafanaURL returns the Grafana URL.
func (b *AlertMessageBuilder) buildGrafanaURL(dashboard string, params map[string]string) string {
	baseURL := fmt.Sprintf("%s/d/%s", b.grafanaBaseURL, dashboard)

	if len(params) == 0 {
		return baseURL
	}

	queryParams := make([]string, 0, len(params))
	for k, v := range params {
		queryParams = append(queryParams, fmt.Sprintf("%s=%s", k, v))
	}

	return fmt.Sprintf("%s?%s", baseURL, strings.Join(queryParams, "&"))
}

// buildMainEmbed builds the main embed.
func (b *AlertMessageBuilder) buildMainEmbed() *discordgo.MessageEmbed {
	// Count unique failed checks.
	uniqueFailedChecks := make(map[string]bool)

	for _, result := range b.results {
		if result.Status == checks.StatusFail {
			uniqueFailedChecks[result.Name] = true
		}
	}

	embed := &discordgo.MessageEmbed{
		Title:     b.getTitle(),
		Color:     hashToColor(b.alert.Network),
		Timestamp: time.Now().Format(time.RFC3339),
		Fields:    make([]*discordgo.MessageEmbedField, 0),
	}

	if logo := clients.GetClientLogo(b.alert.Client); logo != "" {
		embed.Thumbnail = &discordgo.MessageEmbedThumbnail{
			URL: logo,
		}
	}

	embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{
		Name:   fmt.Sprintf("%s %d Active Issues", "⚠️", len(uniqueFailedChecks)),
		Inline: true,
	})

	embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{
		Name:   fmt.Sprintf("🌐 %s", b.alert.Network),
		Inline: true,
	})

	embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{
		Value:  "Check the thread below for a breakdown",
		Inline: false,
	})

	embed.Footer = &discordgo.MessageEmbedFooter{
		Text: fmt.Sprintf("ID: %s", b.checkID),
	}

	return embed
}

// buildActionButtons builds the action buttons.
func (b *AlertMessageBuilder) buildActionButtons() []discordgo.MessageComponent {
	executionClient := "All"
	consensusClient := "All"

	if clients.IsELClient(b.alert.Client) {
		executionClient = b.alert.Client
	}

	if clients.IsCLClient(b.alert.Client) {
		consensusClient = b.alert.Client
	}

	btns := []discordgo.MessageComponent{
		discordgo.Button{
			Label: "📊 Grafana",
			Style: discordgo.LinkButton,
			URL:   b.buildGrafanaURL("cebekx08rl9tsc", map[string]string{"orgId": "1", "var-consensus_client": consensusClient, "var-execution_client": executionClient, "var-network": b.alert.Network}),
		},
		discordgo.Button{
			Label: "📝 Logs",
			Style: discordgo.LinkButton,
			URL:   b.buildGrafanaURL("aebfg1654nqwwd", map[string]string{"orgId": "1", "var-network": b.alert.Network}),
		},
	}

	if b.hiveAvailable {
		btns = append(btns, discordgo.Button{
			Label: "🐝 Hive",
			Style: discordgo.LinkButton,
			URL:   fmt.Sprintf("%s/%s/index.html#summary-sort=name&group-by=client", b.hiveBaseURL, b.alert.Network),
		})
	}

	return []discordgo.MessageComponent{
		discordgo.ActionsRow{
			Components: btns,
		},
	}
}

// Helper method to get the title.
func (b *AlertMessageBuilder) getTitle() string {
	if b.alert.Client != "" {
		return cases.Title(language.English, cases.Compact).String(b.alert.Client)
	}

	return b.alert.Network
}
