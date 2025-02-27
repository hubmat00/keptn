package subscriptionsource

import (
	"context"
	"github.com/keptn/keptn/cp-connector/pkg/types"
	"sync"
	"time"

	"github.com/benbjohnson/clock"
	"github.com/keptn/go-utils/pkg/api/models"
	api "github.com/keptn/go-utils/pkg/api/utils"
	"github.com/keptn/keptn/cp-connector/pkg/logger"
)

type SubscriptionSource interface {
	Start(context.Context, types.RegistrationData, chan []models.EventSubscription, chan error, *sync.WaitGroup) error
	Register(integration models.Integration) (string, error)
	Stop() error
}

var _ SubscriptionSource = FixedSubscriptionSource{}
var _ SubscriptionSource = (*UniformSubscriptionSource)(nil)

// UniformSubscriptionSource represents a source for uniform subscriptions
type UniformSubscriptionSource struct {
	uniformAPI    api.UniformV1Interface
	clock         clock.Clock
	fetchInterval time.Duration
	logger        logger.Logger
	quitC         chan struct{}
}

func (s *UniformSubscriptionSource) Register(integration models.Integration) (string, error) {
	integrationID, err := s.uniformAPI.RegisterIntegration(integration)
	if err != nil {
		return "", err
	}
	return integrationID, nil
}

// WithFetchInterval specifies the interval the subscription source should
// use when polling for new subscriptions
func WithFetchInterval(interval time.Duration) func(s *UniformSubscriptionSource) {
	return func(s *UniformSubscriptionSource) {
		s.fetchInterval = interval
	}
}

// WithLogger sets the logger to use
func WithLogger(logger logger.Logger) func(s *UniformSubscriptionSource) {
	return func(s *UniformSubscriptionSource) {
		s.logger = logger
	}
}

// New creates a new UniformSubscriptionSource
func New(uniformAPI api.UniformV1Interface, options ...func(source *UniformSubscriptionSource)) *UniformSubscriptionSource {
	s := &UniformSubscriptionSource{
		uniformAPI:    uniformAPI,
		clock:         clock.New(),
		fetchInterval: time.Second * 5,
		quitC:         make(chan struct{}, 1),
		logger:        logger.NewDefaultLogger()}
	for _, o := range options {
		o(s)
	}
	return s
}

// Start triggers the execution of the UniformSubscriptionSource
func (s *UniformSubscriptionSource) Start(ctx context.Context, registrationData types.RegistrationData, subscriptionChannel chan []models.EventSubscription, errC chan error, wg *sync.WaitGroup) error {
	s.logger.Debugf("UniformSubscriptionSource: Starting to fetch subscriptions for Integration ID %s", registrationData.ID)
	ticker := s.clock.Ticker(s.fetchInterval)
	go func() {
		s.ping(registrationData.ID, subscriptionChannel)
		for {
			select {
			case <-ctx.Done():
				wg.Done()
				return
			case <-ticker.C:
				s.ping(registrationData.ID, subscriptionChannel)
			case <-s.quitC:
				wg.Done()
				return
			}
		}
	}()
	return nil
}

func (s *UniformSubscriptionSource) Stop() error {
	s.quitC <- struct{}{}
	return nil
}

func (s *UniformSubscriptionSource) ping(registrationId string, subscriptionChannel chan []models.EventSubscription) {
	s.logger.Debugf("UniformSubscriptionSource: Renewing Integration ID %s", registrationId)
	updatedIntegrationData, err := s.uniformAPI.Ping(registrationId)
	if err != nil {
		s.logger.Errorf("Unable to ping control plane: %v", err)
		return
	}
	s.logger.Debugf("UniformSubscriptionSource: Ping successful, got %d subscriptions for %s", len(updatedIntegrationData.Subscriptions), registrationId)
	subscriptionChannel <- updatedIntegrationData.Subscriptions
}

// FixedSubscriptionSource can be used to use a fixed list of subscriptions rather than
// consulting the Keptn API for subscriptions.
// This is useful when you want to consume events from an event source, but NOT register
// as an Keptn integration to the control plane
type FixedSubscriptionSource struct {
	fixedSubscriptions []models.EventSubscription
}

// WithFixedSubscriptions adds a fixed list of subscriptions to the FixedSubscriptionSource
func WithFixedSubscriptions(subscriptions ...models.EventSubscription) func(s *FixedSubscriptionSource) {
	return func(s *FixedSubscriptionSource) {
		s.fixedSubscriptions = subscriptions
	}
}

// NewFixedSubscriptionSource creates a new instance of FixedSubscriptionSource
func NewFixedSubscriptionSource(options ...func(source *FixedSubscriptionSource)) *FixedSubscriptionSource {
	fss := &FixedSubscriptionSource{fixedSubscriptions: []models.EventSubscription{}}
	for _, o := range options {
		o(fss)
	}
	return fss
}

func (s FixedSubscriptionSource) Start(ctx context.Context, data types.RegistrationData, c chan []models.EventSubscription, errC chan error, wg *sync.WaitGroup) error {
	go func() {
		c <- s.fixedSubscriptions
		<-ctx.Done()
		wg.Done()
	}()
	return nil
}

func (s FixedSubscriptionSource) Register(integration models.Integration) (string, error) {
	return "", nil
}

func (s FixedSubscriptionSource) Stop() error {
	return nil
}
