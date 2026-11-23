package connections

import (
	"fmt"
	"net/url"

	"github.com/runnelhq/runnel/internal/connection"
	"github.com/runnelhq/runnel/internal/httpx"
)

// TypeHTTP is the connection type of an HTTP API (ADR 0022, section 7).
const TypeHTTP = "http"

var httpDefinition = connection.Definition{
	Type:        TypeHTTP,
	Label:       "HTTP API",
	Description: "An HTTP API with its base address and sign-in, e.g. a REST API",
	Config:      HTTP{},
	Warn: func(with map[string]any) map[string]string {
		base, _ := with["baseUrl"].(string)
		if u, err := url.Parse(base); err == nil && u.Scheme == "http" && !httpx.IsLoopback(u.Host) {
			return map[string]string{"baseUrl": fmt.Sprintf("%s is not encrypted (http://); requests and answers can be read on the way", base)}
		}
		return nil
	},
}

// HTTP are the fields of a connection of type http.
type HTTP struct {
	BaseURL string            `json:"baseUrl" doc:"Base address of the API; url of http.request is relative to it, e.g. orders?status=open" schema:"required,example=https://api.example.com/v1/,group=API"`
	Headers map[string]string `json:"headers" doc:"Headers of every request, e.g. Accept; values may use ${secret:NAME}" schema:"format=runnel-secret,group=More"`
	Auth    string            `json:"auth" doc:"Sign-in: none, bearer (token), basic (user and password), apiKey (key in a header or the query) or oauth2 (client credentials)" schema:"enum=none|bearer|basic|apiKey|oauth2,default=none,group=Sign-in"`

	Token string `json:"token" doc:"Token for Authorization: Bearer; use ${secret:NAME}" schema:"format=runnel-secret,only=auth:bearer,group=Sign-in"`

	User     string `json:"user" doc:"User name" schema:"example=runnel,only=auth:basic,group=Sign-in"`
	Password string `json:"password" doc:"Password of the user; use ${secret:NAME}" schema:"format=runnel-secret,only=auth:basic,group=Sign-in"`

	APIKey     string `json:"apiKey" doc:"The key; use ${secret:NAME}" schema:"format=runnel-secret,only=auth:apiKey,group=Sign-in"`
	APIKeyName string `json:"apiKeyName" doc:"Name of the header or query parameter that carries the key" schema:"default=X-API-Key,only=auth:apiKey,group=Sign-in"`
	APIKeyIn   string `json:"apiKeyIn" doc:"Where the key goes: header or query" schema:"enum=header|query,default=header,only=auth:apiKey,group=Sign-in"`

	TokenURL     string `json:"tokenUrl" doc:"Token address of the provider (https://), e.g. https://login.microsoftonline.com/<tenant>/oauth2/v2.0/token" schema:"example=https://login.example.com/oauth2/token,only=auth:oauth2,group=Sign-in"`
	ClientID     string `json:"clientId" doc:"Client ID of the registered application" schema:"example=runnel,only=auth:oauth2,group=Sign-in"`
	ClientSecret string `json:"clientSecret" doc:"Client secret of the application; use ${secret:NAME}" schema:"format=runnel-secret,only=auth:oauth2,group=Sign-in"`
	Scope        string `json:"scope" doc:"Scope of the token, e.g. https://graph.microsoft.com/.default; empty: none" schema:"example=api://erp/.default,only=auth:oauth2,group=Sign-in"`
}
