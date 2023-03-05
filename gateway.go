package frontman

import (
	"context"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/gorilla/mux"
	"github.com/hyperioxx/frontman/config"
	"github.com/hyperioxx/frontman/service"
)

// Gateway contains the backend services and the router
type Gateway struct {
	router          *mux.Router
	service         *mux.Router
	backendServices service.ServiceRegistry
	conf            *config.Config
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

// NewGateway creates a new Gateway instance with a Redis client connection factory
func NewGateway(conf *config.Config) (*Gateway, error) {

	// Retrieve the Redis client connection from the factory
	ctx := context.Background()

	// Create a new BackendServices instance
	backendServices, err := service.NewServiceRegistry(ctx, conf.GlobalConfig.ServiceType, conf)
	if err != nil {
		return nil, err
	}

	servicesRouter := NewServicesRouter(backendServices)

	// Create a new router instance
	proxyRouter := mux.NewRouter()

	proxyRouter.HandleFunc("/{proxyPath:.+}", gatewayHandler(backendServices)).Methods("GET", "POST", "PUT", "DELETE", "PATCH", "OPTIONS").MatcherFunc(func(r *http.Request, rm *mux.RouteMatch) bool {
		vars := mux.Vars(r)
		proxyPath := vars["proxyPath"]
		for _, prefix := range []string{"/api/"} {
			if strings.HasPrefix(proxyPath, prefix) {
				return false
			}
		}
		return true
	})

	// Create the Gateway instance
	return &Gateway{
		router:          proxyRouter,
		service:         servicesRouter,
		backendServices: backendServices,
		conf:            conf,
	}, nil
}

// Start starts the server
func (gw *Gateway) Start() error {
	// Create a new HTTP server instance for the /api/services endpoint

	apiAddr := os.Getenv("FRONTMAN_API_ADDR")
	if apiAddr == "" {
		apiAddr = "0.0.0.0:8080"
	}
	gatewayAddr := os.Getenv("FRONTMAN_GATEWAY_ADDR")
	if gatewayAddr == "" {
		gatewayAddr = "0.0.0.0:8000"
	}

	api := &http.Server{
		Addr:    apiAddr,
		Handler: gw.service,
	}
	gateway := &http.Server{
		Addr:    gatewayAddr,
		Handler: gw.router,
	}

	// Start the main HTTP server
	log.Println("Starting Frontman Gateway...")
	go func() {
		if err := gateway.ListenAndServe(); err != nil {
			log.Fatalf("Failed to start Frontman Gateway: %v", err)
		}
	}()

	// Start the /api/services HTTP server
	log.Println("Starting /api/services endpoint...")
	if err := api.ListenAndServe(); err != nil {
		log.Fatalf("Failed to start /api/services endpoint: %v", err)
	}

	return nil
}
