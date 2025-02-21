package controlplane

import (
	"github.com/keptn/go-utils/pkg/api/models"
	api "github.com/keptn/go-utils/pkg/api/utils"
	"github.com/keptn/keptn/distributor/pkg/config"
	"github.com/stretchr/testify/assert"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestControlPlaneRegister(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		assert.Equal(t, "/v1/uniform/registration", req.URL.String())
		reqBody, _ := ioutil.ReadAll(req.Body)
		data := models.Integration{}
		data.FromJSON(reqBody)
		assert.Equal(t, models.Integration{
			Name: "k8s-component",
			MetaData: models.MetaData{
				Hostname:           "k8s-nodename",
				IntegrationVersion: "2.0",
				DistributorVersion: "1.0",
				Location:           "location",
				KubernetesMetaData: models.KubernetesMetaData{
					Namespace:      "k8s-namespace",
					PodName:        "k8s-podname",
					DeploymentName: "k8s-deployment",
				},
			},
			Subscriptions: []models.EventSubscription{{
				Event: "t1",
				Filter: models.EventSubscriptionFilter{
					Projects: []string{"p-filter"},
					Stages:   []string{"s-filter"},
					Services: []string{"sv-filter"},
				},
			},
				{
					Event: "t2",
					Filter: models.EventSubscriptionFilter{
						Projects: []string{"p-filter"},
						Stages:   []string{"s-filter"},
						Services: []string{"sv-filter"},
					},
				},
			},
		}, data)
		rw.Write([]byte(`{"id": "abcde"}`))
	}))
	defer server.Close()

	cfg := config.EnvConfig{
		ProjectFilter:          "p-filter",
		StageFilter:            "s-filter",
		ServiceFilter:          "sv-filter",
		Location:               "location",
		DistributorVersion:     "1.0",
		Version:                "2.0",
		K8sDeploymentName:      "k8s-deployment",
		K8sDeploymentComponent: "k8s-component",
		K8sNamespace:           "k8s-namespace",
		K8sPodName:             "k8s-podname",
		K8sNodeName:            "k8s-nodename",
		PubSubTopic:            "t1,t2",
	}

	controlPlane := New(api.NewUniformHandler(server.URL), config.ConnectionTypeNATS, cfg)
	id, err := controlPlane.Register()
	assert.Nil(t, err)
	assert.Equal(t, "abcde", id)

	id, err = controlPlane.Register()
	assert.Nil(t, err)
	assert.Equal(t, "abcde", id)
}

func TestControlPlaneRegisterFails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		assert.Equal(t, "/v1/uniform/registration", req.URL.String())
		rw.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	controlPlane := New(api.NewUniformHandler(server.URL), config.ConnectionTypeNATS, config.EnvConfig{})
	id, err := controlPlane.Register()
	assert.NotNil(t, err)
	assert.Equal(t, "", id)
}

func TestControlPlaneUnregister(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		if req.Method == http.MethodPost {
			assert.Equal(t, "/v1/uniform/registration", req.URL.String())
			rw.Write([]byte(`{"id": "abcde"}`))
		} else if req.Method == http.MethodDelete {
			assert.Equal(t, "/v1/uniform/registration/abcde", req.URL.String())
		}
	}))
	defer server.Close()

	controlPlane := New(api.NewUniformHandler(server.URL), config.ConnectionTypeNATS, config.EnvConfig{})
	controlPlane.Register()
	err := controlPlane.Unregister()
	assert.Nil(t, err)
}

func TestControlPlaneUnregisterFails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		if req.Method == http.MethodPost {
			assert.Equal(t, "/v1/uniform/registration", req.URL.String())
			rw.Write([]byte(`{"id": "abcde"}`))
		} else if req.Method == http.MethodDelete {
			assert.Equal(t, "/v1/uniform/registration/abcde", req.URL.String())
			rw.WriteHeader(http.StatusBadGateway)
		}
	}))
	defer server.Close()

	controlPlane := New(api.NewUniformHandler(server.URL), config.ConnectionTypeNATS, config.EnvConfig{})
	controlPlane.Register()
	err := controlPlane.Unregister()
	assert.NotNil(t, err)
}

func TestControlPlaneUnregisterWithoutPreviousRegister(t *testing.T) {
	endpointInvoked := false
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		endpointInvoked = true
	}))
	defer server.Close()

	controlPlane := New(api.NewUniformHandler(server.URL), config.ConnectionTypeNATS, config.EnvConfig{})
	err := controlPlane.Unregister()
	assert.NotNil(t, err)
	assert.False(t, endpointInvoked)
}
