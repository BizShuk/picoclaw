package config

import "time"

type A2ASettings struct {
	AgentID          string        `json:"agent_id"          yaml:"agent_id"          env:"PICOCLAW_CHANNELS_A2A_AGENT_ID"`
	Port             int           `json:"port"              yaml:"port"              env:"PICOCLAW_CHANNELS_A2A_PORT"`
	BindAddr         string        `json:"bind_addr"         yaml:"bind_addr"         env:"PICOCLAW_CHANNELS_A2A_BIND_ADDR"`
	Description      string        `json:"description"       yaml:"description"`
	AnnounceInterval time.Duration `json:"announce_interval" yaml:"announce_interval"`
	PeerTTL          time.Duration `json:"peer_ttl"          yaml:"peer_ttl"`
	MaxTurnDefault   int           `json:"max_turn_default"  yaml:"max_turn_default"`
	AskTimeout       time.Duration `json:"ask_timeout"       yaml:"ask_timeout"`
	DialTimeout      time.Duration `json:"dial_timeout"      yaml:"dial_timeout"`
	IdleConnTTL      time.Duration `json:"idle_conn_ttl"     yaml:"idle_conn_ttl"`
	MDNSDomain       string        `json:"mdns_domain"       yaml:"mdns_domain"`
	ServiceType      string        `json:"service_type"      yaml:"service_type"`
}
