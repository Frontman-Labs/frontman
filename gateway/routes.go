package gateway

import (
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Frontman-Labs/frontman/service"
)

type Route struct {
	label    string
	isEnd    bool
	service  *service.BackendService
	children map[string]*Route
}

func refreshClients(bs *service.BackendService, clients map[string]*http.Client, clientLock *sync.Mutex) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			clientLock.Lock()

			// Update the transport settings for each client
			for _, client := range clients {
				transport := client.Transport.(*http.Transport)
				transport.MaxIdleConns = bs.MaxIdleConns
				transport.IdleConnTimeout = bs.MaxIdleTime * time.Second
				transport.TLSHandshakeTimeout = bs.Timeout * time.Second
			}

			clientLock.Unlock()
		}
	}
}

func refreshConnections(bs service.ServiceRegistry, clients map[string]*http.Client, clientLock *sync.Mutex) {

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			services := bs.GetServices()

			// Remove clients that are no longer needed
			clientLock.Lock()
			for url := range clients {
				found := false
				for _, s := range services {
					for _, t := range s.UpstreamTargets {
						if t == url {
							found = true
							break
						}
					}
					if found {
						break
					}
				}
				if !found {
					delete(clients, url)
				}
			}
			clientLock.Unlock()

			// Add or update clients for each service
			for _, s := range services {
				for _, t := range s.UpstreamTargets {
					clientLock.Lock()
					_, ok := clients[t]
					if !ok {
						transport := &http.Transport{
							MaxIdleConns:        s.MaxIdleConns,
							IdleConnTimeout:     s.MaxIdleTime * time.Second,
							TLSHandshakeTimeout: s.Timeout * time.Second,
						}
						client := &http.Client{
							Transport: transport,
						}
						clients[t] = client
					} else {
						clients[t].Transport.(*http.Transport).MaxIdleConns = s.MaxIdleConns
						clients[t].Transport.(*http.Transport).IdleConnTimeout = s.MaxIdleTime * time.Second
						clients[t].Transport.(*http.Transport).TLSHandshakeTimeout = s.Timeout * time.Second
					}
					clientLock.Unlock()
				}
				refreshClients(s, clients, clientLock)
			}
		}
	}
}

func getNextTargetIndex(backendService *service.BackendService, currentIndex int) int {
	numTargets := len(backendService.UpstreamTargets)
	if numTargets == 0 {
		return -1
	}
	if currentIndex >= numTargets-1 {
		return 0
	}
	return currentIndex + 1
}

func getClientForBackendService(bs service.BackendService, target string, clients map[string]*http.Client, clientLock *sync.Mutex) (*http.Client, error) {
	clientLock.Lock()
	defer clientLock.Unlock()

	// Check if the client for this target already exists
	if client, ok := clients[target]; ok {
		return client, nil
	}

	// Create a new transport with the specified settings
	transport := &http.Transport{
		MaxIdleConns:        bs.MaxIdleConns,
		IdleConnTimeout:     bs.MaxIdleTime * time.Second,
		TLSHandshakeTimeout: bs.Timeout * time.Second,
	}

	// Create a new HTTP client with the transport
	client := &http.Client{
		Transport: transport,
	}

	// Add the client to the map of clients
	clients[target] = client

	return client, nil
}

func copyHeaders(dst, src http.Header) {
	for k, v := range src {
		dst[k] = v
	}
}

func buildRoutes(services []*service.BackendService) *Route {
	root := &Route{
		label:    "",
		children: make(map[string]*Route),
	}
	for _, s := range services {
		insertNode(root, s)
	}
	return root
}

func insertNode(node *Route, service *service.BackendService) {
	// Handle domain-based routing first
	if service.Domain != "" {
		domainNode, ok := node.children[service.Domain]
		if !ok {
			domainNode = &Route{
				label:    service.Domain,
				children: make(map[string]*Route),
			}
			node.children[service.Domain] = domainNode
		}
		node = domainNode
	}

	segments := strings.Split(service.Path, "/")
	for _, s := range segments {
		if s == "" {
			continue
		}
		child, ok := node.children[s]
		if !ok {
			child = &Route{
				label:    s,
				children: make(map[string]*Route),
			}
			node.children[s] = child
		}
		node = child
	}

	node.isEnd = true
	node.service = service
}

func findBackendService(root *Route, r *http.Request) *service.BackendService {
	node := root
	pathSegments := strings.Split(r.URL.Path, "/")
	domain := strings.Split(r.Host, ":")[0]

	// Check for domain-based routing first
	domainNode, ok := node.children[domain]
	if ok {
		if domainNode.service != nil {
			return domainNode.service
		}
		node = domainNode
	}

	// Check for path-based routing
	for i, segment := range pathSegments {
		if segment == "" {
			continue
		}

		child, ok := node.children[segment]
		if !ok {
			return node.service
		}

		if child.service != nil && child.service.Domain == domain {
			if i == len(pathSegments)-1 {
				return child.service
			}
			node = child
			continue
		}

		if child.service != nil && child.service.Domain == "" {
			node = child
			continue
		}

		node = child
	}

	return nil
}
