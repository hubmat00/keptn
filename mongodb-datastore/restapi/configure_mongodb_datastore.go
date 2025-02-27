// This file is safe to edit. Once it exists it will not be overwritten
package restapi

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"github.com/keptn/keptn/cp-connector/pkg/eventsource"
	"github.com/keptn/keptn/cp-connector/pkg/subscriptionsource"
	"io/ioutil"
	"net/http"
	"os"
	"strings"
	"sync"

	apierrors "github.com/go-openapi/errors"
	"github.com/go-openapi/runtime"
	"github.com/go-openapi/runtime/middleware"
	"github.com/go-openapi/swag"
	keptnapi "github.com/keptn/go-utils/pkg/api/models"
	"github.com/keptn/keptn/cp-connector/pkg/controlplane"
	"github.com/keptn/keptn/cp-connector/pkg/nats"
	"github.com/keptn/keptn/mongodb-datastore/common"
	"github.com/keptn/keptn/mongodb-datastore/db"
	"github.com/keptn/keptn/mongodb-datastore/handlers"
	"github.com/keptn/keptn/mongodb-datastore/models"
	"github.com/keptn/keptn/mongodb-datastore/restapi/operations"
	"github.com/keptn/keptn/mongodb-datastore/restapi/operations/event"
	"github.com/keptn/keptn/mongodb-datastore/restapi/operations/health"
	log "github.com/sirupsen/logrus"
)

//go:generate swagger generate server --target ../../mongodb-datastore --name mongodb-datastore --spec ../swagger.yaml

func configureFlags(api *operations.MongodbDatastoreAPI) {
	// api.CommandLineOptionsGroups = []swag.CommandLineOptionsGroup{ ... }
}

var mutex = &sync.Mutex{}

func configureAPI(api *operations.MongodbDatastoreAPI) http.Handler {
	// configure the api here
	api.ServeError = apierrors.ServeError
	eventRequestHandler := handlers.NewEventRequestHandler(db.NewMongoDBEventRepo(db.GetMongoDBConnectionInstance()))
	eventRequestHandler.Env.ConfigLog()
	api.Logger = log.Infof

	// start NATS receiver
	go func() {
		err := startControlPlane(context.Background(), api, eventRequestHandler, log.New())
		if err != nil {
			log.Fatal(err)
		}
	}()

	api.JSONConsumer = runtime.JSONConsumer()
	api.JSONProducer = runtime.JSONProducer()

	api.EventGetEventsHandler = event.GetEventsHandlerFunc(func(params event.GetEventsParams) middleware.Responder {
		events, err := eventRequestHandler.GetEvents(params)
		if err != nil {
			if errors.Is(err, common.ErrInvalidEventFilter) {
				return event.NewGetEventsBadRequest().WithPayload(&models.Error{Code: http.StatusBadRequest, Message: swag.String(err.Error())})
			}
			return event.NewGetEventsInternalServerError().WithPayload(&models.Error{Code: http.StatusInternalServerError, Message: swag.String(err.Error())})
		}
		return event.NewGetEventsOK().WithPayload(events)
	})

	api.EventGetEventsByTypeHandler = event.GetEventsByTypeHandlerFunc(func(params event.GetEventsByTypeParams) middleware.Responder {
		events, err := eventRequestHandler.GetEventsByType(params)
		if err != nil {
			if errors.Is(err, common.ErrInvalidEventFilter) {
				return event.NewGetEventsBadRequest().WithPayload(&models.Error{Code: http.StatusBadRequest, Message: swag.String(err.Error())})
			}
			return event.NewGetEventsInternalServerError().WithPayload(&models.Error{Code: http.StatusInternalServerError, Message: swag.String(err.Error())})
		}
		return event.NewGetEventsByTypeOK().WithPayload(events)
	})

	api.HealthGetHealthHandler = health.GetHealthHandlerFunc(func(params health.GetHealthParams) middleware.Responder {
		return health.NewGetHealthOK()
	})
	api.ServerShutdown = func() {}

	return setupGlobalMiddleware(api.Serve(setupMiddlewares))
}

