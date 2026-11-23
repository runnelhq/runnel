package connections

import "github.com/runnelhq/runnel/internal/connection"

// TypeSQLServer is the connection type of a Microsoft SQL Server database.
const TypeSQLServer = "sqlserver"

var sqlServerDefinition = connection.Definition{
	Type:        TypeSQLServer,
	Label:       "SQL Server",
	Description: "A Microsoft SQL Server database, with a user or with the Windows account Runnel runs as",
	Config:      SQLServer{},
	Warn: func(with map[string]any) map[string]string {
		w := map[string]string{}
		if v, _ := with["trustServerCertificate"].(bool); v {
			w["trustServerCertificate"] = "the certificate of the server is not checked; anyone between Runnel and the server could read and change the data"
		}
		if v, _ := with["encrypt"].(string); v == "false" {
			w["encrypt"] = "the connection is not encrypted (only the login is); use it only in a trusted network"
		}
		return w
	},
}

// SQLServer are the fields of a connection of type sqlserver.
type SQLServer struct {
	Host                   string `json:"host" doc:"Name or address of the server" schema:"required,example=db01.example.local,group=Server"`
	Port                   string `json:"port" doc:"TCP port; empty: 1433, or the port of the instance" schema:"example=1433,group=Server"`
	Instance               string `json:"instance" doc:"Named instance, e.g. SQLEXPRESS (needs the SQL Server Browser service); instead of a port" schema:"group=More"`
	Database               string `json:"database" doc:"Name of the database" schema:"required,example=ERP,group=Server"`
	User                   string `json:"user" doc:"SQL Server login; empty: the Windows account Runnel runs as (Windows only)" schema:"group=Sign-in"`
	Password               string `json:"password" doc:"Password of the login; use ${secret:NAME}" schema:"format=runnel-secret,group=Sign-in"`
	Encrypt                string `json:"encrypt" doc:"Encryption of the connection: true, strict (TDS 8) or false (only the login)" schema:"enum=true|strict|false,default=true,group=Security"`
	TrustServerCertificate bool   `json:"trustServerCertificate" doc:"Accept any certificate of the server, e.g. a self-signed one; validate warns" schema:"default=false,group=Security"`
	ConnectTimeout         string `json:"connectTimeout" doc:"Longest time to connect" schema:"format=runnel-duration,default=30s,group=More"`
}
