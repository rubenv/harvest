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

	i := inv[2]
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
}

func TestFetchExpenses(t *testing.T) {
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

	exp, err := hv.FetchExpenses()
	assert.NoError(err)
	assert.True(len(exp) > 0)

	for _, e := range exp {
		log.Printf("%s (%s) -> %s (%g)", e.SpentDate, e.Project.Name, e.Notes, e.TotalCost)
	}
}

func TestIterInvoices(t *testing.T) {
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

	total := 0
	found := false
	for inv, err := range hv.Invoices() {
		assert.NoError(err)
		total += 1
		log.Printf("%s (%s) -> %s (%s -> %s)", inv.Number, inv.Customer.Name, inv.State, inv.IssueDate, inv.SentAt)
		assert.NotNil(inv.Hv)

		if inv.ID == 38903909 {
			found = true

			a, err := inv.GetAttachments()
			assert.NoError(err)
			assert.True(len(a) > 0)

			rc, err := a[0].Download()
			assert.NoError(err)
			assert.NotNil(rc)
			defer rc.Close()

			data, err := io.ReadAll(rc)
			assert.NoError(err)
			assert.True(bytes.HasPrefix(data, []byte("%PDF")))
		}
	}
	assert.True(found)
}
