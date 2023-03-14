package gateway

import (
	"github.com/Frontman-Labs/frontman/loadbalancer"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/Frontman-Labs/frontman/config"
	"github.com/Frontman-Labs/frontman/log"
	"github.com/Frontman-Labs/frontman/plugins"
	"github.com/Frontman-Labs/frontman/service"
)

type testPlugin struct {
	preRequest   func(*http.Request, service.ServiceRegistry, *config.Config) plugins.PluginError
	postResponse func(*http.Response, service.ServiceRegistry, *config.Config) plugins.PluginError
	close        func() plugins.PluginError
}

func (p *testPlugin) Name() string {
	return ""
}

func (p *testPlugin) PreRequest(r *http.Request, s service.ServiceRegistry, c *config.Config) plugins.PluginError {
	if p.preRequest != nil {
		return p.preRequest(r, s, c)
	}
	return nil
}

func (p *testPlugin) PostResponse(resp *http.Response, s service.ServiceRegistry, c *config.Config) plugins.PluginError {
	if p.postResponse != nil {
		return p.postResponse(resp, s, c)
	}
	return nil
}

func (p *testPlugin) Close() plugins.PluginError {
	if p.close != nil {
		return p.close()
	}
	return nil
}

type mockHTTPClient struct {
	mockResponse *http.Response
	mockErr      error
}

func (c *mockHTTPClient) RoundTrip(req *http.Request) (*http.Response, error) {
	return c.mockResponse, c.mockErr
}

