package harvest

import (
	"os"
	"testing"

	"github.com/cheekybits/is"
)

var testUsername = os.Getenv("HV_TEST_USERNAME")
var testPassword = os.Getenv("HV_TEST_PASSWORD")

func TestFetchInvoices(t *testing.T) {
	if testUsername == "" || testPassword == "" {
		t.SkipNow()
	}

	is := is.New(t)

	hv, err := New(testUsername, testPassword)
	is.NoErr(err)
	is.NotNil(hv)

	inv, err := hv.FetchInvoices()
	is.NoErr(err)
	is.True(len(inv) > 0)

	/*
		for _, i := range inv {
			log.Printf("%s (%s) -> %s (%s -> %s)", i.Number, i.Customer.Name, i.State, i.IssueDate, i.SentAt)
		}
	*/

	i := inv[0]
	is.True(len(i.LineItems) > 0)

	r, err := hv.GetRecipients(i.Customer.ID)
	is.NoErr(err)
	is.True(len(r) > 0)
}
