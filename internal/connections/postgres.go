package connections

import "github.com/runnelhq/runnel/internal/connection"

// TypePostgres is the connection type of a PostgreSQL database.
const TypePostgres = "postgres"

var postgresDefinition = connection.Definition{
	Type:        TypePostgres,
	Label:       "PostgreSQL",
	Description: "A PostgreSQL database",
	Config:      Postgres{},
	Renamed:     map[string]string{"connectTimeout": "timeout (as in the other connection types)"},
	Warn: func(with map[string]any) map[string]string {
		switch v, _ := with["sslMode"].(string); v {
		case "disable":
			return map[string]string{"sslMode": "the connection is not encrypted; use it only in a trusted network"}
		case "require":
			return map[string]string{"sslMode": "the certificate of the server is not checked; anyone between Runnel and the server could read and change the data"}
		}
		return nil
	},
}

// Postgres are the fields of a connection of type postgres.
type Postgres struct {
	Host     string `json:"host" doc:"Name or address of the server" schema:"required,example=db01.example.local,group=Server"`
	Port     string `json:"port" doc:"TCP port; empty: 5432" schema:"example=5432,group=Server"`
	Database string `json:"database" doc:"Name of the database" schema:"required,example=shop,group=Server"`
	User     string `json:"user" doc:"User name" schema:"required,example=runnel,group=Sign-in"`
	Password string `json:"password" doc:"Password of the user; use ${secret:NAME}" schema:"format=runnel-secret,group=Sign-in"`
	SSLMode  string `json:"sslMode" doc:"TLS: verify-full (encrypted, certificate and name checked), verify-ca, require (encrypted, not checked) or disable" schema:"enum=verify-full|verify-ca|require|disable,default=verify-full,group=Security"`
	// Timeout is called as in the other connection types (R5b, before: connectTimeout).
	Timeout string `json:"timeout" doc:"Longest time to connect; queries and statements have their own timeout in the action or transform" schema:"format=runnel-duration,default=30s,group=More"`
}