func TestGatewayHandler(t *testing.T) {
	testCases := []struct {
		name               string
		domain             string
		path               string
		scheme             string
		stripPath          bool
		maxIdleConns       int
		maxIdleTime        int
		timeout            int
		upstreamTargets    []string
		requestURL         string
		expectedStatusCode int
		expectedHeader     string
	}{
		{
			name:               "Test Case 1",
			domain:             "test.com",
			path:               "/api",
			scheme:             "https",
			stripPath:          true,
			maxIdleConns:       100,
			maxIdleTime:        10,
			timeout:            5,
			upstreamTargets:    []string{"https://httpbin.org"},
			requestURL:         "https://test.com/api/anything?test",
			expectedStatusCode: http.StatusOK,
			expectedHeader:     "plugin",
		},
		{
			name:               "Test Case 2 - No matching backend service",
			domain:             "test.com",
			path:               "/api",
			scheme:             "https",
			stripPath:          true,
			maxIdleConns:       100,
			maxIdleTime:        10,
			timeout:            5,
			upstreamTargets:    []string{"https://httpbin.org"},
			requestURL:         "https://test.com/notfound",
			expectedStatusCode: http.StatusNotFound,
			expectedHeader:     "",
		},
		{
			name:               "Test Case 3 - Invalid upstream target URL",
			domain:             "test.com",
			path:               "/api",
			scheme:             "https",
			stripPath:          true,
			maxIdleConns:       100,
			maxIdleTime:        10,
			timeout:            5,
			upstreamTargets:    []string{"https://httpbin.or"},
			requestURL:         "https://test.com/api/anythin?test",
			expectedStatusCode: http.StatusBadGateway,
			expectedHeader:     "plugin",
		},
		{
			name:               "Test Case 5 - Backend service with no domain",
			domain:             "",
			path:               "/api",
			scheme:             "https",
			stripPath:          true,
			maxIdleConns:       100,
			maxIdleTime:        10,
			timeout:            5,
			upstreamTargets:    []string{"https://httpbin.org"},
			requestURL:         "https://localhost/api/anything?test",
			expectedStatusCode: http.StatusOK,
			expectedHeader:     "plugin",
		},
		{
			name:               "Test Case 6 - Backend service with domain",
			domain:             "test.com",
			path:               "/api",
			scheme:             "https",
			stripPath:          true,
			maxIdleConns:       100,
			maxIdleTime:        10,
			timeout:            5,
			upstreamTargets:    []string{"https://httpbin.org"},
			requestURL:         "https://test.com/api/anything?test",
			expectedStatusCode: http.StatusOK,
			expectedHeader:     "plugin",
		},
		{
			name:               "Test Case 7 - StripPath is false",
			domain:             "",
			path:               "/api",
			scheme:             "https",
			stripPath:          false,
			maxIdleConns:       100,
			maxIdleTime:        10,
			timeout:            5,
			upstreamTargets:    []string{"https://httpbin.org"},
			requestURL:         "https://localhost/api/anything/test?test",
			expectedStatusCode: http.StatusNotFound,
			expectedHeader:     "plugin",
		},
		{
			name:               "Test Case 8 - Multiple backend targets with localhost domain",
			domain:             "localhost",
			path:               "/api",
			scheme:             "http",
			stripPath:          true,
			maxIdleConns:       100,
			maxIdleTime:        10,
			timeout:            5,
			upstreamTargets:    []string{"http://localhost:8000", "http://localhost:8001", "http://localhost:8002"},
			requestURL:         "http://localhost/api/anything?test",
			expectedStatusCode: http.StatusOK,
			expectedHeader:     "plugin",
		},
	}

	for _, tc := range testCases {

		// Create a mock HTTP client with the desired response

		bs := &service.BackendService{
			Name:            tc.name,
			Domain:          tc.domain,
			Path:            tc.path,
			Scheme:          tc.scheme,
			StripPath:       tc.stripPath,
			MaxIdleConns:    tc.maxIdleConns,
			MaxIdleTime:     time.Duration(tc.maxIdleTime),
			Timeout:         time.Duration(tc.timeout),
			UpstreamTargets: tc.upstreamTargets,
			LoadBalancerPolicy: service.LoadBalancerPolicy{
				Type: loadbalancer.RoundRobin,
			},
		}

		bs.Init()

		clients := make(map[string]*http.Client)

		clients[bs.Name] = &http.Client{Transport: &mockHTTPClient{
			mockResponse: &http.Response{
				StatusCode: tc.expectedStatusCode,
				Header:     make(http.Header),
			},
			mockErr: nil,
		}}
		reg := service.NewMemoryServiceRegistry()
		reg.Services["test"] = bs

		req := httptest.NewRequest("GET", tc.requestURL, nil)
		w := httptest.NewRecorder()

		plugin := &testPlugin{
			preRequest: func(r *http.Request, s service.ServiceRegistry, c *config.Config) plugins.PluginError {
				r.Header.Set("X-Plugin-Header", "plugin")
				return nil
			},
			postResponse: func(resp *http.Response, s service.ServiceRegistry, c *config.Config) plugins.PluginError {
				resp.Header.Set("X-Plugin-Header", "plugin")
				return nil
			},
		}

		logger, err := log.NewZapLogger("info")
		if err != nil {
			t.Errorf("could not create logger due to: %s",err)
		}
		handler := NewAPIGateway(reg, []plugins.FrontmanPlugin{plugin}, &config.Config{}, clients, logger)
		handler.ServeHTTP(w, req)

		// Check the response status code
		if w.Code != tc.expectedStatusCode {
			t.Errorf("[%s] Expected status code %d, got %d", tc.name, tc.expectedStatusCode, w.Code)
		}

		// Check the response headers (plugin and backend)
		if w.Header().Get("X-Plugin-Header") != tc.expectedHeader {
			t.Errorf("[%s] Expected header X-Plugin-Header to be set to '%s', got '%s'", tc.name, tc.expectedHeader, w.Header().Get("X-Plugin-Header"))
		}
	}
}

func TestFindBackendService(t *testing.T) {
	testCases := []struct {
		name           string
		services       []*service.BackendService
		request        *http.Request
		expectedOutput *service.BackendService
	}{
		{
			name: "Matching path with no domain",
			services: []*service.BackendService{
				{
					Domain: "",
					Path:   "/api",
				},
				{
					Domain: "",
					Path:   "/admin",
				},
			},
			request: &http.Request{
				URL: &url.URL{
					Path: "/api/get",
				},
			},
			expectedOutput: &service.BackendService{
				Domain: "",
				Path:   "/api",
			},
		},
		{
			name: "Matching path and domain",
			services: []*service.BackendService{
				{
					Domain: "example.com",
					Path:   "/api",
				},
				{
					Domain: "example.com",
					Path:   "/admin",
				},
			},
			request: &http.Request{
				Host: "example.com",
				URL: &url.URL{
					Path: "/api/get",
				},
			},
			expectedOutput: &service.BackendService{
				Domain: "example.com",
				Path:   "/api",
			},
		},
		{
			name: "No matching backend service",
			services: []*service.BackendService{
				{
					Domain: "",
					Path:   "/api",
				},
				{
					Domain: "example.com",
					Path:   "/admin",
				},
			},
			request: &http.Request{
				URL: &url.URL{
					Path: "/dashboard",
				},
			},
			expectedOutput: nil,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			root := buildRoutes(tc.services)
			result := findBackendService(root, tc.request)
			if !reflect.DeepEqual(result, tc.expectedOutput) {
				t.Errorf("Unexpected result: got %#v, want %#v", result, tc.expectedOutput)
			}
		})
	}
}

