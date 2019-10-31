package handlers

import (
	"go.mongodb.org/mongo-driver/bson"
	"testing"

	keptnutils "github.com/keptn/go-utils/pkg/utils"
	"github.com/magiconair/properties/assert"
)

func TestFlattenRecursively(t *testing.T) {
	logger := keptnutils.NewLogger("", "", "mongodb-service")

	grandchild := bson.D{{"apple", "red"}, {"orange", "orange"}}
	child := bson.D{{"foo", "bar"}, {"grandchild", grandchild}}
	parent := bson.D{{"hello", "world"}, {"child", child}}

	// checks:
	parentMap := flattenRecursively(parent, logger)
	assert.Equal(t, parentMap["hello"], "world", "flatting failed")

	childMap := parentMap["child"].(map[string]interface{})
	assert.Equal(t, childMap["foo"], "bar", "flatting failed")

	grandchildMap := childMap["grandchild"].(map[string]interface{})
	assert.Equal(t, grandchildMap["orange"], "orange", "flatting failed")
}