package gateway

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/Frontman-Labs/frontman/log"
	"github.com/Frontman-Labs/frontman/config"
	"github.com/Frontman-Labs/frontman/plugins"
	"github.com/Frontman-Labs/frontman/service"
)

type APIGateway struct {
	bs                 service.ServiceRegistry
	plugs              []plugins.FrontmanPlugin
	conf               *config.Config
	clients            map[string]*http.Client
	clientLock         sync.Mutex
	currentTargetIndex int
	log                log.Logger
}

func NewAPIGateway(bs service.ServiceRegistry, plugs []plugins.FrontmanPlugin, conf *config.Config, clients map[string]*http.Client, logger log.Logger) *APIGateway {
	return &APIGateway{
		bs:      bs,
		plugs:   plugs,
		conf:    conf,
		clients: clients,
		log: logger,
	}
}


func (g *APIGateway) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	root := buildRoutes(g.bs.GetServices())
	for _, plugin := range g.plugs {

		if err := plugin.PreRequest(req, g.bs, g.conf); err != nil {
			g.log.Errorf("Plugin error: %v", err)
			http.Error(w, err.Error(), err.StatusCode())
			return
		}
	}

	// Find the backend service that matches the request
	backendService := findBackendService(root, req)

	// If the backend service was not found, return a 404 error
	if backendService == nil {
		http.NotFound(w, req)
		return
	}

	// Get the upstream target URL for this request
	upstreamTarget := backendService.GetLoadBalancer().ChooseTarget(backendService.UpstreamTargets)

	var urlPath string
	if backendService.StripPath {
		urlPath = strings.TrimPrefix(req.URL.Path, backendService.Path)
	} else {
		urlPath = backendService.Path
	}

	// Create a new target URL with the service path and scheme

	targetURL, err := url.Parse(upstreamTarget + urlPath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Get or create a new client for this backend service
	client, err := getClientForBackendService(*backendService, backendService.Name, g.clients, &g.clientLock)
	headers := make(http.Header)
	// Copy the headers from the original request
	copyHeaders(headers, req.Header)
	if backendService.AuthConfig != nil {
		tokenValidator := backendService.GetTokenValidator()
		// Backend service has auth config specified
		claims, err := tokenValidator.ValidateToken(headers.Get("Authorization"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusUnauthorized)
			return
		}
		data, err := json.Marshal(claims)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		headers.Add(backendService.GetUserDataHeader(), string(data))

	}
	// Remove the X-Forwarded-For header to prevent spoofing
	headers.Del("X-Forwarded-For")

	// Log a message indicating that the request is being sent to the target service
	g.log.Infof("Sending request to %s: %s %s", upstreamTarget, req.Method, urlPath)

	// Send the request to the target service using the client with the specified transport
	resp, err := client.Do(&http.Request{
		Method:        req.Method,
		URL:           targetURL,
		Proto:         req.Proto,
		ProtoMajor:    req.ProtoMajor,
		ProtoMinor:    req.ProtoMinor,
		Header:        headers,
		Body:          req.Body,
		ContentLength: req.ContentLength,
		Host:          targetURL.Host,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		g.log.Errorf("Error sending request: %v\n", err.Error())
		return
	}

	defer resp.Body.Close()

	for _, plugin := range g.plugs {
		if err := plugin.PostResponse(resp, g.bs, g.conf); err != nil {
			g.log.Errorf("Plugin error: %v", err)
			http.Error(w, err.Error(), err.StatusCode())
			return
		}
	}

	// Log a message indicating that the response has been received from the target service
	g.log.Infof("Response received from %s: %d %s", upstreamTarget, resp.StatusCode, resp.Status)

	// Copy the response headers back to the client
	copyHeaders(w.Header(), resp.Header)

	// Set the status code and body of the response
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}
