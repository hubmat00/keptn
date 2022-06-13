package handler

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	logger "github.com/sirupsen/logrus"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	apimodels "github.com/keptn/go-utils/pkg/api/models"
	"github.com/keptn/keptn/shipyard-controller/db"
	"github.com/keptn/keptn/shipyard-controller/models"
	"go.mongodb.org/mongo-driver/mongo"
)

type IUniformIntegrationHandler interface {
	Register(context *gin.Context)
	KeepAlive(context *gin.Context)
	Unregister(context *gin.Context)
	GetRegistrations(context *gin.Context)
	GetSubscription(context *gin.Context)
	GetSubscriptions(c *gin.Context)
	CreateSubscription(c *gin.Context)
	DeleteSubscription(c *gin.Context)
	UpdateSubscription(c *gin.Context)
}

type UniformIntegrationHandler struct {
	uniformRepo db.UniformRepo
}

func NewUniformIntegrationHandler(uniformRepo db.UniformRepo) *UniformIntegrationHandler {
	return &UniformIntegrationHandler{uniformRepo: uniformRepo}
}

type UniformParamsValidator struct {
	CheckProject bool
}

func (u UniformParamsValidator) Validate(params interface{}) error {
	switch t := params.(type) {
	case apimodels.EventSubscription:
		return u.validateSubscriptionParams(t)
	case apimodels.Integration:
		return u.validateIntegration(t)
	default:
		return nil
	}
}

func (u UniformParamsValidator) validateIntegration(params apimodels.Integration) error {

	// in case of webhook we need to check the project
	if params.Name == "webhook-service" {
		u.CheckProject = true
	}

	for _, subscription := range params.Subscriptions {
		if err := u.validateSubscriptionParams(subscription); err != nil {
			return err
		}

	}

	return nil
}

func (u UniformParamsValidator) validateSubscriptionParams(params apimodels.EventSubscription) error {

	if params.Event == "" {
		return fmt.Errorf("the event must be specified when setting up a subscription")
	}

	if err := u.validateFilterParams(params.Filter, u.CheckProject); err != nil {
		return err
	}

	return nil
}

func (u UniformParamsValidator) validateFilterParams(params apimodels.EventSubscriptionFilter, checkProject bool) error {

	// Since empty project stands for all projects, we cannot impose a project in the validation

	// If the service is specified then also the stage should be
	if params.Services != nil && params.Stages == nil {
		return fmt.Errorf("at least one stage must be specified when setting up a subscription filter for a service")
	}

	//if the service is the webhook it should not be able to apply for all projects
	if checkProject && (params.Projects == nil || len(params.Projects) > 1) {
		return fmt.Errorf("webhook should refer to exactly one project")
	}

	return nil
}

