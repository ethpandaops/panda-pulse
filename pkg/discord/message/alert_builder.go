package message

import (
	"bytes"
	"fmt"
	"net"
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
	affectedInstancesHeader                = "\n**Affected instances**\n```bash\n"
	affectedInstancesLikelyUnrelatedHeader = "\n**Affected instances (likely unrelated)**\n```bash\n"
	infrastructureIssuesHeader             = "\n**Potential infrastructure issues**\n```bash\n"
	sshCommandsHeader                      = "\n**SSH commands**\n"
	codeBlockEnd                           = "```"
	defaultCategoryEmoji                   = "ℹ️"
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
	alert                      *store.MonitorAlert
	checkID                    string
	results                    []*checks.Result
	hiveAvailable              bool
	grafanaBaseURL             string
	hiveBaseURL                string
	rootCauses                 []string // List of clients determined to be root causes
	onlyInfraOrUnrelatedIssues bool     // Flag to indicate if only infrastructure or unrelated issues were detected
}

type Config struct {
	CheckID        string
	Alert          *store.MonitorAlert
	Results        []*checks.Result
	HiveAvailable  bool
	GrafanaBaseURL string
	HiveBaseURL    string
	RootCauses     []string // List of clients determined to be root causes
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
		rootCauses:     cfg.RootCauses,
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
		instanceList := b.buildInstanceList(instances)
		messages = append(messages, instanceList)
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

	// Create a map of root causes for faster lookups.
	rootCauseMap := make(map[string]bool)
	for _, client := range b.rootCauses {
		rootCauseMap[client] = true
	}

	// Check if the current client is itself a root cause.
	isClientRootCause := rootCauseMap[b.alert.Client]

	// Categorise instances.
	regularInstances := make([]instance, 0)
	unrelatedInstances := make([]instance, 0)
	infrastructureIssues := make([]instance, 0)

	for _, inst := range sortedInstances {
		// Check if we might classify this as an infrastructure issue.
		if !b.checkInfrastructureHealth(inst.name) {
			infrastructureIssues = append(infrastructureIssues, inst)

			continue
		}

		// If the client itself is a root cause, all instances are related.
		if isClientRootCause {
			regularInstances = append(regularInstances, inst)

			continue
		}

		// Extract client parts from instance name.
		parts := strings.Split(inst.name, "-")
		if len(parts) < 2 {
			regularInstances = append(regularInstances, inst)

			continue
		}

		// Check if either component is a pre-production client or a root cause.
		var (
			clClient = parts[0]
			elClient string
		)

		if len(parts) > 1 {
			elClient = parts[1]
		}

		if clients.IsPreProductionClient(clClient) || clients.IsPreProductionClient(elClient) ||
			rootCauseMap[clClient] || rootCauseMap[elClient] {
			unrelatedInstances = append(unrelatedInstances, inst)
		} else {
			regularInstances = append(regularInstances, inst)
		}
	}

	var sb strings.Builder

	// Infrastructure issues.
	if len(infrastructureIssues) > 0 {
		sb.WriteString(infrastructureIssuesHeader)

		for _, inst := range infrastructureIssues {
			sb.WriteString(inst.name)
			sb.WriteString("\n")
		}

		sb.WriteString(codeBlockEnd)
	}

	// Regular instances.
	if len(regularInstances) > 0 {
		sb.WriteString(affectedInstancesHeader)

		for _, inst := range regularInstances {
			sb.WriteString(inst.name)
			sb.WriteString("\n")
		}

		sb.WriteString(codeBlockEnd)
	}

	// Likely unrelated instances (eg, ethereumjs the root cause, failing for everyone).
	if len(unrelatedInstances) > 0 {
		sb.WriteString(affectedInstancesLikelyUnrelatedHeader)

		for _, inst := range unrelatedInstances {
			sb.WriteString(inst.name)
			sb.WriteString("\n")
		}

		sb.WriteString(codeBlockEnd)
	}

	// If all issues can be classified as infrastructure issues, set the flag.
	if len(infrastructureIssues) > 0 &&
		len(regularInstances) == 0 &&
		len(unrelatedInstances) == 0 {
		b.onlyInfraOrUnrelatedIssues = true
	}

	// If issues are infrastructure, or classed as unrelated (not-likely root-cause), we won't alert either.
	if len(infrastructureIssues) > 0 && len(regularInstances) == 0 && len(unrelatedInstances) > 0 {
		b.onlyInfraOrUnrelatedIssues = true
	}

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

// checkInfrastructureHealth checks if a machine is responsive by attempting to connect to SSH port
// and validating the SSH handshake starts successfully. A good indicator of a machine being unresponsive
// hinting at a potential infrastructure issue over a client issue.
func (b *AlertMessageBuilder) checkInfrastructureHealth(instanceName string) bool {
	// Build the hostname.
	hostname := fmt.Sprintf("%s.%s.ethpandaops.io", instanceName, b.alert.Network)
	fullHostPort := fmt.Sprintf("%s:22", hostname)

	// First try a basic TCP connection with a short timeout (2 seconds).
	conn, err := net.DialTimeout("tcp", fullHostPort, 2*time.Second)
	if err != nil {
		// Failed to connect - machine has shat the bed?
		return false
	}

	// Set a read deadline to detect hung services. This is blocking.
	if deadlineErr := conn.SetReadDeadline(time.Now().Add(3 * time.Second)); deadlineErr != nil {
		return false
	}

	// Read just a few bytes - SSH server should immediately send identification string
	// We don't need to send anything first for the initial banner.
	buf := make([]byte, 8)
	_, err = conn.Read(buf)

	// Close the connection regardless of result.
	conn.Close()

	// If we couldn't read the SSH banner, the service is hung.
	if err != nil {
		return false
	}

	// Check if the first bytes look like an SSH banner (typically starts with "SSH-").
	if len(buf) >= 4 && string(buf[:4]) == "SSH-" {
		return true
	}

	// If we got data but it doesn't look like SSH, then fail.
	return false
}

// HasOnlyInfraOrUnrelatedIssues returns true if all issues detected are infrastructure or unrelated.
func (b *AlertMessageBuilder) HasOnlyInfraOrUnrelatedIssues() bool {
	return b.onlyInfraOrUnrelatedIssues
}
