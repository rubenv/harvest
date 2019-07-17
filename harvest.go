package harvest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"time"

	"golang.org/x/text/language"
	"golang.org/x/text/message"
	"golang.org/x/text/number"
)

const serverUrl = "https://api.harvestapp.com/api/v2"

const invoiceMessage = `
---------------------------------------------
Invoice Summary
---------------------------------------------
Invoice ID: %s
Issue date: %s
Client: %s
Amount: %s
Due: %s

The detailed invoice is attached as a PDF.

Thank you!
---------------------------------------------
`

type Client struct {
	username string
	password string

	client *http.Client
}

type Invoice struct {
	ID         int64     `json:"id"`
	Number     string    `json:"number"`
	State      string    `json:"state"`
	SentAt     time.Time `json:"sent_at"`
	PaidAt     time.Time `json:"paid_at"`
	IssueDate  string    `json:"issue_date"`
	DueDate    string    `json:"due_date"`
	Customer   *Customer `json:"client"`
	Amount     float64   `json:"amount"`
	Tax        float64   `json:"tax"`
	TaxAmount  float64   `json:"tax_amount"`
	Tax2       float64   `json:"tax2"`
	Tax2Amount float64   `json:"tax2_amount"`

	hv *Client `json:"-"`
}

type Customer struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type Recipient struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

func New(username, password string) (*Client, error) {
	cookieJar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}

	client := &http.Client{
		Jar: cookieJar,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	return &Client{
		username: username,
		password: password,
		client:   client,
	}, nil
}

func (hv *Client) FetchInvoices() ([]*Invoice, error) {
	req, err := http.NewRequest("GET", serverUrl+"/invoices", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Harvest-Account-ID", hv.username)
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", hv.password))

	resp, err := hv.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Failed to load invoices: %d", resp.StatusCode)
	}

	var r struct {
		Invoices []*Invoice `json:"invoices"`
	}

	err = json.NewDecoder(resp.Body).Decode(&r)
	if err != nil {
		return nil, err
	}

	for _, inv := range r.Invoices {
		inv.hv = hv
	}

	return r.Invoices, nil
}

func (hv *Client) GetRecipients(customer int64) ([]*Recipient, error) {
	req, err := http.NewRequest("GET", fmt.Sprintf("%s/contacts?client_id=%d", serverUrl, customer), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Harvest-Account-ID", hv.username)
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", hv.password))

	resp, err := hv.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Failed to load invoices: %d", resp.StatusCode)
	}

	var r struct {
		Contacts []struct {
			Email     string `json:"email"`
			FirstName string `json:"first_name"`
			LastName  string `json:"last_name"`
		} `json:"contacts"`
	}

	err = json.NewDecoder(resp.Body).Decode(&r)
	if err != nil {
		return nil, err
	}

	result := make([]*Recipient, 0)
	for _, c := range r.Contacts {
		result = append(result, &Recipient{
			Email: c.Email,
			Name:  fmt.Sprintf("%s %s", c.FirstName, c.LastName),
		})
	}
	return result, nil
}

type createMessageRequest struct {
	Recipients  []*Recipient `json:"recipients"`
	SendCopy    bool         `json:"send_me_a_copy"`
	IncludeLink bool         `json:"include_link_to_client_invoice"`
	AttachPDF   bool         `json:"attach_pdf"`
	Subject     string       `json:"subject"`
	Body        string       `json:"body"`
}

func (i *Invoice) Send(to []*Recipient) error {
	p := message.NewPrinter(language.Dutch)
	amount := p.Sprintf("€ %v", number.Decimal(i.Amount))

	body := fmt.Sprintf(invoiceMessage, i.Number, i.IssueDate, i.Customer.Name, amount, i.DueDate)

	data, err := json.Marshal(createMessageRequest{
		Recipients:  to,
		SendCopy:    true,
		IncludeLink: true,
		AttachPDF:   true,
		Subject:     fmt.Sprintf("Invoice #%s from Rocketeer Comm.V.", i.Number),
		Body:        body,
	})
	if err != nil {
		return err
	}

	url := fmt.Sprintf("%s/invoices/%d/messages", serverUrl, i.ID)
	req, err := http.NewRequest("POST", url, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-type", "application/json")
	req.Header.Set("Harvest-Account-ID", i.hv.username)
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", i.hv.password))

	resp, err := i.hv.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("Failed to send invoice: %d", resp.StatusCode)
	}

	return nil
}
