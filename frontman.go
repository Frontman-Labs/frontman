package frontman

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"

	"github.com/Frontman-Labs/frontman/config"
	"github.com/Frontman-Labs/frontman/gateway"
	"github.com/Frontman-Labs/frontman/ssl"
	"github.com/Frontman-Labs/frontman/log"
	"github.com/Frontman-Labs/frontman/plugins"
	"github.com/Frontman-Labs/frontman/service"
	"github.com/gorilla/mux"
)

// Frontman contains the backend services and the router
type Frontman struct {
	router          *gateway.APIGateway
	service         *mux.Router
	backendServices service.ServiceRegistry
	conf            *config.Config
	log             log.Logger
}

func NewServicesRouter(backendServices service.ServiceRegistry) *mux.Router {
	router := mux.NewRouter()

	router.HandleFunc("/api/services", getServicesHandler(backendServices)).Methods("GET")
	router.HandleFunc("/api/services", addServiceHandler(backendServices)).Methods("POST")
	router.HandleFunc("/api/services/{name}", removeServiceHandler(backendServices)).Methods("DELETE")
	router.HandleFunc("/api/services/{name}", updateServiceHandler(backendServices)).Methods("PUT")
	router.HandleFunc("/api/health", getHealthHandler(backendServices)).Methods("GET")

	return router
}

// NewGateway creates a new Frontman instance with a Redis client connection factory
func NewFrontman(conf *config.Config, log log.Logger) (*Frontman, error) {

	// Retrieve the Redis client connection from the factory
	ctx := context.Background()

	// Create a new BackendServices instance
	backendServices, err := service.NewServiceRegistry(ctx, conf.GlobalConfig.ServiceType, conf)
	if err != nil {
		return nil, err
	}

	servicesRouter := NewServicesRouter(backendServices)

	// Load plugins
	var plug []plugins.FrontmanPlugin

	// Create new APIGateway instance

	if conf.PluginConfig.Enabled {
		plug, err = plugins.LoadPlugins(conf.PluginConfig.Order)
		if err != nil {
			return nil, err
		}

	}

	// Create new APIGateway instance
	apiGateway := gateway.NewAPIGateway(backendServices, plug, conf, make(map[string]*http.Client), log)

	// Create the Frontman instance
	return &Frontman{
		router:          apiGateway,
		service:         servicesRouter,
		backendServices: backendServices,
		conf:            conf,
		log:             log,
	}, nil
}

func (gw *Frontman) Start() error {
	apiAddr := gw.conf.APIConfig.Addr
	if apiAddr == "" {
		apiAddr = "0.0.0.0:8080"
	}
	gatewayAddr := gw.conf.GatewayConfig.Addr
	if gatewayAddr == "" {
		gatewayAddr = "0.0.0.0:8000"
	}

	var apiHandler http.Handler
	var gatewayHandler http.Handler

	if gw.conf.APIConfig.SSL.Enabled {
		apiHandler = gw.service
		cert, err := ssl.LoadCert(gw.conf.APIConfig.SSL.Cert, gw.conf.APIConfig.SSL.Key)
		if err != nil {
			return err
		}
		apiServer := createServer(apiAddr, apiHandler, &cert)
		gw.log.Infof("Started Frontman API with SSL on %s", apiAddr)
		go func() {
			if err := startServer(apiServer); err != nil {
				gw.log.Fatal(err)
			}
		}()
	} else {
		apiHandler = gw.service
		api := createServer(apiAddr, apiHandler, nil)
		gw.log.Infof("Started Frontman API on %s", apiAddr)
		go func() {
			if err := startServer(api); err != nil {
				gw.log.Fatal(err)
			}
		}()
	}

	if gw.conf.GatewayConfig.SSL.Enabled {
		gatewayHandler = gw.router
		cert, err := ssl.LoadCert(gw.conf.GatewayConfig.SSL.Cert, gw.conf.GatewayConfig.SSL.Key)
		if err != nil {
			return err
		}

		// Redirect HTTP traffic to HTTPS
		httpAddr := "0.0.0.0:80"
		httpRedirect := createRedirectServer(httpAddr, gatewayAddr)
		gw.log.Infof("Started HTTP redirect server on %s", httpAddr)
		go func() {
			if err := startServer(httpRedirect); err != nil {
				gw.log.Fatal(err)
			}
		}()

		gatewayServer := createServer(gatewayAddr, gatewayHandler, &cert)
		gw.log.Infof("Started Frontman Frontman with SSL on %s", gatewayAddr)
		if err := startServer(gatewayServer); err != nil {
			return err
		}
	} else {
		gatewayHandler = gw.router
		gateway := createServer(gatewayAddr, gatewayHandler, nil)
		gw.log.Infof("Started Frontman Frontman on %s", gatewayAddr)
		if err := startServer(gateway); err != nil {
			return err
		}
	}

	return nil
}

func createRedirectServer(addr string, redirectAddr string) *http.Server {
	redirect := func(w http.ResponseWriter, req *http.Request) {
		httpsURL := "https://" + req.Host + req.URL.Path
		http.Redirect(w, req, httpsURL, http.StatusMovedPermanently)
	}
	return &http.Server{
		Addr:    addr,
		Handler: http.HandlerFunc(redirect),
	}
}

func createServer(addr string, handler http.Handler, cert *tls.Certificate) *http.Server {
	server := &http.Server{
		Addr:    addr,
		Handler: handler,
	}
	if cert != nil {
		server.TLSConfig = &tls.Config{
			Certificates: []tls.Certificate{*cert},
		}
	}
	return server
}

func startServer(server *http.Server) error {
	if server.TLSConfig != nil {
		if err := server.ListenAndServeTLS("", ""); err != nil {
			return fmt.Errorf("Failed to start server with TLS: %w", err)
		}
	} else {
		if err := server.ListenAndServe(); err != nil {
			return fmt.Errorf("Failed to start server without TLS: %w", err)
		}
	}
	return nil
}
