package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestChannelA2AConstant(t *testing.T) {
	assert.Equal(t, "a2a", ChannelA2A)
}

func TestA2ASettingsDefaultsZeroValue(t *testing.T) {
	var s A2ASettings
	assert.Empty(t, s.AgentID)
	assert.Equal(t, 0, s.Port)
}
