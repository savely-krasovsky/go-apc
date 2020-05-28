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
	assert.NoError(t, client.Logon("GerasimE", "12345"))
}

func TestClient_ReserveHeadset(t *testing.T) {
	assert.NoError(t, client.ReserveHeadset(71656))
}

func TestClient_ConnHeadset(t *testing.T) {
	assert.NoError(t, client.ConnectHeadset())
}

func TestClient_ListJobs(t *testing.T) {
	jobs, err := client.ListJobs(JobTypeAll)
	require.NoError(t, err)

	_ = jobs
}

func TestClient_AttachJob(t *testing.T) {
	assert.NoError(t, client.AttachJob("TEST_JOB"))
}

/*
func TestClient_ListCallLists(t *testing.T) {
	lists, err := client.ListCallLists()
	require.NoError(t, err)

	_ = lists
}

func TestClient_AGTListCallFields(t *testing.T) {
	fields, err := client.ListCallFields("list1")
	require.NoError(t, err)

	_ = fields
}

func TestClient_ListDataFields(t *testing.T) {
	fields, err := client.ListDataFields(ListTypeOutbound)
	require.NoError(t, err)

	_ = fields
}

func TestClient_SetNotifyKeyField(t *testing.T) {
	assert.NoError(t, client.SetNotifyKeyField(ListTypeOutbound, "DEBT_ID"))
}

func TestClient_SetDataField(t *testing.T) {
	assert.NoError(t, client.SetDataField(ListTypeOutbound, "FIO"))
	assert.NoError(t, client.SetDataField(ListTypeOutbound, "TSUMPAY"))
	assert.NoError(t, client.SetDataField(ListTypeOutbound, "PHONE_ID1"))
}

func TestClient_AvailWork(t *testing.T) {
	assert.NoError(t, client.AvailWork())
}

func TestClient_ReadyNextItem(t *testing.T) {
	assert.NoError(t, client.ReadyNextItem())

	time.Sleep(10 * time.Minute)
}
*/

func TestClient_ListKeys(t *testing.T) {
	keys, err := client.ListKeys()
	assert.NoError(t, err)

	_ = keys
}

func TestClient_DetachJob(t *testing.T) {
	assert.NoError(t, client.DetachJob())
}

func TestClient_DisconnHeadset(t *testing.T) {
	assert.NoError(t, client.DisconnectHeadset())
}

func TestClient_FreeHeadset(t *testing.T) {
	assert.NoError(t, client.FreeHeadset())
}

func TestClient_Logoff(t *testing.T) {
	assert.NoError(t, client.Logoff())
}