func TestGetClientForBackendService(t *testing.T) {
	testCases := []struct {
		name              string
		backendService    service.BackendService
		target            string
		existingClients   map[string]*http.Client
		expectedTransport *http.Transport
	}{
		{
			name: "New client for target",
			backendService: service.BackendService{
				MaxIdleConns:    100,
				MaxIdleTime:     10,
				Timeout:         5,
				UpstreamTargets: []string{"httpbin.org"},
			},
			target:          "httpbin.org",
			existingClients: map[string]*http.Client{},
			expectedTransport: &http.Transport{
				MaxIdleConns:        100,
				IdleConnTimeout:     10 * time.Second,
				TLSHandshakeTimeout: 5 * time.Second,
			},
		},
		{
			name: "Existing client for target",
			backendService: service.BackendService{
				MaxIdleConns:    50,
				MaxIdleTime:     5,
				Timeout:         2,
				UpstreamTargets: []string{"httpbin.org"},
			},
			target: "httpbin.org",
			existingClients: map[string]*http.Client{
				"httpbin.org": &http.Client{
					Transport: &http.Transport{
						MaxIdleConns:        100,
						IdleConnTimeout:     10 * time.Second,
						TLSHandshakeTimeout: 5 * time.Second,
					},
				},
			},
			expectedTransport: &http.Transport{
				MaxIdleConns:        100,
				IdleConnTimeout:     10 * time.Second,
				TLSHandshakeTimeout: 5 * time.Second,
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			clients := tc.existingClients
			clientLock := &sync.Mutex{}

			client, err := getClientForBackendService(tc.backendService, tc.target, clients, clientLock)
			if err != nil {
				t.Errorf("Unexpected error: %v", err)
			}

			transport, ok := client.Transport.(*http.Transport)
			if !ok {
				t.Errorf("Unexpected transport type: %T", client.Transport)
			}

			if !reflect.DeepEqual(transport, tc.expectedTransport) {
				t.Errorf("Unexpected transport: got %v, want %v", transport, tc.expectedTransport)
			}
		})
	}
}

func BenchmarkGatewayHandler(b *testing.B) {
	bs := &service.BackendService{
		Domain:          "test.com",
		Path:            "/api",
		Scheme:          "https",
		StripPath:       true,
		MaxIdleConns:    100,
		MaxIdleTime:     time.Duration(10) * time.Second,
		Timeout:         time.Duration(5) * time.Second,
		UpstreamTargets: []string{"httpbin.org"},
	}
	reg := service.NewMemoryServiceRegistry()
	reg.Services["test"] = bs

	req, _ := http.NewRequest("GET", "https://test.com/api/anything?test", nil)
	w := httptest.NewRecorder()

	plugin := &testPlugin{
		preRequest: func(r *http.Request, s service.ServiceRegistry, c *config.Config) plugins.PluginError {
			r.Header.Set("X-Plugin-Header", "plugin")
			return nil
		},
		postResponse: func(resp *http.Response, s service.ServiceRegistry, c *config.Config) plugins.PluginError {
			resp.Header.Set("X-Plugin-Header", "plugin")
			return nil
		},
	}

	clients := make(map[string]*http.Client)
	clients[bs.UpstreamTargets[0]] = &http.Client{Transport: &mockHTTPClient{
		mockResponse: &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
		},
		mockErr: nil,
	}}

	logger, err := log.NewZapLogger("info")
		if err != nil {
			b.Errorf("could not create logger due to: %s",err)
		}
		handler := NewAPIGateway(reg, []plugins.FrontmanPlugin{plugin}, &config.Config{}, clients, logger)

	for i := 0; i < b.N; i++ {
		handler.ServeHTTP(w, req)
	}
}