// Register creates or updates a uniform integration
// @Summary      Register a uniform integration
// @Description  Register a uniform integration
// @Description  <span class="oauth-scopes">Required OAuth scopes: ${prefix}integrations:write</span>
// @Tags         Uniform
// @Security     ApiKeyAuth
// @Accept       json
// @Produce      json
// @Param        integration  body      apimodels.Integration    true  "Integration"
// @Success      200          {object}  models.RegisterResponse  "ok: registration already exists"
// @Success      201          {object}  models.RegisterResponse  "ok: a new registration has been created"
// @Failure      400          {object}  models.Error             "Invalid payload"
// @Failure      500          {object}  models.Error             "Internal error"
// @Router       /uniform/registration [post]
func (rh *UniformIntegrationHandler) Register(c *gin.Context) {
	integration := &apimodels.Integration{}

	if err := c.ShouldBindJSON(integration); err != nil {
		SetBadRequestErrorResponse(c, err.Error())
		return
	}

	existingIntegrations, err := rh.uniformRepo.GetUniformIntegrations(models.GetUniformIntegrationsParams{
		Name:      integration.Name,
		Namespace: integration.MetaData.KubernetesMetaData.Namespace,
	})

	integrationInfo := fmt.Sprintf("name=%s, namespace=%s", integration.Name, integration.MetaData.KubernetesMetaData.Namespace)
	logger.Debugf("Uniform:Register(): Checking for existing integration for %s", integrationInfo)
	if err == nil && len(existingIntegrations) > 0 {
		logger.Debugf("Uniform:Register(): Found existing integration for %s with id %s", integrationInfo, existingIntegrations[0].ID)
		integration.ID = existingIntegrations[0].ID

		err2 := rh.updateExistingIntegration(integration)
		if err2 != nil {
			SetInternalServerErrorResponse(c, err2.Error())
			return
		}
		c.JSON(http.StatusOK, &models.RegisterResponse{
			ID: integration.ID,
		})
		return
	}

	integrationID := apimodels.IntegrationID{
		Name:      integration.Name,
		Namespace: integration.MetaData.KubernetesMetaData.Namespace,
		NodeName:  integration.MetaData.Hostname,
	}

	hash, err := integrationID.Hash()
	if err != nil {
		SetBadRequestErrorResponse(c, err.Error())
		return
	}

	//setting IDs and last seen timestamp
	integration.ID = hash
	integration.MetaData.LastSeen = time.Now().UTC()
	for i := range integration.Subscriptions {
		s := &integration.Subscriptions[i]
		s.ID = uuid.New().String()
	}

	logger.Debugf("Uniform:Register(): No existing integration found for %s. Creating a new one with ID %s", integrationInfo, integration.ID)
	validator := UniformParamsValidator{false}

	if err := validator.Validate(integration); err != nil {
		SetBadRequestErrorResponse(c, err.Error())
		return
	}

	err = rh.uniformRepo.CreateUniformIntegration(*integration)
	if err != nil {
		// if the integration already exists, update only needed fields
		if errors.Is(err, db.ErrUniformRegistrationAlreadyExists) {
			err2 := rh.updateExistingIntegration(integration)
			if err2 != nil {
				SetInternalServerErrorResponse(c, err2.Error())
				return
			}
			c.JSON(http.StatusOK, &models.RegisterResponse{
				ID: integration.ID,
			})
			return
		}
		SetInternalServerErrorResponse(c, err.Error())
		return
	}

	c.JSON(http.StatusCreated, &models.RegisterResponse{
		ID: integration.ID,
	})
}

func (rh *UniformIntegrationHandler) updateExistingIntegration(integration *apimodels.Integration) error {
	var err error
	result, err := rh.uniformRepo.GetUniformIntegrations(models.GetUniformIntegrationsParams{ID: integration.ID})

	if err != nil {
		return err
	}

	// update uniform only if there is a version upgrade or downgrade
	if result[0].MetaData.IntegrationVersion != integration.MetaData.IntegrationVersion || result[0].MetaData.DistributorVersion != integration.MetaData.DistributorVersion {
		// only update the version information instead of overwriting the complete integration
		_, err = rh.uniformRepo.UpdateVersionInfo(integration.ID, integration.MetaData.IntegrationVersion, integration.MetaData.DistributorVersion)
	} else {
		_, err = rh.uniformRepo.UpdateLastSeen(integration.ID)
	}
	return err
}

// Unregister deletes a uniform integration
// @Summary      Unregister a uniform integration
// @Description  Unregister a uniform integration
// @Description  <span class="oauth-scopes">Required OAuth scopes: ${prefix}integrations:delete</span>
// @Tags         Uniform
// @Security     ApiKeyAuth
// @Accept       json
// @Produce      json
// @Param        integrationID  path  string  true  "integrationID"
// @Success      200
// @Failure      500  {object}  models.Error  "Internal error"
// @Router       /uniform/registration/{integrationID} [delete]
func (rh *UniformIntegrationHandler) Unregister(c *gin.Context) {
	integrationID := c.Param("integrationID")

	if err := rh.uniformRepo.DeleteUniformIntegration(integrationID); err != nil {
		SetInternalServerErrorResponse(c, err.Error())
		return
	}
	c.JSON(http.StatusOK, &models.UnregisterResponse{})
}

// GetRegistrations Retrieve uniform integrations matching the provided filter
// @Summary      Retrieve uniform integrations matching the provided filter
// @Description  Retrieve uniform integrations matching the provided filter
// @Description  <span class="oauth-scopes">Required OAuth scopes: ${prefix}integrations:read</span>
// @Tags         Uniform
// @Security     ApiKeyAuth
// @Accept       json
// @Produce      json
// @Param        id       query     string                   false  "id"
// @Param        name     query     string                   false  "name"
// @Param        project  query     string                   false  "project"
// @Param        stage    query     string                   false  "stage"
// @Param        service  query     string                   false  "service"
// @Success      200      {object}  []apimodels.Integration  "ok"
// @Failure      400      {object}  models.Error             "Invalid payload"
// @Failure      500      {object}  models.Error             "Internal error"
// @Router       /uniform/registration [get]
func (rh *UniformIntegrationHandler) GetRegistrations(c *gin.Context) {
	params := &models.GetUniformIntegrationsParams{}
	if err := c.ShouldBindQuery(params); err != nil {
		SetBadRequestErrorResponse(c, fmt.Sprintf(InvalidRequestFormatMsg, err.Error()))
		return
	}
	uniformIntegrations, err := rh.uniformRepo.GetUniformIntegrations(*params)
	if err != nil {
		SetInternalServerErrorResponse(c, fmt.Sprintf(UnableQueryIntegrationsMsg, err.Error()))
		return
	}

	c.JSON(http.StatusOK, uniformIntegrations)
}