func startControlPlane(ctx context.Context, api *operations.MongodbDatastoreAPI, eventRequestHandler controlplane.Integration, log *log.Logger) error {
	// 1. create a subscription source
	natsConnector := nats.NewFromEnv()
	nats.WithLogger(log)(natsConnector)
	eventSource := eventsource.New(natsConnector, eventsource.WithLogger(log))

	// 2. Create a fixed event subscription with no uniform
	subSource := subscriptionsource.NewFixedSubscriptionSource(subscriptionsource.WithFixedSubscriptions(keptnapi.EventSubscription{Event: "sh.keptn.event.>"}))

	// 3. Create control plane
	controlPlane := controlplane.New(subSource, eventSource, nil, controlplane.WithLogger(log))

	ctx, cancel := context.WithCancel(ctx)

	// 4. Propagate graceful shutdown
	setPreShutDown(api, cancel)
	// 5. Start control plane
	log.Info("Registering control plane")
	return controlPlane.Register(ctx, eventRequestHandler)
}

func setPreShutDown(api *operations.MongodbDatastoreAPI, cancel context.CancelFunc) {
	mutex.Lock()
	api.PreServerShutdown = func() {
		log.Info("Shutting down control plane")
		cancel()
	}
	mutex.Unlock()
}

func getPreShutDown(api *operations.MongodbDatastoreAPI) func() {
	mutex.Lock()
	f := api.PreServerShutdown
	mutex.Unlock()
	return f
}

// The TLS configuration before HTTPS server starts.
func configureTLS(tlsConfig *tls.Config) {
	// Make all necessary changes to the TLS configuration here.
}

// As soon as server is initialized but not run yet, this function will be called.
// If you need to modify a config, store server instance to stop it individually later, this is the place.
// This function can be called multiple times, depending on the number of serving schemes.
// scheme value will be set accordingly: "http", "https" or "unix"
func configureServer(s *http.Server, scheme, addr string) {
	// Make all necessary changes to the Server configuration here.
}

// The middleware configuration is for the handler executors. These do not apply to the swagger.yaml document.
// The middleware executes after routing but before authentication, binding and validation
func setupMiddlewares(handler http.Handler) http.Handler {
	return handler
}

// The middleware configuration happens before anything, this middleware also applies to serving the swagger.yaml document.
// So this is a good place to plug in a panic handling middleware, logging and metrics
func setupGlobalMiddleware(handler http.Handler) http.Handler {

	prefixPath := os.Getenv("PREFIX_PATH")
	if len(prefixPath) > 0 {
		// Set the prefix-path in the swagger.yaml
		input, err := ioutil.ReadFile("swagger-ui/swagger.yaml")
		if err == nil {
			editedSwagger := strings.Replace(string(input), "basePath: /api/mongodb-datastore",
				"basePath: "+prefixPath+"/api/mongodb-datastore", -1)
			err = ioutil.WriteFile("swagger-ui/swagger.yaml", []byte(editedSwagger), 0644)
			if err != nil {
				fmt.Println("Failed to write edited swagger.yaml")
			}
		} else {
			fmt.Println("Failed to set basePath in swagger.yaml")
		}
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Serving ./swagger-ui/
		if strings.Index(r.URL.Path, "/swagger-ui/") == 0 {
			pathToSwaggerUI := "swagger-ui"
			// in case of local execution, the dir is stored in a parent folder
			if _, err := os.Stat(pathToSwaggerUI); os.IsNotExist(err) {
				pathToSwaggerUI = "../../swagger-ui"
			}
			http.StripPrefix("/swagger-ui/", http.FileServer(http.Dir(pathToSwaggerUI))).ServeHTTP(w, r)
			return
		}
		handler.ServeHTTP(w, r)
	})
}
