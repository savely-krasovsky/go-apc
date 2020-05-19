package apc

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewClient(t *testing.T) {
	c, err := NewClient("localhost:22700", DevelopmentLogger())
	require.NoError(t, err)

	go c.Start()

	assert.NoError(t, c.Logon("testuser", "12345"))

	jobs, err := c.ListJobs(JobTypeAll)
	assert.NoError(t, err)
	_ = jobs

	assert.NoError(t, c.Logoff())
}
