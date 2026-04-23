package handler

import (
	"encoding/xml"
	"net/http"
	"net/url"
	"os"

	"github.com/labstack/echo/v4"

	"github.com/flatcar/nebraska/backend/pkg/api/admin"
	apiruntime "github.com/flatcar/nebraska/backend/pkg/api/runtime"
	"github.com/flatcar/nebraska/backend/pkg/auth"
	"github.com/flatcar/nebraska/backend/pkg/codegen"
	"github.com/flatcar/nebraska/backend/pkg/config"
	"github.com/flatcar/nebraska/backend/pkg/logger"
	"github.com/flatcar/nebraska/backend/pkg/omaha"
	"github.com/flatcar/nebraska/backend/pkg/version"
)

const (
	UpdateMaxRequestSize      = 64 * 1024
	GithubAccessManagementURL = "https://github.com/settings/apps/authorizations"
)

type Handler struct {
	// admin provides write access to admin tables. Nil on subscriber instances.
	admin *admin.Service

	// runtime provides write access to runtime tables + all read access
	// (via embedded dbreads.Queries). Available on all instances.
	runtime *apiruntime.Service

	omahaHandler *omaha.Handler
	conf         *config.Config
	clientConf   *codegen.Config
	auth         auth.Authenticator
}

// requirePrimary returns an HTTP 403 error if admin writes are not available.
func (h *Handler) requirePrimary(ctx echo.Context) error {
	if h.admin == nil {
		return ctx.JSON(http.StatusForbidden, map[string]string{
			"error": "admin write operations are not available on subscriber instances",
		})
	}
	return nil
}

var defaultPage = 1
var defaultPerPage = 10

var l = logger.New("nebraska")

func New(adminSvc *admin.Service, runtimeSvc *apiruntime.Service, omahaHandler *omaha.Handler, conf *config.Config, auth auth.Authenticator) (*Handler, error) {
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
		admin:        adminSvc,
		runtime:      runtimeSvc,
		omahaHandler: omahaHandler,
		conf:         conf,
		clientConf:   clientConfig,
		auth:         auth,
	}, nil
}

func (h *Handler) Health(ctx echo.Context) error {
	return ctx.String(http.StatusOK, "OK")
}
