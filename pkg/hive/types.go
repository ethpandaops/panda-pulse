package hive

import (
	"time"
)

// TestResult represents a single test result from Hive
type TestResult struct {
	Name        string            `json:"name"`
	Client      string            `json:"client"`
	Version     string            `json:"version"`
	NTests      int               `json:"ntests"`
	Passes      int               `json:"passes"`
	Fails       int               `json:"fails"`
	FileName    string            `json:"fileName"`
	Timestamp   time.Time         `json:"timestamp"`
	TestSuiteID string            `json:"testSuiteId"`
	Clients     []string          `json:"clients"`
	Versions    map[string]string `json:"versions"`
}

// ClientSummary represents a summary of test results for a specific client
type ClientSummary struct {
	ClientName    string
	ClientVersion string
	TotalTests    int
	PassedTests   int
	FailedTests   int
	PassRate      float64
	TestTypes     []string
}

// SummaryResult represents the overall summary of Hive test results
type SummaryResult struct {
	Network         string
	Timestamp       time.Time
	TotalTests      int
	TotalPasses     int
	TotalFails      int
	OverallPassRate float64
	ClientResults   map[string]*ClientSummary
	TestTypes       map[string]struct{} // Set of unique test types
}

// HiveSummaryAlert represents a Hive summary alert configuration
type HiveSummaryAlert struct {
	Network        string    `json:"network"`
	DiscordChannel string    `json:"discordChannel"`
	DiscordGuildID string    `json:"discordGuildId"`
	Enabled        bool      `json:"enabled"`
	Schedule       string    `json:"schedule"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
}
