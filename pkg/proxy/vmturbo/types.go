package vmturbo

type Transaction struct {
	ServiceId           string         `json:"serviceID,omitempty"`
	EndpointsCounterMap map[string]int `json:"endpointCounter,omitempty"`
}
