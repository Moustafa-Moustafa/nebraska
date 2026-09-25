package handler

import (
	"encoding/xml"
	"net/http"
	"net/url"
	"os"

	"github.com/labstack/echo/v4"

	"github.com/flatcar/nebraska/backend/pkg/auth"
	"github.com/flatcar/nebraska/backend/pkg/codegen"
	"github.com/flatcar/nebraska/backend/pkg/config"
	"github.com/flatcar/nebraska/backend/pkg/handler/admin"
	"github.com/flatcar/nebraska/backend/pkg/handler/runtime"
	"github.com/flatcar/nebraska/backend/pkg/logger"
	"github.com/flatcar/nebraska/backend/pkg/version"
)

const GithubAccessManagementURL = "https://github.com/settings/apps/authorizations"

// Go names an embedded field after its unqualified type name, and both
// sub-handlers are called Handler, so they are embedded through these aliases.
type (
	adminHandler   = admin.Handler
	runtimeHandler = runtime.Handler
)

// Handler serves the endpoints that touch no database and composes the two
// role-scoped sub-handlers. It holds neither write service.
type Handler struct {
	*adminHandler
	*runtimeHandler

	clientConf *codegen.Config
	auth       auth.Authenticator
}

var _ codegen.ServerInterface = (*Handler)(nil)

var l = logger.New("nebraska")

func New(adminH *admin.Handler, runtimeH *runtime.Handler, conf *config.Config, auth auth.Authenticator) (*Handler, error) {
	clientConfig := &codegen.Config{
		AuthMode:        conf.AuthMode,
		NebraskaVersion: version.Version,
		Title:           conf.AppTitle,
		HeaderStyle:     conf.AppHeaderStyle,
	}

	if conf.AppLogoPath != "" {
		svg, err := os.ReadFile(conf.AppLogoPath)
		if err != nil {
			l.Error().Err(err).Msg("Reading svg from path in config")
			return nil, err
		}
		if err := xml.Unmarshal(svg, &struct{}{}); err != nil {
			l.Error().Err(err).Msg("Invalid format for SVG")
			return nil, err
		}
		clientConfig.Logo = string(svg)
	}

	if conf.AuthMode == "github" {
		clientConfig.AccessManagementUrl = "https://github.com/settings/connections/applications/" + conf.GhClientID
	}

	if conf.AuthMode == "oidc" {
		url, err := url.Parse(conf.NebraskaURL)
		if err != nil {
			l.Error().Err(err).Msg("Invalid nebraska-url")
			return nil, err
		}
		url.Path = "/login"
		clientConfig.LoginUrl = url.String()
		clientConfig.AccessManagementUrl = conf.OidcManagementURL

		// Populate OIDC-specific configuration for frontend
		clientConfig.OidcIssuerUrl = &conf.OidcIssuerURL
		clientConfig.OidcClientId = &conf.OidcClientID
		clientConfig.OidcScopes = &conf.OidcScopes
		if conf.OidcLogoutURL != "" {
			clientConfig.OidcLogoutUrl = &conf.OidcLogoutURL
		}
		if conf.OidcAudience != "" {
			clientConfig.OidcAudience = &conf.OidcAudience
		}
	}

	return &Handler{
		adminHandler:   adminH,
		runtimeHandler: runtimeH,
		clientConf:     clientConfig,
		auth:           auth,
	}, nil
}

func (h *Handler) Health(ctx echo.Context) error {
	return ctx.String(http.StatusOK, "OK")
}
