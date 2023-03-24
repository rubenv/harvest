package harvest

import (
	"bytes"
	"io"
	"log"
	"os"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
)

var testAccountID = os.Getenv("HV_TEST_ACCOUNTID")
var testToken = os.Getenv("HV_TEST_TOKEN")

func TestFetchInvoices(t *testing.T) {
	if testAccountID == "" || testToken == "" {
		t.SkipNow()
	}

	assert := assert.New(t)

	accountID, err := strconv.ParseInt(testAccountID, 10, 64)
	assert.NoError(err)
	assert.True(accountID > 0)

	hv, err := New(accountID, testToken)
	assert.NoError(err)
	assert.NotNil(hv)

	inv, err := hv.FetchInvoices()
	assert.NoError(err)
	assert.True(len(inv) > 0)

	for _, i := range inv {
		log.Printf("%s (%s) -> %s (%s -> %s)", i.Number, i.Customer.Name, i.State, i.IssueDate, i.SentAt)
	}

	i := inv[0]
	assert.True(len(i.LineItems) > 0)

	r, err := hv.GetRecipients(i.Customer.ID)
	assert.NoError(err)
	assert.True(len(r) > 0)

	rc, err := i.Download()
	assert.NoError(err)
	assert.NotNil(rc)
	defer rc.Close()

	data, err := io.ReadAll(rc)
	assert.NoError(err)
	assert.True(bytes.HasPrefix(data, []byte("%PDF")))

	a, err := i.GetAttachments()
	assert.NoError(err)
	assert.True(len(a) > 0)

	rc, err = a[0].Download()
	assert.NoError(err)
	assert.NotNil(rc)
	defer rc.Close()

	data, err = io.ReadAll(rc)
	assert.NoError(err)
	assert.True(bytes.HasPrefix(data, []byte("%PDF")))
}