// KeepAlive returns current registration data of an integration
// @Summary      Endpoint for sending heartbeat messages sent from Keptn integrations to the control plane
// @Description  Endpoint for sending heartbeat messages sent from Keptn integrations to the control plane
// @Description  <span class="oauth-scopes">Required OAuth scopes: ${prefix}integrations:write</span>
// @Tags         Uniform
// @Security     ApiKeyAuth
// @Accept       json
// @Produce      json
// @Param        integrationID  path      string                 true  "integrationID"
// @Success      200            {object}  apimodels.Integration  "ok"
// @Failure      404            {object}  models.Error           "Not found"
// @Failure      500            {object}  models.Error           "Internal error"
// @Router       /uniform/registration/{integrationID}/ping [PUT]
func (rh *UniformIntegrationHandler) KeepAlive(c *gin.Context) {
	integrationID := c.Param("integrationID")

	registration, err := rh.uniformRepo.UpdateLastSeen(integrationID)
	if err != nil {
		if errors.Is(err, db.ErrUniformRegistrationNotFound) {
			SetNotFoundErrorResponse(c, err.Error())
			return
		}
		SetInternalServerErrorResponse(c, err.Error())
		return
	}

	c.JSON(http.StatusOK, registration)

}

// CreateSubscription creates a new subscription
// @Summary      Create a new subscription
// @Description  Create a new subscription
// @Description  <span class="oauth-scopes">Required OAuth scopes: ${prefix}integrations:write</span>
// @Tags         Uniform
// @Security     ApiKeyAuth
// @Accept       json
// @Produce      json
// @Param        integrationID  path  string                       true  "integrationID"
// @Param        subscription   body  apimodels.EventSubscription  true  "Subscription"
// @Success      201
// @Failure      400  {object}  models.Error  "Invalid payload"
// @Failure      404  {object}  models.Error  "Not found"
// @Failure      500  {object}  models.Error  "Internal error"
// @Router       /uniform/registration/{integrationID}/subscription [post]
func (rh *UniformIntegrationHandler) CreateSubscription(c *gin.Context) {

	integrationID := c.Param("integrationID")
	subscription := &apimodels.EventSubscription{}

	if err := c.ShouldBindJSON(subscription); err != nil {
		SetBadRequestErrorResponse(c, err.Error())
		return
	}
	validator := UniformParamsValidator{false}

	if err := validator.Validate(subscription); err != nil {
		SetBadRequestErrorResponse(c, err.Error())
		return
	}

	subscription.ID = uuid.New().String()

	err := rh.uniformRepo.CreateOrUpdateSubscription(integrationID, *subscription)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			SetNotFoundErrorResponse(c, err.Error())
			return
		}
		SetInternalServerErrorResponse(c, err.Error())
		return
	}

	c.JSON(http.StatusCreated, &models.CreateSubscriptionResponse{
		ID: subscription.ID,
	})
}

// UpdateSubscription updates or creates a subscription
// @Summary      Update or create a subscription
// @Description  Update or create a subscription
// @Description  <span class="oauth-scopes">Required OAuth scopes: ${prefix}integrations:write</span>
// @Tags         Uniform
// @Security     ApiKeyAuth
// @Accept       json
// @Produce      json
// @Param        integrationID   path  string                       true  "integrationID"
// @Param        subscriptionID  path  string                       true  "subscriptionID"
// @Param        subscription    body  apimodels.EventSubscription  true  "Subscription"
// @Success      201
// @Failure      400  {object}  models.Error  "Invalid payload"
// @Failure      500  {object}  models.Error  "Internal error"
// @Router       /uniform/registration/{integrationID}/subscription/{subscriptionID} [put]
func (rh *UniformIntegrationHandler) UpdateSubscription(c *gin.Context) {

	integrationID := c.Param("integrationID")
	subscriptionID := c.Param("subscriptionID")

	subscription := &apimodels.EventSubscription{}

	if err := c.ShouldBindJSON(subscription); err != nil {
		SetBadRequestErrorResponse(c, err.Error())
		return
	}

	oldIntegration, err := rh.uniformRepo.GetUniformIntegrations(models.GetUniformIntegrationsParams{
		ID: integrationID,
	})

	checkProject := false
	if len(oldIntegration) == 1 && oldIntegration[0].Name == "webhook-service" {
		checkProject = true
	}

	validator := UniformParamsValidator{checkProject}

	if err := validator.Validate(subscription); err != nil {
		SetBadRequestErrorResponse(c, err.Error())
		return
	}

	subscription.ID = subscriptionID

	err = rh.uniformRepo.CreateOrUpdateSubscription(integrationID, *subscription)
	if err != nil {
		//TODO: set appropriate http codes
		SetInternalServerErrorResponse(c, err.Error())
		return
	}

	c.JSON(http.StatusCreated, &models.CreateSubscriptionResponse{
		ID: subscription.ID,
	})
}

