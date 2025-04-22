package discord

import "github.com/prometheus/client_golang/prometheus"

type Metrics struct {
	commandsTotal   *prometheus.CounterVec
	commandErrors   *prometheus.CounterVec
	commandDuration *prometheus.HistogramVec
	lastCommandTS   *prometheus.GaugeVec
}

func NewMetrics(namespace string) *Metrics {
	m := &Metrics{
		commandsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "discord",
			Name:      "commands_total",
			Help:      "Total number of commands executed",
		}, []string{"command", "subcommand", "username"}),

		commandErrors: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "discord",
			Name:      "command_errors_total",
			Help:      "Total number of command errors",
		}, []string{"command", "subcommand", "error_type"}),

		commandDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: "discord",
			Name:      "command_duration_seconds",
			Help:      "Time taken to execute commands",
			Buckets:   []float64{0.01, 0.05, 0.1, 0.5, 1, 5, 10},
		}, []string{"command", "subcommand"}),

		lastCommandTS: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: "discord",
			Name:      "last_command_timestamp",
			Help:      "Timestamp of last command execution",
		}, []string{"command", "subcommand"}),
	}

	prometheus.MustRegister(
		m.commandsTotal,
		m.commandErrors,
		m.commandDuration,
		m.lastCommandTS,
	)

	return m
}

// RecordCommandExecution increments the command execution counter.
func (m *Metrics) RecordCommandExecution(command, subcommand, username string) {
	m.commandsTotal.WithLabelValues(command, subcommand, username).Inc()
}

// RecordCommandError increments the command error counter.
func (m *Metrics) RecordCommandError(command, subcommand, errorType string) {
	m.commandErrors.WithLabelValues(command, subcommand, errorType).Inc()
}

// ObserveCommandDuration records the duration of a command execution.
func (m *Metrics) ObserveCommandDuration(command, subcommand string, duration float64) {
	m.commandDuration.WithLabelValues(command, subcommand).Observe(duration)
}

// SetLastCommandTimestamp sets the timestamp of the last command execution.
func (m *Metrics) SetLastCommandTimestamp(command, subcommand string, timestamp float64) {
	m.lastCommandTS.WithLabelValues(command, subcommand).Set(timestamp)
}
