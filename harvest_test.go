package harvest

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

var testUsername = os.Getenv("HV_TEST_USERNAME")
var testPassword = os.Getenv("HV_TEST_PASSWORD")

func TestFetchInvoices(t *testing.T) {
	if testUsername == "" || testPassword == "" {
		t.SkipNow()
	}

	assert := assert.New(t)

	hv, err := New(testUsername, testPassword)
	assert.NoError(err)
	assert.NotNil(hv)

	inv, err := hv.FetchInvoices()
	assert.NoError(err)
	assert.True(len(inv) > 0)

	/*
		for _, i := range inv {
			log.Printf("%s (%s) -> %s (%s -> %s)", i.Number, i.Customer.Name, i.State, i.IssueDate, i.SentAt)
		}
	*/

	i := inv[0]
	assert.True(len(i.LineItems) > 0)

	r, err := hv.GetRecipients(i.Customer.ID)
	assert.NoError(err)
	assert.True(len(r) > 0)
}
