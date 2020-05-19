package apc

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var client *Client

func TestMain(m *testing.M) {
	c, err := NewClient("localhost:22700", DevelopmentLogger())
	if err != nil {
		return
	}
	client = c

	go c.Start()
	os.Exit(m.Run())
}

func TestClient_Logon(t *testing.T) {
	assert.NoError(t, client.Logon("testuser", "12345"))
}

func TestClient_ListJobs(t *testing.T) {
	jobs, err := client.ListJobs(JobTypeAll)
	require.NoError(t, err)

	_ = jobs
}

func TestClient_Logoff(t *testing.T) {
	assert.NoError(t, client.Logoff())
}
