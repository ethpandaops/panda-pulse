package grafana

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
