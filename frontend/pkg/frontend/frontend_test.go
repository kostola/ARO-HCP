package frontend

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/Azure/ARO-HCP/internal/api"
	"github.com/Azure/ARO-HCP/internal/api/arm"
	"github.com/Azure/ARO-HCP/internal/database"
)

var testLogger = slog.New(slog.NewTextHandler(io.Discard, nil))

func TestReadiness(t *testing.T) {
	tests := []struct {
		name               string
		ready              bool
		expectedStatusCode int
	}{
		{
			name:               "Not ready - returns 500",
			ready:              false,
			expectedStatusCode: http.StatusInternalServerError,
		},
		{
			name:               "Ready - returns 200",
			ready:              true,
			expectedStatusCode: http.StatusOK,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			f := &Frontend{
				dbClient: database.NewCache(),
				metrics:  NewPrometheusEmitter(prometheus.NewRegistry()),
			}
			f.ready.Store(test.ready)
			ts := httptest.NewServer(f.routes())
			ts.Config.BaseContext = func(net.Listener) context.Context {
				return ContextWithLogger(context.Background(), testLogger)
			}

			rs, err := ts.Client().Get(ts.URL + "/healthz")
			if err != nil {
				t.Fatal(err)
			}

			if rs.StatusCode != test.expectedStatusCode {
				t.Errorf("expected status code %d, got %d", test.expectedStatusCode, rs.StatusCode)
			}
		})
	}
}

func TestSubscriptionsGET(t *testing.T) {
	tests := []struct {
		name               string
		subDoc             *database.SubscriptionDocument
		expectedStatusCode int
	}{
		{
			name: "GET Subscription - Doc Exists",
			subDoc: &database.SubscriptionDocument{
				BaseDocument: database.BaseDocument{
					ID: "00000000-0000-0000-0000-000000000000",
				},
				Subscription: &arm.Subscription{
					State:            arm.SubscriptionStateRegistered,
					RegistrationDate: api.Ptr(time.Now().String()),
					Properties:       nil,
				},
			},
			expectedStatusCode: http.StatusOK,
		},
		{
			name:               "GET Subscription - No Doc",
			subDoc:             nil,
			expectedStatusCode: http.StatusNotFound,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			f := &Frontend{
				dbClient: database.NewCache(),
				metrics:  NewPrometheusEmitter(prometheus.NewRegistry()),
			}

			if test.subDoc != nil {
				err := f.dbClient.CreateSubscriptionDoc(context.TODO(), test.subDoc)
				if err != nil {
					t.Fatal(err)
				}
			}

			ts := httptest.NewServer(f.routes())
			ts.Config.BaseContext = func(net.Listener) context.Context {
				ctx := context.Background()
				ctx = ContextWithLogger(ctx, testLogger)
				ctx = ContextWithDBClient(ctx, f.dbClient)
				return ctx
			}

			rs, err := ts.Client().Get(ts.URL + "/subscriptions/00000000-0000-0000-0000-000000000000?api-version=2.0")
			if err != nil {
				t.Fatal(err)
			}

			if rs.StatusCode != test.expectedStatusCode {
				t.Errorf("expected status code %d, got %d", test.expectedStatusCode, rs.StatusCode)
			}
		})
	}
}

func TestSubscriptionsPUT(t *testing.T) {
	tests := []struct {
		name               string
		urlPath            string
		subscription       *arm.Subscription
		subDoc             *database.SubscriptionDocument
		expectedStatusCode int
	}{
		{
			name:    "PUT Subscription - Doc does not exist",
			urlPath: "/subscriptions/00000000-0000-0000-0000-000000000000?api-version=2.0",
			subscription: &arm.Subscription{
				State:            arm.SubscriptionStateRegistered,
				RegistrationDate: api.Ptr(time.Now().String()),
				Properties:       nil,
			},
			subDoc:             nil,
			expectedStatusCode: http.StatusOK,
		},
		{
			name:    "PUT Subscription - Doc Exists",
			urlPath: "/subscriptions/00000000-0000-0000-0000-000000000000?api-version=2.0",
			subscription: &arm.Subscription{
				State:            arm.SubscriptionStateRegistered,
				RegistrationDate: api.Ptr(time.Now().String()),
				Properties:       nil,
			},
			subDoc: &database.SubscriptionDocument{
				BaseDocument: database.BaseDocument{
					ID: "00000000-0000-0000-0000-000000000000",
				},
				Subscription: &arm.Subscription{
					State:            arm.SubscriptionStateRegistered,
					RegistrationDate: api.Ptr(time.Now().String()),
					Properties:       nil,
				},
			},
			expectedStatusCode: http.StatusOK,
		},
		{
			name:    "PUT Subscription - Invalid Subscription",
			urlPath: "/subscriptions/oopsie-i-no-good0?api-version=2.0",
			subscription: &arm.Subscription{
				State:            arm.SubscriptionStateRegistered,
				RegistrationDate: api.Ptr(time.Now().String()),
				Properties:       nil,
			},
			subDoc:             nil,
			expectedStatusCode: http.StatusBadRequest,
		},
		{
			name:    "PUT Subscription - Missing State",
			urlPath: "/subscriptions/00000000-0000-0000-0000-000000000000?api-version=2.0",
			subscription: &arm.Subscription{
				RegistrationDate: api.Ptr(time.Now().String()),
				Properties:       nil,
			},
			subDoc:             nil,
			expectedStatusCode: http.StatusBadRequest,
		},
		{
			name:    "PUT Subscription - Invalid State",
			urlPath: "/subscriptions/00000000-0000-0000-0000-000000000000?api-version=2.0",
			subscription: &arm.Subscription{
				State:            "Bogus",
				RegistrationDate: api.Ptr(time.Now().String()),
				Properties:       nil,
			},
			subDoc:             nil,
			expectedStatusCode: http.StatusBadRequest,
		},
		{
			name:    "PUT Subscription - Missing RegistrationDate",
			urlPath: "/subscriptions/00000000-0000-0000-0000-000000000000?api-version=2.0",
			subscription: &arm.Subscription{
				State:      arm.SubscriptionStateRegistered,
				Properties: nil,
			},
			subDoc:             nil,
			expectedStatusCode: http.StatusBadRequest,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			f := &Frontend{
				dbClient: database.NewCache(),
				metrics:  NewPrometheusEmitter(prometheus.NewRegistry()),
			}

			body, err := json.Marshal(&test.subscription)
			if err != nil {
				t.Fatal(err)
			}

			if test.subDoc != nil {
				err := f.dbClient.CreateSubscriptionDoc(context.TODO(), test.subDoc)
				if err != nil {
					t.Fatal(err)
				}
			}

			ts := httptest.NewServer(f.routes())
			ts.Config.BaseContext = func(net.Listener) context.Context {
				ctx := context.Background()
				ctx = ContextWithLogger(ctx, testLogger)
				ctx = ContextWithDBClient(ctx, f.dbClient)
				return ctx
			}

			req, err := http.NewRequest(http.MethodPut, ts.URL+test.urlPath, bytes.NewReader(body))
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Content-Type", "application/json")

			rs, err := ts.Client().Do(req)
			if err != nil {
				t.Fatal(err)
			}

			if rs.StatusCode != test.expectedStatusCode {
				t.Errorf("expected status code %d, got %d", test.expectedStatusCode, rs.StatusCode)
			}
		})
	}
}