// DeleteSubscription deletes a new subscription
// @Summary      Delete a subscription
// @Description  Delete a subscription
// @Description  <span class="oauth-scopes">Required OAuth scopes: ${prefix}integrations:delete</span>
// @Tags         Uniform
// @Security     ApiKeyAuth
// @Accept       json
// @Produce      json
// @Param        integrationID   path  string  true  "integrationID"
// @Param        subscriptionID  path  string  true  "subscriptionID"
// @Success      200
// @Failure      500  {object}  models.Error  "Internal error"
// @Router       /uniform/registration/{integrationID}/subscription/{subscriptionID} [delete]
func (rh *UniformIntegrationHandler) DeleteSubscription(c *gin.Context) {
	integrationID := c.Param("integrationID")
	subscriptionID := c.Param("subscriptionID")

	err := rh.uniformRepo.DeleteSubscription(integrationID, subscriptionID)
	if err != nil {
		SetInternalServerErrorResponse(c, err.Error())
		return
	}

	c.JSON(http.StatusOK, models.DeleteSubscriptionResponse{})
}

// GetSubscription retrieves an already existing subscription
// @Summary      Retrieve an already existing subscription
// @Description  Retrieve an already existing subscription
// @Description  <span class="oauth-scopes">Required OAuth scopes: ${prefix}integrations:read</span>
// @Tags         Uniform
// @Security     ApiKeyAuth
// @Accept       json
// @Produce      json
// @Param        integrationID   path      string                       true  "integrationID"
// @Param        subscriptionID  path      string                       true  "subscriptionID"
// @Success      200             {object}  apimodels.EventSubscription  "ok"
// @Failure      404             {object}  models.Error                 "Not found"
// @Failure      500             {object}  models.Error                 "Internal error"
// @Router       /uniform/registration/{integrationID}/subscription/{subscriptionID} [get]
func (rh *UniformIntegrationHandler) GetSubscription(c *gin.Context) {
	integrationID := c.Param("integrationID")
	subscriptionID := c.Param("subscriptionID")

	subscription, err := rh.uniformRepo.GetSubscription(integrationID, subscriptionID)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			SetNotFoundErrorResponse(c, err.Error())
			return
		}
		SetInternalServerErrorResponse(c, err.Error())
		return
	}

	c.JSON(http.StatusOK, subscription)
}

// GetSubscriptions retrieves all subscriptions of a uniform integration
// @Summary      Retrieve all subscriptions of a uniform integration
// @Description  Retrieve all subscriptions of a uniform integration
// @Description  <span class="oauth-scopes">Required OAuth scopes: ${prefix}integrations:read</span>
// @Tags         Uniform
// @Security     ApiKeyAuth
// @Accept       json
// @Produce      json
// @Param        integrationID  path      string                         true  "integrationID"
// @Success      200            {object}  []apimodels.EventSubscription  "ok"
// @Failure      404            {object}  models.Error                   "Not found"
// @Failure      500            {object}  models.Error                   "Internal error"
// @Router       /uniform/registration/{integrationID}/subscription [get]
func (rh *UniformIntegrationHandler) GetSubscriptions(c *gin.Context) {
	integrationID := c.Param("integrationID")

	subscriptions, err := rh.uniformRepo.GetSubscriptions(integrationID)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			SetNotFoundErrorResponse(c, err.Error())
			return
		}
		SetInternalServerErrorResponse(c, err.Error())
		return
	}
	c.JSON(http.StatusOK, subscriptions)
}
