package grafana

// Config contains the configuration for the Grafana client.
type Config struct {
	Token            string
	PromDatasourceID string
	BaseURL          string
}

// QueryField represents a field in the Grafana response.
type QueryField struct {
	Labels map[string]string `json:"labels"`
}

// QuerySchema represents the schema in the Grafana response.
type QuerySchema struct {
	Fields []QueryField `json:"fields"`
}

// QueryData represents the data in the Grafana response.
type QueryData struct {
	Values []interface{} `json:"values"`
}

// QueryFrame represents a frame in the Grafana response.
type QueryFrame struct {
	Schema QuerySchema `json:"schema"`
	Data   QueryData   `json:"data"`
}

// QueryPandaPulse represents the PandaPulse section in the Grafana response.
type QueryPandaPulse struct {
	Frames []QueryFrame `json:"frames"`
}

// QueryResults represents the results in the Grafana response.
type QueryResults struct {
	PandaPulse QueryPandaPulse `json:"pandaPulse"`
}

// QueryResponse is the response from a Grafana query.
type QueryResponse struct {
	Results QueryResults `json:"results"`
}

// queryPayload represents the common structure for Grafana queries.
type queryPayload struct {
	Queries []query `json:"queries"`
	From    string  `json:"from"`
	To      string  `json:"to"`
}

type query struct {
	RefID         string                 `json:"refId"`
	Datasource    map[string]interface{} `json:"datasource"`
	Expr          string                 `json:"expr"`
	MaxDataPoints int                    `json:"maxDataPoints"`
	IntervalMs    int                    `json:"intervalMs"`
	Interval      string                 `json:"interval"`
	LegendFormat  string                 `json:"legendFormat,omitempty"`
}
